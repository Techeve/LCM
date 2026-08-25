package domain

import (
	"strings"
	"time"
)

// IPAllowlist ist eine benannte, wiederverwendbare Liste von Quell-IPs bzw.
// -Netzen (IPv4/IPv6, inkl. CIDR). Die Listen sind ein gemeinsamer Pool: sie
// sind nicht an ein einzelnes Feature gebunden, sondern lassen sich an
// mehreren Stellen auswählen - als Quell-Einschränkung in Firewall-Regeln
// sowie als ignoreip/Whitelist bei fail2ban und CrowdSec. Verwaltet unter
// Einstellungen → Allowlists.
type IPAllowlist struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Name identifiziert die Liste in der Auswahl (eindeutig).
	Name        string `gorm:"uniqueIndex;not null" json:"name"`
	Description string `json:"description"`
	// Entries sind die kanonisierten Einträge (eine IP oder ein CIDR je Zeile).
	Entries string `json:"entries"`
}

// EntryList liefert die einzelnen Einträge (leere Zeilen verworfen).
func (a IPAllowlist) EntryList() []string {
	var out []string
	for _, line := range strings.Split(a.Entries, "\n") {
		if e := strings.TrimSpace(line); e != "" {
			out = append(out, e)
		}
	}
	return out
}
