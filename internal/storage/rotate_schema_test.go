package storage

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// TestRotationKenntJedeEncSpalteUndJedeServerRefTabelle liest aus dem Schema
// jede Spalte *_enc und jede Tabelle mit server_ref. Fehlt eine davon in der
// Rotation, bleibt sie nach `lcm rotate-db-key` mit dem alten Schlüssel liegen
// und ist unlesbar - so geschehen mit der Backup-Passphrase, beiden
// Abo-Schlüsseln und den Frühwarn-Befunden.
func TestRotationKenntJedeEncSpalteUndJedeServerRefTabelle(t *testing.T) {
	db, _ := setupEncDB(t)

	encRegistriert := map[string]bool{}
	for _, c := range encryptedColumns {
		encRegistriert[c.table+"."+c.column] = true
	}
	refRegistriert := map[string]bool{}
	for _, tabelle := range serverRefTables {
		refRegistriert[tabelle] = true
	}

	for _, tabelle := range schemaTabellen(t, db) {
		for _, s := range schemaSpalten(t, db, tabelle) {
			if strings.HasSuffix(s.Name, "_enc") && !encRegistriert[tabelle+"."+s.Name] {
				t.Errorf("%s.%s fehlt in encryptedColumns - die Schlüsselrotation ließe es zurück", tabelle, s.Name)
			}
			if s.Name == "server_ref" && !refRegistriert[tabelle] {
				t.Errorf("%s.server_ref fehlt in serverRefTables - die Zuordnung zum Server ginge bei der Rotation verloren", tabelle)
			}
		}
	}
}

// TestRotationErfasstEinstellungenUndFruehwarnBefunde: Die drei Schlüssel in
// den Einstellungen sind nach der Rotation lesbar, und ein Frühwarn-Befund
// hängt weiter an seinem Server.
func TestRotationErfasstEinstellungenUndFruehwarnBefunde(t *testing.T) {
	db, alt := setupEncDB(t)

	pem, err := alt.EncryptString("pem")
	if err != nil {
		t.Fatal(err)
	}
	server := &domain.Server{Name: "web01", Host: "10.0.0.1", ServiceUser: "lcm", HostKeyFingerprint: "f", PrivateKeyEnc: pem}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}
	befund := &domain.AdvisoryFinding{ServerRef: repositories.ServerRef(server.ID), Source: "osv",
		AdvisoryID: "CVE-2026-0001", Kind: "vulnerability", PackageName: "openssl"}
	if err := db.Create(befund).Error; err != nil {
		t.Fatal(err)
	}

	geheim := map[string]string{
		"backup_passphrase_enc":       "backup-passphrase",
		"subscription_key_enc":        "abo-schluessel",
		"subscription_access_key_enc": "abo-zugang",
	}
	settings := &domain.GlobalSettings{ID: 1}
	if err := db.FirstOrCreate(settings).Error; err != nil {
		t.Fatal(err)
	}
	for spalte, klartext := range geheim {
		enc, err := alt.EncryptString(klartext)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(settings).Update(spalte, enc).Error; err != nil {
			t.Fatal(err)
		}
	}

	neu, _ := crypto.NewCipher(crypto.GenerateKey())
	if err := RotateEncryptedFields(db, alt, neu); err != nil {
		t.Fatalf("Rotation: %v", err)
	}
	SetFieldCipher(neu)

	for spalte, klartext := range geheim {
		roh := rawCol(t, db, "global_settings", spalte, 1)
		if got, err := neu.DecryptString(roh); err != nil || got != klartext {
			t.Errorf("global_settings.%s nach der Rotation unlesbar: %q / %v", spalte, got, err)
		}
	}
	offen, err := repositories.NewAdvisoryRepository(db).ActiveForServer(server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(offen) != 1 {
		t.Errorf("Frühwarn-Befund hängt nach der Rotation nicht mehr am Server: %d statt 1", len(offen))
	}
}
