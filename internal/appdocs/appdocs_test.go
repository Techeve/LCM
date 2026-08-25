package appdocs

import (
	"regexp"
	"strings"
	"testing"
)

// TestListLiefertSeitenBeiderSprachen: die Doku ist zweisprachig - fehlt eine
// Seite auf einer Seite, merkt es sonst niemand.
func TestListLiefertSeitenBeiderSprachen(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		pages, err := List(lang)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if len(pages) == 0 {
			t.Fatalf("%s: keine Seiten eingebettet", lang)
		}
		for _, p := range pages {
			if p.Title == "" || p.Title == p.Slug {
				t.Errorf("%s/%s: kein Titel aus der ersten Überschrift gezogen", lang, p.Slug)
			}
		}
	}
	de, _ := List("de")
	en, _ := List("en")
	if len(de) != len(en) {
		t.Errorf("unterschiedlich viele Seiten: de=%d, en=%d - Übersetzung fehlt", len(de), len(en))
	}
}

// TestGetRendertUndFaelltAufEnglischZurueck.
func TestGetRendertUndFaelltAufEnglischZurueck(t *testing.T) {
	p, err := Get("de", "ssh-schluessel")
	if err != nil {
		t.Fatalf("deutsche seite: %v", err)
	}
	if !strings.Contains(p.HTML, "<h1") || !strings.Contains(p.HTML, "PuTTY") {
		t.Errorf("Inhalt sieht nicht gerendert aus:\n%s", p.HTML[:min(300, len(p.HTML))])
	}
	// Unbekannte Sprache => englische Fassung, nicht Fehler.
	if _, err := Get("fr", "ssh-schluessel"); err != nil {
		t.Errorf("unbekannte Sprache sollte auf Englisch zurückfallen: %v", err)
	}
}

// TestGetWeistPfadtricksAb: der Slug kommt aus der URL.
func TestGetWeistPfadtricksAb(t *testing.T) {
	for _, bad := range []string{"../../etc/passwd", "a/b", "..", "Gross", "mit leer", ""} {
		if _, err := Get("de", bad); err == nil {
			t.Errorf("Slug %q wurde angenommen", bad)
		}
	}
}

// TestZweiFaktorSeite: die Anleitung muss den Weg vollständig beschreiben -
// Einrichtung, Notfallcodes und die Probe in einer ZWEITEN Sitzung. Fehlt
// Letzteres, sperren sich Leser aus, und genau das soll die Seite verhindern.
func TestZweiFaktorSeite(t *testing.T) {
	fuer := map[string][]string{
		"de": {"google-authenticator", "QR", "Notfallcodes", "zweites", "rm ~/.google_authenticator", "Verification code"},
		"en": {"google-authenticator", "QR", "emergency", "second", "rm ~/.google_authenticator", "Verification code"},
	}
	for lang, begriffe := range fuer {
		p, err := Get(lang, "ssh-2fa")
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		for _, b := range begriffe {
			if !strings.Contains(p.HTML, b) {
				t.Errorf("%s: %q fehlt in der 2FA-Anleitung", lang, b)
			}
		}
	}
}

// TestQuerverweiseZeigenAufVorhandeneSeiten: ein Verweis auf eine Seite, die
// es nicht gibt, ist schlimmer als keiner.
func TestQuerverweiseZeigenAufVorhandeneSeiten(t *testing.T) {
	verweis := regexp.MustCompile(`href="/#/doku/([a-z0-9-]+)"`)
	for _, lang := range []string{"de", "en"} {
		pages, err := List(lang)
		if err != nil {
			t.Fatal(err)
		}
		existing := map[string]bool{}
		for _, p := range pages {
			existing[p.Slug] = true
		}
		for _, p := range pages {
			voll, err := Get(lang, p.Slug)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range verweis.FindAllStringSubmatch(voll.HTML, -1) {
				if !existing[m[1]] {
					t.Errorf("%s/%s verweist auf die nicht vorhandene Seite %q", lang, p.Slug, m[1])
				}
			}
		}
	}
}

// TestGeraeteAnleitungenNennenDieVoraussetzungen: Bei diesen drei Wegen
// scheitert es fast immer an derselben Kleinigkeit - dem Agent-Port, dem
// Nur-Lese-Benutzer auf dem Router, der erzwungenen 2FA am NAS. Fehlt einer
// dieser Punkte, ist die Anleitung an der entscheidenden Stelle stumm.
func TestGeraeteAnleitungenNennenDieVoraussetzungen(t *testing.T) {
	pflicht := map[string]map[string][]string{
		"agent": {
			"de": {"lcm-agent enroll", "9320", "root", "Token", "systemctl status lcm-agent", "uninstall"},
			"en": {"lcm-agent enroll", "9320", "root", "token", "systemctl status lcm-agent", "uninstall"},
		},
		"routeros": {
			"de": {"group=read", "ssh-keys import", "Fingerprint", "rein lesend"},
			"en": {"group=read", "ssh-keys import", "fingerprint", "read-only"},
		},
		"synology": {
			"de": {"administrators", "5001", "Zertifikat", "Zwei-Faktor"},
			"en": {"administrators", "5001", "ertificate", "two-factor"},
		},
	}
	for slug, sprachen := range pflicht {
		for lang, begriffe := range sprachen {
			p, err := Get(lang, slug)
			if err != nil {
				t.Fatalf("%s/%s: %v", lang, slug, err)
			}
			for _, b := range begriffe {
				if !strings.Contains(p.HTML, b) {
					t.Errorf("%s/%s: %q fehlt", lang, slug, b)
				}
			}
		}
	}
}

// TestJedeSeiteGibtEsInBeidenSprachen: eine einsprachige Seite fällt sonst
// erst auf, wenn jemand die Sprache umschaltet und ins Leere greift.
func TestJedeSeiteGibtEsInBeidenSprachen(t *testing.T) {
	de, err := List("de")
	if err != nil {
		t.Fatal(err)
	}
	en, err := List("en")
	if err != nil {
		t.Fatal(err)
	}
	slugs := func(ps []Page) map[string]bool {
		m := map[string]bool{}
		for _, p := range ps {
			m[p.Slug] = true
		}
		return m
	}
	dm, em := slugs(de), slugs(en)
	for s := range dm {
		if !em[s] {
			t.Errorf("Seite %q fehlt auf Englisch", s)
		}
	}
	for s := range em {
		if !dm[s] {
			t.Errorf("Seite %q fehlt auf Deutsch", s)
		}
	}
	// Die Reihenfolge muss in beiden Sprachen dieselbe sein - sonst springt
	// die Seitenliste beim Umschalten.
	for i := range de {
		if i < len(en) && de[i].Slug != en[i].Slug {
			t.Errorf("Reihenfolge weicht ab an Position %d: de=%q, en=%q", i, de[i].Slug, en[i].Slug)
		}
	}
}

// TestSeitenNennenDieDreiSysteme: der Auftrag war ausdrücklich Linux, macOS
// und Windows - inklusive des PuTTY-Wegs. Bricht jemand später eine Passage
// heraus, fällt es hier auf.
func TestSeitenNennenDieDreiSysteme(t *testing.T) {
	fuer := map[string][]string{
		"de": {"Linux", "macOS", "Windows", "PuTTY", "PuTTYgen", "Pageant", ".ppk", "ssh-keygen", "authorized_keys"},
		"en": {"Linux", "macOS", "Windows", "PuTTY", "PuTTYgen", "Pageant", ".ppk", "ssh-keygen", "authorized_keys"},
	}
	for lang, begriffe := range fuer {
		p, err := Get(lang, "ssh-schluessel")
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		for _, b := range begriffe {
			if !strings.Contains(p.HTML, b) {
				t.Errorf("%s: %q fehlt in der Anleitung", lang, b)
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
