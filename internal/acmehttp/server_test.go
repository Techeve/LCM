package acmehttp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"LCM/internal/infrastructure/acmex"
)

func newServer(t *testing.T) (*Server, *acmex.ChallengeStore) {
	t.Helper()
	tokens := acmex.NewChallengeStore()
	return New(tokens), tokens
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

// startServer öffnet den Server auf einem freien Port; Stop räumt auf.
func startServer(t *testing.T, s *Server) string {
	t.Helper()
	addr := freeAddr(t)
	if err := s.Start(addr); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return addr
}

// raw schickt die Bytes und liefert alles, was der Server zurückschickt, bis er
// die Verbindung schließt. Ein Server, der nicht antwortet, liefert "".
func raw(t *testing.T, addr, request string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// Schließt der Server mitten im Senden (zu große Anfrage), scheitert das
	// Schreiben oder Lesen mit einem Reset: Auch das ist keine Antwort.
	_, _ = io.WriteString(conn, request)
	reply, err := io.ReadAll(conn)
	if err != nil && !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("der Server hat die Verbindung nicht geschlossen: %v (gelesen: %q)", err, reply)
	}
	return string(reply)
}

func getRequest(target string) string {
	return "GET " + target + " HTTP/1.1\r\nHost: lcm.example.com\r\n\r\n"
}

func TestAnswersOnlyTheOpenToken(t *testing.T) {
	s, tokens := newServer(t)
	addr := startServer(t, s)
	if err := tokens.Present(context.Background(), "lcm.example.com", "tok-1_x", "tok-1_x.thumbprint"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://" + addr + challengePath + "tok-1_x")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "tok-1_x.thumbprint" {
		t.Fatalf("offenes Token: %d %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type %q", got)
	}
	for _, leak := range []string{"Server", "Location", "X-Powered-By"} {
		if resp.Header.Get(leak) != "" {
			t.Errorf("Kopfzeile %s verrät mehr als nötig: %q", leak, resp.Header.Get(leak))
		}
	}

	if err := tokens.CleanUp(context.Background(), "lcm.example.com", "tok-1_x", ""); err != nil {
		t.Fatal(err)
	}
	if reply := raw(t, addr, getRequest(challengePath+"tok-1_x")); reply != "" {
		t.Errorf("aufgeräumtes Token bekam eine Antwort: %q", reply)
	}
}

// Alles außer GET auf den Pfad eines offenen Tokens endet ohne ein Byte
// Antwort: keine 404-Seite, keine Weiterleitung, kein Banner.
func TestEverythingElseIsClosedWithoutAnswer(t *testing.T) {
	s, tokens := newServer(t)
	addr := startServer(t, s)
	_ = tokens.Present(context.Background(), "lcm.example.com", "tok", "tok.key")

	cases := map[string]string{
		"unbekanntes Token":          getRequest(challengePath + "unbekannt"),
		"Pfad ohne Token":            getRequest(challengePath),
		"Wurzel":                     getRequest("/"),
		"Weboberfläche":              getRequest("/login?next=%2Fservers"),
		"anderer Pfad":               getRequest("/.well-known/security.txt"),
		"Token mit Abfrage":          getRequest(challengePath + "tok?x=1"),
		"Token mit Pfad dahinter":    getRequest(challengePath + "tok/x"),
		"Pfad aufwärts":              getRequest(challengePath + "../tok"),
		"HEAD":                       "HEAD " + challengePath + "tok HTTP/1.1\r\nHost: x\r\n\r\n",
		"POST":                       "POST " + challengePath + "tok HTTP/1.1\r\nHost: x\r\nContent-Length: 0\r\n\r\n",
		"OPTIONS":                    "OPTIONS * HTTP/1.1\r\nHost: x\r\n\r\n",
		"kein HTTP":                  "hallo\r\n\r\n",
		"zu große Kopfzeile":         "GET / HTTP/1.1\r\nHost: x\r\nX: " + strings.Repeat("a", 2*maxRequestBytes) + "\r\n\r\n",
		"Token als absolute Adresse": "GET http://lcm.example.com" + challengePath + "tok HTTP/1.1\r\nHost: x\r\n\r\n",
	}
	for name, request := range cases {
		if reply := raw(t, addr, request); reply != "" {
			t.Errorf("%s: Antwort %q, erwartet keine", name, reply)
		}
	}
	// Das offene Token ist danach weiter erreichbar.
	if reply := raw(t, addr, getRequest(challengePath+"tok")); !strings.HasSuffix(reply, "tok.key") {
		t.Errorf("offenes Token: %q", reply)
	}
}

func TestStartIsIdempotentAndStopClosesThePort(t *testing.T) {
	s, _ := newServer(t)
	addr := freeAddr(t)
	if s.Running() {
		t.Fatal("vor Start läuft der Listener schon")
	}
	if err := s.Start(addr); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(addr); err != nil || !s.Running() {
		t.Fatalf("zweiter Start auf derselben Adresse: %v, läuft: %v", err, s.Running())
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("offener Port nicht erreichbar: %v", err)
	}
	_ = conn.Close()

	s.Stop()
	s.Stop() // ein zweites Stop schadet nicht
	if s.Running() {
		t.Error("nach Stop läuft der Listener noch")
	}
	if conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Error("der Port ist nach Stop noch offen")
	}
	if err := s.Start(addr); err != nil || !s.Running() {
		t.Errorf("erneuter Start nach Stop: %v", err)
	}
}

// Stop schließt auch Verbindungen, die gerade auf eine Anfrage warten.
func TestStopClosesIdleConnections(t *testing.T) {
	s, _ := newServer(t)
	addr := startServer(t, s)
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	time.Sleep(50 * time.Millisecond) // der Server hat die Verbindung angenommen

	s.Stop()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil || os.IsTimeout(err) {
		t.Errorf("die wartende Verbindung blieb offen: %v", err)
	}
}

func TestStartReportsOccupiedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	s, _ := newServer(t)
	err = s.Start(ln.Addr().String())
	if err == nil || s.Running() {
		t.Fatalf("Start auf belegtem Port: err=%v, läuft=%v", err, s.Running())
	}
	if !strings.Contains(err.Error(), "acme-listener") {
		t.Errorf("Fehler nennt den Listener nicht: %v", err)
	}
}

// errPermission spielt EACCES: Is(os.ErrPermission) trifft darauf zu.
type errPermission struct{}

func (errPermission) Error() string        { return "permission denied" }
func (errPermission) Is(target error) bool { return target == os.ErrPermission }

// Der Hinweis auf die Capability und die Portweiterleitung steht in der
// Meldung, wenn dem Dienst die Berechtigung für den Port fehlt.
func TestBindErrorMentionsCapabilityOnPermissionDenied(t *testing.T) {
	err := bindError(":80", &net.OpError{Op: "listen", Err: errPermission{}})
	for _, want := range []string{"CAP_NET_BIND_SERVICE", "weiterleiten"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("kein Hinweis %q: %v", want, err)
		}
	}
	if other := bindError(":80", net.ErrClosed); strings.Contains(other.Error(), "CAP_NET_BIND_SERVICE") {
		t.Errorf("Hinweis auf die Capability ohne Grund: %v", other)
	}
}

func TestConnectionLimit(t *testing.T) {
	s, tokens := newServer(t)
	s.perIPLimit = maxConns + 1 // hier zählt nur die Grenze für alle Verbindungen
	addr := startServer(t, s)
	_ = tokens.Present(context.Background(), "lcm.example.com", "tok", "tok.key")

	var idle []net.Conn
	defer func() {
		for _, c := range idle {
			_ = c.Close()
		}
	}()
	for range maxConns {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		idle = append(idle, conn)
	}
	time.Sleep(100 * time.Millisecond)

	// Die weitere Verbindung bekommt keinen Platz und wird ohne Antwort geschlossen.
	if reply := raw(t, addr, getRequest(challengePath+"tok")); reply != "" {
		t.Errorf("Verbindung über dem Limit bekam eine Antwort: %q", reply)
	}
}

// Eine Quelladresse besetzt nicht alle Plätze: Über der Grenze je Adresse
// schließt der Server ohne Antwort, andere Adressen und freie Plätze bleiben bedient.
func TestPerSourceConnectionLimit(t *testing.T) {
	s, tokens := newServer(t)
	addr := startServer(t, s)
	_ = tokens.Present(context.Background(), "lcm.example.com", "tok", "tok.key")

	var idle []net.Conn
	defer func() {
		for _, c := range idle {
			_ = c.Close()
		}
	}()
	for range maxConnsPerIP {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		idle = append(idle, conn)
	}
	time.Sleep(100 * time.Millisecond)

	if reply := raw(t, addr, getRequest(challengePath+"tok")); reply != "" {
		t.Errorf("Verbindung über der Grenze je Adresse bekam eine Antwort: %q", reply)
	}

	// Gibt die Adresse einen Platz frei, ist wieder einer da.
	_ = idle[0].Close()
	idle = idle[1:]
	time.Sleep(100 * time.Millisecond)
	if reply := raw(t, addr, getRequest(challengePath+"tok")); !strings.HasSuffix(reply, "tok.key") {
		t.Errorf("nach dem Freigeben: %q", reply)
	}
	// Die beantwortete Verbindung ist erst nach dem Leeren wieder frei.
	for range 50 {
		s.mu.Lock()
		got := s.perIP["127.0.0.1"]
		s.mu.Unlock()
		if got == maxConnsPerIP-1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("die gezählten Verbindungen gehen nicht auf die offenen zurück")
}

// Wer nichts schickt, belegt den Platz nur für die Lesefrist, nicht für die
// ganze Zeit der Verbindung.
func TestSilentConnectionIsClosedAfterReadTimeout(t *testing.T) {
	s, _ := newServer(t)
	addr := startServer(t, s)
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	start := time.Now()
	_ = conn.SetReadDeadline(start.Add(connTimeout))
	_, err = conn.Read(make([]byte, 1))
	if err == nil || os.IsTimeout(err) {
		t.Fatalf("der Server hat die stille Verbindung nicht geschlossen: %v", err)
	}
	if elapsed := time.Since(start); elapsed > readTimeout+2*time.Second {
		t.Errorf("geschlossen nach %v, erwartet etwa %v", elapsed, readTimeout)
	}
}

// Schickt die CA mehr, als der Server liest, bekommt sie trotzdem die ganze
// Antwort: Der Server liest nach der Antwort leer, statt mit ungelesenen
// Daten zu schließen, was die Antwort per Reset verwürfe.
func TestAnswerSurvivesTrailingBytes(t *testing.T) {
	s, tokens := newServer(t)
	addr := startServer(t, s)
	_ = tokens.Present(context.Background(), "lcm.example.com", "tok", "tok.key")

	request := getRequest(challengePath+"tok") + strings.Repeat("x", 40<<10)
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	reply, err := io.ReadAll(conn)
	if err != nil || !strings.HasSuffix(string(reply), "tok.key") {
		t.Errorf("Antwort mit Resten: %q, Fehler %v", reply, err)
	}
}
