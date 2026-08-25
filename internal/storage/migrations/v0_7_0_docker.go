package migrations

import "gorm.io/gorm"

// v0.7.0 führt das Docker-Monitoring ein: die Tabellen docker_containers/
// docker_images und die Server-Spalten has_docker/has_compose legt
// AutoMigrate an. Diese Migration stempelt vorhandene CVE-Funde auf die
// Quelle "os", damit der quellengetrennte Ersetzungsmechanismus (OS-Scan
// vs. Docker-Scan) Alt-Bestände korrekt zuordnet. Idempotent.
//
// Hinweis: Ursprünglich setzte sie zusätzlich die Settings-Spalten
// docker_check_cron/docker_check_enabled - die gibt es seit v0.10.0
// nicht mehr (der Docker-Check läuft als Rule des System-Sync), daher
// wurden diese Statements entfernt (auf neuen Schemata existieren die
// Spalten nicht).
func init() {
	Register(Migration{
		Version: "0.7.0",
		Name:    "0.7.0-enable-docker-check",
		Run: func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE vulnerabilities
				SET source = 'os'
				WHERE source IS NULL OR source = ''`).Error
		},
	})
}
