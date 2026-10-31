package services_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// addAptCache legt einen Cache an und liefert ihn.
func addAptCache(t *testing.T, env *testEnv, name, url string) *domain.AptCache {
	t.Helper()
	cache, err := env.Settings.SaveAptCache(services.AptCacheInput{Name: name, URL: url}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func TestSaveAptCacheValidatesURL(t *testing.T) {
	env := newTestEnv(t)

	for _, bad := range []string{
		"192.168.1.10:3142",       // ohne Schema
		"ftp://cache:3142",        // falsches Schema
		"http://cache:3142/$(id)", // Subshell
		`http://cache:3142/"x"`,   // Quotes
		"http://cache :3142",      // Leerzeichen
	} {
		if _, err := env.Settings.SaveAptCache(services.AptCacheInput{Name: "x", URL: bad}, "admin"); !errors.Is(err, services.ErrAptCacheURLInvalid) {
			t.Errorf("%q: erwartet ErrAptCacheURLInvalid, bekommen %v", bad, err)
		}
	}

	// Gültige URL wird normalisiert gespeichert (trailing slash entfernt).
	cache := addAptCache(t, env, "Standort A", "http://192.168.1.10:3142/")
	if cache.URL != "http://192.168.1.10:3142" {
		t.Errorf("nicht normalisiert: %q", cache.URL)
	}
	// Name doppelt und leere URL werden abgelehnt.
	for _, in := range []services.AptCacheInput{{Name: "Standort A", URL: "http://10.0.0.2:3142"}, {Name: "B"}} {
		if _, err := env.Settings.SaveAptCache(in, "admin"); err == nil {
			t.Errorf("%+v angenommen", in)
		}
	}
}

// TestAptCachesSeveralAndChoice: Mehrere Caches nebeneinander; ohne Wahl gilt
// nur der einzige, bei mehreren wird nicht geraten.
func TestAptCachesSeveralAndChoice(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.Settings.AptCacheURL(0); !errors.Is(err, services.ErrNoAptCacheURL) {
		t.Fatalf("ohne cache: %v", err)
	}
	a := addAptCache(t, env, "Standort A", "http://10.0.0.1:3142")
	if url, _ := env.Settings.AptCacheURL(0); url != a.URL {
		t.Errorf("einziger cache nicht genommen: %q", url)
	}
	b := addAptCache(t, env, "Standort B", "http://10.0.0.2:3142")
	if _, err := env.Settings.AptCacheURL(0); !errors.Is(err, services.ErrNoAptCacheURL) {
		t.Errorf("bei zwei caches wurde geraten: %v", err)
	}
	if url, _ := env.Settings.AptCacheURL(b.ID); url != b.URL {
		t.Errorf("gewählter cache: %q", url)
	}
}

// TestLegacyAptCacheMigratesOnce: Die eine URL aus den alten Einstellungen
// wird zum „Standard-Cache", das alte Feld geleert.
func TestLegacyAptCacheMigratesOnce(t *testing.T) {
	env := newTestEnv(t)
	repo := repositories.NewSettingsRepository(env.DB())
	if err := repo.UpdateFields(map[string]any{"apt_cache_url": "http://10.0.0.9:3142"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := env.Settings.MigrateLegacyAptCache(); err != nil {
			t.Fatal(err)
		}
	}
	caches, _ := env.Settings.ListAptCaches()
	if len(caches) != 1 || caches[0].Name != "Standard-Cache" || caches[0].URL != "http://10.0.0.9:3142" {
		t.Fatalf("übernommen: %+v", caches)
	}
	if st, _ := repo.Get(); st.AptCacheURL != "" {
		t.Error("altes feld nicht geleert")
	}
}

func TestCheckAptCache(t *testing.T) {
	env := newTestEnv(t)

	// Laufender apt-cacher-ng: Report-Seite antwortet mit 200.
	acng := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/acng-report.html" {
			_, _ = w.Write([]byte("<html>Apt-Cacher NG</html>"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer acng.Close()
	status, err := env.Settings.CheckAptCache(addAptCache(t, env, "läuft", acng.URL).ID)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || !status.Reachable || !status.Running || status.HTTPStatus != 200 || status.Name != "läuft" {
		t.Errorf("laufender cache nicht erkannt: %+v", status)
	}

	// Erreichbar, aber kein apt-cacher-ng (404 auf der Report-Seite).
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	status, _ = env.Settings.CheckAptCache(addAptCache(t, env, "fremd", other.URL).ID)
	if !status.Reachable || status.Running {
		t.Errorf("fremder http-dienst falsch gemeldet: %+v", status)
	}

	// Nicht erreichbar (Server geschlossen).
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	status, _ = env.Settings.CheckAptCache(addAptCache(t, env, "tot", deadURL).ID)
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

	status, err := env.Settings.CheckAptCache(addAptCache(t, env, "fremd", foreign.URL).ID)
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
