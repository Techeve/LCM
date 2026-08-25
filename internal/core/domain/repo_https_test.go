package domain_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestRepoURIErkenntBeideAptFormate: Die URL steht in der einzeiligen Form an
// anderer Stelle als in deb822 - und hinter optionalen Optionen in Klammern.
func TestRepoURIErkenntBeideAptFormate(t *testing.T) {
	cases := map[string]string{
		"deb https://deb.debian.org/debian bookworm main":                           "https://deb.debian.org/debian",
		"deb [signed-by=/etc/apt/keyrings/x.asc] http://old.example/debian bw main": "http://old.example/debian",
		"URIs: https://archive.ubuntu.com/ubuntu":                                   "https://archive.ubuntu.com/ubuntu",
		"https://download.docker.com/linux/debian":                                  "https://download.docker.com/linux/debian",
		"deb cdrom:[Debian GNU/Linux]/ bookworm main":                               "",
		"": "",
	}
	for line, want := range cases {
		if got := domain.RepoURI(line); got != want {
			t.Errorf("RepoURI(%q) = %q, erwartet %q", line, got, want)
		}
	}
}

// TestDistroSpiegelErkannt: Die Landesspiegel gehören dazu - sie sind der
// Regelfall auf einem frisch installierten System.
func TestDistroSpiegelErkannt(t *testing.T) {
	for _, uri := range []string{
		"https://deb.debian.org/debian",
		"https://security.debian.org/debian-security",
		"https://ftp.de.debian.org/debian",
		"https://de.archive.ubuntu.com/ubuntu",
		"https://ports.ubuntu.com/ubuntu-ports",
		"https://archive.raspberrypi.com/debian",
	} {
		if !domain.IsDistroMirror(uri) {
			t.Errorf("%s müsste als Distributions-Spiegel gelten", uri)
		}
	}
	// Fremdquellen nicht - sie sprechen von sich aus https, und genau die
	// soll die Rückstellung in Ruhe lassen.
	for _, uri := range []string{
		"https://download.docker.com/linux/debian",
		"https://repo.techeve.de/apt",
		"https://packages.debian.org.angreifer.example/debian",
		"nicht-mal-eine-url",
	} {
		if domain.IsDistroMirror(uri) {
			t.Errorf("%s ist kein Distributions-Spiegel", uri)
		}
	}
}

// TestUnsichereURLsWerdenNichtVorgeschlagen: Die URL landet in einer
// sed-Ersetzung, die als root läuft. Was Sonderzeichen trägt, kommt gar nicht
// erst auf die Kandidatenliste.
func TestUnsichereURLsWerdenNichtVorgeschlagen(t *testing.T) {
	for _, uri := range []string{
		"https://deb.debian.org/$(id)",
		"https://deb.debian.org/a|b",
		"https://deb.debian.org/a&b",
		"https://deb.debian.org/a;rm -rf /",
	} {
		if domain.SafeRepoURI(uri) {
			t.Errorf("%q müsste als unsicher gelten", uri)
		}
		candidates := domain.HTTPSRevertCandidates(nil, []domain.AptRepository{{Line: "deb " + uri + " bookworm main"}})
		if len(candidates) != 0 {
			t.Errorf("%q wurde trotzdem vorgeschlagen: %v", uri, candidates)
		}
	}
	if !domain.SafeRepoURI("https://ftp.de.debian.org/debian") {
		t.Error("eine gewöhnliche Spiegel-URL müsste als sicher gelten")
	}
	// Ein Leerzeichen ist kein Sonderfall, sondern das Trennzeichen: apt liest
	// die Zeile genauso, und der Kandidat ist der Teil davor.
	if got := domain.RepoURI("deb https://deb.debian.org/a b main"); got != "https://deb.debian.org/a" {
		t.Errorf("RepoURI bricht nicht am Leerzeichen ab: %q", got)
	}
}

// TestKandidatenMitProtokoll: Liegt das Protokoll der Umstellung vor, zählt
// allein das - auch für eine Fremdquelle, die LCM tatsächlich umgestellt hat.
// Und der Distributions-Spiegel, den LCM NICHT angefasst hat, bleibt außen
// vor: Er war schon vorher https.
func TestKandidatenMitProtokoll(t *testing.T) {
	repos := []domain.AptRepository{
		{Line: "deb https://deb.debian.org/debian bookworm main"},
		{Line: "deb https://alt.example/firma bookworm main"},
		{Line: "deb https://download.docker.com/linux/debian bookworm stable"},
	}
	got := domain.HTTPSRevertCandidates([]string{"http://alt.example/firma"}, repos)
	if strings.Join(got, ",") != "https://alt.example/firma" {
		t.Errorf("erwartet nur die protokollierte Quelle, war %v", got)
	}
}

// TestKandidatenOhneProtokoll: Auf Servern, die vor dieser Version umgestellt
// wurden, gibt es kein Protokoll. Dann bleiben die Distributions-Spiegel - und
// nur die.
func TestKandidatenOhneProtokoll(t *testing.T) {
	repos := []domain.AptRepository{
		{Line: "deb https://deb.debian.org/debian bookworm main"},
		{Line: "deb https://deb.debian.org/debian bookworm contrib"}, // gleiche URL, zweite Zeile
		{Line: "deb https://download.docker.com/linux/debian bookworm stable"},
		{Line: "deb http://security.debian.org/debian-security bookworm-security main"},
	}
	got := domain.HTTPSRevertCandidates(nil, repos)
	if strings.Join(got, ",") != "https://deb.debian.org/debian" {
		t.Errorf("erwartet nur den Debian-Spiegel (einmal), war %v", got)
	}
}
