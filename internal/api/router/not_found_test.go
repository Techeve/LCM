package router_test

import (
	"fmt"
	"testing"
)

// TestMissingDeepScanReportIs404: Ein Lauf, den es nicht gibt, ist 404 - der
// rohe gorm-Fehler aus dem Repository wurde zum 500.
func TestMissingDeepScanReportIs404(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")

	path := fmt.Sprintf("/api/v1/servers/%d/deep-scan/reports/00000000-0000-4000-8000-000000000000", id)
	resp := doRequest(t, app, "GET", path, admin, "")
	if resp.StatusCode != 404 {
		t.Fatalf("status %d, erwartet 404", resp.StatusCode)
	}
	assertErrorCode(t, resp, "server.not_found")
}

// TestSecurityToolBansMapsErrors: Die Sperrliste meldet bekannte Fehler mit
// ihrem Code - vorher ging jeder als 500 hinaus.
func TestSecurityToolBansMapsErrors(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")

	cases := []struct {
		path   string
		status int
		code   string
	}{
		{"/api/v1/servers/4711/security-tool/bans?tool=fail2ban", 404, "server.not_found"},
		{fmt.Sprintf("/api/v1/servers/%d/security-tool/bans?tool=sshguard", id), 400, "security_tool.unknown"},
		{fmt.Sprintf("/api/v1/servers/%d/security-tool/bans?tool=fail2ban", id), 502, "server.action_failed"},
	}
	for _, tc := range cases {
		resp := doRequest(t, app, "GET", tc.path, admin, "")
		if resp.StatusCode != tc.status {
			t.Errorf("%s: status %d, erwartet %d", tc.path, resp.StatusCode, tc.status)
			continue
		}
		if tc.code != "" {
			assertErrorCode(t, resp, tc.code)
		}
	}
}
