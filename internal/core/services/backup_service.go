package services

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/storage/repositories"
)

// EnvBackupPassphrase liefert die Passphrase für unbeaufsichtigte (geplante)
// Backups. Wird bewusst NICHT in der DB/Config gespeichert (die im Backup
// selbst läge) - sie ist das eine Geheimnis, das der Betreiber getrennt hält.
const EnvBackupPassphrase = "LCM_BACKUP_PASSPHRASE"

// BackupExt ist die Endung des verschlüsselten Backup-Archivs.
const BackupExt = ".lcmbak"

// EnvRestoreAutoRestart steuert, ob LCM sich nach dem Vorbereiten eines Restores
// selbst neu startet, um es anzuwenden. Gesetzt (truthy) → Vorrang vor der
// UI-Einstellung; explizit false-Werte deaktivieren es ebenfalls per Vorrang.
const EnvRestoreAutoRestart = "LCM_RESTORE_AUTO_RESTART"

// ErrBackupNoPassphrase signalisiert, dass weder eine Passphrase übergeben
// noch LCM_BACKUP_PASSPHRASE gesetzt ist - ohne sie kein verschlüsseltes Backup.
var ErrBackupNoPassphrase = errors.New("keine backup-passphrase (Parameter oder " + EnvBackupPassphrase + ")")

// ErrBackupNotFound: das angeforderte Backup existiert nicht (oder ein
// ungültiger/unsicherer Dateiname wurde übergeben).
var ErrBackupNotFound = errors.New("backup nicht gefunden")

// BackupService sichert die LCM-Datenbank samt Master-Key, Konfiguration und
// TLS-Material als EIN passphrase-verschlüsseltes Archiv (.lcmbak). Nur so ist
// das Backup auf einer anderen Instanz wiederherstellbar (die verschlüsselten
// DB-Felder brauchen den Master-Key) - und ein geleaktes Archiv bleibt ohne
// die Passphrase wertlos.
type BackupService struct {
	db         *gorm.DB
	settings   *repositories.SettingsRepository
	dataDir    string
	dbPath     string
	configPath string
	// configDir ist das per config.json vorgegebene Backup-Verzeichnis
	// (leer = nicht gesetzt). Rangfolge in backupDir(): DB-Einstellung →
	// config.json → <data>/backups.
	configDir string
	// cipher entschlüsselt die in den Einstellungen hinterlegte
	// Backup-Passphrase (R2-027). Optional (nil in schlanken Tests).
	cipher *crypto.Cipher
}

func NewBackupService(db *gorm.DB, settings *repositories.SettingsRepository, dataDir, dbPath, configPath string) *BackupService {
	return &BackupService{db: db, settings: settings, dataDir: dataDir, dbPath: dbPath, configPath: configPath}
}

// WithConfigDir hinterlegt das per config.json vorgegebene Backup-Verzeichnis.
func (s *BackupService) WithConfigDir(dir string) *BackupService {
	s.configDir = dir
	return s
}

// WithCipher verdrahtet die Entschlüsselung der gespeicherten
// Backup-Passphrase (R2-027).
func (s *BackupService) WithCipher(c *crypto.Cipher) *BackupService {
	s.cipher = c
	return s
}

// BackupPassphraseSet meldet, ob die Passphrase für unbeaufsichtigte
// (geplante) Backups in der Umgebung hinterlegt ist. Nur das Flag - der
// Wert selbst verlässt den Prozess nie. Die UI warnt damit sichtbar, wenn
// automatische Backups mangels Passphrase fehlschlagen würden.
func BackupPassphraseSet() bool {
	return os.Getenv(EnvBackupPassphrase) != ""
}

// resolvePassphrase nimmt die übergebene Passphrase, sonst
// LCM_BACKUP_PASSPHRASE, sonst die in den Einstellungen hinterlegte
// (AES-GCM; R2-027 - vorher gab es für geplante Backups keinen Weg ohne
// Umgebungsvariable, und der Fehlschlag blieb still).
func (s *BackupService) resolvePassphrase(provided string) (string, error) {
	if provided != "" {
		return provided, nil
	}
	if env := os.Getenv(EnvBackupPassphrase); env != "" {
		return env, nil
	}
	if s.cipher != nil {
		if cfg, err := s.settings.Get(); err == nil && cfg.BackupPassphraseEnc != "" {
			if p, err := s.cipher.DecryptString(cfg.BackupPassphraseEnc); err == nil && p != "" {
				return p, nil
			}
		}
	}
	return "", ErrBackupNoPassphrase
}

// backupDir liefert das Backup-Verzeichnis. Rangfolge: UI-Einstellung
// (DB) → Vorgabe aus config.json → Standard <data>/backups.
func (s *BackupService) backupDir() (string, error) {
	dir := ""
	if cfg, err := s.settings.Get(); err == nil {
		dir = cfg.BackupDir
	}
	if dir == "" {
		dir = s.configDir
	}
	if dir == "" {
		dir = filepath.Join(s.dataDir, "backups")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("backup-verzeichnis anlegen: %w", err)
	}
	return dir, nil
}

// Create erstellt ein vollständiges, passphrase-verschlüsseltes Backup: eine
// konsistente DB-Kopie (VACUUM INTO) plus Master-Key, config.json und
// TLS-Material, gebündelt in EIN .lcmbak-Archiv. Die Passphrase kommt aus dem
// Parameter oder LCM_BACKUP_PASSPHRASE.
func (s *BackupService) Create(trigger, passphrase string) (*domain.Backup, error) {
	// Eine hier NEU angegebene Passphrase muss die Stärke-Policy erfüllen.
	// Aus Umgebung/Einstellungen aufgelöste Passphrasen werden nicht erneut
	// geprüft (siehe EnforceBackupPassphrase) - sie wurden beim Setzen geprüft.
	if passphrase != "" {
		if err := EnforceBackupPassphrase(passphrase); err != nil {
			return nil, err
		}
	}
	pass, err := s.resolvePassphrase(passphrase)
	if err != nil {
		return nil, err
	}
	dir, err := s.backupDir()
	if err != nil {
		return nil, err
	}

	// 1. Konsistente DB-Momentaufnahme via VACUUM INTO in eine Temp-Datei.
	tmpDB := filepath.Join(dir, fmt.Sprintf(".snap-%s.db", time.Now().Format("20060102-150405.000")))
	if err := s.db.Exec("VACUUM INTO ?", tmpDB).Error; err != nil {
		return nil, fmt.Errorf("datenbank-momentaufnahme: %w", err)
	}
	defer os.Remove(tmpDB)

	// 2. Alle für einen portablen Restore nötigen Dateien einsammeln. Die
	//    Quellen werden beim Packen streamend gelesen - die Momentaufnahme
	//    ist die mit Abstand größte Datei und darf nicht im Speicher landen.
	sources := []archiveSource{{Name: "app.db", Path: tmpDB}}
	optional := map[string]string{
		crypto.KeyFileName: filepath.Join(s.dataDir, crypto.KeyFileName), // Master-Key
		"config.json":      s.configPath,
		tlsx.CertFileName:  filepath.Join(s.dataDir, tlsx.CertFileName),
		tlsx.KeyFileName:   filepath.Join(s.dataDir, tlsx.KeyFileName),
	}
	for archiveName, path := range optional {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue // optionale Datei nicht vorhanden
		}
		sources = append(sources, archiveSource{Name: archiveName, Path: path})
	}

	// 3. Bündeln + verschlüsseln, direkt in die Zieldatei. Zuerst unter einem
	//    Temp-Namen: Bricht der Lauf ab, bleibt kein halbes Archiv liegen, das
	//    wie ein gültiges Backup aussieht.
	name := fmt.Sprintf("lcm-backup-%s%s", time.Now().Format("20060102-150405"), BackupExt)
	target := filepath.Join(dir, name)
	tmpTarget := target + ".part"
	size, err := s.writeArchiveFile(tmpTarget, sources, pass)
	if err != nil {
		os.Remove(tmpTarget)
		return nil, err
	}
	if err := os.Rename(tmpTarget, target); err != nil {
		os.Remove(tmpTarget)
		return nil, fmt.Errorf("archiv ablegen: %w", err)
	}

	backup := &domain.Backup{FileName: name, SizeBytes: size, Trigger: trigger}
	if err := s.settings.CreateBackup(backup); err != nil {
		return nil, err
	}
	slog.Info("system backup created", "file", name, "size", size,
		"files", len(sources), "triggered_by", trigger)
	return backup, nil
}

// writeArchiveFile schreibt das verschlüsselte Archiv nach path und liefert
// seine Größe.
func (s *BackupService) writeArchiveFile(path string, sources []archiveSource, pass string) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, fmt.Errorf("archiv anlegen: %w", err)
	}
	// Gepuffert schreiben: Der Blockstrom kommt in kleinen Häppchen aus dem
	// ZIP-Writer, ungepuffert wären das sehr viele winzige Schreibaufrufe.
	bw := bufio.NewWriterSize(f, 1<<20)
	if err := writeEncryptedArchive(bw, sources, pass); err != nil {
		f.Close()
		return 0, fmt.Errorf("archiv verschlüsseln: %w", err)
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return 0, fmt.Errorf("archiv schreiben: %w", err)
	}
	// Sync vor dem Umbenennen: Ein Backup, das den Stromausfall kurz nach dem
	// Schreiben nicht übersteht, ist keines.
	if err := f.Sync(); err != nil {
		f.Close()
		return 0, fmt.Errorf("archiv schreiben: %w", err)
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("archiv schreiben: %w", err)
	}
	return size, nil
}

// Prune entfernt Backup-Dateien über der Aufbewahrungsgrenze.
func (s *BackupService) Prune(keep int) {
	stale, err := s.settings.DeleteBackupsBeyond(keep)
	if err != nil {
		slog.Error("backup retention: deleting metadata failed", "error", err)
		return
	}
	dir, err := s.backupDir()
	if err != nil {
		// Metadaten sind bereits weg - die Dateien blieben sonst still liegen.
		slog.Error("backup retention: backup directory not determinable - files not deleted", "error", err)
		return
	}
	for _, b := range stale {
		if err := os.Remove(filepath.Join(dir, b.FileName)); err != nil && !os.IsNotExist(err) {
			slog.Error("deleting backup file failed", "file", b.FileName, "error", err)
		}
	}
}

// List liefert alle Backup-Metadaten.
func (s *BackupService) List() ([]domain.Backup, error) {
	return s.settings.FindBackups()
}

// BackupPath liefert den absoluten Pfad eines Backups im Backup-Verzeichnis.
// Der Name muss ein einfacher Dateiname mit .lcmbak-Endung sein (kein
// Pfad-Traversal), und die Datei muss existieren - sonst ErrBackupNotFound.
func (s *BackupService) BackupPath(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || filepath.Ext(name) != BackupExt {
		return "", ErrBackupNotFound
	}
	dir, err := s.backupDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); err != nil {
		return "", ErrBackupNotFound
	}
	return p, nil
}

// Delete entfernt ein Backup dauerhaft - Metadaten-Eintrag UND Datei. Der Name
// muss ein einfacher .lcmbak-Dateiname sein (kein Pfad-Traversal). Fehlt der
// Metadaten-Eintrag, ist es ErrBackupNotFound; eine bereits fehlende Datei ist
// kein Fehler (Metadaten werden trotzdem bereinigt).
func (s *BackupService) Delete(name string) error {
	if name == "" || filepath.Base(name) != name || filepath.Ext(name) != BackupExt {
		return ErrBackupNotFound
	}
	rows, err := s.settings.DeleteBackupByName(name)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrBackupNotFound
	}
	dir, err := s.backupDir()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	slog.Info("system backup deleted", "file", name)
	return nil
}

// StageRestoreFromHistory bereitet ein bereits im System vorhandenes Backup
// (aus der Historie) zur Wiederherstellung vor. Fehlerfälle wie StageRestore
// plus ErrBackupNotFound.
func (s *BackupService) StageRestoreFromHistory(name, passphrase string) error {
	path, err := s.BackupPath(name)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("backup lesen: %w", err)
	}
	defer f.Close()
	return s.StageRestoreReader(bufio.NewReaderSize(f, 1<<20), passphrase, 0)
}

// AutoRestartEnabled meldet, ob nach einem vorbereiteten Restore ein
// automatischer Neustart erfolgen soll. LCM_RESTORE_AUTO_RESTART hat Vorrang
// vor der gespeicherten Einstellung.
func (s *BackupService) AutoRestartEnabled() bool {
	if v, ok := os.LookupEnv(EnvRestoreAutoRestart); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	}
	if s.settings != nil {
		if cfg, err := s.settings.Get(); err == nil {
			return cfg.RestoreAutoRestart
		}
	}
	return false
}
