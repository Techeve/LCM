package remote

// Fehlerinjektion für die Strecke Agent → LCM: ein TCP-Proxy, der zwischen
// Agent und Listener sitzt und die Leitung gezielt stören kann (siehe
// chaos_test.go).

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// faultProxy leitet TCP-Verbindungen an ein Ziel weiter und kann sie kappen,
// verzögern, neue Verbindungen abweisen oder in eine oder beide Richtungen
// halb offen hängen lassen (liest weiter, leitet aber nicht mehr weiter).
type faultProxy struct {
	ln net.Listener

	mu       sync.Mutex
	target   string
	refusing bool          // neue Verbindungen sofort schließen
	delay    time.Duration // Verzögerung je weitergeleitetem Block
	conns    map[*proxyConn]struct{}
	attempts []time.Time // Zeitpunkt jedes Verbindungsversuchs (auch abgewiesener)
	closed   bool

	accepted atomic.Int64
}

// proxyConn ist ein Paar aus Agent- und Zielseite.
type proxyConn struct {
	client, server net.Conn
	// dropUp/dropDown: Agent → LCM bzw. LCM → Agent verschlucken.
	dropUp, dropDown atomic.Bool
	// swallowed: Es wurde schon etwas verschluckt - die Verbindung ist nicht
	// mehr zu retten und wird beim Beheben der Störung gekappt.
	swallowed atomic.Bool
}

// newFaultProxy lauscht auf einem freien Port von 127.0.0.1 und leitet an target
// weiter.
func newFaultProxy(target string) (*faultProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &faultProxy{ln: ln, target: target, conns: map[*proxyConn]struct{}{}}
	go p.acceptLoop()
	return p, nil
}

// Addr ist die Adresse, mit der sich der Agent verbindet.
func (p *faultProxy) Addr() string { return p.ln.Addr().String() }

// Accepted zählt die angenommenen Verbindungen (auch die abgewiesenen).
func (p *faultProxy) Accepted() int { return int(p.accepted.Load()) }

// Attempts liefert die Zeitpunkte aller bisherigen Verbindungsversuche.
func (p *faultProxy) Attempts() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Time(nil), p.attempts...)
}

// SetTarget richtet neue Verbindungen auf ein anderes Ziel (Neustart des Servers
// auf anderem Port). Bestehende bleiben unberührt.
func (p *faultProxy) SetTarget(addr string) {
	p.mu.Lock()
	p.target = addr
	p.mu.Unlock()
}

// CutAll kappt alle bestehenden Verbindungen (RST/EOF auf beiden Seiten).
func (p *faultProxy) CutAll() {
	for _, c := range p.snapshot() {
		c.close()
	}
}

// Refuse weist neue Verbindungen ab (Annehmen und sofort Schließen) oder nimmt
// sie wieder an.
func (p *faultProxy) Refuse(on bool) {
	p.mu.Lock()
	p.refusing = on
	p.mu.Unlock()
}

// Outage kappt alles und weist neue Verbindungen ab: das Netz ist weg.
func (p *faultProxy) Outage() {
	p.Refuse(true)
	p.CutAll()
}

// SetDelay verzögert jeden weitergeleiteten Block um d (0 = aus).
func (p *faultProxy) SetDelay(d time.Duration) {
	p.mu.Lock()
	p.delay = d
	p.mu.Unlock()
}

// Blackhole lässt die BESTEHENDEN Verbindungen halb offen hängen: Der Proxy
// liest weiter, leitet in die gewählte Richtung aber nichts mehr weiter. Weder
// Agent noch Server bekommen ein Schließen zu sehen. Neue Verbindungen laufen
// normal, so wie nach dem Ablauf einer NAT-Zuordnung.
func (p *faultProxy) Blackhole(up, down bool) {
	for _, c := range p.snapshot() {
		c.dropUp.Store(up)
		c.dropDown.Store(down)
	}
}

// Heal hebt alle Störungen auf. Verbindungen, in denen schon Daten verschluckt
// wurden, kappt es: Sie wären nicht mehr im Takt, so wie eine echte halb
// offene Verbindung nie von selbst zurückkehrt.
func (p *faultProxy) Heal() {
	p.mu.Lock()
	p.refusing, p.delay = false, 0
	p.mu.Unlock()
	for _, c := range p.snapshot() {
		if c.swallowed.Load() {
			c.close()
		}
	}
}

// Close beendet den Proxy samt aller Verbindungen.
func (p *faultProxy) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	_ = p.ln.Close()
	p.CutAll()
}

func (p *faultProxy) snapshot() []*proxyConn {
	p.mu.Lock()
	defer p.mu.Unlock()
	conns := make([]*proxyConn, 0, len(p.conns))
	for c := range p.conns {
		conns = append(conns, c)
	}
	return conns
}

func (p *faultProxy) acceptLoop() {
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.serve(client)
	}
}

// serve nimmt eine Verbindung an und verbindet sie mit dem Ziel.
func (p *faultProxy) serve(client net.Conn) {
	p.mu.Lock()
	p.attempts = append(p.attempts, time.Now())
	refuse, target, closed := p.refusing, p.target, p.closed
	p.mu.Unlock()
	p.accepted.Add(1)
	if refuse || closed {
		_ = client.Close()
		return
	}
	server, err := net.DialTimeout("tcp", target, 2*time.Second)
	if err != nil {
		_ = client.Close()
		return
	}
	c := &proxyConn{client: client, server: server}
	p.mu.Lock()
	p.conns[c] = struct{}{}
	p.mu.Unlock()
	done := make(chan struct{}, 2)
	go func() { p.pump(c, client, server, true); done <- struct{}{} }()
	go func() { p.pump(c, server, client, false); done <- struct{}{} }()
	<-done
	c.close()
	<-done
	p.mu.Lock()
	delete(p.conns, c)
	p.mu.Unlock()
}

// pump kopiert src nach dst und wendet dabei die aktuelle Störung an. up heißt
// Richtung Agent → LCM.
func (p *faultProxy) pump(c *proxyConn, src, dst net.Conn, up bool) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 && !p.forward(c, dst, buf[:n], up) {
			return
		}
		if err != nil {
			return
		}
	}
}

// forward schickt einen Block weiter; false, wenn das Ziel nicht mehr da ist.
func (p *faultProxy) forward(c *proxyConn, dst net.Conn, data []byte, up bool) bool {
	p.mu.Lock()
	delay := p.delay
	p.mu.Unlock()
	if (up && c.dropUp.Load()) || (!up && c.dropDown.Load()) {
		c.swallowed.Store(true)
		return true
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	_, err := dst.Write(data)
	return err == nil
}

func (c *proxyConn) close() {
	_ = c.client.Close()
	_ = c.server.Close()
}
