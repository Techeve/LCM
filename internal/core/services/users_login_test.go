package services

import (
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// TestParseLastLoginsISO prüft gegen die ECHTE Ausgabe von `last --time-format
// iso` (abgenommen auf Ubuntu 24.04, util-linux 2.39). Das ISO-Format trägt
// den Zeitzonen-Offset - nur damit ist der Zeitpunkt eindeutig.
func TestParseLastLoginsISO(t *testing.T) {
	out := strings.Join([]string{
		`LCMLOGIN|root     pts/3        192.168.201.1    2026-08-15T00:40:09+02:00 - 2026-08-15T02:52:09+02:00  (02:12)`,
		`LCMLOGIN|tony     pts/0        10.0.0.5         2026-08-18T09:12:33+02:00   still logged in`,
		`LCMLOGIN|admin    tty1                          2026-08-16T08:00:00+02:00 - crash                     (02:11)`,
		`LCMLOGIN|kaputte zeile ohne datum`,
		`LCMLOGIN|`,
	}, "\n")

	got := parseLastLogins(out)
	if len(got) != 3 {
		t.Fatalf("erwartet 3 Einträge, bekam %d: %+v", len(got), got)
	}

	// Abgeschlossene Sitzung mit Dauer.
	if got[0].Username != "root" || got[0].TTY != "pts/3" || got[0].FromHost != "192.168.201.1" {
		t.Errorf("Spalten falsch zerlegt: %+v", got[0])
	}
	if got[0].EndedAt == nil || got[0].DurationMinutes() != 132 {
		t.Errorf("Dauer falsch (%d min): %+v", got[0].DurationMinutes(), got[0])
	}
	// Der Offset muss übernommen werden - sonst läge der Zeitpunkt um zwei
	// Stunden daneben.
	if _, offset := got[0].StartedAt.Zone(); offset != 2*3600 {
		t.Errorf("Zeitzonen-Offset nicht übernommen: %v", got[0].StartedAt)
	}

	// Laufende Sitzung.
	if !got[1].StillActive || got[1].EndedAt != nil {
		t.Errorf("laufende Sitzung nicht erkannt: %+v", got[1])
	}

	// Lokale Anmeldung ohne Herkunft, Ende unbekannt (Absturz).
	if got[2].TTY != "tty1" || got[2].FromHost != "" {
		t.Errorf("lokale Anmeldung falsch: %+v", got[2])
	}
	if got[2].EndedAt != nil || got[2].StillActive {
		t.Errorf("Absturz darf weder Ende noch aktiv-Kennzeichen setzen: %+v", got[2])
	}
}

// TestParseLastLoginsWanduhr: ältere last-Fassungen kennen --time-format nicht.
// Dann kommt die Wanduhr-Form ohne Zone - die Historie muss trotzdem stehen.
func TestParseLastLoginsWanduhr(t *testing.T) {
	out := `LCMLOGIN|tony     pts/1        host.example.com Thu Jul 23 00:44:52 2026 - Sat Aug 15 23:25:55 2026 (23+22:41)
LCMLOGIN|tony     pts/0        10.0.0.5         Mon Aug 18 09:12:33 2026   still logged in`
	got := parseLastLogins(out)
	if len(got) != 2 {
		t.Fatalf("erwartet 2 Einträge, bekam %d", len(got))
	}
	if got[0].EndedAt == nil {
		t.Fatalf("mehrtägige Sitzung ohne Ende: %+v", got[0])
	}
	if d := got[0].EndedAt.Sub(got[0].StartedAt); d < 23*24*time.Hour {
		t.Errorf("mehrtägige Dauer falsch: %v", d)
	}
	if !got[1].StillActive {
		t.Errorf("laufende Sitzung im Wanduhr-Format nicht erkannt: %+v", got[1])
	}
}

// TestUsersScanErhebtHistorie: das Skript muss `last` überhaupt aufrufen und
// dabei das ISO-Format bevorzugen.
func TestUsersScanErhebtHistorie(t *testing.T) {
	script := usersScanScript()
	for _, want := range []string{"command -v last", "--time-format iso", "LCMLOGIN|", "-n 300"} {
		if !strings.Contains(script, want) {
			t.Errorf("Scan-Skript ohne %q", want)
		}
	}
	// reboot/shutdown-Zeilen sind keine Benutzer-Anmeldungen.
	if !strings.Contains(script, "reboot*") {
		t.Error("Scan-Skript filtert die reboot-Zeilen nicht heraus")
	}
}

// TestApplyLoginHistoryFuelltLetztenLogin: Debian 13 bringt weder lastlog noch
// lastlog2 mit - dort bliebe „zuletzt angemeldet" dauerhaft leer, während die
// Historie längst Einträge hat. Genau dieser Widerspruch fiel im Live-Test auf.
func TestApplyLoginHistoryFuelltLetztenLogin(t *testing.T) {
	older := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 18, 12, 3, 40, 0, time.UTC)
	ausLastlog := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	users := []domain.ServerUser{
		{Username: "root"},                           // kein lastlog-Wert
		{Username: "tony", LastLoginAt: &ausLastlog}, // lastlog hat geliefert
		{Username: "ohne"},                           // gar keine Anmeldungen
	}
	logins := []domain.ServerUserLogin{
		{Username: "root", StartedAt: older},
		{Username: "root", StartedAt: newer},
		{Username: "tony", StartedAt: newer},
	}
	applyLoginHistory(users, logins)

	if users[0].LastLoginAt == nil || !users[0].LastLoginAt.Equal(newer) {
		t.Errorf("root: erwartet die NEUESTE Anmeldung %v, bekam %v", newer, users[0].LastLoginAt)
	}
	// Ein vorhandener lastlog-Wert bleibt maßgeblich - er ist die Quelle, die
	// das System selbst führt.
	if !users[1].LastLoginAt.Equal(ausLastlog) {
		t.Errorf("tony: lastlog-Wert wurde überschrieben: %v", users[1].LastLoginAt)
	}
	if users[2].LastLoginAt != nil {
		t.Errorf("ohne: ohne Anmeldungen darf nichts gesetzt werden: %v", users[2].LastLoginAt)
	}
}
