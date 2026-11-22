package sniroute_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"LCM/internal/sniroute"
)

// testTLSConfig liefert für jeden Namen dasselbe selbstsignierte Zertifikat;
// die Tests prüfen das Verteilen, nicht die Zertifikatswahl.
func testTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sniroute.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"sniroute.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
}

// routes ordnet Namen Zielen zu; ein Wert aus Atomic erlaubt, sie im Test zu ändern.
func routes(m map[string]string) func(string) string {
	var table atomic.Pointer[map[string]string]
	table.Store(&m)
	return func(name string) string { return (*table.Load())[name] }
}

// startRouter startet einen Router auf einem Loopback-Port. Jedes Ziel in
// targets antwortet auf eine Verbindung mit seinem Namen und schließt sie.
func startRouter(t *testing.T, cfg sniroute.Config, targets ...string) (*sniroute.Router, string) {
	t.Helper()
	if cfg.TLS == nil {
		cfg.TLS = testTLSConfig(t)
	}
	router := sniroute.New(cfg)
	for _, name := range targets {
		answerWithName(router.Listen(name), name)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- router.Serve(ln) }()
	t.Cleanup(func() {
		_ = router.Close()
		if err := <-served; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	return router, ln.Addr().String()
}

func answerWithName(ln net.Listener, name string) {
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte(name))
			_ = conn.Close()
		}
	}()
}

// ask verbindet mit TLS und dem Namen und liefert, welches Ziel antwortet.
// Ohne Namen sendet der Client kein SNI.
func ask(t *testing.T, addr, serverName string) string {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, ServerName: serverName})
	if err != nil {
		t.Fatalf("Handshake mit Name %q: %v", serverName, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	answer, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("Antwort lesen: %v", err)
	}
	return string(answer)
}

func TestVerbindungLandetImZielIhresNamens(t *testing.T) {
	_, addr := startRouter(t, sniroute.Config{
		Resolve:  routes(map[string]string{"ui.test": "ui", "agent.test": "agent", "mcp.test": "mcp"}),
		Fallback: "ui",
	}, "ui", "agent", "mcp")

	for name, want := range map[string]string{"ui.test": "ui", "agent.test": "agent", "mcp.test": "mcp"} {
		if got := ask(t, addr, name); got != want {
			t.Errorf("Name %s: Ziel %q, erwartet %q", name, got, want)
		}
	}
}

func TestOhneSNIUndUnbekannterNameGehenAnDenFallback(t *testing.T) {
	_, addr := startRouter(t, sniroute.Config{
		Resolve:  routes(map[string]string{"agent.test": "agent"}),
		Fallback: "ui",
	}, "ui", "agent")

	// 127.0.0.1 als Ziel: Der Client sendet für eine IP-Adresse kein SNI.
	if got := ask(t, addr, ""); got != "ui" {
		t.Errorf("ohne SNI: Ziel %q, erwartet ui", got)
	}
	if got := ask(t, addr, "unbekannt.test"); got != "ui" {
		t.Errorf("unbekannter Name: Ziel %q, erwartet ui", got)
	}
}

func TestNichtAngeschlossenesZielGehtAnDenFallback(t *testing.T) {
	// mcp.test ist zugeordnet, aber niemand hat sich als mcp angeschlossen.
	_, addr := startRouter(t, sniroute.Config{
		Resolve:  routes(map[string]string{"mcp.test": "mcp"}),
		Fallback: "ui",
	}, "ui")

	if got := ask(t, addr, "mcp.test"); got != "ui" {
		t.Errorf("Ziel %q, erwartet ui", got)
	}
}

func TestZielLaesstSichZurLaufzeitAnUndAbschliessen(t *testing.T) {
	router, addr := startRouter(t, sniroute.Config{
		Resolve:  routes(map[string]string{"mcp.test": "mcp"}),
		Fallback: "ui",
	}, "ui")

	mcp := router.Listen("mcp")
	answerWithName(mcp, "mcp")
	if got := ask(t, addr, "mcp.test"); got != "mcp" {
		t.Errorf("angeschlossen: Ziel %q, erwartet mcp", got)
	}

	_ = mcp.Close() // der Dienst fährt herunter
	if got := ask(t, addr, "mcp.test"); got != "ui" {
		t.Errorf("abgeschlossen: Ziel %q, erwartet ui", got)
	}
}

func TestErneutesAnschliessenErsetztDasAlteZiel(t *testing.T) {
	router, _ := startRouter(t, sniroute.Config{Fallback: "ui"}, "ui")

	first := router.Listen("mcp")
	second := router.Listen("mcp")
	if _, err := first.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("alter Listener: Accept liefert %v, erwartet net.ErrClosed", err)
	}
	// Das alte Schließen darf den neuen nicht abmelden.
	_ = first.Close()
	answerWithName(second, "mcp")
}

func TestAnderungDerZuordnungGiltOhneNeustart(t *testing.T) {
	var target atomic.Value
	target.Store("ui")
	_, addr := startRouter(t, sniroute.Config{
		Resolve:  func(string) string { return target.Load().(string) },
		Fallback: "ui",
	}, "ui", "agent")

	if got := ask(t, addr, "lcm.test"); got != "ui" {
		t.Fatalf("vorher: Ziel %q", got)
	}
	target.Store("agent")
	if got := ask(t, addr, "lcm.test"); got != "agent" {
		t.Errorf("nachher: Ziel %q, erwartet agent", got)
	}
}

func TestStummeVerbindungWirdNachDemZeitlimitGeschlossen(t *testing.T) {
	_, addr := startRouter(t, sniroute.Config{Fallback: "ui", HandshakeTimeout: 100 * time.Millisecond}, "ui")

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	start := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("der Router hat Daten gesendet, erwartet war das Schließen")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("der Router hat die stumme Verbindung nicht geschlossen")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("geschlossen nach %v, erwartet etwa das Zeitlimit", elapsed)
	}
}

func TestHandshakeGrenzeWirftUeberzaehligeVerbindungSofortAb(t *testing.T) {
	_, addr := startRouter(t, sniroute.Config{
		Fallback:             "ui",
		HandshakeTimeout:     2 * time.Second,
		MaxPendingHandshakes: 2,
	}, "ui")

	// Die Accept-Schleife nimmt die Verbindungen der Reihe nach an: Die ersten
	// zwei belegen die beiden Plätze, die dritte kommt darüber hinaus.
	var held []net.Conn
	for range 2 {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		held = append(held, conn)
	}
	third, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = third.Close() }()

	buf := make([]byte, 1)
	_ = third.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := third.Read(buf); err == nil || isTimeout(err) {
		t.Fatalf("dritte Verbindung: Read liefert %v, erwartet sofortiges Schließen", err)
	}

	for i, conn := range held {
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		if _, err := conn.Read(buf); !isTimeout(err) {
			t.Errorf("Verbindung %d: Read liefert %v, erwartet offen bis zum Zeitlimit", i, err)
		}
	}
	for i, conn := range held {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Read(buf); !errors.Is(err, io.EOF) {
			t.Errorf("Verbindung %d: Read liefert %v, erwartet Schließen nach dem Zeitlimit", i, err)
		}
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func TestKeinTLSWirdAbgewiesenUndDerRouterLaeuftWeiter(t *testing.T) {
	_, addr := startRouter(t, sniroute.Config{Fallback: "ui"}, "ui")

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.ReadAll(conn) // Alert und Ende
	_ = conn.Close()

	if got := ask(t, addr, "x.test"); got != "ui" {
		t.Errorf("nach der Fehlverbindung: Ziel %q, erwartet ui", got)
	}
}

func TestCloseBeendetAllesSauber(t *testing.T) {
	router := sniroute.New(sniroute.Config{TLS: testTLSConfig(t), Fallback: "ui", HandshakeTimeout: time.Minute})
	ui := router.Listen("ui")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- router.Serve(ln) }()

	// Eine Verbindung steckt im Handshake fest, wenn Close kommt.
	stuck, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stuck.Close() }()
	time.Sleep(50 * time.Millisecond)

	closed := make(chan error, 1)
	go func() { closed <- router.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close kehrt nicht zurück")
	}
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve: %v, erwartet nil nach Close", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve kehrt nach Close nicht zurück")
	}
	if _, err := ui.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Accept nach Close: %v, erwartet net.ErrClosed", err)
	}
	_ = stuck.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := stuck.Read(make([]byte, 1)); err == nil {
		t.Error("die hängende Verbindung blieb offen")
	}
	if c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		_ = c.Close()
		t.Error("der Port nimmt nach Close noch Verbindungen an")
	}
	if err := router.Close(); err != nil {
		t.Errorf("zweites Close: %v", err)
	}
}

func TestListenNachCloseLiefertGeschlossenenListener(t *testing.T) {
	router := sniroute.New(sniroute.Config{TLS: testTLSConfig(t), Fallback: "ui"})
	_ = router.Close()
	if _, err := router.Listen("ui").Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Accept: %v, erwartet net.ErrClosed", err)
	}
}

func TestNameUndHostVergleichenOhneGrossschreibungPortUndPunkt(t *testing.T) {
	tests := []struct{ in, wantHost string }{
		{"https://LCM.Example.org:9310", "lcm.example.org"},
		{"https://lcm.example.org", "lcm.example.org"},
		{"https://lcm.example.org.:443", "lcm.example.org"},
		{"https://[2001:db8::1]:443", "2001:db8::1"},
		{"", ""},
		{"::keine adresse", ""},
	}
	for _, tt := range tests {
		if got := sniroute.HostOf(tt.in); got != tt.wantHost {
			t.Errorf("HostOf(%q) = %q, erwartet %q", tt.in, got, tt.wantHost)
		}
	}
	if got := sniroute.Name("Agent.Example.ORG."); got != "agent.example.org" {
		t.Errorf("Name = %q", got)
	}
}
