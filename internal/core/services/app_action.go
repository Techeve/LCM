package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
)

// Sicherung und Update einer erkannten Anwendung.
//
// Beides sind Eigene Aktionen, keine rohen Kommandozeilen am Katalogeintrag.
// Der Unterschied ist nicht kosmetisch: So laufen sie über denselben Weg wie
// jede andere Aktion - mit Rechteprüfung beim Anlegen, mit Job-Protokoll und
// mit einer Fundstelle, an der nachlesbar ist, was ausgeführt wurde.
//
// Die Reihenfolge ist bindend: Erst die Sicherung, dann das Update. Schlägt
// die Sicherung fehl, läuft das Update NICHT. Eine fehlgeschlagene Sicherung
// ist der Moment, in dem man ein Update am wenigsten gebrauchen kann.

const (
	// AppActionBackup führt nur die Sicherung aus.
	AppActionBackup = "backup"
	// AppActionUpdate führt das Update aus - mit vorheriger Sicherung, wenn
	// eine hinterlegt ist.
	AppActionUpdate = "update"
)

var (
	// ErrAppNotDetected: diese Anwendung wurde auf dem Server nicht gefunden.
	ErrAppNotDetected = errors.New("diese anwendung ist auf dem server nicht erkannt")
	// ErrAppNoAction: für die gewünschte Sache ist keine Aktion hinterlegt.
	ErrAppNoAction = errors.New("für diese anwendung ist keine passende aktion hinterlegt")
)

// AppActionPlan ist der Ablauf, den RunAppAction ausführen wird - getrennt
// ermittelt, damit die Prüfungen ohne Verbindung stattfinden.
type appActionPlan struct {
	entry   *domain.AppCatalogEntry
	backup  *domain.CustomAction
	update  *domain.CustomAction
	appName string
}

// PlanAppAction prüft, ob sich die gewünschte Aktion ausführen lässt.
func (s *AppService) planAppAction(serverID uint, slug, kind string) (*appActionPlan, error) {
	entry, err := s.apps.FindBySlug(slug)
	if err != nil {
		return nil, err
	}
	detected, err := s.apps.DetectedFor(serverID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, d := range detected {
		if d.Slug == slug {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrAppNotDetected, entry.Name)
	}

	plan := &appActionPlan{entry: entry, appName: entry.Name}
	if entry.BackupActionID != nil && s.actions != nil {
		if action, err := s.actions.FindByID(*entry.BackupActionID); err == nil {
			plan.backup = action
		}
	}
	if kind == AppActionUpdate {
		if entry.UpdateActionID == nil || s.actions == nil {
			return nil, fmt.Errorf("%w: %s", ErrAppNoAction, entry.Name)
		}
		action, err := s.actions.FindByID(*entry.UpdateActionID)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrAppNoAction, entry.Name)
		}
		plan.update = action
		return plan, nil
	}
	if plan.backup == nil {
		return nil, fmt.Errorf("%w: %s", ErrAppNoAction, entry.Name)
	}
	return plan, nil
}

// steps ist die auszuführende Folge - Sicherung zuerst.
func (p *appActionPlan) steps(withBackup bool) []*domain.CustomAction {
	var out []*domain.CustomAction
	if withBackup && p.backup != nil {
		out = append(out, p.backup)
	}
	if p.update != nil {
		out = append(out, p.update)
	}
	return out
}

// RunAppAction führt Sicherung und/oder Update einer erkannten Anwendung aus.
// Läuft als Job, damit das Protokoll dort landet, wo alle anderen auch stehen.
func (e *Executor) RunAppAction(server *domain.Server, slug, kind string, withBackup bool, triggeredBy string) (*domain.Job, error) {
	if e.apps == nil {
		return nil, errors.New("anwendungskatalog nicht verdrahtet")
	}
	plan, err := e.apps.planAppAction(server.ID, slug, kind)
	if err != nil {
		return nil, err
	}
	steps := plan.steps(withBackup)
	if len(steps) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrAppNoAction, plan.appName)
	}

	name := plan.appName + ": " + appActionLabel(kind, withBackup && plan.backup != nil)
	job, err := e.jobs.Start(&server.ID, nil, domain.RuleTypeCustom, name, triggeredBy)
	if err != nil {
		return nil, err
	}
	go e.runAppActionJob(job, server, plan, steps, triggeredBy)
	return job, nil
}

func appActionLabel(kind string, mitSicherung bool) string {
	if kind == AppActionBackup {
		return "Sicherung"
	}
	if mitSicherung {
		return "Sicherung und Update"
	}
	return "Update"
}

func (e *Executor) runAppActionJob(job *domain.Job, server *domain.Server, plan *appActionPlan, steps []*domain.CustomAction, triggeredBy string) {
	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "app:" + plan.entry.Slug, Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	var log strings.Builder
	fmt.Fprintf(&log, "Anwendung %q\n", plan.appName)
	var runErr error
	for _, action := range steps {
		commands := parseActionCommands(action.Commands)
		fmt.Fprintf(&log, "\n== %s - %d Kommando(s)\n", action.Name, len(commands))
		if len(commands) == 0 {
			continue
		}
		if runErr = runActionCommands(conn, server, &log, commands); runErr != nil {
			log.WriteString("\nAbgebrochen - die folgenden Schritte laufen nicht mehr.\n")
			break
		}
	}
	e.finishWithHealth(job, server, log.String(), runErr)
}
