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
	"errors"
	"fmt"
	"log/slog"
	"os"

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
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
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
		cfg, err := agent.LoadConfig(agent.ConfigPath)
		if err != nil {
			return fmt.Errorf("keine gültige konfiguration - zuerst `lcm-agent enroll <url> <token>` ausführen (%w)", err)
		}
		log.Info("lcm-agent starting", "version", version.String(), "server", cfg.URL)
		return agent.NewClient(cfg, log, version.String()).Run()
	case "uninstall":
		return agent.Uninstall()
	case "version", "-v", "--version":
		fmt.Println("lcm-agent " + version.String())
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf(i18n.T("unknown command %q", "unbekanntes Kommando %q"), os.Args[1])
	}
}

func usage() {
	fmt.Println(i18n.T(`lcm-agent - LCM Remote Agent

Usage:
  lcm-agent enroll <lcm-server-url> <token>   Set up the agent and start it as a service
  lcm-agent run                               Service loop (used by systemd)
  lcm-agent uninstall                         Remove the agent from this system
  lcm-agent version                           Show version`,
		`lcm-agent - LCM Remote Agent

Verwendung:
  lcm-agent enroll <lcm-server-url> <token>   Agent einrichten und als Dienst starten
  lcm-agent run                               Dienst-Schleife (nutzt systemd)
  lcm-agent uninstall                         Agent von diesem System entfernen
  lcm-agent version                           Version anzeigen`))
}
