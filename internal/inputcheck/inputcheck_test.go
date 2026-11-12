package inputcheck

import "testing"

func TestIPProblem(t *testing.T) {
	tests := []struct{ in, want string }{
		{"192.168.1.1", ""},
		{"0.0.0.0", ""},
		{"255.255.255.255", ""},
		{"192.168.1.a", ReasonLetter},
		{"abc", ReasonLetter},
		{"192.168.1", ReasonFormat},
		{"1.2.3.4.5", ReasonFormat},
		{"1.2.3.", ReasonFormat},
		{"1..3.4", ReasonFormat},
		{"1.2.3.-4", ReasonFormat},
		{"1.2.3.4 ", ReasonFormat},
		{"", ReasonFormat},
		{"300.1.1.1", ReasonOctet},
		{"1.2.3.999999999999", ReasonOctet},
		{"010.0.0.1", ReasonLeadingZero},
		{"2001:db8::1", ""},
		{"::1", ""},
		{"::", ""},
		{"::ffff:192.168.1.1", ""},
		{"fe80::1", ""},
		{"2001:db8::g", ReasonIPv6},
		{"2001:db8:::1", ReasonIPv6},
		{"1:2:3:4:5:6:7:8:9", ReasonIPv6},
		{"12345::1", ReasonIPv6},
		{"fe80::1%eth0", ReasonIPv6},
		{"::ffff:300.1.1.1", ReasonIPv6},
	}
	for _, tt := range tests {
		if got := IPProblem(tt.in); got != tt.want {
			t.Errorf("IPProblem(%q) = %q, erwartet %q", tt.in, got, tt.want)
		}
	}
}

func TestNetProblem(t *testing.T) {
	tests := []struct{ in, want string }{
		{"10.0.0.0/8", ""},
		{"192.168.1.0/24", ""},
		{"192.168.1.1/32", ""},
		{"0.0.0.0/0", ""},
		{"2001:db8::/32", ""},
		{"::/0", ""},
		{"::1/128", ""},
		{"10.0.0.5/24", ReasonHostBits},
		{"2001:db8::1/32", ReasonHostBits},
		{"10.0.0.0/33", ReasonPrefix},
		{"2001:db8::/129", ReasonPrefix},
		{"10.0.0.0/", ReasonPrefix},
		{"10.0.0.0/a", ReasonPrefix},
		{"10.0.0.0/-1", ReasonPrefix},
		{"10.0.0.0/08", ReasonPrefix},
		{"10.0.0.0/8/8", ReasonPrefix},
		{"10.0.0.0", ReasonPrefix},
		{"10.0.0.x/24", ReasonLetter},
		{"2001:db8::g/32", ReasonIPv6},
	}
	for _, tt := range tests {
		if got := NetProblem(tt.in); got != tt.want {
			t.Errorf("NetProblem(%q) = %q, erwartet %q", tt.in, got, tt.want)
		}
	}
}

func TestAddressOrNetProblem(t *testing.T) {
	for in, want := range map[string]string{"10.0.0.1": "", "10.0.0.0/8": "", "10.0.0.a": ReasonLetter, "10.0.0.1/8": ReasonHostBits} {
		if got := AddressOrNetProblem(in); got != want {
			t.Errorf("AddressOrNetProblem(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

func TestHostProblem(t *testing.T) {
	tests := []struct{ in, want string }{
		{"server01", ""},
		{"web-1.example.com", ""},
		{"web-1.example.com.", ""},
		{"_dmarc.example.com", ""},
		{"10.0.0.5", ""},
		{"2001:db8::1", ""},
		{"[2001:db8::1]", ""},
		{"fe80::1%eth0", ""},
		{"localhost", ""},
		{"1a.example.com", ""},
		{"300.1.1.1", ReasonOctet},
		{"192.168.1.x", ReasonLetter},
		{"192.168.1", ReasonFormat},
		{"10", ReasonFormat},
		{"[2001:db8::1", ReasonIPv6},
		{"[10.0.0.1]", ReasonIPv6},
		{"2001:db8::g", ReasonIPv6},
		{"fe80::1%et h0", ReasonIPv6},
		{"-web.example.com", ReasonHostname},
		{"web-.example.com", ReasonHostname},
		{"web..example.com", ReasonHostname},
		{"web example", ReasonHostname},
		{"web;reboot", ReasonHostname},
		{"", ReasonHostname},
	}
	for _, tt := range tests {
		if got := HostProblem(tt.in); got != tt.want {
			t.Errorf("HostProblem(%q) = %q, erwartet %q", tt.in, got, tt.want)
		}
	}
}

func TestLooksLikeSQLFindsAttacks(t *testing.T) {
	for _, in := range []string{
		"' OR '1'='1",
		"' or 1=1 --",
		"admin'--",
		"admin' #",
		`" OR ""="`,
		"1 OR 1=1",
		"1) OR (1=1",
		"' UNION SELECT username, password FROM users--",
		"x' UNION ALL SELECT NULL,NULL--",
		"UNION/**/SELECT/**/1,2",
		"uNiOn SeLeCt 1",
		"%27%20OR%201%3D1--",
		"1; DROP TABLE servers",
		"'; DELETE FROM users; --",
		"; ATTACH DATABASE '/tmp/x' AS y",
		"SELECT * FROM users WHERE id = 1",
		"INSERT INTO users VALUES (1); --",
		"DROP TABLE servers; --",
		"1 AND SLEEP(5)",
		"1; SELECT pg_sleep(5)",
		"1 AND 1=(SELECT 1 FROM sqlite_master)",
		"select name from information_schema.tables",
		"' AND randomblob(1000000)",
		"1' WAITFOR DELAY '0:0:5'--",
		"SELECT load_extension('x')",
	} {
		if !LooksLikeSQL(in) {
			score, signals := SQLScore(in)
			t.Errorf("nicht erkannt: %q (Punkte %d, %v)", in, score, signals)
		}
	}
}

func TestLooksLikeSQLLeavesTextAlone(t *testing.T) {
	for _, in := range []string{
		"",
		"web01",
		"O'Brien",
		"Rock'n'Roll Server",
		"Tony's Testserver",
		"Backup-Server (Produktion) - nicht neu starten!",
		"Update & Reboot",
		"select the servers from the list",
		"Bitte drop table nicht verwenden",
		"Delete from group after migration",
		"Kunde: Müller & Söhne; Ansprechpartner: Herr Maier",
		"Wartung and 2 weitere Punkte",
		"Linux-Benutzer für Abteilung 12 or 13",
		"C# Anwendungen",
		"Port 8080 -- nur intern",
		"/var/log/*",
		"*/5 * * * *",
		"https://example.com/a?id=1&x=2#frag",
		"max@example.com",
		"Nur 100% Auslastung",
		"where is it",
		"Update server set",
		"192.168.1.1",
		"2001:db8::1",
		"Europe/Berlin",
	} {
		if LooksLikeSQL(in) {
			score, signals := SQLScore(in)
			t.Errorf("Fehlalarm: %q (Punkte %d, %v)", in, score, signals)
		}
	}
}

// TestSQLSignalsUnique: Jedes Merkmal zählt einmal. Ein doppelt eingetragenes
// Muster verdoppelte stillschweigend sein Gewicht.
func TestSQLSignalsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range sqlSignals {
		if seen[s.name] {
			t.Errorf("Merkmal %s ist doppelt eingetragen", s.name)
		}
		seen[s.name] = true
	}
}
