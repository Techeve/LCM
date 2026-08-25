package i18n

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestPackagingFilesAreASCII bewacht eine Zusage, die sich sonst leise
// verliert: Alles, was das Paket an Text ausliefert, muss reines ASCII sein.
//
// Diese Dateien werden bei `apt install`, in `systemctl status` und im Journal
// angezeigt - also in Terminals, Logdateien und CI-Ausgaben, deren
// Zeichenkodierung wir nicht kontrollieren. Ein Umlaut wird dort schnell zu
// unleserlichem Zeichensalat, und niemand bemerkt es beim Schreiben des Codes,
// weil der Editor UTF-8 kann.
//
// Der Test liegt in diesem Paket, weil hier die Regel „Ausgaben sind ASCII"
// zu Hause ist (siehe ASCII/T). Die deutschen Texte in den Paketskripten
// werden bewusst direkt mit ue/ae/oe/ss geschrieben - sie laufen nicht durch
// die Go-Umwandlung.
// Geprüft werden genau die Dateien, die MIT DEM PAKET AUSGELIEFERT werden und
// deren Text ein Anwender zu sehen bekommt: die dpkg-Maintainer-Skripte und
// die systemd-Units. Die übrigen Skripte in packaging/ (Release-Vorbereitung,
// Veröffentlichung ins Repository) sind Entwickler- bzw. CI-Werkzeuge, laufen
// nie auf einem Zielsystem und fallen deshalb nicht unter diese Regel.
func TestPackagingFilesAreASCII(t *testing.T) {
	shipped := []string{
		filepath.Join("..", "..", "packaging", "scripts"), // dpkg-Maintainer-Skripte
		filepath.Join("..", "..", "packaging"),            // systemd-Units (nur *.service)
	}
	var checked int

	walk := func(root string, wantExt map[string]bool) error {
		return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				// Nicht in Unterverzeichnisse absteigen - scripts/ wird
				// separat und mit eigener Endungsliste geprüft.
				if path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if !wantExt[filepath.Ext(path)] {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			return checkASCII(t, path, data)
		})
	}

	if err := walk(shipped[0], map[string]bool{".sh": true}); err != nil {
		t.Fatalf("maintainer-skripte durchlaufen: %v", err)
	}
	if err := walk(shipped[1], map[string]bool{".service": true}); err != nil {
		t.Fatalf("systemd-units durchlaufen: %v", err)
	}

	// Aus packaging/repo-server/ faellt genau eine Datei unter diese Regel:
	// setup-enterprise.sh laeuft auf KUNDENSYSTEMEN (per curl vom
	// Repository-Server geholt) und gibt dort Text aus. Die uebrigen Dateien
	// dort betreiben unseren eigenen Repository-Server und sind wie die
	// CI-Werkzeuge ausgenommen.
	customerFacing := filepath.Join("..", "..", "packaging", "repo-server", "setup-enterprise.sh")
	data, err := os.ReadFile(customerFacing)
	if err != nil {
		t.Fatalf("kundenseitiges setup-skript lesen: %v", err)
	}
	checked++
	if err := checkASCII(t, customerFacing, data); err != nil {
		t.Fatalf("ascii-pruefung: %v", err)
	}
	// Schutz davor, dass der Test durch einen Pfad-/Umbenennungsfehler still
	// nichts mehr prüft und trotzdem grün bleibt.
	if checked < 8 {
		t.Fatalf("nur %d Paketdateien geprueft - erwartet werden mindestens 8 "+
			"(6 Maintainer-Skripte + 2 systemd-Units); stimmt der Pfad noch?", checked)
	}
}

// checkASCII meldet das erste Nicht-ASCII-Zeichen einer Datei mit Zeilennummer.
func checkASCII(t *testing.T, path string, data []byte) error {
	t.Helper()
	for i, b := range data {
		if b < 128 {
			continue
		}
		// Für die Fehlermeldung: Zeile und das beanstandete Zeichen.
		line := strings.Count(string(data[:i]), "\n") + 1
		r, _ := utf8.DecodeRune(data[i:])
		t.Errorf("%s:%d enthaelt Nicht-ASCII-Zeichen %q - "+
			"Paket- und Dienstausgaben muessen reines ASCII sein "+
			"(deutsche Texte mit ue/ae/oe/ss schreiben)",
			filepath.ToSlash(path), line, r)
		return nil // pro Datei genügt der erste Treffer
	}
	return nil
}
