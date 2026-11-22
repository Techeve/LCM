package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"LCM/internal/control"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/netfilter"
)

const tlsUsage = "verwendung: lcm [-data <verzeichnis>] tls reload|status"

// runTLSCommand führt `lcm tls reload` bzw. `lcm tls status` gegen den
// laufenden Dienst aus und liefert den Exit-Code.
func runTLSCommand(dataDir string, args []string, out, errOut io.Writer) int {
	if len(args) != 1 || (args[0] != "reload" && args[0] != "status") {
		fmt.Fprintln(errOut, tlsUsage)
		return 2
	}
	socket, err := controlSocketPath(dataDir, packagedDataDir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if args[0] == "reload" {
		return tlsReload(socket, out, errOut)
	}
	return tlsStatus(socket, out, errOut)
}

func tlsReload(socket string, out, errOut io.Writer) int {
	var report reloadReport
	if err := control.Call(socket, cmdTLSReload, &report); err != nil {
		fmt.Fprintln(errOut, callErrorText(socket, err))
		return 1
	}
	printReloadReport(out, report)
	if !report.OK() {
		return 1
	}
	return 0
}

func tlsStatus(socket string, out, errOut io.Writer) int {
	var status statusReport
	if err := control.Call(socket, cmdTLSStatus, &status); err != nil {
		fmt.Fprintln(errOut, callErrorText(socket, err))
		return 1
	}
	printStatusReport(out, status)
	return 0
}

// controlSocketPath bestimmt die Steuerbuchse wie der Dienst sein
// Datenverzeichnis: -data, sonst LCM_DATA. Ohne beides gilt auf einem
// installierten System die Buchse der Paket-Installation, sonst das
// Verzeichnis der Programmdatei. Ein Fehler beim Prüfen der Paket-Buchse
// (etwa fehlende Berechtigung für /var/lib/lcm) zählt als „vorhanden“: Dann
// sagt der Verbindungsversuch, woran es liegt.
func controlSocketPath(dataDir, packagedDir string) (string, error) {
	if dataDir == "" {
		dataDir = os.Getenv("LCM_DATA")
	}
	if dataDir != "" {
		return control.SocketPath(dataDir), nil
	}
	packaged := control.SocketPath(packagedDir)
	if _, err := os.Stat(packaged); !errors.Is(err, fs.ErrNotExist) {
		return packaged, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return control.SocketPath(filepath.Dir(exe)), nil
}

// callErrorText macht aus einem Fehler des Aufrufs einen Satz für Menschen.
func callErrorText(socket string, err error) string {
	var remote control.RemoteError
	switch {
	case errors.Is(err, fs.ErrPermission):
		return fmt.Sprintf("keine Berechtigung für die Steuerbuchse %s - mit sudo aufrufen", socket)
	case errors.Is(err, control.ErrUnreachable):
		return fmt.Sprintf("LCM ist über %s nicht erreichbar: Der Dienst läuft nicht, "+
			"oder dieser Benutzer darf die Steuerbuchse nicht öffnen - dann mit sudo aufrufen", socket)
	case errors.As(err, &remote):
		return "LCM lehnt den Befehl ab: " + remote.Error()
	default:
		return err.Error()
	}
}

// sourceLabel ist die Quelle eines Zertifikats in der Ausgabe.
func sourceLabel(s tlsx.Source) string {
	switch s {
	case tlsx.SourceConfig:
		return "config.json"
	case tlsx.SourceUpload:
		return "Upload"
	case tlsx.SourceACME:
		return "ACME"
	case tlsx.SourceLCMCA:
		return "LCM-Zertifikat (Rückfall)"
	}
	return string(s)
}

func validUntil(c *tlsx.CertStatus) string {
	text := c.NotAfter.UTC().Format(time.DateOnly)
	if c.Expired {
		text += " (abgelaufen)"
	}
	return text
}

// shortFingerprint kürzt den SHA-256-Fingerprint auf 16 Zeichen.
func shortFingerprint(fp string) string {
	if len(fp) <= 16 {
		return fp
	}
	return fp[:16] + "…"
}

func printReloadReport(w io.Writer, r reloadReport) {
	fmt.Fprintf(w, "Zertifikate neu geladen: %d geladen, %d abgelehnt.\n", len(r.Certificates)-r.Rejected(), r.Rejected())
	if len(r.Certificates) == 0 {
		fmt.Fprintln(w, "Keine eigenen Zertifikate eingetragen - ausgeliefert wird das LCM-Zertifikat.")
	}
	for _, c := range r.Certificates {
		printReloadResult(w, c)
	}
	for _, p := range r.Problems {
		fmt.Fprintf(w, "Problem: %s\n", p)
	}
}

func printReloadResult(w io.Writer, c certResult) {
	if c.Error != "" {
		fmt.Fprintf(w, "  abgelehnt  %s  %s - %s\n", c.ID, c.Error, keptText(c.Kept))
	} else {
		fmt.Fprintf(w, "  geladen    %s  %s  gültig bis %s\n", c.ID, strings.Join(c.Cert.Names, ", "), validUntil(c.Cert))
	}
	if c.Cert == nil {
		return
	}
	for _, warning := range c.Cert.Warnings {
		fmt.Fprintf(w, "             Warnung: %s\n", warning)
	}
}

func keptText(kept bool) string {
	if kept {
		return "bisheriges bleibt aktiv"
	}
	return "es gibt kein bisheriges Zertifikat"
}

func printStatusReport(w io.Writer, s statusReport) {
	fmt.Fprintln(w, "Zertifikate, in der Reihenfolge, in der sie gewählt werden:")
	for _, c := range s.Certificates {
		fmt.Fprintf(w, "\n%s - %s\n", c.ID, sourceLabel(c.Source))
		fmt.Fprintf(w, "  Namen:        %s\n", strings.Join(c.Names, ", "))
		fmt.Fprintf(w, "  Aussteller:   %s\n", c.Issuer)
		fmt.Fprintf(w, "  Gültig bis:   %s\n", validUntil(&c))
		fmt.Fprintf(w, "  Fingerprint:  %s\n", shortFingerprint(c.Fingerprint))
		for _, warning := range c.Warnings {
			fmt.Fprintf(w, "  Warnung:      %s\n", warning)
		}
	}
	fmt.Fprintln(w, "\nÖffentliche Adressen:")
	for _, e := range s.Endpoints {
		printEndpoint(w, e)
	}
	printSharedPort(w, s.SharedPort)
}

func printSharedPort(w io.Writer, shared *sharedStatus) {
	if shared == nil {
		_, _ = fmt.Fprintln(w, "\nGemeinsamer Port: aus (Adressmodus „Ein Port“ in den Einstellungen oder shared_tls_listen in der config.json)")
		return
	}
	state := "aktiv"
	if !shared.Listening {
		state = "nicht geöffnet - die Meldung steht im Log"
	}
	fmt.Fprintf(w, "\nGemeinsamer Port %s (%s): Weboberfläche, Agent und MCP nach dem angefragten Namen\n", shared.Address, state)
	for _, r := range shared.Routes {
		printSharedRoute(w, r)
	}
	fmt.Fprintf(w, "  %-14s auch ohne Namen und für jeden anderen Namen\n", netfilter.EndpointUI.Label())
}

func printSharedRoute(w io.Writer, r sharedRoute) {
	label := netfilter.Endpoint(r.Service).Label()
	switch {
	case r.Host == "":
		fmt.Fprintf(w, "  %-14s keine öffentliche Adresse eingetragen - über den Namen nicht erreichbar\n", label)
	case r.Inactive:
		fmt.Fprintf(w, "  %-14s %s - ausgeschaltet, der Name führt zur Weboberfläche\n", label, r.Host)
	case r.ShadowedBy != "":
		fmt.Fprintf(w, "  %-14s %s - derselbe Name wie %s, die Verbindungen gehen dorthin\n", label, r.Host, netfilter.Endpoint(r.ShadowedBy).Label())
	default:
		fmt.Fprintf(w, "  %-14s %s\n", label, r.Host)
	}
}

func printEndpoint(w io.Writer, e endpointStatus) {
	label := netfilter.Endpoint(e.Name).Label()
	if e.Cert == nil {
		fmt.Fprintf(w, "  %-14s keine öffentliche Adresse eingetragen - ausgeliefert wird das LCM-Zertifikat\n", label)
		return
	}
	fmt.Fprintf(w, "  %-14s %s\n", label, e.URL)
	if e.Cert.Source == tlsx.SourceLCMCA {
		fmt.Fprintf(w, "  %-14s für %s passt kein eigenes Zertifikat: ausgeliefert wird das LCM-Zertifikat\n", "", e.Host)
		return
	}
	fmt.Fprintf(w, "  %-14s für %s wird %s (%s) ausgeliefert\n", "", e.Host, e.Cert.ID, sourceLabel(e.Cert.Source))
}
