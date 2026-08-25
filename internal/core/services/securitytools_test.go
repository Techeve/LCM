package services

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSecurityToolInput(t *testing.T) {
	// fail2ban: gültige IPs bereinigt.
	in := SecurityToolInput{Tool: "fail2ban", AllowlistIPs: []string{" 10.0.0.5 ", "", "192.168.1.1"}}
	if err := validateSecurityToolInput(&in); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(in.AllowlistIPs) != 2 {
		t.Fatalf("IPs falsch bereinigt: %#v", in.AllowlistIPs)
	}
	// unbekanntes Tool.
	if err := validateSecurityToolInput(&SecurityToolInput{Tool: "snort"}); !errors.Is(err, ErrUnknownSecurityTool) {
		t.Fatalf("erwartet ErrUnknownSecurityTool, bekam %v", err)
	}
	// ungültige IP.
	if err := validateSecurityToolInput(&SecurityToolInput{Tool: "fail2ban", AllowlistIPs: []string{"nope"}}); !errors.Is(err, ErrInvalidAllowlistIP) {
		t.Fatalf("erwartet ErrInvalidAllowlistIP, bekam %v", err)
	}
	// CIDR (v4 + v6) wird jetzt akzeptiert und kanonisiert (ignoreip/whitelist
	// vertragen CIDR).
	cidr := SecurityToolInput{Tool: "fail2ban", AllowlistIPs: []string{"10.0.0.0/24", "2001:db8::/32"}}
	if err := validateSecurityToolInput(&cidr); err != nil {
		t.Fatalf("CIDR muss akzeptiert werden: %v", err)
	}
	if len(cidr.AllowlistIPs) != 2 {
		t.Fatalf("CIDR-IPs falsch: %#v", cidr.AllowlistIPs)
	}
	// crowdsec: Default-Collection + Default-LAPI-Modus.
	cs := SecurityToolInput{Tool: "crowdsec"}
	if err := validateSecurityToolInput(&cs); err != nil {
		t.Fatalf("crowdsec: %v", err)
	}
	if len(cs.Collections) != 1 || cs.Collections[0] != "crowdsecurity/sshd" || cs.LapiMode != "local" {
		t.Fatalf("crowdsec-Defaults falsch: %#v", cs)
	}
	// ungültige Collection / LAPI-Modus.
	if err := validateSecurityToolInput(&SecurityToolInput{Tool: "crowdsec", Collections: []string{"bad name!"}}); err == nil {
		t.Fatal("ungültige Collection hätte abgelehnt werden müssen")
	}
	if err := validateSecurityToolInput(&SecurityToolInput{Tool: "crowdsec", LapiMode: "cloud"}); err == nil {
		t.Fatal("ungültiger LAPI-Modus hätte abgelehnt werden müssen")
	}
}

func TestAllowlistIPs(t *testing.T) {
	got := allowlistIPs("10.0.0.5", []string{"10.0.0.5", "192.168.1.1", ""})
	// Loopback + srcIP + extra, dedupliziert.
	want := []string{"127.0.0.1/8", "::1", "10.0.0.5", "192.168.1.1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("allowlistIPs = %#v, erwartet %#v", got, want)
	}
	// leere Quell-IP wird ausgelassen.
	if g := allowlistIPs("", nil); len(g) != 2 {
		t.Fatalf("ohne srcIP nur Loopback erwartet, bekam %#v", g)
	}
}

func TestFail2banInstallScript(t *testing.T) {
	s := fail2banInstallScript(pkgApt, "10.0.0.5", []string{"192.168.1.1"})
	for _, want := range []string{"apt-get install -y", "fail2ban", "ignoreip = 127.0.0.1/8 ::1 10.0.0.5 192.168.1.1", "[sshd]\\nenabled = true", "backend = systemd", "fail2ban-client status sshd"} {
		if !strings.Contains(s, want) {
			t.Fatalf("fail2ban-Skript enthält %q nicht:\n%s", want, s)
		}
	}
}

func TestCrowdsecInstallScript(t *testing.T) {
	// pacman: nicht unterstützt.
	if _, err := crowdsecInstallScript(pkgPacman, "10.0.0.5", nil, SecurityToolInput{Tool: "crowdsec", Collections: []string{"crowdsecurity/sshd"}, LapiMode: "local"}, CrowdSecConfig{}); !errors.Is(err, ErrCrowdSecUnsupported) {
		t.Fatalf("pacman sollte ErrCrowdSecUnsupported liefern, bekam %v", err)
	}
	// apt, Bouncer + Collection + lokale LAPI + Allowlist.
	in := SecurityToolInput{Tool: "crowdsec", Bouncer: true, Collections: []string{"crowdsecurity/sshd"}, LapiMode: "local"}
	s, err := crowdsecInstallScript(pkgApt, "10.0.0.5", nil, in, CrowdSecConfig{})
	if err != nil {
		t.Fatalf("crowdsec apt: %v", err)
	}
	for _, want := range []string{"install -y crowdsec", "cscli collections install crowdsecurity/sshd", "crowdsec-firewall-bouncer", "lcm-whitelist.yaml", "- 10.0.0.5"} {
		if !strings.Contains(s, want) {
			t.Fatalf("crowdsec-Skript enthält %q nicht:\n%s", want, s)
		}
	}
	// remote-LAPI ohne Zugang → Fehler.
	if _, err := crowdsecInstallScript(pkgApt, "10.0.0.5", nil, SecurityToolInput{Tool: "crowdsec", LapiMode: "remote", Collections: []string{"crowdsecurity/sshd"}}, CrowdSecConfig{}); !errors.Is(err, ErrCrowdSecLapiMissing) {
		t.Fatalf("remote ohne Zugang sollte ErrCrowdSecLapiMissing liefern, bekam %v", err)
	}
	// remote-LAPI mit Zugang → schreibt credentials.
	s2, err := crowdsecInstallScript(pkgApt, "10.0.0.5", nil, SecurityToolInput{Tool: "crowdsec", LapiMode: "remote", Collections: []string{"crowdsecurity/sshd"}}, CrowdSecConfig{LapiURL: "http://lapi.example:8080", LapiLogin: "srv1", LapiPassword: "secret"})
	if err != nil {
		t.Fatalf("remote mit Zugang: %v", err)
	}
	if !strings.Contains(s2, "local_api_credentials.yaml") || !strings.Contains(s2, "base64 -d") {
		t.Fatalf("remote-LAPI-Skript unvollständig:\n%s", s2)
	}
	// console ohne Key → Fehler.
	if _, err := crowdsecInstallScript(pkgApt, "10.0.0.5", nil, SecurityToolInput{Tool: "crowdsec", LapiMode: "console", Collections: []string{"crowdsecurity/sshd"}}, CrowdSecConfig{}); !errors.Is(err, ErrCrowdSecConsoleMissing) {
		t.Fatalf("console ohne Key sollte ErrCrowdSecConsoleMissing liefern, bekam %v", err)
	}
}

func TestShellSafe(t *testing.T) {
	if got := shellSafe("ok'value$`\n"); got != "okvalue" {
		t.Fatalf("shellSafe = %q", got)
	}
}
