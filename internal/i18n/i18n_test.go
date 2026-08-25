package i18n

import "testing"

// TestLangDefaultsToEnglish: Englisch ist der Standard - nur ein wirklich auf
// Deutsch eingestelltes System bekommt deutsche Ausgaben.
func TestLangDefaultsToEnglish(t *testing.T) {
	cases := []struct {
		locale string
		want   string
	}{
		{"de_DE.UTF-8", "de"},
		{"de_AT.UTF-8", "de"},
		{"de_CH", "de"},
		{"de", "de"},
		{"de-DE", "de"},
		{"DE_DE.UTF-8", "de"},
		{"en_US.UTF-8", "en"},
		{"en_GB", "en"},
		{"fr_FR.UTF-8", "en"},
		{"C", "en"},
		{"POSIX", "en"},
		{"", "en"},
		// Nicht auf "de" hereinfallen, das nur zufällig so anfängt.
		{"den_DK", "en"},
		{"de_facto_quatsch", "de"}, // de_* gilt bewusst als Deutsch
	}
	for _, c := range cases {
		t.Setenv("LC_ALL", "")
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", c.locale)
		if got := Lang(); got != c.want {
			t.Errorf("LANG=%q: Lang() = %q, erwartet %q", c.locale, got, c.want)
		}
	}
}

// TestLangPrecedence: LC_ALL schlägt LC_MESSAGES schlägt LANG (POSIX).
func TestLangPrecedence(t *testing.T) {
	t.Setenv("LANG", "de_DE.UTF-8")
	t.Setenv("LC_MESSAGES", "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	if got := Lang(); got != "en" {
		t.Errorf("LC_MESSAGES sollte LANG schlagen: %q", got)
	}
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	if got := Lang(); got != "de" {
		t.Errorf("LC_ALL sollte LC_MESSAGES schlagen: %q", got)
	}
}

// TestTIsAlwaysASCII ist die Kernzusage des Pakets: Was T liefert, ist in
// JEDER Umgebung darstellbar - auch wenn im Quelltext Umlaute stehen.
func TestTIsAlwaysASCII(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	got := T("Web interface", "Weboberfläche größer öffnen - Straße")
	want := "Weboberflaeche groesser oeffnen - Strasse"
	if got != want {
		t.Errorf("T() = %q, erwartet %q", got, want)
	}
	for i := 0; i < len(got); i++ {
		if got[i] >= 128 {
			t.Fatalf("Ausgabe enthält Nicht-ASCII an Position %d: %q", i, got)
		}
	}
}

// TestTPicksEnglishByDefault deckt den Regelfall ab.
func TestTPicksEnglishByDefault(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := T("Password", "Passwort"); got != "Password" {
		t.Errorf("T() = %q, erwartet %q", got, "Password")
	}
}

// TestTfFormatsAfterTranslation: Eingesetzte Werte bleiben unangetastet -
// sie sind Daten (Pfade, Fehlertexte), keine Übersetzungen.
func TestTfFormatsAfterTranslation(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	got := Tf("config created at %s", "Konfiguration angelegt unter %s", "/etc/lcm")
	if got != "Konfiguration angelegt unter /etc/lcm" {
		t.Errorf("Tf() = %q", got)
	}
}

// TestASCIIDropsUnknownRunes: Zeichen ohne Entsprechung verschwinden, statt
// als Zeichensalat stehen zu bleiben.
func TestASCIIDropsUnknownRunes(t *testing.T) {
	if got := ASCII("Preis: 10 € für Wärme ✓"); got != "Preis: 10  fuer Waerme " {
		t.Errorf("ASCII() = %q", got)
	}
	// Reines ASCII bleibt unverändert (schneller Pfad).
	in := "plain ascii text 123"
	if got := ASCII(in); got != in {
		t.Errorf("ASCII() veränderte reines ASCII: %q", got)
	}
}
