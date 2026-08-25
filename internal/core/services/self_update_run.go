package services

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
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
//     kein Job mehr läuft, und sagt so lange an, worauf es wartet.

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
)

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

	mu    sync.Mutex
	state SelfUpdateStatus
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
	if s.state.Phase == SelfUpdateWaiting || s.state.Phase == SelfUpdateBackup ||
		s.state.Phase == SelfUpdateRunning {
		s.mu.Unlock()
		return s.Status(), nil // schon angefordert - ein zweiter Klick ändert nichts
	}
	now := time.Now()
	s.state = SelfUpdateStatus{
		Phase: SelfUpdateWaiting, TargetVersion: target,
		RequestedBy: actor, RequestedAt: &now,
	}
	s.mu.Unlock()

	if s.audit != nil {
		s.audit.Log(actor, "system.self-update", "settings", 1, "target="+target)
	}
	safego.Go("self-update:wait", func() { s.waitAndRun(actor, target, withBackup) })
	return s.Status(), nil
}

// waitAndRun wartet, bis kein Job mehr läuft, und startet dann das Update.
func (s *SelfUpdateService) waitAndRun(actor, target string, withBackup bool) {
	deadline := time.Now().Add(SelfUpdateWaitLimit)
	for {
		running := s.runningJobNames()
		s.setWaitingFor(running)
		if len(running) == 0 {
			break
		}
		if time.Now().After(deadline) {
			s.fail(fmt.Sprintf("Abgebrochen: Nach %s liefen immer noch Jobs (%s). "+
				"Das Update wurde nicht eingespielt - bitte später erneut anstoßen.",
				SelfUpdateWaitLimit, strings.Join(running, ", ")))
			return
		}
		time.Sleep(SelfUpdatePoll)
	}

	if withBackup {
		if s.backup == nil {
			s.fail("Sicherung nicht möglich: Der Backup-Dienst ist nicht verdrahtet. " +
				"Das Update wurde nicht eingespielt.")
			return
		}
		s.mu.Lock()
		s.state.Phase = SelfUpdateBackup
		s.mu.Unlock()
		name, err := s.backup(actor)
		if err != nil {
			s.fail("Sicherung fehlgeschlagen: " + err.Error() +
				" - das Update wurde NICHT eingespielt.")
			return
		}
		s.mu.Lock()
		s.state.BackupFile = name
		s.mu.Unlock()
		slog.Info("self-update: backup created", "file", name, "target", target)
	}

	host := LcmHostServer(s.servers)
	if host == nil {
		s.fail("Der LCM-Host steht nicht mehr in der Verwaltung - das Update wurde nicht eingespielt.")
		return
	}
	job, err := s.runJob(domain.RuleTypeUpdate, "LCM auf "+target+" aktualisieren",
		selfUpdateScript(host.RestrictedSudo), actor, nil)
	if err != nil {
		s.fail(err.Error())
		return
	}
	s.mu.Lock()
	s.state.Phase = SelfUpdateRunning
	s.state.JobID = job.ID
	s.state.WaitingFor = nil
	s.mu.Unlock()
	slog.Info("self-update started", "job", job.ID, "target", target, "actor", actor)
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

func (s *SelfUpdateService) setWaitingFor(names []string) {
	s.mu.Lock()
	s.state.WaitingFor = names
	s.mu.Unlock()
}

func (s *SelfUpdateService) fail(msg string) {
	s.mu.Lock()
	s.state.Phase = SelfUpdateFailed
	s.state.WaitingFor = nil
	s.state.Error = msg
	s.mu.Unlock()
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
// Im eingeschränkten Modus ist systemd-run nicht freigegeben; dort bleibt es
// beim direkten apt-Lauf. Der Verbindungsabbruch gehört dann zum Ablauf - der
// Wiederanlauf erkennt ihn und schließt den Job als Erfolg ab
// (SelfUpdateOnRestart in self_update.go).
func selfUpdateScript(restricted bool) string {
	apt := aptUpgradePackagesScript([]string{selfPackageName})
	if restricted {
		return apt
	}
	return "command -v systemd-run >/dev/null 2>&1 || " +
		"{ echo 'FEHLER: systemd-run nicht gefunden - das Selbst-Update braucht systemd.' >&2; exit 1; }\n" +
		"systemd-run --collect --unit=" + selfUpdateUnit + " --description='LCM-Selbst-Update' " +
		"/bin/sh -c " + shellQuote(apt) + "\n" +
		"echo 'Selbst-Update angestossen: Der apt-Lauf laeuft als eigener Dienst (" + selfUpdateUnit +
		") weiter und nimmt LCM beim Neustart mit. Protokoll: journalctl -u " + selfUpdateUnit + "'"
}
