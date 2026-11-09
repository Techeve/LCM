package events

// Nutzlasten der Ereignisse, wie sie als JSON zum Browser gehen. Sie tragen
// nur Kennungen und Zustände - nie Rohausgaben oder Namen: Wer mehr wissen
// will, fragt die REST-API, und die prüft die Rechte.

// ServerBusyData: ein Job auf dem Server ist gestartet, beendet oder reiht sich ein.
type ServerBusyData struct {
	ServerID uint `json:"server_id"`
	// Busy: auf dem Server läuft gerade ein Job.
	Busy bool `json:"busy"`
	// JobID und Kind gehören zu dem Job, der das Ereignis ausgelöst hat.
	JobID string `json:"job_id"`
	Kind  string `json:"kind"`
	// Queued: Zahl der Zeitplan-Läufe, die auf dem Server warten.
	Queued int `json:"queued"`
}

// ServerBusy baut das Ereignis server.busy.
func ServerBusy(d ServerBusyData) Event {
	return Event{Type: TypeServerBusy, ServerID: d.ServerID, JobID: d.JobID, Data: d}
}

// ServerStatusData: die Ampel eines Servers (oder aller) ist neu zu holen.
type ServerStatusData struct {
	ServerID *uint `json:"server_id,omitempty"`
	All      bool  `json:"all,omitempty"`
}

// ServerStatus baut das Ereignis server.status; nil heißt „alle Server".
func ServerStatus(serverID *uint) Event {
	if serverID == nil {
		return Event{Type: TypeServerStatus, All: true, Data: ServerStatusData{All: true}}
	}
	return Event{Type: TypeServerStatus, ServerID: *serverID, Data: ServerStatusData{ServerID: serverID}}
}

// JobProgressData: der laufende Job hat neue Ausgabe.
type JobProgressData struct {
	JobID    string `json:"job_id"`
	ServerID *uint  `json:"server_id"`
}

// JobProgress baut das Ereignis job.progress.
func JobProgress(jobID string, serverID *uint) Event {
	return Event{Type: TypeJobProgress, ServerID: idOf(serverID), JobID: jobID,
		Data: JobProgressData{JobID: jobID, ServerID: serverID}}
}

// JobFinishedData: der Job hat einen Endzustand erreicht.
type JobFinishedData struct {
	JobID    string `json:"job_id"`
	ServerID *uint  `json:"server_id"`
	Status   string `json:"status"`
}

// JobFinished baut das Ereignis job.finished.
func JobFinished(jobID string, serverID *uint, status string) Event {
	return Event{Type: TypeJobFinished, ServerID: idOf(serverID), JobID: jobID,
		Data: JobFinishedData{JobID: jobID, ServerID: serverID, Status: status}}
}

// AlertFiredData: ein Alarm wurde ausgelöst. Bewusst ohne Beschreibung -
// die enthält Rohausgaben und steht im Alarm-Verlauf der REST-API.
type AlertFiredData struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	ServerID *uint  `json:"server_id"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
}

// AlertFired baut das Ereignis alert.fired.
func AlertFired(d AlertFiredData) Event {
	return Event{Type: TypeAlertFired, ServerID: idOf(d.ServerID), Data: d}
}

// BulkProgressData: Stand des Sammel-Updates. Ohne Servernamen - das Update
// läuft über alle Server, die Anzeige sieht aber nur der jeweilige Mandant.
type BulkProgressData struct {
	Running   bool `json:"running"`
	Total     int  `json:"total"`
	Completed int  `json:"completed"`
	Failed    int  `json:"failed"`
}

// BulkProgress baut das Ereignis bulk.progress.
func BulkProgress(d BulkProgressData) Event {
	return Event{Type: TypeBulkProgress, Data: d}
}

// SystemBuildData: Stand des Selbst-Updates von LCM.
type SystemBuildData struct {
	Phase         string   `json:"phase"`
	TargetVersion string   `json:"target_version,omitempty"`
	WaitingFor    []string `json:"waiting_for,omitempty"`
	JobID         string   `json:"job_id,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// SystemBuild baut das Ereignis system.build.
func SystemBuild(d SystemBuildData) Event {
	return Event{Type: TypeSystemBuild, Data: d}
}

// SystemMaintenanceData: ob LCM gerade wegen des Selbst-Updates gesperrt ist.
type SystemMaintenanceData struct {
	Active        bool   `json:"active"`
	Phase         string `json:"phase"`
	TargetVersion string `json:"target_version,omitempty"`
}

// SystemMaintenance baut das Ereignis system.maintenance.
func SystemMaintenance(d SystemMaintenanceData) Event {
	return Event{Type: TypeSystemMaintenance, Data: d}
}

func idOf(id *uint) uint {
	if id == nil {
		return 0
	}
	return *id
}
