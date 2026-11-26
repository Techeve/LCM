package agent

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"LCM/internal/i18n"
	"LCM/internal/remote/wire"
	"LCM/internal/safego"
)

// Gesundheit des Agents nach einem Update (Paket D-4, Fortsetzung von update.go).
//
// Als gesund gilt ein Agent, der binnen HealthWait nach dem Start bereit war
// (beide Abonnements bestätigt, Inventar gesendet) und danach StableAfter
// durchgehalten hat. Dann trägt er es ein (updateState.healthy) und - wenn er das
// Paket-Binary ist - sichert sich als vorigen, bewährten Rückfall.
//
// Wird er nicht bereit, entscheidet er nur dann gegen sich, wenn
//   - es einen Rückfall gibt, der ein anderes Binary ist (sonst ist dieses
//     Binary das bewährte, und ein Ausfall liegt nicht an ihm),
//   - LCM per TCP erreichbar ist (sonst liegt es am Netz, nicht am Binary): Ein
//     Neustart nach einem Update bei gekapptem Netz oder abgeschaltetem LCM
//     darf nicht zurückrollen, und
//   - LCM ihn nicht ausdrücklich abgewiesen hat (lcmRefused): Widerrufener
//     Token, gelöschter Server oder ausgeschalteter Agent-Zugang sind Sache
//     von LCM. Ein Binary, das ein CONNACK oder eine HTTP-Antwort versteht,
//     spricht TLS und Protokoll; hier hülfe der Rückfall nicht.
//
// Dann trägt er „nicht bereit" ein und beendet sich; das Startskript rollt beim
// nächsten Start zurück. Bleibt er unentschieden, prüft er nach HealthWait
// wieder.

// healthWatch meldet, ob der Agent einmal bereit war.
type healthWatch struct {
	ready chan struct{}
	once  sync.Once
}

func newHealthWatch() *healthWatch { return &healthWatch{ready: make(chan struct{})} }

// markReady vermerkt das erste Bereitwerden; mehrfach rufbar.
func (h *healthWatch) markReady() { h.once.Do(func() { close(h.ready) }) }

// startHealth hält fest, wie dieser Prozess gestartet wurde, und startet die
// Beobachtung. Von Run gerufen, bevor die erste Verbindung steht.
func (c *Client) startHealth() {
	if exe, err := os.Executable(); err == nil {
		c.exePath = exe
	}
	c.asPrev = c.exePath != "" && c.exePath == c.update.prevPath()
	if c.asPrev {
		c.log.Warn("running the previous agent binary after a failed update",
			"binary", c.exePath, "rolled_back", fmt.Sprintf("%+v", c.update.rolledBack()))
	}
	safego.Go("agent-health", c.watchHealth)
}

// packagedRun: Dieser Prozess ist das Binary des Pakets (nicht der Rückfall, nicht
// eine Selbstinstallation oder ein Test).
func (c *Client) packagedRun() bool {
	return c.exePath != "" && c.exePath == c.packagedPath
}

func (c *Client) watchHealth() {
	deadline := time.NewTimer(c.timing.HealthWait)
	defer deadline.Stop()
	unproven := c.packagedRun() && c.update.prevDiffers(c.selfBinary)
	c.unproven.Store(unproven)
	for {
		select {
		case <-c.stop:
			return
		case <-c.health.ready:
			c.confirmHealthy()
			return
		case <-deadline.C:
			if unproven && c.blamesBinary() {
				c.giveUp()
				return
			}
			c.log.Warn("agent not ready yet", "waited", c.timing.HealthWait.String(),
				"unproven_binary", unproven, "refused_by_lcm", c.refused.Load())
			deadline.Reset(c.timing.HealthWait)
		}
	}
}

// confirmHealthy wartet die Stabilitätszeit ab und trägt „gesund" ein.
func (c *Client) confirmHealthy() {
	if !c.sleep(c.timing.StableAfter) {
		return
	}
	if err := c.update.healthy(c.asPrev); err != nil {
		c.log.Warn("health state could not be written", "error", err)
		return
	}
	c.unproven.Store(false)
	c.link.kick()
	if c.asPrev || !c.packagedRun() {
		return
	}
	copied, err := c.update.savePrev(c.selfBinary)
	if err != nil {
		c.log.Warn("this binary could not be kept as the way back", "error", err)
		return
	}
	if copied {
		c.log.Info("agent healthy - this binary is now the way back for the next update", "version", c.version)
	}
}

// blamesBinary: Das Ausbleiben der Bereitschaft geht auf das Binary zurück -
// LCM ist erreichbar und hat den Agent nicht ausdrücklich abgewiesen.
func (c *Client) blamesBinary() bool {
	return !c.refused.Load() && c.serverReachable()
}

// giveUp: Das neue Binary wurde nicht bereit, obwohl LCM erreichbar ist und
// den Agent nicht abweist.
func (c *Client) giveUp() {
	c.log.Error("the agent did not become ready in time although the LCM server is reachable and does not refuse it - "+
		"exiting so that the previous binary takes over", "waited", c.timing.HealthWait.String())
	if err := c.update.failed(reasonNotReady); err != nil {
		c.log.Error("verdict could not be written - the start script counts the restart anyway", "error", err)
	}
	c.exit(1)
}

// serverReachable prüft per TCP, ob LCM erreichbar ist (c.reachable ersetzt es in Tests).
func (c *Client) serverReachable() bool {
	if c.reachable != nil {
		return c.reachable()
	}
	u, err := url.Parse(c.cfg.URL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" || u.Scheme == "ws" {
			port = "80"
		}
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), port), 5*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// rolledBack meldet den Rückfall, solange das vorige Binary läuft.
func (c *Client) rolledBack() *RolledBack {
	if !c.asPrev {
		return nil
	}
	return c.update.rolledBack()
}

// inventoryMessage baut das Inventar samt Rückfall.
func (c *Client) inventoryMessage() wire.Inventory {
	inv := c.inventory()
	if rb := c.rolledBack(); rb != nil {
		inv.RolledBack, inv.RolledBackTo, inv.RolledBackReason = rb.From, rb.To, rb.Reason
	}
	return inv
}

// capabilitySelfUpdate: Das Paket bringt das Startskript mit, über das ein
// Update zurückrollt (siehe update.go), und pinnt den Agenten gegen normale
// Upgrades. Nur dann kann LCM ihn gezielt aktualisieren (wire kennt den Namen noch nicht).
const capabilitySelfUpdate = "self-update-v2"

// capabilities liefert die Fähigkeiten dieses Agents für das Inventar.
func (c *Client) capabilities() []string {
	return agentCapabilities(c.tty.capabilities(), guardInstalled())
}

func agentCapabilities(tty []string, guard bool) []string {
	caps := append(append([]string(nil), tty...), wire.CapabilityPing, wire.CapabilityJobs)
	if guard {
		caps = append(caps, capabilitySelfUpdate)
	}
	return caps
}

// Meldungen an die offenen Web-Konsolen beim Wechsel aufs neue Binary (siehe switch.go).
func switchPendingNotice(wait time.Duration) string {
	return i18n.Tf(
		"[lcm-agent] A new agent version is installed. The agent switches to it once all consoles are closed (at the latest in %d minutes); then this console ends - please reconnect.",
		"[lcm-agent] Eine neue Agent-Version ist installiert. Der Agent wechselt, sobald alle Konsolen geschlossen sind (spaetestens in %d Minuten); dann endet diese Konsole - bitte neu verbinden.",
		int(wait.Minutes()))
}

func switchNowNotice() string {
	return i18n.T(
		"[lcm-agent] Switching to the new agent version now - this console ends, please reconnect.",
		"[lcm-agent] Der Agent wechselt jetzt auf die neue Version - diese Konsole endet, bitte neu verbinden.")
}
