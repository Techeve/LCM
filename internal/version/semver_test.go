package version

import "testing"

// TestDebianTildeGiltAlsVorabversion: Das apt-Repository trägt Vorabversionen
// als 1.30.0~beta.1 - nur so sortiert apt die Beta vor das spätere Finale.
// Genau diese Zeichenkette liest die Update-Prüfung aus dem Paket-Index.
//
// Bis hierher zerbrach der Parser daran und gab 0.0.0 zurück: Eine
// Beta im Repository galt damit nie als neuer, und der Balken meldete „Sie
// haben die neueste Version" - während die Info-Seite die neuere Nummer
// daneben anzeigte.
func TestDebianTildeGiltAlsVorabversion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.29.1-beta.1", "1.30.0~beta.1", -1}, // der Fall aus dem Betrieb
		{"1.30.0~beta.1", "1.30.0", -1},        // Vorab vor Finale
		{"1.30.0", "1.30.0~beta.1", 1},
		{"1.30.0~beta.1", "1.30.0-beta.1", 0}, // beide Schreibweisen gleich
		{"1.30.0~beta.2", "1.30.0~beta.1", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, erwartet %d", c.a, c.b, got, c.want)
		}
	}
}

// TestZehnteBetaIstNeuerAlsDieNeunte: Zeichenweise verglichen stünde beta.10
// vor beta.9 - die zehnte Beta wäre kein Update gegenüber der neunten. Bei
// einem Zug, der über Wochen Betas zählt, ist das kein Randfall.
func TestZehnteBetaIstNeuerAlsDieNeunte(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.30.0-beta.9", "1.30.0-beta.10", -1},
		{"1.30.0-beta.10", "1.30.0-beta.9", 1},
		{"1.30.0-beta.2", "1.30.0-beta.2", 0},
		// Kürzer ist kleiner: beta < beta.1.
		{"1.30.0-beta", "1.30.0-beta.1", -1},
		// Text bleibt Text: alpha vor beta.
		{"1.30.0-alpha.1", "1.30.0-beta.1", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, erwartet %d", c.a, c.b, got, c.want)
		}
	}
}
