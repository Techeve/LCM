package main

import (
	"fmt"
	"log/slog"

	"LCM/internal/control"
	"LCM/internal/core/services"
	"LCM/internal/netfilter"
)

// Befehle der Steuerbuchse für die Netzbereiche.
const (
	cmdNetworkStatus = "network.status"
	cmdNetworkReset  = "network.reset"

	// resetByCLI steht als Actor im Audit-Log.
	resetByCLI = "cli"
)

// networkCommands sind die Befehle für die Netzbereiche: `status` zeigt je
// Endpunkt die geltende Liste, `reset` leert die gespeicherte Liste eines
// Endpunkts (der Rettungsweg nach einer Aussperrung).
func networkCommands(network *services.NetworkAccessService) map[string]control.Handler {
	return map[string]control.Handler{
		cmdNetworkStatus: func(string) (any, error) { return network.View() },
		cmdNetworkReset: func(arg string) (any, error) {
			endpoint, ok := netfilter.ParseEndpoint(arg)
			if !ok {
				return nil, fmt.Errorf("unbekannter endpunkt %q - erlaubt sind ui, agent und mcp", arg)
			}
			return nil, network.Reset(endpoint, resetByCLI)
		},
	}
}

// logActiveNetworkLists meldet beim Start, welche Endpunkte eingeschränkt sind.
func logActiveNetworkLists(network *services.NetworkAccessService) {
	views, err := network.View()
	if err != nil {
		slog.Warn("network lists not readable", "error", err)
		return
	}
	for _, v := range views {
		if len(v.Allowed) > 0 {
			slog.Info("access restriction active", "endpoint", v.Endpoint, "entries", len(v.Allowed))
		}
	}
}
