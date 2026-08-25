package services_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// joinTestServer richtet den FakeDialer mit realistischen Scan-Antworten
// ein und joint einen Server.
func joinTestServer(t *testing.T, env *testEnv, name string) uint {
	t.Helper()
	env.Dialer.Responses = map[string]sshx.FakeResponse{
		"apt-get dnf zypper": {Output: "apt-get\n"}, // Debian → apt (Join prüft das)
		"sudo -n id -u":      {Output: "0\n"},       // Service-User erreicht root
		"os-release":         {Output: "NAME=\"Debian GNU/Linux\"\nVERSION=\"12 (bookworm)\"\n"},
		"uname -r":           {Output: "6.1.0-13-amd64\n"},
		"nproc":              {Output: "4\n"},
		"/^Mem:/":            {Output: "7861 2130\n"},
		"df -BM":             {Output: "40000 8000\n"},
		"hostname -I":        {Output: "10.0.0.5 \n"},
		"dpkg-query":         {Output: "nginx 1.22.1\nopenssl 3.0.11\n"},
		"apt list":           {Output: "Listing...\nopenssl/stable-security 3.0.14 amd64 [upgradable from: 3.0.11]\n"},
		"@@@DEB822@@@":       {Output: "deb https://deb.debian.org/debian bookworm main\ndeb http://old.example/debian bookworm main\n"},
	}
	// Host aus dem Namen ableiten, damit mehrere Test-Server EINDEUTIGE Hosts
	// haben (die Duplikat-Host-Prüfung im Join lehnt gleiche Hosts sonst ab).
	server, err := env.Servers.Join(services.JoinRequest{
		Name: name, Host: name + ".test", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join fehlgeschlagen: %v", err)
	}
	return server.ID
}

// stdScanResponses liefert die Standard-Scan-Antworten eines Debian-Servers
// für den FakeDialer (geteilt von den Join-Tests).
func stdScanResponses() map[string]sshx.FakeResponse {
	return map[string]sshx.FakeResponse{
		// Ein Debian-Server hat apt-get. Ohne diese Antwort erkennt der Join
		// keine unterstützte Paketverwaltung und lehnt ab - früher fiel LCM
		// hier still auf apt zurück, weshalb die Tests den Blindfleck nicht
		// bemerkten (BUG-012).
		"apt-get dnf zypper": {Output: "apt-get\n"},
		"sudo -n id -u":      {Output: "0\n"}, // Service-User erreicht root
		"os-release":         {Output: "NAME=\"Debian GNU/Linux\"\nVERSION=\"12 (bookworm)\"\n"},
		"uname -r":           {Output: "6.1.0-13-amd64\n"},
		"nproc":              {Output: "4\n"},
		"/^Mem:/":            {Output: "7861 2130\n"},
		"df -BM":             {Output: "40000 8000\n"},
		"hostname -I":        {Output: "10.0.0.5 \n"},
		"dpkg-query":         {Output: "nginx 1.22.1\nopenssl 3.0.11\n"},
		"apt list":           {Output: "Listing...\nopenssl/stable-security 3.0.14 amd64 [upgradable from: 3.0.11]\n"},
		"@@@DEB822@@@":       {Output: "deb https://deb.debian.org/debian bookworm main\n"},
	}
}

// TestJoinRejectsDuplicateHost: ein zweiter Join mit gleichem Host/IP (anderer
// Name) wird sofort mit ErrServerHostTaken abgelehnt - dieselbe Maschine soll
// nicht doppelt verwaltet werden.
func TestJoinRejectsDuplicateHost(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01") // Host "web01.test"

	env.Dialer.Responses = stdScanResponses()
	_, err := env.Servers.Join(services.JoinRequest{
		Name: "web01-kopie", Host: "web01.test", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if !errors.Is(err, services.ErrServerHostTaken) {
		t.Fatalf("erwartete ErrServerHostTaken, bekam %v", err)
	}
	// Auch mit Groß-/Kleinschreibung und Leerzeichen greift die Prüfung.
	_, err = env.Servers.Join(services.JoinRequest{
		Name: "web01-kopie2", Host: " WEB01.test ", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if !errors.Is(err, services.ErrServerHostTaken) {
		t.Fatalf("getrimmter/anders geschriebener Host sollte ebenfalls als Duplikat gelten, bekam %v", err)
	}
}

// TestLcmHostSetup: Nur der LCM-Host (localhost) bietet die Trivy-/apt-cacher-
// Einrichtung; auf einem normalen Server wird sie abgelehnt.
func TestLcmHostSetup(t *testing.T) {
	env := newTestEnv(t)

	// Normaler Server (Host 10.0.0.5) → keine LCM-Host-Aktion.
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.InstallTrivy(repositories.ScopeAll(), id, "admin"); !errors.Is(err, services.ErrNotLcmHost) {
		t.Fatalf("erwartete ErrNotLcmHost für 10.0.0.5, bekam %v", err)
	}
	if st, err := env.Servers.LcmHostStatus(repositories.ScopeAll(), id); err != nil || st.IsLcmHost {
		t.Fatalf("10.0.0.5 sollte nicht der LCM-Host sein: %+v (err %v)", st, err)
	}

	// LCM-Host (localhost) → Status meldet is_lcm_host; Trivy-Install startet
	// einen Job, der das Aqua-APT-Repo einbindet und trivy installiert.
	env.Dialer.Responses = stdScanResponses()
	// pkgmgr-Probe: apt-basiertes System (für requireLcmHostApt).
	env.Dialer.Responses["apt-get dnf zypper"] = sshx.FakeResponse{Output: "apt-get\n"}
	self, err := env.Servers.Join(services.JoinRequest{
		Name: "lcm-host", Host: "localhost", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join localhost: %v", err)
	}
	st, err := env.Servers.LcmHostStatus(repositories.ScopeAll(), self.ID)
	if err != nil || !st.IsLcmHost {
		t.Fatalf("localhost sollte der LCM-Host sein: %+v (err %v)", st, err)
	}

	env.Dialer.Commands = nil
	job, err := env.Servers.InstallTrivy(repositories.ScopeAll(), self.ID, "admin")
	if err != nil {
		t.Fatalf("InstallTrivy: %v", err)
	}
	done := waitServerJob(t, env, self.ID, domain.RuleTypeScript)
	if done.ID != job.ID {
		t.Fatalf("unerwarteter Job: %+v", done)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "trivy-repo") || !strings.Contains(all, "install -y trivy") {
		t.Errorf("Trivy-Install-Skript fehlt:\n%s", all)
	}

	// Auto-Einrichtung: Hooks setzen; nach erfolgreichem Install soll LCM den
	// CVE-Scan aktivieren und die lokale APT-Cache-URL eintragen.
	cveCh := make(chan struct{}, 1)
	urlCh := make(chan string, 1)
	env.Servers.WithLcmHostConfig(
		func() error { cveCh <- struct{}{}; return nil },
		func(u string) error { urlCh <- u; return nil },
	)

	if _, err := env.Servers.InstallAptCacher(repositories.ScopeAll(), self.ID, "admin"); err != nil {
		t.Fatalf("InstallAptCacher: %v", err)
	}
	select {
	case got := <-urlCh:
		if got != "http://10.0.0.5:3142" {
			t.Errorf("Cache-URL = %q, erwartet http://10.0.0.5:3142", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cache-URL wurde nach der Installation nicht gesetzt")
	}

	if _, err := env.Servers.InstallTrivy(repositories.ScopeAll(), self.ID, "admin"); err != nil {
		t.Fatalf("InstallTrivy (2. Lauf): %v", err)
	}
	select {
	case <-cveCh:
	case <-time.After(5 * time.Second):
		t.Fatal("CVE-Scan wurde nach der Trivy-Installation nicht aktiviert")
	}
}

// TestJoinWithSudoUserFeedsPassword deckt den gemeldeten Fall ab: Root-SSH ist
// deaktiviert, das Onboarding läuft über einen normalen Benutzer mit
// (passwort-pflichtigem) sudo. Das Passwort muss über stdin an `sudo -S`
// gehen - nie in die (protokollierte) Kommandozeile.
func TestJoinWithSudoUserFeedsPassword(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses = stdScanResponses()

	const pw = "s3cr3t-pw"
	_, err := env.Servers.Join(services.JoinRequest{
		Name: "web01", Host: "10.0.0.5", Port: 22, LoginUser: "deploy",
		LoginPassword: pw, ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join mit sudo-user fehlgeschlagen: %v", err)
	}

	joined := strings.Join(env.Dialer.Commands, "\n")
	// Provisionierung lief über password-sudo (sudo liest Passwort von stdin).
	if !strings.Contains(joined, "sudo -S -p '' sh -c") {
		t.Errorf("erwartete password-sudo im Provisioning, bekam:\n%s", joined)
	}
	// Das Passwort wurde per stdin gefüttert.
	fedPassword := false
	for _, in := range env.Dialer.Stdins {
		if in == pw+"\n" {
			fedPassword = true
		}
	}
	if !fedPassword {
		t.Errorf("sudo-passwort nicht über stdin übergeben: %v", env.Dialer.Stdins)
	}
	// ... und steht NICHT in der Kommandozeile (kein Leak ins Protokoll).
	if strings.Contains(joined, pw) {
		t.Error("passwort steht in der kommandozeile (leak)")
	}
}

// TestJoinFallsBackToSuWithoutSudo: hat der Login-Benutzer kein sudo, aber
// su-Zugang zu root (hier: ohne Passwort), läuft die Provisionierung per su.
func TestJoinFallsBackToSuWithoutSudo(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses = stdScanResponses()
	// sudo scheitert in beiden Varianten - su (Default-Antwort exit 0) greift.
	env.Dialer.Responses["sudo -S -p"] = sshx.FakeResponse{Output: "Sorry, try again.", ExitCode: 1}
	env.Dialer.Responses["sudo -n"] = sshx.FakeResponse{Output: "sudo: a password is required", ExitCode: 1}
	// Der provisionierte Service-User hat sehr wohl sudo (dafür schreibt der
	// Join ja /etc/sudoers.d/) - nur der LOGIN-Benutzer nicht. Der längere,
	// spezifischere Schlüssel sticht den allgemeinen "sudo -n" oben.
	env.Dialer.Responses["sudo -n id -u"] = sshx.FakeResponse{Output: "0\n"}

	_, err := env.Servers.Join(services.JoinRequest{
		Name: "web01", Host: "10.0.0.5", Port: 22, LoginUser: "tony",
		LoginPassword: "geheim", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join mit su-fallback fehlgeschlagen: %v", err)
	}

	// Die Provisionierung (useradd/sudoers) lief in einem su-Wrapper.
	provisioned := false
	for _, c := range env.Dialer.Commands {
		if strings.HasPrefix(c, "su root -c") && strings.Contains(c, "useradd") {
			provisioned = true
		}
	}
	if !provisioned {
		t.Errorf("provisionierung sollte per su laufen:\n%s", strings.Join(env.Dialer.Commands, "\n"))
	}
	// Passwort steht nie in einer Kommandozeile.
	if strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "geheim") {
		t.Error("passwort steht in der kommandozeile (leak)")
	}
}

// TestJoinFallsBackToLoginWithoutSudoOrSu: scheitern sowohl sudo als auch su
// (z. B. weil der Login-Benutzer nicht in der wheel-/sudo-Gruppe steckt),
// aber root hat kein Passwort gesetzt, läuft die Provisionierung über das
// login-Programm - eigene PAM-Policy, meist ohne Gruppen-Restriktion.
func TestJoinFallsBackToLoginWithoutSudoOrSu(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses = stdScanResponses()
	// sudo und su scheitern beide - nur login (Default-Antwort exit 0) greift.
	env.Dialer.Responses["sudo -S -p"] = sshx.FakeResponse{Output: "Sorry, try again.", ExitCode: 1}
	env.Dialer.Responses["sudo -n"] = sshx.FakeResponse{Output: "sudo: a password is required", ExitCode: 1}
	// Der provisionierte Service-User hat sehr wohl sudo (dafür schreibt der
	// Join ja /etc/sudoers.d/) - nur der LOGIN-Benutzer nicht. Der längere,
	// spezifischere Schlüssel sticht den allgemeinen "sudo -n" oben.
	env.Dialer.Responses["sudo -n id -u"] = sshx.FakeResponse{Output: "0\n"}
	env.Dialer.Responses["su root -c"] = sshx.FakeResponse{Output: "su: Permission denied", ExitCode: 1}

	_, err := env.Servers.Join(services.JoinRequest{
		Name: "web01", Host: "10.0.0.5", Port: 22, LoginUser: "tony",
		LoginPassword: "geheim", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join mit login-fallback fehlgeschlagen: %v", err)
	}

	// Die Provisionierung lief über "login root" - das eigentliche Skript
	// (inkl. useradd) steckt im stdin, nicht in der Kommandozeile.
	provisioned := false
	for i, c := range env.Dialer.Commands {
		if c == "login root" && strings.Contains(env.Dialer.Stdins[i], "useradd") {
			provisioned = true
		}
	}
	if !provisioned {
		t.Errorf("provisionierung sollte per 'login root' laufen:\n%s", strings.Join(env.Dialer.Commands, "\n"))
	}
	// Passwort steht nie in einer Kommandozeile.
	if strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "geheim") {
		t.Error("passwort steht in der kommandozeile (leak)")
	}
}

// TestJoinAsRootUsesNoSudo stellt sicher, dass beim Root-Login kein sudo-Wrapper
// (und kein stdin-Passwort) verwendet wird.
func TestJoinAsRootUsesNoSudo(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01") // joint als root
	if id == 0 {
		t.Fatal("join fehlgeschlagen")
	}
	joined := strings.Join(env.Dialer.Commands, "\n")
	// Kein Passwort-sudo (sudo -S -p) im gesamten Join. Passwortloses
	// `sudo sh -c` ist auf der Zertifikats-Verbindung des Service-Users
	// legitim (NOPASSWD-sudoers, z. B. für die Lauschport-Erkennung) -
	// nur die Provisionierungs-Schritte des Root-Logins müssen ohne sudo
	// auskommen (useradd/sudoers-Anlage laufen direkt als root).
	if strings.Contains(joined, "sudo -S -p") {
		t.Errorf("root-login sollte ohne passwort-sudo auskommen:\n%s", joined)
	}
	if strings.Contains(joined, "sudo sh -c 'id -u") {
		t.Errorf("provisionierung sollte beim root-login ohne sudo laufen:\n%s", joined)
	}
	for _, in := range env.Dialer.Stdins {
		if in != "" {
			t.Errorf("root-login sollte keinen stdin einspeisen, bekam %q", in)
		}
	}
}

// TestJoinAutoAssignsToSystemGroup prüft, dass ein neu gejointer Server
// automatisch der geschützten System-Gruppe zugeordnet wird.
func TestJoinAutoAssignsToSystemGroup(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	groups, err := env.Groups.List(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	var sysID uint
	for _, g := range groups {
		if g.IsSystem {
			sysID = g.ID
		}
	}
	if sysID == 0 {
		t.Fatal("system-gruppe nicht gefunden")
	}
	full, err := env.Groups.Get(repositories.ScopeAll(), sysID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range full.Servers {
		if s.ID == id {
			found = true
		}
	}
	if !found {
		t.Error("neuer server wurde nicht automatisch der system-gruppe zugeordnet")
	}
}

// TestHardenAndUnhardenSSH prüft, dass die SSH-Härtung gesetzt und wieder
// aufgehoben werden kann (Drop-in schreiben bzw. entfernen, Flag toggeln).
func TestHardenAndUnhardenSSH(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Der Zyklus härten→aufheben setzt eine BELEGTE Härtung voraus: seit
	// R2-014 gilt eine ergebnislose Wirkungskontrolle nicht mehr als Erfolg,
	// und der Fake-Dialer antwortet ohne diesen Eintrag mit leerer Ausgabe.
	env.Dialer.Responses["^passwordauthentication"] = sshx.FakeResponse{
		Output: "passwordauthentication no\n",
	}
	env.Dialer.Commands = nil
	if _, err := env.Servers.HardenSSH(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("härten: %v", err)
	}
	if srv, _ := env.Servers.Get(repositories.ScopeAll(), id); !srv.SSHHardened {
		t.Error("ssh_hardened nach härten nicht gesetzt")
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "60-lcm-hardening.conf") {
		t.Errorf("härtungs-drop-in nicht geschrieben: %v", env.Dialer.Commands)
	}

	env.Dialer.Commands = nil
	if _, err := env.Servers.UnhardenSSH(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("härtung aufheben: %v", err)
	}
	if srv, _ := env.Servers.Get(repositories.ScopeAll(), id); srv.SSHHardened {
		t.Error("ssh_hardened nach aufheben noch gesetzt")
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "rm -f /etc/ssh/sshd_config.d/60-lcm-hardening.conf") {
		t.Errorf("drop-in nicht entfernt: %s", all)
	}
}

func TestJoinServerScansAndEncrypts(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if server.OSName != "Debian GNU/Linux" || server.OSVersion != "12 (bookworm)" {
		t.Errorf("os falsch gescannt: %q / %q", server.OSName, server.OSVersion)
	}
	if server.CPUCores != 4 || server.MemTotalMB != 7861 || server.DiskTotalMB != 40000 {
		t.Errorf("hardware falsch: cores=%d mem=%d disk=%d", server.CPUCores, server.MemTotalMB, server.DiskTotalMB)
	}
	if server.PrivateKeyEnc == "" || server.PublicKey == "" {
		t.Error("keypair wurde nicht erzeugt")
	}
	// Private Key darf NICHT im Klartext (kein PEM-Header) gespeichert sein.
	if len(server.PrivateKeyEnc) > 0 && server.PrivateKeyEnc[:5] == "-----" {
		t.Error("private key liegt unverschlüsselt vor")
	}

	pkgs, _ := env.Servers.Packages(repositories.ScopeAll(), id)
	if len(pkgs) != 2 {
		t.Fatalf("erwartet 2 pakete, bekam %d", len(pkgs))
	}
	outdated, _ := env.Servers.OutdatedPackages(repositories.ScopeAll(), id)
	if len(outdated) != 1 || outdated[0].Name != "openssl" || !outdated[0].Security {
		t.Errorf("update-erkennung falsch: %+v", outdated)
	}
	repos, _ := env.Servers.Repositories(repositories.ScopeAll(), id)
	var insecure int
	for _, r := range repos {
		if r.Insecure {
			insecure++
		}
	}
	if insecure != 1 {
		t.Errorf("erwartet 1 unsichere repo-quelle, bekam %d", insecure)
	}
}

// TestReconnectOverwritesCredentialsKeepsIdentity: ein bestehender Server
// wird neu verbunden (z.B. ausgetauscht). Der Datensatz (ID) bleibt, aber
// Fingerprint und Zertifikat werden überschrieben; Gruppen-/User-Zuordnungen
// bleiben erhalten.
func TestReconnectOverwritesCredentialsKeepsIdentity(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	before, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := before.PrivateKeyEnc

	// Server "ausgetauscht": neuer Host-Key-Fingerprint. Der alte Eintrag
	// gilt als nicht mehr erreichbar - für einen laufend verwalteten Server
	// wird Reconnect seit R2-010 mit Begründung abgelehnt.
	env.Dialer.Fingerprint = "SHA256:NEUERfingerprintNACHaustausch1234567890abc"
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Update("reachable", false).Error; err != nil {
		t.Fatal(err)
	}

	server, err := env.Servers.Reconnect(repositories.ScopeAll(), services.ReconnectRequest{
		ID: id, LoginUser: "root", LoginPassword: "neues-passwort",
		ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("reconnect fehlgeschlagen: %v", err)
	}
	if server.ID != id {
		t.Errorf("identität verloren: id %d -> %d", id, server.ID)
	}
	if server.HostKeyFingerprint != env.Dialer.Fingerprint {
		t.Errorf("fingerprint nicht überschrieben: %q", server.HostKeyFingerprint)
	}
	if server.PrivateKeyEnc == oldKey || server.PrivateKeyEnc == "" {
		t.Error("zertifikat wurde nicht neu erzeugt/überschrieben")
	}
	if !server.Reachable {
		t.Error("server sollte nach reconnect erreichbar sein")
	}

	// Reconnect erzeugt einen protokollierten Job + Sessions.
	jobs, _, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{ServerID: id, Limit: 50})
	var reconnectJob bool
	for _, j := range jobs {
		if j.Type == "reconnect" {
			reconnectJob = true
		}
	}
	if !reconnectJob {
		t.Error("kein reconnect-job protokolliert")
	}
}

// TestReconnectRejectsFingerprintMismatch: der bestätigte Fingerprint muss
// zum tatsächlichen passen (MitM-Schutz auch beim Reconnect).
func TestReconnectRejectsFingerprintMismatch(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Update("reachable", false).Error; err != nil {
		t.Fatal(err)
	}

	_, err := env.Servers.Reconnect(repositories.ScopeAll(), services.ReconnectRequest{
		ID: id, LoginUser: "root", ConfirmedFingerprint: "SHA256:falscher-wert", Actor: "admin",
	})
	if !errors.Is(err, services.ErrFingerprintMismatch) {
		t.Errorf("erwartet ErrFingerprintMismatch, bekam %v", err)
	}
}

func TestJoinRejectsFingerprintMismatch(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.Servers.Join(services.JoinRequest{
		Name: "web01", Host: "10.0.0.5", LoginUser: "root",
		ConfirmedFingerprint: "SHA256:ganz-anderer-fingerprint", Actor: "admin",
	})
	if !errors.Is(err, services.ErrFingerprintMismatch) {
		t.Errorf("erwartet ErrFingerprintMismatch, bekam %v", err)
	}
}

func TestServerStatusTrafficLight(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Frisch gejoint: erreichbar, aber ein Security-Update offen => gelb.
	status, insights, _, err := env.Servers.Status(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if status != "yellow" {
		t.Errorf("erwartet gelb (offenes update), bekam %q", status)
	}
	if len(insights) == 0 {
		t.Error("gelber status ohne insight-begründung")
	}
}

func TestManagerScopeHidesForeignServers(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Ein Manager ohne Gruppenzuordnung darf den Server nicht sehen.
	_, err := env.Servers.Get(repositories.ScopeManager(999), id)
	if !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("tenant isolation verletzt: erwartet ErrNotFound, bekam %v", err)
	}
	list, _ := env.Servers.List(repositories.ScopeManager(999))
	if len(list) != 0 {
		t.Errorf("manager sieht %d fremde server (erwartet 0)", len(list))
	}
}

// TestDecommissionPurgesAllServerData: Beim Entfernen werden ALLE
// server-bezogenen Daten restlos gelöscht - Jobs UND SSH-Protokolle. Der
// Foreign-Key-Constraint darf dabei nicht brechen.
func TestDecommissionPurgesAllServerData(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Der Join hat Jobs und SSH-Protokoll-Sessions erzeugt.
	jobsBefore, _, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{ServerID: id, Limit: 50})
	if len(jobsBefore) == 0 {
		t.Fatal("erwartete mindestens den onboarding-job")
	}
	sessBefore, _ := env.SSHLogs.ServerSessions(repositories.ScopeAll(), id, 0)
	if len(sessBefore) == 0 {
		t.Fatal("erwartete join-protokolle")
	}

	// Einfaches Entfernen (ohne Ziel-Bereinigung).
	if _, err := env.Servers.Decommission(repositories.ScopeAll(), id, "admin", services.DecommissionOptions{}); err != nil {
		t.Fatalf("decommission fehlgeschlagen: %v", err)
	}

	// Server ist weg ...
	if _, err := env.Servers.Get(repositories.ScopeAll(), id); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("server sollte gelöscht sein, bekam %v", err)
	}
	// ... und die Jobs dieses Servers ebenso.
	jobsAfter, _, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{Limit: 50})
	for _, j := range jobsAfter {
		if j.ServerID != nil && *j.ServerID == id {
			t.Errorf("job %s des servers nicht gelöscht", j.ID)
		}
	}
	// ... und keine SSH-Kommandos/-Sessions bleiben zurück.
	var sessions, commands int64
	env.DB().Table("ssh_sessions").Where("server_id = ?", id).Count(&sessions)
	env.DB().Table("ssh_commands").Count(&commands)
	if sessions != 0 {
		t.Errorf("%d ssh-sessions nach löschen übrig", sessions)
	}
	if commands != 0 {
		t.Errorf("%d ssh-kommandos nach löschen übrig (verwaiste kinder)", commands)
	}
}

// TestDecommissionPurgeTargetRemovesUsersAndCredentials: Mit PurgeTarget
// werden auf dem Zielserver die provisionierten Benutzer und die LCM-Zugänge
// entfernt (userdel + authorized_keys), bevor lokal gelöscht wird.
func TestDecommissionPurgeTargetRemovesUsersAndCredentials(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)

	// Einen Linux-Benutzer provisionieren, damit er beim Purge zu entfernen ist.
	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "purgeme", FullName: "Purge Me", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, lu.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	// Kommando-Log der Fake-Verbindung ab hier beobachten.
	env.Dialer.Commands = nil

	if _, err := env.Servers.Decommission(repositories.ScopeAll(), id, "admin", services.DecommissionOptions{PurgeTarget: true}); err != nil {
		t.Fatalf("decommission mit purge fehlgeschlagen: %v", err)
	}

	// Server ist lokal weg.
	if _, err := env.Servers.Get(repositories.ScopeAll(), id); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("server sollte gelöscht sein, bekam %v", err)
	}

	// Die Ziel-Bereinigung muss den provisionierten Benutzer, den Service-User
	// und dessen authorized_keys entfernt haben.
	all := strings.Join(env.Dialer.Commands, "\n")
	for _, want := range []string{
		"userdel -r purgeme",
		"userdel -rf lcm-svc",
		"/home/lcm-svc/.ssh/authorized_keys",
		"rm -f /etc/sudoers.d/lcm-svc",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("bereinigung enthielt %q nicht.\nKommandos:\n%s", want, all)
		}
	}
}

func TestConcurrencyLockBlocksSecondJob(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Erster Job läuft (running).
	first, err := env.Jobs.Start(&id, nil, "script", "Job A", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "running" {
		t.Fatalf("erster job sollte laufen, ist %q", first.Status)
	}
	// Zweiter Job auf demselben Server wird blockiert.
	_, err = env.Jobs.Start(&id, nil, "script", "Job B", "admin")
	if !errors.Is(err, services.ErrServerBusy) {
		t.Errorf("concurrency-lock griff nicht: %v", err)
	}
}

// TestJoinRejectsUnknownPackageManager bildet ein System nach, dessen
// Paketverwaltung LCM gar nicht ermitteln kann (kein apt/dnf/zypper/yum/
// pacman/apk gefunden). Früher fiel LCM bei Unbekanntem still auf apt-get
// zurück, meldete den Join als Erfolg und der Server blieb dauerhaft ohne
// Paketbestand - und wirkte dadurch sogar besonders gesund (BUG-012). Jetzt
// wird er abgelehnt, und es bleibt weder ein Datensatz in LCM noch ein Konto
// auf dem Zielsystem zurück.
func TestJoinRejectsUnknownPackageManager(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses = stdScanResponses()
	// Keine der abgefragten Paketverwaltungen ist vorhanden → leere Ausgabe.
	env.Dialer.Responses["apt-get dnf zypper"] = sshx.FakeResponse{Output: "\n"}

	_, err := env.Servers.Join(services.JoinRequest{
		Name: "exoten-os", Host: "10.0.0.9", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err == nil {
		t.Fatal("erwartete eine Ablehnung - keine bekannte Paketverwaltung")
	}
	if !strings.Contains(err.Error(), "nicht unterstützt") {
		t.Errorf("die Meldung soll die fehlende Unterstützung benennen, bekam: %v", err)
	}

	// Kein halb angelegter Server (BUG-006: "leere Hülle").
	if servers, _ := env.Servers.List(repositories.ScopeAll()); len(servers) != 0 {
		t.Errorf("ein abgelehnter Join darf keinen Server hinterlassen, fand %d", len(servers))
	}
	// Rücknahme auf dem Zielsystem gelaufen (BUG-009).
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "rm -f /etc/sudoers.d/lcm-svc") {
		t.Errorf("sudoers-Eintrag wurde nicht zurückgenommen:\n%s", all)
	}
	if !strings.Contains(all, "userdel") {
		t.Errorf("Service-User wurde nicht zurückgenommen:\n%s", all)
	}
}

// TestJoinAcceptsPacman belegt den Gegenpol: seit dem Voll-Support werden
// Arch-Systeme (pacman) aufgenommen statt abgewiesen - mit erkannter
// Paketverwaltung und eingelesenem Paketbestand aus `pacman -Q`.
func TestJoinAcceptsPacman(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses = stdScanResponses()
	env.Dialer.Responses["apt-get dnf zypper"] = sshx.FakeResponse{Output: "pacman\n"}
	// pacman -Q: installierte Pakete (Name Version). "pacman -Q 2>/dev/null"
	// ist KEIN Teilstring von "pacman -Qu …" → trifft nur die Bestandsabfrage.
	env.Dialer.Responses["pacman -Q 2>/dev/null"] = sshx.FakeResponse{Output: "bash 5.2.021-1\nopenssl 3.2.1-1\n"}
	env.Dialer.Responses["pacman -Qu"] = sshx.FakeResponse{Output: "openssl 3.2.1-1 -> 3.2.2-1\n"}

	srv, err := env.Servers.Join(services.JoinRequest{
		Name: "archbox", Host: "10.0.0.11", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("pacman-System sollte aufgenommen werden, bekam: %v", err)
	}
	if srv.PackageManager != "pacman" {
		t.Errorf("PackageManager sollte pacman sein, war %q", srv.PackageManager)
	}
	pkgs, _ := env.Servers.Packages(repositories.ScopeAll(), srv.ID)
	if len(pkgs) != 2 {
		t.Fatalf("erwartete 2 Pakete aus pacman -Q, fand %d", len(pkgs))
	}
	var openssl *domain.Package
	for i := range pkgs {
		if pkgs[i].Name == "openssl" {
			openssl = &pkgs[i]
		}
	}
	if openssl == nil || openssl.CandidateVersion != "3.2.2-1" {
		t.Errorf("openssl sollte ein Update auf 3.2.2-1 zeigen, bekam %+v", openssl)
	}
}

// TestJoinRejectsWhenServiceUserHasNoRoot bildet den Proxmox-Fall nach
// (BUG-019): /etc/sudoers.d/ existiert, das Programm sudo aber nicht. Der
// sudoers-Eintrag ließ sich schreiben, der Join galt als gelungen - und
// danach scheiterte jede Aktion mit "sudo: command not found".
func TestJoinRejectsWhenServiceUserHasNoRoot(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses = stdScanResponses()
	// Login als root (Provisionierung klappt), aber der Service-User bekommt
	// kein sudo - genau die Proxmox-Konstellation.
	env.Dialer.Responses["sudo -n id -u"] = sshx.FakeResponse{
		Output: "bash: line 1: sudo: command not found", ExitCode: 127,
	}

	_, err := env.Servers.Join(services.JoinRequest{
		Name: "pve", Host: "10.0.0.241", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err == nil {
		t.Fatal("erwartete eine Ablehnung - der Service-User erreicht kein root")
	}
	if !strings.Contains(err.Error(), "sudo") {
		t.Errorf("die Meldung soll sudo als Ursache benennen, bekam: %v", err)
	}
	if servers, _ := env.Servers.List(repositories.ScopeAll()); len(servers) != 0 {
		t.Errorf("ein abgelehnter Join darf keinen Server hinterlassen, fand %d", len(servers))
	}
}

// TestJoinProvisionsWithoutHardcodedTools: die Provisionierung darf weder
// useradd noch systemctl voraussetzen (BUG-008) und muss die Passwortsperre
// des neuen Kontos aufheben (BUG-007), sonst verweigert ein sshd mit
// "UsePAM no" den Key-Login trotz korrekter authorized_keys.
func TestJoinProvisionsWithoutHardcodedTools(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "command -v useradd") || !strings.Contains(all, "adduser -D") {
		t.Errorf("Benutzeranlage soll das Werkzeug nach Verfügbarkeit wählen:\n%s", all)
	}
	if !strings.Contains(all, "usermod -p") {
		t.Errorf("Passwortsperre des Service-Users wird nicht aufgehoben:\n%s", all)
	}
	if !strings.Contains(all, "mkdir -p /etc/sudoers.d") {
		t.Errorf("/etc/sudoers.d wird nicht angelegt (fehlt auf Systemen ohne sudo):\n%s", all)
	}
}

// TestHardenSSHFailsWhenIneffective bildet den openSUSE-Fall nach (BUG-026):
// sshd_config bindet das Drop-in-Verzeichnis nicht ein, die Datei wird
// geschrieben und nie gelesen, der Reload gelingt - und LCM meldete
// "gehärtet", während die Passwort-Anmeldung offen blieb. Bei einer
// Sicherheitsfunktion ist eine unbelegte Erfolgsmeldung schlimmer als ein
// ehrlicher Fehlschlag: wer "gehärtet" liest, schaut nicht mehr hin.
func TestHardenSSHFailsWhenIneffective(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Der Dienst meldet weiterhin Passwort-Anmeldung.
	env.Dialer.Responses["^passwordauthentication"] = sshx.FakeResponse{
		Output: "passwordauthentication yes\n",
	}

	_, err := env.Servers.HardenSSH(repositories.ScopeAll(), id, "admin")
	if err == nil {
		t.Fatal("erwartete einen Fehler - die Härtung wirkt nicht")
	}
	if !strings.Contains(err.Error(), "wirkt aber nicht") {
		t.Errorf("die Meldung soll die fehlende Wirkung benennen, bekam: %v", err)
	}
	// Und der Server darf nicht als gehärtet geführt werden.
	if srv, _ := env.Servers.Get(repositories.ScopeAll(), id); srv.SSHHardened {
		t.Error("ssh_hardened darf ohne nachgewiesene Wirkung nicht gesetzt sein")
	}
}

// TestHardenSSHSucceedsWhenEffective: meldet der Dienst nach dem Neustart
// tatsächlich "no", gilt die Härtung als belegt.
func TestHardenSSHSucceedsWhenEffective(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Responses["^passwordauthentication"] = sshx.FakeResponse{
		Output: "passwordauthentication no\n",
	}
	env.Dialer.Commands = nil

	if _, err := env.Servers.HardenSSH(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("härten: %v", err)
	}
	if srv, _ := env.Servers.Get(repositories.ScopeAll(), id); !srv.SSHHardened {
		t.Error("ssh_hardened nach belegter Härtung nicht gesetzt")
	}

	all := strings.Join(env.Dialer.Commands, "\n")
	// Include-Zeile wird sichergestellt (sonst läge das Drop-in ungelesen da).
	if !strings.Contains(all, "Include /etc/ssh/sshd_config.d/*.conf") {
		t.Errorf("Include-Zeile wird nicht sichergestellt:\n%s", all)
	}
	// Neustart ohne feste systemd-Annahme (BUG-027).
	if !strings.Contains(all, "rc-service sshd") {
		t.Errorf("Neustart deckt OpenRC nicht ab:\n%s", all)
	}
	// Die Wirkung wird überhaupt geprüft.
	if !strings.Contains(all, "sshd -T") {
		t.Errorf("effektive Konfiguration wird nicht ausgewertet:\n%s", all)
	}
}

// TestConfigureFirewallInstallFailureIsHonest: von 14 Testsystemen hatten nur
// drei überhaupt ufw; früher antwortete LCM dort mit "200 ok" und versteckte
// die eigentliche Information im Freitext (BUG-024). Heute installiert LCM
// das vorgesehene Werkzeug selbst - schlägt DIE Installation fehl, muss der
// Job ehrlich scheitern und der Status darf nicht auf "abgesichert" springen.
func TestConfigureFirewallInstallFailureIsHonest(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "rocky01")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Updates(map[string]any{"os_id": "rocky", "package_manager": "dnf", "firewall_tool": ""}).Error; err != nil {
		t.Fatal(err)
	}

	// Kein Werkzeug vorhanden, und die firewalld-Installation scheitert.
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "none\n"}
	env.Dialer.Responses["dnf install -y"] = sshx.FakeResponse{
		Output: "LCM-FEHLER: Firewall-Werkzeug konnte nicht installiert werden\n", ExitCode: 1,
	}

	job, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "80,443", domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusFailed {
		t.Fatalf("fehlgeschlagene installation muss den job scheitern lassen: %+v", done)
	}
	// Der Status darf dabei nicht fälschlich auf "abgesichert" springen.
	if srv, _ := env.Servers.Get(repositories.ScopeAll(), id); srv.FirewallActive {
		t.Error("firewall_active darf ohne firewall nicht gesetzt sein")
	}
}

// TestRestrictSudoFailureCarriesOutput: Ein fehlgeschlagener Lauf kam nur als
// nacktes "exit 1" an der API an. Weil der Aufruf synchron läuft und keinen
// Job in der Historie hinterlässt, war die Ursache danach nicht mehr
// rekonstruierbar - im Langzeittest blieb offen, warum ausgerechnet zwei
// Systeme scheiterten (BUG-031). Die Ausgabe des Zielsystems gehört deshalb in
// die Fehlermeldung.
func TestRestrictSudoFailureCarriesOutput(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Spezifischer Schlüssel: das Skript enthält auch "dpkg-query" (in der
	// sudoers-Whitelist), und bei mehreren Treffern gewinnt der längste.
	env.Dialer.Responses["visudo -cf /etc/sudoers.d/lcm-svc.tmp"] = sshx.FakeResponse{
		Output: "visudo: /etc/sudoers.d/lcm-svc:2 Syntaxfehler nahe 'LCM_ALLOWED'", ExitCode: 1,
	}

	_, err := env.Servers.RestrictSudo(repositories.ScopeAll(), id, "admin")
	if err == nil {
		t.Fatal("erwartete einen Fehler")
	}
	if !strings.Contains(err.Error(), "Syntaxfehler") {
		t.Errorf("die Ausgabe des Zielsystems fehlt in der Meldung: %v", err)
	}
	// Der Modus bleibt unverändert - der Fehlschlag ist transaktional sauber.
	if srv, _ := env.Servers.Get(repositories.ScopeAll(), id); srv.RestrictedSudo {
		t.Error("restricted_sudo darf nach einem Fehlschlag nicht gesetzt sein")
	}
}

// TestReconnectLehntLaufendeServerAb (R2-010): Reconnect ist das
// Neu-Onboarding eines ersetzten Servers - auf einem normal verwalteten,
// erreichbaren Server fragt es sonst scheinbar grundlos nach einem
// Admin-Passwort. Eingeschränkte Server bleiben die Ausnahme: ihr
// dokumentierter Rückweg in den Voll-Modus IST der Reconnect.
func TestReconnectLehntLaufendeServerAb(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	_, err := env.Servers.Reconnect(repositories.ScopeAll(), services.ReconnectRequest{
		ID: id, LoginPassword: "egal", Actor: "admin",
	})
	if err == nil || !strings.Contains(err.Error(), "normal verwaltet") {
		t.Fatalf("Reconnect auf erreichbarem Server muss mit Begründung abgelehnt werden, bekam: %v", err)
	}
}

// TestSandboxNachruesten: Auf Hosts, auf denen Trivy VOR der Sandbox
// eingerichtet wurde, fehlt bubblewrap und der Scanner läuft mit den Rechten
// von LCM. Die Nachrüstung installiert genau dieses eine Paket - Trivy selbst
// bleibt unangetastet, denn LCM sperrt es erst beim Aufruf ein.
func TestSandboxNachruesten(t *testing.T) {
	env := newTestEnv(t)

	// Auf einem normalen Server gibt es die Aktion nicht.
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.InstallSandbox(repositories.ScopeAll(), id, "admin"); !errors.Is(err, services.ErrNotLcmHost) {
		t.Fatalf("erwartete ErrNotLcmHost, bekam %v", err)
	}

	env.Dialer.Responses = stdScanResponses()
	env.Dialer.Responses["apt-get dnf zypper"] = sshx.FakeResponse{Output: "apt-get\n"}
	self, err := env.Servers.Join(services.JoinRequest{
		Name: "lcm-host", Host: "localhost", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join localhost: %v", err)
	}

	env.Dialer.Commands = nil
	if _, err := env.Servers.InstallSandbox(repositories.ScopeAll(), self.ID, "admin"); err != nil {
		t.Fatalf("InstallSandbox: %v", err)
	}
	waitServerJob(t, env, self.ID, domain.RuleTypeScript)
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "install -y bubblewrap") {
		t.Errorf("bubblewrap wird nicht installiert:\n%s", all)
	}
	if strings.Contains(all, "trivy") {
		t.Errorf("Trivy wird angefasst, obwohl nur die Sandbox fehlt:\n%s", all)
	}
}
