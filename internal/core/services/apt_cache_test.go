package services_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"LCM/internal/core/services"
)

func TestUpdateGlobalValidatesAptCacheURL(t *testing.T) {
	env := newTestEnv(t)

	for _, bad := range []string{
		"192.168.1.10:3142",       // ohne Schema
		"ftp://cache:3142",        // falsches Schema
		"http://cache:3142/$(id)", // Subshell
		`http://cache:3142/"x"`,   // Quotes
		"http://cache :3142",      // Leerzeichen
	} {
		if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: &bad}, "admin"); !errors.Is(err, services.ErrAptCacheURLInvalid) {
			t.Errorf("%q: erwartet ErrAptCacheURLInvalid, bekommen %v", bad, err)
		}
	}

	// Gültige URL wird normalisiert gespeichert (trailing slash entfernt).
	updated, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: sp("http://192.168.1.10:3142/")}, "admin")
	if err != nil {
		t.Fatalf("gültige url abgelehnt: %v", err)
	}
	if updated.AptCacheURL != "http://192.168.1.10:3142" {
		t.Errorf("nicht normalisiert: %q", updated.AptCacheURL)
	}

	// Leer = Feature aus, immer gültig.
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: sp("")}, "admin"); err != nil {
		t.Errorf("leere url abgelehnt: %v", err)
	}
}

func TestCheckAptCache(t *testing.T) {
	env := newTestEnv(t)

	// Ohne konfigurierte URL: nicht konfiguriert, kein Fehler.
	status, err := env.Settings.CheckAptCache()
	if err != nil {
		t.Fatalf("check ohne url: %v", err)
	}
	if status.Configured || status.Reachable {
		t.Errorf("unkonfigurierter cache gemeldet als: %+v", status)
	}

	// Laufender apt-cacher-ng: Report-Seite antwortet mit 200.
	acng := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/acng-report.html" {
			_, _ = w.Write([]byte("<html>Apt-Cacher NG</html>"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer acng.Close()
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: &acng.URL}, "admin"); err != nil {
		t.Fatal(err)
	}
	status, err = env.Settings.CheckAptCache()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || !status.Reachable || !status.Running || status.HTTPStatus != 200 {
		t.Errorf("laufender cache nicht erkannt: %+v", status)
	}

	// Erreichbar, aber kein apt-cacher-ng (404 auf der Report-Seite).
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: &other.URL}, "admin"); err != nil {
		t.Fatal(err)
	}
	status, _ = env.Settings.CheckAptCache()
	if !status.Reachable || status.Running {
		t.Errorf("fremder http-dienst falsch gemeldet: %+v", status)
	}

	// Nicht erreichbar (Server geschlossen).
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: &deadURL}, "admin"); err != nil {
		t.Fatal(err)
	}
	status, _ = env.Settings.CheckAptCache()
	if status.Reachable || status.Running {
		t.Errorf("toter cache als erreichbar gemeldet: %+v", status)
	}
}

// TestCheckAptCacheForeign200 deckt die gehärtete Prüfung ab: ein fremder Dienst,
// der die Report-URL mit HTTP 200, aber ohne apt-cacher-ng-Inhalt beantwortet
// (z. B. ein Reverse-Proxy oder ein Dashboard-Login auf demselben Port), darf
// NICHT als „läuft" gelten - sonst bliebe ein stiller Fehlstart unbemerkt.
func TestCheckAptCacheForeign200(t *testing.T) {
	env := newTestEnv(t)

	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 200, aber der Body ist keine apt-cacher-ng-Report-Seite.
		_, _ = w.Write([]byte("<html><body>Welcome to nginx!</body></html>"))
	}))
	defer foreign.Close()

	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: &foreign.URL}, "admin"); err != nil {
		t.Fatal(err)
	}
	status, err := env.Settings.CheckAptCache()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Reachable {
		t.Errorf("fremder Dienst sollte erreichbar sein: %+v", status)
	}
	if status.Running {
		t.Errorf("fremder HTTP-200-Dienst darf nicht als laufender apt-cacher-ng gemeldet werden: %+v", status)
	}
}
