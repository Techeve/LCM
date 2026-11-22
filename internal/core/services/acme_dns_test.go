package services_test

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"LCM/internal/core/services"
)

func dnsUpdate(provider string, values map[string]string) services.ACMEUpdate {
	in := validUpdate()
	in.Challenge = services.ACMEChallengeDNS01
	in.DNSProvider = provider
	in.DNSValues = values
	return in
}

func wantInvalid(t *testing.T, err error, field string, missing bool) {
	t.Helper()
	var invalid *services.ACMEInvalidError
	if !errors.As(err, &invalid) || invalid.Field != field || invalid.Missing != missing {
		t.Fatalf("Fehler %v, erwartet Feld %s (fehlt: %v)", err, field, missing)
	}
}

// storedConfig ist, was in der Datenbank steht, entschlüsselt.
func (f *acmeFixture) storedConfig(t *testing.T) (raw string, values map[string]string) {
	t.Helper()
	st, err := f.settings.Get()
	if err != nil {
		t.Fatal(err)
	}
	raw = st.ACMEDNSConfigEnc
	if raw == "" {
		return "", nil
	}
	plain, err := f.cipher.DecryptString(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(plain), &values); err != nil {
		t.Fatal(err)
	}
	return raw, values
}

// Geheimnisse liegen verschlüsselt in der Datenbank, die Ansicht nennt nur,
// ob sie gesetzt sind; ein Update ohne Geheimfeld behält sie, ein leerer Text
// löscht sie.
func TestACMEDNSSecretsAreEncryptedHiddenKeptAndCleared(t *testing.T) {
	f := newACMEFixture(t)
	v, err := f.svc.Update(dnsUpdate("inwx", map[string]string{
		"username": "benutzer", "password": "geheimes-passwort", "totp_secret": "JBSWY3DPEHPK3PXP", "sandbox": "true",
	}), "admin")
	if err != nil {
		t.Fatal(err)
	}
	raw, stored := f.storedConfig(t)
	if strings.Contains(raw, "geheimes-passwort") || strings.Contains(raw, "benutzer") {
		t.Error("die Spalte enthält Klartext")
	}
	if stored["password"] != "geheimes-passwort" || stored["username"] != "benutzer" {
		t.Errorf("gespeichert: %v", stored)
	}
	view, _ := json.Marshal(v)
	if strings.Contains(string(view), "geheimes-passwort") || strings.Contains(string(view), "JBSWY3DPEHPK3PXP") {
		t.Errorf("die Ansicht enthält ein Geheimnis: %s", view)
	}
	if !v.DNSSecretsSet["password"] || !v.DNSSecretsSet["totp_secret"] ||
		v.DNSValues["username"] != "benutzer" || v.DNSValues["sandbox"] != true {
		t.Errorf("Ansicht: %+v %+v", v.DNSValues, v.DNSSecretsSet)
	}
	if _, leaked := v.DNSValues["password"]; leaked {
		t.Error("ein Geheimfeld steht unter dns_values")
	}

	// Ohne Geheimfelder: bleiben.
	if _, err := f.svc.Update(dnsUpdate("inwx", map[string]string{"username": "neu"}), "admin"); err != nil {
		t.Fatal(err)
	}
	if _, stored = f.storedConfig(t); stored["password"] != "geheimes-passwort" || stored["totp_secret"] != "JBSWY3DPEHPK3PXP" || stored["username"] != "neu" {
		t.Errorf("nach dem Update ohne Geheimfelder: %v", stored)
	}

	// Leer: löschen. Das Passwort ist Pflicht, also scheitert das Einschalten.
	_, err = f.svc.Update(dnsUpdate("inwx", map[string]string{"username": "neu", "password": ""}), "admin")
	wantInvalid(t, err, "dns_values.password", true)
	// Ausgeschaltet lässt sich das Löschen speichern.
	off := dnsUpdate("inwx", map[string]string{"username": "neu", "password": "", "totp_secret": ""})
	off.Enabled = false
	v, err = f.svc.Update(off, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if v.DNSSecretsSet["password"] || v.DNSSecretsSet["totp_secret"] {
		t.Errorf("gelöschte Geheimnisse gelten als gesetzt: %+v", v.DNSSecretsSet)
	}
}

// Beim Wechsel des Anbieters gehen Geheimnisse nicht mit.
func TestACMEDNSSecretsDoNotCarryOverToAnotherProvider(t *testing.T) {
	f := newACMEFixture(t)
	if _, err := f.svc.Update(dnsUpdate("cloudflare", map[string]string{"api_token": "cf"}), "admin"); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.Update(dnsUpdate("hetzner", nil), "admin")
	wantInvalid(t, err, "dns_values.api_token", true)
	if _, stored := f.storedConfig(t); stored["api_token"] != "cf" {
		t.Errorf("eine abgelehnte Änderung hat gespeichert: %v", stored)
	}
}

func TestACMEDNSValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*services.ACMEUpdate)
		field   string
		missing bool
	}{
		{"kein Anbieter", func(u *services.ACMEUpdate) { u.DNSProvider = "" }, "dns_provider", true},
		{"unbekannter Anbieter", func(u *services.ACMEUpdate) { u.DNSProvider = "route53" }, "dns_provider", false},
		{"Pflichtfeld fehlt", func(u *services.ACMEUpdate) { u.DNSValues = nil }, "dns_values.api_token", true},
		{"unbekanntes Feld", func(u *services.ACMEUpdate) { u.DNSValues["zone"] = "x" }, "dns_values.zone", false},
		{"zu viele Prüf-Nameserver", func(u *services.ACMEUpdate) {
			u.DNSNameservers = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9", "8.8.4.4"}
		}, "dns_check_nameservers", false},
		{"ungültiger Prüf-Nameserver", func(u *services.ACMEUpdate) { u.DNSNameservers = []string{"kein host!"} }, "dns_check_nameservers", false},
	}
	f := newACMEFixture(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := dnsUpdate("cloudflare", map[string]string{"api_token": "t"})
			tc.mutate(&in)
			_, err := f.svc.Update(in, "admin")
			wantInvalid(t, err, tc.field, tc.missing)
			if v, _ := f.svc.View(); v.Enabled {
				t.Error("eine abgelehnte Änderung wurde gespeichert")
			}
		})
	}
}

// dns-01 braucht den Listener auf Port 80 nicht: Auch ein Lauf öffnet ihn nie.
func TestACMEDNS01NeverOpensPort80(t *testing.T) {
	f := newACMEFixture(t)
	in := dnsUpdate("cloudflare", map[string]string{"api_token": "t"})
	v, err := f.svc.Update(in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if v.Challenge != "dns-01" || f.listener.Running() || f.listener.starts() != 0 || v.ListenerRunning {
		t.Errorf("Stand %+v: dns-01 öffnete den Listener auf Port 80", v)
	}

	back := validUpdate()
	if v, err = f.svc.Update(back, "admin"); err != nil || v.Challenge != "http-01" {
		t.Fatalf("zurück auf http-01: %v", err)
	}
	if v.DNSProvider != "cloudflare" || !v.DNSSecretsSet["api_token"] {
		t.Errorf("http-01 ließ die DNS-Einstellungen nicht stehen: %+v", v)
	}
}

// exec ist nur wählbar, wenn die config.json ein vorhandenes, ausführbares
// Programm nennt, und das Programm steht nie in den Einstellungen.
func TestACMEDNSExecOnlyWithConfiguredProgram(t *testing.T) {
	f := newACMEFixture(t)
	if v, _ := f.svc.View(); hasProvider(v, "exec") {
		t.Error("exec steht in der Liste, obwohl acme_dns_exec fehlt")
	}
	_, err := f.svc.Update(dnsUpdate("exec", nil), "admin")
	wantInvalid(t, err, "dns_provider", false)

	dir := t.TempDir()
	hook := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	withHook := newACMEFixtureExec(t, hook)
	v, err := withHook.svc.Update(dnsUpdate("exec", map[string]string{"mode": "raw"}), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !hasProvider(v, "exec") || v.DNSProvider != "exec" {
		t.Errorf("Stand: %+v", v)
	}
	// Das Programm lässt sich nicht über die Werte setzen.
	_, err = withHook.svc.Update(dnsUpdate("exec", map[string]string{"program": "/bin/sh", "path": "/bin/sh"}), "admin")
	if err == nil {
		t.Error("exec nahm ein Programm aus den Werten an")
	}

	for name, path := range map[string]string{"fehlt": filepath.Join(dir, "gibt-es-nicht"), "nicht ausführbar": writeFile(t, dir, "plain.txt", 0o644)} {
		broken := newACMEFixtureExec(t, path)
		_, err := broken.svc.Update(dnsUpdate("exec", nil), "admin")
		if err == nil {
			t.Errorf("%s: exec wurde angenommen", name)
		}
	}
}

func writeFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func hasProvider(v services.ACMEView, id string) bool {
	for _, p := range v.DNSProviders {
		if p.ID == id {
			return true
		}
	}
	return false
}

// Die Liste der Anbieter nennt Felder mit Art und Pflicht, und kein Geheimfeld
// trägt einen Wert.
func TestACMEViewListsProvidersWithFields(t *testing.T) {
	f := newACMEFixture(t)
	v, err := f.svc.View()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range v.DNSProviders {
		ids = append(ids, p.ID)
	}
	if got := strings.Join(ids, ","); got != "rfc2136,acmedns,httpreq,cloudflare,hetzner,pdns,inwx,ionos" {
		t.Errorf("Anbieter: %s", got)
	}
	out, _ := json.Marshal(v.DNSProviders[0])
	if !strings.Contains(string(out), `"name":"tsig_secret","type":"secret"`) || !strings.Contains(string(out), `"required":true`) {
		t.Errorf("Felder: %s", out)
	}
}

// Ein Lauf mit dns-01 baut den Anbieter, öffnet keinen Listener und scheitert
// erst bei der CA.
func TestACMERunWithDNS01DoesNotUseListener(t *testing.T) {
	f := newACMEFixture(t)
	in := dnsUpdate("cloudflare", map[string]string{"api_token": "t"})
	in.DirectoryURL = "https://127.0.0.1:1/directory"
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Run("admin"); err == nil {
		t.Fatal("der Lauf gegen eine fehlende CA gelang")
	}
	if len(f.listener.started) != 0 {
		t.Error("der Lauf öffnete den Listener")
	}
}

// txtDNS beantwortet TXT-Fragen mit dem Wert, den httpreq zuletzt gesetzt hat.
type txtDNS struct {
	mu    sync.Mutex
	value string
}

func (d *txtDNS) set(v string) { d.mu.Lock(); d.value = v; d.mu.Unlock() }

func (d *txtDNS) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	reply := new(dns.Msg).SetReply(r)
	d.mu.Lock()
	if q := r.Question[0]; q.Qtype == dns.TypeTXT && d.value != "" {
		reply.Answer = append(reply.Answer, &dns.TXT{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 60}, Txt: []string{d.value}})
	}
	d.mu.Unlock()
	_ = w.WriteMsg(reply)
}

func startTXTDNS(t *testing.T, d *txtDNS) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	srv := &dns.Server{PacketConn: conn, Handler: d, NotifyStartedFunc: func() { close(started) }}
	go func() { _ = srv.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })
	return conn.LocalAddr().String()
}

// startHook spielt einen Endpunkt für den Anbieter httpreq: Er verlangt
// Basic-Auth mit dem gespeicherten Passwort und gibt den Wert an den Test-DNS.
func startHook(t *testing.T, d *txtDNS, wantPassword string) (url string, calls *[]string) {
	t.Helper()
	var mu sync.Mutex
	var log []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pw, _ := r.BasicAuth(); pw != wantPassword {
			http.Error(w, "nein", http.StatusUnauthorized)
			return
		}
		var body struct{ FQDN, Value string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		log = append(log, r.URL.Path+" "+body.FQDN)
		mu.Unlock()
		if r.URL.Path == "/present" {
			d.set(body.Value)
		} else {
			d.set("")
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &log
}

func TestACMEDNSTestSucceedsWithStoredSecretAndAudits(t *testing.T) {
	f := newACMEFixture(t)
	dnsServer := &txtDNS{}
	resolver := startTXTDNS(t, dnsServer)
	hookURL, calls := startHook(t, dnsServer, "gespeichertes-passwort")
	stored := dnsUpdate("httpreq", map[string]string{"endpoint": hookURL, "username": "lcm", "password": "gespeichertes-passwort"})
	if _, err := f.svc.Update(stored, "admin"); err != nil {
		t.Fatal(err)
	}

	// Das Passwort gibt die Probe nicht mit: Es kommt aus der Datenbank.
	err := f.svc.DNSTest(services.ACMEDNSTest{
		Provider: "httpreq", Values: map[string]string{"endpoint": hookURL, "username": "lcm"}, Nameservers: []string{resolver},
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*calls, ","); got != "/present _acme-challenge.lcm.example.com.,/cleanup _acme-challenge.lcm.example.com." {
		t.Errorf("Aufrufe: %s", got)
	}
	if auditCount(t, f.env, "settings.acme.dns_test") != 1 {
		t.Error("kein Audit-Eintrag settings.acme.dns_test")
	}
}

func TestACMEDNSTestFailureIsReported(t *testing.T) {
	services.DNSTestWait = time.Second
	t.Cleanup(func() { services.DNSTestWait = 60 * time.Second })
	f := newACMEFixture(t)
	dnsServer := &txtDNS{}
	resolver := startTXTDNS(t, dnsServer)
	hookURL, calls := startHook(t, &txtDNS{}, "richtig") // setzt nichts im DNS, an dem wir fragen

	// Falsches Passwort: Der Anbieter meldet den Fehler.
	err := f.svc.DNSTest(services.ACMEDNSTest{
		Names: []string{"lcm.example.com"}, Provider: "httpreq",
		Values: map[string]string{"endpoint": hookURL, "username": "lcm", "password": "falsch"}, Nameservers: []string{resolver},
	}, "admin")
	var failed *services.ACMEDNSTestError
	if !errors.As(err, &failed) || !strings.Contains(failed.Detail, "401") {
		t.Fatalf("Fehler: %v", err)
	}

	// Richtig, aber der Eintrag erscheint nicht beim Resolver.
	err = f.svc.DNSTest(services.ACMEDNSTest{
		Names: []string{"lcm.example.com"}, Provider: "httpreq",
		Values: map[string]string{"endpoint": hookURL, "username": "lcm", "password": "richtig"}, Nameservers: []string{resolver},
	}, "admin")
	if !errors.As(err, &failed) || !strings.Contains(failed.Detail, "nicht sichtbar") {
		t.Fatalf("Fehler: %v", err)
	}
	if got := strings.Join(*calls, ","); !strings.Contains(got, "/cleanup") {
		t.Errorf("nach dem Fehler nicht aufgeräumt: %s", got)
	}
	if auditCount(t, f.env, "settings.acme.dns_test") != 2 {
		t.Error("Audit-Einträge für die Proben fehlen")
	}
}

func TestACMEDNSTestNeedsProviderAndName(t *testing.T) {
	f := newACMEFixture(t)
	err := f.svc.DNSTest(services.ACMEDNSTest{Names: []string{"lcm.example.com"}}, "admin")
	wantInvalid(t, err, "dns_provider", true)
	f.names = nil
	err = f.svc.DNSTest(services.ACMEDNSTest{Provider: "cloudflare", Values: map[string]string{"api_token": "t"}}, "admin")
	wantInvalid(t, err, "names", true)
}
