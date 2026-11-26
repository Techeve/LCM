package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// S-02: Der Zertifikats-Pin aus dem Token fällt weg, sobald der Agent CA und
// Schlüssel von LCM gelernt und gespeichert hat.

func dropTestClient(t *testing.T, cfg *Config) (*Client, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.json")
	cfg.URL, cfg.AgentID, cfg.Secret = "https://lcm.example:8443", "a", "s"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	return NewClient(cfg, nil, "test").WithConfigPath(path), path
}

func mustLoad(t *testing.T, path string) *Config {
	t.Helper()
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestGelernteCAEntferntZertifikatsPin: Auf der Verbindung mit CA-Kette lernt
// der Agent CA und Schlüssel, der Zertifikats-Pin geht aus Speicher und Datei,
// und die Verbindung trägt allein der CA-Pin. Der allererste Schlüssel von LCM
// öffnet den Agenten nach dem Schlüsselwechsel nicht mehr.
func TestGelernteCAEntferntZertifikatsPin(t *testing.T) {
	ca := newTestCA(t)
	firstKey := newKey(t)
	first := selfSigned(t, firstKey)
	c, path := dropTestClient(t, &Config{CertFingerprint: fingerprintOf(first)})

	// Erst das alte Zertifikat (lernt den Schlüssel), dann das erste der CA mit
	// demselben Schlüssel (lernt die CA).
	if err := c.verifyPeer("lcm.example")([][]byte{first}, nil); err != nil {
		t.Fatalf("Zertifikats-Pin trägt das erste Zertifikat nicht: %v", err)
	}
	if c.cfg.CertFingerprint == "" {
		t.Fatal("Zertifikats-Pin vor dem Lernen der CA entfernt")
	}
	if err := c.verifyPeer("lcm.example")(ca.chain(t, firstKey), nil); err != nil {
		t.Fatalf("erstes Zertifikat der CA abgelehnt: %v", err)
	}
	saved := mustLoad(t, path)
	if saved.CertFingerprint != "" || c.cfg.CertFingerprint != "" {
		t.Errorf("Zertifikats-Pin blieb: Datei %q, Speicher %q", saved.CertFingerprint, c.cfg.CertFingerprint)
	}
	if saved.CAFingerprint != ca.pin() || saved.KeyPin != keyPin(t, first) {
		t.Errorf("gespeichert: ca %q, key %q", saved.CAFingerprint, saved.KeyPin)
	}

	// Schlüsselwechsel: Das neue Zertifikat der CA geht allein über den CA-Pin.
	renewed := ca.chain(t, newKey(t))
	if err := c.verifyPeer("lcm.example")(renewed, nil); err != nil {
		t.Fatalf("Zertifikat der gepinnten CA mit neuem Schlüssel abgelehnt: %v", err)
	}
	// Mit dem ersten Schlüssel stellt sich niemand mehr dazwischen.
	if err := c.verifyPeer("lcm.example")([][]byte{first}, nil); err == nil {
		t.Error("erstes Zertifikat von LCM gilt nach dem Schlüsselwechsel noch")
	}
	// Auch ein frisch gelesener Agent kommt so durch.
	again := NewClient(mustLoad(t, path), nil, "test")
	if err := again.verifyPeer("lcm.example")(renewed, nil); err != nil {
		t.Errorf("Agent nach Neustart lehnt LCM ab: %v", err)
	}
}

// TestOhneGelernteCABleibtZertifikatsPin: Schickt LCM keine CA (selbstsigniertes
// Zertifikat, wie LCM bis R4, oder Zertifikat ohne Kennung), lernt der Agent nur
// den Schlüssel und behält den Zertifikats-Pin.
func TestOhneGelernteCABleibtZertifikatsPin(t *testing.T) {
	cert := selfSigned(t, newKey(t))
	other := newTestCA(t)
	cases := map[string][][]byte{
		"selbstsigniert":         {cert},
		"CA ohne Kennung":        {other.leaf(t, newKey(t), false, other.cert.NotAfter.AddDate(0, -1, 0)), other.cert.Raw},
		"fremde CA in der Kette": {cert, other.cert.Raw},
	}
	for name, chain := range cases {
		cfg := &Config{CertFingerprint: fingerprintOf(chain[0])}
		c, path := dropTestClient(t, cfg)
		if err := c.verifyPeer("lcm.example")(chain, nil); err != nil {
			t.Fatalf("%s: abgelehnt: %v", name, err)
		}
		saved := mustLoad(t, path)
		if saved.CertFingerprint != fingerprintOf(chain[0]) || c.cfg.CertFingerprint == "" {
			t.Errorf("%s: Zertifikats-Pin ohne gelernte CA entfernt", name)
		}
	}
}

// TestZertifikatsPinBleibtBeiFehlgeschlagenemSpeichern: Gelingt das Schreiben
// nicht, bleibt der Pin in Speicher und Datei; der nächste Verbindungsaufbau
// holt das Entfernen nach, auch wenn CA und Schlüssel dann nichts Neues sind.
func TestZertifikatsPinBleibtBeiFehlgeschlagenemSpeichern(t *testing.T) {
	ca := newTestCA(t)
	key := newKey(t)
	first := selfSigned(t, key)
	c, path := dropTestClient(t, &Config{CertFingerprint: fingerprintOf(first), KeyPin: keyPin(t, first)})
	before, _ := os.ReadFile(path)

	t.Cleanup(func() {
		writeTemp = func(f *os.File, data []byte) error { _, err := f.Write(data); return err }
	})
	writeTemp = func(*os.File, []byte) error { return errors.New("platte voll") }
	chain := ca.chain(t, key)
	if err := c.verifyPeer("lcm.example")(chain, nil); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("Datei trotz Fehler verändert")
	}
	if c.cfg.CertFingerprint == "" {
		t.Error("Zertifikats-Pin im Speicher entfernt, obwohl nichts gespeichert wurde")
	}

	writeTemp = func(f *os.File, data []byte) error { _, err := f.Write(data); return err }
	if err := c.verifyPeer("lcm.example")(chain, nil); err != nil {
		t.Fatal(err)
	}
	saved := mustLoad(t, path)
	if saved.CertFingerprint != "" || saved.CAFingerprint != ca.pin() {
		t.Errorf("nachgeholt: cert %q, ca %q", saved.CertFingerprint, saved.CAFingerprint)
	}
}

// TestZertifikatsPinBleibtOhneKonfigurationspfad: Ohne Ablageort wird nichts
// gespeichert, also auch nichts entfernt.
func TestZertifikatsPinBleibtOhneKonfigurationspfad(t *testing.T) {
	ca := newTestCA(t)
	key := newKey(t)
	first := selfSigned(t, key)
	cfg := &Config{URL: "https://lcm.example", AgentID: "a", Secret: "s",
		CertFingerprint: fingerprintOf(first), KeyPin: keyPin(t, first)}
	c := NewClient(cfg, nil, "test")
	if err := c.verifyPeer("lcm.example")(ca.chain(t, key), nil); err != nil {
		t.Fatal(err)
	}
	if c.cfg.CertFingerprint == "" {
		t.Error("Zertifikats-Pin ohne gespeicherte Konfiguration entfernt")
	}
}
