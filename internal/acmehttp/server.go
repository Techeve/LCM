// Package acmehttp ist der kleine HTTP-Listener, den LCM für die Prüfung
// HTTP-01 einer ACME-CA öffnet (Standard Port 80).
//
// Er ist nur offen, solange eine Prüfung läuft (services.ACMEService öffnet und
// schließt ihn), und kann nichts außer einem: das Token einer offenen Prüfung
// ausliefern. Auf alles andere antwortet er nicht - keine Weiterleitung, keine
// Fehlerseite, kein Banner. Die Verbindung wird ohne ein Byte geschlossen, als
// lauschte dort nichts.
//
// Dafür liest der Server die Anfrage selbst, statt einen HTTP-Server zu
// nutzen: Ein HTTP-Server antwortet auf kaputte oder zu große Anfragen mit
// einer Fehlerseite, und die verriete, dass hier etwas lauscht.
package acmehttp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"LCM/internal/safego"
)

const (
	// challengePath leitet die Pfade der Prüfung ein (RFC 8555, Abschnitt 8.3).
	challengePath = "/.well-known/acme-challenge/"

	// connTimeout begrenzt eine Verbindung von der Annahme bis zum Ende der
	// Antwort; wer langsam sendet, hält keinen Platz lange besetzt.
	connTimeout = 10 * time.Second
	// readTimeout begrenzt das Lesen der Anfrage; die CA schickt sie sofort.
	readTimeout = 3 * time.Second
	// drainTimeout und maxDrainBytes begrenzen das Leeren der Verbindung nach
	// der Antwort (siehe respond).
	drainTimeout  = 500 * time.Millisecond
	maxDrainBytes = 64 << 10
	// maxRequestBytes begrenzt, was der Server von einer Verbindung liest.
	maxRequestBytes = 8 << 10
	// maxConns begrenzt die gleichzeitigen Verbindungen; weitere schließt der
	// Server sofort.
	maxConns = 32
	// maxConnsPerIP begrenzt die gleichzeitigen Verbindungen je Quelladresse,
	// damit eine Adresse nicht alle Plätze besetzt. Die CA prüft von mehreren
	// Orten, jeder mit wenigen Verbindungen.
	maxConnsPerIP = 4
	// acceptRetryDelay ist die Pause nach einem Fehler beim Annehmen (etwa zu
	// viele offene Dateien), bevor der Server es erneut versucht.
	acceptRetryDelay = 100 * time.Millisecond
)

// Tokens liefert die Antwort auf ein offenes Token (acmex.ChallengeStore).
type Tokens interface {
	KeyAuthorization(token string) (keyAuth string, ok bool)
}

// Server verwaltet den Listener: Start und Stop öffnen und schließen den Port
// zur Laufzeit, ohne dass LCM neu startet.
type Server struct {
	tokens Tokens

	// perIPLimit ist maxConnsPerIP; die Tests setzen es anders.
	perIPLimit int

	mu    sync.Mutex
	ln    net.Listener
	addr  string // aktuelle Listen-Adresse, "" = aus
	conns map[net.Conn]struct{}
	perIP map[string]int // offene Verbindungen je Quelladresse
}

func New(tokens Tokens) *Server {
	return &Server{tokens: tokens, perIPLimit: maxConnsPerIP, conns: map[net.Conn]struct{}{}, perIP: map[string]int{}}
}

// Start öffnet den Listener auf addr. Der Port ist beim Rückkehren gebunden
// oder der Fehler sagt, warum nicht. Läuft er schon auf dieser Adresse,
// passiert nichts.
func (s *Server) Start(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil && s.addr == addr {
		return nil
	}
	s.stopLocked()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return bindError(addr, err)
	}
	s.ln, s.addr = ln, addr
	slog.Info("ACME HTTP-01 listener opened", "address", addr)
	safego.Go("acme-http-listener", func() { s.accept(ln) })
	return nil
}

// Stop schließt den Listener und alle offenen Verbindungen.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// Running meldet, ob der Listener offen ist.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ln != nil
}

func (s *Server) stopLocked() {
	if s.ln == nil {
		return
	}
	_ = s.ln.Close()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.ln, s.addr = nil, ""
	clear(s.conns)
	clear(s.perIP)
	slog.Info("ACME HTTP-01 listener closed")
}

// bindError erklärt den häufigsten Grund, warum Port 80 nicht aufgeht: Der
// Dienst läuft ohne die Capability für Ports unter 1024.
func bindError(addr string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("acme-listener: %s lässt sich nicht öffnen (%w) - für Ports unter 1024 braucht LCM die "+
			"capability CAP_NET_BIND_SERVICE (die mitgelieferte lcm.service setzt sie; bei einer eigenen Unit per Drop-in "+
			"(systemctl edit lcm) im Abschnitt [Service] CapabilityBoundingSet=CAP_NET_BIND_SERVICE und "+
			"AmbientCapabilities=CAP_NET_BIND_SERVICE setzen), oder acme_http_listen auf einen höheren Port legen "+
			"und Port 80 dorthin weiterleiten", addr, err)
	}
	return fmt.Errorf("acme-listener: %s lässt sich nicht öffnen: %w", addr, err)
}

// accept nimmt Verbindungen an, bis der Listener schließt.
func (s *Server) accept(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			time.Sleep(acceptRetryDelay)
			continue
		}
		if !s.track(ln, conn) {
			_ = conn.Close()
			continue
		}
		safego.Go("acme-http-conn", func() {
			defer s.untrack(conn)
			s.serve(conn)
		})
	}
}

// track merkt sich die Verbindung, damit Stop sie schließen kann. false: Der
// Listener ist schon zu oder es sind zu viele Verbindungen offen, insgesamt
// oder von dieser Quelladresse.
func (s *Server) track(ln net.Listener, conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ip := sourceIP(conn)
	if s.ln != ln || len(s.conns) >= maxConns || s.perIP[ip] >= s.perIPLimit {
		return false
	}
	s.conns[conn] = struct{}{}
	s.perIP[ip]++
	return true
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, tracked := s.conns[conn]; !tracked {
		return // Stop hat sie schon vergessen
	}
	delete(s.conns, conn)
	ip := sourceIP(conn)
	if s.perIP[ip]--; s.perIP[ip] <= 0 {
		delete(s.perIP, ip)
	}
}

// sourceIP ist die Quelladresse einer Verbindung, ohne Port.
func sourceIP(conn net.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

// serve liest eine Anfrage. Ist es die Prüfung für ein offenes Token, kommt
// die Antwort; in jedem anderen Fall schließt die Verbindung ohne Antwort.
func (s *Server) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	now := time.Now()
	if conn.SetDeadline(now.Add(connTimeout)) != nil || conn.SetReadDeadline(now.Add(readTimeout)) != nil {
		return
	}
	reader := bufio.NewReader(io.LimitReader(conn, maxRequestBytes))
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	keyAuth, ok := s.answerFor(req)
	if !ok {
		return
	}
	respond(conn, keyAuth)
}

// respond schreibt die Antwort von Hand: nur, was die CA braucht. Kein
// Server-Kopf, keine Weiterleitung; das Token ist base64url und kann keine
// Kopfzeile einschleusen.
//
// Danach schließt es nur die Sendeseite und liest kurz leer, was die CA noch
// schickt: Schlösse der Server mit ungelesenen Daten im Puffer, ginge ein
// Reset (RST) an die CA, und sie verwürfe womöglich die Antwort, bevor sie sie liest.
func respond(conn net.Conn, keyAuth string) {
	_, err := fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\n"+
		"Content-Length: %d\r\nConnection: close\r\n\r\n%s", len(keyAuth), keyAuth)
	closer, ok := conn.(interface{ CloseWrite() error })
	if err != nil || !ok {
		return
	}
	_ = closer.CloseWrite()
	_ = conn.SetReadDeadline(time.Now().Add(drainTimeout))
	_, _ = io.Copy(io.Discard, io.LimitReader(conn, maxDrainBytes))
}

// answerFor liefert die Schlüsselautorisierung, wenn die Anfrage ein GET auf
// den Pfad eines offenen Tokens ist. Eine Abfrage hinter dem Token, ein
// anderer Pfad oder eine andere Methode (auch HEAD) zählt nicht.
func (s *Server) answerFor(req *http.Request) (keyAuth string, ok bool) {
	if req.Method != http.MethodGet {
		return "", false
	}
	token, found := strings.CutPrefix(req.RequestURI, challengePath)
	if !found || token == "" {
		return "", false
	}
	return s.tokens.KeyAuthorization(token)
}
