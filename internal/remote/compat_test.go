package remote

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/core/agentcaps"
	"LCM/internal/remote/wire"
)

// Abwärtskompatibilität (Paket E): Der AKTUELLE Hub spricht mit Agenten
// früherer Fassungen. Hier ein Fake auf Protokollebene, der genau das sendet, was
// die Tags v2.0.0 bis v2.3.0 im Inventar schicken (Bytes, wie `git show
// <tag>:internal/agent/client.go` sie erzeugt; die Version kommt wie im Feld aus
// version.String() samt Build-Zusatz). Dasselbe mit den echten Binaries
// der Tags: compat_agent_test.go (LCM_COMPAT_AGENT_TAGS).

// legacyAgents sind die Inventare der Agent-Fassungen vor Ping und Jobs.
var legacyAgents = []struct {
	name      string
	version   string
	inventory string
	console   bool // meldet die Fähigkeit tty (ab 2.2.0)
}{
	{"2.0.0", "2.0.0 (Build 1, a1b2c3d4e5f6)", `{"agent_version":"2.0.0 (Build 1, a1b2c3d4e5f6)","hostname":"vm01","kernel":"6.1.0-13-amd64"}`, false},
	{"2.1.0", "2.1.0 (Build 1, a1b2c3d4e5f6)", `{"agent_version":"2.1.0 (Build 1, a1b2c3d4e5f6)","hostname":"vm01","kernel":"6.1.0-13-amd64","key_pin":"aa","ca_pin":"bb"}`, false},
	{"2.2.0", "2.2.0 (Build 1, a1b2c3d4e5f6)", `{"agent_version":"2.2.0 (Build 1, a1b2c3d4e5f6)","hostname":"vm01","kernel":"6.1.0-13-amd64","key_pin":"aa","ca_pin":"bb","capabilities":["tty"]}`, true},
	{"2.3.0", "2.3.0 (Build 1, a1b2c3d4e5f6)", `{"agent_version":"2.3.0 (Build 1, a1b2c3d4e5f6)","hostname":"vm01","kernel":"6.1.0-13-amd64","key_pin":"aa","ca_pin":"bb","capabilities":["tty"]}`, true},
}

// TestAeltereAgentenGegenAktuellenHub: Der Befehl läuft, es geht kein Ping hinaus,
// die Stille-Frist ist der Wert für alte Agenten, LCM speichert Protokoll 1 und
// die Funktionsprüfung antwortet nach den gemeldeten Fähigkeiten.
func TestAeltereAgentenGegenAktuellenHub(t *testing.T) {
	for _, old := range legacyAgents {
		t.Run(old.name, func(t *testing.T) {
			hub, server, _ := newTestHubWith(t, func(h *Hub) { h.WithPingInterval(30 * time.Millisecond) })
			var pings atomic.Int32
			if err := hub.broker.Subscribe(wire.TopicCmd(server.AgentID), 98, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
				var cmd wire.Command
				if json.Unmarshal(pk.Payload, &cmd) == nil && cmd.Ping {
					pings.Add(1)
				}
			}); err != nil {
				t.Fatal(err)
			}
			a := subscribedAgent(t, hub, server.AgentID)
			a.connectWith([]byte(old.inventory))

			// Gespeichert: Version, Protokoll 1 (Feld fehlt), Fähigkeiten wie gemeldet.
			stored, err := hub.servers.FindByAgentID(server.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.AgentVersion != old.version || stored.AgentProtocol != 1 {
				t.Fatalf("gespeichert: Version %q, Protokoll %d", stored.AgentVersion, stored.AgentProtocol)
			}
			legacy := stored.Agent()
			if legacy.BelowMinimum() {
				t.Fatalf("%s liegt über der Mindestversion %s", old.version, agentcaps.MinAgentVersion)
			}
			if got := legacy.Supports(agentcaps.FeatureConsole); got != old.console {
				t.Errorf("Konsole = %v, erwartet %v", got, old.console)
			}
			if legacy.Supports(agentcaps.FeatureLiveness) || legacy.Supports(agentcaps.FeatureDurableJobs) {
				t.Error("ein alter Agent hat weder Lebendprüfung noch dauerhafte Jobs")
			}
			report := agentcaps.Assess(legacy, "2.4.0")
			if report.State != agentcaps.StateOutdated || !report.UpdateRecommended {
				t.Errorf("Einordnung gegen LCM 2.4.0: %+v", report)
			}

			// Der Befehl läuft, die Stille-Frist ist die für alte Agenten.
			conn, err := hub.Conn(server)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			done := runAsync(conn, "echo kompatibel")
			cmd := a.command(t, 0)
			if want := int(defaultIdleTimeout.Seconds()); cmd.IdleTimeoutSec != want {
				t.Errorf("IdleTimeoutSec = %d, erwartet %d", cmd.IdleTimeoutSec, want)
			}
			a.mu.Lock()
			raw := string(a.rawCmd[0])
			a.mu.Unlock()
			if strings.Contains(raw, "ping") {
				t.Errorf("Kommando an einen alten Agent trägt ein unbekanntes Feld: %s", raw)
			}
			// Das Ergebnis, wie es 2.x-Agenten senden (nur die Felder, die sie kennen).
			reply := `{"id":"` + cmd.ID + `","output":"kompatibel\n","exit_code":0}`
			if err := hub.broker.Publish(wire.TopicRes(server.AgentID), []byte(reply), false, 1); err != nil {
				t.Fatal(err)
			}
			r := <-done
			if r.err != nil || r.out != "kompatibel\n" || r.code != 0 {
				t.Fatalf("Ergebnis: %+v", r)
			}

			// Ein Ping gäbe es im Takt von 30 ms längst: Es kommt keins.
			time.Sleep(300 * time.Millisecond)
			if n := pings.Load(); n != 0 {
				t.Fatalf("%d Pings an einen Agent ohne die Fähigkeit", n)
			}

			// Wie bisher: Die Trennung lässt einen laufenden Aufruf scheitern.
			again := runAsync(conn, "hängt")
			a.command(t, 1)
			a.disconnect()
			select {
			case r := <-again:
				if r.err == nil {
					t.Fatal("der Aufruf an einen alten Agent überlebte die Trennung")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("die Trennung entsperrte den Aufruf nicht")
			}
		})
	}
}

// TestNeuerAgentWirdMitProtokollUndFaehigkeitenGespeichert: Ein Agent der
// aktuellen Fassung meldet Protokoll 2 und alle Fähigkeiten; LCM bietet alles an
// und ordnet ihn als in Ordnung ein.
func TestNeuerAgentWirdMitProtokollUndFaehigkeitenGespeichert(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := subscribedAgent(t, hub, server.AgentID)
	inv := wire.Inventory{
		AgentVersion: "2.4.0", ProtocolVersion: wire.ProtocolVersion, Hostname: "vm01",
		Capabilities: []string{wire.CapabilityTTY, wire.CapabilityPing, wire.CapabilityJobs, wire.CapabilitySelfUpdate},
	}
	payload, _ := json.Marshal(inv)
	a.connectWith(payload)

	stored, err := hub.servers.FindByAgentID(server.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AgentProtocol != wire.ProtocolVersion || stored.AgentCapabilities != "tty,ping,jobs-v2,self-update-v2" {
		t.Fatalf("gespeichert: Protokoll %d, Fähigkeiten %q", stored.AgentProtocol, stored.AgentCapabilities)
	}
	for _, f := range agentcaps.Features() {
		if !stored.Agent().Supports(f) {
			t.Errorf("Funktion %s fehlt", f)
		}
	}
	if report := agentcaps.Assess(stored.Agent(), "2.4.0"); report.State != agentcaps.StateOK {
		t.Errorf("Einordnung: %+v", report)
	}
	hub.mu.Lock()
	_, pingable := hub.pingable[server.AgentID]
	_, durable := hub.jobs[server.AgentID]
	hub.mu.Unlock()
	if !pingable || !durable {
		t.Errorf("pingbar=%v, dauerhaft=%v", pingable, durable)
	}
}

// TestRollbackDesAgentsWirdGespeichertUndGeloescht: Meldet der Agent im Inventar
// ein Rollback, steht es am Server (und in der Einordnung); der nächste Connect
// ohne Meldung löscht es wieder. Der Zeitpunkt des Inventars wird mitgeführt -
// daran erkennt ein Update-Job, dass sich der Agent nach dem Start gemeldet hat.
func TestRollbackDesAgentsWirdGespeichertUndGeloescht(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := subscribedAgent(t, hub, server.AgentID)
	before := time.Now().Add(-time.Second)

	rolled := wire.Inventory{
		AgentVersion: "2.3.0", ProtocolVersion: wire.ProtocolVersion, RolledBack: "2.4.0", RolledBackTo: "2.3.0", RolledBackReason: "not-ready",
		Capabilities: []string{wire.CapabilityTTY, wire.CapabilityPing, wire.CapabilityJobs, wire.CapabilitySelfUpdate},
	}
	payload, _ := json.Marshal(rolled)
	a.connectWith(payload)

	stored, err := hub.servers.FindByAgentID(server.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AgentRollback != "2.4.0" || stored.AgentReportedAt == nil || !stored.AgentReportedAt.After(before) {
		t.Fatalf("gespeichert: Rollback %q, gemeldet %v", stored.AgentRollback, stored.AgentReportedAt)
	}
	if stored.AgentRollbackReason != "not-ready" {
		t.Errorf("Grund = %q", stored.AgentRollbackReason)
	}
	if report := agentcaps.Assess(stored.Agent(), "2.4.0"); report.RolledBack != "2.4.0" || report.RolledBackReason != "not-ready" {
		t.Errorf("Einordnung nennt das Rollback nicht: %+v", report)
	}

	// Derselbe Agent meldet sich ohne Rollback (LCM hat das Paket angeglichen,
	// er startete normal): Der Befund bleibt, die Version ist dieselbe.
	same := wire.Inventory{AgentVersion: "2.3.0 (Build 9)", ProtocolVersion: wire.ProtocolVersion}
	payload, _ = json.Marshal(same)
	a.connectWith(payload)
	stored, _ = hub.servers.FindByAgentID(server.AgentID)
	if stored.AgentRollback != "2.4.0" || stored.AgentRollbackReason != "not-ready" {
		t.Fatalf("Befund verloren: %q / %q", stored.AgentRollback, stored.AgentRollbackReason)
	}

	// Eine andere Version (ein Update gelang) löscht den Befund.
	clean := wire.Inventory{AgentVersion: "2.4.0", ProtocolVersion: wire.ProtocolVersion}
	payload, _ = json.Marshal(clean)
	a.connectWith(payload)
	stored, _ = hub.servers.FindByAgentID(server.AgentID)
	if stored.AgentRollback != "" {
		t.Errorf("Rollback blieb stehen: %q", stored.AgentRollback)
	}
}
