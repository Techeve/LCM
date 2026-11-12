package synology

import (
	"testing"

	"LCM/internal/core/domain"
)

// TestFirewallAbschnitte: "global" steht vorn, die Adapter folgen nach Namen.
// Eine Regel ohne "enable" gilt als an, "enable": false als aus.
func TestFirewallAbschnitte(t *testing.T) {
	sections := []domain.DSMFirewallSection{
		firewallSection("wlan0", map[string]any{"policy": "none"}),
		firewallSection("global", map[string]any{"policy": "drop", "rules": []any{
			map[string]any{"policy": "allow", "ports": "dms"},
			map[string]any{"enable": false, "policy": "drop", "ports": "all"},
			"kaputt",
		}}),
		firewallSection("eth0", map[string]any{}),
	}
	sortFirewallSections(sections)
	var names []string
	for _, s := range sections {
		names = append(names, s.Name)
	}
	if len(names) != 3 || names[0] != "global" || names[1] != "eth0" || names[2] != "wlan0" {
		t.Fatalf("Reihenfolge: %v", names)
	}
	global := sections[0]
	if global.Policy != "drop" || len(global.Rules) != 2 || !global.Rules[0].Enabled || global.Rules[1].Enabled {
		t.Errorf("Regeln: %+v", global)
	}
	if sections[1].Rules == nil || sections[1].Policy != "" {
		t.Errorf("leerer Abschnitt: %+v", sections[1])
	}
}
