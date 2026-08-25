package storage

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
)

// TestFieldEncryptionRoundtrip prüft, dass serializer-getaggte Felder
// (Job.Output) at rest verschlüsselt liegen, aber transparent im Klartext
// gelesen werden - und dass die Master-Key-Rotation sie mitnimmt.
func TestFieldEncryptionRoundtrip(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	cipher, _ := crypto.NewCipher(crypto.GenerateKey())
	SetFieldCipher(cipher)
	defer SetFieldCipher(nil) // globalen Serializer-State nicht in andere Tests lecken

	const secret = "root@host:~# cat /etc/shadow\nsuper-geheim"
	job := &domain.Job{Type: "custom", Name: "t", Status: domain.JobStatusSuccess, Output: secret}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}

	// Rohspalte muss Chiffretext sein (nicht der Klartext).
	var raw string
	if err := db.Raw("SELECT output FROM jobs WHERE id = ?", job.ID).Scan(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if raw == secret || raw == "" {
		t.Fatalf("Rohspalte ist nicht verschlüsselt: %q", raw)
	}

	// Über GORM gelesen muss der Klartext herauskommen.
	var got domain.Job
	if err := db.First(&got, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Output != secret {
		t.Fatalf("entschlüsselter Output = %q, erwartet %q", got.Output, secret)
	}

	// Master-Key-Rotation: neu verschlüsseln, dann mit neuem Cipher lesen.
	newCipher, _ := crypto.NewCipher(crypto.GenerateKey())
	if err := RotateEncryptedFields(db, cipher, newCipher); err != nil {
		t.Fatalf("rotation: %v", err)
	}
	SetFieldCipher(newCipher)
	var afterRot domain.Job
	if err := db.First(&afterRot, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterRot.Output != secret {
		t.Fatalf("nach Rotation Output = %q, erwartet %q", afterRot.Output, secret)
	}
}

// TestServerHostEncryption prüft, dass Host/IPAddresses eines Servers at rest
// verschlüsselt liegen, aber im Klartext gelesen werden (der Verbindungspfad
// nutzt den entschlüsselten Host). Rotation nimmt sie mit.
func TestServerHostEncryption(t *testing.T) {
	db, _ := Open(":memory:")
	_ = Migrate(db)
	cipher, _ := crypto.NewCipher(crypto.GenerateKey())
	SetFieldCipher(cipher)
	defer SetFieldCipher(nil)

	srv := &domain.Server{
		Name: "web01", Host: "10.10.0.11", ServiceUser: "lcm-svc",
		HostKeyFingerprint: "SHA256:x", PrivateKeyEnc: "x", IPAddresses: "10.10.0.11,fe80::1",
	}
	if err := db.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	var rawHost string
	db.Raw("SELECT host FROM servers WHERE id = ?", srv.ID).Scan(&rawHost)
	if rawHost == "10.10.0.11" || rawHost == "" {
		t.Fatalf("host-Rohspalte ist nicht verschlüsselt: %q", rawHost)
	}
	var got domain.Server
	if err := db.First(&got, srv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Host != "10.10.0.11" || got.IPAddresses != "10.10.0.11,fe80::1" {
		t.Fatalf("entschlüsselt: host=%q ip=%q", got.Host, got.IPAddresses)
	}
	if got.Name != "web01" {
		t.Errorf("name entschlüsselt = %q, erwartet web01", got.Name)
	}
	// Name liegt at rest verschlüsselt vor; der Blindindex ist deterministisch
	// (für Eindeutigkeit/Suche) und NICHT der Klartext.
	var rawName, rawBidx string
	db.Raw("SELECT name FROM servers WHERE id = ?", srv.ID).Scan(&rawName)
	db.Raw("SELECT name_bidx FROM servers WHERE id = ?", srv.ID).Scan(&rawBidx)
	if rawName == "web01" || rawName == "" {
		t.Errorf("name-Rohspalte sollte verschlüsselt sein, ist %q", rawName)
	}
	if rawBidx == "web01" || rawBidx == "" {
		t.Errorf("name_bidx sollte der HMAC-Blindindex sein, ist %q", rawBidx)
	}
}

// TestFieldEncryptionLegacyPlaintext stellt sicher, dass VOR der Aktivierung
// geschriebener Klartext weiterhin lesbar ist (toleranter Fallback).
func TestFieldEncryptionLegacyPlaintext(t *testing.T) {
	db, _ := Open(":memory:")
	_ = Migrate(db)

	// Ohne gesetzten Cipher schreiben (Legacy-Klartext).
	SetFieldCipher(nil)
	job := &domain.Job{Type: "custom", Name: "t", Status: domain.JobStatusSuccess, Output: "klartext-legacy"}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}

	// Jetzt Cipher aktivieren und lesen - der nicht entschlüsselbare Klartext
	// muss unverändert zurückkommen, nicht den Lesevorgang brechen.
	cipher, _ := crypto.NewCipher(crypto.GenerateKey())
	SetFieldCipher(cipher)
	defer SetFieldCipher(nil)
	var got domain.Job
	if err := db.First(&got, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Output != "klartext-legacy" {
		t.Fatalf("Legacy-Klartext = %q, erwartet 'klartext-legacy'", got.Output)
	}
}
