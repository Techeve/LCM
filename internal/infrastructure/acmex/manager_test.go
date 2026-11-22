package acmex

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"LCM/internal/infrastructure/tlsx"
)

const testDirectory = "https://ca.example/directory"

// selfSigned stellt ein Zertifikat für die Namen aus, wie es die CA liefern
// würde.
func selfSigned(t *testing.T, names []string, notBefore, notAfter time.Time) bundle {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 100))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return bundle{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}
}

// fakeCA spielt die CA: Sie stellt 90-Tage-Zertifikate aus und merkt sich, was
// man von ihr verlangt hat.
type fakeCA struct {
	t        *testing.T
	now      func() time.Time
	window   *window // Antwort auf die Frage nach ARI; nil = die CA kann es nicht
	obtained [][]string
	renewed  [][]string
	replaced []bundle // die Zertifikate, die der Lauf ersetzen ließ
	fail     error    // schlägt Obtain und Renew fehl
}

func (f *fakeCA) Obtain(_ context.Context, names []string) (bundle, error) {
	if f.fail != nil {
		return bundle{}, f.fail
	}
	f.obtained = append(f.obtained, names)
	return f.issue(names), nil
}

func (f *fakeCA) Renew(_ context.Context, names []string, old bundle) (bundle, error) {
	if f.fail != nil {
		return bundle{}, f.fail
	}
	f.renewed = append(f.renewed, names)
	f.replaced = append(f.replaced, old)
	return f.issue(names), nil
}

func (f *fakeCA) RenewalWindow(context.Context, *x509.Certificate) (*window, error) {
	return f.window, nil
}

func (f *fakeCA) issue(names []string) bundle {
	now := f.now()
	return selfSigned(f.t, names, now.Add(-time.Hour), now.Add(90*24*time.Hour))
}

// managerWith baut einen Manager mit der Uhr und der CA des Tests.
func managerWith(t *testing.T, clock *time.Time, ca *fakeCA) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.now = func() time.Time { return *clock }
	ca.t, ca.now = t, m.now
	m.connect = func(context.Context, Config) (authority, error) { return ca, nil }
	return m
}

func config(names ...string) Config {
	return Config{DirectoryURL: testDirectory, Names: names}
}

func reconcile(t *testing.T, m *Manager, cfg Config) []Outcome {
	t.Helper()
	outcomes, err := m.Reconcile(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return outcomes
}

func TestReconcileIssuesMissingCertificate(t *testing.T) {
	clock := time.Now()
	ca := &fakeCA{}
	m := managerWith(t, &clock, ca)

	out := reconcile(t, m, config("lcm.example.com", "agent.example.com"))
	if len(out) != 1 || out[0].Action != ActionIssued || out[0].Err != nil {
		t.Fatalf("Ausgang: %+v", out)
	}
	if len(ca.obtained) != 1 || !slices.Equal(ca.obtained[0], []string{"lcm.example.com", "agent.example.com"}) {
		t.Fatalf("Obtain: %v", ca.obtained)
	}
	certs, err := m.Certificates()
	if err != nil || len(certs) != 1 || len(certs[0].Names) != 2 {
		t.Fatalf("Zertifikate: %+v, %v", certs, err)
	}
	for _, name := range []string{certFile, keyFile} {
		info, err := os.Stat(filepath.Join(certDir(m.dataDir, out[0].ID), name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v, %v", name, info, err)
		}
	}
}

func TestReconcilePerNameIssuesOneCertificateEach(t *testing.T) {
	clock := time.Now()
	ca := &fakeCA{}
	m := managerWith(t, &clock, ca)
	cfg := config("a.example.com", "b.example.com", "c.example.com")
	cfg.PerName = true

	out := reconcile(t, m, cfg)
	if len(out) != 3 || len(ca.obtained) != 3 {
		t.Fatalf("Ausgang %d, Obtain %d", len(out), len(ca.obtained))
	}
	if certs, _ := m.Certificates(); len(certs) != 3 {
		t.Fatalf("%d Zertifikate auf der Platte", len(certs))
	}
}

// Die Erneuerung folgt ARI, sonst einem Drittel der Laufzeit.
func TestReconcileRenewalDecision(t *testing.T) {
	cases := []struct {
		name      string
		advanceBy time.Duration // so viel Zeit vergeht seit dem Ausstellen
		window    func(now time.Time) *window
		wantRenew bool
	}{
		{"ARI-Fenster in der Vergangenheit", 10 * 24 * time.Hour,
			func(now time.Time) *window { return &window{now.Add(-48 * time.Hour), now.Add(-24 * time.Hour)} }, true},
		{"ARI-Fenster in der Zukunft", 70 * 24 * time.Hour,
			func(now time.Time) *window { return &window{now.Add(48 * time.Hour), now.Add(72 * time.Hour)} }, false},
		{"ohne ARI: mehr als ein Drittel übrig", 30 * 24 * time.Hour,
			func(time.Time) *window { return nil }, false},
		{"ohne ARI: weniger als ein Drittel übrig", 61 * 24 * time.Hour,
			func(time.Time) *window { return nil }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := time.Now()
			ca := &fakeCA{}
			m := managerWith(t, &clock, ca)
			cfg := config("lcm.example.com")
			reconcile(t, m, cfg)

			clock = clock.Add(tc.advanceBy)
			ca.window = tc.window(clock)
			out := reconcile(t, m, cfg)

			renewed := len(ca.renewed) == 1
			if renewed != tc.wantRenew {
				t.Fatalf("erneuert = %v, erwartet %v (Ausgang %+v)", renewed, tc.wantRenew, out)
			}
			if tc.wantRenew && (out[0].Action != ActionRenewed || len(ca.replaced[0].certPEM) == 0) {
				t.Errorf("Ausgang %+v, die CA bekam das alte Zertifikat nicht", out[0])
			}
			if !tc.wantRenew && out[0].Action != ActionKept {
				t.Errorf("Ausgang %+v, erwartet kept", out[0])
			}
		})
	}
}

// Das Fenster einer CA ist bei zwei Installationen nicht derselbe Zeitpunkt,
// bei einem Zertifikat aber von Lauf zu Lauf immer derselbe.
func TestRenewalTimeStaysInTheWindowAndIsStable(t *testing.T) {
	now := time.Now()
	win := &window{Start: now.Add(24 * time.Hour), End: now.Add(72 * time.Hour)}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(1234567890123), NotBefore: now, NotAfter: now.Add(90 * 24 * time.Hour)}

	at := renewalTime(leaf, win)
	if at.Before(win.Start) || !at.Before(win.End) {
		t.Fatalf("%v liegt nicht im Fenster %v - %v", at, win.Start, win.End)
	}
	if again := renewalTime(leaf, win); !again.Equal(at) {
		t.Errorf("springt: %v und %v", at, again)
	}
	if got, want := renewalTime(leaf, nil), leaf.NotAfter.Add(-30*24*time.Hour); !got.Equal(want) {
		t.Errorf("ohne ARI: %v, erwartet %v", got, want)
	}
}

// Eine CA, die nicht antwortet, hält ein gültiges Zertifikat nicht auf: Es
// bleibt, ohne Fehler, bis die Regel ohne ARI greift.
func TestReconcileKeepsValidCertificateWhenCAIsUnreachable(t *testing.T) {
	clock := time.Now()
	m := managerWith(t, &clock, &fakeCA{})
	cfg := config("lcm.example.com")
	reconcile(t, m, cfg)

	m.connect = func(context.Context, Config) (authority, error) { return nil, errors.New("keine verbindung") }
	clock = clock.Add(10 * 24 * time.Hour)
	out := reconcile(t, m, cfg)
	if out[0].Err != nil || out[0].Action != ActionKept {
		t.Fatalf("Ausgang: %+v", out[0])
	}

	clock = clock.Add(60 * 24 * time.Hour) // jetzt ist es fällig, und die CA fehlt
	out = reconcile(t, m, cfg)
	if out[0].Err == nil {
		t.Fatal("ein fälliges Zertifikat ohne CA muss den Fehler melden")
	}
}

func TestReconcileFailureIsReportedPerCertificate(t *testing.T) {
	clock := time.Now()
	ca := &fakeCA{fail: errors.New("ratenlimit erreicht")}
	m := managerWith(t, &clock, ca)

	out := reconcile(t, m, config("lcm.example.com"))
	if out[0].Err == nil || Failures(out) == nil {
		t.Fatalf("Ausgang: %+v", out[0])
	}
	if certs, _ := m.Certificates(); len(certs) != 0 {
		t.Errorf("nach einem Fehler liegt etwas auf der Platte: %+v", certs)
	}
}

// Ändern sich die Namen, entsteht ein neues Zertifikat und das alte fällt
// erst weg, wenn das neue da ist.
func TestReconcileReplacesCertificateWhenNamesChange(t *testing.T) {
	clock := time.Now()
	ca := &fakeCA{}
	m := managerWith(t, &clock, ca)
	first := reconcile(t, m, config("alt.example.com"))

	ca.fail = errors.New("ca nicht erreichbar")
	reconcile(t, m, config("neu.example.com"))
	if certs, _ := m.Certificates(); len(certs) != 1 || certs[0].ID != first[0].ID {
		t.Fatalf("das alte Zertifikat muss bleiben, solange das neue fehlt: %+v", certs)
	}

	ca.fail = nil
	second := reconcile(t, m, config("neu.example.com"))
	certs, _ := m.Certificates()
	if len(certs) != 1 || certs[0].ID != second[0].ID || certs[0].ID == first[0].ID {
		t.Fatalf("nach dem neuen Zertifikat bleibt nur das neue: %+v", certs)
	}
}

// Dieselben Namen bei einer anderen CA sind ein anderes Zertifikat.
func TestCertIDDependsOnDirectoryAndNamesNotOrder(t *testing.T) {
	a := CertID(testDirectory, []string{"a.example.com", "b.example.com"})
	if a != CertID(testDirectory, []string{"b.example.com", "a.example.com"}) {
		t.Error("die Reihenfolge der Namen darf die Kennung nicht ändern")
	}
	if a == CertID("https://staging.example/directory", []string{"a.example.com", "b.example.com"}) {
		t.Error("eine andere CA braucht ein anderes Zertifikat")
	}
	if !idPattern.MatchString(a) {
		t.Errorf("Kennung %q passt nicht zum Muster", a)
	}
}

func TestRemoveAllDeletesIssuedCertificates(t *testing.T) {
	clock := time.Now()
	m := managerWith(t, &clock, &fakeCA{})
	reconcile(t, m, config("lcm.example.com"))

	if err := m.RemoveAll(); err != nil {
		t.Fatal(err)
	}
	if certs, _ := m.Certificates(); len(certs) != 0 {
		t.Errorf("übrig: %+v", certs)
	}
}

// Ein ausgestelltes Zertifikat erscheint mit Quelle acme im Pool - und gilt
// erst nach config und Upload.
func TestIssuedCertificateEntersPoolBehindConfigAndUpload(t *testing.T) {
	clock := time.Now()
	m := managerWith(t, &clock, &fakeCA{})
	reconcile(t, m, config("lcm.example.com"))

	specs, err := Specs(m.dataDir)
	if err != nil || len(specs) != 1 || specs[0].Source != tlsx.SourceACME {
		t.Fatalf("Specs: %+v, %v", specs, err)
	}

	own := selfSigned(t, []string{"lcm.example.com"}, clock.Add(-time.Hour), clock.Add(30*24*time.Hour))
	ownDir := t.TempDir()
	writeFile(t, filepath.Join(ownDir, "cert.pem"), own.certPEM)
	writeFile(t, filepath.Join(ownDir, "key.pem"), own.keyPEM)
	ownSpec := tlsx.PoolSpec{ID: "config-0", Source: tlsx.SourceConfig,
		CertPath: filepath.Join(ownDir, "cert.pem"), KeyPath: filepath.Join(ownDir, "key.pem")}

	pool := newTestPool(t)
	pool.Load(specs)
	if got := pool.StatusFor("lcm.example.com"); got.Source != tlsx.SourceACME {
		t.Fatalf("Quelle %q, erwartet acme", got.Source)
	}
	pool.Load(append(slices.Clone(specs), ownSpec))
	if got := pool.StatusFor("lcm.example.com"); got.Source != tlsx.SourceConfig {
		t.Fatalf("Quelle %q, config geht vor acme", got.Source)
	}
}

func newTestPool(t *testing.T) *tlsx.Pool {
	t.Helper()
	dir := t.TempDir()
	notBefore := time.Now().Add(-time.Hour)
	ca, err := tlsx.EnsureCA(dir, notBefore)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath, err := tlsx.EnsureServerCert(dir, ca, notBefore)
	if err != nil {
		t.Fatal(err)
	}
	store, err := tlsx.NewCertStore(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	return tlsx.NewPool(store)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBackupFilesRoundTripAndRejectForeignNames(t *testing.T) {
	clock := time.Now()
	m := managerWith(t, &clock, &fakeCA{})
	out := reconcile(t, m, config("lcm.example.com"))
	if _, err := loadAccountKey(m.dataDir); err != nil {
		t.Fatal(err)
	}
	if err := saveRegistration(m.dataDir, testDirectory, "", nil); err != nil {
		t.Fatal(err)
	}

	files, err := BackupFiles(m.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		BackupPrefix + accountKeyFile,
		backupRegistration + shortID(testDirectory) + ".json",
		backupCert + out[0].ID + "-" + certFile,
		backupCert + out[0].ID + "-" + keyFile,
	}
	for _, name := range want {
		path, ok := files[name]
		if !ok {
			t.Fatalf("Sicherung fehlt %q (enthält: %v)", name, files)
		}
		target, ok := RestoreTarget(m.dataDir, name)
		if !ok || target != path {
			t.Errorf("%s: Ziel %q, ok=%v, erwartet %q", name, target, ok, path)
		}
	}

	for _, bad := range []string{
		"app.db",
		BackupPrefix + "account.pem",
		backupCert + "../../etc-cert.pem",
		backupCert + "zzzzzzzz-cert.pem",
		backupCert + out[0].ID + "-evil.pem",
		backupRegistration + "../x.json",
		backupRegistration + out[0].ID,
	} {
		if target, ok := RestoreTarget(m.dataDir, bad); ok {
			t.Errorf("%q wurde angenommen: %q", bad, target)
		}
	}
}

func TestAccountKeyIsCreatedOnceWithPrivateMode(t *testing.T) {
	dir := t.TempDir()
	first, err := loadAccountKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadAccountKey(dir)
	if err != nil || !first.Equal(second) {
		t.Fatalf("der Schlüssel muss bleiben: %v", err)
	}
	info, err := os.Stat(filepath.Join(baseDir(dir), accountKeyFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("Rechte: %v, %v", info, err)
	}
	if registrationPath(dir, testDirectory) == registrationPath(dir, "https://other.example/directory") {
		t.Error("je Verzeichnis eine Registrierung")
	}
}

// Ein Zertifikat mit mehr Namen deckt weniger Namen ab: Wer Agent oder MCP
// abschaltet, löst keine neue Ausstellung aus. Ein fremdes, abgelaufenes oder
// zu knappes Zertifikat zählt nicht.
func TestCoversAcceptsSupersetOfSameCA(t *testing.T) {
	m := NewManager(t.TempDir())
	now := time.Now()
	all := []string{"agent.example.com", "lcm.example.com", "mcp.example.com"}
	if err := writeBundle(m.dataDir, CertID(testDirectory, all), selfSigned(t, all, now.Add(-time.Hour), now.Add(48*time.Hour))); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		directory string
		names     []string
		want      bool
	}{
		{"gleiche Namen", testDirectory, all, true},
		{"Teilmenge", testDirectory, []string{"lcm.example.com", "mcp.example.com"}, true},
		{"neuer Name", testDirectory, []string{"lcm.example.com", "neu.example.com"}, false},
		{"andere CA", "https://andere.example/directory", all, false},
	}
	for _, c := range cases {
		if got := m.Covers(c.directory, c.names); got != c.want {
			t.Errorf("%s: Covers = %v, erwartet %v", c.name, got, c.want)
		}
	}

	expired := NewManager(t.TempDir())
	if err := writeBundle(expired.dataDir, CertID(testDirectory, all), selfSigned(t, all, now.Add(-48*time.Hour), now.Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}
	if expired.Covers(testDirectory, all) {
		t.Error("abgelaufenes Zertifikat deckt ab")
	}
}
