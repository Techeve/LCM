package services

import (
	"testing"
	"time"

	"LCM/internal/cron"
)

// TestSystemTakteHaengenAnDerUhr: Die festen System-Takte dürfen nicht davon
// abhängen, WANN der Eintrag angelegt wurde. Reload legt bei jedem Speichern
// einer Regel alle Einträge neu an; mit „@every 30m" rückte der nächste Lauf
// dabei jedes Mal um eine halbe Stunde hinaus. Wer öfter speicherte, hielt
// Alarm-Auswertung, Frühwarnung und Update-Prüfung damit ganz an.
//
// Geprüft wird deshalb: Zwei Anlagen zu verschiedenen Zeitpunkten im selben
// Takt ergeben denselben nächsten Lauf, und der Abstand zum übernächsten ist
// der versprochene Takt.
func TestSystemTakteHaengenAnDerUhr(t *testing.T) {
	cases := []struct {
		name     string
		spec     string
		interval time.Duration
	}{
		{"Alarm-Auswertung", alertCheckCron, 30 * time.Minute},
		{"Frühwarnung", advisoryPollCron, 15 * time.Minute},
		{"Update-Prüfung", updateCheckCron, 3 * time.Hour},
		{"Backup-Watchdog", backupWatchdogCron, 10 * time.Minute},
		{"CVE-Datenbank", cveDBUpdateCron, time.Hour},
	}
	// Kurz nach einem Lauf jedes der Takte: 00:21 liegt hinter 00:20 (Update),
	// 00:10 (Datenbank), 00:15, 00:20 und 00:00.
	base := time.Date(2026, 11, 8, 0, 21, 0, 0, time.Local)
	for _, tc := range cases {
		sched, err := cron.Parse(tc.spec)
		if err != nil {
			t.Fatalf("%s: %q nicht lesbar: %v", tc.name, tc.spec, err)
		}
		next := sched.Next(base)
		if later := sched.Next(base.Add(2*time.Minute + 7*time.Second)); !later.Equal(next) {
			t.Errorf("%s (%q): Anlage um %s ergibt %s, zwei Minuten später %s - der Takt hängt am Reload",
				tc.name, tc.spec, base.Format("15:04"), next.Format("15:04:05"), later.Format("15:04:05"))
		}
		if gap := sched.Next(next).Sub(next); gap != tc.interval {
			t.Errorf("%s (%q): Abstand %s, versprochen %s", tc.name, tc.spec, gap, tc.interval)
		}
	}
}

// TestUpdatePruefungVerteiltSichUeberHosts: Verschiedene Hosts fragen zu
// verschiedenen Minuten, derselbe Host immer zur selben.
func TestUpdatePruefungVerteiltSichUeberHosts(t *testing.T) {
	if updateCheckSpec("lcm-a") != updateCheckSpec("lcm-a") {
		t.Fatal("derselbe Host bekommt verschiedene Takte")
	}
	seen := map[string]bool{}
	for _, host := range []string{"lcm-a", "lcm-b", "lcm-prod", "lcm-demo", "server-17", ""} {
		spec := updateCheckSpec(host)
		if _, err := cron.Parse(spec); err != nil {
			t.Fatalf("%s: ungültiger Takt %q: %v", host, spec, err)
		}
		seen[spec] = true
	}
	if len(seen) < 4 {
		t.Errorf("zu wenig Streuung: %v", seen)
	}
}
