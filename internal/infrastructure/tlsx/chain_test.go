package tlsx

import (
	"encoding/pem"
	"errors"
	"strings"
	"testing"
)

func block(typ string, body string) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: []byte(body)}))
}

// Aus einer Datei mit Zertifikaten und Schlüssel kommen allein die Zertifikate
// heraus, in der Reihenfolge der Datei.
func TestCertificateChainPEMKeepsOnlyCertificates(t *testing.T) {
	file := "Text davor\n" + block("CERTIFICATE", "leaf") + block("EC PRIVATE KEY", "geheim") +
		block("CERTIFICATE", "ca") + block("PRIVATE KEY", "noch-geheim") + "Text danach\n"

	chain, err := CertificateChainPEM([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	if want := block("CERTIFICATE", "leaf") + block("CERTIFICATE", "ca"); string(chain) != want {
		t.Errorf("Kette:\n%s\nerwartet:\n%s", chain, want)
	}
	if strings.Contains(string(chain), "PRIVATE KEY") {
		t.Error("ein Schlüssel ist in der Kette")
	}
}

func TestCertificateChainPEMDropsBlockHeaders(t *testing.T) {
	file := "-----BEGIN CERTIFICATE-----\nProc-Type: 4,ENCRYPTED\n\nbGVhZg==\n-----END CERTIFICATE-----\n"
	chain, err := CertificateChainPEM([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(chain), "Proc-Type") {
		t.Errorf("Kopfzeile in der Kette: %s", chain)
	}
}

func TestCertificateChainPEMNeedsACertificate(t *testing.T) {
	for _, file := range []string{"", "kein pem", block("PRIVATE KEY", "geheim")} {
		if _, err := CertificateChainPEM([]byte(file)); !errors.Is(err, ErrNoCertificate) {
			t.Errorf("%q: %v, erwartet ErrNoCertificate", file, err)
		}
	}
}
