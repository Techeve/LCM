package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/config"
	"LCM/internal/control"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
)

// shortTempDir liegt nah an der Wurzel: Unix-Socket-Pfade sind auf macOS auf
// rund 104 Zeichen begrenzt, t.TempDir() wird dafür oft zu lang.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lcm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// testCert ist ein Zertifikat für lcm.example auf der Platte. Es ist an
// seiner Seriennummer zu erkennen: sie ist die Ablaufzeit in Nanosekunden.
type testCert struct {
	cert, key string
	serial    int64
}

func newTestCert(t *testing.T, notAfter time.Time) testCert {
	t.Helper()
	dir := t.TempDir()
	tlsxtest.ForeignCert(t, dir, notAfter)
	return testCert{
		cert:   filepath.Join(dir, tlsx.CertFileName),
		key:    filepath.Join(dir, tlsx.KeyFileName),
		serial: notAfter.UnixNano(),
	}
}

func serialOf(c *tls.Certificate) int64 { return c.Leaf.SerialNumber.Int64() }

// writeConfig schreibt eine config.json, die nur diese Zertifikate nennt.
func writeConfig(t *testing.T, path string, certs ...testCert) {
	t.Helper()
	files := []config.TLSCertificateFile{}
	for _, c := range certs {
		files = append(files, config.TLSCertificateFile{Cert: c.cert, Key: c.key})
	}
	data, err := json.Marshal(map[string]any{"tls_certificates": files})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

type auditEntry struct{ actor, action, details string }

type reloadFixture struct {
	reloader   *certReloader
	dataDir    string
	configPath string
	audit      *[]auditEntry
	addresses  *publicAddresses
}

// newReloadFixture baut einen Pool samt Reloader. Die config.json nennt zu
// Beginn die Zertifikate initial; der Reloader hat sie noch nicht geladen.
func newReloadFixture(t *testing.T, initial ...testCert) *reloadFixture {
	t.Helper()
	dataDir := shortTempDir(t)
	tlsxtest.ServerCert(t, dataDir, time.Now().Add(-time.Hour))
	store, err := tlsx.NewCertStore(filepath.Join(dataDir, tlsx.CertFileName), filepath.Join(dataDir, tlsx.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dataDir, "config.json")
	writeConfig(t, configPath, initial...)
	cfg := &config.Config{}
	for _, c := range initial {
		cfg.TLSCertificates = append(cfg.TLSCertificates, config.TLSCertificateFile{Cert: c.cert, Key: c.key})
	}
	f := &reloadFixture{dataDir: dataDir, configPath: configPath, audit: &[]auditEntry{}, addresses: &publicAddresses{}}
	f.reloader = newCertReloader(tlsx.NewPool(store), cfg, configPath, dataDir,
		func(actor, action, details string) { *f.audit = append(*f.audit, auditEntry{actor, action, details}) },
		func() publicAddresses { return *f.addresses })
	return f
}

func (f *reloadFixture) served(name string) *tls.Certificate {
	return f.reloader.pool.CertificateFor(name)
}

func TestReloadNimmtGeaenderteConfigAuf(t *testing.T) {
	a := newTestCert(t, time.Now().Add(30*24*time.Hour))
	b := newTestCert(t, time.Now().Add(60*24*time.Hour))
	f := newReloadFixture(t, a)

	if report := f.reloader.Reload(""); !report.OK() {
		t.Fatalf("Start: %+v", report)
	}
	if serialOf(f.served("lcm.example")) != a.serial {
		t.Fatal("nach dem Start gilt nicht das erste Zertifikat")
	}
	if len(*f.audit) != 0 {
		t.Errorf("beim Start darf nichts ins Audit-Log: %+v", *f.audit)
	}

	writeConfig(t, f.configPath, b)
	report := f.reloader.Reload(reloadByCLI)
	if !report.OK() || len(report.Certificates) != 1 || report.Certificates[0].Cert == nil {
		t.Fatalf("Bericht = %+v", report)
	}
	if serialOf(f.served("lcm.example")) != b.serial {
		t.Error("das neue Zertifikat ist nach dem Neuladen nicht aktiv")
	}
	if want := []auditEntry{{"cli", auditTLSReload, "geladen=1 abgelehnt=0"}}; len(*f.audit) != 1 || (*f.audit)[0] != want[0] {
		t.Errorf("Audit = %+v, erwartet %+v", *f.audit, want)
	}
}

func TestReloadMitKaputterConfigBehaeltDenAltenStand(t *testing.T) {
	a := newTestCert(t, time.Now().Add(30*24*time.Hour))
	cases := map[string]string{
		"kaputtes JSON":      `{"tls_certificates":`,
		"ungültiger Eintrag": `{"tls_certificates":[{"cert":"relativ.pem","key":"/x.key"}]}`,
		"fehlende Datei":     "",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f := newReloadFixture(t, a)
			f.reloader.Reload("")
			if body == "" {
				_ = os.Remove(f.configPath)
			} else if err := os.WriteFile(f.configPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			report := f.reloader.Reload(reloadBySignal)
			if report.OK() || len(report.Problems) != 1 || !strings.Contains(report.Problems[0], "bleiben") {
				t.Fatalf("Bericht = %+v, erwartet ein Problem", report)
			}
			if len(report.Certificates) != 1 || report.Certificates[0].Error != "" {
				t.Errorf("das bisherige Zertifikat fehlt im Bericht: %+v", report.Certificates)
			}
			if serialOf(f.served("lcm.example")) != a.serial {
				t.Error("der alte Stand ist nicht mehr aktiv")
			}
			if len(*f.audit) != 1 || (*f.audit)[0].actor != "signal" || !strings.Contains((*f.audit)[0].details, "probleme=1") {
				t.Errorf("Audit = %+v", *f.audit)
			}
		})
	}
}

func TestReloadMeldetAbgelehnteZertifikate(t *testing.T) {
	a := newTestCert(t, time.Now().Add(30*24*time.Hour))
	f := newReloadFixture(t, a)
	f.reloader.Reload("")

	// Das Zertifikat wird unlesbar: Der bisherige Stand bleibt, der Fehler steht im Bericht.
	if err := os.WriteFile(a.cert, []byte("kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := f.reloader.Reload(reloadByCLI)
	if report.Rejected() != 1 || report.OK() || !report.Certificates[0].Kept || report.Certificates[0].Error == "" {
		t.Fatalf("Bericht = %+v", report)
	}
	if serialOf(f.served("lcm.example")) != a.serial {
		t.Error("das bisherige Zertifikat ist nicht mehr aktiv")
	}
	if (*f.audit)[0].details != "geladen=0 abgelehnt=1" {
		t.Errorf("Audit = %+v", *f.audit)
	}
}

func TestReloadLaedtUploads(t *testing.T) {
	f := newReloadFixture(t)
	up := newTestCert(t, time.Now().Add(30*24*time.Hour))
	certPEM, _ := os.ReadFile(up.cert)
	keyPEM, _ := os.ReadFile(up.key)
	id, err := tlsx.SaveUpload(f.dataDir, certPEM, keyPEM, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	report := f.reloader.Reload(reloadByCLI)
	if !report.OK() || len(report.Certificates) != 1 || report.Certificates[0].ID != "upload-"+id {
		t.Fatalf("Bericht = %+v", report)
	}
	if serialOf(f.served("lcm.example")) != up.serial {
		t.Error("der Upload wird nicht ausgeliefert")
	}
}

func TestReloadBeiSignalen(t *testing.T) {
	f := newReloadFixture(t)
	signals := make(chan os.Signal, 2)
	signals <- os.Interrupt
	signals <- os.Interrupt
	close(signals)

	reloadOnSignals(signals, f.reloader)
	if len(*f.audit) != 2 || (*f.audit)[0].actor != reloadBySignal {
		t.Errorf("Audit = %+v, erwartet zwei Einträge von %q", *f.audit, reloadBySignal)
	}
}

func TestStatusNenntDasZertifikatJeEndpunkt(t *testing.T) {
	a := newTestCert(t, time.Now().Add(30*24*time.Hour))
	f := newReloadFixture(t, a)
	f.reloader.Reload("")
	*f.addresses = publicAddresses{UI: "https://lcm.example:9310", MCP: "https://anders.example/mcp"}

	status := f.reloader.Status()
	if len(status.Certificates) != 2 || status.Certificates[0].ID != "config-0" || status.Certificates[1].Source != tlsx.SourceLCMCA {
		t.Errorf("Zertifikate = %+v", status.Certificates)
	}
	ui, agent, mcp := status.Endpoints[0], status.Endpoints[1], status.Endpoints[2]
	if ui.Host != "lcm.example" || ui.Cert == nil || ui.Cert.ID != "config-0" {
		t.Errorf("UI = %+v", ui)
	}
	if agent.Cert != nil || agent.URL != "" {
		t.Errorf("Agent ohne Adresse = %+v", agent)
	}
	if mcp.Host != "anders.example" || mcp.Cert == nil || mcp.Cert.Source != tlsx.SourceLCMCA {
		t.Errorf("MCP = %+v", mcp)
	}
}

// TestSteuerbuchseRundlauf: tls.reload und tls.status über eine echte Buchse,
// samt JSON-Weg der Berichte.
func TestSteuerbuchseRundlauf(t *testing.T) {
	a := newTestCert(t, time.Now().Add(30*24*time.Hour))
	f := newReloadFixture(t, a)
	server, err := control.Listen(control.SocketPath(f.dataDir), controlCommands(f.reloader))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	socket := control.SocketPath(f.dataDir)

	var report reloadReport
	if err := control.Call(socket, cmdTLSReload, &report); err != nil {
		t.Fatal(err)
	}
	if !report.OK() || len(report.Certificates) != 1 || report.Certificates[0].Cert.Names[0] != "lcm.example" {
		t.Errorf("Bericht = %+v", report)
	}
	var status statusReport
	if err := control.Call(socket, cmdTLSStatus, &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Certificates) != 2 || len(status.Endpoints) != 3 {
		t.Errorf("Status = %+v", status)
	}
}

func TestSteuerbuchseImEntwicklungsmodus(t *testing.T) {
	dir := shortTempDir(t)
	server, err := control.Listen(control.SocketPath(dir), controlCommands(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	for _, cmd := range []string{cmdTLSReload, cmdTLSStatus} {
		err := control.Call(control.SocketPath(dir), cmd, nil)
		var remote control.RemoteError
		if !errors.As(err, &remote) || !strings.Contains(string(remote), "--dev") {
			t.Errorf("%s: Fehler = %v, erwartet den Hinweis auf --dev", cmd, err)
		}
	}
}
