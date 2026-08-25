package domain

import "testing"

// Docker legt sein DNAT in nat/PREROUTING ab - also VOR der INPUT-Kette, in
// der ufw filtert. Eine an 0.0.0.0 gebundene Veröffentlichung ist deshalb von
// außen erreichbar, obwohl ufw aktiv ist und den Port nicht freigegeben hat.
// Der Parser muss genau diese Fälle von den harmlosen unterscheiden.
func TestPublishedPortsAndFirewallBypass(t *testing.T) {
	cases := []struct {
		name       string
		ports      string
		wantPorts  []string // "hostip:hostport/proto"
		wantBypass bool
	}{
		{
			name:       "an alle Adressen gebunden (umgeht ufw)",
			ports:      "0.0.0.0:3001->3001/tcp, :::3001->3001/tcp",
			wantPorts:  []string{"0.0.0.0:3001/tcp", ":::3001/tcp"}, // "::" = IPv6-Any
			wantBypass: true,
		},
		{
			name:       "nur auf Loopback (harmlos)",
			ports:      "127.0.0.1:8080->80/tcp",
			wantPorts:  []string{"127.0.0.1:8080/tcp"},
			wantBypass: false,
		},
		{
			name:       "nur exponiert, nicht veroeffentlicht (harmlos)",
			ports:      "8080/tcp, 9000/tcp",
			wantPorts:  nil,
			wantBypass: false,
		},
		{
			name:       "gemischt - eine externe Bindung genuegt",
			ports:      "127.0.0.1:8080->80/tcp, 0.0.0.0:443->443/tcp",
			wantPorts:  []string{"127.0.0.1:8080/tcp", "0.0.0.0:443/tcp"},
			wantBypass: true,
		},
		{
			name:       "an eine konkrete Host-IP gebunden (extern erreichbar)",
			ports:      "192.168.6.122:8000->8000/tcp",
			wantPorts:  []string{"192.168.6.122:8000/tcp"},
			wantBypass: true,
		},
		{name: "ohne Ports", ports: "", wantPorts: nil, wantBypass: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &DockerContainer{Ports: tc.ports}
			var got []string
			for _, p := range c.PublishedPorts() {
				got = append(got, p.HostIP+":"+p.HostPort+"/"+p.Proto)
			}
			if len(got) != len(tc.wantPorts) {
				t.Fatalf("Ports = %v, erwartet %v", got, tc.wantPorts)
			}
			for i := range got {
				if got[i] != tc.wantPorts[i] {
					t.Errorf("Port %d = %q, erwartet %q", i, got[i], tc.wantPorts[i])
				}
			}
			if c.BypassesHostFirewall() != tc.wantBypass {
				t.Errorf("BypassesHostFirewall() = %v, erwartet %v", c.BypassesHostFirewall(), tc.wantBypass)
			}
		})
	}
}
