// Package acmex holt und erneuert Zertifikate bei einer ACME-CA (Let's
// Encrypt, deren Staging oder eine eigene wie step-ca) mit lego.
//
// Eingebunden sind nur die Kernpakete von lego, nie providers/dns: Die
// Fabrik dort zöge alle DNS-Anbieter samt SDKs ins Binary.
package acmex

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"LCM/internal/infrastructure/tlsx"
)

// Ablage unter <dataDir>/tls/acme/:
//
//	account.key                Kontoschlüssel (ECDSA P-256), für alle Verzeichnisse
//	accounts/<id>.json         Registrierung je Verzeichnis-URL
//	certs/<id>/{cert,key}.pem  ausgestelltes Zertifikat (Fullchain) und Schlüssel
//	acmedns/<id>.json          Konten bei einem acme-dns-Server (<id> aus dessen Adresse)
//
// Die Kennung eines Zertifikats ist ein Hash aus Verzeichnis und Namen:
// Ändern sich Namen oder CA, entsteht ein neues Zertifikat, und das alte
// fällt als nicht mehr benötigt weg. Jede Kennung von außen wird streng
// geprüft, bevor sie in einen Pfad kommt.
const (
	accountKeyFile = "account.key"
	certFile       = "cert.pem"
	keyFile        = "key.pem"

	// specPrefix macht aus der Zertifikatskennung die Kennung im Pool.
	specPrefix = "acme-"

	// BackupPrefix leitet die Namen der ACME-Dateien im flachen Backup-Archiv
	// ein: tls-acme-account.key, tls-acme-registration-<id>.json und
	// tls-acme-cert-<id>-{cert,key}.pem.
	BackupPrefix = "tls-acme-"

	backupRegistration = BackupPrefix + "registration-"
	backupCert         = BackupPrefix + "cert-"
	backupACMEDNS      = BackupPrefix + "acmedns-"
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)

func baseDir(dataDir string) string  { return filepath.Join(dataDir, "tls", "acme") }
func certsDir(dataDir string) string { return filepath.Join(baseDir(dataDir), "certs") }
func accountsDir(dataDir string) string {
	return filepath.Join(baseDir(dataDir), "accounts")
}
func certDir(dataDir, id string) string { return filepath.Join(certsDir(dataDir), id) }
func acmeDNSDir(dataDir string) string  { return filepath.Join(baseDir(dataDir), "acmedns") }

// acmeDNSStoragePath ist die Datei mit den Konten bei diesem acme-dns-Server.
// Je Server eine: Ein Konto gilt nur dort, wo es angelegt wurde.
func acmeDNSStoragePath(dataDir, serverURL string) string {
	return filepath.Join(acmeDNSDir(dataDir), shortID(serverURL)+".json")
}

// shortID: die ersten acht Hex-Zeichen von SHA-256 über die Teile.
func shortID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:4])
}

// CertID ist die Kennung des Zertifikats für diese Namen bei dieser CA. Die
// Reihenfolge der Namen spielt keine Rolle.
func CertID(directoryURL string, names []string) string {
	sorted := slices.Sorted(slices.Values(names))
	return shortID(directoryURL, strings.Join(sorted, ","))
}

// CertInfo beschreibt ein ausgestelltes Zertifikat für API und Oberfläche.
type CertInfo struct {
	ID          string    `json:"id"`
	Names       []string  `json:"names"`
	Issuer      string    `json:"issuer"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	Fingerprint string    `json:"fingerprint"` // SHA-256 des Zertifikats, hex
	// RenewAt: ab hier erneuert LCM das Zertifikat.
	RenewAt time.Time `json:"renew_at"`
}

func certInfo(id string, leaf *x509.Certificate, renewAt time.Time) CertInfo {
	return CertInfo{
		ID:          id,
		Names:       tlsx.CertNames(leaf),
		Issuer:      tlsx.IssuerName(leaf),
		NotBefore:   leaf.NotBefore,
		NotAfter:    leaf.NotAfter,
		Fingerprint: tlsx.CertFingerprint(leaf),
		RenewAt:     renewAt,
	}
}

// bundle sind Zertifikat (Fullchain) und Schlüssel als PEM.
type bundle struct {
	certPEM, keyPEM []byte
}

// readBundle liest ein Zertifikat samt Schlüssel; leaf ist das Zertifikat des
// Servers. Ist nichts abgelegt, ist der Fehler os.ErrNotExist.
func readBundle(dataDir, id string) (b bundle, leaf *x509.Certificate, err error) {
	dir := certDir(dataDir, id)
	if b.certPEM, err = os.ReadFile(filepath.Join(dir, certFile)); err != nil {
		return bundle{}, nil, err
	}
	if b.keyPEM, err = os.ReadFile(filepath.Join(dir, keyFile)); err != nil {
		return bundle{}, nil, err
	}
	block, _ := pem.Decode(b.certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return bundle{}, nil, fmt.Errorf("zertifikat %s: keine zertifikatsdaten", id)
	}
	leaf, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return bundle{}, nil, fmt.Errorf("zertifikat %s lesen: %w", id, err)
	}
	return b, leaf, nil
}

// writeBundle legt Zertifikat und Schlüssel ab, beide atomar. Erst der
// Schlüssel: Sieht ein Neuladen das Verzeichnis mitten im Schreiben, fehlt
// noch das Zertifikat, und es lädt nichts Halbes.
func writeBundle(dataDir, id string, b bundle) error {
	dir := certDir(dataDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("zertifikatsverzeichnis anlegen: %w", err)
	}
	if err := tlsx.ReplaceFile(filepath.Join(dir, keyFile), b.keyPEM, 0o600); err != nil {
		return err
	}
	return tlsx.ReplaceFile(filepath.Join(dir, certFile), b.certPEM, 0o600)
}

// certIDs listet die abgelegten Zertifikate; fremde Namen bleiben außen vor.
func certIDs(dataDir string) ([]string, error) {
	entries, err := os.ReadDir(certsDir(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("acme-zertifikate lesen: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && idPattern.MatchString(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// Specs nennt die ausgestellten Zertifikate für den Pool.
func Specs(dataDir string) ([]tlsx.PoolSpec, error) {
	ids, err := certIDs(dataDir)
	if err != nil {
		return nil, err
	}
	specs := make([]tlsx.PoolSpec, 0, len(ids))
	for _, id := range ids {
		dir := certDir(dataDir, id)
		specs = append(specs, tlsx.PoolSpec{
			ID:       specPrefix + id,
			Source:   tlsx.SourceACME,
			CertPath: filepath.Join(dir, certFile),
			KeyPath:  filepath.Join(dir, keyFile),
		})
	}
	return specs, nil
}

// removeCerts löscht alle Zertifikate, deren Kennung keep nicht nennt.
func removeCerts(dataDir string, keep map[string]bool) error {
	ids, err := certIDs(dataDir)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if keep[id] {
			continue
		}
		if err := os.RemoveAll(certDir(dataDir, id)); err != nil {
			return fmt.Errorf("acme-zertifikat %s entfernen: %w", id, err)
		}
	}
	return nil
}

// BackupFiles liefert die ACME-Dateien für die Sicherung: Name im Archiv zu
// Pfad auf der Platte. Konto, Konten bei acme-dns und Zertifikate gehören dazu, damit ein
// zurückgespieltes LCM weder ein neues Konto noch neue Zertifikate braucht.
func BackupFiles(dataDir string) (map[string]string, error) {
	files := map[string]string{}
	addIfExists := func(name, path string) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files[name] = path
		}
	}
	addIfExists(BackupPrefix+accountKeyFile, filepath.Join(baseDir(dataDir), accountKeyFile))
	regs, err := os.ReadDir(accountsDir(dataDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("acme-konten lesen: %w", err)
	}
	for _, e := range regs {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && idPattern.MatchString(id) {
			addIfExists(backupRegistration+e.Name(), filepath.Join(accountsDir(dataDir), e.Name()))
		}
	}
	dnsAccounts, err := os.ReadDir(acmeDNSDir(dataDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("acme-dns-konten lesen: %w", err)
	}
	for _, e := range dnsAccounts {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && idPattern.MatchString(id) {
			addIfExists(backupACMEDNS+e.Name(), filepath.Join(acmeDNSDir(dataDir), e.Name()))
		}
	}
	ids, err := certIDs(dataDir)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		for _, name := range []string{certFile, keyFile} {
			addIfExists(backupCert+id+"-"+name, filepath.Join(certDir(dataDir, id), name))
		}
	}
	return files, nil
}

// RestoreTarget sagt, wohin eine Datei aus dem Archiv beim Zurückspielen
// gehört. ok ist false, wenn der Name keine ACME-Datei ist.
func RestoreTarget(dataDir, archiveName string) (path string, ok bool) {
	if archiveName == BackupPrefix+accountKeyFile {
		return filepath.Join(baseDir(dataDir), accountKeyFile), true
	}
	if rest, found := strings.CutPrefix(archiveName, backupRegistration); found {
		id, isJSON := strings.CutSuffix(rest, ".json")
		if !isJSON || !idPattern.MatchString(id) {
			return "", false
		}
		return filepath.Join(accountsDir(dataDir), rest), true
	}
	if rest, found := strings.CutPrefix(archiveName, backupACMEDNS); found {
		id, isJSON := strings.CutSuffix(rest, ".json")
		if !isJSON || !idPattern.MatchString(id) {
			return "", false
		}
		return filepath.Join(acmeDNSDir(dataDir), rest), true
	}
	rest, found := strings.CutPrefix(archiveName, backupCert)
	if !found {
		return "", false
	}
	id, name, found := strings.Cut(rest, "-")
	if !found || !idPattern.MatchString(id) || (name != certFile && name != keyFile) {
		return "", false
	}
	return filepath.Join(certDir(dataDir, id), name), true
}
