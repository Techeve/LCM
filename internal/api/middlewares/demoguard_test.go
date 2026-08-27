package middlewares

import "testing"

// TestDemoBlocked prüft die Sperrliste der öffentlichen Demo: Zugangs-
// Manipulation, Daten-Abfluss und ausgehende Verbindungen sind gesperrt,
// der normale Demo-Betrieb (Server-Aktionen, Lesen) bleibt frei.
func TestDemoBlocked(t *testing.T) {
	tests := []struct {
		method, path string
		blocked      bool
	}{
		// Zugangs-Manipulation.
		{"POST", "/api/v1/users/", true},
		{"POST", "/api/v1/users/2/reset-password", true},
		{"DELETE", "/api/v1/users/2", true},
		{"GET", "/api/v1/users/", false},
		{"POST", "/api/v1/auth/2fa/setup", true},
		{"POST", "/api/v1/auth/password-reset", true},
		{"POST", "/api/v1/auth/login", false},
		{"POST", "/api/v1/apikeys/", true},
		{"GET", "/api/v1/apikeys/", false},

		// Daten-Abfluss.
		{"GET", "/api/v1/system/backups", false},
		{"GET", "/api/v1/system/backups/x.lcmbak/download", true},
		{"POST", "/api/v1/system/backups/trigger-now", true},
		{"POST", "/api/v1/system/self-update", true},

		// Ausgehende Verbindungen zu Besucher-Zielen.
		{"POST", "/api/v1/servers/probe", true},
		{"POST", "/api/v1/servers/join", true},
		{"POST", "/api/v1/servers/7/reconnect", true},
		{"POST", "/api/v1/servers/7/dns-test", true},
		{"POST", "/api/v1/notification-channels/1/test", true},
		{"PATCH", "/api/v1/settings/global", true},
		{"GET", "/api/v1/settings/global", false},

		// Demo-Betrieb bleibt erlaubt.
		{"POST", "/api/v1/servers/7/update", false},
		{"POST", "/api/v1/servers/7/reboot", false},
		{"POST", "/api/v1/groups/", false},

		// Nicht-API-Pfade (SPA-Auslieferung) bleiben unberührt.
		{"GET", "/index.html", false},
	}
	for _, tt := range tests {
		if got := demoBlocked(tt.method, tt.path); got != tt.blocked {
			t.Errorf("demoBlocked(%s %s) = %v, erwartet %v", tt.method, tt.path, got, tt.blocked)
		}
	}
}
