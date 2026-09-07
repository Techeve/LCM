package controllers

import (
	"bufio"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/logging"
)

// LogsController liefert LCMs eigenes Log an die Oberfläche.
//
// Der Sinn: Wer LCM über die Weboberfläche bedient, kam an seine eigenen
// Störungsmeldungen bisher nicht heran - die stehen auf dem LCM-Host in
// `journalctl -u lcm`. Genau die Zeilen, die eine Fehlersuche abkürzen
// („server unreachable", „queued job started", „degraded: … not writable"),
// lagen damit hinter einer SSH-Sitzung, die viele Betreiber gar nicht führen.
//
// Zugriff über settings:manage: Im Log stehen Namen, Adressen und Kommandos
// ALLER Server - auch derer, die ein Manager mit eingeschränktem Blick nicht
// sehen darf. Eine Einschränkung nach Sichtbarkeit wäre hier nicht ehrlich
// machbar, also bleibt die Ansicht dem Betreiber vorbehalten.
type LogsController struct {
	// path ist die rotierende Logdatei aus dem Datenverzeichnis. Leer, wenn
	// LCM ohne Dateiprotokoll läuft (dann gibt es hier nichts zu zeigen).
	path string
}

func NewLogsController(logFile string) *LogsController {
	return &LogsController{path: logFile}
}

// maxLines begrenzt, wie viele Zeilen eine Abfrage liefern darf. Mehr als das
// ist in einer Oberfläche nicht mehr zu lesen und macht die Antwort nur groß.
const maxLines = 2000

// defaultLines ist die Vorgabe ohne ausdrücklichen Wunsch.
const defaultLines = 300

// List - GET /api/v1/system/logs (settings:manage)
// Liefert das Ende der Logdatei, gefiltert nach Schwere und Text.
func (ctrl *LogsController) List(c fiber.Ctx) error {
	if ctrl.path == "" {
		return fiber.NewError(fiber.StatusNotFound, "kein Dateiprotokoll konfiguriert")
	}
	lines := defaultLines
	if v := c.Query("lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			lines = min(n, maxLines)
		}
	}
	entries, err := logging.Tail(ctrl.path, lines, c.Query("level"), c.Query("q"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "logdatei nicht lesbar: "+err.Error())
	}
	return c.JSON(fiber.Map{"entries": entries, "level": logging.Level()})
}

// Stream - GET /api/v1/system/logs/stream (settings:manage)
// Schickt neue Logzeilen als Server-Sent Events.
//
// SSE und nicht WebSocket: Der Strom geht nur in eine Richtung, läuft durch
// dieselbe Anmelde-Prüfung wie jede andere Anfrage, und der Browser baut ihn
// nach einem Abbruch von selbst wieder auf. Ein WebSocket wäre hier mehr
// Maschinerie für weniger.
func (ctrl *LogsController) Stream(c fiber.Ctx) error {
	if ctrl.path == "" {
		return fiber.NewError(fiber.StatusNotFound, "kein Dateiprotokoll konfiguriert")
	}
	level, query := c.Query("level"), c.Query("q")

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	// Kein Puffern durch einen vorgelagerten Reverse-Proxy - sonst käme der
	// Strom in Schüben an und die Ansicht wäre nicht „live".
	c.Set("X-Accel-Buffering", "no")

	c.SendStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithTimeout(context.Background(), streamMaxAge)
		defer cancel()

		send := func(event, data string) bool {
			if _, err := w.WriteString("event: " + event + "\ndata: " + data + "\n\n"); err != nil {
				return false
			}
			return w.Flush() == nil
		}

		// Ein Herzschlag hält die Verbindung durch Proxys hindurch offen und
		// beendet den Strom, sobald der Browser weg ist - dann scheitert das
		// Schreiben und die Goroutine endet, statt bis zum Zeitlimit zu leben.
		beat := time.NewTicker(streamHeartbeat)
		defer beat.Stop()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-beat.C:
					if !send("ping", "{}") {
						cancel()
						return
					}
				}
			}
		}()

		_ = logging.Follow(ctx, ctrl.path, func(e logging.Entry) {
			if !matches(e, level, query) {
				return
			}
			payload, err := json.Marshal(e)
			if err != nil {
				return
			}
			if !send("log", string(payload)) {
				cancel()
			}
		})
	})
	return nil
}

// Grenzen des Stroms: Ein Browser-Tab, den jemand offen vergisst, soll den
// Dienst nicht dauerhaft beschäftigen; der Browser baut die Verbindung nach
// dem Ende von selbst wieder auf.
const (
	streamMaxAge    = 30 * time.Minute
	streamHeartbeat = 20 * time.Second
)

// matches wendet dieselben Filter auf eine Live-Zeile an wie List auf den
// Verlauf - sonst zeigte die Ansicht nach dem Umschalten zweierlei.
func matches(e logging.Entry, level, query string) bool {
	if level != "" && e.Level != "" &&
		logging.ParseLevel(strings.ToLower(e.Level)) < logging.ParseLevel(strings.ToLower(level)) {
		return false
	}
	if query != "" && !strings.Contains(strings.ToLower(e.Raw), strings.ToLower(query)) {
		return false
	}
	return true
}

// debugWindow ist die Frist, die ein über die Oberfläche eingeschaltetes Debug
// bekommt. Lang genug, um einer Störung nachzugehen; kurz genug, dass niemand
// es dauerhaft anlässt (siehe logging.EnableDebugFor).
const debugWindow = 30 * time.Minute

// Level - GET /api/v1/system/logs/level (settings:manage)
func (ctrl *LogsController) Level(c fiber.Ctx) error {
	return c.JSON(levelResponse())
}

// SetLevel - POST /api/v1/system/logs/level (settings:manage)
// Schaltet das befristete Debug ein oder sofort wieder aus.
func (ctrl *LogsController) SetLevel(c fiber.Ctx) error {
	var req struct {
		Debug bool `json:"debug"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Debug {
		logging.EnableDebugFor(debugWindow)
	} else {
		logging.DisableDebug()
	}
	return c.JSON(levelResponse())
}

func levelResponse() fiber.Map {
	out := fiber.Map{"level": logging.Level(), "debug_until": nil}
	if until := logging.DebugUntil(); !until.IsZero() {
		out["debug_until"] = until
	}
	return out
}
