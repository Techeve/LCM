package services

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"maps"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/cron"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// cleanupCron ist der feste Zeitplan der Log-Bereinigung (täglich 03:30).
// Die Aufbewahrungsdauer selbst ist über die globalen Einstellungen
// konfigurierbar (LogRetentionDays / BackupRetention).
const cleanupCron = "30 3 * * *"

// Die festen Takte unten hängen an der Uhr, nicht an „@every". Ein
// @every-Eintrag zählt ab seiner Anlage, und Reload legt bei jedem Speichern
// einer Regel oder der Einstellungen alle Einträge neu an: Wer öfter
// speicherte, als der Takt lang war, verhinderte den Lauf ganz - Alarm-
// Auswertung, Frühwarnung und Update-Prüfung blieben dann stumm. Ein
// Minuten-Ausdruck überlebt jeden Reload, und die Übersicht zeigt damit den
// echten nächsten Lauf.

// alertCheckCron ist das feste Intervall der Alarm-Auswertung (alle 30 Min):
// die Monitoring-/Trigger-Kriterien werden ausgewertet und lösen bei Bedarf
// eine Benachrichtigung aus.
const alertCheckCron = "*/30 * * * *"

// updateCheckCron ist das feste Intervall der LCM-Selbst-Update-Prüfung:
// alle 3 Stunden. Minute und Stunde im Takt leiten sich aus dem Hostnamen ab
// (updateCheckSpec): Eine feste Uhrzeit ließe jede Installation zur selben
// Minute bei GitLab anfragen, früher verteilte sich das über die Startzeit.
// Bewusst KEINE Einstellungsoption - die Prüfung ist fest im Kern verankert
// und läuft unabhängig von den globalen Einstellungen.
var updateCheckCron = updateCheckSpec(hostnameOrEmpty())

// updateCheckSpec baut den Takt der Update-Prüfung für einen Host: dieselbe
// Minute und Stunde bei jedem Start, aber je Host verschieden.
func updateCheckSpec(host string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	sum := h.Sum32()
	return fmt.Sprintf("%d %d-23/3 * * *", sum%60, (sum/60)%3)
}

// hostnameOrEmpty liefert den Hostnamen, leer, wenn er nicht lesbar ist.
func hostnameOrEmpty() string {
	name, _ := os.Hostname()
	return name
}

// cveDBUpdateCron gleicht die Trivy-Datenbank stündlich ab. Der Hersteller baut
// sie alle sechs Stunden, und ein neuer Stand soll kurz nach dem Bau ankommen.
// Die Bauten starten zu festen UTC-Zeiten, sind aber erst rund eine Stunde
// später fertig; eine lokale Uhrzeit träfe das je nach Sommerzeit daneben.
// Ohne neuen Stand kostet der Lauf nur eine kleine Anfrage an die Registry
// (siehe trivy.DBRefresher). Ohne diesen Zug altert die Datenbank unbegrenzt:
// Der SBOM-Scan lädt bewusst nichts nach (--skip-db-update, Sandbox ohne Netz).
//
// Bewusst KEINE Einstellungsoption, aber wie die Update-Prüfung in der
// Schedule-Übersicht sichtbar. Der Aufruf ist leise; nur ein Fehlschlag erzeugt
// einen Job (siehe Executor.RefreshCVEDB).
//
// Feste Minute statt „@every 1h": Der relative Takt beginnt beim PROZESSSTART
// und würde mit jedem Neustart wandern. Auf dem LCM-Host von Techeve landete
// ein Download so jede Nacht in der Zeitplan-Welle um vier Uhr, die ihn
// umgebracht hatte.
const cveDBUpdateCron = "10 * * * *"

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
// unabhaengig voneinander verrutschen. Als Minuten-Schritt taugt er nur, solange
// er die Stunde teilt (15 tut das); TestSystemTakteHaengenAnDerUhr wacht darüber.
var advisoryPollCron = fmt.Sprintf("*/%d * * * *", domain.AdvisoryPollIntervalMinutes)

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
	KindCVEDB    = "cve-db-update"
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
	// system bildet die Art eines System-Schedules auf seine Cron-EntryID ab -
	// daraus liest die Übersicht den echten nächsten Lauf.
	system map[string]cron.EntryID
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

func NewScheduler(groups *repositories.GroupRepository, settings *repositories.SettingsRepository, executor *Executor) *Scheduler {
	return &Scheduler{
		// cron fängt Panics in jedem Lauf ab: Ein Fehler in Backup, CVE-Scan,
		// Alarm-Auswertung oder Gruppen-Regel beendet nicht den Dienst.
		cron:     cron.New(),
		groups:   groups,
		settings: settings,
		executor: executor,
		entries:  map[uint]cron.EntryID{},
		system:   map[string]cron.EntryID{},
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
	s.cron.Stop()
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
	s.system = map[string]cron.EntryID{}

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
		s.addSystem(KindBackup, backupCron(settings.BackupIntervalHours, settings.BackupTimeOrDefault()), func() {
			s.executor.RunBackup("scheduler")
		})
		// Watchdog: holt überfällige Backups nach (siehe maybeCatchUpBackup).
		s.addSystem("backup-watchdog", backupWatchdogCron, s.maybeCatchUpBackup)
	}
	if settings.LogRetentionDays > 0 {
		s.addSystem(KindCleanup, cleanupCron, func() { s.executor.RunCleanup("scheduler") })
	}
	// CVE-Scan (Trivy) - täglicher Abgleich des Paketbestands.
	if settings.CVEScanEnabled && settings.CVEScanCron != "" {
		s.addSystem(KindCVE, settings.CVEScanCron, func() { s.executor.RunCVEScan("scheduler") })
	}
	// Alarm-Auswertung (Monitoring & Trigger) - nur wenn verdrahtet.
	if s.executor.HasAlerts() {
		s.addSystem(KindAlert, alertCheckCron, func() { s.executor.RunAlertCheck("scheduler") })
	}
	// Trivy-Datenbank - stündlich abgleichen. Ohne Scanner ein No-op
	// (RefreshCVEDB prüft das selbst).
	s.addSystem(KindCVEDB, cveDBUpdateCron, func() { s.executor.RefreshCVEDB("scheduler") })
	// Fruehwarnung (OSV) - nur wenn verdrahtet.
	if s.executor.HasAdvisories() {
		s.addSystem("advisory-poll", advisoryPollCron, func() { s.executor.RunAdvisoryPoll("scheduler") })
		s.addSystem("advisory-mirror", advisoryMirrorCron, func() { s.executor.RunAdvisoryMirror("scheduler") })
		s.addSystem("advisory-enrich", advisoryEnrichCron, func() { s.executor.RunAdvisoryEnrich("scheduler") })
	}
	// Update-Prüfung - fragt fest alle 3 Stunden die GitLab-Releases ab.
	// Bewusst NICHT über die Einstellungen schaltbar: fest im Kern verankert.
	if s.updateCheck != nil {
		s.addSystem(KindUpdate, updateCheckCron, s.updateCheck)
	}
	// Subscription-Lebenszeichen - täglich; ob überhaupt eine Subscription
	// hinterlegt ist, entscheidet die Funktion selbst. Überfällige Prüfungen
	// nach Neustarts holt CheckOnStartupIfStale nach.
	if s.subscriptionCheck != nil {
		s.addSystem("subscription-check", subscriptionCheckCron, s.subscriptionCheck)
	}
	return nil
}

// addSystem registriert einen System-Schedule unter seiner Art. Erwartet den
// gehaltenen Mutex (läuft aus Reload).
func (s *Scheduler) addSystem(kind, spec string, run func()) {
	id, err := s.cron.AddFunc(spec, run)
	if err != nil {
		slog.Error("system schedule could not be registered", "kind", kind, "expr", spec, "error", err)
		return
	}
	s.system[kind] = id
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
// selbst entscheidet, ob etwas zu tun ist. Fest an der Uhr wie die übrigen
// Takte: Ein @every-Watchdog verhungerte beim häufigen Speichern genauso.
const backupWatchdogCron = "*/10 * * * *"

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
	entryFor := maps.Clone(s.entries)
	systemFor := maps.Clone(s.system)
	s.mu.Unlock()
	// Der nächste Lauf eines System-Schedules kommt aus seinem Cron-Eintrag;
	// nur ohne Eintrag (abgeschaltet, Cron noch nicht gestartet) wird er aus
	// dem Ausdruck errechnet.
	nextFor := func(kind, spec string) string {
		if id, ok := systemFor[kind]; ok && nextByEntry[id] != "" {
			return nextByEntry[id]
		}
		return s.nextForSpec(spec)
	}

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
			NextRun:  nextFor(KindBackup, spec), Enabled: true,
		})
	}
	if settings.LogRetentionDays > 0 {
		out = append(out, ScheduleOverview{
			Kind: KindCleanup, Name: "Log-Bereinigung", Type: domain.RuleTypeCleanup,
			CronExpr: cleanupCron, NextRun: nextFor(KindCleanup, cleanupCron), Enabled: true,
		})
	}
	if settings.CVEScanCron != "" {
		out = append(out, ScheduleOverview{
			Kind: KindCVE, Name: "CVE-Scan (Trivy)", Type: domain.RuleTypeCVEScan,
			CronExpr: settings.CVEScanCron, NextRun: nextFor(KindCVE, settings.CVEScanCron),
			Enabled: settings.CVEScanEnabled,
		})
	}
	if s.executor.HasAlerts() {
		out = append(out, ScheduleOverview{
			Kind: KindAlert, Name: "Alarm-Auswertung", Type: domain.RuleTypeAlertCheck,
			CronExpr: alertCheckCron, NextRun: nextFor(KindAlert, alertCheckCron), Enabled: true,
		})
	}
	if s.updateCheck != nil {
		out = append(out, ScheduleOverview{
			Kind: KindUpdate, Name: "Update-Prüfung", Type: KindUpdate,
			CronExpr: updateCheckCron, NextRun: nextFor(KindUpdate, updateCheckCron),
			Enabled: true,
		})
	}
	if s.executor.HasCVEScanner() {
		out = append(out, ScheduleOverview{
			Kind: KindCVEDB, Name: "CVE-Datenbank abgleichen", Type: KindCVEDB,
			CronExpr: cveDBUpdateCron, NextRun: nextFor(KindCVEDB, cveDBUpdateCron),
			Enabled: true,
		})
	}
	return out, nil
}

// nextForSpec liefert den nächsten Lauf eines Cron-Ausdrucks als RFC3339.
func (s *Scheduler) nextForSpec(spec string) string {
	sched, err := cron.Parse(spec)
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
	case KindCVEDB:
		if !s.executor.HasCVEScanner() {
			return fmt.Errorf("auf dieser Instanz ist kein CVE-Scanner eingerichtet")
		}
		safego.Go("system:cve-db-update", func() { s.executor.RunCVEDBUpdate(actor) })
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
