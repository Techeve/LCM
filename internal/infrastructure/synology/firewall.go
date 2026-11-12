package synology

import (
	"encoding/json"
	"slices"
	"strings"

	"LCM/internal/core/domain"
)

// readFirewall liest den Schalter, das aktive Profil und dessen Regeln. Ob
// das Profil schützt, bewertet domain.DSMFirewall.
func (c *Client) readFirewall(s *Settings) {
	fw, raw := c.fetch("SYNO.Core.Security.Firewall")
	s.Firewall = switchOf(fw, raw, keysFirewallOn, nil)
	if !s.Firewall.Known {
		return
	}
	if v, ok := lookup(fw.data, keysFirewallProfile); ok {
		s.Firewall.Text = asText(v)
		s.FirewallProfile = c.readFirewallProfile(s.Firewall.Text)
	}
}

// readFirewallProfile liest die Regeln des Profils. DSM 7.3 gliedert sie je
// Schnittstelle ("global" für alle, dazu etwa "eth0"), jede mit "rules" und
// "policy" für den Fall, dass keine Regel trifft. "name" ist der Profilname,
// kein Abschnitt. Ein nicht lesbares Profil bleibt leer.
func (c *Client) readFirewallProfile(name string) domain.DSMFirewall {
	if name == "" {
		return domain.DSMFirewall{}
	}
	quoted, _ := json.Marshal(name)
	var raw json.RawMessage
	err := c.callWith("SYNO.Core.Security.Firewall.Profile", "get", "", map[string]string{"name": string(quoted)}, &raw)
	if err != nil {
		return domain.DSMFirewall{}
	}
	var profile map[string]any
	if json.Unmarshal(raw, &profile) != nil {
		return domain.DSMFirewall{}
	}
	out := domain.DSMFirewall{Profile: name, Sections: []domain.DSMFirewallSection{}}
	for key, v := range profile {
		part, ok := v.(map[string]any)
		if !ok || key == "name" {
			continue
		}
		out.Sections = append(out.Sections, firewallSection(key, part))
	}
	sortFirewallSections(out.Sections)
	return out
}

func firewallSection(name string, part map[string]any) domain.DSMFirewallSection {
	section := domain.DSMFirewallSection{Name: name, Policy: text(part["policy"]), Rules: []domain.DSMFirewallRule{}}
	rules, _ := part["rules"].([]any)
	for _, r := range rules {
		if rule, ok := r.(map[string]any); ok {
			section.Rules = append(section.Rules, firewallRule(rule))
		}
	}
	return section
}

// firewallRule übernimmt eine Regel. Fehlt "enable", gilt sie als an.
func firewallRule(r map[string]any) domain.DSMFirewallRule {
	enabled, known := asBool(r["enable"])
	return domain.DSMFirewallRule{
		Enabled:       enabled || !known,
		Policy:        text(r["policy"]),
		Protocol:      text(r["protocol"]),
		Ports:         text(r["ports"]),
		PortGroup:     text(r["port_group"]),
		SourceIP:      text(r["source_ip"]),
		SourceIPGroup: text(r["source_ip_group"]),
		Name:          text(r["name"]),
	}
}

// sortFirewallSections stellt "global" voran, die Adapter folgen nach Namen.
func sortFirewallSections(sections []domain.DSMFirewallSection) {
	slices.SortFunc(sections, func(a, b domain.DSMFirewallSection) int {
		if (a.Name == domain.DSMFirewallGlobal) != (b.Name == domain.DSMFirewallGlobal) {
			if a.Name == domain.DSMFirewallGlobal {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// text ist ein Feld als Klartext; ein fehlendes bleibt leer.
func text(v any) string {
	if v == nil {
		return ""
	}
	return asText(v)
}
