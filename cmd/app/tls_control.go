package main

import (
	"errors"
	"log/slog"
	"maps"

	"LCM/internal/control"
	"LCM/internal/core/services"
)

// Befehle der Steuerbuchse für die Zertifikate.
const (
	cmdTLSReload = "tls.reload"
	cmdTLSStatus = "tls.status"
)

var errTLSDisabled = errors.New("im Entwicklungsmodus (--dev) läuft LCM ohne TLS - es gibt keine Zertifikate")

// controlCommands ist die Befehlstabelle der Steuerbuchse. Ohne reloader
// (Entwicklungsmodus, kein Pool) antworten die tls-Befehle mit einer klaren
// Meldung.
func controlCommands(reloader *certReloader) map[string]control.Handler {
	if reloader == nil {
		disabled := func(string) (any, error) { return nil, errTLSDisabled }
		return map[string]control.Handler{cmdTLSReload: disabled, cmdTLSStatus: disabled}
	}
	return map[string]control.Handler{
		cmdTLSReload: func(string) (any, error) { return reloader.Reload(reloadByCLI), nil },
		cmdTLSStatus: func(string) (any, error) { return reloader.Status(), nil },
	}
}

// startControlSocket öffnet die Steuerbuchse im Datenverzeichnis. Scheitert
// das (etwa weil schon eine Instanz läuft), läuft LCM ohne sie weiter. Der
// Aufrufer schließt die Buchse beim Beenden; sie ist nie nil.
func startControlSocket(dataDir string, reloader *certReloader, network *services.NetworkAccessService) (closeFn func()) {
	path := control.SocketPath(dataDir)
	commands := controlCommands(reloader)
	maps.Copy(commands, networkCommands(network))
	server, err := control.Listen(path, commands)
	if err != nil {
		slog.Error("control socket not available - `lcm tls` and `lcm network` commands will not work", "error", err)
		return func() {}
	}
	slog.Info("control socket listening", "path", path)
	return server.Close
}
