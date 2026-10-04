package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/services"
)

func lapiInput(name, url string) services.CrowdSecLapiInput {
	return services.CrowdSecLapiInput{Name: name, URL: url, Login: "lcm-managed", Password: "geheim"}
}

// TestCrowdSecLapisSeveralByName: Mehrere LAPIs stehen nebeneinander, jede
// mit eigenem Namen; die Installation bekommt den Zugang der gewählten.
func TestCrowdSecLapisSeveralByName(t *testing.T) {
	svc, _ := newSettingsService(t)
	techeve, err := svc.SaveCrowdSecLapi(lapiInput("LAPI Techeve", "http://10.0.0.1:8080/"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	s2000, err := svc.SaveCrowdSecLapi(lapiInput("LAPI Service 2000", "https://lapi.service2000.example"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if techeve.URL != "http://10.0.0.1:8080" {
		t.Errorf("url %q, erwartet ohne schrägstrich am ende", techeve.URL)
	}

	cfg, err := svc.CrowdSecConfig(s2000.ID)
	if err != nil || cfg.LapiURL != s2000.URL || cfg.LapiPassword != "geheim" {
		t.Fatalf("zugang der gewählten lapi falsch: %+v, %v", cfg, err)
	}
	// Ohne Wahl gibt es bei zwei LAPIs keine - geraten wird nicht.
	if cfg, _ := svc.CrowdSecConfig(0); cfg.LapiURL != "" {
		t.Errorf("ohne wahl wurde %q genommen", cfg.LapiURL)
	}
}

func TestCrowdSecLapiValidation(t *testing.T) {
	svc, _ := newSettingsService(t)
	if _, err := svc.SaveCrowdSecLapi(lapiInput("A", "http://10.0.0.1:8080"), "admin"); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]services.CrowdSecLapiInput{
		"name doppelt":     lapiInput("A", "http://10.0.0.2:8080"),
		"ohne schema":      lapiInput("B", "10.0.0.2:8080"),
		"quote in url":     lapiInput("B", "http://10.0.0.2:8080/'x"),
		"login mit spaces": {Name: "B", URL: "http://10.0.0.2", Login: "a b", Password: "x"},
		"ohne passwort":    {Name: "B", URL: "http://10.0.0.2", Login: "lcm"},
	} {
		if _, err := svc.SaveCrowdSecLapi(in, "admin"); !errors.Is(err, services.ErrCrowdSecLapiInvalid) {
			t.Errorf("%s: angenommen (%v)", name, err)
		}
	}
}

// TestCrowdSecLapiEditKeepsPassword: Beim Bearbeiten heißt ein leeres
// Passwort „unverändert".
func TestCrowdSecLapiEditKeepsPassword(t *testing.T) {
	svc, _ := newSettingsService(t)
	lapi, _ := svc.SaveCrowdSecLapi(lapiInput("A", "http://10.0.0.1:8080"), "admin")
	if _, err := svc.SaveCrowdSecLapi(services.CrowdSecLapiInput{ID: lapi.ID, Name: "A neu", URL: lapi.URL, Login: "anderer"}, "admin"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := svc.CrowdSecConfig(lapi.ID)
	if cfg.LapiLogin != "anderer" || cfg.LapiPassword != "geheim" {
		t.Errorf("nach dem bearbeiten: %+v", cfg)
	}
}

// TestLegacyCrowdSecLapiMigratesOnce: Die eine LAPI aus den alten
// Einstellungen wird zur „Standard-LAPI", die alten Felder werden geleert.
func TestLegacyCrowdSecLapiMigratesOnce(t *testing.T) {
	svc, repo, cipher := newSettingsServiceWithCipher(t)
	enc, _ := cipher.EncryptString("alt-passwort")
	if err := repo.UpdateFields(map[string]any{
		"crowd_sec_lapi_url": "http://10.0.0.9:8080", "crowd_sec_lapi_login": "lcm-managed", "crowd_sec_lapi_password_enc": enc,
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := svc.MigrateLegacyCrowdSecLapi(); err != nil {
			t.Fatal(err)
		}
	}
	lapis, _ := svc.ListCrowdSecLapis()
	if len(lapis) != 1 || lapis[0].Name != "Standard-LAPI" {
		t.Fatalf("übernommen: %+v", lapis)
	}
	if cfg, _ := svc.CrowdSecConfig(0); cfg.LapiPassword != "alt-passwort" {
		t.Errorf("passwort nicht übernommen: %+v", cfg)
	}
	if st, _ := repo.Get(); st.CrowdSecLapiURL != "" || st.CrowdSecLapiPasswordEnc != "" {
		t.Error("alte felder nicht geleert")
	}
}

// TestHostLapiAdoptsByURL: Die LAPI-Einrichtung auf dem LCM-Host legt
// „LCM-Host" an - beim zweiten Mal aktualisiert sie denselben Eintrag.
func TestHostLapiAdoptsByURL(t *testing.T) {
	svc, _ := newSettingsService(t)
	for _, pw := range []string{"erstes", "zweites"} {
		if err := svc.AdoptHostCrowdSecLapi("http://10.0.0.5:8080", "lcm-managed", pw); err != nil {
			t.Fatal(err)
		}
	}
	lapis, _ := svc.ListCrowdSecLapis()
	if len(lapis) != 1 || lapis[0].Name != "LCM-Host" {
		t.Fatalf("lapis %+v", lapis)
	}
	if cfg, _ := svc.CrowdSecConfig(lapis[0].ID); cfg.LapiPassword != "zweites" {
		t.Errorf("passwort %q, erwartet das neue", cfg.LapiPassword)
	}
}
