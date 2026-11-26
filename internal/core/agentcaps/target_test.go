package agentcaps

import "testing"

// TestPickTarget belegt die Wahl der Zielversion: höchste Version der
// Feature-Reihe von LCM samt Bugfixes, nie eine höhere Reihe, kein Beta für ein
// stabiles LCM, kein Rückbau; ein LCM in einer Vorabversion genau bis zu sich.
func TestPickTarget(t *testing.T) {
	community := []string{"2.0.0", "2.1.0", "2.2.0", "2.2.1", "2.3.0"}
	withBetas := append([]string{"2.4.0~beta.1", "2.4.0~beta.2", "2.4.0~beta.10"}, community...)
	fixes := []string{"2.2.0", "2.2.1", "2.2.3", "2.3.0", "2.3.4", "2.4.0"}
	tests := []struct {
		name      string
		installed string
		available []string
		lcm       string
		limit     string
		action    Action
		target    string
	}{
		{"stabiles LCM, Agent hinterher: höchstes Final", "2.2.0", community, "2.3.0", "", ActionUpdate, "2.3.0"},
		{"Enterprise-LCM hinter Community: nie über die Reihe", "2.1.0", community, "2.2.1", "", ActionUpdate, "2.2.1"},
		{"Enterprise-LCM 2.2.0 nimmt den Bugfix 2.2.3", "2.0.0", fixes, "2.2.0", "", ActionUpdate, "2.2.3"},
		{"LCM 2.3.0 nimmt den Bugfix 2.3.4, nicht 2.4.0", "2.3.0", fixes, "2.3.0", "", ActionUpdate, "2.3.4"},
		{"Agent auf dem Bugfix über LCM bleibt", "2.3.4", fixes, "2.3.0", "", ActionCurrent, "2.3.4"},
		{"gestaffelt: nie über den ersten Server", "2.2.0", fixes, "2.3.0", "2.3.0", ActionUpdate, "2.3.0"},
		{"schon auf dem Ziel", "2.3.0", community, "2.3.0", "", ActionCurrent, "2.3.0"},
		{"stabiles LCM nimmt nie ein Beta, auch kein kleineres", "2.2.0", append([]string{"2.3.0~beta.4"}, "2.2.0", "2.2.1"), "2.3.0", "", ActionUpdate, "2.2.1"},
		{"stabiles LCM 2.4.0, Quelle führt Betas 2.4.0~beta.N", "2.3.0", withBetas, "2.4.0", "", ActionCurrent, "2.3.0"},
		{"Beta-LCM nimmt Beta bis zur eigenen Version", "2.3.0", withBetas, "2.4.0-beta.2", "", ActionUpdate, "2.4.0~beta.2"},
		{"Beta-LCM: beta.10 ist höher als beta.9", "2.3.0", []string{"2.4.0~beta.9", "2.4.0~beta.10"}, "2.4.0-beta.10", "", ActionUpdate, "2.4.0~beta.10"},
		{"Beta-LCM nimmt nie ein höheres Beta", "2.4.0~beta.1", withBetas, "2.4.0-beta.2", "", ActionUpdate, "2.4.0~beta.2"},
		{"Beta-LCM: Final vor der Vorabversion", "2.3.0", []string{"2.3.0", "2.4.0~beta.1"}, "2.4.0-beta.1", "", ActionUpdate, "2.4.0~beta.1"},
		{"Beta-LCM: Agent mit höherem Beta ist neuer", "2.4.0~beta.3", withBetas, "2.4.0-beta.2", "", ActionNewer, ""},
		{"Agent in höherer Reihe: kein Rückbau", "2.4.0", community, "2.3.0", "", ActionNewer, ""},
		{"nichts in der Reihe", "", []string{"2.4.0", "2.5.0"}, "2.3.0", "", ActionNone, ""},
		{"Quelle führt nichts", "2.2.0", nil, "2.3.0", "", ActionNone, ""},
		{"Entwicklungsstand von LCM: keine Wahl", "2.2.0", community, "0.0.0-dev", "", ActionNone, ""},
		{"Installiert unbekannt (Selbstbau)", "0.0.0-dev", community, "2.3.0", "", ActionUpdate, "2.3.0"},
		{"höher installiert als jede Wahl: bleibt", "2.3.0", []string{"2.2.0"}, "2.3.0", "", ActionCurrent, "2.3.0"},
		{"installiert mit Build-Zusatz (Inventar)", "2.3.0 (Build 412, a1b2c3)", fixes, "2.3.0", "", ActionUpdate, "2.3.4"},
		{"Whitespace in der Liste", "2.2.0", []string{" 2.3.0 "}, "2.3.0", "", ActionUpdate, "2.3.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := PickTarget(tt.installed, tt.available, tt.lcm, tt.limit)
			if sel.Action != tt.action || sel.Target != tt.target {
				t.Fatalf("PickTarget(%q, %v, %q, %q) = %s %q, want %s %q",
					tt.installed, tt.available, tt.lcm, tt.limit, sel.Action, sel.Target, tt.action, tt.target)
			}
		})
	}
}

func TestSameVersion(t *testing.T) {
	if !SameVersion("2.4.0~beta.3", "2.4.0-beta.3") {
		t.Error("Tilde und Bindestrich sind dieselbe Version")
	}
	if SameVersion("2.4.0~beta.3", "2.4.0") || SameVersion("", "") {
		t.Error("Beta und Final oder zwei leere Versionen sind nicht gleich")
	}
}
