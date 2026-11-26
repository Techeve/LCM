// Package agentcaps beantwortet an EINER Stelle, was der lcm-agent eines
// Servers kann: Jede Funktion, die einen neueren Agent braucht, fragt hier nach
// (Check), statt Fähigkeitsstrings oder Versionsnummern selbst auszuwerten.
//
// Grundlage sind die Angaben, die der Agent bei jedem Verbindungsaufbau im
// Inventar meldet: Version, Protokollversion und Fähigkeiten (wire.Inventory).
// LCM speichert sie am Server und fragt aus dem Gespeicherten genauso ab wie
// direkt nach dem Connect.
//
// Regeln:
//   - Mindestversion (MinAgentVersion): Ein Agent darunter wird nicht mehr
//     bedient. Check meldet dann für jede Funktion „nein“; übrig bleibt nur
//     „Agent aktualisieren“ (Report.UpdateRecommended).
//   - Fähigkeiten gehen vor Versionsnummern: Ein Agent, der die Fähigkeit
//     meldet, kann die Funktion, gleich was seine Version sagt (Selbstbauten
//     melden „0.0.0-dev“). Die Version dient nur der Mindestprüfung, dem
//     Vergleich mit LCM und dem Hinweis „Braucht Agent ≥ x.y“.
//   - Ein Agent, der neuer ist als LCM (Version oder Protokoll), bekommt keine
//     Sonderrechte: LCM bietet nur an, was es selbst kennt - die Tabelle
//     features ist die Obergrenze.
package agentcaps

import (
	"slices"
	"strings"

	"LCM/internal/remote/wire"
	"LCM/internal/version"
)

// MinAgentVersion ist die älteste Agent-Version, die LCM noch bedient
// (Entscheidung 23.11.2026). Darunter bleibt nur „Agent aktualisieren“.
const MinAgentVersion = "2.0.0"

// Feature ist eine Funktion, die von der Fähigkeit des Agents abhängt.
type Feature string

const (
	// FeatureConsole: Web-Konsole (Terminal im Browser).
	FeatureConsole Feature = "console"
	// FeatureLiveness: Lebendprüfung per Ping (der Agent baut seine Verbindung
	// selbst neu auf, wenn LCM nicht mehr antwortet).
	FeatureLiveness Feature = "liveness"
	// FeatureDurableJobs: Kommandos überstehen Verbindungsabbrüche und
	// Neustarts des Agents (Job-Ablage, siehe wire.CapabilityJobs).
	FeatureDurableJobs Feature = "durable-jobs"
	// FeatureSelfUpdate: Das Update des Agents ist abgesichert (Sperre gegen
	// apt upgrade, Rückfall auf das vorige Programm).
	FeatureSelfUpdate Feature = "self-update"
)

// Ab welcher Agent-Version eine Funktion da ist. Nur für den Hinweis „Braucht
// Agent ≥ x.y“; entschieden wird über die gemeldete Fähigkeit.
//
// Die Fassung von Liveness und Jobs ist die des Releases, das sie ausliefert;
// beim Vorbereiten des Release prüfen, dass sie stimmt.
const (
	sinceConsole = "2.2.0"
	sinceRobust  = "2.4.0"
)

// definition beschreibt, woran LCM eine Funktion beim Agent erkennt.
type definition struct {
	// capability meldet der Agent, wenn er die Funktion kann.
	capability string
	// disabledCapability (optional) meldet er, wenn er sie könnte, der
	// Betreiber sie aber am Server abgeschaltet hat.
	disabledCapability string
	// since ist die erste Agent-Version mit der Funktion.
	since string
}

// features ist die Gesamtheit der Funktionen, die LCM kennt und abhängig vom
// Agent anbietet.
var features = map[Feature]definition{
	FeatureConsole:     {capability: wire.CapabilityTTY, disabledCapability: wire.CapabilityTTYOff, since: sinceConsole},
	FeatureLiveness:    {capability: wire.CapabilityPing, since: sinceRobust},
	FeatureDurableJobs: {capability: wire.CapabilityJobs, since: sinceRobust},
	FeatureSelfUpdate:  {capability: wire.CapabilitySelfUpdate, since: sinceRobust},
}

// Features nennt alle Funktionen, die LCM kennt, in fester Reihenfolge.
func Features() []Feature {
	list := make([]Feature, 0, len(features))
	for f := range features {
		list = append(list, f)
	}
	slices.Sort(list)
	return list
}

// Agent ist, was LCM über einen Agent weiß.
type Agent struct {
	// Version ist die gemeldete Agent-Version (leer: noch nie gemeldet).
	Version string
	// Protocol ist die gemeldete Protokollversion; 0 und kleiner gelten als 1
	// (ältere Agenten schicken das Feld nicht).
	Protocol int
	// Capabilities sind die gemeldeten Fähigkeiten.
	Capabilities []string
	// RolledBack ist die Version, von der der Agent nach einem fehlgeschlagenen
	// Update zurückgerollt hat (leer: kein Rollback gemeldet).
	RolledBack string
	// RolledBackReason ist der Grund (crash-loop, not-ready; leer: unbekannt).
	RolledBackReason string
}

// FromInventory liest die Angaben aus dem Inventar eines frisch verbundenen Agents.
func FromInventory(inv wire.Inventory) Agent {
	return Agent{Version: inv.AgentVersion, Protocol: inv.ProtocolVersion, Capabilities: inv.Capabilities,
		RolledBack: inv.RolledBack, RolledBackReason: inv.RolledBackReason}
}

// FromStored liest die am Server gespeicherten Angaben (Fähigkeiten kommagetrennt).
func FromStored(agentVersion string, protocol int, capabilities string) Agent {
	a := Agent{Version: agentVersion, Protocol: protocol}
	for _, c := range strings.Split(capabilities, ",") {
		if c = strings.TrimSpace(c); c != "" {
			a.Capabilities = append(a.Capabilities, c)
		}
	}
	return a
}

// ProtocolVersion ist die Protokollversion des Agents (mindestens 1).
func (a Agent) ProtocolVersion() int {
	return max(a.Protocol, 1)
}

// Has meldet, ob der Agent die Fähigkeit gemeldet hat.
func (a Agent) Has(capability string) bool {
	return slices.Contains(a.Capabilities, capability)
}

// Reported meldet, ob der Agent sich überhaupt schon einmal gemeldet hat.
func (a Agent) Reported() bool {
	return a.Version != "" || len(a.Capabilities) > 0
}

// versionKnown meldet, ob die Version eine echte Nummer ist. Leer, „0.0.0-dev“
// und Unlesbares gelten als unbekannt: Selbstbauten werden weder als zu alt
// noch als zu neu behandelt, über sie entscheiden allein die Fähigkeiten.
func versionKnown(v string) bool {
	return version.Compare(v, "0.0.0") > 0
}

// BelowMinimum meldet, ob die Agent-Version unter MinAgentVersion liegt.
//
// Vorabversionen zählen wie ihr Final: Ein Agent aus den 2.0-Betas
// (2.0.0~beta.N) gilt als 2.0.0, nicht als zu alt.
func (a Agent) BelowMinimum() bool {
	return versionKnown(a.Version) && version.Compare(releaseOf(a.Version), MinAgentVersion) < 0
}

// releaseOf schneidet die Vorabkennung ab: 2.0.0~beta.3 wird 2.0.0.
func releaseOf(v string) string {
	v = version.Core(v)
	if i := strings.IndexAny(v, "~-"); i >= 0 {
		return v[:i]
	}
	return v
}

// Reason sagt, warum eine Funktion nicht angeboten wird.
type Reason string

const (
	// ReasonBelowMinimum: Der Agent ist älter als die Mindestversion.
	ReasonBelowMinimum Reason = "below_minimum"
	// ReasonOutdated: Der Agent kennt die Funktion noch nicht.
	ReasonOutdated Reason = "outdated"
	// ReasonDisabled: Der Betreiber hat die Funktion am Server abgeschaltet.
	ReasonDisabled Reason = "disabled"
)

// Support ist die Antwort auf „kann der Agent diese Funktion?“.
type Support struct {
	Supported bool   `json:"supported"`
	Reason    Reason `json:"reason,omitempty"`
	// NeedsVersion ist die Agent-Version, ab der es geht („Braucht Agent ≥ …“);
	// nur gesetzt, wenn ein Update helfen würde.
	NeedsVersion string `json:"needs_version,omitempty"`
}

// Check beantwortet, ob der Agent die Funktion kann. Eine Funktion, die LCM
// nicht kennt, kann kein Agent.
func (a Agent) Check(f Feature) Support {
	def, known := features[f]
	if !known {
		return Support{Reason: ReasonOutdated}
	}
	if a.BelowMinimum() {
		return Support{Reason: ReasonBelowMinimum, NeedsVersion: laterOf(def.since, MinAgentVersion)}
	}
	switch {
	case a.Has(def.capability):
		return Support{Supported: true}
	case def.disabledCapability != "" && a.Has(def.disabledCapability):
		return Support{Reason: ReasonDisabled}
	}
	return Support{Reason: ReasonOutdated, NeedsVersion: def.since}
}

// Supports ist die Kurzform von Check(f).Supported.
func (a Agent) Supports(f Feature) bool {
	return a.Check(f).Supported
}

func laterOf(a, b string) string {
	if version.Compare(a, b) >= 0 {
		return a
	}
	return b
}

// State ist die Einordnung des Agents gegenüber diesem LCM.
type State string

const (
	// StateUnknown: Der Agent hat sich noch nie gemeldet.
	StateUnknown State = ""
	// StateOK: Der Agent kann alles, was LCM von ihm braucht.
	StateOK State = "ok"
	// StateOutdated: Der Agent kennt Funktionen noch nicht, die LCM anbietet
	// (gelb).
	StateOutdated State = "outdated"
	// StateUnsupported: Der Agent ist älter als MinAgentVersion (stärker als
	// veraltet).
	StateUnsupported State = "unsupported"
	// StateNewer: Der Agent ist neuer als LCM (Version oder Protokoll).
	StateNewer State = "newer"
)

// Report ist die Einordnung eines Agents samt der Antwort je Funktion - die
// Grundlage für Ampel-Befund und Oberfläche.
type Report struct {
	State         State  `json:"state"`
	AgentVersion  string `json:"agent_version,omitempty"`
	ServerVersion string `json:"server_version"`
	MinVersion    string `json:"min_version"`
	Protocol      int    `json:"protocol"`
	// UpdateRecommended: Eine neuere Agent-Version würde etwas freischalten
	// (State outdated oder unsupported). Hier hängt später der Knopf „Agent
	// aktualisieren“ dran.
	UpdateRecommended bool `json:"update_recommended"`
	// NeedsVersion ist die höchste Version, die für die fehlenden Funktionen
	// nötig ist (leer, wenn nichts fehlt).
	NeedsVersion string `json:"needs_version,omitempty"`
	// Features nennt für jede Funktion, die LCM kennt, ob der Agent sie kann.
	Features map[Feature]Support `json:"features"`
	// Behind: Der Agent liegt (der Nummer nach) hinter diesem LCM. Ein Update
	// bringt ihn auf die höchste Version, die die Paketquelle des Servers führt
	// und die nicht über LCM liegt - der Knopf „Agent aktualisieren“ ist
	// deshalb auch bei einem sonst vollständigen Agent sinnvoll.
	Behind bool `json:"behind"`
	// RolledBack ist die Version, von der der Agent nach einem fehlgeschlagenen
	// Update zurückgerollt hat. Gesetzt heißt: Das letzte Update ging schief;
	// die Ampel zeigt es als Befund, der Agent läuft auf der vorigen Version.
	RolledBack string `json:"rolled_back,omitempty"`
	// RolledBackReason ist der Grund des Rollbacks (crash-loop, not-ready).
	RolledBackReason string `json:"rolled_back_reason,omitempty"`
}

// Assess ordnet den Agent gegenüber einem LCM der Version serverVersion ein.
func Assess(a Agent, serverVersion string) Report {
	r := Report{
		AgentVersion: a.Version, ServerVersion: serverVersion, MinVersion: MinAgentVersion,
		Protocol: a.ProtocolVersion(), Features: make(map[Feature]Support, len(features)),
		RolledBack: a.RolledBack, RolledBackReason: a.RolledBackReason,
	}
	for _, f := range Features() {
		s := a.Check(f)
		r.Features[f] = s
		if s.NeedsVersion != "" && version.Compare(s.NeedsVersion, r.NeedsVersion) > 0 {
			r.NeedsVersion = s.NeedsVersion
		}
	}
	switch {
	case !a.Reported():
		r.State = StateUnknown
	case a.BelowMinimum():
		r.State = StateUnsupported
	case a.newerThan(serverVersion):
		r.State = StateNewer
	case r.NeedsVersion != "":
		r.State = StateOutdated
	default:
		r.State = StateOK
	}
	r.UpdateRecommended = r.State == StateOutdated || r.State == StateUnsupported
	r.Behind = versionKnown(a.Version) && versionKnown(serverVersion) && version.Compare(a.Version, serverVersion) < 0
	return r
}

// newerThan meldet, ob der Agent neuer ist als LCM: ein höheres Protokoll oder
// eine höhere Feature-Reihe. Ein Bugfix derselben Reihe (Agent 2.3.4 unter LCM
// 2.3.0) ist kein Befund, den holt LCM selbst so heran (siehe PickTarget). Ist
// eine der Versionen nicht lesbar (Selbstbau), zählt nur das Protokoll.
func (a Agent) newerThan(serverVersion string) bool {
	return a.ProtocolVersion() > wire.ProtocolVersion || Ahead(a.Version, serverVersion)
}
