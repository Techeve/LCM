package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/portscan"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// JobTypePortScan: Portscan von LCM aus (nur lesend, der Server wird dabei
// nicht angemeldet).
const JobTypePortScan = "port-scan"

// ErrPortScanNoTarget: Der Server hat keine Adresse, die LCM scannen kann -
// ein Agent-Server ohne Host-Angabe oder der LCM-Host über Loopback.
var ErrPortScanNoTarget = portscan.ErrNoTarget

// PortScanService prüft von LCM aus, welche Ports ein verwalteter Server
// anbietet, und gleicht sie mit der LCM-Firewall ab.
//
// Schutz vor Missbrauch als Portscanner (SSRF): Das Ziel ist IMMER die
// gespeicherte Adresse eines Servers, den der Aufrufer sehen darf. Eine
// Adresse aus der Anfrage gibt es nicht. Die öffentliche Demo sperrt den
// Start zusätzlich (middlewares.DemoGuard), Demo-Server werden nie
// kontaktiert.
type PortScanService struct {
	servers *repositories.ServerRepository
	scans   *repositories.PortScanRepository
	jobs    *JobService
	audit   *AuditService
	scanner func() *portscan.Scanner
	lookup  portscan.LookupFunc
}

func NewPortScanService(servers *repositories.ServerRepository, scans *repositories.PortScanRepository,
	jobs *JobService, audit *AuditService) *PortScanService {
	return &PortScanService{servers: servers, scans: scans, jobs: jobs, audit: audit,
		scanner: portscan.New, lookup: portscan.DefaultLookup}
}

// WithScanner ersetzt Scanner und Namensauflösung (Tests).
func (s *PortScanService) WithScanner(scanner func() *portscan.Scanner, lookup portscan.LookupFunc) *PortScanService {
	s.scanner, s.lookup = scanner, lookup
	return s
}

// PortScanView ist die Lesesicht des letzten Laufs samt Bewertung.
type PortScanView struct {
	ScannedAt *time.Time              `json:"scanned_at"`
	JobID     string                  `json:"job_id,omitempty"`
	Targets   []domain.PortScanTarget `json:"targets"`
	Ports     []PortScanPort          `json:"open_ports"`
	// FirewallCompared: Es gab einen LCM-Regelsatz zum Abgleich (nicht bei
	// DSM und RouterOS). FirewallActive: LCM hat die Firewall eingeschaltet -
	// sonst ist der Regelsatz nur ein Entwurf und jeder offene Port ungefiltert.
	FirewallCompared bool `json:"firewall_compared"`
	FirewallActive   bool `json:"firewall_active"`
	Critical         int  `json:"critical"`
	Warnings         int  `json:"warnings"`
}

// Start prüft Zugriff und Adresse und startet den Scan als Job.
func (s *PortScanService) Start(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if !portScannable(server) {
		return nil, ErrPortScanNoTarget
	}
	job, err := s.jobs.Start(&server.ID, nil, JobTypePortScan, "Portscan @ "+server.Name, actor)
	if err != nil {
		return nil, err // u.a. ErrServerBusy → 409
	}
	s.audit.Log(actor, "server.port-scan", "server", id, "")
	safego.GoCleanup("job:port-scan", jobPanicCleanup(s.jobs, job), func() {
		s.run(job, server)
	})
	return job, nil
}

// portScannable: Agent-Server stehen meist hinter NAT und haben keine
// Host-Angabe; der LCM-Host über Loopback sähe nur sich selbst - mitsamt den
// Diensten, die nur lokal lauschen.
func portScannable(server *domain.Server) bool {
	host := strings.TrimSpace(server.Host)
	return !server.IsAgent() && host != "" && !domain.IsLoopbackHost(host)
}

// cancelCloser bricht den Scan ab, wenn der Job abgebrochen wird.
type cancelCloser context.CancelFunc

func (c cancelCloser) Close() error {
	c()
	return nil
}

// run führt den Scan aus und legt das Ergebnis ab.
func (s *PortScanService) run(job *domain.Job, server *domain.Server) {
	if server.IsDemo {
		s.jobs.CompleteSkipped(job, "Demo-Server - Portscan übersprungen.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.jobs.AttachCloser(job.ID, cancelCloser(cancel))

	result, err := s.scan(ctx, job.ID, server)
	if err != nil {
		s.jobs.Complete(job, "", nil, fmt.Errorf("portscan: %w", err))
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		s.jobs.Complete(job, "", nil, err)
		return
	}
	scan := &domain.PortScan{ServerID: server.ID, ScannedAt: time.Now(), JobID: job.ID, Result: string(raw)}
	if err := s.scans.Save(scan); err != nil {
		s.jobs.Complete(job, "", nil, fmt.Errorf("ergebnis speichern: %w", err))
		return
	}
	s.jobs.Complete(job, portScanSummary(result, firewallView(server)), ptrInt(0), nil)
}

// scan bestimmt die Adressen und prüft die Ports.
func (s *PortScanService) scan(ctx context.Context, jobID string, server *domain.Server) (domain.PortScanResult, error) {
	addrs, err := portscan.Targets(ctx, server.Host, splitAddresses(server.IPAddresses), s.lookup)
	if err != nil {
		return domain.PortScanResult{}, err
	}
	scanner := s.scanner()
	scanner.OnProbe = func() { s.jobs.MarkActivity(jobID) }
	probes, err := scanner.Scan(ctx, addrs, portscan.Ports(server.SSHPort, server.DSMPort))
	if errors.Is(err, context.Canceled) {
		return domain.PortScanResult{}, errors.New("abgebrochen")
	}
	if err != nil {
		return domain.PortScanResult{}, err
	}
	return buildResult(addrs, probes), nil
}

// splitAddresses zerlegt das kommagetrennte Adress-Inventar des Servers.
func splitAddresses(raw string) []string {
	var out []string
	for _, a := range strings.Split(raw, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// buildResult ordnet die Versuche ihren Adressen zu.
func buildResult(addrs []netip.Addr, probes []portscan.Probe) domain.PortScanResult {
	byAddr := map[netip.Addr]*domain.PortScanTarget{}
	result := domain.PortScanResult{Targets: make([]domain.PortScanTarget, len(addrs))}
	for i, a := range addrs {
		result.Targets[i] = domain.PortScanTarget{Address: a.String(), Family: family(a), Open: []int{}}
		byAddr[a] = &result.Targets[i]
	}
	for _, p := range probes {
		t := byAddr[p.Addr]
		if t == nil {
			continue
		}
		switch p.State {
		case portscan.Open:
			t.Open = append(t.Open, p.Port)
		case portscan.Closed:
			t.Closed++
		default:
			t.Filtered++
		}
	}
	return result
}

func family(a netip.Addr) string {
	if a.Is4() {
		return "v4"
	}
	return "v6"
}

// Latest liefert den letzten Lauf, bewertet gegen den aktuellen Regelsatz.
// Wurde der Server noch nie gescannt, ist ScannedAt nil.
func (s *PortScanService) Latest(scope repositories.AccessScope, id uint) (*PortScanView, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	fw := firewallView(server)
	view := &PortScanView{Targets: []domain.PortScanTarget{}, Ports: []PortScanPort{},
		FirewallCompared: fw.compared, FirewallActive: server.FirewallActive}
	scan, err := s.scans.Latest(server.ID)
	if errors.Is(err, repositories.ErrNotFound) {
		return view, nil
	}
	if err != nil {
		return nil, err
	}
	var result domain.PortScanResult
	if err := json.Unmarshal([]byte(scan.Result), &result); err != nil {
		return nil, fmt.Errorf("portscan-ergebnis unlesbar: %w", err)
	}
	view.ScannedAt, view.JobID = &scan.ScannedAt, scan.JobID
	view.Targets = result.Targets
	view.Ports = portScanPorts(result, fw)
	view.Critical, view.Warnings = countSeverities(view.Ports)
	return view, nil
}

// portScanSummary ist die Ausgabe des Jobs: Adressen, offene Ports, Befunde.
func portScanSummary(result domain.PortScanResult, fw portFirewall) string {
	var b strings.Builder
	b.WriteString("Portscan von LCM aus (TCP-Verbindungsaufbau)\n")
	for _, t := range result.Targets {
		fmt.Fprintf(&b, "%s: offen %s, geschlossen %d, ohne Antwort %d\n", t.Address, portList(t.Open), t.Closed, t.Filtered)
	}
	ports := portScanPorts(result, fw)
	critical, warnings := countSeverities(ports)
	fmt.Fprintf(&b, "Befunde: %d kritisch, %d Warnung(en)\n", critical, warnings)
	for _, p := range ports {
		if p.Severity == PortSeverityOK {
			continue
		}
		fmt.Fprintf(&b, "  %s %d %s: %s\n", p.Severity, p.Port, p.Service, strings.Join(p.Reasons, ", "))
	}
	return b.String()
}

func portList(ports []int) string {
	if len(ports) == 0 {
		return "keine"
	}
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = fmt.Sprint(p)
	}
	return strings.Join(parts, ", ")
}
