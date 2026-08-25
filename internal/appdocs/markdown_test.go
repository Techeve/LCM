package appdocs

import (
	"strings"
	"testing"
)

// TestRenderMaskiertRohHTML ist die wichtigste Prüfung: Das Ergebnis landet im
// Frontend in einem {@html}-Block. Käme Roh-HTML aus der Quelle durch, wäre
// aus einer Doku-Seite ein Einfallstor geworden.
func TestRenderMaskiertRohHTML(t *testing.T) {
	sources := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		"Text mit `<b>Code</b>` drin",
		`[Link](javascript:alert(1))`,
		`[Link](data:text/html,<script>alert(1)</script>)`,
	}
	for _, q := range sources {
		got := Render(q)
		// Geprüft wird auf ECHTE Tags. Der maskierte Text enthält
		// „onerror=" durchaus - dort ist es aber Fließtext, kein Attribut.
		for _, tag := range []string{"<script", "<img", "<b>"} {
			if strings.Contains(got, tag) {
				t.Errorf("Roh-Tag %q durchgereicht bei %q:\n%s", tag, q, got)
			}
		}
		if strings.Contains(got, "javascript:") || strings.Contains(got, "href=\"data:") {
			t.Errorf("gefährliches Link-Ziel übernommen bei %q:\n%s", q, got)
		}
	}
}

// TestRenderGrundbausteine: die Auszeichnungen, die die Doku wirklich nutzt.
func TestRenderGrundbausteine(t *testing.T) {
	md := strings.Join([]string{
		"# Titel",
		"",
		"Ein Absatz mit **fett**, *kursiv* und `Code`.",
		"",
		"## Abschnitt",
		"",
		"- erster Punkt",
		"- zweiter Punkt",
		"",
		"1. Schritt eins",
		"2. Schritt zwei",
		"",
		"```bash",
		"ssh-keygen -t ed25519",
		"```",
		"",
		"> Ein Hinweis.",
	}, "\n")
	got := Render(md)

	for _, want := range []string{
		`<h1 id="titel">Titel</h1>`,
		"<strong>fett</strong>",
		"<em>kursiv</em>",
		"<code>Code</code>",
		`<h2 id="abschnitt">Abschnitt</h2>`,
		"<ul>",
		"<li>erster Punkt</li>",
		"<ol>",
		"<li>Schritt eins</li>",
		`<pre><code class="language-bash">ssh-keygen -t ed25519</code></pre>`,
		"<blockquote>Ein Hinweis.</blockquote>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("erwartet %q in:\n%s", want, got)
		}
	}
}

// TestRenderListenWerdenGeschlossen: offene Listen sind der klassische Fehler
// eines selbst gebauten Renderers - das Ergebnis wäre kaputtes HTML.
func TestRenderListenWerdenGeschlossen(t *testing.T) {
	got := Render("- a\n- b\n\nDanach\n\n1. x\n2. y\n")
	if strings.Count(got, "<ul>") != strings.Count(got, "</ul>") {
		t.Errorf("ul nicht ausgeglichen:\n%s", got)
	}
	if strings.Count(got, "<ol>") != strings.Count(got, "</ol>") {
		t.Errorf("ol nicht ausgeglichen:\n%s", got)
	}
	if strings.Count(got, "<p>") != strings.Count(got, "</p>") {
		t.Errorf("p nicht ausgeglichen:\n%s", got)
	}
}

// TestRenderTabelle: die Anleitung nutzt Tabellen für Pfad-Übersichten.
func TestRenderTabelle(t *testing.T) {
	got := Render("| System | Pfad |\n|---|---|\n| Linux | `~/.ssh` |\n| Windows | `%USERPROFILE%` |\n")
	for _, want := range []string{"<table>", "<th>System</th>", "<td>Linux</td>", "<code>~/.ssh</code>"} {
		if !strings.Contains(got, want) {
			t.Errorf("erwartet %q in:\n%s", want, got)
		}
	}
}

// TestRenderCodeblockBleibtWoertlich: in Codeblöcken darf nichts als
// Markdown gedeutet werden - sonst zerfielen Beispielbefehle.
func TestRenderCodeblockBleibtWoertlich(t *testing.T) {
	got := Render("```\nHost *\n  IdentityFile ~/.ssh/id_ed25519\n```")
	if !strings.Contains(got, "Host *") {
		t.Errorf("Sternchen im Codeblock wurde umgedeutet:\n%s", got)
	}
	if strings.Contains(got, "<em>") {
		t.Errorf("Kursiv-Auszeichnung im Codeblock angewandt:\n%s", got)
	}
}

// TestSlugifyDeutsch: Anker müssen auch mit Umlauten benutzbar bleiben.
func TestSlugifyDeutsch(t *testing.T) {
	cases := map[string]string{
		"Schlüssel einrichten": "schluessel-einrichten",
		"PuTTY (PPK)":          "putty-ppk",
		"Größe & Umfang":       "groesse--umfang",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, erwartet %q", in, got, want)
		}
	}
}
