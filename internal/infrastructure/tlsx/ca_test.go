package tlsx

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEnsureCAErzeugtWurzelCA: P-384, 20 Jahre, CA mit pathlen 0, Schlüssel
// 0600 - und beim zweiten Start dieselbe CA.
func TestEnsureCAErzeugtWurzelCA(t *testing.T) {
	dir := t.TempDir()
	notBefore := time.Now().Add(-time.Hour)
	ca, err := EnsureCA(dir, notBefore)
	if err != nil {
		t.Fatal(err)
	}
	c := ca.Cert
	if !c.IsCA || c.MaxPathLen != 0 || !c.MaxPathLenZero || c.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Errorf("keine passende CA: ca=%v pathlen=%d zero=%v usage=%v", c.IsCA, c.MaxPathLen, c.MaxPathLenZero, c.KeyUsage)
	}
	if pub, ok := c.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve.Params().BitSize != 384 {
		t.Error("CA-Schlüssel ist nicht P-384")
	}
	if years := c.NotAfter.Year() - c.NotBefore.Year(); years != 20 {
		t.Errorf("Laufzeit %d Jahre", years)
	}
	if mode := fileMode(t, filepath.Join(dir, CAKeyFileName)); mode != 0o600 {
		t.Errorf("CA-Schlüssel mit Rechten %o", mode)
	}
	if !ca.Own {
		t.Error("eigene CA nicht als eigene markiert")
	}
	again, err := EnsureCA(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if again.Fingerprint() != ca.Fingerprint() {
		t.Error("zweiter Start erzeugt eine neue CA")
	}
}

// TestEnsureCAOhneSchluesselBrichtAb: Fehlt nur der Schlüssel, erzeugt LCM
// keine neue CA - die Agenten vertrauen der alten.
func TestEnsureCAOhneSchluesselBrichtAb(t *testing.T) {
	dir := t.TempDir()
	newCA(t, dir)
	if err := os.Remove(filepath.Join(dir, CAKeyFileName)); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureCA(dir, time.Now()); err == nil {
		t.Error("CA ohne Schlüssel angenommen")
	}
}

// TestOpenCANimmtBetreiberCA: Eine Zwischen-CA des Betreibers (Datei mit
// Kette) stellt aus, LCM legt dann keine eigene an. Die ausgelieferte Kette
// enthält die Zwischen-CA und die Zertifikate dahinter.
func TestOpenCANimmtBetreiberCA(t *testing.T) {
	rootDER, rootKey := issue(t, "Betreiber-Root", true, nil, nil)
	root, _ := x509.ParseCertificate(rootDER)
	interDER, interKey := issue(t, "Betreiber-Zwischen-CA", true, root, rootKey)

	adminDir := t.TempDir()
	certFile := filepath.Join(adminDir, "ca.pem")
	keyFile := filepath.Join(adminDir, "ca-key.pem")
	chainPEM := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: interDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})...)
	if err := os.WriteFile(certFile, chainPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(interKey)
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	ca, err := OpenCA(dataDir, certFile, keyFile, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ca.Own || ca.Fingerprint() != fingerprintOf(interDER) {
		t.Errorf("falsche CA: own=%v", ca.Own)
	}
	if fileExists(filepath.Join(dataDir, CAFileName)) {
		t.Error("eigene CA trotz Betreiber-CA angelegt")
	}
	if _, _, err := EnsureServerCert(dataDir, ca, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if pair := loadPair(t, dataDir); len(pair.Certificate) != 3 {
		t.Errorf("Kette mit %d Zertifikaten, erwartet Zertifikat, Zwischen-CA, Root", len(pair.Certificate))
	}
	// Das ausgestellte Zertifikat endet spätestens mit der Zwischen-CA.
	if info := inspect(t, dataDir, ca); info.NotAfter.After(ca.Cert.NotAfter) || !info.FromCA {
		t.Errorf("Zertifikat der Betreiber-CA: %+v", info)
	}
}

// TestOpenCAPrueftDieDateien: nur eine Datei, ein fremder Schlüssel oder ein
// Zertifikat, das keine CA ist, werden abgelehnt.
func TestOpenCAPrueftDieDateien(t *testing.T) {
	dir := t.TempDir()
	newCA(t, dir)
	certFile := filepath.Join(dir, CAFileName)
	if _, err := OpenCA(t.TempDir(), certFile, "", time.Now()); err == nil {
		t.Error("CA ohne Schlüsseldatei angenommen")
	}
	otherDir := t.TempDir()
	newCA(t, otherDir)
	if _, err := OpenCA(t.TempDir(), certFile, filepath.Join(otherDir, CAKeyFileName), time.Now()); err == nil {
		t.Error("fremder Schlüssel angenommen")
	}
	leafDir := t.TempDir()
	der, key := issue(t, "kein-ca", false, nil, nil)
	writeCert(t, leafDir, der, key)
	if _, err := LoadCA(filepath.Join(leafDir, CertFileName), filepath.Join(leafDir, KeyFileName)); err == nil {
		t.Error("Zertifikat ohne CA-Eigenschaft als CA angenommen")
	}
}
