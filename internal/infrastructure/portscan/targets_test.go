package portscan

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func fakeLookup(addrs ...string) LookupFunc {
	return func(context.Context, string, string) ([]netip.Addr, error) {
		var out []netip.Addr
		for _, a := range addrs {
			out = append(out, netip.MustParseAddr(a))
		}
		return out, nil
	}
}

func noLookup(t *testing.T) LookupFunc {
	return func(context.Context, string, string) ([]netip.Addr, error) {
		t.Fatal("eine IP-Angabe darf keine Namensauflösung auslösen")
		return nil, nil
	}
}

func strs(addrs []netip.Addr) []string {
	var out []string
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

func TestTargetsIPDirektPlusIPv6AusDemServer(t *testing.T) {
	got, err := Targets(context.Background(), "192.0.2.10", []string{"192.0.2.10", "fe80::1", "2001:db8::10/64"}, noLookup(t))
	if err != nil {
		t.Fatal(err)
	}
	if s := strs(got); len(s) != 2 || s[0] != "192.0.2.10" || s[1] != "2001:db8::10" {
		t.Fatalf("Ziele %v", s)
	}
}

func TestTargetsNameMitBeidenFamilienUndGrenze(t *testing.T) {
	lookup := fakeLookup("192.0.2.1", "192.0.2.2", "192.0.2.3", "2001:db8::1", "::ffff:192.0.2.1")
	got, err := Targets(context.Background(), "nas.example", []string{"2001:db8::99"}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if s := strs(got); len(s) != 3 || s[0] != "192.0.2.1" || s[1] != "192.0.2.2" || s[2] != "2001:db8::1" {
		t.Fatalf("Ziele %v (je Familie höchstens zwei, gemappte doppelte fallen weg, eigene IPv6 geht vor)", s)
	}
}

func TestTargetsOhneScanbareAdresse(t *testing.T) {
	for _, host := range []string{"", "127.0.0.1", "::1", "0.0.0.0", "fe80::1", "224.0.0.1", "[::1]"} {
		if _, err := Targets(context.Background(), host, nil, noLookup(t)); !errors.Is(err, ErrNoTarget) {
			t.Errorf("%q: erwartet ErrNoTarget, bekam %v", host, err)
		}
	}
}

func TestTargetsNameNichtAufloesbar(t *testing.T) {
	lookup := func(context.Context, string, string) ([]netip.Addr, error) { return nil, errors.New("nxdomain") }
	if _, err := Targets(context.Background(), "weg.example", nil, lookup); err == nil || errors.Is(err, ErrNoTarget) {
		t.Fatalf("erwartet Auflösungsfehler, bekam %v", err)
	}
}
