package services

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"LCM/internal/core/domain"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// cleanupCron ist der feste Zeitplan der Log-Bereinigung (täglich 03:30).
// Die Aufbewahrungsdauer selbst ist über die globalen Einstellungen
// konfigurierbar (LogRetentionDays / BackupRetention).
const cleanupCron = "30 3 * * *"

// alertCheckCron ist das feste Intervall der Alarm-Auswertung (alle 30 Min):
// die Monitoring-/Trigger-Kriterien werden ausgewertet und lösen bei Bedarf
// eine Benachrichtigung aus.
const alertCheckCron = "@every 30m"

// updateCheckCron ist das feste Intervall der LCM-Selbst-Update-Prüfung
// (alle 3 Stunden). Bewusst KEINE Einstellungsoption - die Prüfung, ob eine
// neuere LCM-Version verfügbar ist, ist fest im Kern verankert und läuft
// unabhängig von den globalen Einstellungen.
const updateCheckCron = "@every 3h"

// cveDBUpdateCron zieht die Trivy-Datenbank fest alle 6 Stunden nach - im
// Takt, in dem der Hersteller sie baut. Ohne diesen Zug altert die Datenbank
// unbegrenzt, sobald niemand den Knopf drückt: Der SBOM-Scan lädt bewusst
// nichts nach (--skip-db-update, Sandbox ohne Netz). Wie die Update-Prüfung
// bewusst KEINE Einstellungsoption und - wie backupWatchdogCron - nicht in
// der Schedule-Übersicht: ein interner Wartungstakt, kein Zeitplan des
// Betreibers. Der Aufruf ist leise; nur ein Fehlschlag erzeugt einen Job
// (siehe Executor.RefreshCVEDB).
//
// Feste Uhrzeiten statt „@every 6h": Der relative Takt beginnt beim
// PROZESSSTART, nicht zur vollen Stunde. Auf dem LCM-Host von Techeve hat das
// eine Rückkopplung erzeugt, die sich jede Nacht selbst bestätigte - der
// Dienst wurde um 04:10 abgeräumt, der Takt verankerte sich neu auf 04:10,
// und der nächste Datenbank-Download landete am Folgetag wieder genau in der
// Zeitplan-Welle um vier Uhr, die ihn umgebracht hatte. Feste Zeiten können
// nicht wandern, und 01:40 / 07:40 / 13:40 / 19:40 liegen frei zwischen dem
// Docker-Check (kurz nach Mitternacht), dem System-Sync (04:00), dem
// OSV-Spiegel (04:15), der Anreicherung (04:45) und der Sicherung (05:14).
const cveDBUpdateCron = "40 1,7,13,19 * * *"

// advisoryPollCron ist der Takt der Fruehwarnung. 15 Minuten sind der
// Kompromiss zwischen „schnell genug, um die Trivy-Spur (6-12 h) deutlich zu
// unterbieten" und „selten genug, um einen fremden Dienst nicht zu belasten".
// Der Lauf ist leise (siehe Executor.RunAdvisoryPoll) und wird nur
// registriert, wenn die Fruehwarnung ueberhaupt verdrahtet ist; ob sie
// eingeschaltet ist, entscheidet der Lauf selbst - so wirkt das Umlegen des
// Schalters sofort, ohne Scheduler-Reload.
//
// Der Takt kommt aus der Domaenenschicht, weil die Cache-Gueltigkeit sich an
// ihm bemisst (domain.AdvisoryCacheTTLMin): Beide Werte duerfen nicht
// unabhaengig voneinander verrutschen.
var advisoryPollCron = fmt.Sprintf("@every %dm", domain.AdvisoryPollIntervalMinutes)

// advisoryEnrichCron holt das Ausnutzungs-Signal der EUVD. Taeglich, weil
// sich die Liste in Tagen aendert; zeitlich nach dem CVE-Scan, damit die
// frisch entstandenen Befunde die Markierung gleich mitbekommen.
const advisoryEnrichCron = "45 4 * * *"

// advisoryMirrorCron spiegelt die lokale OSV-Kopie. Nachts, weil der Lauf
// zig Megabyte laedt; vor der Anreicherung, damit deren Abgleich schon auf
// dem frischen Bestand arbeitet. Ob ueberhaupt gespiegelt wird, entscheidet
// der Lauf selbst anhand der Einstellung.
const advisoryMirrorCron = "15 4 * * *"

// Kind-Konstanten der Schedule-Übersicht. Der Docker-Check ist KEIN
// eigener System-Schedule mehr - er läuft als Rule des System-Sync.
const (
	KindSchedule = "schedule"
	KindBackup   = "backup"
	KindCleanup  = "cleanup"
	KindCVE      = "cve-scan"
	KindAlert    = "alert-check"
	KindUpdate   = "update-check"
)

// Scheduler ist der interne Cronjob des LCM. Er registriert zwei Arten von
// Zeitplänen:
//
//   - Gruppen-Schedules (aus der DB): jeder Schedule bündelt mehrere Rules
//     (Health-Check, Sync, Updates, Skripte), die zur Cron-Zeit nacheinander
//     laufen. Grundsatz-Regeln (Enforce) haben KEINEN Zeitplan - sie werden
//     vom Executor bei jeder Verbindung geprüft (siehe executor.go).
//   - System-Schedules aus den globalen Einstellungen: das LCM-Backup und
//     die Log-Bereinigung - diese hängen bewusst NICHT an einer Servergruppe,
//     sondern gehören zum System (konfiguriert unter Einstellungen).
//
// Die eigentliche Ausführung erledigt der Executor asynchron.
type Scheduler struct {
	cron     *cron.Cron
	groups   *repositories.GroupRepository
	settings *repositories.SettingsRepository
	executor *Executor
	// mu schützt entries: Reload läuft aus HTTP-Handlern (Settings/Rules
	// speichern), Overview liest parallel - ohne Lock wäre das ein Map-Race.
	mu sync.Mutex
	// entries bildet die Schedule-ID auf ihre Cron-EntryID ab (für Reload).
	entries map[uint]cron.EntryID
	// updateCheck führt die Update-Prüfung aus (optional verdrahtet).
	updateCheck func()
	// subscriptionCheck führt das tägliche Subscription-Lebenszeichen aus
	// (optional verdrahtet; prüft selbst, ob eine Subscription hinterlegt ist).
	subscriptionCheck func()
	// lastBackupCatchup drosselt das Nachholen überfälliger Backups (siehe
	// maybeCatchUpBackup) auf höchstens einen Versuch pro Backup-Intervall -
	// sonst erzeugte z.B. eine fehlende Backup-Passphrase alle paar Minuten
	// einen weiteren fehlschlagenden Job. Zugriff unter mu.
	lastBackupCatchup time.Time
}

// WithUpdateCheck verdrahtet die periodische Update-Prüfung. Optional -
// ohne sie wird kein Update-Schedule registriert.
func (s *Scheduler) WithUpdateCheck(fn func()) *Scheduler {
	s.updateCheck = fn
	return s
}

// WithSubscriptionCheck verdrahtet das tägliche Subscription-Lebenszeichen
// (verify beim Anbieter-Dienst). Optional.
func (s *Scheduler) WithSubscriptionCheck(fn func()) *Scheduler {
	s.subscriptionCheck = fn
	return s
}

// cronSlogLogger verbindet robfig/cron mit dem strukturierten Log. Ohne ihn
// würde die Recover-Kette ihre Panic-Meldung an einen Standard-Logger schicken
// und damit an der Logdatei vorbei.
type cronSlogLogger struct{}

func (cronSlogLogger) Info(msg string, kv ...any) {
	slog.Debug("cron: "+msg, kv...)
}

func (cronSlogLogger) Error(err error, msg string, kv ...any) {
	slog.Error("cron: "+msg, append([]any{"error", err}, kv...)...)
}

func NewScheduler(groups *repositories.GroupRepository, settings *repositories.SettingsRepository, executor *Executor) *Scheduler {
	return &Scheduler{
		// Standard-Cron unterstützt 5-Feld-Ausdrücke und @every/@daily.
		//
		// cron.Recover ist zwingend: Ohne diese Kette würde ein Panic in einem
		// geplanten Lauf (Backup, CVE-Scan, Alarm-Auswertung, Gruppen-Regel)
		// den GESAMTEN Dienst beenden - robfig/cron startet jeden Lauf in einer
		// eigenen Goroutine und fängt von sich aus nichts ab.
		cron:     cron.New(cron.WithChain(cron.Recover(cronSlogLogger{}))),
		groups:   groups,
		settings: settings,
		executor: executor,
		entries:  map[uint]cron.EntryID{},
	}
}

// Start lädt alle aktiven Schedules und startet den Cron-Loop.
func (s *Scheduler) Start() error {
	if err := s.Reload(); err != nil {
		return err
	}
	s.cron.Start()
	slog.Info("scheduler started", "active_schedules", len(s.cron.Entries()))
	return nil
}

// Stop hält den Scheduler an (Graceful Shutdown).
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

// Reload synchronisiert die registrierten Cron-Einträge mit dem aktuellen
// Stand der aktiven Rules und der globalen Einstellungen. Nach Änderungen
// an Rules/Schedules ODER an den Backup-/Retention-Einstellungen aufrufen.
func (s *Scheduler) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Kompletter Neuaufbau (einfach, robust - überschaubare Anzahl).
	for _, e := range s.cron.Entries() {
		s.cron.Remove(e.ID)
	}
	s.entries = map[uint]cron.EntryID{}

	// 1. Gruppen-Schedules (mit ihren Rules).
	schedules, err := s.groups.FindAllSchedules()
	if err != nil {
		return err
	}
	for i := range schedules {
		sched := schedules[i] // Kopie für die Closure
		if !sched.Enabled {
			continue
		}
		entryID, err := s.cron.AddFunc(sched.CronExpr, func() {
			slog.Debug("schedule fired", "schedule", sched.Name, "cron", sched.CronExpr,
				"rules", len(sched.Rules))
			s.executor.RunSchedule(&sched, "scheduler")
		})
		if err != nil {
			slog.Error("invalid cron expression - schedule skipped",
				"schedule", sched.Name, "expr", sched.CronExpr, "error", err)
			continue
		}
		s.entries[sched.ID] = entryID
	}

	// 2. System-Schedules aus den Einstellungen (Backup + Cleanup).
	settings, err := s.settings.Get()
	if err != nil {
		return err
	}
	if settings.BackupEnabled && settings.BackupIntervalHours > 0 {
		if _, err := s.cron.AddFunc(backupCron(settings.BackupIntervalHours, settings.BackupTimeOrDefault()), func() {
			s.executor.RunBackup("scheduler")
		}); err != nil {
			slog.Error("backup schedule could not be registered", "error", err)
		}
		// Watchdog: holt überfällige Backups nach (siehe maybeCatchUpBackup).
		if _, err := s.cron.AddFunc(backupWatchdogCron, s.maybeCatchUpBackup); err != nil {
			slog.Error("backup watchdog could not be registered", "error", err)
		}
	}
	if settings.LogRetentionDays > 0 {
		if _, err := s.cron.AddFunc(cleanupCron, func() {
			s.executor.RunCleanup("scheduler")
		}); err != nil {
			slog.Error("cleanup schedule could not be registered", "error", err)
		}
	}
	// CVE-Scan (Trivy) - täglicher Abgleich des Paketbestands.
	if settings.CVEScanEnabled && settings.CVEScanCron != "" {
		if _, err := s.cron.AddFunc(settings.CVEScanCron, func() {
			s.executor.RunCVEScan("scheduler")
		}); err != nil {
			slog.Error("cve scan schedule could not be registered",
				"expr", settings.CVEScanCron, "error", err)
		}
	}
	// Alarm-Auswertung (Monitoring & Trigger) - nur wenn verdrahtet.
	if s.executor.HasAlerts() {
		if _, err := s.cron.AddFunc(alertCheckCron, func() {
			s.executor.RunAlertCheck("scheduler")
		}); err != nil {
			slog.Error("alert check schedule could not be registered", "error", err)
		}
	}
	// Trivy-Datenbank - fest alle 6 Stunden nachziehen. Ohne Scanner ein
	// No-op (RefreshCVEDB prüft das selbst).
	if _, err := s.cron.AddFunc(cveDBUpdateCron, func() {
		s.executor.RefreshCVEDB("scheduler")
	}); err != nil {
		slog.Error("cve db update schedule could not be registered",
			"expr", cveDBUpdateCron, "error", err)
	}
	// Fruehwarnung (OSV) - nur wenn verdrahtet.
	if s.executor.HasAdvisories() {
		if _, err := s.cron.AddFunc(advisoryPollCron, func() {
			s.executor.RunAdvisoryPoll("scheduler")
		}); err != nil {
			slog.Error("advisory poll schedule could not be registered",
				"expr", advisoryPollCron, "error", err)
		}
		if _, err := s.cron.AddFunc(advisoryMirrorCron, func() {
			s.executor.RunAdvisoryMirror("scheduler")
		}); err != nil {
			slog.Error("advisory mirror schedule could not be registered",
				"expr", advisoryMirrorCron, "error", err)
		}
		if _, err := s.cron.AddFunc(advisoryEnrichCron, func() {
			s.executor.RunAdvisoryEnrich("scheduler")
		}); err != nil {
			slog.Error("advisory enrichment schedule could not be registered",
				"expr", advisoryEnrichCron, "error", err)
		}
	}
	// Update-Prüfung - fragt fest alle 3 Stunden die GitLab-Releases ab.
	// Bewusst NICHT über die Einstellungen schaltbar: fest im Kern verankert.
	if s.updateCheck != nil {
		if _, err := s.cron.AddFunc(updateCheckCron, s.updateCheck); err != nil {
			slog.Error("update check schedule could not be registered",
				"expr", updateCheckCron, "error", err)
		}
	}
	// Subscription-Lebenszeichen - täglich; ob überhaupt eine Subscription
	// hinterlegt ist, entscheidet die Funktion selbst. Überfällige Prüfungen
	// nach Neustarts holt CheckOnStartupIfStale nach.
	if s.subscriptionCheck != nil {
		if _, err := s.cron.AddFunc(subscriptionCheckCron, s.subscriptionCheck); err != nil {
			slog.Error("subscription check schedule could not be registered",
				"expr", subscriptionCheckCron, "error", err)
		}
	}
	return nil
}

// subscriptionCheckCron ist der feste Zeitplan des Subscription-
// Lebenszeichens (täglich 05:15 - nach der Log-Bereinigung, vor dem
// üblichen CVE-Scan).
const subscriptionCheckCron = "15 5 * * *"

// backupCron baut den Zeitplan des automatischen Backups. Teilt das Intervall
// den Tag (1,2,3,4,6,8,12,24 h), entstehen FESTE Uhrzeiten, abgeleitet aus der
// Anker-Uhrzeit (backup_time) - ein @every-Eintrag zählte dagegen ab dem
// letzten Scheduler-Reload, und jedes Speichern der Einstellungen verschob
// damit den Lauf (R2-034; wer öfter speicherte, als das Intervall lang war,
// bekam nie ein Backup). Krumme Intervalle (z.B. 36 h) lassen sich in cron
// nicht als feste Uhrzeit ausdrücken und behalten @every - dort fängt der
// Nachhol-Watchdog (maybeCatchUpBackup) die Verschiebung ab.
func backupCron(intervalHours int, at string) string {
	t, err := time.Parse("15:04", at)
	if err == nil && intervalHours >= 1 && intervalHours <= 24 && 24%intervalHours == 0 {
		hours := make([]string, 0, 24/intervalHours)
		for h := t.Hour() % intervalHours; h < 24; h += intervalHours {
			hours = append(hours, strconv.Itoa(h))
		}
		return fmt.Sprintf("%d %s * * *", t.Minute(), strings.Join(hours, ","))
	}
	return fmt.Sprintf("@every %dh", intervalHours)
}

// backupWatchdogCron ist das Prüf-Intervall des Backup-Watchdogs - er darf
// deutlich kürzer sein als das Backup-Intervall, weil maybeCatchUpBackup
// selbst entscheidet, ob etwas zu tun ist.
const backupWatchdogCron = "@every 10m"

// backupOverdue meldet, ob das jüngste Backup älter als das Intervall ist
// (bzw. noch gar keines existiert). backups kommt neueste-zuerst sortiert
// aus FindBackups; auch manuelle Backups zählen - sie enthalten denselben
// Stand, ein zusätzliches automatisches wäre redundant.
func backupOverdue(backups []domain.Backup, intervalHours int, now time.Time) bool {
	if len(backups) == 0 {
		return true
	}
	return now.Sub(backups[0].CreatedAt) >= time.Duration(intervalHours)*time.Hour
}

// maybeCatchUpBackup holt ein überfälliges automatisches Backup nach. Nötig,
// weil der @every-Cron-Eintrag ab JETZT zählt: bei jedem Neustart und jedem
// Reload (Einstellungen/Regeln speichern) beginnt das Intervall von vorn -
// eine Instanz, die häufiger neu startet als das Intervall lang ist (z.B.
// durch regelmäßige Updates), käme ohne Nachholen NIE zu einem automatischen
// Backup. Fehlversuche (etwa fehlende Passphrase) werden auf einen pro
// Intervall gedrosselt, damit die Job-Historie nicht mit Fehlschlägen flutet.
func (s *Scheduler) maybeCatchUpBackup() {
	settings, err := s.settings.Get()
	if err != nil || !settings.BackupEnabled || settings.BackupIntervalHours <= 0 {
		return
	}
	interval := time.Duration(settings.BackupIntervalHours) * time.Hour
	s.mu.Lock()
	throttled := !s.lastBackupCatchup.IsZero() && time.Since(s.lastBackupCatchup) < interval
	s.mu.Unlock()
	if throttled {
		return
	}
	backups, err := s.settings.FindBackups()
	if err != nil {
		slog.Error("backup catch-up: history not readable", "error", err)
		return
	}
	if !backupOverdue(backups, settings.BackupIntervalHours, time.Now()) {
		return
	}
	s.mu.Lock()
	s.lastBackupCatchup = time.Now()
	s.mu.Unlock()
	slog.Info("automatic backup overdue - catching up",
		"interval_hours", settings.BackupIntervalHours)
	// Läuft bereits in der Cron-Goroutine - synchron ausführen.
	s.executor.RunBackup("scheduler")
}

// ScheduleOverview beschreibt einen anstehenden Schedule für die UI.
type ScheduleOverview struct {
	Kind       string        `json:"kind"`                  // schedule | backup | cleanup
	ScheduleID uint          `json:"schedule_id,omitempty"` // nur bei Gruppen-Schedules
	Name       string        `json:"name"`
	Type       string        `json:"type,omitempty"` // nur bei System-Schedules
	CronExpr   string        `json:"cron_expr"`
	GroupID    uint          `json:"group_id,omitempty"` // nur bei Gruppen-Schedules
	NextRun    string        `json:"next_run"`           // RFC3339, leer wenn inaktiv
	Enabled    bool          `json:"enabled"`
	Rules      []domain.Rule `json:"rules,omitempty"` // die Rules des Schedules
}

// Overview liefert alle Zeitpläne (Gruppen-Schedules + System-Schedules)
// mit ihrem nächsten Ausführungszeitpunkt - die globale Scheduler-Übersicht.
func (s *Scheduler) Overview() ([]ScheduleOverview, error) {
	schedules, err := s.groups.FindAllSchedules()
	if err != nil {
		return nil, err
	}
	nextByEntry := map[cron.EntryID]string{}
	for _, e := range s.cron.Entries() {
		if !e.Next.IsZero() {
			nextByEntry[e.ID] = e.Next.Format(time.RFC3339)
		}
	}
	// Kopie der Entry-Zuordnung unter Lock - Reload baut die Map parallel um.
	s.mu.Lock()
	entryFor := make(map[uint]cron.EntryID, len(s.entries))
	for id, eid := range s.entries {
		entryFor[id] = eid
	}
	s.mu.Unlock()

	out := make([]ScheduleOverview, 0, len(schedules)+2)
	for i := range schedules {
		sc := &schedules[i]
		ov := ScheduleOverview{
			Kind: KindSchedule, ScheduleID: sc.ID, Name: sc.Name,
			CronExpr: sc.CronExpr, GroupID: sc.GroupID, Enabled: sc.Enabled,
			Rules: sc.Rules,
		}
		if id, ok := entryFor[sc.ID]; ok {
			ov.NextRun = nextByEntry[id]
		}
		out = append(out, ov)
	}

	// System-Schedules (aus den Einstellungen).
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	if settings.BackupEnabled && settings.BackupIntervalHours > 0 {
		spec := backupCron(settings.BackupIntervalHours, settings.BackupTimeOrDefault())
		out = append(out, ScheduleOverview{
			Kind: KindBackup, Name: "System-Backup", Type: domain.RuleTypeBackup,
			CronExpr: spec,
			NextRun:  s.nextForSpec(spec), Enabled: true,
		})
	}
	if settings.LogRetentionDays > 0 {
		out = append(out, ScheduleOverview{
			Kind: KindCleanup, Name: "Log-Bereinigung", Type: domain.RuleTypeCleanup,
			CronExpr: cleanupCron, NextRun: s.nextForSpec(cleanupCron), Enabled: true,
		})
	}
	if settings.CVEScanCron != "" {
		out = append(out, ScheduleOverview{
			Kind: KindCVE, Name: "CVE-Scan (Trivy)", Type: domain.RuleTypeCVEScan,
			CronExpr: settings.CVEScanCron, NextRun: s.nextForSpec(settings.CVEScanCron),
			Enabled: settings.CVEScanEnabled,
		})
	}
	if s.executor.HasAlerts() {
		out = append(out, ScheduleOverview{
			Kind: KindAlert, Name: "Alarm-Auswertung", Type: domain.RuleTypeAlertCheck,
			CronExpr: alertCheckCron, NextRun: s.nextForSpec(alertCheckCron), Enabled: true,
		})
	}
	if s.updateCheck != nil {
		out = append(out, ScheduleOverview{
			Kind: KindUpdate, Name: "Update-Prüfung", Type: KindUpdate,
			CronExpr: updateCheckCron, NextRun: s.nextForSpec(updateCheckCron),
			Enabled: true,
		})
	}
	return out, nil
}

// nextForSpec liefert den nächsten Lauf eines Cron-Ausdrucks als RFC3339.
func (s *Scheduler) nextForSpec(spec string) string {
	sched, err := cronParser.Parse(spec)
	if err != nil {
		return ""
	}
	next := sched.Next(time.Now())
	if next.IsZero() {
		return ""
	}
	return next.Format(time.RFC3339)
}

// TriggerNow führt eine einzelne Gruppen-Rule sofort aus (der aufrufende
// Controller hat die Berechtigung bereits geprüft).
func (s *Scheduler) TriggerNow(rule *domain.Rule, actor string) {
	s.executor.TriggerRuleManually(rule, actor)
}

// TriggerScheduleNow führt alle Rules eines Schedules sofort aus.
func (s *Scheduler) TriggerScheduleNow(sched *domain.Schedule, actor string) {
	s.executor.TriggerScheduleManually(sched, actor)
}

// TriggerSystem führt einen System-Schedule sofort aus. Die Kinds hier müssen
// vollständig zu denen passen, die Overview ausgibt - was in der Übersicht
// steht, trägt dort einen „Jetzt ausführen"-Knopf, und der muss etwas tun.
// TestJederSystemScheduleIstAusloesbar wacht darüber.
func (s *Scheduler) TriggerSystem(kind, actor string) error {
	switch kind {
	case KindBackup:
		safego.Go("system:backup", func() { s.executor.RunBackup(actor) })
	case KindCleanup:
		safego.Go("system:cleanup", func() { s.executor.RunCleanup(actor) })
	case KindCVE:
		safego.Go("system:cve-scan", func() { s.executor.RunCVEScan(actor) })
	case KindAlert:
		safego.Go("system:alert-check", func() { s.executor.RunAlertCheck(actor) })
	case KindUpdate:
		// Die Update-Prüfung ist optional verdrahtet; ohne sie steht sie auch
		// nicht in der Übersicht.
		if s.updateCheck == nil {
			return fmt.Errorf("die Update-Prüfung ist auf dieser Instanz nicht eingerichtet")
		}
		safego.Go("system:update-check", s.updateCheck)
	default:
		return fmt.Errorf("unbekannter system-schedule: %q", kind)
	}
	return nil
}

// TriggerAdvisoryPoll stößt einen Frühwarn-Durchgang sofort an (manueller
// Auslöser aus der Oberfläche) und liefert den Job zurück - die Oberfläche
// wartet darauf und zeigt sein Ergebnis.
func (s *Scheduler) TriggerAdvisoryPoll(actor string) (*domain.Job, error) {
	return s.executor.StartAdvisoryPoll(actor)
}

// TriggerAdvisoryMirror stößt den Spiegellauf der lokalen Kopie an.
func (s *Scheduler) TriggerAdvisoryMirror(actor string) (*domain.Job, error) {
	return s.executor.StartAdvisoryMirror(actor)
}

// TriggerCVEScanServer stößt einen CVE-Scan für einen einzelnen Server an
// (manueller Trigger aus der Server-Detailansicht).
func (s *Scheduler) TriggerCVEScanServer(id uint, actor string) {
	safego.Go("cve-scan:server", func() { s.executor.RunCVEScanServer(id, actor) })
}

// TriggerDeepScanServer stößt den Deep Scan für einen einzelnen Server an
// (manueller Trigger aus der Server-Detailansicht).
func (s *Scheduler) TriggerDeepScanServer(id uint, actor string) {
	safego.Go("deep-scan:server", func() { s.executor.RunDeepScanServer(id, actor) })
}
