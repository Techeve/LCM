package domain

import "time"

// Ursachen, mit denen LCM die Trennung eines Agents verbucht (Hub, nur im
// Speicher). Die ersten drei sind ungewollt, der Rest gewollt.
const (
	DropPing     = "ping"     // Keepalive des Agents blieb aus (halb offene Verbindung)
	DropNetwork  = "network"  // Verbindungs- oder Netzfehler, Abbruch ohne Abschied
	DropTakeover = "takeover" // der Agent verband sich neu, die alte Verbindung galt noch als offen

	DropAgent     = "agent"      // der Agent hat sich selbst abgemeldet (DISCONNECT)
	DropLCM       = "lcm"        // LCM wurde beendet
	DropRevoked   = "revoked"    // Token widerrufen oder Server gelöscht
	DropAccessOff = "access_off" // Agent-Zugang ausgeschaltet
)

// AgentUnstableDrops: Mehr als so viele ungewollte Trennungen in AgentLinkWindow
// machen die Verbindung zum „instabilen“ Befund der Ampel.
const (
	AgentUnstableDrops = 5
	AgentLinkWindow    = time.Hour
)

// UnintendedDrop sagt, ob die Ursache eine ungewollte Trennung beschreibt.
func UnintendedDrop(cause string) bool {
	switch cause {
	case DropPing, DropNetwork, DropTakeover:
		return true
	}
	return false
}

// AgentLink ist die Verbindungsbilanz eines Agents seit dem Start von LCM
// (nur im Speicher, ein Neustart von LCM setzt sie zurück).
type AgentLink struct {
	Connects    int `json:"connects"`
	Disconnects int `json:"disconnects"`
	// UnintendedLastHour zählt die ungewollten Trennungen der letzten Stunde.
	UnintendedLastHour int `json:"unintended_last_hour"`
	// LastCause ist die Ursache der jüngsten ungewollten Trennung in dieser
	// Stunde (leer, wenn es keine gab), LastCauseAt ihr Zeitpunkt.
	LastCause   string     `json:"last_cause,omitempty"`
	LastCauseAt *time.Time `json:"last_cause_at,omitempty"`
}

// Unstable meldet, ob die Verbindung so oft ungewollt abriss, dass die Ampel es
// zeigen soll.
func (l *AgentLink) Unstable() bool {
	return l != nil && l.UnintendedLastHour > AgentUnstableDrops
}

// dropLabel ist die deutsche Beschreibung der Ursache für Texte ohne
// Sprachkatalog (API, Benachrichtigungen); die Oberfläche übersetzt den Schlüssel.
func dropLabel(cause string) string {
	switch cause {
	case DropPing:
		return "Lebenszeichen blieb aus"
	case DropNetwork:
		return "Verbindungs- oder Netzfehler"
	case DropTakeover:
		return "Agent verband sich neu, bevor die alte Verbindung als tot erkannt war"
	}
	return cause
}
