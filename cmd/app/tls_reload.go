package main

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"

	"LCM/internal/config"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/acmex"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/safego"
)

// Auslöser eines Neuladens, wie sie im Audit-Log als Actor stehen. Beim Start
// wird nichts protokolliert (leerer Actor): Der Start steht im Journal, und
// ein Audit-Eintrag je Neustart wäre Lärm.
const (
	reloadByCLI    = "cli"
	reloadBySignal = "signal"
)

// Einträge im Audit-Log zu den Zertifikaten.
const (
	auditTLSReload   = "settings.tls.reload"
	auditTLSUpload   = "settings.tls.upload"
	auditTLSRemove   = "settings.tls.remove"
	auditTLSDownload = "settings.tls.download"
)

// publicAddresses sind die öffentlichen Adressen aus den Einstellungen; leer
// heißt nicht gesetzt.
type publicAddresses struct {
	UI, Agent, MCP string
	// AgentEnabled und MCPEnabled sind die Schalter der Dienste: Am gemeinsamen
	// Port bekommt ein ausgeschalteter Dienst keine Verbindung.
	AgentEnabled, MCPEnabled bool
}

// storedPublicAddresses liest die öffentlichen Adressen aus den
// Einstellungen. Nicht lesbar heißt hier: keine Adresse.
func storedPublicAddresses(settings *services.SettingsService) publicAddresses {
	st, err := settings.Get()
	if err != nil {
		slog.Warn("public addresses not readable", "error", err)
		return publicAddresses{}
	}
	a := settings.AddressesOf(st)
	return publicAddresses{
		UI: st.PublicBaseURL, Agent: st.PublicAgentURL, MCP: st.PublicMCPURL,
		AgentEnabled: a.AgentEnabled, MCPEnabled: a.MCPEnabled,
	}
}

// reloadOnSIGHUP lädt die Zertifikate bei SIGHUP neu (systemctl reload lcm).
// Der Handler ist von dem für SIGINT/SIGTERM getrennt.
func reloadOnSIGHUP(reloader *certReloader) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	safego.Go("sighup-reload", func() { reloadOnSignals(hup, reloader) })
}

// reloadOnSignals lädt je empfangenem Signal neu, bis der Kanal schließt.
func reloadOnSignals(signals <-chan os.Signal, reloader *certReloader) {
	for range signals {
		slog.Info("SIGHUP received - reloading tls certificates")
		reloader.Reload(reloadBySignal)
	}
}

// certReloader lädt den Zertifikatspool aus config.json, Uploads und ACME neu und
// beschreibt seinen Stand. Eine Quelle, die sich nicht lesen lässt, behält
// ihren letzten gültigen Stand.
type certReloader struct {
	pool       *tlsx.Pool
	configPath string
	dataDir    string
	audit      func(actor, action, details string)
	addresses  func() publicAddresses
	// shared beschreibt den gemeinsamen Port für `lcm tls status`; nil ohne ihn.
	shared func() *sharedStatus

	mu          sync.Mutex
	configSpecs []tlsx.PoolSpec // zuletzt gültig gelesen
	uploadSpecs []tlsx.PoolSpec
	acmeSpecs   []tlsx.PoolSpec
}

func newCertReloader(pool *tlsx.Pool, cfg *config.Config, configPath, dataDir string,
	audit func(actor, action, details string), addresses func() publicAddresses) *certReloader {
	return &certReloader{
		pool:        pool,
		configPath:  configPath,
		dataDir:     dataDir,
		audit:       audit,
		addresses:   addresses,
		configSpecs: configCertificateSpecs(cfg.TLSCertificates),
	}
}

// certResult ist der Ausgang für ein Zertifikat beim Neuladen.
type certResult struct {
	ID    string           `json:"id"`
	Error string           `json:"error,omitempty"` // abgelehnt, mit Grund
	Kept  bool             `json:"kept,omitempty"`  // trotz Fehler bleibt der bisherige Stand aktiv
	Cert  *tlsx.CertStatus `json:"cert,omitempty"`  // der jetzt aktive Eintrag
}

// reloadReport ist das Ergebnis eines Neuladens.
type reloadReport struct {
	Certificates []certResult `json:"certificates"`
	// Problems betreffen die Quellen selbst: eine config.json, die sich nicht
	// lesen lässt, oder Uploads, die sich nicht auflisten lassen.
	Problems []string `json:"problems,omitempty"`
}

// Rejected zählt die Zertifikate, die der Pool abgelehnt hat.
func (r reloadReport) Rejected() int {
	n := 0
	for _, c := range r.Certificates {
		if c.Error != "" {
			n++
		}
	}
	return n
}

// OK: alles ließ sich laden.
func (r reloadReport) OK() bool {
	return r.Rejected() == 0 && len(r.Problems) == 0
}

// Reload liest config.json und die Uploads neu ein und lädt den Pool. Vom
// Abschnitt tls_certificates gilt nur er (der Rest der Datei bleibt, was der
// Dienst beim Start gelesen hat). Mit einem actor kommt ein Audit-Eintrag
// dazu.
func (r *certReloader) Reload(actor string) reloadReport {
	r.mu.Lock()
	defer r.mu.Unlock()

	var problems []string
	if files, err := config.LoadTLSCertificates(r.configPath); err != nil {
		problems = append(problems, fmt.Sprintf("%v - die bisherigen Einträge aus der config.json bleiben", err))
	} else {
		r.configSpecs = configCertificateSpecs(files)
	}
	if uploads, err := tlsx.UploadSpecs(r.dataDir); err != nil {
		problems = append(problems, fmt.Sprintf("%v - die bisherigen Uploads bleiben", err))
	} else {
		r.uploadSpecs = uploads
	}
	if issued, err := acmex.Specs(r.dataDir); err != nil {
		problems = append(problems, fmt.Sprintf("%v - die bisherigen ACME-Zertifikate bleiben", err))
	} else {
		r.acmeSpecs = issued
	}

	loaded := r.pool.Load(slices.Concat(r.configSpecs, r.uploadSpecs, r.acmeSpecs))
	logPoolReport(loaded)
	for _, p := range problems {
		slog.Warn("tls certificates: source not readable", "problem", p)
	}
	report := reloadReport{Certificates: certResults(loaded), Problems: problems}
	if actor != "" {
		r.audit(actor, auditTLSReload, auditDetails(report))
	}
	return report
}

func certResults(report tlsx.LoadReport) []certResult {
	results := make([]certResult, 0, len(report))
	for _, l := range report {
		res := certResult{ID: l.ID, Kept: l.Kept, Cert: l.Cert}
		if l.Err != nil {
			res.Error = l.Err.Error()
		}
		results = append(results, res)
	}
	return results
}

func auditDetails(r reloadReport) string {
	details := fmt.Sprintf("geladen=%d abgelehnt=%d", len(r.Certificates)-r.Rejected(), r.Rejected())
	if len(r.Problems) > 0 {
		details += fmt.Sprintf(" probleme=%d", len(r.Problems))
	}
	return details
}

// endpointStatus nennt, welches Zertifikat ein Endpunkt für den Namen seiner
// öffentlichen Adresse ausliefert. Ohne Adresse sind URL, Host und Cert leer:
// Der Endpunkt liefert dann das LCM-Zertifikat.
type endpointStatus struct {
	Name string           `json:"name"` // ui, agent oder mcp
	URL  string           `json:"url,omitempty"`
	Host string           `json:"host,omitempty"`
	Cert *tlsx.CertStatus `json:"cert,omitempty"`
}

// statusReport ist der Stand des Pools.
type statusReport struct {
	Certificates []tlsx.CertStatus `json:"certificates"`
	Endpoints    []endpointStatus  `json:"endpoints"`
	SharedPort   *sharedStatus     `json:"shared_port,omitempty"`
}

// Status beschreibt den Pool und, was jeder Endpunkt für seine öffentliche
// Adresse ausliefert.
func (r *certReloader) Status() statusReport {
	addr := r.addresses()
	report := statusReport{
		Certificates: r.pool.Status(),
		Endpoints: []endpointStatus{
			r.endpointStatus("ui", addr.UI),
			r.endpointStatus("agent", addr.Agent),
			r.endpointStatus("mcp", addr.MCP),
		},
	}
	if r.shared != nil {
		report.SharedPort = r.shared()
	}
	return report
}

func (r *certReloader) endpointStatus(name, rawURL string) endpointStatus {
	status := endpointStatus{Name: name}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return status
	}
	cert := r.pool.StatusFor(u.Hostname())
	status.URL, status.Host, status.Cert = rawURL, u.Hostname(), &cert
	return status
}
