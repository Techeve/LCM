package remote

import (
	"encoding/json"
	"log/slog"
	"testing"
	"unicode/utf8"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/core/agentcaps"
	"LCM/internal/core/domain"
	"LCM/internal/remote/wire"
)

// Fuzz-Ziele für das, was der Hub von einem Agent als Nutzdaten bekommt. Ein
// Agent läuft auf einer fremden Maschine; wer sie übernimmt, kontrolliert jedes
// Byte, das er auf seine Topics schreibt (res, inv, ttyo). Der Hub hat einen
// Panic-Schutz um jeden Handler (safego.Recover), aber der verdeckt Fehler nur:
// Die Ziele rufen die Handler deshalb mit einem Hub auf, der ohne Datenbank
// auskommt, und prüfen die Folgen, nicht nur das Ausbleiben eines Absturzes.
//
// Ohne -fuzz laufen nur die Startkorpora. Lauf:
//
//	go test ./internal/remote -run '^$' -fuzz FuzzHandleResult -fuzztime 60s

const (
	fuzzAgent = "agent-1"
	fuzzReq   = "0b39a4f2-1111-2222-3333-444455556666"
)

func fuzzHub() *Hub { return New(nil, slog.New(slog.DiscardHandler)) }

func fuzzPacket(agentID, suffix string, payload []byte) packets.Packet {
	return packets.Packet{TopicName: "lcm/a/" + agentID + suffix, Payload: payload}
}

// FuzzHandleResult: Ein Ergebnis oder Lebenszeichen auf dem res-Topic. Es darf
// nur den Aufruf des eigenen Agents abschließen, höchstens einmal, und nie
// blockieren.
func FuzzHandleResult(f *testing.F) {
	for _, s := range []string{
		`{"id":"` + fuzzReq + `","output":"ok","exit_code":0}`,
		`{"id":"` + fuzzReq + `","progress":true,"output_bytes":10,"vitals":{"alive":true,"procs":2}}`,
		`{"id":"` + fuzzReq + `","output":"x","exit_code":1,"error":"boom","truncated":true,"recovered":true}`,
		`{"pong":true}`,
		`{"id":"unbekannt","output":"spät","exit_code":0}`,
		`[]`, `null`, `{"id":1}`, ``,
	} {
		f.Add(fuzzAgent, []byte(s))
		f.Add("agent-2", []byte(s)) // ein anderer Agent mit der ID eines fremden Aufrufs
	}
	f.Fuzz(func(t *testing.T, agentID string, payload []byte) {
		h := fuzzHub()
		req := h.register(fuzzReq, fuzzAgent)
		pk := fuzzPacket(agentID, wire.TopicResSuf, payload)
		h.handleResult(nil, packets.Subscription{}, pk)
		h.handleResult(nil, packets.Subscription{}, pk) // QoS 1 stellt doppelt zu

		var res wire.Result
		decoded := json.Unmarshal(payload, &res) == nil
		belongs := wire.AgentIDFromTopic(pk.TopicName) == fuzzAgent
		answered := len(req.ch) > 0
		h.mu.Lock()
		_, waiting := h.pending[fuzzReq]
		h.mu.Unlock()

		if answered && !(decoded && belongs && res.ID == fuzzReq && !res.Progress && !res.Pong) {
			t.Fatalf("Aufruf beantwortet von Agent %q mit %q", agentID, payload)
		}
		if answered == waiting {
			t.Fatalf("Aufruf beantwortet (%v) und wartend (%v) widersprechen sich", answered, waiting)
		}
		if len(req.ch) > 1 {
			t.Fatalf("%d Ergebnisse für einen Aufruf", len(req.ch))
		}
	})
}

// FuzzHandleInventory: Der Teil von handleInventory, der kein Datenbankzugriff
// ist: Fähigkeiten merken, wartende Aufrufe scheitern lassen, Rollback-Befund.
// Das Inventar kommt beim Agent aus dem eigenen Binary, bei einem übernommenen
// Agent von einem Angreifer.
func FuzzHandleInventory(f *testing.F) {
	for _, old := range legacyAgents {
		f.Add([]byte(old.inventory))
	}
	f.Add([]byte(`{"agent_version":"2.4.0 (Build 3, a1b2c3d4e5f6)","protocol_version":2,"hostname":"vm01",` +
		`"capabilities":["tty","ping","jobs-v2","self-update-v2"],"known_jobs":[],` +
		`"rolled_back":"2.4.1","rolled_back_to":"2.4.0","rolled_back_reason":"crash-loop"}`))
	f.Add([]byte(`{"known_jobs":["` + fuzzReq + `"],"capabilities":["jobs-v2"]}`))
	f.Add([]byte(`{"known_jobs":null}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		var inv wire.Inventory
		if json.Unmarshal(payload, &inv) != nil {
			return
		}
		h := fuzzHub()
		waiting := h.register(fuzzReq, fuzzAgent)
		agent := agentcaps.FromInventory(inv)
		h.rememberCapabilities(fuzzAgent, agent)
		h.failUnreachable(nil, fuzzAgent, agent, inv.KnownJobs)
		_ = agentcaps.Assess(agent, "2.4.0")
		_, _ = keptRollback(&domain.Server{AgentVersion: "2.4.0", AgentRollback: "2.4.1"}, inv)
		if len(waiting.ch) > 1 {
			t.Fatalf("%d Ergebnisse für einen Aufruf", len(waiting.ch))
		}
		if b := boundedInventory(inv); !utf8.ValidString(b.AgentVersion) || len(b.AgentVersion) > maxInventoryField ||
			len(b.KeyPin) > maxInventoryField || len(b.CAPin) > maxInventoryField || len(b.Capabilities) > maxInventoryCapabilities ||
			len(b.RolledBack) > maxInventoryField || len(b.RolledBackTo) > maxInventoryField || len(b.RolledBackReason) > maxInventoryField {
			t.Fatalf("Inventar nicht begrenzt: %+v", b)
		}
	})
}

// FuzzHandleTTYFrames: Rahmen eines Agents für eine offene Konsole des Hubs. Die
// Sitzung darf nicht über maxTerminalBacklog hinaus wachsen, und ein fremder
// Agent erreicht sie nie.
func FuzzHandleTTYFrames(f *testing.F) {
	for _, s := range []string{
		`{"s":"S","n":1,"op":"opened"}`,
		`{"s":"S","n":2,"op":"data","d":"aGFsbG8="}`,
		`{"s":"S","n":3,"op":"closed","e":"fertig"}`,
		`{"s":"S","n":9,"op":"data","d":"eA=="}`,
		`{"s":"S","n":18446744073709551615,"op":"data"}`,
	} {
		f.Add(fuzzAgent, []byte(s), []byte(s))
		f.Add("agent-2", []byte(s), []byte(s))
	}
	f.Fuzz(func(t *testing.T, agentID string, first, second []byte) {
		h := fuzzHub()
		h.broker = mqtt.New(&mqtt.Options{InlineClient: true, Logger: slog.New(slog.DiscardHandler)})
		term := &agentTerminal{hub: h, agentID: fuzzAgent, id: "S",
			notify: make(chan struct{}, 1), opened: make(chan error, 1)}
		h.ttys["S"] = term
		for _, payload := range [][]byte{first, second} {
			h.handleTTY(nil, packets.Subscription{}, fuzzPacket(agentID, wire.TopicTTOSuf, payload))
		}
		term.mu.Lock()
		backlog := len(term.backlog)
		term.mu.Unlock()
		if backlog > maxTerminalBacklog+wire.MaxPacketSize {
			t.Fatalf("Bildschirm-Puffer %d Bytes", backlog)
		}
		term.mu.Lock()
		touched := term.ended || backlog > 0
		term.mu.Unlock()
		if wire.AgentIDFromTopic("lcm/a/"+agentID+"/ttyo") != fuzzAgent && touched {
			t.Fatalf("Agent %q hat die Sitzung eines fremden Agents berührt", agentID)
		}
	})
}

func TestBoundedInventoryLaesstEchteInventareUnveraendert(t *testing.T) {
	for _, old := range legacyAgents {
		var inv wire.Inventory
		if err := json.Unmarshal([]byte(old.inventory), &inv); err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(boundedInventory(inv))
		want, _ := json.Marshal(inv)
		if string(got) != string(want) {
			t.Errorf("%s verändert:\n%s\n%s", old.name, got, want)
		}
	}
}

func TestBoundedInventoryKuerztUebergrosseFelder(t *testing.T) {
	long := string(make([]byte, 1<<20))
	caps := make([]string, 1000)
	for i := range caps {
		caps[i] = long
	}
	inv := boundedInventory(wire.Inventory{AgentVersion: long, KeyPin: long, CAPin: long, Capabilities: caps, KnownJobs: []string{}})
	if len(inv.AgentVersion) > maxInventoryField || len(inv.KeyPin) > maxInventoryField || len(inv.CAPin) > maxInventoryField {
		t.Error("Textfelder nicht gekürzt")
	}
	if len(inv.Capabilities) != maxInventoryCapabilities || len(inv.Capabilities[0]) > maxInventoryField {
		t.Errorf("Fähigkeiten: %d Stück, erstes %d Bytes", len(inv.Capabilities), len(inv.Capabilities[0]))
	}
	if inv.KnownJobs == nil {
		t.Error("eine leere Liste bekannter Kommandos muss leer, nicht nil bleiben (nil = meldet nicht)")
	}
	if len(caps[0]) != 1<<20 {
		t.Error("die Eingabe wurde verändert")
	}
}

// Ergebnis-Fälschung: Ein anderer Agent kennt womöglich die ID eines fremden
// Aufrufs (sie ist eine UUID, aber der Test nimmt das Schlimmste an). Sein
// Ergebnis schließt den Aufruf des richtigen Agents nicht ab, ein lebloses
// „progress" hält dessen Uhr nicht an.
func TestFremdesErgebnisSchliesstKeinenAufruf(t *testing.T) {
	h := fuzzHub()
	req := h.register(fuzzReq, fuzzAgent)
	forged := []byte(`{"id":"` + fuzzReq + `","output":"gefälscht","exit_code":0}`)
	progress := []byte(`{"id":"` + fuzzReq + `","progress":true}`)
	before := req.sign

	h.handleResult(nil, packets.Subscription{}, fuzzPacket("agent-2", wire.TopicResSuf, forged))
	h.handleResult(nil, packets.Subscription{}, fuzzPacket("agent-2", wire.TopicResSuf, progress))
	if len(req.ch) != 0 || len(req.progress) != 0 || !req.sign.Equal(before) {
		t.Fatal("ein fremder Agent hat in den Aufruf eingegriffen")
	}
	h.handleResult(nil, packets.Subscription{}, fuzzPacket(fuzzAgent, wire.TopicResSuf, forged))
	if len(req.ch) != 1 {
		t.Fatal("das Ergebnis des richtigen Agents kam nicht an")
	}
}
