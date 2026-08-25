package agent

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strings"
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
}

// NewClient baut den Agent-Client (verbindet noch nicht).
func NewClient(cfg *Config, log *slog.Logger, version string) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{cfg: cfg, runner: NewRunner(), log: log, version: version}
}

// Run verbindet sich (mit endloser Wiederholung) und bedient Kommandos,
// bis der Prozess beendet wird. Für den Service-Betrieb (lcm-agent run).
func (c *Client) Run() error {
	opts, err := c.clientOptions()
	if err != nil {
		return err
	}
	c.mqtt = mqtt.NewClient(opts)
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
			// Zertifikat üblicherweise selbstsigniert ist - an ihre Stelle
			// tritt VerifyPeerCertificate: Fingerprint-Pin aus dem
			// Enrollment-Token ODER reguläre Systemroot-Prüfung.
			InsecureSkipVerify:    true, //nolint:gosec - eigene Prüfung s.u.
			VerifyPeerCertificate: verifyPeer(c.cfg.CertFingerprint, u.Hostname()),
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

// verifyPeer akzeptiert das Server-Zertifikat, wenn sein SHA-256-
// Fingerprint dem Pin aus dem Enrollment-Token entspricht ODER die
// reguläre x509-Prüfung gegen die Systemroots (inkl. Hostname) gelingt.
func verifyPeer(pin, hostname string) func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("server hat kein zertifikat präsentiert")
		}
		if pin != "" {
			sum := sha256.Sum256(rawCerts[0])
			if hex.EncodeToString(sum[:]) == strings.ToLower(pin) {
				return nil
			}
		}
		// Fallback: reguläres Zertifikat (z.B. Let's Encrypt) oder ein
		// rotiertes Self-Signed - dann muss die Kette samt Hostname stimmen.
		leaf, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("zertifikat parsen: %w", err)
		}
		inter := x509.NewCertPool()
		for _, raw := range rawCerts[1:] {
			if c, err := x509.ParseCertificate(raw); err == nil {
				inter.AddCert(c)
			}
		}
		_, err = leaf.Verify(x509.VerifyOptions{DNSName: hostname, Intermediates: inter})
		if err != nil && pin != "" {
			return fmt.Errorf("zertifikat entspricht weder dem gepinnten fingerprint noch den systemroots: %w", err)
		}
		return err
	}
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
// zwischendurch abreißt, geht ein fertiges Ergebnis nicht verloren.
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
	safego.Go("agent-cmd:"+cmd.ID, func() {
		c.log.Info("command started", "id", cmd.ID)
		res := c.runner.Run(cmd)
		c.log.Info("command finished", "id", cmd.ID, "exit", res.ExitCode, "truncated", res.Truncated)
		c.publishResult(res)
	})
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
	inv := wire.Inventory{AgentVersion: c.version}
	if hn, err := os.Hostname(); err == nil {
		inv.Hostname = hn
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		inv.Kernel = strings.TrimSpace(string(out))
	}
	return inv
}
