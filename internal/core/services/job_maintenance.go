package services

import (
	"errors"
	"log/slog"
	"strings"

	"LCM/internal/safego"
)

// Wartung: Während LCM sich selbst aktualisiert, startet kein neuer Job. Der
// Neustart am Ende des Updates schnitte ihn sonst mitten im Lauf ab - auf
// einem fremden Server, der danach halb aktualisiert dasteht.
//
// Unmittelbare Aktionen lehnt die Sperre ab (wer davorsteht, bekommt eine
// klare Antwort). Zeitplan-Läufe reihen sich ein und warten in ihrer
// Warteschlange, bis die Sperre fällt oder ihr eigener Takt abgelaufen ist.
// Startet LCM dabei neu, schließt FailInterruptedOnStartup sie als „nicht
// ausgeführt" ab; ihr Zeitplan holt sie im nächsten Durchgang nach.
//
// Laufende Jobs bleiben unberührt: Auf sie wartet das Selbst-Update.

// ErrMaintenance: LCM wird gerade aktualisiert, neue Jobs starten erst danach.
var ErrMaintenance = errors.New("lcm wird gerade aktualisiert - neue jobs starten erst nach dem update")

// heldBack meldet einen Start, der nur aufgehalten wurde - weil der Server
// belegt ist oder LCM gerade aktualisiert. Beides ist kein Fehler des Jobs.
func heldBack(err error) bool {
	return errors.Is(err, ErrServerBusy) || errors.Is(err, ErrMaintenance)
}

// logStartFailure protokolliert einen gescheiterten Start eines System-Jobs.
// Hält ihn nur die Wartung auf, ist das kein Fehler, sondern Absicht.
func logStartFailure(kind, name string, err error) {
	if errors.Is(err, ErrMaintenance) {
		slog.Info(kind+": held back while lcm is updating", "job", name)
		return
	}
	slog.Error(kind+": job start failed", "job", name, "error", err)
}

// maintenancePass erlaubt während der Wartung genau einen Job: den Update-Lauf
// selbst. Er wird beim Start verbraucht. Der Name bindet ihn an genau diesen
// Lauf - ein Zeitplan-Update auf dem LCM-Host derselben Art käme sonst mit
// durch und nähme dem Selbst-Update den Schein weg.
type maintenancePass struct {
	serverID uint
	jobType  string
	name     string
}

// BeginMaintenance sperrt den Start neuer Jobs.
func (s *JobService) BeginMaintenance() {
	s.mu.Lock()
	s.maintenance = true
	s.pass = nil
	s.mu.Unlock()
	slog.Info("maintenance: new jobs are held until the update is done")
}

// EndMaintenance hebt die Sperre auf und gibt jeden Server mit wartenden
// Zeitplan-Läufen an den nächsten weiter. Die Weitergabe läuft im
// Hintergrund: Der Aufrufer (das Selbst-Update) hält dabei seine eigene
// Sperre, und an der hängt jede schreibende Anfrage.
func (s *JobService) EndMaintenance() {
	s.mu.Lock()
	if !s.maintenance {
		s.mu.Unlock()
		return
	}
	s.maintenance = false
	s.pass = nil
	waiting := make([]uint, 0, len(s.queues))
	for serverID, queue := range s.queues {
		if len(queue) > 0 {
			waiting = append(waiting, serverID)
		}
	}
	s.mu.Unlock()
	slog.Info("maintenance ended", "servers_with_queued_jobs", len(waiting))
	safego.Go("maintenance:release-queues", func() {
		for _, serverID := range waiting {
			s.startNext(serverID)
		}
	})
}

// InMaintenance meldet, ob die Sperre gerade gilt.
func (s *JobService) InMaintenance() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maintenance
}

// PermitDuringMaintenance lässt den nächsten unmittelbaren Start dieser Art auf
// diesem Server trotz Sperre zu - einmal und nur, wenn sein Name mit name
// beginnt (die Paket-Aktion hängt „ @ <Server>" an). Damit kommt der
// Update-Lauf durch, alles andere nicht.
func (s *JobService) PermitDuringMaintenance(serverID uint, jobType, name string) {
	s.mu.Lock()
	s.pass = &maintenancePass{serverID: serverID, jobType: jobType, name: name}
	s.mu.Unlock()
}

// heldByMaintenanceLocked entscheidet, ob die Sperre diesen unmittelbaren
// Start aufhält, und verbraucht dabei den Passierschein. Zeitplan-Läufe
// (enqueue) fragen s.maintenance direkt: Für sie gilt kein Schein. Erwartet
// den gehaltenen Mutex.
func (s *JobService) heldByMaintenanceLocked(serverID *uint, jobType, name string) bool {
	if !s.maintenance {
		return false
	}
	p := s.pass
	if p != nil && serverID != nil && *serverID == p.serverID && jobType == p.jobType && strings.HasPrefix(name, p.name) {
		s.pass = nil
		return false
	}
	return true
}
