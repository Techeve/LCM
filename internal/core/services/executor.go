package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/registry"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// commandForRule liefert das Shell-Kommando eines Rule-Typs, das auf den
// Zielservern ausgeführt wird. Serverlose Systemtypen (backup/cleanup)
// haben keins und werden separat behandelt.
func commandForRule(r *domain.Rule) string {
	switch r.Type {
	case domain.RuleTypeScript:
		return r.Command
	case domain.RuleTypeHealth:
		return "echo lcm-health-ok"
	case domain.RuleTypeSync:
		return "" // Sync läuft über den ProvisioningService, nicht als reines Kommando
	default:
		// Paket-Update-Typen (update/packages/security) laufen über den
		// dedizierten apt-Pfad (runAptRule), nicht hier.
		return ""
	}
}

// Executor führt Rules und Ad-hoc-Kommandos auf Servern aus. Jede
// Ausführung wird als Job protokolliert (mit exaktem Konsolen-Output);
// der Concurrency-Lock des JobService verhindert überlappende Jobs pro
// Server.
type Executor struct {
	servers       *repositories.ServerRepository
	groups        *repositories.GroupRepository
	profiles      *repositories.PrivilegeProfileRepository
	jobs          *JobService
	audit         *AuditService
	provisioning  *ProvisioningService
	backups       *BackupService
	settings      *repositories.SettingsRepository
	connect       func(*domain.Server) (sshx.Conn, error)
	recorder      *SSHRecorder
	customActions *repositories.CustomActionRepository
	scanner       VulnScanner
	registry      registry.Checker
	// apps ist der Anwendungskatalog - nur für den zentralen Anwendungs-Check.
	apps       *AppService
	alerts     *AlertService
	advisories *AdvisoryService
	// ipAllowlistExpand löst benannte IP-Allowlists (IDs) in Quell-CIDRs auf -
	// für die Quell-Einschränkung in Firewall-Grundsatz-Regeln. Optional.
	ipAllowlistExpand func([]uint) ([]string, error)
	// dsmRefresh erhebt den Zustand eines Synology-DSM-Geräts über die
	// DSM-Web-API neu (ServerService.refreshDSM). Als Closure, weil der
	// Executor den ServerService nicht kennt - dasselbe Muster wie connect.
	dsmRefresh func(*domain.Server) (string, error)
	// slots ist die prozessweite Schranke für Server-Läufe (siehe
	// GlobalServerSlots). Sie gehört an den Executor und nicht an den
	// einzelnen Regel-Lauf, weil genau das der Punkt ist: Sie muss über
	// mehrere gleichzeitig gestartete Zeitpläne hinweg gelten.
	slots chan struct{}
}

// WithIPAllowlists verdrahtet die Auflösung benannter IP-Allowlists für die
// Firewall-Grundsatz-Regeln (Enforce-Pfad).
func (e *Executor) WithIPAllowlists(expand func([]uint) ([]string, error)) *Executor {
	e.ipAllowlistExpand = expand
	return e
}

// WithDSMRefresh verdrahtet die Zustandserhebung für Synology-DSM-Geräte
// (Web-API statt SSH). Optional - fehlt sie, melden DSM-Regeln das klar,
// statt still zu scheitern.
func (e *Executor) WithDSMRefresh(fn func(*domain.Server) (string, error)) *Executor {
	e.dsmRefresh = fn
	return e
}

// WithRecorder verdrahtet die SSH-Protokollierung.
func (e *Executor) WithRecorder(rec *SSHRecorder) *Executor {
	e.recorder = rec
	return e
}

// WithScanner verdrahtet den CVE-Scanner (Trivy). Optional - fehlt er, wird
// der CVE-Scan sauber übersprungen (graceful degrade).
func (e *Executor) WithScanner(scanner VulnScanner) *Executor {
	e.scanner = scanner
	return e
}

// WithCustomActions verdrahtet die benutzerdefinierten Aktionen (für den
// Rule-Typ "custom").
func (e *Executor) WithCustomActions(repo *repositories.CustomActionRepository) *Executor {
	e.customActions = repo
	return e
}

// WithAlerts verdrahtet den Alert-Service für die periodische Auswertung der
// Monitoring-/Trigger-Kriterien. Optional - fehlt er, entfällt der Alarm-Check.
func (e *Executor) WithAlerts(alerts *AlertService) *Executor {
	e.alerts = alerts
	return e
}

// WithAdvisories verdrahtet die Fruehwarnung (OSV-Poller). Optional - ohne
// sie faellt der Poll-Takt ersatzlos aus, alles andere laeuft weiter.
func (e *Executor) WithAdvisories(svc *AdvisoryService) *Executor {
	e.advisories = svc
	return e
}

// HasAdvisories meldet, ob die Fruehwarnung verdrahtet ist (steuert die
// Registrierung des Poll-Takts im Scheduler).
func (e *Executor) HasAdvisories() bool { return e.advisories != nil }

// HasAlerts meldet, ob der Alarm-Check verdrahtet ist (steuert die
// Registrierung des Alarm-Schedules).
func (e *Executor) HasAlerts() bool { return e.alerts != nil }

func NewExecutor(
	servers *repositories.ServerRepository,
	groups *repositories.GroupRepository,
	jobs *JobService,
	audit *AuditService,
	provisioning *ProvisioningService,
	backups *BackupService,
	settings *repositories.SettingsRepository,
	connect func(*domain.Server) (sshx.Conn, error),
) *Executor {
	return &Executor{
		servers: servers, groups: groups, jobs: jobs, audit: audit,
		provisioning: provisioning, backups: backups, settings: settings,
		connect: connect,
		slots:   make(chan struct{}, GlobalServerSlots),
	}
}

// RunSchedule führt alle aktiven Rules eines Schedules nacheinander aus.
func (e *Executor) RunSchedule(schedule *domain.Schedule, triggeredBy string) {
	for i := range schedule.Rules {
		rule := schedule.Rules[i]
		if !rule.Enabled {
			continue
		}
		e.RunRule(&rule, triggeredBy)
	}
}

// ruleParallelism begrenzt, wie viele Server einer Rule GLEICHZEITIG
// abgearbeitet werden. Parallelität verkürzt Gruppen-Läufe erheblich
// (20 Server × 3 Min laufen sonst 1 h seriell); die Grenze schützt den
// LCM-Host und das Netz vor einem Verbindungs-Burst. Der Job-Lock pro
// Server bleibt davon unberührt.
const ruleParallelism = 4

// GlobalServerSlots begrenzt dasselbe noch einmal über ALLE Regel-Läufe
// hinweg. ruleParallelism allein reicht nicht: Fallen mehrere Zeitpläne auf
// dieselbe Minute - beim mitgelieferten Stand tun das der Viertelstunden-
// Health-Check und der nächtliche System-Sync -, dann gilt die Grenze je Lauf
// und die Last addiert sich.
//
// Auf dem LCM-Host von Techeve endete das jede Nacht damit, dass der Prozess
// unter Speicherdruck minutenlang nicht mehr vorankam und systemd ihn
// abräumte. Die Schranke staut die Läufe stattdessen auf: Ein nächtlicher
// Durchgang dauert etwas länger, dafür bleibt der Dienst ansprechbar.
const GlobalServerSlots = 8

// RunRule führt eine Rule aus. Serverbezogene Rules laufen auf ALLEN
// Servern der Gruppe (jeder Server als eigener Job mit Concurrency-Lock,
// begrenzt parallel); serverlose System-Rules (Backup, Cleanup) laufen
// einmalig. Kehrt erst zurück, wenn alle Server abgearbeitet sind - die
// Rules eines Schedules bleiben damit untereinander strikt sequenziell.
func (e *Executor) RunRule(rule *domain.Rule, triggeredBy string) {
	// Der Docker-Check ist ein zentraler Lauf auf dem LCM-Host (Registry-
	// Update-Check + Trivy-Image-Scan über ALLE erfassten Images) - er
	// läuft einmal pro Schedule-Lauf, nicht pro Server.
	if rule.Type == domain.RuleTypeDockerCheck {
		e.RunDockerCheck(triggeredBy)
		return
	}
	// Ebenso der Anwendungs-Check: Er fragt die Quellen der Katalogeinträge ab
	// und rührt die Server nicht an.
	if rule.Type == domain.RuleTypeAppCheck {
		e.RunAppCheck(triggeredBy)
		return
	}
	servers, err := e.serversForRule(rule)
	if err != nil {
		slog.Error("rule execution: group servers not loadable", "rule", rule.ID, "error", err)
		return
	}
	sem := make(chan struct{}, ruleParallelism)
	var wg sync.WaitGroup
	for i := range servers {
		server := &servers[i]
		wg.Add(1)
		sem <- struct{}{}
		e.takeSlot()
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer e.freeSlot()
			// Panic-Schutz INNEN (nicht über safego.Go): wg.Done und die
			// Semaphore-Freigabe müssen in jedem Fall laufen, sonst
			// blockierte wg.Wait() den Gruppen-Lauf für immer.
			defer safego.Recover("rule-server:"+server.Name, nil)
			e.runOnServer(server, rule, triggeredBy)
		}()
	}
	wg.Wait()
}

// takeSlot/freeSlot belegen einen Platz der prozessweiten Schranke. Ein
// Executor ohne slots (Tests, die ihn direkt bauen) läuft ungebremst weiter -
// die Schranke ist eine Betriebsgrenze, keine fachliche Bedingung.
func (e *Executor) takeSlot() {
	if e.slots != nil {
		e.slots <- struct{}{}
	}
}

func (e *Executor) freeSlot() {
	if e.slots != nil {
		<-e.slots
	}
}

// serversForRule liefert die Zielserver einer Rule: Rules der System-Gruppe
// gelten für ALLE Server (Basis-Schedules wie Health-Check und Sync), alle
// anderen für die Mitglieds-Server ihrer Gruppe.
func (e *Executor) serversForRule(rule *domain.Rule) ([]domain.Server, error) {
	group, err := e.groups.GroupByID(rule.GroupID)
	if err != nil {
		return nil, err
	}
	if group.IsSystem {
		return e.servers.FindAllUnscoped()
	}
	return e.groups.ServersOfGroup(rule.GroupID)
}

// runOnServer führt eine Rule auf einem einzelnen Server aus.
func (e *Executor) runOnServer(server *domain.Server, rule *domain.Rule, triggeredBy string) {
	ruleID := rule.ID
	job, err := e.jobs.Start(&server.ID, &ruleID, rule.Type, rule.Name+" @ "+server.Name, triggeredBy)
	if err != nil {
		if errors.Is(err, ErrServerBusy) {
			slog.Info("job blocked (server busy)", "server", server.Name, "rule", rule.Name)
			return
		}
		slog.Error("job start failed", "server", server.Name, "error", err)
		return
	}

	// Demo-Server werden nie kontaktiert - die Simulation liefert plausiblen
	// Output und zieht den Datenbestand nach (siehe demo_sim.go).
	if server.IsDemo {
		e.jobs.Complete(job, demoRuleOutput(e.servers, server, rule), ptrInt(0), nil)
		return
	}

	// Eingeschränkter Service-User: Aktionstypen außerhalb der Whitelist
	// (script/custom/reboot) sauber überspringen, statt mit einem
	// kryptischen sudo-Fehler zu enden - so bleibt ein gemischter Schedule grün.
	if server.RestrictedSudo && !restrictedAllowsRule(rule.Type) {
		e.jobs.Complete(job, "im eingeschränkten Modus übersprungen - der Management-Benutzer dieses Servers hat keine Rechte für den Aktionstyp „"+rule.Type+"“", ptrInt(0), nil)
		return
	}

	// Synology DSM: kein SSH, keine Shell - Regeln laufen hier über die
	// DSM-Web-API oder gar nicht. Health-Check und System-Sync erheben den
	// Gerätezustand neu (das ist auf einem API-Gerät genau die Entsprechung
	// eines Health-Pings), alles Shell-Gestützte wird benannt übersprungen,
	// damit ein gemischter Schedule grün bleibt statt an einem Gerätetyp zu
	// scheitern.
	if server.IsSynologyDSM() {
		e.runDSMRule(job, server, rule)
		return
	}

	// Sync-Rules laufen über den ProvisioningService (User-Zertifikate +
	// Hardware-Refresh), nicht als reines Kommando.
	if rule.Type == domain.RuleTypeSync {
		out, runErr := e.provisioning.SyncServer(server, SessionContext{
			ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
			Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
		})
		e.finishWithHealth(job, server, out, runErr)
		return
	}

	// Firewall-Rules aktivieren ufw auf allen Servern der Gruppe mit den in
	// Command hinterlegten zusätzlichen TCP-Ports.
	if rule.Type == domain.RuleTypeFirewall {
		e.runFirewallRule(job, server, rule, triggeredBy)
		return
	}

	// Paket-Update-Rules (update/packages/security) laufen als root über die
	// erkannte Paketverwaltung (apt/dnf/zypper) und frischen danach den
	// Paketbestand auf.
	if script, ok := scriptForRule(server.PackageManager, rule.Type, rule.Command); ok {
		e.runAptRule(job, server, rule, script, triggeredBy)
		return
	}

	// Custom-Aktionen führen die hinterlegte Command-Liste sequenziell aus.
	if rule.Type == domain.RuleTypeCustom {
		e.runCustomRule(job, server, rule, triggeredBy)
		return
	}

	// Docker-Prune räumt ungenutzte Images auf und liest das Inventar neu ein.
	if rule.Type == domain.RuleTypeDockerPrune {
		e.runDockerPruneRule(job, server, rule, triggeredBy)
		return
	}

	// Docker-Update-Unused zieht neue Versionen ungenutzter Images.
	if rule.Type == domain.RuleTypeDockerUpdateUnused {
		e.runDockerUpdateUnusedRule(job, server, rule, triggeredBy)
		return
	}

	// APT-Proxy bindet den Server an den zentralen APT-Cache an.
	if rule.Type == domain.RuleTypeAptProxy {
		e.runAptProxyRule(job, server, rule, triggeredBy)
		return
	}

	// Neustart des Servers - planmäßig oder nur bei Bedarf.
	if rule.Type == domain.RuleTypeReboot || rule.Type == domain.RuleTypeRebootIfNeeded {
		e.runRebootRule(job, server, rule, triggeredBy)
		return
	}

	// DNS-Verfügbarkeitstest (rein lesend).
	if rule.Type == domain.RuleTypeDNSTest {
		e.runDNSTestRule(job, server, rule, triggeredBy)
		return
	}

	// Deep Scan (Kernel-Reboot-Lücke, Kernel-CVEs, Härtungs-Audit; rein lesend).
	if rule.Type == domain.RuleTypeDeepScan {
		e.runDeepScanRule(job, server, rule, triggeredBy)
		return
	}

	cmd := commandForRule(rule)
	if cmd == "" {
		e.jobs.Complete(job, "kein kommando für rule-typ "+rule.Type, ptrInt(0), nil)
		return
	}

	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	// Health-Ping-Sessions bekommen einen stabilen Zweck ("health-check"),
	// damit sie im Protokoll-Tab zuverlässig ausgeblendet werden können -
	// unabhängig vom (frei umbenennbaren) Rule-Namen.
	purpose := "rule:" + rule.Name
	if rule.Type == domain.RuleTypeHealth {
		purpose = "health-check"
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: purpose, Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	output, code, runErr := conn.Run(cmd)
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("kommando endete mit exit-code %d", code)
	}

	// Beim Health-Ping werden zusätzlich alle Grundsatz-Regeln (Enforce)
	// des Servers geprüft und bei Abweichung durchgesetzt - auf derselben
	// Verbindung, protokolliert im selben Job.
	if rule.Type == domain.RuleTypeHealth && runErr == nil {
		report, enfErr := e.enforceOnConn(conn, server)
		if report != "" {
			output = strings.TrimSpace(output) + "\n\n" + report
		}
		// Festplattenbelegung stündlich (gedrosselt) messen und im
		// Speicher-Verlauf fortschreiben - auf derselben Verbindung.
		e.sampleStorage(conn, server)
		// Liegengebliebene Benutzer-Abgleiche nachholen: Der Server ist
		// gerade erreichbar, die Verbindung steht ohnehin. Ohne das bliebe
		// ein entzogener Zugang bis zum nächsten geplanten Sync nutzbar.
		if report, syncErr := e.provisioning.DrainOnConn(conn, server, triggeredBy); report != "" || syncErr != nil {
			if report != "" {
				output = strings.TrimSpace(output) + "\n\n" + strings.TrimSpace(report)
			}
			if enfErr == nil {
				enfErr = syncErr
			}
		}
		runErr = enfErr
	}

	e.finishWithHealth(job, server, output, runErr)
}

// storageSampleInterval ist der Mindestabstand zwischen zwei Speicher-Messungen
// je Server. Der Health-Check läuft häufiger (alle 15 Min); ein Snapshot wird
// aber nur ~stündlich genommen und zum Tagesdurchschnitt verdichtet.
const storageSampleInterval = time.Hour

// sampleStorage misst die Festplattenbelegung des Servers über die bereits
// offene Health-Check-Verbindung - höchstens einmal pro Stunde (Drosselung
// über den Zeitpunkt der letzten Messung). Die Messung aktualisiert den
// Live-Wert am Server und fließt in den Tagesdurchschnitt des Speicher-Verlaufs
// ein. Fehler sind unkritisch (best effort) und brechen den Health-Check nicht.
func (e *Executor) sampleStorage(conn sshx.Conn, server *domain.Server) {
	last, err := e.servers.LatestStorageSampleAt(server.ID)
	if err == nil && !last.IsZero() && time.Since(last) < storageSampleInterval {
		return // innerhalb der letzten Stunde bereits gemessen
	}
	out, code, runErr := conn.Run(diskUsageCmd)
	if runErr != nil || code != 0 {
		return
	}
	total, used := parseTwoInts(out)
	if total <= 0 {
		return
	}
	now := time.Now()
	_ = e.servers.UpdateFields(server.ID, map[string]any{"disk_total_mb": total, "disk_used_mb": used})
	_ = e.servers.RecordStorageSample(server.ID, now.Format("2006-01-02"), total, used, now)
}

// recordEnforcement hält einen tatsächlichen Eingriff einer Grundsatz-Regel
// fest: als INFO im Log und als Audit-Eintrag.
//
// Bis dahin war eine bestandsweite Firewall-Änderung nur an einer
// DEBUG-Zeile und im Output des Health-Check-Jobs erkennbar - für eine
// Aktion, die auf allen Servern einer Gruppe Dienstports schließen kann, ist
// das zu wenig (R2-082 Punkt 4).
func (e *Executor) recordEnforcement(server *domain.Server, rule *domain.Rule, detail string) {
	slog.Info("enforce rule applied", "rule", rule.Name, "server", server.Name, "detail", detail)
	if e.audit != nil {
		e.audit.Log("scheduler", "rule.enforce", "server", server.ID,
			fmt.Sprintf("%s: %s (Grundsatz-Regel „%s\")", server.Name, detail, rule.Name))
	}
}

// enforceOnConn prüft die Grundsatz-Regeln (Enforce) eines Servers auf einer
// bereits offenen Verbindung. Geprüft wird zuerst der Ist-Zustand; NUR bei
// Abweichung wird die Regel durchgesetzt. Liefert einen Report für den
// Job-Output und einen Fehler, wenn eine Durchsetzung fehlschlug.
// WithProfiles verdrahtet die Berechtigungsprofile - gebraucht von der
// Grundsatz-Regel „Rechte-Soll", die die gesetzten Verzeichnisrechte prüft.
func (e *Executor) WithProfiles(repo *repositories.PrivilegeProfileRepository) *Executor {
	e.profiles = repo
	return e
}

func (e *Executor) enforceOnConn(conn sshx.Conn, server *domain.Server) (string, error) {
	rules, err := e.groups.FindEnforceRulesForServer(server.ID)
	if err != nil {
		return "", err
	}
	if len(rules) == 0 {
		return "", nil
	}
	// Regeln desselben Typs aus mehreren Gruppen schließen einander aus -
	// es greift die Gruppe mit dem stärksten Vorrang, die übrigen werden
	// benannt statt ausgeführt.
	active, superseded := resolveEnforceRules(rules)
	lines := make([]string, 0, len(rules)+1)
	lines = append(lines, "Grundsatz-Regeln:")
	var firstErr error
	for i := range active {
		line, err := e.enforceRule(conn, server, &active[i])
		lines = append(lines, line)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, s := range superseded {
		lines = append(lines, supersededNote(s))
	}
	return strings.Join(lines, "\n"), firstErr
}

// enforceRule setzt eine einzelne Grundsatz-Regel um (prüfen → nur bei
// Abweichung anwenden).
func (e *Executor) enforceRule(conn sshx.Conn, server *domain.Server, rule *domain.Rule) (string, error) {
	// Eingeschränkter Modus: script- und apt-proxy-Grundsatzregeln brauchen
	// Root-Shell-/Dateisystemzugriff - sauber überspringen statt fehlzuschlagen.
	if server.RestrictedSudo && (rule.Type == domain.RuleTypeScript || rule.Type == domain.RuleTypeAptProxy) {
		return fmt.Sprintf("  [%s] im eingeschränkten modus übersprungen", rule.Name), nil
	}
	switch rule.Type {
	case domain.RuleTypeFirewall:
		// Proxmox-Systeme: die Firewall-Grundsatz-Regel greift dort nicht
		// (Proxmox-eigene Firewall) - dokumentierter Skip statt Durchsetzung.
		if server.IsProxmox() {
			return fmt.Sprintf("  [%s] proxmox-system - firewall-regel übersprungen", rule.Name), nil
		}
		if server.IsRouterOS() {
			return fmt.Sprintf("  [%s] routeros - firewall-regel übersprungen (keine LCM-Firewall-Verwaltung)", rule.Name), nil
		}
		rules, err := parseFirewallRuleSpec(rule.Command)
		if err != nil {
			// Konfigurationsfehler der Regel: melden, aber den Health-Lauf
			// nicht als Serverfehler werten.
			return fmt.Sprintf("  [%s] ungültige firewall-regel: %v", rule.Name, err), nil
		}
		// Netz für den Altbestand: Regeln, die vor der Eingabeprüfung ohne
		// Soll-Zustand angelegt wurden, dürfen nicht weiter still „alles außer
		// SSH" durchsetzen (R2-082). Nur ein ausdrückliches [] bedeutet das.
		if len(rules) == 0 && strings.TrimSpace(rule.Command) != "[]" {
			slog.Warn("firewall enforce rule without target state - not enforced",
				"rule", rule.Name, "server", server.Name)
			return fmt.Sprintf("  [%s] NICHT durchgesetzt - die Regel trägt keinen erkennbaren Soll-Zustand. "+
				"Freizugebende Ports in der Regel hinterlegen; für „nur SSH offen\" ausdrücklich [] eintragen.", rule.Name), nil
		}
		backend := firewallBackendFor(server)
		if server.RestrictedSudo && backend != domain.FirewallToolUfw {
			return fmt.Sprintf("  [%s] firewall-backend %s braucht volle sudo-rechte - im eingeschränkten modus übersprungen", rule.Name, backend), nil
		}
		be := firewallBackendByName(backend)
		// Benannte Allowlists auflösen (angewendet werden die Quellen, der
		// Hash erfasst sie → Änderungen an Listen lösen ein Neu-Anwenden aus).
		ssh, applied, fwWarnings, err := expandSSHAndRules(serverSSHRule(server), rules, e.ipAllowlistExpand)
		if err != nil {
			return fmt.Sprintf("  [%s] allowlists auflösen fehlgeschlagen: %v", rule.Name, err), nil
		}
		// Portliste und Meldung geben den TATSÄCHLICH angewandten Stand
		// wieder - übersprungene Regeln zählen nicht als offen (R2-071).
		portsCSV := firewallRulesPortsCSV(applied)
		want := strings.TrimSuffix(fmt.Sprintf("%d,%s", server.SSHPort, portsCSV), ",")
		warnSuffix := ""
		if len(fwWarnings) > 0 {
			warnSuffix = "\n  " + strings.Join(fwWarnings, "\n  ")
		}
		status, code, err := conn.Run(privRun(server, be.statusCmd))
		if err == nil && code == 0 && be.inSync(status, ssh, applied) {
			_ = e.servers.UpdateFields(server.ID, map[string]any{
				"firewall_active": true, "firewall_tool": backend,
				"firewall_rules": firewallRulesJSON(rules), "firewall_allowed_ports": portsCSV,
			})
			return fmt.Sprintf("  [%s] firewall ok - regel umgesetzt (ports: %s)%s", rule.Name, want, warnSuffix), nil
		}
		_, usedBackend, err := applyFirewallRules(conn, server, ssh, applied, true)
		if err != nil {
			return fmt.Sprintf("  [%s] ABWEICHUNG - durchsetzung fehlgeschlagen: %v", rule.Name, err), err
		}
		_ = e.servers.UpdateFields(server.ID, map[string]any{
			"firewall_active": true, // durch applyFirewallRules verifiziert
			"firewall_tool":   usedBackend,
			"firewall_rules":  firewallRulesJSON(rules), "firewall_allowed_ports": portsCSV,
		})
		// Eine Grundsatz-Regel, die WIRKLICH eingreift, ändert den Zustand
		// eines Produktivsystems - das gehört ins Log und ins Audit, nicht nur
		// in den Output eines fremden Jobs (R2-082 Punkt 4).
		e.recordEnforcement(server, rule, fmt.Sprintf("Firewall (%s) neu angewendet - Ports: %s", usedBackend, want))
		return fmt.Sprintf("  [%s] abweichung erkannt - firewall (%s) neu angewendet (ports: %s)%s", rule.Name, usedBackend, want, warnSuffix), nil

	case domain.RuleTypeScript:
		// Ein Shell-Kommando hat keinen Soll-Zustand - es lief hier
		// BEDINGUNGSLOS bei jedem Health-Check: auf der System-Gruppe 1344
		// Ausführungen am Tag, keine davon in Job-Historie, Audit oder Log
		// oberhalb von DEBUG (R2-087). Neue script-Grundsatzregeln lehnt
		// DefineRule inzwischen ab; der Altbestand wird hier NICHT mehr
		// ausgeführt, sondern sichtbar gemeldet - ein unbemerkt weiterlaufendes
		// Root-Kommando ist das größere Risiko als eine ausgesetzte Regel.
		slog.Warn("script enforce rule skipped - convert to a schedule",
			"rule", rule.Name, "server", server.Name)
		return fmt.Sprintf("  [%s] NICHT ausgeführt - script-Grundsatzregeln werden nicht mehr unterstützt "+
			"(ein Kommando hat keinen Soll-Zustand und lief hier unprotokolliert alle 15 Minuten). "+
			"Die Regel in einen Zeitplan verschieben; dort erscheint jede Ausführung als eigener Job.", rule.Name), nil

	case domain.RuleTypePermSync:
		return e.enforcePermSync(conn, server, rule)

	case domain.RuleTypeACLSetup:
		return e.enforceACLSetup(conn, server, rule)

	case domain.RuleTypeAptProxy:
		cacheURL, err := e.aptCacheURLFromSettings()
		if err != nil {
			return fmt.Sprintf("  [%s] übersprungen: %v", rule.Name, err), nil
		}
		// Drift-Check: Drop-in vorhanden UND zeigt auf die aktuelle Cache-URL?
		current, _, err := conn.Run(privRun(server, "cat "+aptProxyDropin+" 2>/dev/null || true"))
		if err == nil && strings.Contains(current, `"`+cacheURL+`"`) {
			_ = e.servers.UpdateFields(server.ID, map[string]any{"apt_proxy_active": true})
			return fmt.Sprintf("  [%s] apt-cache ok - regel umgesetzt (%s)", rule.Name, cacheURL), nil
		}
		out, code, err := conn.Run(privRun(server, aptProxyEnableScript(cacheURL)))
		if err == nil && code != 0 {
			err = fmt.Errorf("exit-code %d: %s", code, summarize(out))
		}
		if err != nil {
			return fmt.Sprintf("  [%s] ABWEICHUNG - durchsetzung fehlgeschlagen: %v", rule.Name, err), err
		}
		_ = e.servers.UpdateFields(server.ID, map[string]any{"apt_proxy_active": true})
		e.recordEnforcement(server, rule, "APT-Cache neu angebunden: "+cacheURL)
		return fmt.Sprintf("  [%s] abweichung erkannt - apt-cache neu angebunden (%s)", rule.Name, cacheURL), nil
	}
	return fmt.Sprintf("  [%s] typ %q wird als grundsatz-regel nicht unterstützt", rule.Name, rule.Type), nil
}

// aptCacheURLFromSettings liefert die konfigurierte APT-Cache-URL oder einen
// Fehler, wenn keine hinterlegt ist.
func (e *Executor) aptCacheURLFromSettings() (string, error) {
	settings, err := e.settings.Get()
	if err != nil {
		return "", err
	}
	if settings.AptCacheURL == "" {
		return "", ErrNoAptCacheURL
	}
	return settings.AptCacheURL, nil
}

// runAptProxyRule bindet einen Server per Gruppen-Regel an den zentralen
// APT-Cache an - gleiche Skriptlogik wie die Server-Aktion (Drop-in schreiben,
// per apt-Update prüfen, bei Fehlschlag zurückrollen).
func (e *Executor) runAptProxyRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
	cacheURL, err := e.aptCacheURLFromSettings()
	if err != nil {
		e.jobs.Complete(job, "", nil, err)
		return
	}
	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	output, code, runErr := conn.Run(privRun(server, aptProxyEnableScript(cacheURL)))
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("apt-cache-anbindung endete mit exit-code %d", code)
	}
	_ = e.servers.UpdateFields(server.ID, map[string]any{"apt_proxy_active": runErr == nil})
	e.finishWithHealth(job, server, output, runErr)
}

// runDNSTestRule prüft per Gruppen-Regel auf jedem Server, ob die gepflegten
// DNS-Test-Domains aufgelöst werden können, und speichert das dreistufige
// Ergebnis (full/partial/none) am Server. Rein lesend - gleiche Skriptlogik wie
// die Server-Aktion DNSTest.
func (e *Executor) runDNSTestRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
	// RouterOS hat keine POSIX-Shell - getent/nslookup würden in der
	// RouterOS-CLI nur Syntaxfehler produzieren.
	if server.IsRouterOS() {
		e.jobs.Complete(job, "routeros: dns-test nicht anwendbar - übersprungen", ptrInt(0), nil)
		return
	}
	settings, err := e.settings.Get()
	if err != nil {
		e.jobs.Complete(job, "", nil, err)
		return
	}
	domains := sanitizeDNSDomains(settings.DNSTestDomainList())
	if len(domains) == 0 {
		e.jobs.Complete(job, "keine DNS-Test-Domains gepflegt (Einstellungen → DNS)", ptrInt(0), nil)
		return
	}
	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	output, _, runErr := conn.Run(dnsTestScript(domains))
	if runErr == nil {
		status, detail := parseDNSTest(output)
		fields := map[string]any{
			"dns_test_status": status,
			"dns_test_at":     time.Now(),
			"dns_test_detail": detail,
		}
		if cur, _, curErr := conn.Run(dnsCurrentScript()); curErr == nil {
			fields["dns_current"] = parseDNSList(cur)
		}
		_ = e.servers.UpdateFields(server.ID, fields)
	}
	e.finishWithHealth(job, server, output, runErr)
}

// runFirewallRule wendet eine Firewall-Gruppenregel auf einen Server an:
// bestimmt das Backend (ufw/firewalld/nftables, erkanntes Werkzeug gewinnt),
// installiert es bei Bedarf und gibt SSH + die in rule.Command hinterlegten
// Regeln frei. Die Konfiguration wird am Server persistiert (Anzeige).
func (e *Executor) runFirewallRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
	// Proxmox bringt seine eigene Firewall mit - sie wird dort nie angefasst.
	if server.IsProxmox() {
		e.jobs.Complete(job, "proxmox-system: firewall-regel übersprungen (proxmox verwaltet die firewall selbst)", ptrInt(0), nil)
		return
	}
	if server.IsRouterOS() {
		e.jobs.Complete(job, "routeros: firewall-regel übersprungen (keine LCM-Firewall-Verwaltung möglich)", ptrInt(0), nil)
		return
	}
	rules, err := parseFirewallRuleSpec(rule.Command)
	if err != nil {
		e.jobs.Complete(job, "", nil, fmt.Errorf("ungültige firewall-regel: %w", err))
		return
	}
	backend := firewallBackendFor(server)
	if server.RestrictedSudo && backend != domain.FirewallToolUfw {
		e.jobs.Complete(job, fmt.Sprintf("firewall-backend %s braucht volle sudo-rechte - im eingeschränkten modus übersprungen", backend), ptrInt(0), nil)
		return
	}
	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	ssh, applied, warnings, err := expandSSHAndRules(serverSSHRule(server), rules, e.ipAllowlistExpand)
	if err != nil {
		e.jobs.Complete(job, "", nil, fmt.Errorf("allowlists auflösen: %w", err))
		return
	}
	output, usedBackend, runErr := applyFirewallRules(conn, server, ssh, applied, true)
	if len(warnings) > 0 {
		output += "\n\n" + strings.Join(warnings, "\n")
	}
	if runErr == nil {
		_ = e.servers.UpdateFields(server.ID, map[string]any{
			"firewall_active": true, // durch applyFirewallRules verifiziert
			"firewall_tool":   usedBackend,
			// Ehrlicher Portbestand: nur tatsächlich angewandte Regeln (R2-071).
			"firewall_rules": firewallRulesJSON(rules), "firewall_allowed_ports": firewallRulesPortsCSV(applied),
		})
	}
	e.finishWithHealth(job, server, output, runErr)
}

// runAptRule führt eine Paket-Update-Rule als root aus und liest danach
// den Paketbestand des Servers neu ein.
func (e *Executor) runAptRule(job *domain.Job, server *domain.Server, rule *domain.Rule, script, triggeredBy string) {
	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	output, code, runErr := conn.Run(privRun(server, script))
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("paket-update endete mit exit-code %d", code)
	}
	if runErr == nil {
		rescanPackagesInto(e.servers, conn, server)
		// Nach dem Update die CVE-Bewertung auffrischen, damit keine veralteten
		// Sicherheits-Labels an bereits aktualisierten Paketen hängen bleiben.
		output = strings.TrimSpace(output) + rescanCVEsAfterPackageUpdate(e.scanner, e.servers, e.cveRescanEnabled(), server)
	}
	e.finishWithHealth(job, server, output, runErr)
}

// cveRescanEnabled meldet, ob nach einem Paket-Update die CVE-Bewertung
// automatisch aufgefrischt werden soll (globale Einstellung; Default an).
func (e *Executor) cveRescanEnabled() bool {
	st, err := e.settings.Get()
	return err != nil || st.CVEScanEnabled
}

// runDockerPruneRule entfernt ungenutzte Docker-Images auf dem Server
// (docker image prune -af) und liest danach das Docker-Inventar neu ein.
// Auf Servern ohne Docker ist der Lauf ein sauberer No-Op.
func (e *Executor) runDockerPruneRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
	if !server.HasDocker {
		e.jobs.Complete(job, "docker nicht vorhanden - nichts aufzuräumen", ptrInt(0), nil)
		return
	}
	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	output, code, runErr := conn.Run(privRun(server, dockerPruneScript()))
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("docker-prune endete mit exit-code %d", code)
	}
	if runErr == nil {
		rescanDockerInto(e.servers, conn, server)
	}
	e.finishWithHealth(job, server, output, runErr)
}

// runDockerUpdateUnusedRule zieht neue Versionen aller UNGENUTZTEN Images
// (kein Container referenziert sie), für die der zentrale Registry-Check ein
// Update gemeldet hat, und liest danach das Docker-Inventar neu ein. Genutzte
// Images bleiben unberührt - deren Update läuft bewusst über Compose/Pull.
// Auf Servern ohne Docker ist der Lauf ein sauberer No-Op. Hinweis: die
// Update-Erkennung stammt aus dem täglichen Docker-Check - frisch gezogene
// Stände werden erst nach dessen nächstem Lauf wieder als aktuell gemeldet.
func (e *Executor) runDockerUpdateUnusedRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
	if !server.HasDocker {
		e.jobs.Complete(job, "docker nicht vorhanden - nichts zu aktualisieren", ptrInt(0), nil)
		return
	}
	// Kein Fehler, sondern eine Entscheidung: Der Server ist bewusst
	// ausgenommen. Ein Regellauf über eine Gruppe darf davon nicht rot
	// werden - sonst erzöge die Ausnahme zum Wegsehen bei echten Fehlern.
	if server.DockerUpdatesDisabled {
		e.jobs.Complete(job, "docker-updates sind für diesen server abgeschaltet - übersprungen", ptrInt(0), nil)
		return
	}
	images, err := e.servers.FindDockerImages(server.ID)
	if err != nil {
		e.jobs.Complete(job, "", nil, fmt.Errorf("docker-inventar laden: %w", err))
		return
	}
	var targets []string
	for i := range images {
		img := &images[i]
		if !img.InUse && img.UpdateAvailable && img.Tag != "" {
			targets = append(targets, img.Ref())
		}
	}
	if len(targets) == 0 {
		e.jobs.Complete(job, "keine ungenutzten images mit verfügbarem update", ptrInt(0), nil)
		return
	}

	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	var log strings.Builder
	for _, ref := range targets {
		out, code, runErr := conn.Run(privRun(server, dockerPullScript(ref)))
		if runErr == nil && code != 0 {
			runErr = fmt.Errorf("docker pull %s endete mit exit-code %d: %s", ref, code, summarize(out))
		}
		if runErr != nil {
			e.finishWithHealth(job, server, log.String(), runErr)
			return
		}
		log.WriteString("aktualisiert: " + ref + "\n")
	}
	rescanDockerInto(e.servers, conn, server)
	e.finishWithHealth(job, server, log.String(), nil)
}

// runCustomRule führt die Command-Liste einer Custom-Aktion der Reihe nach
// auf dem Server aus (eine Verbindung, protokolliert). Bricht beim ersten
// fehlschlagenden Kommando ab und meldet, welches es war.
func (e *Executor) runCustomRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
	if e.customActions == nil {
		e.jobs.Complete(job, "", nil, fmt.Errorf("custom-aktionen nicht verfügbar"))
		return
	}
	actionID, err := strconv.ParseUint(strings.TrimSpace(rule.Command), 10, 64)
	if err != nil {
		e.jobs.Complete(job, "", nil, fmt.Errorf("custom-aktion nicht referenziert (command=%q)", rule.Command))
		return
	}
	action, err := e.customActions.FindByID(uint(actionID))
	if err != nil {
		e.jobs.Complete(job, "", nil, fmt.Errorf("custom-aktion %d nicht gefunden: %w", actionID, err))
		return
	}
	commands := parseActionCommands(action.Commands)
	if len(commands) == 0 {
		e.jobs.Complete(job, "custom-aktion enthält keine kommandos", ptrInt(0), nil)
		return
	}

	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	var log strings.Builder
	fmt.Fprintf(&log, "Custom-Aktion %q - %d Kommando(s)\n", action.Name, len(commands))
	runErr := runActionCommands(conn, server, &log, commands)
	e.finishWithHealth(job, server, log.String(), runErr)
}

// runActionCommands führt eine Kommandoliste der Reihe nach aus und schreibt
// mit, was dabei herauskommt. Beim ersten Fehlschlag ist Schluss - was danach
// käme, setzte auf einem Zustand auf, den es nicht gibt.
//
// Kommandos laufen als root (wie alle LCM-Aktionen) - so funktionieren z.B.
// apt/systemctl ohne manuelles sudo.
func runActionCommands(conn sshx.Conn, server *domain.Server, log *strings.Builder, commands []string) error {
	for i, cmd := range commands {
		fmt.Fprintf(log, "\n$ %s\n", cmd)
		out, code, err := conn.Run(privRun(server, cmd))
		log.WriteString(out)
		if err != nil {
			return fmt.Errorf("kommando %d/%d fehlgeschlagen: %w", i+1, len(commands), err)
		}
		if code != 0 {
			return fmt.Errorf("kommando %d/%d endete mit exit-code %d", i+1, len(commands), code)
		}
	}
	return nil
}

// finishWithHealth schließt den Job ab und aktualisiert den
// Erreichbarkeits-Zustand des Servers (letzter Kontakt / Fehler).
func (e *Executor) finishWithHealth(job *domain.Job, server *domain.Server, output string, runErr error) {
	now := time.Now()
	if runErr == nil {
		_ = e.servers.UpdateFields(server.ID, reachableFields(now))
		e.jobs.Complete(job, output, ptrInt(0), nil)
	} else {
		e.jobs.Complete(job, output, nil, runErr)
	}
}

// markUnreachable setzt einen Server auf nicht erreichbar (=> rote Ampel) und
// zaehlt den Fehlschlag mit. Erst mehrere in Folge machen aus „gerade nicht
// erreicht" ein „offline" (siehe domain.OfflineAfterFailedChecks).
func (e *Executor) markUnreachable(server *domain.Server, cause error) {
	_ = e.servers.UpdateFields(server.ID, unreachableFields(server, cause))
}

// unreachableFields sind die Felder eines fehlgeschlagenen Kontakts. Der
// Zaehler wird per SQL-Ausdruck erhoeht, damit parallele Laeufe sich nicht
// gegenseitig ueberschreiben (Read-Modify-Write waere hier eine Wettlaufstelle).
//
// Jeder Fehlschlag landet auch im LOG - hier, damit ALLE Pfade (Health-Check,
// Refresh, Docker, Reboot) dieselbe Zeile schreiben: zu tagelang scheiternden
// Health-Checks stand vorher nichts in der Logdatei, und ein Host-Key-
// Mismatch (moegliche MitM-Attacke) ist ein sicherheitsrelevantes Ereignis,
// das eine Log-Auswertung/SIEM sehen muss, nicht nur die Job-Historie
// (R2-018).
func unreachableFields(server *domain.Server, cause error) map[string]any {
	if errors.Is(cause, sshx.ErrHostKeyMismatch) {
		slog.Warn("host key mismatch - possible mitm attack",
			"server", server.Name, "host", server.Host, "error", cause)
	} else {
		slog.Warn("server unreachable", "server", server.Name, "host", server.Host, "error", cause)
	}
	return map[string]any{
		"reachable":     false,
		"last_error":    cause.Error(),
		"failed_checks": gorm.Expr("failed_checks + 1"),
	}
}

// reachableFields sind die Felder eines erfolgreichen Kontakts - inklusive
// Zaehler-Rueckstellung: Wer antwortet, ist nicht offline.
func reachableFields(now time.Time) map[string]any {
	return map[string]any{
		"reachable": true, "last_seen_at": now, "last_error": "",
		"failed_checks": 0,
	}
}

// runSystemJob führt einen serverlosen System-Job aus (Backup, Cleanup).
// Diese Schedules hängen nicht an einer Servergruppe, sondern werden vom
// Scheduler direkt aus den globalen Einstellungen abgeleitet.
func (e *Executor) runSystemJob(jobType, triggeredBy, name string, fn func() (string, error)) {
	job, err := e.jobs.Start(nil, nil, jobType, name, triggeredBy)
	if err != nil {
		slog.Error("system job start failed", "job", name, "error", err)
		return
	}
	out, runErr := fn()
	e.jobs.Complete(job, out, ptrInt(0), runErr)
	if runErr != nil {
		// Ein gescheiterter System-Job (Backup!) stand bisher NUR in der
		// Job-Historie - im Log fand sich keine Zeile dazu, und wer nicht
		// gezielt nachsah, erfuhr nichts (R2-028).
		slog.Warn("system job failed", "job", name, "error", runErr)
	}
}

// RunBackup erstellt ein System-Backup (als serverloser Job protokolliert).
func (e *Executor) RunBackup(triggeredBy string) {
	e.runSystemJob(domain.RuleTypeBackup, triggeredBy, "System-Backup", func() (string, error) {
		// Geplantes Backup: Passphrase aus LCM_BACKUP_PASSPHRASE (Parameter leer).
		b, err := e.backups.Create(triggeredBy, "")
		if err != nil {
			return "", err
		}
		return "Backup erstellt: " + b.FileName, nil
	})
}

// RefreshCVEDB zieht die Trivy-Datenbank nach - der stille Gegenpart zum
// Knopf „CVE-Datenbank aktualisieren" (ServerService.UpdateCVEDB): Der
// Scheduler ruft das fest alle 6 Stunden auf, im Takt, in dem der Hersteller
// die Datenbank baut. Erfolg wird nur geloggt - vier Job-Einträge pro Tag
// wären Rauschen. Erst ein Fehlschlag erzeugt einen Job mit der vollen
// Trivy-Ausgabe (Proxy, Rate-Limit, kein Netz), damit die Ursache im
// Protokoll steht statt nur im Log. Ohne Scanner passiert nichts: eine
// Instanz ohne Trivy soll nicht alle 6 Stunden einen Fehler produzieren.
func (e *Executor) RefreshCVEDB(triggeredBy string) {
	if e.scanner == nil || !e.scanner.Available() {
		return
	}
	out, err := e.scanner.UpdateDB(context.Background())
	if err == nil {
		slog.Info("cve database refreshed")
		return
	}
	slog.Warn("cve database refresh failed", "error", err)
	job, jobErr := e.jobs.Start(nil, nil, domain.RuleTypeScript, "CVE-Datenbank aktualisieren", triggeredBy)
	if jobErr != nil {
		slog.Error("cve db refresh: job start failed", "error", jobErr)
		return
	}
	e.jobs.Complete(job, out, nil, err)
}

// RunAdvisoryPoll fragt die Online-Quellen nach Befunden zum installierten
// Paketbestand (Fruehwarnung). Laeuft alle 15 Minuten und ist deshalb - wie
// der Datenbank-Zug - LEISE: Der Normalfall ist „nichts Neues", und 96
// Job-Eintraege pro Tag wuerden die Historie unbrauchbar machen. Erst ein
// Fehlschlag erzeugt einen Job, damit eine dauerhaft unerreichbare Quelle
// nicht unbemerkt bleibt.
func (e *Executor) RunAdvisoryPoll(triggeredBy string) {
	if e.advisories == nil || !e.advisories.Enabled() {
		return
	}
	summary, err := e.advisories.Poll(context.Background())
	if err == nil {
		slog.Info("advisory poll finished", "summary", summary)
		return
	}
	slog.Warn("advisory poll failed", "error", err)
	job, jobErr := e.jobs.Start(nil, nil, domain.RuleTypeScript, "Fruehwarnung: Abfrage", triggeredBy)
	if jobErr != nil {
		slog.Error("advisory poll: job start failed", "error", jobErr)
		return
	}
	e.jobs.Complete(job, "", nil, err)
}

// StartAdvisoryPoll und StartAdvisoryMirror stossen die Laeufe an und geben
// den Job ZURUECK, damit die Oberflaeche auf sein Ergebnis warten kann.
//
// Ohne die Rueckgabe blieb es bei „gestartet": Ob der Lauf etwas gefunden,
// nichts zu tun gehabt oder abgebrochen hat, erfuhr niemand - man druckte
// den Knopf erneut. Genau dasselbe Muster nutzt schon der Knopf fuer die
// CVE-Datenbank.
func (e *Executor) StartAdvisoryPoll(actor string) (*domain.Job, error) {
	return e.startAdvisoryJob(actor, "Fruehwarnung: Abfrage", func() (string, error) {
		return e.advisories.Poll(context.Background())
	})
}

func (e *Executor) StartAdvisoryMirror(actor string) (*domain.Job, error) {
	return e.startAdvisoryJob(actor, "Fruehwarnung: lokale Kopie spiegeln", func() (string, error) {
		return e.advisories.RefreshLocalCopy(context.Background())
	})
}

// startAdvisoryJob legt den Job an, startet die Arbeit im Hintergrund und
// liefert den Job sofort zurueck.
func (e *Executor) startAdvisoryJob(actor, name string, fn func() (string, error)) (*domain.Job, error) {
	if e.advisories == nil {
		return nil, fmt.Errorf("die fruehwarnung ist auf dieser instanz nicht eingerichtet")
	}
	job, err := e.jobs.Start(nil, nil, domain.RuleTypeScript, name, actor)
	if err != nil {
		return nil, err
	}
	safego.GoCleanup("job:advisory", jobPanicCleanup(e.jobs, job), func() {
		out, runErr := fn()
		e.jobs.Complete(job, out, ptrInt(0), runErr)
		if runErr != nil {
			slog.Warn("advisory job failed", "job", name, "error", runErr)
		}
	})
	return job, nil
}

// RunAdvisoryMirror spiegelt die OSV-Datenbank fuer die lokale Kopie.
//
// Anders als Poll und Anreicherung ist dieser Lauf NICHT leise: Er laeuft
// einmal taeglich, dauert Minuten und laedt zig Megabyte - das gehoert
// sichtbar ins Protokoll, samt Umfang. Und scheitert er, faellt die
// Fruehwarnung im lokalen Betrieb komplett aus; das darf nicht nur eine
// Log-Zeile sein.
func (e *Executor) RunAdvisoryMirror(triggeredBy string) {
	if e.advisories == nil || !e.advisories.UsesLocalCopy() {
		return
	}
	e.runSystemJob(domain.RuleTypeScript, triggeredBy, "Fruehwarnung: lokale Kopie spiegeln", func() (string, error) {
		return e.advisories.RefreshLocalCopy(context.Background())
	})
}

// RunAdvisoryEnrich holt das Ausnutzungs-Signal (EUVD) und markiert die
// betroffenen Befunde. Taeglich statt viertelstuendlich: Die Liste aendert
// sich in Tagen, nicht in Minuten, und die Quelle gilt als leistungsschwach.
// Leise wie der Poll-Lauf; nur ein Fehlschlag erzeugt einen Job.
func (e *Executor) RunAdvisoryEnrich(triggeredBy string) {
	if e.advisories == nil {
		return
	}
	summary, err := e.advisories.EnrichExploited(context.Background())
	if err == nil {
		slog.Info("advisory enrichment finished", "summary", summary)
		return
	}
	slog.Warn("advisory enrichment failed", "error", err)
	job, jobErr := e.jobs.Start(nil, nil, domain.RuleTypeScript, "Fruehwarnung: Ausnutzungs-Signal", triggeredBy)
	if jobErr != nil {
		slog.Error("advisory enrichment: job start failed", "error", jobErr)
		return
	}
	e.jobs.Complete(job, "", nil, err)
}

// RunCVEScan prüft den Paketbestand ALLER Server gegen Trivy (serverloser
// System-Job, analog zu Backup/Cleanup). Fehlt Trivy, endet der Job mit einem
// klaren Hinweis statt mit einem Fehler (graceful degrade).
func (e *Executor) RunCVEScan(triggeredBy string) {
	e.runSystemJob(domain.RuleTypeCVEScan, triggeredBy, "CVE-Scan", func() (string, error) {
		if e.scanner == nil || !e.scanner.Available() {
			return "Trivy nicht verfügbar - CVE-Scan übersprungen. Trivy installieren, damit installierte Pakete auf bekannte Sicherheitslücken geprüft werden.", nil
		}
		// Datenbank direkt vor dem Durchlauf ziehen - der nächtliche Lauf
		// soll mit der frischesten Ausgabe arbeiten, nicht mit der vom
		// letzten 6-Stunden-Zug. Ein Fehlschlag ist nicht fatal: dann wird
		// wie bisher mit dem vorhandenen Stand gescannt, und der Grund steht
		// im Job - die Ampel meldet eine überalterte Datenbank ohnehin.
		var dbNote string
		if _, err := e.scanner.UpdateDB(context.Background()); err != nil {
			dbNote = fmt.Sprintf("Datenbank-Aktualisierung vor dem Scan fehlgeschlagen - gescannt wird mit dem vorhandenen Stand: %v\n", err)
		}
		servers, err := e.servers.FindAllUnscoped()
		if err != nil {
			return "", err
		}
		var scanned, failed, crit, high int
		var unreachable []string
		for i := range servers {
			if servers[i].IsDemo {
				continue
			}
			c, h, err := scanServerCVEs(context.Background(), e.scanner, e.servers, &servers[i])
			if errors.Is(err, ErrCVEScanUnreachable) {
				// Kein Fehler, sondern eine bewusste Auslassung (R2-017) -
				// sie gehört benannt ins Protokoll, nicht ins WARN-Log.
				unreachable = append(unreachable, servers[i].Name)
				continue
			}
			if err != nil {
				failed++
				slog.Warn("cve scan failed", "server", servers[i].Name, "error", err)
				continue
			}
			scanned++
			crit += c
			high += h
		}
		// Container-Images erfasst der tägliche Docker-Check (eigene
		// Systemregel, eigener Trivy-Lauf je Image-Digest) - hier zu sagen,
		// woher die Container-Zahlen kommen, verhindert dieselbe
		// Fehldeutung wie beim Einzelscan (R2-086).
		summary := cveScanSummary(scanned, failed, crit, high)
		if len(unreachable) > 0 {
			summary += fmt.Sprintf("\nÜbersprungen, weil nicht erreichbar: %s - deren CVE-Stand und Scan-Zeit bleiben unverändert (R2-017: kein Scan über veraltete Paketdaten).",
				strings.Join(unreachable, ", "))
		}
		return dbNote + summary +
			"\nContainer-Images erfasst der Docker-Check (eigene Systemregel).", nil
	})
}

// RunCVEScanServer prüft einen einzelnen Server (manueller Trigger). Läuft
// asynchron; das Ergebnis erscheint als Job in der Historie.
func (e *Executor) RunCVEScanServer(id uint, triggeredBy string) {
	server, err := e.servers.FindByIDUnscoped(id)
	if err != nil {
		slog.Error("cve scan: server not loadable", "server", id, "error", err)
		return
	}
	job, err := e.jobs.Start(&server.ID, nil, domain.RuleTypeCVEScan, "CVE-Scan @ "+server.Name, triggeredBy)
	if err != nil {
		if !errors.Is(err, ErrServerBusy) {
			slog.Error("cve scan job start failed", "server", server.Name, "error", err)
		}
		return
	}
	if e.scanner == nil || !e.scanner.Available() {
		e.jobs.Complete(job, "Trivy nicht verfügbar - CVE-Scan übersprungen.", ptrInt(0), nil)
		return
	}
	ctx := context.Background()
	crit, high, scanErr := scanServerCVEs(ctx, e.scanner, e.servers, server)
	if scanErr != nil {
		e.jobs.Complete(job, "", nil, scanErr)
		return
	}
	summary := cveScanSummary(1, 0, crit, high)
	// Container-Images gehören dazu. Der Einzelscan erfasste bisher nur
	// Betriebssystempakete und meldete trotzdem eine Gesamtzahl: auf einem
	// Server mit 2475 Container-Funden (22 kritisch, 291 hoch) lautete die
	// Antwort „0 kritische, 0 hohe" - formal auf den eigenen Ausschnitt
	// bezogen richtig, in der Sache eine Entwarnung für etwas, das gar nicht
	// geprüft wurde (R2-086). Wer nach einer Meldung gezielt nachscannt,
	// bekommt jetzt beide Quellen.
	summary += "\n" + e.scanServerDockerImages(ctx, server)
	e.jobs.Complete(job, summary, ptrInt(0), nil)
}

// scanServerDockerImages scannt die Container-Images EINES Servers auf CVEs
// (Teilmenge dessen, was der tägliche Docker-Check flottenweit tut) und
// liefert die Zeile fürs Job-Protokoll.
func (e *Executor) scanServerDockerImages(ctx context.Context, server *domain.Server) string {
	if !server.HasDocker {
		return "Container-Images: keine - auf diesem Server ist kein Docker erfasst."
	}
	images, err := e.servers.FindDockerImages(server.ID)
	if err != nil {
		slog.Warn("docker images not loadable for cve scan", "server", server.Name, "error", err)
		return "Container-Images: Bestand nicht lesbar - nur Betriebssystempakete geprüft."
	}
	demo, err := e.demoServerIDs()
	if err != nil {
		demo = nil
	}
	return e.scanDockerImages(ctx, images, demo)
}

// RunAlertCheck wertet die Monitoring-/Trigger-Kriterien aus und löst bei
// Bedarf Benachrichtigungen aus (serverloser System-Job).
func (e *Executor) RunAlertCheck(triggeredBy string) {
	if e.alerts == nil {
		return
	}
	e.runSystemJob(domain.RuleTypeAlertCheck, triggeredBy, "Alarm-Auswertung", func() (string, error) {
		return e.alerts.Evaluate(triggeredBy)
	})
}

// RunCleanup setzt die Log-Retention um (als serverloser Job).
func (e *Executor) RunCleanup(triggeredBy string) {
	e.runSystemJob(domain.RuleTypeCleanup, triggeredBy, "Log-Bereinigung", func() (string, error) {
		return e.runCleanup()
	})
}

// runCleanup setzt die Log-Retention um (konfigurierbare Frist).
func (e *Executor) runCleanup() (string, error) {
	settings, err := e.settings.Get()
	if err != nil {
		return "", err
	}
	deleted, err := e.jobs.CleanupOlderThan(settings.LogRetentionDays)
	if err != nil {
		return "", err
	}
	// SSH-Protokolle und Alarm-Events nach derselben Frist bereinigen.
	var logsDeleted, alertsDeleted int64
	if settings.LogRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -settings.LogRetentionDays)
		logsDeleted, _ = e.recorder.CleanupOlderThan(cutoff)
		if e.alerts != nil {
			alertsDeleted, _ = e.alerts.CleanupEventsOlderThan(cutoff)
		}
	}
	// Backups über der Aufbewahrungsgrenze ebenfalls entfernen.
	e.backups.Prune(settings.BackupRetention)
	// Speicher-Verlauf nach der (auf 90-365 Tage begrenzten) Frist bereinigen.
	storageRetention := domain.ClampStorageHistoryRetention(settings.StorageHistoryRetentionDays)
	cutoff := time.Now().AddDate(0, 0, -storageRetention).Format("2006-01-02")
	storageDeleted, _ := e.servers.DeleteStorageHistoryOlderThan(cutoff)
	// Deep-Scan-Historie: nach ANZAHL statt nach Alter begrenzt. Ein Server,
	// der selten gescannt wird, soll seine wenigen Läufe behalten - auch wenn
	// der letzte Monate zurückliegt; wer täglich scannt, sammelt sonst
	// unbegrenzt Befunde an.
	reportsDeleted, _ := e.servers.CleanupDeepScanReports(deepScanReportsKept)
	return fmt.Sprintf("%d alte job-einträge, %d ssh-protokolle, %d alarm-events, %d speicher-snapshots und %d deep-scan-berichte gelöscht (retention: %d tage logs, %d tage speicher, %d berichte je server)",
		deleted, logsDeleted, alertsDeleted, storageDeleted, reportsDeleted,
		settings.LogRetentionDays, storageRetention, deepScanReportsKept), nil
}

// TriggerRuleManually führt eine Rule sofort asynchron aus.
func (e *Executor) TriggerRuleManually(rule *domain.Rule, actor string) {
	e.audit.Log(actor, "rule.trigger", "rule", rule.ID, rule.Name)
	safego.Go("rule:"+rule.Name, func() { e.RunRule(rule, actor) })
}

// TriggerScheduleManually führt alle Rules eines Schedules sofort
// asynchron aus.
func (e *Executor) TriggerScheduleManually(sched *domain.Schedule, actor string) {
	e.audit.Log(actor, "schedule.trigger", "schedule", sched.ID, sched.Name)
	safego.Go("schedule:"+sched.Name, func() { e.RunSchedule(sched, actor) })
}

func ptrInt(i int) *int { return &i }

// summarize kürzt einen Konsolen-Output für Log-Zwecke.
func summarize(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
