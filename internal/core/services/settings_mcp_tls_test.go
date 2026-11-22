package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/services"
)

// SetMCP schaltet nur ein oder aus; der Listener folgt über den Applier, immer
// mit TLS an den Host der Oberfläche (den kennt der Applier).
func TestSetMCPSchaltetDenListenerUmUndSpeichertNurDenSchalter(t *testing.T) {
	svc, repo := newSettingsService(t)
	applier := &fakeApplier{}
	svc.WithAddressApplier(applier)

	if err := svc.SetMCP(true, "admin"); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.Get()
	if !stored.MCPEnabled {
		t.Errorf("gespeichert: %+v", stored)
	}
	if len(applier.plans) != 1 || !applier.plans[0].MCPEnabled || applier.plans[0].MCPPort != 9330 || applier.committed != 1 {
		t.Errorf("Plan = %+v, übernommen %d", applier.plans, applier.committed)
	}

	if err := svc.SetMCP(true, "admin"); err != nil || len(applier.plans) != 1 {
		t.Errorf("unveränderter Schalter fasst die Listener an: %v, %d Pläne", err, len(applier.plans))
	}
	if err := svc.SetMCP(false, "admin"); err != nil {
		t.Fatal(err)
	}
	if len(applier.plans) != 2 || applier.plans[1].MCPEnabled {
		t.Errorf("Ausschalten: %+v", applier.plans)
	}
}

// Lässt sich der Listener nicht umschalten, lehnt SetMCP ab und speichert nichts.
func TestSetMCPMeldetEinenFehlgeschlagenenWechsel(t *testing.T) {
	svc, repo := newSettingsService(t)
	svc.WithAddressApplier(&fakeApplier{err: errors.New("adresse belegt")})

	err := svc.SetMCP(true, "admin")
	if !errors.Is(err, services.ErrPortUnavailable) {
		t.Fatalf("Fehler = %v, erwartet ErrPortUnavailable", err)
	}
	if stored, _ := repo.Get(); stored.MCPEnabled {
		t.Errorf("trotz Fehler gespeichert: %+v", stored)
	}
}

// Ein Stand von früher mit doppelten Ports blockiert das Umschalten von MCP nicht.
func TestSetMCPAlterStandMitDoppeltenPortsBlockiertNicht(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, _ := repo.Get()
	st.UIPort, st.MCPPort = 9400, 9400 // doppelt vergeben, von Hand in die DB gekommen
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMCP(true, "admin"); err != nil {
		t.Errorf("MCP einschalten: %v", err)
	}
}

// Im Modus C braucht MCP einen eigenen Namen, um sich einschalten zu lassen.
func TestSetMCPImModusCBrauchtEinenNamen(t *testing.T) {
	svc, repo := newSettingsService(t)
	mustUpdate(t, svc, services.GlobalSettingsInput{
		AddressMode: strPtr("shared_port"), PublicBaseURL: strPtr("https://lcm.example.org"),
	})
	wantSettingInvalid(t, svc.SetMCP(true, "admin"), "MCP ohne Namen im Modus C")
	if stored, _ := repo.Get(); stored.MCPEnabled {
		t.Error("MCP wurde trotz Ablehnung eingeschaltet")
	}
	mustUpdate(t, svc, services.GlobalSettingsInput{PublicMCPURL: strPtr("https://mcp.example.org")})
	if err := svc.SetMCP(true, "admin"); err != nil {
		t.Errorf("MCP mit Namen: %v", err)
	}
}
