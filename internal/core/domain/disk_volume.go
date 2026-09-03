package domain

import "strings"

// DiskVolume ist ein eingehängtes Dateisystem (Speicher-Volume) eines Servers,
// wie es im laufenden System sichtbar ist - also das, was dem System
// durchgereicht wurde, NICHT die physische Platte dahinter. Pseudo-Dateisysteme
// (tmpfs, overlay, squashfs …) werden beim Scan bewusst ausgeschlossen.
//
// Das Root-Volume „/" bleibt der maßgebliche Wert für Dashboard-Ampel,
// Verlauf und Prognose (siehe Server.DiskTotalMB/DiskUsedMB) - es ist der
// kritische Faktor, wenn es voll läuft. Die übrigen Volumes werden zusätzlich
// erfasst und angezeigt; überwacht werden sie nur auf ausdrückliche Ansage
// (siehe VolumeMonitor).
type DiskVolume struct {
	ID       uint `gorm:"primarykey" json:"id"`
	ServerID uint `gorm:"index" json:"server_id"`

	Mountpoint string `json:"mountpoint"` // z.B. "/", "/data", "/var"
	Device     string `json:"device"`     // z.B. "/dev/sda1", "/dev/mapper/vg-root"
	Fstype     string `json:"fstype"`     // z.B. "ext4", "xfs", "btrfs"
	TotalMB    int64  `json:"total_mb"`
	UsedMB     int64  `json:"used_mb"`

	// Inodes: Ein Dateisystem kann dichtmachen, obwohl reichlich Platz frei
	// ist - wenn die Inodes erschöpft sind. Typisch bei vielen kleinen
	// Dateien (Maildirs, Session-Dateien, Paket-Caches). Fällt ohne eigene
	// Messung niemandem auf, weil df in der üblichen Form nur Bytes zeigt.
	//
	// 0 = nicht ermittelt. ZFS und Btrfs vergeben Inodes dynamisch: Sie
	// melden zwar eine Zahl, aber eine gerechnete - ein ZFS-Dataset mit 32 GB
	// nennt 65 Millionen Inodes bei 1% Belegung. Die Quote wird dort also nie
	// aussagekräftig und der Befund schlägt schlicht nie an; das ist richtig
	// so, denn erschöpfen kann sich dieser Vorrat auch nicht.
	InodesTotal int64 `json:"inodes_total"`
	InodesUsed  int64 `json:"inodes_used"`

	// ReadOnly: Das Dateisystem ist nur lesbar eingehängt. Bei einem Volume,
	// das beschreibbar sein soll, ist das der sichtbarste Notruf, den der
	// Kernel absetzt: Nach einem I/O- oder Metadatenfehler hängt er das
	// Dateisystem selbsttätig auf „ro" um, damit kein weiterer Schaden
	// entsteht. Wer nicht hinsieht, merkt es erst am ersten Schreibfehler.
	ReadOnly bool `json:"read_only"`
}

// UsagePercent liefert die Belegung dieses Volumes in Prozent (0 wenn unbekannt).
func (v *DiskVolume) UsagePercent() int {
	if v.TotalMB <= 0 {
		return 0
	}
	return int(v.UsedMB * 100 / v.TotalMB)
}

// InodeUsagePercent liefert die Inode-Belegung in Prozent (0 wenn das
// Dateisystem keine meldet).
func (v *DiskVolume) InodeUsagePercent() int {
	if v.InodesTotal <= 0 {
		return 0
	}
	return int(v.InodesUsed * 100 / v.InodesTotal)
}

// IsRoot meldet, ob dies das Root-Volume „/" ist - das für Ampel und Prognose
// maßgebliche Speichermedium.
func (v *DiskVolume) IsRoot() bool {
	return v.Mountpoint == "/"
}

// networkFstypes sind Dateisysteme, deren Speicher auf einem ANDEREN System
// liegt. Sie werden weiter angezeigt - man will sehen, was eingehängt ist -,
// sind aber bewusst nicht überwachbar: Für Füllstand und Gesundheit ist der
// Storage zuständig, der sie anbietet. LCM sähe von hier aus nur die Sicht des
// Clients und würde bei jedem Netz-Aussetzer Alarm schlagen, ohne dass am
// Speicher selbst etwas wäre.
//
// Der Vergleich läuft über den Präfix vor dem Punkt, damit "fuse.sshfs" wie
// "sshfs" behandelt wird.
var networkFstypes = map[string]bool{
	"nfs": true, "nfs3": true, "nfs4": true, "cifs": true, "smbfs": true,
	"smb3": true, "sshfs": true, "glusterfs": true, "ceph": true,
	"afs": true, "9p": true, "davfs": true, "davfs2": true, "beegfs": true,
	"lustre": true, "ocfs2": true, "orangefs": true, "s3fs": true, "rclone": true,
}

// IsNetwork meldet, ob das Volume über das Netz eingehängt ist.
func (v *DiskVolume) IsNetwork() bool {
	fs := strings.ToLower(strings.TrimSpace(v.Fstype))
	if rest, ok := strings.CutPrefix(fs, "fuse."); ok {
		fs = rest
	}
	return networkFstypes[fs]
}

// Monitorable meldet, ob dieses Volume zur Überwachung ANGEBOTEN werden darf.
// Netz-Mounts sind ausgenommen (siehe networkFstypes), ebenso Volumes ohne
// verwertbare Kapazität - und das Root-Volume: Es ist immer überwacht
// (Server.DiskUsagePercent, Verlauf, Prognose). Ein Schalter dafür würde
// denselben Sachverhalt ein zweites Mal melden.
func (v *DiskVolume) Monitorable() bool {
	return !v.IsNetwork() && !v.IsRoot() && v.TotalMB > 0
}

// --- Überwachung einzelner Volumes -------------------------------------------

// VolumeMonitorDefaultPercent ist die Vorgabe, wenn beim Einschalten der
// Überwachung keine eigene Grenze gesetzt wird - dieselbe wie für „/".
const VolumeMonitorDefaultPercent = DiskWarningPercent

// VolumeMonitor ist die Überwachungs-Einstellung für EIN Volume eines Servers.
//
// Warum eine eigene Tabelle und keine Spalten an DiskVolume: disk_volumes ist
// ein Scan-Ergebnis und wird bei jedem Durchgang vollständig ersetzt
// (ReplaceDiskVolumes). Eine Einstellung, die dort steht, wäre nach dem
// nächsten Scan weg. Konfiguration und erhobener Zustand gehören getrennt.
//
// Der Schlüssel ist der Mountpoint, nicht die Volume-ID: Die ID wechselt bei
// jedem Scan, der Mountpoint ist das, was der Betreiber gemeint hat. Wird ein
// Volume ausgehängt und später wieder eingehängt, gilt die Einstellung wieder -
// genau wie erwartet.
type VolumeMonitor struct {
	ID       uint `gorm:"primarykey" json:"id"`
	ServerID uint `gorm:"index:idx_volume_monitor_server_mount,unique,priority:1" json:"server_id"`
	// Mountpoint bleibt im Klartext - anders als die Systemmerkmale des
	// Servers. Er steht ohnehin unverschlüsselt in disk_volumes daneben; ihn
	// hier zu verschlüsseln schützte nichts, machte aber den Unique-Index
	// wirkungslos: AES-GCM verschlüsselt mit Zufalls-Nonce, derselbe Pfad
	// ergäbe jedes Mal einen anderen Wert und die Sperre gegen Doppel-
	// einträge liefe ins Leere.
	Mountpoint string `gorm:"index:idx_volume_monitor_server_mount,unique,priority:2" json:"mountpoint"`

	// WarnPercent/CritPercent sind die Belegungsgrenzen in Prozent.
	// CritPercent 0 = keine eigene kritische Grenze.
	WarnPercent int `json:"warn_percent"`
	CritPercent int `json:"crit_percent"`

	// InodeWarnPercent: eigene Grenze für die Inode-Belegung. 0 = die
	// Belegungsgrenze gilt auch hier.
	InodeWarnPercent int `json:"inode_warn_percent"`
}

// EffectiveWarnPercent liefert die wirksame Warngrenze.
func (m *VolumeMonitor) EffectiveWarnPercent() int {
	if m.WarnPercent <= 0 {
		return VolumeMonitorDefaultPercent
	}
	return m.WarnPercent
}

// EffectiveInodeWarnPercent liefert die wirksame Inode-Warngrenze.
func (m *VolumeMonitor) EffectiveInodeWarnPercent() int {
	if m.InodeWarnPercent <= 0 {
		return m.EffectiveWarnPercent()
	}
	return m.InodeWarnPercent
}
