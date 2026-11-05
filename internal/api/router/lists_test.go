package router_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

const tenantPassword = "Anker5-Leuchtturm!Wind"

// tenants legt zwei Manager mit je einem Server in eigener Gruppe an.
type tenants struct {
	app              *fiber.App
	db               *gorm.DB
	admin, a, b      string // Tokens
	serverA, serverB uint
}

func newTenants(t *testing.T) tenants {
	t.Helper()
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	tn := tenants{app: app, db: db, admin: admin}
	users := map[string]*domain.User{}
	for _, name := range []string{"mandant-a", "mandant-b"} {
		body := `{"username":"` + name + `","password":"` + tenantPassword + `","roles":["manager"]}`
		if r := doRequest(t, app, "POST", "/api/v1/users", admin, body); r.StatusCode != 201 {
			t.Fatalf("user anlegen: %d", r.StatusCode)
		}
		var u domain.User
		db.Where("username = ?", name).First(&u)
		users[name] = &u
	}
	tn.serverA = addCapabilityServer(t, db, "web-a", "apt")
	tn.serverB = addCapabilityServer(t, db, "web-b", "apt")
	addServerToGroup(t, db, "Gruppe A", tn.serverA, users["mandant-a"])
	addServerToGroup(t, db, "Gruppe B", tn.serverB, users["mandant-b"])
	tn.a = loginToken(t, app, "mandant-a", tenantPassword)
	tn.b = loginToken(t, app, "mandant-b", tenantPassword)
	return tn
}

// list ruft einen Listen-Endpunkt ab und liefert {items, total}.
func (tn tenants) list(t *testing.T, token, path string) (items []map[string]any, total int) {
	t.Helper()
	resp := doRequest(t, tn.app, "GET", path, token, "")
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: %d", path, resp.StatusCode)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Items == nil {
		t.Fatalf("GET %s: items ist null statt []", path)
	}
	return body.Items, body.Total
}

func (tn tenants) addJob(t *testing.T, serverID uint, typ, status string, started time.Time, took time.Duration) {
	t.Helper()
	finished := started.Add(took)
	job := &domain.Job{ServerID: &serverID, Type: typ, Name: typ + " job", Status: status, StartedAt: &started, FinishedAt: &finished}
	if err := tn.db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
}

func TestServerlisteUeberDieAPI(t *testing.T) {
	tn := newTenants(t)
	items, total := tn.list(t, tn.admin, "/api/v1/servers?sort=-name&limit=1")
	if total != 2 || len(items) != 1 || items[0]["name"] != "web-b" {
		t.Errorf("Seite: %v, total %d", items, total)
	}
	for _, key := range []string{"status", "insights", "os_support", "incapable", "updates", "groups"} {
		if _, ok := items[0][key]; !ok {
			t.Errorf("Eintrag ohne %q", key)
		}
	}
	if items, _ := tn.list(t, tn.a, "/api/v1/servers"); len(items) != 1 || items[0]["name"] != "web-a" {
		t.Errorf("Mandant A sieht %v", items)
	}
	if items, total := tn.list(t, tn.a, fmt.Sprintf("/api/v1/servers?ids=%d", tn.serverB)); len(items) != 0 || total != 0 {
		t.Errorf("Mandant A fragt Server B per ids: %v", items)
	}
	if items, _ := tn.list(t, tn.admin, "/api/v1/servers?name=WEB-A"); len(items) != 1 {
		t.Errorf("name=: %v", items)
	}
	for _, q := range []string{"sort=quatsch", "status=blau", "group_id=x", "ids=1,x"} {
		resp := doRequest(t, tn.app, "GET", "/api/v1/servers?"+q, tn.admin, "")
		if resp.StatusCode != 400 {
			t.Errorf("%s: %d, erwartet 400", q, resp.StatusCode)
		}
		assertErrorCode(t, resp, "request.invalid_filter")
	}
}

func TestGruppenlisteMitFilter(t *testing.T) {
	tn := newTenants(t)
	items, total := tn.list(t, tn.admin, "/api/v1/server-groups?q=gruppe%20a")
	if total != 1 || len(items) != 1 || items[0]["name"] != "Gruppe A" {
		t.Errorf("q: %v (%d)", items, total)
	}
	if _, total := tn.list(t, tn.a, "/api/v1/server-groups"); total != 1 {
		t.Errorf("Mandant A sieht %d Gruppen", total)
	}
}

func TestJobListeFiltertSortiertUndTrenntMandanten(t *testing.T) {
	tn := newTenants(t)
	now := time.Now().UTC().Truncate(time.Second)
	tn.addJob(t, tn.serverA, "update", "success", now.Add(-3*time.Hour), time.Minute)
	tn.addJob(t, tn.serverA, "health", "success", now.Add(-2*time.Hour), time.Second)
	tn.addJob(t, tn.serverA, "update", "failed", now.Add(-time.Hour), time.Hour)
	tn.addJob(t, tn.serverB, "update", "success", now.Add(-30*time.Minute), time.Minute)

	items, total := tn.list(t, tn.admin, "/api/v1/jobs")
	if total != 4 || items[0]["server_name"] != "web-b" {
		t.Errorf("Vorgabe (neueste zuerst): total %d, erstes %v", total, items[0]["server_name"])
	}
	for _, it := range items {
		if _, embedded := it["server"]; embedded {
			t.Error("Eintrag trägt den ganzen Server statt server_name")
		}
	}
	if items, total := tn.list(t, tn.a, "/api/v1/jobs"); total != 3 || len(items) != 3 {
		t.Errorf("Mandant A: %d Jobs, erwartet 3", total)
	}
	if _, total := tn.list(t, tn.admin, "/api/v1/jobs?hide_health=1"); total != 3 {
		t.Errorf("hide_health=1: %d", total)
	}
	if _, total := tn.list(t, tn.admin, "/api/v1/jobs/history?hide_health=1"); total != 3 {
		t.Errorf("history-Alias: %d", total)
	}
	if _, total := tn.list(t, tn.admin, "/api/v1/jobs?status=failed,blocked&kind=update"); total != 1 {
		t.Errorf("status+kind: %d", total)
	}
	items, _ = tn.list(t, tn.admin, "/api/v1/jobs?sort=-duration&limit=1")
	if items[0]["duration_seconds"] != float64(3600) {
		t.Errorf("-duration: %v", items[0]["duration_seconds"])
	}
	since := now.Add(-100 * time.Minute).Format(time.RFC3339)
	if _, total := tn.list(t, tn.admin, "/api/v1/jobs?since="+since); total != 2 {
		t.Errorf("since: %d", total)
	}
	if items, total := tn.list(t, tn.admin, "/api/v1/jobs?sort=started_at&limit=1&offset=1"); total != 4 || len(items) != 1 {
		t.Errorf("Blättern: %d, %d", total, len(items))
	}
	for _, q := range []string{"since=gestern", "sort=name", "status=quatsch"} {
		if r := doRequest(t, tn.app, "GET", "/api/v1/jobs?"+q, tn.admin, ""); r.StatusCode != 400 {
			t.Errorf("%s: %d, erwartet 400", q, r.StatusCode)
		}
	}
}

// uiJobKinds liest die Arten, die die Oberfläche kennt und im Filter „Art“ anbietet (KIND_KEYS in job-labels.ts).
func uiJobKinds(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("../../../ui/src/pages/jobs/job-labels.ts")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)const KIND_KEYS[^{]*\{(.*?)\n\};`).FindSubmatch(src)
	if block == nil {
		t.Fatal("KIND_KEYS in job-labels.ts nicht gefunden")
	}
	var kinds []string
	for _, m := range regexp.MustCompile(`(?m)^\s+'?([a-z-]+)'?: 'jobs\.kind\.`).FindAllSubmatch(block[1], -1) {
		kinds = append(kinds, string(m[1]))
	}
	if len(kinds) < 30 {
		t.Fatalf("nur %d Arten gelesen, das Muster passt nicht mehr: %v", len(kinds), kinds)
	}
	return kinds
}

// Die Jobliste muss jede Art annehmen, die die Oberfläche zum Filtern anbietet. „harden-ssh“, „join“ und
// „acl-setup“ fehlten in der Liste des Controllers: Der Filter antwortete mit 400.
func TestJobFilterKenntAlleArtenDerOberflaeche(t *testing.T) {
	tn := newTenants(t)
	for _, kind := range uiJobKinds(t) {
		if r := doRequest(t, tn.app, "GET", "/api/v1/jobs?kind="+kind, tn.admin, ""); r.StatusCode != 200 {
			t.Errorf("kind=%s: %d, erwartet 200", kind, r.StatusCode)
		}
	}
	if r := doRequest(t, tn.app, "GET", "/api/v1/jobs?kind=quatsch", tn.admin, ""); r.StatusCode != 400 {
		t.Errorf("kind=quatsch: %d, erwartet 400", r.StatusCode)
	}
}

func TestAlarmVerlaufNurImSichtbereich(t *testing.T) {
	tn := newTenants(t)
	for _, serverID := range []*uint{&tn.serverA, &tn.serverB, nil} {
		event := &domain.AlertEvent{ServerID: serverID, Type: "disk-capacity", Severity: "warning", RuleName: "r"}
		if err := tn.db.Create(event).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, total := tn.list(t, tn.admin, "/api/v1/alert-events"); total != 3 {
		t.Errorf("Admin sieht %d Ereignisse, erwartet 3", total)
	}
	// Die Rolle manager hat alerts:manage nicht; eine eigene Rolle mit dem Recht
	// sieht trotzdem nur die eigenen Server und nichts ohne Server.
	roleWithAlerts(t, tn)
	token := loginToken(t, tn.app, "alarm-a", tenantPassword)
	items, total := tn.list(t, token, "/api/v1/alert-events")
	if total != 1 || len(items) != 1 || items[0]["server_id"] != float64(tn.serverA) {
		t.Errorf("Mandant A sieht %v (%d)", items, total)
	}
	if _, total := tn.list(t, token, fmt.Sprintf("/api/v1/alert-events?server_id=%d", tn.serverB)); total != 0 {
		t.Errorf("Mandant A sieht Ereignisse von Server B: %d", total)
	}
	if r := doRequest(t, tn.app, "GET", "/api/v1/alert-events?since=x", tn.admin, ""); r.StatusCode != 400 {
		t.Errorf("since=x: %d", r.StatusCode)
	}
	if r := doRequest(t, tn.app, "GET", "/api/v1/alert-events", tn.a, ""); r.StatusCode != 403 {
		t.Errorf("Manager ohne alerts:manage: %d, erwartet 403", r.StatusCode)
	}
}

// roleWithAlerts gibt einem Benutzer in Gruppe A das Recht alerts:manage.
func roleWithAlerts(t *testing.T, tn tenants) {
	t.Helper()
	body := `{"username":"alarm-a","password":"` + tenantPassword + `","roles":["manager"]}`
	if r := doRequest(t, tn.app, "POST", "/api/v1/users", tn.admin, body); r.StatusCode != 201 {
		t.Fatalf("user anlegen: %d", r.StatusCode)
	}
	var user domain.User
	var role domain.Role
	var perm domain.Permission
	tn.db.Where("username = ?", "alarm-a").First(&user)
	tn.db.Where("code = ?", domain.PermAlertsManage).First(&perm)
	role = domain.Role{Name: "alarm", Permissions: []domain.Permission{perm}}
	if err := tn.db.Omit("Permissions.*").Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	if err := tn.db.Model(&user).Association("Roles").Append(&role); err != nil {
		t.Fatal(err)
	}
	var group domain.ServerGroup
	tn.db.Where("name = ?", "Gruppe A").First(&group)
	if err := tn.db.Model(&group).Association("Managers").Append(&user); err != nil {
		t.Fatal(err)
	}
}

func TestSammelUpdateStatusUndSelbstUpdateNurMitRecht(t *testing.T) {
	tn := newTenants(t)
	if r := doRequest(t, tn.app, "GET", "/api/v1/security/update-all", tn.a, ""); r.StatusCode != 200 {
		t.Fatalf("update-all als Manager: %d", r.StatusCode)
	}
	for _, method := range []string{"GET", "POST"} {
		if r := doRequest(t, tn.app, method, "/api/v1/system/self-update", tn.a, ""); r.StatusCode != 403 {
			t.Errorf("%s self-update als Manager: %d, erwartet 403", method, r.StatusCode)
		}
	}
	if r := doRequest(t, tn.app, "GET", "/api/v1/system/self-update", tn.admin, ""); r.StatusCode != 200 {
		t.Errorf("self-update als Admin: %d", r.StatusCode)
	}
}

func TestListenAntwortenHabenETag(t *testing.T) {
	tn := newTenants(t)
	resp := doRequest(t, tn.app, "GET", "/api/v1/servers", tn.admin, "")
	tag := resp.Header.Get("ETag")
	if tag == "" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("ETag %q, Cache-Control %q", tag, resp.Header.Get("Cache-Control"))
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	req.Header.Set("X-LCM-Session", tn.admin)
	req.Header.Set("If-None-Match", tag)
	again, err := tn.app.Test(req, testRequestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(again.Body); again.StatusCode != 304 || len(body) != 0 {
		t.Errorf("unverändert: %d, %d Byte", again.StatusCode, len(body))
	}
	// Anderer Inhalt, anderes Etikett: ein anderer Benutzer sieht andere Server.
	other := doRequest(t, tn.app, "GET", "/api/v1/servers", tn.a, "")
	if other.Header.Get("ETag") == tag || strings.Contains(other.Header.Get("ETag"), "W/") {
		t.Errorf("Etikett %q", other.Header.Get("ETag"))
	}
}
