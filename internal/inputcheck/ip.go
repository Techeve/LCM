// Package inputcheck prüft Eingaben, die an mehreren Stellen gebraucht werden:
// IP-Adressen, Netze und Hostnamen sowie die Suchmetrik für SQL in Strings. Die
// Prüfungen nennen den Grund, damit die API einen eindeutigen Fehler zurückgeben
// kann statt „ungültig“.
package inputcheck

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// Gründe, aus denen eine Adresse abgelehnt wird. Leer heißt: in Ordnung.
const (
	ReasonLetter      = "letter"       // Buchstabe in einer IPv4-Adresse
	ReasonOctet       = "octet"        // Zahl größer als 255
	ReasonLeadingZero = "leading_zero" // 010.0.0.1: oktal oder dezimal?
	ReasonFormat      = "format"       // nicht vier Zahlen mit Punkten
	ReasonIPv6        = "ipv6"         // kein gültiges IPv6
	ReasonPrefix      = "prefix"       // Präfixlänge fehlt oder liegt außerhalb
	ReasonHostBits    = "host_bits"    // 10.0.0.5/24 ist kein Netz, sondern ein Tippfehler
	ReasonHostname    = "hostname"     // kein gültiger Hostname
)

// Describe nennt den Grund für Meldungen außerhalb der API (Job-Protokoll,
// Fehler aus Services).
func Describe(reason string) string {
	switch reason {
	case ReasonLetter:
		return "eine IPv4-Adresse enthält keine Buchstaben"
	case ReasonOctet:
		return "jede Zahl einer IPv4-Adresse darf höchstens 255 sein"
	case ReasonLeadingZero:
		return "Zahlen einer IPv4-Adresse dürfen nicht mit 0 beginnen"
	case ReasonIPv6:
		return "keine gültige IPv6-Adresse"
	case ReasonPrefix:
		return "die Präfixlänge fehlt oder liegt außerhalb des Bereichs"
	case ReasonHostBits:
		return "die Adresse liegt innerhalb des Netzes, gemeint ist die Netzadresse"
	case ReasonHostname:
		return "kein gültiger Hostname"
	}
	return "eine IPv4-Adresse besteht aus vier Zahlen mit Punkten"
}

const (
	maxHostnameLen = 253
	maxLabelLen    = 63
)

var (
	hostLabel = regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_-]*[A-Za-z0-9_])?$`)
	zoneName  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)
)

// IPProblem prüft eine einzelne Adresse ohne Präfix und ohne Zone.
func IPProblem(s string) string {
	if strings.Contains(s, ":") {
		return ipv6Problem(s)
	}
	return ipv4Problem(s)
}

func ipv6Problem(s string) string {
	addr, err := netip.ParseAddr(s)
	if err != nil || !addr.Is6() || addr.Zone() != "" {
		return ReasonIPv6
	}
	return ""
}

func ipv4Problem(s string) string {
	parts := strings.Split(s, ".")
	for _, part := range parts {
		if strings.IndexFunc(part, isLetter) >= 0 {
			return ReasonLetter
		}
	}
	if len(parts) != 4 {
		return ReasonFormat
	}
	for _, part := range parts {
		if reason := octetProblem(part); reason != "" {
			return reason
		}
	}
	return ""
}

func octetProblem(part string) string {
	if part == "" || !allDigits(part) {
		return ReasonFormat
	}
	if len(part) > 1 && part[0] == '0' {
		return ReasonLeadingZero
	}
	if n, err := strconv.Atoi(part); err != nil || n > 255 {
		return ReasonOctet
	}
	return ""
}

// NetProblem prüft ein Netz in CIDR-Schreibweise. Gesetzte Host-Bits gelten als
// Fehler: 10.0.0.5/24 meint fast immer 10.0.0.5 oder 10.0.0.0/24, und was
// gemeint ist, soll nicht LCM raten.
func NetProblem(s string) string {
	address, prefix, found := strings.Cut(s, "/")
	if reason := IPProblem(address); reason != "" {
		return reason
	}
	addr := netip.MustParseAddr(address)
	if !found || !allDigits(prefix) || prefix == "" || (len(prefix) > 1 && prefix[0] == '0') {
		return ReasonPrefix
	}
	bits, err := strconv.Atoi(prefix)
	if err != nil || bits > addr.BitLen() {
		return ReasonPrefix
	}
	if masked, _ := addr.Prefix(bits); masked.Addr() != addr {
		return ReasonHostBits
	}
	return ""
}

// AddressOrNetProblem prüft eine einzelne Adresse oder ein Netz.
func AddressOrNetProblem(s string) string {
	if strings.Contains(s, "/") {
		return NetProblem(s)
	}
	return IPProblem(s)
}

// HostProblem prüft einen Hostnamen oder eine Adresse, IPv6 auch in eckigen
// Klammern und mit Zone. Was wie eine IPv4-Adresse aussieht, muss auch eine
// sein: 300.1.1.1 und 192.168.1.x wären sonst gültige Hostnamen.
func HostProblem(s string) string {
	if strings.HasPrefix(s, "[") {
		if !strings.HasSuffix(s, "]") {
			return ReasonIPv6
		}
		return scopedIPv6Problem(s[1 : len(s)-1])
	}
	if strings.Contains(s, ":") {
		return scopedIPv6Problem(s)
	}
	name := strings.TrimSuffix(s, ".")
	if looksLikeIPv4(name) {
		return ipv4Problem(name)
	}
	if name == "" || len(name) > maxHostnameLen {
		return ReasonHostname
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) > maxLabelLen || !hostLabel.MatchString(label) {
			return ReasonHostname
		}
	}
	return ""
}

func scopedIPv6Problem(s string) string {
	address, zone, found := strings.Cut(s, "%")
	if found && !zoneName.MatchString(zone) {
		return ReasonIPv6
	}
	return ipv6Problem(address)
}

// looksLikeIPv4: Der letzte Teil eines Hostnamens ist nie rein numerisch, und
// drei Zahlen am Anfang sind kein Hostname, sondern eine vertippte Adresse.
func looksLikeIPv4(name string) bool {
	parts := strings.Split(name, ".")
	if allDigits(parts[len(parts)-1]) {
		return true
	}
	return len(parts) == 4 && allDigits(parts[0]) && allDigits(parts[1]) && allDigits(parts[2])
}

func allDigits(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

func isLetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}
