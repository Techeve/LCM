package netfilter

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"leer", nil, []string{}},
		{"Leerzeilen fallen weg", []string{"", "  ", "\t"}, []string{}},
		{"einzelne Adresse wird /32", []string{"10.0.0.5"}, []string{"10.0.0.5/32"}},
		{"IPv6 wird /128", []string{"2001:db8::1"}, []string{"2001:db8::1/128"}},
		{"Host-Bits fallen weg", []string{"192.168.1.77/24"}, []string{"192.168.1.0/24"}},
		{"IPv4-in-IPv6-Adresse", []string{"::ffff:10.0.0.5"}, []string{"10.0.0.5/32"}},
		{"IPv4-in-IPv6-Netz", []string{"::ffff:10.0.0.0/104"}, []string{"10.0.0.0/8"}},
		{"Schlüsselwörter klein", []string{"Private", " LOCALHOST "}, []string{"private", "localhost"}},
		{"Duplikate nach Normalisierung", []string{"10.0.0.5", "10.0.0.5/32", "::ffff:10.0.0.5"}, []string{"10.0.0.5/32"}},
		{"Reihenfolge bleibt", []string{"192.168.0.0/16", "10.0.0.0/8"}, []string{"192.168.0.0/16", "10.0.0.0/8"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("= %v, erwartet %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeNamesTheBadEntry(t *testing.T) {
	for _, bad := range []string{"10.0.0.256", "kein-netz", "10.0.0.0/33", "10.0.0.0/"} {
		_, err := Normalize([]string{"10.0.0.0/8", bad})
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", bad)) {
			t.Errorf("%q: Fehler nennt den Eintrag nicht: %v", bad, err)
		}
	}
}

func TestNormalizeLimit(t *testing.T) {
	entries := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("10.%d.0.0/16", i)
		}
		return out
	}
	if _, err := Normalize(entries(MaxNetworkEntries)); err != nil {
		t.Errorf("genau %d Einträge: %v", MaxNetworkEntries, err)
	}
	if _, err := Normalize(entries(MaxNetworkEntries + 1)); err == nil {
		t.Errorf("%d Einträge wurden akzeptiert", MaxNetworkEntries+1)
	}
	// Duplikate zählen nicht mit.
	dups := append(entries(MaxNetworkEntries), entries(MaxNetworkEntries)...)
	if _, err := Normalize(dups); err != nil {
		t.Errorf("Duplikate zählten mit: %v", err)
	}
}

// Ein IPv4-in-IPv6-Netz in der Liste muss die IPv4-Adresse des Clients treffen.
func TestParseMappedPrefixMatchesIPv4Client(t *testing.T) {
	list, err := Parse([]string{"::ffff:10.0.0.0/104"})
	if err != nil {
		t.Fatal(err)
	}
	if !list.Allows(netip.MustParseAddr("10.1.2.3")) || list.Allows(netip.MustParseAddr("11.0.0.1")) {
		t.Error("gemapptes Netz trifft die IPv4-Adresse nicht")
	}
}

func TestHolder(t *testing.T) {
	var nilHolder *Holder
	if !nilHolder.Allows(netip.MustParseAddr("8.8.8.8")) {
		t.Error("nil-Halter muss alles zulassen")
	}
	var h Holder
	if !h.Allows(netip.MustParseAddr("8.8.8.8")) {
		t.Error("leerer Halter muss alles zulassen")
	}
	list, _ := Parse([]string{"10.0.0.0/8"})
	h.Store(list)
	if h.Allows(netip.MustParseAddr("8.8.8.8")) || !h.Allows(netip.MustParseAddr("10.0.0.1")) {
		t.Error("gespeicherte Liste gilt nicht")
	}
	h.Store(Allowlist{})
	if !h.Allows(netip.MustParseAddr("8.8.8.8")) {
		t.Error("geleerte Liste muss alles zulassen")
	}
}

func TestParseEndpoint(t *testing.T) {
	for _, name := range []string{"ui", "agent", "mcp"} {
		if ep, ok := ParseEndpoint(name); !ok || string(ep) != name {
			t.Errorf("%s: %q, %v", name, ep, ok)
		}
	}
	for _, name := range []string{"", "UI", "web", "../ui"} {
		if _, ok := ParseEndpoint(name); ok {
			t.Errorf("%q wurde akzeptiert", name)
		}
	}
}
