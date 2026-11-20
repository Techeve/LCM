package synology

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fixtureDevice ist eine nachgebaute DSM-API, die aus testdata/<gerät>/
// antwortet: je Aufruf die Datei "<api>.<method>.json", mit Parameter configs
// "<api>.<method>.configs.json". Was fehlt, beantwortet sie wie DSM eine
// unbekannte API (Code 102).
//
// dsm72 ist ein DS923+ mit DSM 7.2 und gestörtem Pool, virtualdsm die Form
// von Tonys Testgerät: keine CPU-Angaben außer der Kernzahl, keine SMART-Werte
// und keine Spectre-API.
//
// dsm73 trägt die echten Antworten der Sicherheits-APIs von Tonys VirtualDSM
// mit DSM 7.3.2 (Job-Protokoll vom 09.11.2026, ohne Geheimes). Firewall und
// automatisches Blockieren sind dort gedeutet worden und hier nachgebildet.
//
// set beantwortet sie mit Erfolg und hält den Aufruf samt Formular fest.
// answers ersetzt einzelne Antworten ("<api>.<method>" → Antwort).
type fixtureDevice struct {
	t       *testing.T
	dir     string
	answers map[string]string
	mu      sync.Mutex
	calls   []string // "<pfad> <api> v<version> <method>"
	sets    []setCall
}

// setCall ist ein festgehaltener set-Aufruf: das Formular ohne api,
// version, method und _sid.
type setCall struct {
	api, version string
	params       map[string]string
}

func startFixture(t *testing.T, device string) (*Client, *fixtureDevice) {
	t.Helper()
	dev := &fixtureDevice{t: t, dir: filepath.Join("testdata", device)}
	srv := httptest.NewServer(dev)
	t.Cleanup(srv.Close)
	c := &Client{base: srv.URL + "/webapi/", http: srv.Client()}
	if err := c.Login("lcm", "geheim"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	return c, dev
}

func (d *fixtureDevice) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api, method := r.FormValue("api"), r.FormValue("method")
	d.mu.Lock()
	d.calls = append(d.calls, filepath.Base(r.URL.Path)+" "+api+" v"+r.FormValue("version")+" "+method)
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if api == "SYNO.API.Auth" {
		_, _ = w.Write([]byte(`{"success":true,"data":{"sid":"SID123"}}`))
		return
	}
	if api != "SYNO.API.Auth" && r.FormValue("_sid") != "SID123" {
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":119}}`))
		return
	}
	if method == "set" {
		d.recordSet(r)
	}
	name := api + "." + method
	if body, ok := d.answers[name]; ok {
		_, _ = w.Write([]byte(body))
		return
	}
	if method == "set" {
		_, _ = w.Write([]byte(`{"success":true}`))
		return
	}
	if r.FormValue("configs") != "" {
		name += ".configs"
	}
	body, err := os.ReadFile(filepath.Join(d.dir, name+".json"))
	if err != nil {
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":102}}`))
		return
	}
	_, _ = w.Write(body)
}

func (d *fixtureDevice) recordSet(r *http.Request) {
	call := setCall{api: r.FormValue("api"), version: r.FormValue("version"), params: map[string]string{}}
	for k := range r.PostForm {
		if !reservedParams[k] {
			call.params[k] = r.PostForm.Get(k)
		}
	}
	d.mu.Lock()
	d.sets = append(d.sets, call)
	d.mu.Unlock()
}

func (d *fixtureDevice) called(prefix string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// TestDSM72Speicher: Pools, Volumes und Platten kommen einzeln an, mit
// RAID-Art, laufender Reparatur, SMART und den DSM-eigenen Schwellen.
func TestDSM72Speicher(t *testing.T) {
	c, _ := startFixture(t, "dsm72")
	info, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	st := info.Storage
	if st == nil || len(st.Pools) != 2 || len(st.Volumes) != 2 || len(st.Disks) != 3 {
		t.Fatalf("Speicher unvollständig: %+v", st)
	}
	pool := st.Pools[0]
	if pool.DeviceType != "shr_with_1_disk_protect" || pool.Status != "degraded" ||
		pool.RepairStep != "repairing" || pool.RepairPercent != 37 || len(pool.Disks) != 2 {
		t.Errorf("Pool falsch gelesen: %+v", pool)
	}
	if st.Pools[1].RepairStep != "" || st.Pools[1].RepairPercent != -1 {
		t.Errorf("step none ist keine Reparatur: %+v", st.Pools[1])
	}
	vol := st.Volumes[1]
	if vol.Path != "/volume2" || vol.FSType != "ext4" || !vol.ReadOnly || vol.TotalBytes != 367001600000 {
		t.Errorf("Volume falsch gelesen: %+v", vol)
	}
	if st.Volumes[0].ReadOnly {
		t.Error("is_writable=true darf nicht als nur lesbar gelten")
	}
	bad := st.Disks[1]
	if bad.Status != "crashed" || bad.SmartStatus != "failing" || !bad.BadSectorsExceeded || bad.Temp != 44 || bad.Model != "HAT5300-8T" {
		t.Errorf("Platte falsch gelesen: %+v", bad)
	}
	if !st.Disks[2].LifeBelowThreshold {
		t.Errorf("Restlebensdauer-Schwelle fehlt: %+v", st.Disks[2])
	}
	if info.VolumeStatus != "degraded" {
		t.Errorf("schlechtester Volume-Zustand = %q", info.VolumeStatus)
	}
}

// TestDSM72SystemUndEinstellungen: CPU aus Hersteller/Familie/Serie, die
// Sicherheits-APIs in der höchsten angebotenen Version, alle Werte deutbar.
func TestDSM72SystemUndEinstellungen(t *testing.T) {
	c, dev := startFixture(t, "dsm72")
	info, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if info.CPUModel != "AMD Ryzen R1600" || info.CPUCores != 2 || info.Virtual {
		t.Errorf("CPU falsch: %q %d virtuell=%v", info.CPUModel, info.CPUCores, info.Virtual)
	}
	if !dev.called("query.cgi SYNO.API.Info v1 query") {
		t.Error("API-Verzeichnis nicht über query.cgi abgefragt")
	}
	if !dev.called("entry.cgi SYNO.Core.Security.DSM v5 get") {
		t.Errorf("Security.DSM nicht in der höchsten Version abgefragt: %v", dev.calls)
	}
	s := info.Settings
	switches := map[string]Setting{
		"Firewall": s.Firewall, "SkipIPCheck": s.SkipIPCheck, "CSRF": s.CSRFProtection, "CSP": s.CSP,
		"Iframe": s.IframeBlocked, "Sessions": s.SessionsCleared, "AutoBlock": s.AutoBlock,
		"DoS": s.DoS, "TLS": s.TLSProfile, "Spectre": s.SpectreMeltdown,
	}
	for name, got := range switches {
		if !got.Known {
			t.Errorf("%s nicht gedeutet: %+v", name, got)
		}
	}
	if !s.Firewall.On || s.Firewall.Text != "default" || s.SkipIPCheck.On || !s.CSP.On || !s.IframeBlocked.On {
		t.Errorf("Schalter falsch gedeutet: %+v", s)
	}
	// DoS ist je Schnittstelle geschaltet - an einer an zählt als an.
	if !s.DoS.On {
		t.Error("DoS-Schutz an ovs_eth1 nicht erkannt")
	}
	if s.AutoBlockAttempts != 10 || s.AutoBlockMinutes != 5 || s.TLSProfile.Text != "modern" {
		t.Errorf("Werte falsch: %d/%d %q", s.AutoBlockAttempts, s.AutoBlockMinutes, s.TLSProfile.Text)
	}
}

// TestVirtualDSM: Tonys Testgerät. Ohne CPU- und SMART-Angaben bleibt es bei
// dem, was da ist - kein erfundener Wert, kein Fehlalarm. Was sich nicht
// deuten lässt, ist unbekannt, mit Grund und bereinigter Rohantwort.
func TestVirtualDSM(t *testing.T) {
	c, _ := startFixture(t, "virtualdsm")
	info, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !info.Virtual || info.CPUModel != "" || info.CPUCores != 2 {
		t.Errorf("VirtualDSM falsch erkannt: virtuell=%v %q %d", info.Virtual, info.CPUModel, info.CPUCores)
	}
	disk := info.Storage.Disks[0]
	if disk.SmartStatus != "" || disk.Temp != 0 || disk.BadSectorsExceeded || disk.LifeBelowThreshold {
		t.Errorf("fehlende SMART-Felder müssen leer bleiben: %+v", disk)
	}
	if info.Storage.Volumes[0].ReadOnly {
		t.Error("fehlendes is_writable darf nicht als nur lesbar gelten")
	}
	s := info.Settings
	if !s.Firewall.Known || s.Firewall.On {
		t.Errorf("abgeschaltete Firewall nicht erkannt: %+v", s.Firewall)
	}
	if !s.SkipIPCheck.On || !s.CSRFProtection.On || !s.CSP.Known || s.CSP.On {
		t.Errorf("yes/no-Schreibweise nicht gedeutet: %+v", s)
	}
	if s.AutoBlockAttempts != 20 || s.DoS.On || !s.DoS.Known {
		t.Errorf("AutoBlock/DoS falsch: %d %+v", s.AutoBlockAttempts, s.DoS)
	}
	if s.SpectreMeltdown.Known || !strings.Contains(s.SpectreMeltdown.Reason, "nicht vorhanden") {
		t.Errorf("fehlende Spectre-API nicht benannt: %+v", s.SpectreMeltdown)
	}
	if !s.TLSProfile.Known || s.TLSProfile.On || s.TLSProfile.Text != "intermediate" {
		t.Errorf("TLS-Stufe 2 ist intermediate: %+v", s.TLSProfile)
	}
	if s.IframeBlocked.Known || strings.Contains(s.IframeBlocked.Raw, "geheim") {
		t.Errorf("iFrame unbekannt erwartet, Rohantwort ohne Geheimnis: %+v", s.IframeBlocked)
	}
}

// TestRohantwortBereinigt: Geheime Felder werden geschwärzt, lange Antworten
// gekürzt - die Rohantwort landet im Job-Protokoll.
func TestRohantwortBereinigt(t *testing.T) {
	got := cleanRaw(map[string]any{"passwd": "x", "nested": []any{map[string]any{"SID": "y", "level": 1}}})
	if strings.Contains(got, `"x"`) || strings.Contains(got, `"y"`) || !strings.Contains(got, `"level":1`) {
		t.Errorf("nicht bereinigt: %s", got)
	}
	long := cleanRaw(map[string]any{"a": strings.Repeat("z", 2*rawLimit)})
	if len(long) > rawLimit+20 || !strings.HasSuffix(long, "(gekürzt)") {
		t.Errorf("nicht gekürzt: %d Zeichen", len(long))
	}
}

// TestOhneVerzeichnisVersion1: Liefert das Gerät kein API-Verzeichnis, wird
// mit Version 1 über entry.cgi gefragt statt gar nicht.
func TestOhneVerzeichnisVersion1(t *testing.T) {
	c, dev := startFixture(t, "leer")
	c.collectSettings()
	if !dev.called("entry.cgi SYNO.Core.Security.Firewall v1 get") {
		t.Errorf("ohne Verzeichnis nicht mit v1 versucht: %v", dev.calls)
	}
}

// TestDSM73EchteFeldnamen: die Schreibweisen von DSM 7.3.2, wie Tonys
// VirtualDSM sie liefert. Alle Schalter sind gedeutet, der DoS-Schutz über
// den Parameter configs, die TLS-Stufe über ihre Nummer.
func TestDSM73EchteFeldnamen(t *testing.T) {
	c, dev := startFixture(t, "dsm73")
	info, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	s := info.Settings
	switches := map[string]Setting{
		"SkipIPCheck": s.SkipIPCheck, "CSRF": s.CSRFProtection, "CSP": s.CSP,
		"Iframe": s.IframeBlocked, "Sessions": s.SessionsCleared, "Spectre": s.SpectreMeltdown,
		"DoS": s.DoS,
	}
	for name, got := range switches {
		if !got.Known {
			t.Errorf("%s nicht gedeutet: %+v", name, got)
		}
	}
	if s.DoS.On {
		t.Error("DoS-Schutz an eth0 ist aus")
	}
	if s.SkipIPCheck.On || !s.CSRFProtection.On || !s.CSP.On || !s.IframeBlocked.On || !s.SessionsCleared.On || !s.SpectreMeltdown.On {
		t.Errorf("Schalter falsch gedeutet: %+v", s)
	}
	if !s.TLSProfile.Known || !s.TLSProfile.On || s.TLSProfile.Text != "modern" {
		t.Errorf("TLS-Stufe 1 ist modern: %+v", s.TLSProfile)
	}
	// Das aktive Profil erlaubt DSM und Web und verwirft am Ende alles übrige.
	fp := s.FirewallProfile
	if fp.Profile != "default" || len(fp.Sections) != 1 || fp.Sections[0].Name != "global" || fp.Sections[0].Policy != "none" {
		t.Fatalf("Firewall-Profil: %+v", fp)
	}
	if fp.EnabledRules() != 3 || !fp.DenyByDefault() {
		t.Errorf("3 Regeln mit „alles verwerfen“ erwartet: %+v", fp)
	}
	first := fp.Sections[0].Rules[0]
	if !first.Enabled || first.Policy != "allow" || first.Ports != "dms,dms_https" || first.Protocol != "tcp" || first.SourceIPGroup != "all" {
		t.Errorf("erste Regel: %+v", first)
	}
	if !dev.called("entry.cgi SYNO.Core.Security.DoS v2 get") {
		t.Errorf("DoS nicht in Version 2 abgefragt: %v", dev.calls)
	}
}

// TestTLSStufeOhneZuordnung: Eine Nummer ohne Zuordnung bleibt unbekannt und
// nennt sich.
func TestTLSStufeOhneZuordnung(t *testing.T) {
	got := tlsLevel(fetched{data: map[string]any{"default-level": float64(4)}}, "roh")
	if got.Known || got.Text != "Stufe 4" || got.Raw != "roh" {
		t.Errorf("unbekannte Stufe: %+v", got)
	}
	if got := tlsLevel(fetched{data: map[string]any{"default-level": float64(3)}}, ""); !got.Known || got.On || got.Text != "old" {
		t.Errorf("Stufe 3 ist old: %+v", got)
	}
	// Ein Dienst mit eigener Stufe geht der Standardstufe vor.
	own := map[string]any{"default-level": float64(2), "services": map[string]any{"dsm": map[string]any{"current-level": float64(1)}}}
	if got := tlsLevel(fetched{data: own}, ""); !got.Known || got.Text != "modern" {
		t.Errorf("Stufe des Dienstes dsm übersehen: %+v", got)
	}
}
