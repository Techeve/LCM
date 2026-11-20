package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
	"LCM/internal/remote/wire"
)

// selfSigned stellt ein selbstsigniertes Zertifikat für key aus - wie LCM es
// bis R4 tat, auch bei der Erneuerung mit demselben Schlüssel.
func selfSigned(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "LCM", Organization: []string{"LCM"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// testCA ist eine CA für die Tests, gebaut ohne tlsx - so lassen sich auch
// Zertifikate ausstellen, die LCM nie ausstellen würde.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newTestCA(t *testing.T) testCA {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "Test-CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return testCA{cert: cert, key: key}
}

// leaf stellt ein Server-Zertifikat für key aus; marker setzt die Kennung
// von LCM, notAfter das Ende.
func (ca testCA) leaf(t *testing.T, key *ecdsa.PrivateKey, marker bool, notAfter time.Time) []byte {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "LCM"},
		NotBefore: notAfter.AddDate(0, -3, 0), NotAfter: notAfter,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if marker {
		u, _ := url.Parse(wire.ServerCertMarker)
		tmpl.URIs = []*url.URL{u}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func (ca testCA) chain(t *testing.T, key *ecdsa.PrivateKey) [][]byte {
	t.Helper()
	return [][]byte{ca.leaf(t, key, true, time.Now().AddDate(0, 3, 0)), ca.cert.Raw}
}

func (ca testCA) pin() string { return fingerprintOf(ca.cert.Raw) }

func keyPin(t *testing.T, der []byte) string {
	t.Helper()
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return keyPinOf(cert)
}

// TestCheckPeerLerntSchluesselAusBestaetigtemZertifikat: Passt der
// Zertifikats-Pin, liefert die Prüfung den Schlüssel-Pin. Ohne bestätigtes
// Zertifikat lernt der Agent nichts.
func TestCheckPeerLerntSchluesselAusBestaetigtemZertifikat(t *testing.T) {
	cert := selfSigned(t, newKey(t))

	learned, err := checkPeer([][]byte{cert}, pins{cert: fingerprintOf(cert)}, "lcm.example")
	if err != nil {
		t.Fatal(err)
	}
	if learned.key != keyPin(t, cert) || learned.ca != "" {
		t.Errorf("gelernt %+v, erwartet nur den Schlüssel-Pin", learned)
	}
	other := selfSigned(t, newKey(t))
	if learned, err := checkPeer([][]byte{other}, pins{cert: fingerprintOf(cert)}, "lcm.example"); err == nil || learned.any() {
		t.Errorf("fremdes Zertifikat: gelernt %+v, err %v", learned, err)
	}
}

// TestCheckPeerNimmtErneuertesZertifikatMitGleichemSchluessel: Das erneuerte
// Zertifikat hat einen neuen Fingerprint. Mit dem Schlüssel-Pin bleibt die
// Verbindung, ohne ihn (Agent bis 2.0) nicht; ein anderer Schlüssel bleibt
// draußen.
func TestCheckPeerNimmtErneuertesZertifikatMitGleichemSchluessel(t *testing.T) {
	key := newKey(t)
	oldCert := selfSigned(t, key)
	p := pins{cert: fingerprintOf(oldCert), key: keyPin(t, oldCert)}
	renewed := selfSigned(t, key)

	if _, err := checkPeer([][]byte{renewed}, p, "lcm.example"); err != nil {
		t.Errorf("erneuertes Zertifikat mit Schlüssel-Pin abgelehnt: %v", err)
	}
	if _, err := checkPeer([][]byte{renewed}, pins{cert: p.cert}, "lcm.example"); err == nil {
		t.Error("ohne Schlüssel-Pin darf das erneuerte Zertifikat nicht durchgehen")
	}
	foreign := selfSigned(t, newKey(t))
	if _, err := checkPeer([][]byte{foreign}, p, "lcm.example"); err == nil {
		t.Error("Zertifikat mit fremdem Schlüssel angenommen")
	}
}

// TestSchritt1AgentLerntCAUeberSchluesselPin: LCM stellt das erste
// Zertifikat der CA mit dem bisherigen Schlüssel aus. Ein Agent mit
// Schlüssel-Pin nimmt es an und lernt die CA aus der Kette - mit den echten
// Dateien von tlsx.
func TestSchritt1AgentLerntCAUeberSchluesselPin(t *testing.T) {
	dir := t.TempDir()
	tlsxtest.LegacySelfSigned(t, dir, time.Now().Add(-time.Hour))
	legacy := servedChain(t, dir)
	p := pins{cert: fingerprintOf(legacy[0]), key: keyPin(t, legacy[0])}

	ca, err := tlsx.EnsureCA(dir, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := tlsx.IssueServerCert(dir, ca, time.Now().Add(-time.Hour), false); err != nil {
		t.Fatal(err)
	}
	learned, err := checkPeer(servedChain(t, dir), p, "lcm.example")
	if err != nil {
		t.Fatalf("erstes Zertifikat der CA abgelehnt: %v", err)
	}
	if learned.ca != ca.Fingerprint() {
		t.Errorf("CA-Pin %q gelernt, erwartet %q", learned.ca, ca.Fingerprint())
	}

	// Schritt 2: neuer Schlüssel - nur mit dem gelernten CA-Pin.
	if err := tlsx.IssueServerCert(dir, ca, time.Now().Add(-time.Hour), true); err != nil {
		t.Fatal(err)
	}
	renewed := servedChain(t, dir)
	if _, err := checkPeer(renewed, p, "lcm.example"); err == nil {
		t.Error("neuer Schlüssel ohne CA-Pin angenommen")
	}
	p.ca = learned.ca
	learned, err = checkPeer(renewed, p, "lcm.example")
	if err != nil {
		t.Fatalf("Zertifikat der gepinnten CA mit neuem Schlüssel abgelehnt: %v", err)
	}
	if learned.key != keyPin(t, renewed[0]) {
		t.Error("neuen Schlüssel-Pin nicht gelernt")
	}
}

// servedChain liefert die Kette, die LCM aus dem Datenverzeichnis ausliefert.
func servedChain(t *testing.T, dir string) [][]byte {
	t.Helper()
	pair, err := tls.LoadX509KeyPair(filepath.Join(dir, tlsx.CertFileName), filepath.Join(dir, tlsx.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	return pair.Certificate
}

// TestCAPinLehntFremdeAb: Mit CA-Pin gehen weder das Zertifikat einer
// anderen CA noch ein fremdes Zertifikat neben der echten CA durch, auch
// keins der gepinnten CA ohne Kennung (eine Betreiber-CA stellt auch für
// andere Dienste aus) und kein abgelaufenes.
func TestCAPinLehntFremdeAb(t *testing.T) {
	ca := newTestCA(t)
	p := pins{ca: ca.pin()}
	if _, err := checkPeer(ca.chain(t, newKey(t)), p, "lcm.example"); err != nil {
		t.Fatalf("gültiges Zertifikat der CA abgelehnt: %v", err)
	}
	other := newTestCA(t)
	cases := map[string][][]byte{
		"andere CA":             other.chain(t, newKey(t)),
		"fremd neben echter CA": {other.leaf(t, newKey(t), true, time.Now().AddDate(0, 1, 0)), ca.cert.Raw},
		"ohne Kennung":          {ca.leaf(t, newKey(t), false, time.Now().AddDate(0, 1, 0)), ca.cert.Raw},
		"abgelaufen":            {ca.leaf(t, newKey(t), true, time.Now().Add(-time.Hour)), ca.cert.Raw},
		"ohne Kette":            {ca.leaf(t, newKey(t), true, time.Now().AddDate(0, 1, 0))},
	}
	for name, chain := range cases {
		if learned, err := checkPeer(chain, p, "lcm.example"); err == nil || learned.any() {
			t.Errorf("%s: angenommen (gelernt %+v)", name, learned)
		}
	}
}

// TestSchluesselPinLerntKeineFremdeCA: Auch auf einer vertrauten Verbindung
// wird nur eine CA gelernt, die das Zertifikat wirklich ausgestellt hat.
func TestSchluesselPinLerntKeineFremdeCA(t *testing.T) {
	key := newKey(t)
	cert := selfSigned(t, key)
	other := newTestCA(t)
	learned, err := checkPeer([][]byte{cert, other.cert.Raw}, pins{key: keyPin(t, cert)}, "lcm.example")
	if err != nil {
		t.Fatal(err)
	}
	if learned.ca != "" {
		t.Errorf("CA gelernt, die das Zertifikat nicht ausgestellt hat: %q", learned.ca)
	}
}

// TestVerifyPeerSpeichertPins: Der Dienst schreibt die gelernten Pins in
// seine Konfiguration und meldet sie im Inventar. Ein CA-Pin aus dem Token
// bleibt stehen, solange die Kette keine CA zeigt.
func TestVerifyPeerSpeichertPins(t *testing.T) {
	ca := newTestCA(t)
	key := newKey(t)
	legacy := selfSigned(t, key)
	path := filepath.Join(t.TempDir(), "agent.json")
	cfg := &Config{URL: "https://lcm.example:8443", AgentID: "a", Secret: "s",
		CertFingerprint: fingerprintOf(legacy), CAFingerprint: ca.pin()}
	c := NewClient(cfg, nil, "test").WithConfigPath(path)

	if err := c.verifyPeer("lcm.example")([][]byte{legacy}, nil); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.KeyPin != keyPin(t, legacy) || saved.CAFingerprint != ca.pin() {
		t.Errorf("gespeichert: key %q, ca %q", saved.KeyPin, saved.CAFingerprint)
	}

	// Das erste Zertifikat der CA mit demselben Schlüssel, danach eins mit
	// neuem: beide gehen durch, der Schlüssel-Pin folgt.
	if err := c.verifyPeer("lcm.example")(ca.chain(t, key), nil); err != nil {
		t.Fatal(err)
	}
	next := ca.chain(t, newKey(t))
	if err := c.verifyPeer("lcm.example")(next, nil); err != nil {
		t.Fatal(err)
	}
	saved, _ = LoadConfig(path)
	if saved.KeyPin != keyPin(t, next[0]) {
		t.Error("Schlüssel-Pin folgt dem neuen Schlüssel nicht")
	}
	inv := c.inventory()
	if inv.KeyPin != saved.KeyPin || inv.CAPin != ca.pin() {
		t.Errorf("Inventar meldet key %q, ca %q", inv.KeyPin, inv.CAPin)
	}
}
