package services

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Snap-Aktionen: aktualisieren und entfernen.
//
// Snaps waren bisher nur eine Liste. Das ist eine halbe Auskunft: Die Spalte
// „Update" zeigt seit jeher, dass eines bereitliegt - einspielen ließ es sich
// nur auf der Konsole. Die Aktionen entsprechen deshalb denen der
// apt-Pakete: alle aktualisieren, ein einzelnes aktualisieren, eines
// entfernen.
//
// Anders als bei apt gibt es KEINE Versionsauswahl. Ein Snap trägt Revisionen
// und einen Kanal; ein Downgrade läuft über `snap revert` und ist eine eigene
// Sache - hier ein Feld anzubieten, das etwas anderes tut als bei apt, wäre
// irreführend.

var (
	// ErrNoSnaps: keine gültigen Snap-Namen angegeben.
	ErrNoSnaps = errors.New("keine gültigen snap-namen angegeben")
	// ErrInvalidSnap: der Name passt nicht auf die Snap-Namenskonvention.
	ErrInvalidSnap = errors.New("ungültiger snap-name")
	// ErrProtectedSnap: das Entfernen dieses Snaps würde die Snap-Verwaltung
	// selbst oder die Laufzeit aller übrigen Snaps abräumen.
	ErrProtectedSnap = errors.New("dieses snap ist die grundlage der snap-verwaltung und kann nicht über LCM entfernt werden")
)

// reSnapName: Snap-Namen sind kleingeschrieben, dürfen Ziffern und
// Bindestriche enthalten und beginnen mit einem Buchstaben. Alle erlaubten
// Zeichen sind shell-sicher - das ist hier der eigentliche Zweck der Prüfung.
var reSnapName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// protectedSnaps sind die Snaps, an denen die Snap-Verwaltung selbst hängt.
// `snap remove snapd` nimmt die Verwaltung mit, `snap remove core22` allen
// darauf aufbauenden Snaps die Laufzeit.
var protectedSnaps = map[string]bool{
	"snapd": true, "bare": true, "lxd": true, "snap-store": true,
}

// reBaseSnap trifft die Basis-Snaps: core, core18, core22 … Bewusst mit
// Zeilenende - ein Präfix-Vergleich auf „core" hielte auch „coreutils-snap"
// für unantastbar, und das ist ein gewöhnliches Snap.
var reBaseSnap = regexp.MustCompile(`^core[0-9]*$`)

func isProtectedSnap(name string) bool {
	return protectedSnaps[name] || reBaseSnap.MatchString(name)
}

// parseSnapNames zerlegt und prüft eine Namensliste (Komma oder Whitespace),
// dedupliziert in Eingabereihenfolge.
func parseSnapNames(s string) ([]string, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == ';'
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		name := strings.ToLower(strings.TrimSpace(f))
		if name == "" {
			continue
		}
		if !reSnapName.MatchString(name) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidSnap, f)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, ErrNoSnaps
	}
	return out, nil
}

// snapRefreshAllScript aktualisiert alle Snaps.
func snapRefreshAllScript() string { return "snap refresh" }

// snapRefreshScript aktualisiert die genannten Snaps. Die Namen sind
// vorgeprüft (reSnapName) und damit shell-sicher.
func snapRefreshScript(names []string) string {
	return "snap refresh " + strings.Join(names, " ")
}

// snapRemoveScript entfernt die genannten Snaps.
//
// `--purge` bleibt bewusst weg: Ohne das legt snapd vor dem Entfernen eine
// Momentaufnahme der Snap-Daten an, die sich mit `snap restore` zurückholen
// lässt. Genau das will man, wenn sich das Entfernen als Irrtum herausstellt.
func snapRemoveScript(names []string) string {
	return "snap remove " + strings.Join(names, " ")
}
