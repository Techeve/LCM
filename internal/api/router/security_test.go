package router_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/totp"
	"LCM/internal/storage/repositories"
)

// consoleUser legt eine Rolle an, die NUR die Konsole und das Lesen von Servern
// erlaubt, und einen Benutzer damit - ohne Gruppenzuordnung, also mit leerem
// Sichtbereich.
func consoleUser(t *testing.T, app *fiber.App, db *gorm.DB) string {
	t.Helper()
	roles := repositories.NewRoleRepository(db)
	role := &domain.Role{Name: "konsole", Description: "nur Konsole"}
	if err := roles.Create(role); err != nil {
		t.Fatal(err)
	}
	var perms []domain.Permission
	for _, code := range []string{domain.PermServersRead, domain.PermServersConsole} {
		p, err := roles.EnsurePermission(code, code)
		if err != nil {
			t.Fatal(err)
		}
		perms = append(perms, *p)
	}
	if err := roles.SetPermissions(role, perms); err != nil {
		t.Fatal(err)
	}
	users := services.NewUserService(repositories.NewUserRepository(db), roles)
	if _, err := users.CreateUser("konsole", "", "Regen9-Amsel!Turmfalk", "", "", []string{"konsole"}, "test"); err != nil {
		t.Fatal(err)
	}
	return loginToken(t, app, "konsole", "Regen9-Amsel!Turmfalk")
}

// TestTerminalFahrkarteNurImSichtbereich: Das Konsolen-Recht allein reicht
// nicht - die Fahrkarte gibt es nur für Server, die der Benutzer sehen darf.
// Connect öffnet mit ScopeAll und verlässt sich auf genau diese Prüfung.
func TestTerminalFahrkarteNurImSichtbereich(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	if err := repositories.NewServerRepository(db).Create(&domain.Server{Name: "fremd", Host: "fremd.test", SSHPort: 22}); err != nil {
		t.Fatal(err)
	}
	var server domain.Server
	if err := db.Where("name_bidx = ?", repositories.BlindIndex("fremd")).First(&server).Error; err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/servers/%d/terminal/ticket", server.ID)

	konsole := consoleUser(t, app, db)
	if r := doRequest(t, app, "POST", path, konsole, ""); r.StatusCode != 404 {
		t.Errorf("Server außerhalb des Sichtbereichs: erwartet 404, bekam %d", r.StatusCode)
	}
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	if r := doRequest(t, app, "POST", path, admin, ""); r.StatusCode != 200 {
		t.Errorf("Admin sieht alles: erwartet 200, bekam %d", r.StatusCode)
	}
}

// TestEigeneEmailNurMitPasswort: Die E-Mail-Adresse empfängt den Passwort-
// Reset - eine erbeutete Sitzung darf sie nicht umbiegen. Der Name bleibt frei.
func TestEigeneEmailNurMitPasswort(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin, err := repositories.NewUserRepository(db).FindByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	token := loginToken(t, app, "admin", "test-admin-passwort")
	path := fmt.Sprintf("/api/v1/users/%d/profile", admin.ID)

	if r := doRequest(t, app, "PATCH", path, token, `{"email":"neu@example.com","first_name":"A","last_name":"B"}`); r.StatusCode != 401 {
		t.Errorf("E-Mail ohne Passwort: erwartet 401, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, app, "PATCH", path, token, `{"email":"neu@example.com","current_password":"falsch"}`); r.StatusCode != 401 {
		t.Errorf("E-Mail mit falschem Passwort: erwartet 401, bekam %d", r.StatusCode)
	}
	// Der Name ist ohne Hürde änderbar - die Adresse bleibt dabei, wie sie ist.
	if r := doRequest(t, app, "PATCH", path, token, `{"email":"`+admin.Email+`","first_name":"Ada","last_name":"Lovelace"}`); r.StatusCode != 200 {
		t.Errorf("nur der Name ohne Passwort: erwartet 200, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, app, "PATCH", path, token, `{"email":"neu@example.com","first_name":"Ada","current_password":"test-admin-passwort"}`); r.StatusCode != 200 {
		t.Errorf("E-Mail mit Passwort: erwartet 200, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, app, "PATCH", path, token, `{"email":"NEU@example.com","first_name":"Ada"}`); r.StatusCode != 200 {
		t.Errorf("dieselbe Adresse anders geschrieben ist keine Änderung: erwartet 200, bekam %d", r.StatusCode)
	}
}

// TestRumpfBudgetVorDemLesen: Das große Body-Limit gilt nur dem Backup-Upload.
// Auf allen anderen Routen wird ein überlanger oder längenloser Rumpf an der
// Kopfzeile abgewiesen - bevor er gelesen wird.
func TestRumpfBudgetVorDemLesen(t *testing.T) {
	app := newTestApp(t)

	big := strings.NewReader(`{"username":"admin","password":"` + strings.Repeat("x", 1<<20) + `"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", big)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, testRequestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 413 {
		t.Errorf("1 MiB auf /auth/login: erwartet 413, bekam %d", resp.StatusCode)
	}

	chunked := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"x"}`))
	chunked.Header.Set("Content-Type", "application/json")
	chunked.TransferEncoding = []string{"chunked"}
	chunked.ContentLength = 0 // unbekannt - Go schreibt dann chunked, ohne Content-Length
	resp, err = app.Test(chunked, testRequestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 411 {
		t.Errorf("chunked auf /auth/login: erwartet 411, bekam %d", resp.StatusCode)
	}

	// Der Upload-Pfad ist ausgenommen: derselbe große Rumpf scheitert dort
	// nicht am Budget (sondern erst im Handler an der fehlenden Datei).
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	big.Seek(0, 0)
	req = httptest.NewRequest("POST", "/api/v1/system/backups/restore-upload", big)
	req.Header.Set("Authorization", "Bearer "+admin)
	resp, err = app.Test(req, testRequestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == 413 || resp.StatusCode == 411 {
		t.Errorf("Backup-Upload darf nicht am Budget scheitern, bekam %d", resp.StatusCode)
	}
}

// TestLoginDeckeltPasswortlaenge: Ein kilobytelanges „Passwort" wird nicht
// gehasht - anonymes Rechenzeit-Verbrennen.
func TestLoginDeckeltPasswortlaenge(t *testing.T) {
	app := newTestApp(t)
	body := `{"username":"admin","password":"` + strings.Repeat("a", services.MaxLoginPasswordBytes+1) + `"}`
	if r := doRequest(t, app, "POST", "/api/v1/auth/login", "", body); r.StatusCode != 401 {
		t.Errorf("überlanges Passwort: erwartet 401, bekam %d", r.StatusCode)
	}
}

// TestZweitfaktorChallengeGiltEinmal: Nach dem zweiten Anmeldeschritt ist die
// Challenge verbraucht - auch mit einem weiteren gültigen Code entsteht keine
// zweite Sitzung.
func TestZweitfaktorChallengeGiltEinmal(t *testing.T) {
	app := newTestApp(t)
	token := loginToken(t, app, "admin", "test-admin-passwort")

	r := doRequest(t, app, "POST", "/api/v1/auth/2fa/setup", token, "")
	if r.StatusCode != 200 {
		t.Fatalf("2fa setup: %d", r.StatusCode)
	}
	var setup struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&setup); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	codeAt := func(d time.Duration) string {
		code, err := totp.Code(setup.Secret, now.Add(d))
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	if r := doRequest(t, app, "POST", "/api/v1/auth/2fa/enable", token, `{"code":"`+codeAt(0)+`"}`); r.StatusCode != 200 {
		t.Fatalf("2fa enable: %d", r.StatusCode)
	}

	r = doRequest(t, app, "POST", "/api/v1/auth/login", "", `{"username":"admin","password":"test-admin-passwort"}`)
	var step struct {
		Challenge string `json:"challenge"`
	}
	if err := json.NewDecoder(r.Body).Decode(&step); err != nil || step.Challenge == "" {
		t.Fatalf("login liefert keine challenge (status %d): %v", r.StatusCode, err)
	}
	// Der Code der Aktivierung ist verbraucht; die Nachbarperioden liegen im
	// Prüffenster und sind noch unbenutzt.
	if r := doRequest(t, app, "POST", "/api/v1/auth/login/2fa", "", `{"challenge":"`+step.Challenge+`","code":"`+codeAt(30*time.Second)+`"}`); r.StatusCode != 200 {
		t.Fatalf("2. Schritt mit gültigem Code: erwartet 200, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, app, "POST", "/api/v1/auth/login/2fa", "", `{"challenge":"`+step.Challenge+`","code":"`+codeAt(-30*time.Second)+`"}`); r.StatusCode != 401 {
		t.Errorf("verbrauchte Challenge: erwartet 401, bekam %d", r.StatusCode)
	}
}
