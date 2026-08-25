// Package mcp implementiert die MCP-Schnittstelle (Model Context Protocol)
// von LCM: ein separater, per Bearer-API-Key authentifizierter HTTP-Listener,
// über den KI-Agenten AUSSCHLIESSLICH read-only Eigenschaften der verwalteten
// Server abrufen. Es gibt keine schreibenden/konfigurierenden Tools, und die
// ausgelieferten Daten durchlaufen ein kuratiertes Whitelist-DTO (ServerView),
// das per Konstruktion keine Passwörter, Zugangsdaten, kryptografischen
// Schlüssel oder Fingerprints enthält.
package mcp

import "time"

// ServerView ist die read-only Sicht eines verwalteten Servers für KI-Agenten.
//
// SICHERHEIT: Dieses DTO enthält BEWUSST nur unbedenkliche Eigenschaften. Es
// darf NIEMALS um Felder erweitert werden, die Geheimnisse tragen - kein
// Login-Benutzer, kein Passwort/Login-Passwort (auch nicht verschlüsselt),
// kein Private Key, kein Public Key, kein Host-Key-Fingerprint, kein
// Agent-Token(-Hash/-Prefix). Der MCP-Handler serialisiert ausschließlich
// dieses DTO, nie das domain.Server-Struct.
type ServerView struct {
	// --- Inventar & Status ---
	ID          uint       `json:"id"`
	Name        string     `json:"name"`
	Host        string     `json:"host"`
	IPAddresses string     `json:"ip_addresses,omitempty"`
	OSName      string     `json:"os_name"`
	OSVersion   string     `json:"os_version"`
	OSID        string     `json:"os_id,omitempty"`
	Transport   string     `json:"transport"` // ssh / agent / routeros
	Reachable   bool       `json:"reachable"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
	Status      string     `json:"status"` // Ampel: excellent/green/yellow/red
	Insights    []string   `json:"insights,omitempty"`

	// --- Hardware-Metriken ---
	CPUModel         string `json:"cpu_model,omitempty"`
	CPUCores         int    `json:"cpu_cores,omitempty"`
	MemTotalMB       int64  `json:"mem_total_mb,omitempty"`
	MemUsedMB        int64  `json:"mem_used_mb,omitempty"`
	DiskTotalMB      int64  `json:"disk_total_mb,omitempty"`
	DiskUsedMB       int64  `json:"disk_used_mb,omitempty"`
	DiskUsagePercent int    `json:"disk_usage_percent,omitempty"`
	KernelVersion    string `json:"kernel_version,omitempty"`
	Virtualization   string `json:"virtualization,omitempty"`

	// --- Paket-/Update-Stand ---
	OutdatedPackages        int    `json:"outdated_packages"`
	RebootRequired          bool   `json:"reboot_required"`
	RouterOSChannel         string `json:"routeros_channel,omitempty"`
	RouterOSLatestVersion   string `json:"routeros_latest_version,omitempty"`
	RouterOSUpdateAvailable bool   `json:"routeros_update_available,omitempty"`

	// --- Sicherheitslage ---
	CVECritical    int    `json:"cve_critical"`
	CVEHigh        int    `json:"cve_high"`
	SSHHardened    bool   `json:"ssh_hardened"`
	FirewallActive bool   `json:"firewall_active"`
	FirewallTool   string `json:"firewall_tool,omitempty"`
	HardeningIndex *int   `json:"hardening_index,omitempty"`
	ProxmoxType    string `json:"proxmox_type,omitempty"`
}

// FleetSummary ist die aggregierte Flottensicht (Zählungen).
type FleetSummary struct {
	Total             int            `json:"total"`
	Reachable         int            `json:"reachable"`
	Unreachable       int            `json:"unreachable"`
	ByStatus          map[string]int `json:"by_status"`
	UpdatesAvailable  int            `json:"updates_available"`
	CriticalCVEServer int            `json:"servers_with_critical_cve"`
}

// summarize baut die Flottensicht aus den ServerViews.
func summarize(views []ServerView) FleetSummary {
	fs := FleetSummary{Total: len(views), ByStatus: map[string]int{}}
	for _, v := range views {
		if v.Reachable {
			fs.Reachable++
		} else {
			fs.Unreachable++
		}
		fs.ByStatus[v.Status]++
		if v.OutdatedPackages > 0 || v.RouterOSUpdateAvailable {
			fs.UpdatesAvailable++
		}
		if v.CVECritical > 0 {
			fs.CriticalCVEServer++
		}
	}
	return fs
}
