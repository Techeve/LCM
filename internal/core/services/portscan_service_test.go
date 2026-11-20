package services_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/portscan"
	"LCM/internal/storage/repositories"
)

// fakeNetz spielt die Gegenseite: Offen sind die genannten Adressen mit
// Port, alles andere lehnt ab. Jede gewählte Adresse wird festgehalten.
type fakeNetz struct {
	mu      sync.Mutex
	offen   map[string]bool
	gewählt []string
}

func (n *fakeNetz) dial(_ context.Context, _, address string) (net.Conn, error) {
	n.mu.Lock()
	n.gewählt = append(n.gewählt, address)
	offen := n.offen[address]
	n.mu.Unlock()
	if !offen {
		return nil, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
	}
	a, b := net.Pipe()
	_ = b.Close()
	return a, nil
}

func (n *fakeNetz) hosts() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, a := range n.gewählt {
		host, _, _ := net.SplitHostPort(a)
		if !seen[host] {
			seen[host] = true
			out = append(out, host)
		}
	}
	slices.Sort(out)
	return out
}

func portScanEnv(t *testing.T, offen ...string) (*testEnv, *services.PortScanService, *fakeNetz) {
	t.Helper()
	env := newTestEnv(t)
	netz := &fakeNetz{offen: map[string]bool{}}
	for _, a := range offen {
		netz.offen[a] = true
	}
	scanner := func() *portscan.Scanner {
		s := portscan.New()
		s.Dial, s.Gap, s.Timeout = netz.dial, 0, time.Second
		return s
	}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		t.Fatal("eine IP-Angabe darf keine Namensauflösung auslösen")
		return nil, nil
	}
	db := env.DB()
	svc := services.NewPortScanService(repositories.NewServerRepository(db), repositories.NewPortScanRepository(db),
		env.Jobs, services.NewAuditService(repositories.NewAuditRepository(db))).WithScanner(scanner, lookup)
	return env, svc, netz
}

func portScanServer(t *testing.T, env *testEnv, s domain.Server) *domain.Server {
	t.Helper()
	if s.Name == "" {
		s.Name = "web01"
	}
	s.ServiceUser, s.SSHPort = "lcm", 22
	if err := repositories.NewServerRepository(env.DB()).Create(&s); err != nil {
		t.Fatal(err)
	}
	return &s
}

func runPortScan(t *testing.T, env *testEnv, svc *services.PortScanService, id uint) *domain.Job {
	t.Helper()
	job, err := svc.Start(repositories.ScopeAll(), id, "admin")
	if err != nil {
		t.Fatalf("scan starten: %v", err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("scan fehlgeschlagen: %s", done.Output)
	}
	return done
}

func portByNumber(ports []services.PortScanPort, n int) *services.PortScanPort {
	for i := range ports {
		if ports[i].Port == n {
			return &ports[i]
		}
	}
	return nil
}

// Der Scan trifft genau die Adressen des gespeicherten Servers (Host plus
// seine gemeldete IPv6) - keine andere. Die Bewertung gleicht mit dem
// LCM-Regelsatz ab: SSH und freigegebene Ports sind in Ordnung, ein
// fehlender Port warnt, eine Datenbank ist kritisch.
func TestPortScanBewertetGegenDieFirewall(t *testing.T) {
	env, svc, netz := portScanEnv(t,
		"192.0.2.10:22", "192.0.2.10:80", "192.0.2.10:443", "192.0.2.10:3306", "[2001:db8::10]:443")
	server := portScanServer(t, env, domain.Server{
		Host: "192.0.2.10", IPAddresses: "192.0.2.10, fe80::1, 2001:db8::10",
		FirewallActive: true, FirewallRules: `[{"port":443,"proto":"tcp","ip_version":"v4"},{"port":3306,"proto":"tcp"}]`,
	})

	runPortScan(t, env, svc, server.ID)

	if got := netz.hosts(); !slices.Equal(got, []string{"192.0.2.10", "2001:db8::10"}) {
		t.Fatalf("gescannte Adressen %v - erlaubt sind nur die des Servers", got)
	}
	view, err := svc.Latest(repositories.ScopeAll(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.ScannedAt == nil || !view.FirewallCompared || !view.FirewallActive {
		t.Fatalf("Sicht unvollständig: %+v", view)
	}
	want := map[int]string{22: services.PortSeverityOK, 80: services.PortSeverityWarning,
		443: services.PortSeverityWarning, 3306: services.PortSeverityCritical}
	for port, sev := range want {
		p := portByNumber(view.Ports, port)
		if p == nil || p.Severity != sev {
			t.Errorf("Port %d: %+v, erwartet %s", port, p, sev)
		}
	}
	// 443 ist nur für IPv4 freigegeben, aber auch über IPv6 offen.
	if p := portByNumber(view.Ports, 443); p == nil || !slices.Equal(p.Families, []string{"v4", "v6"}) || *p.InFirewall {
		t.Errorf("443: %+v", p)
	}
	if view.Critical != 1 || view.Warnings != 2 {
		t.Errorf("Zählung %d/%d, erwartet 1 kritisch, 2 Warnungen", view.Critical, view.Warnings)
	}
}

// Eine neue Freigabe wirkt sofort auf die Bewertung, ohne neuen Scan.
func TestPortScanBewertetBeimLesenNeu(t *testing.T) {
	env, svc, _ := portScanEnv(t, "192.0.2.10:8080")
	server := portScanServer(t, env, domain.Server{Host: "192.0.2.10", FirewallActive: true})
	runPortScan(t, env, svc, server.ID)

	if err := env.DB().Model(&domain.Server{}).Where("id = ?", server.ID).
		Update("firewall_allowed_ports", "8080").Error; err != nil {
		t.Fatal(err)
	}
	view, err := svc.Latest(repositories.ScopeAll(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p := portByNumber(view.Ports, 8080); p == nil || p.Severity != services.PortSeverityOK {
		t.Fatalf("8080 nach der Freigabe: %+v", p)
	}
}

// DSM: LCM führt dort keinen Regelsatz - kein Abgleich, nur riskante Dienste.
func TestPortScanDSMOhneFirewallAbgleich(t *testing.T) {
	env, svc, _ := portScanEnv(t, "192.0.2.20:5001", "192.0.2.20:111")
	server := portScanServer(t, env, domain.Server{Name: "nas", Host: "192.0.2.20", OSID: domain.OSIDSynologyDSM})
	runPortScan(t, env, svc, server.ID)

	view, err := svc.Latest(repositories.ScopeAll(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.FirewallCompared {
		t.Fatal("DSM darf nicht gegen einen LCM-Regelsatz abgeglichen werden")
	}
	if p := portByNumber(view.Ports, 5001); p == nil || p.Severity != services.PortSeverityOK || p.InFirewall != nil {
		t.Errorf("5001: %+v", p)
	}
	if p := portByNumber(view.Ports, 111); p == nil || p.Severity != services.PortSeverityCritical {
		t.Errorf("111 (rpcbind): %+v", p)
	}
}

// Wer den Server nicht sehen darf, kann ihn weder scannen noch das Ergebnis
// lesen - und ein Server ohne scanbare Adresse startet gar nicht erst.
func TestPortScanNurFuerSichtbareServerMitAdresse(t *testing.T) {
	env, svc, netz := portScanEnv(t)
	server := portScanServer(t, env, domain.Server{Host: "192.0.2.10"})
	fremd := repositories.ScopeManager(9999)

	if _, err := svc.Start(fremd, server.ID, "eve"); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("Start ohne Sicht: %v", err)
	}
	if _, err := svc.Latest(fremd, server.ID); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("Lesen ohne Sicht: %v", err)
	}
	for _, host := range []string{"127.0.0.1", "localhost", ""} {
		s := portScanServer(t, env, domain.Server{Name: "h" + host, Host: host})
		if _, err := svc.Start(repositories.ScopeAll(), s.ID, "admin"); !errors.Is(err, services.ErrPortScanNoTarget) {
			t.Errorf("Host %q: %v, erwartet ErrPortScanNoTarget", host, err)
		}
	}
	agent := portScanServer(t, env, domain.Server{Name: "agent", Host: "192.0.2.60", Transport: domain.TransportAgent})
	if _, err := svc.Start(repositories.ScopeAll(), agent.ID, "admin"); !errors.Is(err, services.ErrPortScanNoTarget) {
		t.Errorf("Agent-Server: %v, erwartet ErrPortScanNoTarget", err)
	}
	if len(netz.hosts()) != 0 {
		t.Fatalf("es wurde gewählt: %v", netz.hosts())
	}
}

// Demo-Server werden nie kontaktiert.
func TestPortScanUeberspringtDemoServer(t *testing.T) {
	env, svc, netz := portScanEnv(t)
	server := portScanServer(t, env, domain.Server{Host: "192.0.2.30", IsDemo: true})
	runPortScan(t, env, svc, server.ID)
	if len(netz.hosts()) != 0 {
		t.Fatalf("Demo-Server kontaktiert: %v", netz.hosts())
	}
	view, err := svc.Latest(repositories.ScopeAll(), server.ID)
	if err != nil || view.ScannedAt != nil {
		t.Fatalf("Demo-Server hat ein Ergebnis: %+v / %v", view, err)
	}
}

// Ein Abbruch des Jobs beendet auch die laufenden Verbindungsversuche.
func TestPortScanAbbruchBeendetDenScan(t *testing.T) {
	env, svc, _ := portScanEnv(t)
	gestartet := make(chan struct{}, 64)
	beendet := make(chan struct{}, 64)
	svc.WithScanner(func() *portscan.Scanner {
		s := portscan.New()
		s.Gap, s.Timeout = 0, time.Minute
		s.Dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			gestartet <- struct{}{}
			<-ctx.Done()
			beendet <- struct{}{}
			return nil, ctx.Err()
		}
		return s
	}, nil)
	server := portScanServer(t, env, domain.Server{Host: "192.0.2.40"})
	job, err := svc.Start(repositories.ScopeAll(), server.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	<-gestartet
	aborted, err := env.Jobs.Abort(repositories.ScopeAll(), job.ID, "admin")
	if err != nil || aborted.Status != domain.JobStatusAborted {
		t.Fatalf("abbrechen: %+v / %v", aborted, err)
	}
	select {
	case <-beendet:
	case <-time.After(2 * time.Second):
		t.Fatal("der Verbindungsversuch lief nach dem Abbruch weiter")
	}
}
