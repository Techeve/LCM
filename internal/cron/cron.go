package cron

import (
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"time"
)

// EntryID benennt einen Eintrag, etwa zum Entfernen.
type EntryID int

// Entry ist der Stand eines Eintrags. Next ist die Nullzeit, solange der
// Cron nicht läuft oder der Ausdruck keinen Termin mehr hat.
type Entry struct {
	ID   EntryID
	Next time.Time
}

type entry struct {
	id       EntryID
	schedule Schedule
	next     time.Time
	run      func()
}

// Cron führt Funktionen nach Cron-Ausdrücken aus, jeden Lauf in einer
// eigenen Goroutine. Ein Panic in einem Lauf wird protokolliert und beendet
// den Dienst nicht. Termine gelten in der Ortszeit (time.Local), sofern der
// Ausdruck kein Zonen-Präfix trägt.
type Cron struct {
	mu      sync.Mutex
	entries []*entry
	lastID  EntryID
	running bool
	// wake weckt die Schleife, wenn sich die Einträge ändern; stop beendet
	// sie, done meldet ihr Ende.
	wake chan struct{}
	stop chan struct{}
	done chan struct{}
	jobs sync.WaitGroup
}

// New legt einen angehaltenen Cron an.
func New() *Cron {
	return &Cron{}
}

// AddFunc registriert fn unter spec. Läuft der Cron schon, zählt der erste
// Termin ab jetzt, sonst ab Start.
func (c *Cron) AddFunc(spec string, fn func()) (EntryID, error) {
	sched, err := Parse(spec)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastID++
	e := &entry{id: c.lastID, schedule: sched, run: fn}
	if c.running {
		e.next = sched.Next(time.Now())
		c.notify()
	}
	c.entries = append(c.entries, e)
	return e.id, nil
}

// Remove entfernt einen Eintrag; ein laufender Lauf läuft zu Ende.
func (c *Cron) Remove(id EntryID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = slices.DeleteFunc(c.entries, func(e *entry) bool { return e.id == id })
	if c.running {
		c.notify()
	}
}

// Entries liefert den Stand aller Einträge.
func (c *Cron) Entries() []Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Entry, len(c.entries))
	for i, e := range c.entries {
		out[i] = Entry{ID: e.id, Next: e.next}
	}
	return out
}

// Start berechnet die ersten Termine und startet die Schleife. Ein zweiter
// Aufruf tut nichts.
func (c *Cron) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return
	}
	c.running = true
	now := time.Now()
	for _, e := range c.entries {
		e.next = e.schedule.Next(now)
	}
	c.wake = make(chan struct{}, 1)
	c.stop = make(chan struct{})
	c.done = make(chan struct{})
	go c.loop(c.wake, c.stop, c.done)
}

// Stop hält die Schleife an und wartet, bis alle laufenden Läufe fertig
// sind.
func (c *Cron) Stop() {
	c.mu.Lock()
	running, done := c.running, c.done
	if running {
		c.running = false
		close(c.stop)
	}
	c.mu.Unlock()
	if running {
		<-done
	}
	c.jobs.Wait()
}

// notify weckt die Schleife, damit sie ihren Wecker neu stellt. Erwartet
// den gehaltenen Mutex.
func (c *Cron) notify() {
	select {
	case c.wake <- struct{}{}:
	default: // ein Wecksignal steht schon aus
	}
}

func (c *Cron) loop(wake, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		timer := time.NewTimer(c.untilNext())
		select {
		case <-stop:
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
			c.runDue(time.Now())
		}
	}
}

// idle ist die Wartezeit ohne anstehenden Termin; ein neuer Eintrag weckt
// die Schleife ohnehin.
const idle = 100000 * time.Hour

// untilNext liefert die Zeit bis zum frühesten Termin.
func (c *Cron) untilNext() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	var earliest time.Time
	for _, e := range c.entries {
		if !e.next.IsZero() && (earliest.IsZero() || e.next.Before(earliest)) {
			earliest = e.next
		}
	}
	if earliest.IsZero() {
		return idle
	}
	return time.Until(earliest)
}

// runDue startet jeden fälligen Eintrag und rechnet seinen nächsten Termin
// ab now.
func (c *Cron) runDue(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		if e.next.IsZero() || e.next.After(now) {
			continue
		}
		c.startJob(e.run)
		e.next = e.schedule.Next(now)
	}
}

func (c *Cron) startJob(run func()) {
	c.jobs.Add(1)
	go func() {
		defer c.jobs.Done()
		defer recoverJob()
		run()
	}()
}

// stackBufSize begrenzt den protokollierten Stacktrace.
const stackBufSize = 64 << 10

// recoverJob fängt einen Panic in einem Lauf ab und protokolliert ihn samt
// Stacktrace.
func recoverJob() {
	r := recover()
	if r == nil {
		return
	}
	buf := make([]byte, stackBufSize)
	buf = buf[:runtime.Stack(buf, false)]
	err, ok := r.(error)
	if !ok {
		err = fmt.Errorf("%v", r)
	}
	slog.Error("cron: panic", "error", err, "stack", "...\n"+string(buf))
}
