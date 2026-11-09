package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"LCM/internal/config"
	"LCM/internal/infrastructure/tlsx"
)

// healthcheckTimeout: Ein Dienst, der binnen fuenf Sekunden nicht auf seinen
// eigenen Loopback antwortet, ist fuer den Zweck dieser Pruefung nicht
// gesund. Laenger zu warten verzoegert nur die Erkennung.
const healthcheckTimeout = 5 * time.Second

// runHealthcheck fragt den eigenen Health-Endpunkt ab. Rueckgabe nil = gesund.
//
// Warum das ins Binary gehoert und nicht in die Container-Zeile: Das
// Runtime-Image ist ein Scratch-Image - es enthaelt keine Shell, kein wget
// und kein curl. Vor allem aber war die bisherige Zeile
// (`wget http://127.0.0.1:9310/...`) schlicht falsch: LCM spricht
// standardmaessig HTTPS mit Self-Signed-Zertifikat, HTTP gibt es nur mit
// --dev. Jeder Container nach unserer eigenen Anleitung galt damit dauerhaft
// als „unhealthy", ohne dass irgendetwas kaputt war.
func runHealthcheck(configPath, dataDir string) error {
	dir, err := resolveDataDir(dataDir)
	if err != nil {
		return err
	}
	if configPath == "" {
		configPath = filepath.Join(dir, config.DefaultFileName)
	}
	cfg, err := config.LoadFrom(configPath)
	if err != nil {
		return fmt.Errorf("konfiguration lesen: %w", err)
	}

	// Immer ueber den Loopback: Die Bind-Adresse ist im Container 0.0.0.0,
	// und die ist keine Zieladresse.
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)

	// Ob der laufende Dienst TLS spricht, haengt an seinem --dev-Flag - das
	// sieht dieser Prozess nicht. Deshalb erst HTTPS (der Regelfall) und nur
	// bei einem TLS-Protokollfehler HTTP. Kein blindes Durchprobieren: Ein
	// echter Fehler (Verbindung abgelehnt, Zeitueberschreitung, HTTP 500)
	// wird gemeldet, statt in einem zweiten Versuch unterzugehen.
	err = probeHealth("https://"+addr, tlsx.PinnedTo(dir))
	if err != nil && tlsx.IsNotTLS(err) {
		return probeHealth("http://"+addr, nil)
	}
	return err
}

// probeHealth holt den Health-Endpunkt und wertet den Status aus.
func probeHealth(base string, tlsCfg *tls.Config) error {
	client := &http.Client{
		Timeout:   healthcheckTimeout,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	resp, err := client.Get(base + "/api/v1/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s meldet HTTP %d", base, resp.StatusCode)
	}
	return nil
}
