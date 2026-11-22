package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// syncEnv ist ein Dienst mit Datenbank und einer config.json zum Abgleichen.
type syncEnv struct {
	t     *testing.T
	path  string
	repo  *repositories.SettingsRepository
	svc   *services.SettingsService
	sync  *configSync
	logs  *bytes.Buffer
	locks envLocks
}

// newSyncEnv legt die Datenbank (Stand wie eine Installation von vorher: Ports
// der Vorgaben, Agent an) und die config.json mit dem Inhalt body an.
func newSyncEnv(t *testing.T, body string) *syncEnv {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	cipher, err := crypto.NewCipher(crypto.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewSettingsRepository(db)
	if err := repo.Save(&domain.GlobalSettings{
		AgentEnabled: true, MCPTLS: true, AddressMode: domain.AddressModeSeparate,
		UIPort: 9310, AgentPort: 9320, MCPPort: 9330, SharedTLSPort: 443,
	}); err != nil {
		t.Fatal(err)
	}
	audit := services.NewAuditService(repositories.NewAuditRepository(db))
	svc := services.NewSettingsService(repo, cipher, audit, nil).
		WithAddressConfig(services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}

	logs := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &syncEnv{t: t, path: path, repo: repo, svc: svc, logs: logs}
}

func (e *syncEnv) start() {
	e.t.Helper()
	e.sync = newConfigSync(e.path, e.repo, e.svc, e.locks)
	e.sync.Start()
}

func (e *syncEnv) settings() *domain.GlobalSettings {
	e.t.Helper()
	st, err := e.repo.Get()
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

func (e *syncEnv) file() (config.Mirror, map[string]bool) {
	e.t.Helper()
	m, present, err := config.ReadMirror(e.path)
	if err != nil {
		e.t.Fatal(err)
	}
	return m, present
}

// edit ändert die Datei von Hand: Der Schlüssel bekommt den JSON-Wert value.
func (e *syncEnv) edit(key, value string) {
	e.t.Helper()
	data, err := os.ReadFile(e.path)
	if err != nil {
		e.t.Fatal(err)
	}
	members := string(data)
	needle := `"` + key + `":`
	i := strings.Index(members, needle)
	if i < 0 {
		e.t.Fatalf("Schlüssel %s steht nicht in der Datei:\n%s", key, data)
	}
	end := strings.IndexAny(members[i:], "\n")
	line := members[i : i+end]
	comma := ""
	if strings.HasSuffix(line, ",") {
		comma = ","
	}
	members = members[:i] + needle + " " + value + comma + members[i+end:]
	if err := os.WriteFile(e.path, []byte(members), 0o640); err != nil {
		e.t.Fatal(err)
	}
}

const legacyFile = `{
  "host": "0.0.0.0",
  "port": 9310,
  "agent_port": 9320,
  "tls_ca_key": "/etc/lcm/ca-key.pem",
  "zusatz": {"bleibt": true},
  "allowed_ips": [],
  "agent_allowed_ips": [],
  "mcp_allowed_ips": [],
  "trust_proxy_header": false,
  "trusted_proxies": [],
  "shared_tls_listen": ""
}`

// Beim ersten Start fehlen die neuen Schlüssel: Die Datenbank füllt sie, alle
// anderen Schlüssel bleiben.
func TestConfigSyncStartFuelltFehlendeSchluessel(t *testing.T) {
	e := newSyncEnv(t, legacyFile)
	e.start()

	m, present := e.file()
	for _, key := range config.MirrorKeys {
		if !present[key] {
			t.Errorf("Schlüssel %s fehlt in der Datei", key)
		}
	}
	if m.AddressMode != "separate" || m.MCPPort != 9330 || !m.AgentEnabled || m.MCPEnabled {
		t.Errorf("Werte = %+v", m)
	}
	raw, _ := os.ReadFile(e.path)
	for _, want := range []string{`"tls_ca_key": "/etc/lcm/ca-key.pem"`, `"zusatz"`, `"host": "0.0.0.0"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s ging verloren:\n%s", want, raw)
		}
	}
	if info, _ := os.Stat(e.path); info.Mode().Perm() != 0o640 {
		t.Errorf("Rechte = %v, erwartet 0640", info.Mode().Perm())
	}
}

// Eine Datei von vor dem Abgleich trägt die Vorgaben, die LCM selbst
// hineingeschrieben hat: Sie überschreiben den Port der Datenbank nicht.
func TestConfigSyncAlteVorgabenUeberschreibenDieDatenbankNicht(t *testing.T) {
	e := newSyncEnv(t, legacyFile)
	if err := e.repo.UpdateFields(map[string]any{"ui_port": 9420}); err != nil {
		t.Fatal(err)
	}
	e.start()

	if got := e.settings().UIPort; got != 9420 {
		t.Errorf("ui_port = %d, erwartet 9420 aus der Datenbank", got)
	}
	if m, _ := e.file(); m.Port != 9420 {
		t.Errorf("port in der Datei = %d, erwartet 9420", m.Port)
	}
}

// Ein Wert von der Vorgabe weg ist in einer alten Datei Absicht: Er gilt.
func TestConfigSyncAlterWertAusDerDateiGilt(t *testing.T) {
	e := newSyncEnv(t, strings.Replace(legacyFile, `"port": 9310`, `"port": 9500`, 1))
	e.start()

	if got := e.settings().UIPort; got != 9500 {
		t.Errorf("ui_port = %d, erwartet 9500 aus der Datei", got)
	}
	if !strings.Contains(e.logs.String(), "key=port") {
		t.Errorf("keine Log-Zeile für den übernommenen Schlüssel:\n%s", e.logs)
	}
}

// agent_port 0 hieß "Agent aus": agent_enabled wird false, der Port der
// Datenbank bleibt, die Datei bekommt ihn zurück.
func TestConfigSyncAgentPortNullHeisstAgentAus(t *testing.T) {
	e := newSyncEnv(t, strings.Replace(legacyFile, `"agent_port": 9320`, `"agent_port": 0`, 1))
	e.start()

	st := e.settings()
	if st.AgentEnabled || st.AgentPort != 9320 {
		t.Errorf("agent_enabled=%v agent_port=%d, erwartet aus und 9320", st.AgentEnabled, st.AgentPort)
	}
	if m, _ := e.file(); m.AgentPort != 9320 || m.AgentEnabled {
		t.Errorf("Datei: agent_port=%d agent_enabled=%v", m.AgentPort, m.AgentEnabled)
	}
}

// Ein altes shared_tls_listen heißt Modus "ein Port" mit seinem Port; der Host
// davor bleibt in der Datei.
func TestConfigSyncSharedTLSListenWirdModusC(t *testing.T) {
	e := newSyncEnv(t, strings.Replace(legacyFile, `"shared_tls_listen": ""`, `"shared_tls_listen": "127.0.0.1:8443"`, 1))
	if _, err := e.svc.UpdateGlobal(services.GlobalSettingsInput{PublicBaseURL: ptrTo("https://lcm.example.org")}, "test"); err != nil {
		t.Fatal(err)
	}
	e.start()

	st := e.settings()
	if st.AddressMode != domain.AddressModeSharedPort || st.SharedTLSPort != 8443 {
		t.Errorf("Modus=%q Port=%d, erwartet shared_port und 8443", st.AddressMode, st.SharedTLSPort)
	}
	if m, _ := e.file(); m.SharedTLSListen != "127.0.0.1:8443" || m.AddressMode != "shared_port" {
		t.Errorf("Datei: listen=%q mode=%q", m.SharedTLSListen, m.AddressMode)
	}

	// Ein neuer Port aus der Oberfläche behält den Host der Datei.
	if _, err := e.svc.UpdateGlobal(services.GlobalSettingsInput{SharedTLSPort: ptrTo(9443)}, "test"); err != nil {
		t.Fatal(err)
	}
	e.svc.WithConfigSync(e.sync.Sync)
	if err := e.sync.Sync(); err != nil {
		t.Fatal(err)
	}
	if m, _ := e.file(); m.SharedTLSListen != "127.0.0.1:9443" {
		t.Errorf("shared_tls_listen = %q, erwartet 127.0.0.1:9443", m.SharedTLSListen)
	}
}

// Speichern in der Oberfläche schreibt die Werte in die Datei.
func TestConfigSyncSpeichernSchreibtDieDatei(t *testing.T) {
	e := newSyncEnv(t, legacyFile)
	e.start()
	e.svc.WithConfigSync(e.sync.Sync)

	st, err := e.svc.UpdateGlobal(services.GlobalSettingsInput{
		AddressMode: ptrTo("one_name"), PublicHost: ptrTo("lcm.example.org"),
		MCPPort: ptrTo(9331), MCPEnabled: ptrTo(true),
		TrustedProxies: ptrTo([]string{"10.0.0.5"}), TrustProxyHeader: ptrTo(true),
	}, "admin")
	if err != nil || st.ConfigSyncError != "" {
		t.Fatalf("Speichern: %v, %q", err, st.ConfigSyncError)
	}
	m, _ := e.file()
	if m.AddressMode != "one_name" || m.PublicHost != "lcm.example.org" || m.MCPPort != 9331 || !m.MCPEnabled ||
		!m.TrustProxyHeader || len(m.TrustedProxies) != 1 || m.TrustedProxies[0] != "10.0.0.5/32" {
		t.Errorf("Datei = %+v", m)
	}
	if raw, _ := os.ReadFile(e.path); !strings.Contains(string(raw), `"zusatz"`) {
		t.Errorf("unbekannter Schlüssel ging verloren:\n%s", raw)
	}
}

// Nach einer Abgleichung gilt jede Änderung in der Datei als Handänderung - auch
// ein Wert, der der Vorgabe gleicht - und kommt in die Datenbank, mit einer
// Log-Zeile je Schlüssel.
func TestConfigSyncUebernimmtHandaenderungenBeimStart(t *testing.T) {
	e := newSyncEnv(t, legacyFile)
	e.start()
	if _, err := e.svc.UpdateGlobal(services.GlobalSettingsInput{UIPort: ptrTo(9420)}, "admin"); err != nil {
		t.Fatal(err)
	}
	e.svc.WithConfigSync(e.sync.Sync)
	if err := e.sync.Sync(); err != nil {
		t.Fatal(err)
	}

	e.edit("port", "9310")
	e.edit("mcp_enabled", "true")
	e.edit("allowed_ips", `["10.0.0.0/8"]`)
	e.logs.Reset()
	e.start()

	st := e.settings()
	if st.UIPort != 9310 || !st.MCPEnabled || strings.Join(services.StoredNets(st, "ui"), ",") != "10.0.0.0/8" {
		t.Errorf("ui_port=%d mcp=%v ui-liste=%v", st.UIPort, st.MCPEnabled, services.StoredNets(st, "ui"))
	}
	for _, key := range []string{"key=port", "key=mcp_enabled", "key=allowed_ips"} {
		if strings.Count(e.logs.String(), key) != 1 {
			t.Errorf("erwartet genau eine Log-Zeile mit %s:\n%s", key, e.logs)
		}
	}
}

// Die Datei hinkt nur hinterher (ein Schreibfehler, ein zurückgespielter
// Stand): Die Datenbank gilt, die Datei folgt ihr.
func TestConfigSyncDateiHinktHinterher(t *testing.T) {
	e := newSyncEnv(t, legacyFile)
	e.start()
	if err := e.repo.UpdateFields(map[string]any{"ui_port": 9420}); err != nil {
		t.Fatal(err)
	}
	e.start()

	if got := e.settings().UIPort; got != 9420 {
		t.Errorf("ui_port = %d, erwartet 9420 aus der Datenbank", got)
	}
	if m, _ := e.file(); m.Port != 9420 {
		t.Errorf("port in der Datei = %d, erwartet 9420", m.Port)
	}
}

// Ein ungültiger Wert in der Datei bleibt draußen: Es gilt die Datenbank, die
// Datei behält den Wert (zum Korrigieren), das Log nennt den Fehler - und die
// gültigen Änderungen daneben gehen durch.
func TestConfigSyncUngueltigeHandaenderung(t *testing.T) {
	e := newSyncEnv(t, legacyFile)
	e.start()
	e.edit("port", "99999")
	e.edit("trusted_proxies", `["0.0.0.0/0"]`)
	e.edit("trust_proxy_header", "true")
	e.edit("mcp_port", "9331")
	e.edit("allowed_ips", `["kein-netz"]`)
	e.logs.Reset()
	e.start()

	st := e.settings()
	if st.UIPort != 9310 || st.TrustProxyHeader || len(st.TrustedProxies) != 0 || len(services.StoredNets(st, "ui")) != 0 {
		t.Errorf("ungültige Werte übernommen: port=%d trust=%v proxies=%v ui=%v",
			st.UIPort, st.TrustProxyHeader, st.TrustedProxies, services.StoredNets(st, "ui"))
	}
	if st.MCPPort != 9331 {
		t.Errorf("mcp_port = %d: die gültige Änderung ging verloren", st.MCPPort)
	}
	for _, key := range []string{"key=port", "key=trusted_proxies", "key=trust_proxy_header", "key=allowed_ips"} {
		if !strings.Contains(e.logs.String(), "level=WARN") || !strings.Contains(e.logs.String(), key) {
			t.Errorf("keine Warnung zu %s:\n%s", key, e.logs)
		}
	}
	m, _ := e.file()
	if m.Port != 99999 || len(m.AllowedIPs) != 1 || m.AllowedIPs[0] != "kein-netz" {
		t.Errorf("die ungültigen Werte wurden aus der Datei geräumt: %+v", m)
	}
}

// Eine Umgebungsvariable legt den Port fest: Die Datei bleibt dort unberührt, in
// beide Richtungen.
func TestConfigSyncUmgebungSperrtDenSchluessel(t *testing.T) {
	e := newSyncEnv(t, strings.Replace(legacyFile, `"port": 9310`, `"port": 9500`, 1))
	e.locks = envLocks{port: true}
	e.start()

	if got := e.settings().UIPort; got != 9310 {
		t.Errorf("ui_port = %d: die Datei übernahm einen von der Umgebung gesperrten Wert", got)
	}
	if m, _ := e.file(); m.Port != 9500 {
		t.Errorf("port in der Datei = %d: LCM schrieb einen von der Umgebung gesperrten Wert", m.Port)
	}
}

// Ein Schreibfehler hält LCM nicht auf: Sync meldet ihn, nichts geht verloren.
func TestConfigSyncSchreibfehler(t *testing.T) {
	e := newSyncEnv(t, legacyFile)
	e.start()
	if err := os.Remove(e.path); err != nil {
		t.Fatal(err)
	}
	if err := e.sync.Sync(); err == nil {
		t.Error("Sync ohne Datei meldet keinen Fehler")
	}
}
