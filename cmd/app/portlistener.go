package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/safego"
)

// uiRetireGrace ist die Frist, die der alte Port der Oberfläche nach einem
// Wechsel noch offen bleibt: Der Browser, der den Wechsel ausgelöst hat, bekommt
// seine Antwort auf der alten Verbindung und kann auf die neue Adresse wechseln.
const uiRetireGrace = 10 * time.Second

// portListener bedient eine App auf einem TCP-Port, der sich zur Laufzeit
// wechseln lässt. Alle Methoden sind für nil (den Dienst gibt es nicht) ausgelegt. Der Wechsel bindet den neuen Port zuerst: Scheitert das, läuft
// der alte weiter. Erst danach nimmt der neue Listener die App in Betrieb, und
// der alte schließt nach der Frist grace. Offene Verbindungen bleiben davon
// unberührt.
type portListener struct {
	label     string // für Meldungen: "Oberfläche", "Agent"
	app       *fiber.App
	tlsConfig func() *tls.Config // nil: unverschlüsselt (--dev)
	grace     time.Duration
	// onOpen wird mit der Adresse gerufen, sobald ein neuer Listener bedient. Optional.
	onOpen func(addr string)

	mu   sync.Mutex
	ln   net.Listener
	addr string // "" = kein Listener
}

func newPortListener(label string, app *fiber.App, tlsConfig func() *tls.Config, grace time.Duration) *portListener {
	return &portListener{label: label, app: app, tlsConfig: tlsConfig, grace: grace}
}

// Open bindet addr und bedient die App darauf (beim Start).
func (p *portListener) Open(addr string) error {
	if p == nil {
		return nil
	}
	commit, _, err := p.Prepare(addr)
	if err != nil {
		return err
	}
	commit()
	return nil
}

// Prepare bindet addr (leer: Listener aus), ohne den laufenden anzufassen.
// Scheitert das Binden, kommt der Fehler zurück und alles bleibt beim Alten.
// commit setzt den Wechsel um, abort gibt die gebundene Adresse unbenutzt frei.
func (p *portListener) Prepare(addr string) (commit, abort func(), err error) {
	noop := func() {}
	if p == nil || p.current() == addr {
		return noop, noop, nil
	}
	if addr == "" {
		return func() { p.switchTo(nil, "") }, noop, nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("%s %s: %w", p.label, addr, withBindHint(addr, err))
	}
	return func() { p.switchTo(ln, addr) }, func() { _ = ln.Close() }, nil
}

func (p *portListener) current() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.addr
}

// switchTo nimmt ln in Betrieb (nil: kein Listener mehr) und schließt den alten
// nach der Frist.
func (p *portListener) switchTo(ln net.Listener, addr string) {
	p.mu.Lock()
	old := p.ln
	p.ln, p.addr = ln, addr
	p.mu.Unlock()

	if ln != nil {
		p.serve(ln)
		slog.Info("listener opened", "service", p.label, "address", addr)
		if p.onOpen != nil {
			p.onOpen(addr)
		}
	}
	if old != nil {
		time.AfterFunc(p.grace, func() { _ = old.Close() })
	}
}

// serve bedient die App auf ln, mit TLS, wenn der Pool da ist. Fiber nimmt für
// einen vorhandenen Listener keine TLS-Konfiguration an, darum wird er hier
// verpackt; geschlossen wird der unverpackte.
func (p *portListener) serve(ln net.Listener) {
	serving := ln
	if p.tlsConfig != nil {
		serving = tls.NewListener(ln, p.tlsConfig())
	}
	safego.Go("listener-"+p.label, func() {
		if err := p.app.Listener(serving, fiber.ListenConfig{DisableStartupMessage: true}); err != nil && !isClosedListener(err) {
			slog.Error("listener terminated", "service", p.label, "error", err)
		}
	})
}

// Close schließt den Listener sofort (beim Beenden).
func (p *portListener) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	ln := p.ln
	p.ln, p.addr = nil, ""
	p.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

// isClosedListener erkennt das Ende eines Listeners, den wir selbst geschlossen
// haben: kein Fehler.
func isClosedListener(err error) bool {
	return errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection")
}

// withBindHint hängt an einen verweigerten Port unter 1024 den Hinweis zur
// Capability an (siehe bindHint).
func withBindHint(addr string, err error) error {
	if hint := bindHint(addr, err); hint != "" {
		return fmt.Errorf("%w - %s", err, hint)
	}
	return err
}
