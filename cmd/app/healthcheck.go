package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
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
	err = probeHealth("https://"+addr, tlsPinnedTo(dir))
	if err != nil && isNotTLS(err) {
		return probeHealth("http://"+addr, nil)
	}
	return err
}

// isNotTLS erkennt den einen Fall, der einen zweiten Versuch rechtfertigt:
// Die Gegenstelle spricht kein TLS (Dienst laeuft mit --dev).
//
// Zwei Wege, weil die Antwort an zwei Stellen auffliegt: Erkennt der
// HTTP-Client die Klartext-Antwort selbst, meldet er ErrSchemeMismatch;
// scheitert schon der Handshake, kommt ein tls.RecordHeaderError.
func isNotTLS(err error) bool {
	var re tls.RecordHeaderError
	return errors.Is(err, http.ErrSchemeMismatch) || errors.As(err, &re)
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

// tlsPinnedTo bindet die Pruefung an genau das Zertifikat, das der Dienst
// ausliefert (Datei im Datenverzeichnis).
//
// Warum gepinnt statt ueber die Namen im Zertifikat geprueft: Das
// mitgelieferte Self-Signed-Zertifikat deckt 127.0.0.1 ab, ein vom Betreiber
// hinterlegtes eigenes Zertifikat aber typischerweise nur seine Domain. Eine
// Namenspruefung wuerde dann fehlschlagen, obwohl der Dienst laeuft - der
// Container startete in einer Schleife neu. Gleichzeitig ist „Pruefung aus"
// keine Antwort: Wir vergleichen, ob die Gegenstelle exakt das Zertifikat
// vorzeigt, das hier auf der Platte liegt. Findet sich keins, bleibt nur die
// Erreichbarkeit - das ist ehrlicher als ein erfundener Vertrauensanker.
func tlsPinnedTo(dataDir string) *tls.Config {
	pem, err := os.ReadFile(filepath.Join(dataDir, tlsx.CertFileName))
	if err != nil {
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // siehe Kommentar: nur Loopback, kein Anker vorhanden
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // unlesbares Zertifikat, siehe oben
	}
	return &tls.Config{
		// Die Namenspruefung uebernimmt VerifyPeerCertificate; ohne das
		// scheitert schon der Handshake an der Adresse 127.0.0.1.
		InsecureSkipVerify: true, //nolint:gosec // ersetzt durch die Pinnung unten
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("die Gegenstelle zeigt kein Zertifikat vor")
			}
			cert, err := x509.ParseCertificate(raw[0])
			if err != nil {
				return err
			}
			if _, err := cert.Verify(x509.VerifyOptions{Roots: pool}); err != nil {
				return fmt.Errorf("das vorgezeigte Zertifikat ist nicht das aus dem Datenverzeichnis: %w", err)
			}
			return nil
		},
	}
}
