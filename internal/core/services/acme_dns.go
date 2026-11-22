package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/acmex"
)

// DNS-01: Die Werte des Anbieters stehen als ein JSON in der Datenbank, als
// Ganzes verschlüsselt: Geheimnisse und der Rest. Die API liefert die
// Geheimnisse nie aus; sie meldet je Geheimfeld nur, ob es gesetzt ist.

const (
	maxDNSNameservers = 3

	dnsTestTimeout = 3 * time.Minute
)

// DNSTestWait ist, wie lange die Probe auf den TXT-Eintrag wartet. Ein
// Anbieter, der länger braucht, kann im Lauf trotzdem gelingen: Dort gilt
// sein eigenes Zeitlimit. Tests kürzen den Wert.
var DNSTestWait = 60 * time.Second

// ACMEDNSTestError: Die Probe des DNS-Anbieters ist gescheitert.
type ACMEDNSTestError struct{ Detail string }

func (e *ACMEDNSTestError) Error() string { return e.Detail }

// ACMEDNSTest bestimmt die Probe. Was fehlt, kommt aus den gespeicherten
// Einstellungen; Geheimnisse, die nicht dabei sind, ebenfalls.
type ACMEDNSTest struct {
	Names       []string
	Provider    string
	Values      map[string]string
	Nameservers []string
}

// dnsView sind die Angaben zu DNS-01 im Stand.
type dnsView struct {
	Provider    string
	Values      map[string]any
	SecretsSet  map[string]bool
	Nameservers []string
}

func (s *ACMEService) dnsView(st *domain.GlobalSettings) dnsView {
	v := dnsView{Provider: st.ACMEDNSProvider, Values: map[string]any{}, SecretsSet: map[string]bool{},
		Nameservers: cleanEntries(strings.Split(st.ACMEDNSNameservers, ","))}
	p, ok := acmex.LookupDNSProvider(st.ACMEDNSProvider)
	if !ok {
		return v
	}
	stored := s.storedDNSValues(st)
	for _, f := range p.Fields {
		switch {
		case f.Kind == acmex.FieldSecret:
			v.SecretsSet[f.Name] = stored[f.Name] != ""
		case stored[f.Name] != "":
			v.Values[f.Name] = viewValue(f, stored[f.Name])
		}
	}
	return v
}

// viewValue gibt Zahlen und Wahrheitswerte als solche aus.
func viewValue(f acmex.Field, value string) any {
	switch f.Kind {
	case acmex.FieldBool:
		return value == "true"
	case acmex.FieldNumber:
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return value
}

// storedDNSValues entschlüsselt die gespeicherten Werte. Lässt sich die
// Spalte nicht lesen (anderer Schlüssel nach einer Wiederherstellung), gelten
// die Werte als nicht gesetzt: Der Admin trägt sie neu ein.
func (s *ACMEService) storedDNSValues(st *domain.GlobalSettings) map[string]string {
	if st.ACMEDNSConfigEnc == "" {
		return nil
	}
	plain, err := s.cipher.DecryptString(st.ACMEDNSConfigEnc)
	if err != nil {
		slog.Warn("acme dns settings not readable", "error", err)
		return nil
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(plain), &values); err != nil {
		slog.Warn("acme dns settings not readable", "error", err)
		return nil
	}
	return values
}

// dnsSettings prüft die Angaben zu DNS-01 und liefert die Spalten, die zu
// speichern sind. Bei einer anderen Challenge bleibt alles, wie es ist.
func (s *ACMEService) dnsSettings(st *domain.GlobalSettings, in ACMEUpdate) (map[string]any, error) {
	if in.Challenge != ACMEChallengeDNS01 {
		return nil, nil
	}
	nameservers := strings.Join(in.DNSNameservers, ",")
	if in.DNSProvider == "" {
		if in.Enabled {
			return nil, &ACMEInvalidError{Field: "dns_provider", Missing: true}
		}
		return map[string]any{"acme_dns_provider": "", "acme_dns_config_enc": "", "acme_dns_nameservers": nameservers}, nil
	}
	values, err := s.dnsValues(st, in.DNSProvider, in.DNSValues, in.Enabled)
	if err != nil {
		return nil, err
	}
	enc, err := s.encryptDNSValues(values)
	if err != nil {
		return nil, err
	}
	return map[string]any{"acme_dns_provider": in.DNSProvider, "acme_dns_config_enc": enc, "acme_dns_nameservers": nameservers}, nil
}

// dnsValues bringt die Werte aus der Anfrage und die gespeicherten
// Geheimnisse zusammen und prüft sie. Ein Geheimnis, das die Anfrage nicht
// nennt, bleibt (nur beim selben Anbieter); ein leeres wird gelöscht. Fehlende
// Pflichtfelder rügt nur, wer ACME einschaltet.
func (s *ACMEService) dnsValues(st *domain.GlobalSettings, id string, sent map[string]string, enabled bool) (map[string]string, error) {
	p, err := s.selectableProvider(id)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for name, value := range sent {
		if value != "" {
			values[name] = value
		}
	}
	if st.ACMEDNSProvider == id {
		stored := s.storedDNSValues(st)
		for _, name := range p.SecretFields() {
			if _, given := sent[name]; !given && stored[name] != "" {
				values[name] = stored[name]
			}
		}
	}
	if ferr := p.Validate(values); ferr != nil && (enabled || !ferr.Missing) {
		return nil, &ACMEInvalidError{Field: "dns_values." + ferr.Field, Detail: ferr.Detail, Missing: ferr.Missing}
	}
	return values, nil
}

// selectableProvider findet den Anbieter, soweit ein Admin ihn wählen darf:
// exec nur, wenn die config.json ein Programm festlegt und es da ist.
func (s *ACMEService) selectableProvider(id string) (acmex.DNSProvider, error) {
	available := acmex.DNSProviders(s.dnsEnv.ExecPath != "")
	i := slices.IndexFunc(available, func(p acmex.DNSProvider) bool { return p.ID == id })
	if i < 0 {
		if id == acmex.ProviderExec {
			return acmex.DNSProvider{}, &ACMEInvalidError{Field: "dns_provider", Detail: "exec braucht acme_dns_exec in der config.json"}
		}
		return acmex.DNSProvider{}, &ACMEInvalidError{Field: "dns_provider", Detail: "unbekannter anbieter"}
	}
	if id == acmex.ProviderExec {
		if err := checkProgram(s.dnsEnv.ExecPath); err != nil {
			return acmex.DNSProvider{}, &ACMEInvalidError{Field: "dns_provider", Detail: "acme_dns_exec: " + err.Error()}
		}
	}
	return available[i], nil
}

// checkProgram: Der Pfad nennt eine ausführbare Datei.
func checkProgram(path string) error {
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return errors.New("die datei fehlt")
	case !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0:
		return errors.New("keine ausführbare datei")
	}
	return nil
}

func (s *ACMEService) encryptDNSValues(values map[string]string) (string, error) {
	plain, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return s.cipher.EncryptString(string(plain))
}

// normalizeNameservers: Prüf-Nameserver ohne Leere und Doppelte, höchstens drei.
func normalizeNameservers(servers []string) ([]string, error) {
	out := []string{}
	for _, server := range servers {
		server = strings.TrimSpace(server)
		if server == "" || slices.Contains(out, server) {
			continue
		}
		if err := acmex.CheckNameserver(server); err != nil {
			return nil, fmt.Errorf("%q: %w", server, err)
		}
		out = append(out, server)
	}
	if len(out) > maxDNSNameservers {
		return nil, fmt.Errorf("höchstens %d nameserver", maxDNSNameservers)
	}
	return out, nil
}

// solver richtet die Challenge ein, mit der der Lauf die Namen belegt.
func (s *ACMEService) solver(st *domain.GlobalSettings) (acmex.Solver, error) {
	if st.ACMEChallenge != ACMEChallengeDNS01 {
		return acmex.HTTP01(challengeWindow{svc: s}), nil
	}
	provider, err := acmex.NewDNSProvider(st.ACMEDNSProvider, s.storedDNSValues(st), s.dnsEnv)
	if err != nil {
		return nil, fmt.Errorf("dns-anbieter %s: %w", st.ACMEDNSProvider, err)
	}
	return acmex.DNS01(provider, cleanEntries(strings.Split(st.ACMEDNSNameservers, ","))), nil
}

// DNSTest setzt einen TXT-Eintrag _acme-challenge.<erster Name> mit einem
// Zufallswert über den Anbieter, wartet kurz auf seine Sichtbarkeit und
// räumt ihn wieder auf. Es läuft im Aufruf und nicht neben einem ACME-Lauf.
func (s *ACMEService) DNSTest(in ACMEDNSTest, actor string) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrACMERunning
	}
	defer s.running.Store(false)

	name, provider, nameservers, err := s.prepareDNSTest(in)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), dnsTestTimeout)
	defer cancel()
	err = acmex.ProbeDNS(ctx, provider, name, nameservers, DNSTestWait)
	detail := fmt.Sprintf("%s, %s: ok", in.Provider, name)
	if err != nil {
		detail = fmt.Sprintf("%s, %s: %v", in.Provider, name, err)
	}
	s.audit.Log(actor, "settings.acme.dns_test", "settings", 1, detail)
	if err != nil {
		return &ACMEDNSTestError{Detail: err.Error()}
	}
	return nil
}

// prepareDNSTest ergänzt die Probe aus den gespeicherten Einstellungen und
// baut den Anbieter.
func (s *ACMEService) prepareDNSTest(in ACMEDNSTest) (name string, provider acmex.Provider, nameservers []string, err error) {
	st, err := s.settings.Get()
	if err != nil {
		return "", nil, nil, err
	}
	if in.Provider == "" {
		in.Provider, in.Values = st.ACMEDNSProvider, s.storedDNSValues(st)
		in.Nameservers = cleanEntries(strings.Split(st.ACMEDNSNameservers, ","))
	}
	if in.Provider == "" {
		return "", nil, nil, &ACMEInvalidError{Field: "dns_provider", Missing: true}
	}
	if len(in.Names) == 0 {
		in.Names = s.runNames(st)
	}
	names, err := normalizeACMENames(in.Names, ACMEChallengeDNS01)
	if err != nil {
		return "", nil, nil, &ACMEInvalidError{Field: "names", Detail: err.Error()}
	}
	if len(names) == 0 {
		return "", nil, nil, &ACMEInvalidError{Field: "names", Missing: true}
	}
	nameservers, err = normalizeNameservers(in.Nameservers)
	if err != nil {
		return "", nil, nil, &ACMEInvalidError{Field: "dns_check_nameservers", Detail: err.Error()}
	}
	values, err := s.dnsValues(st, in.Provider, in.Values, true)
	if err != nil {
		return "", nil, nil, err
	}
	provider, err = acmex.NewDNSProvider(in.Provider, values, s.dnsEnv)
	if err != nil {
		return "", nil, nil, &ACMEDNSTestError{Detail: err.Error()}
	}
	return names[0], provider, nameservers, nil
}
