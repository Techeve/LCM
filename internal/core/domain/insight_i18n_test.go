package domain

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"testing"
)

// insightAufruf findet die Befund-Schlüssel, die als Literal im Quelltext
// stehen: insight("warning", "diskLow", …). Dynamisch zusammengesetzte
// Schlüssel (os.SummaryKey, key += "Container") kann der Ausdruck nicht
// erfassen - die bleiben ungeprüft.
var insightAufruf = regexp.MustCompile(`insight\([^,]+,\s*"([a-zA-Z][a-zA-Z0-9]*)"`)

// TestJederBefundHatEineUebersetzung schließt eine Lücke, die mir dreimal
// hintereinander passiert ist.
//
// Die Oberfläche zeigt einen Befund über t('insights.' + key) an. Fehlt der
// Eintrag im Sprachkatalog, erscheint dem Benutzer der rohe Schlüssel statt
// eines Satzes - ohne Fehler, ohne Warnung, und im Go-Test fällt es nicht auf,
// weil dort nur die deutsche Rückfallebene geprüft wird.
//
// Die Sprachprüfung (ui/scripts/check-locales.mjs) durchsucht den Quelltext nach benutzten
// Schlüsseln; diese hier entstehen aber erst zur Laufzeit aus den Daten und
// stehen nirgends in der Oberfläche. Deshalb die Prüfung von dieser Seite.
func TestJederBefundHatEineUebersetzung(t *testing.T) {
	quelle, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	weitere, err := os.ReadFile("storage_insights.go")
	if err != nil {
		t.Fatal(err)
	}

	faehigkeit, err := os.ReadFile("capability_insights.go")
	if err != nil {
		t.Fatal(err)
	}

	schluessel := map[string]bool{}
	for _, treffer := range insightAufruf.FindAllStringSubmatch(string(quelle)+string(weitere)+string(faehigkeit), -1) {
		schluessel[treffer[1]] = true
	}
	if len(schluessel) < 5 {
		t.Fatalf("nur %d Befund-Schlüssel gefunden - der Ausdruck passt nicht mehr", len(schluessel))
	}

	for _, katalog := range []string{
		"../../../ui/src/locales/de.json",
		"../../../ui/src/locales/en.json",
	} {
		inhalt, err := os.ReadFile(katalog)
		if err != nil {
			t.Fatalf("%s: %v", katalog, err)
		}
		// Nur der insights-Abschnitt zählt: Ein gleichnamiger Schlüssel aus
		// einem anderen Bereich wäre keine Übersetzung des Befunds.
		var sprache struct {
			Insights map[string]any `json:"insights"`
		}
		if err := json.Unmarshal(inhalt, &sprache); err != nil {
			t.Fatalf("%s: %v", katalog, err)
		}
		if len(sprache.Insights) == 0 {
			t.Fatalf("%s: der insights-Abschnitt wurde nicht gefunden", katalog)
		}

		var fehlen []string
		for k := range schluessel {
			if _, ok := sprache.Insights[k]; !ok {
				fehlen = append(fehlen, k)
			}
		}
		sort.Strings(fehlen)
		if len(fehlen) > 0 {
			t.Errorf("%s: diese Befunde haben keine Übersetzung: %v\n"+
				"Dem Benutzer erschiene der rohe Schlüssel statt eines Satzes.", katalog, fehlen)
		}
	}
}
