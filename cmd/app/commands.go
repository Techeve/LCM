package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"LCM/internal/core/services"
	"LCM/internal/perms"
)

// subcommand beschreibt ein Unterkommando für die Hilfe. Versteckte Kommandos
// (hidden) ruft nur das Paket auf; sie stehen nicht in der Hilfe.
type subcommand struct {
	name   string
	usage  string
	desc   string
	hidden bool
}

var subcommands = []subcommand{
	{name: "rotate-db-key", usage: "rotate-db-key", desc: "Master-Key rotieren und alle verschlüsselten Felder neu verschlüsseln"},
	{name: "tls", usage: "tls reload|status", desc: "Zertifikate des laufenden Dienstes neu laden bzw. anzeigen"},
	{name: "network", usage: "network status|reset", desc: "erlaubte Netzbereiche anzeigen bzw. nach einer Aussperrung zurücksetzen"},
	{name: "credentials", usage: "credentials init", desc: "Master-Key in ein systemd-Credential überführen (--with-key auto|host|tpm2|host+tpm2)"},
	{name: "help", usage: "help", desc: "diese Hilfe"},
	{name: "restricted-provision", hidden: true},
}

// cliOptions sind die globalen Schalter, die Unterkommandos brauchen.
type cliOptions struct {
	configPath string
	dataDir    string
}

// exitUsage ist der Exit-Code bei unbekanntem Kommando.
const exitUsage = 2

// runSubcommand führt ein Unterkommando aus, wenn args eines nennt. handled
// ist false, wenn keine Argumente da sind - dann startet der Server. Ein
// unbekanntes Wort startet den Server NIE: `lcm help` oder ein Tippfehler
// würde sonst mit dem Standard-Datenverzeichnis einen Dienst anlegen.
func runSubcommand(args []string, opts cliOptions, stdout, stderr io.Writer) (code int, handled bool) {
	if len(args) == 0 {
		return 0, false
	}
	switch args[0] {
	case "help":
		printUsage(stdout)
		return 0, true
	case "rotate-db-key":
		return reportError(rotateDBKey(opts.configPath, opts.dataDir), stderr), true
	case "restricted-provision":
		// Versteckt: gibt das Skript aus, das Helper, sudoers-Whitelist und
		// Shims des eingeschränkten Modus schreibt (postinstall.sh).
		user := ""
		if len(args) > 1 {
			user = args[1]
		}
		script, err := services.RestrictedProvisionScript(user)
		if err != nil {
			return reportError(err, stderr), true
		}
		fmt.Fprint(stdout, script)
		return 0, true
	case "tls":
		return runTLSCommand(opts.dataDir, args[1:], stdout, stderr), true
	case "network":
		return runNetworkCommand(opts.dataDir, args[1:], stdout, stderr), true
	case "credentials":
		if len(args) < 2 || args[1] != "init" {
			fmt.Fprintln(stderr, "verwendung: lcm credentials init [--with-key auto|host|tpm2|host+tpm2]")
			return 1, true
		}
		return reportError(credentialsInit(opts.configPath, opts.dataDir, args[2:]), stderr), true
	}
	fmt.Fprintf(stderr, "lcm: unbekanntes Kommando %q\n\n", args[0])
	printUsage(stderr)
	return exitUsage, true
}

func reportError(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	fmt.Fprintln(stderr, "lcm:", err)
	return 1
}

// printUsage listet Kommandos (ohne versteckte) und Schalter.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Verwendung: lcm [Schalter] [Kommando]")
	fmt.Fprintln(w, "\nOhne Kommando startet der Server. Kommandos:")
	for _, c := range subcommands {
		if !c.hidden {
			fmt.Fprintf(w, "  lcm %-22s %s\n", c.usage, c.desc)
		}
	}
	fmt.Fprintln(w, "\nSchalter:")
	flag.CommandLine.SetOutput(w)
	flag.PrintDefaults()
}

// dataDirRefusedError erklärt, warum ein Systemverzeichnis nicht als
// Datenverzeichnis taugt, und nennt den richtigen Aufruf. Bei einer
// Paket-Installation (packagedConfigPath vorhanden) zeigt der Text auf deren
// Aufruf und den Dienst.
func dataDirRefusedError(dir string, packaged bool) error {
	msg := fmt.Sprintf("%s ist ein Systemverzeichnis und kann kein Datenverzeichnis sein - "+
		"das Datenverzeichnis mit -data angeben, z.B. `lcm -data /var/lib/lcm`", dir)
	if packaged {
		msg += fmt.Sprintf("; diese Maschine hat eine Paket-Installation (%s): der Dienst läuft als "+
			"`lcm -config %s -data %s` (Status: `systemctl status lcm`)",
			packagedConfigPath, packagedConfigPath, packagedDataDir)
	}
	return fmt.Errorf("%s", msg)
}

// checkDataDir lehnt Systemverzeichnisse als Datenverzeichnis ab.
func checkDataDir(dir string) error {
	if !perms.IsSystemDir(dir) {
		return nil
	}
	_, err := os.Stat(packagedConfigPath)
	return dataDirRefusedError(dir, err == nil)
}
