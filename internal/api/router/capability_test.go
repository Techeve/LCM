package router_test

import (
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// addCapabilityServer legt einen Server mit der gewünschten Paketverwaltung an,
// ohne eine SSH-Verbindung aufzubauen.
func addCapabilityServer(t *testing.T, db *gorm.DB, name, mgr string) uint {
	t.Helper()
	server := &domain.Server{
		// NameBIdx und Ref tragen eindeutige Indizes; leer kollidierten zwei Server.
		Name: name, NameBIdx: name, Ref: name, Host: name + ".test", SSHPort: 22, ServiceUser: "lcm",
		HostKeyFingerprint: "SHA256:test", PrivateKeyEnc: "x", PackageManager: mgr,
	}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}
	return server.ID
}

func failCapability(t *testing.T, db *gorm.DB, c domain.ServerCapability) {
	t.Helper()
	c.State = domain.CapabilityFailed
	if err := repositories.NewCapabilityRepository(db).Save(&c); err != nil {
		t.Fatal(err)
	}
}

type capabilityEntry struct {
	Category  string `json:"category"`
	State     string `json:"state"`
	Effective string `json:"effective"`
	Stale     string `json:"stale"`
	Repair    string `json:"repair"`
	Retryable bool   `json:"retryable"`
}

func getCapabilities(t *testing.T, app *fiber.App, token string, id uint) []capabilityEntry {
	t.Helper()
	resp := doRequest(t, app, "GET", fmt.Sprintf("/api/v1/servers/%d/capabilities", id), token, "")
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET capabilities: status %d: %s", resp.StatusCode, body)
	}
	var out []capabilityEntry
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCapabilitiesLiefertVierKategorienMitBerechnetenFeldern(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")

	// Ohne Verlauf: vier Einträge in Anzeigereihenfolge, alle in Ordnung.
	got := getCapabilities(t, app, admin, id)
	if len(got) != 4 {
		t.Fatalf("%d Einträge, erwartet 4: %+v", len(got), got)
	}
	for i, category := range domain.CapabilityCategories {
		if got[i].Category != category || got[i].State != domain.CapabilityOK || got[i].Effective != "" ||
			got[i].Repair != "" || got[i].Retryable {
			t.Errorf("Eintrag %d: %+v", i, got[i])
		}
	}

	failCapability(t, db, domain.ServerCapability{
		ServerID: id, Category: domain.CapabilityPackages, ConsecutiveFailures: 2,
		LastErrorClass: domain.ErrClassDpkgInterrupted, LastJobType: domain.RuleTypeUpdate,
	})
	failCapability(t, db, domain.ServerCapability{
		ServerID: id, Category: domain.CapabilityRules, ConsecutiveFailures: 1,
		LastErrorClass: domain.ErrClassUnknown, LastJobType: domain.RuleTypeScript,
	})
	got = getCapabilities(t, app, admin, id)
	pkgs, rules, cve := got[1], got[2], got[3]
	if pkgs.State != domain.CapabilityFailed || pkgs.Effective != "critical" ||
		pkgs.Repair != "dpkg_configure" || !pkgs.Retryable {
		t.Errorf("packages: %+v", pkgs)
	}
	// Ein Skript ohne erhaltene Regel lässt sich nicht erneut auslösen; gelb.
	if rules.Effective != "warning" || rules.Repair != "" || rules.Retryable {
		t.Errorf("rules: %+v", rules)
	}
	if cve.State != domain.CapabilityOK || cve.Retryable {
		t.Errorf("cve_scan: %+v", cve)
	}

	// Dieselbe Fehlerklasse auf dnf: keine Reparatur dafür.
	dnf := addCapabilityServer(t, db, "rhel01", "dnf")
	failCapability(t, db, domain.ServerCapability{
		ServerID: dnf, Category: domain.CapabilityPackages,
		ConsecutiveFailures: 1, LastErrorClass: domain.ErrClassBrokenDeps, LastJobType: domain.RuleTypeUpdate,
	})
	if p := getCapabilities(t, app, admin, dnf)[1]; p.Repair != "" || !p.Retryable {
		t.Errorf("dnf packages: %+v", p)
	}
	// zypper kennt fix_deps.
	suse := addCapabilityServer(t, db, "suse01", "zypper")
	failCapability(t, db, domain.ServerCapability{
		ServerID: suse, Category: domain.CapabilityPackages,
		ConsecutiveFailures: 1, LastErrorClass: domain.ErrClassBrokenDeps, LastJobType: domain.RuleTypeUpdate,
	})
	if p := getCapabilities(t, app, admin, suse)[1]; p.Repair != "fix_deps" {
		t.Errorf("zypper packages: %+v", p)
	}
}

func TestCapabilitiesUnbekannterServerIst404(t *testing.T) {
	app := newTestApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	if resp := doRequest(t, app, "GET", "/api/v1/servers/999/capabilities", admin, ""); resp.StatusCode != 404 {
		t.Errorf("erwartet 404, bekam %d", resp.StatusCode)
	}
}

// readOnlyUserToken legt eine Rolle mit nur servers:read und einen Benutzer
// dazu an.
func readOnlyUserToken(t *testing.T, app *fiber.App, db *gorm.DB, admin string) string {
	t.Helper()
	roles := repositories.NewRoleRepository(db)
	perm, err := roles.EnsurePermission(domain.PermServersRead, "Server lesen")
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.Create(&domain.Role{Name: "serverleser", Permissions: []domain.Permission{*perm}}); err != nil {
		t.Fatal(err)
	}
	resp := doRequest(t, app, "POST", "/api/v1/users", admin,
		`{"username":"leser","password":"Anker5-Leuchtturm!Wind","roles":["serverleser"]}`)
	if resp.StatusCode != 201 {
		t.Fatalf("user anlegen: status %d", resp.StatusCode)
	}
	return loginToken(t, app, "leser", "Anker5-Leuchtturm!Wind")
}

func TestRepairUndRetrySindSchreibzugriffe(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")
	reader := readOnlyUserToken(t, app, db, admin)

	repair := fmt.Sprintf("/api/v1/servers/%d/packages/repair", id)
	retry := fmt.Sprintf("/api/v1/servers/%d/capabilities/packages/retry", id)
	if resp := doRequest(t, app, "POST", repair, reader, `{"action":"fix_deps"}`); resp.StatusCode != 403 {
		t.Errorf("repair ohne servers:write: erwartet 403, bekam %d", resp.StatusCode)
	}
	if resp := doRequest(t, app, "POST", retry, reader, ""); resp.StatusCode != 403 {
		t.Errorf("retry ohne servers:write: erwartet 403, bekam %d", resp.StatusCode)
	}
	// Ohne Anmeldung gar nicht.
	if resp := doRequest(t, app, "POST", repair, "", `{"action":"fix_deps"}`); resp.StatusCode != 401 {
		t.Errorf("repair ohne Token: erwartet 401, bekam %d", resp.StatusCode)
	}
	// Lesen darf der Leser - sobald der Server zu einer seiner Gruppen gehört
	// (ohne Gruppe wäre er für ihn nicht da: 404).
	var leser domain.User
	if err := db.Where("username = ?", "leser").First(&leser).Error; err != nil {
		t.Fatal(err)
	}
	addServerToGroup(t, db, "Leser-Gruppe", id, &leser)
	if resp := doRequest(t, app, "GET", fmt.Sprintf("/api/v1/servers/%d/capabilities", id), reader, ""); resp.StatusCode != 200 {
		t.Errorf("capabilities mit servers:read: erwartet 200, bekam %d", resp.StatusCode)
	}
}

func TestRepairLehntUnpassendesAb(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	apt := addCapabilityServer(t, db, "web01", "apt")
	dnf := addCapabilityServer(t, db, "rhel01", "dnf")

	cases := []struct {
		name   string
		id     uint
		body   string
		status int
	}{
		{"unbekannte Aktion", apt, `{"action":"format_c"}`, 400},
		{"leere Aktion", apt, `{}`, 400},
		{"kaputter Body", apt, `{`, 400},
		{"dnf kennt keine Reparatur", dnf, `{"action":"fix_deps"}`, 400},
		{"dnf und dpkg", dnf, `{"action":"dpkg_configure"}`, 400},
		{"unbekannter Server", 999, `{"action":"fix_deps"}`, 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/v1/servers/%d/packages/repair", tc.id)
			resp := doRequest(t, app, "POST", path, admin, tc.body)
			if resp.StatusCode != tc.status {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("erwartet %d, bekam %d: %s", tc.status, resp.StatusCode, body)
			}
		})
	}
	// Keine Reparatur wurde gestartet.
	var jobs int64
	db.Model(&domain.Job{}).Where("type IN ?", []string{"repair-dpkg", "repair-deps"}).Count(&jobs)
	if jobs != 0 {
		t.Errorf("%d Reparatur-Jobs angelegt", jobs)
	}
}

func TestRetryLehntUnpassendesAb(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")
	failCapability(t, db, domain.ServerCapability{
		ServerID: id, Category: domain.CapabilityRules, ConsecutiveFailures: 1,
		LastErrorClass: domain.ErrClassUnknown, LastJobType: domain.RuleTypeScript,
	})

	cases := []struct {
		name     string
		category string
		status   int
	}{
		{"Kategorie in Ordnung", "packages", 409},
		{"nicht wiederholbare Aktion", "rules", 409},
		{"unbekannte Kategorie", "gibtsnicht", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/v1/servers/%d/capabilities/%s/retry", id, tc.category)
			if resp := doRequest(t, app, "POST", path, admin, ""); resp.StatusCode != tc.status {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("erwartet %d, bekam %d: %s", tc.status, resp.StatusCode, body)
			}
		})
	}
	if resp := doRequest(t, app, "POST", "/api/v1/servers/999/capabilities/packages/retry", admin, ""); resp.StatusCode != 404 {
		t.Errorf("unbekannter Server: erwartet 404, bekam %d", resp.StatusCode)
	}
}

// TestCapabilitiesMandantentrennung: Ein Manager sieht und bedient nur die
// Server seiner Gruppen. Ein Server fremder Gruppen ist für ihn nicht da (404,
// nicht 403) - bei Lesen, „Erneut versuchen" und Reparatur.
func TestCapabilitiesMandantentrennung(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	resp := doRequest(t, app, "POST", "/api/v1/users", admin,
		`{"username":"gruppe-a","password":"Anker5-Leuchtturm!Wind","roles":["manager"]}`)
	if resp.StatusCode != 201 {
		t.Fatalf("user anlegen: status %d", resp.StatusCode)
	}
	var manager domain.User
	if err := db.Where("username = ?", "gruppe-a").First(&manager).Error; err != nil {
		t.Fatal(err)
	}
	token := loginToken(t, app, "gruppe-a", "Anker5-Leuchtturm!Wind")

	own := addCapabilityServer(t, db, "web-a", "apt")
	foreign := addCapabilityServer(t, db, "web-b", "apt")
	addServerToGroup(t, db, "Gruppe A", own, &manager)
	addServerToGroup(t, db, "Gruppe B", foreign, nil)
	for _, id := range []uint{own, foreign} {
		failCapability(t, db, domain.ServerCapability{
			ServerID: id, Category: domain.CapabilityPackages, ConsecutiveFailures: 1,
			LastErrorClass: domain.ErrClassBrokenDeps, LastJobType: domain.RuleTypeUpdate,
		})
	}

	// Gegenprobe: der eigene Server ist erreichbar.
	if resp := doRequest(t, app, "GET", fmt.Sprintf("/api/v1/servers/%d/capabilities", own), token, ""); resp.StatusCode != 200 {
		t.Fatalf("eigener Server: erwartet 200, bekam %d", resp.StatusCode)
	}

	requests := []struct {
		name, method, path, body string
	}{
		{"lesen", "GET", "/api/v1/servers/%d/capabilities", ""},
		{"erneut versuchen", "POST", "/api/v1/servers/%d/capabilities/packages/retry", ""},
		{"reparieren", "POST", "/api/v1/servers/%d/packages/repair", `{"action":"fix_deps"}`},
	}
	for _, rq := range requests {
		t.Run(rq.name, func(t *testing.T) {
			resp := doRequest(t, app, rq.method, fmt.Sprintf(rq.path, foreign), token, rq.body)
			if resp.StatusCode != 404 {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("fremder Server: erwartet 404, bekam %d: %s", resp.StatusCode, body)
			}
		})
	}
	// Es wurde nichts gestartet.
	var jobs int64
	db.Model(&domain.Job{}).Where("server_id = ?", foreign).Count(&jobs)
	if jobs != 0 {
		t.Errorf("%d Jobs auf dem fremden Server angelegt", jobs)
	}
}

// addServerToGroup legt eine Gruppe mit dem Server an; manager nil = ohne Manager.
func addServerToGroup(t *testing.T, db *gorm.DB, name string, serverID uint, manager *domain.User) {
	t.Helper()
	group := &domain.ServerGroup{Name: name, Servers: []domain.Server{{ID: serverID}}}
	if manager != nil {
		group.Managers = []domain.User{*manager}
	}
	if err := db.Omit("Servers.*", "Managers.*").Create(group).Error; err != nil {
		t.Fatal(err)
	}
}
