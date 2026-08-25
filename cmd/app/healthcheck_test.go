package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/infrastructure/tlsx"
)

// Die Pruefung ersetzt eine Zeile, die nie funktioniert hat: Beide Dockerfiles
// riefen `wget http://127.0.0.1:9310/api/v1/health` - LCM spricht aber
// standardmaessig HTTPS. Jeder Container nach unserer Anleitung galt dauerhaft
// als „unhealthy". Diese Tests halten fest, dass die neue Pruefung TLS kann,
// den Ausweichweg fuer --dev kennt und einen kranken Dienst auch als solchen
// meldet.

// testUmgebung legt ein Datenverzeichnis mit Zertifikat und config.json an und
// liefert den freien Port, auf dem der Test-Dienst lauschen soll.
func testUmgebung(t *testing.T) (dataDir string, port int) {
	t.Helper()
	dataDir = t.TempDir()
	if _, _, err := tlsx.EnsureSelfSigned(dataDir, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("zertifikat erzeugen: %v", err)
	}
	// Freien Port ermitteln und sofort wieder freigeben - der Test-Dienst
	// belegt ihn gleich selbst.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port = l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"host": "127.0.0.1", "port": port}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dataDir, port
}

// starteDienst startet einen Test-Dienst auf dem Port und liefert die
// Abschaltfunktion. certDir bestimmt, WELCHES Zertifikat ausgeliefert wird -
// bei "" laeuft der Dienst unverschluesselt (wie LCM mit --dev).
func startService(t *testing.T, port int, certDir string, status int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: mux}
	t.Cleanup(func() { _ = srv.Close() })

	go func() {
		if certDir == "" {
			_ = srv.ListenAndServe()
			return
		}
		_ = srv.ListenAndServeTLS(
			filepath.Join(certDir, tlsx.CertFileName),
			filepath.Join(certDir, tlsx.KeyFileName),
		)
	}()
	waitForPort(t, port)
}

func waitForPort(t *testing.T, port int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dienst auf port %d ist nicht hochgekommen", port)
}

func TestHealthcheckMitTLS(t *testing.T) {
	dataDir, port := testUmgebung(t)
	startService(t, port, dataDir, http.StatusOK)

	if err := runHealthcheck("", dataDir); err != nil {
		t.Errorf("gesunder Dienst ueber HTTPS muss als gesund gelten, bekam: %v", err)
	}
}

// TestHealthcheckOhneTLS: Mit --dev laeuft LCM unverschluesselt. Der Prozess,
// der die Pruefung ausfuehrt, sieht dieses Flag nicht - also muss er den Fall
// erkennen, statt einen Dienst faelschlich fuer tot zu erklaeren.
func TestHealthcheckOhneTLS(t *testing.T) {
	dataDir, port := testUmgebung(t)
	startService(t, port, "", http.StatusOK)

	if err := runHealthcheck("", dataDir); err != nil {
		t.Errorf("gesunder Dienst ueber HTTP muss als gesund gelten, bekam: %v", err)
	}
}

// TestHealthcheckMeldetKrankenDienst: Der Health-Endpunkt antwortet mit 503,
// wenn die Selbstueberwachung anschlaegt (z. B. Datenbank unerreichbar). Ein
// Healthcheck, der nur auf „antwortet ueberhaupt" prueft, wuerde genau den
// Fall verschlafen, fuer den es ihn gibt.
func TestHealthcheckMeldetKrankenDienst(t *testing.T) {
	dataDir, port := testUmgebung(t)
	startService(t, port, dataDir, http.StatusServiceUnavailable)

	err := runHealthcheck("", dataDir)
	if err == nil {
		t.Fatal("HTTP 503 muss als nicht gesund gelten")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("die Meldung soll den Status nennen, bekam: %v", err)
	}
}

func TestHealthcheckOhneDienst(t *testing.T) {
	dataDir, _ := testUmgebung(t)
	if err := runHealthcheck("", dataDir); err == nil {
		t.Error("ohne laufenden Dienst darf die Pruefung nicht gruen sein")
	}
}

// TestHealthcheckPinntAufDasEigeneZertifikat haelt fest, dass die Pruefung
// nicht blind jedes TLS akzeptiert. Der Dienst zeigt hier ein FREMDES
// Zertifikat vor - dieselbe Adresse, dieselbe Antwort, nur eben nicht unser
// Dienst. Ohne die Pinnung waere das gruen.
func TestHealthcheckPinntAufDasEigeneZertifikat(t *testing.T) {
	dataDir, port := testUmgebung(t)
	fremd := t.TempDir()
	if _, _, err := tlsx.EnsureSelfSigned(fremd, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	startService(t, port, fremd, http.StatusOK)

	if err := runHealthcheck("", dataDir); err == nil {
		t.Error("ein fremdes Zertifikat darf die Pruefung nicht bestehen")
	}
}

// TestHealthcheckOhneZertifikatBleibtBenutzbar: Liegt im Datenverzeichnis kein
// Zertifikat (etwa weil der Dienst noch nie gestartet ist oder es woanders
// verwaltet wird), faellt die Pruefung auf reine Erreichbarkeit zurueck statt
// einen Anker zu erfinden.
func TestHealthcheckOhneZertifikatBleibtBenutzbar(t *testing.T) {
	dataDir, port := testUmgebung(t)
	certDir := t.TempDir()
	if _, _, err := tlsx.EnsureSelfSigned(certDir, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	startService(t, port, certDir, http.StatusOK)
	if err := os.Remove(filepath.Join(dataDir, tlsx.CertFileName)); err != nil {
		t.Fatal(err)
	}

	if err := runHealthcheck("", dataDir); err != nil {
		t.Errorf("ohne Anker soll die Erreichbarkeit zaehlen, bekam: %v", err)
	}
}

// TestIsNotTLSTrenntDieFaelle: Der Ausweich auf HTTP darf NUR greifen, wenn
// die Gegenstelle kein TLS spricht. Griffe er auch bei anderen Fehlern, wuerde
// ein echtes Problem in einem zweiten Versuch untergehen.
func TestIsNotTLSTrenntDieFaelle(t *testing.T) {
	if !isNotTLS(tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}) {
		t.Error("ein Klartext-Dienst muss als „kein TLS\" erkannt werden")
	}
	if isNotTLS(fmt.Errorf("connect: connection refused")) {
		t.Error("eine abgelehnte Verbindung ist kein TLS-Problem - kein zweiter Versuch")
	}
}
