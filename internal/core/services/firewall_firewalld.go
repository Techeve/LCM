package services

import (
	"fmt"
	"regexp"
	"strings"

	"LCM/internal/core/domain"
)

// firewalld-Backend (RHEL/Rocky/Alma, Fedora, CentOS Stream, openSUSE/SLES).
// Eigenheiten: LCM verwaltet die Default-Zone DEKLARATIV - Ports, Rich-Rules
// UND Zonen-Dienste werden neu gesetzt (gleiche Semantik wie der ufw-Reset).
// Die vorbelegten Zonen-Dienste der Distribution werden dabei entfernt -
// sie ließen sonst mehr offen als konfiguriert (R2-057: Cockpit auf RHEL).
// Einfache Freigaben laufen über --add-port, Familie oder Bind-Adresse über
// Rich-Rules.

// firewalldRichRules rendert eine Regel als eine oder mehrere Rich-Rules -
// exakt in der kanonischen Attributreihenfolge, in der firewalld sie bei
// --list-rich-rules zurückgibt (Grundlage des In-Sync-Set-Vergleichs;
// source ist als IP/CIDR validiert, die Einbettung daher injektionssicher).
// Reihenfolge der Attribute: family, source, port, protocol, accept. Mit
// Quell-Allowlist entsteht je Quelle (nach Familie) eine eigene Rich-Rule.
func firewalldRichRules(r domain.FirewallRule) []string {
	build := func(family, source string) string {
		src := ""
		if source != "" {
			src = fmt.Sprintf(` source address="%s"`, source)
		}
		return fmt.Sprintf(`rule family="%s"%s port port="%d" protocol="%s" accept`, family, src, r.Port, r.Proto)
	}
	if len(r.Sources) == 0 {
		family := "ipv4"
		if r.IPVersion == "v6" {
			family = "ipv6"
		}
		return []string{build(family, "")}
	}
	v4, v6 := sourcesByFamily(r.Sources)
	var out []string
	for _, s := range v4 {
		out = append(out, build("ipv4", s))
	}
	for _, s := range v6 {
		out = append(out, build("ipv6", s))
	}
	return out
}

// firewalldRuleIsSimple: Regeln ohne Familien-Einschränkung UND ohne
// Quell-Allowlist brauchen keine Rich-Rule - --add-port gilt für beide
// Familien und alle Quellen.
func firewalldRuleIsSimple(r domain.FirewallRule) bool {
	return (r.IPVersion == "" || r.IPVersion == "any") && len(r.Sources) == 0
}

// firewalldEnableScript setzt die Default-Zone deklarativ neu: Dienst
// aktivieren, vorhandene Ports + Rich-Rules der Zone entfernen, SSH und die
// Regeln freigeben, ein einziges --reload macht alles zugleich wirksam
// (die bestehende SSH-Sitzung überlebt als etablierte Verbindung).
func firewalldEnableScript(ssh domain.FirewallRule, rules []domain.FirewallRule) string {
	lines := []string{
		"set -e",
		"command -v firewall-cmd >/dev/null 2>&1 || { echo '" + firewallToolMissingMarker(domain.FirewallToolFirewalld) + "'; exit 1; }",
		"systemctl enable --now firewalld >/dev/null 2>&1 || true",
		"firewall-cmd --state >/dev/null 2>&1 || { echo 'LCM-FEHLER: firewalld laeuft nicht'; exit 1; }",
		`Z=$(firewall-cmd --get-default-zone)`,
		`for p in $(firewall-cmd --permanent --zone="$Z" --list-ports); do firewall-cmd --permanent --zone="$Z" --remove-port="$p" >/dev/null || true; done`,
		`firewall-cmd --permanent --zone="$Z" --list-rich-rules | while IFS= read -r r; do [ -n "$r" ] && firewall-cmd --permanent --zone="$Z" --remove-rich-rule="$r" >/dev/null || true; done`,
		// Auch die ZONEN-DIENSTE entfernen: die Default-Zone bringt je nach
		// Distribution vorbelegte Dienste mit (RHEL: ssh, dhcpv6-client,
		// cockpit). Ohne diesen Schritt blieb Cockpit (9090/tcp) erreichbar,
		// obwohl nur SSH konfiguriert war und die Anzeige 22/tcp zeigte -
		// die wirksame Firewall war weiter als die konfigurierte (R2-057).
		// SSH bleibt offen: der explizite Port wird unten VOR dem einen
		// --reload wieder freigegeben.
		`for s in $(firewall-cmd --permanent --zone="$Z" --list-services); do firewall-cmd --permanent --zone="$Z" --remove-service="$s" >/dev/null || true; done`,
	}
	// SSH-Freigabe (bind-fähig) und alle Regeln über denselben Renderer.
	for _, r := range append([]domain.FirewallRule{ssh}, rules...) {
		if firewalldRuleIsSimple(r) {
			lines = append(lines, fmt.Sprintf(`firewall-cmd --permanent --zone="$Z" --add-port=%d/%s >/dev/null`, r.Port, r.Proto))
		} else {
			for _, rr := range firewalldRichRules(r) {
				lines = append(lines, `firewall-cmd --permanent --zone="$Z" --add-rich-rule='`+rr+`' >/dev/null`)
			}
		}
	}
	lines = append(lines,
		"firewall-cmd --reload >/dev/null",
		`echo "LCM-ZONE: $Z"`,
		"firewall-cmd --state",
		`firewall-cmd --zone="$Z" --list-ports`,
		`firewall-cmd --zone="$Z" --list-rich-rules`,
		`echo "LCM-SERVICES: $(firewall-cmd --zone="$Z" --list-services)"`,
	)
	return strings.Join(lines, "\n")
}

// firewalldDisableScript stoppt und deaktiviert den firewalld-Dienst.
func firewalldDisableScript() string {
	return strings.Join([]string{
		"command -v firewall-cmd >/dev/null 2>&1 || { echo '" + firewallToolMissingMarker(domain.FirewallToolFirewalld) + "'; exit 1; }",
		"systemctl disable --now firewalld >/dev/null 2>&1 || true",
		"echo 'LCM: firewalld deaktiviert'",
		"firewall-cmd --state 2>/dev/null || true",
	}, "\n")
}

// firewalldStatusCmd liest den Ist-Zustand: Laufstatus + Ports + Rich-Rules
// der Default-Zone.
const firewalldStatusCmd = `firewall-cmd --state 2>/dev/null; Z=$(firewall-cmd --get-default-zone 2>/dev/null); firewall-cmd --zone="$Z" --list-ports 2>/dev/null; firewall-cmd --zone="$Z" --list-rich-rules 2>/dev/null; echo "LCM-SERVICES: $(firewall-cmd --zone="$Z" --list-services 2>/dev/null)"; true`

// firewalldActiveFromOutput: firewalld meldet "running" bzw. "not running" -
// der Substring-Test muss die Verneinung ausschließen (zeilenweise Prüfung).
func firewalldActiveFromOutput(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "running" {
			return true
		}
	}
	return false
}

// reFirewalldPort matcht Port/Proto-Token aus --list-ports ("22/tcp 443/udp").
var reFirewalldPort = regexp.MustCompile(`\b(\d+)/(tcp|udp)\b`)

// firewalldInSync vergleicht den Ist-Zustand mit dem Soll: aktiv, die
// Zonen-Ports EXAKT SSH + einfache Regeln, die Rich-Rules EXAKT die
// gerenderten Familien-/Bind-Regeln. Ein Rendering-Unterschied führt
// schlimmstenfalls zu einem harmlosen Neu-Anwenden.
func firewalldInSync(statusOutput string, ssh domain.FirewallRule, rules []domain.FirewallRule) bool {
	if !firewalldActiveFromOutput(statusOutput) {
		return false
	}
	wantPorts := map[string]bool{}
	wantRich := map[string]bool{}
	for _, r := range append([]domain.FirewallRule{ssh}, rules...) {
		if firewalldRuleIsSimple(r) {
			wantPorts[fmt.Sprintf("%d/%s", r.Port, r.Proto)] = true
		} else {
			for _, rr := range firewalldRichRules(r) {
				wantRich[rr] = true
			}
		}
	}
	gotPorts := map[string]bool{}
	gotRich := map[string]bool{}
	for _, line := range strings.Split(statusOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "rule ") {
			gotRich[trimmed] = true
			continue
		}
		// Verbliebene ZONEN-DIENSTE sind immer Drift: LCM rendert den
		// Soll-Zustand ausschließlich als Ports/Rich-Rules; ein Dienst wie
		// cockpit öffnet Ports, die in keiner Anzeige auftauchen (R2-057).
		if rest, ok := strings.CutPrefix(trimmed, "LCM-SERVICES:"); ok {
			if strings.TrimSpace(rest) != "" {
				return false
			}
			continue
		}
		if trimmed == "running" || strings.HasPrefix(trimmed, "LCM-ZONE:") {
			continue
		}
		for _, m := range reFirewalldPort.FindAllStringSubmatch(trimmed, -1) {
			gotPorts[m[1]+"/"+m[2]] = true
		}
	}
	if len(gotPorts) != len(wantPorts) || len(gotRich) != len(wantRich) {
		return false
	}
	for p := range wantPorts {
		if !gotPorts[p] {
			return false
		}
	}
	for r := range wantRich {
		if !gotRich[r] {
			return false
		}
	}
	return true
}

// firewalldAllowPortCmd/firewalldRemovePortCmd: Laufzeit-Einzelfreigabe für
// den SSH-Portwechsel; die abschließende Voll-Anwendung persistiert.
func firewalldAllowPortCmd(port int) string {
	return fmt.Sprintf("command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --add-port=%d/tcp >/dev/null 2>&1 || true", port)
}

func firewalldRemovePortCmd(port int) string {
	return fmt.Sprintf("command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --remove-port=%d/tcp >/dev/null 2>&1 || true", port)
}
