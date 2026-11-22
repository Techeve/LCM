package router_test

import (
	"testing"

	"gorm.io/gorm"

	"LCM/internal/api/router"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// sharedPortApp ist die Test-App im Modus „ein Port“ mit dem Namen der
// Oberfläche: Die Einstellungen prüfen die Namen der öffentlichen Adressen.
func sharedPortApp(t *testing.T) (do func(method, path, body string) map[string]any, status func() int) {
	t.Helper()
	app, _ := buildTestAppDB(t, 0, nil, func(d *router.Deps, db *gorm.DB) {
		cipher, err := crypto.NewCipher(crypto.GenerateKey())
		if err != nil {
			t.Fatal(err)
		}
		audit := services.NewAuditService(repositories.NewAuditRepository(db))
		d.Settings = services.NewSettingsService(repositories.NewSettingsRepository(db), cipher, audit, nil)
	})
	token := loginToken(t, app, "admin", adminPassword)
	var last int
	do = func(method, path, body string) map[string]any {
		resp := doRequest(t, app, method, path, token, body)
		last = resp.StatusCode
		return decodeMap(t, resp)
	}
	if do("PATCH", "/api/v1/settings/global", `{"address_mode":"shared_port","public_base_url":"https://lcm.example.org"}`); last != 200 {
		t.Fatalf("Modus ein Port einrichten: %d", last)
	}
	return do, func() int { return last }
}

func TestOeffentlicheAdressenMitGleichemNamenAmGemeinsamenPort(t *testing.T) {
	do, status := sharedPortApp(t)
	do("PATCH", "/api/v1/settings/global", `{"public_agent_url":"https://agent.example.org"}`)
	if status() != 200 {
		t.Fatalf("Agent mit eigenem Namen: %d", status())
	}

	// Gleicher Name, anderer Port: auf dem gemeinsamen Port derselbe Host.
	body := do("PATCH", "/api/v1/settings/global", `{"public_agent_url":"https://lcm.example.org:9320"}`)
	if status() != 422 || body["code"] != "settings.public_host_conflict" {
		t.Fatalf("%d %v, erwartet 422 settings.public_host_conflict", status(), body)
	}
	params, _ := body["params"].(map[string]any)
	if params["host"] != "lcm.example.org" || params["first"] != "UI" || params["second"] != "Agent" {
		t.Errorf("params = %v", params)
	}

	// Mit MCP ebenso, auch solange MCP aus ist: Der Name entscheidet am
	// gemeinsamen Port, welcher Dienst die Verbindung bekommt.
	body = do("PATCH", "/api/v1/settings/global", `{"public_mcp_url":"https://lcm.example.org"}`)
	if status() != 422 || body["code"] != "settings.public_host_conflict" {
		t.Errorf("MCP mit dem Namen der UI: %d %v, erwartet 422 settings.public_host_conflict", status(), body)
	}

	do("PATCH", "/api/v1/settings/global", `{"public_mcp_url":"https://mcp.example.org"}`)
	if status() != 200 {
		t.Errorf("MCP mit eigenem Namen: %d", status())
	}
}
