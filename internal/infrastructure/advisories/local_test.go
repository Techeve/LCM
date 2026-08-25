package advisories

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// dumpServer liefert ein Zip-Archiv mit den gegebenen OSV-Datensätzen unter
// dem Pfad, den auch der echte Bucket verwendet.
func dumpServer(t *testing.T, ecosystem string, records map[string]string) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range records {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+ecosystem+"/all.zip" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
}

const opensslRecord = `{
  "id":"CVE-2026-1","summary":"Loch in openssl","modified":"2026-08-18T06:00:00Z",
  "aliases":["DSA-9999-1"],
  "database_specific":{"severity":"CRITICAL"},
  "affected":[
    {"package":{"name":"openssl","ecosystem":"Debian:12"},
     "ranges":[{"type":"ECOSYSTEM","events":[{"introduced":"0"},{"fixed":"3.0.14-1"}]}]},
    {"package":{"name":"openssl","ecosystem":"Debian:11"},
     "ranges":[{"type":"ECOSYSTEM","events":[{"introduced":"0"},{"fixed":"1.1.1w-1"}]}]}
  ]}`

const offenerRecord = `{
  "id":"CVE-2026-2","summary":"Ohne Fix","modified":"2026-08-18T06:00:00Z",
  "database_specific":{"severity":"HIGH"},
  "affected":[
    {"package":{"name":"bash","ecosystem":"Debian:12"},
     "ranges":[{"type":"ECOSYSTEM","events":[{"introduced":"0"}]}]}
  ]}`

func mirrored(t *testing.T, dir string, ecosystems ...string) *LocalOSV {
	t.Helper()
	srv := dumpServer(t, "Debian", map[string]string{
		"CVE-2026-1.json": opensslRecord,
		"CVE-2026-2.json": offenerRecord,
	})
	t.Cleanup(srv.Close)

	local := NewLocalOSV(dir, srv.URL)
	if _, err := local.Refresh(context.Background(), ecosystems); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	return local
}

// TestLocalOhneKopieNichtVerfuegbar ist der wichtigste Test dieser Datei:
// Eine Quelle ohne Daten darf NICHT als verfügbar gelten. Täte sie es,
// meldete sie für jedes Paket „nichts gefunden" - ein sauberes Ergebnis für
// etwas, das nie geprüft wurde.
func TestLocalOhneKopieNichtVerfuegbar(t *testing.T) {
	local := NewLocalOSV(t.TempDir(), "")
	if local.Available() {
		t.Error("ohne gespiegelte Daten darf die Quelle nicht verfügbar sein")
	}
	if _, err := local.Query(context.Background(), []string{"pkg:deb/debian/openssl@3.0.11?distro=debian-12"}); err == nil {
		t.Error("Abfrage ohne Kopie muss einen Fehler ergeben, kein leeres Ergebnis")
	}
}

// TestLocalFindetBetroffeneVersion prüft den Kernfall: installierte Version
// unterhalb der behebenden gilt als betroffen, darüber nicht.
func TestLocalFindetBetroffeneVersion(t *testing.T) {
	local := mirrored(t, t.TempDir(), "Debian:12")
	if !local.Available() {
		t.Fatal("nach dem Spiegeln muss die Quelle verfügbar sein")
	}

	betroffen := "pkg:deb/debian/openssl@3.0.11-1?distro=debian-12"
	sauber := "pkg:deb/debian/openssl@3.0.14-1?distro=debian-12"
	got, err := local.Query(context.Background(), []string{betroffen, sauber})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got[betroffen]) != 1 || got[betroffen][0].ID != "CVE-2026-1" {
		t.Errorf("betroffene Version nicht erkannt: %+v", got[betroffen])
	}
	if len(got[sauber]) != 0 {
		t.Errorf("behobene Version darf nicht treffen: %+v", got[sauber])
	}
}

// TestLocalOhneFixBleibtBetroffen: Fehlt eine behebende Version, ist die
// Lücke offen - dann gilt jede installierte Version als betroffen. Genau
// dieser Fall darf nicht durchrutschen, nur weil keine Obergrenze dasteht.
func TestLocalOhneFixBleibtBetroffen(t *testing.T) {
	local := mirrored(t, t.TempDir(), "Debian:12")
	purl := "pkg:deb/debian/bash@5.2-15?distro=debian-12"
	got, err := local.Query(context.Background(), []string{purl})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got[purl]) != 1 {
		t.Errorf("offene Lücke muss treffen: %+v", got[purl])
	}
}

// TestLocalTrenntDistributionen: Dieselbe Meldung nennt je Distribution eine
// andere behebende Version. Wer das vermischt, erklärt einen Bookworm-Server
// anhand der Bullseye-Zahlen für sauber.
func TestLocalTrenntDistributionen(t *testing.T) {
	local := mirrored(t, t.TempDir(), "Debian:11", "Debian:12")

	// 1.1.1w-1 behebt es auf Debian 11 - auf Debian 12 wäre dieselbe Version
	// weiterhin betroffen (dort behebt erst 3.0.14-1).
	bullseye := "pkg:deb/debian/openssl@1.1.1w-1?distro=debian-11"
	bookworm := "pkg:deb/debian/openssl@1.1.1w-1?distro=debian-12"
	got, err := local.Query(context.Background(), []string{bullseye, bookworm})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got[bullseye]) != 0 {
		t.Errorf("auf Debian 11 behoben, dürfte nicht treffen: %+v", got[bullseye])
	}
	if len(got[bookworm]) != 1 {
		t.Errorf("auf Debian 12 noch betroffen, müsste treffen: %+v", got[bookworm])
	}
}

// TestLocalSpiegeltNurGebrauchtes: Was nicht im Bestand vorkommt, wird auch
// nicht indiziert - sonst wäre die Kopie ein Vielfaches größer als nötig.
func TestLocalSpiegeltNurGebrauchtes(t *testing.T) {
	local := mirrored(t, t.TempDir(), "Debian:12")
	index, err := local.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for key := range index {
		if strings.HasPrefix(key, "Debian:11") {
			t.Errorf("nicht angefordertes Ökosystem im Index: %q", key)
		}
	}
}

// TestLocalLiefertBeschreibungen: Titel, Schwere, Fix-Version und Aliase
// stecken bereits im Index - ein zweiter Abruf wäre sinnlos.
func TestLocalLiefertBeschreibungen(t *testing.T) {
	local := mirrored(t, t.TempDir(), "Debian:12")
	got, err := local.Details(context.Background(), []string{"CVE-2026-1"})
	if err != nil {
		t.Fatalf("Details: %v", err)
	}
	d := got["CVE-2026-1"]
	if d.Severity != domain.SeverityCritical || d.Title == "" {
		t.Errorf("Beschreibung unvollständig: %+v", d)
	}
	if d.FixedVersions["openssl"] != "3.0.14-1" {
		t.Errorf("behebende Version falsch: %+v", d.FixedVersions)
	}
	if len(d.Aliases) != 1 || d.Aliases[0] != "DSA-9999-1" {
		t.Errorf("Aliase fehlen: %+v", d.Aliases)
	}
}

// TestEcosystemForPurl prüft die Zuordnung Distro → OSV-Ökosystem, samt der
// bewussten Leerantwort für Unbekanntes: Lieber keine Aussage als eine
// falsche Zuordnung.
func TestEcosystemForPurl(t *testing.T) {
	cases := map[string]string{
		"pkg:deb/debian/openssl@1?distro=debian-12":    "Debian:12",
		"pkg:deb/ubuntu/openssl@1?distro=ubuntu-22.04": "Ubuntu:22.04",
		"pkg:rpm/rocky/openssl@1?distro=rocky-9":       "Rocky Linux:9",
		"pkg:deb/exotic/openssl@1?distro=exotic-1":     "",
		"pkg:deb/debian/openssl@1":                     "",
		"kaputt":                                       "",
	}
	for purl, want := range cases {
		if got := EcosystemForPurl(purl); got != want {
			t.Errorf("EcosystemForPurl(%q) = %q, erwartet %q", purl, got, want)
		}
	}
}

// TestLocalRefreshUeberlebtKaputteDatensaetze: Eine unlesbare Einzeldatei
// darf den ganzen Spiegellauf nicht kippen.
func TestLocalRefreshUeberlebtKaputteDatensaetze(t *testing.T) {
	srv := dumpServer(t, "Debian", map[string]string{
		"kaputt.json":     `{ das ist kein json`,
		"CVE-2026-1.json": opensslRecord,
		"liesmich.txt":    "kein datensatz",
	})
	defer srv.Close()

	local := NewLocalOSV(t.TempDir(), srv.URL)
	if _, err := local.Refresh(context.Background(), []string{"Debian:12"}); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	purl := "pkg:deb/debian/openssl@3.0.11-1?distro=debian-12"
	got, err := local.Query(context.Background(), []string{purl})
	if err != nil || len(got[purl]) != 1 {
		t.Errorf("der lesbare Datensatz muss trotzdem ankommen: %+v (%v)", got, err)
	}
}
