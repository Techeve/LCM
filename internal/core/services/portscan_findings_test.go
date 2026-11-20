package services

import (
	"testing"

	"LCM/internal/core/domain"
)

// Ein verlegter SSH-Port gilt als SSH und als freigegeben; ein riskanter
// Dienst bleibt kritisch, auch wenn die Firewall ihn freigibt.
func TestRatePortSSHUndRiskanteDienste(t *testing.T) {
	server := &domain.Server{SSHPort: 2222, FirewallRules: `[{"port":6379,"proto":"tcp"},{"port":53,"proto":"udp"}]`}
	fw := firewallView(server)

	ssh := ratePort(2222, []string{"v4"}, fw)
	if ssh.Service != "ssh" || ssh.Severity != PortSeverityOK {
		t.Errorf("SSH-Port: %+v", ssh)
	}
	redis := ratePort(6379, []string{"v4"}, fw)
	if redis.Severity != PortSeverityCritical || redis.InFirewall == nil || !*redis.InFirewall {
		t.Errorf("Redis trotz Freigabe: %+v", redis)
	}
	// Eine UDP-Freigabe deckt keinen offenen TCP-Port.
	if dns := ratePort(53, []string{"v4"}, fw); dns.Severity != PortSeverityWarning {
		t.Errorf("53/tcp mit nur UDP-Freigabe: %+v", dns)
	}
	// Ohne Regelsatz-Abgleich (DSM) kein Firewall-Urteil.
	nas := firewallView(&domain.Server{OSID: domain.OSIDSynologyDSM})
	if p := ratePort(80, []string{"v4"}, nas); p.InFirewall != nil || p.Severity != PortSeverityOK {
		t.Errorf("DSM: %+v", p)
	}
}
