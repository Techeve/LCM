package domain

import (
	"fmt"
	"strconv"
	"time"
)

// Stand der Schwachstellen-Datenbank des CVE-Scanners (Trivy).
//
// Warum das ueberhaupt bewertet wird: Trivy laedt seine Datenbank beim Scan
// selbst nach - aber nur mit Netzzugang zur Registry. Ist der LCM-Host
// abgeschottet, haengt hinter einem Proxy oder laeuft in ein Rate-Limit, dann
// WARNT Trivy und scannt mit der alten Datenbank weiter. Das Ergebnis ist dann
// kein Fehler, sondern „keine Sicherheitsluecken gefunden" - also ein falsches
// Grün. Eine drei Wochen alte Datenbank sieht identisch aus wie ein sauberer
// Server; genau diese Verwechslung soll hier unmoeglich werden.
//
// Bezugsgroesse ist UpdatedAt (wann der Hersteller die Datenbank gebaut hat),
// NICHT der Download-Zeitpunkt: Wer dieselbe alte Datenbank taeglich neu
// herunterlaedt, hat trotzdem alte Daten.

// Schwellen der Frische-Bewertung. Trivy erneuert die Datenbank im
// 24-Stunden-Rhythmus (NextUpdate liegt jeweils einen Tag nach UpdatedAt).
//
//   - 48 h ueberlebt bewusst einen ausgefallenen Nachtlauf, ohne dass echtes
//     Verrotten durchrutscht.
//   - Ab 7 Tagen ist die Aussagekraft so weit weg, dass die Ergebnisse nicht
//     mehr als aktuelle Sicherheitsbewertung durchgehen.
const (
	CVEDBStaleAfter    = 48 * time.Hour
	CVEDBCriticalAfter = 7 * 24 * time.Hour
)

// Frische-Stufen der Datenbank.
const (
	CVEDBFresh    = "fresh"    // aktuell
	CVEDBStale    = "stale"    // aelter als CVEDBStaleAfter
	CVEDBCritical = "critical" // aelter als CVEDBCriticalAfter
	CVEDBUnknown  = "unknown"  // kein Scanner oder kein Zeitstempel lesbar
)

// CVEDBStatus beschreibt den Zustand von Scanner und Datenbank.
type CVEDBStatus struct {
	// Available: Ist der Scanner (Trivy-Binary) ueberhaupt einsatzbereit?
	Available bool `json:"available"`
	// Version des Scanner-Binaries, z.B. "0.72.0".
	Version string `json:"version,omitempty"`
	// UpdatedAt: wann der Hersteller diese Datenbank gebaut hat.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// NextUpdate: wann der Hersteller die naechste Ausgabe vorsieht.
	NextUpdate *time.Time `json:"next_update,omitempty"`
	// DownloadedAt: wann dieser Host sie geholt hat. Nur zur Diagnose -
	// fuer die Frische ist UpdatedAt massgeblich.
	DownloadedAt *time.Time `json:"downloaded_at,omitempty"`
	// Freshness ist die abgeleitete Stufe (siehe CVEDBFresh/-Stale/…).
	Freshness string `json:"freshness"`
	// AgeHours ist das Alter der Datenbank in Stunden (abgerundet).
	AgeHours int `json:"age_hours"`
	// Error haelt fest, warum der Stand nicht ermittelbar war.
	Error string `json:"error,omitempty"`
	// Sandboxed: Laeuft der Scanner eingesperrt (siehe Paket sandbox)? Das
	// gehoert sichtbar in die Oberflaeche: ein ungesandboxter Scanner kaeme
	// als Kindprozess von LCM an dessen Datenverzeichnis und damit an
	// Master-Key und Datenbank. Ein stiller Rueckfall waere die schlechteste
	// Variante - dann hielte man sich fuer geschuetzt, ohne es zu sein.
	Sandboxed bool `json:"sandboxed"`
	// SandboxBackend: welcher Mechanismus greift ("bubblewrap"/"landlock").
	SandboxBackend string `json:"sandbox_backend,omitempty"`
	// SandboxNote: bei Sandboxed=false die Ursache im Klartext.
	SandboxNote string `json:"sandbox_note,omitempty"`
}

// EvaluateCVEDB leitet Frische-Stufe und Alter aus dem Zeitstempel ab. now mit
// Zero-Wert deaktiviert die Bewertung (Stufe bleibt „unbekannt") - dieselbe
// Konvention wie bei TrafficLightInput.Now, damit schlanke Tests ohne
// Zeitbezug auskommen.
func (s *CVEDBStatus) EvaluateCVEDB(now time.Time) {
	if !s.Available || s.UpdatedAt == nil || s.UpdatedAt.IsZero() || now.IsZero() {
		s.Freshness = CVEDBUnknown
		s.AgeHours = 0
		return
	}
	age := now.Sub(*s.UpdatedAt)
	if age < 0 {
		// Datenbank aus der Zukunft (schiefe Uhr) - als aktuell werten, aber
		// nicht mit einem negativen Alter herumlaufen.
		age = 0
	}
	s.AgeHours = int(age / time.Hour)
	switch {
	case age >= CVEDBCriticalAfter:
		s.Freshness = CVEDBCritical
	case age >= CVEDBStaleAfter:
		s.Freshness = CVEDBStale
	default:
		s.Freshness = CVEDBFresh
	}
}

// IsStale meldet, ob die Datenbank ueberaltert ist (stale ODER critical).
func (s CVEDBStatus) IsStale() bool {
	return s.Freshness == CVEDBStale || s.Freshness == CVEDBCritical
}

// AgeDescription beschreibt das Alter in Worten - fuer Alarm-Texte und den
// Status-Befund. Bewusst grob: „vor 3 Tagen" ist hier aussagekraeftiger als
// eine Stundenzahl.
// AgeKey ist dieselbe Altersangabe als Übersetzungsschlüssel samt Parameter -
// zusammengesetzte Sätze lassen sich nicht übersetzen, die Bausteine schon.
func (s CVEDBStatus) AgeKey() (string, map[string]string) {
	switch {
	case s.AgeHours >= 48:
		return "cveDbStaleDays", map[string]string{"count": strconv.Itoa(s.AgeHours / 24)}
	case s.AgeHours >= 1:
		return "cveDbStaleHours", map[string]string{"count": strconv.Itoa(s.AgeHours)}
	default:
		return "cveDbStaleRecent", nil
	}
}

func (s CVEDBStatus) AgeDescription() string {
	switch {
	case s.AgeHours >= 48:
		return fmt.Sprintf("vor %d Tagen", s.AgeHours/24)
	case s.AgeHours >= 1:
		return fmt.Sprintf("vor %d Stunden", s.AgeHours)
	default:
		return "vor weniger als einer Stunde"
	}
}
