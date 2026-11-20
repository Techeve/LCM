package tlsx

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPublicKeyPinPasstZumGeliefertenZertifikat: Der Pin im Join-Befehl muss
// genau der sein, den curl --pinnedpubkey am Zertifikat errechnet, das LCM
// ausliefert - SHA-256 über die SubjectPublicKeyInfo, Base64. Ein Pin über
// das ganze Zertifikat (wie der Fingerprint fürs Token) passte nie.
func TestPublicKeyPinPasstZumGeliefertenZertifikat(t *testing.T) {
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

	pin, err := PublicKeyPin(certPath)
	if err != nil {
		t.Fatalf("PublicKeyPin: %v", err)
	}
	if pin != want {
		t.Errorf("Pin %q, curl erwartet %q", pin, want)
	}
	fp, _ := Fingerprint(certPath)
	if pin == fp {
		t.Error("Pin und Fingerprint dürfen nicht dasselbe sein")
	}
}

// TestPublicKeyPinOhneZertifikat: Ohne lesbares Zertifikat gibt es keinen
// Pin - der Aufrufer fällt dann auf die normale Prüfung zurück.
func TestPublicKeyPinOhneZertifikat(t *testing.T) {
	dir := t.TempDir()
	if _, err := PublicKeyPin(filepath.Join(dir, "fehlt.pem")); err == nil {
		t.Error("fehlende Datei muss ein Fehler sein")
	}
	kaputt := filepath.Join(dir, "kaputt.pem")
	if err := os.WriteFile(kaputt, []byte("kein pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PublicKeyPin(kaputt); err == nil {
		t.Error("Datei ohne Zertifikat muss ein Fehler sein")
	}
}
