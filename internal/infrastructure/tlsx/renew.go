package tlsx

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"LCM/internal/remote/wire"
)

// Stand des Server-Zertifikats.
//
// Bis 2.0 lieferte LCM ein selbstsigniertes Zertifikat aus, und die Agenten
// pinnten es über den Fingerprint, ab R2 zusätzlich über den Schlüssel
// (KeyPin). Heute stellt die CA das Zertifikat aus. Inspect sagt, welches
// gerade liegt, damit die Erneuerung (services.CertRenewal) den Umstieg
// steuern kann.

// CertInfo beschreibt das Zertifikat im Datenverzeichnis.
type CertInfo struct {
	NotAfter time.Time
	// Own: LCM verwaltet es - das alte selbstsignierte oder eins mit der
	// Kennung wire.ServerCertMarker. Nur das erneuert LCM; ein vom Betreiber
	// hinterlegtes tauscht der Betreiber.
	Own bool
	// FromCA: die aktuelle CA hat es ausgestellt. Ein eigenes Zertifikat
	// ohne das (das alte selbstsignierte oder eins einer früheren CA) stellt
	// LCM auf die aktuelle CA um.
	FromCA bool
	// KeyPin ist der Pin des öffentlichen Schlüssels (siehe KeyPinOf).
	KeyPin string
	// Fingerprint ist der SHA-256 des Zertifikats (hex).
	Fingerprint string
}

// Inspect liest das Zertifikat im Datenverzeichnis; ca ist die aktuelle CA
// (nil = keine).
func Inspect(dataDir string, ca *CA) (CertInfo, error) {
	der, err := firstCertificate(filepath.Join(dataDir, CertFileName))
	if err != nil {
		return CertInfo{}, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return CertInfo{}, fmt.Errorf("zertifikat lesen: %w", err)
	}
	return CertInfo{
		NotAfter:    cert.NotAfter,
		Own:         isLegacySelfSigned(cert) || hasMarker(cert),
		FromCA:      hasMarker(cert) && issuedBy(cert, ca),
		KeyPin:      KeyPinOf(cert),
		Fingerprint: fingerprintOf(der),
	}, nil
}

// KeyPinOf liefert den Pin des öffentlichen Schlüssels, wie ihn der Agent
// führt: SHA-256 über die SubjectPublicKeyInfo, hex, klein geschrieben.
func KeyPinOf(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

// isLegacySelfSigned erkennt das selbstsignierte Zertifikat früherer
// Fassungen: Name und Organisation „LCM", Aussteller gleich Inhaber, mit
// sich selbst signiert.
func isLegacySelfSigned(cert *x509.Certificate) bool {
	if cert.Subject.CommonName != "LCM" || len(cert.Subject.Organization) != 1 ||
		cert.Subject.Organization[0] != "LCM" {
		return false
	}
	if cert.Issuer.String() != cert.Subject.String() {
		return false
	}
	// Nicht CheckSignatureFrom: Das verlangt eine CA, und das alte
	// Zertifikat ist keine.
	return cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil
}

// hasMarker: das Zertifikat trägt die Kennung, die nur LCM vergibt.
func hasMarker(cert *x509.Certificate) bool {
	for _, uri := range cert.URIs {
		if uri.String() == wire.ServerCertMarker {
			return true
		}
	}
	return false
}

// issuedBy: ca hat das Zertifikat ausgestellt.
func issuedBy(cert *x509.Certificate, ca *CA) bool {
	return ca != nil && bytes.Equal(cert.RawIssuer, ca.Cert.RawSubject) && cert.CheckSignatureFrom(ca.Cert) == nil
}

// replaceFile schreibt data unter path, über eine Zwischendatei daneben.
func replaceFile(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("zertifikat speichern: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("zertifikat ablegen: %w", err)
	}
	return nil
}

// CertStore hält das ausgelieferte Zertifikat und tauscht es ohne Neustart:
// Der Listener fragt bei jedem Handshake GetCertificate, Reload liest die
// Dateien neu. Laufende Verbindungen behalten ihr Zertifikat.
type CertStore struct {
	certPath, keyPath string
	current           atomic.Pointer[tls.Certificate]
}

// NewCertStore lädt Zertifikat und Schlüssel.
func NewCertStore(certPath, keyPath string) (*CertStore, error) {
	s := &CertStore{certPath: certPath, keyPath: keyPath}
	return s, s.Reload()
}

// Reload liest Zertifikat und Schlüssel neu. Scheitert das, bleibt das
// bisherige Zertifikat in Betrieb.
func (s *CertStore) Reload() error {
	pair, err := tls.LoadX509KeyPair(s.certPath, s.keyPath)
	if err != nil {
		return fmt.Errorf("zertifikat laden: %w", err)
	}
	s.current.Store(&pair)
	return nil
}

// TLSConfig ist die Server-Konfiguration für den Listener.
func (s *CertStore) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return s.current.Load(), nil
		},
	}
}
