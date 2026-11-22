package services_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// modeC stellt den Modus C mit dem Namen der Oberfläche ein.
func modeC(t *testing.T, svc *services.SettingsService, extra services.GlobalSettingsInput) {
	t.Helper()
	extra.AddressMode = strPtr("shared_port")
	extra.PublicBaseURL = strPtr("https://lcm.example.org")
	mustUpdate(t, svc, extra)
}

// enableSharedPortDirectly schaltet den Modus C an den Prüfungen vorbei ein, wie
// ein Stand von früher: ohne Namen.
func enableSharedPortDirectly(t *testing.T, repo *repositories.SettingsRepository) {
	t.Helper()
	st, err := repo.Get()
	if err != nil {
		t.Fatal(err)
	}
	st.AddressMode = domain.AddressModeSharedPort
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
}

// Im Modus C ist nur der Name der Oberfläche Pflicht: Agent und MCP dürfen
// leer bleiben.
func TestModusCNurDieOberflaecheBrauchtEinenNamen(t *testing.T) {
	svc, _ := newSettingsService(t)
	modeC(t, svc, services.GlobalSettingsInput{})

	svc2, _ := newSettingsService(t)
	_, err := svc2.UpdateGlobal(services.GlobalSettingsInput{
		AddressMode: strPtr("shared_port"), PublicAgentURL: strPtr("https://agent.example.org"),
	}, "admin")
	wantSettingInvalid(t, err, "Modus C ohne Namen der Oberfläche")
}

// Gesetzte Namen müssen im Modus C weiter verschieden sein, auch bei
// ausgeschaltetem Dienst.
func TestModusCGesetzteNamenBleibenVerschieden(t *testing.T) {
	svc, _ := newSettingsService(t)
	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{
		AddressMode: strPtr("shared_port"), PublicBaseURL: strPtr("https://lcm.example.org"),
		PublicAgentURL: strPtr("https://lcm.example.org"),
	}, "admin")
	wantSettingInvalid(t, err, "gleicher Name für Oberfläche und Agent")
}

// Einschalten ohne Namen im Modus C lehnt das Speichern ab.
func TestModusCEinschaltenOhneNamenWirdAbgelehnt(t *testing.T) {
	svc, repo := newSettingsService(t)
	modeC(t, svc, services.GlobalSettingsInput{})

	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{AgentEnabled: boolPtr(true)}, "admin")
	wantSettingInvalid(t, err, "Agent ohne Namen einschalten")
	_, err = svc.UpdateGlobal(services.GlobalSettingsInput{MCPEnabled: boolPtr(true)}, "admin")
	wantSettingInvalid(t, err, "MCP ohne Namen einschalten")
	if st, _ := repo.Get(); st.AgentEnabled || st.MCPEnabled {
		t.Errorf("abgelehnte Schalter wurden gespeichert: agent=%v mcp=%v", st.AgentEnabled, st.MCPEnabled)
	}

	// Mit Namen im selben Speichern geht es.
	mustUpdate(t, svc, services.GlobalSettingsInput{
		PublicAgentURL: strPtr("https://agent.example.org"), AgentEnabled: boolPtr(true),
		PublicMCPURL: strPtr("https://mcp.example.org"), MCPEnabled: boolPtr(true),
	})
	if st, _ := repo.Get(); !st.AgentEnabled || !st.MCPEnabled {
		t.Errorf("Dienste mit Namen: agent=%v mcp=%v", st.AgentEnabled, st.MCPEnabled)
	}
}

// Wer den Namen eines Dienstes im Modus C leer speichert, schaltet ihn dabei aus.
func TestModusCLeererNameSchaltetDenDienstAus(t *testing.T) {
	svc, repo := newSettingsService(t)
	modeC(t, svc, services.GlobalSettingsInput{
		PublicAgentURL: strPtr("https://agent.example.org"), AgentEnabled: boolPtr(true),
		PublicMCPURL: strPtr("https://mcp.example.org"), MCPEnabled: boolPtr(true),
	})

	mustUpdate(t, svc, services.GlobalSettingsInput{PublicAgentURL: strPtr("")})
	st, _ := repo.Get()
	if st.AgentEnabled || !st.MCPEnabled {
		t.Errorf("Agent-Name leer: agent=%v mcp=%v, erwartet aus, an", st.AgentEnabled, st.MCPEnabled)
	}
	mustUpdate(t, svc, services.GlobalSettingsInput{PublicMCPURL: strPtr("")})
	if st, _ := repo.Get(); st.MCPEnabled {
		t.Error("MCP-Name leer: MCP blieb an")
	}
	if urls := mustAddresses(t, svc).URLs; urls.Agent != "" || urls.MCP != "" {
		t.Errorf("wirksame Adressen ohne Namen: %+v, erwartet leer", urls)
	}
}

// Der Wechsel in den Modus C ohne Namen für Agent und MCP schaltet sie aus; in
// den Modi A und B ändert ein leerer Name nichts.
func TestWechselInModusCSchaltetDienstohneNamenAus(t *testing.T) {
	svc, repo := newSettingsService(t)
	mustUpdate(t, svc, services.GlobalSettingsInput{AgentEnabled: boolPtr(true), MCPEnabled: boolPtr(true)})
	mustUpdate(t, svc, services.GlobalSettingsInput{PublicAgentURL: strPtr("")})
	if st, _ := repo.Get(); !st.AgentEnabled || !st.MCPEnabled {
		t.Fatalf("Modus A: leerer Name darf nichts ausschalten (agent=%v mcp=%v)", st.AgentEnabled, st.MCPEnabled)
	}

	modeC(t, svc, services.GlobalSettingsInput{})
	if st, _ := repo.Get(); st.AgentEnabled || st.MCPEnabled {
		t.Errorf("Wechsel in Modus C: agent=%v mcp=%v, erwartet beide aus", st.AgentEnabled, st.MCPEnabled)
	}
}

// Das Speichern anderer Felder fasst die Schalter im Modus C nicht an, auch wenn
// ein Dienst aus früherer Zeit ohne Namen an ist.
func TestAndereFelderLassenDieSchalterInRuhe(t *testing.T) {
	svc, repo := newSettingsService(t)
	mustUpdate(t, svc, services.GlobalSettingsInput{AgentEnabled: boolPtr(true)})
	enableSharedPortDirectly(t, repo) // der gemeinsame Port kommt erst jetzt dazu

	days := 30
	mustUpdate(t, svc, services.GlobalSettingsInput{LogRetentionDays: &days})
	if st, _ := repo.Get(); !st.AgentEnabled {
		t.Error("ein fremdes Feld schaltete den Agenten aus")
	}
}

// LCM_AGENT_PORT=0 sperrt den Agenten auf aus; Einschalten lehnt
// das Speichern ab. Im Modus C gilt die Sperre nicht: Dort bedient der
// gemeinsame Port den Agenten.
func TestAgentPortNullSperrtDenAgenten(t *testing.T) {
	svc := addressService(t, services.AddressConfig{
		Scheme: "https", FallbackHost: "localhost", Agent: services.FixedPort{Set: true, Value: 0},
	})
	mustUpdate(t, svc, services.GlobalSettingsInput{AgentEnabled: boolPtr(false)})

	a := mustAddresses(t, svc)
	if !a.Locks.AgentEnabled || a.AgentEnabled || a.AgentListenPort() != 0 {
		t.Errorf("gesperrt: Sperre=%v an=%v Port=%d", a.Locks.AgentEnabled, a.AgentEnabled, a.AgentListenPort())
	}
	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{AgentEnabled: boolPtr(true)}, "admin")
	wantSettingInvalid(t, err, "Agent trotz LCM_AGENT_PORT=0 einschalten")

	modeC(t, svc, services.GlobalSettingsInput{
		PublicAgentURL: strPtr("https://agent.example.org"), AgentEnabled: boolPtr(true),
	})
	if a := mustAddresses(t, svc); a.Locks.AgentEnabled || !a.AgentEnabled {
		t.Errorf("Modus C: Sperre=%v an=%v, erwartet frei und an", a.Locks.AgentEnabled, a.AgentEnabled)
	}
}

// Die Schalter wirken über die Listener: Ein ausgeschalteter Agent hat keinen
// Port im Plan, ein eingeschalteter MCP bekommt seinen.
func TestSchalterErreichenDieListener(t *testing.T) {
	svc, _ := newSettingsService(t)
	mustUpdate(t, svc, services.GlobalSettingsInput{AgentEnabled: boolPtr(true)})
	applier := &fakeApplier{}
	svc.WithAddressApplier(applier)

	mustUpdate(t, svc, services.GlobalSettingsInput{AgentEnabled: boolPtr(false), MCPEnabled: boolPtr(true)})
	if len(applier.plans) != 1 {
		t.Fatalf("Pläne: %+v", applier.plans)
	}
	if plan := applier.plans[0]; plan.AgentEnabled || plan.AgentPort != 0 || !plan.MCPEnabled || plan.MCPPort != 9330 {
		t.Errorf("Plan = %+v", plan)
	}

	mustUpdate(t, svc, services.GlobalSettingsInput{AgentEnabled: boolPtr(true)})
	if plan := applier.plans[1]; !plan.AgentEnabled || plan.AgentPort != 9320 {
		t.Errorf("Agent wieder an: %+v", plan)
	}
}

// Nach dem Speichern von Adressen oder Schaltern läuft der Haken (ACME prüft die
// Abdeckung); ein fremdes Feld löst ihn nicht aus.
func TestSpeichernMeldetAdressAenderungen(t *testing.T) {
	svc, _ := newSettingsService(t)
	calls := 0
	svc.WithAddressesSaved(func() { calls++ })

	days := 30
	mustUpdate(t, svc, services.GlobalSettingsInput{LogRetentionDays: &days})
	if calls != 0 {
		t.Fatalf("ein fremdes Feld löste den Haken aus (%d)", calls)
	}
	mustUpdate(t, svc, services.GlobalSettingsInput{PublicBaseURL: strPtr("https://lcm.example.org")})
	mustUpdate(t, svc, services.GlobalSettingsInput{MCPEnabled: boolPtr(true)})
	if err := svc.SetMCP(false, "admin"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("Haken %d-mal gerufen, erwartet 3", calls)
	}
}
