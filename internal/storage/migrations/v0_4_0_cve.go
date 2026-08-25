package migrations

import "gorm.io/gorm"

// v0.4.0 führt den CVE-Scan (Trivy) ein: neue Tabelle vulnerabilities und die
// Server-Spalten last_cve_scan_at/cve_scan_error legt AutoMigrate an; die
// Settings-Spalten cve_scan_enabled/cve_scan_cron ebenso (mit gorm-Defaults,
// die aber nur für NEUE Zeilen greifen).
//
// Diese Migration zieht die bestehende (einzige) Settings-Zeile nach: Sie
// aktiviert den täglichen CVE-Scan und setzt einen Standard-Zeitplan, falls
// noch keiner hinterlegt ist. So läuft der Scan nach dem Update ohne manuelles
// Zutun an (Trivy-Verfügbarkeit vorausgesetzt; sonst greift der graceful
// degrade). Idempotent: nur leere/NULL-Cron-Werte werden gefüllt.
func init() {
	Register(Migration{
		Version: "0.4.0",
		Name:    "0.4.0-enable-cve-scan",
		Run: func(tx *gorm.DB) error {
			if err := tx.Exec(`UPDATE global_settings
				SET cve_scan_cron = '0 4 * * *'
				WHERE cve_scan_cron IS NULL OR cve_scan_cron = ''`).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE global_settings
				SET cve_scan_enabled = 1
				WHERE cve_scan_enabled IS NULL`).Error
		},
	})
}
