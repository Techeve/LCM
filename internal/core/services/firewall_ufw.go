package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
)

// ufw-Backend (Ubuntu; außerdem Default für Distributionen ohne eigene
// Zuordnung). Eigenheiten: ufw arbeitet deklarativ über einen kompletten
// Reset-und-Neuaufbau; eine Regel ohne Zielangabe gilt immer für IPv4 UND
// IPv6 - eine reine v4-/v6-Freigabe wird über das Ziel 0.0.0.0/0 bzw. ::/0
// ausgedrückt.

// ufwRuleTarget liefert das "to"-Ziel einer Regel: die Familien-Wildcard
// (any = beide Familien).
func ufwRuleTarget(r domain.FirewallRule) string {
	switch r.IPVersion {
	case "v4":
		return "0.0.0.0/0"
	case "v6":
		return "::/0"
	}
	return "any"
}

// ufwRuleCmds rendert die Freigabe(n) einer Regel. Ohne Quell-Allowlist eine
// Zeile (`to <ziel>`); mit Allowlist eine Zeile je Quell-IP (`from <src> to
// <ziel>`) - so darf nur die Allowlist auf den Port. Der lcm-Kommentar
// markiert die Regeln als LCM-verwaltet.
func ufwRuleCmds(r domain.FirewallRule) []string {
	return ufwRuleCmdsWithComment(r, ufwComment("lcm", r.Comment))
}

// ufwRuleCmdsWithComment rendert eine Regel mit vorgegebenem Kommentartext -
// die SSH-Zeile trägt dort den Regelsatz-Hash statt der Bemerkung.
func ufwRuleCmdsWithComment(r domain.FirewallRule, comment string) []string {
	if len(r.Sources) == 0 {
		return []string{fmt.Sprintf("ufw allow proto %s to %s port %d comment %s", r.Proto, ufwRuleTarget(r), r.Port, shellQuote(comment))}
	}
	out := make([]string, 0, len(r.Sources))
	for _, src := range r.Sources {
		out = append(out, fmt.Sprintf("ufw allow proto %s from %s to %s port %d comment %s", r.Proto, src, ufwRuleTarget(r), r.Port, shellQuote(comment)))
	}
	return out
}

// ufwComment hängt die Bemerkung des Betreibers an die LCM-Marke. So bleibt
// die Regel als LCM-verwaltet erkennbar UND der Zweck steht in `ufw status`
// - auch für jemanden, der nur auf der Maschine sitzt.
func ufwComment(mark, note string) string {
	if note = firewallCommentClean(note); note == "" {
		return mark
	}
	return mark + ": " + note
}

// ufwEnableScript baut ein DEKLARATIVES ufw-Skript: die Regeln werden
// vollständig neu gesetzt (reset → default deny incoming), SSH und die
// konfigurierten Regeln werden freigegeben, dann wird die Firewall aktiviert.
// Der SSH-Port wird IMMER erlaubt, damit man sich nicht aussperrt; die
// bestehende SSH-Sitzung überlebt den Reset (etablierte Verbindung). Der
// Regelsatz-Hash hängt als Kommentar an der SSH-Regel und macht Drift
// (auch bei IP-Version/Quellen) im Status sichtbar.
func ufwEnableScript(ssh domain.FirewallRule, rules []domain.FirewallRule) string {
	steps := []string{
		"command -v ufw >/dev/null 2>&1 || { echo '" + firewallToolMissingMarker(domain.FirewallToolUfw) + "'; exit 1; }",
		"ufw --force reset",
		"ufw default deny incoming",
		"ufw default allow outgoing",
		// SSH-Freigabe mit dem Regelsatz-Hash als Kommentar.
	}
	// Die SSH-Freigabe läuft durch denselben Renderer wie jede andere Regel
	// (sie kann Quellen tragen); der Regelsatz-Hash hängt als Kommentar dran.
	steps = append(steps, ufwRuleCmdsWithComment(ssh, "lcm:"+firewallRulesHash(ssh, rules))...)
	for _, r := range rules {
		steps = append(steps, ufwRuleCmds(r)...)
	}
	steps = append(steps, "ufw --force enable", "ufw status verbose")
	return strings.Join(steps, " && ")
}

// ufwDisableScript deaktiviert die ufw-Firewall.
func ufwDisableScript() string {
	return strings.Join([]string{
		"command -v ufw >/dev/null 2>&1 || { echo '" + firewallToolMissingMarker(domain.FirewallToolUfw) + "'; exit 1; }",
		"ufw --force disable",
		"ufw status",
	}, " && ")
}

// ufwStatusCmd liest den Ist-Zustand (Aktiv-Status + Regeln inkl. Kommentare).
const ufwStatusCmd = "ufw status verbose 2>/dev/null || true"

// ufwActiveFromOutput erkennt am ufw-Output, ob die Firewall aktiv ist.
func ufwActiveFromOutput(output string) bool {
	return strings.Contains(output, "Status: active")
}

// reUfwAllow matcht freigegebene Ports in "ufw status"-Zeilen, z.B.
// "22/tcp  ALLOW  Anywhere", "80/tcp (v6)  ALLOW IN  Anywhere (v6)",
// "10.0.0.5 443/tcp  ALLOW IN  Anywhere" (Regel mit Zieladresse).
var reUfwAllow = regexp.MustCompile(`(?m)^(?:\S+\s+)?(\d+)/(tcp|udp)(?:\s+\(v6\))?\s+ALLOW`)

// ufwPortsFromStatus extrahiert die freigegebenen Ports (protokollunabhängig)
// aus einem "ufw status"-Output - Grundlage der Legacy-CSV-Anzeige
// (v6-Duplikate fallen im Set zusammen).
func ufwPortsFromStatus(output string) map[string]bool {
	ports := map[string]bool{}
	for _, m := range reUfwAllow.FindAllStringSubmatch(output, -1) {
		ports[m[1]] = true
	}
	return ports
}

// ufwPortProtosFromStatus extrahiert die freigegebenen Port/Protokoll-Paare
// ("80/tcp") - der Set-Vergleich der In-Sync-Prüfung.
func ufwPortProtosFromStatus(output string) map[string]bool {
	set := map[string]bool{}
	for _, m := range reUfwAllow.FindAllStringSubmatch(output, -1) {
		set[m[1]+"/"+m[2]] = true
	}
	return set
}

// ufwInSync prüft, ob der ufw-Status dem Soll einer Firewall-Regel entspricht:
// aktiv, der Regelsatz-Hash sichtbar (erfasst Bind/IP-Version) UND die
// freigegebenen Port/Protokoll-Paare EXAKT SSH + Regelsatz. Der Hash allein
// erkennt keine von Hand ergänzten Regeln, das Port-Set keine Quell-Änderungen
// - beide zusammen schon. Nur bei Abweichung wird die Firewall neu angewendet.
func ufwInSync(statusOutput string, ssh domain.FirewallRule, rules []domain.FirewallRule) bool {
	if !ufwActiveFromOutput(statusOutput) {
		return false
	}
	if !strings.Contains(statusOutput, "lcm:"+firewallRulesHash(ssh, rules)) {
		return false
	}
	want := map[string]bool{strconv.Itoa(ssh.Port) + "/tcp": true}
	for _, r := range rules {
		want[fmt.Sprintf("%d/%s", r.Port, r.Proto)] = true
	}
	got := ufwPortProtosFromStatus(statusOutput)
	if len(got) != len(want) {
		return false
	}
	for p := range want {
		if !got[p] {
			return false
		}
	}
	return true
}

// ufwAllowPortCmd/ufwRemovePortCmd: inkrementelle Einzelfreigabe für den
// SSH-Portwechsel (alter und neuer Port kurzzeitig parallel offen). Gegen
// ein fehlendes Werkzeug abgesichert - der Portwechsel selbst darf daran
// nicht scheitern.
func ufwAllowPortCmd(port int) string {
	return fmt.Sprintf("command -v ufw >/dev/null 2>&1 && ufw allow %d/tcp || true", port)
}

func ufwRemovePortCmd(port int) string {
	return fmt.Sprintf("command -v ufw >/dev/null 2>&1 && ufw delete allow %d/tcp || true", port)
}
