package services

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"LCM/internal/config"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/tlsx"
)

// restoreStagingName ist das Unterverzeichnis im Datenverzeichnis, in das ein
// Restore entpackt wird. Angewendet wird es erst beim (Neu-)Start, BEVOR die
// Datenbank geöffnet wird - die laufende SQLite-Verbindung und der geladene
// Master-Key lassen sich nicht unterm Prozess tauschen.
const restoreStagingName = "restore-staging"

// restoreMarker wird als LETZTES geschrieben; nur wenn er existiert, gilt das
// Staging als vollständig und wird beim Start angewendet (schützt vor
// halb-entpackten Ständen nach einem Abbruch).
const restoreMarker = ".ready"

// erlaubte Dateien im Archiv → ihr Zielname im Datenverzeichnis. app.db wird
// gesondert behandelt (Ziel ist der konfigurierte DatabasePath).
var restoreConfigFiles = []string{crypto.KeyFileName, "config.json", tlsx.CertFileName, tlsx.KeyFileName}

var ErrBackupIncomplete = errors.New("backup-archiv enthält keine datenbank (app.db)")

// StageRestore entschlüsselt ein Backup-Archiv aus dem Speicher. Bequemer
// Einstieg für Tests und kleine Archive; der Weg über StageRestoreReader
// belegt keinen Speicher in Archivgröße.
func (s *BackupService) StageRestore(archive []byte, passphrase string) error {
	return s.StageRestoreReader(bytes.NewReader(archive), passphrase, 0)
}

// StageRestoreReader entschlüsselt ein Backup-Archiv und legt seinen Inhalt im
// Staging-Verzeichnis ab. Angewendet wird es erst beim nächsten Start
// (ApplyStagedRestore). Falsche Passphrase → ErrBackupPassphrase.
//
// Jede Datei wird einzeln aus dem Archiv in ihre Staging-Datei kopiert - die
// Datenbank liegt dabei nie vollständig im Speicher.
// maxExtracted begrenzt die Summe der entpackten Dateien (0 = unbegrenzt). Ein
// hochgeladenes Archiv ist zwar in seiner Größe begrenzt, sein Inhalt aber
// komprimiert - ohne Grenze könnte ein präpariertes Archiv die Platte füllen.
// Für Archive aus der eigenen Historie gibt es die Grenze nicht: Dort ist die
// Datenbank so groß, wie sie eben ist, und sie muss zurückspielbar bleiben.
func (s *BackupService) StageRestoreReader(archive io.Reader, passphrase string, maxExtracted int64) error {
	if passphrase == "" {
		return ErrBackupNoPassphrase
	}
	staging := filepath.Join(s.dataDir, restoreStagingName)
	_ = os.RemoveAll(staging) // etwaigen Rest eines früheren Versuchs verwerfen
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return fmt.Errorf("staging anlegen: %w", err)
	}

	hasDB := false
	count := 0
	var extracted int64
	err := extractEncryptedArchive(archive, passphrase, s.dataDir, func(name string, r io.Reader) error {
		// Pfad-Traversal ausschließen: nur einfache Dateinamen zulassen.
		if filepath.Base(name) != name {
			return fmt.Errorf("ungültiger dateiname im archiv: %q", name)
		}
		f, err := os.OpenFile(filepath.Join(staging, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("staging schreiben (%s): %w", name, err)
		}
		var src io.Reader = r
		if maxExtracted > 0 {
			src = io.LimitReader(r, maxExtracted-extracted+1)
		}
		written, err := io.Copy(f, src)
		if err != nil {
			f.Close()
			return fmt.Errorf("staging schreiben (%s): %w", name, err)
		}
		extracted += written
		if maxExtracted > 0 && extracted > maxExtracted {
			f.Close()
			return fmt.Errorf("archiv überschreitet das Größenlimit (%d MiB)", maxExtracted>>20)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("staging schreiben (%s): %w", name, err)
		}
		if name == "app.db" {
			hasDB = true
		}
		count++
		return nil
	})
	if err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if !hasDB {
		_ = os.RemoveAll(staging)
		return ErrBackupIncomplete
	}

	// Marker zuletzt - signalisiert Vollständigkeit.
	if err := os.WriteFile(filepath.Join(staging, restoreMarker), []byte("ready\n"), 0o600); err != nil {
		return fmt.Errorf("staging-marker: %w", err)
	}
	slog.Info("restore prepared (staging) - will be applied on next start",
		"staging", staging, "files", count)
	return nil
}

// ApplyStagedRestore wendet ein vorbereitetes Staging an: config.json, Master-
// Key und TLS-Material werden ersetzt, danach - anhand des WIEDERHERGESTELLTEN
// DatabasePath - die Datenbank. Muss beim Start VOR dem Öffnen der DB laufen.
// Liefert (true, nil), wenn ein Restore angewendet wurde.
func ApplyStagedRestore(dataDir, configPath string) (bool, error) {
	staging := filepath.Join(dataDir, restoreStagingName)
	if _, err := os.Stat(filepath.Join(staging, restoreMarker)); err != nil {
		return false, nil // nichts vorbereitet
	}

	// 1. Konfig-/Key-/TLS-Dateien ersetzen.
	for _, name := range restoreConfigFiles {
		src := filepath.Join(staging, name)
		if _, err := os.Stat(src); err != nil {
			continue // optionale Datei nicht im Archiv
		}
		dst := filepath.Join(dataDir, name)
		if name == "config.json" && configPath != "" {
			dst = configPath
		}
		if err := moveFile(src, dst, 0o600); err != nil {
			// config.json gehört im Paket bewusst root, der Dienst darf sie nur
			// lesen (/etc/lcm ist root:lcm 0750). Daran den ganzen Restore
			// scheitern zu lassen wäre das schlechteste Ergebnis: Der Start
			// bricht ab, das Staging bleibt liegen - und weil es beim nächsten
			// Start erneut gezogen wird, käme LCM überhaupt nicht mehr hoch.
			// Die Konfiguration ist der einzige Teil, ohne den ein Restore
			// trotzdem sinnvoll ist; sie wird daneben abgelegt, und der
			// Betreiber wird deutlich darauf hingewiesen.
			if name != "config.json" {
				return false, fmt.Errorf("restore %s: %w", name, err)
			}
			aside := filepath.Join(dataDir, "config.json.aus-backup")
			if mvErr := moveFile(src, aside, 0o600); mvErr != nil {
				slog.Error("restore: configuration could neither be applied nor set aside",
					"target", dst, "error", err, "aside_error", mvErr)
			} else {
				slog.Warn("restore: configuration NOT applied - no write permission. "+
					"The restored file has been placed alongside; copy it manually as root if needed.",
					"target", dst, "restored_copy", aside, "error", err)
			}
		}
	}

	// 2. DatabasePath aus der JETZT wiederhergestellten Konfiguration bestimmen.
	dbPath := filepath.Join(dataDir, "app.db")
	if configPath == "" {
		configPath = filepath.Join(dataDir, config.DefaultFileName)
	}
	if cfg, err := config.LoadFrom(configPath); err == nil && cfg.DatabasePath != "" {
		if filepath.IsAbs(cfg.DatabasePath) {
			dbPath = cfg.DatabasePath
		} else {
			dbPath = filepath.Join(dataDir, cfg.DatabasePath)
		}
	}

	// 3. Datenbank ersetzen; alte WAL/SHM entfernen, sonst korrumpieren sie
	//    die frisch eingespielte DB.
	if err := moveFile(filepath.Join(staging, "app.db"), dbPath, 0o600); err != nil {
		return false, fmt.Errorf("restore datenbank: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix)
	}

	// 4. Staging aufräumen - der Restore ist abgeschlossen.
	if err := os.RemoveAll(staging); err != nil {
		slog.Warn("restore: staging could not be removed", "error", err)
	}
	slog.Info("restore applied", "database", dbPath)
	return true, nil
}

// moveFile verschiebt src nach dst (Rename; bei geräteübergreifendem Ziel
// Copy+Remove) und setzt die Dateirechte.
func moveFile(src, dst string, perm os.FileMode) error {
	if err := os.Rename(src, dst); err == nil {
		return os.Chmod(dst, perm)
	}
	// Fallback: über Gerätegrenzen hinweg kopieren.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
