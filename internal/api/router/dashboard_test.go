package router_test

import (
	"encoding/json"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

func TestDashboardEndpunktImSichtbereich(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	for _, name := range []string{"gruppe-a", "gruppe-b"} {
		if r := doRequest(t, app, "POST", "/api/v1/users", admin,
			`{"username":"`+name+`","password":"Anker5-Leuchtturm!Wind","roles":["manager"]}`); r.StatusCode != 201 {
			t.Fatalf("user anlegen: %d", r.StatusCode)
		}
	}
	var a, b domain.User
	db.Where("username = ?", "gruppe-a").First(&a)
	db.Where("username = ?", "gruppe-b").First(&b)
	serverA := addCapabilityServer(t, db, "web-a", "apt")
	serverB := addCapabilityServer(t, db, "web-b", "apt")
	addServerToGroup(t, db, "Gruppe A", serverA, &a)
	addServerToGroup(t, db, "Gruppe B", serverB, &b)

	get := func(token string) (int, services.Dashboard) {
		resp := doRequest(t, app, "GET", "/api/v1/dashboard", token, "")
		var d services.Dashboard
		if resp.StatusCode == 200 {
			if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode, d
	}

	if status, _ := get(""); status != 401 {
		t.Errorf("ohne Anmeldung: %d, erwartet 401", status)
	}
	status, dash := get(loginToken(t, app, "gruppe-a", "Anker5-Leuchtturm!Wind"))
	if status != 200 {
		t.Fatalf("Manager A: %d", status)
	}
	if dash.Tiles.Total != 1 {
		t.Errorf("Manager A sieht %d Server, erwartet 1", dash.Tiles.Total)
	}
	for _, item := range dash.Attention {
		if item.ServerID != serverA {
			t.Errorf("Manager A sieht fremden Server %d unter „Aufmerksamkeit“", item.ServerID)
		}
	}
	if dash.RecentAlerts == nil || len(dash.RecentAlerts) != 0 {
		t.Errorf("recent_alerts ohne Recht muss eine leere Liste sein: %v", dash.RecentAlerts)
	}

	if _, all := get(admin); all.Tiles.Total != 2 {
		t.Errorf("Admin sieht %d Server, erwartet 2", all.Tiles.Total)
	}
}
