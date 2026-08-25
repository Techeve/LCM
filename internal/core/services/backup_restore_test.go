package services

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// RestoreStaged meldet, ob ein anwendbares Staging vorliegt - reiner
// Test-Helfer (der Produktionspfad prüft den Marker in ApplyStagedRestore
// selbst, daher lebt die Funktion hier statt im Produktionscode).
func RestoreStaged(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, restoreStagingName, restoreMarker))
	return err == nil
}

func TestStageAndApplyRestore(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.json")
	// Vorhandene (alte) Dateien, die der Restore ersetzen soll.
	_ = os.WriteFile(filepath.Join(dataDir, crypto.KeyFileName), []byte("ALT-key"), 0o600)
	_ = os.WriteFile(configPath, []byte(`{"jwt_secret":"alt"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dataDir, "app.db"), []byte("ALT-db"), 0o600)

	// Ein Backup-Archiv mit neuen Inhalten bauen.
	archive, err := buildEncryptedArchive([]archiveFile{
		{Name: "app.db", Data: []byte("NEU-db-inhalt")},
		{Name: crypto.KeyFileName, Data: []byte("NEU-key")},
		{Name: "config.json", Data: []byte(`{"jwt_secret":"neues-secret-mit-mindestens-32-zeichen!"}`)},
	}, "pw")
	if err != nil {
		t.Fatal(err)
	}

	bs := NewBackupService(nil, nil, dataDir, "", configPath)

	// Falsche Passphrase → kein Staging.
	if err := bs.StageRestore(archive, "falsch"); !errors.Is(err, ErrBackupPassphrase) {
		t.Fatalf("falsche Passphrase: erwartet ErrBackupPassphrase, bekam %v", err)
	}
	if RestoreStaged(dataDir) {
		t.Fatal("nach fehlgeschlagenem Staging sollte nichts vorbereitet sein")
	}

	// Korrekt stagen.
	if err := bs.StageRestore(archive, "pw"); err != nil {
		t.Fatalf("StageRestore: %v", err)
	}
	if !RestoreStaged(dataDir) {
		t.Fatal("Staging sollte als bereit gemeldet werden")
	}
	// Vor dem Anwenden sind die alten Dateien noch unverändert.
	if b, _ := os.ReadFile(configPath); string(b) != `{"jwt_secret":"alt"}` {
		t.Error("config.json wurde vor Apply verändert")
	}

	// Anwenden.
	applied, err := ApplyStagedRestore(dataDir, configPath)
	if err != nil {
		t.Fatalf("ApplyStagedRestore: %v", err)
	}
	if !applied {
		t.Fatal("erwartete applied=true")
	}
	if b, _ := os.ReadFile(filepath.Join(dataDir, "app.db")); string(b) != "NEU-db-inhalt" {
		t.Errorf("app.db nicht wiederhergestellt: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dataDir, crypto.KeyFileName)); string(b) != "NEU-key" {
		t.Errorf("lcm.key nicht wiederhergestellt: %q", b)
	}
	if b, _ := os.ReadFile(configPath); string(b) == `{"jwt_secret":"alt"}` {
		t.Error("config.json nicht wiederhergestellt")
	}
	// Staging ist aufgeräumt, ein zweiter Apply ist ein No-op.
	if RestoreStaged(dataDir) {
		t.Error("Staging sollte nach Apply entfernt sein")
	}
	if applied, _ := ApplyStagedRestore(dataDir, configPath); applied {
		t.Error("ohne Staging sollte Apply false liefern")
	}
}

func TestBackupPathAndHistoryRestore(t *testing.T) {
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "app.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Backup{}, &domain.GlobalSettings{}); err != nil {
		t.Fatal(err)
	}
	bs := NewBackupService(db, repositories.NewSettingsRepository(db), dataDir,
		filepath.Join(dataDir, "app.db"), filepath.Join(dataDir, "config.json"))

	// Ein gültiges Archiv ins Backup-Verzeichnis legen.
	archive, err := buildEncryptedArchive([]archiveFile{{Name: "app.db", Data: []byte("db-inhalt")}}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	backupsDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "lcm-backup-20250101-000000.lcmbak"
	if err := os.WriteFile(filepath.Join(backupsDir, name), archive, 0o600); err != nil {
		t.Fatal(err)
	}

	// BackupPath: gültiger Name wird aufgelöst.
	if _, err := bs.BackupPath(name); err != nil {
		t.Fatalf("BackupPath(gültig): %v", err)
	}
	// BackupPath weist Traversal, falsche Endung und fehlende Datei ab.
	for _, bad := range []string{"../etc/passwd", "foo.txt", "gibtsnicht.lcmbak", ""} {
		if _, err := bs.BackupPath(bad); !errors.Is(err, ErrBackupNotFound) {
			t.Errorf("BackupPath(%q): erwartet ErrBackupNotFound, bekam %v", bad, err)
		}
	}

	// Aus der Historie stagen: falsche Passphrase → kein Staging.
	if err := bs.StageRestoreFromHistory(name, "falsch"); !errors.Is(err, ErrBackupPassphrase) {
		t.Errorf("history falsche Passphrase: erwartet ErrBackupPassphrase, bekam %v", err)
	}
	if RestoreStaged(dataDir) {
		t.Error("nach Fehlschlag sollte kein Staging vorliegen")
	}
	// Fehlendes Backup → ErrBackupNotFound.
	if err := bs.StageRestoreFromHistory("weg.lcmbak", "pw"); !errors.Is(err, ErrBackupNotFound) {
		t.Errorf("history fehlend: erwartet ErrBackupNotFound, bekam %v", err)
	}
	// Korrekt stagen.
	if err := bs.StageRestoreFromHistory(name, "pw"); err != nil {
		t.Fatalf("StageRestoreFromHistory: %v", err)
	}
	if !RestoreStaged(dataDir) {
		t.Error("Staging sollte bereit sein")
	}
}

func TestAutoRestartEnabled(t *testing.T) {
	// Ohne Env und ohne Settings-Zeile: Default false.
	bs := NewBackupService(nil, nil, t.TempDir(), "", "")
	if bs.AutoRestartEnabled() {
		t.Error("ohne Konfiguration sollte Auto-Restart aus sein")
	}
	// Env-Override hat Vorrang (truthy → an, alles andere → aus).
	t.Setenv(EnvRestoreAutoRestart, "true")
	if !bs.AutoRestartEnabled() {
		t.Error("LCM_RESTORE_AUTO_RESTART=true → an")
	}
	t.Setenv(EnvRestoreAutoRestart, "0")
	if bs.AutoRestartEnabled() {
		t.Error("LCM_RESTORE_AUTO_RESTART=0 → aus")
	}

	// Ohne Env entscheidet die gespeicherte Einstellung.
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "app.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.GlobalSettings{}); err != nil {
		t.Fatal(err)
	}
	settings := repositories.NewSettingsRepository(db)
	if err := settings.Save(&domain.GlobalSettings{RestoreAutoRestart: true}); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv(EnvRestoreAutoRestart)
	bs2 := NewBackupService(db, settings, dataDir, "", "")
	if !bs2.AutoRestartEnabled() {
		t.Error("Einstellung RestoreAutoRestart=true → an")
	}
}

// TestStageRestoreStreamsLargeArchive: Der Restore-Weg darf die Datenbank
// ebenso wenig am Stück in den Speicher legen wie der Backup-Weg. Der Test
// schickt ein mehrblockiges Archiv durch und prüft, dass die Datei im Staging
// vollständig und unverändert ankommt.
func TestStageRestoreStreamsLargeArchive(t *testing.T) {
	payload := make([]byte, 3*archiveChunkSize+777)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	archive, err := buildEncryptedArchive([]archiveFile{
		{Name: "app.db", Data: payload},
		{Name: "lcm.key", Data: []byte("schluessel")},
	}, "pw")
	if err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	svc := &BackupService{dataDir: dataDir}
	if err := svc.StageRestoreReader(bytes.NewReader(archive), "pw", 0); err != nil {
		t.Fatalf("StageRestoreReader: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dataDir, restoreStagingName, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("app.db im Staging weicht ab (%d statt %d Byte)", len(got), len(payload))
	}
	if _, err := os.Stat(filepath.Join(dataDir, restoreStagingName, restoreMarker)); err != nil {
		t.Fatal("Fertig-Marker fehlt")
	}
}

// TestStageRestoreRejectsArchiveWithoutDB: Ein Archiv ohne Datenbank darf kein
// Staging hinterlassen - sonst würde der nächste Start ein halbes Restore
// anwenden.
func TestStageRestoreRejectsArchiveWithoutDB(t *testing.T) {
	archive, err := buildEncryptedArchive([]archiveFile{{Name: "lcm.key", Data: []byte("nur-key")}}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	svc := &BackupService{dataDir: dataDir}
	if err := svc.StageRestore(archive, "pw"); !errors.Is(err, ErrBackupIncomplete) {
		t.Fatalf("erwartet ErrBackupIncomplete, bekam %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, restoreStagingName)); !os.IsNotExist(err) {
		t.Fatal("unvollständiges Staging blieb liegen")
	}
}

// TestApplyStagedRestoreSurvivesReadOnlyConfig: Im Paket gehört
// /etc/lcm/config.json root, der Dienst darf sie nur lesen. Scheiterte der
// Restore daran, brach der Start ab - und weil das Staging liegen blieb, bei
// jedem weiteren Start erneut. Der Restore muss die Datenbank trotzdem
// einspielen und die Konfiguration daneben ablegen.
func TestApplyStagedRestoreSurvivesReadOnlyConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("als root greifen Dateirechte nicht")
	}
	dataDir := t.TempDir()

	// Nicht beschreibbares Konfig-Verzeichnis wie im Paket (root:lcm 0750).
	confDir := filepath.Join(t.TempDir(), "etc")
	if err := os.MkdirAll(confDir, 0o750); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(confDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"database_path":"app.db"}`), 0o400); err != nil {
		t.Fatal(err)
	}
	// Wie im Paket: weder die Datei noch das Verzeichnis sind für den Dienst
	// beschreibbar (root:lcm 0640 in root:lcm 0750).
	if err := os.Chmod(confDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(confDir, 0o750)
		_ = os.Chmod(configPath, 0o640)
	})

	archive, err := buildEncryptedArchive([]archiveFile{
		{Name: "app.db", Data: []byte("wiederhergestellte-datenbank")},
		{Name: "config.json", Data: []byte(`{"database_path":"app.db","jwt_secret":"aus-backup"}`)},
	}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	svc := &BackupService{dataDir: dataDir}
	if err := svc.StageRestore(archive, "pw"); err != nil {
		t.Fatal(err)
	}

	applied, err := ApplyStagedRestore(dataDir, configPath)
	if err != nil {
		t.Fatalf("Restore bricht an der schreibgeschützten Konfiguration ab: %v", err)
	}
	if !applied {
		t.Fatal("Restore wurde nicht angewendet")
	}
	// Die Datenbank ist da - darauf kommt es an.
	db, err := os.ReadFile(filepath.Join(dataDir, "app.db"))
	if err != nil || string(db) != "wiederhergestellte-datenbank" {
		t.Fatalf("Datenbank nicht wiederhergestellt: %v / %q", err, db)
	}
	// Die Konfiguration liegt daneben, damit sie von Hand übernommen werden kann.
	aside, err := os.ReadFile(filepath.Join(dataDir, "config.json.aus-backup"))
	if err != nil {
		t.Fatalf("zurückgelegte Konfiguration fehlt: %v", err)
	}
	if !bytes.Contains(aside, []byte("aus-backup")) {
		t.Fatalf("zurückgelegte Konfiguration hat den falschen Inhalt: %s", aside)
	}
	// Das Staging ist aufgeräumt - der nächste Start zieht es nicht erneut.
	if _, err := os.Stat(filepath.Join(dataDir, restoreStagingName)); !os.IsNotExist(err) {
		t.Fatal("Staging blieb liegen - nächster Start würde es erneut anwenden")
	}
}

// TestStageRestoreEnforcesExtractLimit: Beim Upload gilt eine Obergrenze für
// die Summe der entpackten Dateien - ein komprimiertes Archiv könnte sonst mit
// wenigen MiB Upload die Platte füllen.
func TestStageRestoreEnforcesExtractLimit(t *testing.T) {
	// Gut komprimierbar: viel Inhalt, kleines Archiv.
	archive, err := buildEncryptedArchive([]archiveFile{
		{Name: "app.db", Data: bytes.Repeat([]byte("A"), 4<<20)},
	}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	svc := &BackupService{dataDir: dataDir}
	err = svc.StageRestoreReader(bytes.NewReader(archive), "pw", 1<<20)
	if err == nil {
		t.Fatal("Entpack-Grenze wurde nicht durchgesetzt")
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, restoreStagingName)); !os.IsNotExist(statErr) {
		t.Error("abgebrochenes Staging blieb liegen")
	}
	// Ohne Grenze geht dasselbe Archiv durch.
	if err := svc.StageRestoreReader(bytes.NewReader(archive), "pw", 0); err != nil {
		t.Fatalf("ohne Grenze: %v", err)
	}
}
