// Package apierr beschreibt Fehlerantworten der API mit festem Code.
//
// Jede Antwort hat die Form
//
//		{"error": "invalid request body", "code": "request.invalid_body", "params": {…}}
//
//	  - error: englischer Text für API-Clients, Skripte und MCP.
//	  - code: stabiler Schlüssel, den die Oberfläche in die gewählte Sprache
//	    übersetzt (Schlüssel errors.<code> in ui/src/locales/*.json).
//	  - params: benannte Werte für die Platzhalter der Übersetzung.
//
// Fehler werden als Paketvariablen mit Define angelegt. So kennt das Paket
// alle Codes (Codes), und ein Test prüft, dass jeder Code übersetzt ist.
package apierr

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/gofiber/fiber/v3"
)

// Error ist ein API-Fehler. Er wird von Handlern zurückgegeben wie ein
// fiber.Error; der zentrale ErrorHandler schreibt ihn als JSON.
type Error struct {
	Status  int
	Code    string
	Message string
	Params  map[string]any
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// With liefert eine Kopie mit einem zusätzlichen Parameter. Die definierte
// Variable selbst bleibt unverändert.
func (e *Error) With(key string, value any) *Error {
	out := *e
	out.Params = make(map[string]any, len(e.Params)+1)
	maps.Copy(out.Params, e.Params)
	out.Params[key] = value
	return &out
}

var (
	registryMu sync.Mutex
	registry   = map[string]*Error{}
)

// Define legt einen Fehler an und meldet seinen Code an. Ein Code darf nur
// einmal vergeben werden; doppelte Codes sind ein Programmierfehler.
func Define(status int, code, message string) *Error {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[code]; dup {
		panic("apierr: code defined twice: " + code)
	}
	e := &Error{Status: status, Code: code, Message: message}
	registry[code] = e
	return e
}

// Codes liefert alle angemeldeten Codes sortiert.
func Codes() []string {
	registryMu.Lock()
	defer registryMu.Unlock()
	return slices.Sorted(maps.Keys(registry))
}

// Allgemeine Fehler, die nicht zu einem Fachbereich gehören.
var (
	Internal        = Define(500, "internal", "internal server error")
	InvalidBody     = Define(400, "request.invalid_body", "invalid request body")
	InvalidID       = Define(400, "request.invalid_id", "invalid id")
	OutOfRange      = Define(422, "validation.out_of_range", "number out of range")
	NotFound        = Define(404, "not_found", "not found")
	UnknownEndpoint = Define(404, "request.unknown_endpoint", "unknown API endpoint")
	Unauthorized    = Define(401, "auth.required", "authentication required")
	Forbidden       = Define(403, "auth.forbidden", "missing permission")
	TooManyRequests = Define(429, "request.rate_limited", "too many requests")
	// Maintenance: LCM aktualisiert sich gerade selbst; Aktionen sind bis zum
	// Neustart gesperrt (siehe middlewares.MaintenanceGuard).
	Maintenance = Define(423, "system.maintenance", "LCM is being updated, actions are locked until it has restarted")
)

// StatusOf liefert den HTTP-Status, den der zentrale ErrorHandler für err
// schreiben wird: den eines API-Fehlers oder eines rohen fiber-Fehlers, sonst
// 500. Für Protokolle, die den Status vor dem ErrorHandler kennen müssen.
func StatusOf(err error) int {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Status
	}
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return fe.Code
	}
	return Internal.Status
}

// FromStatus ordnet einem rohen HTTP-Status (etwa aus fiber selbst, wenn ein
// Rumpf zu groß ist) einen Code zu.
func FromStatus(status int, message string) *Error {
	code := fmt.Sprintf("http.%d", status)
	return &Error{Status: status, Code: code, Message: message}
}
