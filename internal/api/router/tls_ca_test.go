package router_test

import (
	"errors"
	"io"
	"testing"

	"LCM/internal/api/router"
)

const caPath = "/api/v1/system/tls/ca.pem"

// TestTLSCADownloadNurFuerAdmins: Die CA gibt es als PEM für Admins, nicht
// ohne Anmeldung und nicht für Manager.
func TestTLSCADownloadNurFuerAdmins(t *testing.T) {
	const caPEM = "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"
	app, _ := buildTestApp(t, 0, nil, func(d *router.Deps) {
		d.TLSCACert = func() ([]byte, error) { return []byte(caPEM), nil }
	})
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	resp := doRequest(t, app, "GET", caPath, admin, "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != caPEM {
		t.Fatalf("als Admin: %d %q", resp.StatusCode, body)
	}
	if r := doRequest(t, app, "GET", caPath, "", ""); r.StatusCode != 401 {
		t.Errorf("ohne Anmeldung: %d, erwartet 401", r.StatusCode)
	}
	user := `{"username":"manager1","password":"` + tenantPassword + `","roles":["manager"]}`
	if r := doRequest(t, app, "POST", "/api/v1/users", admin, user); r.StatusCode != 201 {
		t.Fatalf("user anlegen: %d", r.StatusCode)
	}
	if r := doRequest(t, app, "GET", caPath, loginToken(t, app, "manager1", tenantPassword), ""); r.StatusCode != 403 {
		t.Errorf("als Manager: %d, erwartet 403", r.StatusCode)
	}
}

// TestTLSCADownloadOhneCA: Ohne CA (--dev) antwortet die Route mit 404.
func TestTLSCADownloadOhneCA(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil, func(d *router.Deps) {
		d.TLSCACert = func() ([]byte, error) { return nil, errors.New("keine ca") }
	})
	if r := doRequest(t, app, "GET", caPath, loginToken(t, app, "admin", "test-admin-passwort"), ""); r.StatusCode != 404 {
		t.Errorf("ohne CA: %d, erwartet 404", r.StatusCode)
	}
}
