package domain

import (
	"testing"
	"time"
)

// TestPackagesStaleWarnAfter: Gültige Werte gelten unverändert; 0 (Altbestand)
// und Werte außerhalb der Grenzen fallen auf die Vorgabe von 48 Stunden zurück.
func TestPackagesStaleWarnAfter(t *testing.T) {
	cases := []struct {
		name  string
		hours int
		want  time.Duration
	}{
		{"Vorgabe", 48, 48 * time.Hour},
		{"Untergrenze", PackagesStaleWarnHoursMin, 24 * time.Hour},
		{"Obergrenze", PackagesStaleWarnHoursMax, 720 * time.Hour},
		{"Altbestand 0", 0, 48 * time.Hour},
		{"unter Untergrenze", 23, 48 * time.Hour},
		{"über Obergrenze", 721, 48 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := GlobalSettings{PackagesStaleWarnHours: c.hours}
			if got := g.PackagesStaleWarnAfter(); got != c.want {
				t.Errorf("PackagesStaleWarnAfter() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestPackagesStaleCriticalAfter: Tage werden in Stunden umgerechnet; 0 und
// Werte außerhalb von 2-90 Tagen fallen auf die Vorgabe von 7 Tagen zurück.
func TestPackagesStaleCriticalAfter(t *testing.T) {
	cases := []struct {
		name string
		days int
		want time.Duration
	}{
		{"Vorgabe", 7, 7 * 24 * time.Hour},
		{"Untergrenze", PackagesStaleCriticalDaysMin, 2 * 24 * time.Hour},
		{"Obergrenze", PackagesStaleCriticalDaysMax, 90 * 24 * time.Hour},
		{"Altbestand 0", 0, 7 * 24 * time.Hour},
		{"unter Untergrenze", 1, 7 * 24 * time.Hour},
		{"über Obergrenze", 91, 7 * 24 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := GlobalSettings{PackagesStaleCriticalDays: c.days}
			if got := g.PackagesStaleCriticalAfter(); got != c.want {
				t.Errorf("PackagesStaleCriticalAfter() = %v, want %v", got, c.want)
			}
		})
	}
}
