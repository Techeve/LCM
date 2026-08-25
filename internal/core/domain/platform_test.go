package domain

import (
	"testing"
	"time"
)

func TestOSSupportStatus(t *testing.T) {
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)

	// Ubuntu 24.04 LTS: aktuell, unterstützt.
	u := OSSupportStatus("ubuntu", "24.04", "Ubuntu", now)
	if !u.Known || !u.Supported || !u.IsLTS || !u.UpToDate {
		t.Errorf("ubuntu 24.04: %+v", u)
	}
	if u.Severity != "" {
		t.Errorf("ubuntu 24.04 sollte keine warnung sein: %q", u.Severity)
	}

	// Ubuntu 20.04 LTS: EOL 2025-05 → nicht mehr unterstützt (kritisch).
	old := OSSupportStatus("ubuntu", "20.04", "Ubuntu", now)
	if !old.Known || old.Supported || old.Severity != "critical" {
		t.Errorf("ubuntu 20.04 sollte EOL/kritisch sein: %+v", old)
	}

	// Ubuntu 22.04 LTS: unterstützt, aber neuere LTS verfügbar (keine Warnung,
	// aber nicht up-to-date).
	lts := OSSupportStatus("ubuntu", "22.04", "Ubuntu", now)
	if !lts.Supported || lts.UpToDate || lts.Severity != "" {
		t.Errorf("ubuntu 22.04: %+v", lts)
	}

	// Debian 12 (bookworm): unterstützt bis 2028.
	d := OSSupportStatus("debian", "12", "Debian GNU/Linux", now)
	if !d.Known || !d.Supported {
		t.Errorf("debian 12: %+v", d)
	}

	// Debian 10: EOL 2024-06 → kritisch.
	d10 := OSSupportStatus("debian", "10", "Debian GNU/Linux", now)
	if d10.Supported || d10.Severity != "critical" {
		t.Errorf("debian 10 sollte EOL/kritisch sein: %+v", d10)
	}

	// Debian 11: Support bis Ende August 2026 (EOL 2026-08). Im letzten Monat
	// davor (August 2026) → noch unterstützt, aber „läuft bald aus" ⇒ kritisch.
	soon := OSSupportStatus("debian", "11", "Debian GNU/Linux", time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	if !soon.Supported || !soon.EOLSoon || soon.Severity != "critical" {
		t.Errorf("debian 11 (August 2026) sollte kurz vor EOL/kritisch sein: %+v", soon)
	}
	// Zwei Monate vor dem Support-Ende (Juli 2026) noch nicht kritisch.
	notyet := OSSupportStatus("debian", "11", "Debian GNU/Linux", time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	if notyet.EOLSoon || notyet.Severity != "" {
		t.Errorf("debian 11 (Juli 2026) sollte noch nicht kritisch sein: %+v", notyet)
	}

	// Distribution aus OSName erraten, wenn ID fehlt.
	guess := OSSupportStatus("", "24.04", "Ubuntu 24.04 LTS", now)
	if !guess.Known || guess.Distro != "Ubuntu" {
		t.Errorf("distro-erkennung aus name fehlgeschlagen: %+v", guess)
	}

	// Unbekannte Distribution / Zero-Zeit ⇒ keine Aussage.
	if OSSupportStatus("arch", "rolling", "Arch Linux", now).Known {
		t.Error("arch sollte nicht bekannt sein")
	}
	if OSSupportStatus("ubuntu", "24.04", "Ubuntu", time.Time{}).Known {
		t.Error("zero-zeit sollte keine bewertung liefern")
	}
}

// TestTrafficLightOSEOL: ein EOL-Betriebssystem erzeugt einen kritischen
// Insight und macht den (erreichbaren) Server rot.
func TestTrafficLightOSEOL(t *testing.T) {
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	s := &Server{Reachable: true, OSID: "ubuntu", OSVersionID: "20.04", OSName: "Ubuntu"}
	status, insights := s.TrafficLight(TrafficLightInput{Now: now})
	if status != ServerStatusRed {
		t.Fatalf("EOL-OS sollte rot sein, ist %q", status)
	}
	found := false
	for _, in := range insights {
		if in.Severity == "critical" && contains(in.Message, "End-of-Life") {
			found = true
		}
	}
	if !found {
		t.Errorf("kritischer EOL-Insight fehlt: %+v", insights)
	}

	// Ohne Now-Bezug bleibt die OS-Bewertung außen vor (grün).
	if st, _ := s.TrafficLight(TrafficLightInput{}); st != ServerStatusGreen {
		t.Errorf("ohne Now sollte kein OS-Insight greifen: %q", st)
	}
}

// TestTrafficLightOSEOLSoon: ein noch unterstütztes, aber in weniger als einem
// Monat auslaufendes Betriebssystem macht den Server ebenfalls rot.
func TestTrafficLightOSEOLSoon(t *testing.T) {
	// Debian 11 läuft Ende August 2026 aus - im August ist das „< 1 Monat".
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	s := &Server{Reachable: true, OSID: "debian", OSVersionID: "11", OSName: "Debian GNU/Linux"}
	status, _ := s.TrafficLight(TrafficLightInput{Now: now})
	if status != ServerStatusRed {
		t.Fatalf("OS kurz vor EOL sollte rot sein, ist %q", status)
	}
}

// TestTrafficLightUnreachableUncritical: ein als „Nichterreichbarkeit
// unkritisch" markierter Server behält innerhalb der Kulanzfrist seinen aus den
// zuletzt erfassten Daten berechneten Status; erst danach wird er rot.
func TestTrafficLightUnreachableUncritical(t *testing.T) {
	now := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	seen := now.AddDate(0, 0, -10) // vor 10 Tagen zuletzt gesehen

	// Standard (ohne Toleranz): nicht erreichbar → rot.
	strict := &Server{Reachable: false, LastSeenAt: &seen, SSHHardened: true, FirewallActive: true}
	if st, _ := strict.TrafficLight(TrafficLightInput{Now: now}); st != ServerStatusRed {
		t.Fatalf("ohne Toleranz sollte nicht erreichbar = rot sein, ist %q", st)
	}

	// Mit Toleranz, innerhalb der 28-Tage-Frist (10 Tage offline): behält Status.
	// Ohne CVEs, SSH gehärtet + Firewall → „Sehr gut" (excellent) bleibt erhalten.
	tol := &Server{Reachable: false, UnreachableUncritical: true, UnreachableGraceDays: 28,
		LastSeenAt: &seen, SSHHardened: true, FirewallActive: true}
	if st, _ := tol.TrafficLight(TrafficLightInput{Now: now, TotalVulns: 0}); st != ServerStatusExcellent {
		t.Fatalf("toleriert & innerhalb Frist sollte Status behalten (excellent), ist %q", st)
	}

	// Nach Ablauf der Frist (offline seit 40 Tagen): rot wegen Nichterreichbarkeit.
	old := now.AddDate(0, 0, -40)
	expired := &Server{Reachable: false, UnreachableUncritical: true, UnreachableGraceDays: 28,
		LastSeenAt: &old, SSHHardened: true, FirewallActive: true}
	if st, _ := expired.TrafficLight(TrafficLightInput{Now: now}); st != ServerStatusRed {
		t.Fatalf("nach Ablauf der Kulanzfrist sollte rot sein, ist %q", st)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestIsLcmHostPort: nur Loopback UND Standard-SSH-Port zählen als LCM-Host -
// ein Port-Forward (127.0.0.1:<hoher Port>) auf eine andere Maschine nicht.
func TestIsLcmHostPort(t *testing.T) {
	cases := []struct {
		host string
		port int
		want bool
	}{
		{"localhost", 22, true},
		{"127.0.0.1", 0, true}, // 0 = Standard 22
		{"::1", 22, true},
		{"127.0.0.1", 2221, false}, // Port-Forward auf Container
		{"localhost", 2222, false},
		{"10.0.0.5", 22, false}, // kein Loopback
	}
	for _, c := range cases {
		s := &Server{Host: c.host, SSHPort: c.port}
		if got := s.IsLcmHost(); got != c.want {
			t.Errorf("IsLcmHost(%s:%d) = %v, erwartet %v", c.host, c.port, got, c.want)
		}
	}
}

// TestRHELFamilieWirdBewertet: Bis hierher schwieg die Ampel zur ganzen
// rpm-Welt - os_support war schlicht null. Ein RHEL 7 ohne Support sah damit
// aus wie ein frisch installiertes RHEL 10.
func TestRHELFamilieWirdBewertet(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		osID, versionID, osName string
		wantDistro              string
		wantSupported           bool
	}{
		// Die Minor-Version steht in VERSION_ID; der Zyklus hängt an der Major.
		{"rhel", "10.2", "Red Hat Enterprise Linux", "Red Hat Enterprise Linux", true},
		{"rhel", "9.5", "Red Hat Enterprise Linux", "Red Hat Enterprise Linux", true},
		{"rhel", "7.9", "Red Hat Enterprise Linux", "Red Hat Enterprise Linux", false},
		{"rocky", "10.0", "Rocky Linux", "Rocky Linux", true},
		{"almalinux", "9.5", "AlmaLinux", "AlmaLinux", true},
		// CentOS Stream läuft dem zugehörigen RHEL voraus und endet früher.
		{"centos", "10", "CentOS Stream", "CentOS Stream", true},
		{"centos", "8", "CentOS Stream", "CentOS Stream", false},
	}
	for _, f := range cases {
		got := OSSupportStatus(f.osID, f.versionID, f.osName, now)
		if !got.Known {
			t.Errorf("%s %s: keine Bewertung", f.osID, f.versionID)
			continue
		}
		if got.Distro != f.wantDistro {
			t.Errorf("%s: Distro = %q, erwartet %q", f.osID, got.Distro, f.wantDistro)
		}
		if got.Supported != f.wantSupported {
			t.Errorf("%s %s: Supported = %v, erwartet %v", f.osID, f.versionID, got.Supported, f.wantSupported)
		}
	}
}

// TestRHELOhneOsIDWirdAmNamenErkannt: Ältere Aufnahmen tragen keine OSID -
// dann bleibt der Name aus /etc/os-release.
func TestRHELOhneOsIDWirdAmNamenErkannt(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	got := OSSupportStatus("", "9.5", "Red Hat Enterprise Linux", now)
	if !got.Known || got.Distro != "Red Hat Enterprise Linux" {
		t.Errorf("am Namen nicht erkannt: %+v", got)
	}
}
