package domain

// Firewall-Werkzeuge, die LCM je Distribution verwaltet. Die Zuordnung
// (welche Distribution welches Werkzeug bekommt) liegt in
// services/firewall_backend.go; hier stehen nur die Bezeichner.
const (
	FirewallToolUfw       = "ufw"
	FirewallToolFirewalld = "firewalld"
	FirewallToolNftables  = "nftables"
)

// FirewallRule ist eine einzelne Freigabe-Regel der von LCM verwalteten
// Firewall. Neben dem Port lassen sich Protokoll (TCP/UDP), IP-Version und
// die erlaubten Quellen festlegen. Serialisiert als JSON-Array in
// Server.FirewallRules bzw. im Command einer Firewall-Gruppen-Regel.
//
// Eine Ziel-/Bind-Adresse gibt es bewusst nicht mehr: sie beantwortete die
// Frage „auf welcher lokalen Adresse ist der Port offen", während die
// erlaubten Quellen die praktisch entscheidende Frage beantworten „von wo
// darf jemand darauf zugreifen". Zwei Felder für zwei ähnlich klingende
// Zwecke haben in der Oberfläche mehr verwirrt als geholfen. Ein „bind" aus
// älteren gespeicherten Regelsätzen wird beim Lesen ignoriert.
type FirewallRule struct {
	Port  int    `json:"port"`  // 1-65535
	Proto string `json:"proto"` // "tcp" | "udp"
	// IPVersion: "any" | "v4" | "v6" ("" wird als any gelesen).
	IPVersion string `json:"ip_version,omitempty"`
	// AllowlistIDs referenzieren benannte IP-Allowlists (gemeinsamer Pool).
	// Sind welche gesetzt, gibt die Regel den Port NUR für die Quell-IPs
	// dieser Listen frei (Quell-Einschränkung) - sonst niemand. Ohne
	// Allowlist gilt die Regel für alle Quellen (wie bisher).
	AllowlistIDs []uint `json:"allowlist_ids,omitempty"`
	// SourceIPs sind händisch eingetragene Quell-IPs/-CIDRs (kanonisch),
	// unabhängig von benannten Allowlists. Sie wirken wie AllowlistIDs als
	// Quell-Einschränkung und werden mit den aufgelösten Allowlist-IPs
	// vereinigt (Union). Leer = keine manuelle Einschränkung.
	SourceIPs []string `json:"source_ips,omitempty"`
	// Sources sind die beim Anwenden aufgelösten konkreten Quell-CIDRs
	// (Union aus SourceIPs und den referenzierten Allowlists). Transient
	// (nicht persistiert/serialisiert) - wird kurz vor dem Rendern befüllt.
	Sources []string `json:"-"`
	// Comment ist eine freie Bemerkung des Betreibers („wofür ist diese
	// Freigabe?"). Reine Dokumentation; wo das Werkzeug Kommentare kennt
	// (ufw, nftables), wandert sie in den Regelsatz auf dem Zielsystem und
	// ist dort auch ohne LCM lesbar.
	Comment string `json:"comment,omitempty"`
}

// FirewallSSHSources sind die Quell-Einschränkungen der stets erzwungenen
// SSH-Freigabe - dieselbe Form wie bei jeder anderen Regel, nur getrennt
// gespeichert, weil die SSH-Zeile keine gewöhnliche Regel ist (sie lässt
// sich nicht löschen und ihr Port folgt dem Server).
type FirewallSSHSources struct {
	AllowlistIDs []uint   `json:"allowlist_ids,omitempty"`
	SourceIPs    []string `json:"source_ips,omitempty"`
}

// ListeningPort ist ein beim Scan erkannter lauschender Socket (ss -tulnp).
// Dient als Vorschlag im Firewall-Dialog: „dieser Dienst lauscht - Port
// freigeben?". Serialisiert als JSON-Array in Server.ListeningPorts.
type ListeningPort struct {
	Port      int    `json:"port"`
	Proto     string `json:"proto"`      // tcp|udp
	Bind      string `json:"bind"`       // 0.0.0.0, ::, oder konkrete Adresse
	IPVersion string `json:"ip_version"` // v4|v6
	Process   string `json:"process,omitempty"`
}
