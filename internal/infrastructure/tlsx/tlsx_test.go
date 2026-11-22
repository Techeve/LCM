package tlsx

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"testing"
	"time"
)

// TestCurlKeyPinPasstZumGeliefertenZertifikat: Der Pin im Join-Befehl muss
// genau der sein, den curl --pinnedpubkey am Zertifikat errechnet, das LCM
// ausliefert - SHA-256 über die SubjectPublicKeyInfo, Base64. Ein Pin über
// das ganze Zertifikat (wie der Fingerprint fürs Token) passte nie.
func TestCurlKeyPinPasstZumGeliefertenZertifikat(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, err := EnsureServerCert(dir, newCA(t, dir), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	served, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(served.RawSubjectPublicKeyInfo)
	want := base64.StdEncoding.EncodeToString(sum[:])

	pin := CurlKeyPin(served)
	if pin != want {
		t.Errorf("Pin %q, curl erwartet %q", pin, want)
	}
	fp, _ := Fingerprint(certPath)
	if pin == fp || CertFingerprint(served) != fp {
		t.Error("Pin und Fingerprint dürfen nicht dasselbe sein, der Fingerprint muss zur Datei passen")
	}
}
