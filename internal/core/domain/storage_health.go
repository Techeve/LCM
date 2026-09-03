package domain

import "time"

// Speicher-Gesundheit unterhalb der reinen Belegung.
//
// Ein volles Dateisystem sieht man an df. Was man dort NICHT sieht: ein
// ZFS-Pool, der seit drei Wochen im Zustand DEGRADED läuft, weil eine Platte
// ausgefallen ist; Btrfs, das stillschweigend Prüfsummenfehler zählt; ein
// MD-RAID, das ohne Redundanz weiterläuft; ein LVM-Thin-Pool, dessen
// Metadaten volllaufen. Keines dieser Systeme meldet sich von selbst - man
// muss hinsehen, und genau das tut hier niemand regelmäßig.
//
// Deshalb erhebt der Scan diese Zustände mit und macht sie sichtbar.

// Arten überwachter Speicher-Verbünde.
const (
	StorageKindZFS     = "zfs"      // zpool
	StorageKindBtrfs   = "btrfs"    // Btrfs-Dateisystem
	StorageKindMDRaid  = "mdraid"   // Linux-Software-RAID (/proc/mdstat)
	StorageKindLVMThin = "lvm_thin" // LVM-Thin-Pool
)

// Zustände eines Speicher-Verbunds, über alle Arten hinweg vereinheitlicht.
// Die Rohangabe der jeweiligen Technik bleibt daneben in RawState stehen.
const (
	// StorageStateHealthy: keine Beanstandung.
	StorageStateHealthy = "healthy"
	// StorageStateDegraded: läuft noch, aber ohne die vorgesehene Redundanz
	// oder mit gezählten Fehlern. Der Zustand, der am häufigsten übersehen
	// wird - es funktioniert ja alles.
	StorageStateDegraded = "degraded"
	// StorageStateFaulted: nicht mehr benutzbar oder unmittelbar vor
	// Datenverlust.
	StorageStateFaulted = "faulted"
	// StorageStateUnknown: der Zustand ließ sich nicht ermitteln (Werkzeug
	// fehlt, keine Rechte). Bewusst kein Alarm - Nichtwissen ist kein Befund.
	StorageStateUnknown = "unknown"
)

// StorageHealth ist der erhobene Zustand EINES Speicher-Verbunds (Pool,
// Dateisystem, RAID-Verbund) eines Servers.
//
// Reines Scan-Ergebnis: Die Tabelle wird bei jedem Durchgang ersetzt. Es hängt
// keine Konfiguration daran - die Überwachung dieser Zustände ist nicht
// abschaltbar, weil sie nichts kostet und ein DEGRADED-Pool immer eine
// Meldung wert ist.
type StorageHealth struct {
	ID       uint `gorm:"primarykey" json:"id"`
	ServerID uint `gorm:"index" json:"server_id"`

	Kind string `json:"kind"` // StorageKind*
	// Name des Verbunds, z.B. "tank", "md0", "vg0/thinpool".
	// Verschlüsselt at rest wie die übrigen Systemmerkmale.
	Name string `gorm:"serializer:aesgcm" json:"name"`

	// State ist der vereinheitlichte Zustand (StorageState*), RawState die
	// Angabe der Technik selbst ("ONLINE", "DEGRADED", "active", "clean").
	// Beides, weil die Oberfläche über State entscheidet, der Betreiber aber
	// den Originalwert sehen will - danach sucht er in der Dokumentation.
	State    string `json:"state"`
	RawState string `gorm:"serializer:aesgcm" json:"raw_state"`

	// Message fasst zusammen, was nicht stimmt (leer, wenn alles in Ordnung).
	Message string `gorm:"serializer:aesgcm" json:"message"`

	// UsagePercent ist der Füllstand des Verbunds - bei ZFS die
	// Pool-Kapazität, bei LVM-Thin der Datenanteil. 0 = nicht ermittelt.
	//
	// Diese Angabe ist NICHT dieselbe wie die des eingehängten Dateisystems:
	// Ein ZFS-Dataset meldet über df den freien Platz des Pools, und ein
	// Thin-Volume meldet seine virtuelle Größe. Der Pool dahinter kann voll
	// sein, während df noch reichlich Platz zeigt.
	UsagePercent int `json:"usage_percent"`
	// MetaPercent ist der Metadaten-Füllstand (LVM-Thin). Läuft er voll, ist
	// der Pool nicht mehr zu retten - und er läuft VOR den Daten voll.
	MetaPercent int `json:"meta_percent"`
	// FragmentPercent ist die Fragmentierung (ZFS).
	FragmentPercent int `json:"fragment_percent"`

	// Errors ist die Summe der gezählten Fehler (ZFS: read+write+cksum,
	// Btrfs: alle device-stats-Zähler). Kumulativ seit dem letzten
	// Zurücksetzen - jeder Wert über 0 gehört angesehen.
	Errors int64 `json:"errors"`

	// LastScrub ist der Zeitpunkt des letzten Scrubs/Checks (nil = unbekannt
	// oder nie). Ein Pool, der nie geprüft wird, merkt stille Datenfehler
	// erst, wenn jemand die Datei liest - meist zu spät.
	LastScrub *time.Time `json:"last_scrub"`
}

// Beanstandet meldet, ob dieser Zustand eine Meldung wert ist.
func (h *StorageHealth) Beanstandet() bool {
	return h.State == StorageStateDegraded || h.State == StorageStateFaulted
}

// Severity liefert die Dringlichkeit für Befund-Anzeige und Alarm.
func (h *StorageHealth) Severity() string {
	if h.State == StorageStateFaulted {
		return "critical"
	}
	return "warning"
}
