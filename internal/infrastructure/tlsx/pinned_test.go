package tlsx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCert legt ein Zertifikat samt Schlüssel im Datenverzeichnis ab.
func writeCert(t *testing.T, dir string, certDER []byte, key *ecdsa.PrivateKey, chain ...[]byte) {
	t.Helper()
	pemData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	for _, c := range chain {
		pemData = append(pemData, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c})...)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, CertFileName), pemData, 0o644); err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}

// issue stellt ein Zertifikat aus; parent nil heißt selbstsigniert.
func issue(t *testing.T, cn string, isCA bool, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

// handshake liefert über TLS das Zertifikat served aus und
// meldet, ob der Client mit PinnedTo(pinDir) den Handshake annimmt.
func handshake(t *testing.T, served tls.Certificate, pinDir string) error {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{served}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c.(*tls.Conn).Handshake()
		_ = c.Close()
	}()
	conn, err := tls.Dial("tcp", ln.Addr().String(), PinnedTo(pinDir))
	if err == nil {
		_ = conn.Close()
	}
	return err
}

func loadPair(t *testing.T, dir string) tls.Certificate {
	t.Helper()
	pair, err := tls.LoadX509KeyPair(filepath.Join(dir, CertFileName), filepath.Join(dir, KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

// TestPinnedToNimmtAbgelaufenesZertifikat: Der Healthcheck fragt, ob der
// Dienst lebt. Ein abgelaufenes, aber genau das eigene Zertifikat ist kein
// Grund, den Container neu zu starten.
func TestPinnedToNimmtAbgelaufenesZertifikat(t *testing.T) {
	dir := t.TempDir()
	ca, err := EnsureCA(dir, time.Now().AddDate(-2, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureServerCert(dir, ca, time.Now().AddDate(-2, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if err := handshake(t, loadPair(t, dir), dir); err != nil {
		t.Errorf("eigenes, abgelaufenes Zertifikat abgelehnt: %v", err)
	}
}

// TestPinnedToLehntFremdesZertifikatDerselbenCA: Liegt die CA in der Datei
// (Betreiber-Zertifikat mit Kette), darf ein anderes Zertifikat derselben CA
// nicht als „das eigene" durchgehen.
func TestPinnedToLehntFremdesZertifikatDerselbenCA(t *testing.T) {
	caDER, caKey := issue(t, "Test-CA", true, nil, nil)
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	ownDER, ownKey := issue(t, "lcm.example", false, ca, caKey)
	otherDER, otherKey := issue(t, "fremd.example", false, ca, caKey)

	dir := t.TempDir()
	writeCert(t, dir, ownDER, ownKey, caDER)
	if err := handshake(t, loadPair(t, dir), dir); err != nil {
		t.Fatalf("eigenes Zertifikat abgelehnt: %v", err)
	}
	otherDir := t.TempDir()
	writeCert(t, otherDir, otherDER, otherKey, caDER)
	if err := handshake(t, loadPair(t, otherDir), dir); err == nil {
		t.Error("fremdes Zertifikat derselben CA angenommen")
	}
}
