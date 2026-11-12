package agent

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	mqtt    mqtt.Client

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
}

// NewClient baut den Agent-Client (verbindet noch nicht).
func NewClient(cfg *Config, log *slog.Logger, version string) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{cfg: cfg, runner: NewRunner(), log: log, version: version}
}

// WithConfigPath legt fest, wohin neu gelernte Pins geschrieben werden
// (der Dienst: ConfigPath).
func (c *Client) WithConfigPath(path string) *Client {
	c.configPath = path
	return c
}

// Run verbindet sich (mit endloser Wiederholung) und bedient Kommandos,
// bis der Prozess beendet wird. Für den Service-Betrieb (lcm-agent run).
func (c *Client) Run() error {
	opts, err := c.clientOptions()
	if err != nil {
		return err
	}
	c.mqtt = mqtt.NewClient(opts)
	// Vor dem Connect: Mit ConnectRetry wartet der Connect, bis LCM
	// erreichbar ist - ein neues Binary soll trotzdem übernommen werden.
	safego.Go("agent-binary-switch", c.switchOnNewBinary)
	token := c.mqtt.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		// ConnectRetry ist aktiv - hierher kommt es nur bei endgültigen
		// Fehlern (z.B. kaputte Broker-URL).
		return fmt.Errorf("verbindung zum lcm-server: %w", err)
	}
	select {} // Dienst läuft, bis systemd ihn beendet
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
// Token, LWT, Auto-Reconnect und die TLS-Prüfung (Pin ODER Systemroots).
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
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10*time.Second).
		SetMaxReconnectInterval(2*time.Minute).
		SetKeepAlive(30*time.Second).
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

	opts.SetOnConnectHandler(func(cl mqtt.Client) {
		c.log.Info("connected to lcm server", "broker", brokerURL)
		c.onConnect(cl)
	})
	opts.SetConnectionLostHandler(func(cl mqtt.Client, err error) {
		c.log.Warn("connection lost - reconnecting", "error", err)
	})
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
	return pins{cert: c.cfg.CertFingerprint, key: c.cfg.KeyPin, ca: c.cfg.CAFingerprint}
}

// learn übernimmt neue Pins und schreibt sie in die Konfiguration. Scheitert
// das Schreiben, gelten sie bis zum nächsten Neustart nur im Speicher - das
// Protokoll sagt es, die Verbindung steht trotzdem.
func (c *Client) learn(learned pins) {
	c.pinMu.Lock()
	defer c.pinMu.Unlock()
	changed := false
	if learned.key != "" && learned.key != c.cfg.KeyPin {
		c.cfg.KeyPin, changed = learned.key, true
	}
	if learned.ca != "" && learned.ca != c.cfg.CAFingerprint {
		c.cfg.CAFingerprint, changed = learned.ca, true
	}
	if !changed || c.configPath == "" {
		return
	}
	if err := c.cfg.Save(c.configPath); err != nil {
		c.log.Warn("certificate pins could not be saved", "error", err)
		return
	}
	c.log.Info("pins of the lcm certificate saved - renewed certificates of the lcm ca are accepted")
}

// onConnect meldet den Agent nach jedem (Re-)Connect an. Reihenfolge ist
// wichtig: ZUERST das Kommando-Topic abonnieren, DANN Status/Inventar
// senden. Der Server stößt den Erst-Scan erst beim Eintreffen des Inventars
// an - so ist die cmd-Subscription garantiert aktiv, bevor das erste
// Kommando publiziert wird (bei CleanSession gäbe es sonst keine
// Nachzustellung und das Kommando ginge verloren).
func (c *Client) onConnect(cl mqtt.Client) {
	id := c.cfg.AgentID
	tok := cl.Subscribe(wire.TopicCmd(id), 1, func(_ mqtt.Client, msg mqtt.Message) {
		c.handleCommand(msg.Payload())
	})
	tok.Wait() // sicherstellen, dass die Subscription registriert ist
	cl.Publish(wire.TopicStatus(id), 1, true, []byte(wire.StatusOnline))
	if inv, err := json.Marshal(c.inventory()); err == nil {
		cl.Publish(wire.TopicInv(id), 1, false, inv)
	}
}

// handleCommand führt ein Kommando asynchron aus (bzw. bricht es ab) und
// stellt das Ergebnis mit Wiederholung zu - auch wenn die Verbindung
// zwischendurch abreißt, geht ein fertiges Ergebnis nicht verloren. Eine
// zweite Zustellung derselben ID führt es nicht noch einmal aus.
func (c *Client) handleCommand(payload []byte) {
	var cmd wire.Command
	if err := json.Unmarshal(payload, &cmd); err != nil || cmd.ID == "" {
		return
	}
	if cmd.Cancel {
		c.log.Info("command aborted (server instruction)", "id", cmd.ID)
		c.runner.Cancel(cmd.ID)
		return
	}
	if !c.runner.Claim(cmd.ID) {
		c.log.Info("command delivered again - ignored", "id", cmd.ID)
		return
	}
	if !c.work.begin() {
		// Der Agent wechselt gerade aufs neue Binary und trennt sich gleich;
		// LCM erfährt es über die Trennung und lässt das Kommando scheitern.
		c.log.Warn("switching to the new binary - command not run", "id", cmd.ID)
		return
	}
	safego.Go("agent-cmd:"+cmd.ID, func() {
		defer c.work.end()
		c.log.Info("command started", "id", cmd.ID)
		res := c.runner.Run(cmd, func(outputBytes int) {
			c.publishProgress(cmd.ID, outputBytes)
		})
		c.log.Info("command finished", "id", cmd.ID, "exit", res.ExitCode, "truncated", res.Truncated)
		c.publishResult(res)
	})
}

// publishProgress meldet, dass das Kommando noch arbeitet. Anders als das
// Ergebnis wird es NICHT wiederholt: Ein verlorenes Lebenszeichen holt das
// nächste 30 Sekunden später ein, und ein nachgereichtes wäre ohnehin
// wertlos - es soll ja den aktuellen Zustand belegen.
func (c *Client) publishProgress(id string, outputBytes int) {
	payload, err := json.Marshal(wire.Result{ID: id, Progress: true, OutputBytes: outputBytes})
	if err != nil {
		return
	}
	c.mqtt.Publish(wire.TopicRes(c.cfg.AgentID), 1, false, payload)
}

// publishResult stellt ein Ergebnis zu und wiederholt bei Verbindungs-
// problemen (bis zu 2 h - danach ist der Job serverseitig längst per
// Watchdog/Timeout fehlgeschlagen und das Ergebnis wertlos).
func (c *Client) publishResult(res wire.Result) {
	payload, err := json.Marshal(res)
	if err != nil {
		return
	}
	deadline := time.Now().Add(2 * time.Hour)
	for time.Now().Before(deadline) {
		token := c.mqtt.Publish(wire.TopicRes(c.cfg.AgentID), 1, false, payload)
		if token.WaitTimeout(30*time.Second) && token.Error() == nil {
			return
		}
		time.Sleep(5 * time.Second)
	}
	c.log.Warn("result could not be delivered (giving up)", "id", res.ID)
}

// inventory sammelt die Basis-Infos für den LCM-Server (best effort).
func (c *Client) inventory() wire.Inventory {
	p := c.pins()
	inv := wire.Inventory{AgentVersion: c.version, KeyPin: p.key, CAPin: p.ca}
	if hn, err := os.Hostname(); err == nil {
		inv.Hostname = hn
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		inv.Kernel = strings.TrimSpace(string(out))
	}
	return inv
}
