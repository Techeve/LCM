package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestRegistrierungsstandWirdGelesen: Die Ausgabe von subscription-manager
// entscheidet über einen Ampel-Befund - was hier falsch zugeordnet wird,
// erzeugt entweder einen Fehlalarm oder verschweigt einen ungepflegten Server.
func TestRegistrierungsstandWirdGelesen(t *testing.T) {
	cases := map[string]string{
		"Overall Status: Current":        domain.RHSMRegistered,
		"overall status: current":        domain.RHSMRegistered,
		"Overall Status: Not registered": domain.RHSMUnregistered,
		"Overall Status: Insufficient":   domain.RHSMInvalid,
		"Overall Status: Invalid":        domain.RHSMInvalid,
		"Overall Status: Unknown":        domain.RHSMInvalid,
		// Kein subscription-manager: leere Ausgabe, keine Aussage.
		"":                  "",
		"irgendwas anderes": "",
	}
	for out, want := range cases {
		if got := parseRHSMStatus(out); got != want {
			t.Errorf("%q → %q, erwartet %q", out, got, want)
		}
	}
}

// TestSimpleContentAccessGiltAlsRegistriert: Seit Simple Content Access prüft
// Red Hat keine Berechtigungen mehr und meldet "Disabled". Das als Mangel zu
// werten hieße, den Regelfall anzumahnen.
func TestSimpleContentAccessGiltAlsRegistriert(t *testing.T) {
	if got := parseRHSMStatus("Overall Status: Disabled\nContent Access Mode is set to Simple Content Access."); got != domain.RHSMRegistered {
		t.Errorf("Disabled → %q, erwartet registriert", got)
	}
}

// TestRhsmSkriptBleibtStummOhneSubscriptionManager: Auf Rocky, AlmaLinux und
// der ganzen Debian-Welt gibt es das Werkzeug nicht - dort darf nichts
// behauptet werden.
func TestRhsmSkriptBleibtStummOhneSubscriptionManager(t *testing.T) {
	if !strings.HasPrefix(rhsmStatusScript, "command -v subscription-manager >/dev/null 2>&1 || exit 0") {
		t.Errorf("das Skript prüft nicht zuerst auf das Werkzeug:\n%s", rhsmStatusScript)
	}
	// Ersatzweg für den eingeschränkten Modus: Das Consumer-Zertifikat gibt es
	// nur bei einer Registrierung.
	if !strings.Contains(rhsmStatusScript, "/etc/pki/consumer/cert.pem") {
		t.Errorf("kein Ersatzweg ohne Root:\n%s", rhsmStatusScript)
	}
}
