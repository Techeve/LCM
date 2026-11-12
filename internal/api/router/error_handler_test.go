package router

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
)

// captureLog leitet slog für die Dauer eines Tests in einen Puffer um.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func errorApp(err error) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: jsonErrorHandler})
	app.Get("/x", func(fiber.Ctx) error { return err })
	return app
}

// TestErrorHandlerLogsInternalCause: Ein unerwarteter Fehler wird zum 500 -
// der Client sieht die Ursache nicht, das Log muss sie haben.
func TestErrorHandlerLogsInternalCause(t *testing.T) {
	logBuf := captureLog(t)
	resp, err := errorApp(errors.New("datenbank gesperrt")).Test(httptest.NewRequest("GET", "/x", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 || strings.Contains(string(body), "datenbank gesperrt") {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}
	if !strings.Contains(logBuf.String(), "datenbank gesperrt") || !strings.Contains(logBuf.String(), "/x") {
		t.Fatalf("ursache fehlt im log: %q", logBuf.String())
	}
}

// TestErrorHandlerKeepsExpectedErrorsQuiet: Erwartete Fehler (404, 409, …)
// gehören nicht ins Fehler-Log.
func TestErrorHandlerKeepsExpectedErrorsQuiet(t *testing.T) {
	logBuf := captureLog(t)
	resp, err := errorApp(apierr.NotFound).Test(httptest.NewRequest("GET", "/x", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 || logBuf.Len() != 0 {
		t.Fatalf("status %d, log %q", resp.StatusCode, logBuf.String())
	}
}
