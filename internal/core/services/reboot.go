package services

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// rebootDelaySeconds ist die Verzögerung zwischen SSH-Rückmeldung und dem
// tatsächlichen Neustart - genug Zeit, damit der auslösende SSH-Exec-Aufruf
// sauber mit Exit 0 zurückkehrt, BEVOR der Neustart selbst die Verbindung
// kappt (sonst würde der Job fälschlich als Verbindungsfehler enden).
const rebootDelaySeconds = 2

// rebootScript baut das Neustart-Kommando - vom SSH-Kanal ABGEKOPPELT
// (nohup + Hintergrund), damit der Aufruf sofort zurückkehrt.
func rebootScript() string {
	return fmt.Sprintf("nohup sh -c 'sleep %d; reboot' >/dev/null 2>&1 &", rebootDelaySeconds)
}

// Rückkehr-Überwachung nach einem Neustart. Bewusst exportierte Variablen
// statt Konstanten: Tests setzen sie herunter, statt in Echtzeit zu warten
// (dasselbe Muster wie RepoBaseURL in update_check.go).
var (
	// RebootSettleDelay ist die Anlaufsperre, BEVOR der erste
	// Verbindungsversuch als Rückkehr zählt.
	//
	// Sie ist nicht optional: Das Neustart-Kommando kehrt sofort zurück, der
	// Server fährt aber erst Sekunden später herunter (rebootDelaySeconds
	// plus die Zeit, die das System zum Beenden braucht). Ohne diese Sperre
	// träfe der erste Ping den NOCH LAUFENDEN Server an, und die Überwachung
	// meldete einen Neustart als erfolgreich abgeschlossen, der noch gar
	// nicht begonnen hat.
	RebootSettleDelay = 15 * time.Second
	// RebootPingInterval ist der Abstand zwischen zwei Verbindungsversuchen.
	RebootPingInterval = 2 * time.Second
	// RebootMaxWait ist das Zeitfenster für die Rückkehr. Danach gilt der
	// Neustart als fehlgeschlagen - dann steht ein Server, der nicht
	// wiederkommt, als solcher da und nicht als „läuft".
	RebootMaxWait = 10 * time.Minute
)

// completeRebootJob wertet das Ergebnis des Neustart-Kommandos aus. Bei
// Erfolg bleibt der Job OFFEN und die Rückkehr wird überwacht (siehe
// awaitRebootReturn) - vorher endete er sofort mit „ausgelöst", und ob der
// Server je wiederkam, erfuhr man erst beim nächsten Health-Check.
//
// dial prüft die Erreichbarkeit, onOnline erfasst die Daten nach der
// Rückkehr neu (beides optional; ohne dial wird nicht gewartet).
func completeRebootJob(jobs *JobService, servers *repositories.ServerRepository, server *domain.Server, job *domain.Job, output string, code int, runErr error, dial func() error, onOnline func()) {
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("neustart-kommando endete mit exit-code %d", code)
	}
	if runErr != nil {
		jobs.Complete(job, output, ptrInt(code), runErr)
		return
	}
	// Bewusst OHNE failed_checks: Der Server ist hier erwartungsgemäß kurz
	// weg, das ist kein fehlgeschlagener Erreichbarkeits-Check. Käme er nicht
	// zurück, zählen die nächsten Health-Pings ihn ohnehin als offline.
	_ = servers.UpdateFields(server.ID, map[string]any{
		"reachable": false, "last_error": "Neustart ausgelöst - sollte in Kürze wieder online sein.",
	})
	output += "\nNeustart ausgelöst - der Server ist für kurze Zeit nicht erreichbar."
	if dial == nil {
		jobs.Complete(job, output, ptrInt(0), nil)
		return
	}
	awaitRebootReturn(jobs, servers, server, job, output, dial, onOnline)
}

// awaitRebootReturn wartet, bis der Server wieder antwortet, und schließt den
// Job erst dann ab. Kommt er innerhalb von rebootMaxWait nicht zurück,
// scheitert der Job mit genau dieser Aussage.
func awaitRebootReturn(jobs *JobService, servers *repositories.ServerRepository, server *domain.Server, job *domain.Job, output string, dial func() error, onOnline func()) {
	back, err := pollRebootReturn(dial, RebootSettleDelay, RebootPingInterval, RebootMaxWait)
	if err != nil {
		jobs.Complete(job, output, nil, err)
		return
	}
	_ = servers.UpdateFields(server.ID, map[string]any{
		"reachable": true, "last_error": "",
	})
	output += fmt.Sprintf("\nServer nach %s wieder erreichbar.", back.Round(time.Second))
	if onOnline != nil {
		output += "\nDatenerfassung angestoßen."
	}
	jobs.Complete(job, output, ptrInt(0), nil)
	// ERST nach dem Abschluss: Die Datenerfassung läuft als eigener Job und
	// liefe sonst in die noch bestehende Server-Sperre.
	//
	// Der Stand von vor dem Neustart ist überholt - laufender Kernel,
	// Betriebszeit und ein eventuell erledigtes „Neustart erforderlich"
	// ändern sich genau hier.
	if onOnline != nil {
		onOnline()
	}
}

// pollRebootReturn ist die Warteschleife selbst: erst die Anlaufsperre
// abwarten, dann im festen Takt anklopfen, bis der Server antwortet oder das
// Zeitfenster abläuft. Liefert die Wartedauer bis zur Rückkehr - oder den
// Fehler, der die Zeitüberschreitung samt letztem Verbindungsfehler nennt.
//
// Bewusst ohne Job und Datenbank: So ist das Verhalten prüfbar, ohne einen
// Server neu zu starten.
func pollRebootReturn(dial func() error, settle, interval, maxWait time.Duration) (time.Duration, error) {
	time.Sleep(settle)

	started := time.Now()
	deadline := started.Add(maxWait)
	var lastErr error
	for {
		if err := dial(); err == nil {
			return time.Since(started), nil
		} else {
			lastErr = err
		}
		if !time.Now().Before(deadline) {
			msg := fmt.Sprintf("der Server war nach dem Neustart auch nach %s nicht wieder erreichbar", maxWait)
			if lastErr != nil {
				msg += " (letzter Versuch: " + lastErr.Error() + ")"
			}
			return 0, errors.New(msg)
		}
		time.Sleep(interval)
	}
}

// Reboot startet den Server neu (Ein-Klick-Aktion aus der Server-
// Detailansicht). Braucht vollen Root-Zugriff - im eingeschränkten
// Sudo-Modus gesperrt (reboot steht nicht auf der Whitelist).
func (s *ServerService) Reboot(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureFullSudo(server); err != nil {
		return nil, err
	}
	job, err := s.jobs.Start(&server.ID, nil, domain.RuleTypeReboot, "Neustart @ "+server.Name, actor)
	if err != nil {
		return nil, err // u.a. ErrServerBusy → der Controller mappt auf 409
	}
	s.audit.Log(actor, "server.reboot", "server", id, server.Name)
	safego.GoCleanup("job:reboot", jobPanicCleanup(s.jobs, job), func() {
		s.runRebootJob(job, server, actor)
	})
	return job, nil
}

func (s *ServerService) runRebootJob(job *domain.Job, server *domain.Server, actor string) {
	if server.IsDemo {
		s.jobs.Complete(job, "demo-server: neustart simuliert (kein ssh-kontakt)", ptrInt(0), nil)
		return
	}
	conn, err := s.connect(server)
	if err != nil {
		_ = s.servers.UpdateFields(server.ID, unreachableFields(server, err))
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = s.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: actor,
		Purpose: "reboot", Host: server.Host, User: server.ServiceUser,
	})

	output, code, runErr := conn.Run(privRun(server, rebootScript()))
	// Diese Verbindung MUSS vor der Warteschleife weg - und zwar sofort, nicht
	// per defer: Sie zeigt auf den herunterfahrenden Server und belegt den
	// einen ausführenden Verbindungs-Slot des Servers (ConnLimiter). Bliebe
	// sie offen, wartete die Rückkehr-Überwachung auf einen Slot, den erst sie
	// selbst freigeben würde.
	_ = conn.Close()

	// Die Überwachung braucht eine EIGENE Verbindung je Versuch - die obige
	// ist mit dem Server gefallen. Sie wird sofort wieder geschlossen: geprüft
	// wird die Erreichbarkeit, nicht mehr.
	dial := func() error {
		c, err := s.connect(server)
		if err != nil {
			return err
		}
		_ = c.Close()
		return nil
	}
	completeRebootJob(s.jobs, s.servers, server, job, output, code, runErr, dial, func() {
		// Eigener Job, deshalb ERST nach dem Abschluss des Neustart-Jobs
		// (sonst stünde die Server-Sperre im Weg). ScopeAll: der Lauf gehört
		// dem System, nicht dem auslösenden Benutzer.
		if _, err := s.RefreshAll(repositories.ScopeAll(), server.ID, actor); err != nil {
			slog.Warn("datenerfassung nach neustart nicht gestartet", "server", server.ID, "error", err)
		}
	})
}

// runRebootRule ist das Gegenstück für die geplante Gruppen-Regel
// (RuleTypeReboot). Der eingeschränkte Sudo-Modus wird bereits vom
// generischen Gate in runOnServer abgefangen (reboot steht nicht in
// restrictedAllowsRule) - hier also kein zusätzlicher Check nötig.
func (e *Executor) runRebootRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
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

	// „Nur bei Bedarf": Das System wird gefragt, bevor es umgelegt wird. Ein
	// Neustart ohne Anlass kostet eine Auszeit und bringt nichts.
	if rule.Type == domain.RuleTypeRebootIfNeeded && !rebootRequiredNow(server, conn) {
		// Den gespeicherten Stand gleich ehrlich halten: Wer die Regel laufen
		// sieht, soll in der Übersicht nicht weiter „Neustart nötig" lesen.
		_ = e.servers.UpdateFields(server.ID, map[string]any{"reboot_required": false})
		_ = conn.Close()
		e.jobs.Complete(job, "Kein Neustart nötig - das System fordert keinen an. Übersprungen.", ptrInt(0), nil)
		return
	}

	output, code, runErr := conn.Run(privRun(server, rebootScript()))
	// Vor der Warteschleife schließen - siehe runRebootJob: sonst hält diese
	// Verbindung den Slot, auf den die Überwachung wartet.
	_ = conn.Close()

	dial := func() error {
		c, err := e.connect(server)
		if err != nil {
			return err
		}
		_ = c.Close()
		return nil
	}
	// Die Regel überwacht die Rückkehr ebenso - ein geplanter Neustart, der
	// den Server nicht zurückbringt, ist derselbe Vorfall wie ein von Hand
	// ausgelöster. Die Neuerfassung übernimmt hier der reguläre Scan-Lauf;
	// der Executor kennt den ServerService nicht (siehe dsmRefresh).
	completeRebootJob(e.jobs, e.servers, server, job, output, code, runErr, dial, nil)
}

// rebootRequiredNow fragt den Server LIVE, ob er einen Neustart anfordert -
// dieselbe Prüfung wie im Scan (/var/run/reboot-required, needs-restarting,
// zypper needs-rebooting), nur zum Zeitpunkt der Regel.
//
// Der gespeicherte Wert taugt hier nicht: Zwischen dem letzten Scan und dem
// Wartungsfenster liegt genau das Update, das den Neustart nötig macht.
func rebootRequiredNow(server *domain.Server, conn sshx.Conn) bool {
	return scanRebootRequired(server.PackageManager, func(_, cmd string) string {
		out, code, err := conn.Run(privRun(server, cmd))
		if err != nil || code != 0 {
			return ""
		}
		return out
	})
}
