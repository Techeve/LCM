package router_test

import (
	"fmt"
	"testing"
)

// TestReposHTTPSRoutenBrauchenSchreibrecht: Prüfung und Umstellung sind
// Schreibzugriffe - die Prüfung verbindet sich mit dem Server und merkt sich
// das Ergebnis.
func TestReposHTTPSRoutenBrauchenSchreibrecht(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")
	reader := readOnlyUserToken(t, app, db, admin)

	for _, path := range []string{"https-check", "secure"} {
		url := fmt.Sprintf("/api/v1/servers/%d/repositories/%s", id, path)
		if resp := doRequest(t, app, "POST", url, reader, ""); resp.StatusCode != 403 {
			t.Errorf("%s ohne servers:write: erwartet 403, bekam %d", path, resp.StatusCode)
		}
		if resp := doRequest(t, app, "POST", url, "", ""); resp.StatusCode != 401 {
			t.Errorf("%s ohne Token: erwartet 401, bekam %d", path, resp.StatusCode)
		}
	}
}

// TestReposHTTPSLehntUnpassendesAb: Eine Auswahl außerhalb der http-Quellen
// des Servers ist ein Fehler der Anfrage, die Prüfung gibt es nur für apt.
func TestReposHTTPSLehntUnpassendesAb(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	apt := addCapabilityServer(t, db, "web01", "apt")
	dnf := addCapabilityServer(t, db, "rhel01", "dnf")

	secure := fmt.Sprintf("/api/v1/servers/%d/repositories/secure", apt)
	resp := doRequest(t, app, "POST", secure, admin, `{"uris":["http://evil.example/x"]}`)
	if code := decodeMap(t, resp)["code"]; resp.StatusCode != 400 || code != "server.repo_not_securable" {
		t.Errorf("fremde auswahl: %d %v", resp.StatusCode, code)
	}
	check := fmt.Sprintf("/api/v1/servers/%d/repositories/https-check", dnf)
	resp = doRequest(t, app, "POST", check, admin, "")
	if code := decodeMap(t, resp)["code"]; resp.StatusCode != 422 || code != "repo.package_manager_mismatch" {
		t.Errorf("prüfung auf dnf: %d %v", resp.StatusCode, code)
	}
}
