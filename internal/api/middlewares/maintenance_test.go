package middlewares

import "testing"

// TestMaintenanceBlocked: Während des Updates ist alles Schreibende gesperrt,
// bis auf das, was zum Zuschauen und Abbrechen gehört.
func TestMaintenanceBlocked(t *testing.T) {
	cases := []struct {
		method, path string
		blocked      bool
	}{
		{"GET", "/api/v1/servers", false},
		{"POST", "/api/v1/servers/3/packages/upgrade-all", true},
		{"PATCH", "/api/v1/settings", true},
		{"DELETE", "/api/v1/servers/3", true},
		{"POST", "/api/v1/users/", true},
		{"POST", "/api/v1/auth/login", false},
		{"POST", "/api/v1/auth/logout", false},
		{"PATCH", "/api/v1/auth/me/preferences", false},
		{"POST", "/api/v1/events/ticket", false},
		{"DELETE", "/api/v1/system/self-update", false},
		{"POST", "/api/v1/system/self-update", false},
		{"POST", "/api/v1/system/release-notes/seen", false},
		{"POST", "/somewhere/else", false},
		{"POST", "/API/V1/servers/3/reboot", true},
		{"DELETE", "/Api/V1/System/Self-Update", false},
	}
	for _, c := range cases {
		if got := maintenanceBlocked(c.method, c.path); got != c.blocked {
			t.Errorf("%s %s: gesperrt=%v, erwartet %v", c.method, c.path, got, c.blocked)
		}
	}
}
