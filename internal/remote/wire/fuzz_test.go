package wire

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"
)

// Fuzz-Ziele für alles, was der Hub vom Agent und der Agent vom Hub als Bytes
// bekommt. Der Agent läuft als root auf einer fremden Maschine, und der Hub
// liest, was ein womöglich gekaperter Agent schickt: Keine dieser Nachrichten
// darf mit einem Panic oder einem Speicherverbrauch weit über ihrer Größe
// beantwortet werden.
//
// Eigenschaften je Ziel:
//   - kein Panic, egal welche Bytes;
//   - was sich dekodieren lässt, kodiert sich stabil: Kodieren, Dekodieren und
//     erneut Kodieren ergibt dieselben Bytes (Ungültiges UTF-8 wird beim ersten
//     Kodieren zu U+FFFD, danach ändert sich nichts mehr);
//   - der Speicher, den das Dekodieren braucht, bleibt in einem festen
//     Vielfachen der Eingabe.
//
// Ohne -fuzz laufen nur die Startkorpora (f.Add und testdata/fuzz), das sind
// Millisekunden. Lauf:
//
//	go test ./internal/remote/wire -run '^$' -fuzz FuzzDecodeResult -fuzztime 60s

// Die Inventare der Agenten 2.0.0 bis 2.3.0, Byte für Byte wie in
// internal/remote/compat_test.go (legacyAgents), dazu der aktuelle Stand mit
// allen Feldern.
var inventorySeeds = []string{
	`{"agent_version":"2.0.0 (Build 1, a1b2c3d4e5f6)","hostname":"vm01","kernel":"6.1.0-13-amd64"}`,
	`{"agent_version":"2.1.0 (Build 1, a1b2c3d4e5f6)","hostname":"vm01","kernel":"6.1.0-13-amd64","key_pin":"aa","ca_pin":"bb"}`,
	`{"agent_version":"2.2.0 (Build 1, a1b2c3d4e5f6)","hostname":"vm01","kernel":"6.1.0-13-amd64","key_pin":"aa","ca_pin":"bb","capabilities":["tty"]}`,
	`{"agent_version":"2.3.0 (Build 1, a1b2c3d4e5f6)","hostname":"vm01","kernel":"6.1.0-13-amd64","key_pin":"aa","ca_pin":"bb","capabilities":["tty"]}`,
	`{"agent_version":"2.4.0 (Build 3, a1b2c3d4e5f6)","protocol_version":2,"hostname":"vm01","kernel":"6.1.0-13-amd64",` +
		`"key_pin":"0123456789abcdef","ca_pin":"fedcba9876543210","capabilities":["tty","ping","jobs-v2","self-update-v2"],` +
		`"known_jobs":["0b39a4f2-1111-2222-3333-444455556666"],"rolled_back":"2.4.1","rolled_back_to":"2.4.0","rolled_back_reason":"crash-loop"}`,
}

var commandSeeds = []string{
	`{"id":"0b39a4f2-1111-2222-3333-444455556666","cmd":"dpkg -l"}`,
	`{"id":"0b39a4f2-1111-2222-3333-444455556666","cmd":"sudo -S apt-get update","stdin":"geheim\n","idle_timeout_sec":2700}`,
	`{"id":"0b39a4f2-1111-2222-3333-444455556666","cancel":true}`,
	`{"ping":true}`,
	`{"id":"../../etc/passwd","cmd":"true"}`,
	`{"id":"","cmd":"true"}`,
}

var resultSeeds = []string{
	`{"id":"0b39a4f2-1111-2222-3333-444455556666","output":"ok\n","exit_code":0}`,
	`{"id":"0b39a4f2-1111-2222-3333-444455556666","output":"E: Sperre\n","exit_code":100,"error":"exit 100","truncated":true}`,
	`{"id":"0b39a4f2-1111-2222-3333-444455556666","output":"","exit_code":0,"progress":true,"output_bytes":1048576,` +
		`"vitals":{"alive":true,"procs":4,"cpu_ms":1200,"io_bytes":65536,"silent_sec":12,"waiting":"lock"}}`,
	`{"id":"0b39a4f2-1111-2222-3333-444455556666","output":"fertig","exit_code":0,"recovered":true}`,
	`{"pong":true}`,
}

var frameSeeds = []string{
	`{"s":"0b39a4f2-1111-2222-3333-444455556666","n":1,"op":"open","t":"xterm-256color","c":120,"r":40}`,
	`{"s":"0b39a4f2-1111-2222-3333-444455556666","n":2,"op":"data","d":"bHMgLWxhCg=="}`,
	`{"s":"0b39a4f2-1111-2222-3333-444455556666","n":3,"op":"resize","c":80,"r":24}`,
	`{"s":"0b39a4f2-1111-2222-3333-444455556666","n":4,"op":"close"}`,
	`{"s":"0b39a4f2-1111-2222-3333-444455556666","n":1,"op":"opened"}`,
	`{"s":"0b39a4f2-1111-2222-3333-444455556666","n":9,"op":"closed","e":"zu viele konsolen auf diesem server offen"}`,
	`{"s":"x","n":18446744073709551615,"op":"data","d":""}`,
}

// fuzzCodec prüft Dekodieren, stabiles Kodieren und den Speicher für einen Typ.
func fuzzCodec[T any](t *testing.T, data []byte) {
	t.Helper()
	var v T
	used := allocated(func() { _ = json.Unmarshal(data, &v) })
	if limit := allocLimit(len(data)); used > limit {
		t.Fatalf("Dekodieren von %d Bytes braucht %d Bytes Speicher (Grenze %d)", len(data), used, limit)
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return
	}
	first, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Kodieren: %v", err)
	}
	var again T
	if err := json.Unmarshal(first, &again); err != nil {
		t.Fatalf("eigene Ausgabe nicht lesbar: %v\n%s", err, first)
	}
	second, err := json.Marshal(again)
	if err != nil {
		t.Fatalf("zweites Kodieren: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Kodieren nicht stabil:\n%s\n%s", first, second)
	}
}

// allocated misst die Bytes, die fn auf dem Heap anfordert. Ein Fuzz-Worker
// führt immer nur einen Aufruf zugleich aus, die Messung ist also brauchbar;
// die Grenze unten ist großzügig genug für Rauschen.
func allocated(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// allocLimit ist der erlaubte Speicher für das Dekodieren von n Bytes JSON:
// Strings, Base64 und Slices wachsen auf ein kleines Vielfaches, nie auf mehr.
func allocLimit(n int) uint64 { return 64*uint64(n) + 1<<20 }

func FuzzDecodeCommand(f *testing.F) {
	for _, s := range commandSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) { fuzzCodec[Command](t, data) })
}

func FuzzDecodeResult(f *testing.F) {
	for _, s := range resultSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) { fuzzCodec[Result](t, data) })
}

func FuzzDecodeInventory(f *testing.F) {
	for _, s := range inventorySeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) { fuzzCodec[Inventory](t, data) })
}

func FuzzDecodeTTYFrame(f *testing.F) {
	for _, s := range frameSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) { fuzzCodec[TTYFrame](t, data) })
}

// FuzzSequencer füttert den Sequencer mit beliebig vielen Rahmen aus Nummer und
// Nutzlastlänge. Er muss jede Nummer höchstens einmal und in aufsteigender
// Reihe liefern und darf nie mehr als maxHeldBytes zurückhalten: Hinter ihm
// sitzt auf beiden Seiten ein Gegenüber, das er nicht kontrolliert.
func FuzzSequencer(f *testing.F) {
	f.Add([]byte{2, 0, 1, 0, 3, 0, 1, 0})
	f.Add([]byte{1, 0, 2, 0, 3, 0, 4, 0, 1, 0})
	f.Add([]byte{200, 255, 255, 255, 255, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		var s Sequencer
		var last uint64
		for i := 0; i+1 < len(data); i += 2 {
			// Nutzlast bis knapp 1 MiB, gemeldet über ein Byte; allokiert wird nur
			// kurz vor dem Push.
			seq := uint64(data[i])
			payload := make([]byte, int(data[i+1])*4096)
			ready, err := s.Push(TTYFrame{Seq: seq, Data: payload})
			for _, fr := range ready {
				if fr.Seq <= last {
					t.Fatalf("Nummer %d nach %d geliefert", fr.Seq, last)
				}
				last = fr.Seq
			}
			if err != nil {
				return
			}
			if held := s.heldBytes(); held > maxHeldBytes {
				t.Fatalf("Sequencer hält %d Bytes (Grenze %d)", held, maxHeldBytes)
			}
		}
	})
}

// FuzzParseToken: Das Token kommt vom Anwender (Eingabe von enroll), also von
// einem Menschen, der beliebiges Zeug einfügt.
func FuzzParseToken(f *testing.F) {
	f.Add(EncodeToken(Token{AgentID: "0b39a4f2-1111-2222-3333-444455556666", Secret: "s3cr3t",
		CertFingerprint: "aa11", CAFingerprint: "cc33", CustomCAFingerprint: "ee55"}))
	f.Add(EncodeToken(Token{AgentID: "agent-1", Secret: "geheim"}))
	f.Add("lcma1.")
	f.Add("lcma1.e30")
	f.Add("lcma1.@@@@")
	f.Add("kein-token")
	f.Fuzz(func(t *testing.T, token string) {
		tok, err := ParseToken(token)
		if err != nil {
			return
		}
		if tok.AgentID == "" || tok.Secret == "" {
			t.Fatalf("unvollständiges Token angenommen: %+v", tok)
		}
		back, err := ParseToken(EncodeToken(tok))
		if err != nil || back != tok {
			t.Fatalf("Token nicht rundlaufend: %+v -> %+v (%v)", tok, back, err)
		}
	})
}

// FuzzAgentIDFromTopic: Der Topic-Name kommt vom Broker, enthält aber den Namen,
// den der Client beim Veröffentlichen angab.
func FuzzAgentIDFromTopic(f *testing.F) {
	f.Add("lcm/a/0b39a4f2-1111-2222-3333-444455556666/res")
	f.Add("lcm/a//res")
	f.Add("lcm/a/")
	f.Add("lcm/a/x")
	f.Add("")
	f.Fuzz(func(t *testing.T, topic string) {
		id := AgentIDFromTopic(topic)
		if id == "" {
			return
		}
		if len(id) >= len(topic) || topic[len(topicPrefix):len(topicPrefix)+len(id)] != id {
			t.Fatalf("ID %q gehört nicht an den Anfang von %q", id, topic)
		}
		for i := 0; i < len(id); i++ {
			if id[i] == '/' {
				t.Fatalf("ID %q enthält einen Schrägstrich", id)
			}
		}
	})
}
