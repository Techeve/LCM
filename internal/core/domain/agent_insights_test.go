package domain

import (
	"strings"
	"testing"
	"time"

	"LCM/internal/core/agentcaps"
)

// Der Agent färbt die EINE Status-Pille: veraltet und neuer als LCM gelb, unter
// der Mindestversion rot - jeweils mit dem Befund als Grund.
func TestAgentBefundFaerbtDieAmpel(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		agent  *agentcaps.Report
		status string
		key    string
	}{
		{"kein Agent", nil, ServerStatusExcellent, ""},
		{"Agent in Ordnung", &agentcaps.Report{State: agentcaps.StateOK}, ServerStatusExcellent, ""},
		{"noch nie gemeldet", &agentcaps.Report{State: agentcaps.StateUnknown}, ServerStatusExcellent, ""},
		{"veraltet", &agentcaps.Report{State: agentcaps.StateOutdated, AgentVersion: "2.0.0", NeedsVersion: "2.4.0"},
			ServerStatusYellow, "agentOutdated"},
		{"neuer als LCM", &agentcaps.Report{State: agentcaps.StateNewer, AgentVersion: "2.5.0", ServerVersion: "2.4.0"},
			ServerStatusYellow, "agentNewer"},
		{"unter der Mindestversion", &agentcaps.Report{State: agentcaps.StateUnsupported, AgentVersion: "1.48.1", MinVersion: "2.0.0"},
			ServerStatusRed, "agentUnsupported"},
		// Der Rollback sagt, warum der Agent veraltet ist - ein Befund, nicht zwei.
		{"Update zurückgerollt", &agentcaps.Report{State: agentcaps.StateOutdated, AgentVersion: "2.3.0", NeedsVersion: "2.4.0", RolledBack: "2.4.0"},
			ServerStatusYellow, "agentRolledBack"},
		{"zurückgerollt, aber ok", &agentcaps.Report{State: agentcaps.StateOK, AgentVersion: "2.4.0", RolledBack: "2.4.1"},
			ServerStatusYellow, "agentRolledBack"},
		{"zurückgerollt unter der Mindestversion bleibt rot", &agentcaps.Report{State: agentcaps.StateUnsupported, AgentVersion: "1.48.1", MinVersion: "2.0.0", RolledBack: "2.0.0"},
			ServerStatusRed, "agentUnsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Server{Reachable: true, Transport: TransportAgent, SSHHardened: true, FirewallActive: true}
			status, insights := s.TrafficLight(TrafficLightInput{Now: now, Agent: tt.agent})
			if status != tt.status {
				t.Fatalf("Ampel = %q, want %q (Befunde %+v)", status, tt.status, insights)
			}
			var got string
			for _, in := range insights {
				if len(in.Key) > 5 && in.Key[:5] == "agent" {
					got = in.Key
				}
			}
			if got != tt.key {
				t.Fatalf("Befund = %q, want %q", got, tt.key)
			}
		})
	}
}

// Reißt die Verbindung des Agents oft ungewollt ab, färbt das die EINE Pille
// gelb - mit Zahl und letzter Ursache; bis einschließlich fünf bleibt sie ruhig.
func TestInstabileVerbindungFaerbtDieAmpelGelb(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		link *AgentLink
		want bool
	}{
		{"kein Hub", nil, false},
		{"fünf Abbrüche sind noch ruhig", &AgentLink{UnintendedLastHour: 5, LastCause: DropNetwork}, false},
		{"sechs Abbrüche sind instabil", &AgentLink{UnintendedLastHour: 6, LastCause: DropPing}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Server{Reachable: true, Transport: TransportAgent, SSHHardened: true, FirewallActive: true}
			status, insights := s.TrafficLight(TrafficLightInput{Now: now, AgentLink: tt.link})
			var found *StatusInsight
			for i := range insights {
				if insights[i].Key == "agentLinkUnstable" {
					found = &insights[i]
				}
			}
			if (found != nil) != tt.want {
				t.Fatalf("Befund = %+v, want vorhanden: %v", found, tt.want)
			}
			if !tt.want {
				if status != ServerStatusExcellent {
					t.Fatalf("Ampel = %q, want %q", status, ServerStatusExcellent)
				}
				return
			}
			if status != ServerStatusYellow || found.Severity != "warning" {
				t.Fatalf("Ampel = %q, Schwere %q, want gelb/warning", status, found.Severity)
			}
			if found.Params["count"] != "6" || found.Params["disconnect"] != DropPing {
				t.Fatalf("Parameter = %v", found.Params)
			}
			if !strings.Contains(found.Message, "6 Mal") || !strings.Contains(found.Message, dropLabel(DropPing)) {
				t.Fatalf("Text = %q", found.Message)
			}
		})
	}
}

func TestUngewollteTrennungen(t *testing.T) {
	for _, cause := range []string{DropPing, DropNetwork, DropTakeover} {
		if !UnintendedDrop(cause) {
			t.Errorf("%s muss ungewollt sein", cause)
		}
	}
	for _, cause := range []string{DropAgent, DropLCM, DropRevoked, DropAccessOff, ""} {
		if UnintendedDrop(cause) {
			t.Errorf("%q darf nicht ungewollt sein", cause)
		}
	}
}
