// Package appdocs liefert die in LCM mitgelieferte Anwender-Doku aus. Die
// Seiten sind Markdown-Dateien im Repository, die ins Binary eingebettet
// werden - pflegen heißt damit: Datei bearbeiten, fertig.
//
// Abgrenzung zur großen Doku unter docs/: Dort steht das Handbuch für
// Betreiber (wird als eigene Website gebaut). Hier stehen kurze Anleitungen
// für die Menschen, die LCM benutzen - erreichbar direkt in der Anwendung
// unter /doku, ohne Anmeldung, damit sie auch aus einer Aktivierungs-Mail
// heraus offensteht.
package appdocs

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed pages
var pagesFS embed.FS

// Page ist eine Doku-Seite.
type Page struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	// Order steuert die Reihenfolge in der Übersicht (Zahl im Dateinamen).
	Order int `json:"-"`
	// HTML ist der gerenderte Inhalt (nur bei der Einzelabfrage gefüllt).
	HTML string `json:"html,omitempty"`
}

// defaultLang gilt, wenn die angefragte Sprache nicht vorliegt.
const defaultLang = "en"

// normalizeLang bildet die Anfrage auf eine vorhandene Sprache ab.
func normalizeLang(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	if strings.HasPrefix(l, "de") {
		return "de"
	}
	return defaultLang
}

// List liefert alle Seiten einer Sprache, sortiert nach der Nummer im
// Dateinamen (10-…, 20-… - so lässt sich später etwas dazwischenschieben).
func List(lang string) ([]Page, error) {
	dir := "pages/" + normalizeLang(lang)
	entries, err := fs.ReadDir(pagesFS, dir)
	if err != nil {
		return nil, fmt.Errorf("doku-verzeichnis %s: %w", dir, err)
	}
	var pages []Page
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := fs.ReadFile(pagesFS, dir+"/"+e.Name())
		if err != nil {
			continue
		}
		order, slug := splitName(e.Name())
		pages = append(pages, Page{Slug: slug, Title: titleOf(string(raw), slug), Order: order})
	}
	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Order != pages[j].Order {
			return pages[i].Order < pages[j].Order
		}
		return pages[i].Slug < pages[j].Slug
	})
	return pages, nil
}

// Get liefert eine einzelne Seite samt gerendertem HTML. Fehlt sie in der
// gewünschten Sprache, wird die englische Fassung versucht - eine fehlende
// Übersetzung soll die Seite nicht verschwinden lassen.
func Get(lang, slug string) (*Page, error) {
	if !validSlug(slug) {
		return nil, fmt.Errorf("ungültiger seitenname")
	}
	for _, l := range []string{normalizeLang(lang), defaultLang} {
		if p := find(l, slug); p != nil {
			return p, nil
		}
	}
	return nil, fmt.Errorf("seite %q nicht gefunden", slug)
}

func find(lang, slug string) *Page {
	entries, err := fs.ReadDir(pagesFS, "pages/"+lang)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		order, s := splitName(e.Name())
		if s != slug {
			continue
		}
		raw, err := fs.ReadFile(pagesFS, "pages/"+lang+"/"+e.Name())
		if err != nil {
			return nil
		}
		body := string(raw)
		return &Page{Slug: s, Title: titleOf(body, s), Order: order, HTML: Render(body)}
	}
	return nil
}

// splitName zerlegt "10-ssh-schluessel.md" in Reihenfolge und Slug.
func splitName(name string) (int, string) {
	base := strings.TrimSuffix(name, ".md")
	if i := strings.Index(base, "-"); i > 0 {
		if n, err := strconv.Atoi(base[:i]); err == nil {
			return n, base[i+1:]
		}
	}
	return 1 << 30, base
}

// titleOf nimmt die erste Überschrift als Titel; fehlt sie, dient der Slug.
func titleOf(body, fallback string) string {
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "# "))
		}
	}
	return fallback
}

// validSlug hält Pfadtrickserei aus dem Dateizugriff heraus. Die Seiten liegen
// zwar im Binary und nicht im Dateisystem, aber der Name kommt aus der URL -
// er wird geprüft, bevor er irgendwo eingesetzt wird.
func validSlug(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}
