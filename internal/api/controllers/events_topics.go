package controllers

import (
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"LCM/internal/api/apierr"
	"LCM/internal/core/domain"
	"LCM/internal/core/events"
	"LCM/internal/storage/repositories"
)

// Themen des Ereigniskanals. Ein Thema sagt, WAS ein Tab sehen will; ob er es
// sehen DARF, entscheidet das Backend für jedes Ereignis einzeln (access).
const (
	topicServers   = "servers"   // server.busy und server.status aller sichtbaren Server
	topicServer    = "server"    // server:<id> - alle Ereignisse dieses Servers
	topicJobs      = "jobs"      // Job-Ereignisse der sichtbaren Server
	topicJob       = "job"       // job:<id> - Ereignisse dieses Jobs
	topicAlerts    = "alerts"    // ausgelöste Alarme
	topicBulk      = "bulk"      // Sammel-Update
	topicSystem    = "system"    // Version und Selbst-Update
	topicDashboard = "dashboard" // alles, was das Dashboard verändert
)

var (
	errBadTopic       = apierr.Define(400, "events.bad_topic", "unknown or malformed topic")
	errForbiddenTopic = apierr.Define(403, "events.forbidden_topic", "topic not available")
	errTooManyTopics  = apierr.Define(400, "events.too_many_topics", "too many topics on this connection")
	errBadMessage     = apierr.Define(400, "events.bad_message", "invalid message")
)

// topic ist ein geparstes Thema.
type topic struct {
	kind     string
	serverID uint   // bei server:<id>
	jobID    string // bei job:<id>
}

// parseTopic liest ein Thema aus seiner Textform ("servers", "server:42" …).
func parseTopic(raw string) (topic, *apierr.Error) {
	name, arg, hasArg := strings.Cut(raw, ":")
	switch name {
	case topicServers, topicJobs, topicAlerts, topicBulk, topicSystem, topicDashboard:
		if hasArg {
			return topic{}, errBadTopic
		}
		return topic{kind: name}, nil
	case topicServer:
		id, err := strconv.ParseUint(arg, 10, 32)
		if err != nil || id == 0 {
			return topic{}, errBadTopic
		}
		return topic{kind: name, serverID: uint(id)}, nil
	case topicJob:
		if _, err := uuid.Parse(arg); err != nil || len(arg) != 36 {
			return topic{}, errBadTopic
		}
		return topic{kind: name, jobID: arg}, nil
	}
	return topic{}, errBadTopic
}

// String ist die Textform, wie sie der Client kennt.
func (t topic) String() string {
	switch t.kind {
	case topicServer:
		return topicServer + ":" + strconv.FormatUint(uint64(t.serverID), 10)
	case topicJob:
		return topicJob + ":" + t.jobID
	}
	return t.kind
}

// specific ist true für Themen, die auf genau einen Server oder Job zeigen.
// Trifft ein Ereignis mehrere abonnierte Themen, trägt der Rahmen das
// spezifischste.
func (t topic) specific() bool { return t.kind == topicServer || t.kind == topicJob }

// matches sagt, ob ein Ereignis zu diesem Thema gehört - unabhängig davon, ob
// der Benutzer es sehen darf.
func (t topic) matches(e events.Event) bool {
	switch t.kind {
	case topicServers:
		return e.Type == events.TypeServerBusy || e.Type == events.TypeServerStatus
	case topicServer:
		return e.ServerID == t.serverID || (e.All && e.Type == events.TypeServerStatus)
	case topicJobs:
		return e.Type == events.TypeJobProgress || e.Type == events.TypeJobFinished || e.Type == events.TypeServerBusy
	case topicJob:
		return e.JobID == t.jobID
	case topicAlerts:
		return e.Type == events.TypeAlertFired
	case topicBulk:
		return e.Type == events.TypeBulkProgress
	case topicSystem:
		return e.Type == events.TypeSystemBuild
	case topicDashboard:
		return e.Type == events.TypeServerStatus || e.Type == events.TypeJobFinished || e.Type == events.TypeAlertFired
	}
	return false
}

// subscribePermission ist das Recht, das ein Thema voraussetzt. Es entscheidet
// nur darüber, ob das Abonnieren angenommen wird (und der Client eine Antwort
// bekommt); durchgesetzt wird beim Zustellen jedes Ereignisses.
func (t topic) subscribePermission() string {
	switch t.kind {
	case topicJobs, topicJob:
		return domain.PermJobsRead
	case topicAlerts:
		return domain.PermAlertsManage
	case topicBulk:
		return domain.PermPackagesRead
	case topicSystem:
		return domain.PermSettingsManage
	}
	return domain.PermServersRead
}

// eventPermission ist das Recht, das nötig ist, um ein Ereignis über dieses
// Thema zu sehen. Es folgt den REST-Endpunkten, aus denen der Tab die Einzelheiten
// nachlädt: Was dort 403 wäre, wird auch hier nicht gemeldet.
func eventPermission(t topic, eventType string) string {
	switch eventType {
	case events.TypeJobProgress, events.TypeJobFinished:
		// Das Dashboard zeigt die letzten Jobs mit dem Recht auf die Serverliste.
		if t.kind == topicDashboard {
			return domain.PermServersRead
		}
		return domain.PermJobsRead
	case events.TypeAlertFired:
		return domain.PermAlertsManage
	case events.TypeBulkProgress:
		return domain.PermPackagesRead
	case events.TypeSystemBuild:
		return domain.PermSettingsManage
	}
	return domain.PermServersRead
}

// access ist die Sicht eines Benutzers auf Rechte und Server zu einem
// Zeitpunkt. Ein access wird nie verändert, nur ersetzt - so lesen es die
// Goroutinen einer Verbindung ohne Sperre.
type access struct {
	user    *domain.User
	scope   repositories.AccessScope
	visible map[uint]struct{} // sichtbare Server; unbenutzt bei scope.All
	loaded  time.Time         // Zeitpunkt, an dem visible geladen wurde
}

func (a *access) canSee(serverID uint) bool {
	if a.scope.All {
		return true
	}
	_, ok := a.visible[serverID]
	return ok
}

// deliverable ist die Mandantentrennung für Ereignisse: Das Thema muss passen,
// der Benutzer braucht das Recht auf diese Art von Ereignis, und der Server des
// Ereignisses muss in seinem Sichtbereich liegen.
func (a *access) deliverable(t topic, e events.Event) bool {
	return t.matches(e) &&
		a.user.HasPermission(eventPermission(t, e.Type)) &&
		a.visibleTo(e)
}

// visibleTo prüft nur den Sichtbereich.
func (a *access) visibleTo(e events.Event) bool {
	switch {
	case e.All:
		return true
	case e.ServerID != 0:
		return a.canSee(e.ServerID)
	case isJobEvent(e.Type) || e.Type == events.TypeAlertFired:
		// Ohne Server (System-Job, Selbstbeobachtung) sieht es nur, wer alles sieht.
		return a.scope.All
	}
	return true // Sammel-Update und System: das Recht entscheidet
}

func isJobEvent(eventType string) bool {
	return eventType == events.TypeJobProgress || eventType == events.TypeJobFinished
}
