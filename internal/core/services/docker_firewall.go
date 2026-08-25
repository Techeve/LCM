package services

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// DockerPortExposure ist ein von Docker veröffentlichter, von außen
// erreichbarer Port samt zugehörigem Container.
type DockerPortExposure struct {
	Container string `json:"container"`
	HostIP    string `json:"host_ip"`
	HostPort  string `json:"host_port"`
	Proto     string `json:"proto"`
}

// dockerPortExposures sammelt alle von außen erreichbaren Docker-Ports eines
// Servers.
//
// Hintergrund: Docker legt sein DNAT in nat/PREROUTING ab - also VOR der
// INPUT-Kette, in der ufw filtert. Veröffentlichte Container-Ports sind
// deshalb erreichbar, obwohl ufw aktiv ist und sie nicht freigegeben hat.
// Das ist Absicht von Docker und kein Fehler in LCM; falsch war allein die
// Meldung "Firewall aktiv, nur 22/443 offen", die den Zustand des Systems
// unzutreffend beschrieb (BUG-023).
//
// LCM greift bewusst NICHT ein: ob ein Port öffentlich sein soll, ist eine
// Betriebsentscheidung und je Container verschieden. Gemeldet wird er.
func dockerPortExposures(servers *repositories.ServerRepository, server *domain.Server) []DockerPortExposure {
	if !server.HasDocker {
		return nil
	}
	containers, err := servers.FindDockerContainers(server.ID)
	if err != nil {
		return nil
	}
	var out []DockerPortExposure
	for i := range containers {
		c := &containers[i]
		// Nur laufende Container binden tatsächlich Ports.
		if !strings.EqualFold(c.State, "running") {
			continue
		}
		for _, p := range c.PublishedPorts() {
			if !p.ExternallyReachable() {
				continue
			}
			out = append(out, DockerPortExposure{
				Container: c.Name, HostIP: p.HostIP, HostPort: p.HostPort, Proto: p.Proto,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostPort != out[j].HostPort {
			return out[i].HostPort < out[j].HostPort
		}
		return out[i].Container < out[j].Container
	})
	return out
}

// dockerFirewallInsight qualifiziert die Firewall-Aussage, wenn Container-Ports
// an ihr vorbei erreichbar sind.
//
// Bewusst Severity "info": Der Befund ändert die Ampelfarbe NICHT. Ein
// veröffentlichter Port ist auf einem Docker-Host der Normalfall - würde er
// den Server dauerhaft gelb färben, wäre das Signal wertlos und niemand sähe
// mehr hin. Gemeldet wird er trotzdem, denn ohne ihn behauptet LCM etwas
// Falsches über die Erreichbarkeit des Systems.
func dockerFirewallInsight(exposures []DockerPortExposure, firewallActive bool) *domain.StatusInsight {
	if !firewallActive || len(exposures) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var ports []string
	for _, e := range exposures {
		label := e.HostPort + "/" + e.Proto + " (" + e.Container + ")"
		if seen[label] {
			continue
		}
		seen[label] = true
		ports = append(ports, label)
	}
	list := strings.Join(ports, ", ")
	return &domain.StatusInsight{
		Severity: "info",
		Key:      "dockerPortsExposed",
		Params:   map[string]string{"count": strconv.Itoa(len(ports)), "ports": list},
		Message: fmt.Sprintf("Firewall aktiv, aber %d Docker-%s an ihr vorbei erreichbar: %s. "+
			"Docker veröffentlicht Ports vor der ufw-Kette - das ist so vorgesehen und wird von LCM nicht verändert.",
			len(ports), plural(len(ports), "Port ist", "Ports sind"), list),
	}
}

// plural wählt die passende Form (formatCount liegt im domain-Paket und
// bringt die Zahl selbst mit - hier steht sie schon im Satz).
func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
