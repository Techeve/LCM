package agent

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"LCM/internal/remote/wire"
)

// customLeaf stellt ein eigenes Zertifikat des Betreibers aus: für die
// Namen, ohne die Kennung von LCM.
func (ca testCA) customLeaf(t *testing.T, key *ecdsa.PrivateKey, notAfter time.Time, eku []x509.ExtKeyUsage, names ...string) []byte {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: names[0]},
		DNSNames:  names,
		NotBefore: notAfter.AddDate(0, -3, 0), NotAfter: notAfter,
		ExtKeyUsage: eku,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// customChain ist die Kette eines gültigen eigenen Zertifikats für lcm.home.
func (ca testCA) customChain(t *testing.T, key *ecdsa.PrivateKey) [][]byte {
	t.Helper()
	leaf := ca.customLeaf(t, key, time.Now().AddDate(0, 3, 0), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, "lcm.home")
	return [][]byte{leaf, ca.cert.Raw}
}

// TestEigeneCAMitNamenspruefung: Die gepinnte CA des Betreibers trägt ein
// Zertifikat nur für den Namen der Server-Adresse. Ohne Pin gilt es nicht,
// denn die Heim-CA steht nicht in den Systemroots.
func TestEigeneCAMitNamenspruefung(t *testing.T) {
	home := newTestCA(t)
	p := pins{customCAs: []string{home.pin()}}
	valid := home.customChain(t, newKey(t))

	learned, err := checkPeer(valid, p, "lcm.home")
	if err != nil {
		t.Fatalf("gültiges Zertifikat der eigenen CA abgelehnt: %v", err)
	}
	if !slices.Equal(learned.customCAs, []string{home.pin()}) || learned.ca != "" {
		t.Errorf("gelernt %+v, erwartet nur die eigene CA", learned)
	}

	other := newTestCA(t)
	cases := map[string]struct {
		chain    [][]byte
		pins     pins
		hostname string
	}{
		"falscher Name":       {valid, p, "anderer.home"},
		"Name fehlt":          {valid, p, ""},
		"ohne Pin auf die CA": {valid, pins{}, "lcm.home"},
		"Pin auf andere CA":   {valid, pins{customCAs: []string{other.pin()}}, "lcm.home"},
		"andere CA neben echter": {
			[][]byte{other.customLeaf(t, newKey(t), time.Now().AddDate(0, 1, 0), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, "lcm.home"), home.cert.Raw},
			p, "lcm.home"},
		"abgelaufen": {
			[][]byte{home.customLeaf(t, newKey(t), time.Now().Add(-time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, "lcm.home"), home.cert.Raw},
			p, "lcm.home"},
		"nicht für Server": {
			[][]byte{home.customLeaf(t, newKey(t), time.Now().AddDate(0, 1, 0), []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, "lcm.home"), home.cert.Raw},
			p, "lcm.home"},
		"ohne Kette": {valid[:1], p, "lcm.home"},
	}
	for name, c := range cases {
		if learned, err := checkPeer(c.chain, c.pins, c.hostname); err == nil || learned.any() {
			t.Errorf("%s: angenommen (gelernt %+v)", name, learned)
		}
	}
}

// TestEigeneCAPinsBleibenGetrennt: Die Pins der beiden Arten bleiben
// getrennt. Ein Zertifikat der LCM-CA geht nicht über customCAs (und braucht
// dann einen Namen), eines der Heim-CA nicht über ca.
func TestEigeneCAPinsBleibenGetrennt(t *testing.T) {
	lcm := newTestCA(t)
	home := newTestCA(t)
	lcmChain := lcm.chain(t, newKey(t))
	homeChain := home.customChain(t, newKey(t))

	if _, err := checkPeer(lcmChain, pins{customCAs: []string{lcm.pin()}}, "lcm.home"); err == nil {
		t.Error("LCM-Zertifikat ohne passenden Namen über customCAs angenommen")
	}
	if _, err := checkPeer(homeChain, pins{ca: home.pin()}, "lcm.home"); err == nil {
		t.Error("eigenes Zertifikat ohne Kennung über den LCM-CA-Pin angenommen")
	}
}

// TestLCMZertifikatOhneNamenspruefung: Mit dem CA-Pin gilt das Zertifikat der
// LCM-CA weiter für jeden Namen, auch wenn der Agent eigene CAs pinnt.
func TestLCMZertifikatOhneNamenspruefung(t *testing.T) {
	lcm := newTestCA(t)
	home := newTestCA(t)
	p := pins{ca: lcm.pin(), customCAs: []string{home.pin()}}
	for _, host := range []string{"lcm.home", "10.0.0.5", "irgendein.name", ""} {
		if _, err := checkPeer(lcm.chain(t, newKey(t)), p, host); err != nil {
			t.Errorf("host %q: Zertifikat der LCM-CA abgelehnt: %v", host, err)
		}
	}
}

// TestFremdeCAWirdNichtAlsLCMCAGelernt: Eine Verbindung mit einem eigenen
// Zertifikat, vom Zertifikats-Pin bestätigt, lernt die CA nur als eigene. Der
// Anker der LCM-CA bleibt leer, auch wenn die Kette eine CA zeigt.
func TestFremdeCAWirdNichtAlsLCMCAGelernt(t *testing.T) {
	home := newTestCA(t)
	chain := home.customChain(t, newKey(t))

	learned, err := checkPeer(chain, pins{cert: fingerprintOf(chain[0])}, "lcm.home")
	if err != nil {
		t.Fatal(err)
	}
	if learned.ca != "" {
		t.Errorf("fremde CA als LCM-CA gelernt: %q", learned.ca)
	}
	if !slices.Equal(learned.customCAs, []string{home.pin()}) {
		t.Errorf("eigene CA nicht gelernt: %+v", learned)
	}
}

// TestKeineEigeneCAAusUnbestaetigterVerbindung: Nur ein Pin bestätigt, was
// gelernt wird - eine Verbindung, die allein die Systemroots tragen, lehrt
// nichts.
func TestKeineEigeneCAAusUnbestaetigterVerbindung(t *testing.T) {
	home := newTestCA(t)
	if learned, err := checkPeer(home.customChain(t, newKey(t)), pins{}, "lcm.home"); err == nil || learned.any() {
		t.Errorf("ohne Pin: err %v, gelernt %+v", err, learned)
	}
}

// TestRueckfallAufLCMZertifikatNachEigenem: Der Agent hält den LCM-CA-Pin,
// lernt aus einem eigenen Zertifikat (hier über den Zertifikats-Pin aus dem
// Token) Schlüssel und CA des Betreibers - und kommt danach mit dem
// Zertifikat der LCM-CA wieder durch, etwa weil der Betreiber das eigene
// entfernt hat. Der LCM-CA-Pin bleibt dabei unberührt.
func TestRueckfallAufLCMZertifikatNachEigenem(t *testing.T) {
	lcm := newTestCA(t)
	home := newTestCA(t)
	custom := home.customChain(t, newKey(t))
	path := filepath.Join(t.TempDir(), "agent.json")
	cfg := &Config{URL: "https://lcm.home:9320", AgentID: "a", Secret: "s",
		CertFingerprint: fingerprintOf(custom[0]), CAFingerprint: lcm.pin()}
	c := NewClient(cfg, nil, "test").WithConfigPath(path)

	if err := c.verifyPeer("lcm.home")(custom, nil); err != nil {
		t.Fatalf("eigenes Zertifikat abgelehnt: %v", err)
	}
	saved, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CAFingerprint != lcm.pin() {
		t.Errorf("LCM-CA-Pin überschrieben: %q", saved.CAFingerprint)
	}
	if !slices.Equal(saved.CustomCAFingerprints, []string{home.pin()}) || saved.KeyPin != keyPin(t, custom[0]) {
		t.Errorf("gespeichert: eigene CAs %v, Schlüssel %q", saved.CustomCAFingerprints, saved.KeyPin)
	}

	// Das eigene Zertifikat ist weg: LCM liefert wieder das der LCM-CA, mit
	// neuem Schlüssel und unter dem Namen, den es nicht kennt.
	fallback := lcm.chain(t, newKey(t))
	if err := c.verifyPeer("lcm.home")(fallback, nil); err != nil {
		t.Fatalf("Rückfall auf das LCM-Zertifikat abgelehnt: %v", err)
	}
	saved, _ = LoadConfig(path)
	if saved.CAFingerprint != lcm.pin() || saved.KeyPin != keyPin(t, fallback[0]) {
		t.Errorf("nach dem Rückfall: ca %q, Schlüssel %q", saved.CAFingerprint, saved.KeyPin)
	}
}

// TestCustomCAListeHoechstensVier: Die ältesten fallen heraus, ein bekannter
// Eintrag verschiebt nichts.
func TestCustomCAListeHoechstensVier(t *testing.T) {
	cfg := &Config{}
	for _, fp := range []string{"a", "b", "c", "d", "e", "f"} {
		cfg.rememberCustomCA(fp)
	}
	if want := []string{"c", "d", "e", "f"}; !slices.Equal(cfg.CustomCAFingerprints, want) {
		t.Errorf("Liste %v, erwartet %v", cfg.CustomCAFingerprints, want)
	}
	if cfg.rememberCustomCA("d") {
		t.Error("bekannte CA hat die Liste geändert")
	}
	if want := []string{"c", "d", "e", "f"}; !slices.Equal(cfg.CustomCAFingerprints, want) {
		t.Errorf("Liste nach bekanntem Eintrag %v", cfg.CustomCAFingerprints)
	}
}

// TestEnrollUebernimmtEigeneCAAusToken: h im Token landet in der
// Konfiguration; ein Token ohne h lässt das Feld leer.
func TestEnrollUebernimmtEigeneCAAusToken(t *testing.T) {
	tok := wire.Token{AgentID: "a", Secret: "s", CertFingerprint: "f1", CAFingerprint: "c1", CustomCAFingerprint: "h1"}
	cfg := configFromToken("https://lcm.home:9320", tok)
	if cfg.CAFingerprint != "c1" || !slices.Equal(cfg.CustomCAFingerprints, []string{"h1"}) {
		t.Errorf("Konfiguration %+v", cfg)
	}
	tok.CustomCAFingerprint = ""
	if cfg := configFromToken("https://lcm.home:9320", tok); len(cfg.CustomCAFingerprints) != 0 {
		t.Errorf("ohne h: eigene CAs %v", cfg.CustomCAFingerprints)
	}
}
