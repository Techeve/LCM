package services

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/events"
	"LCM/internal/infrastructure/runtimeenv"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"
)

// Das Selbst-Update auf Knopfdruck: LCM spielt sein eigenes Debian-Paket auf
// dem Rechner ein, auf dem es läuft. Der Weg dorthin ist derselbe wie bei
// jeder anderen Paket-Aktion - ein Job über SSH auf dem selbst-registrierten
// LCM-Host. Zwei Dinge machen den Fall besonders:
//
//  1. Das Paket nimmt beim Installieren den Dienst mit. Auf einem Host mit
//     vollem sudo hängt LCM den apt-Lauf deshalb per systemd-run in einen
//     eigenen, kurzlebigen Dienst: Sonst liefe dpkg als Kind der
//     SSH-Sitzung, die LCM selbst offen hält - der Neustart schnitte den
//     Lauf mitten im Einspielen ab.
//  2. Auf dem Server laufen womöglich gerade Jobs. Sie mitzunehmen wäre ein
//     abgebrochenes Update auf einem fremden Server. LCM wartet deshalb, bis
//     kein Job mehr läuft, und sagt so lange an, worauf es wartet. Ab der
//     Anforderung startet kein neuer Job mehr, und LCM nimmt keine Aktionen
//     an (Wartungssperre, siehe job_maintenance.go und
//     middlewares.MaintenanceGuard) - sonst käme hinter dem letzten Job
//     immer noch einer nach, und der Neustart träfe ihn.

// Phasen des angeforderten Selbst-Updates.
const (
	SelfUpdateIdle    = "idle"    // nichts angefordert
	SelfUpdateWaiting = "waiting" // angefordert, wartet auf laufende Jobs
	SelfUpdateBackup  = "backup"  // Sicherung läuft, bevor aktualisiert wird
	SelfUpdateRunning = "running" // der Update-Job läuft
	SelfUpdateFailed  = "failed"  // konnte nicht gestartet werden
)

const (
	// selfPackageName ist der Name des eigenen Debian-Pakets.
	selfPackageName = "lcm"
	// selfUpdateUnit ist die transiente systemd-Unit, in der der apt-Lauf
	// weiterläuft, nachdem LCM sich selbst beendet hat.
	selfUpdateUnit = "lcm-self-update"
)

// Warte-Takt und Geduld des Selbst-Updates. Variablen, damit Tests sie auf
// Millisekunden herunterdrehen können - geprüft wird das Verhalten, nicht die
// Uhr (wie bei RebootSettleDelay & Co.).
var (
	// SelfUpdatePoll ist der Abstand, in dem geprüft wird, ob die laufenden
	// Jobs durch sind.
	SelfUpdatePoll = 5 * time.Second
	// SelfUpdateWaitLimit begrenzt das Warten. Ein Job, der stundenlang läuft
	// (oder hängt), darf das Update nicht auf ewig aufhalten - dann ist eine
	// klare Absage besser als eine Anzeige, die sich nie ändert.
	SelfUpdateWaitLimit = 30 * time.Minute
	// SelfUpdateRestartLimit ist die Frist für den Neustart, nachdem der
	// Update-Lauf angestoßen ist. Der apt-Lauf geht danach in einer eigenen
	// Unit weiter; scheitert er dort, startet LCM nie neu. Ohne Frist bliebe
	// die Sperre dann für immer stehen.
	SelfUpdateRestartLimit = 15 * time.Minute
)

// MaintenanceGate ist die Sperre neuer Jobs während des Updates (JobService).
type MaintenanceGate interface {
	BeginMaintenance()
	EndMaintenance()
	PermitDuringMaintenance(serverID uint, jobType, name string)
}

// ErrSelfUpdateNotCancelable: Das Update wartet nicht mehr auf Jobs, es lässt
// sich nicht mehr abbrechen.
var ErrSelfUpdateNotCancelable = errors.New("das update wartet nicht mehr und lässt sich nicht mehr abbrechen")

// Die Pfade, an denen das Debian-Paket Binary und Unit ablegt (siehe
// packaging/nfpm.yaml). Als Variablen, damit die Tests beide Fälle prüfen
// können - sonst hinge das Ergebnis daran, wo der Test gerade läuft.
var (
	selfBinaryPath = "/usr/bin/lcm"
	selfUnitPath   = "/lib/systemd/system/lcm.service"
)

// ErrSelfUpdateUnavailable: Auf dieser Installation gibt es kein
// Selbst-Update. Der Grund steht im Status.
var ErrSelfUpdateUnavailable = errors.New("selbst-update auf dieser installation nicht möglich")

// SelfUpdateStatus ist die Sicht der Oberfläche auf das Selbst-Update.
type SelfUpdateStatus struct {
	// Supported: Auf dieser Installation lässt sich LCM per Knopfdruck
	// aktualisieren. Ist es false, nennt Reason den Grund - die Oberfläche
	// zeigt ihn an, statt eine Schaltfläche anzubieten, die scheitern muss.
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`

	Phase         string     `json:"phase"`
	TargetVersion string     `json:"target_version"`
	RequestedBy   string     `json:"requested_by"`
	RequestedAt   *time.Time `json:"requested_at"`
	// WaitingFor sind die Jobs, die noch laufen. Genau diese Namen gehören in
	// die Ansage: „es dauert noch" ohne das Wofür ist keine Auskunft.
	WaitingFor []string `json:"waiting_for"`
	JobID      string   `json:"job_id"`
	// BackupFile ist die Sicherung, die vor dem Update entstanden ist. Sie
	// gehört in den Status: Wer nach einem missglückten Update sucht, braucht
	// den Dateinamen und nicht die Auskunft, dass „gesichert wurde".
	BackupFile string `json:"backup_file"`
	Error      string `json:"error"`
}

// SelfUpdateService steuert das angeforderte Selbst-Update.
type SelfUpdateService struct {
	jobs    *repositories.JobRepository
	servers *repositories.ServerRepository
	audit   *AuditService
	// runJob startet den Paket-Job auf dem LCM-Host (ServerService).
	runJob func(jobType, name, script, actor string, after func(ok bool)) (*domain.Job, error)
	// updateStatus liefert den zwischengespeicherten Update-Stand
	// (SettingsService) - daraus kommt die Zielversion.
	updateStatus func() (*UpdateStatus, error)
	// backup erstellt die Sicherung vor dem Update. Optional - ohne ihn
	// aktualisiert LCM ungesichert, und der Status sagt das auch.
	backup func(actor string) (string, error)
	// containerCheck meldet die Betriebsart. Als Feld, damit Tests beide
	// Zweige prüfen können: Sonst hinge das Ergebnis daran, WO sie gerade
	// laufen - auf einem Entwicklerrechner „Host", im CI-Container
	// „Container", der jeweils andere Zweig nie.
	containerCheck func() bool

	// gate sperrt neue Jobs vom Anfordern bis zum Neustart. Optional - ohne
	// ihn wartet das Update nur auf die laufenden Jobs.
	gate MaintenanceGate

	mu    sync.Mutex
	state SelfUpdateStatus
	// run zählt die Starts; siehe waitAndRun.
	run uint64
	// bus meldet jeden Phasenwechsel an die Oberfläche (system.build); nil =
	// keine Ereignisse.
	bus *events.Bus
}

func NewSelfUpdateService(
	jobs *repositories.JobRepository,
	servers *repositories.ServerRepository,
	audit *AuditService,
	runJob func(jobType, name, script, actor string, after func(ok bool)) (*domain.Job, error),
	updateStatus func() (*UpdateStatus, error),
) *SelfUpdateService {
	return &SelfUpdateService{
		jobs: jobs, servers: servers, audit: audit,
		runJob: runJob, updateStatus: updateStatus,
		state: SelfUpdateStatus{Phase: SelfUpdateIdle},
	}
}

// WithBackup verdrahtet die Sicherung, die vor dem Update läuft.
func (s *SelfUpdateService) WithBackup(fn func(actor string) (string, error)) *SelfUpdateService {
	s.backup = fn
	return s
}

// WithMaintenanceGate verdrahtet die Sperre neuer Jobs.
func (s *SelfUpdateService) WithMaintenanceGate(gate MaintenanceGate) *SelfUpdateService {
	s.gate = gate
	return s
}

// WithEvents verdrahtet den Ereignisbus (optional).
func (s *SelfUpdateService) WithEvents(bus *events.Bus) *SelfUpdateService {
	s.bus = bus
	return s
}

// publishState meldet den aktuellen Stand. Aufzurufen NACH dem Freigeben von
// s.mu.
func (s *SelfUpdateService) publishState() {
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()
	s.bus.Publish(events.SystemBuild(events.SystemBuildData{
		Phase: st.Phase, TargetVersion: st.TargetVersion, WaitingFor: st.WaitingFor,
		JobID: st.JobID, Error: st.Error,
	}))
	s.bus.Publish(events.SystemMaintenance(s.maintenanceData(st)))
}

// Maintenance ist die Sicht jedes Benutzers auf die Sperre.
func (s *SelfUpdateService) Maintenance() events.SystemMaintenanceData {
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()
	return s.maintenanceData(st)
}

func (s *SelfUpdateService) maintenanceData(st SelfUpdateStatus) events.SystemMaintenanceData {
	if !phaseLocked(st.Phase) {
		return events.SystemMaintenanceData{Phase: SelfUpdateIdle}
	}
	return events.SystemMaintenanceData{Active: true, Phase: st.Phase, TargetVersion: st.TargetVersion}
}

// Locked meldet, ob LCM wegen des Updates gerade keine Aktionen annimmt.
func (s *SelfUpdateService) Locked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return phaseLocked(s.state.Phase)
}

// phaseLocked: Von der Anforderung bis zum Neustart ist gesperrt.
func phaseLocked(phase string) bool {
	return phase == SelfUpdateWaiting || phase == SelfUpdateBackup || phase == SelfUpdateRunning
}

// Status liefert den aktuellen Stand samt Machbarkeits-Prüfung.
func (s *SelfUpdateService) Status() SelfUpdateStatus {
	s.mu.Lock()
	out := s.state
	s.mu.Unlock()
	out.Reason = s.unavailableReason()
	out.Supported = out.Reason == ""
	return out
}

// Start fordert das Selbst-Update an. Der Aufruf kehrt sofort zurück: Gewartet
// und aktualisiert wird im Hintergrund, den Fortschritt holt sich die
// Oberfläche über Status.
// Start nimmt withBackup entgegen: Vor dem Einspielen wird gesichert, und
// schlägt die Sicherung fehl, wird NICHT aktualisiert. Dieselbe Reihenfolge
// wie beim Update einer erkannten Anwendung - eine fehlgeschlagene Sicherung
// ist der Moment, in dem man ein Update am wenigsten gebrauchen kann.
func (s *SelfUpdateService) Start(actor string, withBackup bool) (SelfUpdateStatus, error) {
	if reason := s.unavailableReason(); reason != "" {
		return s.Status(), fmt.Errorf("%w: %s", ErrSelfUpdateUnavailable, reason)
	}
	target, err := s.targetVersion()
	if err != nil {
		return s.Status(), err
	}

	s.mu.Lock()
	if phaseLocked(s.state.Phase) {
		s.mu.Unlock()
		return s.Status(), nil // schon angefordert - ein zweiter Klick ändert nichts
	}
	now := time.Now()
	s.run++
	run := s.run
	s.state = SelfUpdateStatus{
		Phase: SelfUpdateWaiting, TargetVersion: target,
		RequestedBy: actor, RequestedAt: &now,
	}
	s.setGateLocked(true)
	s.mu.Unlock()
	s.publishState()

	if s.audit != nil {
		s.audit.Log(actor, "system.self-update", "settings", 1, "target="+target)
	}
	safego.Go("self-update:wait", func() { s.waitAndRun(run, actor, target, withBackup) })
	return s.Status(), nil
}

// Cancel bricht das angeforderte Update ab, solange es noch auf laufende Jobs
// wartet. Danach nicht mehr: Eine halbe Sicherung oder ein angestoßener
// apt-Lauf lassen sich nicht sauber zurücknehmen.
func (s *SelfUpdateService) Cancel(actor string) (SelfUpdateStatus, error) {
	s.mu.Lock()
	if s.state.Phase != SelfUpdateWaiting {
		s.mu.Unlock()
		return s.Status(), ErrSelfUpdateNotCancelable
	}
	target := s.state.TargetVersion
	s.state = SelfUpdateStatus{Phase: SelfUpdateIdle}
	s.setGateLocked(false)
	s.mu.Unlock()
	s.publishState()
	if s.audit != nil {
		s.audit.Log(actor, "system.self-update-cancel", "settings", 1, "target="+target)
	}
	slog.Info("self-update cancelled", "target", target, "actor", actor)
	return s.Status(), nil
}

// setGateLocked schaltet die Sperre neuer Jobs zusammen mit der Phase. Erwartet
// den gehaltenen s.mu: Phase und Sperre dürfen nicht auseinanderlaufen, wenn
// Abbruch und neuer Start sich überholen. Die Reihenfolge der Sperren ist
// immer s.mu vor JobService.mu, der Job-Dienst ruft nie zurück.
func (s *SelfUpdateService) setGateLocked(on bool) {
	switch {
	case s.gate == nil:
	case on:
		s.gate.BeginMaintenance()
	default:
		s.gate.EndMaintenance()
	}
}

// Jeder Start bekommt eine eigene Laufnummer (run). Die Schritte im
// Hintergrund wirken nur, solange ihr Lauf noch der aktuelle ist - sonst
// könnte nach Abbruch und neuem Start der alte Lauf weitermachen oder den
// neuen als gescheitert melden.

// waitAndRun wartet, bis kein Job mehr läuft, sichert und startet dann das
// Update.
func (s *SelfUpdateService) waitAndRun(run uint64, actor, target string, withBackup bool) {
	if !s.waitForJobs(run) {
		return
	}
	next := SelfUpdateRunning
	if withBackup {
		next = SelfUpdateBackup
	}
	if !s.leaveWaiting(run, next) {
		return // in genau diesem Moment abgebrochen
	}
	if withBackup && !s.runBackup(run, actor, target) {
		return
	}
	s.launch(run, actor, target)
}

// waitForJobs wartet, bis kein Job mehr läuft. false heißt: abgebrochen oder
// zu lange gewartet - dann geht es nicht weiter.
func (s *SelfUpdateService) waitForJobs(run uint64) bool {
	deadline := time.Now().Add(SelfUpdateWaitLimit)
	for {
		running := s.runningJobNames()
		if !s.setWaitingFor(run, running) {
			return false // abgebrochen
		}
		if len(running) == 0 {
			return true
		}
		if time.Now().After(deadline) {
			s.fail(run, fmt.Sprintf("Abgebrochen: Nach %s liefen immer noch Jobs (%s). "+
				"Das Update wurde nicht eingespielt - bitte später erneut anstoßen.",
				SelfUpdateWaitLimit, strings.Join(running, ", ")))
			return false
		}
		time.Sleep(SelfUpdatePoll)
	}
}

// runBackup sichert vor dem Update. Scheitert die Sicherung, wird nicht
// aktualisiert.
func (s *SelfUpdateService) runBackup(run uint64, actor, target string) bool {
	if s.backup == nil {
		s.fail(run, "Sicherung nicht möglich: Der Backup-Dienst ist nicht verdrahtet. "+
			"Das Update wurde nicht eingespielt.")
		return false
	}
	name, err := s.backup(actor)
	if err != nil {
		s.fail(run, "Sicherung fehlgeschlagen: "+err.Error()+
			" - das Update wurde NICHT eingespielt.")
		return false
	}
	s.mu.Lock()
	if s.run != run || s.state.Phase != SelfUpdateBackup {
		s.mu.Unlock()
		return false
	}
	s.state.BackupFile = name
	s.state.Phase = SelfUpdateRunning
	s.mu.Unlock()
	s.publishState()
	slog.Info("self-update: backup created", "file", name, "target", target)
	return true
}

// launch startet den Update-Job. Die Phase steht schon auf running, bevor der
// Job läuft: Scheitert er sofort, darf sein Ergebnis nicht hinterher von
// „running" überschrieben werden.
func (s *SelfUpdateService) launch(run uint64, actor, target string) {
	host := LcmHostServer(s.servers)
	if host == nil {
		s.fail(run, "Der LCM-Host steht nicht mehr in der Verwaltung - das Update wurde nicht eingespielt.")
		return
	}
	name := "LCM auf " + target + " aktualisieren"
	if s.gate != nil {
		s.gate.PermitDuringMaintenance(host.ID, domain.RuleTypeUpdate, name)
	}
	job, err := s.runJob(domain.RuleTypeUpdate, name, selfUpdateScript(host.RestrictedSudo), actor,
		func(ok bool) { s.afterUpdateJob(run, ok) })
	if err != nil {
		s.fail(run, err.Error())
		return
	}
	s.mu.Lock()
	if s.run == run && s.state.JobID == "" {
		s.state.JobID = job.ID // auch nach einem sofortigen Fehlschlag: Die Ausgabe steht in diesem Job
	}
	s.mu.Unlock()
	s.publishState()
	slog.Info("self-update started", "job", job.ID, "target", target, "actor", actor)
}

// leaveWaiting wechselt aus dem Warten in die nächste Phase - nur, wenn
// niemand das Update inzwischen abgebrochen hat. Prüfen und Wechseln sind ein
// Schritt, damit Cancel und der Start der Sicherung nicht aneinander
// vorbeilaufen.
func (s *SelfUpdateService) leaveWaiting(run uint64, phase string) bool {
	s.mu.Lock()
	if s.run != run || s.state.Phase != SelfUpdateWaiting {
		s.mu.Unlock()
		return false
	}
	s.state.Phase = phase
	s.state.WaitingFor = nil
	s.mu.Unlock()
	s.publishState()
	return true
}

// afterUpdateJob erfährt das Ende des Update-Jobs. Scheitert er, läuft LCM in
// der alten Version weiter, und die Sperre fällt. Gelingt er, hat er nur den
// apt-Lauf angestoßen; der Neustart kommt gleich. Bleibt er aus, ist der Lauf
// in seiner eigenen Unit gescheitert - dann fällt die Sperre nach der Frist.
func (s *SelfUpdateService) afterUpdateJob(run uint64, ok bool) {
	if !ok {
		s.fail(run, "Der Update-Lauf ist fehlgeschlagen, LCM läuft weiter in Version "+version.Version+
			". Die Ausgabe steht im Job.")
		return
	}
	time.Sleep(SelfUpdateRestartLimit)
	s.fail(run, fmt.Sprintf("LCM hat %s nach dem Anstoß nicht neu gestartet und läuft weiter in Version %s. "+
		"Vermutlich ist der apt-Lauf gescheitert, Protokoll: journalctl -u %s",
		SelfUpdateRestartLimit, version.Version, selfUpdateUnit))
}

// runningJobNames liefert die Namen der aktuell laufenden Jobs.
func (s *SelfUpdateService) runningJobNames() []string {
	jobs, err := s.jobs.FindRunning()
	if err != nil {
		slog.Error("self-update: running jobs not determinable", "error", err)
		return nil // lieber weitermachen als ewig auf eine kaputte Abfrage warten
	}
	names := make([]string, 0, len(jobs))
	for i := range jobs {
		names = append(names, jobs[i].Name)
	}
	return names
}

// setWaitingFor schreibt die Jobs, auf die gewartet wird. false heißt: Dieser
// Lauf wartet nicht mehr (abgebrochen oder abgelöst).
func (s *SelfUpdateService) setWaitingFor(run uint64, names []string) bool {
	s.mu.Lock()
	if s.run != run || s.state.Phase != SelfUpdateWaiting {
		s.mu.Unlock()
		return false
	}
	changed := !slices.Equal(s.state.WaitingFor, names)
	s.state.WaitingFor = names
	s.mu.Unlock()
	if changed {
		s.publishState()
	}
	return true
}

// fail meldet den Lauf als gescheitert und hebt die Sperre auf - nur, solange
// er noch der aktuelle ist und läuft. Ein abgebrochener oder abgelöster Lauf
// meldet nichts mehr.
func (s *SelfUpdateService) fail(run uint64, msg string) {
	s.mu.Lock()
	if s.run != run || !phaseLocked(s.state.Phase) {
		s.mu.Unlock()
		return
	}
	s.state.Phase = SelfUpdateFailed
	s.state.WaitingFor = nil
	s.state.Error = msg
	s.setGateLocked(false)
	s.mu.Unlock()
	s.publishState()
	slog.Warn("self-update not carried out", "reason", msg)
}

// targetVersion ist die Version, auf die aktualisiert wird - die des
// Paketkanals, auf dem der Host steht. Liegt dort nichts Neueres, gibt es
// nichts zu tun.
func (s *SelfUpdateService) targetVersion() (string, error) {
	status, err := s.updateStatus()
	if err != nil {
		return "", err
	}
	if !status.UpdateAvailable {
		return "", fmt.Errorf("es liegt keine neuere Version vor (installiert: %s)", version.Version)
	}
	return status.LatestVersion, nil
}

// unavailableReason nennt den Grund, warum es hier kein Selbst-Update gibt -
// leer, wenn es eines gibt. Die drei Bedingungen prüfen dasselbe aus drei
// Richtungen: Läuft LCM auf einem Host, kommt es aus dem Paket, und kann es
// diesen Host per SSH als root erreichen?
func (s *SelfUpdateService) unavailableReason() string {
	if s.inContainer() {
		return "LCM läuft in einem Container - dort wird das Image ausgetauscht, LCM aktualisiert sich nicht selbst."
	}
	if !installedAsPackage() {
		return "Diese Installation kommt nicht aus dem Debian-Paket - das Update muss dort eingespielt werden, wo LCM herkommt."
	}
	host := LcmHostServer(s.servers)
	if host == nil || host.PackageManager != "apt" {
		return "Der LCM-Host steht nicht als apt-Server in der Verwaltung - Selbst-Registrierung prüfen."
	}
	return ""
}

func (s *SelfUpdateService) inContainer() bool {
	if s.containerCheck != nil {
		return s.containerCheck()
	}
	return runtimeenv.InContainer()
}

// installedAsPackage meldet, ob dieses LCM aus dem Debian-Paket stammt: Das
// laufende Binary liegt dort, wo das Paket es hinlegt, und die Unit-Datei des
// Pakets ist vorhanden. Ein selbst gebautes Binary aus dem Arbeitsbaum
// erfüllt beides nicht - und für das gibt es kein apt-Update.
func installedAsPackage() bool {
	exe, err := os.Executable()
	if err != nil || exe != selfBinaryPath {
		return false
	}
	_, err = os.Stat(selfUnitPath)
	return err == nil
}

// selfUpdateScript baut den Update-Lauf für den LCM-Host.
//
// Mit vollem sudo hängt systemd-run den apt-Lauf in eine eigene, transiente
// Unit. Das ist der Kern der Sache: Der Lauf gehört damit weder zur
// SSH-Sitzung noch zur Kontrollgruppe von lcm.service und überlebt den
// Neustart, den das Paket selbst auslöst. Das Protokoll steht danach im
// Journal (`journalctl -u lcm-self-update`).
//
// Im eingeschränkten Modus läuft derselbe Ablauf über das Helper-Unterkommando
// host-self-update (root über die sudoers-Whitelist) - der Helper trägt das
// Skript des Voll-Modus.
func selfUpdateScript(restricted bool) string {
	apt := aptUpgradePackagesScript([]string{selfPackageName})
	if restricted {
		return helperCmd("host-self-update")
	}
	return "command -v systemd-run >/dev/null 2>&1 || " +
		"{ echo 'FEHLER: systemd-run nicht gefunden - das Selbst-Update braucht systemd.' >&2; exit 1; }\n" +
		"systemd-run --collect --unit=" + selfUpdateUnit + " --description='LCM-Selbst-Update' " +
		"/bin/sh -c " + shellQuote(apt) + "\n" +
		"echo 'Selbst-Update angestossen: Der apt-Lauf laeuft als eigener Dienst (" + selfUpdateUnit +
		") weiter und nimmt LCM beim Neustart mit. Protokoll: journalctl -u " + selfUpdateUnit + "'"
}
