package services

import (
	"strconv"
	"strings"
	"testing"
)

// Auf Systemen ohne iproute2 (schlanke Images, ältere Installationen) gibt es
// kein `ss` - dort blieb das Port-Inventar leer, und die Firewall-Seite hatte
// nichts vorzuschlagen. netstat deckt genau diese Systeme ab; sein Format
// unterscheidet sich in der Spaltenlage und beim Prozessnamen.
func TestParseListeningPortsNetstat(t *testing.T) {
	out := `Active Internet connections (only servers)
Proto Recv-Q Send-Q Local Address           Foreign Address         State       PID/Program name
tcp        0      0 0.0.0.0:22              0.0.0.0:*               LISTEN      713/sshd
tcp        0      0 127.0.0.1:3306          0.0.0.0:*               LISTEN      901/mariadbd
tcp6       0      0 :::80                   :::*                    LISTEN      1204/nginx
udp        0      0 0.0.0.0:53              0.0.0.0:*                           640/dnsmasq
`
	got := parseListeningPorts(out)
	want := map[string]string{ // port/proto → Prozess
		"22/tcp": "sshd",
		"80/tcp": "nginx",
		"53/udp": "dnsmasq",
	}
	if len(got) != len(want) {
		t.Fatalf("erwartet %d Einträge (Loopback fliegt raus), bekam %d: %+v", len(want), len(got), got)
	}
	for _, p := range got {
		key := strconv.Itoa(p.Port) + "/" + p.Proto
		proc, ok := want[key]
		if !ok {
			t.Errorf("unerwarteter Eintrag %s", key)
			continue
		}
		if p.Process != proc {
			t.Errorf("%s: Prozess = %q, erwartet %q", key, p.Process, proc)
		}
	}
	// 80/tcp kam aus einer tcp6-Zeile mit :: - das ist IPv6, nicht IPv4.
	for _, p := range got {
		if p.Port == 80 && p.IPVersion != "v6" {
			t.Errorf("tcp6-Zeile als %s gelesen", p.IPVersion)
		}
	}
}

// Der Scan muss den Rückfall überhaupt anbieten - sonst bleibt das Inventar
// auf Systemen ohne ss leer.
func TestScanListeningPortsFallsBackToNetstat(t *testing.T) {
	var seen string
	scanListeningPorts("lcm-svc", true, func(_, cmd string) string { seen = cmd; return "" })
	for _, want := range []string{"ss -tulnpH", "netstat -tulnp"} {
		if !strings.Contains(seen, want) {
			t.Errorf("Scan-Kommando enthält %q nicht: %s", want, seen)
		}
	}
}
