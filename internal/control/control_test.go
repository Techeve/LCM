package control

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// socketPath liegt in einem kurzen Verzeichnis: Unix-Socket-Pfade sind auf
// macOS auf rund 104 Zeichen begrenzt, t.TempDir() wird dafür oft zu lang.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lcm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, SocketName)
}

func echoHandlers() map[string]Handler {
	return map[string]Handler{
		"ping": func(string) (any, error) { return map[string]string{"antwort": "pong"}, nil },
		"fail": func(string) (any, error) { return nil, errors.New("kaputt") },
		"echo": func(arg string) (any, error) { return map[string]string{"arg": arg}, nil },
	}
}

func listen(t *testing.T, path string) *Server {
	t.Helper()
	s, err := Listen(path, echoHandlers())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestAnfrageUndAntwort(t *testing.T) {
	path := socketPath(t)
	listen(t, path)

	var got map[string]string
	if err := Call(path, "ping", &got); err != nil {
		t.Fatal(err)
	}
	if got["antwort"] != "pong" {
		t.Errorf("Ergebnis = %v", got)
	}

	var remote RemoteError
	if err := Call(path, "fail", nil); !errors.As(err, &remote) || remote != "kaputt" {
		t.Errorf("Fehler des Befehls = %v", err)
	}
}

// Das Argument erreicht den Befehl; ohne Argument bleibt es leer.
func TestBefehlMitArgument(t *testing.T) {
	path := socketPath(t)
	listen(t, path)

	var got map[string]string
	if err := CallWithArg(path, "echo", "agent", &got); err != nil || got["arg"] != "agent" {
		t.Errorf("mit Argument: %v, %v", got, err)
	}
	if err := Call(path, "echo", &got); err != nil || got["arg"] != "" {
		t.Errorf("ohne Argument: %v, %v", got, err)
	}
}

func TestBuchseHatRechte0600(t *testing.T) {
	path := socketPath(t)
	listen(t, path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("Rechte = %v, erwartet 0600", info.Mode().Perm())
	}
}

func TestUnbekannterBefehl(t *testing.T) {
	path := socketPath(t)
	listen(t, path)

	err := Call(path, "network.reset", nil)
	var remote RemoteError
	if !errors.As(err, &remote) || !strings.Contains(string(remote), "unbekannter befehl") {
		t.Errorf("Fehler = %v", err)
	}
}

// roh schickt Rohdaten und liefert die Antwort des Servers.
func roh(t *testing.T, path string, data string) string {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	return string(buf[:n])
}

func TestZuGrosseAnfrageWirdAbgelehnt(t *testing.T) {
	path := socketPath(t)
	listen(t, path)

	answer := roh(t, path, `{"cmd":"`+strings.Repeat("a", 5000)+"\"}\n")
	if !strings.Contains(answer, "größer als") {
		t.Errorf("Antwort = %q", answer)
	}
	// Der Server bedient danach weiter.
	if err := Call(path, "ping", nil); err != nil {
		t.Errorf("nach der großen Anfrage: %v", err)
	}
}

func TestKeinJsonWirdAbgelehnt(t *testing.T) {
	path := socketPath(t)
	listen(t, path)

	if answer := roh(t, path, "hallo\n"); !strings.Contains(answer, "kein gültiges json") {
		t.Errorf("Antwort = %q", answer)
	}
}

func TestVerwaisteDateiWirdErsetzt(t *testing.T) {
	path := socketPath(t)
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false) // wie nach einem Absturz
	_ = stale.Close()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("die verwaiste Buchse fehlt: %v", err)
	}

	listen(t, path)
	if err := Call(path, "ping", nil); err != nil {
		t.Errorf("nach dem Ersetzen: %v", err)
	}
}

func TestLaufendeInstanzWirdNichtGestohlen(t *testing.T) {
	path := socketPath(t)
	first := listen(t, path)

	second, err := Listen(path, echoHandlers())
	if !errors.Is(err, ErrInUse) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("Fehler = %v, erwartet ErrInUse", err)
	}
	if err := Call(path, "ping", nil); err != nil {
		t.Errorf("die erste Instanz antwortet nicht mehr: %v", err)
	}
	first.Close()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("Close lässt die Datei liegen: %v", err)
	}
}

func TestRegulaereDateiBleibtLiegen(t *testing.T) {
	path := socketPath(t)
	if err := os.WriteFile(path, []byte("wichtig"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path, echoHandlers()); err == nil {
		t.Fatal("eine Datei, die keine Buchse ist, darf nicht ersetzt werden")
	}
	if data, _ := os.ReadFile(path); string(data) != "wichtig" {
		t.Errorf("Datei verändert: %q", data)
	}
}

func TestCallOhneDienstIstNichtErreichbar(t *testing.T) {
	err := Call(socketPath(t), "ping", nil)
	if !errors.Is(err, ErrUnreachable) {
		t.Errorf("Fehler = %v, erwartet ErrUnreachable", err)
	}
}
