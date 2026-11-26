package main

import (
	"strings"
	"testing"
	"time"
)

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func baseFacts(now time.Time) Facts {
	return Facts{
		Line: "community", Now: now, LastTag: "v2.3.2", Version: "2.3.2",
		Relevant: true, Tags: []string{"v2.3.2", "v2.2.3"},
	}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name    string
		now     string
		change  func(*Facts)
		release bool
		version string
		reason  string
	}{
		{"Dienstag mit Anlass", "2026-12-01T05:00:00Z", func(*Facts) {}, true, "2.3.3", "Dienstag"},
		{"Montag mit Anlass", "2026-11-30T05:00:00Z", func(*Facts) {}, false, "", "nicht Dienstag"},
		{"Mittwoch mit Anlass", "2026-12-02T05:00:00Z", func(*Facts) {}, false, "", "nicht Dienstag"},
		{"Dienstag ohne Anlass", "2026-12-01T05:00:00Z", func(f *Facts) { f.Relevant = false }, false, "", "kein Commit"},
		{"Sicherheit am Mittwoch", "2026-12-02T05:00:00Z", func(f *Facts) { f.Security = true }, true, "2.3.3", "Sicherheitsupdate"},
		{"Sicherheit ohne Anlass", "2026-12-02T05:00:00Z", func(f *Facts) { f.Security = true; f.Relevant = false }, false, "", "kein Commit"},
		// Montag 23:30 UTC ist in Berlin schon Dienstag 00:30 (Winterzeit)
		{"Zeitzone: Dienstag nach Mitternacht", "2026-11-30T23:30:00Z", func(*Facts) {}, true, "2.3.3", "Dienstag"},
		// Dienstag 23:30 UTC ist in Berlin schon Mittwoch
		{"Zeitzone: Mittwoch nach Mitternacht", "2026-12-01T23:30:00Z", func(*Facts) {}, false, "", "nicht Dienstag"},
		{"vergebene Nummer wird übersprungen", "2026-12-01T05:00:00Z", func(f *Facts) { f.Tags = append(f.Tags, "v2.3.3-beta.1", "v2.3.4") }, true, "2.3.5", "Dienstag"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := baseFacts(at(t, c.now))
			c.change(&f)
			got, err := Decide(f)
			if err != nil {
				t.Fatal(err)
			}
			if got.Release != c.release || got.Version != c.version || !strings.Contains(got.Reason, c.reason) {
				t.Errorf("Decide = %+v; erwartet Release=%v Version=%q Grund mit %q", got, c.release, c.version, c.reason)
			}
		})
	}
}

func TestAnySecurity(t *testing.T) {
	if !anySecurity([]string{"fix(ui): x", "fix(deps): update module y to v2 [security]"}) {
		t.Error("[security] nicht erkannt")
	}
	if !anySecurity([]string{"fix(deps): update module y to v2 [SECURITY]"}) {
		t.Error("[SECURITY] nicht erkannt")
	}
	if anySecurity([]string{"fix(core): Sicherheitslücke in der Anmeldung"}) {
		t.Error("Freitext ohne Merkmal gilt als Sicherheitsupdate")
	}
}
