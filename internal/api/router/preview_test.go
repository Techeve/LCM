package router_test

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

type previewBody struct {
	Action   string `json:"action"`
	Commands []struct {
		Run string `json:"run"`
		As  string `json:"as"`
	} `json:"commands"`
	Packages   []map[string]string `json:"packages"`
	Services   []string            `json:"services"`
	Reboot     string              `json:"reboot"`
	Reversible bool                `json:"reversible"`
	Duration   struct {
		Min int `json:"min_seconds"`
		Max int `json:"max_seconds"`
	} `json:"duration"`
	Notes []struct {
		Code   string         `json:"code"`
		Params map[string]any `json:"params"`
	} `json:"notes"`
}

func previewURL(id uint, action, query string) string {
	url := fmt.Sprintf("/api/v1/servers/%d/actions/%s/preview", id, action)
	if query != "" {
		url += "?" + query
	}
	return url
}

// getPreview ruft die Vorschau ab und liefert Status und rohen Rumpf.
func getPreview(t *testing.T, app *fiber.App, token, url string) (int, string) {
	t.Helper()
	resp := doRequest(t, app, "GET", url, token, "")
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func errorCodeOf(t *testing.T, body string) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("kein Fehlerrumpf: %s", body)
	}
	return e.Code
}

func TestPreviewEndpointShape(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")
	if err := db.Create(&domain.Package{
		ServerRef: repositories.ServerRef(id), Name: "openssl", Version: "3.0.14", CandidateVersion: "3.0.15",
	}).Error; err != nil {
		t.Fatal(err)
	}

	status, body := getPreview(t, app, admin, previewURL(id, "upgrade-all", ""))
	if status != 200 {
		t.Fatalf("status %d: %s", status, body)
	}
	var p previewBody
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if p.Action != "upgrade-all" || len(p.Commands) != 1 || p.Commands[0].As != "root" ||
		!strings.Contains(p.Commands[0].Run, "upgrade") {
		t.Errorf("Rumpf: %+v", p)
	}
	if len(p.Packages) != 1 || p.Packages[0]["name"] != "openssl" || p.Packages[0]["from"] != "3.0.14" || p.Packages[0]["to"] != "3.0.15" {
		t.Errorf("Pakete: %+v", p.Packages)
	}
	if p.Reboot != "no" || p.Duration.Min <= 0 || p.Duration.Max < p.Duration.Min {
		t.Errorf("Neustart/Dauer: %+v", p)
	}
	// Leere Listen sind [] und nicht null; services entfällt ohne Daten.
	if !strings.Contains(body, `"notes":[{`) || strings.Contains(body, `"services"`) {
		t.Errorf("Form der Listen: %s", body)
	}
	_, empty := getPreview(t, app, admin, previewURL(id, "refresh", ""))
	if !strings.Contains(empty, `"packages":[]`) {
		t.Errorf("packages muss [] sein: %s", empty)
	}
}

func TestPreviewEndpointArgumentsAndErrors(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	apt := addCapabilityServer(t, db, "web01", "apt")
	dnf := addCapabilityServer(t, db, "rhel01", "dnf")

	cases := []struct {
		name   string
		url    string
		status int
		code   string
		want   string
	}{
		{"einzelnes Paket", previewURL(apt, "update", "package=openssl"), 200, "", "install --only-upgrade -y openssl"},
		{"mehrere Pakete", previewURL(dnf, "update", "package=openssl,curl"), 200, "", "dnf -y --refresh upgrade openssl curl"},
		{"dnf entfernen", previewURL(dnf, "remove", "package=nginx"), 200, "", "dnf -y remove nginx"},
		{"Version", previewURL(dnf, "update", "package=openssl&version=3.0.15"), 200, "", "downgrade openssl-3.0.15"},
		{"ungültiger Paketname", previewURL(apt, "update", "package=Bad$Name"), 422, "package.invalid_name", ""},
		{"kein Paket", previewURL(apt, "remove", ""), 422, "package.none_given", ""},
		{"geschütztes Paket", previewURL(apt, "remove", "package=sudo"), 422, "package.protected", ""},
		{"Reparatur apt", previewURL(apt, "repair", "kind=dpkg_configure"), 200, "", "dpkg --configure -a"},
		{"Reparatur dnf nicht möglich", previewURL(dnf, "repair", "kind=fix_deps"), 400, "server.repair_unsupported", ""},
		{"unbekannte Aktion", previewURL(apt, "format-c", ""), 404, "server.preview_unknown_action", ""},
		{"unbekannter Server", previewURL(9999, "refresh", ""), 404, "server.not_found", ""},
		{"Snap ohne snapd", previewURL(apt, "snaps-refresh-all", ""), 409, "server.no_snapd", ""},
		{"Retry ohne Fehlschlag", previewURL(apt, "retry", "category=packages"), 409, "server.capability_not_failed", ""},
		{"Custom unbekannt", previewURL(apt, "custom", "id=4711"), 404, "custom_action.not_found", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := getPreview(t, app, admin, tc.url)
			if status != tc.status {
				t.Fatalf("status %d, erwartet %d: %s", status, tc.status, body)
			}
			if tc.code != "" && errorCodeOf(t, body) != tc.code {
				t.Errorf("code %q, erwartet %q: %s", errorCodeOf(t, body), tc.code, body)
			}
			if tc.want != "" && !strings.Contains(body, tc.want) {
				t.Errorf("Rumpf ohne %q: %s", tc.want, body)
			}
		})
	}
}

// userWithPermission legt eine Rolle mit genau dieser Berechtigung und einen
// Benutzer dazu an.
func userWithPermission(t *testing.T, app *fiber.App, db *gorm.DB, admin, username, perm string) (token string, user domain.User) {
	t.Helper()
	roles := repositories.NewRoleRepository(db)
	p, err := roles.EnsurePermission(perm, perm)
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.Create(&domain.Role{Name: "rolle-" + username, Permissions: []domain.Permission{*p}}); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"username":%q,"password":"Anker5-Leuchtturm!Wind","roles":["rolle-%s"]}`, username, username)
	if resp := doRequest(t, app, "POST", "/api/v1/users", admin, body); resp.StatusCode != 201 {
		t.Fatalf("user anlegen: status %d", resp.StatusCode)
	}
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		t.Fatal(err)
	}
	return loginToken(t, app, username, "Anker5-Leuchtturm!Wind"), user
}

// TestPreviewScopeAndPermission: Die Vorschau verlangt servers:read und zeigt
// nur Server der eigenen Gruppen - sonst 404 wie jeder Server-Endpunkt.
func TestPreviewScopeAndPermission(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")
	reader, readerUser := userWithPermission(t, app, db, admin, "leser", domain.PermServersRead)
	noServers, _ := userWithPermission(t, app, db, admin, "nurjobs", domain.PermJobsRead)
	url := previewURL(id, "upgrade-all", "")

	if status, _ := getPreview(t, app, "", url); status != 401 {
		t.Errorf("ohne Anmeldung: %d", status)
	}
	if status, _ := getPreview(t, app, noServers, url); status != 403 {
		t.Errorf("ohne servers:read: %d", status)
	}
	// Mit Recht, aber ohne Gruppe mit diesem Server: nicht sichtbar.
	status, body := getPreview(t, app, reader, url)
	if status != 404 || errorCodeOf(t, body) != "server.not_found" {
		t.Errorf("Server außerhalb des Zugriffsbereichs: %d %s", status, body)
	}
	addServerToGroup(t, db, "Leser-Gruppe", id, &readerUser)
	if status, body := getPreview(t, app, reader, url); status != 200 {
		t.Errorf("Server in eigener Gruppe: %d %s", status, body)
	}
}

// TestPreviewCustomNeedsRulesManageAndMasks: Das gespeicherte Skript einer
// Custom-Aktion sieht nur, wer sie auch verwalten darf - und auch dann ohne
// Geheimnisse.
func TestPreviewCustomNeedsRulesManageAndMasks(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")
	act := domain.CustomAction{
		Name:     "Sicherung",
		Commands: "DB_PASSWORD=hunter2 mysqldump app\ncurl --token geheim123 https://x.test",
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	url := previewURL(id, "custom", fmt.Sprintf("id=%d", act.ID))

	reader, user := userWithPermission(t, app, db, admin, "leser", domain.PermServersRead)
	addServerToGroup(t, db, "Leser-Gruppe", id, &user)
	if status, _ := getPreview(t, app, reader, url); status != 403 {
		t.Errorf("Custom-Vorschau ohne rules:manage: %d", status)
	}

	status, body := getPreview(t, app, admin, url)
	if status != 200 {
		t.Fatalf("status %d: %s", status, body)
	}
	if strings.Contains(body, "hunter2") || strings.Contains(body, "geheim123") {
		t.Errorf("Geheimnis in der Antwort: %s", body)
	}
	if !strings.Contains(body, "DB_PASSWORD=••••") || !strings.Contains(body, "preview.secrets_masked") {
		t.Errorf("Maskierung fehlt: %s", body)
	}
}
