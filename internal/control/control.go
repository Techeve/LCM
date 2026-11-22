// Package control ist die Steuerbuchse des laufenden Dienstes: ein Unix-Socket
// im Datenverzeichnis, über den Unterbefehle wie `lcm tls reload` den Dienst
// ansprechen, ohne dass er neu startet.
//
// Das Protokoll ist bewusst klein: eine Zeile JSON als Anfrage
// (`{"cmd":"tls.reload"}`, bei Bedarf mit `"arg"`), eine Zeile JSON als
// Antwort, dann schließt der Server. Wer die Buchse öffnen darf, regelt die Dateirechte (0600, Dienst-
// nutzer und root); eine eigene Anmeldung gibt es nicht.
package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"LCM/internal/safego"
)

const (
	// SocketName ist der Dateiname der Buchse im Datenverzeichnis.
	SocketName = "control.sock"

	// maxRequestBytes begrenzt eine Anfrage; ein Befehl ist eine kurze Zeile.
	maxRequestBytes = 4096
	// ioTimeout gilt für das Lesen der Anfrage und das Schreiben der Antwort.
	ioTimeout = 5 * time.Second
	// probeTimeout: so lange darf der Verbindungsversuch auf eine vorhandene
	// Buchse dauern, bevor sie als verwaist gilt.
	probeTimeout = time.Second
	// drainBytes: so viel ungelesene Anfrage wird nach der Antwort noch
	// verworfen, damit der Client die Antwort lesen kann, statt einen
	// abgebrochenen Socket zu sehen.
	drainBytes = 64 << 10
)

// ErrInUse: unter dem Pfad antwortet schon eine laufende Instanz.
var ErrInUse = errors.New("steuerbuchse wird von einer laufenden instanz benutzt")

// ErrUnreachable: der Client erreicht die Buchse nicht.
var ErrUnreachable = errors.New("steuerbuchse nicht erreichbar")

// Request ist die Anfrage: ein Befehl aus der Befehlstabelle, bei Bedarf mit
// einem Argument (`network.reset` nennt den Endpunkt). Ältere Clients ohne
// Arg bleiben gültig.
type Request struct {
	Cmd string `json:"cmd"`
	Arg string `json:"arg,omitempty"`
}

// Response ist die Antwort. Bei OK steht das Ergebnis des Befehls in Result,
// sonst die Fehlermeldung in Error.
type Response struct {
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// Handler führt einen Befehl aus; arg ist das Argument der Anfrage, leer ohne
// eines. Das Ergebnis geht als JSON an den Client; ein Fehler wird zur
// Fehlerantwort.
type Handler func(arg string) (any, error)

// Server nimmt Anfragen an der Buchse an, eine nach der anderen.
type Server struct {
	listener net.Listener
	path     string
	handlers map[string]Handler
	done     sync.WaitGroup
}

// Listen öffnet die Buchse unter path (Rechte 0600) und bedient sie im
// Hintergrund. Eine verwaiste Datei aus einem früheren Lauf wird ersetzt;
// antwortet dort noch eine Instanz, bleibt sie unberührt und es gibt
// ErrInUse.
func Listen(path string, handlers map[string]Handler) (*Server, error) {
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("steuerbuchse öffnen: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("rechte der steuerbuchse setzen: %w", err)
	}
	s := &Server{listener: listener, path: path, handlers: handlers}
	s.done.Add(1)
	safego.Go("control-socket", s.serve)
	return s, nil
}

// removeStaleSocket räumt eine Buchse weg, auf der niemand mehr lauscht. Eine
// Datei, die keine Buchse ist, und eine, an der sich jemand anmelden lässt,
// bleiben liegen.
func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("steuerbuchse prüfen: %w", err)
	}
	if info.Mode().Type() != os.ModeSocket {
		return fmt.Errorf("%s existiert und ist keine buchse", path)
	}
	if conn, err := net.DialTimeout("unix", path, probeTimeout); err == nil {
		_ = conn.Close()
		return fmt.Errorf("%w: %s", ErrInUse, path)
	}
	return os.Remove(path)
}

// Close schließt die Buchse, wartet auf die laufende Anfrage und entfernt die
// Datei.
func (s *Server) Close() {
	_ = s.listener.Close()
	s.done.Wait()
	_ = os.Remove(s.path)
}

func (s *Server) serve() {
	defer s.done.Done()
	for {
		conn, err := s.listener.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			slog.Warn("control socket: accept failed", "error", err)
			continue
		}
		s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	resp := s.answer(conn)
	_ = conn.SetWriteDeadline(time.Now().Add(ioTimeout))
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		slog.Warn("control socket: writing the response failed", "error", err)
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(ioTimeout))
	_, _ = io.Copy(io.Discard, io.LimitReader(conn, drainBytes))
}

// answer liest die Anfrage und führt den Befehl aus.
func (s *Server) answer(conn net.Conn) Response {
	req, err := readRequest(conn)
	if err != nil {
		return Response{Error: err.Error()}
	}
	handler, ok := s.handlers[req.Cmd]
	if !ok {
		return Response{Error: fmt.Sprintf("unbekannter befehl %q", req.Cmd)}
	}
	result, err := handler(req.Arg)
	if err != nil {
		return Response{Error: err.Error()}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return Response{Error: fmt.Sprintf("ergebnis nicht darstellbar: %v", err)}
	}
	return Response{OK: true, Result: raw}
}

func readRequest(conn net.Conn) (Request, error) {
	var req Request
	_ = conn.SetReadDeadline(time.Now().Add(ioTimeout))
	line, err := bufio.NewReader(io.LimitReader(conn, maxRequestBytes+1)).ReadBytes('\n')
	if len(line) > maxRequestBytes {
		return req, fmt.Errorf("anfrage größer als %d bytes", maxRequestBytes)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return req, fmt.Errorf("anfrage lesen: %w", err)
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return req, errors.New("anfrage ist kein gültiges json")
	}
	return req, nil
}

// RemoteError ist die Fehlermeldung, mit der der Dienst einen Befehl
// abgelehnt hat.
type RemoteError string

func (e RemoteError) Error() string { return string(e) }

// callTimeout begrenzt einen ganzen Aufruf; ein Neuladen liest nur Dateien.
const callTimeout = 30 * time.Second

// Call schickt cmd an die Buchse unter path und liest das Ergebnis in result
// (nil, wenn der Befehl keines liefert). Ist die Buchse nicht erreichbar,
// wickelt der Fehler ErrUnreachable ein; lehnt der Dienst den Befehl ab, ist
// er ein RemoteError.
func Call(path, cmd string, result any) error {
	return CallWithArg(path, cmd, "", result)
}

// CallWithArg ist Call für einen Befehl mit Argument.
func CallWithArg(path, cmd, arg string, result any) error {
	conn, err := net.DialTimeout("unix", path, probeTimeout)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(callTimeout))

	if err := json.NewEncoder(conn).Encode(Request{Cmd: cmd, Arg: arg}); err != nil {
		return fmt.Errorf("anfrage senden: %w", err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("antwort lesen: %w", err)
	}
	if !resp.OK {
		return RemoteError(resp.Error)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, result)
}

// SocketPath ist der Pfad der Buchse zu einem Datenverzeichnis.
func SocketPath(dataDir string) string {
	return filepath.Join(dataDir, SocketName)
}
