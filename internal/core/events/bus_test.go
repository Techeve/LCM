package events

import (
	"sync"
	"testing"
	"time"
)

func receive(t *testing.T, s *Subscription) Event {
	t.Helper()
	select {
	case e := <-s.Events():
		return e
	case <-time.After(time.Second):
		t.Fatal("kein Ereignis angekommen")
		return Event{}
	}
}

func TestPublishErreichtAlleAbonnenten(t *testing.T) {
	bus := NewBus()
	a, b := bus.Subscribe(), bus.Subscribe()
	defer a.Close()
	defer b.Close()

	bus.Publish(Event{Type: TypeJobFinished, JobID: "j1"})

	if got := receive(t, a); got.JobID != "j1" {
		t.Errorf("a: %+v", got)
	}
	if got := receive(t, b); got.JobID != "j1" {
		t.Errorf("b: %+v", got)
	}
}

func TestLangsamerAbonnentBlockiertNichtUndBekommtResync(t *testing.T) {
	bus := NewBus()
	slow := bus.Subscribe()
	fast := bus.Subscribe()
	defer slow.Close()
	defer fast.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range subscriberBuffer + 50 {
			bus.Publish(Event{Type: TypeServerStatus, All: true})
			// fast liest mit, slow nie.
			<-fast.Events()
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish hat auf den langsamen Abonnenten gewartet")
	}

	select {
	case <-slow.Resync():
	default:
		t.Error("der langsame Abonnent bekam kein Resync-Signal")
	}
	select {
	case <-fast.Resync():
		t.Error("der schnelle Abonnent bekam ein Resync-Signal")
	default:
	}
}

func TestCloseBeendetDieZustellung(t *testing.T) {
	bus := NewBus()
	s := bus.Subscribe()
	s.Close()
	s.Close() // doppeltes Schließen ist harmlos

	bus.Publish(Event{Type: TypeJobFinished})

	if n := bus.Subscribers(); n != 0 {
		t.Errorf("Abonnenten = %d, erwartet 0", n)
	}
	select {
	case e := <-s.Events():
		t.Errorf("nach Close ein Ereignis erhalten: %+v", e)
	default:
	}
}

func TestNilBusIstHarmlos(t *testing.T) {
	var bus *Bus
	bus.Publish(Event{Type: TypeJobFinished})
	if g := bus.Generation(TypeJobFinished); g != 0 {
		t.Errorf("Generation = %d", g)
	}
}

func TestGenerationZaehltNurDieGenanntenArten(t *testing.T) {
	bus := NewBus()
	before := bus.Generation(TypeServerStatus, TypeJobFinished)

	bus.Publish(Event{Type: TypeJobProgress})
	if bus.Generation(TypeServerStatus, TypeJobFinished) != before {
		t.Error("job.progress hat die Generation verändert")
	}
	bus.Publish(Event{Type: TypeJobFinished})
	bus.Publish(Event{Type: TypeServerStatus})
	if got := bus.Generation(TypeServerStatus, TypeJobFinished); got != before+2 {
		t.Errorf("Generation = %d, erwartet %d", got, before+2)
	}
}

// TestParallelerBetrieb läuft mit -race: Veröffentlichen, Anmelden und
// Abmelden gleichzeitig.
func TestParallelerBetrieb(t *testing.T) {
	bus := NewBus()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 500 {
				bus.Publish(ServerStatus(nil))
			}
		})
	}
	for range 4 {
		wg.Go(func() {
			for range 100 {
				s := bus.Subscribe()
				select {
				case <-s.Events():
				default:
				}
				s.Close()
			}
		})
	}
	wg.Wait()
	if n := bus.Subscribers(); n != 0 {
		t.Errorf("Abonnenten übrig: %d", n)
	}
}

func TestServerStatusNilMeintAlle(t *testing.T) {
	if e := ServerStatus(nil); !e.All || e.ServerID != 0 {
		t.Errorf("%+v", e)
	}
	id := uint(7)
	if e := ServerStatus(&id); e.All || e.ServerID != 7 {
		t.Errorf("%+v", e)
	}
}
