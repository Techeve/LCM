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

	out, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, nil, "admin")
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
	_, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, nil, "admin")
	if err == nil {
		t.Fatal("fehlgeschlagene umstellung muss als fehler gemeldet werden")
	}
}

// Scheitert ein Spiegel, muss die Meldung ihn nennen: Die Oberfläche zeigt im
// Fehlerfall nur diesen Text, nicht die Ausgabe des Skripts.
func TestSecureRepositoriesNamesFailedSource(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Responses["sed -i"] = sshx.FakeResponse{
		Output: "Err:2 https://de.archive.ubuntu.com/ubuntu noble InRelease\n" +
			"  Could not connect to de.archive.ubuntu.com:443\n" +
			"LCM: apt-update nach der umstellung fehlgeschlagen - alle aenderungen zurueckgerollt\n",
		ExitCode: 1,
	}
	_, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, nil, "admin")
	if err == nil || !strings.Contains(err.Error(), "https://de.archive.ubuntu.com/ubuntu noble") {
		t.Fatalf("meldung nennt die quelle nicht: %v", err)
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
	if _, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, nil, "admin"); err != nil {
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

// seedRepos legt den Quellen-Bestand des Servers an; http-Zeilen gelten als
// unverschlüsselt.
func seedRepos(t *testing.T, env *testEnv, id uint, lines ...string) {
	t.Helper()
	repos := make([]domain.AptRepository, 0, len(lines))
	for _, line := range lines {
		repos = append(repos, domain.AptRepository{Line: line, Insecure: strings.Contains(line, "http://")})
	}
	if err := repositories.NewServerRepository(env.DB()).ReplaceRepositories(id, repos); err != nil {
		t.Fatal(err)
	}
}

func dialedWith(env *testEnv, part string) []string {
	var found []string
	for _, c := range env.Dialer.Commands {
		if strings.Contains(c, part) {
			found = append(found, c)
		}
	}
	return found
}

const (
	landesSpiegel    = "http://de.archive.ubuntu.com/ubuntu"
	sicherheitsQuell = "http://security.ubuntu.com/ubuntu"
)

// TestHTTPSPruefungMerktQuellenOhneHTTPS: Die Prüfung läuft ohne root, meldet
// je Quelle ein Ergebnis samt Empfehlung und merkt sich die Quellen ohne HTTPS
// am Server.
func TestHTTPSPruefungMerktQuellenOhneHTTPS(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	seedRepos(t, env, id,
		"deb "+landesSpiegel+" noble main",
		"deb "+landesSpiegel+" noble-updates main",
		"deb "+sicherheitsQuell+" noble-security main",
		"deb https://download.docker.com/linux/ubuntu noble stable",
	)
	env.Dialer.Responses["apt-helper"] = sshx.FakeResponse{
		Output: "LCM-HTTPS fail " + landesSpiegel + "\nLCM-HTTPS ok " + sicherheitsQuell + "\n",
	}

	check, err := env.Servers.CheckRepositoriesHTTPS(repositories.ScopeAll(), id, "admin")
	if err != nil {
		t.Fatalf("prüfung: %v", err)
	}
	want := []services.RepoHTTPSResult{
		{URI: landesSpiegel, State: services.RepoHTTPSNone, Alternative: "https://archive.ubuntu.com/ubuntu"},
		{URI: sicherheitsQuell, State: services.RepoHTTPSOK},
	}
	if len(check.Results) != 2 || check.Results[0] != want[0] || check.Results[1] != want[1] {
		t.Errorf("ergebnis %+v, erwartet %+v", check.Results, want)
	}
	cmds := dialedWith(env, "apt-helper")
	if len(cmds) != 1 || strings.Contains(cmds[0], "sudo") {
		t.Errorf("erwartet eine Probe ohne sudo: %q", cmds)
	}
	// Je URL genau eine Probe, auch wenn sie in zwei Zeilen steht.
	if strings.Count(cmds[0], "probe '"+landesSpiegel+"'") != 1 {
		t.Errorf("landesspiegel nicht genau einmal geprüft: %s", cmds[0])
	}

	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if server.HTTPSUnavailableURLs != landesSpiegel {
		t.Errorf("gemerkt %q, erwartet %q", server.HTTPSUnavailableURLs, landesSpiegel)
	}
}

// TestHTTPSPruefungNurAufApt: gleiche Guards wie die Umstellung.
func TestHTTPSPruefungNurAufApt(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	setPackageManager(t, env, id, "dnf")
	if _, err := env.Servers.CheckRepositoriesHTTPS(repositories.ScopeAll(), id, "admin"); !errors.Is(err, services.ErrRepoPackageManagerMismatch) {
		t.Errorf("prüfung auf dnf: %v", err)
	}
}

// TestHTTPSUmstellungDerAuswahl: Nur die gewählte Quelle kommt ins Skript,
// der Landesspiegel ohne HTTPS bleibt draußen - und behält seine Markierung,
// solange er http ist.
func TestHTTPSUmstellungDerAuswahl(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	seedRepos(t, env, id, "deb "+landesSpiegel+" noble main", "deb "+sicherheitsQuell+" noble-security main")
	setServerFields(t, env, id, map[string]any{"https_unavailable_urls": landesSpiegel})
	env.Dialer.Responses["URLS="] = sshx.FakeResponse{Output: "LCM: ausgewaehlte paketquellen auf https umgestellt\n"}
	env.Dialer.Responses["@@@DEB822@@@"] = sshx.FakeResponse{
		Output: "deb " + landesSpiegel + " noble main\ndeb https://security.ubuntu.com/ubuntu noble-security main\n",
	}

	if _, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, []string{sicherheitsQuell}, "admin"); err != nil {
		t.Fatalf("umstellung: %v", err)
	}
	cmds := dialedWith(env, "URLS=")
	if len(cmds) != 1 || !strings.Contains(cmds[0], sicherheitsQuell) || strings.Contains(cmds[0], landesSpiegel) {
		t.Errorf("unerwartetes kommando: %q", cmds)
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if server.HTTPSUnavailableURLs != landesSpiegel {
		t.Errorf("markierung %q, erwartet %q", server.HTTPSUnavailableURLs, landesSpiegel)
	}
}

// TestHTTPSMarkierungFaelltMitDerQuelle: Ist der Landesspiegel getauscht, gibt
// es nichts mehr zu markieren.
func TestHTTPSMarkierungFaelltMitDerQuelle(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	setServerFields(t, env, id, map[string]any{"https_unavailable_urls": landesSpiegel})
	env.Dialer.Responses["sed -i"] = sshx.FakeResponse{Output: "LCM: alle paketquellen auf https umgestellt\n"}
	env.Dialer.Responses["@@@DEB822@@@"] = sshx.FakeResponse{Output: "deb https://archive.ubuntu.com/ubuntu noble main\n"}

	if _, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, nil, "admin"); err != nil {
		t.Fatalf("umstellung: %v", err)
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if server.HTTPSUnavailableURLs != "" {
		t.Errorf("markierung blieb stehen: %q", server.HTTPSUnavailableURLs)
	}
}

// TestHTTPSUmstellungLehntFremdeAuswahlAb: Was nicht als http-Quelle im
// Bestand steht, kommt nicht in die Kommandozeile - es läuft gar nichts.
func TestHTTPSUmstellungLehntFremdeAuswahlAb(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	seedRepos(t, env, id, "deb "+sicherheitsQuell+" noble-security main", "deb https://download.docker.com/linux/ubuntu noble stable")
	env.Dialer.Commands = nil

	for _, uri := range []string{"http://evil.example/x", "https://download.docker.com/linux/ubuntu", "https://security.ubuntu.com/ubuntu"} {
		_, err := env.Servers.SecureRepositories(repositories.ScopeAll(), id, []string{sicherheitsQuell, uri}, "admin")
		if !errors.Is(err, services.ErrRepoNotSecurable) {
			t.Errorf("%s: erwartet ErrRepoNotSecurable, war %v", uri, err)
		}
	}
	if len(env.Dialer.Commands) != 0 {
		t.Errorf("trotz Ablehnung lief etwas: %q", env.Dialer.Commands)
	}
}
