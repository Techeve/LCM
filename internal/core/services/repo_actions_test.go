package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

func TestSecureRepositoriesSwitchesToHTTPSAndRescans(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Das Umstell-Skript meldet Erfolg; der Rescan liefert nur noch https.
	env.Dialer.Responses["sed -i"] = sshx.FakeResponse{
		Output: "LCM: alle paketquellen auf https umgestellt\n",
	}
	env.Dialer.Responses["@@@DEB822@@@"] = sshx.FakeResponse{
		Output: "deb https://deb.debian.org/debian bookworm main\ndeb https://old.example/debian bookworm main\n",
	}

	out, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, "admin")
	if err != nil {
		t.Fatalf("secure fehlgeschlagen: %v", err)
	}
	if !strings.Contains(out, "umgestellt") {
		t.Errorf("unerwarteter output: %q", out)
	}
	// Nach dem Rescan darf keine Quelle mehr als unsicher gelten.
	repos, _ := env.Servers.Repositories(repositories.ScopeAll(), id)
	for _, r := range repos {
		if r.Insecure {
			t.Errorf("quelle nach umstellung noch unsicher: %q", r.Line)
		}
	}
}

func TestSecureRepositoriesReportsRollback(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// apt-get update schlägt fehl => Skript rollt zurück und endet mit exit 1.
	env.Dialer.Responses["sed -i"] = sshx.FakeResponse{
		Output:   "LCM: apt-update nach der umstellung fehlgeschlagen - alle aenderungen zurueckgerollt\n",
		ExitCode: 1,
	}
	_, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, "admin")
	if err == nil {
		t.Fatal("fehlgeschlagene umstellung muss als fehler gemeldet werden")
	}
}

func TestAddKnownRepository(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Unbekannter Katalog-Key wird abgelehnt.
	if _, err := env.Servers.AddKnownRepository(repositories.ScopeAll(), id, "gibtsnicht", "admin"); !errors.Is(err, services.ErrUnknownRepo) {
		t.Errorf("unbekanntes repo nicht abgelehnt: %v", err)
	}

	// Docker aus dem Katalog: Skript läuft durch, Rescan enthält die Quelle.
	env.Dialer.Responses["keyrings/docker.asc"] = sshx.FakeResponse{Output: "OK\n"}
	env.Dialer.Responses["@@@DEB822@@@"] = sshx.FakeResponse{
		Output: "deb https://deb.debian.org/debian bookworm main\ndeb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian bookworm stable\n",
	}
	out, err := env.Servers.AddKnownRepository(repositories.ScopeAll(), id, "docker", "admin")
	if err != nil {
		t.Fatalf("docker-repo einrichten: %v", err)
	}
	if out == "" {
		t.Error("kein output")
	}
	repos, _ := env.Servers.Repositories(repositories.ScopeAll(), id)
	found := false
	for _, r := range repos {
		if strings.Contains(r.Line, "download.docker.com") {
			found = true
			if r.Insecure {
				t.Error("docker-quelle (https) als unsicher markiert")
			}
		}
	}
	if !found {
		t.Errorf("docker-quelle fehlt nach rescan: %+v", repos)
	}
}

// TestAddKnownRepositoryRejectsPackageManagerMismatch: eine apt-Quelle lässt
// sich nicht auf einem pacman-Server einrichten - LCM weist das ab, statt ein
// unpassendes Repository zu hinterlassen.
func TestAddKnownRepositoryRejectsPackageManagerMismatch(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses = stdScanResponses()
	env.Dialer.Responses["apt-get dnf zypper"] = sshx.FakeResponse{Output: "pacman\n"}
	env.Dialer.Responses["pacman -Q 2>/dev/null"] = sshx.FakeResponse{Output: "bash 5.2-1\n"}
	srv, err := env.Servers.Join(services.JoinRequest{
		Name: "archbox", Host: "10.0.0.21", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("pacman-join sollte klappen: %v", err)
	}
	// docker ist eine apt-Quelle → passt nicht zu pacman.
	_, err = env.Servers.AddKnownRepository(repositories.ScopeAll(), srv.ID, "docker", "admin")
	if !errors.Is(err, services.ErrRepoPackageManagerMismatch) {
		t.Errorf("apt-Quelle auf pacman-Server sollte abgelehnt werden, bekam: %v", err)
	}
}

func TestKnownReposCatalogIsSecure(t *testing.T) {
	// Alle mitgelieferten Katalog-Quellen müssen https und signed-by verwenden.
	for _, r := range domain.DefaultKnownRepos() {
		if !strings.HasPrefix(r.KeyURL, "https://") {
			t.Errorf("%s: key-url nicht https: %q", r.Key, r.KeyURL)
		}
		if !strings.Contains(r.Line, "https://") {
			t.Errorf("%s: quelle nicht https: %q", r.Key, r.Line)
		}
		if !strings.Contains(r.Line, "signed-by=/etc/apt/keyrings/"+r.Key+".asc") {
			t.Errorf("%s: signed-by fehlt oder falscher keyring: %q", r.Key, r.Line)
		}
	}
}

// TestUmstellungMerktSichDieUmgestelltenQuellen: Nach der Umstellung muss der
// Server wissen, was sich zurückstellen ließe - sonst bliebe die Rückstellung
// eine Rateaufgabe. Ohne Protokoll (hier: der Rescan liefert nichts aus dem
// Sicherungsverzeichnis) sind es die Distributions-Spiegel.
func TestUmstellungMerktSichDieUmgestelltenQuellen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Responses["sed -i"] = sshx.FakeResponse{Output: "LCM: alle paketquellen auf https umgestellt\n"}
	env.Dialer.Responses["@@@DEB822@@@"] = sshx.FakeResponse{
		Output: "deb https://deb.debian.org/debian bookworm main\ndeb https://download.docker.com/linux/debian bookworm stable\n",
	}
	if _, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("secure fehlgeschlagen: %v", err)
	}

	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if server.HTTPSRevertURLs != "https://deb.debian.org/debian" {
		t.Errorf("Kandidaten = %q - die Fremdquelle gehört nicht dazu", server.HTTPSRevertURLs)
	}
}

// TestRueckstellungDrehtNurDieVorgemerktenQuellen: Der eigentliche Wunsch -
// die Standardquellen zurück auf http, die Fremdquelle mit eigenem https
// unangetastet.
func TestRueckstellungDrehtNurDieVorgemerktenQuellen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.UpdateFields(id, map[string]any{"https_revert_urls": "https://deb.debian.org/debian"}); err != nil {
		t.Fatal(err)
	}

	env.Dialer.Responses["URLS="] = sshx.FakeResponse{Output: "LCM: paketquellen auf http zurueckgestellt\n"}
	env.Dialer.Responses["@@@DEB822@@@"] = sshx.FakeResponse{
		Output: "deb http://deb.debian.org/debian bookworm main\ndeb https://download.docker.com/linux/debian bookworm stable\n",
	}

	out, err := env.Servers.RevertRepositoriesHTTPS(repositories.ScopeAll(), id, nil, "admin")
	if err != nil {
		t.Fatalf("Rückstellung fehlgeschlagen: %v", err)
	}
	if !strings.Contains(out, "zurueckgestellt") {
		t.Errorf("unerwarteter Output: %q", out)
	}

	// Das ausgeführte Kommando nennt genau eine URL - die Fremdquelle nicht.
	var cmd string
	for _, c := range env.Dialer.Commands {
		if strings.Contains(c, "URLS=") {
			cmd = c
		}
	}
	if !strings.Contains(cmd, "https://deb.debian.org/debian") {
		t.Errorf("unerwartetes Kommando: %q", cmd)
	}
	if strings.Contains(cmd, "docker.com") {
		t.Errorf("die Fremdquelle wurde mit zurückgestellt: %q", cmd)
	}

	// Nach dem Rescan ist die Standardquelle wieder http, die Fremdquelle nicht.
	repos, _ := env.Servers.Repositories(repositories.ScopeAll(), id)
	for _, r := range repos {
		wantInsecure := strings.Contains(r.Line, "deb.debian.org")
		if r.Insecure != wantInsecure {
			t.Errorf("%q: insecure=%v, erwartet %v", r.Line, r.Insecure, wantInsecure)
		}
	}
}

// TestRueckstellungOhneKandidatenLehntAb: Auf einem Server, für den nichts
// vorgemerkt ist, gibt es nichts zurückzustellen - und LCM fängt nicht an,
// Quellen auf Verdacht herunterzustufen.
func TestRueckstellungOhneKandidatenLehntAb(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	if _, err := env.Servers.RevertRepositoriesHTTPS(repositories.ScopeAll(), id, nil, "admin"); !errors.Is(err, services.ErrNoRevertCandidates) {
		t.Errorf("erwartet ErrNoRevertCandidates, war %v", err)
	}
}
