package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/i18n"
)

func sampleStatus(now time.Time) Status {
	contact := now.Add(-3 * time.Second)
	return Status{
		Version: "2.4.0", Started: now.Add(-4 * time.Hour), Updated: now,
		Connection: ConnConnected, Since: now.Add(-12 * time.Minute), LastContact: &contact,
		Reconnects:     2,
		LastDisconnect: &Disconnect{At: now.Add(-20 * time.Minute), Reason: "verbindung verloren: EOF"},
		Jobs: []StatusJob{
			{ID: "job-1", State: jobRunning, Started: now.Add(-5 * time.Minute)},
			{ID: "job-2", State: jobFinished, Started: now.Add(-2 * time.Hour)},
		},
		Unproven: true,
	}
}

// Die Datei hält den Zustand, trägt 0644 und lässt kein Zwischenstück liegen.
func TestWriteStatusInhaltUndRechte(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run", "lcm-agent") // das Verzeichnis legt der Agent selbst an
	want := sampleStatus(time.Now().UTC().Truncate(time.Second))
	if err := WriteStatus(dir, want); err != nil {
		t.Fatal(err)
	}
	got, _, err := ReadStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != want.Version || got.Connection != ConnConnected || got.Reconnects != 2 || !got.Unproven ||
		len(got.Jobs) != 2 || got.Jobs[0].ID != "job-1" || got.Jobs[1].State != jobFinished ||
		got.LastDisconnect == nil || got.LastDisconnect.Reason != want.LastDisconnect.Reason ||
		got.LastContact == nil || !got.LastContact.Equal(*want.LastContact) {
		t.Fatalf("gelesen: %+v", got)
	}

	info, err := os.Stat(filepath.Join(dir, StatusFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("Rechte = %v, want 0644", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("Verzeichnis enthält mehr als die Datei: %v", entries)
	}
}

// Ein Leser sieht nie eine halb geschriebene Datei.
func TestWriteStatusIstAtomar(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := WriteStatus(dir, sampleStatus(now)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 300 {
			if err := WriteStatus(dir, sampleStatus(time.Now())); err != nil {
				t.Error(err)
				return
			}
		}
	})
	for range 300 {
		if _, _, err := ReadStatus(dir); err != nil {
			t.Fatalf("halb geschriebene Datei gelesen: %v", err)
		}
	}
	wg.Wait()
}

func TestReadStatusFehlendeDatei(t *testing.T) {
	_, _, err := ReadStatus(t.TempDir())
	if !os.IsNotExist(err) && !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("Fehler = %v", err)
	}
}

func TestStatusStale(t *testing.T) {
	now := time.Now()
	if (Status{Updated: now.Add(-StatusStale)}).Stale(now) {
		t.Error("genau an der Grenze noch frisch")
	}
	if !(Status{Updated: now.Add(-StatusStale - time.Second)}).Stale(now) {
		t.Error("darüber veraltet")
	}
}

func germanOutput(t *testing.T) {
	t.Helper()
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	if i18n.Lang() != "de" {
		t.Fatal("Sprache nicht auf Deutsch gestellt")
	}
}

func TestFormatStatusFrisch(t *testing.T) {
	germanOutput(t)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	out := FormatStatus(sampleStatus(now), now)
	for _, want := range []string{
		"lcm-agent:", "2.4.0",
		"verbunden seit 12 min",
		"vor 3 s",
		"2 seit dem Start",
		"vor 20 min - verbindung verloren: EOF",
		"job-1 laeuft seit 5 min",
		"job-2 fertig, Ergebnis von LCM noch nicht bestaetigt (gestartet vor 2 h)",
		"unbewaehrt",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Ausgabe ohne %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "scheint nicht zu laufen") {
		t.Errorf("frische Datei als veraltet gemeldet:\n%s", out)
	}
}

func TestFormatStatusVeraltetNenntDenStand(t *testing.T) {
	germanOutput(t)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	s := sampleStatus(now.Add(-5 * time.Minute))
	s.Unproven = false
	out := FormatStatus(s, now)
	if !strings.HasPrefix(out, "Der Agent scheint nicht zu laufen - der Stand ist 5 min alt.") {
		t.Errorf("Hinweis fehlt:\n%s", out)
	}
	if !strings.Contains(out, "bewaehrt") || strings.Contains(out, "unbewaehrt") {
		t.Errorf("Binary-Zeile:\n%s", out)
	}
}

func TestFormatStatusOhneBesonderheiten(t *testing.T) {
	germanOutput(t)
	now := time.Now()
	out := FormatStatus(Status{Version: "2.4.0", Updated: now, Connection: ConnConnecting, Since: now}, now)
	for _, want := range []string{"baut Verbindung auf seit 0 s", "noch keiner", "keine seit dem Start", "keine"} {
		if !strings.Contains(out, want) {
			t.Errorf("Ausgabe ohne %q:\n%s", want, out)
		}
	}
}

func TestFormatStatusRueckfall(t *testing.T) {
	germanOutput(t)
	now := time.Now()
	s := sampleStatus(now)
	s.RolledBack = &RolledBack{From: "2.5.0", To: "2.4.0", Reason: reasonNotReady}
	if out := FormatStatus(s, now); !strings.Contains(out, "Version 2.5.0 ist gescheitert (not-ready), es laeuft 2.4.0") {
		t.Errorf("Rückfall fehlt:\n%s", out)
	}
}

func TestAge(t *testing.T) {
	tests := map[time.Duration]string{
		-time.Second: "0 s", 5 * time.Second: "5 s", 90 * time.Second: "1 min",
		3 * time.Hour: "3 h", 50 * time.Hour: "2 d",
	}
	for d, want := range tests {
		if got := age(d); got != want {
			t.Errorf("age(%s) = %q, want %q", d, got, want)
		}
	}
}

// Ein gescheiterter Verbindungsaufbau ist keine Trennung: Vor der ersten
// bereiten Verbindung bleibt „Letzte Trennung" leer, auch wenn der erste
// Versuch abgewiesen wurde (CI-Flake: der Broker lauschte noch nicht).
func TestGescheiterterAufbauIstKeineTrennung(t *testing.T) {
	c := NewClient(&Config{AgentID: "a"}, nil, "test")
	now := time.Now()
	c.link.connecting(now.Add(-time.Second))
	c.link.lost(now.Add(-900*time.Millisecond), "verbinden: connection refused")
	c.link.connecting(now.Add(-800 * time.Millisecond))
	c.link.ready(now.Add(-700 * time.Millisecond))

	s := c.snapshot(now)
	if s.Connection != ConnConnected || s.Reconnects != 0 || s.LastDisconnect != nil {
		t.Fatalf("erste Verbindung nach abgewiesenem Versuch: %+v", s)
	}

	c.link.lost(now, "verbindung verloren: EOF")
	c.link.connecting(now)
	c.link.lost(now, "verbinden: connection refused")
	s = c.snapshot(now)
	if s.LastDisconnect == nil || s.LastDisconnect.Reason != "verbindung verloren: EOF" {
		t.Fatalf("Trennung einer bereiten Verbindung fehlt oder wurde überschrieben: %+v", s.LastDisconnect)
	}
}

// Der Agent schreibt, was er weiß - und nichts Geheimes.
func TestSnapshotNimmtJobsLinkUndBewaehrungAuf(t *testing.T) {
	jobs := t.TempDir()
	c := NewClient(&Config{URL: "https://lcm.example", AgentID: "agent-1", Secret: "streng-geheim"}, nil, "2.4.0 (Build 7)").WithJobsDir(jobs)
	if _, err := c.runner.store.create("job-1", "sleep 100"); err != nil {
		t.Fatal(err)
	}
	c.unproven.Store(true)
	now := time.Now()
	c.link.ready(now.Add(-time.Minute))
	c.link.lost(now.Add(-30*time.Second), "verbindung verloren")
	c.link.connecting(now.Add(-20 * time.Second))
	c.link.ready(now.Add(-10 * time.Second))

	s := c.snapshot(now)
	if s.Version != "2.4.0 (Build 7)" || s.Connection != ConnConnected || s.Reconnects != 1 || !s.Unproven {
		t.Fatalf("Zustand: %+v", s)
	}
	if len(s.Jobs) != 1 || s.Jobs[0].ID != "job-1" || s.Jobs[0].State != jobRunning || s.Jobs[0].Started.IsZero() {
		t.Fatalf("Jobs: %+v", s.Jobs)
	}
	if s.LastDisconnect == nil || s.LastDisconnect.Reason != "verbindung verloren" {
		t.Fatalf("Trennung: %+v", s.LastDisconnect)
	}
	raw, _ := json.Marshal(s)
	for _, secret := range []string{"streng-geheim", "agent-1", "lcm.example"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("Zustandsdatei enthält %q: %s", secret, raw)
		}
	}
}

func TestSnapshotOhneJobsHatLeereListe(t *testing.T) {
	c := NewClient(&Config{AgentID: "a"}, nil, "x").WithJobsDir(t.TempDir())
	raw, _ := json.Marshal(c.snapshot(time.Now()))
	if !bytes.Contains(raw, []byte(`"jobs":[]`)) {
		t.Fatalf("jobs sollte [] sein: %s", raw)
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"", slog.LevelInfo, false},
		{"info", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{"warn", slog.LevelWarn, false},
		{" WARN ", slog.LevelWarn, false},
		{"error", slog.LevelInfo, true},
		{"laut", slog.LevelInfo, true},
	}
	for _, tt := range tests {
		got, err := ParseLogLevel(tt.in)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("ParseLogLevel(%q) = %v, %v", tt.in, got, err)
		}
	}
}

func TestResolveLogLevelSchalterGehtVor(t *testing.T) {
	if got, err := ResolveLogLevel("debug", "warn"); got != slog.LevelDebug || err != nil {
		t.Errorf("Schalter vor Konfiguration: %v, %v", got, err)
	}
	if got, err := ResolveLogLevel("", "warn"); got != slog.LevelWarn || err != nil {
		t.Errorf("Konfiguration: %v, %v", got, err)
	}
	if got, err := ResolveLogLevel("", ""); got != slog.LevelInfo || err != nil {
		t.Errorf("Vorgabe: %v, %v", got, err)
	}
	if got, err := ResolveLogLevel("", "laut"); got != slog.LevelInfo || err == nil {
		t.Errorf("unbrauchbar: %v, %v (info mit Fehler erwartet)", got, err)
	}
}

func TestLogLevelAusDerKonfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	cfg := &Config{URL: "https://lcm.example", AgentID: "a", Secret: "s", LogLevel: "warn"}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil || loaded.LogLevel != "warn" {
		t.Fatalf("geladen: %+v, %v", loaded, err)
	}
}

// warnLog liefert ein Journal auf Stufe warn, wie es `log_level: warn` einstellt.
func warnLog() (*slog.Logger, *lockedBuffer) {
	buf := &lockedBuffer{}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})), buf
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Trennung, Wiederverbindung und Binary-Wechsel stehen mit Grund im Journal, auch
// auf Stufe warn.
func TestBinaerWechselStehtAufWarn(t *testing.T) {
	log, out := warnLog()
	c := NewClient(&Config{AgentID: "a"}, log, "2.4.0")
	c.logSwitch("/usr/bin/lcm-agent")
	for _, want := range []string{"level=WARN", "switching to the new lcm-agent binary", "reason=", "new binary installed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Journal ohne %q: %s", want, out.String())
		}
	}
}

// Mit echtem Broker: Der Broker trennt den Agent, der baut neu auf; Stop trennt
// gewollt. Alles steht auch auf warn im Journal, die Zustandsdatei hält es fest.
func TestStatusDateiUndJournalBeiTrennungUndReconnect(t *testing.T) {
	broker, addr := startHelperBroker(t)
	log, journal := warnLog()
	dir := t.TempDir()
	cfg := &Config{URL: "http://" + addr, AgentID: "agent-status", Secret: "x"}
	c := NewClient(cfg, log, "test").WithJobsDir(t.TempDir()).WithStatusDir(dir).
		WithTiming(Timing{BackoffMin: 20 * time.Millisecond, BackoffMax: 50 * time.Millisecond, StatusEvery: 20 * time.Millisecond})
	done := make(chan struct{})
	go func() { _ = c.Run(); close(done) }()
	t.Cleanup(func() { c.Stop(); <-done })

	waitStatus := func(what string, cond func(Status) bool) Status {
		t.Helper()
		var s Status
		waitHelper(t, what, func() bool {
			var err error
			s, _, err = ReadStatus(dir)
			return err == nil && cond(s)
		})
		return s
	}
	first := waitStatus("verbunden", func(s Status) bool { return s.Connection == ConnConnected })
	if first.Reconnects != 0 || first.LastDisconnect != nil || first.LastContact == nil {
		t.Fatalf("erste Verbindung: %+v", first)
	}

	cl, ok := broker.Clients.Get(cfg.AgentID)
	if !ok {
		t.Fatal("agent nicht am broker")
	}
	_ = broker.DisconnectClient(cl, packets.ErrServerShuttingDown)
	s := waitStatus("wieder verbunden", func(s Status) bool { return s.Reconnects == 1 && s.Connection == ConnConnected })
	if s.LastDisconnect == nil || s.LastDisconnect.Reason == "" {
		t.Fatalf("Trennung ohne Grund: %+v", s)
	}

	c.Stop()
	<-done
	final, _, err := ReadStatus(dir)
	if err != nil || final.Connection != ConnDisconnected || final.LastDisconnect == nil {
		t.Fatalf("Stand nach Stop: %+v, %v", final, err)
	}

	logged := journal.String()
	for _, want := range []string{
		"connection to lcm ended - reconnecting", "reconnected to lcm server", "disconnected from lcm - the agent is stopping", "reason=",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("Journal (warn) ohne %q:\n%s", want, logged)
		}
	}
	if strings.Contains(logged, "level=INFO") || strings.Contains(logged, "level=DEBUG") {
		t.Errorf("Journal auf warn enthält niedrigere Stufen:\n%s", logged)
	}
}
