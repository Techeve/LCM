package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/services"
)

// TestUngueltigerCVEZeitplanIstEingabefehler: Ein Tippfehler im Zeitplan ist
// eine Fehleingabe (400), kein interner Fehler (500).
func TestUngueltigerCVEZeitplanIstEingabefehler(t *testing.T) {
	svc, _ := newSettingsService(t)
	cron := "@every xyz"
	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{CVEScanCron: &cron}, "admin")
	if !errors.Is(err, services.ErrSettingInvalid) {
		t.Fatalf("erwartet ErrSettingInvalid, bekam %v", err)
	}
}
