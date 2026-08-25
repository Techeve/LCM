// Kommando upgradecheck prüft, ob ein Upgrade über viele Versionen die Daten
// vollständig lässt.
//
// Hintergrund: Am 20.08.2026 startete LCM nach einem Update nicht mehr, weil
// eine Migration nur auf einer BESTEHENDEN Datenbank zuschlägt. Die gesamte
// Testsuite lief grün - sie arbeitet ausnahmslos gegen frische Datenbanken.
//
//	upgradecheck erfassen -db pfad/app.db -version 1.11.0 -out vorher.json
//	upgradecheck vergleichen -vorher vorher.json -nachher nachher.json \
//	    -erwartungen packaging/upgrade-test/erwartungen.json
//
// Rückgabe von "vergleichen": 0 wenn jede Abweichung erklärt ist, sonst 1.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Verwendung: upgradecheck erfassen|vergleichen [Optionen]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "erfassen":
		err = befehlErfassen(os.Args[2:])
	case "vergleichen":
		err = befehlVergleichen(os.Args[2:])
	default:
		err = fmt.Errorf("unbekannter Befehl %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FEHLER:", err)
		os.Exit(2)
	}
}

func befehlErfassen(args []string) error {
	fs := flag.NewFlagSet("erfassen", flag.ExitOnError)
	db := fs.String("db", "", "Pfad zur SQLite-Datei")
	version := fs.String("version", "", "Version, die diesen Stand erzeugt hat")
	out := fs.String("out", "", "Zieldatei (JSON)")
	_ = fs.Parse(args)
	if *db == "" || *out == "" {
		return fmt.Errorf("-db und -out sind Pflicht")
	}
	inv, err := erfasse(*db, *version)
	if err != nil {
		return err
	}
	roh, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, roh, 0o644); err != nil {
		return err
	}
	fmt.Printf("Bestandsaufnahme %s: %d Tabellen, %d Tabellen mit Identitäten\n",
		*version, len(inv.Tabellen), len(inv.Identitaet))
	return nil
}

func befehlVergleichen(args []string) error {
	fs := flag.NewFlagSet("vergleichen", flag.ExitOnError)
	beforePath := fs.String("vorher", "", "Bestandsaufnahme vor dem Upgrade")
	afterPath := fs.String("nachher", "", "Bestandsaufnahme nach dem Upgrade")
	expectedPath := fs.String("erwartungen", "", "Datei mit erklärten Abweichungen")
	_ = fs.Parse(args)

	vorher, err := ladeInventar(*beforePath)
	if err != nil {
		return err
	}
	nachher, err := ladeInventar(*afterPath)
	if err != nil {
		return err
	}
	var erwartungen []Erwartung
	if *expectedPath != "" {
		roh, err := os.ReadFile(*expectedPath)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(roh, &erwartungen); err != nil {
			return fmt.Errorf("erwartungen lesen: %w", err)
		}
	}

	befunde := vergleiche(vorher, nachher, erwartungen)
	fmt.Printf("Upgrade %s -> %s\n", vorher.Version, nachher.Version)
	fmt.Printf("  Tabellen: %d -> %d\n", len(vorher.Tabellen), len(nachher.Tabellen))

	var unerklaert int
	for _, b := range befunde {
		if b.Erklaert {
			fmt.Printf("  ok      %-22s %s\n            %s\n", b.Tabelle, b.Text, b.Grund)
			continue
		}
		unerklaert++
		fmt.Printf("  FEHLER  %-22s %s\n", b.Tabelle, b.Text)
	}
	if unerklaert == 0 {
		fmt.Println("  Keine unerklärte Abweichung - die Daten sind vollständig.")
		return nil
	}
	fmt.Printf("\n%d unerklärte Abweichung(en).\n", unerklaert)
	fmt.Println("Ist die Änderung gewollt, gehört sie in die Erwartungsdatei -")
	fmt.Println("mit Version, Art und Begründung. Sonst ist hier Datenverlust.")
	os.Exit(1)
	return nil
}

func ladeInventar(path string) (*Inventar, error) {
	if path == "" {
		return nil, fmt.Errorf("-vorher und -nachher sind Pflicht")
	}
	roh, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inv Inventar
	if err := json.Unmarshal(roh, &inv); err != nil {
		return nil, fmt.Errorf("%s lesen: %w", path, err)
	}
	return &inv, nil
}
