package netfilter

import (
	"bytes"
	"net/netip"
	"strings"
)

// ProxyTrust legt fest, wessen X-Forwarded-For-Kopfzeile geglaubt wird.
//
// Enabled schaltet die Auswertung überhaupt ein (config.json
// trust_proxy_header). Proxies nennt die Gegenstellen, die tatsächlich der
// Reverse-Proxy sind (trusted_proxies): Nur wenn die direkte TCP-Verbindung
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
