package agent

import (
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
	cfg := &Config{URL: "https://lcm.example.com", AgentID: "id-1", Secret: "geheim", CertFingerprint: "ff00"}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if *loaded != *cfg {
		t.Fatalf("roundtrip: %+v != %+v", loaded, cfg)
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
			res := r.Run(wire.Command{ID: tt.name, Cmd: tt.cmd, Stdin: tt.stdin})
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
	go func() { done <- r.Run(wire.Command{ID: "long", Cmd: "sleep 30"}) }()
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

func TestLimitedBufferTruncation(t *testing.T) {
	b := &limitedBuffer{max: 10}
	if _, err := b.Write([]byte("1234567890ABC")); err != nil {
		t.Fatal(err)
	}
	if !b.truncated || b.String() != "1234567890" {
		t.Fatalf("truncation kaputt: %q truncated=%v", b.String(), b.truncated)
	}
	// Weitere Writes bleiben verworfen, melden aber Erfolg (Writer-Vertrag).
	if n, _ := b.Write([]byte("xyz")); n != 3 {
		t.Fatal("write muss volle länge melden")
	}
	if !strings.HasPrefix(b.String(), "1234567890") || len(b.String()) != 10 {
		t.Fatalf("buffer gewachsen: %q", b.String())
	}
}
