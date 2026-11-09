package controllers

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/events"
	"LCM/internal/storage/repositories"
)

// Die Tests hier prüfen den Hub ohne Datenbank: Anmeldung und Sichtbarkeit
// stellen Funktionen, die der Test steuert. Der Weg über die echte Anmeldung
// steht in router/events_test.go.

// hubFixture ist ein Hub hinter einem echten Listener. Eine Middleware hängt
// den Test-User an, wie es middlewares.Authenticate täte.
type hubFixture struct {
	hub     *EventHub
	bus     *events.Bus
	addr    string
	user    atomic.Pointer[domain.User]
	valid   atomic.Bool // Ergebnis der erneuten Anmeldungsprüfung
	visible atomic.Pointer[[]uint]
}

func permsUser(id uint, role string, perms ...string) *domain.User {
	r := domain.Role{Name: role}
	for _, p := range perms {
		r.Permissions = append(r.Permissions, domain.Permission{Code: p})
	}
	return &domain.User{ID: id, Username: "u" + strconv.Itoa(int(id)), Active: true, Roles: []domain.Role{r}}
}

func adminUser() *domain.User {
	return permsUser(1, domain.RoleAdmin, domain.PermServersRead, domain.PermJobsRead,
		domain.PermAlertsManage, domain.PermPackagesRead, domain.PermSettingsManage)
}

func managerUser(id uint) *domain.User {
	return permsUser(id, domain.RoleManager, domain.ManagerPermissions()...)
}

func newHubFixture(t *testing.T, user *domain.User) *hubFixture {
	t.Helper()
	f := &hubFixture{bus: events.NewBus()}
	f.user.Store(user)
	f.valid.Store(true)
	none := []uint{}
	f.visible.Store(&none)

	f.hub = newEventHub(f.bus,
		func(credential) (*domain.User, error) {
			if !f.valid.Load() {
				return nil, errors.New("ungültig")
			}
			return f.user.Load(), nil
		},
		func(repositories.AccessScope) ([]uint, error) { return *f.visible.Load(), nil })
	f.hub.pingEvery = time.Hour
	f.hub.revalidateEvery = time.Hour

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("auth.user", f.user.Load())
		c.Locals("auth.apikey", "lcm_test") // ohne Origin zugelassen
		return c.Next()
	})
	app.Get("/events", f.hub.Connect)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.addr = ln.Addr().String()
	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() {
		_ = f.hub.Close()
		_ = app.Shutdown()
	})
	return f
}

// client ist die Gegenseite: Eine Goroutine liest alle Rahmen in einen Kanal.
// Nötig, weil nach einem Lesefehler (auch einem Zeitablauf) jedes weitere
// Lesen auf derselben Verbindung scheitert.
type client struct {
	conn   *websocket.Conn
	frames chan map[string]any
	end    chan error // Grund des Endes, nachdem frames geschlossen ist
}

func (f *hubFixture) dial(t *testing.T) *client {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+f.addr+"/events", http.Header{"X-API-Key": {"x"}})
	if err != nil {
		t.Fatalf("verbinden: %v (status %v)", err, resp)
	}
	c := &client{conn: conn, frames: make(chan map[string]any, 1024), end: make(chan error, 1)}
	go func() {
		defer close(c.frames)
		for {
			var fr map[string]any
			if err := conn.ReadJSON(&fr); err != nil {
				c.end <- err
				return
			}
			c.frames <- fr
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return c
}

// next liefert den nächsten Rahmen.
func (c *client) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case fr, ok := <-c.frames:
		if !ok {
			t.Fatal("Verbindung beendet, kein Rahmen mehr")
		}
		return fr
	case <-time.After(3 * time.Second):
		t.Fatal("kein Rahmen angekommen")
		return nil
	}
}

// nextType liest, bis ein Rahmen dieser Art kommt.
func (c *client) nextType(t *testing.T, typ string) map[string]any {
	t.Helper()
	for range 200 {
		if fr := c.next(t); fr["type"] == typ {
			return fr
		}
	}
	t.Fatalf("kein Rahmen der Art %q", typ)
	return nil
}

// silent prüft, dass in der Frist kein Rahmen (außer Pings) ankommt.
func (c *client) silent(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case fr, ok := <-c.frames:
			if ok && fr["type"] != "ping" {
				t.Fatalf("unerwarteter Rahmen: %v", fr)
			}
			if !ok {
				t.Fatal("Verbindung unerwartet beendet")
			}
		case <-deadline:
			return
		}
	}
}

func (c *client) send(t *testing.T, v any) {
	t.Helper()
	if err := c.conn.WriteJSON(v); err != nil {
		t.Fatal(err)
	}
}

func (c *client) subscribe(t *testing.T, topics ...string) map[string]any {
	t.Helper()
	c.send(t, map[string]any{"type": "subscribe", "topics": topics})
	return c.nextType(t, "subscribed")
}

// closeCode wartet auf das Ende der Verbindung und liefert den Schließcode.
func (c *client) closeCode(t *testing.T) int {
	t.Helper()
	for {
		select {
		case _, ok := <-c.frames:
			if ok {
				continue
			}
			var ce *websocket.CloseError
			if err := <-c.end; errors.As(err, &ce) {
				return ce.Code
			} else {
				t.Fatalf("Verbindung ohne Schließcode beendet: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("die Verbindung wurde nicht geschlossen")
		}
	}
}

func ptr[T any](v T) *T { return &v }

const someJob = "11111111-1111-1111-1111-111111111111"

func TestHubHelloUndAbonnieren(t *testing.T) {
	f := newHubFixture(t, adminUser())
	f.hub.WithBuildInfo(func() (string, string) { return "1.2.3", "77" })
	c := f.dial(t)

	hello := c.next(t)
	if hello["type"] != "hello" || hello["version"] != "1.2.3" || hello["build"] != "77" {
		t.Errorf("hello: %v", hello)
	}
	ack := c.subscribe(t, "servers", "jobs")
	if topics := ack["topics"].([]any); len(topics) != 2 || topics[0] != "jobs" || topics[1] != "servers" {
		t.Errorf("subscribed: %v", ack)
	}

	f.bus.Publish(events.ServerBusy(events.ServerBusyData{ServerID: 5, Busy: true, JobID: "j", Kind: "update", Queued: 2}))
	ev := c.nextType(t, "event")
	data := ev["data"].(map[string]any)
	// Das Ereignis gehört zu „servers" und „jobs": ein Rahmen, bei Gleichstand
	// das Thema mit dem kleineren Namen.
	if ev["event"] != "server.busy" || ev["topic"] != "jobs" || data["server_id"] != float64(5) || data["queued"] != float64(2) {
		t.Errorf("event: %v", ev)
	}
	c.silent(t, 100*time.Millisecond) // nicht doppelt

	c.send(t, map[string]any{"type": "unsubscribe", "topics": []string{"servers", "jobs"}})
	c.nextType(t, "subscribed")
	f.bus.Publish(events.ServerStatus(nil))
	c.silent(t, 150*time.Millisecond)
}

func TestHubSpezifischstesThemaGewinnt(t *testing.T) {
	f := newHubFixture(t, adminUser())
	c := f.dial(t)
	c.subscribe(t, "servers", "server:5", "dashboard")

	f.bus.Publish(events.ServerStatus(ptr(uint(5))))
	if ev := c.nextType(t, "event"); ev["topic"] != "server:5" {
		t.Errorf("topic = %v, erwartet server:5", ev["topic"])
	}
	c.silent(t, 100*time.Millisecond) // nicht dreimal
}

func TestHubFehlerrahmen(t *testing.T) {
	f := newHubFixture(t, managerUser(2)) // Manager: keine alerts
	c := f.dial(t)
	c.send(t, map[string]any{"type": "subscribe",
		"topics": []string{"bogus", "server:0", "job:keine-uuid", "alerts", "servers"}})

	want := []string{"events.bad_topic@bogus", "events.bad_topic@server:0", "events.bad_topic@job:keine-uuid",
		"events.forbidden_topic@alerts"}
	for _, w := range want {
		e := c.nextType(t, "error")
		if got := e["code"].(string) + "@" + e["topic"].(string); got != w {
			t.Errorf("Fehler %s, erwartet %s", got, w)
		}
	}
	if ack := c.nextType(t, "subscribed"); len(ack["topics"].([]any)) != 1 {
		t.Errorf("nur servers hätte angenommen werden dürfen: %v", ack)
	}

	c.send(t, map[string]any{"type": "quatsch"})
	if e := c.nextType(t, "error"); e["code"] != "events.bad_message" {
		t.Errorf("unbekannte Art: %v", e)
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, []byte("{kaputt")); err != nil {
		t.Fatal(err)
	}
	if e := c.nextType(t, "error"); e["code"] != "events.bad_message" {
		t.Errorf("kaputtes JSON: %v", e)
	}
}

func TestHubThemenGrenze(t *testing.T) {
	f := newHubFixture(t, adminUser())
	c := f.dial(t)

	first := make([]string, maxEventTopics)
	second := make([]string, maxEventTopics)
	for i := range maxEventTopics {
		first[i] = "server:" + strconv.Itoa(i+1)
		second[i] = "server:" + strconv.Itoa(maxEventTopics+i+1)
	}
	if ack := c.subscribe(t, first...); len(ack["topics"].([]any)) != maxEventTopics {
		t.Fatalf("erste Runde: %v", ack)
	}

	// Die zweite Runde ist über der Grenze: jedes Thema bekommt einen Fehler,
	// die Liste bleibt bei 64.
	c.send(t, map[string]any{"type": "subscribe", "topics": second})
	rejected := 0
	for {
		fr := c.next(t)
		if fr["type"] == "error" && fr["code"] == "events.too_many_topics" {
			rejected++
		}
		if fr["type"] == "subscribed" {
			if n := len(fr["topics"].([]any)); n != maxEventTopics {
				t.Errorf("Themen nach der zweiten Runde: %d", n)
			}
			break
		}
	}
	if rejected != maxEventTopics {
		t.Errorf("abgewiesen: %d, erwartet %d", rejected, maxEventTopics)
	}

	// Mehr als 64 Themen in EINER Nachricht ist schon eine ungültige Nachricht.
	c.send(t, map[string]any{"type": "subscribe", "topics": append(first, "servers")})
	if e := c.nextType(t, "error"); e["code"] != "events.bad_message" {
		t.Errorf("zu lange Liste: %v", e)
	}
}

func TestHubZuGrosseNachrichtSchliesst(t *testing.T) {
	f := newHubFixture(t, adminUser())
	c := f.dial(t)
	big := make([]byte, maxEventMessageBytes+100)
	for i := range big {
		big[i] = 'a'
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, big); err != nil {
		t.Fatal(err)
	}
	if code := c.closeCode(t); code != websocket.CloseMessageTooBig {
		t.Errorf("Schließcode %d, erwartet %d", code, websocket.CloseMessageTooBig)
	}
}

// Mandantentrennung: Ein Manager bekommt nur Ereignisse seiner Server. Einen
// fremden Server kann er weder abonnieren, noch erreichen ihn dessen Ereignisse
// über „servers", „jobs" oder „dashboard".
func TestHubMandantentrennung(t *testing.T) {
	f := newHubFixture(t, managerUser(2))
	f.visible.Store(&[]uint{10})
	c := f.dial(t)

	if ack := c.subscribe(t, "servers", "jobs", "dashboard", "server:10"); len(ack["topics"].([]any)) != 4 {
		t.Fatalf("eigener Server: %v", ack)
	}
	c.send(t, map[string]any{"type": "subscribe", "topics": []string{"server:20"}})
	if e := c.nextType(t, "error"); e["code"] != "events.forbidden_topic" {
		t.Errorf("fremden Server abonnieren: %v", e)
	}
	c.nextType(t, "subscribed")

	// Fremde Ereignisse jeder Art: nichts kommt an.
	f.bus.Publish(events.ServerBusy(events.ServerBusyData{ServerID: 20, Busy: true}))
	f.bus.Publish(events.ServerStatus(ptr(uint(20))))
	f.bus.Publish(events.JobProgress(someJob, ptr(uint(20))))
	f.bus.Publish(events.JobFinished(someJob, ptr(uint(20)), "success"))
	f.bus.Publish(events.JobFinished(someJob, nil, "success")) // System-Job: nur für Admins
	f.bus.Publish(events.AlertFired(events.AlertFiredData{ID: "a", ServerID: ptr(uint(20))}))
	c.silent(t, 200*time.Millisecond)

	// Eigene Ereignisse kommen an.
	f.bus.Publish(events.ServerBusy(events.ServerBusyData{ServerID: 10, Busy: true}))
	if ev := c.nextType(t, "event"); ev["topic"] != "server:10" {
		t.Errorf("eigener Server: %v", ev)
	}
}

// Auch ein Thema mit eigener Kennung öffnet nichts: job:<id> eines fremden Jobs
// meldet nichts, solange dessen Server nicht sichtbar ist.
func TestHubJobThemaIstGefiltert(t *testing.T) {
	f := newHubFixture(t, managerUser(2))
	f.visible.Store(&[]uint{10})
	c := f.dial(t)
	c.subscribe(t, "job:"+someJob)

	f.bus.Publish(events.JobFinished(someJob, ptr(uint(20)), "failed"))
	c.silent(t, 150*time.Millisecond)
	f.bus.Publish(events.JobFinished(someJob, ptr(uint(10)), "success"))
	if ev := c.nextType(t, "event"); ev["topic"] != "job:"+someJob {
		t.Errorf("eigener Job: %v", ev)
	}
}

// Admins sehen auch Ereignisse ohne Server (System-Jobs, Selbstbeobachtung).
func TestHubAdminSiehtServerloses(t *testing.T) {
	f := newHubFixture(t, adminUser())
	c := f.dial(t)
	c.subscribe(t, "jobs", "alerts", "bulk", "system")

	f.bus.Publish(events.JobFinished(someJob, nil, "success"))
	f.bus.Publish(events.AlertFired(events.AlertFiredData{ID: "a", Type: "backup_stale"}))
	f.bus.Publish(events.BulkProgress(events.BulkProgressData{Running: true, Total: 3}))
	f.bus.Publish(events.SystemBuild(events.SystemBuildData{Phase: "running"}))
	for _, want := range []string{"job.finished", "alert.fired", "bulk.progress", "system.build"} {
		if ev := c.nextType(t, "event"); ev["event"] != want {
			t.Errorf("Ereignis %v, erwartet %s", ev["event"], want)
		}
	}
}

// Ohne das Recht auf die Art des Ereignisses kommt es nicht an - auch nicht
// über ein erlaubtes Thema.
func TestHubRechtJeEreignisart(t *testing.T) {
	user := permsUser(3, "nur-server", domain.PermServersRead) // kein jobs:read, kein alerts:manage
	f := newHubFixture(t, user)
	f.visible.Store(&[]uint{10})
	c := f.dial(t)
	c.subscribe(t, "server:10", "dashboard")

	// Über server:10 wäre job.finished ein Ereignis ohne jobs:read; über das
	// Dashboard reicht servers:read (wie bei GET /dashboard).
	f.bus.Publish(events.JobFinished(someJob, ptr(uint(10)), "failed"))
	if ev := c.nextType(t, "event"); ev["topic"] != "dashboard" {
		t.Errorf("job.finished: %v", ev)
	}
	f.bus.Publish(events.AlertFired(events.AlertFiredData{ID: "a", ServerID: ptr(uint(10))}))
	f.bus.Publish(events.JobProgress(someJob, ptr(uint(10))))
	c.silent(t, 150*time.Millisecond)
}

// Gruppenzuweisung: Nach dem Verwerfen aller Ampeln wird die Sichtbarkeit neu
// geladen, und der Server wird erreichbar.
func TestHubSichtbereichWirdNachgeladen(t *testing.T) {
	f := newHubFixture(t, managerUser(2))
	c := f.dial(t)
	c.subscribe(t, "servers")

	f.bus.Publish(events.ServerBusy(events.ServerBusyData{ServerID: 30, Busy: true}))
	c.silent(t, 100*time.Millisecond)

	f.visible.Store(&[]uint{30})
	f.bus.Publish(events.ServerStatus(nil)) // folgt auf jede ändernde Anfrage
	c.nextType(t, "event")
	f.bus.Publish(events.ServerBusy(events.ServerBusyData{ServerID: 30, Busy: true}))
	if ev := c.nextType(t, "event"); ev["event"] != "server.busy" {
		t.Errorf("nach der Zuweisung: %v", ev)
	}
}

func TestHubErneuteAnmeldungsPruefung(t *testing.T) {
	f := newHubFixture(t, adminUser())
	f.hub.revalidateEvery = 30 * time.Millisecond
	c := f.dial(t)
	c.next(t) // hello

	c.silent(t, 120*time.Millisecond) // mehrere Prüfungen bestanden

	f.valid.Store(false)
	if code := c.closeCode(t); code != closeUnauthorized {
		t.Errorf("Schließcode %d, erwartet %d", code, closeUnauthorized)
	}
}

func TestHubRechteWerdenBeiPruefungAufgefrischt(t *testing.T) {
	f := newHubFixture(t, adminUser())
	f.hub.revalidateEvery = 30 * time.Millisecond
	c := f.dial(t)
	c.subscribe(t, "system")

	// Dem Benutzer wird settings:manage entzogen: system.build kommt nicht mehr an.
	f.user.Store(permsUser(1, domain.RoleAdmin, domain.PermServersRead))
	time.Sleep(120 * time.Millisecond)
	f.bus.Publish(events.SystemBuild(events.SystemBuildData{Phase: "running"}))
	c.silent(t, 150*time.Millisecond)
}

func TestHubCloseUserSchliesstNurDiesenBenutzer(t *testing.T) {
	f := newHubFixture(t, adminUser())
	mine := f.dial(t)
	mine.next(t) // hello: die Verbindung steht, bevor der Benutzer wechselt

	f.user.Store(managerUser(2))
	other := f.dial(t)
	other.next(t)

	f.hub.CloseUser(1)
	if code := mine.closeCode(t); code != closeUnauthorized {
		t.Errorf("Schließcode %d, erwartet %d", code, closeUnauthorized)
	}
	other.silent(t, 100*time.Millisecond) // lebt noch
}

func TestHubVerbindungsgrenzeJeSitzung(t *testing.T) {
	f := newHubFixture(t, adminUser())
	for range maxEventConnsPerSession {
		f.dial(t).next(t) // hello
	}
	over := f.dial(t)
	if code := over.closeCode(t); code != closeTooManyConnections {
		t.Errorf("Schließcode %d, erwartet %d", code, closeTooManyConnections)
	}
}

func TestHubPing(t *testing.T) {
	f := newHubFixture(t, adminUser())
	f.hub.pingEvery = 20 * time.Millisecond
	f.dial(t).nextType(t, "ping")
}

// Kommt der Tab nicht mit, gehen Ereignisse verloren - und er erfährt es.
func TestHubResyncBeiUeberlauf(t *testing.T) {
	f := newHubFixture(t, managerUser(2)) // nur Manager laden ihre Sichtbarkeit nach
	var blocking atomic.Bool
	release := make(chan struct{})
	f.hub.visibleIDs = func(repositories.AccessScope) ([]uint, error) {
		if blocking.Load() {
			<-release
		}
		return nil, nil
	}
	c := f.dial(t)
	c.subscribe(t, "servers")

	// Den Schreiber festhalten: Das Verwerfen aller Ampeln lädt die Sichtbarkeit
	// neu, und das blockiert hier. Währenddessen läuft der Puffer über.
	blocking.Store(true)
	f.bus.Publish(events.ServerStatus(nil))
	time.Sleep(50 * time.Millisecond)
	for range 400 {
		f.bus.Publish(events.ServerBusy(events.ServerBusyData{ServerID: 99}))
	}
	blocking.Store(false)
	close(release)

	c.nextType(t, "resync")
}

func TestHubCloseBeendetAlleVerbindungen(t *testing.T) {
	f := newHubFixture(t, adminUser())
	c := f.dial(t)
	c.next(t)

	_ = f.hub.Close()
	if code := c.closeCode(t); code != websocket.CloseGoingAway {
		t.Errorf("Schließcode %d, erwartet %d", code, websocket.CloseGoingAway)
	}
	f.hub.mu.Lock()
	defer f.hub.mu.Unlock()
	if len(f.hub.conns) != 0 || len(f.hub.perKey) != 0 {
		t.Errorf("Verbindungen übrig: %d, Schlüssel: %d", len(f.hub.conns), len(f.hub.perKey))
	}
}

// TestHubWartungSiehtJeder: Die Sperre während des Selbst-Updates trifft jeden
// Benutzer, also erfährt auch jeder davon. Den Fortschritt (system.build)
// bekommt weiterhin nur, wer die Einstellungen verwaltet.
func TestHubWartungSiehtJeder(t *testing.T) {
	f := newHubFixture(t, managerUser(2))
	c := f.dial(t)
	c.subscribe(t, "system")

	f.bus.Publish(events.SystemBuild(events.SystemBuildData{Phase: "waiting"}))
	f.bus.Publish(events.SystemMaintenance(events.SystemMaintenanceData{Active: true, Phase: "waiting"}))
	ev := c.nextType(t, "event")
	if ev["event"] != events.TypeSystemMaintenance {
		t.Errorf("erwartet system.maintenance, bekam %v", ev)
	}
	c.silent(t, 100*time.Millisecond)
}
