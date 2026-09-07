package services

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"LCM/internal/safego"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

var (
	ErrAlertRuleNameRequired = errors.New("name der alarmregel ist erforderlich")
	ErrAlertRuleTypeInvalid  = errors.New("unbekannter alarm-typ")
	ErrAlertSeverityInvalid  = errors.New("ungültige dringlichkeitsstufe")
	ErrAlertGroupUnknown     = errors.New("unbekannte servergruppe")
)

// selfAlertServerName steht in Ereignissen der Selbstbeobachtung anstelle
// eines Servernamens - die Spalte bleibt lesbar, ohne einen unbeteiligten
// Rechner zu benennen.
const selfAlertServerName = "LCM"

// AlertService verwaltet die Alarm-Regeln und wertet die Monitoring- &
// Trigger-Kriterien aus: Kapazitäts-Alarme (harte Grenzen), prädiktive
// Speicheranalyse, Security/CVE, Patch-Management (überfällige Updates) und
// Downtime/Heartbeat. Bei Auslösung wird - sofern ein Kanal hinterlegt ist -
// über den NotificationService benachrichtigt (mit Cooldown-Entprellung).
type AlertService struct {
	rules    *repositories.AlertRepository
	servers  *repositories.ServerRepository
	groups   *repositories.GroupRepository
	notifier *NotificationService
	audit    *AuditService
	// notifyWG zählt laufende asynchrone Zustellungen (R2-033) - für Tests
	// und geordneten Shutdown abwartbar (WaitForNotifications).
	notifyWG sync.WaitGroup
	// now ist die Zeitquelle (in Tests überschreibbar).
	now func() time.Time
	// weightList liefert die CVE-Hochgewichtungs-Liste (globale Einstellung);
	// nil = eingebaute Standardliste. Die CVE-Alarme werten wie die Ampel die
	// GEWICHTETE Schwere (Docker runter, exponierte Pakete rauf).
	weightList func() []string
	// aptCacheURL liefert die konfigurierte APT-Cache-URL (globale
	// Einstellung) für den apt_cacher_down-Alarm; nil/leer = Prüfung entfällt.
	aptCacheURL func() (string, error)
	// crowdsecLapi führt den LAPI-Erreichbarkeits-Check aus (globale
	// Einstellungen) - Grundlage des crowdsec_lapi_down-Alarms; nil oder
	// „nicht konfiguriert" = Prüfung entfällt.
	crowdsecLapi func() (*CrowdSecLapiStatus, error)
	// cveDBStatus liefert den Stand der Schwachstellen-Datenbank des
	// CVE-Scanners - Grundlage des cve_db_stale-Alarms; nil = Prüfung entfällt.
	cveDBStatus func() domain.CVEDBStatus
	// backupStatus liefert Aktivierung, Intervall und den Zeitpunkt des
	// jüngsten Backups - Grundlage des backup_stale-Alarms; nil = Prüfung
	// entfällt.
	backupStatus func() (enabled bool, intervalHours int, newest *time.Time, err error)
	// advisoryFindings liefert die offenen Fruehwarn-Befunde eines Servers.
	advisoryFindings func(serverID uint) ([]domain.AdvisoryFinding, error)
}

func NewAlertService(rules *repositories.AlertRepository, servers *repositories.ServerRepository, groups *repositories.GroupRepository, notifier *NotificationService, audit *AuditService) *AlertService {
	return &AlertService{rules: rules, servers: servers, groups: groups, notifier: notifier, audit: audit, now: time.Now}
}

// WithCVEWeightList verdrahtet die CVE-Hochgewichtungs-Liste (optional).
func (s *AlertService) WithCVEWeightList(fn func() []string) *AlertService {
	s.weightList = fn
	return s
}

// WithAptCacheChecker verdrahtet den Zugriff auf die konfigurierte
// APT-Cache-URL (optional) - Grundlage des apt_cacher_down-Alarms.
func (s *AlertService) WithAptCacheChecker(fn func() (string, error)) *AlertService {
	s.aptCacheURL = fn
	return s
}

// WithCrowdSecLapiChecker verdrahtet den CrowdSec-LAPI-Erreichbarkeits-Check
// (optional) - Grundlage des crowdsec_lapi_down-Alarms.
func (s *AlertService) WithCrowdSecLapiChecker(fn func() (*CrowdSecLapiStatus, error)) *AlertService {
	s.crowdsecLapi = fn
	return s
}

// WithCVEDBChecker verdrahtet den Stand der Schwachstellen-Datenbank
// (optional) - Grundlage des cve_db_stale-Alarms.
func (s *AlertService) WithCVEDBChecker(fn func() domain.CVEDBStatus) *AlertService {
	s.cveDBStatus = fn
	return s
}

// WithAdvisoryFindings verdrahtet die Fruehwarn-Befunde fuer den
// advisory-Alarm. Optional - ohne sie feuert der Typ nie, statt zu paniken.
func (s *AlertService) WithAdvisoryFindings(fn func(serverID uint) ([]domain.AdvisoryFinding, error)) *AlertService {
	s.advisoryFindings = fn
	return s
}

// WithBackupStatus verdrahtet den Backup-Zustand (optional) - Grundlage des
// backup_stale-Alarms.
func (s *AlertService) WithBackupStatus(fn func() (bool, int, *time.Time, error)) *AlertService {
	s.backupStatus = fn
	return s
}

func (s *AlertService) cveWeightList() []string {
	if s.weightList != nil {
		return s.weightList()
	}
	return (&domain.GlobalSettings{}).CVEHighWeightList()
}

// ---- Verwaltung der Alarm-Regeln --------------------------------------------

// AlertRuleInput sind die über die UI/API änderbaren Felder einer Regel.
type AlertRuleInput struct {
	Name             string
	Type             string
	Enabled          bool
	GroupIDs         []uint
	ChannelID        *uint
	Severity         string
	ThresholdPercent int
	ForecastDays     int
	MaxOutdated      int
	MinSeverity      string
	HeartbeatHours   int
	CooldownMinutes  int
}

func (s *AlertService) List() ([]domain.AlertRule, error) { return s.rules.FindAll() }

func (s *AlertService) ListEvents(limit int) ([]domain.AlertEvent, error) {
	return s.rules.FindEvents(limit)
}

// ListEventsFiltered: seitenweise Event-Historie mit Gesamtzahl (R2-023).
func (s *AlertService) ListEventsFiltered(f repositories.AlertEventFilter) ([]domain.AlertEvent, int64, error) {
	return s.rules.FindEventsFiltered(f)
}

// CleanupEventsOlderThan entfernt Alarm-Events vor dem Stichtag - Teil der
// täglichen Log-Bereinigung, damit die Event-Historie nicht unbegrenzt wächst.
func (s *AlertService) CleanupEventsOlderThan(cutoff time.Time) (int64, error) {
	return s.rules.DeleteEventsOlderThan(cutoff)
}

func validAlertType(t string) bool {
	switch t {
	case domain.AlertTypeDiskCapacity, domain.AlertTypeStorageForecast,
		domain.AlertTypeSecurityCVE, domain.AlertTypeFailedUpdates, domain.AlertTypeHeartbeat,
		domain.AlertTypeRebootRequired, domain.AlertTypeAptCacherDown,
		domain.AlertTypeCrowdSecLapiDown, domain.AlertTypeDeepScan,
		domain.AlertTypeCVEDBStale, domain.AlertTypeBackupStale,
		domain.AlertTypeAdvisory:
		return true
	default:
		return false
	}
}

func validAlertSeverity(s string) bool {
	switch s {
	case domain.AlertSeverityInfo, domain.AlertSeverityWarning, domain.AlertSeverityCritical:
		return true
	default:
		return false
	}
}

// applyInput überträgt die Eingabefelder auf eine Regel (mit Validierung).
func applyInput(rule *domain.AlertRule, in AlertRuleInput) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return ErrAlertRuleNameRequired
	}
	if !validAlertType(in.Type) {
		return ErrAlertRuleTypeInvalid
	}
	severity := in.Severity
	if severity == "" {
		severity = domain.AlertSeverityWarning
	}
	if !validAlertSeverity(severity) {
		return ErrAlertSeverityInvalid
	}
	rule.Name = name
	rule.Type = in.Type
	rule.Enabled = in.Enabled
	rule.ChannelID = in.ChannelID
	// MinSeverity ist eine CVE-Schwere (critical/high/medium/low/unknown),
	// KEINE Alarm-Schwere. Ungültige Werte bekämen in SeverityAtLeast den
	// Map-Nullwert Rang 0 (= critical) - die Regel würde dann still nur noch
	// auf kritische CVEs anspringen. Leer = Default.
	if in.MinSeverity != "" && domain.NormalizeSeverity(in.MinSeverity) != in.MinSeverity {
		return fmt.Errorf("%w: min_severity %q (erlaubt: critical, high, medium, low)", ErrAlertSeverityInvalid, in.MinSeverity)
	}
	rule.Severity = severity
	rule.ThresholdPercent = in.ThresholdPercent
	rule.ForecastDays = in.ForecastDays
	rule.MaxOutdated = in.MaxOutdated
	rule.MinSeverity = in.MinSeverity
	rule.HeartbeatHours = in.HeartbeatHours
	rule.CooldownMinutes = in.CooldownMinutes
	return nil
}

func (s *AlertService) Create(in AlertRuleInput, actor string) (*domain.AlertRule, error) {
	rule := &domain.AlertRule{}
	if err := applyInput(rule, in); err != nil {
		return nil, err
	}
	groups, err := s.groupsFor(rule.Type, in.GroupIDs)
	if err != nil {
		return nil, err
	}
	rule.Groups = groups
	if err := s.rules.Create(rule); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "alert-rule.create", "alert-rule", rule.ID, rule.Name)
	return s.rules.FindByID(rule.ID)
}

// groupsFor löst die gewählten Servergruppen auf - außer bei
// Selbstbeobachtungs-Alarmen: Dort bleibt die Zuordnung leer, weil es nichts
// einzugrenzen gibt, wenn das Geprüfte nur einmal existiert. Eine gespeicherte
// Gruppe würde die Regel wieder von Servereinträgen abhängig machen; genau das
// hat sie im Container stumm gemacht.
func (s *AlertService) groupsFor(alertType string, ids []uint) ([]domain.ServerGroup, error) {
	if domain.IsSelfAlert(alertType) {
		return nil, nil
	}
	return s.resolveGroups(ids)
}

// resolveGroups lädt die gewählten Gruppen. Eine unbekannte ID wird benannt
// statt still ignoriert - sonst gälte die Regel plötzlich für alle Server.
func (s *AlertService) resolveGroups(ids []uint) ([]domain.ServerGroup, error) {
	var groups []domain.ServerGroup
	for _, id := range ids {
		group, err := s.groups.GroupByID(id)
		if err != nil {
			return nil, fmt.Errorf("%w: %d (%v)", ErrAlertGroupUnknown, id, err)
		}
		groups = append(groups, *group)
	}
	return groups, nil
}

func (s *AlertService) Update(id uint, in AlertRuleInput, actor string) (*domain.AlertRule, error) {
	rule, err := s.rules.FindByID(id)
	if err != nil {
		return nil, err
	}
	if err := applyInput(rule, in); err != nil {
		return nil, err
	}
	groups, err := s.groupsFor(rule.Type, in.GroupIDs)
	if err != nil {
		return nil, err
	}
	rule.Groups = groups
	if err := s.rules.Update(rule); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "alert-rule.update", "alert-rule", id, rule.Name)
	return s.rules.FindByID(id)
}

func (s *AlertService) Delete(id uint, actor string) error {
	rule, err := s.rules.FindByID(id)
	if err != nil {
		return err
	}
	if err := s.rules.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "alert-rule.delete", "alert-rule", id, rule.Name)
	return nil
}

// ---- Auswertung (Monitoring & Trigger) --------------------------------------

// finding bündelt das Ergebnis einer Kriterien-Prüfung für einen Server.
type finding struct {
	fired       bool
	description string
	details     string
}

// Evaluate prüft alle aktiven Alarm-Regeln gegen die Zielserver und löst bei
// Grenzverletzung - entprellt über den Cooldown - eine Benachrichtigung aus.
// Rückgabe ist eine Zusammenfassung für den Job-Output.
func (s *AlertService) Evaluate(actor string) (string, error) {
	rules, err := s.rules.FindEnabled()
	if err != nil {
		return "", err
	}
	var checked, fired, notified, suppressed int
	for i := range rules {
		rule := &rules[i]
		// Selbstbeobachtung läuft OHNE Server: Backup-Stand, CVE-Datenbank,
		// Paket-Cache und LAPI gibt es je einmal, nicht je Server. Vorher
		// hing die Prüfung an einem Servereintrag für den eigenen Rechner -
		// fehlte der (Container, oder von Hand gelöscht), blieb sie stumm.
		if domain.IsSelfAlert(rule.Type) {
			checked++
			outcome := s.fire(rule, nil, s.evaluateSelf(rule))
			fired += outcome.fired
			notified += outcome.notified
			suppressed += outcome.suppressed
			continue
		}
		servers, err := s.targetServers(rule)
		if err != nil {
			slog.Warn("alert evaluation: target servers not loadable", "rule", rule.Name, "error", err)
			continue
		}
		for j := range servers {
			server := &servers[j]
			if server.IsDemo {
				continue
			}
			checked++
			outcome := s.fire(rule, server, s.evaluateServer(rule, server))
			fired += outcome.fired
			notified += outcome.notified
			suppressed += outcome.suppressed
		}
	}
	summary := fmt.Sprintf("%d prüfung(en), %d alarm(e) ausgelöst, %d benachrichtigung(en) angestoßen",
		checked, fired, notified)
	if suppressed > 0 {
		summary += fmt.Sprintf(", %d wegen Sperrfrist unterdrückt", suppressed)
	}
	if actor != "" {
		slog.Info("alert evaluation finished", "checked", checked, "fired", fired,
			"notified", notified, "suppressed", suppressed, "actor", actor)
	}
	return summary, nil
}

// tally zählt das Ergebnis EINER Prüfung für die Zusammenfassung.
type tally struct{ fired, notified, suppressed int }

// fire wickelt den Weg vom Befund zum Ereignis ab: Sperrfrist prüfen,
// protokollieren, benachrichtigen. server darf nil sein - dann gilt der Befund
// LCM selbst und nicht einem verwalteten Server (domain.IsSelfAlert).
func (s *AlertService) fire(rule *domain.AlertRule, server *domain.Server, f finding) tally {
	if !f.fired {
		return tally{}
	}
	var serverID *uint
	if server != nil {
		serverID = &server.ID
	}
	if s.inCooldown(rule, serverID) {
		// Der Zustand verletzt die Regel, die Benachrichtigung ist nur durch
		// die Sperrfrist unterdrückt - das gehört ins Lagebild, sonst wirkt
		// „0 Alarme" wie „alles in Ordnung" (R2-063).
		return tally{suppressed: 1}
	}
	out := tally{fired: 1}
	if s.recordAndNotify(rule, server, f) {
		out.notified = 1
	}
	return out
}

// evaluateSelf wertet einen Selbstbeobachtungs-Alarm aus - ohne Server, weil
// das Geprüfte nur einmal existiert.
func (s *AlertService) evaluateSelf(rule *domain.AlertRule) finding {
	switch rule.Type {
	case domain.AlertTypeAptCacherDown:
		return s.evalAptCacherDown()
	case domain.AlertTypeCrowdSecLapiDown:
		return s.evalCrowdSecLapiDown()
	case domain.AlertTypeCVEDBStale:
		return s.evalCVEDBStale()
	case domain.AlertTypeBackupStale:
		return s.evalBackupStale()
	default:
		return finding{}
	}
}

// targetServers liefert die Server, für die eine Regel gilt: bei gewählten
// Gruppen deren Mitglieder (ein Server in mehreren Gruppen zählt einmal),
// sonst alle Server.
func (s *AlertService) targetServers(rule *domain.AlertRule) ([]domain.Server, error) {
	if len(rule.Groups) == 0 {
		all, err := s.servers.FindAllUnscoped()
		if err != nil {
			return nil, err
		}
		// Ein Server in Wartung ist absichtlich aus - ihn zu bemängeln, wäre
		// genau der Fehlalarm, der die echten unglaubwürdig macht.
		return domain.ActiveServers(all), nil
	}
	seen := map[uint]bool{}
	var servers []domain.Server
	for i := range rule.Groups {
		members, err := s.groups.ServersOfGroup(rule.Groups[i].ID)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			servers = append(servers, m)
		}
	}
	return domain.ActiveServers(servers), nil
}

// evaluateServer wertet das Kriterium einer Regel für einen Server aus.
func (s *AlertService) evaluateServer(rule *domain.AlertRule, server *domain.Server) finding {
	switch rule.Type {
	case domain.AlertTypeDiskCapacity:
		return s.evalDiskCapacity(rule, server)
	case domain.AlertTypeStorageForecast:
		return s.evalStorageForecast(rule, server)
	case domain.AlertTypeSecurityCVE:
		return s.evalSecurityCVE(rule, server)
	case domain.AlertTypeFailedUpdates:
		return s.evalFailedUpdates(rule, server)
	case domain.AlertTypeHeartbeat:
		return s.evalHeartbeat(rule, server)
	case domain.AlertTypeRebootRequired:
		return s.evalRebootRequired(server)
	case domain.AlertTypeDeepScan:
		return s.evalDeepScan(server)
	case domain.AlertTypeAdvisory:
		return s.evalAdvisory(rule, server)
	default:
		return finding{}
	}
}

// evalAdvisory feuert, wenn die Fruehwarnung offene, nicht zur Kenntnis
// genommene Befunde ab der Mindest-Schwere meldet.
//
// Zwei Besonderheiten gegenueber security_cve:
//
//   - Schadpakete (MAL-) zaehlen IMMER mit, egal welche Schwelle gesetzt ist.
//     Ihre Schwere ist in den Quellen meist gar nicht gefuellt; wer sie an
//     einer Schwelle misst, laesst den schlimmsten Fall durchrutschen.
//   - Bestaetigte Befunde (AcknowledgedBy) zaehlen nicht. Das ist das Ventil
//     fuer „bekannt, bewusst so": ohne es bliebe nur, die ganze Regel
//     abzuschalten, um einen einzelnen Dauerbefund stummzustellen - und damit
//     waere man auch fuer alles andere blind.
func (s *AlertService) evalAdvisory(rule *domain.AlertRule, server *domain.Server) finding {
	if s.advisoryFindings == nil {
		return finding{}
	}
	findings, err := s.advisoryFindings(server.ID)
	if err != nil {
		slog.Warn("advisory alert: findings not loadable", "server", server.Name, "error", err)
		return finding{}
	}
	min := rule.MinCVESeverity()
	var relevant, malware int
	for i := range findings {
		f := &findings[i]
		// Die Sonderstellung der Schadpakete steckt in EffectiveSeverity
		// (dort gilt malware immer als kritisch) - hier wird sie NICHT ein
		// zweites Mal ausformuliert. Die Zaehlung dient nur dem Meldetext.
		if f.Acknowledged() || !domain.SeverityAtLeast(f.EffectiveSeverity(), min) {
			continue
		}
		relevant++
		if f.Kind == domain.AdvisoryKindMalware {
			malware++
		}
	}
	if relevant == 0 {
		return finding{}
	}
	if malware > 0 {
		return finding{
			fired:       true,
			description: fmt.Sprintf("%d boesartige(s) Paket(e) auf %s gemeldet", malware, server.Name),
			details: "Die Fruehwarnung meldet Schadpakete (MAL-Kennung). Das ist kein Update-Thema: " +
				"das Paket gehoert vom Server - entfernen oder auf eine unbelastete Version zuruecksetzen.",
		}
	}
	return finding{
		fired:       true,
		description: fmt.Sprintf("%d neue(r) Fruehwarn-Befund(e) ab Schwere %q auf %s", relevant, min, server.Name),
		details:     "Gemeldet von der Online-Quelle, bevor der taegliche CVE-Scan sie kennt. Details unter Sicherheit → Fruehwarnung.",
	}
}

// evalBackupStale feuert, wenn automatische Backups aktiviert sind, das
// jüngste Backup aber deutlich älter ist als das Intervall erlaubt - oder gar
// keines existiert. Selbstbeobachtung: LCM sichert sich selbst, nicht je
// Server (domain.IsSelfAlert).
//
// Die Kulanz von einem vollen Intervall (Alarm ab dem DOPPELTEN Intervall-
// Alter) lässt dem Nachhol-Watchdog und einem einzelnen Fehlversuch Raum,
// bevor gemeldet wird - ein einmalig verschobener Lauf ist kein Vorfall,
// zwei ausgefallene Intervalle sind einer.
func (s *AlertService) evalBackupStale() finding {
	if s.backupStatus == nil {
		return finding{}
	}
	enabled, intervalHours, newest, err := s.backupStatus()
	if err != nil {
		slog.Warn("backup_stale: zustand nicht lesbar", "error", err)
		return finding{}
	}
	// Bewusst deaktivierte Backups sind kein Alarmfall - wer sie abschaltet,
	// hat entschieden; ein Daueralarm ließe sich nur durch Einschalten
	// abstellen und würde ignoriert.
	if !enabled || intervalHours <= 0 {
		return finding{}
	}
	if newest == nil {
		return finding{
			fired:       true,
			description: "Kein System-Backup vorhanden",
			details: "Automatische Backups sind aktiviert, aber es existiert noch keines. " +
				"Job-Historie auf fehlgeschlagene System-Backups prüfen (häufigste Ursache: fehlende Backup-Passphrase).",
		}
	}
	age := s.now().Sub(*newest)
	limit := 2 * time.Duration(intervalHours) * time.Hour
	if age < limit {
		return finding{}
	}
	return finding{
		fired:       true,
		description: "System-Backup überfällig",
		details: fmt.Sprintf("Das jüngste Backup ist %.0f Stunden alt - erlaubt sind %d Stunden (Intervall %d h, Kulanz ein Intervall). "+
			"Job-Historie auf fehlgeschlagene System-Backups prüfen.", age.Hours(), intervalHours*2, intervalHours),
	}
}

// evalDeepScan feuert, wenn der letzte Deep Scan Warnungen/kritische Befunde
// ergeben hat (Härtung/Fehlkonfiguration oder die Kernel-Reboot-Lücke).
func (s *AlertService) evalDeepScan(server *domain.Server) finding {
	if server.DeepScanWarnings > 0 || server.KernelRebootPending {
		parts := []string{}
		if server.DeepScanWarnings > 0 {
			parts = append(parts, fmt.Sprintf("%d Härtungs-/Konfigurationswarnung(en)", server.DeepScanWarnings))
		}
		if server.KernelRebootPending {
			parts = append(parts, "laufender Kernel veraltet (Neustart nötig)")
		}
		return finding{
			fired:       true,
			description: fmt.Sprintf("Deep Scan meldet Befunde auf %s", server.Name),
			details:     strings.Join(parts, "; "),
		}
	}
	return finding{}
}

// evalCVEDBStale prüft, ob die Schwachstellen-Datenbank des CVE-Scanners
// überaltert ist. Selbstbeobachtung: Scanner und Datenbank liegen zentral, es
// wird einmal geprüft - sonst käme EINE Ursache als vielfacher Alarm an.
//
// Der Alarm ist das Gegenstück zum reinen Hinweis in der Ampel: Ein stiller
// Befund fällt nur auf, wenn jemand hinsieht - bei einer verrottenden
// Datenbank ist das die falsche Erwartung, weil das Ergebnis nach außen wie
// „keine Sicherheitslücken" aussieht.
func (s *AlertService) evalCVEDBStale() finding {
	if s.cveDBStatus == nil {
		return finding{}
	}
	st := s.cveDBStatus()
	// Kein Scanner installiert = Feature nicht in Benutzung: stumm bleiben,
	// statt dauerhaft einen Alarm zu erzeugen, den niemand abstellen kann.
	if !st.Available {
		return finding{}
	}
	// Datenbank noch nie geladen - das ist der schlimmere Fall als „alt":
	// dann hat noch kein Scan echte Daten gesehen.
	if st.UpdatedAt == nil {
		return finding{
			fired:       true,
			description: "CVE-Scanner ohne Schwachstellen-Datenbank",
			details: "Die Datenbank wurde noch nie geladen - die CVE-Bewertung aller Server ist damit ohne Grundlage. " +
				st.Error,
		}
	}
	if !st.IsStale() {
		return finding{}
	}
	return finding{
		fired:       true,
		description: fmt.Sprintf("CVE-Datenbank veraltet (Stand %s)", st.AgeDescription()),
		details: fmt.Sprintf(
			"Die Schwachstellen-Datenbank des CVE-Scanners ist %s gebaut worden (Trivy %s). "+
				"Solange sie nicht erneuert wird, beruhen ALLE CVE-Ergebnisse auf diesem Stand - "+
				"„keine Sicherheitslücken gefunden\" heißt dann nur, dass seitdem nicht mehr nachgesehen wurde.",
			st.AgeDescription(), st.Version),
	}
}

// evalAptCacherDown prüft den zentralen apt-cacher-ng-Dienst über dieselbe
// Report-Seite wie die APT-Cache-Detailseite (fetchAcngReport).
// Selbstbeobachtung: den Dienst gibt es einmal. Ist keine APT-Cache-URL
// konfiguriert, ist das Feature nicht in Benutzung und der Alarm bleibt stumm.
func (s *AlertService) evalAptCacherDown() finding {
	if s.aptCacheURL == nil {
		return finding{}
	}
	url, err := s.aptCacheURL()
	if err != nil || url == "" {
		return finding{}
	}
	status, _ := fetchAcngReport(url)
	if status.Running {
		return finding{}
	}
	return finding{
		fired:       true,
		description: "apt-cacher-ng nicht erreichbar",
		details:     status.Message,
	}
}

// evalCrowdSecLapiDown prüft die zentrale CrowdSec-LAPI über denselben
// Login-Check wie die CrowdSec-Einstellungsseite (probeCrowdSecLapi).
// Selbstbeobachtung: die LAPI ist ein zentraler Dienst. Ist keine LAPI
// konfiguriert, ist das Feature nicht in Benutzung und der Alarm bleibt stumm.
func (s *AlertService) evalCrowdSecLapiDown() finding {
	if s.crowdsecLapi == nil {
		return finding{}
	}
	status, err := s.crowdsecLapi()
	if err != nil || status == nil || !status.Configured {
		return finding{}
	}
	if status.Running {
		return finding{}
	}
	return finding{
		fired:       true,
		description: "CrowdSec-LAPI nicht erreichbar",
		details:     status.Message,
	}
}

// evalRebootRequired feuert, wenn das System beim letzten Scan selbst einen
// Neustart angefordert hat (z. B. nach Kernel-/libc-Update). Reines
// Boolean-Kriterium - die Wiederholungs-Dämpfung übernimmt der Cooldown.
func (s *AlertService) evalRebootRequired(server *domain.Server) finding {
	if server.RebootRequired {
		return finding{
			fired:       true,
			description: fmt.Sprintf("Neustart erforderlich auf %s", server.Name),
			details:     "Das System fordert einen Neustart an, um installierte Updates (z. B. Kernel) vollständig zu aktivieren.",
		}
	}
	return finding{}
}

func (s *AlertService) evalDiskCapacity(rule *domain.AlertRule, server *domain.Server) finding {
	threshold := rule.DiskThreshold()
	usage := server.DiskUsagePercent()
	if server.DiskTotalMB > 0 && usage >= threshold {
		return finding{
			fired:       true,
			description: fmt.Sprintf("Festplatte auf %s zu %d%% belegt (Grenze: %d%%)", server.Name, usage, threshold),
			details:     fmt.Sprintf("Belegt: %s von %s.", domain.FormatMiB(server.DiskUsedMB), domain.FormatMiB(server.DiskTotalMB)),
		}
	}
	return finding{}
}

func (s *AlertService) evalStorageForecast(rule *domain.AlertRule, server *domain.Server) finding {
	history, err := s.servers.FindStorageHistory(server.ID)
	if err != nil {
		return finding{}
	}
	forecast := domain.ComputeForecast(history, server.DiskTotalMB)
	if forecast.InsufficientData || forecast.Unlimited {
		return finding{}
	}
	limitDays := rule.ForecastThresholdDays()
	if forecast.DaysRemaining <= limitDays {
		return finding{
			fired:       true,
			description: fmt.Sprintf("Speicher auf %s voraussichtlich in %d Tag(en) erschöpft (Frist: %d)", server.Name, forecast.DaysRemaining, limitDays),
			details:     "Basis: lineare Hochrechnung des historischen Speicher-Verlaufs.",
		}
	}
	return finding{}
}

func (s *AlertService) evalSecurityCVE(rule *domain.AlertRule, server *domain.Server) finding {
	// Gewichtete Schwere (wie die Status-Ampel): Docker-CVEs zählen nur für
	// als relevant markierte Container, exponierte/lauschende Pakete eine
	// Stufe rauf.
	facts, err := s.servers.VulnerabilityFacts(server.ID)
	if err != nil {
		return finding{}
	}
	summary := weightedVulnSummary(facts, s.cveWeightList(), splitCSVList(server.ListeningPackages), dockerRelevantRefs(s.servers, server))
	min := rule.MinCVESeverity()
	count := 0
	for sev, n := range summary {
		if domain.SeverityAtLeast(sev, min) {
			count += n
		}
	}
	if count > 0 {
		return finding{
			fired:       true,
			description: fmt.Sprintf("%d Sicherheitslücke(n) ab Schwere %q auf %s", count, min, server.Name),
			details:     fmt.Sprintf("CVE-Übersicht: %s", formatSeveritySummary(summary)),
		}
	}
	return finding{}
}

func (s *AlertService) evalFailedUpdates(rule *domain.AlertRule, server *domain.Server) finding {
	n, err := s.servers.CountOutdatedPackages(server.ID)
	if err != nil {
		return finding{}
	}
	if int(n) > rule.MaxOutdated {
		return finding{
			fired:       true,
			description: fmt.Sprintf("%d überfällige(s) Paket-Update(s) auf %s (Grenze: %d)", n, server.Name, rule.MaxOutdated),
			details:     "Bitte ausstehende Updates einspielen oder fehlgeschlagene Updates prüfen.",
		}
	}
	return finding{}
}

func (s *AlertService) evalHeartbeat(rule *domain.AlertRule, server *domain.Server) finding {
	timeout := rule.HeartbeatTimeout()
	now := s.now()
	if server.LastSeenAt == nil {
		return finding{
			fired:       true,
			description: fmt.Sprintf("Kein Kontakt zu %s (noch nie erreicht)", server.Name),
			details:     fmt.Sprintf("Loss-of-Signal-Timeout: %s.", timeout),
		}
	}
	if age := now.Sub(*server.LastSeenAt); age > timeout {
		return finding{
			fired:       true,
			description: fmt.Sprintf("Kein Kontakt zu %s seit %s (Timeout: %s)", server.Name, server.LastSeenAt.Format("2006-01-02 15:04"), timeout),
			details:     fmt.Sprintf("Letzter Kontakt vor %s.", age.Round(time.Minute)),
		}
	}
	return finding{}
}

// inCooldown meldet, ob für (Regel, Server) innerhalb des Cooldown-Fensters
// bereits ein Event existiert. serverID nil = Selbstbeobachtung; die Sperre
// gilt dann für die Regel als Ganzes.
func (s *AlertService) inCooldown(rule *domain.AlertRule, serverID *uint) bool {
	cd := rule.Cooldown()
	if cd <= 0 {
		return false // 0 = keine Sperre (R2-063)
	}
	last, err := s.rules.LastEventAt(rule.ID, serverID)
	if err != nil || last.IsZero() {
		return false
	}
	return s.now().Sub(last) < cd
}

// recordAndNotify protokolliert den Alarm und versendet - sofern ein Kanal
// hinterlegt ist - die Benachrichtigung. Rückgabe: benachrichtigt ja/nein.
//
// server nil = Selbstbeobachtung: Das Ereignis bekommt keinen Serverbezug und
// als Namen „LCM". Ein erfundener Servername wäre schlimmer als keiner - er
// zeigte auf einen Rechner, der mit dem Befund nichts zu tun hat.
func (s *AlertService) recordAndNotify(rule *domain.AlertRule, server *domain.Server, f finding) bool {
	var serverID *uint
	serverName := selfAlertServerName
	groupName := ""
	if server != nil {
		serverID, serverName = &server.ID, server.Name
		names := make([]string, 0, len(rule.Groups))
		for i := range rule.Groups {
			names = append(names, rule.Groups[i].Name)
		}
		groupName = strings.Join(names, ", ")
	}
	event := domain.AlertEvent{
		CreatedAt:   s.now(),
		RuleID:      rule.ID,
		ServerID:    serverID,
		RuleName:    rule.Name,
		ServerName:  serverName,
		GroupName:   groupName,
		Type:        rule.Type,
		Severity:    rule.Severity,
		Code:        rule.Type,
		Description: f.description,
	}

	// Das Event wird ZUERST gespeichert, der Versand danach asynchron
	// nachgetragen: vorher lief jeder Zustellversuch synchron in der
	// Auswertung - ein unerreichbarer SMTP-Host kostete ~3 s je Alarm,
	// die halbstündige Auswertung wuchs in den Minutenbereich und ihre
	// Nebenläufigkeitssperre wies derweil andere Jobs ab (R2-033). Das
	// vorbildliche Fehlerverhalten bleibt: das Event existiert unabhängig
	// vom Versandausgang, der Fehler landet in notify_error.
	if err := s.rules.CreateEvent(&event); err != nil {
		slog.Error("alert event could not be stored", "rule", rule.Name, "error", err)
		return false
	}
	if rule.ChannelID == nil || s.notifier == nil {
		return false
	}
	channel, err := s.notifier.Get(*rule.ChannelID)
	if err != nil {
		_ = s.rules.UpdateEventNotify(event.ID, false, err.Error())
		return false
	}
	notifyEvent := domain.NotificationEvent{
		Timestamp:   event.CreatedAt,
		Severity:    rule.Severity,
		ServerGroup: groupName,
		ServerName:  serverName,
		Code:        rule.Type,
		Description: f.description,
		Details:     f.details,
	}
	eventID, ruleName := event.ID, rule.Name
	s.notifyWG.Add(1)
	safego.Go("alert:notify", func() {
		defer s.notifyWG.Done()
		if err := s.notifier.Send(channel, notifyEvent); err != nil {
			slog.Warn("alert notification failed", "rule", ruleName, "server", serverName, "error", err)
			_ = s.rules.UpdateEventNotify(eventID, false, err.Error())
			return
		}
		_ = s.rules.UpdateEventNotify(eventID, true, "")
	})
	return true
}

// WaitForNotifications blockiert, bis alle angestoßenen Zustellungen
// abgeschlossen sind - für Tests und den geordneten Shutdown.
func (s *AlertService) WaitForNotifications() { s.notifyWG.Wait() }

// formatSeveritySummary formatiert die CVE-Zählung je Schweregrad kompakt.
func formatSeveritySummary(summary map[string]int) string {
	order := []string{domain.SeverityCritical, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow}
	parts := make([]string, 0, len(order))
	for _, sev := range order {
		if n, ok := summary[sev]; ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", sev, n))
		}
	}
	if len(parts) == 0 {
		return "keine"
	}
	return strings.Join(parts, ", ")
}
