package services

import (
	"fmt"
	"strings"

	"LCM/internal/core/domain"
)

// Backend-Registry der Multi-Distro-Firewall. Jede Distribution bekommt ihr
// vorgesehenes Werkzeug (ufw / firewalld / nftables); ein bereits
// installiertes Werkzeug gewinnt IMMER - LCM installiert nie eine zweite
// Firewall neben einer vorhandenen (zwei aktive Paketfilter bekämpfen sich).

// firewallBackend bündelt die Skript-Bauer eines Firewall-Werkzeugs. Die
// Implementierungen sind bewusst je Backend getrennt (firewall_ufw.go,
// firewall_firewalld.go, firewall_nftables.go) und gehen auf die Eigenheiten
// des jeweiligen Werkzeugs ein; diese Struktur hält nur die gemeinsame Form.
type firewallBackend struct {
	name          string
	enableScript  func(ssh domain.FirewallRule, rules []domain.FirewallRule) string
	disableScript func() string
	statusCmd     string
	activeFrom    func(output string) bool
	inSync        func(statusOutput string, ssh domain.FirewallRule, rules []domain.FirewallRule) bool
	allowPortCmd  func(port int) string
	removePortCmd func(port int) string
}

// firewallBackendByName liefert das Backend zu einem Werkzeug-Namen.
// Unbekannte Namen fallen dokumentiert auf ufw zurück (defensiv - die
// Aufrufer reichen nur Werte aus firewallBackendFor durch).
func firewallBackendByName(name string) firewallBackend {
	switch name {
	case domain.FirewallToolFirewalld:
		return firewallBackend{
			name:          domain.FirewallToolFirewalld,
			enableScript:  firewalldEnableScript,
			disableScript: firewalldDisableScript,
			statusCmd:     firewalldStatusCmd,
			activeFrom:    firewalldActiveFromOutput,
			inSync:        firewalldInSync,
			allowPortCmd:  firewalldAllowPortCmd,
			removePortCmd: firewalldRemovePortCmd,
		}
	case domain.FirewallToolNftables:
		return firewallBackend{
			name:          domain.FirewallToolNftables,
			enableScript:  nftablesEnableScript,
			disableScript: nftablesDisableScript,
			statusCmd:     nftablesStatusCmd,
			activeFrom:    nftablesActiveFromOutput,
			inSync:        nftablesInSync,
			allowPortCmd:  nftablesAllowPortCmd,
			removePortCmd: nftablesRemovePortCmd,
		}
	}
	return firewallBackend{
		name:          domain.FirewallToolUfw,
		enableScript:  ufwEnableScript,
		disableScript: ufwDisableScript,
		statusCmd:     ufwStatusCmd,
		activeFrom:    ufwActiveFromOutput,
		inSync:        ufwInSync,
		allowPortCmd:  ufwAllowPortCmd,
		removePortCmd: ufwRemovePortCmd,
	}
}

// firewallDesignatedBackend ordnet der Distribution (os-release ID) ihr
// vorgesehenes Firewall-Werkzeug zu. Bewusst über die OSID statt pkgFamily -
// Ubuntu und Debian teilen sich apt, bekommen aber verschiedene Backends.
// Alle bekannten IDs stehen EXPLIZIT hier (BUG-012-Konvention: kein stiller
// Fallback für Bekanntes); nicht aufgeführte Distributionen erhalten
// dokumentiert ufw als Standard.
func firewallDesignatedBackend(osID string) string {
	id := strings.ToLower(strings.TrimSpace(osID))
	switch id {
	case "ubuntu":
		return domain.FirewallToolUfw
	case "rhel", "rocky", "almalinux", "fedora", "centos", "centos-stream", "sles":
		return domain.FirewallToolFirewalld
	case "debian", "arch", "alpine":
		return domain.FirewallToolNftables
	}
	if strings.HasPrefix(id, "opensuse") { // opensuse-leap, opensuse-tumbleweed, …
		return domain.FirewallToolFirewalld
	}
	return domain.FirewallToolUfw
}

// firewallBackendFor bestimmt das wirksame Backend eines Servers: das beim
// Scan erkannte installierte Werkzeug gewinnt (nie ein zweites daneben
// installieren), sonst das für die Distribution vorgesehene.
func firewallBackendFor(server *domain.Server) string {
	switch server.FirewallTool {
	case domain.FirewallToolUfw, domain.FirewallToolFirewalld, domain.FirewallToolNftables:
		return server.FirewallTool
	}
	return firewallDesignatedBackend(server.OSID)
}

// FirewallBackendFor ist die exportierte Hülle für die API-Schicht: das
// wirksame Backend eines Servers (für den Backend-Badge im Firewall-Dialog).
func FirewallBackendFor(server *domain.Server) string {
	return firewallBackendFor(server)
}

// firewallDetectCmd erkennt das VERWALTETE Firewall-Werkzeug auf dem Ziel.
// Reihenfolge: ufw > firewalld > nftables. Ein bloß vorhandenes nft-Binary
// zählt NICHT - es liegt auf fast jedem modernen System (iptables-nft);
// nftables gilt nur als Firewall, wenn die LCM-Tabelle existiert oder der
// nftables-Dienst aktiviert ist (systemd oder OpenRC). Braucht für den
// nft-Zweig root (privRun); ufw/firewalld werden auch ohne erkannt.
const firewallDetectCmd = `if command -v ufw >/dev/null 2>&1; then echo ufw; ` +
	`elif command -v firewall-cmd >/dev/null 2>&1; then echo firewalld; ` +
	`elif command -v nft >/dev/null 2>&1 && { nft list table inet lcm >/dev/null 2>&1 || ` +
	`systemctl is-enabled nftables >/dev/null 2>&1 || rc-update show 2>/dev/null | grep -q nftables; }; then echo nftables; ` +
	`else echo none; fi`

// parseFirewallDetect liest das Ergebnis von firewallDetectCmd.
func parseFirewallDetect(out string) string {
	first := strings.TrimSpace(out)
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = strings.TrimSpace(first[:i])
	}
	switch first {
	case domain.FirewallToolUfw, domain.FirewallToolFirewalld, domain.FirewallToolNftables:
		return first
	}
	return ""
}

// resolveFirewallBackend verbindet Erkennung und Konfliktprüfung:
//  1. Ist ein Werkzeug vorhanden → dieses verwenden, nichts installieren.
//  2. Ist keines vorhanden → das vorgesehene Werkzeug der Distribution
//     installieren (best-effort-Install + hartes command -v als Nachweis).
//
// Rückgabe: Backend-Name und Installations-Skript ("" = nichts zu tun).
func resolveFirewallBackend(server *domain.Server, detected string) (backend, installScript string) {
	if detected != "" {
		return detected, ""
	}
	designated := firewallDesignatedBackend(server.OSID)
	verifyBin := map[string]string{
		domain.FirewallToolUfw:       "ufw",
		domain.FirewallToolFirewalld: "firewall-cmd",
		domain.FirewallToolNftables:  "nft",
	}[designated]
	install := pkgInstallScript(server.PackageManager, []string{designated}) +
		fmt.Sprintf("; command -v %s >/dev/null 2>&1 || { echo '%s'; exit 1; }", verifyBin, firewallInstallFailedMarker)
	return designated, install
}
