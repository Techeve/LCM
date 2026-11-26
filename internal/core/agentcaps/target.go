package agentcaps

import (
	"strings"

	"LCM/internal/version"
)

// Zielversion eines Agent-Updates.
//
// Es gibt EINEN Agent für alle Kanäle: das Paket lcm-agent aus der Community-
// Quelle (dazu die Beta-Suite, wenn der Server sie eingerichtet hat). Auch die
// Server eines Enterprise-LCM nutzen ihn, denn der Enterprise-Kanal führt kein
// lcm-agent. Ein Enterprise-LCM kann deshalb hinter dem Community-Stand
// zurückliegen.
//
// Darum bestimmt LCM die Zielversion, nicht apt: die höchste Version, die der
// Server zur Auswahl hat und die zur Feature-Reihe dieses LCM passt.
//
//   - Ein stabiles LCM nimmt Finale bis zu seiner Feature-Reihe (Major.Feature)
//     samt aller Bugfixes darin: LCM 2.3.0 zieht den Agent auf 2.3.7, nie auf
//     2.4.0. So erreichen Agent-Fixes auch Server, deren LCM (etwa im
//     Enterprise-Kanal) den Fix selbst noch nicht hat.
//   - Ein stabiles LCM nimmt nie eine Vorabversion (Beta, RC), auch keine, die
//     numerisch darunter liegt: 2.3.0~beta.4 ist kein Stand, den ein stabiles
//     2.3.0 für sich gutheißt.
//   - Ein LCM in einer Vorabversion nimmt Vorabversionen und Finale bis genau
//     zu seiner eigenen Version: Zwischen zwei Betas kann sich mehr ändern als
//     ein Bugfix.
//   - limit begrenzt zusätzlich auf eine feste Version (gestaffelte Läufe: der
//     Stand, den der erste Server erreicht hat).
//   - Es gibt kein Downgrade: Ist der Agent bereits auf oder über der besten
//     Wahl, bleibt er stehen. Liegt er in einer höheren Feature-Reihe als LCM,
//     heißt das „neuer als LCM“ - dort hilft ein LCM-Update, kein Agent-Rückbau.

// Action sagt, was ein Update des Agents tun würde.
type Action string

const (
	// ActionUpdate: Es gibt eine passende neuere Version.
	ActionUpdate Action = "update"
	// ActionCurrent: Der Agent steht auf der besten Version, die LCM zulässt.
	ActionCurrent Action = "current"
	// ActionNewer: Der Agent ist neuer als LCM.
	ActionNewer Action = "newer"
	// ActionNone: Keine Version zur Auswahl, die LCM zulässt, oder LCM kennt
	// seine eigene Version nicht (Entwicklungsstand).
	ActionNone Action = "none"
)

// Selection ist das Ergebnis von PickTarget.
type Selection struct {
	Action Action `json:"action"`
	// Installed ist die installierte Agent-Version (leer: unbekannt).
	Installed string `json:"installed,omitempty"`
	// Target ist die gewählte Zielversion, wie sie die Paketquelle schreibt
	// (z. B. 2.4.0~beta.3); nur bei ActionUpdate und ActionCurrent gesetzt.
	Target string `json:"target,omitempty"`
	// Ceiling ist die LCM-Version, gegen die gewählt wurde.
	Ceiling string `json:"ceiling,omitempty"`
	// Limit ist die zusätzliche feste Obergrenze (leer: keine).
	Limit string `json:"limit,omitempty"`
}

// IsPrerelease meldet, ob die Version eine Vorabversion ist (Debian-Tilde
// „2.4.0~beta.1“ oder SemVer-Bindestrich „2.4.0-rc1“).
func IsPrerelease(v string) bool {
	return strings.ContainsAny(version.Core(v), "~-")
}

// PickTarget wählt die Zielversion aus available (die Versionen, die die
// Paketquelle des Servers führt, in beliebiger Reihenfolge). lcm ist die
// Version dieses LCM, limit eine zusätzliche feste Obergrenze (leer: keine).
func PickTarget(installed string, available []string, lcm, limit string) Selection {
	sel := Selection{Installed: installed, Ceiling: lcm, Limit: limit}
	if !versionKnown(lcm) {
		sel.Action = ActionNone
		return sel
	}
	if Ahead(installed, lcm) {
		sel.Action = ActionNewer
		return sel
	}
	best := ""
	for _, v := range available {
		v = strings.TrimSpace(v)
		if !eligible(v, lcm, limit) {
			continue
		}
		if best == "" || version.Compare(v, best) > 0 {
			best = v
		}
	}
	switch {
	case best == "":
		sel.Action = ActionNone
	case !versionKnown(installed) || version.Compare(best, installed) > 0:
		sel.Action, sel.Target = ActionUpdate, best
	default:
		// Der Agent steht auf oder über der besten Wahl: Er bleibt.
		sel.Action, sel.Target = ActionCurrent, installed
	}
	return sel
}

// Ahead meldet, ob der Agent über dem liegt, was LCM zulässt: bei einem
// stabilen LCM eine höhere Feature-Reihe (2.4.0 unter LCM 2.3.x; ein Bugfix
// derselben Reihe ist nicht neuer), bei einem LCM in einer Vorabversion jede
// höhere Version.
func Ahead(agent, lcm string) bool {
	if !versionKnown(agent) || !versionKnown(lcm) {
		return false
	}
	if IsPrerelease(lcm) {
		return version.Compare(agent, lcm) > 0
	}
	return version.FeatureBehind(lcm, agent)
}

// eligible meldet, ob LCM der Version lcm die Version v zulässt.
func eligible(v, lcm, limit string) bool {
	if !versionKnown(v) {
		return false
	}
	if limit != "" && version.Compare(v, limit) > 0 {
		return false
	}
	if IsPrerelease(lcm) {
		return version.Compare(v, lcm) <= 0
	}
	return !IsPrerelease(v) && !version.FeatureBehind(lcm, v)
}

// SameVersion meldet, ob zwei Schreibweisen dieselbe Version sind (2.4.0~beta.1
// und 2.4.0-beta.1 sind es).
func SameVersion(a, b string) bool {
	return versionKnown(a) && versionKnown(b) && version.Compare(a, b) == 0
}
