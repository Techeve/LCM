package netfilter

import (
	"strings"
	"testing"
)

func TestClientIP(t *testing.T) {
	proxies, err := Parse([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	empty := ProxyTrust{Enabled: true}
	listed := ProxyTrust{Enabled: true, Proxies: proxies}
	off := ProxyTrust{}

	tests := []struct {
		name   string
		remote string
		xff    string
		trust  ProxyTrust
		want   string
		wantOK bool
	}{
		{name: "peer ohne proxy", remote: "203.0.113.9", trust: off, want: "203.0.113.9", wantOK: true},
		{name: "xff ignoriert ohne trust", remote: "203.0.113.9", xff: "10.0.0.1", trust: off, want: "203.0.113.9", wantOK: true},
		{name: "leere proxy-liste vertraut niemandem", remote: "10.0.0.1", xff: "203.0.113.9", trust: empty, want: "10.0.0.1", wantOK: true},
		{name: "leerer xff faellt auf peer", remote: "10.0.0.1", trust: listed, want: "10.0.0.1", wantOK: true},
		{name: "ungueltiger xff faellt auf peer", remote: "10.0.0.1", xff: "kaputt", trust: listed, want: "10.0.0.1", wantOK: true},
		{name: "gelisteter proxy wird geglaubt", remote: "10.0.0.1", xff: "203.0.113.9", trust: listed, want: "203.0.113.9", wantOK: true},
		{name: "fremder peer wird NICHT geglaubt", remote: "198.51.100.7", xff: "127.0.0.1", trust: listed, want: "198.51.100.7", wantOK: true},
		{name: "gefaelschter linker eintrag zaehlt nicht", remote: "10.0.0.1", xff: "127.0.0.1, 203.0.113.9", trust: listed, want: "203.0.113.9", wantOK: true},
		{name: "proxy-kette wird uebersprungen", remote: "10.0.0.1", xff: "198.51.100.1, 203.0.113.9, 10.0.0.3, 10.0.0.2", trust: listed, want: "203.0.113.9", wantOK: true},
		{name: "nur proxys: der linkeste gilt", remote: "10.0.0.1", xff: "10.0.0.4, 10.0.0.3", trust: listed, want: "10.0.0.4", wantOK: true},
		{name: "unlesbar rechts: peer", remote: "10.0.0.1", xff: "203.0.113.9, kaputt", trust: listed, want: "10.0.0.1", wantOK: true},
		{name: "unlesbar in der mitte: letzte gute adresse", remote: "10.0.0.1", xff: "203.0.113.9, kaputt, 10.0.0.3", trust: listed, want: "10.0.0.3", wantOK: true},
		{name: "leerer eintrag beendet die kette", remote: "10.0.0.1", xff: "203.0.113.9,,10.0.0.3", trust: listed, want: "10.0.0.3", wantOK: true},
		{name: "v4-in-v6 wird entpackt", remote: "::ffff:203.0.113.9", trust: off, want: "203.0.113.9", wantOK: true},
		{name: "v4-in-v6 peer gegen proxy-liste", remote: "::ffff:10.0.0.1", xff: "::ffff:203.0.113.9", trust: listed, want: "203.0.113.9", wantOK: true},
		{name: "ungueltige peer-ip", remote: "nicht-ip", trust: off, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, ok := ClientIP(tt.remote, tt.xff, tt.trust)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v, erwartet %v", ok, tt.wantOK)
			}
			if ok && addr.String() != tt.want {
				t.Errorf("ip=%s, erwartet %s", addr.String(), tt.want)
			}
		})
	}
}

// TestJoinForwardedFor: Mehrere Kopfzeilen ergeben eine Liste in ihrer
// Reihenfolge - die zuletzt angehängte Zeile des Proxys steht rechts.
func TestJoinForwardedFor(t *testing.T) {
	got := JoinForwardedFor([][]byte{[]byte("127.0.0.1"), []byte("203.0.113.9")})
	if got != "127.0.0.1,203.0.113.9" {
		t.Errorf("JoinForwardedFor = %q", got)
	}
	if JoinForwardedFor(nil) != "" {
		t.Error("ohne Kopfzeile erwartet leer")
	}
}

// Der Halter liefert „niemandem vertrauen“, solange er leer ist oder fehlt, und
// die gespeicherte Regel nach jedem Austausch.
func TestProxyTrustHolder(t *testing.T) {
	var nilHolder *ProxyTrustHolder
	if nilHolder.Load().Enabled {
		t.Error("nil-Halter vertraut")
	}
	if (&ProxyTrustHolder{}).Load().Enabled {
		t.Error("leerer Halter vertraut")
	}

	proxies, err := Parse([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	holder := NewProxyTrustHolder(ProxyTrust{Enabled: true, Proxies: proxies})
	if got, _ := ClientIP("10.0.0.1", "203.0.113.9", holder.Load()); got.String() != "203.0.113.9" {
		t.Errorf("vertrauter Peer: %s", got)
	}
	holder.Store(ProxyTrust{})
	if got, _ := ClientIP("10.0.0.1", "203.0.113.9", holder.Load()); got.String() != "10.0.0.1" {
		t.Errorf("nach dem Austausch zählt die Kopfzeile noch: %s", got)
	}
}

// trusted_proxies: Normalisieren wie bei den Netzbereichen, aber ohne einen
// Bereich über alle Adressen - in jeder Schreibweise.
func TestNormalizeProxies(t *testing.T) {
	got, err := NormalizeProxies([]string{" 10.0.0.5 ", "192.168.0.0/16", "Private", "10.0.0.5", "::1", "::ffff:10.1.0.0/112"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "10.0.0.5/32,192.168.0.0/16,private,::1/128,10.1.0.0/16"; strings.Join(got, ",") != want {
		t.Errorf("Liste = %v, erwartet %s", got, want)
	}
	for _, bad := range []string{
		"0.0.0.0/0", "::/0", "::ffff:0.0.0.0/96", "::ffff:0:0/96", "0.0.0.0/00", "1.2.3.4/0", "nicht-ip", "10.0.0.0/33",
	} {
		if _, err := NormalizeProxies([]string{"10.0.0.5", bad}); err == nil {
			t.Errorf("%q wurde akzeptiert", bad)
		}
	}
	if got, err := NormalizeProxies(nil); err != nil || len(got) != 0 {
		t.Errorf("leere Liste: %v, %v", got, err)
	}
}
