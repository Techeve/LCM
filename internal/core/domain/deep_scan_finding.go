package domain

import (
	"time"

	"gorm.io/gorm"
)

// Deep-Scan-Kategorien: welcher Teil des Deep Scans einen Befund erzeugt hat.
const (
	DeepScanKernel    = "kernel"    // Kernel-CVE / laufender vs. installierter Kernel
	DeepScanRestart   = "restart"   // Dienste mit alten Bibliotheken (needrestart)
	DeepScanMisconfig = "misconfig" // Härtung/Fehlkonfiguration (Lynis oder Eigenprüfung)
)

// Deep-Scan-Schweregrade - bewusst dieselben drei Stufen wie StatusInsight,
// damit Befunde direkt in die Ampel/Insights einfließen können.
const (
	DeepScanInfo     = "info"
	DeepScanWarning  = "warning"
	DeepScanCritical = "critical"
)

// DeepScanFinding ist ein einzelner Befund eines Deep Scans auf einem Server
// (Kernel-Reboot-Lücke, Dienst mit alten Bibliotheken, Härtungs-Warnung/
// -Empfehlung). Jeder Befund gehört zu genau einem Lauf (ReportID) - so
// bleibt nachvollziehbar, wann er zum ersten Mal auftrat und ob er im
// nächsten Lauf noch da war (siehe DeepScanReport).
type DeepScanFinding struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID
	CreatedAt time.Time `json:"created_at"`

	ServerID uint `gorm:"not null;index:idx_deepscan_server" json:"server_id"`
	// ReportID ordnet den Befund seinem Lauf zu.
	ReportID string `gorm:"type:text;index:idx_deepscan_report" json:"report_id"`
	// IsNew: dieser Befund stand im vorhergehenden Lauf noch nicht. Beim
	// allerersten Lauf ist das Feld bei allen Befunden false - dort ist
	// nichts „neu", es ist schlicht der erste Stand.
	IsNew bool `json:"is_new"`

	// Category: kernel|restart|misconfig.
	Category string `gorm:"index" json:"category"`
	// Severity: info|warning|critical.
	Severity string `gorm:"index" json:"severity"`
	// Title ist die kurze Befund-Zeile; Detail optional ausführlicher.
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Ref ist eine optionale Quelle (z.B. eine Lynis-Test-ID oder eine URL).
	Ref string `json:"ref"`
	// Tool hält fest, woher der Befund stammt (needrestart|lynis|lcm).
	Tool string `json:"tool"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (f *DeepScanFinding) BeforeCreate(*gorm.DB) error {
	if f.ID == "" {
		f.ID = newUUID()
	}
	return nil
}

// DeepScanSeverityAtLeast meldet, ob sev mindestens warning-schwer ist (für die
// Ampel-Kopplung: nur echte Warnungen/kritische Befunde zählen, info nicht).
func DeepScanSeverityAtLeast(sev, min string) bool {
	rank := map[string]int{DeepScanCritical: 0, DeepScanWarning: 1, DeepScanInfo: 2}
	return rank[sev] <= rank[min]
}
