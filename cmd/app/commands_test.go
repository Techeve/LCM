package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnbekanntesKommandoStartetNichtDenServer: Das war der Auslöser: `lcm help`
// fiel durch bis zum Serverstart und legte Dateien in /usr/bin an.
func TestUnbekanntesKommandoStartetNichtDenServer(t *testing.T) {
	for _, arg := range []string{"help-me", "hilfe", "start", "Help", "--"} {
		var out, errOut bytes.Buffer
		code, handled := runSubcommand([]string{arg}, cliOptions{}, &out, &errOut)
		if !handled || code != exitUsage {
			t.Errorf("%q: handled=%v code=%d, erwartet handled und Exit %d", arg, handled, code, exitUsage)
		}
		if !strings.Contains(errOut.String(), "unbekanntes Kommando") || !strings.Contains(errOut.String(), "rotate-db-key") {
			t.Errorf("%q: Fehlermeldung ohne Kommandoliste: %q", arg, errOut.String())
		}
		if out.Len() != 0 {
			t.Errorf("%q: stdout muss leer bleiben, war %q", arg, out.String())
		}
	}
}

func TestKeinKommandoStartetDenServer(t *testing.T) {
	code, handled := runSubcommand(nil, cliOptions{}, &bytes.Buffer{}, &bytes.Buffer{})
	if handled || code != 0 {
		t.Errorf("ohne Argumente muss der Server starten: handled=%v code=%d", handled, code)
	}
}

func TestHelpZeigtKommandosOhneVersteckte(t *testing.T) {
	var out, errOut bytes.Buffer
	code, handled := runSubcommand([]string{"help"}, cliOptions{}, &out, &errOut)
	if !handled || code != 0 || errOut.Len() != 0 {
		t.Fatalf("help: handled=%v code=%d stderr=%q", handled, code, errOut.String())
	}
	for _, want := range []string{"rotate-db-key", "tls", "network", "credentials"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Hilfe ohne %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "restricted-provision") {
		t.Error("restricted-provision muss versteckt bleiben")
	}
}

func TestVersteckteKommandosSindBekannt(t *testing.T) {
	var out, errOut bytes.Buffer
	_, handled := runSubcommand([]string{"restricted-provision", "root"}, cliOptions{}, &out, &errOut)
	if !handled || strings.Contains(errOut.String(), "unbekanntes Kommando") {
		t.Errorf("restricted-provision gilt als unbekannt: %q", errOut.String())
	}
}

func TestCredentialsOhneInitMeldetVerwendung(t *testing.T) {
	var errOut bytes.Buffer
	code, handled := runSubcommand([]string{"credentials"}, cliOptions{}, &bytes.Buffer{}, &errOut)
	if !handled || code != 1 || !strings.Contains(errOut.String(), "credentials init") {
		t.Errorf("handled=%v code=%d stderr=%q", handled, code, errOut.String())
	}
}

// TestDatenverzeichnisSystemverzeichnisAbgelehnt: ein Symlink auf ein
// Systemverzeichnis zählt wie das Verzeichnis selbst.
func TestDatenverzeichnisSystemverzeichnisAbgelehnt(t *testing.T) {
	t.Setenv("LCM_DATA", "")
	for _, dir := range []string{"/", "/usr/bin", "/bin", "/etc", "/usr/local/bin", "/usr/lib"} {
		if _, err := resolveDataDir(dir); err == nil {
			t.Errorf("%s wurde als Datenverzeichnis akzeptiert", dir)
		}
	}
	link := filepath.Join(t.TempDir(), "daten")
	if err := os.Symlink("/etc", link); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveDataDir(link); err == nil {
		t.Error("Symlink auf /etc wurde akzeptiert")
	}
	ok := filepath.Join(t.TempDir(), "lcm")
	if got, err := resolveDataDir(ok); err != nil || got != ok {
		t.Errorf("normales Verzeichnis: %q, %v", got, err)
	}
}

func TestDatenverzeichnisFehlerMitPaketHinweis(t *testing.T) {
	plain := dataDirRefusedError("/usr/bin", false).Error()
	if !strings.Contains(plain, "-data /var/lib/lcm") || strings.Contains(plain, "systemctl") {
		t.Errorf("ohne Paket: %q", plain)
	}
	packaged := dataDirRefusedError("/usr/bin", true).Error()
	for _, want := range []string{"-data /var/lib/lcm", "-config /etc/lcm/config.json", "systemctl status lcm"} {
		if !strings.Contains(packaged, want) {
			t.Errorf("Paket-Hinweis ohne %q: %q", want, packaged)
		}
	}
}
