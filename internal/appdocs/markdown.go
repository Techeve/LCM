package appdocs

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Markdown-Renderer für die mitgelieferte Doku - bewusst eine Teilmenge und
// bewusst selbst gebaut (Regel: docs/reference/dependencies.md). Keine
// Kryptographie, und die Eingabe kommt nicht von außen: Die Seiten sind ins
// Binary eingebettet und stammen aus diesem Repository. Ein Bug im Renderer
// heißt „die Seite sieht schief aus", nicht „jemand kommt herein".
//
// Roh-HTML wird NICHT durchgereicht, sondern maskiert. Das ist die eine
// Stelle, an der ein Fehler teuer wäre: Der erzeugte HTML-Code landet im
// Frontend in einem {@html}-Block. Alles, was aus der Quelle kommt, geht
// deshalb erst durch html.EscapeString, bevor eigene Tags entstehen.
//
// Unterstützt: Überschriften, Absätze, Listen (auch nummeriert und
// verschachtelt über Einrückung), Codeblöcke mit Sprachangabe, Tabellen,
// Zitat-/Hinweisblöcke, Trennlinien sowie inline Code, fett, kursiv und Links.

var (
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reBold       = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reItalic     = regexp.MustCompile(`(^|[^*])\*([^*]+)\*`)
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reHeading    = regexp.MustCompile(`^(#{1,4})\s+(.*)$`)
	reOrdered    = regexp.MustCompile(`^(\s*)(\d+)\.\s+(.*)$`)
	reBullet     = regexp.MustCompile(`^(\s*)[-*]\s+(.*)$`)
)

// openList ist eine gerade offene Liste im Ausgabestrom.
type openList struct {
	tag    string // "ul" oder "ol"
	indent int    // Einrücktiefe der Quellzeile
}

// Render wandelt Markdown in HTML.
func Render(md string) string {
	var out strings.Builder
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")

	// lists hält die offenen Listen (ul/ol) samt Einrücktiefe, damit
	// verschachtelte Aufzählungen wieder korrekt geschlossen werden.
	var lists []openList
	inPara := false

	closeLists := func(toIndent int) {
		for len(lists) > 0 && lists[len(lists)-1].indent >= toIndent {
			fmt.Fprintf(&out, "</%s>\n", lists[len(lists)-1].tag)
			lists = lists[:len(lists)-1]
		}
	}
	closePara := func() {
		if inPara {
			out.WriteString("</p>\n")
			inPara = false
		}
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Codeblock: bis zum schließenden ``` einsammeln, Inhalt unverändert
		// (nur maskiert) übernehmen.
		if strings.HasPrefix(trimmed, "```") {
			closePara()
			closeLists(0)
			lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			cls := ""
			if lang != "" {
				cls = ` class="language-` + html.EscapeString(lang) + `"`
			}
			fmt.Fprintf(&out, "<pre><code%s>%s</code></pre>\n", cls,
				html.EscapeString(strings.Join(code, "\n")))
			continue
		}

		if trimmed == "" {
			closePara()
			closeLists(0)
			continue
		}

		if m := reHeading.FindStringSubmatch(trimmed); m != nil {
			closePara()
			closeLists(0)
			level := len(m[1])
			text := inline(m[2])
			fmt.Fprintf(&out, "<h%d id=\"%s\">%s</h%d>\n", level, slugify(m[2]), text, level)
			continue
		}

		if trimmed == "---" || trimmed == "***" {
			closePara()
			closeLists(0)
			out.WriteString("<hr>\n")
			continue
		}

		// Tabelle: Kopfzeile, Trennzeile, dann Datenzeilen.
		if strings.HasPrefix(trimmed, "|") && i+1 < len(lines) && isTableDivider(lines[i+1]) {
			closePara()
			closeLists(0)
			i += renderTable(&out, lines[i:])
			continue
		}

		// Zitat/Hinweis.
		if strings.HasPrefix(trimmed, ">") {
			closePara()
			closeLists(0)
			var quote []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">"); i++ {
				quote = append(quote, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">")))
			}
			i--
			fmt.Fprintf(&out, "<blockquote>%s</blockquote>\n", inline(strings.Join(quote, " ")))
			continue
		}

		if m := reOrdered.FindStringSubmatch(line); m != nil {
			indent := len(m[1])
			closePara()
			lists = openListFor(&out, lists, "ol", indent)
			fmt.Fprintf(&out, "<li>%s</li>\n", inline(m[3]))
			continue
		}
		if m := reBullet.FindStringSubmatch(line); m != nil {
			indent := len(m[1])
			closePara()
			lists = openListFor(&out, lists, "ul", indent)
			fmt.Fprintf(&out, "<li>%s</li>\n", inline(m[2]))
			continue
		}

		// Fortsetzungszeile innerhalb einer Liste (eingerückter Fließtext).
		if len(lists) > 0 && strings.HasPrefix(line, "  ") {
			out.WriteString(" " + inline(trimmed) + "\n")
			continue
		}

		closeLists(0)
		if !inPara {
			out.WriteString("<p>")
			inPara = true
		} else {
			out.WriteString(" ")
		}
		out.WriteString(inline(trimmed))
	}
	closePara()
	closeLists(0)
	return out.String()
}

// isTableDivider erkennt die Trennzeile einer Tabelle (|---|:--:|).
func isTableDivider(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "|") {
		return false
	}
	for _, r := range t {
		if r != '|' && r != '-' && r != ':' && r != ' ' {
			return false
		}
	}
	return strings.Contains(t, "-")
}

// renderTable gibt die Tabelle aus und liefert die Zahl der verbrauchten
// Zeilen (abzüglich der aktuellen).
func renderTable(out *strings.Builder, lines []string) int {
	cells := func(s string) []string {
		s = strings.TrimSpace(s)
		s = strings.TrimPrefix(s, "|")
		s = strings.TrimSuffix(s, "|")
		parts := strings.Split(s, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	out.WriteString("<div class=\"table-responsive\"><table><thead><tr>")
	for _, c := range cells(lines[0]) {
		fmt.Fprintf(out, "<th>%s</th>", inline(c))
	}
	out.WriteString("</tr></thead><tbody>")
	used := 1 // Trennzeile
	for i := 2; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			break
		}
		out.WriteString("<tr>")
		for _, c := range cells(lines[i]) {
			fmt.Fprintf(out, "<td>%s</td>", inline(c))
		}
		out.WriteString("</tr>")
		used++
	}
	out.WriteString("</tbody></table></div>\n")
	return used
}

// inline maskiert den Text und setzt danach die Inline-Auszeichnungen.
// Reihenfolge ist wesentlich: erst maskieren, dann eigene Tags erzeugen -
// sonst käme Roh-HTML aus der Quelle durch.
func inline(s string) string {
	s = html.EscapeString(s)
	s = reInlineCode.ReplaceAllString(s, "<code>$1</code>")
	s = reBold.ReplaceAllString(s, "<strong>$1</strong>")
	s = reItalic.ReplaceAllString(s, "$1<em>$2</em>")
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		p := reLink.FindStringSubmatch(m)
		href := p[2]
		// Nur unbedenkliche Ziele: http(s), Anker und interne Pfade.
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") &&
			!strings.HasPrefix(href, "#") && !strings.HasPrefix(href, "/") {
			return p[1]
		}
		ext := ""
		if strings.HasPrefix(href, "http") {
			ext = ` target="_blank" rel="noopener noreferrer"`
		}
		return fmt.Sprintf(`<a href="%s"%s>%s</a>`, href, ext, p[1])
	})
	return s
}

// slugify baut aus einer Überschrift eine Anker-Kennung.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		case r == 'ä':
			b.WriteString("ae")
		case r == 'ö':
			b.WriteString("oe")
		case r == 'ü':
			b.WriteString("ue")
		case r == 'ß':
			b.WriteString("ss")
		}
	}
	return strings.Trim(b.String(), "-")
}

// openListFor sorgt dafür, dass für die gegebene Einrücktiefe die passende
// Liste offen ist: tiefere Ebenen werden geschlossen, eine neue wird geöffnet,
// wenn es auf dieser Tiefe noch keine gibt.
func openListFor(out *strings.Builder, lists []openList, tag string, indent int) []openList {
	for len(lists) > 0 && lists[len(lists)-1].indent > indent {
		fmt.Fprintf(out, "</%s>\n", lists[len(lists)-1].tag)
		lists = lists[:len(lists)-1]
	}
	if len(lists) == 0 || lists[len(lists)-1].indent < indent {
		fmt.Fprintf(out, "<%s>\n", tag)
		return append(lists, openList{tag: tag, indent: indent})
	}
	// Gleiche Tiefe, aber anderer Listentyp: umschalten.
	if lists[len(lists)-1].tag != tag {
		fmt.Fprintf(out, "</%s>\n<%s>\n", lists[len(lists)-1].tag, tag)
		lists[len(lists)-1] = openList{tag: tag, indent: indent}
	}
	return lists
}
