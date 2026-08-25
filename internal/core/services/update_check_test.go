package services_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"
)

// withFakeRepo lenkt RepoBaseURL für die Testdauer auf einen lokalen
// Test-Server um, der body liefert (bzw. status, falls != 200). Der zuletzt
// angefragte Pfad landet in *gotPath - daran hängt die Prüfung, dass gegen
// den richtigen Kanal geschaut wird.
func withFakeRepo(t *testing.T, status int, body string) *string {
	t.Helper()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	orig := services.RepoBaseURL
	services.RepoBaseURL = srv.URL
	t.Cleanup(func() { services.RepoBaseURL = orig })
	return &gotPath
}

const samplePackagesIndex = `Package: irrelevant-package
Version: 9.9.9
Architecture: amd64

Package: lcm
Priority: optional
Section: admin
Architecture: amd64
Version: 42.7.3
Depends: adduser
Description: LCM - zentrale Verwaltung von Linux-Servern über SSH.

Package: another-package
Version: 1.0.0
`

// TestCheckForUpdateParsesRealPackagesFormat: die Version des lcm-Stanzas wird
// korrekt aus einem mehrere Pakete umfassenden Debian-Packages-Index gelesen
// (nicht das erste/letzte Paket im Index, RFC822-Stanza-Format).
func TestCheckForUpdateParsesRealPackagesFormat(t *testing.T) {
	withFakeRepo(t, http.StatusOK, samplePackagesIndex)
	env := newTestEnv(t)

	if err := env.Settings.CheckForUpdate(); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	st, err := env.Settings.UpdateStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.LatestVersion != "42.7.3" {
		t.Errorf("erwartet '42.7.3', bekam %q", st.LatestVersion)
	}
	if st.Error != "" {
		t.Errorf("kein fehler erwartet, bekam %q", st.Error)
	}
	if st.CheckedAt == nil {
		t.Error("checked_at sollte gesetzt sein")
	}
}

// TestCheckForUpdateAvailable: erkennt korrekt, ob die im Repo gefundene
// Version neuer ist als die laufende (SemVer-Vergleich).
func TestCheckForUpdateAvailable(t *testing.T) {
	newer := "999." + strings.TrimPrefix(version.Version, "v") // garantiert höher
	withFakeRepo(t, http.StatusOK, "Package: lcm\nVersion: "+newer+"\n")
	env := newTestEnv(t)

	if err := env.Settings.CheckForUpdate(); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	st, err := env.Settings.UpdateStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !st.UpdateAvailable {
		t.Errorf("erwartet update_available=true (aktuell %q, neu %q)", st.CurrentVersion, st.LatestVersion)
	}
	if st.CurrentVersion != version.Version {
		t.Errorf("current_version sollte version.Version sein, bekam %q", st.CurrentVersion)
	}
}

// TestCheckForUpdateHandlesMissingPackage: fehlt der lcm-Stanza im Index (z.B.
// falsches Repo/kaputte Antwort), landet ein Fehler im Status statt eines
// Absturzes - die letzte bekannte Version bleibt dabei unangetastet.
func TestCheckForUpdateHandlesMissingPackage(t *testing.T) {
	withFakeRepo(t, http.StatusOK, "Package: irgendwas-anderes\nVersion: 1.0.0\n")
	env := newTestEnv(t)

	if err := env.Settings.CheckForUpdate(); err == nil {
		t.Fatal("erwartete einen fehler (kein lcm-paket im index)")
	}
	st, err := env.Settings.UpdateStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.Error == "" {
		t.Error("erwartete eine fehlermeldung im status")
	}
	if st.LatestVersion != "" {
		t.Errorf("latest_version sollte leer bleiben, bekam %q", st.LatestVersion)
	}
}

// TestCheckForUpdateHandlesHTTPError: ein Repo-Fehler (z.B. 503) landet als
// Fehlertext im Status, statt den Aufrufer hart abbrechen zu lassen.
func TestCheckForUpdateHandlesHTTPError(t *testing.T) {
	withFakeRepo(t, http.StatusServiceUnavailable, "")
	env := newTestEnv(t)

	if err := env.Settings.CheckForUpdate(); err == nil {
		t.Fatal("erwartete einen fehler bei HTTP 503")
	}
	st, _ := env.Settings.UpdateStatus()
	if st.Error == "" {
		t.Error("erwartete eine fehlermeldung im status")
	}
}

// TestCheckForUpdateFolgtDemPaketkanal: Die Prüfung schaut in den Kanal, auf
// dem der Host tatsächlich steht - nicht immer in den Community-Kanal.
//
// Genau das war der Fehler: Wer auf Beta stand, bekam die Version aus
// „stable" gemeldet. Da die stabile Version einer Vorabversion typischerweise
// hinterherhinkt, sah es dort dauerhaft nach „alles aktuell" aus, obwohl im
// eigenen Kanal längst eine neuere Vorabversion lag.
func TestCheckForUpdateFolgtDemPaketkanal(t *testing.T) {
	cases := []struct {
		channel  string
		wantPath string
	}{
		{domain.AptChannelCommunity, "/dists/stable/main/binary-amd64/Packages"},
		{domain.AptChannelBeta, "/dists/beta/main/binary-amd64/Packages"},
	}
	for _, c := range cases {
		t.Run(c.channel, func(t *testing.T) {
			gotPath := withFakeRepo(t, http.StatusOK, "Package: lcm\nVersion: 42.7.3\n")
			env := newTestEnv(t)
			if err := repositories.NewSettingsRepository(env.DB()).UpdateFields(map[string]any{
				"subscription_apt_channel": c.channel,
			}); err != nil {
				t.Fatalf("kanal setzen: %v", err)
			}

			if err := env.Settings.CheckForUpdate(); err != nil {
				t.Fatalf("CheckForUpdate: %v", err)
			}
			if *gotPath != c.wantPath {
				t.Errorf("kanal %q: erwartete pfad %q, angefragt wurde %q", c.channel, c.wantPath, *gotPath)
			}
			st, _ := env.Settings.UpdateStatus()
			if st.Channel != c.channel {
				t.Errorf("der status sollte den geprüften kanal nennen: erwartet %q, bekam %q", c.channel, st.Channel)
			}
			if st.LatestVersion != "42.7.3" {
				t.Errorf("version aus dem kanal-index: bekam %q", st.LatestVersion)
			}
		})
	}
}

// TestHoechsteVersionAusDemIndex: Der Paket-Index führt alle vorgehaltenen
// Fassungen. Die erste zu nehmen hieße, sich auf die Reihenfolge des
// Repository-Servers zu verlassen - ordnet er anders, meldete LCM eine alte
// Version als „neueste" und nie wieder ein Update.
func TestHoechsteVersionAusDemIndex(t *testing.T) {
	index := "Package: lcm\nVersion: 1.28.0~beta.1\nArchitecture: amd64\n\n" +
		"Package: lcm-agent\nVersion: 9.9.9\nArchitecture: amd64\n\n" +
		"Package: lcm\nVersion: 1.30.0~beta.1\nArchitecture: amd64\n\n" +
		"Package: lcm\nVersion: 1.29.1~beta.1\nArchitecture: amd64\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(index))
	}))
	defer srv.Close()

	got, err := services.FetchLatestRepoVersionForTest(srv.URL, "", "")
	if err != nil {
		t.Fatalf("index lesen: %v", err)
	}
	if got != "1.30.0~beta.1" {
		t.Errorf("gelesen: %q, erwartet die höchste Fassung 1.30.0~beta.1", got)
	}
}
