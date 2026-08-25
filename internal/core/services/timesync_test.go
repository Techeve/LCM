package services

import (
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// Eine falsch gehende Uhr ist der unauffälligste Fehler im Betrieb: das System
// läuft weiter und meldet nichts, während TLS-Prüfungen, Protokoll-Reihenfolge
// über mehrere Server, TOTP-Codes und signierte Paket-Metadaten daran
// zerbrechen. Deshalb wird der Zustand gemessen, nicht angenommen.

func TestParseTimeState(t *testing.T) {
	out := `epoch=1785941882
tz=Europe/Berlin
ntp_service=chrony
ntp_sync=yes
ntp_servers=0.pool.ntp.org 1.pool.ntp.org 0.pool.ntp.org`
	st := parseTimeState(out)
	if st.Timezone != "Europe/Berlin" {
		t.Errorf("Zeitzone: %q", st.Timezone)
	}
	if st.Epoch != 1785941882 {
		t.Errorf("Epoch: %d", st.Epoch)
	}
	if st.NTPService != "chrony" || !st.NTPSync {
		t.Errorf("Zeitdienst falsch gelesen: %q sync=%v", st.NTPService, st.NTPSync)
	}
	// Doppelte Einträge fliegen raus, die Reihenfolge bleibt.
	if st.NTPServers != "0.pool.ntp.org,1.pool.ntp.org" {
		t.Errorf("Zeitserver: %q", st.NTPServers)
	}
}

// TestParseTimeStateOhneZeitdienst: ein System ohne NTP ist ein gültiger
// Zustand, kein Parse-Fehler.
func TestParseTimeStateOhneZeitdienst(t *testing.T) {
	st := parseTimeState("epoch=100\ntz=UTC\nntp_service=\nntp_sync=no\nntp_servers=")
	if st.NTPService != "" || st.NTPSync || st.NTPServers != "" {
		t.Errorf("leerer Zeitdienst falsch gelesen: %+v", st)
	}
	if st.Timezone != "UTC" {
		t.Errorf("Zeitzone: %q", st.Timezone)
	}
}

func TestClockOffsetSeconds(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	if got := clockOffsetSeconds(1_000_045, now); got != 45 {
		t.Errorf("Server geht 45 s vor, bekam %d", got)
	}
	if got := clockOffsetSeconds(999_940, now); got != -60 {
		t.Errorf("Server geht 60 s nach, bekam %d", got)
	}
	// Kein Zeitstempel gelesen = kein Versatz behaupten. Sonst stünde jeder
	// Server, dessen `date` nicht lief, mit einem gewaltigen Scheinversatz da.
	if got := clockOffsetSeconds(0, now); got != 0 {
		t.Errorf("ohne Server-Zeit darf kein Versatz gemeldet werden, bekam %d", got)
	}
}

// TestValidTimezone: der Wert landet in einem als root ausgeführten Kommando.
func TestValidTimezone(t *testing.T) {
	for _, ok := range []string{"Europe/Berlin", "UTC", "America/Argentina/Salta", "Etc/GMT+5"} {
		if _, err := validTimezone(ok); err != nil {
			t.Errorf("%q sollte gültig sein: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Europe/Berlin; rm -rf /", "../../etc/passwd", "$(id)", "a/b/c/d/e",
		strings.Repeat("x", 70)} {
		if _, err := validTimezone(bad); err == nil {
			t.Errorf("%q hätte abgelehnt werden müssen", bad)
		}
	}
}

func TestValidNTPServers(t *testing.T) {
	got, err := validNTPServers([]string{" 0.pool.ntp.org ", "", "192.168.1.1", "time.cloudflare.com"})
	if err != nil {
		t.Fatalf("gültige Server abgelehnt: %v", err)
	}
	if len(got) != 3 || got[0] != "0.pool.ntp.org" {
		t.Errorf("Bereinigung falsch: %v", got)
	}
	if _, err := validNTPServers([]string{"pool.ntp.org; reboot"}); err == nil {
		t.Error("Kommando-Einschleusung im Hostnamen wurde nicht abgelehnt")
	}
	if _, err := validNTPServers([]string{"a", "b", "c", "d", "e"}); err == nil {
		t.Error("mehr als MaxNTPServers wurde nicht abgelehnt")
	}
}

// TestTimezoneSkriptLiestZurueck: eine geschriebene Datei ist kein Beleg -
// ohne systemd greift timedatectl nicht, und /etc/timezone allein bleibt ohne
// den passenden localtime-Symlink wirkungslos.
func TestTimezoneSkriptLiestZurueck(t *testing.T) {
	s := timezoneApplyScript("Europe/Berlin")
	for _, want := range []string{"timedatectl set-timezone", "/usr/share/zoneinfo/", "ln -sf", "NOW_TZ", "exit 1"} {
		if !strings.Contains(s, want) {
			t.Errorf("Skript enthält %q nicht", want)
		}
	}
}

// TestNTPSkriptBelegtDieSynchronisierung: „Dienst gestartet" heißt nicht „Uhr
// geht richtig". Ohne Nachweis endet die Aktion mit einem eigenen Exit-Code.
func TestNTPSkriptBelegtDieSynchronisierung(t *testing.T) {
	s := ntpApplyScript([]string{"0.pool.ntp.org", "1.pool.ntp.org"})
	for _, want := range []string{"chronyc", "systemd-timesyncd", "ntp.conf", "SYNCED=yes", "exit 2"} {
		if !strings.Contains(s, want) {
			t.Errorf("Skript enthält %q nicht", want)
		}
	}
	// Bestehende Zeitserver werden ersetzt, der Rest der Datei bleibt.
	if !strings.Contains(s, `grep -vE '^[ \t]*(server|pool)[ \t]'`) {
		t.Error("die Konfiguration wird nicht zeilenweise ersetzt - fremde Einstellungen gingen verloren")
	}
	if !strings.Contains(s, ".lcm-bak") {
		t.Error("keine Sicherung der bestehenden Konfiguration")
	}
}

// TestUhrenversatzWirdGemeldet: die Ampel muss den Versatz benennen - mit der
// Richtung, sonst sucht man an der falschen Stelle.
func TestUhrenversatzWirdGemeldet(t *testing.T) {
	base := domain.Server{Reachable: true, PackageManager: "apt"}
	checked := time.Now()

	vor := base
	vor.ClockOffsetSeconds = 120
	vor.TimeCheckedAt = &checked
	_, insights := vor.TrafficLight(domain.TrafficLightInput{})
	if !hasInsight(insights, "120 Sekunden vor") {
		t.Errorf("Vorgehende Uhr nicht gemeldet: %+v", insights)
	}

	nach := base
	nach.ClockOffsetSeconds = -90
	nach.TimeCheckedAt = &checked
	_, insights = nach.TrafficLight(domain.TrafficLightInput{})
	if !hasInsight(insights, "90 Sekunden nach") {
		t.Errorf("Nachgehende Uhr nicht gemeldet: %+v", insights)
	}

	// Ein paar Sekunden sind Messrauschen (SSH-Laufzeit) - kein Hinweis.
	klein := base
	klein.ClockOffsetSeconds = 3
	klein.TimeCheckedAt = &checked
	klein.NTPService = "chrony"
	klein.NTPSynchronized = true
	_, insights = klein.TrafficLight(domain.TrafficLightInput{})
	if hasInsight(insights, "Sekunden") {
		t.Errorf("kleiner Versatz darf nicht gemeldet werden: %+v", insights)
	}
}

// TestContainerUhrVerweistAufDenHost: in einem Container kommt die Uhr vom
// Host. Der Versatz bleibt eine Warnung - ein falsch gehender Host reißt alle
// Container mit -, aber die Meldung muss sagen, wo zu suchen ist.
func TestContainerUhrVerweistAufDenHost(t *testing.T) {
	c := domain.Server{Reachable: true, PackageManager: "apt", Virtualization: "lxc"}
	checked := time.Now()
	c.TimeCheckedAt = &checked
	c.ClockOffsetSeconds = 300

	_, insights := c.TrafficLight(domain.TrafficLightInput{})
	if !hasInsight(insights, "Virtualisierungs-Host") {
		t.Errorf("die Meldung verweist nicht auf den Host: %+v", insights)
	}

	// Und ohne Versatz wird im Container KEIN fehlender Zeitdienst angemahnt -
	// dort ist keiner vorgesehen.
	ok := c
	ok.ClockOffsetSeconds = 0
	_, insights = ok.TrafficLight(domain.TrafficLightInput{})
	if hasInsight(insights, "Zeitdienst") {
		t.Errorf("im Container darf kein Zeitdienst angemahnt werden: %+v", insights)
	}
}

// TestFehlenderZeitdienstIstEinHinweis: die Uhr stimmt gerade, aber nichts
// hält sie. Noch ist nichts kaputt - also Hinweis, keine Warnung.
func TestFehlenderZeitdienstIstEinHinweis(t *testing.T) {
	s := domain.Server{Reachable: true, PackageManager: "apt", Virtualization: "kvm"}
	checked := time.Now()
	s.TimeCheckedAt = &checked

	status, insights := s.TrafficLight(domain.TrafficLightInput{})
	if !hasInsight(insights, "kein Zeitdienst") {
		t.Errorf("fehlender Zeitdienst nicht gemeldet: %+v", insights)
	}
	if status == domain.ServerStatusYellow {
		t.Error("ein fehlender Zeitdienst allein darf die Ampel nicht auf Gelb ziehen")
	}
}

func hasInsight(insights []domain.StatusInsight, substr string) bool {
	for _, i := range insights {
		if strings.Contains(i.Message, substr) {
			return true
		}
	}
	return false
}
