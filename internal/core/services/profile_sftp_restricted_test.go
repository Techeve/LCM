package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// TestSFTPKontoImEingeschraenktenModus: Ein Konto mit Kontotyp „nur
// Dateizugriff" bekam im eingeschränkten Modus /bin/bash und kein
// ForceCommand - also eine volle Shell. Jetzt schreibt der Helper das
// sshd-Drop-in und setzt nologin.
func TestSFTPKontoImEingeschraenktenModus(t *testing.T) {
	env, server, _ := profileSyncSetup(t, true, services.ProfileInput{
		Name: "Daten", Slug: "daten", AccountType: domain.AccountTypeSFTP,
	})
	_, all := syncCommands(t, env, server)
	// Die Parameter stehen shell-quotiert in einem sh -c '…'.
	for _, want := range []string{
		`lcm-helper profile-sftp '\''daten'\''`,
		`lcm-helper user-ensure '\''anna'\'' '\''nologin'\''`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("fehlt: %s\n%s", want, all)
		}
	}
	if strings.Contains(all, `'\''anna'\'' '\''/bin/bash'\''`) {
		t.Errorf("sftp-konto bekommt eine shell:\n%s", all)
	}
}
