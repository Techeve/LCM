package tlsx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// legacySelfSigned legt das selbstsignierte Zertifikat früherer Fassungen ab
// (wie tlsxtest.LegacySelfSigned, das hier wegen des Importzyklus fehlt).
func legacySelfSigned(t *testing.T, dir string, notBefore time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "LCM", Organization: []string{"LCM"}},
		NotBefore:             notBefore,
		NotAfter:              notBefore.AddDate(1, 0, 0),
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	writeCert(t, dir, der, key)
}

// newCA legt die eigene CA im Datenverzeichnis an.
func newCA(t *testing.T, dir string) *CA {
	t.Helper()
	ca, err := EnsureCA(dir, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func inspect(t *testing.T, dir string, ca *CA) CertInfo {
	t.Helper()
	info, err := Inspect(dir, ca)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// TestInspectUnterscheidetDieZertifikate: altes selbstsigniertes, von der
// CA ausgestelltes und vom Betreiber hinterlegtes Zertifikat.
func TestInspectUnterscheidetDieZertifikate(t *testing.T) {
	legacy := t.TempDir()
	ca := newCA(t, legacy)
	legacySelfSigned(t, legacy, time.Now().Add(-time.Hour))
	if info := inspect(t, legacy, ca); !info.Own || info.FromCA {
		t.Errorf("altes Zertifikat: %+v", info)
	}

	fromCA := t.TempDir()
	ca2 := newCA(t, fromCA)
	if _, _, err := EnsureServerCert(fromCA, ca2, time.Now()); err != nil {
		t.Fatal(err)
	}
	if info := inspect(t, fromCA, ca2); !info.Own || !info.FromCA {
		t.Errorf("Zertifikat der CA: %+v", info)
	}
	// Gegen eine andere CA: eigenes Zertifikat, aber nicht von dieser CA.
	if info := inspect(t, fromCA, ca); !info.Own || info.FromCA {
		t.Errorf("Zertifikat einer früheren CA: %+v", info)
	}

	foreign := t.TempDir()
	der, key := issue(t, "lcm.example", false, nil, nil)
	writeCert(t, foreign, der, key)
	if info := inspect(t, foreign, ca); info.Own {
		t.Error("Betreiber-Zertifikat als eigenes erkannt")
	}
}

// TestIssueServerCertSchritt1BehaeltSchluessel: Der Umstieg vom alten
// selbstsignierten Zertifikat behält den Schlüssel - Agenten mit
// Schlüssel-Pin nehmen das erste Zertifikat der CA an.
func TestIssueServerCertSchritt1BehaeltSchluessel(t *testing.T) {
	dir := t.TempDir()
	ca := newCA(t, dir)
	legacySelfSigned(t, dir, time.Now().AddDate(-1, 0, 10))
	before := inspect(t, dir, ca)

	if err := IssueServerCert(dir, ca, time.Now().Add(-time.Hour), false); err != nil {
		t.Fatal(err)
	}
	after := inspect(t, dir, ca)
	if after.Fingerprint == before.Fingerprint {
		t.Fatal("nichts ausgestellt")
	}
	if after.KeyPin != before.KeyPin {
		t.Error("Schlüssel gewechselt")
	}
	if !after.FromCA {
		t.Error("neues Zertifikat nicht von der CA")
	}
	if _, err := os.Stat(filepath.Join(dir, CertFileName+".new")); !os.IsNotExist(err) {
		t.Error("Zwischendatei liegengeblieben")
	}
}

// TestIssueServerCertMitNeuemSchluessel: Die monatliche Erneuerung tauscht
// den Schlüssel, der Schlüssel bleibt 0600.
func TestIssueServerCertMitNeuemSchluessel(t *testing.T) {
	dir := t.TempDir()
	ca := newCA(t, dir)
	if _, _, err := EnsureServerCert(dir, ca, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	before := inspect(t, dir, ca)
	if err := IssueServerCert(dir, ca, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	after := inspect(t, dir, ca)
	if after.KeyPin == before.KeyPin {
		t.Error("Schlüssel nicht gewechselt")
	}
	if mode := fileMode(t, filepath.Join(dir, KeyFileName)); mode != 0o600 {
		t.Errorf("Schlüssel mit Rechten %o", mode)
	}
	loadPair(t, dir) // Zertifikat und Schlüssel gehören zusammen
}

// TestServerCertKetteUndLaufzeit: lcm-cert.pem enthält Zertifikat und CA,
// das Zertifikat gilt 90 Tage, trägt die Kennung und die alten Namen.
func TestServerCertKetteUndLaufzeit(t *testing.T) {
	dir := t.TempDir()
	ca := newCA(t, dir)
	notBefore := time.Now().Add(-time.Hour)
	if _, _, err := EnsureServerCert(dir, ca, notBefore); err != nil {
		t.Fatal(err)
	}
	pair := loadPair(t, dir)
	if len(pair.Certificate) != 2 || string(pair.Certificate[1]) != string(ca.Cert.Raw) {
		t.Fatalf("Kette mit %d Zertifikaten, erwartet Zertifikat + CA", len(pair.Certificate))
	}
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	if got := leaf.NotAfter.Sub(leaf.NotBefore); got != ServerCertValidity {
		t.Errorf("Laufzeit %s", got)
	}
	if !hasMarker(leaf) || leaf.IsCA || len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "localhost" {
		t.Errorf("Zertifikat: marker=%v ca=%v dns=%v", hasMarker(leaf), leaf.IsCA, leaf.DNSNames)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "localhost"}); err != nil {
		t.Errorf("Kette prüft nicht: %v", err)
	}
}

// TestRecoverPairBeendetUnterbrochenenTausch: Ein Absturz nach dem neuen
// Schlüssel, aber vor dem neuen Zertifikat hinterlässt kein unpassendes Paar.
func TestRecoverPairBeendetUnterbrochenenTausch(t *testing.T) {
	dir := t.TempDir()
	ca := newCA(t, dir)
	certPath, keyPath, err := EnsureServerCert(dir, ca, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Den Tausch von Hand nachstellen: neuer Schlüssel schon am Platz, das
	// neue Zertifikat liegt noch daneben.
	signer, keyPEM, err := serverKey(certPath, keyPath, true)
	if err != nil {
		t.Fatal(err)
	}
	der, err := ca.issue(signer.Public(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath+".new", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := EnsureServerCert(dir, ca, time.Now()); err != nil {
		t.Fatal(err)
	}
	loadPair(t, dir)
	if _, err := os.Stat(certPath + ".new"); !os.IsNotExist(err) {
		t.Error("neues Zertifikat liegt noch daneben")
	}
}

// TestCertStoreTauschtOhneNeustart: Nach Reload liefert der Listener das
// erneuerte Zertifikat samt Kette aus.
func TestCertStoreTauschtOhneNeustart(t *testing.T) {
	dir := t.TempDir()
	ca := newCA(t, dir)
	certPath, keyPath, err := EnsureServerCert(dir, ca, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewCertStore(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	served := func() [][]byte {
		c, _ := store.TLSConfig().GetCertificate(nil)
		return c.Certificate
	}
	first := served()[0]
	if err := IssueServerCert(dir, ca, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	if string(served()[0]) == string(first) {
		t.Error("nach Reload noch das alte Zertifikat")
	}
	if len(served()) != 2 {
		t.Error("ausgeliefert ohne CA in der Kette")
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
