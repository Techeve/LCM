package advisories

import (
	"testing"

	"LCM/internal/core/domain"
)

// TestCvssBaseScore prüft die Umrechnung gegen veröffentlichte Vektoren mit
// bekanntem Basiswert. Ein Rechenfehler hier verschiebt die Schwere ganzer
// Befundmengen - und damit die Schwelle, ab der ein Alarm auslöst.
func TestCvssBaseScore(t *testing.T) {
	cases := []struct {
		vector string
		want   float64
	}{
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8},
		{"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H", 7.8},
		{"CVSS:3.1/AV:L/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 8.4},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N", 6.1},
		{"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:N/I:N/A:H", 5.5},
		{"CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:N/A:N", 5.9},
		{"CVSS:3.0/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H", 7.5},
	}
	for _, c := range cases {
		got, ok := cvssBaseScore(c.vector)
		if !ok {
			t.Errorf("%s: nicht auswertbar", c.vector)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %.1f, erwartet %.1f", c.vector, got, c.want)
		}
	}
}

// TestCvssLehntFremdeFassungenAb: Ein CVSS-2- oder CVSS-4-Vektor darf NICHT
// mit der 3er-Formel gerechnet werden - der Wert sähe belastbar aus und wäre
// falsch.
func TestCvssLehntFremdeFassungenAb(t *testing.T) {
	for _, vector := range []string{
		"AV:N/AC:L/Au:N/C:P/I:P/A:P",                       // CVSS 2
		"CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H", // CVSS 4
		"unsinn",
	} {
		if _, ok := cvssBaseScore(vector); ok {
			t.Errorf("%q hätte nicht gerechnet werden dürfen", vector)
		}
	}
}

// TestSeverityFromReihenfolge prüft die Rangfolge der Quellen: ausdrückliche
// Angabe, dann CVSS-Vektor, dann die Dringlichkeit der Distribution.
func TestSeverityFromReihenfolge(t *testing.T) {
	// Ausdrückliche Angabe schlägt alles.
	if got := severityFrom("CRITICAL", []string{"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:N/I:N/A:H"}, "low"); got != domain.SeverityCritical {
		t.Errorf("ausdrückliche Angabe missachtet: %q", got)
	}
	// Ohne sie zählt der Vektor (5.5 = mittel).
	if got := severityFrom("", []string{"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:N/I:N/A:H"}, "high"); got != domain.SeverityMedium {
		t.Errorf("CVSS-Vektor missachtet: %q", got)
	}
	// Ohne beides die Dringlichkeit - der Regelfall bei Debian.
	if got := severityFrom("", nil, "high"); got != domain.SeverityHigh {
		t.Errorf("Dringlichkeit missachtet: %q", got)
	}
}

// TestSeverityForUrgency: „unimportant" ist eine Bewertung und wird als
// niedrig geführt; „not yet assigned" ist KEINE und bleibt unbekannt -
// eine erfundene Stufe wäre schlimmer als keine.
func TestSeverityForUrgency(t *testing.T) {
	cases := map[string]string{
		"high":             domain.SeverityHigh,
		"medium":           domain.SeverityMedium,
		"low":              domain.SeverityLow,
		"unimportant":      domain.SeverityLow,
		"not yet assigned": domain.SeverityUnknown,
		"end-of-life":      domain.SeverityUnknown,
		"":                 domain.SeverityUnknown,
	}
	for urgency, want := range cases {
		if got := severityForUrgency(urgency); got != want {
			t.Errorf("severityForUrgency(%q) = %q, erwartet %q", urgency, got, want)
		}
	}
}
