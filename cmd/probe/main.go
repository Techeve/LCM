// Command lcm-probe misst den LCM-Host und die Datenbank, schreibt jede
// Messung sofort dauerhaft weg und wertet die Tagesdateien selbst aus - auf
// dem Host oder auf jedem Rechner, auf den man sie kopiert hat.
//
//	lcm-probe run                       Messen (systemd-Dienst)
//	lcm-probe report [--since 7d]       Textbericht
//	lcm-probe report --html datei.html  Bericht mit Diagrammen
//	lcm-probe query --from --to         Zeilen als CSV oder JSON
//	lcm-probe check                     lesbare Quellen und ihre Herkunft
package main

import (
	"fmt"
	"os"

	"LCM/internal/version"
)

const defaultDir = "/var/lib/lcm-probe"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "run":
		err = runCommand(args)
	case "report":
		err = reportCommand(args)
	case "query":
		err = queryCommand(args)
	case "check":
		err = checkCommand(args)
	case "version", "--version":
		fmt.Println("lcm-probe", version.String())
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unbekannter Befehl %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lcm-probe:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `lcm-probe - Messung des LCM-Hosts und der Datenbank

  lcm-probe run       [--dir D] [--url U] [--key-file F]   messen (Dienst)
  lcm-probe report    [--dir D] [--since 7d | --from T --to T] [--html datei.html]
  lcm-probe query     [--dir D] --from T --to T [--type host|db|...] [--fields a,b] [--format csv|json]
  lcm-probe check     lesbare Quellen und ob Host- oder Containerwert
  lcm-probe version

Zeiten: 2026-11-08T04:00, "2026-11-08 04:00", 04:00 (heute) oder relativ wie 90m, 6h, 7d.
`)
}
