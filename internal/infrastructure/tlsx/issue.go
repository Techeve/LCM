package tlsx

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"LCM/internal/remote/wire"
)

// Server-Zertifikat, ausgestellt von der CA.
//
// Es gilt 90 Tage und wird erneuert, wenn weniger als 60 übrig sind - also
// monatlich. Die Datei lcm-cert.pem enthält das Zertifikat und dahinter die
// CA: So bekommt der Agent beim Handshake die Kette und erkennt die gepinnte
// CA darin.

// ServerCertValidity ist die Laufzeit des Server-Zertifikats.
const ServerCertValidity = 90 * 24 * time.Hour

// EnsureServerCert stellt sicher, dass Zertifikat und Schlüssel im
// Datenverzeichnis existieren; fehlt eins, stellt die CA ein neues mit neuem
// Schlüssel aus. Ein vom Betreiber hinterlegtes Zertifikat bleibt, wie es ist.
// Ein beim Absturz unterbrochener Tausch wird vorher zu Ende gebracht.
func EnsureServerCert(dataDir string, ca *CA, notBefore time.Time) (certPath, keyPath string, err error) {
	certPath = filepath.Join(dataDir, CertFileName)
	keyPath = filepath.Join(dataDir, KeyFileName)
	recoverPair(certPath, keyPath)
	if fileExists(certPath) && fileExists(keyPath) {
		return certPath, keyPath, nil
	}
	if err := IssueServerCert(dataDir, ca, notBefore, true); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

// IssueServerCert stellt das Server-Zertifikat aus, gültig ab notBefore.
// newKey=false behält den Schlüssel des bisherigen Zertifikats - so nehmen
// Agenten, die erst den Schlüssel pinnen, auch das erste Zertifikat der CA an
// und lernen die CA daraus.
func IssueServerCert(dataDir string, ca *CA, notBefore time.Time, newKey bool) error {
	if ca == nil {
		return errors.New("keine ca zum ausstellen")
	}
	certPath := filepath.Join(dataDir, CertFileName)
	keyPath := filepath.Join(dataDir, KeyFileName)
	signer, keyPEM, err := serverKey(certPath, keyPath, newKey)
	if err != nil {
		return err
	}
	der, err := ca.issue(signer.Public(), notBefore)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	certPEM = append(certPEM, ca.PEM()...)
	return writePair(certPath, keyPath, certPEM, keyPEM)
}

// serverKey liefert den Schlüssel fürs neue Zertifikat: einen neuen (samt
// PEM zum Ablegen) oder den bisherigen (PEM nil - die Datei bleibt).
func serverKey(certPath, keyPath string, newKey bool) (crypto.Signer, []byte, error) {
	if newKey {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		keyPEM, err := marshalKey(key)
		return key, keyPEM, err
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("zertifikat und schlüssel laden: %w", err)
	}
	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, nil, errors.New("der schlüssel kann nicht signieren")
	}
	return signer, nil, nil
}

// issue stellt das Server-Zertifikat für pub aus. Es endet spätestens mit
// der CA.
func (ca *CA) issue(pub crypto.PublicKey, notBefore time.Time) ([]byte, error) {
	tmpl, err := serverTemplate(notBefore)
	if err != nil {
		return nil, err
	}
	if tmpl.NotAfter.After(ca.Cert.NotAfter) {
		tmpl.NotAfter = ca.Cert.NotAfter
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, pub, ca.key)
	if err != nil {
		return nil, fmt.Errorf("server-zertifikat ausstellen: %w", err)
	}
	return der, nil
}

// serverTemplate: Namen wie beim früheren selbstsignierten Zertifikat, dazu
// die Kennung wire.ServerCertMarker, an der Agenten und Inspect das
// Zertifikat von LCM erkennen.
func serverTemplate(notBefore time.Time) (*x509.Certificate, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	marker, err := url.Parse(wire.ServerCertMarker)
	if err != nil {
		return nil, err
	}
	return &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "LCM", Organization: []string{"LCM"}},
		NotBefore:             notBefore,
		NotAfter:              notBefore.Add(ServerCertValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		URIs:                  []*url.URL{marker},
		BasicConstraintsValid: true,
	}, nil
}

func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}

// writePair legt Zertifikat und Schlüssel ab. Ohne neuen Schlüssel (keyPEM
// nil) wird nur das Zertifikat getauscht. Mit neuem liegen erst beide als
// .new daneben, dann rückt der Schlüssel an seinen Platz, zuletzt das
// Zertifikat - was ein Absturz dazwischen hinterlässt, räumt recoverPair
// beim nächsten Start auf.
func writePair(certPath, keyPath string, certPEM, keyPEM []byte) error {
	if keyPEM == nil {
		return replaceFile(certPath, certPEM, 0o644)
	}
	if err := writeFresh(keyPath+".new", keyPEM, 0o600); err != nil {
		return fmt.Errorf("tls-key speichern: %w", err)
	}
	if err := writeFresh(certPath+".new", certPEM, 0o644); err != nil {
		_ = os.Remove(keyPath + ".new")
		return fmt.Errorf("zertifikat speichern: %w", err)
	}
	if err := os.Rename(keyPath+".new", keyPath); err != nil {
		return fmt.Errorf("tls-key ablegen: %w", err)
	}
	if err := os.Rename(certPath+".new", certPath); err != nil {
		return fmt.Errorf("zertifikat ablegen: %w", err)
	}
	return nil
}

// writeFresh schreibt eine neue Datei mit genau diesen Rechten - eine
// liegengebliebene behielte beim Überschreiben ihre alten.
func writeFresh(path string, data []byte, mode os.FileMode) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// recoverPair bringt einen unterbrochenen Tausch (writePair) zu Ende: Liegt
// das neue Zertifikat noch daneben, gehört es zum neuen Schlüssel - der liegt
// schon an seinem Platz oder noch daneben. Passt nichts zusammen, war der
// Tausch noch nicht begonnen, und die Reste fliegen weg.
func recoverPair(certPath, keyPath string) {
	newCert, newKey := certPath+".new", keyPath+".new"
	if !fileExists(newCert) {
		_ = os.Remove(newKey)
		return
	}
	key := keyPath
	if fileExists(newKey) {
		key = newKey
	}
	if _, err := tls.LoadX509KeyPair(newCert, key); err != nil {
		_ = os.Remove(newCert)
		_ = os.Remove(newKey)
		return
	}
	if key == newKey {
		_ = os.Rename(newKey, keyPath)
	}
	_ = os.Rename(newCert, certPath)
}
