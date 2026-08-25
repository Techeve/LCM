package trivy

import (
	"context"

	"LCM/internal/core/domain"
)

// runner führt die vier Dinge aus, die LCM von Trivy braucht - und sonst
// nichts. Alles Übrige (SBOM bauen, Ergebnis-Cache, JSON auswerten, den
// DB-Stand zwischenspeichern) liegt eine Ebene darüber in Trivy und gilt für
// jede Ausführungsart gleichermaßen.
//
// Warum diese Naht: Im Container-Betrieb läuft Trivy in einem EIGENEN
// Container - LCMs Runtime-Image ist ein Scratch-Image ohne Shell und ohne
// Trivy-Binary. Getauscht wird damit nur, WIE die JSON-Ausgabe entsteht
// (lokaler Prozess oder HTTP), nicht was danach damit passiert. Ohne die
// Naht müsste der zweite Weg SBOM-Bau, Cache und Parser mitbringen - und
// beide Kopien müssten für immer gleich bleiben.
type runner interface {
	// available meldet, ob dieser Weg überhaupt nutzbar ist.
	available() bool
	// scanSBOM prüft ein CycloneDX-SBOM und liefert die rohe Trivy-Ausgabe.
	scanSBOM(ctx context.Context, sbom []byte) ([]byte, error)
	// scanImage prüft ein Container-Image aus der Registry.
	scanImage(ctx context.Context, ref string) ([]byte, error)
	// info liefert Scanner-Version, DB-Stand und Abschottung.
	info(ctx context.Context) domain.CVEDBStatus
	// updateDB lädt die Schwachstellen-Datenbank; Rückgabe ist die Ausgabe
	// des Werkzeugs fürs Job-Protokoll (auch im Fehlerfall gefüllt).
	updateDB(ctx context.Context) (string, error)
}
