package agent

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	mqttpackets "github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/gorilla/websocket"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

// Rollback eines Agent-Updates nur, wenn das Binary schuld ist: Weist LCM den
// Agent ausdrücklich ab (Token widerrufen, Server gelöscht, Agent-Zugang aus),
// war das neue Binary gut und bleibt. Die Tests lassen eine echte
// Verbindungsschleife gegen verschiedene Gegenstellen laufen.

// denyAll weist jeden Zugang ab (CONNACK mit Ablehnung), wie LCM bei
// widerrufenem Token oder gelöschtem Server.
type denyAll struct{ mqtt.HookBase }

func (*denyAll) ID() string { return "deny-all" }

func (*denyAll) Provides(b byte) bool { return b == mqtt.OnConnectAuthenticate }

func (*denyAll) OnConnectAuthenticate(*mqtt.Client, packets.Packet) bool { return false }

// connectTo verbindet den unbewährten Agenten des Rigs per echter
// Verbindungsschleife mit der Gegenstelle unter url.
func (r *healthRig) connectTo(t *testing.T, url string) {
	t.Helper()
	r.c.cfg.URL, r.c.cfg.AgentID, r.c.cfg.Secret = url, "00000000-0000-4000-8000-00000000b007", "egal"
	r.c.timing = Timing{
		HealthWait: 400 * time.Millisecond, StableAfter: 20 * time.Millisecond,
		ConnectTimeout: 200 * time.Millisecond, SubscribeWait: 200 * time.Millisecond,
		BackoffMin: 20 * time.Millisecond, BackoffMax: 50 * time.Millisecond,
	}.withDefaults()
	opts, err := r.c.clientOptions()
	if err != nil {
		t.Fatal(err)
	}
	go r.c.supervise(opts)
}

// TestAuthAbweisungRollbackBleibtAus: Der Broker lehnt die Zugangsdaten mit
// einem CONNACK ab. Das Binary versteht den Broker - kein Rückfall, solange
// LCM erreichbar ist und ablehnt.
func TestAuthAbweisungRollbackBleibtAus(t *testing.T) {
	r := newHealthRig(t)
	writeFile(t, r.c.update.prevPath(), "binary-A")
	_, addr := startBrokerWith(t, new(denyAll))

	r.connectTo(t, "http://"+addr)
	go r.c.watchHealth()

	if code, ok := r.exited(1500 * time.Millisecond); ok {
		t.Fatalf("der Agent rollt zurück (exit %d), obwohl LCM ihn abweist", code)
	}
	if !r.c.refused.Load() {
		t.Error("die Abweisung wurde nicht erkannt")
	}
	if r.c.update.starts() != 0 || r.c.update.read("fail-reason") != "" {
		t.Errorf("verdikt trotz Abweisung: starts=%d reason=%q", r.c.update.starts(), r.c.update.read("fail-reason"))
	}
}

// TestUpgradeAbweisungRollbackBleibtAus: LCM (oder sein Proxy) beantwortet das
// WebSocket-Upgrade mit 403/503 - etwa bei ausgeschaltetem Agent-Zugang.
func TestUpgradeAbweisungRollbackBleibtAus(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			r := newHealthRig(t)
			writeFile(t, r.c.update.prevPath(), "binary-A")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "abgewiesen", status)
			}))
			t.Cleanup(srv.Close)

			r.connectTo(t, srv.URL)
			go r.c.watchHealth()

			if code, ok := r.exited(1500 * time.Millisecond); ok {
				t.Fatalf("der Agent rollt zurück (exit %d), obwohl LCM das Upgrade abweist", code)
			}
			if !r.c.refused.Load() {
				t.Error("die Abweisung wurde nicht erkannt")
			}
		})
	}
}

// TestHandshakeNieErfolgreichRollback: LCM ist per TCP erreichbar, aber das
// Binary kommt nie zu einem funktionierenden Austausch (die Gegenstelle nimmt
// die Verbindung an und antwortet nie) - das bisherige Verhalten: Rückfall.
func TestHandshakeNieErfolgreichRollback(t *testing.T) {
	r := newHealthRig(t)
	writeFile(t, r.c.update.prevPath(), "binary-A")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, conn) }() // liest, antwortet nie
		}
	}()

	r.connectTo(t, "http://"+ln.Addr().String())
	go r.c.watchHealth()

	if code, ok := r.exited(3 * time.Second); !ok || code != 1 {
		t.Fatalf("exit = %d %v, erwartet Rückfall mit 1", code, ok)
	}
	if r.c.refused.Load() {
		t.Error("ein stummes Gegenüber gilt nicht als Abweisung")
	}
	if r.c.update.read("fail-reason") != "not-ready\n" {
		t.Errorf("verdikt: %q", r.c.update.read("fail-reason"))
	}
}

// TestBereitIstBewaehrt: Verbindet sich der Agent und wird bereit, gilt er als
// gesund - ohne Rückfall, mit gesichertem Binary.
func TestBereitIstBewaehrt(t *testing.T) {
	r := newHealthRig(t)
	writeFile(t, r.c.update.prevPath(), "binary-A")
	writeFile(t, r.c.update.path("starts"), "2\n")
	_, addr := startHelperBroker(t)

	r.connectTo(t, "http://"+addr)
	go r.c.watchHealth()

	waitState(t, "starts = 0", func() bool { return r.c.update.starts() == 0 && r.c.update.read("starts") != "" })
	waitState(t, "Kopie", func() bool {
		b, _ := os.ReadFile(r.c.update.prevPath())
		return string(b) == "binary-B"
	})
	if r.c.refused.Load() {
		t.Error("ein bereiter Agent gilt als abgewiesen")
	}
	if _, ok := r.exited(100 * time.Millisecond); ok {
		t.Error("ein gesunder Agent beendet sich nicht")
	}
}

// TestAbweisungEndetMitDerNaechstenVerbindung: Die Abweisung gilt nur bis zum
// nächsten Versuch - kommt LCM wieder an und das Binary wird bereit, ist alles gut.
func TestAbweisungEndetMitDerNaechstenVerbindung(t *testing.T) {
	c, _ := testClient()
	c.refused.Store(true)
	c.reachable = func() bool { return true }
	if c.blamesBinary() {
		t.Error("abgewiesener Agent: das Binary ist nicht schuld")
	}
	c.refused.Store(false)
	if !c.blamesBinary() {
		t.Error("erreichbares LCM ohne Abweisung: das Binary ist schuld")
	}
	c.reachable = func() bool { return false }
	if c.blamesBinary() {
		t.Error("ohne Netz ist das Binary nicht schuld")
	}
}

// TestLcmRefusedErkenntNurAusdruecklicheAbweisungen: Welche Verbindungsfehler als
// Abweisung durch LCM zählen - und welche offen bleiben.
func TestLcmRefusedErkenntNurAusdruecklicheAbweisungen(t *testing.T) {
	network := func(cause error) error { return fmt.Errorf("%w : %w", mqttpackets.ErrorNetworkError, cause) }
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"falsche Zugangsdaten", fmt.Errorf("verbinden: %w", mqttpackets.ErrorRefusedBadUsernameOrPassword), true},
		{"nicht berechtigt", fmt.Errorf("verbinden: %w", mqttpackets.ErrorRefusedNotAuthorised), true},
		{"Kennung abgelehnt", fmt.Errorf("verbinden: %w", mqttpackets.ErrorRefusedIDRejected), true},
		{"Server nicht verfügbar", fmt.Errorf("verbinden: %w", mqttpackets.ErrorRefusedServerUnavailable), true},
		{"Upgrade abgelehnt (401/403/429/503)", fmt.Errorf("verbinden: %w", network(websocket.ErrBadHandshake)), true},

		{"Netzfehler", fmt.Errorf("verbinden: %w", network(io.EOF)), false},
		{"Zeitüberschreitung", fmt.Errorf("verbinden: %w", network(errors.New("i/o timeout"))), false},
		{"keine Antwort des Brokers", fmt.Errorf("verbinden: %w", errNoAnswer), false},
		{"Protokollverstoß", fmt.Errorf("verbinden: %w", mqttpackets.ErrorProtocolViolation), false},
		{"Protokollversion", fmt.Errorf("verbinden: %w", mqttpackets.ErrorRefusedBadProtocolVersion), false},
		{"TLS-Prüfung", fmt.Errorf("verbinden: %w", network(errors.New("tls: failed to verify certificate"))), false},
		{"Beenden", errStopped, false},
		{"kein Fehler", nil, false},
	} {
		if got := lcmRefused(tt.err); got != tt.want {
			t.Errorf("%s: lcmRefused = %v, erwartet %v", tt.name, got, tt.want)
		}
	}
}
