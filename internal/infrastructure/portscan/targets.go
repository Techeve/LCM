package portscan

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// ErrNoTarget: Für den Server gibt es keine Adresse, die sich von LCM aus
// sinnvoll scannen lässt (Agent ohne Adresse, LCM-Host über Loopback, nur
// link-lokale Adressen).
var ErrNoTarget = errors.New("keine von LCM aus scanbare Adresse")

// maxPerFamily begrenzt die Adressen je Familie: Ein Name mit vielen
// A-Einträgen (Lastverteiler) soll keinen Scan über ein halbes Netz auslösen.
const maxPerFamily = 2

// LookupFunc löst einen Namen in Adressen auf (net.Resolver.LookupNetIP).
type LookupFunc func(ctx context.Context, network, host string) ([]netip.Addr, error)

// DefaultLookup ist der Resolver des Systems.
func DefaultLookup(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, network, host)
}

// Targets bestimmt die Adressen eines Servers: die Host-Angabe (IP oder per
// DNS aufgelöster Name, IPv4 und IPv6). Liefert der Host keine IPv6-Adresse,
// kommt die erste geeignete IPv6-Adresse aus den vom Server gemeldeten
// Adressen dazu (known) - so wird ein Server, der per IPv4 eingetragen ist,
// auch über IPv6 geprüft. Loopback, link-lokale, Multicast- und
// unspezifizierte Adressen fallen heraus.
func Targets(ctx context.Context, host string, known []string, lookup LookupFunc) ([]netip.Addr, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, ErrNoTarget
	}
	addrs, err := hostAddrs(ctx, host, lookup)
	if err != nil {
		return nil, err
	}
	targets := limitFamilies(scannable(addrs))
	if !hasV6(targets) {
		if v6, ok := firstV6(known); ok {
			targets = append(targets, v6)
		}
	}
	if len(targets) == 0 {
		return nil, ErrNoTarget
	}
	return targets, nil
}

// hostAddrs liest eine IP direkt, einen Namen über den Resolver.
func hostAddrs(ctx context.Context, host string, lookup LookupFunc) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return []netip.Addr{addr}, nil
	}
	addrs, err := lookup(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("name %s nicht auflösbar: %w", host, err)
	}
	return addrs, nil
}

// Scannable meldet, ob eine Adresse als Ziel taugt.
func Scannable(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() && !addr.IsLoopback() && !addr.IsUnspecified() &&
		!addr.IsMulticast() && !addr.IsLinkLocalUnicast() && !addr.IsLinkLocalMulticast()
}

// scannable filtert ungeeignete und doppelte Adressen heraus.
func scannable(addrs []netip.Addr) []netip.Addr {
	seen := map[netip.Addr]bool{}
	var out []netip.Addr
	for _, a := range addrs {
		a = a.Unmap().WithZone("")
		if !Scannable(a) || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// limitFamilies behält je Familie höchstens maxPerFamily Adressen.
func limitFamilies(addrs []netip.Addr) []netip.Addr {
	var v4, v6 int
	var out []netip.Addr
	for _, a := range addrs {
		count := &v6
		if a.Is4() {
			count = &v4
		}
		if *count >= maxPerFamily {
			continue
		}
		*count++
		out = append(out, a)
	}
	return out
}

func hasV6(addrs []netip.Addr) bool {
	for _, a := range addrs {
		if a.Is6() {
			return true
		}
	}
	return false
}

// firstV6 sucht in den gemeldeten Adressen (auch in CIDR-Form) die erste
// geeignete IPv6-Adresse.
func firstV6(known []string) (netip.Addr, bool) {
	for _, raw := range known {
		raw = strings.TrimSpace(raw)
		if prefix, err := netip.ParsePrefix(raw); err == nil {
			raw = prefix.Addr().String()
		}
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			continue
		}
		addr = addr.WithZone("")
		if addr.Is6() && !addr.Is4In6() && Scannable(addr) {
			return addr, true
		}
	}
	return netip.Addr{}, false
}
