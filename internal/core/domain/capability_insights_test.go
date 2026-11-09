package domain

import (
	"strings"
	"testing"
	"time"
)

var capNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// failedCapability baut eine seit zwei Tagen fehlgeschlagene Fähigkeit.
func failedCapability(category, class string, failures int) ServerCapability {
	since := capNow.Add(-48 * time.Hour)
	return ServerCapability{
		Category: category, State: CapabilityFailed, ConsecutiveFailures: failures,
		FailingSince: &since, LastErrorClass: class, LastJobName: "Nachtlauf",
	}
}

func findInsight(insights []StatusInsight, key string) *StatusInsight {
	for i := range insights {
		if insights[i].Key == key {
			return &insights[i]
		}
	}
	return nil
}

func TestAktionsfehlerFaerbenDieAmpel(t *testing.T) {
	healthy := Server{Reachable: true}
	baseline, _ := healthy.TrafficLight(TrafficLightInput{Now: capNow})
	lock := func(n int) ServerCapability {
		c := failedCapability(CapabilityPackages, ErrClassLockBusy, n)
		c.LastErrorParams = map[string]string{"pid": "1234", "process": "unattended-upgr"}
		return c
	}
	tests := []struct {
		name     string
		in       TrafficLightInput
		status   string
		key      string
		severity string
	}{
		{"Sperre einmal: gelb", TrafficLightInput{Capabilities: []ServerCapability{lock(1)}},
			ServerStatusYellow, "actionFailed.packages", "warning"},
		{"Sperre zweimal: rot", TrafficLightInput{Capabilities: []ServerCapability{lock(2)}},
			ServerStatusRed, "actionFailed.packages", "critical"},
		{"dpkg unterbrochen: sofort rot",
			TrafficLightInput{Capabilities: []ServerCapability{failedCapability(CapabilityPackages, ErrClassDpkgInterrupted, 1)}},
			ServerStatusRed, "actionFailed.packages", "critical"},
		{"Verbindung gescheitert: rot",
			TrafficLightInput{Capabilities: []ServerCapability{failedCapability(CapabilityConnection, ErrClassNetwork, 1)}},
			ServerStatusRed, "actionFailed.connection", "critical"},
		{"Regel gescheitert: gelb",
			TrafficLightInput{Capabilities: []ServerCapability{failedCapability(CapabilityRules, ErrClassUnknown, 5)}},
			ServerStatusYellow, "actionFailed.rules", "warning"},
		{"CVE-Scan gescheitert: gelb",
			TrafficLightInput{Capabilities: []ServerCapability{failedCapability(CapabilityCVEScan, ErrClassScannerUnavailable, 1)}},
			ServerStatusYellow, "actionFailed.cve_scan", "warning"},
		{"Paketstand veraltet: gelb",
			TrafficLightInput{PackagesStale: PackagesStaleWarning, PackagesStaleSince: capNow.Add(-72 * time.Hour)},
			ServerStatusYellow, "packagesStale", "warning"},
		{"Paketstand lange veraltet: rot",
			TrafficLightInput{PackagesStale: PackagesStaleCritical, PackagesStaleSince: capNow.Add(-9 * 24 * time.Hour)},
			ServerStatusRed, "packagesStaleCritical", "critical"},
		{"Paketquelle fehlerhaft: gelb",
			TrafficLightInput{Capabilities: []ServerCapability{
				{Category: CapabilityPackages, State: CapabilityOK, WarningClass: ErrClassRepoError, WarningMessage: "ppa.example/x"}}},
			ServerStatusYellow, "repoWarning", "warning"},
		{"apt-Hinweis: nur Info, Farbe bleibt",
			TrafficLightInput{Capabilities: []ServerCapability{
				{Category: CapabilityPackages, State: CapabilityOK, WarningClass: aptWarningClass, WarningMessage: "W: x"}}},
			baseline, "aptWarning", "info"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Now = capNow
			status, insights := healthy.TrafficLight(tc.in)
			if status != tc.status {
				t.Errorf("Ampel %q, erwartet %q", status, tc.status)
			}
			got := findInsight(insights, tc.key)
			if got == nil {
				t.Fatalf("Befund %q fehlt: %+v", tc.key, insights)
			}
			if got.Severity != tc.severity {
				t.Errorf("Stufe %q, erwartet %q", got.Severity, tc.severity)
			}
		})
	}
}

// Ein gesunder Zustand erzeugt keinen Befund, auch nicht mit Zeilen im
// Zustand "ok".
func TestGesundeFaehigkeitenErzeugenKeinenBefund(t *testing.T) {
	s := Server{Reachable: true}
	var caps []ServerCapability
	for _, category := range CapabilityCategories {
		caps = append(caps, ServerCapability{Category: category, State: CapabilityOK, ConsecutiveFailures: 0})
	}
	want, wantInsights := s.TrafficLight(TrafficLightInput{Now: capNow})
	got, gotInsights := s.TrafficLight(TrafficLightInput{Now: capNow, Capabilities: caps})
	if got != want || len(gotInsights) != len(wantInsights) {
		t.Errorf("mit gesunden Fähigkeiten %q/%d Befunde, ohne %q/%d", got, len(gotInsights), want, len(wantInsights))
	}
}

// Der Fall, der den Umbau auslöste: Ein später erfolgreicher Health-Ping
// heilt nur die Verbindung, nicht die fehlgeschlagene Paketverwaltung.
func TestFehlgeschlagenesUpdateBleibtTrotzSpaeteremHealthPingRot(t *testing.T) {
	s := Server{Reachable: true}
	caps := []ServerCapability{
		{Category: CapabilityConnection, State: CapabilityOK, LastSuccessAt: &capNow},
		failedCapability(CapabilityPackages, ErrClassBrokenDeps, 1),
	}
	status, insights := s.TrafficLight(TrafficLightInput{Now: capNow, Capabilities: caps})
	if status != ServerStatusRed {
		t.Errorf("Ampel %q, erwartet rot", status)
	}
	if findInsight(insights, "actionFailed.connection") != nil {
		t.Error("die gesunde Verbindung darf keinen Befund erzeugen")
	}
}

// Wird die Nichterreichbarkeit geduldet, darf der Verbindungsfehler den Server
// nicht doch rot färben - sonst wäre die Einstellung wirkungslos.
func TestGeduldeteNichterreichbarkeitBleibtNichtRot(t *testing.T) {
	s := Server{Reachable: false, UnreachableUncritical: true}
	caps := []ServerCapability{failedCapability(CapabilityConnection, ErrClassNetwork, 3)}
	status, insights := s.TrafficLight(TrafficLightInput{Now: capNow, Capabilities: caps})
	if status == ServerStatusRed {
		t.Errorf("Ampel rot trotz geduldeter Nichterreichbarkeit: %+v", insights)
	}
	if findInsight(insights, "actionFailed.connection") != nil {
		t.Error("Verbindungsbefund darf bei geduldeter Nichterreichbarkeit nicht erscheinen")
	}
}

func TestAktionsfehlerParameterUndText(t *testing.T) {
	c := failedCapability(CapabilityPackages, ErrClassLockBusy, 3)
	c.LastErrorParams = map[string]string{"pid": "1234", "process": "unattended-upgr", "count": "falsch"}
	in := actionFailedInsight(c, CapabilityCritical, capNow)

	wantParams := map[string]string{
		"count": "3", "causeKey": "lock_busy", "pid": "1234", "process": "unattended-upgr",
		"since": capNow.Add(-48 * time.Hour).Format(time.RFC3339),
	}
	for k, v := range wantParams {
		if in.Params[k] != v {
			t.Errorf("Parameter %s = %q, erwartet %q", k, in.Params[k], v)
		}
	}
	if _, ok := in.Params["job"]; ok {
		t.Error("job gehört nur zu Regeln")
	}
	want := "Updates schlagen seit 2 Tagen fehl (3×): dpkg-Sperre durch unattended-upgr (PID 1234)"
	if in.Message != want {
		t.Errorf("Text %q, erwartet %q", in.Message, want)
	}
}

func TestAktionsfehlerTextJeKategorie(t *testing.T) {
	tests := []struct {
		cap  ServerCapability
		want string
	}{
		{failedCapability(CapabilityConnection, ErrClassNetwork, 2),
			"Verbindung scheitert seit 2 Tagen (2×): Netzwerkproblem - DNS, Proxy bzw. Paket-Cache prüfen"},
		{failedCapability(CapabilityRules, ErrClassTimeout, 1),
			"Regel „Nachtlauf“ schlägt seit 2 Tagen fehl (1×): Zeitüberschreitung - der Vorgang hing und wurde abgebrochen"},
		{failedCapability(CapabilityCVEScan, ErrClassScannerUnavailable, 4),
			"CVE-Scan schlägt seit 2 Tagen fehl (4×): CVE-Scanner nicht verfügbar"},
	}
	for _, tc := range tests {
		if got := actionFailedMessage(tc.cap, capNow); got != tc.want {
			t.Errorf("%s: %q, erwartet %q", tc.cap.Category, got, tc.want)
		}
	}
	if got := actionFailedInsight(tests[1].cap, CapabilityWarning, capNow).Params["job"]; got != "Nachtlauf" {
		t.Errorf("Regel-Befund ohne job-Parameter: %q", got)
	}
}

func TestAktionsfehlerTextOhneZeitbezug(t *testing.T) {
	c := failedCapability(CapabilityPackages, ErrClassUnknown, 1)
	c.LastErrorMessage = "E: etwas Seltsames"
	got := actionFailedMessage(c, time.Time{})
	want := "Updates schlagen fehl (1×): E: etwas Seltsames"
	if got != want {
		t.Errorf("Text %q, erwartet %q", got, want)
	}
}

func TestFehlerursacheJeKlasse(t *testing.T) {
	tests := []struct {
		name   string
		class  string
		params map[string]string
		raw    string
		want   string
	}{
		{"Sperre mit Prozess", ErrClassLockBusy, map[string]string{"pid": "7", "process": "apt"}, "", "dpkg-Sperre durch apt (PID 7)"},
		{"Sperre nur PID", ErrClassLockBusy, map[string]string{"pid": "7"}, "", "dpkg-Sperre durch einen anderen Prozess (PID 7)"},
		{"Sperre ohne Halter", ErrClassLockBusy, nil, "", "dpkg-Sperre: ein anderer Prozess hält die Paketverwaltung fest"},
		{"dpkg unterbrochen", ErrClassDpkgInterrupted, nil, "", "dpkg wurde unterbrochen"},
		{"Abhängigkeiten", ErrClassBrokenDeps, map[string]string{"packages": "a,b"}, "", "Abhängigkeiten nicht erfüllbar: a, b"},
		{"Abhängigkeiten ohne Namen", ErrClassBrokenDeps, nil, "", "Abhängigkeiten sind nicht erfüllbar"},
		{"Platte voll", ErrClassDiskFull, map[string]string{"path": "/var"}, "", "Kein Speicherplatz mehr auf /var"},
		{"Platte voll ohne Pfad", ErrClassDiskFull, nil, "", "Kein Speicherplatz mehr auf dem Server"},
		{"Quelle", ErrClassRepoError, map[string]string{"repo": "ppa.x"}, "", "Paketquelle fehlerhaft: ppa.x"},
		{"Quelle noch nicht gültig", ErrClassRepoError, map[string]string{"repo": "ppa.x", "not_valid_yet": "true"}, "",
			"Paketquelle fehlerhaft: ppa.x (noch nicht gültig - Serverzeit prüfen)"},
		{"Quelle unbekannt", ErrClassRepoError, nil, "", "Eine Paketquelle ist fehlerhaft"},
		{"Netz", ErrClassNetwork, nil, "", "Netzwerkproblem - DNS, Proxy bzw. Paket-Cache prüfen"},
		{"Scanner", ErrClassScannerUnavailable, nil, "", "CVE-Scanner nicht verfügbar"},
		{"unbekannt mit Rohtext", ErrClassUnknown, nil, "Rohtext", "Rohtext"},
		{"leere Klasse ohne Rohtext", "", nil, "", "Ursache unbekannt - Details stehen in der Job-Ausgabe"},
	}
	for _, tc := range tests {
		if got := failureCause(tc.class, tc.params, tc.raw); got != tc.want {
			t.Errorf("%s: %q, erwartet %q", tc.name, got, tc.want)
		}
	}
}

func TestDauerAlsText(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{10 * time.Second, "weniger als einer Minute"},
		{time.Minute, "1 Minute"},
		{5 * time.Minute, "5 Minuten"},
		{time.Hour, "1 Stunde"},
		{23 * time.Hour, "23 Stunden"},
		{24 * time.Hour, "1 Tag"},
		{50 * time.Hour, "2 Tagen"},
		{-time.Hour, "weniger als einer Minute"},
	}
	for _, tc := range tests {
		if got := durationText(tc.d); got != tc.want {
			t.Errorf("%v: %q, erwartet %q", tc.d, got, tc.want)
		}
	}
}

func TestPaketstandBefundText(t *testing.T) {
	in := packagesStaleInsight(PackagesStaleWarning, capNow.Add(-72*time.Hour), capNow)
	want := "Paketstand veraltet seit 3 Tagen - LCM hat die Paketliste nicht mehr erfolgreich aktualisiert"
	if in.Message != want {
		t.Errorf("Text %q, erwartet %q", in.Message, want)
	}
	if !strings.HasPrefix(in.Params["since"], "2026-10-28T12:00:00") {
		t.Errorf("since = %q", in.Params["since"])
	}
}

// Scheitert die Paketverwaltung, ersetzt der Fehlschlag die Quellen-Warnung
// des letzten Erfolgs - sonst stünde dieselbe Quelle zweimal da.
func TestQuellenWarnungEntfaelltBeiFehlschlag(t *testing.T) {
	s := Server{Reachable: true}
	failed := failedCapability(CapabilityPackages, ErrClassRepoError, 1)
	failed.WarningClass, failed.WarningMessage = ErrClassRepoError, "http://example.invalid bookworm"
	_, insights := s.TrafficLight(TrafficLightInput{Now: capNow, Capabilities: []ServerCapability{failed}})
	if findInsight(insights, "repoWarning") != nil {
		t.Error("Quellen-Warnung neben dem Fehlschlag gemeldet")
	}
	if findInsight(insights, "actionFailed.packages") == nil {
		t.Error("Fehlschlag fehlt")
	}
}
