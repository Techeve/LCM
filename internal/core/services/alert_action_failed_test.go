package services_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// rawOutput steht als Rohtext im Fehlerzustand. Er darf in keinem Alarm
// auftauchen.
const rawOutput = "E: Unable to locate /home/ops/secret-repo token=hunter2"

type actionFailedEnv struct {
	*alertEnv
	caps *repositories.CapabilityRepository
}

func newActionFailedEnv(t *testing.T) *actionFailedEnv {
	t.Helper()
	env := newAlertEnv(t)
	caps := repositories.NewCapabilityRepository(env.db)
	env.alerts.WithCapabilities(caps)
	return &actionFailedEnv{alertEnv: env, caps: caps}
}

// rule legt eine aktive action_failed-Regel an (Cooldown 360 Minuten).
func (e *actionFailedEnv) rule(t *testing.T, mut func(*services.AlertRuleInput)) *domain.AlertRule {
	t.Helper()
	in := services.AlertRuleInput{
		Name: "Aktion fehlgeschlagen", Type: domain.AlertTypeActionFailed, Enabled: true,
		Severity: domain.AlertSeverityCritical, CooldownMinutes: 360,
	}
	if mut != nil {
		mut(&in)
	}
	rule, err := e.alerts.Create(in, "admin")
	if err != nil {
		t.Fatalf("Regel anlegen: %v", err)
	}
	return rule
}

// failed ist eine fehlgeschlagene Kategorie mit Rohtext.
func failed(serverID uint, category, class string) domain.ServerCapability {
	since := time.Now().Add(-2 * time.Hour)
	return domain.ServerCapability{
		ServerID: serverID, Category: category, State: domain.CapabilityFailed,
		FailingSince: &since, ConsecutiveFailures: 3,
		LastErrorClass: class, LastErrorMessage: rawOutput,
		LastJobName: "Updates einspielen",
	}
}

func (e *actionFailedEnv) save(t *testing.T, c domain.ServerCapability) {
	t.Helper()
	if err := e.caps.Save(&c); err != nil {
		t.Fatalf("Zustand speichern: %v", err)
	}
}

// seedEvent legt ein früheres Ereignis an (für Sperrfrist und Erinnerung).
func (e *actionFailedEnv) seedEvent(t *testing.T, rule *domain.AlertRule, server *domain.Server, code string, age time.Duration) {
	t.Helper()
	ev := &domain.AlertEvent{
		CreatedAt: time.Now().Add(-age), RuleID: rule.ID, ServerID: &server.ID,
		RuleName: rule.Name, ServerName: server.Name, Type: rule.Type,
		Severity: rule.Severity, Code: code,
	}
	if err := e.alertRepo.CreateEvent(ev); err != nil {
		t.Fatal(err)
	}
}

func (e *actionFailedEnv) transition(c domain.ServerCapability, recovered bool) {
	e.alerts.OnCapabilityTransition(c.ServerID, c, recovered)
	e.alerts.WaitForNotifications()
}

func (e *actionFailedEnv) eventsWithCode(t *testing.T, code string) []domain.AlertEvent {
	t.Helper()
	all, err := e.alerts.ListEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.AlertEvent
	for _, ev := range all {
		if ev.Code == code {
			out = append(out, ev)
		}
	}
	return out
}

func (e *actionFailedEnv) eventCount(t *testing.T) int {
	t.Helper()
	all, _ := e.alerts.ListEvents(100)
	return len(all)
}

func TestActionFailedUebergangLoestTrotzSperrfristAus(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	rule := env.rule(t, nil)
	// Ein junges Ereignis einer ANDEREN Kategorie und ein junges Ereignis
	// derselben: Der Wechsel selbst ist die Nachricht und umgeht die Sperrfrist.
	env.seedEvent(t, rule, server, "action_failed:rules", time.Minute)
	env.seedEvent(t, rule, server, "action_failed:packages", time.Minute)

	env.transition(failed(server.ID, domain.CapabilityPackages, domain.ErrClassBrokenDeps), false)

	events := env.eventsWithCode(t, "action_failed:packages")
	if len(events) != 2 {
		t.Fatalf("der Übergang muss sofort ein Ereignis anlegen: %d Ereignisse mit dem Code", len(events))
	}
	if !strings.Contains(events[0].Description, "web01") || events[0].Severity != domain.AlertSeverityCritical {
		t.Errorf("Ereignis unvollständig: %+v", events[0])
	}
}

func TestActionFailedEntdoppelungJeKategorie(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	env.rule(t, nil)
	packages := failed(server.ID, domain.CapabilityPackages, domain.ErrClassDiskFull)
	rules := failed(server.ID, domain.CapabilityRules, domain.ErrClassUnknown)
	env.save(t, packages)
	env.save(t, rules)

	env.transition(packages, false)
	env.transition(rules, false)

	if n := len(env.eventsWithCode(t, "action_failed:packages")); n != 1 {
		t.Errorf("packages: %d Ereignisse, erwartet 1", n)
	}
	if n := len(env.eventsWithCode(t, "action_failed:rules")); n != 1 {
		t.Errorf("rules: %d Ereignisse, erwartet 1", n)
	}
	// Die halbstündige Auswertung direkt danach erinnert nicht: beide
	// Kategorien liegen innerhalb der Sperrfrist.
	summary, err := env.alerts.Evaluate("test")
	if err != nil {
		t.Fatal(err)
	}
	if env.eventCount(t) != 2 || !strings.Contains(summary, "2 wegen Sperrfrist unterdrückt") {
		t.Errorf("keine Erinnerung innerhalb der Sperrfrist erwartet: %d Ereignisse, %q", env.eventCount(t), summary)
	}
}

func TestActionFailedKeineErinnerungInnerhalbDerSperrfrist(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	rule := env.rule(t, nil) // 360 Minuten
	env.save(t, failed(server.ID, domain.CapabilityPackages, domain.ErrClassNetwork))
	env.seedEvent(t, rule, server, "action_failed:packages", time.Hour)
	// Ein junges Ereignis eines anderen Codes sperrt diese Kategorie nicht.
	env.seedEvent(t, rule, server, "action_failed:rules", time.Minute)

	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if n := len(env.eventsWithCode(t, "action_failed:packages")); n != 1 {
		t.Errorf("vor Ablauf der Sperrfrist keine Erinnerung: %d Ereignisse", n)
	}
}

func TestActionFailedErinnerungNachSperrfrist(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	rule := env.rule(t, nil) // 360 Minuten
	env.save(t, failed(server.ID, domain.CapabilityPackages, domain.ErrClassNetwork))
	env.seedEvent(t, rule, server, "action_failed:packages", 7*time.Hour)

	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	env.alerts.WaitForNotifications()
	if n := len(env.eventsWithCode(t, "action_failed:packages")); n != 2 {
		t.Errorf("nach Ablauf der Sperrfrist muss erinnert werden: %d Ereignisse", n)
	}
}

func TestActionFailedErinnerungOhneErstesEreignis(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	env.rule(t, nil)
	env.save(t, failed(server.ID, domain.CapabilityConnection, domain.ErrClassNetwork))

	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if n := len(env.eventsWithCode(t, "action_failed:connection")); n != 1 {
		t.Errorf("ein bestehender Fehler ohne Ereignis muss gemeldet werden: %d", n)
	}
}

func TestActionFailedEntwarnungBeiGenesung(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	env.rule(t, nil)

	healed := domain.ServerCapability{ServerID: server.ID, Category: domain.CapabilityPackages, State: domain.CapabilityOK}
	env.transition(healed, true)

	events := env.eventsWithCode(t, "action_failed:packages:recovered")
	if len(events) != 1 {
		t.Fatalf("erwartet 1 Entwarnung, bekam %d", len(events))
	}
	if events[0].Severity != domain.AlertSeverityInfo {
		t.Errorf("die Entwarnung ist Info, nicht %q", events[0].Severity)
	}
	if want := "Entwarnung: Updates laufen wieder (web01)"; events[0].Description != want {
		t.Errorf("Beschreibung %q, erwartet %q", events[0].Description, want)
	}
	// Die Entwarnung sperrt eine spätere Erinnerung nicht.
	if n := len(env.eventsWithCode(t, "action_failed:packages")); n != 0 {
		t.Errorf("die Entwarnung darf kein Fehler-Ereignis sein: %d", n)
	}
}

// TestActionFailedUebergaengeKommenInReihenfolge: Wechsel, die kurz
// hintereinander eintreffen, werden in dieser Reihenfolge verarbeitet - sonst
// käme die Entwarnung vor dem Alarm, und der Empfänger hielte den Fehler für
// bestehend.
func TestActionFailedUebergaengeKommenInReihenfolge(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	env.rule(t, nil)
	down := failed(server.ID, domain.CapabilityPackages, domain.ErrClassBrokenDeps)
	up := domain.ServerCapability{ServerID: server.ID, Category: domain.CapabilityPackages, State: domain.CapabilityOK}

	const rounds = 20
	for i := 0; i < rounds; i++ {
		env.alerts.OnCapabilityTransition(server.ID, down, false)
		env.alerts.OnCapabilityTransition(server.ID, up, true)
	}
	env.alerts.WaitForNotifications()

	var events []domain.AlertEvent
	if err := env.db.Order("rowid").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2*rounds {
		t.Fatalf("%d Ereignisse, erwartet %d", len(events), 2*rounds)
	}
	for i, ev := range events {
		want := "action_failed:packages"
		if i%2 == 1 {
			want += ":recovered"
		}
		if ev.Code != want {
			t.Fatalf("Ereignis %d hat Code %q, erwartet %q", i, ev.Code, want)
		}
	}
}

func TestActionFailedRegelMitFremderGruppeSchweigt(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	other := env.createServer(t, "db01", nil)
	group := &domain.ServerGroup{Name: "Datenbanken"}
	if err := env.groups.Create(group); err != nil {
		t.Fatal(err)
	}
	if err := env.groups.AddServer(group, other); err != nil {
		t.Fatal(err)
	}
	env.rule(t, func(in *services.AlertRuleInput) { in.GroupIDs = []uint{group.ID} })
	c := failed(server.ID, domain.CapabilityPackages, domain.ErrClassNetwork)
	env.save(t, c)

	env.transition(c, false)
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if n := env.eventCount(t); n != 0 {
		t.Fatalf("Server außerhalb der Regelgruppe: %d Ereignisse, erwartet 0", n)
	}

	// Gegenprobe: das Gruppenmitglied wird gemeldet.
	member := failed(other.ID, domain.CapabilityPackages, domain.ErrClassNetwork)
	env.transition(member, false)
	if n := env.eventCount(t); n != 1 {
		t.Errorf("Gruppenmitglied: %d Ereignisse, erwartet 1", n)
	}
}

func TestActionFailedWartungUndDemoSchweigen(t *testing.T) {
	env := newActionFailedEnv(t)
	maintenance := env.createServer(t, "wartung", func(s *domain.Server) { s.Maintenance = true })
	demo := env.createServer(t, "demo", func(s *domain.Server) { s.IsDemo = true })
	env.rule(t, nil)
	for _, s := range []*domain.Server{maintenance, demo} {
		c := failed(s.ID, domain.CapabilityPackages, domain.ErrClassNetwork)
		env.save(t, c)
		env.transition(c, false)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if n := env.eventCount(t); n != 0 {
		t.Errorf("Wartung und Demo dürfen nicht alarmieren: %d Ereignisse", n)
	}
}

func TestActionFailedLockBusyErstAbDemZweitenFehlschlag(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	env.rule(t, nil)
	c := failed(server.ID, domain.CapabilityPackages, domain.ErrClassLockBusy)
	c.ConsecutiveFailures = 1
	env.save(t, c)

	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if n := env.eventCount(t); n != 0 {
		t.Errorf("eine einmal belegte Sperre ist keinen Alarm wert: %d", n)
	}
}

func TestActionFailedStillstand(t *testing.T) {
	cases := []struct {
		name      string
		sinceDays float64
		want      int
	}{
		{"rot nach mehr als 7 Tagen", 10, 1},
		{"gelb bleibt ein Hinweis der Ampel", 3, 0},
		{"frisch", 0.1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newActionFailedEnv(t)
			server := env.createServer(t, "web01", nil)
			env.rule(t, nil)
			last := time.Now().Add(-time.Duration(tc.sinceDays * 24 * float64(time.Hour)))
			env.save(t, domain.ServerCapability{
				ServerID: server.ID, Category: domain.CapabilityPackages,
				State: domain.CapabilityOK, LastSuccessAt: &last,
			})

			if _, err := env.alerts.Evaluate("test"); err != nil {
				t.Fatal(err)
			}
			if n := len(env.eventsWithCode(t, "action_failed:packages_stale")); n != tc.want {
				t.Errorf("%d Stillstand-Ereignisse, erwartet %d", n, tc.want)
			}
		})
	}
}

func TestActionFailedStillstandOhnePaketzeileZaehltAbAnlage(t *testing.T) {
	env := newActionFailedEnv(t)
	env.createServer(t, "alt", func(s *domain.Server) { s.CreatedAt = time.Now().Add(-30 * 24 * time.Hour) })
	env.createServer(t, "neu", nil)
	env.rule(t, nil)

	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events := env.eventsWithCode(t, "action_failed:packages_stale")
	if len(events) != 1 || events[0].ServerName != "alt" {
		t.Errorf("nur der alte Server ohne Erfolg ist im Stillstand: %+v", events)
	}
}

func TestActionFailedStillstandDoppeltNichtEinenGemeldetenFehler(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	env.rule(t, nil)
	c := failed(server.ID, domain.CapabilityPackages, domain.ErrClassBrokenDeps)
	last := time.Now().Add(-10 * 24 * time.Hour)
	c.LastSuccessAt = &last
	env.save(t, c)

	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if n := len(env.eventsWithCode(t, "action_failed:packages_stale")); n != 0 {
		t.Errorf("der Stillstand wiederholt einen schon gemeldeten Fehler: %d", n)
	}
	if n := len(env.eventsWithCode(t, "action_failed:packages")); n != 1 {
		t.Errorf("der Fehler selbst wird gemeldet: %d", n)
	}
}

// TestActionFailedMeldetKeinenRohtext: Alarme tragen Kategorie, Klasse und
// Hinweis - nie die Ausgabe des Servers (Pfade, Hostnamen, Zugangsdaten).
func TestActionFailedMeldetKeinenRohtext(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)

	var mu sync.Mutex
	var bodies []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
	}))
	defer hook.Close()
	channel, err := env.notification.Create(services.ChannelInput{
		Name: "Hook", Type: domain.ChannelTypeWebhook, Enabled: true,
		Config: `{"format":"generic"}`, Secret: hook.URL,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// Cooldown 0: Die Auswertung erinnert sofort, beide Wege werden geprüft.
	env.rule(t, func(in *services.AlertRuleInput) { in.ChannelID, in.CooldownMinutes = &channel.ID, 0 })

	// Unbekannte Klasse: Die Ursache fällt sonst auf den Rohtext zurück.
	c := failed(server.ID, domain.CapabilityPackages, domain.ErrClassUnknown)
	env.save(t, c)
	env.transition(c, false)                               // sofort
	if _, err := env.alerts.Evaluate("test"); err != nil { // Erinnerung
		t.Fatal(err)
	}
	env.alerts.WaitForNotifications()

	events, _ := env.alerts.ListEvents(10)
	if len(events) == 0 {
		t.Fatal("kein Ereignis")
	}
	for _, ev := range events {
		if strings.Contains(ev.Description, "hunter2") || strings.Contains(ev.Description, "secret-repo") {
			t.Errorf("Rohtext im Ereignis: %q", ev.Description)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("keine Benachrichtigung angekommen")
	}
	for _, body := range bodies {
		if strings.Contains(body, "hunter2") || strings.Contains(body, "secret-repo") || strings.Contains(body, "Unable to locate") {
			t.Errorf("Rohtext in der Benachrichtigung: %s", body)
		}
	}
	if !strings.Contains(bodies[0], "Fehlerklasse: unknown") || !strings.Contains(bodies[0], "Updates einspielen") {
		t.Errorf("Details nennen Klasse und Job nicht: %s", bodies[0])
	}
}

func TestActionFailedLockBusyNenntNurNamenUndPID(t *testing.T) {
	env := newActionFailedEnv(t)
	server := env.createServer(t, "web01", nil)
	env.rule(t, nil)
	c := failed(server.ID, domain.CapabilityPackages, domain.ErrClassLockBusy)
	c.LastErrorParams = map[string]string{"process": "unattended-upgr", "pid": "1234"}
	env.transition(c, false)

	events := env.eventsWithCode(t, "action_failed:packages")
	if len(events) != 1 || !strings.Contains(events[0].Description, "unattended-upgr (PID 1234)") {
		t.Errorf("Sperre mit Name und PID erwartet: %+v", events)
	}
}
