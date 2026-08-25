package middlewares

import "testing"

// TestSafeKeyPrefix (R2-049): der Wert eines ABGEWIESENEN Keys ist
// angreifergewählt - ins Log darf nur ein gekürztes, zeichensicheres Prefix,
// nie der volle Wert und nie Steuerzeichen (Log-Zeilen-Fälschung).
func TestSafeKeyPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"lcm1.abcdefghijklmnop", "lcm1.abcdefg"[:12]},
		{"kurz", "kurz"},
		{"böse\nzeile=gefaelscht", "bsezeilegefa"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := safeKeyPrefix(tc.in); got != tc.want {
			t.Errorf("safeKeyPrefix(%q) = %q, erwartet %q", tc.in, got, tc.want)
		}
	}
}
