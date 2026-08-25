package version

import (
	"strconv"
	"strings"
)

// Compare vergleicht zwei Semantic Versions ("major.minor.patch" mit
// optionalem Prerelease-Suffix wie "-dev" oder "-rc1").
// Rückgabe: -1 wenn a < b, 0 wenn gleich, +1 wenn a > b.
//
// Prerelease-Versionen gelten bei gleichen Nummern als KLEINER als die
// Release-Version (1.2.0-rc1 < 1.2.0), gemäß SemVer-Spezifikation.
// Nicht parsebare Versionen gelten als 0.0.0.
//
// Erkannt wird auch die DEBIAN-Schreibweise mit Tilde (1.30.0~beta.1). Das
// apt-Repository trägt sie so, weil apt nur damit die Beta VOR das spätere
// Finale sortiert - und die Update-Prüfung liest ihre Zahlen genau von dort.
// Ohne diese Zeile las sie jedes Beta als 0.0.0 und meldete nie ein Update.
func Compare(a, b string) int {
	na, prea := parse(a)
	nb, preb := parse(b)
	for i := range 3 {
		if na[i] != nb[i] {
			if na[i] < nb[i] {
				return -1
			}
			return 1
		}
	}
	// Nummern gleich: Prerelease < Release.
	switch {
	case prea == preb:
		return 0
	case prea != "" && preb == "":
		return -1
	case prea == "" && preb != "":
		return 1
	default:
		return comparePrerelease(prea, preb)
	}
}

// comparePrerelease vergleicht zwei Prerelease-Angaben punktweise und
// Zahlenblöcke als Zahlen.
//
// Zeichenweise verglichen stünde beta.10 VOR beta.9 - die zehnte Beta wäre
// damit kein Update gegenüber der neunten. Bei einem Zug, der über Wochen
// Betas zählt, ist das kein Randfall.
func comparePrerelease(a, b string) int {
	fa, fb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(fa) || i < len(fb); i++ {
		// Die kürzere Angabe ist die kleinere: beta < beta.1.
		if i >= len(fa) {
			return -1
		}
		if i >= len(fb) {
			return 1
		}
		na, erra := strconv.Atoi(fa[i])
		nb, errb := strconv.Atoi(fb[i])
		switch {
		case erra == nil && errb == nil:
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
		default:
			// Gemischt oder beides Text: zeichenweise. Eine Zahl gilt dabei
			// als kleiner als Text - so will es die SemVer-Spezifikation.
			if c := strings.Compare(fa[i], fb[i]); c != 0 {
				return c
			}
		}
	}
	return 0
}

// IsRelease meldet, ob v eine parsebare Release-Version ohne
// Prerelease-Suffix ist (z.B. "1.2.0", nicht "0.0.0-dev").
func IsRelease(v string) bool {
	nums, pre := parse(v)
	return pre == "" && (nums[0] > 0 || nums[1] > 0 || nums[2] > 0)
}

// parse zerlegt "1.2.3-rc1" in [1 2 3] und "rc1" - und ebenso die
// Debian-Schreibweise "1.2.3~rc1", in der das apt-Repository seine Vorabversionen
// führt.
func parse(v string) ([3]int, string) {
	var nums [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// Was zuerst kommt, trennt: beide Zeichen leiten dasselbe ein.
	trenner := strings.IndexAny(v, "-~")
	core, pre := v, ""
	if trenner >= 0 {
		core, pre = v[:trenner], v[trenner+1:]
	}
	parts := strings.Split(core, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{}, ""
		}
		nums[i] = n
	}
	return nums, pre
}
