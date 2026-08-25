package domain

// DiskVolume ist ein eingehängtes Dateisystem (Speicher-Volume) eines Servers,
// wie es im laufenden System sichtbar ist - also das, was dem System
// durchgereicht wurde, NICHT die physische Platte dahinter. Pseudo-Dateisysteme
// (tmpfs, overlay, squashfs …) werden beim Scan bewusst ausgeschlossen.
//
// Das Root-Volume „/" bleibt der maßgebliche Wert für Dashboard-Ampel,
// Verlauf und Prognose (siehe Server.DiskTotalMB/DiskUsedMB) - es ist der
// kritische Faktor, wenn es voll läuft. Die übrigen Volumes werden zusätzlich
// erfasst und angezeigt, damit man auch deren Belegung im Blick hat.
type DiskVolume struct {
	ID       uint `gorm:"primarykey" json:"id"`
	ServerID uint `gorm:"index" json:"server_id"`

	Mountpoint string `json:"mountpoint"` // z.B. "/", "/data", "/var"
	Device     string `json:"device"`     // z.B. "/dev/sda1", "/dev/mapper/vg-root"
	Fstype     string `json:"fstype"`     // z.B. "ext4", "xfs", "btrfs"
	TotalMB    int64  `json:"total_mb"`
	UsedMB     int64  `json:"used_mb"`
}

// UsagePercent liefert die Belegung dieses Volumes in Prozent (0 wenn unbekannt).
func (v *DiskVolume) UsagePercent() int {
	if v.TotalMB <= 0 {
		return 0
	}
	return int(v.UsedMB * 100 / v.TotalMB)
}

// IsRoot meldet, ob dies das Root-Volume „/" ist - das für Ampel und Prognose
// maßgebliche Speichermedium.
func (v *DiskVolume) IsRoot() bool {
	return v.Mountpoint == "/"
}
