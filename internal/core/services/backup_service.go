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

	"filippo.io/age"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/creds"
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
var ErrBackupNoPassphrase = errors.New("keine backup-passphrase (Parameter, " + EnvBackupPassphrase + " oder Einstellungen) und keine empfänger-schlüssel")

// ErrInvalidBackupRecipient: Eine Zeile der Empfänger-Liste ist kein
// öffentlicher age-Schlüssel (age1…).
var ErrInvalidBackupRecipient = errors.New("ungültiger empfänger-schlüssel")

// maxBackupRecipients deckelt die Liste - mehr Empfänger als Menschen mit
// Schlüsselverantwortung braucht niemand.
const maxBackupRecipients = 20

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
	// masterKey ist der Schlüssel, mit dem die Datenbankfelder verschlüsselt
	// sind. Er gehört in jedes Archiv - sonst ist die Sicherung auf einer
	// anderen Maschine nicht lesbar. Liegt er als Datei im Datenverzeichnis,
	// wird die gepackt; kommt er aus einem systemd-Credential, gibt es diese
	// Datei nicht, und der Schlüssel muss aus dem Speicher ins Archiv.
	masterKey []byte
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

// WithMasterKey hinterlegt den Master-Key für das Archiv (siehe masterKey).
func (s *BackupService) WithMasterKey(key []byte) *BackupService {
	s.masterKey = key
	return s
}

// BackupPassphraseSet meldet, ob die Passphrase für unbeaufsichtigte
// (geplante) Backups außerhalb der Datenbank hinterlegt ist - als
// Umgebungsvariable oder als systemd-Credential. Nur das Flag - der Wert
// selbst verlässt den Prozess nie. Die UI warnt damit sichtbar, wenn
// automatische Backups mangels Passphrase fehlschlagen würden.
func BackupPassphraseSet() bool {
	if os.Getenv(EnvBackupPassphrase) != "" {
		return true
	}
	_, ok := creds.Read(creds.BackupPassphrase)
	return ok
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
	if p, ok := creds.Read(creds.BackupPassphrase); ok && p != "" {
		return p, nil
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
	// Womit wird verschlüsselt? Eine ausdrücklich mitgegebene Passphrase
	// gewinnt (der Mensch hat sie gewählt); sonst die hinterlegten Empfänger-
	// Schlüssel; sonst die Passphrase aus Umgebung, Credential oder
	// Einstellungen. Eine hier NEU angegebene Passphrase muss die
	// Stärke-Policy erfüllen; aufgelöste wurden beim Setzen geprüft.
	var pass string
	recipients := s.recipients()
	mode := domain.BackupEncryptionPassphrase
	switch {
	case passphrase != "":
		if err := EnforceBackupPassphrase(passphrase); err != nil {
			return nil, err
		}
		pass, recipients = passphrase, nil
	case len(recipients) > 0:
		mode = domain.BackupEncryptionRecipients
	default:
		var err error
		if pass, err = s.resolvePassphrase(""); err != nil {
			return nil, err
		}
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
			if archiveName == crypto.KeyFileName && len(s.masterKey) > 0 {
				// Kein lcm.key auf der Platte (systemd-Credential): Der
				// Schlüssel kommt aus dem Speicher ins Archiv - in derselben
				// Form, damit ein Restore ihn wie eine Datei ablegen kann.
				sources = append(sources, archiveSource{Name: archiveName, Data: crypto.KeyFileContent(s.masterKey)})
			}
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
	size, err := s.writeArchiveFile(tmpTarget, sources, pass, recipients)
	if err != nil {
		os.Remove(tmpTarget)
		return nil, err
	}
	if err := os.Rename(tmpTarget, target); err != nil {
		os.Remove(tmpTarget)
		return nil, fmt.Errorf("archiv ablegen: %w", err)
	}

	backup := &domain.Backup{FileName: name, SizeBytes: size, Trigger: trigger, Encryption: mode}
	if err := s.settings.CreateBackup(backup); err != nil {
		return nil, err
	}
	slog.Info("system backup created", "file", name, "size", size,
		"files", len(sources), "triggered_by", trigger, "encryption", mode)
	return backup, nil
}

// recipients liefert die hinterlegten Empfänger-Schlüssel. Eine unlesbare
// Liste (beim Speichern geprüft, sollte nicht vorkommen) zählt als keine -
// dann greift die Passphrase, und das Protokoll sagt warum.
func (s *BackupService) recipients() []age.Recipient {
	cfg, err := s.settings.Get()
	if err != nil {
		return nil
	}
	recipients, err := ParseBackupRecipients(cfg.BackupRecipients)
	if err != nil {
		slog.Error("backup recipients unusable - falling back to passphrase", "error", err)
		return nil
	}
	return recipients
}

// RecipientsConfigured meldet, ob Empfänger-Schlüssel hinterlegt sind - dann
// braucht das geplante Backup keine Passphrase.
func (s *BackupService) RecipientsConfigured() bool {
	return len(s.recipients()) > 0
}

// ParseBackupRecipients liest die Empfänger-Liste (ein öffentlicher age-
// Schlüssel je Zeile, Leerzeilen und #-Kommentare erlaubt).
func ParseBackupRecipients(text string) ([]age.Recipient, error) {
	var out []age.Recipient
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r, err := age.ParseX25519Recipient(line)
		if err != nil {
			return nil, fmt.Errorf("%w (zeile %d)", ErrInvalidBackupRecipient, i+1)
		}
		out = append(out, r)
		if len(out) > maxBackupRecipients {
			return nil, fmt.Errorf("%w: höchstens %d empfänger", ErrInvalidBackupRecipient, maxBackupRecipients)
		}
	}
	return out, nil
}

// NormalizeBackupRecipients prüft die Liste und liefert sie bereinigt zurück -
// eine Zeile je Schlüssel, ohne Leerzeilen und Kommentare.
func NormalizeBackupRecipients(text string) (string, error) {
	recipients, err := ParseBackupRecipients(text)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(recipients))
	for _, r := range recipients {
		lines = append(lines, r.(*age.X25519Recipient).String())
	}
	return strings.Join(lines, "\n"), nil
}

// GenerateBackupRecipient erzeugt ein Schlüsselpaar für die Sicherungen. Der
// private Schlüssel geht EINMALIG an den Aufrufer und wird nirgends
// gespeichert - genau wie beim SSH-Schlüssel eines Linux-Benutzers.
func GenerateBackupRecipient() (publicKey, privateKey string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.Recipient().String(), id.String(), nil
}

// writeArchiveFile schreibt das verschlüsselte Archiv nach path und liefert
// seine Größe - an die Empfänger, wenn welche übergeben sind, sonst mit der
// Passphrase.
func (s *BackupService) writeArchiveFile(path string, sources []archiveSource, pass string, recipients []age.Recipient) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, fmt.Errorf("archiv anlegen: %w", err)
	}
	// Gepuffert schreiben: Der Blockstrom kommt in kleinen Häppchen aus dem
	// ZIP-Writer, ungepuffert wären das sehr viele winzige Schreibaufrufe.
	bw := bufio.NewWriterSize(f, 1<<20)
	var encErr error
	if len(recipients) > 0 {
		encErr = writeAgeArchive(bw, sources, recipients)
	} else {
		encErr = writeEncryptedArchive(bw, sources, pass)
	}
	if encErr != nil {
		f.Close()
		return 0, fmt.Errorf("archiv verschlüsseln: %w", encErr)
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
	// Bei der Gelegenheit die Reste abgebrochener Läufe mitnehmen - Prune
	// läuft nach jeder Sicherung, hier ist der Ordner ohnehin schon offen.
	s.CleanStaleTemp(staleTempAge)
}

// staleTempAge ist das Alter, ab dem eine Zwischendatei als liegengeblieben
// gilt, wenn im laufenden Betrieb aufgeräumt wird. Ein echter Sicherungslauf
// schreibt seine Momentaufnahme in Minuten; sechs Stunden liegen so weit
// darüber, dass eine gerade entstehende Datei nie erwischt wird.
const staleTempAge = 6 * time.Hour

// isStaleTempName meldet die Zwischendateien eines Sicherungslaufs: die
// Momentaufnahme der Datenbank (.snap-…​.db) und das halbfertige Archiv
// (….lcmbak.part).
func isStaleTempName(name string) bool {
	if strings.HasPrefix(name, ".snap-") && strings.HasSuffix(name, ".db") {
		return true
	}
	return strings.HasSuffix(name, BackupExt+".part")
}

// CleanStaleTemp entfernt liegengebliebene Zwischendateien aus dem
// Backup-Verzeichnis und meldet, wie viele es waren.
//
// Warum es sie überhaupt gibt: Der reguläre Lauf räumt selbst auf, aber ein
// hart beendeter Prozess kommt nicht mehr dazu - ein Neustart mitten im
// Kopieren genügt. Zurück bleibt dann eine Momentaufnahme der Datenbank, und
// die ist ANDERS als die Archive daneben nicht verschlüsselt. Auf einem
// Produktivsystem lag so eine Datei vier Wochen lang herum (143 MB), ohne
// dass irgendetwas sie je wieder angefasst hätte.
//
// olderThan schützt einen gerade laufenden Sicherungslauf: 0 beim Start des
// Dienstes (dort kann keiner laufen, der Prozess ist neu), sonst staleTempAge.
func (s *BackupService) CleanStaleTemp(olderThan time.Duration) int {
	dir, err := s.backupDir()
	if err != nil {
		slog.Error("stale backup files: backup directory not determinable", "error", err)
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !isStaleTempName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if olderThan > 0 && time.Since(info.ModTime()) < olderThan {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			slog.Error("removing a leftover backup file failed", "file", e.Name(), "error", err)
			continue
		}
		// Eine Warnung, keine Nebenbemerkung: Die Datei belegt, dass ein
		// Sicherungslauf abgebrochen ist - und bis hierher lag eine
		// unverschlüsselte Kopie der Datenbank auf der Platte.
		slog.Warn("leftover file of an interrupted backup removed",
			"file", e.Name(), "size", info.Size(), "modified", info.ModTime().Format(time.RFC3339))
		removed++
	}
	return removed
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
