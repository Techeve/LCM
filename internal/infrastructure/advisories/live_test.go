package advisories

import (
	"context"
	"os"
	"testing"
)

// Live-Prüfungen gegen die echten Quellen.
//
// Opt-in über LCM_LIVE_ADVISORIES=1 und bewusst NICHT Teil des normalen
// Laufs: Sie brauchen Netz, laden zig Megabyte und hängen vom Zustand
// fremder Dienste ab - in der CI wären sie eine Quelle für Rot ohne eigenen
// Fehler. Ihr Zweck ist die Gegenprobe von Hand: Stimmen unsere Annahmen
// über Format und Inhalt der Quellen noch?
//
//	LCM_LIVE_ADVISORIES=1 go test ./internal/infrastructure/advisories/ -run TestLive -v

func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv("LCM_LIVE_ADVISORIES") == "" {
		t.Skip("Live-Test übersprungen (LCM_LIVE_ADVISORIES=1 setzen)")
	}
}

// TestLiveMirror spiegelt Debian 12 und prüft, dass eine bekannt veraltete
// openssl-Version Befunde MIT bewerteter Schwere ergibt.
//
// Der Schwere-Teil ist der eigentliche Punkt: Debian liefert keine
// ausdrückliche Angabe, und ohne Auswertung von CVSS-Vektor und
// Dringlichkeit wären sämtliche Befunde „unbekannt" - ein Alarm ab „hoch"
// könnte dann für Betriebssystempakete prinzipiell nie auslösen.
func TestLiveMirror(t *testing.T) {
	skipUnlessLive(t)

	local := NewLocalOSV(t.TempDir(), "")
	summary, err := local.Refresh(context.Background(), []string{"Debian:12"})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	t.Log(summary)

	purl := "pkg:deb/debian/openssl@3.0.11-1~deb12u2?distro=debian-12"
	got, err := local.Query(context.Background(), []string{purl})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got[purl]) == 0 {
		t.Fatal("eine veraltete openssl-Version müsste offene Meldungen haben")
	}

	ids := make([]string, 0, len(got[purl]))
	for _, a := range got[purl] {
		ids = append(ids, a.ID)
	}
	details, err := local.Details(context.Background(), ids)
	if err != nil {
		t.Fatalf("Details: %v", err)
	}
	verteilung := map[string]int{}
	for _, d := range details {
		verteilung[d.Severity]++
	}
	t.Logf("%d Treffer, Schwere-Verteilung: %v", len(ids), verteilung)
	if verteilung["unknown"] == len(details) {
		t.Error("keine einzige bewertete Schwere - die Auswertung von CVSS/Dringlichkeit greift nicht")
	}
}

// TestLiveEUVD prüft, dass der KEV-Dump erreichbar ist und plausibel gefüllt
// ankommt.
func TestLiveEUVD(t *testing.T) {
	skipUnlessLive(t)

	got, err := NewEUVD("").ExploitedCVEs(context.Background())
	if err != nil {
		t.Fatalf("ExploitedCVEs: %v", err)
	}
	t.Logf("%d ausgenutzte Schwachstellen bekannt", len(got))
	if len(got) < 100 {
		t.Errorf("unplausibel wenige Einträge: %d", len(got))
	}
}

// TestLiveOSVQuery prüft die Online-Abfrage gegen api.osv.dev.
func TestLiveOSVQuery(t *testing.T) {
	skipUnlessLive(t)

	purl := "pkg:deb/debian/openssl@3.0.11-1~deb12u2?distro=debian-12"
	got, err := NewOSV("").Query(context.Background(), []string{purl})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got[purl]) == 0 {
		t.Fatal("erwartet: Meldungen zu einer veralteten openssl-Version")
	}
	t.Logf("%d Meldungen online gefunden", len(got[purl]))
}
