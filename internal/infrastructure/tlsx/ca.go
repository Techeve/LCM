package tlsx

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Die eigene CA von LCM.
//
// Agenten pinnen die CA statt eines einzelnen Zertifikats: Das
// Server-Zertifikat gilt nur 90 Tage, wird monatlich mit neuem Schlüssel
// erneuert, und jeder Agent nimmt es an, weil die gepinnte CA es ausgestellt
// hat. Die CA entsteht beim ersten Start im Datenverzeichnis und gilt 20
// Jahre. Alternativ hinterlegt der Betreiber eine eigene CA (etwa eine
// Zwischen-CA seiner PKI) über tls_ca_cert/tls_ca_key in der config.json -
// dann erzeugt LCM keine.

// Dateinamen der eigenen CA im Datenverzeichnis.
const (
	CAFileName    = "lcm-ca.pem"
	CAKeyFileName = "lcm-ca-key.pem"
)

// caValidityYears ist die Laufzeit der eigenen CA.
const caValidityYears = 20

// CA stellt die Server-Zertifikate aus.
type CA struct {
	// Cert ist das ausstellende CA-Zertifikat.
	Cert *x509.Certificate
	// chain sind Cert und die Zertifikate dahinter aus derselben Datei (bei
	// einer Zwischen-CA des Betreibers), DER - so liefert LCM sie aus.
	chain [][]byte
	key   crypto.Signer
	// Own: die CA im Datenverzeichnis, die LCM selbst erzeugt hat.
	Own bool
}

// Fingerprint ist der Pin der CA: SHA-256 über das CA-Zertifikat, hex.
func (ca *CA) Fingerprint() string {
	return fingerprintOf(ca.Cert.Raw)
}

// PEM liefert den öffentlichen Teil (CA samt Kette dahinter) zum Import in
// Browser oder Reverse-Proxy.
func (ca *CA) PEM() []byte {
	var out []byte
	for _, der := range ca.chain {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	return out
}

// OpenCA liefert die CA: die des Betreibers, wenn certFile und keyFile
// gesetzt sind, sonst die eigene im Datenverzeichnis (beim ersten Start
// erzeugt).
func OpenCA(dataDir, certFile, keyFile string, notBefore time.Time) (*CA, error) {
	if certFile == "" && keyFile == "" {
		return EnsureCA(dataDir, notBefore)
	}
	if certFile == "" || keyFile == "" {
		return nil, errors.New("tls_ca_cert und tls_ca_key gehören zusammen")
	}
	return LoadCA(certFile, keyFile)
}

// EnsureCA lädt die eigene CA und erzeugt sie, wenn ihr Zertifikat fehlt.
// Ein Schlüssel ohne Zertifikat stammt aus einem abgebrochenen ersten
// Anlegen und wird ersetzt. Fehlt dagegen nur der Schlüssel, bricht LCM ab:
// Eine neue CA hieße neue Pins, und die Agenten vertrauen der alten.
func EnsureCA(dataDir string, notBefore time.Time) (*CA, error) {
	certPath := filepath.Join(dataDir, CAFileName)
	keyPath := filepath.Join(dataDir, CAKeyFileName)
	if !fileExists(certPath) {
		if err := createCA(certPath, keyPath, notBefore); err != nil {
			return nil, err
		}
	}
	ca, err := LoadCA(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	ca.Own = true
	return ca, nil
}

// createCA erzeugt Schlüssel (P-384, 0600) und Zertifikat der eigenen CA.
// Der Schlüssel liegt zuerst auf der Platte, das Zertifikat zuletzt.
func createCA(certPath, keyPath string, notBefore time.Time) error {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl, err := caTemplate(notBefore)
	if err != nil {
		return err
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return fmt.Errorf("ca ausstellen: %w", err)
	}
	keyPEM, err := marshalKey(key)
	if err != nil {
		return err
	}
	if err := replaceFile(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	return replaceFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

// caTemplate: Wurzel-CA, die nur Server-Zertifikate ausstellt (pathlen 0).
func caTemplate(notBefore time.Time) (*x509.Certificate, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	return &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "LCM Root CA", Organization: []string{"LCM"}},
		NotBefore:             notBefore,
		NotAfter:              notBefore.AddDate(caValidityYears, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}, nil
}

// LoadCA liest CA-Zertifikat (samt Kette dahinter) und Schlüssel und prüft,
// dass beide zusammengehören und die CA Zertifikate ausstellen darf.
func LoadCA(certPath, keyPath string) (*CA, error) {
	chain, err := readCertificates(certPath)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return nil, fmt.Errorf("ca-zertifikat in %s: %w", certPath, err)
	}
	if !cert.IsCA || (cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0) {
		return nil, fmt.Errorf("%s ist kein ca-zertifikat", certPath)
	}
	key, err := readPrivateKey(keyPath)
	if err != nil {
		return nil, err
	}
	if !samePublicKey(cert.PublicKey, key.Public()) {
		return nil, fmt.Errorf("schlüssel %s gehört nicht zum ca-zertifikat %s", keyPath, certPath)
	}
	return &CA{Cert: cert, chain: chain, key: key}, nil
}

// readCertificates liest alle Zertifikate einer PEM-Datei (DER).
func readCertificates(path string) ([][]byte, error) {
	rest, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			out = append(out, block.Bytes)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("keine zertifikatsdaten in %s", path)
	}
	return out, nil
}

// readPrivateKey liest einen privaten Schlüssel (PKCS#8, EC oder PKCS#1).
// Die Fehlermeldung nennt nur die Datei, nie den Inhalt.
func readPrivateKey(path string) (crypto.Signer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("kein schlüssel in %s", path)
	}
	key, err := parsePrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("schlüssel in %s nicht lesbar", path)
	}
	return key, nil
}

func parsePrivateKey(der []byte) (crypto.Signer, error) {
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("unbekanntes schlüsselformat")
}

// marshalKey kodiert einen Schlüssel als PKCS#8-PEM.
func marshalKey(key crypto.Signer) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// samePublicKey vergleicht zwei öffentliche Schlüssel (alle Schlüsseltypen
// der Standardbibliothek kennen Equal).
func samePublicKey(a, b crypto.PublicKey) bool {
	pub, ok := a.(interface{ Equal(crypto.PublicKey) bool })
	return ok && pub.Equal(b)
}
