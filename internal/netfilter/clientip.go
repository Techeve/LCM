package netfilter

import (
	"net/netip"
	"strings"
)

// ProxyTrust legt fest, wessen X-Forwarded-For-Kopfzeile geglaubt wird.
//
// Enabled schaltet die Auswertung überhaupt ein (config.json
// trust_proxy_header). Proxies grenzt sie auf die Gegenstellen ein, die
// tatsächlich der Reverse-Proxy sind (trusted_proxies): Nur wenn die direkte
// TCP-Verbindung von einer dieser Adressen kommt, zählt die Kopfzeile. Eine
// leere Liste heißt „jeder Peer" - das bisherige Verhalten, und genau der
// Grund für die Liste: Erreicht ein Client den LCM-Port am Proxy vorbei,
// kann er sich sonst mit einer gefälschten Kopfzeile eine beliebige Adresse
// geben - in die IP-Allowlist hinein und aus der Anmeldesperre heraus.
type ProxyTrust struct {
	Enabled bool
	Proxies Allowlist
}

// Trusts meldet, ob die Kopfzeile eines Peers geglaubt wird.
func (t ProxyTrust) Trusts(peer netip.Addr) bool {
	return t.Enabled && t.Proxies.Allows(peer)
}

// ClientIP ermittelt die maßgebliche Client-Adresse aus der Peer-Adresse der
// TCP-Verbindung (remoteIP) und dem X-Forwarded-For-Header (xff). Wird der
// Peer als Proxy vertraut, gilt die ERSTE Adresse des Headers - konventionell
// der ursprüngliche Client; fehlt der Header oder ist er unbrauchbar, bleibt
// es bei der Peer-Adresse. ok=false: nicht einmal die Peer-Adresse ist eine
// IP-Adresse.
func ClientIP(remoteIP, xff string, trust ProxyTrust) (netip.Addr, bool) {
	peer, err := netip.ParseAddr(strings.TrimSpace(remoteIP))
	if err != nil {
		return netip.Addr{}, false
	}
	peer = peer.Unmap()
	if xff == "" || !trust.Trusts(peer) {
		return peer, true
	}
	first := xff
	if i := strings.IndexByte(xff, ','); i >= 0 {
		first = xff[:i]
	}
	if addr, err := netip.ParseAddr(strings.TrimSpace(first)); err == nil {
		return addr.Unmap(), true
	}
	return peer, true
}
