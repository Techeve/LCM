package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
)

// DSMFirewall sind die Regeln des aktiven Firewall-Profils von DSM. Sie
// liegen wie DSMChecks als JSON in einer Textspalte und werden bei jeder
// Erfassung ganz ersetzt. Ohne Profilnamen ist das Profil nicht gelesen.
type DSMFirewall struct {
	Profile  string               `json:"profile"`
	Sections []DSMFirewallSection `json:"sections"`
}

// DSMFirewallSection sind die Regeln einer Schnittstelle. "global" gilt für
// alle, sonst steht dort der Adapter, etwa "eth0". Die Regeln gelten von oben
// nach unten; trifft keine, entscheidet Policy ("none" = nicht festgelegt,
// lässt durch).
type DSMFirewallSection struct {
	Name   string            `json:"name"`
	Policy string            `json:"policy"`
	Rules  []DSMFirewallRule `json:"rules"`
}

// DSMFirewallRule ist eine Regel, wie DSM sie liefert. Ports sind die
// Dienstnamen von DSM ("dms,dms_https") oder Portnummern, "all" heißt alle.
type DSMFirewallRule struct {
	Enabled       bool   `json:"enabled"`
	Policy        string `json:"policy"`
	Protocol      string `json:"protocol"`
	Ports         string `json:"ports"`
	PortGroup     string `json:"port_group"`
	SourceIP      string `json:"source_ip"`
	SourceIPGroup string `json:"source_ip_group"`
	Name          string `json:"name"`
}

// DSMFirewallGlobal ist der Abschnitt für alle Schnittstellen.
const DSMFirewallGlobal = "global"

// Read sagt, ob das Profil gelesen wurde.
func (f DSMFirewall) Read() bool { return f.Profile != "" }

// EnabledRules zählt die eingeschalteten Regeln aller Abschnitte.
func (f DSMFirewall) EnabledRules() int {
	n := 0
	for _, section := range f.Sections {
		for _, rule := range section.Rules {
			if rule.Enabled {
				n++
			}
		}
	}
	return n
}

// DenyByDefault: Was keine Regel erlaubt, wird verweigert. Maßgeblich ist der
// Abschnitt für alle Schnittstellen; Regeln an einem einzelnen Adapter
// schützen die übrigen nicht. Fehlt er, muss jeder Abschnitt verweigern.
func (f DSMFirewall) DenyByDefault() bool {
	for _, section := range f.Sections {
		if section.Name == DSMFirewallGlobal {
			return section.DeniesRest()
		}
	}
	if len(f.Sections) == 0 {
		return false
	}
	for _, section := range f.Sections {
		if !section.DeniesRest() {
			return false
		}
	}
	return true
}

// DeniesRest: Der Abschnitt verweigert, was keine Regel davor erlaubt, über
// seine Vorgabe oder über eine eingeschaltete Regel, die alles verweigert.
// Eine verweigernde Regel für einzelne Ports zählt nicht. Die Regeln gelten
// von oben nach unten: Steht davor eine Regel, die alles erlaubt, lässt der
// Abschnitt trotzdem alles durch.
func (s DSMFirewallSection) DeniesRest() bool {
	for _, rule := range s.Rules {
		if !rule.Enabled || !rule.matchesEverything() {
			continue
		}
		return isDenyPolicy(rule.Policy)
	}
	return isDenyPolicy(s.Policy)
}

// matchesEverything: trifft alle Ports aller Protokolle von allen Quellen.
func (r DSMFirewallRule) matchesEverything() bool {
	return matchesAll(r.Ports, r.PortGroup) &&
		matchesAll(r.Protocol) &&
		matchesAll(r.SourceIP, r.SourceIPGroup)
}

func isDenyPolicy(policy string) bool {
	p := strings.ToLower(policy)
	return strings.Contains(p, "deny") || strings.Contains(p, "drop")
}

// matchesAll: Mindestens ein Feld ist "all" und keines nennt etwas anderes.
// DSM lässt je nach Fassung eines der beiden Felder einer Angabe weg.
func matchesAll(values ...string) bool {
	found := false
	for _, v := range values {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "all":
			found = true
		case "":
		default:
			return false
		}
	}
	return found
}

// Value schreibt das Profil als JSON; ein nicht gelesenes als Leerstring.
func (f DSMFirewall) Value() (driver.Value, error) {
	if !f.Read() {
		return "", nil
	}
	b, err := json.Marshal(f)
	return string(b), err
}

// Scan liest das Profil zurück. Ein leerer Wert ist „nicht gelesen“.
func (f *DSMFirewall) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		*f = DSMFirewall{}
		return nil
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return errors.New("dsm_firewall: unerwarteter Spaltentyp")
	}
	*f = DSMFirewall{}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, f)
}
