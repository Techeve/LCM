package storage

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// blockParamsProfile legt das Profil an, in das der Baustein eingehängt wird -
// ohne es greift der Fremdschlüssel der Verwendung.
func blockParamsProfile(t *testing.T, db *gorm.DB) uint {
	t.Helper()
	profile := domain.PrivilegeProfile{Name: "Web-Betrieb", Slug: "web-betrieb"}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	return profile.ID
}

// blockParamsDB baut eine Alt-Datenbank mit einem Baustein in deutscher
// Fassung - samt einer Verwendung, die ihn in ein Profil einhängt.
func blockParamsDB(t *testing.T, params, values string) (*domain.ProfileBlock, *domain.ProfileBlockUse) {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	block := domain.ProfileBlock{
		Name: "nginx betreiben", Slug: "nginx-betreiben", Params: params,
		Variants: []domain.ProfileBlockVariant{{
			Family:       domain.BlockFamilyAll,
			SudoCommands: "/usr/bin/systemctl --no-pager restart {dienst}",
			EditPaths:    "/etc/{dienst}/nginx.conf",
			PathRules:    "read /var/log/{dienst}",
		}},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatal(err)
	}
	use := domain.ProfileBlockUse{ProfileID: blockParamsProfile(t, db), BlockID: block.ID, Values: values}
	if err := db.Create(&use).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateBlockParamsToEnglish(db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var after domain.ProfileBlock
	if err := db.Preload("Variants").First(&after, block.ID).Error; err != nil {
		t.Fatal(err)
	}
	var useAfter domain.ProfileBlockUse
	if err := db.First(&useAfter, use.ID).Error; err != nil {
		t.Fatal(err)
	}
	return &after, &useAfter
}

// TestBlockParamsWanderVollstaendig ist die Zusage der Migration: Der Name
// wandert an allen drei Stellen zugleich. Bliebe die Verwendung zurück, hätte
// der Platzhalter keinen Wert mehr, die Regel fiele bei der Prüfung durch -
// und das Profil verlöre lautlos genau diese Rechte.
func TestBlockParamsWanderVollstaendig(t *testing.T) {
	block, use := blockParamsDB(t, "dienst", "dienst=nginx")

	if block.Params != "service" {
		t.Errorf("Parameterliste = %q, erwartet %q", block.Params, "service")
	}
	v := block.Variants[0]
	for feld, text := range map[string]string{
		"sudo_commands": v.SudoCommands, "edit_paths": v.EditPaths, "path_rules": v.PathRules,
	} {
		if !strings.Contains(text, "{service}") || strings.Contains(text, "{dienst}") {
			t.Errorf("%s wurde nicht umbenannt: %q", feld, text)
		}
	}
	if use.Values != "service=nginx" {
		t.Errorf("die Verwendung hält noch %q - der Wert erreicht den Platzhalter nicht mehr", use.Values)
	}

	// Der eingehängte Baustein muss danach wieder eine vollständige Regel
	// ergeben: Das ist der Punkt, an dem sich ein halber Umzug rächen würde.
	rendered := domain.SubstituteBlockParams(v.SudoCommands, domain.ParseBlockValues(use.Values))
	if strings.Contains(rendered, "{") {
		t.Errorf("die Regel hat noch einen offenen Platzhalter: %q", rendered)
	}
}

// TestBlockParamsLassenWerteInRuhe: Umbenannt wird nur der Name links vom
// Gleichheitszeichen. Ein Wert, der zufällig so heißt, geht die Migration
// nichts an.
func TestBlockParamsLassenWerteInRuhe(t *testing.T) {
	_, use := blockParamsDB(t, "dienst", "dienst=dienst")
	if use.Values != "service=dienst" {
		t.Errorf("Werte = %q, erwartet %q", use.Values, "service=dienst")
	}
}

// TestBlockParamsOhneKollision: Trägt ein selbst angelegter Baustein beide
// Namen, bleibt er unangetastet - zwei Platzhalter zu einem zu verschmelzen
// würde stillschweigend andere Regeln erzeugen.
func TestBlockParamsOhneKollision(t *testing.T) {
	block, use := blockParamsDB(t, "dienst,service", "dienst=nginx")
	if block.Params != "dienst,service" {
		t.Errorf("Parameterliste = %q, erwartet unverändert", block.Params)
	}
	if use.Values != "dienst=nginx" {
		t.Errorf("Werte = %q, erwartet unverändert", use.Values)
	}
}

// TestBlockParamsZweiterLaufAendertNichts: Die Migration läuft bei jedem
// Start mit.
func TestBlockParamsZweiterLaufAendertNichts(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	block := domain.ProfileBlock{
		Name: "nginx", Slug: "nginx", Params: "dienst",
		Variants: []domain.ProfileBlockVariant{{
			Family: domain.BlockFamilyAll, SudoCommands: "/usr/bin/systemctl restart {dienst}",
		}},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatal(err)
	}
	use := domain.ProfileBlockUse{ProfileID: blockParamsProfile(t, db), BlockID: block.ID, Values: "dienst=nginx"}
	if err := db.Create(&use).Error; err != nil {
		t.Fatal(err)
	}
	for lauf := 1; lauf <= 2; lauf++ {
		if err := migrateBlockParamsToEnglish(db); err != nil {
			t.Fatalf("lauf %d: %v", lauf, err)
		}
	}
	var after domain.ProfileBlockUse
	if err := db.First(&after, use.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Values != "service=nginx" {
		t.Errorf("nach zwei Läufen %q, erwartet %q", after.Values, "service=nginx")
	}
}

// TestAppCatalogPfadPlatzhalter: Derselbe Umzug im Anwendungskatalog - dort
// ist {pfad} fest eingebaut, nicht frei benannt.
func TestAppCatalogPfadPlatzhalter(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	entry := domain.AppCatalogEntry{
		Name: "Eigene App", Slug: "eigene-app", Markers: "path /opt/app/app",
		VersionCommand: "{pfad} --version", Compare: domain.CompareSemver,
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateBlockParamsToEnglish(db); err != nil {
		t.Fatalf("migration: %v", err)
	}
	var after domain.AppCatalogEntry
	if err := db.First(&after, entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.VersionCommand != "{path} --version" {
		t.Errorf("Versionskommando = %q, erwartet %q", after.VersionCommand, "{path} --version")
	}
}

// TestBlockParamsVorDemSeeden hält die Reihenfolge fest, an der diese
// Migration hängt. Das Seeden schreibt den mitgelieferten Bausteinen die neuen
// Parameternamen - die Verwendungen kennt es aber nicht. Liefe es zuerst,
// fände die Migration keinen deutschen Namen mehr und ließe die Werte liegen;
// der Platzhalter bliebe leer und das Profil verlöre die Rechte.
//
// Der Test baut den Zustand einer Alt-Installation nach: ein mitgelieferter
// Baustein in deutscher Fassung, eingehängt in ein Profil.
func TestBlockParamsVorDemSeeden(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	block := domain.ProfileBlock{
		Name: "Systemd-Dienst betreiben", Slug: "systemd-dienst", Params: "dienst", Builtin: true,
		Variants: []domain.ProfileBlockVariant{{
			Family: domain.BlockFamilyAll, SudoCommands: "/usr/bin/systemctl --no-pager restart {dienst}",
		}},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatal(err)
	}
	use := domain.ProfileBlockUse{
		ProfileID: blockParamsProfile(t, db), BlockID: block.ID, Values: "dienst=nginx",
	}
	if err := db.Create(&use).Error; err != nil {
		t.Fatal(err)
	}

	// Dieselbe Reihenfolge wie beim Start: erst Migrate, dann Seed.
	if err := migrateBlockParamsToEnglish(db); err != nil {
		t.Fatalf("migration: %v", err)
	}
	if err := ensureProfileBlocks(db); err != nil {
		t.Fatalf("seeden: %v", err)
	}

	var after domain.ProfileBlock
	if err := db.Preload("Variants").Where("slug = ?", "systemd-dienst").First(&after).Error; err != nil {
		t.Fatal(err)
	}
	var useAfter domain.ProfileBlockUse
	if err := db.First(&useAfter, use.ID).Error; err != nil {
		t.Fatal(err)
	}

	values := domain.ParseBlockValues(useAfter.Values)
	for _, cmd := range domain.BlockLines(after.Variants[0].SudoCommands) {
		rendered := domain.SubstituteBlockParams(cmd, values)
		if strings.Contains(rendered, "{") {
			t.Fatalf("offener Platzhalter nach Migration und Seeden: %q (Werte: %q)", rendered, useAfter.Values)
		}
	}
}
