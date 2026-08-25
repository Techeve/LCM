package migrations

import "gorm.io/gorm"

// v0.3.0 führt die Multi-Distro-Paketverwaltung ein (apt/dnf/yum/zypper) samt
// Snap-Erfassung, Virtualisierungs- und OS-Support-Erkennung. Das Schema
// (neue Spalten an servers, Tabelle snap_packages) legt AutoMigrate an; hier
// wird nur der Bestand nachgezogen:
//
// Backfill des Feldes servers.package_manager aus der bereits gescannten
// Distribution (os_id/os_name). So zeigen bestehende Server ihre
// Paketverwaltung sofort nach dem Update an, ohne auf einen erneuten Scan
// warten zu müssen. Snaps, Virtualisierung und OS-Support-Status füllen sich
// beim nächsten Reconnect bzw. beim täglichen System-Sync.
//
// Idempotent: gesetzt wird nur, wo package_manager noch leer ist.
func init() {
	Register(Migration{
		Version: "0.3.0",
		Name:    "0.3.0-backfill-package-manager",
		Run: func(tx *gorm.DB) error {
			empty := "(package_manager IS NULL OR package_manager = '')"
			// Debian/Ubuntu & Derivate → apt.
			if err := tx.Exec(`UPDATE servers SET package_manager = 'apt' WHERE ` + empty + `
				AND (lower(os_id) IN ('ubuntu','debian','linuxmint','pop','raspbian','devuan','kali')
				     OR lower(os_name) LIKE '%ubuntu%' OR lower(os_name) LIKE '%debian%'
				     OR lower(os_name) LIKE '%mint%')`).Error; err != nil {
				return err
			}
			// RHEL-Familie → dnf.
			if err := tx.Exec(`UPDATE servers SET package_manager = 'dnf' WHERE ` + empty + `
				AND (lower(os_id) IN ('rhel','centos','rocky','almalinux','fedora','ol','amzn')
				     OR lower(os_name) LIKE '%red hat%' OR lower(os_name) LIKE '%centos%'
				     OR lower(os_name) LIKE '%rocky%' OR lower(os_name) LIKE '%alma%'
				     OR lower(os_name) LIKE '%fedora%')`).Error; err != nil {
				return err
			}
			// SUSE → zypper.
			if err := tx.Exec(`UPDATE servers SET package_manager = 'zypper' WHERE ` + empty + `
				AND (lower(os_id) LIKE '%suse%' OR lower(os_name) LIKE '%suse%')`).Error; err != nil {
				return err
			}
			// has_snap: wer bereits erfasste Snaps hat, hat snapd - für alle
			// anderen füllt es der nächste Scan (Reconnect/System-Sync).
			return tx.Exec(`UPDATE servers SET has_snap = 1 WHERE id IN
				(SELECT DISTINCT server_id FROM snap_packages)`).Error
		},
	})
}
