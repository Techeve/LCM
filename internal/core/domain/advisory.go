package domain

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// Herkunft eines Frühwarn-Befunds.
const (
	// AdvisorySourceOSV ist die Online-Abfrage gegen api.osv.dev - die
	// schnelle Spur: dort stehen GitHub-Advisories und Schadpaket-Meldungen
	// binnen Minuten, während die Trivy-Datenbank erst alle 6 Stunden gebaut
	// und beim Betreiber eingespielt wird.
	AdvisorySourceOSV = "osv"
	// AdvisorySourceEUVD ist die EU-Schwachstellendatenbank der ENISA. Sie
	// erzeugt keine eigenen Befunde, sondern reichert vorhandene um das
	// Signal „wird aktiv ausgenutzt" an (Etappe C).
	AdvisorySourceEUVD = "euvd"
)

// Art eines Befunds. Die Unterscheidung ist keine Kosmetik: Eine
// Sicherheitslücke bewertet man nach Schwere und plant das Update ein; ein
// bösartiges Paket ist immer ein Sofortfall, unabhängig von jeder Schwere-
// Angabe - es gehört vom Server, nicht auf die Liste.
const (
	AdvisoryKindVulnerability = "vulnerability"
	AdvisoryKindMalware       = "malware"
)

// malwarePrefix kennzeichnet Schadpaket-Meldungen im OSV-Bestand
// (OpenSSF Malicious Packages, IDs der Form "MAL-2026-1234").
const malwarePrefix = "MAL-"

// AdvisoryKindFor leitet die Art aus der Advisory-Kennung ab.
func AdvisoryKindFor(advisoryID string) string {
	if strings.HasPrefix(strings.ToUpper(advisoryID), malwarePrefix) {
		return AdvisoryKindMalware
	}
	return AdvisoryKindVulnerability
}

// AdvisoryFinding ist ein Frühwarn-Befund aus einer Online-Quelle zu einem
// installierten Paket.
//
// Warum eine eigene Tabelle neben Vulnerability: Der CVE-Bestand aus dem
// Trivy-Scan wird bei jedem Lauf vollständig ersetzt - er beschreibt einen
// Zustand, keinen Verlauf. Für eine Frühwarnung ist der Verlauf aber die
// eigentliche Information: FirstSeenAt trägt die Aussage „seit wann wissen
// wir davon", und nur weil sie erhalten bleibt, lässt sich ein neuer Fund von
// einem seit Tagen bekannten unterscheiden. Ein Ersetzen-je-Lauf würde jeden
// Poll-Durchgang zu lauter „neuen" Funden machen.
type AdvisoryFinding struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ServerRef ist das deterministische Server-Token (HMAC, siehe
	// ServerRef im Repository-Paket) - wie bei Vulnerability verrät die
	// Datenbank die Zuordnung Befund→Server damit nicht im Klartext.
	ServerRef string `gorm:"not null;index:idx_adv_server;index:idx_adv_unique,unique" json:"-"`

	// Source: AdvisorySource* - welche Quelle den Befund gemeldet hat.
	Source string `gorm:"not null;index:idx_adv_unique,unique" json:"source"`
	// AdvisoryID ist die Kennung der Quelle: CVE-…, GHSA-…, MAL-…, EUVD-…
	AdvisoryID string `gorm:"not null;index:idx_adv_unique,unique" json:"advisory_id"`
	// Kind: AdvisoryKind* - abgeleitet aus der Kennung (siehe AdvisoryKindFor).
	Kind string `gorm:"not null;index" json:"kind"`

	PackageName      string `gorm:"not null;index:idx_adv_unique,unique" json:"package_name"`
	InstalledVersion string `json:"installed_version"`
	// FixedVersion ist die Version, die den Befund behebt. Leer heißt: kein
	// Fix bekannt - dann bleibt nur, die Version einzufrieren oder das Paket
	// zu entfernen.
	FixedVersion string `json:"fixed_version"`
	// Severity: critical|high|medium|low|unknown (normalisiert klein).
	Severity string `gorm:"index" json:"severity"`
	Title    string `json:"title"`
	URL      string `json:"url"`

	// Exploited meldet, dass die Lücke nachweislich aktiv ausgenutzt wird
	// (EUVD-Anreicherung, Etappe C).
	Exploited bool `gorm:"default:false" json:"exploited"`

	// FirstSeenAt ist der Zeitpunkt, zu dem LCM den Befund zum ersten Mal
	// gesehen hat - nicht das Veröffentlichungsdatum der Quelle.
	FirstSeenAt time.Time `gorm:"index" json:"first_seen_at"`
	// ResolvedAt ist gesetzt, sobald der Befund nicht mehr auftaucht (Update
	// eingespielt, Paket entfernt). Der Eintrag bleibt als Historie stehen;
	// taucht derselbe Befund wieder auf, wird er wiedereröffnet statt doppelt
	// angelegt.
	ResolvedAt *time.Time `gorm:"index" json:"resolved_at"`
	// AcknowledgedBy hält fest, wer den Befund zur Kenntnis genommen hat.
	// Ein bestätigter Befund löst keinen Alarm mehr aus - das ist das
	// Ventil für „bekannt, bewusst so": ohne es müsste man die ganze Regel
	// abschalten, um einen einzelnen Dauerbefund stummzustellen.
	AcknowledgedBy string     `json:"acknowledged_by"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (a *AdvisoryFinding) BeforeCreate(*gorm.DB) error {
	if a.ID == "" {
		a.ID = newUUID()
	}
	return nil
}

// Acknowledged meldet, ob der Befund zur Kenntnis genommen wurde.
func (a *AdvisoryFinding) Acknowledged() bool { return a.AcknowledgedBy != "" }

// EffectiveSeverity liefert die Schwere für Anzeige und Alarm. Schadpakete
// gelten IMMER als kritisch: Die Quellen führen für sie meist gar keine
// Schwere, und eine fehlende Angabe („unknown") würde einen Volltreffer -
// Schadcode auf dem Server - unter jeder Schwelle durchrutschen lassen.
func (a *AdvisoryFinding) EffectiveSeverity() string {
	if a.Kind == AdvisoryKindMalware {
		return SeverityCritical
	}
	if a.Severity == "" {
		return SeverityUnknown
	}
	return a.Severity
}
