package router_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Die API-Beschreibung (docs/static/openapi.yaml) ist von Hand gepflegt - sie
// enthält Erklärungen, die kein Generator liefern kann. Der Preis dafür ist,
// dass sie hinterherhinkt: Zuletzt fehlten 52 Endpunkte, darunter der ganze
// Bereich der Berechtigungsprofile. Wer die Beschreibung liest, hält sie aber
// für vollständig - das ist schlimmer als eine erkennbar unfertige Datei.
//
// Dieser Test schließt die Lücke von der anderen Seite: Er vergleicht die
// Routen des Routers mit den Pfaden der Beschreibung. Ein neuer Endpunkt ohne
// Eintrag fällt damit im Testlauf auf und nicht erst dem Anwender.
//
// Bewusst nur der Abgleich der Pfade, nicht der Inhalte: Ob eine Beschreibung
// stimmt, kann kein Test beurteilen - dass es sie überhaupt gibt, schon.

// reGroup findet die Gruppen-Definitionen des Routers: name := parent.Group("/prefix"…
var reGroup = regexp.MustCompile(`(\w+)\s*:=\s*(\w+)\.Group\("([^"]+)"`)

// reRoute findet die Routen: gruppe.Methode("/pfad"…
var reRoute = regexp.MustCompile(`(\w+)\.(Get|Post|Put|Patch|Delete)\("([^"]*)"`)

// reParam übersetzt die Fiber-Schreibweise :id in die OpenAPI-Form {id}.
var reParam = regexp.MustCompile(`:(\w+)`)

// routerRouten liest die registrierten Routen aus dem Quelltext des Routers.
func routerRouten(t *testing.T) map[string]bool {
	t.Helper()
	daten, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("router.go nicht lesbar: %v", err)
	}
	source := string(daten)

	praefix := map[string]string{}
	for _, m := range reGroup.FindAllStringSubmatch(source, -1) {
		praefix[m[1]] = praefix[m[2]] + m[3]
	}

	routen := map[string]bool{}
	for _, m := range reRoute.FindAllStringSubmatch(source, -1) {
		basis, ok := praefix[m[1]]
		if !ok {
			continue // kein Gruppen-Aufruf (z.B. eine lokale Variable)
		}
		path := strings.TrimSuffix(basis+m[3], "/")
		if path == "" {
			path = "/"
		}
		routen[strings.ToUpper(m[2])+" "+reParam.ReplaceAllString(path, "{$1}")] = true
	}
	if len(routen) < 100 {
		t.Fatalf("nur %d Routen erkannt - die Auswertung von router.go passt nicht mehr", len(routen))
	}
	return routen
}

var (
	rePfad    = regexp.MustCompile(`^  (/\S*):\s*$`)
	reMethode = regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
)

// dokuRouten liest die beschriebenen Pfade aus der openapi.yaml.
func dokuRouten(t *testing.T) map[string]bool {
	t.Helper()
	daten, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "static", "openapi.yaml"))
	if err != nil {
		t.Fatalf("openapi.yaml nicht lesbar: %v", err)
	}
	routen := map[string]bool{}
	path := ""
	for _, line := range strings.Split(string(daten), "\n") {
		if m := rePfad.FindStringSubmatch(line); m != nil {
			path = m[1]
			continue
		}
		if m := reMethode.FindStringSubmatch(line); m != nil && path != "" {
			routen[strings.ToUpper(m[1])+" "+path] = true
		}
	}
	return routen
}

func TestJedeRouteStehtInDerAPIBeschreibung(t *testing.T) {
	routen, doku := routerRouten(t), dokuRouten(t)

	var missing []string
	for r := range routen {
		if !doku[r] {
			missing = append(missing, r)
		}
	}
	sort.Strings(missing)
	for _, r := range missing {
		t.Errorf("nicht in docs/static/openapi.yaml beschrieben: %s", r)
	}
}

// TestDieAPIBeschreibungErfindetNichts ist die Gegenrichtung: Ein Pfad, den es
// im Router nicht (mehr) gibt, führt jeden in die Irre, der danach greift.
func TestDieAPIBeschreibungErfindetNichts(t *testing.T) {
	routen, doku := routerRouten(t), dokuRouten(t)

	var extra []string
	for r := range doku {
		if !routen[r] {
			extra = append(extra, r)
		}
	}
	sort.Strings(extra)
	for _, r := range extra {
		t.Errorf("beschrieben, aber im Router nicht vorhanden: %s", r)
	}
}
