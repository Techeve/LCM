package advisories

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEUVDLiestDenKevDump prüft die Übersetzung des Dumps in eine Menge von
// CVE-Kennungen - und dass der eigene User-Agent mitgeht: Ohne ihn antwortet
// das Gateway vor der API mit 403.
func TestEUVDLiestDenKevDump(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/kev/dump" {
			t.Errorf("unerwarteter Pfad %q", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "LCM/") {
			t.Errorf("eigener User-Agent fehlt: %q", r.Header.Get("User-Agent"))
		}
		io.WriteString(w, `[
		  {"cveId":"CVE-2021-22555","euvdId":"EUVD-2021-9696","dateAdded":"2025-10-06"},
		  {"cveId":"cve-2026-1234","euvdId":"EUVD-2026-1","dateAdded":"2026-08-01"}
		]`)
	}))
	defer srv.Close()

	got, err := NewEUVD(srv.URL).ExploitedCVEs(context.Background())
	if err != nil {
		t.Fatalf("ExploitedCVEs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("erwartet 2 Kennungen, bekam %d", len(got))
	}
	if !got["CVE-2021-22555"] {
		t.Error("bekannte Kennung fehlt")
	}
	// Kleinschreibung in der Quelle darf den Abgleich nicht sprengen.
	if !got["CVE-2026-1234"] {
		t.Error("Kennung wurde nicht normalisiert")
	}
}

// TestEUVDLehntLeerenDumpAb ist der wichtigste Test dieser Datei: Ein leerer
// Dump ist kein „nichts wird ausgenutzt", sondern ein Ausfall der
// Gegenstelle. Durchgereicht würde er reihenweise Ausnutzungs-Markierungen
// zurücknehmen - die Lage sähe schlagartig harmloser aus, als sie ist.
func TestEUVDLehntLeerenDumpAb(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[]`)
	}))
	defer srv.Close()

	if _, err := NewEUVD(srv.URL).ExploitedCVEs(context.Background()); err == nil {
		t.Fatal("leerer Dump muss einen Fehler ergeben")
	}
}

// TestEUVDMeldetFehlerstatus: Ein 403 (die bekannte Gateway-Falle) darf nicht
// als leere Liste durchgehen.
func TestEUVDMeldetFehlerstatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := NewEUVD(srv.URL).ExploitedCVEs(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("erwartet Fehler mit Statuscode, bekam %v", err)
	}
}
