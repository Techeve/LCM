package domain

import (
	"strings"
	"testing"
	"time"
)

func ptr(t time.Time) *time.Time { return &t }

// TestEvaluateCVEDBSchwellen prueft die Frische-Stufen an ihren Grenzen.
// Die Schwellen sind der Kern des Features: Zu streng erzeugt Dauerlaerm,
// zu locker laesst eine verrottete Datenbank als „aktuell" durchgehen.
func TestEvaluateCVEDBSchwellen(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		age  time.Duration
		want string
	}{
		{"frisch geladen", 0, CVEDBFresh},
		{"gestern gebaut", 20 * time.Hour, CVEDBFresh},
		{"knapp unter der Warnschwelle", 47 * time.Hour, CVEDBFresh},
		{"genau 48 Stunden", 48 * time.Hour, CVEDBStale},
		{"drei Tage", 72 * time.Hour, CVEDBStale},
		{"knapp unter kritisch", 7*24*time.Hour - time.Hour, CVEDBStale},
		{"genau sieben Tage", 7 * 24 * time.Hour, CVEDBCritical},
		{"drei Wochen", 21 * 24 * time.Hour, CVEDBCritical},
	}
	for _, c := range cases {
		st := CVEDBStatus{Available: true, UpdatedAt: ptr(now.Add(-c.age))}
		st.EvaluateCVEDB(now)
		if st.Freshness != c.want {
			t.Errorf("%s (Alter %v): Stufe %q, erwartet %q", c.name, c.age, st.Freshness, c.want)
		}
		if want := int(c.age / time.Hour); st.AgeHours != want {
			t.Errorf("%s: AgeHours %d, erwartet %d", c.name, st.AgeHours, want)
		}
	}
}

// TestEvaluateCVEDBUnbekannt: Ohne Scanner, ohne Zeitstempel oder ohne
// Zeitbasis darf NICHT „frisch" herauskommen - sonst waere das Fehlen der
// Information von einer aktuellen Datenbank nicht zu unterscheiden, und genau
// diese Verwechslung soll das Feature verhindern.
func TestEvaluateCVEDBUnbekannt(t *testing.T) {
	now := time.Now()
	cases := map[string]CVEDBStatus{
		"kein Scanner":     {Available: false, UpdatedAt: ptr(now)},
		"kein Zeitstempel": {Available: true},
		"Nullzeitstempel":  {Available: true, UpdatedAt: ptr(time.Time{})},
	}
	for name, st := range cases {
		st.EvaluateCVEDB(now)
		if st.Freshness != CVEDBUnknown {
			t.Errorf("%s: Stufe %q, erwartet %q", name, st.Freshness, CVEDBUnknown)
		}
		if st.IsStale() {
			t.Errorf("%s: unbekannt darf nicht als ueberaltert gelten", name)
		}
	}
	// Ohne Zeitbasis (Zero-Now) bleibt die Bewertung aus - dieselbe
	// Konvention wie bei TrafficLightInput.Now.
	st := CVEDBStatus{Available: true, UpdatedAt: ptr(now.Add(-30 * 24 * time.Hour))}
	st.EvaluateCVEDB(time.Time{})
	if st.Freshness != CVEDBUnknown {
		t.Errorf("ohne Zeitbasis: Stufe %q, erwartet %q", st.Freshness, CVEDBUnknown)
	}
}

// TestEvaluateCVEDBUhrInDerZukunft: Eine schiefe Uhr darf kein negatives
// Alter erzeugen (und schon gar keinen Ueberalterungs-Befund).
func TestEvaluateCVEDBUhrInDerZukunft(t *testing.T) {
	now := time.Now()
	st := CVEDBStatus{Available: true, UpdatedAt: ptr(now.Add(6 * time.Hour))}
	st.EvaluateCVEDB(now)
	if st.Freshness != CVEDBFresh || st.AgeHours != 0 {
		t.Errorf("Zukunfts-Zeitstempel: Stufe %q, Alter %d - erwartet fresh/0", st.Freshness, st.AgeHours)
	}
}

// TestIsStale trennt die beiden ueberalterten Stufen von den uebrigen.
func TestIsStale(t *testing.T) {
	stale := map[string]bool{
		CVEDBFresh: false, CVEDBUnknown: false,
		CVEDBStale: true, CVEDBCritical: true,
	}
	for freshness, want := range stale {
		if got := (CVEDBStatus{Freshness: freshness}).IsStale(); got != want {
			t.Errorf("IsStale(%q) = %v, erwartet %v", freshness, got, want)
		}
	}
}

func TestAgeDescription(t *testing.T) {
	cases := map[int]string{
		0: "vor weniger als einer Stunde", 5: "vor 5 Stunden",
		48: "vor 2 Tagen", 170: "vor 7 Tagen",
	}
	for hours, want := range cases {
		if got := (CVEDBStatus{AgeHours: hours}).AgeDescription(); got != want {
			t.Errorf("AgeDescription(%dh) = %q, erwartet %q", hours, got, want)
		}
	}
}

// TestStaleDBFaerbtAmpelNicht ist der Kern der Vorgabe: Eine ueberalterte
// Datenbank ist ein HINWEIS, kein Befund. Sie darf weder aus „Sehr gut" ein
// „OK" machen noch aus „OK" ein „Gelb" - sonst faerbte eine einzige Ursache
// auf dem LCM-Host die gesamte Serverliste um.
func TestStaleDBFaerbtAmpelNicht(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	stale := CVEDBStatus{Available: true, UpdatedAt: ptr(now.Add(-10 * 24 * time.Hour))}
	stale.EvaluateCVEDB(now)

	// Makelloser Server: bleibt „Sehr gut", bekommt aber den Hinweis.
	perfect := Server{Reachable: true, SSHHardened: true, FirewallActive: true}
	base := TrafficLightInput{Now: now, TotalVulns: 0}
	statusOhne, _ := perfect.TrafficLight(base)
	withDB := base
	withDB.CVEDB = stale
	statusMit, insightsMit := perfect.TrafficLight(withDB)
	if statusOhne != ServerStatusExcellent || statusMit != statusOhne {
		t.Errorf("Ampel aenderte sich durch die alte Datenbank: %q -> %q", statusOhne, statusMit)
	}
	if len(insightsMit) != 1 || insightsMit[0].Severity != "info" {
		t.Fatalf("erwartet genau ein Info-Hinweis, bekam %+v", insightsMit)
	}
	if !strings.Contains(insightsMit[0].Message, "vor 10 Tagen") {
		t.Errorf("Hinweis nennt das Alter nicht: %q", insightsMit[0].Message)
	}

	// Server mit echtem Handlungsbedarf: bleibt gelb, Hinweis kommt dazu.
	yellowIn := TrafficLightInput{Now: now, OutdatedPackages: 3, CVEDB: stale}
	server := Server{Reachable: true}
	status, insights := server.TrafficLight(yellowIn)
	if status != ServerStatusYellow {
		t.Errorf("Status %q, erwartet gelb", status)
	}
	var infos, actionable int
	for _, i := range insights {
		if i.Severity == "info" {
			infos++
		} else {
			actionable++
		}
	}
	if infos != 1 || actionable != 1 {
		t.Errorf("erwartet 1 Hinweis + 1 Befund, bekam %d/%d: %+v", infos, actionable, insights)
	}
}

// TestFrischeDBOhneHinweis: Bei aktueller Datenbank darf nichts erscheinen -
// ein Dauerhinweis wuerde abstumpfen und den Ernstfall mit verdecken.
func TestFrischeDBOhneHinweis(t *testing.T) {
	now := time.Now()
	fresh := CVEDBStatus{Available: true, UpdatedAt: ptr(now.Add(-3 * time.Hour))}
	fresh.EvaluateCVEDB(now)
	server := Server{Reachable: true, SSHHardened: true, FirewallActive: true}
	status, insights := server.TrafficLight(TrafficLightInput{Now: now, CVEDB: fresh})
	if status != ServerStatusExcellent || len(insights) != 0 {
		t.Errorf("frische Datenbank erzeugte Befunde: %q %+v", status, insights)
	}
}
