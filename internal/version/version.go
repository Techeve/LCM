// Package version hält die Versionsinformationen der Anwendung.
//
// Die Werte werden beim Build per -ldflags injiziert (siehe Makefile und
// .gitlab-ci.yml):
//
//	-X LCM/internal/version.Version=$(cat VERSION)
//	-X LCM/internal/version.Build=$(cat .buildnumber)
//	-X LCM/internal/version.Commit=$(git rev-parse HEAD)
//
// Die Versionsnummer (Semantic Versioning) pflegt man in der Datei
// VERSION im Projekt-Root; die Build-Nummer (.buildnumber) zählt das
// Makefile bei jedem Build automatisch hoch, in der CI ist es die
// Pipeline-Nummer.
//
// Commit ist der entscheidende Wert für die Nachvollziehbarkeit: Version und
// Build-Nummer sagen NICHT eindeutig, welcher Quellstand läuft - ein lokal
// gebautes Binary kann dieselbe Version tragen wie ein Release und trotzdem
// anderen Code enthalten. Der Commit-SHA beantwortet das zweifelsfrei, und das
// Suffix "-dirty" macht Builds aus einem Arbeitsbaum mit uncommitteten
// Änderungen als solche kenntlich (siehe IsDirty).
package version

import (
	"fmt"
	"strings"
)

var (
	// Version ist die Semantic Version (aus der Datei VERSION).
	Version = "0.0.0-dev"
	// Build ist die fortlaufende Build-Nummer (aus .buildnumber bzw. der
	// CI-Pipeline-Nummer).
	Build = "0"
	// BuiltAt ist der UTC-Zeitpunkt des Builds (RFC 3339).
	BuiltAt = "unbekannt"
	// Commit ist der Git-Commit, aus dem gebaut wurde (voller SHA), mit dem
	// Suffix "-dirty", wenn der Arbeitsbaum uncommittete Änderungen enthielt.
	// Leer = unbekannt (z.B. `go build` ohne -ldflags, `go test`).
	Commit = ""
)

// dirtySuffix markiert Builds aus einem unsauberen Arbeitsbaum.
const dirtySuffix = "-dirty"

// ShortCommit liefert den Commit gekürzt auf 12 Zeichen (inkl. "-dirty",
// falls gesetzt) - lang genug, um im Projekt eindeutig zu sein, kurz genug
// für Footer und Logzeilen. Leer, wenn kein Commit injiziert wurde.
func ShortCommit() string {
	c := strings.TrimSuffix(Commit, dirtySuffix)
	if c == "" {
		return ""
	}
	if len(c) > 12 {
		c = c[:12]
	}
	if IsDirty() {
		return c + dirtySuffix
	}
	return c
}

// IsDirty meldet, ob aus einem Arbeitsbaum mit uncommitteten Änderungen
// gebaut wurde. Ein solches Binary ist KEIN reproduzierbarer Release-Stand:
// sein Verhalten lässt sich aus keinem Commit des Repositorys ableiten.
func IsDirty() bool { return strings.HasSuffix(Commit, dirtySuffix) }

// String liefert die menschenlesbare Vollversion, z.B.
// "1.5.1 (Build 42, a1b2c3d4e5f6)". Ohne injizierten Commit bleibt es beim
// bisherigen Format "1.5.1 (Build 42)".
func String() string {
	if c := ShortCommit(); c != "" {
		return fmt.Sprintf("%s (Build %s, %s)", Version, Build, c)
	}
	return fmt.Sprintf("%s (Build %s)", Version, Build)
}
