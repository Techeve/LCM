package acmex

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/challenge/dns01"
	"github.com/go-acme/lego/v5/providers/dns/acmedns"
	"github.com/go-acme/lego/v5/providers/dns/cloudflare"
	"github.com/go-acme/lego/v5/providers/dns/dnsupdate"
	"github.com/go-acme/lego/v5/providers/dns/exec"
	"github.com/go-acme/lego/v5/providers/dns/hetzner"
	"github.com/go-acme/lego/v5/providers/dns/httpreq"
	"github.com/go-acme/lego/v5/providers/dns/inwx"
	"github.com/go-acme/lego/v5/providers/dns/ionos"
	"github.com/go-acme/lego/v5/providers/dns/pdns"
)

const (
	apiTimeout = 30 * time.Second // Zeitlimit je Aufruf an die API eines Anbieters

	modeStandard = "standard"
	modeRaw      = "raw" // lego reicht Domain, Token und Schlüsselautorisierung unverändert weiter
)

var tsigAlgorithms = []string{"hmac-sha256", "hmac-sha384", "hmac-sha512", "hmac-sha224", "hmac-sha1"}

var dnsProviders = []DNSProvider{
	{
		// RFC 2136 (dynamisches Update): PowerDNS, BIND, Knot, Technitium.
		// lego nennt den Anbieter dnsupdate.
		ID: "rfc2136",
		Fields: withTimings(
			Field{Name: "nameserver", Kind: FieldText, Required: true, check: CheckNameserver},
			Field{Name: "tsig_key", Kind: FieldText},
			Field{Name: "tsig_algorithm", Kind: FieldSelect, Options: tsigAlgorithms, Default: "hmac-sha256"},
			Field{Name: "tsig_secret", Kind: FieldSecret, check: checkBase64},
		),
		verify: verifyTSIG,
		build:  buildRFC2136,
	},
	{
		// Konten bei acme-dns legt lego beim ersten Lauf selbst an und nennt
		// dabei den CNAME, den der Admin einmal setzen muss.
		ID:     "acmedns",
		Fields: []Field{{Name: "api_base", Kind: FieldText, Required: true, check: checkHTTPURL}},
		build:  buildACMEDNS,
	},
	{
		ID: "httpreq",
		Fields: withTimings(
			Field{Name: "endpoint", Kind: FieldText, Required: true, check: checkHTTPURL},
			Field{Name: "mode", Kind: FieldSelect, Options: []string{modeStandard, modeRaw}, Default: modeStandard},
			Field{Name: "username", Kind: FieldText},
			Field{Name: "password", Kind: FieldSecret},
		),
		build: buildHTTPReq,
	},
	{
		// Das Programm steht in der config.json (acme_dns_exec), nie in den
		// Einstellungen: Ein Konto in der Oberfläche darf keinen Befehl auf
		// dem LCM-Host festlegen.
		ID: ProviderExec,
		Fields: withTimings(
			Field{Name: "mode", Kind: FieldSelect, Options: []string{modeStandard, modeRaw}, Default: modeStandard},
		),
		build: buildExec,
	},
	{
		ID:     "cloudflare",
		Fields: withTimings(Field{Name: "api_token", Kind: FieldSecret, Required: true}),
		build:  buildCloudflare,
	},
	{
		// Der Hetzner-Anbieter von lego v5 kennt zwei Wege: das Token der
		// Cloud-API (neu) und den Schlüssel der alten DNS Console (veraltet).
		// LCM nimmt den neuen.
		ID:     "hetzner",
		Fields: withTimings(Field{Name: "api_token", Kind: FieldSecret, Required: true}),
		build:  buildHetzner,
	},
	{
		ID: "pdns",
		Fields: withTimings(
			Field{Name: "api_url", Kind: FieldText, Required: true, check: checkHTTPURL},
			Field{Name: "api_key", Kind: FieldSecret, Required: true},
			Field{Name: "server_name", Kind: FieldText, Default: "localhost"},
		),
		build: buildPDNS,
	},
	{
		ID: "inwx",
		Fields: withTimings(
			Field{Name: "username", Kind: FieldText, Required: true},
			Field{Name: "password", Kind: FieldSecret, Required: true},
			Field{Name: "totp_secret", Kind: FieldSecret},
			Field{Name: "sandbox", Kind: FieldBool},
		),
		build: buildINWX,
	},
	{
		ID:     "ionos",
		Fields: withTimings(Field{Name: "api_key", Kind: FieldSecret, Required: true, check: checkIONOSKey}),
		build:  buildIONOS,
	},
}

// provider macht aus dem typisierten Ergebnis eines lego-Konstruktors eine
// Schnittstelle, ohne dass ein Fehler einen "nil-Zeiger in Schnittstelle"
// hinterlässt.
func provider[P challenge.Provider](p P, err error) (challenge.Provider, error) {
	if err != nil {
		return nil, err
	}
	return p, nil
}

func apiClient() *http.Client { return &http.Client{Timeout: apiTimeout} }

func verifyTSIG(values map[string]string) *FieldError {
	if (values["tsig_key"] == "") != (values["tsig_secret"] == "") {
		return &FieldError{Field: "tsig_secret", Detail: "tsig_key und tsig_secret gehören zusammen"}
	}
	return nil
}

func checkIONOSKey(s string) error {
	prefix, secret, found := strings.Cut(s, ".")
	if !found || prefix == "" || secret == "" {
		return errors.New("erwartet wird <präfix>.<geheimnis>")
	}
	return nil
}

func buildRFC2136(b dnsBuild) (challenge.Provider, error) {
	propagation, polling := b.timeouts(defaultTiming)
	algorithm := b.values["tsig_algorithm"]
	if algorithm == "" {
		algorithm = "hmac-sha256"
	}
	return provider(dnsupdate.NewDNSProviderConfig(&dnsupdate.Config{
		Nameserver:         b.values["nameserver"],
		DNSTimeout:         10 * time.Second,
		TSIGAlgorithm:      algorithm,
		TSIGKey:            b.values["tsig_key"],
		TSIGSecret:         b.values["tsig_secret"],
		PropagationTimeout: propagation,
		PollingInterval:    polling,
		TTL:                dns01.DefaultTTL,
		SequenceInterval:   dns01.DefaultPropagationTimeout,
	}))
}

func buildACMEDNS(b dnsBuild) (challenge.Provider, error) {
	// lego schreibt die Datei, legt aber das Verzeichnis nicht an.
	if err := os.MkdirAll(filepath.Dir(b.storage), 0o700); err != nil {
		return nil, fmt.Errorf("verzeichnis für acme-dns-konten: %w", err)
	}
	return provider(acmedns.NewDNSProviderConfig(&acmedns.Config{
		APIBase:     b.values["api_base"],
		StoragePath: b.storage,
	}))
}

func buildHTTPReq(b dnsBuild) (challenge.Provider, error) {
	endpoint, err := url.Parse(b.values["endpoint"])
	if err != nil {
		return nil, err
	}
	propagation, polling := b.timeouts(defaultTiming)
	return provider(httpreq.NewDNSProviderConfig(&httpreq.Config{
		Endpoint:           endpoint,
		Mode:               legoMode(b.values["mode"]),
		Username:           b.values["username"],
		Password:           b.values["password"],
		PropagationTimeout: propagation,
		PollingInterval:    polling,
		HTTPClient:         apiClient(),
	}))
}

func buildExec(b dnsBuild) (challenge.Provider, error) {
	propagation, polling := b.timeouts(defaultTiming)
	return provider(exec.NewDNSProviderConfig(&exec.Config{
		Program:            b.execPath,
		Mode:               legoMode(b.values["mode"]),
		PropagationTimeout: propagation,
		PollingInterval:    polling,
		SequenceInterval:   dns01.DefaultPropagationTimeout,
	}))
}

// legoMode: lego kennt den Rohmodus als "RAW", sonst leer.
func legoMode(mode string) string {
	if mode == modeRaw {
		return "RAW"
	}
	return ""
}

func buildCloudflare(b dnsBuild) (challenge.Provider, error) {
	propagation, polling := b.timeouts(timing{2 * time.Minute, dns01.DefaultPollingInterval})
	return provider(cloudflare.NewDNSProviderConfig(&cloudflare.Config{
		AuthToken:          b.values["api_token"],
		TTL:                120,
		PropagationTimeout: propagation,
		PollingInterval:    polling,
		HTTPClient:         apiClient(),
	}))
}

func buildHetzner(b dnsBuild) (challenge.Provider, error) {
	propagation, polling := b.timeouts(timing{2 * time.Minute, dns01.DefaultPollingInterval})
	return provider(hetzner.NewDNSProviderConfig(&hetzner.Config{
		APIToken:           b.values["api_token"],
		TTL:                60,
		PropagationTimeout: propagation,
		PollingInterval:    polling,
		HTTPClient:         apiClient(),
	}))
}

func buildPDNS(b dnsBuild) (challenge.Provider, error) {
	host, err := url.Parse(b.values["api_url"])
	if err != nil {
		return nil, err
	}
	serverName := b.values["server_name"]
	if serverName == "" {
		serverName = "localhost"
	}
	propagation, polling := b.timeouts(timing{2 * time.Minute, 2 * time.Second})
	return provider(pdns.NewDNSProviderConfig(&pdns.Config{
		Host:       host,
		APIKey:     b.values["api_key"],
		ServerName: serverName,
		// Ohne Angabe fragte lego beim Bauen die Version beim Server ab.
		// PowerDNS ab 4.0 spricht /api/v1.
		APIVersion:         1,
		TTL:                dns01.DefaultTTL,
		PropagationTimeout: propagation,
		PollingInterval:    polling,
		HTTPClient:         apiClient(),
	}))
}

func buildINWX(b dnsBuild) (challenge.Provider, error) {
	propagation, polling := b.timeouts(timing{6 * time.Minute, dns01.DefaultPollingInterval})
	return provider(inwx.NewDNSProviderConfig(&inwx.Config{
		Username:           b.values["username"],
		Password:           b.values["password"],
		SharedSecret:       b.values["totp_secret"],
		Sandbox:            b.values["sandbox"] == "true",
		TTL:                300,
		PropagationTimeout: propagation,
		PollingInterval:    polling,
	}))
}

func buildIONOS(b dnsBuild) (challenge.Provider, error) {
	propagation, polling := b.timeouts(timing{15 * time.Minute, dns01.DefaultPollingInterval})
	return provider(ionos.NewDNSProviderConfig(&ionos.Config{
		APIKey:             b.values["api_key"],
		TTL:                300,
		PropagationTimeout: propagation,
		PollingInterval:    polling,
		HTTPClient:         apiClient(),
	}))
}
