package router_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"LCM/internal/api/router"
	"LCM/internal/dbmetrics"
	"LCM/internal/storage"
)

// TestDatenbankMessung: lcm-probe holt die Momentaufnahme mit einem
// read-Schlüssel, Prometheus dieselben Zahlen als Text. Beides nur mit
// settings:manage.
func TestDatenbankMessung(t *testing.T) {
	// Die Momentaufnahme liest Seitenzahlen aus irgendeiner Datenbank; die
	// Zähler sammelt dbmetrics.Default, an der hängt auch die des Routers.
	metricsDB, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	app, db := buildTestApp(t, 0, nil, func(d *router.Deps) {
		d.DBMetrics = dbmetrics.NewReporter(dbmetrics.Default, metricsDB, "")
	})
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	key := createAPIKey(t, app, admin, `{"name":"lcm-probe","scope":"read"}`)

	resp := apiKeyRequest(t, app, "GET", "/api/v1/system/db", key, "")
	if resp.StatusCode != 200 {
		t.Fatalf("system/db: status %d", resp.StatusCode)
	}
	var snap dbmetrics.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Statements) == 0 || snap.File.PageSize == 0 {
		t.Fatalf("Momentaufnahme leer: %+v", snap.File)
	}

	resp = apiKeyRequest(t, app, "GET", "/metrics", key, "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "# TYPE lcm_db_lock_wait_seconds histogram") {
		t.Fatalf("metrics: status %d, %.200s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Content-Type %q", ct)
	}

	leser := readOnlyUserToken(t, app, db, admin)
	for _, path := range []string{"/api/v1/system/db", "/metrics"} {
		if resp := doRequest(t, app, "GET", path, "", ""); resp.StatusCode != 401 {
			t.Errorf("%s ohne Anmeldung: status %d, erwartet 401", path, resp.StatusCode)
		}
		if resp := doRequest(t, app, "GET", path, leser, ""); resp.StatusCode != 403 {
			t.Errorf("%s ohne settings:manage: status %d, erwartet 403", path, resp.StatusCode)
		}
	}
}
