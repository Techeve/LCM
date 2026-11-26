package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	mqttpackets "github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/gorilla/websocket"

	"LCM/internal/remote/wire"
	"LCM/internal/safego"
)

// Die Verbindung des Agents zu LCM hält eine eigene Schleife (supervise), nicht
// paho: Jeder Verbindungsaufbau bekommt einen frischen paho-Client, und die
// Schleife wartet zwischen den Versuchen mit Backoff und Streuung. Gründe:
//   - Bereit meldet sich der Agent erst, wenn der Broker beide Abonnements
//     bestätigt hat (B1). Scheitert eines, wird die Verbindung verworfen und
//     neu aufgebaut - ein frischer Aufbau ist robuster als das Abonnement auf
//     einer Verbindung zu wiederholen, deren Zustand man nicht kennt.
//   - pahos eigener Backoff streut nicht (B13).
//   - Hängt etwas in paho, ist der Client wegwerfbar.

// errConnectionEnded ist der Grund, den offene Konsolen bei einem Abriss sehen.
var errConnectionEnded = errors.New("verbindung zu lcm abgerissen")

var (
	errStopped      = errors.New("agent wird beendet")
	errServerSilent = errors.New("lcm schickt keine pings mehr")
	errNoAnswer     = errors.New("keine antwort des brokers")
)

// subscribeFailed ist der Rückgabecode des Brokers für ein abgelehntes
// Abonnement (MQTT 3.1.1: 0x80).
const subscribeFailed = 0x80

// sessionQuiesce ist die Zeit, die das saubere Trennen einer Verbindung hat.
const sessionQuiesce = 250

// Run verbindet sich (mit endloser Wiederholung) und bedient Kommandos, bis
// Stop gerufen wird. Für den Service-Betrieb (lcm-agent run). Fehler gibt es
// nur, wenn die Konfiguration unbrauchbar ist (z.B. kaputte Broker-URL) -
// ein nicht erreichbares LCM wiederholt die Schleife, ohne je aufzugeben.
func (c *Client) Run() error {
	opts, err := c.clientOptions()
	if err != nil {
		return err
	}
	c.notify = notifierFromEnv()
	c.adoptJobs()
	c.startHealth()
	c.startStatus()
	safego.Go("agent-binary-switch", c.switchOnNewBinary)
	c.beat.extend(c.timing.connectBudget())
	if every, ok := c.watchdog(); ok {
		safego.Go("agent-watchdog", func() { c.feedWatchdog(every) })
	}
	c.tell("READY=1") // Die Hauptschleife läuft; ob LCM erreichbar ist, ist Sache der Schleife
	c.supervise(opts)
	c.tell("STOPPING=1")
	c.writeFinalStatus()
	return nil
}

// Stop beendet Run: Die Verbindung wird sauber getrennt. Laufende Kommandos
// bleiben dabei unberührt: Sie laufen ohne den Agent weiter, und der nächste
// nimmt sie wieder auf (jobs.go). Mehrfach rufbar.
func (c *Client) Stop() {
	c.stopOnce.Do(func() {
		close(c.stop)
		c.runner.Detach()
	})
}

func (c *Client) stopped() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

// current ist die aktuelle Verbindung (nil vor dem ersten Aufbau).
func (c *Client) current() mqtt.Client {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.mqtt
}

func (c *Client) setCurrent(cl mqtt.Client) {
	c.connMu.Lock()
	c.mqtt = cl
	c.connMu.Unlock()
}

// supervise baut die Verbindung auf, hält sie und baut sie nach jedem Ende
// neu auf, bis Stop gerufen wird.
func (c *Client) supervise(opts *mqtt.ClientOptions) {
	wait := newBackoff(c.timing.BackoffMin, c.timing.BackoffMax)
	for {
		c.link.connecting(time.Now())
		readyAt, err := c.session(opts)
		c.tty.closeAll(errConnectionEnded) // LCM hat seine Seite der Konsolen verloren
		c.link.lost(time.Now(), err.Error())
		if c.stopped() {
			c.log.Warn("disconnected from lcm - the agent is stopping", "reason", err)
			return
		}
		// Nur eine Verbindung, die eine Weile hielt, setzt den Backoff zurück:
		// Eine, die gleich wieder abreißt, soll ihn weiter wachsen lassen.
		if !readyAt.IsZero() && time.Since(readyAt) >= c.timing.StableAfter {
			wait.reset()
		}
		d := wait.next()
		c.log.Warn("connection to lcm ended - reconnecting", "reason", err, "in", d.Round(time.Millisecond))
		c.beat.extend(d + c.timing.connectBudget())
		if !c.sleep(d) {
			return
		}
	}
}

// sleep wartet d; false, wenn Stop dazwischenkam.
func (c *Client) sleep(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.stop:
		return false
	}
}

// session führt eine Verbindung von ihrem Aufbau bis zu ihrem Ende. Es liefert
// den Zeitpunkt, an dem der Agent bereit war (Null, wenn nie), und den Grund
// des Endes.
func (c *Client) session(opts *mqtt.ClientOptions) (time.Time, error) {
	lost := make(chan error, 1)
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		defer safego.Recover("agent-connection-lost", nil)
		select {
		case lost <- err:
		default:
		}
	})
	cl := mqtt.NewClient(opts)
	defer cl.Disconnect(sessionQuiesce)
	c.setCurrent(cl)

	c.beat.extend(c.timing.connectBudget())
	if err := c.await(cl.Connect(), c.timing.ConnectTimeout+time.Second); err != nil {
		c.refused.Store(lcmRefused(err))
		return time.Time{}, fmt.Errorf("verbinden: %w", err)
	}
	c.refused.Store(false)
	c.live.connected(time.Now())
	if err := c.subscribe(cl); err != nil {
		return time.Time{}, err
	}
	c.announce(cl)
	c.health.markReady()
	safego.Go("agent-resend-results", c.resendResults) // Ergebnisse, die LCM noch nicht bestätigt hat
	c.logReady(opts.Servers[0].String())
	c.link.ready(time.Now())
	return time.Now(), c.hold(lost)
}

// logReady meldet die bereite Verbindung. Die erste steht im Info-Protokoll, jede
// Wiederverbindung auf Warn: Sie soll auch bei log_level warn im Journal stehen.
func (c *Client) logReady(broker string) {
	if !c.link.reconnected() {
		c.log.Info("connected to lcm server", "broker", broker)
		return
	}
	last := c.link.lastDisconnect()
	c.log.Warn("reconnected to lcm server", "broker", broker, "disconnected_at", last.At.Format(time.RFC3339), "reason", last.Reason)
}

// refusals sind die Antworten des Brokers (CONNACK), mit denen LCM den Agent
// ausdrücklich abweist: falsche oder widerrufene Zugangsdaten, gelöschter
// Server, keine Berechtigung, nicht verfügbar.
var refusals = []error{
	mqttpackets.ErrorRefusedBadUsernameOrPassword,
	mqttpackets.ErrorRefusedNotAuthorised,
	mqttpackets.ErrorRefusedIDRejected,
	mqttpackets.ErrorRefusedServerUnavailable,
}

// lcmRefused meldet, ob der Fehler beim Verbinden eine ausdrückliche Abweisung
// durch LCM ist: ein CONNACK mit Ablehnung oder ein WebSocket-Upgrade, das eine
// HTTP-Antwort ablehnt (401, 403, 429, 503 bei ausgeschaltetem Agent-Zugang).
// In beiden Fällen hat das Gegenüber geantwortet und der Agent es verstanden:
// ein Beleg dafür, dass TLS und Protokoll des Binarys funktionieren. Alles
// andere (Zeitüberschreitung, Abbruch, TLS-Fehler, Protokollverstoß) bleibt
// offen.
func lcmRefused(err error) bool {
	if errors.Is(err, websocket.ErrBadHandshake) {
		return true
	}
	for _, refusal := range refusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}

// await wartet auf die Bestätigung des Brokers - höchstens d und nicht über
// Stop hinaus.
func (c *Client) await(tok mqtt.Token, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-tok.Done():
		return tok.Error()
	case <-timer.C:
		return errNoAnswer
	case <-c.stop:
		return errStopped
	}
}

// subscribe abonniert Kommando- und Konsolen-Topic. Reihenfolge ist wichtig:
// ZUERST abonnieren, DANN Status und Inventar senden (announce). Der Server
// stößt den Erst-Scan erst beim Eintreffen des Inventars an - so ist die
// cmd-Subscription garantiert aktiv, bevor das erste Kommando publiziert wird
// (bei CleanSession gäbe es sonst keine Nachzustellung, und das Kommando ginge
// verloren). Jedes Abonnement muss bestätigt sein.
func (c *Client) subscribe(cl mqtt.Client) error {
	id := c.cfg.AgentID
	err := c.subscribeTopic(cl, wire.TopicCmd(id), func(_ mqtt.Client, msg mqtt.Message) {
		defer safego.Recover("agent-command-handler", nil)
		c.handleCommand(msg.Payload())
	})
	if err != nil {
		return err
	}
	return c.subscribeTopic(cl, wire.TopicTTY(id), func(_ mqtt.Client, msg mqtt.Message) {
		defer safego.Recover("agent-tty-handler", nil)
		c.handleFrame(msg.Payload())
	})
}

func (c *Client) subscribeTopic(cl mqtt.Client, topic string, handler mqtt.MessageHandler) error {
	tok := cl.Subscribe(topic, 1, handler)
	if err := c.await(tok, c.timing.SubscribeWait); err != nil {
		return fmt.Errorf("abonnement %s: %w", topic, err)
	}
	// Lehnt der Broker ab, bleibt der Fehler des Tokens leer - die Ablehnung
	// steht nur im Rückgabecode.
	if st, ok := tok.(*mqtt.SubscribeToken); ok && st.Result()[topic] >= subscribeFailed {
		return fmt.Errorf("abonnement %s: vom broker abgelehnt", topic)
	}
	return nil
}

// handleFrame reicht einen Rahmen der Web-Konsole an die Sitzungen.
func (c *Client) handleFrame(payload []byte) {
	var f wire.TTYFrame
	if err := json.Unmarshal(payload, &f); err == nil {
		c.tty.handle(f)
	}
}

// announce meldet den Agent an: Status und Inventar.
func (c *Client) announce(cl mqtt.Client) {
	id := c.cfg.AgentID
	cl.Publish(wire.TopicStatus(id), 1, true, []byte(wire.StatusOnline))
	if inv, err := json.Marshal(c.inventoryMessage()); err == nil {
		cl.Publish(wire.TopicInv(id), 1, false, inv)
	}
}

// hold hält die Verbindung, bis sie abreißt, LCM verstummt oder Stop kommt, und
// meldet dem systemd-Wächter bei jeder Prüfung, dass die Schleife lebt.
func (c *Client) hold(lost <-chan error) error {
	tick := time.NewTicker(c.timing.checkEvery())
	defer tick.Stop()
	c.beat.extend(3 * c.timing.checkEvery())
	for {
		select {
		case err := <-lost:
			return fmt.Errorf("verbindung verloren: %w", err)
		case <-c.stop:
			return errStopped
		case <-tick.C:
			if c.live.silent(time.Now(), c.timing.silenceLimit()) {
				return errServerSilent
			}
			c.beat.extend(3 * c.timing.checkEvery())
		}
	}
}

// liveness hält fest, wann LCM zuletzt gepingt hat (Lebendprüfung, siehe
// wire.CapabilityPing).
type liveness struct {
	mu sync.Mutex
	// expect: LCM pingt, denn es hat das schon einmal getan. Ein älteres LCM
	// schickt nie ein Ping - dann darf der Agent keins verlangen.
	expect bool
	since  time.Time // Beginn der Verbindung
	last   time.Time // letztes Ping auf dieser Verbindung, Null = noch keins
}

// connected setzt die Uhr für eine neue Verbindung zurück.
func (l *liveness) connected(now time.Time) {
	l.mu.Lock()
	l.since, l.last = now, time.Time{}
	l.mu.Unlock()
}

// pinged verbucht ein Ping von LCM.
func (l *liveness) pinged(now time.Time) {
	l.mu.Lock()
	l.last, l.expect = now, true
	l.mu.Unlock()
}

// silent meldet, ob LCM länger als limit nicht gepingt hat, obwohl es sollte.
// War auf dieser Verbindung noch gar kein Ping da, hat sich LCM vermutlich
// zurückentwickelt (älterer Stand): Der Agent verlangt dann keine mehr, bis
// wieder eins kommt - sonst baute er alle limit-Zeiten neu auf.
func (l *liveness) silent(now time.Time, limit time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.expect {
		return false
	}
	ref := l.last
	if ref.IsZero() {
		ref = l.since
	}
	if now.Sub(ref) <= limit {
		return false
	}
	if l.last.IsZero() {
		l.expect = false
	}
	return true
}

// answerPing beantwortet ein Ping von LCM. QoS 0: Ein verspätetes Pong hat
// keinen Wert, und es soll nichts in der Warteschlange eines toten Links
// stauen.
func (c *Client) answerPing() {
	c.log.Debug("ping from lcm answered")
	c.live.pinged(time.Now())
	payload, err := json.Marshal(wire.Result{Pong: true})
	cl := c.current()
	if err != nil || cl == nil {
		return
	}
	cl.Publish(wire.TopicRes(c.cfg.AgentID), 0, false, payload)
}

// feedWatchdog füttert den systemd-Wächter im Takt every, solange die
// Verbindungsschleife lebt. Bleibt das Futter aus, startet systemd den Agent
// neu.
func (c *Client) feedWatchdog(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			if !c.beat.alive() {
				c.log.Error("connection loop is stuck - watchdog is no longer fed, systemd will restart the agent")
				continue
			}
			c.tell("WATCHDOG=1")
		}
	}
}

// tell meldet systemd einen Zustand (No-op ohne NOTIFY_SOCKET).
func (c *Client) tell(msg string) {
	if err := c.notify.send(msg); err != nil {
		c.log.Warn("systemd notification failed", "message", msg, "error", err)
	}
}
