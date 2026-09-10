package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// staleEnv baut einen BackupService mit eigenem Backup-Verzeichnis.
func staleEnv(t *testing.T) (*BackupService, string) {
	t.Helper()
	dataDir := t.TempDir()
	dbFile := filepath.Join(dataDir, "app.db")
	db, err := gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Backup{}, &domain.GlobalSettings{}); err != nil {
		t.Fatal(err)
	}
	bs := NewBackupService(db, repositories.NewSettingsRepository(db), dataDir, dbFile, "")
	dir, err := bs.backupDir()
	if err != nil {
		t.Fatal(err)
	}
	return bs, dir
}

// write legt eine Datei mit gewünschtem Alter an.
func write(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCleanStaleTempRaeumtResteWeg: Ein hart beendeter Sicherungslauf laesst
// die Momentaufnahme der Datenbank und ein halbfertiges Archiv liegen. Die
// Momentaufnahme ist NICHT verschluesselt - sie darf nicht liegen bleiben.
func TestCleanStaleTempRaeumtResteWeg(t *testing.T) {
	bs, dir := staleEnv(t)
	snap := write(t, dir, ".snap-20260812-055706.230.db", 30*24*time.Hour)
	part := write(t, dir, "lcm-backup-20260812-055706.lcmbak.part", 30*24*time.Hour)
	// Was bleiben MUSS: das fertige Archiv und alles Fremde.
	archiv := write(t, dir, "lcm-backup-20260909-051406.lcmbak", time.Hour)
	fremd := write(t, dir, "notiz.txt", time.Hour)

	if n := bs.CleanStaleTemp(0); n != 2 {
		t.Errorf("erwartet 2 entfernte Reste, bekam %d", n)
	}
	for _, p := range []string{snap, part} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("Rest liegt noch da: %s", p)
		}
	}
	for _, p := range []string{archiv, fremd} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("darf nicht angefasst werden: %s (%v)", p, err)
		}
	}
}

// TestCleanStaleTempSchontLaufendenLauf: Im laufenden Betrieb raeumt Prune
// mit einer Altersgrenze auf. Die Momentaufnahme einer gerade laufenden
// Sicherung ist frisch und muss ueberleben - sonst zerstoerte das Aufraeumen
// genau die Sicherung, die es schuetzen soll.
func TestCleanStaleTempSchontLaufendenLauf(t *testing.T) {
	bs, dir := staleEnv(t)
	frisch := write(t, dir, ".snap-20260909-221500.000.db", time.Minute)
	alt := write(t, dir, ".snap-20260812-055706.230.db", 30*24*time.Hour)

	if n := bs.CleanStaleTemp(staleTempAge); n != 1 {
		t.Errorf("erwartet 1 entfernten Rest, bekam %d", n)
	}
	if _, err := os.Stat(frisch); err != nil {
		t.Errorf("die Momentaufnahme eines laufenden Laufs wurde entfernt: %v", err)
	}
	if _, err := os.Stat(alt); !os.IsNotExist(err) {
		t.Error("der alte Rest liegt noch da")
	}
}

// TestPruneRaeumtResteMit: Prune laeuft nach jeder Sicherung - die Reste
// gehoeren dort mit weg, ohne dass jemand daran denken muss.
func TestPruneRaeumtResteMit(t *testing.T) {
	bs, dir := staleEnv(t)
	alt := write(t, dir, ".snap-20260812-055706.230.db", 30*24*time.Hour)

	bs.Prune(14)

	if _, err := os.Stat(alt); !os.IsNotExist(err) {
		t.Error("Prune hat den Rest liegen lassen")
	}
}

// TestBackupTraegtMasterKeyAuchOhneDatei: Nach der Umstellung auf ein
// systemd-Credential gibt es keine lcm.key im Datenverzeichnis. Das Archiv
// muss den Schlüssel trotzdem enthalten - sonst wäre die Sicherung auf einer
// anderen Maschine wertlos.
func TestBackupTraegtMasterKeyAuchOhneDatei(t *testing.T) {
	bs, _ := staleEnv(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	bs.WithMasterKey(key)

	b, err := bs.Create("test", "Korrekt-Pferd-Batterie-42")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path, err := bs.BackupPath(b.FileName)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	files, err := openEncryptedArchive(raw, "Korrekt-Pferd-Batterie-42")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Name == "lcm.key" {
			if string(f.Data) != string(crypto.KeyFileContent(key)) {
				t.Error("Master-Key im Archiv stimmt nicht mit dem Speicher überein")
			}
			return
		}
	}
	t.Error("Archiv enthält keinen Master-Key, obwohl er im Speicher lag")
}
