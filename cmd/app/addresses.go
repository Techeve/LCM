package main

import (
	"net"
	"os"
	"strconv"

	"LCM/internal/config"
	"LCM/internal/core/services"
)

// addressConfig sagt den Einstellungen, was die Umgebung festlegt (siehe
// services.AddressConfig): LCM_PORT und LCM_AGENT_PORT, auch mit dem Wert der
// Vorgabe, denn LCM kann eine Umgebungsvariable nicht ändern. Alles andere
// steht in den Einstellungen; die config.json spiegelt sie nur (config_sync.go)
// und sperrt kein Feld.
func addressConfig(cfg *config.Config, dev bool) services.AddressConfig {
	scheme := "https"
	if dev {
		scheme = "http"
	}
	return services.AddressConfig{
		UI:           envPort(cfg.Port, "LCM_PORT"),
		Agent:        envPort(cfg.AgentPort, "LCM_AGENT_PORT"),
		ACMEHTTPPort: listenPort(cfg.ACMEHTTPListen),
		Scheme:       scheme,
		FallbackHost: fallbackHost(cfg.Host),
	}
}

// envPort legt den Port fest, wenn die Umgebungsvariable gesetzt ist; value ist
// der Wert, den cfg danach hat (die Umgebung hat ihn bereits übernommen).
func envPort(value int, envName string) services.FixedPort {
	if _, fromEnv := os.LookupEnv(envName); fromEnv {
		return services.FixedPort{Set: true, Value: value}
	}
	return services.FixedPort{}
}

// listenPort ist der Port einer Listen-Adresse wie ":443"; 0, wenn sie keinen hat.
func listenPort(addr string) int {
	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portText)
	return port
}

// fallbackHost ist der Name der Adressen, solange keine öffentliche gesetzt
// ist: der Bind-Host der Oberfläche.
//
// BEWUSST NICHT aus dem Request: der Host-Header ist frei fälschbar, und ein
// Angreifer könnte damit einen echten Passwort-Reset-Link mit gültigem Token
// auf seine eigene Domain ausstellen lassen (Kontoübernahme per Klick).
// Bindet LCM auf eine Wildcard-Adresse, ist von außen kein Name bekannt -
// dann wird localhost verwendet: der Link ist dann ggf. nicht aus der Ferne
// nutzbar, aber niemals irreführend. Der Betreiber setzt public_base_url.
func fallbackHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return "localhost"
	}
	return host
}
