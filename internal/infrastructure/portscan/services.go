// Package portscan prüft von LCM aus, welche TCP-Ports ein verwalteter Server
// tatsächlich anbietet. Gescannt wird eine feste Liste gängiger Dienste mit
// einem einfachen Verbindungsaufbau (TCP connect) - langsam, mit wenigen
// gleichzeitigen Verbindungen und kurzer Wartezeit je Port.
//
// Das Paket kennt keine Server und keine Datenbank: Welche Adressen gescannt
// werden dürfen, entscheidet der Aufrufer (services.PortScanService) - dort
// stammen sie ausschließlich aus dem gespeicherten Server, nie aus einer
// Anfrage.
package portscan

// Service ist ein Eintrag der festen Port-Liste.
type Service struct {
	Port int
	// Name ist die Kennung des Dienstes (zugleich Schlüssel der Hinweise in
	// der Oberfläche).
	Name string
	// Risky: Der Dienst gehört nicht ins offene Netz - Klartext-Protokolle,
	// Datenbanken, der Docker-Socket ohne TLS, rpcbind. Ist er von LCM aus
	// erreichbar, ist das ein Befund höherer Stufe.
	Risky bool
}

// Common ist die feste Liste der gescannten Ports. UDP fehlt bewusst (SNMP,
// DNS): Ein TCP-Verbindungsaufbau sagt dort nichts, und eine verlässliche
// UDP-Prüfung bräuchte Antworten je Protokoll.
var Common = []Service{
	{Port: 21, Name: "ftp"},
	{Port: 22, Name: "ssh"},
	{Port: 23, Name: "telnet", Risky: true},
	{Port: 80, Name: "http"},
	{Port: 111, Name: "rpcbind", Risky: true},
	{Port: 139, Name: "netbios"},
	{Port: 389, Name: "ldap"},
	{Port: 443, Name: "https"},
	{Port: 445, Name: "smb"},
	{Port: 636, Name: "ldaps"},
	{Port: 1883, Name: "mqtt"},
	{Port: 2049, Name: "nfs"},
	{Port: 2375, Name: "docker", Risky: true},
	{Port: 2376, Name: "docker-tls"},
	{Port: 3306, Name: "mysql", Risky: true},
	{Port: 3389, Name: "rdp"},
	{Port: 5000, Name: "dsm-http"},
	{Port: 5001, Name: "dsm-https"},
	{Port: 5432, Name: "postgresql", Risky: true},
	{Port: 5900, Name: "vnc"},
	{Port: 6379, Name: "redis", Risky: true},
	{Port: 6443, Name: "kubernetes"},
	{Port: 8006, Name: "proxmox"},
	{Port: 8080, Name: "http-alt"},
	{Port: 8291, Name: "winbox"},
	{Port: 8728, Name: "routeros-api"},
	{Port: 8729, Name: "routeros-api-tls"},
	{Port: 8883, Name: "mqtt-tls"},
	{Port: 10250, Name: "kubelet"},
	{Port: 27017, Name: "mongodb", Risky: true},
}

// Lookup liefert den Eintrag zu einem Port. Ports außerhalb der Liste (etwa
// ein verlegter SSH-Port) kommen mit leerem Namen zurück.
func Lookup(port int) Service {
	for _, s := range Common {
		if s.Port == port {
			return s
		}
	}
	return Service{Port: port}
}

// Ports liefert die Portnummern der festen Liste plus die übergebenen
// zusätzlichen Ports (z.B. der SSH-Port des Servers), ohne Doppelte und
// ohne ungültige Nummern.
func Ports(extra ...int) []int {
	seen := map[int]bool{}
	var ports []int
	add := func(p int) {
		if p < 1 || p > 65535 || seen[p] {
			return
		}
		seen[p] = true
		ports = append(ports, p)
	}
	for _, s := range Common {
		add(s.Port)
	}
	for _, p := range extra {
		add(p)
	}
	return ports
}
