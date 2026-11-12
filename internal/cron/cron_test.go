package cron

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitFor wartet höchstens drei Sekunden auf ch.
func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s blieb aus", what)
	}
}

func TestLaufZumTermin(t *testing.T) {
	c := New()
	ran := make(chan struct{}, 10)
	if _, err := c.AddFunc("@every 1s", func() { ran <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	c.Start()
	defer c.Stop()
	waitFor(t, ran, "der Lauf")
}

func TestUngueltigerAusdruck(t *testing.T) {
	if _, err := New().AddFunc("* * *", func() {}); err == nil {
		t.Fatal("drei Felder angenommen")
	}
}

// TestNextErstAbStart: Wie bisher steht der nächste Termin erst fest,
// wenn der Cron läuft; danach rechnen Einträge ab ihrer Anlage.
func TestNextErstAbStart(t *testing.T) {
	c := New()
	before, _ := c.AddFunc("0 3 * * *", func() {})
	if next := c.Entries()[0].Next; !next.IsZero() {
		t.Fatalf("vor dem Start schon ein Termin: %s", next)
	}
	from := time.Now()
	c.Start()
	defer c.Stop()
	after, _ := c.AddFunc("*/5 * * * *", func() {})
	entries := c.Entries()
	to := time.Now()

	// Zwischen from und to kann eine Fünf-Minuten-Grenze liegen.
	want := map[EntryID]string{before: "0 3 * * *", after: "*/5 * * * *"}
	for _, e := range entries {
		sched, _ := Parse(want[e.ID])
		if !e.Next.Equal(sched.Next(from)) && !e.Next.Equal(sched.Next(to)) {
			t.Errorf("eintrag %d: nächster Termin %s, erwartet %s", e.ID, e.Next, sched.Next(from))
		}
	}
}

func TestRemove(t *testing.T) {
	c := New()
	c.Start()
	defer c.Stop()
	ran := make(chan struct{}, 10)
	id, _ := c.AddFunc("@every 1s", func() { ran <- struct{}{} })
	keep, _ := c.AddFunc("@every 1h", func() {})
	c.Remove(id)
	if entries := c.Entries(); len(entries) != 1 || entries[0].ID != keep {
		t.Fatalf("nach Remove: %+v", entries)
	}
	select {
	case <-ran:
		t.Fatal("entfernter Eintrag lief trotzdem")
	case <-time.After(1500 * time.Millisecond):
	}
}

// TestPanicWirdAbgefangen: Ein Panic in einem Lauf landet im Log und
// beendet weder den Prozess noch den Cron.
func TestPanicWirdAbgefangen(t *testing.T) {
	var buf syncBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	c := New()
	panicked := make(chan struct{}, 10)
	_, _ = c.AddFunc("@every 1s", func() {
		defer func() { panicked <- struct{}{} }()
		panic("kaputt")
	})
	c.Start()
	waitFor(t, panicked, "der erste Lauf")
	waitFor(t, panicked, "der zweite Lauf")
	c.Stop()

	log := buf.String()
	if !strings.Contains(log, "cron: panic") || !strings.Contains(log, "kaputt") {
		t.Errorf("Panic nicht protokolliert: %s", log)
	}
}

// TestStopWartetAufLaeufe: Stop kehrt erst zurück, wenn laufende Läufe
// fertig sind - der Dienst fährt sonst mitten in einem Backup herunter.
func TestStopWartetAufLaeufe(t *testing.T) {
	c := New()
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	_, _ = c.AddFunc("@every 1s", func() {
		started <- struct{}{}
		<-release
	})
	c.Start()
	waitFor(t, started, "der Lauf")

	stopped := make(chan struct{})
	go func() {
		c.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop kehrte vor dem Ende des Laufs zurück")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	waitFor(t, stopped, "das Ende von Stop")
}

// syncBuffer ist ein bytes.Buffer, den Läufe und Test gleichzeitig nutzen.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
