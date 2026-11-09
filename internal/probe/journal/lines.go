package journal

import (
	"encoding/json"

	"LCM/internal/probe/host"
)

// StartLine steht am Anfang jedes Laufs von lcm-probe.
type StartLine struct {
	Head
	Version      string `json:"version"`
	PID          int    `json:"pid"`
	Hostname     string `json:"hostname"`
	BootID       string `json:"boot_id,omitempty"` // im Container die des Hosts
	IntervalMS   int64  `json:"interval_ms"`
	DBIntervalMS int64  `json:"db_interval_ms"`
	URL          string `json:"url"`
}

// SourcesLine nennt die Herkunft der Hostwerte.
type SourcesLine struct {
	Head
	Sources host.Sources `json:"sources"`
}

// HostLine sind die Hostwerte eines Intervalls. DT ist die Länge des
// Intervalls in Millisekunden nach der monotonen Uhr.
type HostLine struct {
	Head
	DT int64 `json:"dt"`
	host.Sample
	Self Self `json:"self"`
}

// Self ist der eigene Verbrauch von lcm-probe.
type Self struct {
	CPUMS   float64 `json:"cpu_ms"`  // CPU-Zeit im Intervall
	RSS     uint64  `json:"rss"`     // Bytes
	SyncMS  float64 `json:"sync_ms"` // Dauer des letzten fdatasync
	Written uint64  `json:"written"` // geschriebene Bytes seit dem Start
}

// DBLine ist eine Momentaufnahme von GET /api/v1/system/db.
type DBLine struct {
	Head
	FetchMS float64         `json:"fetch_ms"`
	DB      json.RawMessage `json:"db"`
}

// DownLine: LCM war nicht erreichbar oder antwortete mit einem Fehler.
type DownLine struct {
	Head
	Error string `json:"error"`
}

// StopLine: lcm-probe wurde sauber beendet. Fehlt sie vor einem Start, ist
// der Lauf abgebrochen (Absturz, Stillstand, harter Neustart).
type StopLine struct {
	Head
	Signal string `json:"signal"`
}
