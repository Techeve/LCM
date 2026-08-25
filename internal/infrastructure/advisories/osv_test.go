package advisories

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestOSVQueryOrdnetErgebnisseDenPurlsZu: Die Antwort von querybatch ist
// positionsgleich zur Anfrage - dieser Test hält fest, dass die Zuordnung
// darüber läuft und nicht über irgendeine Kennung im Ergebnis.
func TestOSVQueryOrdnetErgebnisseDenPurlsZu(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/querybatch" {
			t.Errorf("unerwarteter Pfad %q", r.URL.Path)
		}
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "LCM/") {
			t.Errorf("eigener User-Agent fehlt: %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req osvBatchRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("Anfrage nicht lesbar: %v", err)
		}
		if len(req.Queries) != 3 {
			t.Fatalf("erwartet 3 Anfragen, bekam %d", len(req.Queries))
		}
		// Zweiter purl sauber, erster und dritter betroffen.
		io.WriteString(w, `{"results":[
		  {"vulns":[{"id":"CVE-2026-1","modified":"2026-08-18T06:00:00Z"}]},
		  {},
		  {"vulns":[{"id":"MAL-2026-9","modified":"2026-08-18T07:00:00Z"}]}
		]}`)
	}))
	defer srv.Close()

	purls := []string{"pkg:deb/debian/openssl@3.0.11", "pkg:deb/debian/bash@5.2", "pkg:npm/left-pad@1.0.0"}
	got, err := NewOSV(srv.URL).Query(context.Background(), purls)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("erwartet 2 betroffene purls, bekam %d", len(got))
	}
	if got[purls[0]][0].ID != "CVE-2026-1" {
		t.Errorf("erster purl falsch zugeordnet: %+v", got[purls[0]])
	}
	if _, ok := got[purls[1]]; ok {
		t.Error("sauberer purl darf keinen Eintrag haben")
	}
	if got[purls[2]][0].ID != "MAL-2026-9" {
		t.Errorf("dritter purl falsch zugeordnet: %+v", got[purls[2]])
	}
}

// TestQueryPurlUebersetztDebian hält den Fund fest, der ohne Prüfung gegen
// den echten Dienst unentdeckt geblieben wäre: Mit "?distro=debian-12"
// antwortet api.osv.dev mit NULL Treffern - nicht mit einem Fehler, sondern
// mit einem sauberen Ergebnis. Erwartet wird dort der Codename.
func TestQueryPurlUebersetztDebian(t *testing.T) {
	cases := map[string]string{
		// Debian: Version → Codename.
		"pkg:deb/debian/openssl@3.0.11-1~deb12u2?distro=debian-12": "pkg:deb/debian/openssl@3.0.11-1~deb12u2?distro=bookworm",
		"pkg:deb/debian/bash@5.2-15?distro=debian-11":              "pkg:deb/debian/bash@5.2-15?distro=bullseye",
		// Unbekannte Debian-Version: lieber ohne Qualifier fragen (und
		// womöglich zu viel melden) als still nichts zu finden.
		"pkg:deb/debian/bash@5.2-15?distro=debian-99": "pkg:deb/debian/bash@5.2-15",
		// Andere Distributionen: Qualifier weg, OSV wertet ihn nicht aus.
		"pkg:deb/ubuntu/openssl@3.0.2-0ubuntu1.10?distro=ubuntu-22.04": "pkg:deb/ubuntu/openssl@3.0.2-0ubuntu1.10",
		"pkg:rpm/rocky/openssl@3.0.7-24.el9?distro=rocky-9":            "pkg:rpm/rocky/openssl@3.0.7-24.el9",
		// Ohne Qualifier bleibt alles, wie es ist.
		"pkg:npm/left-pad@1.0.0": "pkg:npm/left-pad@1.0.0",
	}
	for in, want := range cases {
		if got := queryPurl(in); got != want {
			t.Errorf("queryPurl(%q)\n  = %q\n  erwartet %q", in, got, want)
		}
	}
}

// TestOSVQueryMeldetVersetzteAntwort: Kommen weniger Ergebnisse zurück als
// angefragt, wäre jede Zuordnung ab der Lücke falsch - und zwar unbemerkt.
// Deshalb ist das ein Fehler und keine Kürzung.
func TestOSVQueryMeldetVersetzteAntwort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"results":[{}]}`)
	}))
	defer srv.Close()

	_, err := NewOSV(srv.URL).Query(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("versetzte Antwort muss einen Fehler ergeben")
	}
	if !strings.Contains(err.Error(), "antworten auf") {
		t.Errorf("Fehlertext benennt die Ursache nicht: %v", err)
	}
}

// TestOSVDetailsLiestSchwereUndFix prüft die Übersetzung eines
// OSV-Datensatzes: Schwere normalisiert, behebende Version je Paket.
func TestOSVDetailsLiestSchwereUndFix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/vulns/CVE-2026-1" {
			t.Errorf("unerwarteter Pfad %q", r.URL.Path)
		}
		io.WriteString(w, `{
		  "id":"CVE-2026-1","summary":"Pufferueberlauf in openssl",
		  "modified":"2026-08-18T06:00:00Z",
		  "database_specific":{"severity":"CRITICAL"},
		  "affected":[{"package":{"name":"openssl"},
		    "ranges":[{"events":[{"introduced":"0"},{"fixed":"3.0.14-1"}]}]}]
		}`)
	}))
	defer srv.Close()

	got, err := NewOSV(srv.URL).Details(context.Background(), []string{"CVE-2026-1"})
	if err != nil {
		t.Fatalf("Details: %v", err)
	}
	d := got["CVE-2026-1"]
	if d.Severity != domain.SeverityCritical {
		t.Errorf("Schwere falsch: %q", d.Severity)
	}
	if d.FixedVersions["openssl"] != "3.0.14-1" {
		t.Errorf("behebende Version falsch: %+v", d.FixedVersions)
	}
	if d.Title == "" || d.URL == "" {
		t.Errorf("Titel/URL fehlen: %+v", d)
	}
}

// TestOSVDetailsUeberspringtFehler: Eine nicht abrufbare Beschreibung darf
// den Durchgang nicht abbrechen - der Befund steht ja bereits fest, es fehlt
// nur sein Titel.
func TestOSVDetailsUeberspringtFehler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "kaputt") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, `{"id":"CVE-2026-2","summary":"ok","modified":"2026-08-18T06:00:00Z"}`)
	}))
	defer srv.Close()

	got, err := NewOSV(srv.URL).Details(context.Background(), []string{"kaputt", "CVE-2026-2"})
	if err != nil {
		t.Fatalf("Details darf nicht scheitern: %v", err)
	}
	if _, ok := got["kaputt"]; ok {
		t.Error("fehlgeschlagene Kennung darf kein Ergebnis liefern")
	}
	if got["CVE-2026-2"].Title != "ok" {
		t.Error("die abrufbare Beschreibung fehlt")
	}
}

// TestOSVQueryTeiltGrosseMengenAuf belegt, dass mehr als osvBatchSize purls
// über mehrere Aufrufe gehen - die API nimmt nicht mehr auf einmal an.
func TestOSVQueryTeiltGrosseMengenAuf(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		var req osvBatchRequest
		json.Unmarshal(body, &req)
		if len(req.Queries) > osvBatchSize {
			t.Errorf("Block zu groß: %d", len(req.Queries))
		}
		w.Write([]byte(`{"results":[` + strings.TrimSuffix(strings.Repeat("{},", len(req.Queries)), ",") + `]}`))
	}))
	defer srv.Close()

	purls := make([]string, osvBatchSize+5)
	for i := range purls {
		purls[i] = "pkg:deb/debian/p" + string(rune('a'+i%26))
	}
	if _, err := NewOSV(srv.URL).Query(context.Background(), purls); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if calls != 2 {
		t.Errorf("erwartet 2 Aufrufe, waren %d", calls)
	}
}
