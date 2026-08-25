package services

import (
	"errors"
	"strings"
	"testing"
)

// TestRueckstellungNurAusDerKandidatenliste: Die Auswahl ist die
// Sicherheitsgrenze. Was der Scan nicht als rückstellbar ermittelt hat, darf
// auch über die API nicht in die Kommandozeile gelangen - sonst ließe sich
// jede beliebige Fremdquelle auf http herunterstufen.
func TestRueckstellungNurAusDerKandidatenliste(t *testing.T) {
	candidates := "https://deb.debian.org/debian,https://security.debian.org/debian-security"

	alle, err := revertTargets(candidates, nil)
	if err != nil || len(alle) != 2 {
		t.Fatalf("leere Auswahl müsste alle Kandidaten liefern: %v (%v)", alle, err)
	}
	eine, err := revertTargets(candidates, []string{"https://deb.debian.org/debian"})
	if err != nil || strings.Join(eine, ",") != "https://deb.debian.org/debian" {
		t.Errorf("Teilauswahl: %v (%v)", eine, err)
	}
	if _, err := revertTargets(candidates, []string{"https://download.docker.com/linux/debian"}); !errors.Is(err, ErrNotRevertible) {
		t.Errorf("fremde Quelle wurde nicht abgewiesen: %v", err)
	}
	if _, err := revertTargets("", nil); !errors.Is(err, ErrNoRevertCandidates) {
		t.Errorf("ohne Kandidaten erwartet ErrNoRevertCandidates, war %v", err)
	}
}

// TestRueckstellungRolltZurueck: Ohne funktionierendes apt-get update wäre der
// Server ohne Paketquellen - das Skript muss den Vorzustand wiederherstellen
// und mit Fehler enden, statt den Schaden stehen zu lassen.
func TestRueckstellungRolltZurueck(t *testing.T) {
	script := revertHTTPSScript([]string{"https://deb.debian.org/debian"})
	for _, part := range []string{
		`cp "$f" "$f.lcm-bak"`,                      // Sicherung vorher
		"if apt-get update; then",                   // Probe danach
		`mv "$f" "${f%.lcm-bak}"`,                   // Rückrollen
		"exit 1",                                    // und als Fehler melden
		`sed -i "s|$u|http://${u#https://}|g" "$f"`, // nur das Schema tauschen
	} {
		if !strings.Contains(script, part) {
			t.Errorf("im Skript fehlt %q:\n%s", part, script)
		}
	}
	// Genau die genannte URL, keine Pauschal-Ersetzung wie bei der Umstellung.
	if strings.Contains(script, "s|https://|http://|g") {
		t.Errorf("das Skript stellt pauschal um:\n%s", script)
	}
}

// TestUmstellungHebtDieSicherungAuf: Ohne aufgehobene Sicherung ließe sich
// später nicht mehr sagen, welche Quelle vorher http war - die Rückstellung
// müsste raten. Eine vorhandene Sicherung bleibt dabei stehen: die älteste ist
// der echte Vorzustand.
func TestUmstellungHebtDieSicherungAuf(t *testing.T) {
	if !strings.Contains(keepBackupsScript, httpsBackupDir) {
		t.Errorf("die Sicherung landet nicht im Protokoll:\n%s", keepBackupsScript)
	}
	if !strings.Contains(keepBackupsScript, `if [ -f "$dest" ]; then rm -f "$f"`) {
		t.Errorf("eine vorhandene Sicherung wird überschrieben:\n%s", keepBackupsScript)
	}
	if strings.Contains(keepBackupsScript, "-delete") {
		t.Errorf("die Sicherung wird noch gelöscht:\n%s", keepBackupsScript)
	}
}

// TestHelferKenntDieRueckstellung: Im eingeschränkten Modus läuft alles über
// den Helper. Fehlte das Unterkommando dort, wäre die Schaltfläche in der
// Oberfläche sichtbar, liefe aber ins Leere (Helfer-Parität, Etappe F).
func TestHelferKenntDieRueckstellung(t *testing.T) {
	script := lcmHelperScript
	for _, part := range []string{
		"repos-http)",
		"repos_http() {",
		"valid_repo_url",
		httpsBackupDir,
	} {
		if !strings.Contains(script, part) {
			t.Errorf("im Helper fehlt %q", part)
		}
	}
	// Der Helper prüft die URLs selbst - er ist die Grenze, nicht der Aufrufer.
	if !strings.Contains(script, `for u in "$@"; do valid_repo_url "$u" || die`) {
		t.Error("der Helper prüft die übergebenen URLs nicht")
	}
}
