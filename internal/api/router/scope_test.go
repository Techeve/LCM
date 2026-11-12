package router_test

import (
	"encoding/json"
	"fmt"
	"io"
	"testing"
)

// TestServerAppsNurImSichtbereich: Die Anwendungen eines Servers sieht nur,
// wer den Server sieht - wie bei jeder anderen Server-Ansicht.
func TestServerAppsNurImSichtbereich(t *testing.T) {
	tn := newTenants(t)
	if r := doRequest(t, tn.app, "GET", fmt.Sprintf("/api/v1/servers/%d/apps", tn.serverB), tn.a, ""); r.StatusCode != 404 {
		t.Errorf("fremder Server: erwartet 404, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, tn.app, "GET", fmt.Sprintf("/api/v1/servers/%d/apps", tn.serverA), tn.a, ""); r.StatusCode != 200 {
		body, _ := io.ReadAll(r.Body)
		t.Errorf("eigener Server: erwartet 200, bekam %d: %s", r.StatusCode, body)
	}
}

// TestGlobalePinsNurMitSichtAufAlleServer: Ein globaler Pin wirkt auf alle
// Server. Ein Gruppenverwalter legt Pins für seine Server an, globale aber
// weder an noch löscht er sie - auch nicht über einen eigenen Server.
func TestGlobalePinsNurMitSichtAufAlleServer(t *testing.T) {
	tn := newTenants(t)
	pins := fmt.Sprintf("/api/v1/servers/%d/packages/pins", tn.serverA)

	if r := doRequest(t, tn.app, "POST", pins, tn.a, `{"name":"curl","global":true}`); r.StatusCode != 403 {
		t.Errorf("globaler Pin als Verwalter: erwartet 403, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, tn.app, "POST", pins+"/kernel", tn.a, `{"global":true}`); r.StatusCode != 403 {
		t.Errorf("globaler Kernelschutz als Verwalter: erwartet 403, bekam %d", r.StatusCode)
	}
	own := createPin(t, tn, tn.a, pins, `{"name":"curl"}`)
	if r := doRequest(t, tn.app, "DELETE", fmt.Sprintf("%s/%d", pins, own), tn.a, ""); r.StatusCode != 200 {
		t.Errorf("eigenen Server-Pin löschen: erwartet 200, bekam %d", r.StatusCode)
	}

	global := createPin(t, tn, tn.admin, pins, `{"name":"openssl","global":true}`)
	if r := doRequest(t, tn.app, "DELETE", fmt.Sprintf("%s/%d", pins, global), tn.a, ""); r.StatusCode != 403 {
		t.Errorf("globalen Pin als Verwalter löschen: erwartet 403, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, tn.app, "DELETE", fmt.Sprintf("%s/%d", pins, global), tn.admin, ""); r.StatusCode != 200 {
		t.Errorf("globalen Pin als Admin löschen: erwartet 200, bekam %d", r.StatusCode)
	}
}

// createPin legt einen Pin an und liefert seine ID.
func createPin(t *testing.T, tn tenants, token, path, body string) uint {
	t.Helper()
	r := doRequest(t, tn.app, "POST", path, token, body)
	if r.StatusCode != 201 {
		text, _ := io.ReadAll(r.Body)
		t.Fatalf("Pin %s anlegen: erwartet 201, bekam %d: %s", body, r.StatusCode, text)
	}
	var pin struct {
		ID uint `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&pin); err != nil {
		t.Fatal(err)
	}
	return pin.ID
}
