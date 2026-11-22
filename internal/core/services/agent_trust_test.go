package services_test

// Tests für die Pins aus dem tatsächlich ausgelieferten Zertifikat: Token
// und Download-Befehl folgen dem Pool, nicht mehr dem Zertifikat im
// Datenverzeichnis.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"LCM/internal/core/services"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
	"LCM/internal/storage/repositories"
)

// trustPool ist ein Pool mit dem Zertifikat der LCM-CA als Rückfall und
// einem eigenen Zertifikat einer privaten CA für lcm.home (und der IP
// 192.0.2.7), dazu die Fingerprints, gegen die die Tests prüfen.
type trustPool struct {
	pool       *tlsx.Pool
	lcmCA      string // Fingerprint der LCM-CA
	lcmLeaf    *x509.Certificate
	homeCA     string // Fingerprint der privaten CA
	customLeaf *x509.Certificate
}

func newTrustPool(t *testing.T, chainInFile bool) trustPool {
	t.Helper()
	dir := t.TempDir()
	ca := tlsxtest.ServerCert(t, dir, time.Now().Add(-time.Hour))
	store, err := tlsx.NewCertStore(filepath.Join(dir, tlsx.CertFileName), filepath.Join(dir, tlsx.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	pool := tlsx.NewPool(store)

	homeCA, homeKey := newHomeCA(t)
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "lcm.home"},
		DNSNames: []string{"lcm.home"}, IPAddresses: []net.IP{net.ParseIP("192.0.2.7")},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(0, 3, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, homeCA, &leafKey.PublicKey, homeKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if chainInFile {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: homeCA.Raw})...)
	}
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	spec := tlsx.PoolSpec{ID: "config-0", Source: tlsx.SourceConfig,
		CertPath: filepath.Join(dir, "home.pem"), KeyPath: filepath.Join(dir, "home.key")}
	if err := os.WriteFile(spec.CertPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.KeyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, r := range pool.Load([]tlsx.PoolSpec{spec}) {
		if r.Err != nil {
			t.Fatalf("eigenes Zertifikat nicht geladen: %v", r.Err)
		}
	}
	leaf, _ := x509.ParseCertificate(der)
	lcmLeaf, _ := x509.ParseCertificate(pool.CertificateFor("").Certificate[0])
	return trustPool{pool: pool, lcmCA: ca.Fingerprint(), lcmLeaf: lcmLeaf,
		homeCA: tlsx.CertFingerprint(homeCA), customLeaf: leaf}
}

func newHomeCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "Heim-CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return cert, key
}

func curlPin(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// TestAgentTrustForFolgtDemAusgeliefertenZertifikat: Je nach Name liefert
// der Pool das eigene Zertifikat oder das der LCM-CA, und die Pins gehören
// zu genau diesem. Die LCM-CA steht immer dabei, für den Rückfall.
func TestAgentTrustForFolgtDemAusgeliefertenZertifikat(t *testing.T) {
	tp := newTrustPool(t, true)
	custom := services.AgentTrust{
		CertFingerprint: tlsx.CertFingerprint(tp.customLeaf), CustomCAFingerprint: tp.homeCA,
		CAFingerprint: tp.lcmCA, KeyPin: curlPin(tp.customLeaf),
	}
	lcm := services.AgentTrust{
		CertFingerprint: tlsx.CertFingerprint(tp.lcmLeaf),
		CAFingerprint:   tp.lcmCA, KeyPin: curlPin(tp.lcmLeaf),
	}
	cases := []struct {
		name string
		host string
		want services.AgentTrust
	}{
		{"Name des eigenen Zertifikats", "lcm.home", custom},
		{"Schreibweise: Großbuchstaben, Schlusspunkt", "LCM.Home.", custom},
		{"anderer Name", "lcm.example.org", lcm},
		{"ohne Namen", "", lcm},
		// Ein Client schickt für eine IP kein SNI und bekommt den Rückfall,
		// auch wenn das eigene Zertifikat die IP nennt.
		{"IPv4", "192.0.2.7", lcm},
		{"IPv6 in Klammern", "[2001:db8::1]", lcm},
	}
	for _, c := range cases {
		if got := services.AgentTrustFor(tp.pool, tp.lcmCA, c.host); got != c.want {
			t.Errorf("%s (%q):\n got  %+v\n want %+v", c.name, c.host, got, c.want)
		}
	}
}

// TestAgentTrustForOhneCAInDerKette: Fehlt die CA in der Datei des eigenen
// Zertifikats, bleibt h leer; f und c stehen trotzdem.
func TestAgentTrustForOhneCAInDerKette(t *testing.T) {
	tp := newTrustPool(t, false)
	got := services.AgentTrustFor(tp.pool, tp.lcmCA, "lcm.home")
	if got.CustomCAFingerprint != "" || got.CertFingerprint != tlsx.CertFingerprint(tp.customLeaf) || got.CAFingerprint != tp.lcmCA {
		t.Errorf("Pins ohne CA in der Kette: %+v", got)
	}
}

// TestAgentTrustForOhneLCMCA: Liefert LCM ein Zertifikat des Betreibers im
// Datenverzeichnis, das die CA nicht ausstellt, ist c leer wie bisher.
func TestAgentTrustForOhneLCMCA(t *testing.T) {
	tp := newTrustPool(t, true)
	got := services.AgentTrustFor(tp.pool, "", "unbekannt.example")
	if got.CAFingerprint != "" || got.CertFingerprint != tlsx.CertFingerprint(tp.lcmLeaf) {
		t.Errorf("Pins: %+v", got)
	}
}

// TestTokenTraegtPinsDesAusgeliefertenZertifikats: Das Token für den Host
// der Agent-Adresse trägt f, h und c des Pool-Zertifikats; der Download-Pin
// folgt dem Host der Oberfläche. Eine öffentliche Agent-Adresse geht dem Host
// im Browser vor.
func TestTokenTraegtPinsDesAusgeliefertenZertifikats(t *testing.T) {
	env := newTestEnv(t)
	tp := newTrustPool(t, true)
	env.Servers.WithAgentTrust(func(host string) services.AgentTrust {
		return services.AgentTrustFor(tp.pool, tp.lcmCA, host)
	})

	_, token, err := env.Servers.CreateAgentServer("eigen", "tester", "lcm.home")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := services.ParseAgentToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.CertFingerprint != tlsx.CertFingerprint(tp.customLeaf) || parsed.CustomCAFingerprint != tp.homeCA ||
		parsed.CAFingerprint != tp.lcmCA {
		t.Errorf("Token für den eigenen Namen: %+v", parsed)
	}

	_, token, err = env.Servers.CreateAgentServer("lcm-eigen", "tester", "192.0.2.7")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ = services.ParseAgentToken(token)
	if parsed.CertFingerprint != tlsx.CertFingerprint(tp.lcmLeaf) || parsed.CustomCAFingerprint != "" || parsed.CAFingerprint != tp.lcmCA {
		t.Errorf("Token für die IP: %+v", parsed)
	}

	if pin := env.Servers.AgentDownloadPin("lcm.home"); pin != curlPin(tp.customLeaf) {
		t.Errorf("Download-Pin für den eigenen Namen: %q", pin)
	}
	if pin := env.Servers.AgentDownloadPin("192.0.2.7"); pin != curlPin(tp.lcmLeaf) {
		t.Errorf("Download-Pin für die IP: %q", pin)
	}
}

// TestTokenFolgtDerOeffentlichenAgentAdresse: Ist eine Agent-Adresse
// eingestellt, zählt ihr Host, nicht der Name im Browser - der Join-Assistent
// setzt die Adresse des Agents genauso. Auch beim neuen Token.
func TestTokenFolgtDerOeffentlichenAgentAdresse(t *testing.T) {
	env := newTestEnv(t)
	tp := newTrustPool(t, true)
	env.Servers.WithAgentTrust(func(host string) services.AgentTrust {
		return services.AgentTrustFor(tp.pool, tp.lcmCA, host)
	})
	settings := repositories.NewSettingsRepository(env.DB())
	env.Servers.WithAgentURL(func() string {
		st, _ := settings.Get()
		return st.PublicAgentURL
	})
	if err := settings.UpdateFields(map[string]any{"public_agent_url": "https://lcm.home:9320"}); err != nil {
		t.Fatal(err)
	}

	server, token, err := env.Servers.CreateAgentServer("hinter-adresse", "tester", "192.0.2.7")
	if err != nil {
		t.Fatal(err)
	}
	if parsed, _ := services.ParseAgentToken(token); parsed.CustomCAFingerprint != tp.homeCA {
		t.Errorf("Token ignoriert die Agent-Adresse: %+v", parsed)
	}
	token, err = env.Servers.RegenerateAgentToken(repositories.ScopeAll(), server.ID, "tester", "192.0.2.7")
	if err != nil {
		t.Fatal(err)
	}
	if parsed, _ := services.ParseAgentToken(token); parsed.CustomCAFingerprint != tp.homeCA {
		t.Errorf("neues Token ignoriert die Agent-Adresse: %+v", parsed)
	}

	if err := settings.UpdateFields(map[string]any{"public_agent_url": "https://agent.example.org"}); err != nil {
		t.Fatal(err)
	}
	_, token, err = env.Servers.CreateAgentServer("andere-adresse", "tester", "lcm.home")
	if err != nil {
		t.Fatal(err)
	}
	if parsed, _ := services.ParseAgentToken(token); parsed.CustomCAFingerprint != "" {
		t.Errorf("Agent-Adresse ohne eigenes Zertifikat, Token trägt h: %+v", parsed)
	}
}
