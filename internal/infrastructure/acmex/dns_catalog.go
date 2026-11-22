package acmex

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/challenge/dns01"

	"LCM/internal/inputcheck"
)

// Die Anbieter für DNS-01 sind einzeln eingebunden (dns_providers.go), nie
// über die Fabrik providers/dns. Jeder Anbieter entsteht aus festen Werten
// über NewDNSProviderConfig: NewDNSProvider und NewDefaultConfig lesen
// Umgebungsvariablen, und eine Variable wie CLOUDFLARE_DNS_API_TOKEN am LCM-
// Prozess darf nie ein Konto ersetzen.

// FieldKind ist die Art eines Eingabefelds.
type FieldKind string

const (
	FieldText   FieldKind = "text"
	FieldSecret FieldKind = "secret" // wird nie ausgeliefert
	FieldBool   FieldKind = "bool"   // Werte "true" und "false"
	FieldSelect FieldKind = "select"
	FieldNumber FieldKind = "number" // ganze Sekunden
)

// Field beschreibt ein Feld, aus dem die Oberfläche das Formular baut.
type Field struct {
	Name     string    `json:"name"`
	Kind     FieldKind `json:"type"`
	Required bool      `json:"required"`
	Options  []string  `json:"options,omitempty"` // bei select
	// Default ist der Wert, den LCM bei leerem Feld nimmt (nur Anzeige).
	Default string `json:"default,omitempty"`

	check func(string) error
}

// DNSProvider beschreibt einen Anbieter: Kennung und Felder.
type DNSProvider struct {
	ID     string  `json:"id"`
	Fields []Field `json:"fields"`

	// verify prüft, was über einzelne Felder hinausgeht.
	verify func(values map[string]string) *FieldError
	build  func(in dnsBuild) (challenge.Provider, error)
}

// dnsBuild sind die Eingaben, aus denen ein Anbieter entsteht.
type dnsBuild struct {
	values   map[string]string
	storage  string // Datei für Konten bei acme-dns
	execPath string
}

// DNSEnv ist, was LCM selbst beisteuert und nie aus den Einstellungen kommt.
type DNSEnv struct {
	DataDir string
	// ExecPath ist das Programm für den Anbieter exec (config.json).
	ExecPath string
}

// FieldError: Ein Feld fehlt oder ist ungültig.
type FieldError struct {
	Field   string
	Missing bool
	Detail  string
}

func (e *FieldError) Error() string {
	if e.Missing {
		return e.Field + ": fehlt"
	}
	return e.Field + ": " + e.Detail
}

const (
	// ProviderExec ist der Anbieter, der ein Programm des Hosts aufruft.
	ProviderExec = "exec"

	maxValueLength = 2048

	fieldPropagationTimeout = "propagation_timeout"
	fieldPollingInterval    = "polling_interval"
)

// Standardwerte von lego, je Anbieter verschieden.
type timing struct{ propagation, polling time.Duration }

var defaultTiming = timing{dns01.DefaultPropagationTimeout, dns01.DefaultPollingInterval}

// DNSProviders nennt die Anbieter; exec nur, wenn die config.json ein
// Programm dafür festlegt: Ein Konto in der Oberfläche darf keinen Befehl auf
// dem LCM-Host bestimmen.
func DNSProviders(execEnabled bool) []DNSProvider {
	return slices.DeleteFunc(slices.Clone(dnsProviders), func(p DNSProvider) bool {
		return p.ID == ProviderExec && !execEnabled
	})
}

// LookupDNSProvider findet einen Anbieter nach Kennung.
func LookupDNSProvider(id string) (DNSProvider, bool) {
	i := slices.IndexFunc(dnsProviders, func(p DNSProvider) bool { return p.ID == id })
	if i < 0 {
		return DNSProvider{}, false
	}
	return dnsProviders[i], true
}

// SecretFields nennt die Geheimfelder des Anbieters.
func (p DNSProvider) SecretFields() []string {
	var names []string
	for _, f := range p.Fields {
		if f.Kind == FieldSecret {
			names = append(names, f.Name)
		}
	}
	return names
}

// Validate prüft die Werte gegen die Felder des Anbieters. Nichts davon
// braucht das Netz.
func (p DNSProvider) Validate(values map[string]string) *FieldError {
	for name := range values {
		if !slices.ContainsFunc(p.Fields, func(f Field) bool { return f.Name == name }) {
			return &FieldError{Field: name, Detail: "unbekanntes feld"}
		}
	}
	for _, f := range p.Fields {
		if err := f.validate(values[f.Name]); err != nil {
			return err
		}
	}
	if p.verify != nil {
		return p.verify(values)
	}
	return nil
}

func (f Field) validate(value string) *FieldError {
	if value == "" {
		if f.Required {
			return &FieldError{Field: f.Name, Missing: true}
		}
		return nil
	}
	if len(value) > maxValueLength {
		return &FieldError{Field: f.Name, Detail: "der wert ist zu lang"}
	}
	var err error
	switch f.Kind {
	case FieldSelect:
		if !slices.Contains(f.Options, value) {
			err = fmt.Errorf("erlaubt sind %s", strings.Join(f.Options, ", "))
		}
	case FieldBool:
		if value != "true" && value != "false" {
			err = errors.New("erlaubt sind true und false")
		}
	case FieldNumber:
		err = checkSeconds(value)
	}
	if err == nil && f.check != nil {
		err = f.check(value)
	}
	if err != nil {
		return &FieldError{Field: f.Name, Detail: err.Error()}
	}
	return nil
}

// NewDNSProvider baut den Anbieter aus den Werten. Er prüft sie vorher;
// ohne Netz.
func NewDNSProvider(id string, values map[string]string, env DNSEnv) (challenge.Provider, error) {
	p, ok := LookupDNSProvider(id)
	if !ok {
		return nil, fmt.Errorf("unbekannter dns-anbieter %q", id)
	}
	if id == ProviderExec && env.ExecPath == "" {
		return nil, errors.New("der anbieter exec braucht acme_dns_exec in der config.json")
	}
	if err := p.Validate(values); err != nil {
		return nil, err
	}
	return p.build(dnsBuild{values: values, storage: acmeDNSStoragePath(env.DataDir, values["api_base"]), execPath: env.ExecPath})
}

// withTimings hängt Zeitlimit und Prüfintervall an die Felder: ein Paar, das
// alle Anbieter außer acme-dns kennen.
func withTimings(fields ...Field) []Field {
	return append(fields,
		Field{Name: fieldPropagationTimeout, Kind: FieldNumber},
		Field{Name: fieldPollingInterval, Kind: FieldNumber},
	)
}

// seconds liest ein Feld in Sekunden; leer heißt Standard des Anbieters.
func (b dnsBuild) seconds(name string, fallback time.Duration) time.Duration {
	n, err := strconv.Atoi(b.values[name])
	if err != nil || n <= 0 {
		return fallback
	}
	return time.Duration(n) * time.Second
}

func (b dnsBuild) timeouts(def timing) (propagation, polling time.Duration) {
	return b.seconds(fieldPropagationTimeout, def.propagation), b.seconds(fieldPollingInterval, def.polling)
}

func checkSeconds(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 3600 {
		return errors.New("erlaubt sind 1 bis 3600 sekunden")
	}
	return nil
}

// CheckNameserver prüft "host" oder "host:port".
func CheckNameserver(s string) error {
	host := s
	if h, port, err := net.SplitHostPort(s); err == nil {
		n, convErr := strconv.Atoi(port)
		if convErr != nil || n < 1 || n > 65535 {
			return errors.New("ungültiger port")
		}
		host = h
	}
	if inputcheck.HostProblem(host) != "" {
		return errors.New("kein gültiger host")
	}
	return nil
}

// checkHTTPURL prüft eine Adresse mit http oder https und Host. Interne
// Adressen ohne TLS sind erlaubt (PowerDNS-API, eigener Endpunkt im LAN).
func checkHTTPURL(s string) error {
	u, err := url.Parse(s)
	switch {
	case err != nil || u.Host == "":
		return errors.New("keine adresse")
	case u.Scheme != "http" && u.Scheme != "https":
		return errors.New("die adresse muss mit http:// oder https:// beginnen")
	case u.User != nil:
		return errors.New("die adresse darf keine zugangsdaten enthalten")
	}
	return nil
}

func checkBase64(s string) error {
	if _, err := base64.StdEncoding.DecodeString(s); err != nil {
		return errors.New("kein base64")
	}
	return nil
}
