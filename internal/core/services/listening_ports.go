package services

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Inventar lauschender Sockets (ss -tulnp) - die Vorschläge im
// Firewall-Dialog: „dieser Dienst lauscht auf Port X - freigeben?".

// listeningPortsCap begrenzt das Inventar (Schutz vor absurden Systemen mit
// tausenden Sockets; für Vorschläge reicht die Spitze).
const listeningPortsCap = 100

// scanListeningPorts erfasst lauschende TCP/UDP-Sockets als JSON-Inventar.
// Mit sudo sind auch die Prozessnamen fremder Benutzer sichtbar; im
// eingeschränkten Modus läuft ss ohne Rechte (Ports/Bind-Adressen sind
// trotzdem vollständig, Prozessnamen ggf. leer - für Vorschläge genug).
// Auf Systemen ohne ss bleibt das Inventar leer (best effort).
func scanListeningPorts(loginUser string, restricted bool, run func(label, cmd string) string) string {
	cmd := listeningPortsCmd
	if !restricted {
		cmd = wrapSudo(loginUser, false, cmd)
	}
	return listeningPortsJSON(parseListeningPorts(run("listening-ports", cmd)))
}

// listeningPortsCmd fragt die lauschenden Sockets ab. `ss` (iproute2) ist der
// Normalfall; fehlt es - schlanke Images, Alpine mit busybox, ältere
// Installationen -, sprang die Abfrage bisher ohne Ergebnis heraus und die
// Firewall-Seite hatte schlicht keine Vorschläge anzubieten. `netstat` als
// Rückfall deckt genau diese Systeme ab; das letzte `true` hält den Scan
// fehlerfrei, wenn keines von beiden da ist.
const listeningPortsCmd = "ss -tulnpH 2>/dev/null || ss -tulnH 2>/dev/null || " +
	"netstat -tulnp 2>/dev/null || netstat -tuln 2>/dev/null || true"

// reListeningProcess zieht den ersten Prozessnamen aus der users:-Spalte,
// z.B. users:(("sshd",pid=713,fd=3)).
var reListeningProcess = regexp.MustCompile(`users:\(\("([^"]+)"`)

// reNetstatProcess zieht den Prozessnamen aus der netstat-Spalte
// „PID/Program name", z.B. `713/sshd`.
var reNetstatProcess = regexp.MustCompile(`\s\d+/([^\s/]+)`)

// parseListeningPorts parst `ss -tulnpH`-Zeilen wie
//
//	tcp  LISTEN 0 4096       0.0.0.0:22      0.0.0.0:* users:(("sshd",pid=1,fd=3))
//	udp  UNCONN 0 0    [::ffff:0.0.0.0]:8080       *:*
//	tcp  LISTEN 0 511            [::]:80         [::]:* users:(("nginx",pid=2,fd=8))
//
// Loopback-Sockets fliegen raus (für die Firewall irrelevant), v4-gemappte
// v6-Adressen ([::ffff:…]) zählen als IPv4, Interface-Scopes (%eth0) werden
// abgeschnitten. Dedupliziert und stabil sortiert.
func parseListeningPorts(out string) []domain.ListeningPort {
	seen := map[string]bool{}
	var ports []domain.ListeningPort
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		// netstat schreibt die Adressfamilie an das Protokoll (tcp6/udp6);
		// für uns zählt nur tcp/udp - die Familie steht in der Adresse.
		proto := strings.ToLower(strings.TrimSuffix(fields[0], "6"))
		if proto != "tcp" && proto != "udp" {
			continue
		}
		// Spaltenlage unterscheidet die beiden Werkzeuge: ss hat an Stelle 1
		// den Zustand (LISTEN/UNCONN), netstat die Recv-Q (eine Zahl). Die
		// lokale Adresse steht entsprechend an Stelle 4 bzw. 3.
		idxLocal := 4
		if _, err := strconv.Atoi(fields[1]); err == nil {
			idxLocal = 3
		}
		if len(fields) <= idxLocal {
			continue
		}
		local := fields[idxLocal]
		idx := strings.LastIndex(local, ":")
		if idx < 0 {
			continue
		}
		port, err := strconv.Atoi(local[idx+1:])
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		addr := strings.Trim(local[:idx], "[]")
		if i := strings.IndexByte(addr, '%'); i >= 0 { // fe80::1%eth0
			addr = addr[:i]
		}
		// v4-gemappte v6-Wildcard/-Adresse → als IPv4 behandeln.
		if rest := strings.TrimPrefix(addr, "::ffff:"); rest != addr && strings.Count(rest, ".") == 3 {
			addr = rest
		}
		if addr == "*" || addr == "" {
			addr = "0.0.0.0"
		}
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() {
			continue
		}
		ipv := "v4"
		if ip.To4() == nil {
			ipv = "v6"
		}
		p := domain.ListeningPort{Port: port, Proto: proto, Bind: ip.String(), IPVersion: ipv}
		if m := reListeningProcess.FindStringSubmatch(line); m != nil {
			p.Process = m[1]
		} else if m := reNetstatProcess.FindStringSubmatch(line); m != nil {
			p.Process = m[1]
		}
		key := strconv.Itoa(p.Port) + "/" + p.Proto + "/" + p.IPVersion + "/" + p.Bind
		if seen[key] {
			continue
		}
		seen[key] = true
		ports = append(ports, p)
	}
	sort.SliceStable(ports, func(i, j int) bool {
		a, b := ports[i], ports[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}
		if a.IPVersion != b.IPVersion {
			return a.IPVersion < b.IPVersion
		}
		return a.Bind < b.Bind
	})
	if len(ports) > listeningPortsCap {
		ports = ports[:listeningPortsCap]
	}
	return ports
}

// reListeningPID zieht die PID aus der users:-Spalte einer ss-Zeile.
var reListeningPID = regexp.MustCompile(`pid=([0-9]+)`)

// nonLoopbackListenerPIDs liefert die PIDs aller Prozesse, die auf einer
// NICHT-Loopback-Adresse lauschen - dedupliziert, numerisch validiert (die
// Werte landen in einem Shell-Skript). Die Adress-Auswertung nutzt dieselbe
// Normalisierung wie parseListeningPorts (v4-gemappte v6-Adressen, Scopes) -
// ein zeilenweises grep auf dem Zielsystem übersah solche Formen und machte
// den Umfang von listening_packages je Server verschieden (R2-084).
func nonLoopbackListenerPIDs(ssOut string) []string {
	seen := map[string]bool{}
	var pids []string
	for _, line := range strings.Split(ssOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		proto := strings.ToLower(fields[0])
		if proto != "tcp" && proto != "udp" {
			continue
		}
		local := fields[4]
		idx := strings.LastIndex(local, ":")
		if idx < 0 {
			continue
		}
		addr := strings.Trim(local[:idx], "[]")
		if i := strings.IndexByte(addr, '%'); i >= 0 {
			addr = addr[:i]
		}
		if rest := strings.TrimPrefix(addr, "::ffff:"); rest != addr && strings.Count(rest, ".") == 3 {
			addr = rest
		}
		if addr == "*" || addr == "" {
			addr = "0.0.0.0"
		}
		ip := net.ParseIP(addr)
		if ip == nil || ip.IsLoopback() {
			continue
		}
		for _, m := range reListeningPID.FindAllStringSubmatch(line, -1) {
			if pid := m[1]; !seen[pid] {
				seen[pid] = true
				pids = append(pids, pid)
			}
		}
	}
	sort.Strings(pids)
	return pids
}

// listeningPortsJSON serialisiert das Inventar (leer = "[]").
func listeningPortsJSON(ports []domain.ListeningPort) string {
	if len(ports) == 0 {
		return "[]"
	}
	b, err := json.Marshal(ports)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ScanListeningPortsNow liest das Port-Inventar SOFORT vom Server und legt es
// als neuen Stand ab. Ohne diesen Weg konnte der Firewall-Dialog nur zeigen,
// was der letzte Server-Scan zufällig erfasst hatte: Wer einen Dienst neu
// installiert hatte, sah dessen Port dort schlicht nicht - und bekam auch
// keinen Hinweis, woran es liegt. Der Aufruf ist lesend auf dem Zielsystem
// (ss/netstat), verändert dort also nichts.
func (s *ServerService) ScanListeningPortsNow(scope repositories.AccessScope, id uint, actor string) ([]domain.ListeningPort, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureSSHTransport(server); err != nil {
		return nil, err
	}
	if server.IsDemo {
		// Demo-Server werden nie kontaktiert - der hinterlegte Stand ist alles,
		// was es gibt.
		return parseListeningPortsJSON(server.ListeningPorts), nil
	}
	conn, err := s.connectRec(server, "listening-ports", actor)
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	raw := scanListeningPorts(server.ServiceUser, server.RestrictedSudo, func(_, cmd string) string {
		out, _, runErr := conn.Run(privRun(server, cmd))
		if runErr != nil {
			return ""
		}
		return out
	})
	if err := s.servers.UpdateFields(id, map[string]any{"listening_ports": raw}); err != nil {
		return nil, err
	}
	return parseListeningPortsJSON(raw), nil
}

// parseListeningPortsJSON liest das gespeicherte Inventar zurück (leer bei
// unlesbarem Stand - Vorschläge sind Komfort, kein Grund für einen Fehler).
func parseListeningPortsJSON(raw string) []domain.ListeningPort {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var ports []domain.ListeningPort
	if err := json.Unmarshal([]byte(raw), &ports); err != nil {
		return nil
	}
	return ports
}
