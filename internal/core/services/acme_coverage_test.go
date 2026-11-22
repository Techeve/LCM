package services_test

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

	"LCM/internal/infrastructure/acmex"
)

// failingCA: Nichts lauscht bei der CA, ein Lauf scheitert schnell.
const failingCA = "https://127.0.0.1:1/directory"

// autoFixture hat ACME eingerichtet, mit dem selbst ausstellenden Dienst, ohne
// dass der erste Lauf schon lief.
func autoFixture(t *testing.T) *acmeFixture {
	t.Helper()
	f := newACMEFixture(t)
	f.svc.WithAutoIssue()
	return f
}

func enableACME(t *testing.T, f *acmeFixture) {
	t.Helper()
	in := validUpdate()
	in.DirectoryURL = failingCA
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}
}

// failedRuns zählt die Läufe, die gescheitert sind - jeder Lauf gegen die
// fehlende CA hinterlässt einen Eintrag.
func failedRuns(t *testing.T, f *acmeFixture) int64 {
	t.Helper()
	waitIdle(t, f.svc)
	return auditCount(t, f.env, "settings.acme.failed")
}

// Nach dem Speichern von Let's Encrypt (erstes Einrichten) stellt LCM von selbst
// aus, ohne „Jetzt ausstellen“.
func TestACMESaveStartsARunWhenNoCertificateCoversTheNames(t *testing.T) {
	f := autoFixture(t)
	enableACME(t, f)

	if n := failedRuns(t, f); n != 1 {
		t.Fatalf("Läufe nach dem Speichern: %d, erwartet 1", n)
	}
	if v, _ := f.svc.View(); v.LastRunAt == nil || v.LastError == "" {
		t.Errorf("Zustand: %+v", v)
	}
}

// Ohne den selbst ausstellenden Dienst startet das Speichern nichts (so laufen
// die übrigen Tests ohne Hintergrundlauf); EnsureCoverage selbst startet immer.
func TestACMESaveWithoutAutoIssueStartsNothing(t *testing.T) {
	f := newACMEFixture(t)
	enableACME(t, f)
	if n := failedRuns(t, f); n != 0 {
		t.Fatalf("Läufe nach dem Speichern: %d, erwartet 0", n)
	}
	f.svc.EnsureCoverage()
	if n := failedRuns(t, f); n != 1 {
		t.Errorf("Läufe nach EnsureCoverage: %d, erwartet 1", n)
	}
}

// Aus, nicht eingerichtet oder ohne Namen: nichts zu tun.
func TestACMEEnsureCoverageDoesNothingWhenNotConfigured(t *testing.T) {
	f := autoFixture(t)
	f.svc.EnsureCoverage() // aus
	if n := failedRuns(t, f); n != 0 {
		t.Fatalf("ACME aus: %d Läufe", n)
	}

	enableACME(t, f)
	waitIdle(t, f.svc)
	base := auditCount(t, f.env, "settings.acme.failed")
	f.names = nil
	f.svc.EnsureCoverage()
	if n := failedRuns(t, f); n != base {
		t.Errorf("ohne Namen: %d Läufe, erwartet %d", n, base)
	}
}

// Ein gültiges Zertifikat für diese CA und alle Namen genügt: kein Lauf. Ein
// abgelaufenes oder eines für andere Namen genügt nicht.
func TestACMEEnsureCoverageSkipsWhenCertificateCoversAllNames(t *testing.T) {
	f := newACMEFixture(t)
	in := validUpdate()
	in.DirectoryURL = failingCA
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}
	id := acmex.CertID(failingCA, f.names)
	writeACMECert(t, f.dataDir, id, f.names, time.Now().Add(30*24*time.Hour))

	f.svc.EnsureCoverage()
	if n := failedRuns(t, f); n != 0 {
		t.Fatalf("mit gültigem Zertifikat: %d Läufe, erwartet 0", n)
	}

	writeACMECert(t, f.dataDir, id, f.names, time.Now().Add(-time.Hour)) // abgelaufen
	f.svc.EnsureCoverage()
	if n := failedRuns(t, f); n != 1 {
		t.Errorf("mit abgelaufenem Zertifikat: %d Läufe, erwartet 1", n)
	}
}

// Der Fehler aus dem Handtest: Das Zertifikat entstand für Oberfläche und Agent;
// kommt MCP als Name dazu, stellt LCM neu aus.
func TestACMENewNameWithoutCoverageStartsARun(t *testing.T) {
	f := autoFixture(t)
	in := validUpdate()
	in.DirectoryURL = failingCA
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, f.svc)
	// Das Zertifikat der ersten zwei Namen liegt vor, so wie nach dem ersten Lauf.
	writeACMECert(t, f.dataDir, acmex.CertID(failingCA, f.names), f.names, time.Now().Add(30*24*time.Hour))
	base := auditCount(t, f.env, "settings.acme.failed")
	f.svc.EnsureCoverage()
	if n := failedRuns(t, f); n != base {
		t.Fatalf("abgedeckt: %d Läufe, erwartet %d", n, base)
	}

	f.names = append(f.names, "mcp.example.com") // MCP eingeschaltet
	f.svc.EnsureCoverage()
	if n := failedRuns(t, f); n != base+1 {
		t.Errorf("neuer Name: %d Läufe, erwartet %d", n, base+1)
	}
}

// Nach einem gescheiterten Lauf von selbst startet LCM für dieselben Namen eine
// Stunde lang nicht neu; „Jetzt ausstellen“ geht immer, und neue Namen sind ein
// anderer Fall.
func TestACMEAutoRunBacksOffAfterFailure(t *testing.T) {
	f := autoFixture(t)
	enableACME(t, f)
	if n := failedRuns(t, f); n != 1 {
		t.Fatalf("erster Lauf: %d", n)
	}

	f.svc.EnsureCoverage()
	if n := failedRuns(t, f); n != 1 {
		t.Errorf("in der Wartezeit: %d Läufe, erwartet 1", n)
	}

	if err := f.svc.IssueNow("admin"); err != nil {
		t.Fatal(err)
	}
	if n := failedRuns(t, f); n != 2 {
		t.Errorf("Jetzt ausstellen: %d Läufe, erwartet 2", n)
	}

	f.names = []string{"lcm.example.com", "agent.example.com", "mcp.example.com"}
	f.svc.EnsureCoverage()
	if n := failedRuns(t, f); n != 3 {
		t.Errorf("andere Namen: %d Läufe, erwartet 3", n)
	}
}

// Wer die Einstellungen speichert, hat die Lage geändert: Die Wartezeit gilt dann nicht mehr.
func TestACMESaveEndsTheBackoff(t *testing.T) {
	f := autoFixture(t)
	enableACME(t, f)
	if n := failedRuns(t, f); n != 1 {
		t.Fatalf("erster Lauf: %d", n)
	}
	enableACME(t, f)
	if n := failedRuns(t, f); n != 2 {
		t.Errorf("nach dem erneuten Speichern: %d Läufe, erwartet 2", n)
	}
}

// writeACMECert legt ein Zertifikat samt Schlüssel so ab, wie LCM es nach einem Lauf findet.
func writeACMECert(t *testing.T, dataDir, id string, names []string, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: names[0]}, DNSNames: names,
		NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(dataDir, "tls", "acme", "certs", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"cert.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		"key.pem":  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
