// Package netfilter stellt eine schlanke IP-Allowlist bereit: Aus einer
// Liste von Konfigurationseinträgen (Schlüsselwörter oder IP/CIDR) wird ein
// Matcher gebaut, der prüft, ob eine Client-Adresse zugelassen ist.
//
// Nur Standardbibliothek (net/netip) - kein Bezug zur HTTP-Schicht, damit das
// Paket von der Config validiert und von der Middleware genutzt werden kann.
package netfilter

import (
	"fmt"
	"net/netip"
	"strings"
)

// Schlüsselwörter, die zu Präfix-Mengen expandiert werden.
const (
	keywordLocalhost = "localhost"
	keywordLoopback  = "loopback"
	keywordPrivate   = "private"
)

// loopbackPrefixes deckt die lokale Maschine ab (IPv4 + IPv6).
var loopbackPrefixes = mustPrefixes(
	"127.0.0.0/8",
	"::1/128",
)

// privatePrefixes deckt „private Netzwerke" ab: RFC1918, IPv6 Unique Local
// (ULA), Link-Local sowie die lokale Maschine (Loopback ist trivial privat).
var privatePrefixes = mustPrefixes(
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16", // IPv4 Link-Local
	"fc00::/7",       // IPv6 ULA
	"fe80::/10",      // IPv6 Link-Local
	"127.0.0.0/8",
	"::1/128",
)

// Allowlist ist eine Menge zugelassener IP-Präfixe. Die Nullwert-Allowlist
// (bzw. eine aus leerer Eingabe gebaute) lässt ALLES zu - so bleibt das
// Verhalten ohne Konfiguration unverändert (keine Einschränkung).
type Allowlist struct {
	prefixes []netip.Prefix
}

// IsEmpty meldet, ob keine Einschränkung konfiguriert ist (= alles erlaubt).
func (a Allowlist) IsEmpty() bool { return len(a.prefixes) == 0 }

// Allows prüft, ob addr zugelassen ist. Eine leere Allowlist lässt alles zu.
// IPv4-in-IPv6-gemappte Adressen werden vor dem Vergleich entpackt.
func (a Allowlist) Allows(addr netip.Addr) bool {
	if len(a.prefixes) == 0 {
		return true
	}
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap()
	for _, p := range a.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Parse baut eine Allowlist aus den Konfigurationseinträgen. Jeder Eintrag ist
// entweder ein Schlüsselwort ("localhost"/"loopback"/"private") oder eine
// IP-Adresse bzw. ein CIDR-Bereich ("192.168.1.10", "10.0.0.0/8", "::1",
// "fd00::/8"). Leere/Whitespace-Einträge werden ignoriert. Bei einem
// ungültigen Eintrag liefert Parse einen aussagekräftigen Fehler.
func Parse(entries []string) (Allowlist, error) {
	var prefixes []netip.Prefix
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		switch strings.ToLower(entry) {
		case keywordLocalhost, keywordLoopback:
			prefixes = append(prefixes, loopbackPrefixes...)
			continue
		case keywordPrivate:
			prefixes = append(prefixes, privatePrefixes...)
			continue
		}
		p, err := parsePrefix(entry)
		if err != nil {
			return Allowlist{}, fmt.Errorf("ungültiger Eintrag %q (erwartet Schlüsselwort, IP-Adresse oder CIDR): %w", entry, err)
		}
		prefixes = append(prefixes, p)
	}
	return Allowlist{prefixes: prefixes}, nil
}

// parsePrefix akzeptiert sowohl eine einzelne IP-Adresse (wird zu /32 bzw.
// /128) als auch einen CIDR-Bereich; Host-Bits werden normalisiert.
func parsePrefix(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, err
		}
		return p.Masked(), nil
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// mustPrefixes parst eine feste Liste von CIDRs (nur für Paket-Konstanten).
func mustPrefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}
