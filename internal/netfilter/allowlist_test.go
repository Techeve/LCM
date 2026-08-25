package netfilter

import (
	"net/netip"
	"testing"
)

func TestAllowlistEmptyAllowsEverything(t *testing.T) {
	a, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !a.IsEmpty() {
		t.Error("leere Eingabe sollte eine leere Allowlist ergeben")
	}
	for _, ip := range []string{"8.8.8.8", "192.168.1.5", "::1", "2001:db8::1"} {
		if !a.Allows(netip.MustParseAddr(ip)) {
			t.Errorf("leere Allowlist muss %s zulassen", ip)
		}
	}
}

func TestAllowlistWhitespaceEntriesIgnored(t *testing.T) {
	a, err := Parse([]string{"  ", "", "\t"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.IsEmpty() {
		t.Error("nur Whitespace-Einträge müssen als leer gelten (alles erlaubt)")
	}
}

func TestAllowlistKeywords(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		allow   []string
		deny    []string
	}{
		{
			name:    "localhost",
			entries: []string{"localhost"},
			allow:   []string{"127.0.0.1", "127.5.6.7", "::1"},
			deny:    []string{"192.168.1.1", "10.0.0.1", "8.8.8.8"},
		},
		{
			name:    "loopback alias",
			entries: []string{"loopback"},
			allow:   []string{"127.0.0.1", "::1"},
			deny:    []string{"192.168.0.1"},
		},
		{
			name:    "private umfasst RFC1918 + ULA + loopback",
			entries: []string{"private"},
			allow:   []string{"10.1.2.3", "172.16.5.5", "172.31.255.254", "192.168.100.1", "169.254.1.1", "fd00::1", "127.0.0.1", "::1", "fe80::1"},
			deny:    []string{"8.8.8.8", "1.1.1.1", "172.32.0.1", "2001:db8::1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Parse(tt.entries)
			if err != nil {
				t.Fatal(err)
			}
			for _, ip := range tt.allow {
				if !a.Allows(netip.MustParseAddr(ip)) {
					t.Errorf("%s sollte erlaubt sein", ip)
				}
			}
			for _, ip := range tt.deny {
				if a.Allows(netip.MustParseAddr(ip)) {
					t.Errorf("%s sollte verweigert sein", ip)
				}
			}
		})
	}
}

func TestAllowlistCIDRAndSingleIP(t *testing.T) {
	a, err := Parse([]string{"203.0.113.5", "10.0.0.0/8", "2001:db8::/32"})
	if err != nil {
		t.Fatal(err)
	}
	allow := []string{"203.0.113.5", "10.255.1.1", "2001:db8:abcd::1"}
	deny := []string{"203.0.113.6", "11.0.0.1", "2001:dead::1"}
	for _, ip := range allow {
		if !a.Allows(netip.MustParseAddr(ip)) {
			t.Errorf("%s sollte erlaubt sein", ip)
		}
	}
	for _, ip := range deny {
		if a.Allows(netip.MustParseAddr(ip)) {
			t.Errorf("%s sollte verweigert sein", ip)
		}
	}
}

// Host-Bits im CIDR sind erlaubt und werden normalisiert (192.168.1.5/24).
func TestAllowlistCIDRWithHostBits(t *testing.T) {
	a, err := Parse([]string{"192.168.1.5/24"})
	if err != nil {
		t.Fatalf("host-bits im cidr sollten akzeptiert werden: %v", err)
	}
	if !a.Allows(netip.MustParseAddr("192.168.1.200")) {
		t.Error("192.168.1.200 sollte im /24 liegen")
	}
	if a.Allows(netip.MustParseAddr("192.168.2.1")) {
		t.Error("192.168.2.1 liegt außerhalb des /24")
	}
}

// IPv4-in-IPv6-gemappte Adressen (::ffff:127.0.0.1) matchen den IPv4-Eintrag.
func TestAllowlistUnmapsV4in6(t *testing.T) {
	a, err := Parse([]string{"localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Allows(netip.MustParseAddr("::ffff:127.0.0.1")) {
		t.Error("v4-in-v6-gemapptes loopback sollte erlaubt sein")
	}
}

func TestAllowlistInvalidEntry(t *testing.T) {
	for _, bad := range []string{"nicht-eine-ip", "999.1.1.1", "10.0.0.0/40", "192.168/16"} {
		if _, err := Parse([]string{bad}); err == nil {
			t.Errorf("Eintrag %q hätte einen Fehler liefern müssen", bad)
		}
	}
}

func TestAllowlistInvalidAddrDenied(t *testing.T) {
	a, _ := Parse([]string{"localhost"})
	if a.Allows(netip.Addr{}) {
		t.Error("ungültige Adresse muss verweigert werden")
	}
}
