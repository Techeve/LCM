package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestFirewallDesignatedBackend prüft die Distribution→Werkzeug-Zuordnung:
// Ubuntu→ufw; RHEL-Welt/Fedora/openSUSE→firewalld; Debian/Arch/Alpine→nftables;
// Unbekanntes→ufw (dokumentierter Standard).
func TestFirewallDesignatedBackend(t *testing.T) {
	cases := map[string]string{
		"ubuntu":              domain.FirewallToolUfw,
		"rhel":                domain.FirewallToolFirewalld,
		"rocky":               domain.FirewallToolFirewalld,
		"almalinux":           domain.FirewallToolFirewalld,
		"fedora":              domain.FirewallToolFirewalld,
		"centos":              domain.FirewallToolFirewalld,
		"centos-stream":       domain.FirewallToolFirewalld,
		"sles":                domain.FirewallToolFirewalld,
		"opensuse-leap":       domain.FirewallToolFirewalld,
		"opensuse-tumbleweed": domain.FirewallToolFirewalld,
		"debian":              domain.FirewallToolNftables,
		"arch":                domain.FirewallToolNftables,
		"alpine":              domain.FirewallToolNftables,
		"gentoo":              domain.FirewallToolUfw, // unbekannt → Default
		"":                    domain.FirewallToolUfw,
		"  Ubuntu  ":          domain.FirewallToolUfw, // Normalisierung
	}
	for osID, want := range cases {
		if got := firewallDesignatedBackend(osID); got != want {
			t.Errorf("osID %q: backend %q, erwartet %q", osID, got, want)
		}
	}
}

// TestFirewallBackendForDetectedWins: ein erkanntes Werkzeug schlägt das
// für die Distribution vorgesehene (nie eine zweite Firewall installieren).
func TestFirewallBackendForDetectedWins(t *testing.T) {
	s := &domain.Server{OSID: "ubuntu", FirewallTool: domain.FirewallToolFirewalld}
	if got := firewallBackendFor(s); got != domain.FirewallToolFirewalld {
		t.Errorf("erkanntes firewalld muss gewinnen, bekam %q", got)
	}
	s = &domain.Server{OSID: "debian"} // nichts erkannt → designated
	if got := firewallBackendFor(s); got != domain.FirewallToolNftables {
		t.Errorf("debian ohne erkanntes tool: %q, erwartet nftables", got)
	}
	s = &domain.Server{OSID: "rocky", FirewallTool: "unfug"} // ungültig → designated
	if got := firewallBackendFor(s); got != domain.FirewallToolFirewalld {
		t.Errorf("ungültiges tool darf nicht gewinnen: %q", got)
	}
}

// TestParseFirewallRuleSpec: JSON- und Legacy-CSV-Format.
func TestParseFirewallRuleSpec(t *testing.T) {
	// Legacy-CSV: nur TCP/any, ungültige Einträge nachsichtig verworfen.
	rules, err := parseFirewallRuleSpec("80, 443, 443, abc, 70000")
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	if len(rules) != 2 || rules[0].Port != 80 || rules[0].Proto != "tcp" || rules[0].IPVersion != "any" || rules[1].Port != 443 {
		t.Errorf("csv falsch geparst: %+v", rules)
	}
	// JSON (auch mit führendem Leerraum).
	rules, err = parseFirewallRuleSpec(`  [{"port":53,"proto":"udp","ip_version":"v4"},{"port":443,"proto":"tcp","comment":"Webshop"}]`)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(rules) != 2 || rules[0].Port != 53 || rules[0].Proto != "udp" || rules[0].IPVersion != "v4" {
		t.Errorf("json falsch geparst: %+v", rules)
	}
	if rules[1].Comment != "Webshop" {
		t.Errorf("bemerkung ging beim parsen verloren: %+v", rules[1])
	}
	// Altbestand: ein gespeichertes "bind" gibt es nicht mehr - es darf beim
	// Lesen weder stören noch heimlich weiterwirken.
	old, err := parseFirewallRuleSpec(`[{"port":443,"proto":"tcp","bind":"10.0.0.5","ip_version":"v4"}]`)
	if err != nil || len(old) != 1 || old[0].Port != 443 || old[0].IPVersion != "v4" {
		t.Errorf("alter regelsatz mit bind nicht lesbar: %+v (%v)", old, err)
	}
	// Kaputtes JSON ist ein harter Fehler (keine stille Nachsicht).
	if _, err := parseFirewallRuleSpec(`[{"port":`); err == nil {
		t.Error("kaputtes json muss fehlschlagen")
	}
	// Leer = keine Zusatz-Regeln.
	if rules, err := parseFirewallRuleSpec("  "); err != nil || len(rules) != 0 {
		t.Errorf("leerer spec: rules=%v err=%v", rules, err)
	}
}

// TestNormalizeFirewallRules: Validierung, Normalisierung, Dedup, Sortierung.
func TestNormalizeFirewallRules(t *testing.T) {
	// Portgrenzen.
	if _, err := normalizeFirewallRules([]domain.FirewallRule{{Port: 0, Proto: "tcp"}}); err == nil {
		t.Error("port 0 muss fehlschlagen")
	}
	if _, err := normalizeFirewallRules([]domain.FirewallRule{{Port: 70000, Proto: "tcp"}}); err == nil {
		t.Error("port 70000 muss fehlschlagen")
	}
	// Protokoll-Whitelist ("" → tcp, Groß-/Kleinschreibung egal).
	rules, err := normalizeFirewallRules([]domain.FirewallRule{{Port: 80}, {Port: 53, Proto: "UDP"}})
	if err != nil || rules[1].Proto != "tcp" && rules[0].Proto != "tcp" {
		t.Fatalf("proto-normalisierung: %+v (%v)", rules, err)
	}
	if _, err := normalizeFirewallRules([]domain.FirewallRule{{Port: 80, Proto: "icmp"}}); err == nil {
		t.Error("proto icmp muss fehlschlagen")
	}
	// IP-Version-Whitelist.
	if _, err := normalizeFirewallRules([]domain.FirewallRule{{Port: 80, Proto: "tcp", IPVersion: "v5"}}); err == nil {
		t.Error("ip-version v5 muss fehlschlagen")
	}
	// Quell-IPs: ungültig, CIDR-Kanonisierung, Familien-Konflikt.
	if _, err := normalizeFirewallRules([]domain.FirewallRule{{Port: 80, Proto: "tcp", SourceIPs: []string{"kein-ip"}}}); err == nil {
		t.Error("ungültige quell-ip muss fehlschlagen")
	}
	rules, err = normalizeFirewallRules([]domain.FirewallRule{{Port: 80, Proto: "tcp", SourceIPs: []string{"10.1.2.3/24"}}})
	if err != nil || len(rules[0].SourceIPs) != 1 || rules[0].SourceIPs[0] != "10.1.2.0/24" {
		t.Errorf("cidr-kanonisierung: %+v (%v)", rules, err)
	}
	if _, err := normalizeFirewallRules([]domain.FirewallRule{{Port: 80, Proto: "tcp", IPVersion: "v6", SourceIPs: []string{"10.0.0.1"}}}); err == nil {
		t.Error("v4-quelle an einer v6-regel muss fehlschlagen")
	}
	// Dedup + kanonische Sortierung (Port, Proto, Version).
	rules, _ = normalizeFirewallRules([]domain.FirewallRule{
		{Port: 443, Proto: "tcp"}, {Port: 80, Proto: "udp"}, {Port: 80, Proto: "tcp"}, {Port: 443, Proto: "TCP"},
	})
	if len(rules) != 3 || rules[0].Port != 80 || rules[0].Proto != "tcp" || rules[1].Proto != "udp" || rules[2].Port != 443 {
		t.Errorf("dedup/sortierung: %+v", rules)
	}
}

// TestFirewallRulesHelpers: CSV-Zusammenfassung + stabiler Hash.
func TestFirewallRulesHelpers(t *testing.T) {
	rules, _ := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 443, Proto: "tcp"}, {Port: 443, Proto: "udp"}, {Port: 80, Proto: "tcp"},
	})
	if csv := firewallRulesPortsCSV(rules); csv != "80,443" {
		t.Errorf("ports-csv: %q", csv)
	}
	h1 := firewallRulesHash(sshRuleFor(22), rules)
	h2 := firewallRulesHash(sshRuleFor(22), rules)
	if h1 != h2 || len(h1) != 12 {
		t.Errorf("hash instabil oder falsche länge: %q vs %q", h1, h2)
	}
	if firewallRulesHash(sshRuleFor(2222), rules) == h1 {
		t.Error("ssh-port muss den hash ändern")
	}
	other, _ := normalizeFirewallRules([]domain.FirewallRule{{Port: 80, Proto: "tcp", IPVersion: "v4"}})
	if firewallRulesHash(sshRuleFor(22), other) == h1 {
		t.Error("bind muss den hash ändern")
	}
}

// TestSSHSourceRendering: die (nie löschbare) SSH-Freigabe lässt sich über die
// erlaubten Quellen einschränken - in allen drei Backends korrekt gerendert.
// Eine Ziel-/Bind-Adresse gibt es nicht mehr; die Quelle ist die Frage, die
// zählt („von wo darf sich jemand anmelden").
func TestSSHSourceRendering(t *testing.T) {
	rules := []domain.FirewallRule{{Port: 443, Proto: "tcp", IPVersion: "any"}}
	// Die Quellen werden vor dem Rendern aufgelöst (wie im Betrieb).
	restricted := func(port int, srcs ...string) domain.FirewallRule {
		r := sshRuleFor(port, srcs...)
		r.Sources = srcs
		return r
	}

	// Ohne Quell-Einschränkung: SSH offen für alle (v4+v6).
	if ufw := ufwEnableScript(sshRuleFor(2222), rules); !strings.Contains(ufw, "ufw allow proto tcp to any port 2222 comment 'lcm:") {
		t.Errorf("ufw ohne quellen falsch:\n%s", ufw)
	}
	// Mit Quelle: nur diese Adresse darf auf den SSH-Port.
	ufw := ufwEnableScript(restricted(2222, "10.0.0.5"), rules)
	if !strings.Contains(ufw, "ufw allow proto tcp from 10.0.0.5 to any port 2222 comment 'lcm:") {
		t.Errorf("ufw mit quelle falsch:\n%s", ufw)
	}

	// firewalld: ohne Quellen --add-port, mit Quelle Rich-Rule.
	if fd := firewalldEnableScript(sshRuleFor(2222), rules); !strings.Contains(fd, `--add-port=2222/tcp`) {
		t.Errorf("firewalld ohne quellen falsch:\n%s", fd)
	}
	fd := firewalldEnableScript(restricted(2222, "10.0.0.0/24"), rules)
	if !strings.Contains(fd, `rule family="ipv4" source address="10.0.0.0/24" port port="2222" protocol="tcp" accept`) {
		t.Errorf("firewalld mit quelle falsch:\n%s", fd)
	}
	if strings.Contains(fd, `--add-port=2222/tcp`) {
		t.Errorf("firewalld: eingeschränktes SSH darf nicht als --add-port erscheinen:\n%s", fd)
	}

	// nftables: ohne Quellen schlicht, mit v6-Quelle ip6 saddr.
	if nft := nftRuleset(sshRuleFor(2222), rules); !strings.Contains(nft, "tcp dport 2222 accept") {
		t.Errorf("nft ohne quellen falsch:\n%s", nft)
	}
	if nft := nftRuleset(restricted(2222, "2001:db8::1"), rules); !strings.Contains(nft, "ip6 saddr { 2001:db8::1 } tcp dport 2222 accept") {
		t.Errorf("nft mit v6-quelle falsch:\n%s", nft)
	}

	// Die SSH-Quellen fließen in den Hash ein (Drift-Erkennung).
	if firewallRulesHash(sshRuleFor(2222), rules) == firewallRulesHash(restricted(2222, "10.0.0.5"), rules) {
		t.Error("ssh-quellen müssen den hash ändern")
	}
	// sshFirewallRule gilt immer für beide Adressfamilien.
	if r := sshRuleFor(22); r.IPVersion != "any" || r.Proto != "tcp" || r.Port != 22 {
		t.Errorf("sshFirewallRule falsch: %+v", r)
	}
}

// TestAllowlistSourceRendering: eine Regel mit Quell-Allowlist gibt den Port
// nur für die Allowlist-IPs frei - je Backend korrekt gerendert.
func TestAllowlistSourceRendering(t *testing.T) {
	// Regel mit aufgelösten Quellen (v4 + v6 gemischt).
	r := domain.FirewallRule{Port: 8443, Proto: "tcp", Sources: []string{"10.0.0.0/24", "192.168.1.5", "2001:db8::/32"}}

	// ufw: je Quelle eine from-Zeile.
	ufw := ufwRuleCmds(r)
	if len(ufw) != 3 {
		t.Fatalf("ufw: erwartet 3 zeilen, bekam %d: %v", len(ufw), ufw)
	}
	if !strings.Contains(ufw[0], "ufw allow proto tcp from 10.0.0.0/24 to any port 8443") {
		t.Errorf("ufw-quell-zeile falsch: %v", ufw)
	}

	// firewalld: je Quelle eine Rich-Rule mit source address (v4 vor v6).
	fd := firewalldRichRules(r)
	if len(fd) != 3 {
		t.Fatalf("firewalld: erwartet 3 rich-rules, bekam %d: %v", len(fd), fd)
	}
	if fd[0] != `rule family="ipv4" source address="10.0.0.0/24" port port="8443" protocol="tcp" accept` {
		t.Errorf("firewalld-quell-rich-rule falsch: %q", fd[0])
	}
	if !strings.Contains(fd[2], `family="ipv6" source address="2001:db8::/32"`) {
		t.Errorf("firewalld-v6-quelle falsch: %q", fd[2])
	}

	// nftables: je Familie ein saddr-Set.
	nft := nftRuleLines(r)
	if len(nft) != 2 {
		t.Fatalf("nft: erwartet 2 zeilen (v4+v6), bekam %d: %v", len(nft), nft)
	}
	if nft[0] != "ip saddr { 10.0.0.0/24, 192.168.1.5 } tcp dport 8443 accept" {
		t.Errorf("nft-v4-saddr falsch: %q", nft[0])
	}
	if nft[1] != "ip6 saddr { 2001:db8::/32 } tcp dport 8443 accept" {
		t.Errorf("nft-v6-saddr falsch: %q", nft[1])
	}

	// Ohne Quellen: klassische einzelne Freigabe (kein from/saddr).
	plain := domain.FirewallRule{Port: 80, Proto: "tcp", IPVersion: "any"}
	if got := ufwRuleCmds(plain); len(got) != 1 || strings.Contains(got[0], "from ") {
		t.Errorf("ufw ohne quellen falsch: %v", got)
	}
	if got := nftRuleLines(plain); len(got) != 1 || strings.Contains(got[0], "saddr") {
		t.Errorf("nft ohne quellen falsch: %v", got)
	}
}

// TestExpandRuleAllowlists: leere Auflösung → Regel wird ausgelassen (Port
// bleibt zu), nie „von überall".
func TestExpandRuleAllowlists(t *testing.T) {
	rules := []domain.FirewallRule{
		{Port: 80, Proto: "tcp"},                            // ohne Allowlist → bleibt
		{Port: 443, Proto: "tcp", AllowlistIDs: []uint{1}},  // löst zu IPs auf → bleibt mit Sources
		{Port: 8080, Proto: "tcp", AllowlistIDs: []uint{2}}, // löst leer auf → weg
	}
	expand := func(ids []uint) ([]string, error) {
		if len(ids) == 1 && ids[0] == 1 {
			return []string{"10.0.0.5"}, nil
		}
		return nil, nil // leere Liste
	}
	out, _, err := expandRuleAllowlists(rules, expand)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("erwartet 2 regeln (leere allowlist ausgelassen), bekam %d: %+v", len(out), out)
	}
	if out[1].Port != 443 || len(out[1].Sources) != 1 || out[1].Sources[0] != "10.0.0.5" {
		t.Errorf("aufgelöste quellen falsch: %+v", out[1])
	}
	// Hash ändert sich, wenn sich die Quellen ändern.
	h1 := firewallRulesHash(sshRuleFor(22), out)
	out[1].Sources = []string{"10.0.0.6"}
	if firewallRulesHash(sshRuleFor(22), out) == h1 {
		t.Error("geänderte quellen müssen den hash ändern")
	}
}

// TestExpandRuleSourceIPs: händische Quell-IPs wirken allein und als Union
// mit aufgelösten Allowlists; sie halten eine Regel am Leben, deren
// Allowlist leer auflöst (die Einschränkung bleibt bestehen).
func TestExpandRuleSourceIPs(t *testing.T) {
	rules := []domain.FirewallRule{
		{Port: 443, Proto: "tcp", SourceIPs: []string{"203.0.113.7"}},                            // nur manuell
		{Port: 8443, Proto: "tcp", AllowlistIDs: []uint{1}, SourceIPs: []string{"198.51.100.9"}}, // Union
		{Port: 9000, Proto: "tcp", AllowlistIDs: []uint{2}, SourceIPs: []string{"192.0.2.1"}},    // Liste leer, manuell bleibt
		{Port: 9100, Proto: "tcp", AllowlistIDs: []uint{1}, SourceIPs: []string{"10.0.0.5"}},     // Duplikat mit Allowlist
	}
	expand := func(ids []uint) ([]string, error) {
		if len(ids) == 1 && ids[0] == 1 {
			return []string{"10.0.0.5"}, nil
		}
		return nil, nil
	}
	out, _, err := expandRuleAllowlists(rules, expand)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 {
		t.Fatalf("erwartet 4 regeln, bekam %d: %+v", len(out), out)
	}
	if got := out[0].Sources; len(got) != 1 || got[0] != "203.0.113.7" {
		t.Errorf("nur-manuell falsch: %v", got)
	}
	if got := out[1].Sources; len(got) != 2 || got[0] != "10.0.0.5" || got[1] != "198.51.100.9" {
		t.Errorf("union falsch (erwartet sortiert [10.0.0.5 198.51.100.9]): %v", got)
	}
	if got := out[2].Sources; len(got) != 1 || got[0] != "192.0.2.1" {
		t.Errorf("manuelle IP muss leere allowlist überleben: %v", got)
	}
	if got := out[3].Sources; len(got) != 1 || got[0] != "10.0.0.5" {
		t.Errorf("duplikate müssen dedupliziert werden: %v", got)
	}
}

// TestNftSourceFamilyGuard: eine Quelle der falschen Adressfamilie darf an
// einer familien-gebundenen Regel keine Freigabe erzeugen - sonst stünde im
// Regelsatz eine Zeile, die nie greift (und der Port wäre scheinbar geregelt).
func TestNftSourceFamilyGuard(t *testing.T) {
	r := domain.FirewallRule{Port: 8443, Proto: "tcp", IPVersion: "v4", Sources: []string{"10.1.0.0/24"}}
	lines := nftRuleLines(r)
	if len(lines) != 1 || !strings.Contains(lines[0], "ip saddr { 10.1.0.0/24 }") {
		t.Errorf("nft v4-quelle falsch: %v", lines)
	}
	r.Sources = []string{"2001:db8::/32"}
	if lines := nftRuleLines(r); len(lines) != 0 {
		t.Errorf("familien-fremde quelle darf nicht rendern: %v", lines)
	}
}

// TestExpandFiltersSourceFamily: die Auflösung verwirft Allowlist-Einträge
// der falschen Adressfamilie; bleibt nichts übrig, fällt die Regel weg.
func TestExpandFiltersSourceFamily(t *testing.T) {
	rules := []domain.FirewallRule{
		{Port: 443, Proto: "tcp", IPVersion: "v4", AllowlistIDs: []uint{1}},  // gemischte Liste → nur v4
		{Port: 8443, Proto: "tcp", IPVersion: "v6", AllowlistIDs: []uint{2}}, // nur-v4-Liste an v6-Regel → weg
	}
	expand := func(ids []uint) ([]string, error) {
		if ids[0] == 1 {
			return []string{"10.0.0.5", "2001:db8::1"}, nil
		}
		return []string{"10.0.0.5"}, nil
	}
	out, _, err := expandRuleAllowlists(rules, expand)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Port != 443 {
		t.Fatalf("erwartet nur die 443-regel, bekam: %+v", out)
	}
	if got := out[0].Sources; len(got) != 1 || got[0] != "10.0.0.5" {
		t.Errorf("v6-eintrag muss an v4-regel gefiltert werden: %v", got)
	}
}

// TestNormalizeSourceIPs: Kanonisierung (CIDR-Netzadresse), Dedup, Sortierung;
// ungültige Einträge brechen streng ab.
func TestNormalizeSourceIPs(t *testing.T) {
	rules, err := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 443, Proto: "tcp", SourceIPs: []string{" 10.0.0.7 ", "10.0.0.7", "192.168.1.9/24", "2001:DB8::1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.7", "192.168.1.0/24", "2001:db8::1"}
	if got := rules[0].SourceIPs; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("normalisierung falsch: %v (erwartet %v)", got, want)
	}
	if _, err := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 443, Proto: "tcp", SourceIPs: []string{"keine-ip"}},
	}); err == nil {
		t.Error("ungültige quell-ip muss abgelehnt werden")
	}
	// Manuelle Quelle der falschen Familie → strenger Fehler.
	if _, err := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 443, Proto: "tcp", IPVersion: "v4", SourceIPs: []string{"2001:db8::1"}},
	}); err == nil {
		t.Error("v6-quelle an v4-regel muss abgelehnt werden")
	}
	// Runde durch JSON (Persistenz-Format) erhält source_ips.
	spec := firewallRulesJSON(rules)
	again, err := parseFirewallRuleSpec(spec)
	if err != nil || len(again) != 1 || len(again[0].SourceIPs) != 3 {
		t.Errorf("json-runde verliert source_ips: %v %v", again, err)
	}
}

// TestUfwEnableScript: Rich-Rule-Rendering im ufw-Skript. Eigenheit: eine
// reine v4-/v6-Freigabe läuft über die Familien-Wildcard als Zieladresse.
func TestUfwEnableScript(t *testing.T) {
	rules, _ := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 443, Proto: "tcp"},                                       // any
		{Port: 8080, Proto: "tcp", IPVersion: "v4"},                     // nur v4
		{Port: 53, Proto: "udp", IPVersion: "v6"},                       // nur v6
		{Port: 5432, Proto: "tcp", SourceIPs: []string{"192.168.1.10"}}, // Quelle
	})
	// Quellen sind zur Renderzeit bereits aufgelöst (expandSSHAndRules).
	// normalizeFirewallRules sortiert - die Regel deshalb über den Port suchen.
	setSources(rules, 5432, "192.168.1.10")
	script := ufwEnableScript(sshRuleFor(22), rules)
	for _, want := range []string{
		"ufw --force reset",
		"ufw default deny incoming",
		"ufw allow proto tcp to any port 22 comment 'lcm:" + firewallRulesHash(sshRuleFor(22), rules) + "'",
		"ufw allow proto tcp to any port 443",
		"ufw allow proto tcp to 0.0.0.0/0 port 8080",
		"ufw allow proto udp to ::/0 port 53",
		"ufw allow proto tcp from 192.168.1.10 to any port 5432",
		"ufw --force enable",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("ufw-skript enthält %q nicht:\n%s", want, script)
		}
	}
	if !strings.Contains(ufwDisableScript(), "ufw --force disable") {
		t.Error("disable-skript ohne disable-kommando")
	}
	if !strings.Contains(script, firewallToolMissingMarker(domain.FirewallToolUfw)) {
		t.Error("fehlendes-tool-marker fehlt im skript")
	}
}

// TestUfwInSync: aktiv + Hash + exaktes Port/Proto-Set.
func TestUfwInSync(t *testing.T) {
	rules, _ := normalizeFirewallRules([]domain.FirewallRule{{Port: 80, Proto: "tcp"}})
	hash := firewallRulesHash(sshRuleFor(22), rules)
	ok := "Status: active\n" +
		"22/tcp                     ALLOW IN    Anywhere                   # lcm:" + hash + "\n" +
		"80/tcp                     ALLOW IN    Anywhere\n" +
		"22/tcp (v6)                ALLOW IN    Anywhere (v6)\n" +
		"80/tcp (v6)                ALLOW IN    Anywhere (v6)\n"
	if !ufwInSync(ok, sshRuleFor(22), rules) {
		t.Error("konformer status muss in sync sein")
	}
	// Fremder Extra-Port → Drift (auch wenn der Hash noch stimmt).
	if ufwInSync(ok+"9999/tcp                   ALLOW IN    Anywhere\n", sshRuleFor(22), rules) {
		t.Error("extra-port muss drift sein")
	}
	// Fehlender Hash (z.B. Bind von Hand geändert) → Drift.
	if ufwInSync(strings.ReplaceAll(ok, "lcm:"+hash, "lcm:ffffffffffff"), sshRuleFor(22), rules) {
		t.Error("hash-abweichung muss drift sein")
	}
	if ufwInSync(strings.ReplaceAll(ok, "Status: active", "Status: inactive"), sshRuleFor(22), rules) {
		t.Error("inaktive firewall muss drift sein")
	}
	// Regel mit Quell-Einschränkung in der Status-Zeile wird erkannt
	// (Set-Vergleich über die Ports).
	rules2, _ := normalizeFirewallRules([]domain.FirewallRule{{Port: 443, Proto: "tcp", SourceIPs: []string{"10.0.0.5"}}})
	hash2 := firewallRulesHash(sshRuleFor(22), rules2)
	st := "Status: active\n" +
		"22/tcp                     ALLOW IN    Anywhere                   # lcm:" + hash2 + "\n" +
		"443/tcp                    ALLOW IN    10.0.0.5                   # lcm\n" +
		"22/tcp (v6)                ALLOW IN    Anywhere (v6)\n"
	if !ufwInSync(st, sshRuleFor(22), rules2) {
		t.Error("quell-regel-status muss in sync sein")
	}
}

// TestFirewalldEnableScript: deklaratives Zonen-Management + Rich-Rules.
func TestFirewalldEnableScript(t *testing.T) {
	rules, _ := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 443, Proto: "tcp"},                                      // einfach → --add-port
		{Port: 8080, Proto: "udp", IPVersion: "v6"},                    // Familie → Rich-Rule
		{Port: 5432, Proto: "tcp", SourceIPs: []string{"10.0.0.0/24"}}, // Quelle → Rich-Rule
	})
	setSources(rules, 5432, "10.0.0.0/24")
	script := firewalldEnableScript(sshRuleFor(22), rules)
	for _, want := range []string{
		"systemctl enable --now firewalld",
		`--remove-port="$p"`,
		`--remove-rich-rule="$r"`,
		`--add-port=22/tcp`,
		`--add-port=443/tcp`,
		`--add-rich-rule='rule family="ipv6" port port="8080" protocol="udp" accept'`,
		`--add-rich-rule='rule family="ipv4" source address="10.0.0.0/24" port port="5432" protocol="tcp" accept'`,
		"firewall-cmd --reload",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("firewalld-skript enthält %q nicht:\n%s", want, script)
		}
	}
	if !strings.Contains(firewalldDisableScript(), "systemctl disable --now firewalld") {
		t.Error("firewalld-disable ohne stop")
	}
}

// TestFirewalldInSyncRoundTrip: das gerenderte Rich-Rule-Format muss exakt
// dem entsprechen, was der In-Sync-Vergleich aus dem Status liest.
func TestFirewalldInSyncRoundTrip(t *testing.T) {
	rules, _ := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 443, Proto: "tcp"},
		{Port: 5432, Proto: "tcp", SourceIPs: []string{"10.0.0.5"}},
	})
	setSources(rules, 5432, "10.0.0.5")
	status := "running\n22/tcp 443/tcp\n" + firewalldRichRules(rules[1])[0] + "\n"
	if !firewalldInSync(status, sshRuleFor(22), rules) {
		t.Errorf("kanonischer status muss in sync sein:\n%s", status)
	}
	// "not running" darf NICHT als aktiv gelten (Substring-Falle).
	if firewalldActiveFromOutput("not running\n") {
		t.Error("'not running' fälschlich als aktiv erkannt")
	}
	if !firewalldActiveFromOutput("running\n") {
		t.Error("'running' nicht als aktiv erkannt")
	}
	// Fremder Port → Drift; fehlende Rich-Rule → Drift.
	if firewalldInSync("running\n22/tcp 443/tcp 9999/tcp\n"+firewalldRichRules(rules[1])[0]+"\n", sshRuleFor(22), rules) {
		t.Error("extra-port muss drift sein")
	}
	if firewalldInSync("running\n22/tcp 443/tcp\n", sshRuleFor(22), rules) {
		t.Error("fehlende rich-rule muss drift sein")
	}
}

// TestNftablesRuleset: atomares Ersetzungs-Idiom + Regel-Rendering.
func TestNftablesRuleset(t *testing.T) {
	rules, _ := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 443, Proto: "tcp"},
		{Port: 8080, Proto: "tcp", IPVersion: "v4"},
		{Port: 53, Proto: "udp", IPVersion: "v6"},
		{Port: 5432, Proto: "tcp", SourceIPs: []string{"10.0.0.5"}},
		{Port: 8443, Proto: "tcp", SourceIPs: []string{"2001:db8::1"}},
	})
	setSources(rules, 5432, "10.0.0.5")
	setSources(rules, 8443, "2001:db8::1")
	rs := nftRuleset(sshRuleFor(22), rules)
	// Atomare Ersetzung: Tabelle idempotent anlegen, löschen, neu definieren -
	// alles in EINER Datei/Transaktion.
	if !strings.HasPrefix(rs, "table inet lcm\ndelete table inet lcm\ntable inet lcm {") {
		t.Errorf("atomares idiom fehlt:\n%s", rs)
	}
	for _, want := range []string{
		"policy drop;",
		`ct state established,related accept comment "lcm:` + firewallRulesHash(sshRuleFor(22), rules) + `"`,
		`iif "lo" accept`,
		"meta l4proto ipv6-icmp accept",
		"tcp dport 22 accept",
		"tcp dport 443 accept",
		"meta nfproto ipv4 tcp dport 8080 accept",
		"meta nfproto ipv6 udp dport 53 accept",
		"ip saddr { 10.0.0.5 } tcp dport 5432 accept",
		"ip6 saddr { 2001:db8::1 } tcp dport 8443 accept",
	} {
		if !strings.Contains(rs, want) {
			t.Errorf("nft-regelsatz enthält %q nicht:\n%s", want, rs)
		}
	}
	// Nie das gesamte Regelwerk fluten (würde Docker & Co. killen).
	if strings.Contains(rs, "flush ruleset") {
		t.Error("flush ruleset ist verboten")
	}

	script := nftablesEnableScript(sshRuleFor(22), rules)
	for _, want := range []string{
		"nft -c -f /etc/nftables.d/lcm.nft.tmp", // Syntaxprüfung VOR dem Anwenden
		"nft -f /etc/nftables.d/lcm.nft",
		`include "/etc/nftables.d/`,
		"systemctl enable nftables >/dev/null 2>&1 || rc-update add nftables default", // OpenRC-Fallback (Alpine)
		"nft list table inet lcm",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("nft-enable-skript enthält %q nicht:\n%s", want, script)
		}
	}
	dis := nftablesDisableScript()
	if !strings.Contains(dis, "nft delete table inet lcm") || !strings.Contains(dis, "rm -f /etc/nftables.d/lcm.nft") {
		t.Errorf("nft-disable unvollständig:\n%s", dis)
	}
}

// TestNftablesInSync: Hash-Kommentar in der Tabellen-Ausgabe.
func TestNftablesInSync(t *testing.T) {
	rules, _ := normalizeFirewallRules([]domain.FirewallRule{{Port: 80, Proto: "tcp"}})
	hash := firewallRulesHash(sshRuleFor(22), rules)
	out := "table inet lcm {\n\tchain input {\n\t\tct state established,related accept comment \"lcm:" + hash + "\"\n\t}\n}\n"
	if !nftablesInSync(out, sshRuleFor(22), rules) {
		t.Error("konforme tabelle muss in sync sein")
	}
	if nftablesInSync("LCM: keine lcm-tabelle\n", sshRuleFor(22), rules) {
		t.Error("fehlende tabelle muss drift sein")
	}
	if nftablesInSync(strings.ReplaceAll(out, hash, "ffffffffffff"), sshRuleFor(22), rules) {
		t.Error("hash-abweichung muss drift sein")
	}
}

// TestParseFirewallDetect + resolveFirewallBackend: Konflikt-/Install-Logik.
func TestResolveFirewallBackend(t *testing.T) {
	for out, want := range map[string]string{
		"ufw\n": "ufw", "firewalld\n": "firewalld", "nftables\n": "nftables",
		"none\n": "", "": "", "quatsch\n": "", "ufw\nweitere zeile\n": "ufw",
	} {
		if got := parseFirewallDetect(out); got != want {
			t.Errorf("detect %q → %q, erwartet %q", out, got, want)
		}
	}
	// Erkanntes Werkzeug → verwenden, NICHTS installieren (auch wenn es nicht
	// das vorgesehene ist).
	ubuntu := &domain.Server{OSID: "ubuntu", PackageManager: "apt"}
	backend, install := resolveFirewallBackend(ubuntu, "firewalld")
	if backend != "firewalld" || install != "" {
		t.Errorf("konflikt: backend=%q install=%q", backend, install)
	}
	// Nichts erkannt → vorgesehenes Werkzeug installieren (dnf auf Rocky).
	rocky := &domain.Server{OSID: "rocky", PackageManager: "dnf"}
	backend, install = resolveFirewallBackend(rocky, "")
	if backend != "firewalld" || !strings.Contains(install, "dnf install -y") || !strings.Contains(install, "firewalld") {
		t.Errorf("rocky-install: backend=%q install=%q", backend, install)
	}
	if !strings.Contains(install, "command -v firewall-cmd") || !strings.Contains(install, firewallInstallFailedMarker) {
		t.Errorf("install ohne nachweis/marker: %q", install)
	}
	// Alpine → apk + nftables.
	alpine := &domain.Server{OSID: "alpine", PackageManager: "apk"}
	backend, install = resolveFirewallBackend(alpine, "")
	if backend != "nftables" || !strings.Contains(install, "apk add") || !strings.Contains(install, "nftables") {
		t.Errorf("alpine-install: backend=%q install=%q", backend, install)
	}
}

// TestParseListeningPorts: ss-tulnpH-Parser (v4/v6/wildcard/mapped/scope/
// loopback/ohne Prozess).
func TestParseListeningPorts(t *testing.T) {
	out := `tcp   LISTEN 0      4096         0.0.0.0:22        0.0.0.0:*    users:(("sshd",pid=713,fd=3))
tcp   LISTEN 0      511             [::]:80           [::]:*    users:(("nginx",pid=812,fd=8),("nginx",pid=813,fd=8))
udp   UNCONN 0      0            0.0.0.0:68        0.0.0.0:*    users:(("dhclient",pid=600,fd=6))
tcp   LISTEN 0      4096       127.0.0.1:8125      0.0.0.0:*    users:(("statsd",pid=900,fd=1))
tcp   LISTEN 0      4096           [::1]:6379         [::]:*
tcp   LISTEN 0      1024 [::ffff:0.0.0.0]:8080            *:*
udp   UNCONN 0      0    [fe80::1%eth0]:546           [::]:*
tcp   LISTEN 0      4096         0.0.0.0:22        0.0.0.0:*    users:(("sshd",pid=713,fd=4))
kaputt zeile
`
	ports := parseListeningPorts(out)
	// Loopback (8125, 6379) raus, Duplikat (22) dedupliziert → 5 Einträge.
	if len(ports) != 5 {
		t.Fatalf("erwartet 5 einträge, bekam %d: %+v", len(ports), ports)
	}
	// Sortiert nach Port: 22, 68, 80, 546, 8080.
	if ports[0].Port != 22 || ports[0].Proto != "tcp" || ports[0].Bind != "0.0.0.0" || ports[0].IPVersion != "v4" || ports[0].Process != "sshd" {
		t.Errorf("ssh-eintrag falsch: %+v", ports[0])
	}
	if ports[1].Port != 68 || ports[1].Proto != "udp" || ports[1].Process != "dhclient" {
		t.Errorf("dhclient-eintrag falsch: %+v", ports[1])
	}
	if ports[2].Port != 80 || ports[2].Bind != "::" || ports[2].IPVersion != "v6" || ports[2].Process != "nginx" {
		t.Errorf("nginx-eintrag falsch: %+v", ports[2])
	}
	if ports[3].Port != 546 || ports[3].Bind != "fe80::1" || ports[3].IPVersion != "v6" {
		t.Errorf("scope-eintrag falsch: %+v", ports[3])
	}
	// v4-gemappte Wildcard zählt als IPv4, Prozess unbekannt (ohne sudo).
	if ports[4].Port != 8080 || ports[4].IPVersion != "v4" || ports[4].Bind != "0.0.0.0" || ports[4].Process != "" {
		t.Errorf("mapped-eintrag falsch: %+v", ports[4])
	}
	if listeningPortsJSON(nil) != "[]" {
		t.Error("leeres inventar muss [] sein")
	}
}

// TestFirewalldZonenDiensteAlsDrift (R2-057): Ein verbliebener Zonen-Dienst
// (z. B. cockpit) öffnet Ports, die in keiner Port-Anzeige auftauchen. Er
// muss als Drift gelten und das Enable-Skript muss ihn entfernen.
func TestFirewalldZonenDiensteAlsDrift(t *testing.T) {
	rules := []domain.FirewallRule{}
	// Ist-Zustand: aktiv, SSH offen - aber cockpit hängt noch als Dienst.
	withService := "running\n22/tcp\n\nLCM-SERVICES: cockpit dhcpv6-client"
	if firewalldInSync(withService, sshRuleFor(22), rules) {
		t.Error("ein verbliebener Zonen-Dienst muss als Drift gelten (R2-057)")
	}
	// Ohne Dienste: in sync.
	clean := "running\n22/tcp\n\nLCM-SERVICES: "
	if !firewalldInSync(clean, sshRuleFor(22), rules) {
		t.Error("ohne Zonen-Dienste und mit korrektem SSH-Port sollte in-sync gelten")
	}
	// Das Enable-Skript entfernt die Dienste explizit.
	script := firewalldEnableScript(sshRuleFor(22), rules)
	if !strings.Contains(script, "--list-services") || !strings.Contains(script, "--remove-service=") {
		t.Errorf("Enable-Skript entfernt keine Zonen-Dienste:\n%s", script)
	}
}

// sshRuleFor baut die SSH-Freigabe für die Tests (ohne Quell-Einschränkung) -
// die Backends bekommen die fertige Regel, nicht mehr Port und Bind einzeln.
// setSources füllt die aufgelösten Quellen einer Regel (im Betrieb macht das
// expandSSHAndRules). Gesucht wird über den Port, weil normalizeFirewallRules
// die Reihenfolge kanonisiert.
func setSources(rules []domain.FirewallRule, port int, sources ...string) {
	for i := range rules {
		if rules[i].Port == port {
			rules[i].Sources = sources
			return
		}
	}
}

func sshRuleFor(port int, sources ...string) domain.FirewallRule {
	return sshFirewallRule(port, domain.FirewallSSHSources{SourceIPs: sources})
}

// Die Bemerkung des Betreibers gehört dorthin, wo sie ohne LCM lesbar ist:
// in den Regelsatz auf dem Zielsystem. ufw und nftables können das.
func TestFirewallRuleCommentRendered(t *testing.T) {
	rule := domain.FirewallRule{Port: 9100, Proto: "tcp", IPVersion: "any", Comment: "Prometheus"}
	ufw := strings.Join(ufwRuleCmds(rule), "\n")
	if !strings.Contains(ufw, "comment 'lcm: Prometheus'") {
		t.Errorf("ufw trägt die Bemerkung nicht: %s", ufw)
	}
	nft := strings.Join(nftRuleLines(rule), "\n")
	if !strings.Contains(nft, `comment "Prometheus"`) {
		t.Errorf("nftables trägt die Bemerkung nicht: %s", nft)
	}
	// Mit Quell-Einschränkung ebenso (eigener Renderpfad je Adressfamilie).
	withSrc := rule
	withSrc.Sources = []string{"192.168.6.129"}
	nftSrc := strings.Join(nftRuleLines(withSrc), "\n")
	if !strings.Contains(nftSrc, `comment "Prometheus"`) {
		t.Errorf("nftables mit Quellen ohne Bemerkung: %s", nftSrc)
	}
	// Anführungszeichen im Text würden die nft-Syntax zerlegen.
	tricky := domain.FirewallRule{Port: 80, Proto: "tcp", Comment: `Web "A" \ B`}
	if out := strings.Join(nftRuleLines(tricky), ""); strings.Count(out, `"`) != 2 {
		t.Errorf("Anführungszeichen nicht entschärft: %s", out)
	}
}
