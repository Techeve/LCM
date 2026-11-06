package middlewares

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// TestAccessLogSchwaerztDieFahrkarte: Die Einmal-Fahrkarte eines WebSockets reist
// in der URL, steht aber nie im Protokoll - auch nicht im Debug-Level, in dem
// die Abfrage mitgeschrieben wird.
func TestAccessLogSchwaerztDieFahrkarte(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	app := fiber.New()
	app.Use(AccessLog(logger))
	app.Get("/api/v1/events", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusUnauthorized) })

	req := httptest.NewRequest("GET", "/api/v1/events?ticket=geheime-fahrkarte&cols=80", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	line := out.String()
	if strings.Contains(line, "geheime-fahrkarte") {
		t.Errorf("die Fahrkarte steht im Zugriffsprotokoll: %s", line)
	}
	if !strings.Contains(line, "cols=80") || !strings.Contains(line, "REDACTED") {
		t.Errorf("Abfrage ohne Schwärzung oder ohne die übrigen Parameter: %s", line)
	}
}

// lockedBuffer nimmt Zeilen aus der Server-Goroutine entgegen, während der Test liest.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestAccessLogIgnoriertAbgebrocheneVerbindung: Verwirft ein Browser eine vorab
// geöffnete Verbindung per RST, läuft die Kette ohne Anfrage mit Status 502. Das
// gehört nicht als Fehler ins Protokoll; ein echter 502 dagegen schon.
func TestAccessLogIgnoriertAbgebrocheneVerbindung(t *testing.T) {
	var out lockedBuffer
	app := fiber.New()
	app.Use(AccessLog(slog.New(slog.NewTextHandler(&out, nil))))
	app.Get("/kaputt", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusBadGateway) })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	conn, err := net.DialTCP("tcp", nil, ln.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetLinger(0); err != nil { // Schließen sendet RST statt FIN
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://%s/kaputt", ln.Addr()))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for deadline := time.Now().Add(2 * time.Second); !strings.Contains(out.String(), "path=/kaputt"); {
		if time.Now().After(deadline) {
			t.Fatalf("echter 502 fehlt im Protokoll: %s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(out.String(), "path=/ ") {
		t.Errorf("abgebrochene Verbindung steht im Zugriffsprotokoll: %s", out.String())
	}
}

func TestRedactQuery(t *testing.T) {
	for in, want := range map[string]string{
		"":                        "",
		"a=1":                     "a=1",
		"ticket=x":                "ticket=REDACTED",
		"term=xterm&ticket=x&b=2": "b=2&term=xterm&ticket=REDACTED",
		"ticket=a&ticket=b":       "ticket=REDACTED",
		"%zz":                     "",
	} {
		if got := redactQuery(in); got != want {
			t.Errorf("redactQuery(%q) = %q, erwartet %q", in, got, want)
		}
	}
}
