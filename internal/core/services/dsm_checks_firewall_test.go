package services

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/synology"
)

// TestFirewallVerweigertDenRest: Eine eingeschaltete Firewall ist nur in
// Ordnung, wenn das aktive Profil verweigert, was keine Regel erlaubt. Ohne
// eingeschaltete Regeln oder ohne „alles verweigern“ weicht sie ab; ein nicht
// gelesenes Profil bleibt bei „an“.
func TestFirewallVerweigertDenRest(t *testing.T) {
	allow := domain.DSMFirewallRule{Enabled: true, Policy: "allow", Protocol: "tcp", Ports: "http,https", PortGroup: "service", SourceIP: "all", SourceIPGroup: "all"}
	dropAll := domain.DSMFirewallRule{Enabled: true, Policy: "drop", Protocol: "all", Ports: "all", PortGroup: "all", SourceIP: "all", SourceIPGroup: "all"}
	dropOne := domain.DSMFirewallRule{Enabled: true, Policy: "drop", Protocol: "tcp", Ports: "22", PortGroup: "custom", SourceIP: "all", SourceIPGroup: "all"}
	allowAll := dropAll
	allowAll.Policy = "allow"
	off := dropAll
	off.Enabled = false
	global := func(policy string, rules ...domain.DSMFirewallRule) domain.DSMFirewall {
		return domain.DSMFirewall{Profile: "default", Sections: []domain.DSMFirewallSection{{Name: "global", Policy: policy, Rules: rules}}}
	}
	eth0Only := domain.DSMFirewall{Profile: "default", Sections: []domain.DSMFirewallSection{
		{Name: "global", Policy: "none", Rules: []domain.DSMFirewallRule{allow}},
		{Name: "eth0", Policy: "none", Rules: []domain.DSMFirewallRule{dropAll}},
	}}
	adaptersOnly := domain.DSMFirewall{Profile: "default", Sections: []domain.DSMFirewallSection{
		{Name: "eth0", Policy: "deny"},
		{Name: "wlan0", Policy: "none", Rules: []domain.DSMFirewallRule{dropAll}},
	}}
	cases := []struct {
		name    string
		profile domain.DSMFirewall
		want    string
		value   string
		rules   string
	}{
		{"leer", global("none"), domain.DSMCheckDeviation, "an (default, keine Regeln)", "0"},
		{"nur erlauben", global("none", allow, allow), domain.DSMCheckDeviation, "an (default, 2 Regeln)", "2"},
		{"alles verwerfen am Ende", global("none", allow, allow, dropAll), domain.DSMCheckOK, "an (default, 3 Regeln)", ""},
		{"Vorgabe verweigern", global("deny", allow), domain.DSMCheckOK, "an (default, 1 Regel)", ""},
		{"Vorgabe verweigern ohne Regeln", global("drop"), domain.DSMCheckOK, "an (default, keine Regeln)", ""},
		{"alles erlauben vor alles verwerfen", global("none", allowAll, dropAll), domain.DSMCheckDeviation, "an (default, 2 Regeln)", "2"},
		{"alles erlauben trotz Vorgabe verweigern", global("deny", allowAll), domain.DSMCheckDeviation, "an (default, 1 Regel)", "1"},
		{"einzelner Port verworfen", global("none", allow, dropOne), domain.DSMCheckDeviation, "an (default, 2 Regeln)", "2"},
		{"nur abgeschaltete Regeln", global("none", off), domain.DSMCheckDeviation, "an (default, keine Regeln)", "0"},
		{"nur an einer Schnittstelle", eth0Only, domain.DSMCheckDeviation, "an (default, 2 Regeln)", "2"},
		{"ohne global, alle verweigern", adaptersOnly, domain.DSMCheckOK, "an (default, 1 Regel)", ""},
		{"nicht gelesen", domain.DSMFirewall{}, domain.DSMCheckOK, "an (default)", ""},
	}
	on := synology.Setting{Known: true, On: true, Text: "default"}
	for _, c := range cases {
		got := firewallCheck(synology.Settings{Firewall: on, FirewallProfile: c.profile})
		if got.State != c.want || got.Value != c.value || got.Params["rules"] != c.rules {
			t.Errorf("%s: %s %q %v, erwartet %s %q rules=%q", c.name, got.State, got.Value, got.Params, c.want, c.value, c.rules)
		}
	}
}
