package services

import (
	"slices"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/portscan"
)

// Bewertung eines offenen Ports. Eine Stufe je Port: Ein riskanter Dienst
// bleibt kritisch, auch wenn ihn die Firewall ausdrücklich freigibt - die
// Freigabe macht ihn nicht harmloser.
const (
	PortSeverityCritical = "critical"
	PortSeverityWarning  = "warning"
	PortSeverityOK       = "ok"
)

// Gründe hinter der Stufe (Schlüssel der Texte in der Oberfläche).
const (
	PortReasonRisky       = "risky_service"
	PortReasonNotInFwRule = "not_in_firewall"
)

// PortScanPort ist ein offener Port über alle Adressen des Servers.
type PortScanPort struct {
	Port     int      `json:"port"`
	Service  string   `json:"service"`
	Families []string `json:"families"`
	// InFirewall: Der Port steht im LCM-Regelsatz (für jede Familie, auf der
	// er offen ist). nil = kein Abgleich möglich (DSM, RouterOS).
	InFirewall *bool    `json:"in_firewall"`
	Risky      bool     `json:"risky"`
	Severity   string   `json:"severity"`
	Reasons    []string `json:"reasons"`
}

// portFirewall ist die Sicht der LCM-Firewall auf einen Server.
type portFirewall struct {
	compared bool
	sshPort  int
	rules    []domain.FirewallRule
}

// firewallView liest den Regelsatz, den LCM für den Server führt. Für
// API-Geräte (DSM, RouterOS) führt LCM keinen - dort gibt es keinen Abgleich.
func firewallView(server *domain.Server) portFirewall {
	if server.IsAPIDevice() {
		return portFirewall{}
	}
	sshPort := server.SSHPort
	if sshPort == 0 {
		sshPort = 22
	}
	return portFirewall{compared: true, sshPort: sshPort, rules: firewallRulesFromServer(server)}
}

// covers meldet, ob der Regelsatz den TCP-Port für die Familie freigibt. Der
// SSH-Port ist immer freigegeben (die Regel erzwingt LCM selbst).
func (f portFirewall) covers(port int, family string) bool {
	if port == f.sshPort {
		return true
	}
	for _, r := range f.rules {
		if r.Port == port && r.Proto == "tcp" && (r.IPVersion == "any" || r.IPVersion == family) {
			return true
		}
	}
	return false
}

// portScanPorts fasst die offenen Ports aller Adressen zusammen und bewertet
// sie gegen den Regelsatz.
func portScanPorts(result domain.PortScanResult, fw portFirewall) []PortScanPort {
	families := map[int][]string{}
	var order []int
	for _, t := range result.Targets {
		for _, p := range t.Open {
			if _, ok := families[p]; !ok {
				order = append(order, p)
			}
			if !slices.Contains(families[p], t.Family) {
				families[p] = append(families[p], t.Family)
			}
		}
	}
	slices.Sort(order)
	ports := make([]PortScanPort, 0, len(order))
	for _, p := range order {
		ports = append(ports, ratePort(p, families[p], fw))
	}
	return ports
}

// ratePort bewertet einen offenen Port.
func ratePort(port int, families []string, fw portFirewall) PortScanPort {
	svc := portscan.Lookup(port)
	if port == fw.sshPort && svc.Name == "" {
		svc.Name = "ssh"
	}
	out := PortScanPort{Port: port, Service: svc.Name, Families: families, Risky: svc.Risky, Severity: PortSeverityOK, Reasons: []string{}}
	if fw.compared {
		in := true
		for _, fam := range families {
			in = in && fw.covers(port, fam)
		}
		out.InFirewall = &in
		if !in {
			out.Severity = PortSeverityWarning
			out.Reasons = append(out.Reasons, PortReasonNotInFwRule)
		}
	}
	if svc.Risky {
		out.Severity = PortSeverityCritical
		out.Reasons = append(out.Reasons, PortReasonRisky)
	}
	return out
}

// countSeverities zählt die Befunde je Stufe.
func countSeverities(ports []PortScanPort) (critical, warnings int) {
	for _, p := range ports {
		switch p.Severity {
		case PortSeverityCritical:
			critical++
		case PortSeverityWarning:
			warnings++
		}
	}
	return critical, warnings
}
