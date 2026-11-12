package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// ufwStub ist eine ufw-Attrappe: Sie schreibt jeden Aufruf mit und lehnt wie
// ufw 0.36 jede IPv6-Adresse ab, wenn /etc/default/ufw IPV6=no setzt.
const ufwStub = `#!/bin/sh
echo "$*" >> "$UFW_LOG"
v6=no; prev=""
for a in "$@"; do
  case "$prev" in from|to) case "$a" in *:*) v6=yes ;; esac ;; esac
  prev="$a"
done
if [ "$v6" = yes ] && ! grep -q '^IPV6=yes' "$UFW_DEFAULT"; then
  echo 'ERROR: IPv6 support not enabled'; exit 1
fi
exit 0
`

// runUfwScript führt das Skript mit der Attrappe gegen ein /etc/default/ufw
// mit dem gegebenen IPV6-Wert aus. Rückgabe: Ausgabe, Exit-Code, Aufrufe.
func runUfwScript(t *testing.T, script, ipv6 string) (string, int, string) {
	t.Helper()
	dir := t.TempDir()
	defaults := filepath.Join(dir, "ufw.default")
	writeSandboxFile(t, defaults, "# /etc/default/ufw\nIPV6="+ipv6+"\nDEFAULT_INPUT_POLICY=\"DROP\"\n", 0o644)
	writeSandboxFile(t, filepath.Join(dir, "bin", "ufw"), ufwStub, 0o755)
	cmd := exec.Command("sh", "-c", strings.ReplaceAll(script, "/etc/default/ufw", defaults))
	cmd.Env = []string{"PATH=" + filepath.Join(dir, "bin") + ":/usr/bin:/bin", "UFW_LOG=" + dir + "/log", "UFW_DEFAULT=" + defaults}
	out, err := cmd.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(dir + "/log")
	return string(out), code, string(log)
}

// ufwFixtureRules ist der Regelsatz der Fixtures: SSH für alle, 80 für
// beide Familien, 5432 nur für eine IPv4- und eine IPv6-Quelle, 8080 nur
// IPv4, 53/udp nur IPv6.
func ufwFixtureRules(t *testing.T) (domain.FirewallRule, []domain.FirewallRule) {
	t.Helper()
	rules, err := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 80, Proto: "tcp"},
		{Port: 5432, Proto: "tcp", SourceIPs: []string{"10.0.0.5", "2001:db8::/32"}},
		{Port: 8080, Proto: "tcp", IPVersion: "v4"},
		{Port: 53, Proto: "udp", IPVersion: "v6"},
	})
	if err != nil {
		t.Fatal(err)
	}
	setSources(rules, 5432, "10.0.0.5", "2001:db8::/32")
	return sshRuleFor(22), rules
}

// ufwVerboseFixture ist die Ausgabe von `ufw status verbose` nach dem
// Anwenden des Fixture-Regelsatzes: ufw zeigt erst alle IPv4-, dann alle
// IPv6-Regeln, das Ziel 0.0.0.0/0 bzw. ::/0 nur als Port, die IPv6-Hälfte
// mit „(v6)" - aber nur ohne Quelle. Eine Freigabe für eine IPv6-Quelle steht
// ohne den Zusatz da (an ufw 0.36 unter Ubuntu 24.04 nachgesehen).
func ufwVerboseFixture(hash string) string {
	return `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere                   # lcm:` + hash + `
80/tcp                     ALLOW IN    Anywhere                   # lcm
5432/tcp                   ALLOW IN    10.0.0.5                   # lcm
8080/tcp                   ALLOW IN    Anywhere                   # lcm
22/tcp (v6)                ALLOW IN    Anywhere (v6)              # lcm:` + hash + `
80/tcp (v6)                ALLOW IN    Anywhere (v6)              # lcm
5432/tcp                   ALLOW IN    2001:db8::/32              # lcm
53/udp (v6)                ALLOW IN    Anywhere (v6)              # lcm

`
}

// TestUfwInSyncLiestAlleStatusFormate: `ufw status`, `ufw status verbose`
// und `ufw status numbered` schreiben dieselben Regeln verschieden (ALLOW
// gegenüber ALLOW IN, Nummern vorne). Alle drei müssen als in sync gelten.
func TestUfwInSyncLiestAlleStatusFormate(t *testing.T) {
	ssh, rules := ufwFixtureRules(t)
	hash := firewallRulesHash(ssh, rules)
	verbose := ufwVerboseFixture(hash)
	plain := `Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere                   # lcm:` + hash + `
80/tcp                     ALLOW       Anywhere                   # lcm
5432/tcp                   ALLOW       10.0.0.5                   # lcm
8080/tcp                   ALLOW       Anywhere                   # lcm
22/tcp (v6)                ALLOW       Anywhere (v6)              # lcm:` + hash + `
80/tcp (v6)                ALLOW       Anywhere (v6)              # lcm
5432/tcp                   ALLOW       2001:db8::/32              # lcm
53/udp (v6)                ALLOW       Anywhere (v6)              # lcm
`
	numbered := `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere                   # lcm:` + hash + `
[ 2] 80/tcp                     ALLOW IN    Anywhere                   # lcm
[ 3] 5432/tcp                   ALLOW IN    10.0.0.5                   # lcm
[ 4] 8080/tcp                   ALLOW IN    Anywhere                   # lcm
[ 5] 22/tcp (v6)                ALLOW IN    Anywhere (v6)              # lcm:` + hash + `
[ 6] 80/tcp (v6)                ALLOW IN    Anywhere (v6)              # lcm
[ 7] 5432/tcp                   ALLOW IN    2001:db8::/32              # lcm
[ 8] 53/udp (v6)                ALLOW IN    Anywhere (v6)              # lcm
`
	for name, status := range map[string]string{"verbose": verbose, "status": plain, "numbered": numbered} {
		if !ufwInSync(status, ssh, rules) {
			t.Errorf("%s: konformer status muss in sync sein:\n%v", name, ufwOpenRules(status))
		}
	}
}

// TestUfwInSyncErkenntQuellenUndRegelnOhnePort hält den Review-Fund fest:
// Verglichen wurde nur Port/Protokoll. Eine von Hand geänderte Quell-Adresse
// oder eine zusätzliche Freigabe ohne Port (`ufw allow from 1.2.3.4`, öffnet
// für diese Quelle ALLES) galt als in sync und wurde nie zurückgenommen.
func TestUfwInSyncErkenntQuellenUndRegelnOhnePort(t *testing.T) {
	ssh, rules := ufwFixtureRules(t)
	ok := ufwVerboseFixture(firewallRulesHash(ssh, rules))
	for name, status := range map[string]string{
		"quelle geändert":     strings.Replace(ok, "5432/tcp                   ALLOW IN    10.0.0.5", "5432/tcp                   ALLOW IN    10.0.0.6", 1),
		"quelle aufgehoben":   strings.Replace(ok, "5432/tcp                   ALLOW IN    10.0.0.5", "5432/tcp                   ALLOW IN    Anywhere", 1),
		"v6-quelle geändert":  strings.Replace(ok, "2001:db8::/32", "2001:db8::/16", 1),
		"ohne port v4":        ok + "Anywhere                   ALLOW IN    1.2.3.4\n",
		"ohne port v6":        ok + "Anywhere (v6)              ALLOW IN    2001:db8::1\n",
		"ohne port mit proto": ok + "Anywhere/tcp               ALLOW IN    1.2.3.4/tcp\n",
		"extra port":          ok + "9999/tcp                   ALLOW IN    Anywhere\n",
		"limit statt allow":   strings.Replace(ok, "22/tcp                     ALLOW IN", "22/tcp                     LIMIT IN", 1),
		"weiterleitung":       ok + "Anywhere on eth1           ALLOW FWD   Anywhere on eth0\n",
		// Ein langes IPv6-Ziel füllt seine Spalte, dahinter steht nur ein
		// Leerzeichen - die Zeile muss trotzdem erkannt werden.
		"langes ziel":     ok + "2001:db8:1234:5678:9abc:def0:1234:5678 443/tcp ALLOW IN    Anywhere (v6)\n",
		"v4-hälfte fehlt": strings.Replace(ok, "80/tcp                     ALLOW IN    Anywhere                   # lcm\n", "", 1),
	} {
		if ufwInSync(status, ssh, rules) {
			t.Errorf("%s: muss als abweichung gelten", name)
		}
	}
}

// TestUfwInSyncIgnoriertSperrendeRegeln: DENY, REJECT und ausgehende
// Freigaben öffnen nichts nach innen - sie sind kein Grund, die Firewall neu
// aufzusetzen.
func TestUfwInSyncIgnoriertSperrendeRegeln(t *testing.T) {
	ssh, rules := ufwFixtureRules(t)
	status := ufwVerboseFixture(firewallRulesHash(ssh, rules)) +
		"Anywhere                   DENY IN     203.0.113.9\n" +
		"3306/tcp                   REJECT IN   Anywhere\n" +
		"Anywhere                   ALLOW OUT   Anywhere on eth0\n"
	if !ufwInSync(status, ssh, rules) {
		t.Errorf("sperrende regeln dürfen keine abweichung sein: %v", ufwOpenRules(status))
	}
}

// TestUfwInSyncOhneIPv6: Mit IPV6=no in /etc/default/ufw legt ufw keine
// IPv6-Hälfte an. Verlangte der Vergleich sie trotzdem, würde die Firewall
// bei jedem Lauf zurückgesetzt.
func TestUfwInSyncOhneIPv6(t *testing.T) {
	rules, _ := normalizeFirewallRules([]domain.FirewallRule{
		{Port: 80, Proto: "tcp"},
		{Port: 5432, Proto: "tcp", SourceIPs: []string{"10.0.0.5"}},
	})
	setSources(rules, 5432, "10.0.0.5")
	ssh := sshRuleFor(22)
	status := "Status: active\n\n" +
		"To                         Action      From\n" +
		"--                         ------      ----\n" +
		"22/tcp                     ALLOW IN    Anywhere                   # lcm:" + firewallRulesHash(ssh, rules) + "\n" +
		"80/tcp                     ALLOW IN    Anywhere                   # lcm\n" +
		"5432/tcp                   ALLOW IN    10.0.0.5                   # lcm\n"
	if !ufwInSync(status, ssh, rules) {
		t.Error("ohne ipv6 darf die fehlende v6-hälfte keine abweichung sein")
	}
	// Die Quelle zählt auch dort.
	if ufwInSync(strings.Replace(status, "10.0.0.5", "10.0.0.6", 1), ssh, rules) {
		t.Error("geänderte quelle muss auch ohne ipv6 abweichung sein")
	}
}

// TestUfwInSyncMitSSHQuellen: Die SSH-Freigabe mit Quell-Einschränkung
// erscheint je Quelle, ohne „Anywhere"-Zeile.
func TestUfwInSyncMitSSHQuellen(t *testing.T) {
	ssh := sshRuleFor(22)
	ssh.Sources = []string{"192.0.2.0/24", "2001:db8:1::/48"}
	status := "Status: active\n" +
		"22/tcp                     ALLOW IN    192.0.2.0/24               # lcm:" + firewallRulesHash(ssh, nil) + "\n" +
		"22/tcp                     ALLOW IN    2001:db8:1::/48            # lcm:" + firewallRulesHash(ssh, nil) + "\n"
	if !ufwInSync(status, ssh, nil) {
		t.Errorf("ssh mit quellen muss in sync sein: %v", ufwOpenRules(status))
	}
	if ufwInSync(status+"22/tcp                     ALLOW IN    Anywhere\n", ssh, nil) {
		t.Error("ssh für alle geöffnet muss abweichung sein")
	}
}

// TestUfwFieldKeySchreibweisen: ufw zeigt Adressen in seiner Schreibweise -
// /32 und /128 fallen weg, 0.0.0.0/0 heißt „Anywhere". Ohne Angleichung
// wäre eine so eingetragene Quelle bei jedem Lauf eine Abweichung, und die
// Firewall würde alle 15 Minuten zurückgesetzt.
func TestUfwFieldKeySchreibweisen(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.5/32":            "10.0.0.5",
		"2001:DB8:0:0::1/128":    "2001:db8::1",
		"0.0.0.0/0":              "Anywhere",
		"::/0":                   "Anywhere (v6)",
		"22/tcp   (v6)":          "22/tcp (v6)",
		"2001:0db8::/32 443/tcp": "2001:db8::/32 443/tcp",
	} {
		if got := ufwFieldKey(in); got != want {
			t.Errorf("%q: %q erwartet, bekam %q", in, want, got)
		}
	}
}

// TestUfwOhneIPv6LaesstV6FreigabenAus: Mit IPV6=no lehnt ufw eine reine
// IPv6-Freigabe ab, und die &&-Kette brach mitten im Aufbau ab - nach dem
// Reset, ohne die übrigen Regeln. Jetzt laufen die IPv4-Teile, die IPv6-Teile
// stehen als Hinweis in der Ausgabe.
func TestUfwOhneIPv6LaesstV6FreigabenAus(t *testing.T) {
	ssh, rules := ufwFixtureRules(t)
	script := ufwEnableScript(ssh, rules)

	out, code, log := runUfwScript(t, script, "no")
	if code != 0 {
		t.Fatalf("aufbau ohne ipv6 bricht ab (exit %d):\n%s", code, out)
	}
	for _, want := range []string{"port 22 ", "port 80 ", "from 10.0.0.5 to any port 5432", "to 0.0.0.0/0 port 8080", "--force enable"} {
		if !strings.Contains(log, want) {
			t.Errorf("%q wurde nicht angewendet:\n%s", want, log)
		}
	}
	if strings.Contains(log, "2001:db8::/32") || strings.Contains(log, "::/0") {
		t.Errorf("ipv6-freigabe trotz IPV6=no versucht:\n%s", log)
	}
	for _, want := range []string{"ausgelassen: 53/udp (nur IPv6)", "ausgelassen: 5432/tcp von 2001:db8::/32"} {
		if !strings.Contains(out, want) {
			t.Errorf("hinweis %q fehlt:\n%s", want, out)
		}
	}

	// Mit IPv6 läuft alles wie bisher.
	if out, code, log := runUfwScript(t, script, "yes"); code != 0 || !strings.Contains(log, "from 2001:db8::/32") || !strings.Contains(log, "to ::/0 port 53") {
		t.Errorf("mit ipv6 fehlt etwas (exit %d):\n%s\n%s", code, out, log)
	}
}

// TestUfwSSHNurUeberIPv6OhneIPv6BrichtVorDemResetAb: Bliebe ohne IPv6 keine
// einzige SSH-Freigabe übrig, sperrte die eingeschaltete Firewall SSH aus.
// Dann wird gar nicht erst zurückgesetzt.
func TestUfwSSHNurUeberIPv6OhneIPv6BrichtVorDemResetAb(t *testing.T) {
	ssh := sshRuleFor(22)
	ssh.Sources = []string{"2001:db8:1::/48"}
	out, code, log := runUfwScript(t, ufwEnableScript(ssh, nil), "no")
	if code != 1 || strings.Contains(log, "reset") || !strings.Contains(out, "sperrte sonst SSH aus") {
		t.Errorf("kein abbruch vor dem reset (exit %d):\n%s\n%s", code, out, log)
	}
	// Mit einer IPv4-Quelle daneben bleibt SSH erreichbar - kein Abbruch.
	ssh.Sources = append(ssh.Sources, "192.0.2.0/24")
	if out, code, _ := runUfwScript(t, ufwEnableScript(ssh, nil), "no"); code != 0 {
		t.Errorf("abbruch trotz ipv4-quelle (exit %d):\n%s", code, out)
	}
}

// TestUfwInSyncOhneIPv6MitV6Quelle: Ohne IPv6 legt das Skript die Freigabe
// für eine IPv6-Quelle nicht an. Verlangte der Vergleich sie trotzdem, würde
// die Firewall bei jedem Lauf zurückgesetzt.
func TestUfwInSyncOhneIPv6MitV6Quelle(t *testing.T) {
	ssh, rules := ufwFixtureRules(t)
	var kept []string
	for _, line := range strings.Split(ufwVerboseFixture(firewallRulesHash(ssh, rules)), "\n") {
		if !strings.Contains(line, "(v6)") && !strings.Contains(line, "2001:db8") {
			kept = append(kept, line)
		}
	}
	if status := strings.Join(kept, "\n"); !ufwInSync(status, ssh, rules) {
		t.Errorf("ohne ipv6 muss der status in sync sein:\n%s", status)
	}
}
