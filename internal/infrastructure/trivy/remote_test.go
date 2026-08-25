package trivy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// Der Sidecar-Weg muss dieselben Zusagen halten wie der lokale - und im
// Fehlerfall etwas sagen, mit dem der Betreiber arbeiten kann. Ein Sidecar,
// der schweigt oder als „nicht in Benutzung" durchgeht, waere die
// gefaehrlichste Variante: Die Oberflaeche zeigte dann keine Funde, und das
// saehe aus wie Entwarnung.

const testToken = "geheim-1234"

// fakeSidecar spielt cmd/trivyd. handler bekommt Pfad und Koerper.
func fakeSidecar(t *testing.T, handler func(path string, body []byte) (int, string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"ungültiges oder fehlendes Token"}`))
			return
		}
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		status, out := handler(r.URL.Path, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(out))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const leererBericht = `{"Results":[]}`

func TestSidecarScanReichtDasSbomDurch(t *testing.T) {
	var gesehen []byte
	srv := fakeSidecar(t, func(path string, body []byte) (int, string) {
		if path == PathInfo {
			return 200, `{"available":true,"version":"0.74.0","updated_at":"2026-08-20T06:00:00Z"}`
		}
		gesehen = body
		return 200, `{"Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-2026-1","PkgName":"openssl","InstalledVersion":"1.0","FixedVersion":"1.1","Severity":"HIGH"}]}]}`
	})

	scanner := NewRemote(srv.URL, testToken)
	vulns, err := scanner.Scan(context.Background(), Target{
		OSID: "debian", OSVersionID: "12", PackageManager: "apt",
		Packages: []domain.Package{{Name: "openssl", Version: "1.0"}},
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(vulns) != 1 || vulns[0].CVEID != "CVE-2026-1" {
		t.Fatalf("unerwartetes Ergebnis: %+v", vulns)
	}
	// Das SBOM geht direkt ueber die Leitung - ohne Umweg ueber eine Datei.
	if !strings.Contains(string(gesehen), "openssl") || !strings.Contains(string(gesehen), "bomFormat") {
		t.Errorf("der Sidecar bekam kein CycloneDX-SBOM zu sehen: %s", string(gesehen))
	}
}

// TestSidecarInfoMeldetDenContainerAlsGrenze: Im Sidecar sperrt kein
// bubblewrap ein - der Container selbst ist die Grenze, und er enthaelt weder
// LCMs Datenbank noch den Master-Key. Das muss die Oberflaeche genau so
// sagen: „ohne Sandbox" waere falsch, „bubblewrap" waere gelogen.
func TestSidecarInfoMeldetDenContainerAlsGrenze(t *testing.T) {
	srv := fakeSidecar(t, func(string, []byte) (int, string) {
		return 200, `{"available":true,"version":"0.74.0","updated_at":"2026-08-20T06:00:00Z"}`
	})

	st := NewRemote(srv.URL, testToken).Info(context.Background())
	if !st.Available || st.Version != "0.74.0" {
		t.Fatalf("Version/Verfuegbarkeit fehlen: %+v", st)
	}
	if st.UpdatedAt == nil || st.UpdatedAt.Year() != 2026 {
		t.Errorf("DB-Stand nicht uebernommen: %+v", st.UpdatedAt)
	}
	if !st.Sandboxed || st.SandboxBackend != SidecarBackend {
		t.Errorf("die Abschottung muss als %q gemeldet werden, bekam %q (sandboxed=%v)",
			SidecarBackend, st.SandboxBackend, st.Sandboxed)
	}
}

// TestSidecarTotIstEinFehlerKeinAus haelt die wichtigste Zusage fest.
// Available() heisst „konfiguriert", nicht „erreichbar": Waere ein toter
// Sidecar „nicht verfuegbar", liessen Ampel, Docker-Pruefung und der
// cve_db_stale-Alarm ihn stillschweigend durchgehen - ein Ausfall saehe
// exakt aus wie ein abgeschalteter CVE-Scan.
func TestSidecarTotIstEinFehlerKeinAus(t *testing.T) {
	// Adresse ohne Dienst dahinter.
	scanner := NewRemote("http://127.0.0.1:1", testToken)

	if !scanner.Available() {
		t.Fatal("ein konfigurierter Sidecar gilt als verfuegbar - sonst schweigt die Bewertung")
	}
	st := scanner.Info(context.Background())
	if st.Error == "" {
		t.Fatal("ein unerreichbarer Sidecar muss einen Fehler melden")
	}
	if !strings.Contains(st.Error, "nicht erreichbar") {
		t.Errorf("die Meldung soll die Ursache benennen: %s", st.Error)
	}
	if _, err := scanner.Scan(context.Background(), Target{OSID: "debian", OSVersionID: "12", PackageManager: "apt"}); err == nil {
		t.Error("ein Scan gegen einen toten Sidecar darf nicht als Erfolg durchgehen")
	}
}

// TestSidecarFalschesTokenErklaertSich: Das ist der haeufigste
// Einrichtungsfehler. Eine nackte 401 zwingt zum Raten.
func TestSidecarFalschesTokenErklaertSich(t *testing.T) {
	srv := fakeSidecar(t, func(string, []byte) (int, string) { return 200, leererBericht })

	scanner := NewRemote(srv.URL, "falsches-token")
	_, err := scanner.Scan(context.Background(), Target{OSID: "debian", OSVersionID: "12", PackageManager: "apt"})
	if err == nil {
		t.Fatal("mit falschem Token darf kein Ergebnis entstehen")
	}
	if !strings.Contains(err.Error(), "LCM_TRIVY_TOKEN") {
		t.Errorf("die Meldung soll sagen, WAS zu pruefen ist: %s", err)
	}
}

// TestSidecarUebersetztBekannteTrivyFehler: Die Uebersetzung von trivy-db#435
// hing frueher am stderr des lokalen Prozesses. Ueber den Sidecar kommt
// derselbe Text als Fehlermeldung - sie muss auch dort greifen, sonst steht
// beim Betreiber wieder ein roher Stacktrace.
func TestSidecarUebersetztBekannteTrivyFehler(t *testing.T) {
	srv := fakeSidecar(t, func(path string, _ []byte) (int, string) {
		if path == PathInfo {
			return 200, `{"available":true,"version":"0.74.0","updated_at":"2026-08-20T06:00:00Z"}`
		}
		return http.StatusBadGateway,
			`{"error":"exit status 1: FATAL failed to get Red Hat advisories: unable to find CPE indices"}`
	})

	_, err := NewRemote(srv.URL, testToken).Scan(context.Background(),
		Target{OSID: "rocky", OSVersionID: "9.3", PackageManager: "dnf"})
	if err == nil {
		t.Fatal("erwartete einen Fehler")
	}
	if !strings.Contains(err.Error(), "trivy-db#435") {
		t.Errorf("bekanntes Fehlerbild wurde nicht uebersetzt: %s", err)
	}
}

func TestSidecarUpdateDBLiefertDieAusgabe(t *testing.T) {
	srv := fakeSidecar(t, func(path string, _ []byte) (int, string) {
		if path == PathUpdateDB {
			return 200, `{"output":"Downloading DB... 100%"}`
		}
		return 200, `{"available":true}`
	})

	out, err := NewRemote(srv.URL, testToken).UpdateDB(context.Background())
	if err != nil {
		t.Fatalf("UpdateDB: %v", err)
	}
	if !strings.Contains(out, "Downloading DB") {
		t.Errorf("die Ausgabe gehoert ins Job-Protokoll, bekam %q", out)
	}
}

// TestSidecarZeitpunkteRundLaufen: Die Zeitpunkte gehen als RFC-3339 ueber die
// Leitung. Ein stiller Parse-Fehler wuerde „nie geladen" ergeben - also den
// schlimmsten Zustand vortaeuschen.
func TestSidecarZeitpunkteRundLaufen(t *testing.T) {
	wann := time.Date(2026, 8, 20, 6, 0, 0, 0, time.UTC)
	s := wann.Format(time.RFC3339)
	got := infoFromSidecar(InfoResponse{Available: true, UpdatedAt: &s})
	if got.UpdatedAt == nil || !got.UpdatedAt.Equal(wann) {
		t.Errorf("Zeitpunkt ging verloren: %+v", got.UpdatedAt)
	}
	// Unlesbares ergibt nil und nicht die Epoche - sonst waere die Datenbank
	// scheinbar von 1970 und die Ampel dauerhaft rot.
	kaputt := "gestern"
	if got := infoFromSidecar(InfoResponse{UpdatedAt: &kaputt}); got.UpdatedAt != nil {
		t.Errorf("unlesbarer Zeitpunkt muss nil ergeben, bekam %v", got.UpdatedAt)
	}
}

// TestSidecarAntwortIstBegrenzt: Ohne Grenze zoege eine defekte Gegenstelle
// LCM den Speicher leer.
func TestSidecarAntwortIstBegrenzt(t *testing.T) {
	if maxSidecarResponse <= 0 || maxSidecarResponse > 128<<20 {
		t.Errorf("unplausible Antwortgrenze: %d", maxSidecarResponse)
	}
	var probe InfoResponse
	if err := json.Unmarshal([]byte(`{"available":true}`), &probe); err != nil {
		t.Fatal(err)
	}
}
