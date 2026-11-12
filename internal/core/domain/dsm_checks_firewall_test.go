package domain

import "testing"

// TestBefundFirewall: Der Befund nennt das Profil und ist eine Warnung wie
// eine abgeschaltete Firewall. Ohne Regeln heißt er „keine Regeln“, mit
// Regeln ohne „alles verweigern“ „verweigert nichts“.
func TestBefundFirewall(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]string
		want   string
	}{
		{"keine Regeln", map[string]string{"profile": "default", "rules": "0"}, "dsmFirewallNoRules"},
		{"ältere Prüfung ohne rules", map[string]string{"profile": "default"}, "dsmFirewallNoRules"},
		{"verweigert nichts", map[string]string{"profile": "default", "rules": "2"}, "dsmFirewallNoDeny"},
		{"ausgeschaltet", nil, "dsmFirewallOff"},
	}
	for _, c := range cases {
		checks := DSMChecks{{Key: DSMCheckFirewall, State: DSMCheckDeviation, Params: c.params}}
		befunde, _ := checks.insights()
		if len(befunde) != 1 || befunde[0].Key != c.want || befunde[0].Severity != "warning" {
			t.Fatalf("%s: %+v", c.name, befunde)
		}
		if c.params != nil && befunde[0].Params["profile"] != "default" {
			t.Errorf("%s: Profil fehlt: %+v", c.name, befunde[0].Params)
		}
	}
}

// TestDSMFirewallSpalte: Das Profil übersteht die Textspalte; ein nicht
// gelesenes wird zum Leerstring und wieder leer.
func TestDSMFirewallSpalte(t *testing.T) {
	in := DSMFirewall{Profile: "default", Sections: []DSMFirewallSection{{Name: "global", Policy: "none", Rules: []DSMFirewallRule{{Enabled: true, Policy: "drop", Ports: "all"}}}}}
	v, err := in.Value()
	if err != nil {
		t.Fatal(err)
	}
	var out DSMFirewall
	if err := out.Scan(v); err != nil || out.Profile != "default" || len(out.Sections) != 1 || out.Sections[0].Rules[0].Ports != "all" {
		t.Fatalf("zurückgelesen: %+v %v", out, err)
	}
	if v, _ := (DSMFirewall{}).Value(); v != "" {
		t.Errorf("nicht gelesen muss leer sein: %q", v)
	}
	if err := out.Scan(""); err != nil || out.Read() {
		t.Errorf("Leerstring muss nicht gelesen sein: %+v %v", out, err)
	}
}
