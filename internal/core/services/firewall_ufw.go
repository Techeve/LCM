package services

import (
	"fmt"
	"regexp"
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
	lines := ufwRuleLines(r, comment)
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.cmd)
	}
	return out
}

// ufwLine ist eine Freigabe-Zeile; v6 heißt, sie gilt nur für IPv6 (Ziel ::/0
// oder eine IPv6-Quelle).
type ufwLine struct {
	cmd  string
	desc string
	v6   bool
}

// ufwRuleLines rendert die Freigaben einer Regel samt Adressfamilie.
func ufwRuleLines(r domain.FirewallRule, comment string) []ufwLine {
	port := fmt.Sprintf("%d/%s", r.Port, r.Proto)
	if len(r.Sources) == 0 {
		return []ufwLine{{
			cmd:  fmt.Sprintf("ufw allow proto %s to %s port %d comment %s", r.Proto, ufwRuleTarget(r), r.Port, shellQuote(comment)),
			desc: port + " (nur IPv6)",
			v6:   r.IPVersion == "v6",
		}}
	}
	out := make([]ufwLine, 0, len(r.Sources))
	for _, src := range r.Sources {
		out = append(out, ufwLine{
			cmd:  fmt.Sprintf("ufw allow proto %s from %s to %s port %d comment %s", r.Proto, src, ufwRuleTarget(r), r.Port, shellQuote(comment)),
			desc: port + " von " + src,
			v6:   r.IPVersion == "v6" || strings.Contains(src, ":"),
		})
	}
	return out
}

// ufwIPv6Probe setzt $LCM_UFW_V6. Mit IPV6=no in /etc/default/ufw lehnt ufw
// jede reine IPv6-Freigabe mit „IPv6 support not enabled" ab - und weil das
// Skript mit && verkettet ist, brach damit der ganze Aufbau ab (an ufw 0.36
// nachgesehen). ufw schaltet IPv6 nur bei IPV6=yes ein; fehlt die Zeile oder
// ist die Datei nicht lesbar, bleibt es beim Versuch.
const ufwIPv6Probe = `{ LCM_UFW_V6=yes; if grep -qsiE '^[[:space:]]*IPV6=' /etc/default/ufw && ! grep -qsiE '^[[:space:]]*IPV6="?yes"?[[:space:]]*$' /etc/default/ufw; then LCM_UFW_V6=no; fi; }`

// ufwGuardedCmd lässt eine reine IPv6-Freigabe auf einem Host ohne IPv6 mit
// sichtbarem Hinweis aus.
func ufwGuardedCmd(l ufwLine) string {
	if !l.v6 {
		return l.cmd
	}
	note := "LCM: Hinweis - IPv6 ist in ufw abgeschaltet (IPV6=no in /etc/default/ufw), Freigabe ausgelassen: " + l.desc
	return `{ if [ "$LCM_UFW_V6" = yes ]; then ` + l.cmd + `; else echo ` + shellQuote(note) + `; fi; }`
}

// ufwSSHGuard bricht VOR dem Reset ab, wenn SSH nur über IPv6-Quellen
// freigegeben ist und der Host kein IPv6 in ufw hat: Ohne jede SSH-Freigabe
// sperrte die Firewall nach dem Einschalten SSH aus.
func ufwSSHGuard(ssh []ufwLine) string {
	for _, l := range ssh {
		if !l.v6 {
			return ""
		}
	}
	msg := "LCM: SSH ist nur für IPv6-Quellen freigegeben, ufw hat IPv6 aber abgeschaltet (IPV6=no) - abgebrochen, die Firewall sperrte sonst SSH aus"
	return `{ [ "$LCM_UFW_V6" = yes ] || { echo ` + shellQuote(msg) + ` >&2; exit 1; }; }`
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
//
// Reine IPv6-Freigaben laufen nur, wenn ufw IPv6 eingeschaltet hat
// (ufwIPv6Probe); sonst stehen sie als Hinweis in der Ausgabe.
func ufwEnableScript(ssh domain.FirewallRule, rules []domain.FirewallRule) string {
	// Die SSH-Freigabe läuft durch denselben Renderer wie jede andere Regel
	// (sie kann Quellen tragen); der Regelsatz-Hash hängt als Kommentar dran.
	sshLines := ufwRuleLines(ssh, "lcm:"+firewallRulesHash(ssh, rules))
	steps := []string{
		"command -v ufw >/dev/null 2>&1 || { echo '" + firewallToolMissingMarker(domain.FirewallToolUfw) + "'; exit 1; }",
		ufwIPv6Probe,
	}
	if guard := ufwSSHGuard(sshLines); guard != "" {
		steps = append(steps, guard)
	}
	steps = append(steps, "ufw --force reset", "ufw default deny incoming", "ufw default allow outgoing")
	lines := sshLines
	for _, r := range rules {
		lines = append(lines, ufwRuleLines(r, ufwComment("lcm", r.Comment))...)
	}
	for _, l := range lines {
		steps = append(steps, ufwGuardedCmd(l))
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

// reUfwRule zerlegt eine Regelzeile von `ufw status` (auch `verbose` und
// `numbered`) in Ziel, Aktion, Richtung und Quelle. Die Spalten sind nicht
// verlässlich durch zwei Leerzeichen getrennt - ein langes IPv6-Ziel füllt
// seine Spalte ganz aus -, die Aktion in Großbuchstaben steht aber immer
// dazwischen. Ohne `verbose` fehlt bei eingehenden Regeln das IN.
var reUfwRule = regexp.MustCompile(`^(?:\[\s*\d+\]\s*)?(\S.*?)\s+(ALLOW|DENY|REJECT|LIMIT)(?:\s+(IN|OUT|FWD))?\s+(\S.*)$`)

// ufwOpenRules liest die Regeln aus dem Status, die etwas ÖFFNEN (ALLOW und
// LIMIT, eingehend oder weitergeleitet), als Menge von Schlüsseln. Erfasst
// wird jede solche Zeile, auch eine ohne Port: `ufw allow from 1.2.3.4`
// erscheint als „Anywhere  ALLOW IN  1.2.3.4" und öffnet für diese Quelle
// alles. Sperrende Regeln (DENY, REJECT) und ausgehende bleiben außen vor -
// sie öffnen nichts.
func ufwOpenRules(status string) map[string]bool {
	keys := map[string]bool{}
	for _, line := range strings.Split(status, "\n") {
		m := reUfwRule.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || m[3] == "OUT" || (m[2] != "ALLOW" && m[2] != "LIMIT") {
			continue
		}
		dir := m[3]
		if dir == "" {
			dir = "IN"
		}
		from, _, _ := strings.Cut(m[4], "#")
		keys[ufwRuleKey(m[2]+" "+dir, m[1], from)] = true
	}
	return keys
}

// ufwRuleKey bildet den Vergleichsschlüssel einer Regel aus Aktion, Ziel und
// Quelle. Adressen werden kanonisch geschrieben (10.0.0.5/32 = 10.0.0.5,
// IPv6 in Kurzform), sonst zählten Schreibweisen als Abweichung.
func ufwRuleKey(action, to, from string) string {
	return action + "|" + ufwFieldKey(to) + "|" + ufwFieldKey(from)
}

// ufwFieldKey normalisiert eine Spalte: Leerraum zusammengefasst, Adressen
// kanonisch. Die Quelle 0.0.0.0/0 bzw. ::/0 zeigt ufw als „Anywhere" - ohne
// diese Übersetzung wäre eine so eingetragene Quelle eine Dauer-Abweichung.
func ufwFieldKey(field string) string {
	tokens := strings.Fields(field)
	for i, tok := range tokens {
		canon, v4, err := canonicalAddr(tok)
		if err != nil {
			continue
		}
		switch {
		case canon == "0.0.0.0/0":
			canon = "Anywhere"
		case canon == "::/0":
			canon = "Anywhere (v6)"
		case v4:
			canon = strings.TrimSuffix(canon, "/32")
		default:
			canon = strings.TrimSuffix(canon, "/128")
		}
		tokens[i] = canon
	}
	return strings.Join(tokens, " ")
}

// ufwWantRules sind die Schlüssel, die die Freigaben einer Regel im Status
// erzeugen. ufw zeigt das Ziel 0.0.0.0/0 bzw. ::/0 nicht an, sondern nur den
// Port. Ohne Quelle entsteht je Familie eine Zeile mit „Anywhere", die
// IPv6-Hälfte mit „(v6)" an Ziel und Quelle. Mit Quelle entsteht eine Zeile
// je Quell-Adresse, und dort hängt ufw das „(v6)" NICHT an, auch nicht bei
// einer IPv6-Quelle: „5432/tcp  ALLOW IN  2001:db8::/32" (an ufw 0.36
// nachgesehen).
func ufwWantRules(r domain.FirewallRule) []string {
	port := fmt.Sprintf("%d/%s", r.Port, r.Proto)
	if len(r.Sources) == 0 {
		var keys []string
		if r.IPVersion != "v6" {
			keys = append(keys, ufwRuleKey("ALLOW IN", port, "Anywhere"))
		}
		if r.IPVersion != "v4" {
			keys = append(keys, ufwRuleKey("ALLOW IN", port+" (v6)", "Anywhere (v6)"))
		}
		return keys
	}
	keys := make([]string, 0, len(r.Sources))
	for _, src := range r.Sources {
		keys = append(keys, ufwRuleKey("ALLOW IN", port, src))
	}
	return keys
}

// ufwInSync prüft, ob der ufw-Status dem Soll einer Firewall-Regel entspricht:
// aktiv, der Regelsatz-Hash sichtbar UND die öffnenden Regeln im Status
// EXAKT die von SSH und Regelsatz - mit Port, Protokoll, Familie und Quelle.
// Der Hash allein erkennt nichts, was von Hand an den Regeln geändert wurde;
// ein Vergleich nur über Port/Protokoll übersah eine geänderte Quell-Adresse
// und jede Freigabe ohne Port. Nur bei Abweichung wird neu angewendet.
//
// Zeigt der Status gar keine IPv6-Regel, fordert der Vergleich auch keine:
// Mit IPV6=no in /etc/default/ufw legt ufw die IPv6-Hälfte nie an, und
// Freigaben für IPv6-Quellen lässt das Skript dort aus - die Firewall würde
// sonst bei jedem Lauf neu aufgesetzt. Durchgelassen wird damit nur ein
// FEHLEN aller IPv6-Freigaben - das schließt, öffnet nichts.
func ufwInSync(statusOutput string, ssh domain.FirewallRule, rules []domain.FirewallRule) bool {
	if !ufwActiveFromOutput(statusOutput) {
		return false
	}
	if !strings.Contains(statusOutput, "lcm:"+firewallRulesHash(ssh, rules)) {
		return false
	}
	got := ufwOpenRules(statusOutput)
	withV6 := false
	for key := range got {
		withV6 = withV6 || ufwKeyIsV6(key)
	}
	want := map[string]bool{}
	for _, r := range append([]domain.FirewallRule{ssh}, rules...) {
		for _, key := range ufwWantRules(r) {
			if withV6 || !ufwKeyIsV6(key) {
				want[key] = true
			}
		}
	}
	if len(got) != len(want) {
		return false
	}
	for key := range want {
		if !got[key] {
			return false
		}
	}
	return true
}

// ufwKeyIsV6 meldet, ob ein Schlüssel aus ufwRuleKey zu IPv6 gehört: Ziel
// mit „(v6)" oder eine IPv6-Quelle (die zeigt ufw ohne den Zusatz).
func ufwKeyIsV6(key string) bool {
	parts := strings.SplitN(key, "|", 3)
	return len(parts) == 3 && (strings.Contains(parts[1], "(v6)") || strings.Contains(parts[2], ":"))
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
