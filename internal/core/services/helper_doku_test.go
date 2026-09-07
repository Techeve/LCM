package services

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// lcmDateiName findet die Dateien, die der Helper unter eigenem Namen auf dem
// Server anlegt - LCM-eigene Dateien tragen "lcm" im Namen.
var lcmDateiName = regexp.MustCompile(`[0-9A-Za-z_-]*lcm[0-9A-Za-z._-]*\.(?:conf|list|nft|pref)`)

// TestJedeVomHelperGeschriebeneDateiStehtInDerDoku hält die Anleitung
// „Was LCM auf dem Server ändert" (internal/appdocs) an dem fest, was der
// Helper tatsächlich schreibt.
//
// Warum als Test und nicht als Vorsatz: Eine Anleitung zum Rückgängigmachen
// ist wertlos, sobald sie eine Datei nicht kennt - wer ihr folgt, hält den
// Server für sauber und übersieht die eine Änderung, die noch wirkt. Genau das
// gab es schon: Beim Portwechsel schreibt der Helper auf Systemen mit
// Socket-Aktivierung zusätzlich ein Drop-in für die Socket-Unit. Wer nur das
// sshd-Drop-in löscht, lässt den Server weiter auf dem alten Port lauschen.
//
// Umbenannte oder neue Dateien fallen damit beim Bauen auf, nicht beim
// Aufräumen eines fremden Servers.
func TestJedeVomHelperGeschriebeneDateiStehtInDerDoku(t *testing.T) {
	helfer, err := os.ReadFile("lcm_helper.go")
	if err != nil {
		t.Fatal(err)
	}
	gefunden := map[string]bool{}
	for _, name := range lcmDateiName.FindAllString(string(helfer), -1) {
		gefunden[name] = true
	}
	if len(gefunden) == 0 {
		t.Fatal("keine LCM-Dateinamen im Helper gefunden - der Ausdruck passt nicht mehr")
	}

	for _, seite := range []string{
		"../../appdocs/pages/de/60-serveraenderungen.md",
		"../../appdocs/pages/en/60-serveraenderungen.md",
	} {
		doku, err := os.ReadFile(seite)
		if err != nil {
			t.Fatalf("%s: %v", seite, err)
		}
		var fehlen []string
		for name := range gefunden {
			if !strings.Contains(string(doku), name) {
				fehlen = append(fehlen, name)
			}
		}
		sort.Strings(fehlen)
		if len(fehlen) > 0 {
			t.Errorf("%s nennt diese vom Helper geschriebenen Dateien nicht: %v\n"+
				"Wer der Anleitung folgt, hielte den Server danach für unverändert.", seite, fehlen)
		}
	}
}
