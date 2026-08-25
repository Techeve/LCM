package services

import (
	"fmt"
	"strings"

	"LCM/internal/core/domain"
)

// nftables-Backend (Debian, Arch, Alpine). Eigenheiten: LCM besitzt eine
// EIGENE Tabelle `inet lcm` und fasst fremde Tabellen (Docker, fail2ban, …)
// nie an - insbesondere NIE `flush ruleset`. Der Regelsatz wird atomar
// ersetzt: „Tabelle anlegen (idempotent) → löschen → neu definieren" in
// EINER nft-Datei ist eine einzige Transaktion, es gibt kein Zeitfenster
// ohne Regeln. Persistenz über /etc/nftables.d/lcm.nft + Glob-Include in
// der Distributions-Konfiguration (Debian/Arch: /etc/nftables.conf,
// Alpine: /etc/nftables.nft), Dienststart via systemd oder OpenRC.

// nftConfigPath ist die von LCM verwaltete Regelsatz-Datei auf dem Ziel.
const nftConfigPath = "/etc/nftables.d/lcm.nft"

// nftFamilyPrefix liefert das Familien-Präfix einer Regel (nfproto), ohne
// Quell-Einschränkung.
func nftFamilyPrefix(r domain.FirewallRule) string {
	switch {
	case r.IPVersion == "v4":
		return "meta nfproto ipv4 "
	case r.IPVersion == "v6":
		return "meta nfproto ipv6 "
	}
	return ""
}

// nftRuleLines rendert die Freigabe(n) einer Regel. Ohne Quell-Einschränkung
// eine Zeile; mit Quellen je Adressfamilie eine Zeile mit `ip saddr { … }`
// bzw. `ip6 saddr { … }` (nft-Set) - so dürfen nur diese Quellen auf den
// Port.
func nftRuleLines(r domain.FirewallRule) []string {
	// Bemerkung des Betreibers als nft-Kommentar anhängen: So steht der Zweck
	// der Freigabe auch in `nft list table inet lcm` - lesbar für jemanden,
	// der auf der Maschine sitzt und LCM nicht kennt.
	note := ""
	if c := firewallCommentClean(r.Comment); c != "" {
		note = fmt.Sprintf(" comment %q", c)
	}
	if len(r.Sources) == 0 {
		return []string{fmt.Sprintf("%s%s dport %d accept%s", nftFamilyPrefix(r), r.Proto, r.Port, note)}
	}
	v4, v6 := sourcesByFamily(r.Sources)
	var out []string
	if len(v4) > 0 && r.IPVersion != "v6" {
		out = append(out, fmt.Sprintf("ip saddr { %s } %s dport %d accept%s", strings.Join(v4, ", "), r.Proto, r.Port, note))
	}
	if len(v6) > 0 && r.IPVersion != "v4" {
		out = append(out, fmt.Sprintf("ip6 saddr { %s } %s dport %d accept%s", strings.Join(v6, ", "), r.Proto, r.Port, note))
	}
	return out
}

// nftRuleset baut den kompletten LCM-Regelsatz als nft-Datei: input-Hook mit
// policy drop, etablierte Verbindungen (die laufende SSH-Sitzung!), Loopback
// und ICMP/ICMPv6 erlaubt, SSH-Port IMMER offen (Aussperr-Schutz), dann die
// konfigurierten Regeln. Der Regelsatz-Hash hängt als Kommentar an der
// ct-Regel und macht Drift bei `nft list table` billig erkennbar.
func nftRuleset(ssh domain.FirewallRule, rules []domain.FirewallRule) string {
	var b strings.Builder
	b.WriteString("table inet lcm\n")
	b.WriteString("delete table inet lcm\n")
	b.WriteString("table inet lcm {\n")
	b.WriteString("    chain input {\n")
	b.WriteString("        type filter hook input priority 0; policy drop;\n")
	fmt.Fprintf(&b, "        ct state established,related accept comment \"lcm:%s\"\n", firewallRulesHash(ssh, rules))
	b.WriteString("        ct state invalid drop\n")
	b.WriteString("        iif \"lo\" accept\n")
	b.WriteString("        ip protocol icmp accept\n")
	b.WriteString("        meta l4proto ipv6-icmp accept\n")
	// SSH-Freigabe und alle Regeln über denselben Renderer.
	for _, r := range append([]domain.FirewallRule{ssh}, rules...) {
		for _, line := range nftRuleLines(r) {
			b.WriteString("        " + line + "\n")
		}
	}
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

// nftablesEnableScript schreibt den Regelsatz (base64 - keine Quoting-Fallen),
// prüft ihn mit `nft -c` VOR dem Anwenden, wendet ihn atomar an und richtet
// die Persistenz ein: Glob-Include in der vorhandenen Distributions-
// Konfiguration ergänzen (ein Glob ohne Treffer ist für nft legal - ein
// späteres Deaktivieren bricht den Boot also nie) und den Dienst aktivieren
// (systemd, sonst OpenRC/Alpine).
func nftablesEnableScript(ssh domain.FirewallRule, rules []domain.FirewallRule) string {
	return strings.Join([]string{
		"set -e",
		"command -v nft >/dev/null 2>&1 || { echo '" + firewallToolMissingMarker(domain.FirewallToolNftables) + "'; exit 1; }",
		"mkdir -p /etc/nftables.d",
		writeFileB64(nftRuleset(ssh, rules), nftConfigPath+".tmp"),
		"nft -c -f " + nftConfigPath + ".tmp",
		"mv " + nftConfigPath + ".tmp " + nftConfigPath,
		"nft -f " + nftConfigPath,
		`for cfg in /etc/nftables.conf /etc/nftables.nft; do [ -f "$cfg" ] || continue; grep -q 'include "/etc/nftables.d/' "$cfg" || printf '\ninclude "/etc/nftables.d/*.nft"\n' >> "$cfg"; done`,
		`if [ ! -f /etc/nftables.conf ] && [ ! -f /etc/nftables.nft ]; then printf '#!/usr/sbin/nft -f\ninclude "/etc/nftables.d/*.nft"\n' > /etc/nftables.conf; fi`,
		"systemctl enable nftables >/dev/null 2>&1 || rc-update add nftables default >/dev/null 2>&1 || true",
		"nft list table inet lcm",
	}, "\n")
}

// nftablesDisableScript entfernt die LCM-Tabelle und die Regelsatz-Datei.
// Der nftables-Dienst bleibt aktiviert - er kann fremde Regelwerke tragen.
func nftablesDisableScript() string {
	return strings.Join([]string{
		"command -v nft >/dev/null 2>&1 || { echo '" + firewallToolMissingMarker(domain.FirewallToolNftables) + "'; exit 1; }",
		"nft list table inet lcm >/dev/null 2>&1 && nft delete table inet lcm || true",
		"rm -f " + nftConfigPath,
		"echo 'LCM: nftables-Regelwerk entfernt'",
	}, "\n")
}

// nftablesStatusCmd liest den Ist-Zustand der LCM-Tabelle.
const nftablesStatusCmd = "nft list table inet lcm 2>/dev/null || echo 'LCM: keine lcm-tabelle'"

// nftablesActiveFromOutput: die Firewall gilt als aktiv, wenn die LCM-Tabelle
// existiert (sie enthält immer den drop-Policy-Input-Hook).
func nftablesActiveFromOutput(output string) bool {
	return strings.Contains(output, "table inet lcm {")
}

// nftablesInSync: Tabelle vorhanden und der Regelsatz-Hash unverändert -
// der Hash erfasst SSH-Port, SSH-Quellen, Ports, Protokolle, IP-Version,
// Quellen und Bemerkungen.
func nftablesInSync(statusOutput string, ssh domain.FirewallRule, rules []domain.FirewallRule) bool {
	return nftablesActiveFromOutput(statusOutput) &&
		strings.Contains(statusOutput, "lcm:"+firewallRulesHash(ssh, rules))
}

// nftablesAllowPortCmd öffnet einen Port inkrementell (SSH-Portwechsel):
// nur wenn die LCM-Tabelle existiert; die abschließende Voll-Anwendung
// stellt den Soll-Zustand wieder her.
func nftablesAllowPortCmd(port int) string {
	return fmt.Sprintf("command -v nft >/dev/null 2>&1 && nft list table inet lcm >/dev/null 2>&1 && nft insert rule inet lcm input tcp dport %d accept || true", port)
}

// nftablesRemovePortCmd: bewusster No-Op - das Entfernen einzelner Regeln
// bräuchte Handle-Arithmetik; der SSH-Portwechsel endet ohnehin mit einer
// vollständigen Neu-Anwendung des Regelsatzes.
func nftablesRemovePortCmd(port int) string {
	_ = port
	return "true"
}
