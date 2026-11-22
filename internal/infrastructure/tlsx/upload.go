package tlsx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Hochgeladene Zertifikate liegen unter <dataDir>/tls/uploads/<id>/ als
// cert.pem und key.pem. Die ID ist eine kurze Hex-Zeichenfolge, die LCM
// vergibt; jede ID von außen wird streng geprüft, bevor sie in einen Pfad
// kommt.

const (
	uploadCertFile = "cert.pem"
	uploadKeyFile  = "key.pem"

	// uploadSpecPrefix macht aus der Upload-ID die ID im Pool.
	uploadSpecPrefix = "upload-"

	// UploadBackupPrefix leitet die Namen der Upload-Dateien im flachen
	// Backup-Archiv ein: tls-upload-<id>-cert.pem und -key.pem.
	UploadBackupPrefix = "tls-upload-"
)

var (
	// ErrUploadID: die ID hat nicht die Form, die LCM vergibt.
	ErrUploadID = errors.New("ungültige upload-id")
	// ErrUploadNotFound: es gibt kein Zertifikat mit dieser ID.
	ErrUploadNotFound = errors.New("upload nicht gefunden")
	// ErrNotUpload: das Zertifikat stammt nicht aus einem Upload.
	ErrNotUpload = errors.New("zertifikat stammt nicht aus einem upload")

	uploadIDPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)
)

// InvalidCertificateError: Zertifikat und Schlüssel taugen nicht als
// Server-Zertifikat. Der Text sagt, woran es liegt.
type InvalidCertificateError struct{ Err error }

func (e *InvalidCertificateError) Error() string { return e.Err.Error() }
func (e *InvalidCertificateError) Unwrap() error { return e.Err }

// UploadIDOf macht aus der ID im Pool („upload-<id>“) die Upload-ID. ok ist
// false, wenn das Zertifikat aus einer anderen Quelle stammt.
func UploadIDOf(poolID string) (id string, ok bool) {
	return strings.CutPrefix(poolID, uploadSpecPrefix)
}

func uploadsDir(dataDir string) string { return filepath.Join(dataDir, "tls", "uploads") }

// SaveUpload prüft Zertifikat und Schlüssel und legt sie unter einer neuen
// ID ab. Was die Prüfung nicht besteht, wird nicht geschrieben.
func SaveUpload(dataDir string, certPEM, keyPEM []byte, now time.Time) (id string, err error) {
	if _, err := ValidatePair(certPEM, keyPEM, now); err != nil {
		return "", &InvalidCertificateError{Err: err}
	}
	if err := os.MkdirAll(uploadsDir(dataDir), 0o700); err != nil {
		return "", fmt.Errorf("upload-verzeichnis anlegen: %w", err)
	}
	id, dir, err := newUploadDir(dataDir)
	if err != nil {
		return "", err
	}
	// Erst der Schlüssel: Sieht ein Neuladen das Verzeichnis mitten im
	// Schreiben, fehlt noch das Zertifikat und es lädt nichts Halbes.
	for _, f := range []struct {
		name string
		data []byte
	}{{uploadKeyFile, keyPEM}, {uploadCertFile, certPEM}} {
		if err := ReplaceFile(filepath.Join(dir, f.name), f.data, 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	return id, nil
}

// newUploadDir legt das Verzeichnis für eine frische ID an. Mkdir schlägt
// bei einer vergebenen ID fehl, dann wird neu gewürfelt.
func newUploadDir(dataDir string) (id, dir string, err error) {
	for range 5 {
		buf := make([]byte, 4)
		if _, err := rand.Read(buf); err != nil {
			return "", "", fmt.Errorf("upload-id erzeugen: %w", err)
		}
		id = hex.EncodeToString(buf)
		dir = filepath.Join(uploadsDir(dataDir), id)
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return id, dir, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", "", fmt.Errorf("upload-verzeichnis anlegen: %w", err)
		}
	}
	return "", "", errors.New("keine freie upload-id gefunden")
}

// RemoveUpload löscht ein hochgeladenes Zertifikat samt Schlüssel.
func RemoveUpload(dataDir, id string) error {
	if !uploadIDPattern.MatchString(id) {
		return fmt.Errorf("%w: %q", ErrUploadID, id)
	}
	dir := filepath.Join(uploadsDir(dataDir), id)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return ErrUploadNotFound
	}
	return os.RemoveAll(dir)
}

// PruneUploads entfernt alle Upload-Verzeichnisse, deren ID nicht in keep
// steht. Andere Einträge unter tls/uploads bleiben unberührt.
func PruneUploads(dataDir string, keep map[string]bool) error {
	ids, err := uploadIDs(dataDir)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if keep[id] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(uploadsDir(dataDir), id)); err != nil {
			return fmt.Errorf("upload %s entfernen: %w", id, err)
		}
	}
	return nil
}

// UploadSpecs nennt die hochgeladenen Zertifikate für den Pool.
func UploadSpecs(dataDir string) ([]PoolSpec, error) {
	ids, err := uploadIDs(dataDir)
	if err != nil {
		return nil, err
	}
	specs := make([]PoolSpec, 0, len(ids))
	for _, id := range ids {
		dir := filepath.Join(uploadsDir(dataDir), id)
		specs = append(specs, PoolSpec{
			ID:       uploadSpecPrefix + id,
			Source:   SourceUpload,
			CertPath: filepath.Join(dir, uploadCertFile),
			KeyPath:  filepath.Join(dir, uploadKeyFile),
		})
	}
	return specs, nil
}

// uploadIDs listet die Upload-Verzeichnisse; fremde Namen bleiben außen vor.
func uploadIDs(dataDir string) ([]string, error) {
	entries, err := os.ReadDir(uploadsDir(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("uploads lesen: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && uploadIDPattern.MatchString(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// UploadBackupFiles liefert die Upload-Dateien für die Sicherung: Name im
// Archiv zu Pfad auf der Platte.
func UploadBackupFiles(dataDir string) (map[string]string, error) {
	ids, err := uploadIDs(dataDir)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, id := range ids {
		for _, name := range []string{uploadCertFile, uploadKeyFile} {
			path := filepath.Join(uploadsDir(dataDir), id, name)
			if fileExists(path) {
				files[UploadBackupPrefix+id+"-"+name] = path
			}
		}
	}
	return files, nil
}

// UploadRestoreTarget sagt, wohin eine Datei aus dem Archiv beim Zurückspielen
// gehört. ok ist false, wenn der Name keine Upload-Datei ist.
func UploadRestoreTarget(dataDir, archiveName string) (path string, ok bool) {
	rest, found := strings.CutPrefix(archiveName, UploadBackupPrefix)
	if !found {
		return "", false
	}
	id, name, found := strings.Cut(rest, "-")
	if !found || !uploadIDPattern.MatchString(id) || (name != uploadCertFile && name != uploadKeyFile) {
		return "", false
	}
	return filepath.Join(uploadsDir(dataDir), id, name), true
}
