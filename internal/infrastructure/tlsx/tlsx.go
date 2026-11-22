// Package tlsx stellt das TLS-Setup des integrierten Webservers bereit.
//
// Sicherheitsvorgabe: LCM kommuniziert im produktiven Betrieb zwingend
// über HTTPS. Beim ersten Start entsteht eine eigene CA (ca.go), die das
// Server-Zertifikat ausstellt und monatlich erneuert (issue.go) -
// out-of-the-box HTTPS. Eigene Zertifikate kommen über den Pool dazu
// (pool.go): Er liefert beim Handshake das zum Namen passende aus, und das
// der CA bleibt der Rückfall. Unverschlüsseltes HTTP ist nur im
// Entwicklungsmodus (--dev) erlaubt.
package tlsx

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
)

// Dateinamen des Server-Zertifikats im Datenverzeichnis.
const (
	CertFileName = "lcm-cert.pem"
	KeyFileName  = "lcm-key.pem"
)

// Fingerprint liefert den SHA-256-Fingerprint (hex, klein geschrieben) des
// ersten Zertifikats in der PEM-Datei - für das Cert-Pinning der lcm-agents
// (LCM Remote): der Fingerprint wird ins Enrollment-Token eingebettet und
// vom Agent beim TLS-Handshake gegen das präsentierte Zertifikat geprüft.
func Fingerprint(certPath string) (string, error) {
	der, err := firstCertificate(certPath)
	if err != nil {
		return "", err
	}
	return fingerprintOf(der), nil
}

// fingerprintOf ist der SHA-256 eines Zertifikats (DER), hex.
func fingerprintOf(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// CurlKeyPin liefert den Pin des öffentlichen Schlüssels im Format von
// curl --pinnedpubkey: SHA-256 über die SubjectPublicKeyInfo, Base64. Damit
// nimmt der Join-Befehl das Zertifikat der eigenen CA nur an, wenn es den
// Schlüssel genau dieses LCM trägt - ein -k ohne Pin nähme jedes an. Der
// Schlüssel wechselt monatlich, der Befehl wird aber jedes Mal frisch
// erzeugt.
func CurlKeyPin(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// CertFingerprint ist der SHA-256 eines Zertifikats (hex), wie ihn der Agent
// im Token führt.
func CertFingerprint(cert *x509.Certificate) string {
	return fingerprintOf(cert.Raw)
}

// firstCertificate liest das erste Zertifikat einer PEM-Datei - das des
// Servers selbst, dahinter folgt gegebenenfalls die Kette.
func firstCertificate(certPath string) ([]byte, error) {
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("keine zertifikatsdaten in %s", certPath)
	}
	return block.Bytes, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
