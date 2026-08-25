package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProbeCrowdSecLapi deckt die drei Ausgänge des LAPI-Checks ab: Login OK,
// Login abgelehnt (erreichbar, aber unbrauchbar) und nicht erreichbar.
func TestProbeCrowdSecLapi(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/watchers/login" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			MachineID string `json:"machine_id"`
			Password  string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.MachineID == "lcm-managed" && body.Password == "geheim" {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "jwt"})
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	ok := probeCrowdSecLapi(srv.URL, "lcm-managed", "geheim")
	if !ok.Configured || !ok.Reachable || !ok.Running {
		t.Errorf("Login OK erwartet, bekam %+v", ok)
	}

	rejected := probeCrowdSecLapi(srv.URL, "lcm-managed", "falsch")
	if !rejected.Reachable || rejected.Running {
		t.Errorf("erreichbar-aber-abgelehnt erwartet, bekam %+v", rejected)
	}
	if rejected.HTTPStatus != http.StatusForbidden {
		t.Errorf("HTTPStatus = %d, erwartet 403", rejected.HTTPStatus)
	}

	// Server beendet → Verbindungsfehler.
	srv.Close()
	down := probeCrowdSecLapi(srv.URL, "lcm-managed", "geheim")
	if down.Reachable || down.Running {
		t.Errorf("nicht-erreichbar erwartet, bekam %+v", down)
	}
	if down.Message == "" {
		t.Error("Message fehlt bei nicht erreichbarer LAPI")
	}
}

// TestProbeCrowdSecLapiWrongService: unter der URL antwortet ein fremder
// Dienst (404 auf den Login-Pfad) - erreichbar, aber nicht „running".
func TestProbeCrowdSecLapiWrongService(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	st := probeCrowdSecLapi(srv.URL, "a", "b")
	if !st.Reachable || st.Running {
		t.Errorf("fremder Dienst: erreichbar-aber-nicht-running erwartet, bekam %+v", st)
	}
}
