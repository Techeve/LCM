package main

import (
	"testing"

	"LCM/internal/config"
)

// Ohne Umgebungsvariable legt nichts einen Port fest, auch nicht ein Wert der
// config.json, der von der Vorgabe abweicht: Die config.json spiegelt die
// Einstellungen und sperrt kein Feld.
func TestAddressConfigOhneUmgebungSperrtNichts(t *testing.T) {
	cfg := &config.Config{Host: "0.0.0.0", Port: 9400, AgentPort: 0, SharedTLSListen: "127.0.0.1:8443", ACMEHTTPListen: ":80"}
	ac := addressConfig(cfg, false)
	if ac.UI.Set || ac.Agent.Set {
		t.Errorf("Ports gesperrt: %+v", ac)
	}
	if ac.ACMEHTTPPort != 80 || ac.FallbackHost != "localhost" || ac.Scheme != "https" {
		t.Errorf("Rest = %+v", ac)
	}
}

// Eine Umgebungsvariable sperrt, auch mit dem Wert der Vorgabe: LCM kann sie
// nicht ändern. LCM_AGENT_PORT=0 sperrt den Agenten auf aus.
func TestAddressConfigUmgebungSperrt(t *testing.T) {
	t.Setenv("LCM_PORT", "9310")
	ac := addressConfig(&config.Config{Port: 9310, AgentPort: 9320}, false)
	if !ac.UI.Set || ac.UI.Value != 9310 || ac.Agent.Set {
		t.Errorf("UI/Agent = %+v / %+v", ac.UI, ac.Agent)
	}

	t.Setenv("LCM_AGENT_PORT", "0")
	ac = addressConfig(&config.Config{Port: 9310, AgentPort: 0}, false)
	if !ac.Agent.Set || ac.Agent.Value != 0 {
		t.Errorf("Agent aus (0) muss sperren: %+v", ac.Agent)
	}
	if locks := envLocksOf(ac); !locks.port || !locks.agentPort || !locks.agentDisabled {
		t.Errorf("Sperren = %+v", locks)
	}
}

// Im Entwicklungsmodus gibt es kein TLS: Die Adressen haben http.
func TestAddressConfigEntwicklungsmodus(t *testing.T) {
	if ac := addressConfig(&config.Config{Port: 9310, AgentPort: 9320}, true); ac.Scheme != "http" {
		t.Errorf("Dev = %+v", ac)
	}
}
