package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"LCM/internal/infrastructure/trivy"
)

// probeSelf ist der Healthcheck des Sidecars - aus demselben Grund im Binary
// wie beim Hauptdienst: Das Image bringt kein wget und kein curl mit, und ein
// Healthcheck, der nicht laufen kann, meldet dauerhaft „unhealthy".
//
// Bewusst OHNE Token: Diese Prüfung sagt „der Prozess lebt und antwortet",
// mehr nicht. Sie hätte das Token sonst im Klartext in der Container-Zeile
// stehen - sichtbar in jedem `docker inspect`.
func probeSelf(listen string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, "http://"+loopbackAddr(listen)+trivy.PathHealth, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz meldet HTTP %d", resp.StatusCode)
	}
	return nil
}

// loopbackAddr macht aus der Lausch-Adresse eine Zieladresse. „:9330" und
// „0.0.0.0:9330" sind zum Lauschen gedacht, nicht zum Verbinden.
func loopbackAddr(listen string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return "127.0.0.1:9330"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// contextWithTimeout bindet den Trivy-Lauf an die Anfrage UND an ein
// Zeitlimit. Bricht LCM ab (Nutzer schliesst die Seite, Job wird gestoppt),
// endet auch der Prozess hier - sonst liefen verwaiste Scans weiter und
// hielten die Serialisierung besetzt.
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
