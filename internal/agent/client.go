package agent

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"LCM/internal/remote/wire"
	"LCM/internal/safego"
)

// Client ist die MQTT-Verbindung des Agents zum LCM-Server: verbinden,
// Kommandos entgegennehmen, Ergebnisse (auch über Reconnects hinweg)
// zustellen.
type Client struct {
	cfg     *Config
	runner  *Runner
	log     *slog.Logger
	version string

	// mqtt ist die aktuelle Verbindung; mit jedem Verbindungsaufbau entsteht
	// ein neuer paho-Client (connection.go). Zugriff über current/setCurrent.
	connMu sync.Mutex
	mqtt   mqtt.Client

	// configPath ist die Datei, in der neu gelernte Pins landen (leer =
	// nur im Speicher, etwa beim Verbindungstest des Enrollments - das
	// speichert die Konfiguration danach selbst).
	configPath string
	// pinMu schützt die Pins in cfg: Der TLS-Handshake schreibt sie, das
	// Inventar liest sie.
	pinMu sync.Mutex

	// work zählt die Kommandos in Arbeit - der Wechsel aufs neue Binary
	// nach einem Upgrade wartet, bis keins mehr läuft (switch.go).
	work workState

	// tty sind die offenen Web-Konsolen (tty.go).
	tty *terminals

	// timing sind die Zeiten der Verbindung (timing.go).
	timing Timing

	// run führt ein Kommando aus (der Runner; in Tests ersetzt).
	run func(cmd wire.Command, progress func(outputBytes int)) wire.Result

	// delivering hält die Ergebnisse, die gerade zugestellt werden, samt der
	// Verbindung, auf der es geschieht - damit kein zweiter Weg dasselbe auf
	// derselben Verbindung noch einmal sendet. Auf einer neuen schon: Das Senden
	// auf der alten hängt womöglich an einer toten Leitung.
	deliveryMu sync.Mutex
	delivering map[string]mqtt.Client

	// stop wird mit Stop geschlossen und beendet Run.
	stop     chan struct{}
	stopOnce sync.Once
	// live hält fest, ob LCM noch pingt (connection.go); beat ist das
	// Lebenszeichen der Verbindungsschleife an den systemd-Wächter (notify.go).
	live   liveness
	beat   heartbeat
	notify *notifier

	// Rückfall auf das vorige Binary (update.go, health.go): update ist das
	// Zustandsverzeichnis, health meldet das erste Bereitwerden. exePath ist
	// das laufende Binary, asPrev: Es ist der Rückfall (nicht das Paket-Binary).
	// Die übrigen Felder sind Nähte für Tests.
	update       updateState
	health       *healthWatch
	exePath      string
	asPrev       bool
	packagedPath string      // Binary des Pakets (debBinaryPath)
	selfBinary   string      // Quelle der eigenen Kopie (/proc/self/exe)
	exit         func(int)   // beendet den Prozess (os.Exit)
	reachable    func() bool // ist LCM per TCP erreichbar? (nil: echter Test)
	// refused: Beim jüngsten Verbindungsversuch hat LCM den Agent ausdrücklich
	// abgewiesen (lcmRefused). Dann liegt ein Ausbleiben der Bereitschaft nicht
	// am Binary.
	refused atomic.Bool

	// Zustandsdatei (status.go): link ist, was sie über die Verbindung sagt,
	// unproven: Dieses Binary kam mit einem Update und hat sich noch nicht
	// bewährt. statusDir leer = keine Datei; statusFailed: Das Schreiben ist schon
	// einmal gescheitert und gemeldet; statusDone schließt, wenn der Schreiber
	// im Takt (statusLoop) beendet ist.
	link         *linkState
	started      time.Time
	unproven     atomic.Bool
	statusDir    string
	statusFailed bool
	statusDone   chan struct{}
}

// NewClient baut den Agent-Client (verbindet noch nicht).
func NewClient(cfg *Config, log *slog.Logger, version string) *Client {
	if log == nil {
		log = slog.Default()
	}
	c := &Client{cfg: cfg, runner: NewRunner(), log: log, version: version, timing: DefaultTiming(),
		stop: make(chan struct{}), delivering: map[string]mqtt.Client{},
		update: updateState{dir: UpdateDir}, health: newHealthWatch(),
		link: newLinkState(time.Now()), started: time.Now(),
		packagedPath: debBinaryPath, selfBinary: "/proc/self/exe", exit: os.Exit}
	c.runner.log = log
	c.run = c.runner.Run
	c.tty = newTerminals(c.publishTTY, c.terminalDisabled, log)
	return c
}

// terminalDisabled liest den Schalter des Betreibers (TerminalOffName) bei
// jedem Gebrauch: lcm-agent terminal off wirkt so sofort, ohne Neustart.
func (c *Client) terminalDisabled() bool {
	dir := ConfigDir
	if c.configPath != "" {
		dir = filepath.Dir(c.configPath)
	}
	return TerminalDisabled(dir)
}

// publishTTY schickt einen Terminal-Rahmen an LCM. Die Reihenfolge stellt die
// Nummer sicher; ein Rahmen, der bei abgerissener Verbindung verloren geht,
// beendet ohnehin die Sitzung. Mit wait wartet es auf die Bestätigung - das
// drosselt die Bildschirmausgabe auf das Tempo der Leitung.
func (c *Client) publishTTY(f wire.TTYFrame, wait bool) {
	payload, err := json.Marshal(f)
	cl := c.current()
	if err != nil || cl == nil {
		return
	}
	token := cl.Publish(wire.TopicTTYOut(c.cfg.AgentID), 1, false, payload)
	if wait {
		token.WaitTimeout(ttyPublishWait)
	}
}

// WithJobsDir legt die Job-Ablage fest (der Dienst: JobsDir; Tests).
func (c *Client) WithJobsDir(dir string) *Client {
	c.runner.WithJobsDir(dir)
	return c
}

// WithConfigPath legt fest, wohin neu gelernte Pins geschrieben werden
// (der Dienst: ConfigPath).
func (c *Client) WithConfigPath(path string) *Client {
	c.configPath = path
	return c
}

// TestConnection prüft beim Enrollment einmalig, ob Verbindung UND
// Authentifizierung funktionieren (ohne Retry, mit Timeout).
func (c *Client) TestConnection(timeout time.Duration) error {
	opts, err := c.clientOptions()
	if err != nil {
		return err
	}
	opts.SetConnectRetry(false)
	opts.SetAutoReconnect(false)
	opts.SetConnectTimeout(timeout)
	probe := mqtt.NewClient(opts)
	token := probe.Connect()
	if !token.WaitTimeout(timeout) {
		return errors.New("zeitüberschreitung beim verbindungstest")
	}
	if err := token.Error(); err != nil {
		return err
	}
	probe.Disconnect(250)
	return nil
}

// clientOptions baut die paho-Optionen: wss-Broker, Zugangsdaten aus dem
// Token, LWT und die TLS-Prüfung (Pin ODER Systemroots). Den Wiederaufbau
// übernimmt nicht paho, sondern die Verbindungsschleife (connection.go): Nur
// so lässt sich die Wartezeit streuen, und nur so gilt der Agent erst nach
// bestätigtem Abonnement als bereit.
func (c *Client) clientOptions() (*mqtt.ClientOptions, error) {
	brokerURL, err := c.cfg.BrokerURL()
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(c.cfg.URL)

	opts := mqtt.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID(c.cfg.AgentID).
		SetUsername(c.cfg.AgentID).
		SetPassword(c.cfg.Secret).
		SetCleanSession(true).
		SetAutoReconnect(false).
		SetConnectRetry(false).
		SetConnectTimeout(c.timing.ConnectTimeout).
		SetKeepAlive(c.timing.KeepAlive).
		SetPingTimeout(c.timing.PingTimeout).
		SetOrderMatters(false).
		SetBinaryWill(wire.TopicStatus(c.cfg.AgentID), []byte(wire.StatusOffline), 1, true)

	if strings.HasPrefix(brokerURL, "wss://") {
		opts.SetTLSConfig(&tls.Config{
			// Die Standard-Verifikation ist abgeschaltet, weil das LCM-
			// Zertifikat von der eigenen CA stammt (früher selbstsigniert) -
			// an ihre Stelle tritt VerifyPeerCertificate: Zertifikats-,
			// Schlüssel- oder CA-Pin ODER reguläre Systemroot-Prüfung.
			InsecureSkipVerify:    true, //nolint:gosec - eigene Prüfung s.u.
			VerifyPeerCertificate: c.verifyPeer(u.Hostname()),
			MinVersion:            tls.VersionTLS12,
		})
	}

	return opts, nil
}

// verifyPeer prüft das Server-Zertifikat (checkPeer) und merkt sich
// Schlüssel- und CA-Pin, sobald ein Pin gepasst hat.
func (c *Client) verifyPeer(hostname string) func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		learned, err := checkPeer(rawCerts, c.pins(), hostname)
		if err == nil {
			c.learn(learned)
		}
		return err
	}
}

// pins liefert die Vertrauensanker aus der Konfiguration.
func (c *Client) pins() pins {
	c.pinMu.Lock()
	defer c.pinMu.Unlock()
	return pins{cert: c.cfg.CertFingerprint, key: c.cfg.KeyPin, ca: c.cfg.CAFingerprint,
		customCAs: slices.Clone(c.cfg.CustomCAFingerprints)}
}

// learn übernimmt neue Pins und schreibt sie in die Konfiguration. Scheitert
// das Schreiben, gelten sie bis zum nächsten Neustart nur im Speicher - das
// Protokoll sagt es, die Verbindung steht trotzdem.
//
// Hat die Verbindung CA und Schlüssel bestätigt, fällt der Zertifikats-Pin aus
// dem Token weg (S-02): Er gälte sonst für das allererste Zertifikat von LCM
// ewig, auch wenn LCM den Schlüssel längst gewechselt hat. Er geht im selben
// atomaren Schreibvorgang wie der CA-Pin und nur, wenn dieser Vorgang gelingt;
// scheitert er, bleibt der Zertifikats-Pin und der nächste Verbindungsaufbau
// versucht es erneut.
func (c *Client) learn(learned pins) {
	c.pinMu.Lock()
	defer c.pinMu.Unlock()
	changed := c.adoptLearned(learned)
	dropCert := c.canDropCertPin(learned)
	if (!changed && !dropCert) || c.configPath == "" {
		return
	}
	next := *c.cfg
	if dropCert {
		next.CertFingerprint = ""
	}
	if err := next.Save(c.configPath); err != nil {
		c.log.Warn("certificate pins could not be saved", "error", err)
		return
	}
	c.cfg.CertFingerprint = next.CertFingerprint
	c.log.Info("pins of the lcm certificate saved - renewed certificates of the lcm ca are accepted")
	if dropCert {
		c.log.Info("certificate pin from the token removed - the lcm ca and key pin now vouch for the server")
	}
}

// adoptLearned trägt die gelernten Pins in die Konfiguration im Speicher ein
// und meldet, ob sich etwas geändert hat.
func (c *Client) adoptLearned(learned pins) bool {
	changed := false
	if learned.key != "" && learned.key != c.cfg.KeyPin {
		c.cfg.KeyPin, changed = learned.key, true
	}
	if learned.ca != "" && learned.ca != c.cfg.CAFingerprint {
		c.cfg.CAFingerprint, changed = learned.ca, true
	}
	for _, fp := range learned.customCAs {
		if c.cfg.rememberCustomCA(fp) {
			changed = true
		}
	}
	return changed
}

// canDropCertPin meldet, ob der Zertifikats-Pin weg kann: Die Verbindung hat
// CA und Schlüssel bestätigt und beide stehen jetzt in der Konfiguration. Ein
// Agent, dessen LCM keine CA mitschickt (alt, selbstsigniert, eigenes
// Zertifikat), lernt keine und behält den Pin.
func (c *Client) canDropCertPin(learned pins) bool {
	return c.cfg.CertFingerprint != "" && learned.ca != "" && learned.key != "" &&
		c.cfg.CAFingerprint == learned.ca && c.cfg.KeyPin == learned.key
}

// handleCommand führt ein Kommando asynchron aus (bzw. bricht es ab) und
// stellt das Ergebnis zu - auch wenn die Verbindung zwischendurch abreißt, geht
// ein fertiges Ergebnis nicht verloren (deliver). Eine zweite Zustellung
// derselben ID führt es nicht noch einmal aus.
func (c *Client) handleCommand(payload []byte) {
	var cmd wire.Command
	if err := json.Unmarshal(payload, &cmd); err != nil {
		return
	}
	c.link.contact(time.Now())
	if cmd.Ping {
		c.answerPing()
		return
	}
	if cmd.ID == "" {
		return
	}
	if cmd.Cancel {
		c.log.Info("command aborted (server instruction)", "id", cmd.ID)
		c.runner.Cancel(cmd.ID)
		return
	}
	if !c.runner.Claim(cmd.ID) {
		c.log.Info("command delivered again - ignored", "id", cmd.ID)
		c.resendStored(cmd.ID)
		return
	}
	if !c.work.begin() {
		c.refuse(cmd.ID)
		return
	}
	c.link.kick()
	safego.Go("agent-cmd:"+cmd.ID, func() {
		defer c.link.kick()
		defer c.work.end()
		c.log.Info("command started", "id", cmd.ID)
		res := c.runGuarded(cmd)
		if c.stopped() {
			return // der Agent wird beendet: das Kommando läuft weiter, der nächste stellt es zu
		}
		c.log.Info("command finished", "id", cmd.ID, "exit", res.ExitCode, "truncated", res.Truncated)
		c.deliver(res, false)
	})
}

// errSwitching ist die Antwort auf ein Kommando, das der Agent beim Wechsel aufs
// neue Binary nicht mehr annimmt.
const errSwitching = "agent wechselt gerade auf eine neue version - bitte erneut versuchen"

// refuseWait ist die Zeit, die die Absage des Kommandos hat: Der Wechsel folgt
// sofort.
const refuseWait = 2 * time.Second

// refuse sagt LCM ab, dass der Agent das Kommando nicht ausführt: Er wechselt
// gerade aufs neue Binary und trennt sich gleich. Ohne Antwort wartete der Aufruf,
// weil das Kommando nie lief und nie ein Ergebnis kommt.
func (c *Client) refuse(id string) {
	c.runner.Unclaim(id)
	c.log.Warn("switching to the new binary - command not run", "id", id)
	res := wire.Result{ID: id, ExitCode: -1, Error: errSwitching}
	if err := c.sendResult(c.current(), res, refuseWait); err != nil {
		c.log.Warn("refusal not delivered - lcm learns it from the next inventory", "id", id, "error", err)
	}
}

// resendStored stellt das Ergebnis eines erneut zugestellten Kommandos noch
// einmal zu, wenn es fertig, aber unbestätigt in der Ablage liegt - nie einen
// Fehler an seiner Stelle.
func (c *Client) resendStored(id string) {
	if res, ok := c.runner.Result(id); ok {
		safego.Go("agent-resend:"+id, func() { c.deliver(res, true) })
	}
}

// runGuarded führt das Kommando aus. Ein Panic im Agent kostet nur dieses
// Kommando: Er wird mit Stacktrace protokolliert, und LCM bekommt ein
// Fehlerergebnis, statt bis zur Stille-Frist zu warten.
func (c *Client) runGuarded(cmd wire.Command) (res wire.Result) {
	defer safego.Recover("agent-cmd:"+cmd.ID, func(err error) {
		res = wire.Result{ID: cmd.ID, ExitCode: -1, Error: "interner fehler im agent (panic): " + err.Error()}
	})
	return c.run(cmd, func(outputBytes int) {
		c.publishProgress(cmd.ID, outputBytes)
	})
}

// publishProgress meldet, dass das Kommando noch lebt, samt dem, was der Agent
// daran sieht (Vitals). Anders als das Ergebnis wird es NICHT wiederholt: Ein
// verlorenes Lebenszeichen holt das nächste 30 Sekunden später ein, und ein
// nachgereichtes wäre ohnehin wertlos - es soll ja den aktuellen Zustand belegen.
func (c *Client) publishProgress(id string, outputBytes int) {
	res := wire.Result{ID: id, Progress: true, OutputBytes: outputBytes}
	if v, ok := c.runner.Vitals(id); ok {
		res.Vitals = &v
	}
	payload, err := json.Marshal(res)
	if err != nil {
		return
	}
	if cl := c.current(); cl != nil {
		cl.Publish(wire.TopicRes(c.cfg.AgentID), 1, false, payload)
	}
}

// resultAckWait ist die Zeit, die LCM hat, den Empfang eines Ergebnisses zu
// bestätigen.
const resultAckWait = 30 * time.Second

var errNotConnected = errors.New("nicht mit lcm verbunden")

// deliver stellt ein Ergebnis einmal zu (C-6): Es wird gesendet und gilt mit dem
// PUBACK des Brokers als zugestellt - dann nimmt der Runner den Job aus der
// Ablage. Scheitert es (keine Verbindung, keine Bestätigung), bleibt der Job in
// der Ablage, und resendResults stellt ihn nach der nächsten Verbindung zu,
// auch aus einem neuen Prozess. Eine Wiederholungsschleife gibt es nicht mehr: Sie
// hielt bis zu zwei Stunden den Wechsel aufs neue Binary auf.
//
// Warum das PUBACK genügt: Der Broker von LCM nimmt das Ergebnis in derselben
// Verarbeitung an, in der er es dem wartenden Aufruf zustellt. Ein Ergebnis, das
// LCM nach der Bestätigung noch verlöre (Absturz von LCM), hätte keinen
// Aufrufer mehr, denn der lebte im selben Prozess. Eine eigene Quittung
// brächte nur einen Nachrichtentyp, den ältere LCM-Fassungen nicht kennen.
//
// Das Ergebnis gilt als zugestellt (true), auch wenn gerade ein anderer Weg
// dasselbe sendet; false nur, wenn das Senden scheiterte.
func (c *Client) deliver(res wire.Result, recovered bool) bool {
	cl := c.current()
	if !c.startDelivery(res.ID, cl) {
		return true
	}
	defer c.endDelivery(res.ID, cl)
	res.Recovered = recovered
	if err := c.sendResult(cl, res, resultAckWait); err != nil {
		c.log.Warn("result not delivered - it stays in the job store and is sent after the next connection",
			"id", res.ID, "error", err)
		return false
	}
	c.runner.Ack(res.ID)
	return true
}

func (c *Client) sendResult(cl mqtt.Client, res wire.Result, wait time.Duration) error {
	payload, err := json.Marshal(res)
	if err != nil {
		return err
	}
	if cl == nil {
		return errNotConnected
	}
	return c.await(cl.Publish(wire.TopicRes(c.cfg.AgentID), 1, false, payload), wait)
}

func (c *Client) startDelivery(id string, cl mqtt.Client) bool {
	c.deliveryMu.Lock()
	defer c.deliveryMu.Unlock()
	if busy, ok := c.delivering[id]; ok && busy == cl {
		return false
	}
	c.delivering[id] = cl
	return true
}

func (c *Client) endDelivery(id string, cl mqtt.Client) {
	c.deliveryMu.Lock()
	if c.delivering[id] == cl {
		delete(c.delivering, id)
	}
	c.deliveryMu.Unlock()
}

// resendResults stellt alle noch nicht bestätigten Ergebnisse der Job-Ablage zu.
// Es läuft nach jedem Verbindungsaufbau und beim Start des Agents.
func (c *Client) resendResults() {
	for _, id := range c.runner.Pending() {
		res, ok := c.runner.Result(id)
		if !ok {
			continue
		}
		if !c.deliver(res, true) || c.stopped() {
			return // die Verbindung taugt nicht; die nächste versucht es wieder
		}
	}
}

// adoptJobs nimmt die Jobs wieder auf, die ein früherer Agent-Prozess gestartet
// hat: Sie laufen weiter, und ihr Ergebnis kommt über deliver an. Bis sie fertig
// sind, wechselt der Agent nicht aufs neue Binary.
func (c *Client) adoptJobs() {
	c.runner.Adopt(func(id string, follow func(progress func(int)) wire.Result) {
		c.work.begin()
		safego.Go("agent-adopt:"+id, func() {
			defer c.work.end()
			res := follow(func(outputBytes int) { c.publishProgress(id, outputBytes) })
			if c.stopped() {
				return
			}
			c.log.Info("command finished", "id", id, "exit", res.ExitCode, "truncated", res.Truncated)
			c.deliver(res, true)
		})
	})
}

// inventory sammelt die Basis-Infos für den LCM-Server (best effort).
func (c *Client) inventory() wire.Inventory {
	p := c.pins()
	inv := wire.Inventory{
		AgentVersion: c.version, ProtocolVersion: wire.ProtocolVersion, KeyPin: p.key, CAPin: p.ca,
		Capabilities: c.capabilities(),
		KnownJobs:    c.runner.Known(),
	}
	if hn, err := os.Hostname(); err == nil {
		inv.Hostname = hn
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		inv.Kernel = strings.TrimSpace(string(out))
	}
	return inv
}
