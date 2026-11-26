// lcm-agent ist der LCM-Remote-Agent: er läuft als systemd-Dienst auf dem
// verwalteten Server und verbindet sich AUSGEHEND per MQTT-über-WebSocket
// mit dem LCM-Server - für Server hinter NAT, ohne feste IP oder unterwegs.
//
// Einrichtung mit genau zwei Parametern (Adresse + Enrollment-Token aus dem
// LCM-Onboarding):
//
//	sudo lcm-agent enroll https://lcm.example.com:8443 lcma1.…
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"LCM/internal/agent"
	"LCM/internal/i18n"
	"LCM/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("Error:", "Fehler:"), err)
		os.Exit(1)
	}
}

func run() error {
	level := new(slog.LevelVar) // info, bis `run` die Stufe aus Schalter und Konfiguration kennt
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	if len(os.Args) < 2 {
		usage()
		return errors.New(i18n.T("no command given", "kein Kommando angegeben"))
	}
	switch os.Args[1] {
	case "enroll":
		if len(os.Args) != 4 {
			return fmt.Errorf("verwendung: lcm-agent enroll <lcm-server-url> <token>")
		}
		return agent.Enroll(os.Args[2], os.Args[3], version.String(), log)
	case "run":
		return runCommand(os.Args[2:], log, level)
	case "status":
		return statusCommand(os.Args[2:], os.Stdout)
	case "terminal":
		return terminalCommand(os.Args[2:])
	case "uninstall":
		return agent.Uninstall()
	case "version", "-v", "--version":
		return versionCommand(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf(i18n.T("unknown command %q", "unbekanntes Kommando %q"), os.Args[1])
	}
}

// runCommand ist der Dienst: lcm-agent run [--log-level debug|info|warn].
func runCommand(args []string, log *slog.Logger, level *slog.LevelVar) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	logLevel := flags.String("log-level", "", "debug, info or warn (overrides log_level in agent.json)")
	if err := flags.Parse(args); err != nil {
		return errors.New(i18n.T("usage: lcm-agent run [--log-level debug|info|warn]", "verwendung: lcm-agent run [--log-level debug|info|warn]"))
	}
	cfg, err := agent.LoadConfig(agent.ConfigPath)
	if err != nil {
		return fmt.Errorf("keine gültige konfiguration - zuerst `lcm-agent enroll <url> <token>` ausführen (%w)", err)
	}
	agent.WarnLooseMode(agent.ConfigPath, log)
	parsed, levelErr := agent.ResolveLogLevel(*logLevel, cfg.LogLevel)
	level.Set(parsed)
	if levelErr != nil {
		log.Warn("log level not usable - using info", "error", levelErr)
	}
	log.Info("lcm-agent starting", "version", version.String(), "server", cfg.URL, "log_level", parsed.String())
	client := agent.NewClient(cfg, log, version.String()).WithConfigPath(agent.ConfigPath).WithStatusDir(agent.StatusDir)
	stopOnSignal(client, log)
	return client.Run()
}

// statusCommand liest die Zustandsdatei des laufenden Agents und gibt sie lesbar
// aus, mit --json roh. Ist die Datei alt oder fehlt sie, läuft der Agent
// vermutlich nicht: Das Kommando sagt es und endet mit einem Fehler.
func statusCommand(args []string, out io.Writer) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--json") {
		return errors.New(i18n.T("usage: lcm-agent status [--json]", "verwendung: lcm-agent status [--json]"))
	}
	return printStatus(agent.StatusDir, len(args) == 1, time.Now(), out)
}

func printStatus(dir string, asJSON bool, now time.Time, out io.Writer) error {
	status, raw, err := agent.ReadStatus(dir)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New(i18n.T("no status file - the agent does not seem to be running", "keine Zustandsdatei - der Agent scheint nicht zu laufen"))
	}
	if err != nil {
		return err
	}
	if asJSON {
		_, _ = out.Write(raw)
	} else {
		_, _ = io.WriteString(out, agent.FormatStatus(status, now))
	}
	if status.Stale(now) {
		return errors.New(i18n.T("the agent does not seem to be running", "der Agent scheint nicht zu laufen"))
	}
	return nil
}

// stopOnSignal trennt bei SIGTERM/SIGINT (systemctl stop, Strg-C) sauber von
// LCM, statt die Verbindung einfach abreißen zu lassen.
func stopOnSignal(client *agent.Client, log *slog.Logger) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-sig
		// Warn, damit die gewollte Trennung auch bei log_level warn im Journal steht.
		log.Warn("signal received - disconnecting from lcm", "reason", "signal "+s.String())
		client.Stop()
	}()
}

func usage() {
	fmt.Println(i18n.T(`lcm-agent - LCM Remote Agent

Usage:
  lcm-agent enroll <lcm-server-url> <token>   Set up the agent and start it as a service
  lcm-agent run [--log-level debug|info|warn] Service loop (used by systemd)
  lcm-agent status [--json]                   Show connection, jobs and update state of the running agent
  lcm-agent terminal on|off|status            Allow or forbid the web console via this agent
  lcm-agent uninstall                         Remove the agent from this system
  lcm-agent version [--json]                  Show version (--json: protocol, capabilities, pin, rollback state)`,
		`lcm-agent - LCM Remote Agent

Verwendung:
  lcm-agent enroll <lcm-server-url> <token>   Agent einrichten und als Dienst starten
  lcm-agent run [--log-level debug|info|warn] Dienst-Schleife (nutzt systemd)
  lcm-agent status [--json]                   Verbindung, Jobs und Update-Stand des laufenden Agents zeigen
  lcm-agent terminal on|off|status            Web-Konsole über diesen Agent erlauben oder verbieten
  lcm-agent uninstall                         Agent von diesem System entfernen
  lcm-agent version [--json]                  Version anzeigen (--json: Protokoll, Fähigkeiten, Pin, Rückfall-Stand)`))
}

// versionCommand zeigt die Version; mit --json alles, was das Update-Skript des
// Servers und der Support wissen wollen. Das Startskript (packaging/lcm-agent-start.sh)
// liest die erste Zeile "lcm-agent <version>" auch von älteren Binaries - sie
// bleibt unverändert.
func versionCommand(args []string) error {
	switch {
	case len(args) == 0:
		fmt.Println("lcm-agent " + version.String())
		return nil
	case len(args) == 1 && args[0] == "--json":
		out, err := json.MarshalIndent(agent.CollectVersionInfo("", version.Version, version.String()), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	return errors.New(i18n.T("usage: lcm-agent version [--json]", "verwendung: lcm-agent version [--json]"))
}

// terminalCommand schaltet die Web-Konsole über diesen Agent an oder ab:
// lcm-agent terminal on|off|status.
func terminalCommand(args []string) error {
	usageErr := errors.New(i18n.T("usage: lcm-agent terminal on|off|status", "verwendung: lcm-agent terminal on|off|status"))
	if len(args) != 1 {
		return usageErr
	}
	switch args[0] {
	case "status":
		if agent.TerminalDisabled(agent.ConfigDir) {
			fmt.Println(i18n.T("Web console: off", "Web-Konsole: aus"))
		} else {
			fmt.Println(i18n.T("Web console: on", "Web-Konsole: an"))
		}
		return nil
	case "on", "off":
		if err := agent.SetTerminal(agent.ConfigDir, args[0] == "on"); err != nil {
			return err
		}
		fmt.Println(i18n.T("Saved. Applies right away, open consoles end with the next input; LCM shows it after the next reconnect (now: systemctl restart lcm-agent).",
			"Gespeichert. Gilt sofort, offene Konsolen enden bei der nächsten Eingabe; LCM zeigt es nach dem nächsten Verbindungsaufbau an (sofort: systemctl restart lcm-agent)."))
		return nil
	}
	return usageErr
}
