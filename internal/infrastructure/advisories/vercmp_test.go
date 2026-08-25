package advisories

import "testing"

// TestCompareDeb prüft den dpkg-Vergleich an den Fällen, an denen naive
// Verfahren scheitern. Jeder einzelne davon hieße in der Praxis: ein
// verwundbarer Server wird für sauber erklärt (oder umgekehrt).
func TestCompareDeb(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		why  string
	}{
		{"1.0", "1.0", 0, "gleich"},
		// Zahlen sind Zahlen, kein Text: sonst wäre 1.9 größer als 1.10.
		{"1.9", "1.10", -1, "numerischer Abschnitt"},
		{"1.10", "1.9", 1, "numerischer Abschnitt umgekehrt"},
		{"1.0", "1.00", 0, "führende Nullen zählen nicht"},
		// Die Tilde: Vorabversionen sind KLEINER als die Freigabe.
		{"1.0~rc1", "1.0", -1, "Tilde sortiert vor dem Ende"},
		{"1.0~rc1", "1.0~rc2", -1, "Tilde-Vorabversionen untereinander"},
		{"1.0~~", "1.0~", -1, "doppelte Tilde"},
		// Die Epoche schlägt alles.
		{"1:1.0", "2.0", 1, "Epoche schlägt höhere Version"},
		{"2:1.0", "1:9.9", 1, "höhere Epoche gewinnt"},
		{"1.0", "1:1.0", -1, "fehlende Epoche ist 0"},
		// Revision am letzten Bindestrich.
		{"1.0-1", "1.0-2", -1, "Revision"},
		{"1.0-1", "1.0", 1, "mit Revision schlägt ohne"},
		{"1.0-1-1", "1.0-1-2", -1, "Bindestrich im Upstream-Teil"},
		// Echte Debian-Versionen aus dem Bestand.
		{"3.0.11-1~deb12u2", "3.0.11-1~deb12u3", -1, "Debian-Sicherheitsupdate"},
		{"3.0.11-1~deb12u2", "3.0.11-1", -1, "Tilde-Suffix ist kleiner als ohne"},
		{"1.1.1n-0+deb11u5", "1.1.1n-0+deb11u4", 1, "Plus-Suffix"},
		{"5.2.15-2+b7", "5.2.15-2", 1, "Binary-NMU"},
		// Buchstaben gegen Satzzeichen.
		{"1.0a", "1.0+", -1, "Buchstabe vor Satzzeichen"},
	}
	for _, c := range cases {
		if got := compareDeb(c.a, c.b); got != c.want {
			t.Errorf("compareDeb(%q, %q) = %d, erwartet %d (%s)", c.a, c.b, got, c.want, c.why)
		}
		// Gegenrichtung muss spiegeln - sonst wäre die Ordnung inkonsistent
		// und das Ergebnis hinge von der Aufrufreihenfolge ab.
		if got := compareDeb(c.b, c.a); got != -c.want {
			t.Errorf("compareDeb(%q, %q) = %d, erwartet %d (Gegenrichtung)", c.b, c.a, got, -c.want)
		}
	}
}

// TestDebTildenOrdnung prüft die kanonische Kette aus der dpkg-Testsuite:
// "~~" < "~~a" < "~" < "" < "a". Sie ist der Prüfstein der Tilde-Regel -
// wer sie besteht, hat den Sonderfall vollständig, wer sie reißt, ordnet
// Vorabversionen falsch ein.
func TestDebTildenOrdnung(t *testing.T) {
	kette := []string{"1.0~~", "1.0~~a", "1.0~", "1.0", "1.0a"}
	for i := 0; i < len(kette)-1; i++ {
		if got := compareDeb(kette[i], kette[i+1]); got != -1 {
			t.Errorf("%q < %q erwartet, bekam %d", kette[i], kette[i+1], got)
		}
	}
	// Und transitiv über die ganze Kette: das erste Glied ist kleiner als
	// jedes spätere.
	for i := 1; i < len(kette); i++ {
		if compareDeb(kette[0], kette[i]) != -1 {
			t.Errorf("%q müsste kleiner sein als %q", kette[0], kette[i])
		}
	}
}

// TestCompareRPM prüft den rpmvercmp-Vergleich.
func TestCompareRPM(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		why  string
	}{
		{"1.0", "1.0", 0, "gleich"},
		{"1.9", "1.10", -1, "numerisch"},
		{"1.0", "1.0.1", -1, "längere Version"},
		// Trennzeichen bewerten in rpm nicht, sie trennen nur.
		{"1.0.1", "1_0_1", 0, "Trennzeichen sind gleichwertig"},
		// Ziffern schlagen Buchstaben.
		{"1.1", "1.a", 1, "Ziffer schlägt Buchstabe"},
		// Tilde wie bei dpkg.
		{"1.0~rc1", "1.0", -1, "Tilde"},
		{"1.0~rc1", "1.0~rc2", -1, "Tilde untereinander"},
		// Epoche.
		{"1:1.0", "2.0", 1, "Epoche schlägt Version"},
		// Echte RPM-Versionen.
		{"3.0.7-24.el9", "3.0.7-25.el9", -1, "Release-Zähler"},
		{"3.0.7-24.el9", "3.0.7-24.el9_2", -1, "Punktfreigabe"},
	}
	for _, c := range cases {
		if got := compareRPM(c.a, c.b); got != c.want {
			t.Errorf("compareRPM(%q, %q) = %d, erwartet %d (%s)", c.a, c.b, got, c.want, c.why)
		}
		if got := compareRPM(c.b, c.a); got != -c.want {
			t.Errorf("compareRPM(%q, %q) = %d, erwartet %d (Gegenrichtung)", c.b, c.a, got, -c.want)
		}
	}
}

// TestCompareVersionsWaehltDasVerfahren: Derselbe Text kann je nach Pakettyp
// verschieden geordnet sein - hier am Trennzeichen, das rpm ignoriert und
// dpkg bewertet.
func TestCompareVersionsWaehltDasVerfahren(t *testing.T) {
	if CompareVersions("rpm", "1.0.1", "1_0_1") != 0 {
		t.Error("rpm sollte Trennzeichen gleich behandeln")
	}
	if CompareVersions("deb", "1.0.1", "1_0_1") == 0 {
		t.Error("dpkg sollte Trennzeichen bewerten")
	}
}
