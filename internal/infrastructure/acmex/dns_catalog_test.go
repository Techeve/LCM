package acmex

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/challenge/dns01"
)

// validValues sind gültige Werte je Anbieter, nur Pflichtfelder.
var validValues = map[string]map[string]string{
	"rfc2136":    {"nameserver": "ns1.example.com:53"},
	"acmedns":    {"api_base": "https://auth.acme-dns.io"},
	"httpreq":    {"endpoint": "https://dns.example.com/hook"},
	"exec":       {},
	"cloudflare": {"api_token": "cf-token"},
	"hetzner":    {"api_token": "hz-token"},
	"pdns":       {"api_url": "http://pdns.internal:8081", "api_key": "pdns-key"},
	"inwx":       {"username": "benutzer", "password": "passwort"},
	"ionos":      {"api_key": "praefix.geheimnis"},
}

func testEnv(t *testing.T) DNSEnv {
	t.Helper()
	return DNSEnv{DataDir: t.TempDir(), ExecPath: "/usr/local/bin/lcm-dns-hook"}
}

// Jeder Anbieter entsteht aus festen Werten, ohne Netz.
func TestEveryProviderBuildsFromFixedValues(t *testing.T) {
	env := testEnv(t)
	all := DNSProviders(true)
	if len(all) != len(validValues) {
		t.Fatalf("%d Anbieter, %d Testfälle", len(all), len(validValues))
	}
	for _, p := range all {
		t.Run(p.ID, func(t *testing.T) {
			built, err := NewDNSProvider(p.ID, validValues[p.ID], env)
			if err != nil || built == nil {
				t.Fatalf("%v", err)
			}
		})
	}
}

// Eine Variable am LCM-Prozess ersetzt nie ein Konto: Die Anbieter lesen die
// Umgebung nicht. Sichtbar wird es am Zeitlimit - hätte ein Anbieter
// NewDefaultConfig genommen, stünde hier der Wert der Variable.
func TestProvidersIgnoreEnvironment(t *testing.T) {
	for _, prefix := range []string{"DNSUPDATE", "RFC2136", "HTTPREQ", "EXEC", "CLOUDFLARE", "CF", "HETZNER", "PDNS", "INWX", "IONOS", "ACME_DNS"} {
		t.Setenv(prefix+"_PROPAGATION_TIMEOUT", "7777")
		t.Setenv(prefix+"_POLLING_INTERVAL", "7777")
		t.Setenv(prefix+"_TTL", "7777")
	}
	for name, value := range map[string]string{
		"CLOUDFLARE_DNS_API_TOKEN": "aus-der-umgebung", "CF_DNS_API_TOKEN": "aus-der-umgebung",
		"HETZNER_API_TOKEN": "aus-der-umgebung", "PDNS_API_KEY": "aus-der-umgebung", "PDNS_API_URL": "http://umgebung:1",
		"INWX_USERNAME": "aus-der-umgebung", "INWX_PASSWORD": "aus-der-umgebung", "IONOS_API_KEY": "a.b",
		"DNSUPDATE_NAMESERVER": "umgebung:53", "HTTPREQ_ENDPOINT": "http://umgebung/", "EXEC_PATH": "/bin/umgebung",
		"ACME_DNS_API_BASE": "http://umgebung",
	} {
		t.Setenv(name, value)
	}
	env := testEnv(t)
	for _, p := range DNSProviders(true) {
		t.Run(p.ID, func(t *testing.T) {
			// Ohne Pflichtfelder gelingt es auch mit gesetzter Variable nicht.
			if len(validValues[p.ID]) > 0 {
				if _, err := NewDNSProvider(p.ID, map[string]string{}, env); err == nil {
					t.Error("Anbieter ohne Werte gebaut: Die Umgebung ersetzte sie")
				}
			}
			built, err := NewDNSProvider(p.ID, validValues[p.ID], env)
			if err != nil {
				t.Fatal(err)
			}
			timeouts, ok := built.(challenge.ProviderTimeout)
			if !ok {
				return // acme-dns kennt keine Zeitlimits
			}
			if timeout, interval := timeouts.Timeout(); timeout >= 7000*time.Second || interval >= 7000*time.Second {
				t.Errorf("Zeitlimit %v / %v stammt aus der Umgebung", timeout, interval)
			}
		})
	}
}

// Die Zeitlimits aus den Einstellungen gelten, sonst die Standardwerte des
// Anbieters.
func TestProviderTimings(t *testing.T) {
	env := testEnv(t)
	values := map[string]string{"api_token": "t", fieldPropagationTimeout: "90", fieldPollingInterval: "5"}
	built, err := NewDNSProvider("cloudflare", values, env)
	if err != nil {
		t.Fatal(err)
	}
	if timeout, interval := built.(challenge.ProviderTimeout).Timeout(); timeout != 90*time.Second || interval != 5*time.Second {
		t.Errorf("Zeitlimits: %v / %v", timeout, interval)
	}
	built, err = NewDNSProvider("cloudflare", map[string]string{"api_token": "t"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if timeout, interval := built.(challenge.ProviderTimeout).Timeout(); timeout != 2*time.Minute || interval != 2*time.Second {
		t.Errorf("Standard: %v / %v", timeout, interval)
	}
}

// Der Quelltext nimmt nie die Fabrik und nie die Konstruktoren, die die
// Umgebung lesen.
func TestSourceAvoidsFactoryAndEnvironmentConstructors(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			if imp.Path.Value == `"github.com/go-acme/lego/v5/providers/dns"` {
				t.Errorf("%s importiert die Fabrik providers/dns", file)
			}
		}
		src, _ := os.ReadFile(file)
		for _, banned := range []string{".NewDNSProvider()", "NewDefaultConfig()"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s nutzt %s, das die Umgebung liest", file, banned)
			}
		}
	}
}

func TestValidateRequiredFieldsPerProvider(t *testing.T) {
	cases := []struct {
		provider string
		values   map[string]string
		field    string
		missing  bool
	}{
		{"rfc2136", map[string]string{}, "nameserver", true},
		{"rfc2136", map[string]string{"nameserver": "ns1.example.com", "tsig_key": "k"}, "tsig_secret", false},
		{"rfc2136", map[string]string{"nameserver": "ns1.example.com", "tsig_secret": "c2VjcmV0"}, "tsig_secret", false},
		{"rfc2136", map[string]string{"nameserver": "ns1.example.com:99999"}, "nameserver", false},
		{"rfc2136", map[string]string{"nameserver": "ns1.example.com", "tsig_algorithm": "hmac-md5"}, "tsig_algorithm", false},
		{"rfc2136", map[string]string{"nameserver": "ns1.example.com", "tsig_key": "k", "tsig_secret": "kein base64!"}, "tsig_secret", false},
		{"acmedns", map[string]string{}, "api_base", true},
		{"acmedns", map[string]string{"api_base": "ftp://x"}, "api_base", false},
		{"httpreq", map[string]string{}, "endpoint", true},
		{"httpreq", map[string]string{"endpoint": "https://user:pw@dns.example.com"}, "endpoint", false},
		{"httpreq", map[string]string{"endpoint": "https://dns.example.com", "mode": "wild"}, "mode", false},
		{"cloudflare", map[string]string{}, "api_token", true},
		{"hetzner", map[string]string{}, "api_token", true},
		{"pdns", map[string]string{"api_key": "k"}, "api_url", true},
		{"pdns", map[string]string{"api_url": "http://x:8081"}, "api_key", true},
		{"pdns", map[string]string{"api_url": "pdns:8081", "api_key": "k"}, "api_url", false},
		{"inwx", map[string]string{"password": "p"}, "username", true},
		{"inwx", map[string]string{"username": "u"}, "password", true},
		{"inwx", map[string]string{"username": "u", "password": "p", "sandbox": "ja"}, "sandbox", false},
		{"ionos", map[string]string{}, "api_key", true},
		{"ionos", map[string]string{"api_key": "ohne-punkt"}, "api_key", false},
		{"cloudflare", map[string]string{"api_token": "t", fieldPropagationTimeout: "0"}, fieldPropagationTimeout, false},
		{"cloudflare", map[string]string{"api_token": "t", fieldPollingInterval: "viel"}, fieldPollingInterval, false},
		{"cloudflare", map[string]string{"api_token": "t", "unbekannt": "x"}, "unbekannt", false},
		{"cloudflare", map[string]string{"api_token": strings.Repeat("x", maxValueLength+1)}, "api_token", false},
	}
	for _, tc := range cases {
		p, ok := LookupDNSProvider(tc.provider)
		if !ok {
			t.Fatalf("Anbieter %s fehlt", tc.provider)
		}
		err := p.Validate(tc.values)
		if err == nil || err.Field != tc.field || err.Missing != tc.missing {
			t.Errorf("%s %v: %+v, erwartet Feld %s (fehlt: %v)", tc.provider, tc.values, err, tc.field, tc.missing)
		}
	}
	for id, values := range validValues {
		p, _ := LookupDNSProvider(id)
		if err := p.Validate(values); err != nil {
			t.Errorf("%s mit gültigen Werten: %v", id, err)
		}
	}
}

// exec ist nur wählbar und baubar, wenn die config.json ein Programm nennt.
func TestExecProviderNeedsConfiguredProgram(t *testing.T) {
	if slices.ContainsFunc(DNSProviders(false), func(p DNSProvider) bool { return p.ID == ProviderExec }) {
		t.Error("exec steht ohne acme_dns_exec in der Liste")
	}
	if !slices.ContainsFunc(DNSProviders(true), func(p DNSProvider) bool { return p.ID == ProviderExec }) {
		t.Error("exec fehlt trotz acme_dns_exec")
	}
	if _, err := NewDNSProvider(ProviderExec, nil, DNSEnv{DataDir: t.TempDir()}); err == nil {
		t.Error("exec ohne Programm gebaut")
	}
	if _, err := NewDNSProvider(ProviderExec, map[string]string{"program": "/bin/sh"}, testEnv(t)); err == nil {
		t.Error("exec nahm ein Programm aus den Werten an")
	}
}

func TestFieldsListSecretsAndKinds(t *testing.T) {
	for _, p := range DNSProviders(true) {
		if p.ID == "inwx" && !slices.Equal(p.SecretFields(), []string{"password", "totp_secret"}) {
			t.Errorf("inwx Geheimfelder: %v", p.SecretFields())
		}
		for _, f := range p.Fields {
			if f.Kind == FieldSelect && len(f.Options) == 0 {
				t.Errorf("%s.%s: select ohne Auswahl", p.ID, f.Name)
			}
		}
	}
}

// Ein Anbieter, der setzt und wieder entfernt: für die Probe.
type recordingProvider struct {
	presentErr, cleanErr error
	presented, cleaned   int
	onPresent            func(fqdnDomain, keyAuth string)
}

func (r *recordingProvider) Present(_ context.Context, domain, _, keyAuth string) error {
	r.presented++
	if r.onPresent != nil {
		r.onPresent(domain, keyAuth)
	}
	return r.presentErr
}

func (r *recordingProvider) CleanUp(context.Context, string, string, string) error {
	r.cleaned++
	return r.cleanErr
}

func TestProbeDNSSetsWaitsAndCleansUpWithRFC2136(t *testing.T) {
	fake := startFakeDNS(t, "example.com")
	p, err := NewDNSProvider("rfc2136", fake.rfc2136Values(), testEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	// Währenddessen steht der Eintrag im Test-DNS.
	seen := make(chan map[string][]string, 1)
	watch := &watchingProvider{Provider: p, after: func() { seen <- fake.records() }}

	if err := ProbeDNS(context.Background(), watch, "*.lcm.example.com", []string{fake.addr}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if records := <-seen; len(records["_acme-challenge.lcm.example.com."]) != 1 {
		t.Errorf("Eintrag während der Probe: %v", records)
	}
	if records := fake.records(); len(records) != 0 {
		t.Errorf("nach der Probe bleibt: %v", records)
	}
	if updates, unsigned := fake.stats(); updates != 2 || unsigned != 0 {
		t.Errorf("%d Updates (erwartet: setzen und entfernen), %d ohne gültige TSIG-Signatur", updates, unsigned)
	}
}

// watchingProvider ruft nach dem Setzen after auf.
type watchingProvider struct {
	Provider
	after func()
}

func (w *watchingProvider) Present(ctx context.Context, domain, token, keyAuth string) error {
	if err := w.Provider.Present(ctx, domain, token, keyAuth); err != nil {
		return err
	}
	w.after()
	return nil
}

func TestProbeDNSReportsProviderError(t *testing.T) {
	provider := &recordingProvider{presentErr: errors.New("api: token ungültig")}
	err := ProbeDNS(context.Background(), provider, "lcm.example.com", []string{"127.0.0.1:1"}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "token ungültig") {
		t.Fatalf("Fehler: %v", err)
	}
}

// Wird der Eintrag nicht sichtbar, meldet die Probe es - und räumt trotzdem auf.
func TestProbeDNSTimesOutButCleansUp(t *testing.T) {
	fake := startFakeDNS(t, "example.com")
	provider := &recordingProvider{} // setzt nichts im Test-DNS
	err := ProbeDNS(context.Background(), provider, "lcm.example.com", []string{fake.addr}, 3*time.Second)
	if err == nil || !strings.Contains(err.Error(), "nicht sichtbar") {
		t.Fatalf("Fehler: %v", err)
	}
	if provider.cleaned != 1 {
		t.Errorf("aufgeräumt: %d mal", provider.cleaned)
	}
}

func TestProbeDNSReportsCleanUpError(t *testing.T) {
	fake := startFakeDNS(t, "example.com")
	provider := &recordingProvider{cleanErr: errors.New("löschen verweigert")}
	provider.onPresent = func(domain, keyAuth string) {
		info := lookupInfo(domain, keyAuth)
		fake.set(info.fqdn, info.value)
	}
	err := ProbeDNS(context.Background(), provider, "lcm.example.com", []string{fake.addr}, 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "löschen verweigert") {
		t.Fatalf("Fehler: %v", err)
	}
}

// Bei RFC 2136 ohne gültige Signatur weist der Server das Update ab; der
// Fehler erreicht den Aufrufer.
func TestRFC2136WrongSecretIsRejected(t *testing.T) {
	fake := startFakeDNS(t, "example.com")
	values := fake.rfc2136Values()
	values["tsig_secret"] = "ZmFsc2NoZXMtc2VjcmV0" // base64, aber nicht das des Servers
	p, err := NewDNSProvider("rfc2136", values, testEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := ProbeDNS(context.Background(), p, "lcm.example.com", []string{fake.addr}, 2*time.Second); err == nil {
		t.Fatal("Probe mit falschem Secret gelang")
	}
}

type challengeInfo struct{ fqdn, value string }

// lookupInfo ist, was lego aus Name und Schlüsselautorisierung für den
// TXT-Eintrag ableitet.
func lookupInfo(domain, keyAuth string) challengeInfo {
	info := dns01.GetChallengeInfo(context.Background(), domain, keyAuth)
	return challengeInfo{fqdn: info.EffectiveFQDN, value: info.Value}
}
