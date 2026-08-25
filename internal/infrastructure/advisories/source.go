// Package advisories fragt Online-Schwachstellenquellen nach Befunden zu
// installierten Paketen - die schnelle Spur neben dem Trivy-Scan.
//
// Warum es diese zweite Spur gibt: Trivy wertet eine lokale Datenbank aus,
// die der Hersteller alle 6 Stunden baut und die der Betreiber danach erst
// herunterladen muss. Eine gestern veröffentlichte Meldung erreicht den
// Server damit frühestens nach Stunden. Für den Fall, um den es hier geht -
// ein bösartiges Paket oder eine gerade bekannt gewordene Lücke, die eine
// sofortige Entscheidung verlangt - ist das zu spät. Die Quellen dieses
// Pakets werden direkt abgefragt und liefern binnen Minuten.
package advisories

import (
	"context"
	"time"
)

// Advisory ist eine Meldung einer Quelle zu einem Paket.
type Advisory struct {
	// ID ist die Kennung der Quelle: CVE-…, GHSA-…, MAL-…
	ID string
	// Modified ist der Änderungsstempel der Quelle - daran hängt, ob eine
	// gespeicherte Beschreibung noch gilt.
	Modified time.Time
}

// Detail ist die Beschreibung einer Meldung.
type Detail struct {
	ID       string
	Severity string // normalisiert klein (critical|high|medium|low|unknown)
	Title    string
	URL      string
	Modified time.Time
	// FixedVersions bildet Paketnamen auf die behebende Version ab. Leer
	// heißt: kein Fix bekannt.
	FixedVersions map[string]string
	// Aliases sind weitere Kennungen derselben Sache. Sie sind für die
	// Anreicherung unverzichtbar: Distributionen führen ihre Meldungen unter
	// eigenen Nummern (DSA-…, USN-…), und nur über die Aliase lässt sich eine
	// solche Meldung mit einer CVE-bezogenen Liste abgleichen.
	Aliases []string
}

// Source ist eine abfragbare Schwachstellenquelle.
//
// Bewusst dieselbe Form wie trivy.Scanner: eine schmale Schnittstelle, hinter
// der die echte Implementierung und ein Fake für die Tests stehen. Der
// aufrufende Dienst kennt weder HTTP noch das Format der jeweiligen Quelle.
type Source interface {
	// Name ist die Kennung der Quelle (domain.AdvisorySource*).
	Name() string
	// Available meldet, ob die Quelle nutzbar ist (verdrahtet und aktiviert).
	Available() bool
	// Query liefert je purl die bekannten Meldungen. Ein purl ohne Eintrag im
	// Ergebnis gilt als geprüft und unauffällig - das ist der Normalfall und
	// nicht von einem Fehler zu unterscheiden, wenn man ihn nicht so
	// behandelt; Fehler kommen deshalb ausschließlich über err.
	Query(ctx context.Context, purls []string) (map[string][]Advisory, error)
	// Details holt die Beschreibungen zu den Kennungen nach.
	Details(ctx context.Context, ids []string) (map[string]Detail, error)
}
