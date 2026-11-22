package tlsx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// leafOpts beschreibt ein Test-Zertifikat. Leer gilt: eine Stunde gültig,
// serverAuth, selbst ausgestellt.
type leafOpts struct {
	cn          string
	dns         []string
	ips         []net.IP
	notBefore   time.Time
	notAfter    time.Time
	eku         []x509.ExtKeyUsage
	issuer      *x509.Certificate // nil: selbst ausgestellt
	issuerKey   *ecdsa.PrivateKey
	chainInFile bool // den Aussteller hinter das Zertifikat setzen
}

type testCert struct {
	certPEM, keyPEM []byte
	cert            *x509.Certificate
	key             *ecdsa.PrivateKey
}

func newTestCert(t *testing.T, o leafOpts) testCert {
	t.Helper()
	if o.notBefore.IsZero() {
		o.notBefore = time.Now().Add(-time.Hour)
	}
	if o.notAfter.IsZero() {
		o.notAfter = time.Now().Add(time.Hour)
	}
	if o.eku == nil {
		o.eku = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	if o.cn == "" {
		o.cn = "test"
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: o.cn},
		DNSNames:              o.dns,
		IPAddresses:           o.ips,
		NotBefore:             o.notBefore,
		NotAfter:              o.notAfter,
		ExtKeyUsage:           o.eku,
		BasicConstraintsValid: true,
	}
	parent, parentKey := tmpl, key
	if o.issuer != nil {
		parent, parentKey = o.issuer, o.issuerKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if o.chainInFile {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: o.issuer.Raw})...)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return testCert{
		certPEM: certPEM,
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		cert:    cert,
		key:     key,
	}
}

// newTestCA erzeugt eine Zwischen-CA, die newTestCert als issuer nimmt.
func newTestCA(t *testing.T, cn string) testCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCert{cert: cert, key: key}
}

// writeSpec legt das Zertifikat in dir ab und liefert die passende PoolSpec.
func writeSpec(t *testing.T, dir, id string, source Source, c testCert) PoolSpec {
	t.Helper()
	spec := PoolSpec{ID: id, Source: source, CertPath: filepath.Join(dir, id+".pem"), KeyPath: filepath.Join(dir, id+".key")}
	if err := os.WriteFile(spec.CertPath, c.certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.KeyPath, c.keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return spec
}

// newTestPool baut einen Pool mit dem Zertifikat der eigenen CA als Rückfall.
func newTestPool(t *testing.T) *Pool {
	t.Helper()
	dir := t.TempDir()
	certPath, keyPath, err := EnsureServerCert(dir, newCA(t, dir), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewCertStore(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	return NewPool(store)
}

func mustLoad(t *testing.T, p *Pool, specs ...PoolSpec) LoadReport {
	t.Helper()
	report := p.Load(specs)
	for _, r := range report {
		if r.Err != nil {
			t.Fatalf("%s: %v", r.ID, r.Err)
		}
	}
	return report
}

func pick(t *testing.T, p *Pool, name string) *x509.Certificate {
	t.Helper()
	got, err := p.TLSConfig().GetCertificate(&tls.ClientHelloInfo{ServerName: name})
	if err != nil {
		t.Fatal(err)
	}
	return got.Leaf
}

func TestPoolWaehltNachNamen(t *testing.T) {
	p := newTestPool(t)
	dir := t.TempDir()
	exact := newTestCert(t, leafOpts{cn: "exakt", dns: []string{"lcm.example.org"}})
	wild := newTestCert(t, leafOpts{cn: "wild", dns: []string{"*.agents.example.org"}})
	byIP := newTestCert(t, leafOpts{cn: "ip", ips: []net.IP{net.ParseIP("192.0.2.7")}})
	mustLoad(t, p,
		writeSpec(t, dir, "config-0", SourceConfig, exact),
		writeSpec(t, dir, "config-1", SourceConfig, wild),
		writeSpec(t, dir, "config-2", SourceConfig, byIP))

	cases := []struct {
		name string
		want *x509.Certificate
	}{
		{"lcm.example.org", exact.cert},
		{"LCM.Example.org", exact.cert},
		{"a.agents.example.org", wild.cert},
		{"192.0.2.7", byIP.cert},
	}
	for _, c := range cases {
		if got := pick(t, p, c.name); !got.Equal(c.want) {
			t.Errorf("%q: falsches Zertifikat (%s)", c.name, got.Subject.CommonName)
		}
	}
}

func TestPoolRueckfallOhneSNIUndOhneTreffer(t *testing.T) {
	p := newTestPool(t)
	mustLoad(t, p, writeSpec(t, t.TempDir(), "config-0", SourceConfig,
		newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}})))
	fallback := p.fallback.current.Load().Leaf

	for _, name := range []string{"", "anderer.example.org", "a.b.agents.example.org"} {
		if got := pick(t, p, name); !got.Equal(fallback) {
			t.Errorf("%q: erwartet das Zertifikat der LCM-CA, bekam %s", name, got.Subject.CommonName)
		}
	}
	if got := p.CertificateFor("lcm.example.org"); got.Leaf.Subject.CommonName != "test" {
		t.Errorf("CertificateFor wählt anders als der Handshake: %s", got.Leaf.Subject.CommonName)
	}
}

func TestPoolLookupMeldetRueckfall(t *testing.T) {
	p := newTestPool(t)
	mustLoad(t, p, writeSpec(t, t.TempDir(), "config-0", SourceConfig,
		newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}})))

	if _, fallback := p.Lookup("lcm.example.org"); fallback {
		t.Error("eigenes Zertifikat als Rückfall gemeldet")
	}
	for _, name := range []string{"", "anderer.example.org"} {
		if _, fallback := p.Lookup(name); !fallback {
			t.Errorf("%q: Rückfall nicht gemeldet", name)
		}
	}
}

func TestPoolRangfolgeConfigVorUploadVorAcme(t *testing.T) {
	p := newTestPool(t)
	dir := t.TempDir()
	names := []string{"lcm.example.org"}
	acme := newTestCert(t, leafOpts{cn: "acme", dns: names})
	upload := newTestCert(t, leafOpts{cn: "upload", dns: names})
	config := newTestCert(t, leafOpts{cn: "config", dns: names})

	// Die Reihenfolge der Specs ist bewusst falsch herum.
	mustLoad(t, p,
		writeSpec(t, dir, "acme-0", SourceACME, acme),
		writeSpec(t, dir, "upload-1", SourceUpload, upload))
	if got := pick(t, p, names[0]); got.Subject.CommonName != "upload" {
		t.Errorf("upload vor acme: bekam %s", got.Subject.CommonName)
	}
	mustLoad(t, p,
		writeSpec(t, dir, "acme-0", SourceACME, acme),
		writeSpec(t, dir, "upload-1", SourceUpload, upload),
		writeSpec(t, dir, "config-0", SourceConfig, config))
	if got := pick(t, p, names[0]); got.Subject.CommonName != "config" {
		t.Errorf("config vor upload: bekam %s", got.Subject.CommonName)
	}
}

// Ein hochgeladenes Zertifikat für einen Namen geht vor dem ACME-Zertifikat, das
// den Namen mit enthält - auch nach dem Neuausstellen und der Erneuerung. Der
// Name der anderen Dienste bleibt beim ACME-Zertifikat.
func TestPoolUploadBleibtVorAcmeNachNeuausstellungUndErneuerung(t *testing.T) {
	p := newTestPool(t)
	dir := t.TempDir()
	mcp := "mcp.example.org"
	other := "lcm.example.org"
	upload := newTestCert(t, leafOpts{cn: "upload", dns: []string{mcp}})

	acmeOld := newTestCert(t, leafOpts{cn: "acme-alt", dns: []string{other, mcp}})
	mustLoad(t, p, writeSpec(t, dir, "upload-0", SourceUpload, upload), writeSpec(t, dir, "acme-0", SourceACME, acmeOld))
	acmeNew := newTestCert(t, leafOpts{cn: "acme-neu", dns: []string{other, mcp, "agent.example.org"}})
	mustLoad(t, p, writeSpec(t, dir, "upload-0", SourceUpload, upload), writeSpec(t, dir, "acme-1", SourceACME, acmeNew))

	if got := pick(t, p, mcp); got.Subject.CommonName != "upload" {
		t.Errorf("MCP nach dem Neuausstellen: bekam %s, erwartet das hochgeladene", got.Subject.CommonName)
	}
	if got := pick(t, p, other); got.Subject.CommonName != "acme-neu" {
		t.Errorf("Oberfläche: bekam %s, erwartet das ACME-Zertifikat", got.Subject.CommonName)
	}
}

func TestPoolKaputtesZertifikatLaesstAltenEintragAktiv(t *testing.T) {
	p := newTestPool(t)
	dir := t.TempDir()
	spec := writeSpec(t, dir, "config-0", SourceConfig,
		newTestCert(t, leafOpts{cn: "alt", dns: []string{"lcm.example.org"}}))
	mustLoad(t, p, spec)

	if err := os.WriteFile(spec.CertPath, []byte("kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := p.Load([]PoolSpec{spec})
	if len(report) != 1 || report[0].Err == nil || !report[0].Kept || report[0].Cert == nil {
		t.Fatalf("Bericht = %+v, erwartet Fehler mit behaltenem Eintrag", report)
	}
	if got := pick(t, p, "lcm.example.org"); got.Subject.CommonName != "alt" {
		t.Errorf("der alte Eintrag ist nicht mehr aktiv: %s", got.Subject.CommonName)
	}

	// Ein Zertifikat, das nie geladen war, bleibt ohne Eintrag.
	neu := PoolSpec{ID: "config-1", Source: SourceConfig, CertPath: filepath.Join(dir, "fehlt.pem"), KeyPath: filepath.Join(dir, "fehlt.key")}
	report = p.Load([]PoolSpec{spec, neu})
	if report[1].Err == nil || report[1].Kept || report[1].Cert != nil {
		t.Errorf("Bericht für nie geladenes Zertifikat = %+v", report[1])
	}
}

func TestPoolWeggefalleneSpecVerschwindet(t *testing.T) {
	p := newTestPool(t)
	dir := t.TempDir()
	a := writeSpec(t, dir, "config-0", SourceConfig, newTestCert(t, leafOpts{cn: "a", dns: []string{"a.example.org"}}))
	b := writeSpec(t, dir, "config-1", SourceConfig, newTestCert(t, leafOpts{cn: "b", dns: []string{"b.example.org"}}))
	mustLoad(t, p, a, b)
	mustLoad(t, p, b)

	if got := pick(t, p, "a.example.org"); got.Subject.CommonName == "a" {
		t.Error("das weggefallene Zertifikat wird noch ausgeliefert")
	}
	if got := pick(t, p, "b.example.org"); got.Subject.CommonName != "b" {
		t.Errorf("b: bekam %s", got.Subject.CommonName)
	}
}

func TestPoolStatusEnthaeltRueckfall(t *testing.T) {
	p := newTestPool(t)
	c := newTestCert(t, leafOpts{cn: "lcm", dns: []string{"lcm.example.org"}, ips: []net.IP{net.ParseIP("192.0.2.7")}})
	mustLoad(t, p, writeSpec(t, t.TempDir(), "config-0", SourceConfig, c))

	list := p.Status()
	if len(list) != 2 {
		t.Fatalf("Status hat %d Einträge, erwartet 2: %+v", len(list), list)
	}
	got := list[0]
	if got.ID != "config-0" || got.Source != SourceConfig || got.Issuer != "lcm" ||
		strings.Join(got.Names, ",") != "lcm.example.org,192.0.2.7" ||
		got.Fingerprint != fingerprintOf(c.cert.Raw) || !got.NotAfter.Equal(c.cert.NotAfter) {
		t.Errorf("Eintrag = %+v", got)
	}
	fallback := list[1]
	if fallback.Source != SourceLCMCA || fallback.Issuer != "LCM Root CA" || len(fallback.Names) == 0 {
		t.Errorf("Rückfall = %+v", fallback)
	}
}

func TestPoolOhneEintraegeLiefertRueckfall(t *testing.T) {
	p := newTestPool(t)
	if got := pick(t, p, "lcm.example.org"); got == nil {
		t.Fatal("kein Zertifikat vor dem ersten Load")
	}
	if got := p.Status(); len(got) != 1 || got[0].Source != SourceLCMCA {
		t.Errorf("Status = %+v", got)
	}
}

func TestPoolMeldetWarnungenImBericht(t *testing.T) {
	p := newTestPool(t)
	ca := newTestCA(t, "Zwischen-CA")
	c := newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}, issuer: ca.cert, issuerKey: ca.key})
	report := mustLoad(t, p, writeSpec(t, t.TempDir(), "config-0", SourceConfig, c))
	if len(report[0].Cert.Warnings) != 1 {
		t.Errorf("Warnungen = %v, erwartet eine (Kette unvollständig)", report[0].Cert.Warnings)
	}
}

// TestPoolUebergehtAbgelaufeneEintraege: Ein Zertifikat, das nach dem Laden
// abläuft, wird nicht mehr gewählt - erst das nächste mit demselben Namen,
// sonst der Rückfall. Der Status markiert es.
func TestPoolUebergehtAbgelaufeneEintraege(t *testing.T) {
	p := newTestPool(t)
	dir := t.TempDir()
	names := []string{"lcm.example.org"}
	short := newTestCert(t, leafOpts{cn: "kurz", dns: names, notAfter: time.Now().Add(time.Hour)})
	long := newTestCert(t, leafOpts{cn: "lang", dns: names, notAfter: time.Now().Add(48 * time.Hour)})
	mustLoad(t, p,
		writeSpec(t, dir, "config-0", SourceConfig, short),
		writeSpec(t, dir, "upload-1", SourceUpload, long))
	if got := pick(t, p, names[0]); got.Subject.CommonName != "kurz" {
		t.Fatalf("vor dem Ablauf: bekam %s", got.Subject.CommonName)
	}

	p.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if got := pick(t, p, names[0]); got.Subject.CommonName != "lang" {
		t.Errorf("nach Ablauf des ersten: bekam %s", got.Subject.CommonName)
	}
	if got := p.StatusFor(names[0]); got.ID != "upload-1" || got.Expired {
		t.Errorf("StatusFor = %+v", got)
	}
	if list := p.Status(); !list[0].Expired || list[1].Expired {
		t.Errorf("Expired im Status: %+v", list)
	}

	p.now = func() time.Time { return time.Now().Add(72 * time.Hour) }
	if got := pick(t, p, names[0]); !got.Equal(p.fallback.current.Load().Leaf) {
		t.Errorf("alle abgelaufen: erwartet den Rückfall, bekam %s", got.Subject.CommonName)
	}
	if got := p.StatusFor(names[0]); got.Source != SourceLCMCA {
		t.Errorf("StatusFor ohne gültigen Treffer = %+v", got)
	}
}
