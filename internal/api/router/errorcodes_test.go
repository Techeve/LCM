package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"LCM/internal/api/apierr"
	_ "LCM/internal/remote" // meldet die Codes des Agent-Gateways an (nicht über den Router eingebunden)
)

// localeDir liegt relativ zum Testverzeichnis (internal/api/router).
const localeDir = "../../../ui/src/locales"

// TestErrorCodesTranslated stellt sicher, dass jeder angemeldete Fehlercode in
// jeder Sprachdatei als errors.<code> übersetzt ist (Punkte im Code = Pfad im
// verschachtelten JSON). Dieses Paket importiert Controller und Middlewares und
// meldet damit fast alle Codes an; das Agent-Gateway kommt per Blind-Import dazu.
func TestErrorCodesTranslated(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		t.Run(lang, func(t *testing.T) {
			errorsTree := loadErrorsTree(t, lang)
			for _, code := range apierr.Codes() {
				text, ok := lookupCode(errorsTree, code)
				if !ok || strings.TrimSpace(text) == "" {
					t.Errorf("errors.%s fehlt in %s.json", code, lang)
				}
			}
		})
	}
}

// TestLocalesHaveNoStaleKeys findet Tippfehler und verwaiste Schlüssel: Jeder
// Blattschlüssel unter errors muss ein angemeldeter Code oder ein Rückfall der
// Oberfläche sein.
func TestLocalesHaveNoStaleKeys(t *testing.T) {
	known := map[string]bool{"http.generic": true, "network": true}
	for _, code := range apierr.Codes() {
		known[code] = true
	}
	for _, lang := range []string{"de", "en"} {
		for _, key := range leafKeys(loadErrorsTree(t, lang), "") {
			if !known[key] {
				t.Errorf("errors.%s in %s.json gehört zu keinem Fehlercode", key, lang)
			}
		}
	}
}

// TestLocalePlaceholdersMatch prüft, dass Deutsch und Englisch je Schlüssel
// dieselben Platzhalter ({name}) verwenden.
func TestLocalePlaceholdersMatch(t *testing.T) {
	de, en := loadErrorsTree(t, "de"), loadErrorsTree(t, "en")
	for _, key := range leafKeys(de, "") {
		deText, _ := lookupCode(de, key)
		enText, ok := lookupCode(en, key)
		if !ok {
			continue // fehlende Schlüssel meldet TestErrorCodesTranslated
		}
		if a, b := placeholders(deText), placeholders(enText); !slices.Equal(a, b) {
			t.Errorf("errors.%s: Platzhalter de %v, en %v", key, a, b)
		}
	}
}

// leafKeys liefert die gepunkteten Pfade aller Blätter des Baums.
func leafKeys(node map[string]any, prefix string) []string {
	var keys []string
	for name, child := range node {
		path := prefix + name
		if branch, ok := child.(map[string]any); ok {
			keys = append(keys, leafKeys(branch, path+".")...)
			continue
		}
		keys = append(keys, path)
	}
	return keys
}

var placeholderPattern = regexp.MustCompile(`\{([a-z_]+)\}`)

// placeholders liefert die sortierten Platzhalter-Namen eines Textes.
func placeholders(text string) []string {
	var names []string
	for _, m := range placeholderPattern.FindAllStringSubmatch(text, -1) {
		names = append(names, m[1])
	}
	slices.Sort(names)
	return names
}

// TestFallbackTexts prüft die Rückfall-Texte, die die Oberfläche selbst nutzt.
func TestFallbackTexts(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		errorsTree := loadErrorsTree(t, lang)
		for _, key := range []string{"http.generic", "network"} {
			if text, ok := lookupCode(errorsTree, key); !ok || text == "" {
				t.Errorf("errors.%s fehlt in %s.json", key, lang)
			}
		}
	}
}

// loadErrorsTree liest den Teilbaum "errors" einer Sprachdatei.
func loadErrorsTree(t *testing.T, lang string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(localeDir, lang+".json"))
	if err != nil {
		t.Fatalf("Sprachdatei %s: %v", lang, err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("Sprachdatei %s ist kein gültiges JSON: %v", lang, err)
	}
	tree, ok := root["errors"].(map[string]any)
	if !ok {
		t.Fatalf("Sprachdatei %s hat keinen Teilbaum \"errors\"", lang)
	}
	return tree
}

// lookupCode folgt dem gepunkteten Code durch den verschachtelten Baum.
func lookupCode(tree map[string]any, code string) (string, bool) {
	var node any = tree
	for _, part := range strings.Split(code, ".") {
		branch, ok := node.(map[string]any)
		if !ok {
			return "", false
		}
		if node, ok = branch[part]; !ok {
			return "", false
		}
	}
	text, ok := node.(string)
	return text, ok
}
