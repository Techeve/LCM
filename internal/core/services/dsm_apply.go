package services

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/synology"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// JobTypeDSMApply: DSM-Sicherheitseinstellungen auf den Sollwert setzen.
const JobTypeDSMApply = "dsm-apply"

var (
	// ErrNotDSM: Die Aktion gibt es nur für Synology DSM.
	ErrNotDSM = errors.New("nur für Synology DSM verfügbar")
	// ErrDSMNotSettable: Diese Einstellung setzt LCM nicht (unbekannt oder
	// die Firewall, siehe domain.DSMSettable).
	ErrDSMNotSettable = errors.New("diese DSM-Einstellung setzt LCM nicht")
	// ErrDSMNoTargets: keine Einstellung gewählt.
	ErrDSMNoTargets = errors.New("keine DSM-Einstellung gewählt")
)

// ApplyDSMTargets setzt die genannten Sicherheitseinstellungen eines
// DSM-Geräts auf ihren Sollwert, alle in einem Job. Danach erfasst der Job
// das Gerät neu, damit Prüfungen und Befunde den neuen Stand zeigen.
func (s *ServerService) ApplyDSMTargets(scope repositories.AccessScope, id uint, keys []string, actor string) (*domain.Job, error) {
	keys, err := dsmTargetKeys(keys)
	if err != nil {
		return nil, err
	}
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if !server.IsSynologyDSM() {
		return nil, ErrNotDSM
	}
	job, err := s.jobs.Start(&server.ID, nil, JobTypeDSMApply, "DSM-Sollwerte setzen @ "+server.Name, actor)
	if err != nil {
		return nil, err // u.a. ErrServerBusy → 409
	}
	s.audit.Log(actor, "server.dsm-apply", "server", id, server.Name+": "+strings.Join(keys, ", "))
	safego.GoCleanup("job:dsm-apply", jobPanicCleanup(s.jobs, job), func() {
		s.runDSMApply(job, server, keys)
	})
	return job, nil
}

// dsmTargetKeys prüft die Schlüssel gegen die setzbaren und ordnet sie wie
// die DSM-Systemsteuerung, ohne Doppelte.
func dsmTargetKeys(keys []string) ([]string, error) {
	for _, key := range keys {
		if !domain.IsDSMSettable(key) {
			return nil, fmt.Errorf("%w: %s", ErrDSMNotSettable, key)
		}
	}
	var out []string
	for _, key := range domain.DSMSettable {
		if slices.Contains(keys, key) {
			out = append(out, key)
		}
	}
	if len(out) == 0 {
		return nil, ErrDSMNoTargets
	}
	return out, nil
}

// runDSMApply setzt Schlüssel für Schlüssel. Scheitert einer, geht es mit
// dem nächsten weiter; der Job gilt dann als fehlgeschlagen.
func (s *ServerService) runDSMApply(job *domain.Job, server *domain.Server, keys []string) {
	if server.IsDemo {
		s.jobs.CompleteSkipped(job, "Demo-Gerät - DSM-Sollwerte nicht gesetzt, kein Kontakt zum Gerät.")
		return
	}
	client, err := s.dsmClient(server)
	if err != nil {
		s.jobs.Complete(job, "", nil, err)
		return
	}
	defer client.Logout()

	var log strings.Builder
	failed := applyDSMKeys(client, server.DSMChecks, keys, &log)
	info, checks, err := s.collectDSM(server, client)
	if err != nil {
		s.jobs.Complete(job, log.String(), nil, fmt.Errorf("gerät neu erfassen: %w", err))
		return
	}
	log.WriteString("\nNeu erfasst:\n")
	for _, key := range keys {
		fmt.Fprintf(&log, "  %-12s %s\n", key, dsmCheckState(checks, key))
	}
	log.WriteString("\n" + dsmSettingsLog(info.Settings, checks))
	if failed > 0 {
		s.jobs.Complete(job, log.String(), nil, fmt.Errorf("%d von %d Einstellungen nicht gesetzt", failed, len(keys)))
		return
	}
	s.jobs.Complete(job, log.String(), ptrInt(0), nil)
}

// applyDSMKeys setzt die Schlüssel und schreibt je Schlüssel den Stand
// vorher und das Ergebnis ins Protokoll. Liefert die Zahl der Fehlschläge.
func applyDSMKeys(client *synology.Client, before domain.DSMChecks, keys []string, log *strings.Builder) int {
	failed := 0
	for _, key := range keys {
		fmt.Fprintf(log, "%s: vorher %s\n", key, dsmCheckState(before, key))
		note, err := client.ApplyTarget(key)
		if err != nil {
			failed++
			fmt.Fprintf(log, "  fehlgeschlagen: %v\n", err)
			continue
		}
		fmt.Fprintf(log, "  %s\n", note)
	}
	return failed
}

// dsmCheckState beschreibt den Stand einer Prüfung in einer Zeile.
func dsmCheckState(checks domain.DSMChecks, key string) string {
	for _, c := range checks {
		if c.Key != key {
			continue
		}
		switch c.State {
		case domain.DSMCheckOK:
			return c.Value + " - Soll erfüllt"
		case domain.DSMCheckDeviation:
			return c.Value + " - weicht vom Soll ab"
		}
		return "unbekannt"
	}
	return "nicht gelesen"
}
