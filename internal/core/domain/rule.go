package domain

import "time"

// Regel-Typen: Was eine Rule bei Ausführung auf den Servern ihrer
// Gruppe tut.
const (
	RuleTypeUpdate      = "update"       // alle Pakete aktualisieren (apt-get upgrade)
	RuleTypePackages    = "packages"     // nur benannte Pakete aktualisieren; Command = Paketnamen (kommagetrennt)
	RuleTypeSecurity    = "security"     // nur Security-/Bugfix-Updates (aus -security-Quellen)
	RuleTypePackageScan = "package-scan" // Paketliste aktualisieren: apt-get update + Bestandsaufnahme (installiert nichts)
	RuleTypeAutoremove  = "autoremove"   // nicht mehr benötigte Pakete entfernen (apt autoremove und Pendants)
	RuleTypeScript      = "script"       // spezifisches Command/Skript ausführen
	RuleTypeCustom      = "custom"       // benutzerdefinierte Aktion (Command-Liste); Command = CustomAction-ID
	RuleTypeSync        = "sync"         // User-Zertifikate + Hardware-Daten synchronisieren
	RuleTypeHealth      = "health"       // reiner Verfügbarkeits-Check (Ping/SSH)
	RuleTypeFirewall    = "firewall"     // Firewall (ufw/firewalld/nftables) durchsetzen; Command = JSON-Array von FirewallRule (Legacy: CSV-Portliste)
	RuleTypeBackup      = "backup"       // System-Backup des LCM selbst (nur System-Gruppe)
	RuleTypeCleanup     = "cleanup"      // Log-Retention: alte Jobs/Outputs löschen
	RuleTypeCVEScan     = "cve-scan"     // CVE-Scan (Trivy) des Paketbestands aller Server
	RuleTypeDockerCheck = "docker-check" // zentraler Docker-Lauf: Registry-Update-Check + Image-CVE-Scan
	// RuleTypeAppCheck fragt für jede Anwendung im Katalog die neueste
	// Version bei ihrer Quelle ab. Ein zentraler Lauf, kein Server-Lauf: Die
	// neueste Version hängt an der Anwendung, nicht am einzelnen Server.
	RuleTypeAppCheck = "app-check"

	// RuleTypeDockerUpdate ist der Job-Typ der Docker-Aktionen
	// (compose pull && up -d bzw. docker pull) aus der Server-Detailansicht.
	RuleTypeDockerUpdate = "docker-update"

	// RuleTypeDockerPrune räumt ungenutzte Docker-Images auf
	// (docker image prune -af) - als geplante Gruppen-Regel.
	RuleTypeDockerPrune = "docker-prune"

	// RuleTypeDockerUpdateUnused zieht neue Versionen UNGENUTZTER Images
	// (kein Container referenziert sie), für die der Registry-Check ein
	// Update gemeldet hat - als geplante Gruppen-Regel. Genutzte Images
	// bleiben unberührt (deren Update erfolgt bewusst über Compose/Pull).
	RuleTypeDockerUpdateUnused = "docker-update-unused"

	// RuleTypeAptProxy bindet die Server der Gruppe an den zentralen
	// APT-Cache an (Drop-in mit Acquire::http/https::Proxy) - als geplante
	// Regel oder als Grundsatz-Regel (Drift-Check bei jeder Verbindung).
	RuleTypeAptProxy = "apt-proxy"

	// RuleTypeAlertCheck wertet die Monitoring-/Trigger-Kriterien aus und
	// stößt bei Bedarf den Notification-Service an (serverloser System-Job,
	// analog zu Backup/Cleanup/CVE-Scan).
	RuleTypeAlertCheck = "alert-check"

	// RuleTypeReboot startet den Server neu (systemctl reboot). Sowohl als
	// Ein-Klick-Aktion in der Server-Detailansicht als auch als geplante
	// Gruppen-Regel nutzbar. Braucht vollen Root-Zugriff - im eingeschränkten
	// Sudo-Modus gesperrt (siehe restrictedAllowsRule).
	RuleTypeReboot = "reboot"

	// RuleTypeRebootIfNeeded startet den Server nur dann neu, wenn er selbst
	// einen Neustart anfordert - die Regel fürs Wartungsfenster.
	//
	// Der Unterschied zu RuleTypeReboot ist betrieblich der ganze Punkt: Ein
	// planmäßiger Neustart kostet jedes Mal eine Auszeit, auch wenn es nichts
	// zu übernehmen gibt. Gefragt wird deshalb das System selbst
	// (/var/run/reboot-required, needs-restarting -r, zypper
	// needs-rebooting) - und zwar LIVE, nicht der zuletzt erfasste Wert:
	// Zwischen dem letzten Scan und dem Wartungsfenster liegt genau das
	// Update, das den Neustart nötig macht.
	RuleTypeRebootIfNeeded = "reboot-if-needed"

	// RuleTypeDNSTest prüft auf jedem Server der Gruppe, ob die in den globalen
	// Einstellungen gepflegten Test-Domains aufgelöst werden können - als
	// Ein-Klick-Aktion und als geplante Gruppen-Regel. Rein lesend (getent/
	// nslookup), daher auch im eingeschränkten Sudo-Modus erlaubt. Kein
	// Grundsatz-/Enforce-Typ (ein Test hat kein „Drift beheben").
	RuleTypeDNSTest = "dns-test"

	// RuleTypeDeepScan führt die tiefergehende Sicherheitsprüfung aus:
	// Kernel-Reboot-Lücke (needrestart), Kernel-CVEs (Trivy) und ein
	// Härtungs-/Fehlkonfigurations-Audit (Lynis bzw. LCM-Eigenprüfungen). Rein
	// lesend, daher auch im eingeschränkten Sudo-Modus erlaubt (needrestart/
	// lynis sind read-only Audit-Tools). Als Ein-Klick-Aktion und geplante Regel.
	RuleTypeDeepScan = "deep-scan"

	// RuleTypeACLSetup richtet die ACL-Unterstützung ein (Paket `acl`), ohne
	// die Verzeichnisrechte aus Berechtigungsprofilen wirkungslos bleiben.
	// Als Ein-Klick-Aktion an der Gruppe und als Grundsatz-Regel: Ihr
	// Soll-Zustand ist „setfacl vorhanden und nutzbar", damit rüstet die
	// Gruppe neue Mitglieder ohne Zutun nach.
	RuleTypeACLSetup = "acl-setup"

	// RuleTypePermSync hält den Rechte-Soll gegen Drift: Paketaktualisierungen
	// setzen Modi zurück, systemd-tmpfiles stellt sie beim Booten wieder her,
	// und eine ersetzte Datei verliert ihre ACL. Die Regel vergleicht und
	// greift NUR bei Abweichung ein.
	RuleTypePermSync = "perm-sync"
)

// Schedule ist ein Zeitplan einer Servergruppe. Ein Schedule bündelt
// mehrere Rules, die zur Cron-Zeit nacheinander auf den Servern der
// Gruppe ausgeführt werden. Zusätzlich ist er manuell auslösbar
// (trigger-now).
type Schedule struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	GroupID uint         `gorm:"not null;index" json:"group_id"`
	Group   *ServerGroup `json:"-"`

	Name     string `gorm:"not null" json:"name"`
	CronExpr string `gorm:"not null" json:"cron_expr"` // z.B. "0 3 * * *"
	Enabled  bool   `gorm:"default:true" json:"enabled"`

	// System-Schedules (Health-Check, System-Sync) sind nicht löschbar.
	IsSystem bool `gorm:"default:false" json:"is_system"`

	Rules []Rule `gorm:"foreignKey:ScheduleID" json:"rules,omitempty"`
}

// Rule ist eine Regel auf einer Servergruppe. Sie läuft auf eine von
// zwei Arten:
//
//   - Am Schedule (ScheduleID gesetzt): der Zeitplan des Schedules löst
//     sie zusammen mit dessen übrigen Rules aus.
//   - Als Grundsatz-Regel (Enforce): kein Zeitplan - sie wird bei jeder
//     Verbindung zum Server (mindestens beim regelmäßigen Health-Ping)
//     geprüft und nur bei Abweichung durchgesetzt.
type Rule struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	GroupID uint         `gorm:"not null;index" json:"group_id"`
	Group   *ServerGroup `json:"-"`

	// Genau eines von beiden: ScheduleID (zeitgesteuert) oder Enforce
	// (grundsätzlich, bei jeder Verbindung).
	ScheduleID *uint `gorm:"index" json:"schedule_id"`
	Enforce    bool  `gorm:"default:false" json:"enforce"`

	Name    string `gorm:"not null" json:"name"`
	Type    string `gorm:"not null" json:"type"` // RuleType*
	Command string `json:"command"`              // RuleTypeScript: Kommando; RuleTypeFirewall: FirewallRule-JSON (Legacy: Portliste)
	Enabled bool   `gorm:"default:true" json:"enabled"`

	// System-Rules (Health-Check, Sync) sind nicht löschbar und ihr Typ
	// ist nicht änderbar.
	IsSystem bool `gorm:"default:false" json:"is_system"`
}
