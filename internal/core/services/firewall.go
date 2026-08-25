package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
)

// parseFirewallPorts normalisiert eine kommagetrennte Portliste: nur gültige
// TCP-Ports (1-65535), dedupliziert, in Eingabereihenfolge. Ungültige
// Einträge werden verworfen.
func parseFirewallPorts(s string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			continue
		}
		key := strconv.Itoa(n)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// firewallToolMissingPrefix meldet dem Aufrufer, dass das Firewall-Werkzeug
// auf dem Zielsystem gar nicht vorhanden ist. Zuvor endete das Skript in
// diesem Fall mit exit 0 und die API antwortete "200 ok" - über eine
// Gruppenregel, die über viele Server läuft, war damit nicht erkennbar, dass
// die Absicherung faktisch nie stattgefunden hat (BUG-024). Der Marker wird
// als Präfix geprüft, weil je Backend das konkrete Werkzeug benannt wird.
const firewallToolMissingPrefix = "LCM-FEHLER: Firewall-Werkzeug"

// firewallToolMissingMarker baut die vollständige Fehlerzeile für ein Backend.
func firewallToolMissingMarker(tool string) string {
	return firewallToolMissingPrefix + " " + tool + " ist auf diesem System nicht installiert"
}

// firewallInstallFailedMarker: die automatische Installation des vorgesehenen
// Firewall-Werkzeugs ist fehlgeschlagen (Paket nicht verfügbar o.ä.).
const firewallInstallFailedMarker = "LCM-FEHLER: Firewall-Werkzeug konnte nicht installiert werden"

// parseFirewallRuleSpec akzeptiert beide Regel-Formate: beginnt der getrimmte
// String mit '[', ist es ein JSON-Array von FirewallRule; sonst die
// Legacy-CSV-Portliste (nur TCP, alle Adressen). Das Ergebnis ist immer
// normalisiert (validiert, dedupliziert, kanonisch sortiert).
func parseFirewallRuleSpec(s string) ([]domain.FirewallRule, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var rules []domain.FirewallRule
		if err := json.Unmarshal([]byte(trimmed), &rules); err != nil {
			return nil, fmt.Errorf("ungültiges Firewall-Regel-JSON: %w", err)
		}
		return normalizeFirewallRules(rules)
	}
	var rules []domain.FirewallRule
	for _, p := range parseFirewallPorts(trimmed) {
		n, _ := strconv.Atoi(p)
		rules = append(rules, domain.FirewallRule{Port: n, Proto: "tcp"})
	}
	return normalizeFirewallRules(rules)
}

// normalizeFirewallRules validiert, normalisiert, dedupliziert und sortiert
// Regeln kanonisch (stabile Grundlage für Hash- und In-Sync-Vergleiche).
// Anders als die nachsichtige Legacy-CSV (ungültige Einträge verwerfen) sind
// explizite JSON-Regeln streng: jeder Fehler bricht ab - eine still
// verworfene Freigabe wäre schwer zu diagnostizieren.
func normalizeFirewallRules(rules []domain.FirewallRule) ([]domain.FirewallRule, error) {
	seen := map[string]bool{}
	out := []domain.FirewallRule{}
	for _, r := range rules {
		if r.Port < 1 || r.Port > 65535 {
			return nil, fmt.Errorf("ungültiger Port %d (erlaubt: 1-65535)", r.Port)
		}
		proto := strings.ToLower(strings.TrimSpace(r.Proto))
		if proto == "" {
			proto = "tcp"
		}
		if proto != "tcp" && proto != "udp" {
			return nil, fmt.Errorf("ungültiges Protokoll %q (erlaubt: tcp, udp)", r.Proto)
		}
		ipv := strings.ToLower(strings.TrimSpace(r.IPVersion))
		if ipv == "" {
			ipv = "any"
		}
		if ipv != "any" && ipv != "v4" && ipv != "v6" {
			return nil, fmt.Errorf("ungültige IP-Version %q (erlaubt: any, v4, v6)", r.IPVersion)
		}
		ids := normalizeAllowlistIDs(r.AllowlistIDs)
		srcIPs, err := normalizeSourceIPs(r.SourceIPs)
		if err != nil {
			return nil, err
		}
		// Eine AUSDRÜCKLICH mitgeschickte, aber leere source_ips-Liste ohne
		// Allowlist ist abzulehnen: sie wurde als „keine Einschränkung"
		// gelesen und öffnete den Port still für jeden - das genaue
		// Gegenteil dessen, was jemand beabsichtigt, der eine
		// Quell-Beschränkung leert (R2-083, fail-open). Wer wirklich keine
		// Einschränkung will, lässt das Feld weg.
		if r.SourceIPs != nil && len(srcIPs) == 0 && len(ids) == 0 {
			return nil, fmt.Errorf("Regel Port %d: leere source_ips-Liste - Quell-IPs angeben, eine Allowlist wählen oder das Feld ganz weglassen (= bewusst ohne Einschränkung)", r.Port)
		}
		// Manuelle Quell-IPs müssen zur IP-Version der Regel passen - eine
		// v6-Quelle an einer v4-Regel wäre nie wirksam und deutet auf einen
		// Eingabefehler hin.
		if ipv != "any" {
			for _, src := range srcIPs {
				srcVer := "v4"
				if strings.Contains(src, ":") {
					srcVer = "v6"
				}
				if srcVer != ipv {
					return nil, fmt.Errorf("Quell-IP %s passt nicht zur IP-Version %s", src, ipv)
				}
			}
		}
		norm := domain.FirewallRule{
			Port: r.Port, Proto: proto, IPVersion: ipv,
			AllowlistIDs: ids, SourceIPs: srcIPs, Comment: firewallCommentClean(r.Comment),
		}
		// Die Bemerkung gehört NICHT in den Dedup-Schlüssel: zwei Regeln, die
		// sich nur in der Notiz unterscheiden, sind dieselbe Freigabe.
		key := fmt.Sprintf("%d/%s/%s/%v/%v", norm.Port, norm.Proto, norm.IPVersion, ids, srcIPs)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, norm)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}
		if a.IPVersion != b.IPVersion {
			return a.IPVersion < b.IPVersion
		}
		if ka, kb := fmt.Sprint(a.AllowlistIDs), fmt.Sprint(b.AllowlistIDs); ka != kb {
			return ka < kb
		}
		return fmt.Sprint(a.SourceIPs) < fmt.Sprint(b.SourceIPs)
	})
	return out, nil
}

// normalizeSourceIPs kanonisiert händisch eingetragene Quell-IPs/-CIDRs einer
// Regel (via canonicalAddr), dedupliziert und sortiert sie. Wie bei den
// JSON-Regeln insgesamt gilt: streng - ein ungültiger Eintrag bricht ab,
// statt still verworfen zu werden.
func normalizeSourceIPs(ips []string) ([]string, error) {
	if len(ips) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, raw := range ips {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		canon, _, err := canonicalAddr(s)
		if err != nil {
			return nil, fmt.Errorf("ungültige Quell-IP %q (IP oder CIDR erwartet)", s)
		}
		if !seen[canon] {
			seen[canon] = true
			out = append(out, canon)
		}
	}
	sort.Strings(out)
	return out, nil
}

// normalizeAllowlistIDs sortiert, dedupliziert und verwirft Nullen.
func normalizeAllowlistIDs(ids []uint) []uint {
	if len(ids) == 0 {
		return nil
	}
	seen := map[uint]bool{}
	var out []uint
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// expandRuleAllowlists löst die Quell-Einschränkung jeder Regel in konkrete
// Quell-CIDRs (rule.Sources) auf - kurz VOR dem Rendern und dem Hash. Die
// Quellen sind die Union aus den händischen SourceIPs und den über die
// injizierte Closure aufgelösten AllowlistIDs (dedupliziert, sortiert).
// Verlangt eine Regel eine Einschränkung (SourceIPs oder AllowlistIDs
// gesetzt), deren Auflösung aber leer ist (Liste leer oder Familie passt
// nicht), wird sie ausgelassen: der Port bleibt zu, LCM öffnet ihn NIE
// versehentlich für alle. Jede so ausgelassene Regel wird als WARNUNG
// benannt - vorher verschwand sie wortlos, und Anzeige, Datenbestand und
// Audit-Log behaupteten übereinstimmend einen offenen Port, den es auf dem
// System nicht gab (R2-071). Eine UNBEKANNTE Allowlist-ID ist ein Fehler
// (ExpandIPAllowlists). Rückgabe: anzuwendende Regeln + Warnungen.
func expandRuleAllowlists(rules []domain.FirewallRule, expand func([]uint) ([]string, error)) ([]domain.FirewallRule, []string, error) {
	out := make([]domain.FirewallRule, 0, len(rules))
	var warnings []string
	for _, r := range rules {
		if len(r.AllowlistIDs) == 0 && len(r.SourceIPs) == 0 {
			r.Sources = nil
			out = append(out, r)
			continue
		}
		sources := append([]string(nil), r.SourceIPs...)
		if len(r.AllowlistIDs) > 0 && expand != nil {
			s, err := expand(r.AllowlistIDs)
			if err != nil {
				return nil, nil, err
			}
			sources = append(sources, s...)
		}
		sources = dedupSortedStrings(sources)
		// Quellen der falschen Adressfamilie verwerfen: eine v4-Regel darf
		// durch einen v6-Allowlist-Eintrag nie eine v6-Freigabe bekommen
		// (und umgekehrt) - sonst würden die Renderer eine Familien-fremde
		// Regel erzeugen.
		if r.IPVersion == "v4" || r.IPVersion == "v6" {
			v4, v6 := sourcesByFamily(sources)
			if r.IPVersion == "v4" {
				sources = v4
			} else {
				sources = v6
			}
		}
		if len(sources) == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"LCM-WARNUNG: Regel Port %d/%s übersprungen - die Quell-Einschränkung löst zu 0 IPs auf (Allowlist leer oder Adressfamilie passt nicht); der Port bleibt GESCHLOSSEN.",
				r.Port, r.Proto))
			continue
		}
		r.Sources = sources
		out = append(out, r)
	}
	return out, warnings, nil
}

// dedupSortedStrings dedupliziert und sortiert eine String-Liste (leere
// Einträge werden verworfen); leeres Ergebnis = nil.
func dedupSortedStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// sourcesByFamily teilt kanonische Quell-CIDRs nach Adressfamilie (v4/v6).
func sourcesByFamily(sources []string) (v4, v6 []string) {
	for _, s := range sources {
		if strings.Contains(s, ":") {
			v6 = append(v6, s)
		} else {
			v4 = append(v4, s)
		}
	}
	return v4, v6
}

// canonicalAddr prüft eine Adresse (IP oder CIDR) und liefert die kanonische
// Schreibweise plus die Adressfamilie. Die Werte landen in Firewall-
// Konfigurationen - nur geparste Adressen sind zulässig.
func canonicalAddr(s string) (canon string, v4 bool, err error) {
	if ip := net.ParseIP(s); ip != nil {
		return ip.String(), ip.To4() != nil, nil
	}
	if ip, ipnet, cidrErr := net.ParseCIDR(s); cidrErr == nil {
		return ipnet.String(), ip.To4() != nil, nil
	}
	return "", false, fmt.Errorf("ungültige Adresse %q (IP oder CIDR erwartet)", s)
}

// firewallRulesJSON serialisiert normalisierte Regeln kanonisch (leer = "[]").
func firewallRulesJSON(rules []domain.FirewallRule) string {
	if len(rules) == 0 {
		return "[]"
	}
	b, err := json.Marshal(rules)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// firewallRulesPortsCSV verdichtet Regeln zur Legacy-Portliste (aufsteigend,
// dedupliziert) - für FirewallAllowedPorts (Anzeige + Abwärtskompatibilität).
func firewallRulesPortsCSV(rules []domain.FirewallRule) string {
	seen := map[int]bool{}
	ports := []int{}
	for _, r := range rules {
		if !seen[r.Port] {
			seen[r.Port] = true
			ports = append(ports, r.Port)
		}
	}
	sort.Ints(ports)
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ",")
}

// firewallRulesHash bildet einen stabilen Kurz-Hash über SSH-Port, SSH-Quellen
// und den kanonischen Regelsatz. Er wird als Kommentar in die Firewall
// geschrieben (ufw comment / nft-Regel-Kommentar) und erlaubt der
// Grundsatz-Regel-Prüfung einen billigen Drift-Vergleich, der auch
// IP-Version, Quellen und Bemerkung erfasst.
func firewallRulesHash(ssh domain.FirewallRule, rules []domain.FirewallRule) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ssh=%d/%v/%v;", ssh.Port, ssh.AllowlistIDs, ssh.Sources)
	for _, r := range rules {
		// Sowohl die Referenzen als auch die aufgelösten Quellen fließen ein:
		// so erkennt die Grundsatz-Regel-Prüfung auch geänderte Listeninhalte.
		// Die Bemerkung zählt mit: Sie steht bei ufw/nftables im Regelsatz
		// auf dem Zielsystem, eine Änderung ist also eine echte Abweichung.
		fmt.Fprintf(&b, "%d/%s/%s/%v/%v/%s;", r.Port, r.Proto, r.IPVersion, r.AllowlistIDs, r.Sources, r.Comment)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:12]
}

// sshFirewallRule baut die immer erzwungene SSH-Freigabe als FirewallRule:
// der SSH-Port (TCP) für beide Adressfamilien, eingeschränkt auf die
// erlaubten Quellen. So rendern die Backends die SSH-Regel über denselben Weg
// wie jede andere Regel.
func sshFirewallRule(sshPort int, src domain.FirewallSSHSources) domain.FirewallRule {
	return domain.FirewallRule{
		Port: sshPort, Proto: "tcp", IPVersion: "any",
		AllowlistIDs: src.AllowlistIDs, SourceIPs: src.SourceIPs,
	}
}

// firewallRulesFromServer liest die maßgebliche Regel-Konfiguration eines
// Servers: FirewallRules (JSON), sonst die Legacy-CSV FirewallAllowedPorts.
// Best effort - eine unlesbare Konfiguration ergibt eine leere Liste
// (SSH bleibt trotzdem immer freigegeben).
func firewallRulesFromServer(server *domain.Server) []domain.FirewallRule {
	spec := strings.TrimSpace(server.FirewallRules)
	if spec == "" || spec == "[]" {
		spec = server.FirewallAllowedPorts
	}
	rules, err := parseFirewallRuleSpec(spec)
	if err != nil {
		return nil
	}
	return rules
}

// parseSSHSources liest die Quell-Einschränkung der SSH-Freigabe aus ihrer
// JSON-Ablage. Unlesbares gilt als „keine Einschränkung" - die SSH-Freigabe
// darf an einem kaputten Feld nicht scheitern, sonst sperrt sie aus.
func parseSSHSources(raw string) domain.FirewallSSHSources {
	var src domain.FirewallSSHSources
	if strings.TrimSpace(raw) == "" {
		return src
	}
	if err := json.Unmarshal([]byte(raw), &src); err != nil {
		return domain.FirewallSSHSources{}
	}
	return src
}

// sshSourcesJSON serialisiert die Quell-Einschränkung; leer → "".
func sshSourcesJSON(src domain.FirewallSSHSources) string {
	if len(src.AllowlistIDs) == 0 && len(src.SourceIPs) == 0 {
		return ""
	}
	b, err := json.Marshal(src)
	if err != nil {
		return ""
	}
	return string(b)
}

// serverSSHRule baut die SSH-Freigabe eines Servers aus Port und den
// gespeicherten Quell-Einschränkungen.
func serverSSHRule(server *domain.Server) domain.FirewallRule {
	return sshFirewallRule(server.SSHPort, parseSSHSources(server.FirewallSSHSources))
}

// expandSSHAndRules löst die Allowlists der SSH-Freigabe UND der übrigen
// Regeln in einem Zug auf - die SSH-Zeile läuft dabei durch dieselbe
// Familien- und Dedup-Behandlung wie jede andere Regel.
func expandSSHAndRules(ssh domain.FirewallRule, rules []domain.FirewallRule,
	expand func([]uint) ([]string, error)) (domain.FirewallRule, []domain.FirewallRule, []string, error) {
	all, warnings, err := expandRuleAllowlists(append([]domain.FirewallRule{ssh}, rules...), expand)
	if err != nil {
		return ssh, nil, warnings, err
	}
	return all[0], all[1:], warnings, nil
}

// firewallCommentMax begrenzt die Bemerkung. Sie landet im Regelsatz auf dem
// Zielsystem (ufw-Kommentar, nft-comment) - beide vertragen keine beliebig
// langen Werte, und als Notiz reicht eine Zeile.
const firewallCommentMax = 120

// firewallCommentClean macht aus der freien Eingabe einen Wert, der gefahrlos
// in einen Regelsatz wandert: eine Zeile, keine Steuerzeichen, keine
// Anführungszeichen (die würden die nft-Syntax zerlegen), begrenzte Länge.
func firewallCommentClean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '"' || r == '\'' || r == '\\':
			return ' '
		case r < 0x20 || r == 0x7f:
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > firewallCommentMax {
		s = strings.TrimSpace(s[:firewallCommentMax])
	}
	return s
}
