package agentcaps

import (
	"testing"

	"LCM/internal/remote/wire"
)

// Was die Agent-Fassungen aus dem Feld melden (Inventory je Tag): 2.0.0 und
// 2.1.0 schicken keine Fähigkeiten, ab 2.2.0 kommt tty, ab der Robustheit
// ping und jobs-v2 samt Protokoll 2.
var (
	agent200 = Agent{Version: "2.0.0"}
	agent210 = Agent{Version: "2.1.0"}
	agent230 = Agent{Version: "2.3.0", Capabilities: []string{wire.CapabilityTTY}}
	agentNew = Agent{Version: "2.4.0", Protocol: 2, Capabilities: []string{
		wire.CapabilityTTY, wire.CapabilityPing, wire.CapabilityJobs, wire.CapabilitySelfUpdate}}
)

func TestCheckNachFaehigkeit(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		feature Feature
		want    Support
	}{
		{"2.0.0 ohne Konsole", agent200, FeatureConsole, Support{Reason: ReasonOutdated, NeedsVersion: "2.2.0"}},
		{"2.1.0 ohne Konsole", agent210, FeatureConsole, Support{Reason: ReasonOutdated, NeedsVersion: "2.2.0"}},
		{"2.3.0 mit Konsole", agent230, FeatureConsole, Support{Supported: true}},
		{"2.3.0 ohne Lebendprüfung", agent230, FeatureLiveness, Support{Reason: ReasonOutdated, NeedsVersion: "2.4.0"}},
		{"2.3.0 ohne Jobs", agent230, FeatureDurableJobs, Support{Reason: ReasonOutdated, NeedsVersion: "2.4.0"}},
		{"neu kann alles", agentNew, FeatureDurableJobs, Support{Supported: true}},
		{"neu mit Ping", agentNew, FeatureLiveness, Support{Supported: true}},
		{"Konsole am Server abgeschaltet",
			Agent{Version: "2.3.0", Capabilities: []string{wire.CapabilityTTYOff}}, FeatureConsole, Support{Reason: ReasonDisabled}},
		{"unbekannte Funktion", agentNew, Feature("zukunft"), Support{Reason: ReasonOutdated}},
		{"noch nie gemeldet", Agent{}, FeatureConsole, Support{Reason: ReasonOutdated, NeedsVersion: "2.2.0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.agent.Check(tt.feature); got != tt.want {
				t.Fatalf("Check(%s) = %+v, want %+v", tt.feature, got, tt.want)
			}
		})
	}
}

// Die Fähigkeit entscheidet, nicht die Versionsnummer: Ein Selbstbau meldet
// „0.0.0-dev“, kann aber alles, was er meldet.
func TestFaehigkeitGehtVorVersion(t *testing.T) {
	dev := Agent{Version: "0.0.0-dev", Capabilities: []string{wire.CapabilityTTY, wire.CapabilityJobs}}
	if !dev.Supports(FeatureConsole) || !dev.Supports(FeatureDurableJobs) {
		t.Fatal("ein Selbstbau mit Fähigkeiten muss sie anbieten dürfen")
	}
	if dev.Supports(FeatureLiveness) {
		t.Fatal("ohne gemeldete Fähigkeit keine Lebendprüfung")
	}
	if dev.BelowMinimum() {
		t.Fatal("ein Selbstbau gilt nicht als zu alt")
	}
}

func TestMindestversion(t *testing.T) {
	tests := []struct {
		version string
		below   bool
	}{
		{"2.0.0", false},
		{"2.3.0", false},
		{"2.3.0-beta.2", false},
		{"2.3.0~beta.2", false},
		{"2.0.0~beta.17", false}, // Betas der 2.0 gelten als 2.0.0 (Entscheidung 24.11.)
		{"1.48.1", true},
		{"1.9.0", true},
		{"0.0.0-dev", false},
		{"", false},
		{"kaputt", false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := (Agent{Version: tt.version}).BelowMinimum(); got != tt.below {
				t.Fatalf("BelowMinimum(%q) = %v, want %v", tt.version, got, tt.below)
			}
		})
	}
}

// Unter der Mindestversion geht nichts mehr außer dem Update - auch dann nicht,
// wenn der Agent eine Fähigkeit meldet.
func TestUnterMindestversionKeineFunktion(t *testing.T) {
	old := Agent{Version: "1.48.1", Capabilities: []string{wire.CapabilityTTY, wire.CapabilityPing, wire.CapabilityJobs, wire.CapabilitySelfUpdate}}
	for _, f := range Features() {
		got := old.Check(f)
		if got.Supported || got.Reason != ReasonBelowMinimum {
			t.Fatalf("Check(%s) = %+v, want below_minimum", f, got)
		}
		if got.NeedsVersion == "" {
			t.Fatalf("Check(%s): ohne Hinweis, welche Version nötig ist", f)
		}
	}
	// Auch das Update selbst muss die Mindestversion erreichen.
	if got := old.Check(FeatureConsole).NeedsVersion; got != "2.2.0" {
		t.Fatalf("NeedsVersion = %q", got)
	}
}

func TestProtokollVersionFehlendGiltAlsEins(t *testing.T) {
	if got := (Agent{}).ProtocolVersion(); got != 1 {
		t.Fatalf("ohne Angabe: %d", got)
	}
	if got := (Agent{Protocol: -3}).ProtocolVersion(); got != 1 {
		t.Fatalf("negativ: %d", got)
	}
	if got := agentNew.ProtocolVersion(); got != 2 {
		t.Fatalf("neu: %d", got)
	}
}

func TestFromStored(t *testing.T) {
	a := FromStored("2.3.0", 0, "tty, ping,,jobs-v2")
	if len(a.Capabilities) != 3 || !a.Has("tty") || !a.Has("ping") || !a.Has("jobs-v2") {
		t.Fatalf("Fähigkeiten: %v", a.Capabilities)
	}
	if got := FromStored("", 0, "").Capabilities; len(got) != 0 {
		t.Fatalf("leer: %v", got)
	}
	inv := FromInventory(wire.Inventory{AgentVersion: "2.4.0", ProtocolVersion: 2, Capabilities: []string{"tty"}})
	if inv.Version != "2.4.0" || inv.Protocol != 2 || !inv.Has("tty") {
		t.Fatalf("FromInventory: %+v", inv)
	}
}

func TestAssess(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		server  string
		state   State
		update  bool
		needs   string
		console Reason
	}{
		{"nie gemeldet", Agent{}, "2.4.0", StateUnknown, false, "2.4.0", ReasonOutdated},
		{"alles da", agentNew, "2.4.0", StateOK, false, "", ""},
		{"alles da, LCM neuer", agentNew, "2.5.0", StateOK, false, "", ""},
		{"2.3.0 an 2.4.0", agent230, "2.4.0", StateOutdated, true, "2.4.0", ""},
		{"2.0.0 an 2.4.0", agent200, "2.4.0", StateOutdated, true, "2.4.0", ReasonOutdated},
		{"unter Mindestversion", Agent{Version: "1.48.1"}, "2.4.0", StateUnsupported, true, "2.4.0", ReasonBelowMinimum},
		{"neuer als LCM", Agent{Version: "2.5.0", Protocol: 2, Capabilities: agentNew.Capabilities}, "2.4.0", StateNewer, false, "", ""},
		{"echte Meldung mit Build-Zusatz", Agent{Version: "2.3.0 (Build 412, a1b2c3d4e5f6)", Protocol: 2, Capabilities: []string{wire.CapabilityTTY}},
			"2.4.0", StateOutdated, true, "2.4.0", ""},
		{"Build-Zusatz, Agent in höherer Reihe", Agent{Version: "2.5.0 (Build 3)", Protocol: 2, Capabilities: agentNew.Capabilities}, "2.4.0", StateNewer, false, "", ""},
		{"Bugfix über LCM ist nicht neuer", Agent{Version: "2.4.3", Protocol: 2, Capabilities: agentNew.Capabilities}, "2.4.0", StateOK, false, "", ""},
		{"Beta-Agent der 2.0 gilt als 2.0.0", Agent{Version: "2.0.0~beta.7"}, "2.4.0", StateOutdated, true, "2.4.0", ReasonOutdated},
		{"Beta des Agents neuer als Beta von LCM",
			Agent{Version: "2.4.0~beta.2", Protocol: 2, Capabilities: agentNew.Capabilities}, "2.4.0-beta.1", StateNewer, false, "", ""},
		{"Protokoll neuer als LCM", Agent{Version: "2.4.0", Protocol: wire.ProtocolVersion + 1, Capabilities: agentNew.Capabilities},
			"2.4.0", StateNewer, false, "", ""},
		{"Selbstbau mit allem", Agent{Version: "0.0.0-dev", Protocol: 2, Capabilities: agentNew.Capabilities}, "2.4.0", StateOK, false, "", ""},
		{"LCM als Selbstbau", agentNew, "0.0.0-dev", StateOK, false, "", ""},
		{"Konsole abgeschaltet ist nicht veraltet",
			Agent{Version: "2.4.0", Protocol: 2, Capabilities: []string{wire.CapabilityTTYOff, wire.CapabilityPing, wire.CapabilityJobs, wire.CapabilitySelfUpdate}},
			"2.4.0", StateOK, false, "", ReasonDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Assess(tt.agent, tt.server)
			if r.State != tt.state {
				t.Errorf("State = %q, want %q", r.State, tt.state)
			}
			if r.UpdateRecommended != tt.update {
				t.Errorf("UpdateRecommended = %v, want %v", r.UpdateRecommended, tt.update)
			}
			if tt.state != StateUnknown && r.NeedsVersion != tt.needs {
				t.Errorf("NeedsVersion = %q, want %q", r.NeedsVersion, tt.needs)
			}
			if got := r.Features[FeatureConsole].Reason; got != tt.console {
				t.Errorf("Konsole: Grund %q, want %q", got, tt.console)
			}
			if r.MinVersion != MinAgentVersion || r.ServerVersion != tt.server || len(r.Features) != len(Features()) {
				t.Errorf("Bericht unvollständig: %+v", r)
			}
		})
	}
}

// TestAssessBehindUndRolledBack: „Hinter LCM“ unabhängig von den Funktionen,
// und das gemeldete Rollback steht im Bericht.
func TestAssessBehindUndRolledBack(t *testing.T) {
	full := []string{wire.CapabilityTTY, wire.CapabilityPing, wire.CapabilityJobs, wire.CapabilitySelfUpdate}
	tests := []struct {
		name   string
		agent  Agent
		server string
		behind bool
	}{
		{"gleich", Agent{Version: "2.4.0", Protocol: 2, Capabilities: full}, "2.4.0", false},
		{"vollständig, aber hinter LCM", Agent{Version: "2.4.0", Protocol: 2, Capabilities: full}, "2.4.1", true},
		{"Beta hinter dem Final", Agent{Version: "2.4.0~beta.2", Protocol: 2, Capabilities: full}, "2.4.0", true},
		{"neuer als LCM", Agent{Version: "2.5.0", Protocol: 2, Capabilities: full}, "2.4.0", false},
		{"Selbstbau", Agent{Version: "0.0.0-dev", Protocol: 2, Capabilities: full}, "2.4.0", false},
	}
	for _, tt := range tests {
		if got := Assess(tt.agent, tt.server).Behind; got != tt.behind {
			t.Errorf("%s: Behind = %t, want %t", tt.name, got, tt.behind)
		}
	}
	a := Agent{Version: "2.3.0", Capabilities: nil, RolledBack: "2.4.0"}
	if got := Assess(a, "2.4.0").RolledBack; got != "2.4.0" {
		t.Errorf("RolledBack = %q", got)
	}
}
