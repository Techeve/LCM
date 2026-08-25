package services

import (
	"strings"
	"testing"
)

// TestServiceCommandCoversInitSystems: LCM verwaltet Debian, RHEL, SUSE, Arch
// UND Alpine. Ein reines `systemctl` wuerde auf Alpine (OpenRC) wirkungslos
// durchlaufen - die Aktion meldete Erfolg, ohne etwas zu tun.
func TestServiceCommandCoversInitSystems(t *testing.T) {
	for _, action := range []string{SecurityToolActionStart, SecurityToolActionStop, SecurityToolActionRestart} {
		cmd := serviceCommand(action, "fail2ban")
		for _, want := range []string{"systemctl", "service ", "rc-service"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("%s: %q fehlt in %q", action, want, cmd)
			}
		}
	}
	if !strings.Contains(serviceCommand(SecurityToolActionEnable, "crowdsec"), "rc-update add") {
		t.Error("enable deckt OpenRC nicht ab")
	}
	if !strings.Contains(serviceCommand(SecurityToolActionDisable, "crowdsec"), "rc-update del") {
		t.Error("disable deckt OpenRC nicht ab")
	}
}

// TestUninstallRemovesBouncer: Bleibt der CrowdSec-Firewall-Bouncer beim
// Entfernen zurueck, sperrt er weiter nach Regeln, die niemand mehr pflegt -
// im schlimmsten Fall sperrt er dauerhaft aus.
func TestUninstallRemovesBouncer(t *testing.T) {
	script := uninstallScript("apt", SecurityToolCrowdSec)
	for _, want := range []string{"crowdsec-firewall-bouncer-iptables", "crowdsec-firewall-bouncer-nftables", "/etc/crowdsec"} {
		if !strings.Contains(script, want) {
			t.Errorf("uninstall-skript enthaelt %q nicht:\n%s", want, script)
		}
	}
	// Der Dienst muss VOR dem Paketentfernen gestoppt werden.
	if strings.Index(script, "stop") > strings.Index(script, "rm -rf") {
		t.Error("dienst wird nicht vor dem entfernen gestoppt")
	}
}

// TestUninstallFail2banKeepsCrowdSecUntouched: ein Werkzeug entfernen darf das
// andere nicht mitnehmen.
func TestUninstallFail2banKeepsCrowdSecUntouched(t *testing.T) {
	script := uninstallScript("apt", SecurityToolFail2ban)
	if strings.Contains(script, "crowdsec") {
		t.Errorf("fail2ban-deinstallation fasst crowdsec an:\n%s", script)
	}
	if !strings.Contains(script, "/etc/fail2ban") {
		t.Error("fail2ban-konfiguration wird nicht entfernt")
	}
}

// TestAllowlistUpdateKeepsLoopback: Ohne Loopback in der Allowlist sperrt sich
// ein Dienst potenziell selbst aus. Die Einrichtung garantiert das bereits -
// die nachtraegliche Aenderung muss dieselbe Zusage halten.
func TestAllowlistUpdateKeepsLoopback(t *testing.T) {
	for _, tool := range []string{SecurityToolFail2ban, SecurityToolCrowdSec} {
		script := allowlistUpdateScript(tool, "10.0.0.5", []string{"192.168.1.0/24"})
		for _, want := range []string{"127.0.0.1/8", "::1", "10.0.0.5", "192.168.1.0/24"} {
			if !strings.Contains(script, want) {
				t.Errorf("%s: %q fehlt in der neuen allowlist:\n%s", tool, want, script)
			}
		}
	}
}

// TestAllowlistUpdateReloads: Eine geschriebene Datei ohne Nachladen waere
// wirkungslos, bis jemand den Dienst zufaellig neu startet.
func TestAllowlistUpdateReloads(t *testing.T) {
	f2b := allowlistUpdateScript(SecurityToolFail2ban, "10.0.0.5", nil)
	if !strings.Contains(f2b, "fail2ban-client reload") {
		t.Errorf("fail2ban laedt die allowlist nicht nach:\n%s", f2b)
	}
	cs := allowlistUpdateScript(SecurityToolCrowdSec, "10.0.0.5", nil)
	if !strings.Contains(cs, "systemctl restart crowdsec") {
		t.Errorf("crowdsec laedt die allowlist nicht nach:\n%s", cs)
	}
}

// TestUnbanCoversAllJails: fail2ban sperrt je Jail. Wer nur sshd entsperrt,
// laesst die IP in allen anderen Jails gesperrt - der Anwender haelt sich fuer
// entsperrt und kommt trotzdem nicht rein.
func TestUnbanCoversAllJails(t *testing.T) {
	script := unbanScript(SecurityToolFail2ban, "203.0.113.7")
	if !strings.Contains(script, "Jail list") {
		t.Errorf("unban geht nicht ueber alle jails:\n%s", script)
	}
	if !strings.Contains(script, "203.0.113.7") {
		t.Error("ip fehlt im unban-kommando")
	}
	cs := unbanScript(SecurityToolCrowdSec, "203.0.113.7")
	if !strings.Contains(cs, "cscli decisions delete --ip 203.0.113.7") {
		t.Errorf("crowdsec-unban falsch:\n%s", cs)
	}
}

// TestParseBansFail2ban: Format "jail|ip", Leerzeilen und Muell werden
// ignoriert statt als Sperre gezaehlt.
func TestParseBansFail2ban(t *testing.T) {
	out := "sshd|203.0.113.7\nsshd|198.51.100.4\n\nkaputt\nnginx|203.0.113.9\n"
	bans := parseBans(SecurityToolFail2ban, out)
	if len(bans) != 3 {
		t.Fatalf("erwartet 3 sperren, bekommen %d: %+v", len(bans), bans)
	}
	if bans[0].IP != "203.0.113.7" || bans[0].Scope != "sshd" {
		t.Errorf("erste sperre falsch geparst: %+v", bans[0])
	}
	if bans[2].Scope != "nginx" {
		t.Errorf("jail der dritten sperre falsch: %+v", bans[2])
	}
}

// TestParseBansCrowdSec: cscli liefert JSON.
func TestParseBansCrowdSec(t *testing.T) {
	out := `[{"value":"203.0.113.7","scenario":"crowdsecurity/ssh-bf","duration":"3h59m","origin":"crowdsec"}]`
	bans := parseBans(SecurityToolCrowdSec, out)
	if len(bans) != 1 {
		t.Fatalf("erwartet 1 sperre, bekommen %d", len(bans))
	}
	if bans[0].IP != "203.0.113.7" || bans[0].Scope != "crowdsecurity/ssh-bf" {
		t.Errorf("falsch geparst: %+v", bans[0])
	}
	if bans[0].Since != "3h59m" {
		t.Errorf("restdauer fehlt: %+v", bans[0])
	}
}

// TestParseBansToleratesGarbage: Ist das Werkzeug nicht installiert oder
// antwortet unerwartet, darf die Oberflaeche eine leere Liste sehen - keinen
// Absturz und keine erfundenen Eintraege.
func TestParseBansToleratesGarbage(t *testing.T) {
	for _, out := range []string{"", "command not found", "{kein json}"} {
		if n := len(parseBans(SecurityToolCrowdSec, out)); n != 0 {
			t.Errorf("crowdsec: aus %q wurden %d sperren", out, n)
		}
	}
	if n := len(parseBans(SecurityToolFail2ban, "")); n != 0 {
		t.Errorf("fail2ban: leere ausgabe ergab %d sperren", n)
	}
}
