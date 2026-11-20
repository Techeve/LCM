// Package tlsxtest hilft Tests anderer Pakete, die das TLS-Setup von LCM
// brauchen: die eigene CA samt Server-Zertifikat oder das selbstsignierte
// Zertifikat früherer Fassungen (für die Tests des Umstiegs).
package tlsxtest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"LCM/internal/infrastructure/tlsx"
)

// ServerCert legt CA und Server-Zertifikat im Datenverzeichnis an, beide
// gültig ab notBefore.
func ServerCert(t testing.TB, dir string, notBefore time.Time) *tlsx.CA {
	t.Helper()
	ca, err := tlsx.EnsureCA(dir, notBefore)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tlsx.EnsureServerCert(dir, ca, notBefore); err != nil {
		t.Fatal(err)
	}
	return ca
}

// LegacySelfSigned legt das selbstsignierte Zertifikat ab, wie LCM bis R4
// es erzeugt hat: ein Jahr gültig ab notBefore, Schlüssel als EC PRIVATE KEY.
func LegacySelfSigned(t testing.TB, dir string, notBefore time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(notBefore.UnixNano()),
		Subject:               pkix.Name{CommonName: "LCM", Organization: []string{"LCM"}},
		NotBefore:             notBefore,
		NotAfter:              notBefore.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		BasicConstraintsValid: true,
	}
	writeSelfSigned(t, dir, tmpl, key)
}

// ForeignCert legt ein Zertifikat ab, wie es ein Betreiber hinterlegt:
// selbstsigniert für lcm.example, ohne die Kennung von LCM, gültig bis
// notAfter.
func ForeignCert(t testing.TB, dir string, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(notAfter.UnixNano()),
		Subject:      pkix.Name{CommonName: "lcm.example"},
		DNSNames:     []string{"lcm.example"},
		NotBefore:    notAfter.AddDate(0, -3, 0),
		NotAfter:     notAfter,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	writeSelfSigned(t, dir, tmpl, key)
}

// writeSelfSigned signiert tmpl mit key und legt Zertifikat und Schlüssel ab.
func writeSelfSigned(t testing.TB, dir string, tmpl *x509.Certificate, key *ecdsa.PrivateKey) {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, tlsx.CertFileName), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	write(t, filepath.Join(dir, tlsx.KeyFileName), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
}

func write(t testing.TB, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
