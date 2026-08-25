// Package tlsx stellt das TLS-Setup des integrierten Webservers bereit.
//
// Sicherheitsvorgabe: LCM kommuniziert im produktiven Betrieb zwingend
// über HTTPS. Beim ersten Start wird automatisch ein Self-Signed-
// Zertifikat erzeugt (out-of-the-box HTTPS); alternativ kann ein eigenes
// Zertifikat hinterlegt werden. Unverschlüsseltes HTTP ist nur im
// Entwicklungsmodus (--dev) erlaubt.
package tlsx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Dateinamen des Self-Signed-Zertifikats im Datenverzeichnis.
const (
	CertFileName = "lcm-cert.pem"
	KeyFileName  = "lcm-key.pem"
)

// Fingerprint liefert den SHA-256-Fingerprint (hex, klein geschrieben) des
// ersten Zertifikats in der PEM-Datei - für das Cert-Pinning der lcm-agents
// (LCM Remote): der Fingerprint wird ins Enrollment-Token eingebettet und
// vom Agent beim TLS-Handshake gegen das präsentierte Zertifikat geprüft.
func Fingerprint(certPath string) (string, error) {
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("keine zertifikatsdaten in %s", certPath)
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

// EnsureSelfSigned stellt sicher, dass Zertifikat und Schlüssel im
// Datenverzeichnis existieren, und erzeugt sie beim ersten Start.
// Liefert die Pfade zu Zertifikat und Schlüssel.
func EnsureSelfSigned(dataDir string, notBefore time.Time) (certPath, keyPath string, err error) {
	certPath = filepath.Join(dataDir, CertFileName)
	keyPath = filepath.Join(dataDir, KeyFileName)

	if fileExists(certPath) && fileExists(keyPath) {
		return certPath, keyPath, nil
	}

	certPEM, keyPEM, err := generateSelfSigned(notBefore)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return "", "", fmt.Errorf("zertifikat speichern: %w", err)
	}
	// Der Private Key gehört nur dem Service-User (0600).
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return "", "", fmt.Errorf("tls-key speichern: %w", err)
	}
	return certPath, keyPath, nil
}

// generateSelfSigned erzeugt ein ECDSA-Zertifikat (P-256), gültig für
// localhost und die lokalen IPs, ein Jahr lang.
func generateSelfSigned(notBefore time.Time) (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "LCM", Organization: []string{"LCM"}},
		NotBefore:             notBefore,
		NotAfter:              notBefore.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
