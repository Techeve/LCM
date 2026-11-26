package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"LCM/internal/remote/wire"
)

func TestNormalizeServerURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"https komplett", "https://lcm.example.com:8443", "https://lcm.example.com:8443", false},
		{"ohne schema → https", "lcm.example.com:8443", "https://lcm.example.com:8443", false},
		{"pfad wird entfernt", "https://lcm.example.com/irgendwas/", "https://lcm.example.com", false},
		{"http bleibt (dev)", "http://127.0.0.1:8090", "http://127.0.0.1:8090", false},
		{"whitespace", "  https://lcm.example.com  ", "https://lcm.example.com", false},
		{"leer", "", "", true},
		{"falsches schema", "ftp://lcm.example.com", "", true},
		{"nur port, ohne namen", "https://:8443", "", true},
		{"nur port ohne schema", ":8443", "", true},
		{"nur schema", "https://", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeServerURL(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBrokerURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://lcm.example.com:8443", "wss://lcm.example.com:8443/mqtt"},
		{"http://127.0.0.1:8090", "ws://127.0.0.1:8090/mqtt"},
	}
	for _, tt := range tests {
		cfg := &Config{URL: tt.url, AgentID: "a", Secret: "s"}
		got, err := cfg.BrokerURL()
		if err != nil {
			t.Fatalf("BrokerURL(%q): %v", tt.url, err)
		}
		if got != tt.want {
			t.Fatalf("BrokerURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestConfigSaveLoad(t *testing.T) {
	path := t.TempDir() + "/agent.json"
	cfg := &Config{URL: "https://lcm.example.com", AgentID: "id-1", Secret: "geheim", CertFingerprint: "ff00",
		CustomCAFingerprints: []string{"aa11", "bb22"}}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, cfg) {
		t.Fatalf("roundtrip: %+v != %+v", loaded, cfg)
	}
}

// TestConfigOhneEigeneCAs: Eine Konfiguration aus einem Agent vor den eigenen
// Zertifikaten lädt unverändert, und ohne Pins schreibt Save das Feld nicht.
func TestConfigOhneEigeneCAs(t *testing.T) {
	path := t.TempDir() + "/agent.json"
	old := `{"url":"https://lcm.example.com","agent_id":"id-1","secret":"geheim","ca_fingerprint":"cc33"}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CAFingerprint != "cc33" || len(cfg.CustomCAFingerprints) != 0 {
		t.Errorf("alte Konfiguration gelesen als %+v", cfg)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	if strings.Contains(string(saved), "custom_ca_fingerprints") {
		t.Errorf("leeres Feld geschrieben: %s", saved)
	}
}

func TestRunnerRun(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		stdin    string
		wantOut  string
		wantCode int
	}{
		{"echo", "echo hallo", "", "hallo\n", 0},
		{"exit-code", "echo weh >&2; exit 3", "", "weh\n", 3},
		{"stdin", "cat", "aus stdin", "aus stdin", 0},
		{"stdout+stderr kombiniert", "echo eins; echo zwei >&2", "", "eins\nzwei\n", 0},
	}
	r := NewRunner()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := r.Run(wire.Command{ID: tt.name, Cmd: tt.cmd, Stdin: tt.stdin}, nil)
			if res.Error != "" {
				t.Fatalf("unerwarteter fehler: %s", res.Error)
			}
			if res.Output != tt.wantOut || res.ExitCode != tt.wantCode {
				t.Fatalf("got (%q, %d), want (%q, %d)", res.Output, res.ExitCode, tt.wantOut, tt.wantCode)
			}
		})
	}
}

func TestRunnerCancel(t *testing.T) {
	r := NewRunner()
	done := make(chan wire.Result, 1)
	go func() { done <- r.Run(wire.Command{ID: "long", Cmd: "sleep 30"}, nil) }()
	// Warten, bis das Kommando registriert ist, dann abbrechen.
	deadline := time.After(5 * time.Second)
	for {
		r.mu.Lock()
		_, running := r.running["long"]
		r.mu.Unlock()
		if running {
			break
		}
		select {
		case <-deadline:
			t.Fatal("kommando nie gestartet")
		case <-time.After(10 * time.Millisecond):
		}
	}
	r.Cancel("long")
	select {
	case res := <-done:
		if res.ExitCode == 0 {
			t.Fatalf("abgebrochenes kommando meldet exit 0: %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel hat das kommando nicht beendet")
	}
}

// TestReadOutputBehaeltAnfangUndEnde: Über dem Limit bleiben die erste und die
// letzte Hälfte stehen, die Mitte fällt weg und wird gezählt. Vorher blieb nur
// der Anfang - die Fehler am Ende eines apt-Laufs fehlten.
func TestReadOutputBehaeltAnfangUndEnde(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out")
	if err := os.WriteFile(path, []byte("12345abcdefghXYZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	head, tail, omitted, err := readOutput(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if string(head) != "12345" || string(tail) != "ghXYZ" || omitted != 6 {
		t.Fatalf("got head=%q tail=%q omitted=%d", head, tail, omitted)
	}
	got := joinOutput(head, tail, omitted)
	if !strings.HasPrefix(got, "12345\n") || !strings.HasSuffix(got, "\nghXYZ") || !strings.Contains(got, "6 Bytes") {
		t.Fatalf("ausgabe ohne hinweis auf die lücke: %q", got)
	}
}

// TestReadOutputKleineAusgabeUndFehlendeDatei: Unter dem Limit kommt alles in
// den Anfang; fehlt die Datei, war die Ausgabe leer.
func TestReadOutputKleineAusgabeUndFehlendeDatei(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	if err := os.WriteFile(path, []byte("kurz"), 0o600); err != nil {
		t.Fatal(err)
	}
	head, tail, omitted, err := readOutput(path, 100)
	if err != nil || string(head) != "kurz" || len(tail) != 0 || omitted != 0 {
		t.Fatalf("got (%q, %q, %d, %v)", head, tail, omitted, err)
	}
	if head, tail, omitted, err = readOutput(filepath.Join(dir, "gibt-es-nicht"), 100); err != nil || len(head)+len(tail)+omitted != 0 {
		t.Fatalf("fehlende datei: (%q, %q, %d, %v)", head, tail, omitted, err)
	}
}
