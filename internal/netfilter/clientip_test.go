package netfilter

import "testing"

func TestClientIP(t *testing.T) {
	proxies, err := Parse([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	all := ProxyTrust{Enabled: true}
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
		{name: "xff genutzt, jeder peer", remote: "10.0.0.1", xff: "203.0.113.9", trust: all, want: "203.0.113.9", wantOK: true},
		{name: "xff erste adresse ist der client", remote: "10.0.0.1", xff: "203.0.113.9, 10.0.0.2", trust: all, want: "203.0.113.9", wantOK: true},
		{name: "leerer xff faellt auf peer", remote: "198.51.100.7", trust: all, want: "198.51.100.7", wantOK: true},
		{name: "ungueltiger xff faellt auf peer", remote: "198.51.100.7", xff: "kaputt", trust: all, want: "198.51.100.7", wantOK: true},
		{name: "gelisteter proxy wird geglaubt", remote: "10.0.0.1", xff: "203.0.113.9", trust: listed, want: "203.0.113.9", wantOK: true},
		{name: "fremder peer wird NICHT geglaubt", remote: "198.51.100.7", xff: "127.0.0.1", trust: listed, want: "198.51.100.7", wantOK: true},
		{name: "v4-in-v6 wird entpackt", remote: "::ffff:203.0.113.9", trust: off, want: "203.0.113.9", wantOK: true},
		{name: "v4-in-v6 peer gegen proxy-liste", remote: "::ffff:10.0.0.1", xff: "203.0.113.9", trust: listed, want: "203.0.113.9", wantOK: true},
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
