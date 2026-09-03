package logging

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const beispielLog = `time=2026-09-01T08:00:00.000+02:00 level=INFO msg="scheduler started" active_schedules=11
time=2026-09-01T08:00:01.000+02:00 level=DEBUG msg="ssh command" host=web01
LCM v3.5.0 - Banner ohne logfmt
time=2026-09-01T08:00:02.000+02:00 level=WARN msg="server unreachable" server=web01
time=2026-09-01T08:00:03.000+02:00 level=ERROR msg="job start failed" server=web01
`

func schreibeLog(t *testing.T, inhalt string) string {
	t.Helper()
	pfad := filepath.Join(t.TempDir(), "lcm.log")
	if err := os.WriteFile(pfad, []byte(inhalt), 0o600); err != nil {
		t.Fatal(err)
	}
	return pfad
}

// TestTailZerlegtUndBehaeltAlles: Auch Zeilen, die nicht von slog stammen
// (Banner, GORM), gehören in die Ansicht - eine Zeile zu verschlucken, weil
// sie nicht ins Schema passt, wäre in einer Fehlersuche das Schlechteste.
func TestTailZerlegtUndBehaeltAlles(t *testing.T) {
	entries, err := Tail(schreibeLog(t, beispielLog), 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("erwartet 5 Zeilen, waren %d", len(entries))
	}
	if entries[0].Level != "INFO" || entries[0].Msg != "scheduler started" {
		t.Errorf("erste Zeile falsch zerlegt: %+v", entries[0])
	}
	// Die Banner-Zeile hat kein Level - sie bleibt trotzdem drin.
	if entries[2].Level != "" || entries[2].Raw == "" {
		t.Errorf("Zeile ohne logfmt darf nicht verschwinden: %+v", entries[2])
	}
}

// TestTailFiltertNachSchwere: Wer nach Störungen sucht, will die
// Debug-Zeilen nicht sehen.
func TestTailFiltertNachSchwere(t *testing.T) {
	entries, err := Tail(schreibeLog(t, beispielLog), 0, "warn", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Level == "INFO" || e.Level == "DEBUG" {
			t.Errorf("zu leichte Zeile durchgelassen: %+v", e)
		}
	}
	if len(entries) != 3 { // WARN, ERROR und die Zeile ohne Level
		t.Errorf("erwartet 3 Zeilen (inkl. der ohne Level), waren %d", len(entries))
	}
}

// TestTailFiltertNachText: Die Volltextsuche greift auf die ganze Zeile, nicht
// nur auf die Meldung - der Servername steht in einem Feld dahinter.
func TestTailFiltertNachText(t *testing.T) {
	entries, err := Tail(schreibeLog(t, beispielLog), 0, "", "unreachable")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Msg != "server unreachable" {
		t.Errorf("Textsuche falsch: %+v", entries)
	}
}

// TestTailBegrenzt: Die Anzeige zeigt die JÜNGSTEN Zeilen, nicht die ältesten.
func TestTailBegrenzt(t *testing.T) {
	entries, err := Tail(schreibeLog(t, beispielLog), 2, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("erwartet 2 Zeilen, waren %d", len(entries))
	}
	if entries[1].Msg != "job start failed" {
		t.Errorf("die jüngste Zeile fehlt: %+v", entries[1])
	}
}

// TestFollowLiefertNeueZeilen: Die Live-Ansicht beginnt am aktuellen Ende und
// meldet, was danach dazukommt.
func TestFollowLiefertNeueZeilen(t *testing.T) {
	pfad := schreibeLog(t, beispielLog)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	neu := make(chan Entry, 4)
	go func() { _ = Follow(ctx, pfad, func(e Entry) { neu <- e }) }()

	// Kurz warten, damit Follow am Ende steht, dann anhängen.
	time.Sleep(50 * time.Millisecond)
	f, err := os.OpenFile(pfad, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("time=2026-09-01T08:00:04.000+02:00 level=INFO msg=\"queued job started\"\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	select {
	case e := <-neu:
		if e.Msg != "queued job started" {
			t.Errorf("falsche Zeile gemeldet: %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("die neue Zeile wurde nicht gemeldet")
	}
}
