package domain

import (
	"os"
	"regexp"
	"sort"
	"strings"
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
// Der bestehende i18n-Test durchsucht die .svelte-Dateien nach benutzten
// Schlüsseln; diese hier entstehen aber erst zur Laufzeit aus den Daten und
// stehen nirgends im Frontend-Quelltext. Deshalb die Prüfung von dieser Seite.
func TestJederBefundHatEineUebersetzung(t *testing.T) {
	quelle, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	weitere, err := os.ReadFile("storage_insights.go")
	if err != nil {
		t.Fatal(err)
	}

	schluessel := map[string]bool{}
	for _, treffer := range insightAufruf.FindAllStringSubmatch(string(quelle)+string(weitere), -1) {
		schluessel[treffer[1]] = true
	}
	if len(schluessel) < 5 {
		t.Fatalf("nur %d Befund-Schlüssel gefunden - der Ausdruck passt nicht mehr", len(schluessel))
	}

	for _, katalog := range []string{
		"../../../frontend/src/locales/de.js",
		"../../../frontend/src/locales/en.js",
	} {
		inhalt, err := os.ReadFile(katalog)
		if err != nil {
			t.Fatalf("%s: %v", katalog, err)
		}
		// Der insights-Abschnitt reicht bis zum nächsten Abschnitt auf
		// derselben Ebene - nur dort darf gesucht werden, sonst zählte ein
		// gleichnamiger Schlüssel aus einem anderen Bereich mit.
		text := string(inhalt)
		start := strings.Index(text, "\n  insights: {")
		if start < 0 {
			t.Fatalf("%s: der insights-Abschnitt wurde nicht gefunden", katalog)
		}
		ende := strings.Index(text[start+4:], "\n  },")
		if ende < 0 {
			t.Fatalf("%s: das Ende des insights-Abschnitts wurde nicht gefunden", katalog)
		}
		abschnitt := text[start : start+4+ende]

		var fehlen []string
		for k := range schluessel {
			if !strings.Contains(abschnitt, "\n    "+k+":") {
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
