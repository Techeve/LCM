package domain

import (
	"time"

	"gorm.io/gorm"
)

// DeepScanReport ist EIN Deep-Scan-Lauf: ein datierter, in sich abgeschlossener
// Bericht mit den Befunden dieses Zeitpunkts.
//
// Zuvor hielt LCM nur den jeweils letzten Befundbestand - eine flache Liste
// ohne Datum. Wer denselben Server zweimal scannte, konnte nicht erkennen,
// was neu dazugekommen und was seit dem letzten Mal behoben war; jeder Lauf
// überschrieb die Vorgeschichte. Genau diese Frage - „habe ich etwas
// erreicht?" - beantwortet erst der Vergleich zweier Läufe.
type DeepScanReport struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID
	CreatedAt time.Time `json:"created_at"`

	ServerID uint `gorm:"not null;index:idx_dsreport_server" json:"server_id"`

	// Zählung der Befunde dieses Laufs, nach Schwere.
	Critical int `json:"critical"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`

	// Fortschritt gegenüber dem unmittelbar vorhergehenden Report desselben
	// Servers. Beim allerersten Lauf sind beide 0 - es gibt nichts zu
	// vergleichen, und „12 neue Befunde" wäre dort eine irreführende Aussage.
	NewFindings      int `json:"new_findings"`
	ResolvedFindings int `json:"resolved_findings"`
	// ResolvedTitles hält fest, WAS verschwunden ist (JSON-Array von Titeln).
	// Ohne diese Liste bliebe der Fortschritt eine nackte Zahl; mit ihr sieht
	// man, welche Härtungslücke man tatsächlich geschlossen hat.
	ResolvedTitles string `json:"resolved_titles"`

	// Zustand zum Zeitpunkt des Laufs - festgehalten, damit ein alter Report
	// auch dann noch stimmt, wenn sich der Server längst geändert hat.
	HardeningIndex      *int   `json:"hardening_index"`
	KernelRebootPending bool   `json:"kernel_reboot_pending"`
	Tools               string `json:"tools"` // z.B. "needrestart,lynis"

	// Findings dieses Laufs (per ReportID verknüpft).
	Findings []DeepScanFinding `gorm:"foreignKey:ReportID;constraint:OnDelete:CASCADE" json:"findings,omitempty"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (r *DeepScanReport) BeforeCreate(*gorm.DB) error {
	if r.ID == "" {
		r.ID = newUUID()
	}
	return nil
}

// DeepScanFindingKey ist die Identität eines Befunds ÜBER Läufe hinweg. Sie
// entscheidet, ob ein Befund im neuen Lauf „derselbe" ist wie im alten -
// Grundlage für neu/behoben. Die Befund-ID taugt dafür nicht: sie wird je Lauf
// neu vergeben. Kategorie und Titel zusammen sind stabil und beschreiben die
// Sache; die Schwere bleibt bewusst außen vor, damit eine hochgestufte
// Warnung nicht zugleich als „behoben" und „neu" erscheint.
func DeepScanFindingKey(f DeepScanFinding) string {
	return f.Category + "\x00" + f.Title
}
