package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func TestValidDNSServers(t *testing.T) {
	// Gültige IPv4/IPv6-Mischung wird bereinigt zurückgegeben.
	got, err := validDNSServers([]string{" 1.1.1.1 ", "", "2606:4700:4700::1111"})
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(got) != 2 || got[0] != "1.1.1.1" || got[1] != "2606:4700:4700::1111" {
		t.Fatalf("Bereinigung falsch: %#v", got)
	}
	// Ungültige IP wird abgelehnt.
	if _, err := validDNSServers([]string{"nicht-ip"}); err == nil {
		t.Fatal("ungültige IP hätte abgelehnt werden müssen")
	}
	// Mehr als das Maximum wird abgelehnt.
	if _, err := validDNSServers([]string{"1.1.1.1", "8.8.8.8", "9.9.9.9", "8.8.4.4"}); err == nil {
		t.Fatalf("mehr als %d DNS-Server hätten abgelehnt werden müssen", domain.MaxDNSServers)
	}
}

func TestParseDNSTest(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		status string
	}{
		{"alle ok", "OK github.com\nOK cloudflare.com\n", domain.DNSTestFull},
		{"teilweise", "OK github.com\nFAIL deb.debian.org\n", domain.DNSTestPartial},
		{"keine", "FAIL github.com\nFAIL deb.debian.org\n", domain.DNSTestNone},
		{"leer", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, detail := parseDNSTest(c.out)
			if status != c.status {
				t.Fatalf("Status = %q, erwartet %q (detail %q)", status, c.status, detail)
			}
		})
	}
	// Detailzeile trennt OK und FAIL.
	_, detail := parseDNSTest("OK a.com\nFAIL b.com\n")
	if !strings.Contains(detail, "OK: a.com") || !strings.Contains(detail, "FAIL: b.com") {
		t.Fatalf("Detail unvollständig: %q", detail)
	}
}

func TestParseDNSList(t *testing.T) {
	if got := parseDNSList(" 1.1.1.1  8.8.8.8 \n"); got != "1.1.1.1, 8.8.8.8" {
		t.Fatalf("parseDNSList = %q", got)
	}
	if got := parseDNSList("   "); got != "" {
		t.Fatalf("leere Eingabe sollte leer bleiben, war %q", got)
	}
}

func TestSanitizeDNSDomains(t *testing.T) {
	got := sanitizeDNSDomains([]string{"github.com", " bad domain ", "a_b-c.example.com", "rm -rf /"})
	if len(got) != 2 || got[0] != "github.com" || got[1] != "a_b-c.example.com" {
		t.Fatalf("Sanitisierung falsch: %#v", got)
	}
}

func TestDNSApplyScript(t *testing.T) {
	script := dnsApplyScript([]string{"1.1.1.1", "8.8.8.8"}, "example.com")
	// Beide Zweige (resolved-Drop-in und resolv.conf) samt Test + Rollback.
	for _, want := range []string{
		dnsResolvedDropin, dnsResolvConf, "DNS=1.1.1.1 8.8.8.8",
		"nameserver 1.1.1.1", "resolve_ok 'example.com'", "zurueckgerollt",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Apply-Skript enthält %q nicht:\n%s", want, script)
		}
	}
	// Leere Liste => Rückbau-Skript.
	if un := dnsApplyScript(nil, ""); !strings.Contains(un, "DNS-Verwaltung entfernt") {
		t.Fatalf("leere Liste sollte das Rückbau-Skript liefern: %s", un)
	}
}

func TestDNSTestScript(t *testing.T) {
	script := dnsTestScript([]string{"github.com", "deb.debian.org"})
	for _, want := range []string{"getent hosts", "nslookup", "'github.com'", "'deb.debian.org'", "echo \"OK $d\""} {
		if !strings.Contains(script, want) {
			t.Fatalf("Test-Skript enthält %q nicht:\n%s", want, script)
		}
	}
}
