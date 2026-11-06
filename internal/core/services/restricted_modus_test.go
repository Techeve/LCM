package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// Tests zum eingeschränkten Modus: Aktionen, die root brauchen, laufen über den
// LCM-Helper - mit demselben Rumpf wie im Voll-Modus, und der Helper lehnt
// schlechte Parameter ab.

// TestRumpfIstImVollModusUndImHelperDerselbe: Die Root-Schritte gibt es einmal.
// Im Voll-Modus steht der Rumpf im Skript, im Helper unter einem Unterkommando
// - stünden dort zwei Fassungen, liefe der eingeschränkte Modus irgendwann
// anders als der Voll-Modus.
func TestRumpfIstImVollModusUndImHelperDerselbe(t *testing.T) {
	ssh := sshFirewallRule(22, domain.FirewallSSHSources{})
	rules := []domain.FirewallRule{{Port: 80, Proto: "tcp", IPVersion: "any"}}
	apt := defaultAptChannelPaths()
	pins := []domain.PackagePin{{Name: "bash", NoRemove: true, Hold: true}}
	cs, err := crowdsecInstallScript(false, pkgApt, "10.0.0.5", nil,
		SecurityToolInput{Tool: "crowdsec", LapiMode: "local", Collections: []string{"crowdsecurity/sshd"}}, CrowdSecConfig{})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, full, body string }{
		{"Kanal enterprise", enterpriseChannelScript("https://r.example/e", "id", "geheim"), enterpriseChannelBody(apt)},
		{"Kanal beta", betaChannelScript("https://r.example"), betaChannelBody(apt)},
		{"Kanal community", communityRevertScript(), communityRevertBody()},
		{"Pins apt", pkgPinScript(false, pkgApt, pins), pinAptBody},
		{"Pins dnf", pkgPinScript(false, pkgDnf, pins), pinDnfBody},
		{"Pins pacman", pkgPinScript(false, pkgPacman, pins), pinPacmanBody},
		{"nftables anwenden", nftablesEnableScript(ssh, rules), nftablesApplyBody()},
		{"nftables abschalten", nftablesDisableScript(), nftablesDisableScript()},
		{"nftables Status", nftablesStatusCmd, nftablesStatusCmd},
		{"nftables Port", nftablesAllowPortCmd(2222), nftablesAllowPortBody},
		{"firewalld anwenden", firewalldEnableScript(ssh, rules), firewalldApplyBody()},
		{"firewalld abschalten", firewalldDisableScript(), firewalldDisableScript()},
		{"firewalld Status", firewalldStatusCmd, firewalldStatusCmd},
		{"firewalld Port", firewalldRemovePortCmd(2222), firewalldRemovePortBody},
		{"fail2ban einrichten", fail2banInstallScript(false, pkgApt, "10.0.0.5", nil), fail2banConfigBody},
		{"fail2ban Allowlist", allowlistUpdateScript(false, SecurityToolFail2ban, "10.0.0.5", nil), fail2banConfigBody},
		{"CrowdSec Repo", cs, crowdsecRepoBody},
		{"CrowdSec Collections", cs, crowdsecCollectionsBody},
		{"CrowdSec einrichten", cs, crowdsecConfigureBody},
		{"CrowdSec Allowlist", allowlistUpdateScript(false, SecurityToolCrowdSec, "10.0.0.5", nil), crowdsecConfigureBody},
		{"Dienst start", securityServiceStep(false, SecurityToolActionStart, SecurityToolCrowdSec), securityServiceBody(SecurityToolActionStart)},
		{"Dienst restart", securityServiceStep(false, SecurityToolActionRestart, SecurityToolFail2ban), securityServiceBody(SecurityToolActionRestart)},
		{"fail2ban entfernen", uninstallScript(false, pkgApt, SecurityToolFail2ban), purgeFail2banBody},
		{"CrowdSec entfernen", uninstallScript(false, pkgApt, SecurityToolCrowdSec), purgeCrowdSecBody},
		{"fail2ban entsperren", unbanScript(false, SecurityToolFail2ban, "203.0.113.7"), unbanFail2banBody},
		{"CrowdSec entsperren", unbanScript(false, SecurityToolCrowdSec, "203.0.113.7"), unbanCrowdSecBody},
		{"fail2ban Sperrliste", banListCommand(false, SecurityToolFail2ban), banListFail2banBody},
		{"CrowdSec Sperrliste", banListCommand(false, SecurityToolCrowdSec), banListCrowdSecBody},
		{"CrowdSec LAPI-URL", crowdsecLapiURLCommand(false), crowdsecLapiURLBody},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(c.full, c.body) {
				t.Errorf("Voll-Modus-Skript enthält den Rumpf nicht:\n%s", c.full)
			}
			if !strings.Contains(lcmHelperScript, c.body) {
				t.Errorf("der Helper enthält den Rumpf nicht")
			}
		})
	}
}

// TestEingeschraenkterModusRuftDenHelper: Im eingeschränkten Modus steht an
// der Stelle des Root-Schritts ein Helper-Aufruf - und nichts von dem, was
// root braucht (Dateien unter /etc, systemctl, fail2ban-client, cscli, nft).
func TestEingeschraenkterModusRuftDenHelper(t *testing.T) {
	ssh := sshFirewallRule(22, domain.FirewallSSHSources{})
	rules := []domain.FirewallRule{{Port: 80, Proto: "tcp", IPVersion: "any", Comment: "web"}}
	srv := &domain.Server{ServiceUser: "svc", RestrictedSudo: true, PackageManager: pkgApt}
	pins := []domain.PackagePin{{Name: "linux-image-*", NoRemove: true}, {Name: "bash", Hold: true}}
	cs, err := crowdsecInstallScript(true, pkgApt, "10.0.0.5", nil,
		SecurityToolInput{Tool: "crowdsec", LapiMode: "remote", Collections: []string{"crowdsecurity/sshd"}},
		CrowdSecConfig{LapiURL: "http://lapi.example:8080", LapiLogin: "srv1", LapiPassword: "geheim"})
	if err != nil {
		t.Fatal(err)
	}
	nft := firewallBackendOf(srv, domain.FirewallToolNftables)
	fwd := firewallBackendOf(srv, domain.FirewallToolFirewalld)

	cases := []struct {
		name   string
		script string
		helper []string // erwartete Helper-Aufrufe
	}{
		{"fail2ban einrichten", fail2banInstallScript(true, pkgApt, "10.0.0.5", nil), []string{"lcm-helper fail2ban-config '127.0.0.1/8' '::1' '10.0.0.5'"}},
		{"CrowdSec einrichten", cs, []string{"lcm-helper crowdsec-repo 'deb'", "lcm-helper crowdsec-collections 'crowdsecurity/sshd'", "lcm-helper crowdsec-configure 'remote' 'http://lapi.example:8080' 'srv1'"}},
		{"Dienst", securityServiceStep(true, SecurityToolActionStop, SecurityToolCrowdSec), []string{"lcm-helper security-service 'crowdsec' 'stop'"}},
		{"Entfernen", uninstallScript(true, pkgApt, SecurityToolFail2ban), []string{"lcm-helper security-service 'fail2ban' 'stop'", "lcm-helper security-purge 'fail2ban'"}},
		{"Allowlist", allowlistUpdateScript(true, SecurityToolFail2ban, "10.0.0.5", nil), []string{"lcm-helper fail2ban-config"}},
		{"Entsperren", unbanScript(true, SecurityToolCrowdSec, "203.0.113.7"), []string{"lcm-helper security-unban 'crowdsec' '203.0.113.7'"}},
		{"Sperrliste", banListCommand(true, SecurityToolFail2ban), []string{"lcm-helper security-bans 'fail2ban'"}},
		{"LAPI-URL", crowdsecLapiURLCommand(true), []string{"lcm-helper crowdsec-lapi-url"}},
		{"Pins apt", pkgPinScript(true, pkgApt, pins), []string{"lcm-helper package-pins 'apt' 'linux-image-*' 'bash'"}},
		{"Pins dnf", pkgPinScript(true, pkgDnf, pins), []string{"lcm-helper package-pins 'dnf'"}},
		{"Pins pacman", pkgPinScript(true, pkgPacman, pins), []string{"lcm-helper package-pins 'pacman'"}},
		{"nftables anwenden", nft.enableScript(ssh, rules), []string{"lcm-helper firewall-apply 'nftables'"}},
		{"nftables abschalten", nft.disableScript(), []string{"lcm-helper firewall-disable 'nftables'"}},
		{"nftables Status", nft.statusCmd, []string{"lcm-helper firewall-status 'nftables'"}},
		{"nftables Port", nft.allowPortCmd(2222), []string{"lcm-helper firewall-port 'allow' 'nftables' '2222'"}},
		{"firewalld anwenden", fwd.enableScript(ssh, rules), []string{"lcm-helper firewall-apply 'firewalld' '--add-port=22/tcp' '--add-port=80/tcp'"}},
		{"firewalld Port", fwd.removePortCmd(2222), []string{"lcm-helper firewall-port 'remove' 'firewalld' '2222'"}},
	}
	rootOnly := []string{"/etc/", "systemctl", "fail2ban-client reload", "cscli collections", "cscli lapi", "cscli console", "nft -f", "nft -c", "firewall-cmd", "apt-mark", "base64 -d"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, want := range c.helper {
				if !strings.Contains(c.script, want) {
					t.Errorf("%q fehlt:\n%s", want, c.script)
				}
			}
			for _, bad := range rootOnly {
				if strings.Contains(c.script, bad) {
					t.Errorf("%q steht im Restricted-Skript - das braucht root und gehört in den Helper:\n%s", bad, c.script)
				}
			}
		})
	}

	// ufw steht auf der Whitelist und bleibt ohne Helper.
	ufw := firewallBackendOf(srv, domain.FirewallToolUfw)
	if strings.Contains(ufw.enableScript(ssh, rules), "lcm-helper") {
		t.Error("ufw braucht den Helper nicht")
	}
	// Voll-Modus: nie ein Helper-Aufruf.
	full := &domain.Server{ServiceUser: "svc", PackageManager: pkgApt}
	if strings.Contains(firewallBackendOf(full, domain.FirewallToolNftables).enableScript(ssh, rules), "lcm-helper") {
		t.Error("im Voll-Modus läuft nftables ohne Helper")
	}
}

// TestZugangsdatenStehenNichtImKlartext: Geheimnisse, die als Variable oder als
// Helper-Parameter an den Server gehen, schwärzt die Protokollierung - auch in
// der Form '\” der sudo-Verpackung.
func TestZugangsdatenStehenNichtImKlartext(t *testing.T) {
	const secret = "deadbeef00112233445566778899aabbccddeeff00112233"
	srv := &domain.Server{ServiceUser: "svc", RestrictedSudo: true}
	cs := func(restricted bool) string {
		s, err := crowdsecInstallScript(restricted, pkgApt, "10.0.0.5", nil,
			SecurityToolInput{Tool: "crowdsec", LapiMode: "remote", Collections: []string{"crowdsecurity/sshd"}},
			CrowdSecConfig{LapiURL: "http://lapi.example:8080", LapiLogin: "srv1", LapiPassword: secret})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	console := func(restricted bool) string {
		s, err := crowdsecInstallScript(restricted, pkgApt, "10.0.0.5", nil,
			SecurityToolInput{Tool: "crowdsec", LapiMode: "console", Collections: []string{"crowdsecurity/sshd"}},
			CrowdSecConfig{ConsoleKey: secret})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	scripts := map[string]string{
		"Kanal Voll":           enterpriseChannelScript("https://r.example/e", "id", secret),
		"Kanal Helper":         hostScript(srv, "", "host-apt-channel", "enterprise", "https://r.example/e", "id", helperB64(secret)),
		"CrowdSec Voll":        cs(false),
		"CrowdSec Helper":      cs(true),
		"CrowdSec Konsole V":   console(false),
		"CrowdSec Konsole Hlp": console(true),
	}
	for name, script := range scripts {
		for _, wrapped := range []string{script, privRun(srv, script), "sudo sh -c '" + strings.ReplaceAll(script, "'", `'\''`) + "'"} {
			red := redactSecrets(wrapped)
			for _, leak := range []string{secret, helperB64(secret)} {
				if strings.Contains(red, leak) {
					t.Errorf("%s: %q bleibt im Protokoll stehen:\n%s", name, leak, red)
				}
			}
		}
	}
}

// helperTestScript schreibt den Helper in ein Temp-Verzeichnis.
func helperTestScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lcm-helper")
	if err := os.WriteFile(path, []byte(lcmHelperScript), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestHelperLehntSchlechteParameterAb: Jedes neue Unterkommando weist Parameter
// ab, die nicht in die erwartete Form passen - und zwar bevor es etwas
// schreibt. Alle Fälle hier scheitern in der Prüfung; kein Fall erreicht einen
// Rumpf.
func TestHelperLehntSchlechteParameterAb(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("kein base64")
	}
	helper := helperTestScript(t)
	b64 := helperB64
	zeilen := func(s string) string { return b64(s) }

	cases := [][]string{
		{"host-apt-channel"},
		{"host-apt-channel", "unbekannt"},
		{"host-apt-channel", "community", "extra"},
		{"host-apt-channel", "beta"},
		{"host-apt-channel", "beta", "ftp://x.example"},
		{"host-apt-channel", "beta", "https://a.example/x;rm"},
		{"host-apt-channel", "enterprise", "http://r.example/e", "id", b64("geheim")},
		{"host-apt-channel", "enterprise", "https://r.example/e", "id;rm", b64("geheim")},
		{"host-apt-channel", "enterprise", "https://r.example/e", "id", "!!!kein-base64"},
		{"host-apt-channel", "enterprise", "https://r.example/e", "id", b64("ge heim;rm")},
		{"package-pins", "apt", "a;b", "-"},
		{"package-pins", "apt", "-", "x y"},
		{"package-pins", "apt", "$(id)", "-"},
		{"package-pins", "apt", "GROSS", "-"},
		{"package-pins", "apt", "-o", "-"},
		{"package-pins", "zypper", "-", "-"},
		{"firewall-apply", "nftables", zeilen("flush ruleset")},
		{"firewall-apply", "nftables", zeilen("table inet lcm\ndelete table inet lcm\ntable inet lcm {\n    chain input {\n        type filter hook input priority 0; policy drop;\n        include \"/etc/shadow\"\n    }\n}")},
		{"firewall-apply", "nftables", zeilen("table ip nat\n")},
		{"firewall-apply", "nftables", zeilen("table inet lcm {\n    chain input {\n        type filter hook output priority 0; policy accept;\n    }\n}")},
		{"firewall-apply", "nftables", "!!!"},
		{"firewall-apply", "firewalld", "--add-port=22/tcp;id"},
		{"firewall-apply", "firewalld", "--remove-port=22/tcp"},
		{"firewall-apply", "firewalld", `--add-rich-rule=rule family="ipv4" port port="22" protocol="tcp" reject`},
		{"firewall-apply", "firewalld", "--direct", "--passthrough"},
		{"firewall-apply", "iptables", "x"},
		{"firewall-disable", "ufw"},
		{"firewall-status", "iptables"},
		{"firewall-port", "allow", "nftables", "22;id"},
		{"firewall-port", "flush", "nftables", "22"},
		{"fail2ban-config"},
		{"fail2ban-config", "1.2.3.4;id"},
		{"fail2ban-config", "-1"},
		{"crowdsec-repo", "evil"},
		{"crowdsec-collections", "--help"},
		{"crowdsec-collections", "a;b"},
		{"crowdsec-configure", "bogus", "-", "-", "-", "-", "1.2.3.4"},
		{"crowdsec-configure", "remote", "ftp://x", "l", b64("pw"), "-", "1.2.3.4"},
		{"crowdsec-configure", "remote", "http://x", "l;id", b64("pw"), "-", "1.2.3.4"},
		{"crowdsec-configure", "remote", "http://x", "l", b64("a\nb"), "-", "1.2.3.4"},
		{"crowdsec-configure", "console", "-", "-", "-", "key;id", "1.2.3.4"},
		{"crowdsec-configure", "keep", "-", "-", "-", "-", "evil;ip"},
		{"crowdsec-configure", "keep", "-", "-", "-", "-"},
		{"security-service", "evil", "start"},
		{"security-service", "fail2ban", "reboot"},
		{"security-purge", "evil"},
		{"security-unban", "fail2ban", "x;id"},
		{"security-unban", "fail2ban", "--help"},
		{"security-unban", "evil", "1.2.3.4"},
		{"security-bans", "evil"},
		// Zeilenumbrüche: grep prüft zeilenweise, eine passende Zeile darf nicht reichen.
		{"host-apt-channel", "beta", "https://a.example/x\nevil"},
		{"host-apt-channel", "enterprise", "https://r.example/e", "id", b64("geheim\n[x]")},
		{"package-pins", "apt", "nginx\nDPkg::Pre-Invoke {\"x\"};", "-"},
		{"firewall-port", "allow", "nftables", "22\n23"},
		{"firewall-apply", "firewalld", "--add-port=22/tcp\n--direct"},
		{"fail2ban-config", "1.2.3.4\n[sshd]\nactionban = x"},
		{"crowdsec-collections", "a\n--help"},
		{"crowdsec-configure", "console", "-", "-", "-", "key\nx", "1.2.3.4"},
		{"security-unban", "fail2ban", "1.2.3.4\nx"},
		{"timezone", "Europe/Berlin\nx"},
		// Eine Leerzeile vorne darf den Rest des Regelsatzes nicht verdecken.
		{"firewall-apply", "nftables", zeilen("\ninclude \"/etc/shadow\"\ntable inet lcm")},
	}
	for _, args := range cases {
		cmd := exec.Command(sh, append([]string{helper}, args...)...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("%v wurde angenommen:\n%s", args, out)
			continue
		}
		if !strings.Contains(string(out), "lcm-helper:") {
			t.Errorf("%v: Ablehnung ohne Meldung des Helpers:\n%s", args, out)
		}
	}
}

// helperFunktion schneidet eine Shell-Funktion aus dem Helper heraus.
func helperFunktion(t *testing.T, name string) string {
	t.Helper()
	start := strings.Index(lcmHelperScript, name+"() {")
	if start < 0 {
		t.Fatalf("Funktion %s fehlt im Helper", name)
	}
	end := strings.Index(lcmHelperScript[start:], "\n}\n")
	return lcmHelperScript[start : start+end+3]
}

// TestNftablesRegelsatzPruefungKenntLcmsRegelsatz: Die Prüfung im Helper lässt
// genau zu, was nftRuleset erzeugt - sonst scheiterte eine echte Anwendung - und
// nichts darüber hinaus.
func TestNftablesRegelsatzPruefungKenntLcmsRegelsatz(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	fn := helperFunktion(t, "nft_ruleset_bad_lines")
	bad := func(ruleset string) string {
		out, _ := exec.Command(sh, "-c", fn+"\nnft_ruleset_bad_lines \"$1\"", "sh", ruleset).Output()
		return strings.TrimSpace(string(out))
	}

	ssh := sshFirewallRule(2222, domain.FirewallSSHSources{SourceIPs: []string{"10.0.0.0/24"}})
	ssh.Sources = []string{"10.0.0.0/24", "2001:db8::/32"}
	rules := []domain.FirewallRule{
		{Port: 80, Proto: "tcp", IPVersion: "any", Comment: "Web Server (öffentlich)"},
		{Port: 443, Proto: "tcp", IPVersion: "v4", Sources: []string{"192.0.2.1", "198.51.100.0/24"}, Comment: "intern"},
		{Port: 53, Proto: "udp", IPVersion: "v6"},
		{Port: 8443, Proto: "tcp", IPVersion: "any", Sources: []string{"2001:db8::1"}},
	}
	if got := bad(nftRuleset(ssh, rules)); got != "" {
		t.Errorf("Helper lehnt LCMs eigenen Regelsatz ab, Zeile: %q", got)
	}
	for _, line := range []string{
		"flush ruleset",
		"table ip nat",
		"        tcp dport 22 accept; drop",
		"        tcp dport 22 accept comment \"a\\\"b\"",
		"        include \"/etc/nftables.conf\"",
		"        type filter hook output priority 0; policy accept;",
		"        ip saddr { 1.2.3.4; } tcp dport 22 accept",
		"        tcp dport 22 jump other",
	} {
		if got := bad("table inet lcm\n" + line); got != strings.TrimSpace(line) {
			t.Errorf("Zeile %q wurde nicht abgelehnt (Ausgabe: %q)", line, got)
		}
	}
}

// TestFirewalldFreigabenPassenZurPruefung: Alles, was firewalldApplyArgs
// erzeugt, besteht die Prüfung des Helpers.
func TestFirewalldFreigabenPassenZurPruefung(t *testing.T) {
	// Die zwei Muster stehen im Helper als -e '…'; hier werden sie von dort gelesen.
	muster := regexp.MustCompile(`-e '(\^--add-[^']+\$)'`).FindAllStringSubmatch(lcmHelperScript, -1)
	if len(muster) != 2 {
		t.Fatalf("erwartet zwei Muster im Helper, gefunden %d", len(muster))
	}
	var res []*regexp.Regexp
	for _, m := range muster {
		res = append(res, regexp.MustCompile(m[1]))
	}
	ssh := sshFirewallRule(22, domain.FirewallSSHSources{})
	ssh.Sources = []string{"10.0.0.0/24"}
	rules := []domain.FirewallRule{
		{Port: 80, Proto: "tcp", IPVersion: "any"},
		{Port: 53, Proto: "udp", IPVersion: "v4"},
		{Port: 443, Proto: "tcp", IPVersion: "any", Sources: []string{"192.0.2.1", "2001:db8::/32"}},
		{Port: 25, Proto: "tcp", IPVersion: "v6"},
	}
	for _, a := range firewalldApplyArgs(ssh, rules) {
		ok := false
		for _, re := range res {
			ok = ok || re.MatchString(a)
		}
		if !ok {
			t.Errorf("Helper würde %q ablehnen", a)
		}
	}
}

// TestPinRuempfeSchreibenDieDateien: Die Pin-Rümpfe laufen wirklich - mit
// Attrappen für apt-mark, dpkg-query und rpm und umgebogenen Pfaden.
func TestPinRuempfeSchreibenDieDateien(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	attrappe := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	marks := filepath.Join(dir, "apt-mark.log")
	attrappe("apt-mark", `echo "$@" >> `+marks+`; [ "$1" = showhold ] && echo altes-paket; exit 0`)
	attrappe("dpkg-query", `printf 'bash\nlibstdc++6\nlinux-image-6.1\nnginx\n'`)
	attrappe("rpm", `printf 'bash\nkernel-core\nnginx\n'`)
	attrappe("dnf", "exit 0")
	// Eine Datei, auf die das Muster linux-image-* passen würde, läge es als
	// Dateiname vor: die Rümpfe dürfen die Muster nicht gegen das Verzeichnis auflösen.
	if err := os.WriteFile(filepath.Join(bin, "linux-image-DATEI"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	lauf := func(restricted bool, mgr string, pins []domain.PackagePin) {
		t.Helper()
		script := pkgPinScript(restricted, mgr, pins)
		for _, p := range []string{"/etc/apt/apt.conf.d", "/etc/dnf/protected.d", "/etc/pacman.d", "/etc/pacman.conf"} {
			script = strings.ReplaceAll(script, p, dir+p)
		}
		for _, p := range []string{"/etc/apt/apt.conf.d", "/etc/dnf/protected.d", "/etc/pacman.d"} {
			_ = os.MkdirAll(dir+p, 0o755)
		}
		_ = os.MkdirAll(dir+"/etc", 0o755)
		cmd := exec.Command(sh, "-c", script)
		cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin"}
		cmd.Dir = bin // dort liegen Dateien: ein Muster mit * dürfte nicht zu Dateinamen werden
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", mgr, err, out)
		}
	}
	lies := func(p string) string {
		b, _ := os.ReadFile(dir + p)
		return string(b)
	}
	pins := []domain.PackagePin{
		{Name: "linux-image-*", NoRemove: true},
		{Name: "libstdc++6", NoRemove: true},
		{Name: "bash", Hold: true},
	}

	lauf(false, pkgApt, pins)
	apt := lies(aptPinFile)
	for _, want := range []string{"APT::NeverAutoRemove", `"^linux-image-.*$";`, `"^libstdc\+\+6$";`} {
		if !strings.Contains(apt, want) {
			t.Errorf("apt-Schutzdatei enthält %q nicht:\n%s", want, apt)
		}
	}
	if strings.Contains(apt, "DATEI") {
		t.Errorf("das Muster wurde gegen Dateinamen aufgelöst:\n%s", apt)
	}
	if strings.Contains(apt, "bash") {
		t.Errorf("ein reiner Hold-Pin gehört nicht in die Schutzdatei:\n%s", apt)
	}
	log, _ := os.ReadFile(marks)
	if !strings.Contains(string(log), "unhold altes-paket") || !strings.Contains(string(log), "hold bash") {
		t.Errorf("apt-mark: erst alte Holds lösen, dann bash setzen:\n%s", log)
	}

	lauf(false, pkgApt, nil)
	if lies(aptPinFile) != "" {
		t.Error("ohne Pins muss die apt-Schutzdatei verschwinden")
	}

	lauf(false, pkgDnf, pins[:1])
	if got := lies(dnfPinFile); got != "" {
		t.Errorf("dnf: kein installiertes Paket passt auf linux-image-*, die Datei bleibt leer: %q", got)
	}
	lauf(false, pkgDnf, []domain.PackagePin{{Name: "kern*", NoRemove: true}, {Name: "nginx", NoRemove: true}})
	if got := lies(dnfPinFile); got != "kernel-core\nnginx\n" {
		t.Errorf("dnf-Schutzdatei: %q", got)
	}

	lauf(false, pkgPacman, pins)
	pm := lies("/etc/pacman.d/99-lcm-package-pins.conf")
	for _, want := range []string{"HoldPkg = libstdc++6 linux-image-*", "IgnorePkg = bash"} {
		if !strings.Contains(pm, want) {
			t.Errorf("pacman-Datei enthält %q nicht:\n%s", want, pm)
		}
	}
	if !strings.Contains(lies("/etc/pacman.conf"), "Include = ") {
		t.Error("pacman.conf bekommt den Include nicht")
	}
	lauf(false, pkgPacman, pins)
	if strings.Count(lies("/etc/pacman.conf"), "Include = ") != 1 {
		t.Error("der Include in pacman.conf muss idempotent bleiben")
	}
}

// TestKernelAufraeumenImEingeschraenktenModus: Das Skript braucht keine
// Root-Shell, die Aktion wird nicht mehr abgewiesen - und ihre Sicherungen
// stehen weiter im Skript.
func TestKernelAufraeumenImEingeschraenktenModus(t *testing.T) {
	s := &ServerService{}
	server := &domain.Server{
		RestrictedSudo: true, PackageManager: pkgApt, KernelVersion: "6.8.0-45-generic",
		InstalledKernels: `[{"name":"linux-image-6.8.0-45-generic","release":"6.8.0-45-generic"},{"name":"linux-image-6.8.0-40-generic","release":"6.8.0-40-generic"},` +
			`{"name":"linux-image-6.8.0-35-generic","release":"6.8.0-35-generic"},{"name":"linux-image-6.8.0-30-generic","release":"6.8.0-30-generic"}]`,
	}
	plan, releases, err := s.planKernelCleanup(server)
	if err != nil {
		t.Fatalf("im eingeschränkten Modus abgewiesen: %v", err)
	}
	if len(releases) == 0 {
		t.Fatal("keine Kernel zum Entfernen ausgewählt")
	}
	for _, want := range []string{`uname -r`, `grep -v -F "$RUNNING"`, "lcm_apt -y purge"} {
		if !strings.Contains(plan.script, want) {
			t.Errorf("Sicherung %q fehlt im Skript", want)
		}
	}
	for _, bad := range []string{"sudo", "systemctl", "rm ", "/etc/"} {
		if strings.Contains(plan.script, bad) {
			t.Errorf("%q braucht root und steht im Kernel-Skript", bad)
		}
	}
}

// TestPaketkanalImEingeschraenktenModus: Die Umstellung des LCM-Hosts läuft in
// beiden Modi mit demselben Rumpf - im Voll-Modus als Skript, im
// eingeschränkten über das Helper-Unterkommando host-apt-channel. Vorher lief
// sie dort unprivilegiert und scheiterte an /etc/apt.
func TestPaketkanalImEingeschraenktenModus(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		svc, settings, db := newSubscriptionEnvDB(t)
		if err := db.Create(&domain.Server{Name: "lcm-host", Host: "localhost", SSHPort: 22, PackageManager: "apt", RestrictedSudo: restricted}).Error; err != nil {
			t.Fatal(err)
		}
		key, err := svc.cipher.EncryptString("deadbeef00112233")
		if err != nil {
			t.Fatal(err)
		}
		if err := settings.UpdateFields(map[string]any{
			"subscription_access_key_enc": key, "subscription_status": "active",
			"subscription_repo_url": "https://repo.example/enterprise", "instance_id": "11111111-aaaa-4bbb-8ccc-000000000001",
		}); err != nil {
			t.Fatal(err)
		}
		var scripts []string
		svc.WithLcmHostRunner(func(name, script, actor string, after func(ok bool)) (*domain.Job, error) {
			scripts = append(scripts, script)
			return &domain.Job{}, nil
		})
		for _, channel := range []string{domain.AptChannelCommunity, domain.AptChannelBeta, domain.AptChannelEnterprise} {
			if _, err := svc.SetAptChannel(channel, "admin"); err != nil {
				t.Fatalf("restricted=%v %s: %v", restricted, channel, err)
			}
		}
		if len(scripts) != 3 {
			t.Fatalf("erwartet 3 Skripte, bekommen %d", len(scripts))
		}
		for i, s := range scripts {
			isHelper := strings.HasPrefix(s, "lcm-helper host-apt-channel ")
			if isHelper != restricted {
				t.Errorf("restricted=%v Skript %d: Helper-Aufruf=%v:\n%s", restricted, i, isHelper, s)
			}
		}
		if restricted {
			if !strings.HasPrefix(scripts[1], "lcm-helper host-apt-channel 'beta' 'https://") {
				t.Errorf("beta: %s", scripts[1])
			}
			if strings.Contains(scripts[2], "deadbeef00112233") {
				t.Errorf("enterprise: der Zugangsschlüssel steht im Klartext:\n%s", scripts[2])
			}
			if !strings.Contains(scripts[2], helperB64("deadbeef00112233")) {
				t.Errorf("enterprise: der Zugangsschlüssel geht nicht base64-kodiert an den Helper:\n%s", scripts[2])
			}
		}
	}
}

// TestHelperKenntIPv6AdressenInURLs: Was die Go-Seite als LAPI-URL annimmt, darf
// der Helper nicht als ungültig abweisen. Der Login danach scheitert absichtlich,
// damit der Fall keinen Rumpf erreicht.
func TestHelperKenntIPv6AdressenInURLs(t *testing.T) {
	out, err := exec.Command("sh", helperTestScript(t), "crowdsec-configure", "remote", "http://[::1]:8080", "l;id", helperB64("pw"), "-", "1.2.3.4").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "lapi-login") {
		t.Errorf("erwartet Ablehnung des Logins, nicht der URL: %s", out)
	}
}

// TestLapiUrlBleibtBeiFehlerStehen: Ein Helper ohne crowdsec-lapi-url antwortet
// mit einer Fehlermeldung, die als URL gespeichert würde.
func TestLapiUrlBleibtBeiFehlerStehen(t *testing.T) {
	server := &domain.Server{RestrictedSudo: true, ServiceUser: "lcm-svc"}
	cases := []struct {
		name, out string
		code      int
		want      map[string]any
	}{
		{"alter Helper", "lcm-helper: unbekanntes kommando: crowdsec-lapi-url\n", 1, nil},
		{"Fremdtext trotz Erfolg", "sudo: kaputt\n", 0, nil},
		{"nicht lesbar", "__na__\n", 0, nil},
		{"URL", "https://lapi.example:8080\n", 0, map[string]any{"crowdsec_lapi_url": "https://lapi.example:8080"}},
		{"Datei ohne url", "", 0, map[string]any{"crowdsec_lapi_url": ""}},
	}
	for _, c := range cases {
		conn := &scriptedConn{handler: func(string, string) (string, int) { return c.out, c.code }}
		if got := crowdsecLapiURLFields(conn, server); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, erwartet %v", c.name, got, c.want)
		}
	}
}

// TestEinrichtungFragtZuerstDenHelper: Vor der Paketinstallation steht die
// Frage nach dem Helper-Stand, damit ein alter Helper nichts halb einrichtet.
func TestEinrichtungFragtZuerstDenHelper(t *testing.T) {
	script := fail2banInstallScript(true, pkgApt, "10.0.0.1", nil)
	probe, install := strings.Index(script, "lcm-helper version"), strings.Index(script, pkgInstallScript(pkgApt, []string{"fail2ban"}))
	if probe < 0 || install < probe || !strings.Contains(script, lcmHelperVersion) {
		t.Errorf("Die Helper-Prüfung muss vor den Paketen stehen:\n%s", script)
	}
	if strings.Contains(fail2banInstallScript(false, pkgApt, "10.0.0.1", nil), "lcm-helper") {
		t.Error("Voll-Modus fragt den Helper")
	}
}
