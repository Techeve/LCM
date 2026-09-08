package domain

import "testing"

// TestHybrideVerfahrenGeltenAlsQuantensicher: Alle heute verbreiteten
// Post-Quanten-Verfahren für SSH sind Hybride - sie kombinieren X25519 mit
// einem quantenresistenten Teil. Genau die müssen erkannt werden.
func TestHybrideVerfahrenGeltenAlsQuantensicher(t *testing.T) {
	for _, kex := range []string{
		"mlkem768x25519-sha256",              // was LCM selbst anbietet
		"sntrup761x25519-sha512@openssh.com", // was OpenSSH 9.6 anbietet
		"mlkem1024x448-sha512",
	} {
		if !KexPostQuantum(kex) {
			t.Errorf("%s wird nicht als quantensicher erkannt", kex)
		}
	}
}

// TestKlassischeVerfahrenGeltenNichtAlsSicher: curve25519 ist der Rückfall,
// auf dem LCM heute mit Ubuntu 24.04 landet - beide Seiten können je ein
// Post-Quanten-Verfahren, aber nicht dasselbe.
func TestKlassischeVerfahrenGeltenNichtAlsSicher(t *testing.T) {
	for _, kex := range []string{
		"curve25519-sha256",
		"curve25519-sha256@libssh.org",
		"ecdh-sha2-nistp256",
		"diffie-hellman-group14-sha256",
	} {
		if KexPostQuantum(kex) {
			t.Errorf("%s wird fälschlich als quantensicher geführt", kex)
		}
	}
}

// TestOhneErfassungWirdNichtBewertet: Ein Agent-Server hat keinen
// SSH-Handshake, und vor dem ersten Scan gibt es keinen Wert. Nichtwissen
// darf nicht als „unsicher" durchgehen - dieselbe Regel wie bei den
// Speicher-Verbünden.
func TestOhneErfassungWirdNichtBewertet(t *testing.T) {
	if KexKnown("") || KexKnown("   ") {
		t.Error("ein leerer Wert gilt als erfasst")
	}
	if KexPostQuantum("") {
		t.Error("ein leerer Wert gilt als quantensicher")
	}
}

// TestHinweisNurBeiKlassischerVerbindung: Der Befund erscheint genau dann,
// wenn ein Verfahren erfasst wurde UND es klassisch ist.
func TestHinweisNurBeiKlassischerVerbindung(t *testing.T) {
	faelle := []struct {
		kex      string
		erwartet bool
	}{
		{"curve25519-sha256", true},
		{"mlkem768x25519-sha256", false},
		{"", false}, // nicht erfasst - keine Aussage
	}
	for _, f := range faelle {
		s := &Server{
			Name: "web01", Reachable: true, KexAlgorithm: f.kex,
			OSName: "Debian GNU/Linux", DiskTotalMB: 100, DiskUsedMB: 10,
		}
		_, hinweise := s.TrafficLight(TrafficLightInput{OutdatedPackages: 0})
		var da bool
		for _, h := range hinweise {
			if h.Key == "kexClassic" {
				da = true
				if h.Severity != "info" {
					t.Errorf("%q: erwartet Hinweis (info), bekam %q", f.kex, h.Severity)
				}
				if h.Params["kex"] != f.kex {
					t.Errorf("%q: das Verfahren steht nicht in der Meldung: %v", f.kex, h.Params)
				}
			}
		}
		if da != f.erwartet {
			t.Errorf("kex=%q: Befund vorhanden=%v, erwartet=%v", f.kex, da, f.erwartet)
		}
	}
}
