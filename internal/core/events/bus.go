// Package events ist der schlanke In-Process-Ereignisbus von LCM.
//
// Die Dienste melden hier, DASS sich etwas geändert hat (Job gestartet, Ampel
// verworfen, Alarm ausgelöst ...); wer es wissen will, abonniert. Der Bus
// transportiert keine Daten, die man nicht ohnehin per REST holen könnte -
// ein Ereignis heißt „hat sich geändert", die Einzelheiten holt der Empfänger
// über die normale, rechtegeprüfte API.
//
// Zwei Eigenschaften sind Absicht:
//
//   - Veröffentlichen blockiert nie. Der Aufrufer sitzt mitten in einem
//     Job-Abschluss oder einer Alarm-Auswertung; ein langsamer Abonnent
//     (zäher Browser, voller Socket) darf ihn nicht aufhalten.
//   - Ein Abonnent, der nicht mitkommt, verliert Ereignisse und erfährt das:
//     Sein Resync-Signal sagt „es fehlt etwas, lade alles neu". Das ist
//     ehrlicher als stilles Verwerfen und billiger als eine unbegrenzte
//     Warteschlange.
//
// Der Bus startet keine Goroutinen. Wer abonniert, muss die Subscription
// schließen.
package events

import (
	"sync"
	"sync/atomic"
)

// Ereignisarten. Die Namen sind Teil des Protokolls zum Browser (siehe
// controllers.EventHub) und dürfen nicht umbenannt werden.
const (
	TypeServerBusy   = "server.busy"   // Job gestartet, beendet oder wartend auf einem Server
	TypeServerStatus = "server.status" // Ampel geändert oder Statuscache verworfen
	TypeJobProgress  = "job.progress"  // ein laufender Job hat neue Ausgabe
	TypeJobFinished  = "job.finished"  // ein Job hat einen Endzustand erreicht
	TypeAlertFired   = "alert.fired"   // ein Alarm wurde ausgelöst
	TypeBulkProgress = "bulk.progress" // Fortschritt des Sammel-Updates
	TypeSystemBuild  = "system.build"  // neue Version oder Selbst-Update-Fortschritt
)

// subscriberBuffer ist die Zahl der Ereignisse, die ein Abonnent aufstauen
// darf, bevor er als „nicht mitgekommen" gilt.
const subscriberBuffer = 256

// Event ist eine Meldung auf dem Bus. Die Routing-Felder benutzt der
// Empfänger, um zu entscheiden, WEM das Ereignis gezeigt werden darf; Data ist
// die JSON-Nutzlast für den Browser (siehe payload.go).
type Event struct {
	Type string
	// ServerID: Server, auf den sich das Ereignis bezieht (0 = keiner, etwa
	// bei serverlosen System-Jobs, Selbstbeobachtungs-Alarmen oder System-Ereignissen).
	ServerID uint
	// All: das Ereignis betrifft alle Server (verworfener Statuscache).
	All bool
	// JobID: Job, auf den sich das Ereignis bezieht (leer = keiner).
	JobID string
	Data  any
}

// Bus verteilt Ereignisse an alle Abonnenten. Der Nullwert ist NICHT
// brauchbar, aber ein nil-Bus ist es: Publish und Generation tun dann nichts,
// damit Dienste ohne verdrahteten Bus (Tests) keine Fallunterscheidung brauchen.
type Bus struct {
	mu   sync.RWMutex
	subs map[*Subscription]struct{}
	// counts zählt die veröffentlichten Ereignisse je Art (Generation).
	counts sync.Map // Art → *atomic.Uint64
}

// NewBus legt einen leeren Bus an.
func NewBus() *Bus {
	return &Bus{subs: map[*Subscription]struct{}{}}
}

// Subscription ist ein Abonnement aller Ereignisse. Wer filtern will, filtert
// beim Empfang.
type Subscription struct {
	bus    *Bus
	events chan Event
	resync chan struct{}
}

// Subscribe meldet einen neuen Abonnenten an. Close gibt ihn wieder frei.
func (b *Bus) Subscribe() *Subscription {
	s := &Subscription{
		bus:    b,
		events: make(chan Event, subscriberBuffer),
		resync: make(chan struct{}, 1),
	}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Events liefert die Ereignisse in der Reihenfolge der Veröffentlichung. Der
// Kanal wird nie geschlossen - wer fertig ist, ruft Close und hört auf zu lesen.
func (s *Subscription) Events() <-chan Event { return s.events }

// Resync meldet, dass Ereignisse verloren gingen, weil der Abonnent zu langsam
// war. Der Empfänger soll seinem Gegenüber sagen, dass es alles neu laden muss.
func (s *Subscription) Resync() <-chan struct{} { return s.resync }

// Close beendet das Abonnement. Mehrfaches Schließen ist harmlos.
func (s *Subscription) Close() {
	s.bus.mu.Lock()
	delete(s.bus.subs, s)
	s.bus.mu.Unlock()
}

// Publish verteilt ein Ereignis an alle Abonnenten und kehrt sofort zurück.
// Ist der Puffer eines Abonnenten voll, verliert er das Ereignis und bekommt
// stattdessen das Resync-Signal.
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}
	b.counter(e.Type).Add(1)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		select {
		case s.events <- e:
		default:
			select {
			case s.resync <- struct{}{}:
			default: // Signal steht schon an
			}
		}
	}
}

// Generation liefert die Summe der bisher veröffentlichten Ereignisse der
// genannten Arten. Ändert sich der Wert, ist seit dem letzten Blick etwas
// passiert - damit entwertet ein Zwischenspeicher sich selbst, ohne
// mitzuhören und ohne eigene Goroutine.
func (b *Bus) Generation(types ...string) uint64 {
	if b == nil {
		return 0
	}
	var sum uint64
	for _, t := range types {
		sum += b.counter(t).Load()
	}
	return sum
}

func (b *Bus) counter(eventType string) *atomic.Uint64 {
	if c, ok := b.counts.Load(eventType); ok {
		return c.(*atomic.Uint64)
	}
	c, _ := b.counts.LoadOrStore(eventType, new(atomic.Uint64))
	return c.(*atomic.Uint64)
}

// Subscribers liefert die Zahl der aktuellen Abonnenten (Diagnose und Tests).
func (b *Bus) Subscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
