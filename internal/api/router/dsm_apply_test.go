package router_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"LCM/internal/core/domain"
)

// DSM-Sollwerte: Starten braucht servers:write, nur erlaubte Schlüssel,
// nur DSM-Geräte. Ein Demo-Gerät startet den Job, kontaktiert aber nichts.
func TestDSMApplyEndpunkt(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	nas := addCapabilityServer(t, db, "nas01", "")
	if err := db.Model(&domain.Server{}).Where("id = ?", nas).
		Updates(map[string]any{"os_id": domain.OSIDSynologyDSM, "is_demo": true}).Error; err != nil {
		t.Fatal(err)
	}
	linux := addCapabilityServer(t, db, "web01", "apt")
	reader := readOnlyUserToken(t, app, db, admin)
	path := func(id uint) string { return fmt.Sprintf("/api/v1/servers/%d/dsm/apply", id) }

	tests := []struct {
		name, token, path, body string
		status                  int
		code                    string
	}{
		{"ohne servers:write", reader, path(nas), `{"keys":["csp"]}`, 403, ""},
		{"Firewall", admin, path(nas), `{"keys":["csp","firewall"]}`, 422, "dsm.firewall_not_settable"},
		{"unbekannter Schlüssel", admin, path(nas), `{"keys":["telnet"]}`, 422, "dsm.not_settable"},
		{"keine Schlüssel", admin, path(nas), `{}`, 400, "request.missing_fields"},
		{"Linux-Server", admin, path(linux), `{"keys":["csp"]}`, 409, "server.not_dsm"},
		{"unbekannter Server", admin, path(9999), `{"keys":["csp"]}`, 404, "server.not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doRequest(t, app, "POST", tt.path, tt.token, tt.body)
			if resp.StatusCode != tt.status {
				t.Fatalf("erwartet %d, bekam %d", tt.status, resp.StatusCode)
			}
			if tt.code != "" {
				assertErrorCode(t, resp, tt.code)
			}
		})
	}

	resp := doRequest(t, app, "POST", path(nas), admin, `{"keys":["dos","csp"]}`)
	if resp.StatusCode != 202 {
		t.Fatalf("Start: erwartet 202, bekam %d", resp.StatusCode)
	}
	var started struct {
		JobID   string `json:"job_id"`
		JobName string `json:"job_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil || started.JobID == "" {
		t.Fatalf("keine Job-ID: %v", err)
	}
	if started.JobName != "DSM-Sollwerte setzen @ nas01" {
		t.Errorf("Jobname %q", started.JobName)
	}
	waitJobDone(t, db, started.JobID)
}
