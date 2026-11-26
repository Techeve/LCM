package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/acmex"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
	"LCM/internal/storage/repositories"
)

func TestBackupCreateBundlesAndEncrypts(t *testing.T) {
	dataDir := t.TempDir()
	// Master-Key + Config, die mitgesichert werden sollen.
	if err := os.WriteFile(filepath.Join(dataDir, crypto.KeyFileName), []byte("master-key-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"jwt_secret":"streng-geheim"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// CA und Server-Zertifikat gehören dazu: Ohne die CA vertrauten die
	// Agenten einem zurückgespielten LCM nach der nächsten Erneuerung nicht.
	tlsxtest.ServerCert(t, dataDir, time.Now().Add(-time.Hour))
	uploadID := saveTestUpload(t, dataDir)
	acmeDir := filepath.Join(dataDir, "tls", "acme")
	if err := os.MkdirAll(filepath.Join(acmeDir, "certs", "0a1b2c3d"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"account.key", "certs/0a1b2c3d/cert.pem", "certs/0a1b2c3d/key.pem"} {
		if err := os.WriteFile(filepath.Join(acmeDir, f), []byte("acme"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dbFile := filepath.Join(dataDir, "app.db")
	db, err := gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Backup{}, &domain.GlobalSettings{}); err != nil {
		t.Fatal(err)
	}
	bs := NewBackupService(repositories.NewDatabaseRepository(db), repositories.NewSettingsRepository(db), dataDir, dbFile, configPath)

	// Ohne Passphrase (kein Env) muss Create klar fehlschlagen.
	if _, err := bs.Create("test", ""); !errors.Is(err, ErrBackupNoPassphrase) {
		t.Fatalf("ohne Passphrase erwartet ErrBackupNoPassphrase, bekam %v", err)
	}

	b, err := bs.Create("test", "Korrekt-Pferd-Batterie-42")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if filepath.Ext(b.FileName) != BackupExt {
		t.Errorf("Endung = %q, erwartet %s", filepath.Ext(b.FileName), BackupExt)
	}

	// Archiv einlesen und entschlüsseln.
	blob, err := os.ReadFile(filepath.Join(dataDir, "backups", b.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openEncryptedArchive(blob, "falsch"); err == nil {
		t.Error("falsche Passphrase sollte fehlschlagen")
	}
	files, err := openEncryptedArchive(blob, "Korrekt-Pferd-Batterie-42")
	if err != nil {
		t.Fatalf("öffnen: %v", err)
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f.Name] = true
	}
	for _, want := range []string{"app.db", crypto.KeyFileName, "config.json",
		tlsx.CertFileName, tlsx.KeyFileName, tlsx.CAFileName, tlsx.CAKeyFileName,
		tlsx.UploadBackupPrefix + uploadID + "-cert.pem", tlsx.UploadBackupPrefix + uploadID + "-key.pem",
		acmex.BackupPrefix + "account.key", acmex.BackupPrefix + "cert-0a1b2c3d-cert.pem", acmex.BackupPrefix + "cert-0a1b2c3d-key.pem"} {
		if !names[want] {
			t.Errorf("Archiv fehlt %q (enthält: %v)", want, names)
		}
	}
}

// saveTestUpload legt ein hochgeladenes Zertifikat im Datenverzeichnis ab.
func saveTestUpload(t *testing.T, dataDir string) string {
	t.Helper()
	pairDir := t.TempDir()
	tlsxtest.ForeignCert(t, pairDir, time.Now().AddDate(0, 1, 0))
	certPEM, err := os.ReadFile(filepath.Join(pairDir, tlsx.CertFileName))
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(pairDir, tlsx.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	id, err := tlsx.SaveUpload(dataDir, certPEM, keyPEM, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestBackupDelete prüft das Löschen: Datei + Metadaten weg, unbekannte Namen
// und Pfad-Traversal werden abgelehnt.
func TestBackupDelete(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, crypto.KeyFileName), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"jwt_secret":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dbFile := filepath.Join(dataDir, "app.db")
	db, err := gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Backup{}, &domain.GlobalSettings{}); err != nil {
		t.Fatal(err)
	}
	bs := NewBackupService(repositories.NewDatabaseRepository(db), repositories.NewSettingsRepository(db), dataDir, dbFile, configPath)

	b, err := bs.Create("test", "Korrekt-Pferd-Batterie-42")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := filepath.Join(dataDir, "backups", b.FileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Datei sollte existieren: %v", err)
	}

	// Pfad-Traversal / falsche Endung abgelehnt.
	if err := bs.Delete("../etc/passwd"); !errors.Is(err, ErrBackupNotFound) {
		t.Errorf("Traversal: erwartet ErrBackupNotFound, bekam %v", err)
	}
	// Unbekannter Name.
	if err := bs.Delete("nicht-da.lcmbak"); !errors.Is(err, ErrBackupNotFound) {
		t.Errorf("unbekannt: erwartet ErrBackupNotFound, bekam %v", err)
	}

	// Echtes Löschen: Datei UND Metadaten weg.
	if err := bs.Delete(b.FileName); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Datei sollte gelöscht sein, stat-err=%v", err)
	}
	list, err := bs.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("Metadaten sollten weg sein, sind aber %d", len(list))
	}
}

// TestResolvePassphraseAusEinstellungen (R2-027): Das GEPLANTE Backup (leerer
// Parameter) muss die in den Einstellungen hinterlegte, verschlüsselte
// Passphrase verwenden - Rangfolge: Parameter → Umgebung → Einstellungen.
func TestResolvePassphraseAusEinstellungen(t *testing.T) {
	t.Setenv(EnvBackupPassphrase, "")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.GlobalSettings{}); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewSettingsRepository(db)
	cipher, _ := crypto.NewCipher(make([]byte, 32))
	enc, _ := cipher.EncryptString("gespeicherte-passphrase")
	if err := repo.Save(&domain.GlobalSettings{BackupPassphraseEnc: enc}); err != nil {
		t.Fatal(err)
	}
	bs := NewBackupService(repositories.NewDatabaseRepository(db), repo, t.TempDir(), ":memory:", "").WithCipher(cipher)

	// Ohne Parameter und ohne Umgebung: die gespeicherte greift.
	if p, err := bs.resolvePassphrase(""); err != nil || p != "gespeicherte-passphrase" {
		t.Fatalf("gespeicherte Passphrase nicht verwendet: %q / %v", p, err)
	}
	// Parameter hat Vorrang.
	if p, _ := bs.resolvePassphrase("direkt"); p != "direkt" {
		t.Errorf("Parameter muss Vorrang haben, bekam %q", p)
	}
	// Umgebung schlägt die gespeicherte.
	t.Setenv(EnvBackupPassphrase, "aus-der-umgebung")
	if p, _ := bs.resolvePassphrase(""); p != "aus-der-umgebung" {
		t.Errorf("Umgebungsvariable muss Vorrang vor der gespeicherten haben, bekam %q", p)
	}
	// Ohne alles: ehrlicher Fehler.
	t.Setenv(EnvBackupPassphrase, "")
	bs2 := NewBackupService(repositories.NewDatabaseRepository(db), repo, t.TempDir(), ":memory:", "").WithCipher(cipher)
	_ = repo.UpdateFields(map[string]any{"backup_passphrase_enc": ""})
	if _, err := bs2.resolvePassphrase(""); !errors.Is(err, ErrBackupNoPassphrase) {
		t.Errorf("ohne jede Passphrase: erwartet ErrBackupNoPassphrase, bekam %v", err)
	}
}
