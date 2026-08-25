package domain

import "testing"

// TestIsOfflineSchwelle: Ein EINZELNER fehlgeschlagener Kontakt ist im Betrieb
// Alltag (Paketverlust, laufender Neustart, kurzer Netz-Aussetzer) und noch
// keine Aussage - erst der zweite in Folge macht daraus „offline".
func TestIsOfflineSchwelle(t *testing.T) {
	cases := []struct {
		name         string
		reachable    bool
		failedChecks int
		want         bool
	}{
		{"erreichbar", true, 0, false},
		{"erreichbar trotz alter Fehlschläge", true, 5, false},
		{"gerade nicht erreicht (1. Fehlschlag)", false, 1, false},
		{"zweiter Fehlschlag in Folge", false, 2, true},
		{"dauerhaft weg", false, 47, true},
		{"nicht erreichbar, aber noch nie geprüft", false, 0, false},
	}
	for _, c := range cases {
		s := Server{Reachable: c.reachable, FailedChecks: c.failedChecks}
		if got := s.IsOffline(); got != c.want {
			t.Errorf("%s: IsOffline() = %v, erwartet %v", c.name, got, c.want)
		}
	}
}

// TestIsOfflineUnabhaengigVonToleranz ist der Kern der Änderung: Das
// Offline-Kennzeichen hing vorher an „Nichterreichbarkeit unkritisch" und
// erschien NUR bei Servern, die deswegen nicht rot wurden. Ein ganz normal
// ausgefallener Server bekam es nie - dabei ist er der eigentliche Fall.
func TestIsOfflineUnabhaengigVonToleranz(t *testing.T) {
	for _, tolerant := range []bool{false, true} {
		s := Server{Reachable: false, FailedChecks: 3, UnreachableUncritical: tolerant}
		if !s.IsOffline() {
			t.Errorf("UnreachableUncritical=%v: Server sollte als offline gelten", tolerant)
		}
	}
}
