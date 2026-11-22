package netfilter

import (
	"bytes"
	"fmt"
	"net/netip"
	"strings"
	"sync/atomic"
)

// ProxyTrust legt fest, wessen X-Forwarded-For-Kopfzeile geglaubt wird.
//
// Enabled schaltet die Auswertung überhaupt ein (Einstellung
// trust_proxy_header). Proxies nennt die Gegenstellen, die tatsächlich der
// Reverse-Proxy sind (Einstellung trusted_proxies): Nur wenn die direkte TCP-Verbindung
// von einer dieser Adressen kommt, zählt die Kopfzeile - und auch dann nur
// bis zum ersten Eintrag von rechts, der selbst kein vertrauter Proxy ist
// (siehe ClientIP).
//
// Eine leere Liste heißt „niemandem vertrauen": Die Kopfzeile wird nie
// ausgewertet, LCM warnt beim Start. Früher hieß leer „jeder Peer" - wer den
// LCM-Port am Proxy vorbei erreichte, gab sich mit einer gefälschten
// Kopfzeile jede beliebige Adresse: in die IP-Allowlist hinein und aus der
// Anmeldesperre heraus.
type ProxyTrust struct {
	Enabled bool
	Proxies Allowlist
}

// Trusts meldet, ob die Kopfzeile eines Peers geglaubt wird. Ohne Liste
// niemals - anders als bei der Allowlist heißt leer hier nicht „alle".
func (t ProxyTrust) Trusts(peer netip.Addr) bool {
	return t.Enabled && !t.Proxies.IsEmpty() && t.Proxies.Allows(peer)
}

// ClientIP ermittelt die maßgebliche Client-Adresse aus der Peer-Adresse der
// TCP-Verbindung (remoteIP) und dem X-Forwarded-For-Header (xff). Nur wenn
// der Peer ein vertrauter Proxy ist, zählt der Header (siehe forwardedClient);
// fehlt er oder ist er unbrauchbar, bleibt es bei der Peer-Adresse. ok=false:
// nicht einmal die Peer-Adresse ist eine IP-Adresse.
func ClientIP(remoteIP, xff string, trust ProxyTrust) (netip.Addr, bool) {
	peer, err := netip.ParseAddr(strings.TrimSpace(remoteIP))
	if err != nil {
		return netip.Addr{}, false
	}
	peer = peer.Unmap()
	if xff == "" || !trust.Trusts(peer) {
		return peer, true
	}
	return forwardedClient(peer, xff, trust.Proxies), true
}

// forwardedClient geht X-Forwarded-For von rechts nach links durch. Jeder
// Proxy hängt die Adresse seines Gegenübers rechts an; was links davon steht,
// kann der Client selbst mitgeschickt haben. Maßgeblich ist deshalb der
// rechteste Eintrag, der kein vertrauter Proxy ist. Sind alle Einträge
// vertraute Proxys, gilt der linkeste. An einem unlesbaren Eintrag endet die
// Kette: Es bleibt bei der letzten gültigen Adresse, zuerst also beim Peer.
func forwardedClient(peer netip.Addr, xff string, proxies Allowlist) netip.Addr {
	client := peer
	rest := xff
	for rest != "" {
		var entry string
		if i := strings.LastIndexByte(rest, ','); i >= 0 {
			rest, entry = rest[:i], rest[i+1:]
		} else {
			rest, entry = "", rest
		}
		addr, err := netip.ParseAddr(strings.TrimSpace(entry))
		if err != nil {
			return client
		}
		client = addr.Unmap()
		if !proxies.Allows(client) {
			return client
		}
	}
	return client
}

// JoinForwardedFor fügt mehrere X-Forwarded-For-Zeilen einer Anfrage in
// ihrer Reihenfolge zu einer Liste zusammen (RFC 9110, 5.3). Ein Proxy wie
// HAProxy hängt seine Angabe als EIGENE Zeile an; wer nur die erste Zeile
// liest, sähe dann ausgerechnet die des Clients.
func JoinForwardedFor(values [][]byte) string {
	return string(bytes.Join(values, []byte(",")))
}

// ProxyTrustHolder hält die Vertrauensregel austauschbar zur Laufzeit. Jede
// Anfrage liest sie neu; ein Austausch wirkt sofort und ohne Neustart. Der
// Nullwert und nil vertrauen niemandem (nil für Apps ohne Proxy, etwa Tests).
type ProxyTrustHolder struct {
	trust atomic.Pointer[ProxyTrust]
}

// NewProxyTrustHolder legt einen Halter mit der Regel trust an.
func NewProxyTrustHolder(trust ProxyTrust) *ProxyTrustHolder {
	h := &ProxyTrustHolder{}
	h.Store(trust)
	return h
}

// Load liefert die aktuelle Regel; ohne Halter oder Regel gilt „niemandem
// vertrauen".
func (h *ProxyTrustHolder) Load() ProxyTrust {
	if h == nil {
		return ProxyTrust{}
	}
	if trust := h.trust.Load(); trust != nil {
		return *trust
	}
	return ProxyTrust{}
}

// Store ersetzt die Regel.
func (h *ProxyTrustHolder) Store(trust ProxyTrust) { h.trust.Store(&trust) }

// NormalizeProxies prüft die Einträge von trusted_proxies und liefert sie in
// einheitlicher Schreibweise (siehe Normalize). Zusätzlich abgelehnt wird jeder
// Bereich, der alle Adressen umfasst (0.0.0.0/0, ::/0, auch als
// IPv4-in-IPv6 geschrieben): Damit glaubte LCM jedem Peer die Kopfzeile, und
// jeder Client im Netz gäbe sich mit einem gefälschten X-Forwarded-For jede
// Adresse - in die IP-Allowlist hinein und aus der Anmeldesperre heraus.
func NormalizeProxies(entries []string) ([]string, error) {
	normalized, err := Normalize(entries)
	if err != nil {
		return nil, err
	}
	for _, entry := range normalized {
		if isKeyword(entry) {
			continue
		}
		if prefix, err := parsePrefix(entry); err == nil && prefix.Bits() == 0 {
			return nil, fmt.Errorf("eintrag %q umfasst alle Adressen - ein Proxy muss eine bestimmte Adresse oder ein bestimmtes Netz sein", entry)
		}
	}
	return normalized, nil
}
