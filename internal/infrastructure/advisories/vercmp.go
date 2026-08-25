package advisories

import "strings"

// Versionsvergleich für Distributions-Pakete.
//
// Warum das hier überhaupt steht: Die Online-Abfrage bei OSV nimmt einem
// diese Arbeit ab - man schickt eine Version, man bekommt die Treffer. Die
// lokale Kopie liefert dagegen nur rohe Versionsspannen ("betroffen bis
// 1.2.3-4"), und ob die installierte Version darunter fällt, muss LCM selbst
// entscheiden.
//
// Naive Vergleiche (String, SemVer, Zahlen splitten) sind hier alle falsch,
// und zwar auf die gefährliche Art: Sie liefern ein Ergebnis, das meistens
// stimmt. "1.10" ist größer als "1.9", "1.0~rc1" ist KLEINER als "1.0", und
// "2:1.0" schlägt "1:9.9". Wer das übergeht, erklärt einen verwundbaren
// Server für sauber.
//
// Umgesetzt sind die Algorithmen der Paketverwaltungen selbst: dpkg
// (deb-version(7)) und rpm (rpmvercmp).

// compareDeb vergleicht zwei Debian-Versionen: -1, 0 oder 1.
//
// Aufbau: [epoche:]upstream[-revision]. Die Epoche schlägt alles andere -
// sie existiert genau dafür, eine Versionsfolge zurücksetzen zu können.
func compareDeb(a, b string) int {
	epochA, restA := splitEpoch(a)
	epochB, restB := splitEpoch(b)
	if epochA != epochB {
		return sign(epochA - epochB)
	}
	upA, revA := splitRevision(restA)
	upB, revB := splitRevision(restB)
	if c := compareDebPart(upA, upB); c != 0 {
		return c
	}
	return compareDebPart(revA, revB)
}

// splitEpoch trennt die Epoche ab (fehlt sie, ist sie 0).
func splitEpoch(v string) (int, string) {
	i := strings.IndexByte(v, ':')
	if i < 0 {
		return 0, v
	}
	n, ok := atoiSafe(v[:i])
	if !ok {
		return 0, v
	}
	return n, v[i+1:]
}

// splitRevision trennt die Debian-Revision am LETZTEN Bindestrich ab - der
// Upstream-Teil darf selbst Bindestriche enthalten.
func splitRevision(v string) (upstream, revision string) {
	i := strings.LastIndexByte(v, '-')
	if i < 0 {
		return v, ""
	}
	return v[:i], v[i+1:]
}

// compareDebPart vergleicht upstream- bzw. revision-Teile nach dem
// dpkg-Verfahren: abwechselnd nicht-numerische und numerische Abschnitte,
// erstere zeichenweise nach einer eigenen Ordnung, letztere als Zahl.
func compareDebPart(a, b string) int {
	for len(a) > 0 || len(b) > 0 {
		// Nicht-numerischer Abschnitt.
		i, j := 0, 0
		for i < len(a) && !isDigit(a[i]) {
			i++
		}
		for j < len(b) && !isDigit(b[j]) {
			j++
		}
		if c := compareAlpha(a[:i], b[:j]); c != 0 {
			return c
		}
		a, b = a[i:], b[j:]

		// Numerischer Abschnitt: führende Nullen sind bedeutungslos, deshalb
		// als Zahl vergleichen und nicht als Text.
		i, j = 0, 0
		for i < len(a) && isDigit(a[i]) {
			i++
		}
		for j < len(b) && isDigit(b[j]) {
			j++
		}
		if c := compareNumeric(a[:i], b[:j]); c != 0 {
			return c
		}
		a, b = a[i:], b[j:]
	}
	return 0
}

// compareAlpha vergleicht zwei nicht-numerische Abschnitte zeichenweise.
func compareAlpha(a, b string) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var ca, cb int
		if i < len(a) {
			ca = debCharOrder(a[i])
		}
		if i < len(b) {
			cb = debCharOrder(b[i])
		}
		if ca != cb {
			return sign(ca - cb)
		}
	}
	return 0
}

// debCharOrder bildet ein Zeichen auf seinen Rang ab. Die Tilde ist der
// eigentliche Grund für diese Funktion: Sie sortiert VOR dem Ende der
// Zeichenkette, damit "1.0~rc1" kleiner ist als "1.0" - genau so werden
// Vorabversionen ausgedrückt.
func debCharOrder(c byte) int {
	switch {
	case c == '~':
		return -1
	case isAlpha(c):
		return int(c)
	default:
		return int(c) + 256
	}
}

// compareNumeric vergleicht zwei Ziffernfolgen als Zahl, ohne sie zu
// konvertieren - Versionsnummern können jede Länge haben.
func compareNumeric(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return sign(len(a) - len(b))
	}
	return strings.Compare(a, b)
}

// compareRPM vergleicht zwei RPM-Versionen (rpmvercmp).
//
// Verwandt mit dpkg, aber nicht gleich: Es gibt kein Revisions-Konzept am
// letzten Bindestrich, Trennzeichen werden übersprungen statt verglichen,
// und Buchstaben gelten grundsätzlich als kleiner als Ziffern.
func compareRPM(a, b string) int {
	epochA, restA := splitEpoch(a)
	epochB, restB := splitEpoch(b)
	if epochA != epochB {
		return sign(epochA - epochB)
	}
	return compareRPMPart(restA, restB)
}

func compareRPMPart(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		// Die Tilde hat auch in rpm Vorrang-Bedeutung (Vorabversionen).
		ta, tb := strings.HasPrefix(a, "~"), strings.HasPrefix(b, "~")
		if ta || tb {
			if ta != tb {
				if ta {
					return -1
				}
				return 1
			}
			a, b = a[1:], b[1:]
			continue
		}
		// Trennzeichen überspringen: In rpm trennen sie nur, sie bewerten nicht.
		a, b = strings.TrimLeftFunc(a, isSeparator), strings.TrimLeftFunc(b, isSeparator)
		if len(a) == 0 || len(b) == 0 {
			break
		}

		numeric := isDigit(a[0])
		if numeric != isDigit(b[0]) {
			// Ziffern schlagen Buchstaben.
			if numeric {
				return 1
			}
			return -1
		}
		i, j := 0, 0
		for i < len(a) && isDigit(a[i]) == numeric && (numeric || isAlpha(a[i])) {
			i++
		}
		for j < len(b) && isDigit(b[j]) == numeric && (numeric || isAlpha(b[j])) {
			j++
		}
		segA, segB := a[:i], b[:j]
		var c int
		if numeric {
			c = compareNumeric(segA, segB)
		} else {
			c = strings.Compare(segA, segB)
		}
		if c != 0 {
			return c
		}
		a, b = a[i:], b[j:]
	}
	switch {
	case len(a) == len(b):
		return 0
	case len(a) > 0:
		// Eine verbleibende Tilde macht die längere Version zur KLEINEREN.
		if strings.HasPrefix(a, "~") {
			return -1
		}
		return 1
	default:
		if strings.HasPrefix(b, "~") {
			return 1
		}
		return -1
	}
}

// CompareVersions vergleicht zwei Versionen nach dem Verfahren des jeweiligen
// Pakettyps ("deb" oder "rpm").
func CompareVersions(pkgType, a, b string) int {
	if pkgType == "rpm" {
		return compareRPM(a, b)
	}
	return compareDeb(a, b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isSeparator(r rune) bool {
	return !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && r != '~'
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// atoiSafe wandelt eine reine Ziffernfolge; ok=false bei allem anderen.
func atoiSafe(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}
