package storage

import (
	"path/filepath"
	"testing"
)

// Schema von linux_users und linux_user_ssh_keys, wie es vor den
// Berechtigungsprofilen aussah: ohne default_profile_id und ohne den
// Fremdschluessel darauf.
const altSchema = `
CREATE TABLE ` + "`linux_users`" + ` (
  ` + "`id`" + ` integer PRIMARY KEY AUTOINCREMENT,
  ` + "`created_at`" + ` datetime, ` + "`updated_at`" + ` datetime,
  ` + "`username`" + ` text NOT NULL, ` + "`full_name`" + ` text, ` + "`email`" + ` text,
  ` + "`shell`" + ` text DEFAULT "/bin/bash",
  ` + "`sudo`" + ` numeric DEFAULT false, ` + "`active`" + ` numeric DEFAULT true,
  ` + "`password_enc`" + ` text, ` + "`username_bidx`" + ` text);
CREATE UNIQUE INDEX idx_linux_users_username_bidx ON linux_users(username_bidx);
CREATE TABLE ` + "`linux_user_ssh_keys`" + ` (
  ` + "`id`" + ` integer PRIMARY KEY AUTOINCREMENT,
  ` + "`created_at`" + ` datetime, ` + "`updated_at`" + ` datetime,
  ` + "`linux_user_id`" + ` integer NOT NULL, ` + "`name`" + ` text NOT NULL,
  ` + "`public_key`" + ` text NOT NULL,
  CONSTRAINT ` + "`fk_linux_users_ssh_keys`" + ` FOREIGN KEY (` + "`linux_user_id`" + `)
    REFERENCES ` + "`linux_users`" + `(` + "`id`" + `));
`

// TestMigrateBestandsdatenbank deckt den Fall ab, den alle uebrigen Tests
// nicht sehen: das Update einer BESTEHENDEN Datenbank.
//
// Kommt an einem Modell ein Fremdschluessel dazu, baut GORM die Tabelle unter
// SQLite neu und loescht dabei die alte. Zeigt eine andere Tabelle mit Zeilen
// darauf, scheiterte dieses DROP TABLE bei scharfer Pruefung - LCM startete
// nach dem Update nicht mehr. Auf einer leeren Datenbank passiert das nie,
// weil die Tabelle gleich mit Constraint entsteht.
//
// Bewusst eine Datei-Datenbank: Bei ":memory:" gibt es ohnehin nur eine
// Verbindung, und die Pruefung gilt pro Verbindung. Nur so deckt der Test
// auch ab, dass die Migration ihre Verbindung zusammenhaelt.
func TestMigrateBestandsdatenbank(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "bestand.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range split(altSchema) {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("alt-schema anlegen: %v", err)
		}
	}
	// Ohne Kindzeile keine Constraint-Verletzung - sie ist der Kern des Falls.
	if err := db.Exec(`INSERT INTO linux_users (id, username, username_bidx) VALUES (1, 'x', 'x')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO linux_user_ssh_keys (linux_user_id, name, public_key)
		VALUES (1, 'laptop', 'ssh-ed25519 AAAA')`).Error; err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migration einer bestandsdatenbank scheiterte: %v", err)
	}

	// Die neue Spalte ist da ...
	var spalten []struct {
		Name string `gorm:"column:name"`
	}
	if err := db.Raw("PRAGMA table_info(linux_users)").Scan(&spalten).Error; err != nil {
		t.Fatal(err)
	}
	if !enthaelt(spalten, "default_profile_id") {
		t.Error("default_profile_id fehlt nach der Migration")
	}

	// ... und der Umbau hat die Kindzeile nicht verloren.
	var key int64
	if err := db.Raw(`SELECT count(*) FROM linux_user_ssh_keys WHERE linux_user_id = 1`).Scan(&key).Error; err != nil {
		t.Fatal(err)
	}
	if key != 1 {
		t.Errorf("ssh-schluessel nach der migration: %d, erwartet 1", key)
	}

	// Keine verwaisten Zeilen - und die Pruefung ist wieder scharf.
	var verletzungen []struct {
		Table string `gorm:"column:table"`
	}
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&verletzungen).Error; err != nil {
		t.Fatal(err)
	}
	if len(verletzungen) != 0 {
		t.Errorf("verwaiste zeilen nach der migration: %v", verletzungen)
	}
	var scharf int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&scharf).Error; err != nil {
		t.Fatal(err)
	}
	if scharf != 1 {
		t.Error("fremdschluessel-pruefung blieb nach der migration ausgesetzt")
	}
}

func split(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		cur += string(r)
		if r == ';' {
			out = append(out, cur)
			cur = ""
		}
	}
	return out
}

func enthaelt(spalten []struct {
	Name string `gorm:"column:name"`
}, name string) bool {
	for _, s := range spalten {
		if s.Name == name {
			return true
		}
	}
	return false
}
