package main

import (
	"fmt"
	"time"
	_ "time/tzdata" // die Alpine-Images haben keine Zeitzonendatenbank
)

// Wann ein Patch-Release einer Linie fällig ist, steht hier und nirgends
// sonst: Dienstag (Europe/Berlin) mit Release-Anlass seit dem letzten Tag der
// Linie, oder jeden anderen Tag, wenn dieser Anlass ein Sicherheitsupdate ist.

// Facts sind die Eingaben der Entscheidung, aus dem Repository gelesen.
type Facts struct {
	Line     string
	Now      time.Time
	LastTag  string   // Tag der Linie, ab dem gezählt wird
	Version  string   // VERSION der Linie
	Relevant bool     // Commits mit Release-Anlass seit LastTag (tools/release)
	Security bool     // ein Commit mit [security] seit LastTag
	Tags     []string // alle Tags des Repositorys
}

// Decision ist das Ergebnis: ob und welche Version erscheint, und warum.
type Decision struct {
	Release bool
	Version string
	Reason  string
}

// Decide entscheidet über das Patch-Release einer Linie.
func Decide(f Facts) (Decision, error) {
	if !f.Relevant {
		return Decision{Reason: fmt.Sprintf("seit %s kein Commit mit Release-Anlass (fix/perf/refactor/feat)", f.LastTag)}, nil
	}
	reason := ""
	switch {
	case f.Security:
		reason = fmt.Sprintf("Sicherheitsupdate seit %s - kein Warten bis Dienstag", f.LastTag)
	default:
		tuesday, err := isTuesdayInBerlin(f.Now)
		if err != nil {
			return Decision{}, err
		}
		if !tuesday {
			return Decision{Reason: fmt.Sprintf("Release-Anlass seit %s, aber heute ist nicht Dienstag und es gibt kein Sicherheitsupdate", f.LastTag)}, nil
		}
		reason = fmt.Sprintf("Dienstag, Release-Anlass seit %s", f.LastTag)
	}
	next, err := nextFreePatch(f.Version, f.Tags)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Release: true, Version: next, Reason: reason}, nil
}

func isTuesdayInBerlin(now time.Time) (bool, error) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return false, fmt.Errorf("zeitzone Europe/Berlin: %w", err)
	}
	return now.In(berlin).Weekday() == time.Tuesday, nil
}
