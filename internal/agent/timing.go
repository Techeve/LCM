package agent

import (
	"math/rand/v2"
	"time"

	"LCM/internal/remote/wire"
)

// Timing sind die Zeiten der Verbindung des Agents zu LCM. Ein Feld mit dem
// Nullwert gilt als nicht gesetzt und nimmt den Vorgabewert (DefaultTiming);
// Tests mit Fehlerinjektion verkürzen sie über Client.WithTiming.
type Timing struct {
	KeepAlive      time.Duration // Abstand der MQTT-Pings (TCP-Ebene)
	PingTimeout    time.Duration // so lange wartet er auf die Antwort darauf
	ConnectTimeout time.Duration // Obergrenze eines Verbindungsversuchs
	SubscribeWait  time.Duration // Obergrenze für die Bestätigung eines Abonnements
	BackoffMin     time.Duration // Wartezeit vor dem ersten Wiederaufbau
	BackoffMax     time.Duration // Obergrenze der Wartezeit zwischen den Versuchen
	StableAfter    time.Duration // ab dieser Dauer gilt eine Verbindung als stabil: der Backoff beginnt neu
	ServerPing     time.Duration // Takt der Server-Pings (wire.PingInterval); bleiben drei aus, baut er neu auf
	HealthWait     time.Duration // so lange darf ein neues Binary brauchen, bis es bereit ist (sonst Rückfall, health.go)
	ConsoleWait    time.Duration // so lange wartet der Wechsel aufs neue Binary auf offene Web-Konsolen (switch.go)
	StatusEvery    time.Duration // Takt der Zustandsdatei (status.go)
}

// DefaultTiming sind die Zeiten im Betrieb.
func DefaultTiming() Timing {
	return Timing{
		KeepAlive:      30 * time.Second,
		PingTimeout:    10 * time.Second,
		ConnectTimeout: 15 * time.Second,
		SubscribeWait:  10 * time.Second,
		BackoffMin:     500 * time.Millisecond,
		BackoffMax:     2 * time.Minute,
		StableAfter:    time.Minute,
		ServerPing:     wire.PingInterval,
		HealthWait:     5 * time.Minute,
		ConsoleWait:    30 * time.Minute,
		StatusEvery:    10 * time.Second,
	}
}

// withDefaults füllt jedes nicht gesetzte Feld mit dem Vorgabewert.
func (t Timing) withDefaults() Timing {
	d := DefaultTiming()
	fill := func(v *time.Duration, def time.Duration) {
		if *v <= 0 {
			*v = def
		}
	}
	fill(&t.KeepAlive, d.KeepAlive)
	fill(&t.PingTimeout, d.PingTimeout)
	fill(&t.ConnectTimeout, d.ConnectTimeout)
	fill(&t.SubscribeWait, d.SubscribeWait)
	fill(&t.BackoffMin, d.BackoffMin)
	fill(&t.BackoffMax, d.BackoffMax)
	fill(&t.StableAfter, d.StableAfter)
	fill(&t.ServerPing, d.ServerPing)
	fill(&t.HealthWait, d.HealthWait)
	fill(&t.ConsoleWait, d.ConsoleWait)
	fill(&t.StatusEvery, d.StatusEvery)
	return t
}

// serverPingMisses ist die Zahl der Server-Pings in Folge, die ausbleiben
// dürfen, bevor der Agent seine Verbindung für tot hält.
const serverPingMisses = 3

// silenceLimit ist die längste Zeit ohne Server-Ping, die er hinnimmt.
func (t Timing) silenceLimit() time.Duration { return serverPingMisses * t.ServerPing }

// checkEvery ist der Takt, in dem der Agent seine Verbindung prüft.
func (t Timing) checkEvery() time.Duration { return t.ServerPing / 3 }

// connectBudget ist die Zeit, die ein kompletter Verbindungsaufbau (Verbinden
// und beide Abonnements) höchstens braucht, plus Luft. Solange darf der
// Wächter dem Aufbau vertrauen.
func (t Timing) connectBudget() time.Duration {
	return t.ConnectTimeout + 2*t.SubscribeWait + 10*time.Second
}

// backoffJitter ist die Streuung der Wartezeit: ±25 %, damit sich viele Agenten
// nach einem Ausfall von LCM nicht im Gleichschritt wieder melden.
const backoffJitter = 0.25

// backoff liefert die Wartezeiten zwischen Verbindungsversuchen: verdoppelnd
// von min bis max, jeweils mit Streuung. Die Obergrenze gilt auch nach der
// Streuung. (Nicht threadsicher - nur die Verbindungsschleife nutzt es.)
type backoff struct {
	lo, hi time.Duration
	cur    time.Duration // Basis des nächsten Schritts, 0 = von vorn
}

func newBackoff(lo, hi time.Duration) *backoff { return &backoff{lo: lo, hi: hi} }

// next liefert die nächste Wartezeit.
func (b *backoff) next() time.Duration {
	if b.cur == 0 {
		b.cur = b.lo
	}
	wait := jittered(b.cur, rand.Float64())
	b.cur = min(2*b.cur, b.hi)
	return min(wait, b.hi)
}

// reset beginnt wieder bei der kürzesten Wartezeit.
func (b *backoff) reset() { b.cur = 0 }

// jittered streut d um ±backoffJitter; r ist eine Zufallszahl in [0, 1).
func jittered(d time.Duration, r float64) time.Duration {
	return time.Duration(float64(d) * (1 + backoffJitter*(2*r-1)))
}

// WithTiming setzt die Zeiten der Verbindung (nicht gesetzte Felder behalten
// ihren Vorgabewert). Vor Run aufrufen.
func (c *Client) WithTiming(t Timing) *Client {
	c.timing = t.withDefaults()
	return c
}
