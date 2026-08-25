package services_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// sampleAcngReport ist ein realistischer Ausschnitt der echten apt-cacher-ng
// report.html (conf/report.html im Quellcode) nach Platzhalter-Ersetzung -
// genug, um den Statistik-Parser gegen das echte Format zu prüfen.
const sampleAcngReport = `<html><head><title>Apt-Cacher NG maintenance</title></head><body>
<h1>Apt-Cacher NG</h1>
<h2>Transfer statistics</h2>
<table>
<tr><td class="coltitle">Data fetched:</td>
<td class="colcont"><img src="x" width=42 height=11> 1.2 GB</td>
<td class="colcont"><img src="x" width=3 height=11> 45.3 MB</td></tr>
<tr><td class="coltitle">Data served:</td>
<td class="colcont"><img src="x" width=88 height=11> 3.4 GB</td>
<td class="colcont"><img src="x" width=9 height=11> 120.1 MB</td></tr>
</table>
</body></html>`

// joinLcmHost joint einen Server unter Host "localhost" (= LCM-Host) mit
// apt-Paketverwaltung, analog TestLcmHostSetup.
func joinLcmHost(t *testing.T, env *testEnv) uint {
	t.Helper()
	env.Dialer.Responses = stdScanResponses()
	env.Dialer.Responses["apt-get dnf zypper"] = sshx.FakeResponse{Output: "apt-get\n"}
	self, err := env.Servers.Join(services.JoinRequest{
		Name: "lcm-host", Host: "localhost", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join localhost: %v", err)
	}
	return self.ID
}

// TestAptCacherDetailNotInstalled: ohne apt-cacher-ng im Paketbestand bleibt
// die Detailseite auf "nicht installiert" - kein HTTP-/SSH-Aufruf nötig.
func TestAptCacherDetailNotInstalled(t *testing.T) {
	env := newTestEnv(t)
	id := joinLcmHost(t, env)

	detail, err := env.Servers.AptCacherDetail(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatalf("AptCacherDetail: %v", err)
	}
	if detail.Installed {
		t.Error("erwartet installed=false ohne apt-cacher-ng im Paketbestand")
	}
	if detail.Status != nil || detail.Stats != nil {
		t.Errorf("ohne Installation sollten Status/Stats leer bleiben: %+v", detail)
	}
}

// TestAptCacherDetailRejectsNonLcmHost: ein normaler Server bekommt
// ErrNotLcmHost, unabhängig vom Paketbestand.
func TestAptCacherDetailRejectsNonLcmHost(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.AptCacherDetail(repositories.ScopeAll(), id); err == nil {
		t.Fatal("erwartete einen Fehler für einen Nicht-LCM-Host")
	}
}

// TestAptCacherDetailWithStats: apt-cacher-ng ist installiert und per HTTP
// erreichbar - die Transfer-Statistiken werden aus dem Report geparst, und
// der "permanentes Caching"-Schalter per SSH ausgelesen.
func TestAptCacherDetailWithStats(t *testing.T) {
	env := newTestEnv(t)
	id := joinLcmHost(t, env)

	// apt-cacher-ng im erfassten Paketbestand hinterlegen.
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.ReplacePackages(id, []domain.Package{{Name: "apt-cacher-ng", Version: "3.7.4"}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleAcngReport))
	}))
	defer srv.Close()
	env.Servers.WithAptCacheURL(func() (string, error) { return srv.URL, nil })

	// SSH-Antwort: NO_CRON_RUN=1 ist gesetzt (permanentes Caching aktiv).
	// Kein Anführungszeichen im Substring-Schlüssel - das Kommando läuft
	// gewrappt (sudo sh -c '…'), die inneren Quotes werden dabei escaped.
	env.Dialer.Responses["NO_CRON_RUN=1"] = sshx.FakeResponse{Output: "yes\n", ExitCode: 0}

	detail, err := env.Servers.AptCacherDetail(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatalf("AptCacherDetail: %v", err)
	}
	if !detail.Installed {
		t.Fatal("erwartet installed=true")
	}
	if detail.Status == nil || !detail.Status.Running {
		t.Fatalf("erwartet einen laufenden Status, bekam %+v", detail.Status)
	}
	if detail.Stats == nil {
		t.Fatal("erwartete geparste Transfer-Statistiken")
	}
	if detail.Stats.DataFetched != "1.2 GB" || detail.Stats.DataFetchedRecent != "45.3 MB" {
		t.Errorf("data fetched falsch geparst: %+v", detail.Stats)
	}
	if detail.Stats.DataServed != "3.4 GB" || detail.Stats.DataServedRecent != "120.1 MB" {
		t.Errorf("data served falsch geparst: %+v", detail.Stats)
	}
	if !detail.PermanentCache {
		t.Error("erwartet permanent_cache=true (NO_CRON_RUN=1 gesetzt)")
	}
}

// TestRestartAptCacher: startet einen Job, der den Dienst neu startet - nur
// für den LCM-Host erlaubt.
func TestRestartAptCacher(t *testing.T) {
	env := newTestEnv(t)
	id := joinLcmHost(t, env)

	env.Dialer.Commands = nil
	job, err := env.Servers.RestartAptCacher(repositories.ScopeAll(), id, "admin")
	if err != nil {
		t.Fatalf("RestartAptCacher: %v", err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeScript)
	if done.ID != job.ID {
		t.Fatalf("unerwarteter Job: %+v", done)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "systemctl restart apt-cacher-ng") {
		t.Errorf("Neustart-Kommando fehlt:\n%s", all)
	}

	web01 := joinTestServer(t, env, "web01")
	if _, err := env.Servers.RestartAptCacher(repositories.ScopeAll(), web01, "admin"); err == nil {
		t.Error("erwartete Ablehnung auf einem Nicht-LCM-Host")
	}
}

// TestSetAptCacherPermanentCache: aktiviert und deaktiviert den Schalter -
// beide Skripte laufen als SSH-Job auf dem LCM-Host.
func TestSetAptCacherPermanentCache(t *testing.T) {
	env := newTestEnv(t)
	id := joinLcmHost(t, env)

	env.Dialer.Commands = nil
	if _, err := env.Servers.SetAptCacherPermanentCache(repositories.ScopeAll(), id, true, "admin"); err != nil {
		t.Fatalf("aktivieren: %v", err)
	}
	waitServerJob(t, env, id, domain.RuleTypeScript)
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "NO_CRON_RUN=1") {
		t.Errorf("erwartete NO_CRON_RUN=1 im Aktivierungs-Skript:\n%s", all)
	}

	env.Dialer.Commands = nil
	if _, err := env.Servers.SetAptCacherPermanentCache(repositories.ScopeAll(), id, false, "admin"); err != nil {
		t.Fatalf("deaktivieren: %v", err)
	}
	waitServerJob(t, env, id, domain.RuleTypeScript)
	all = strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "/^NO_CRON_RUN=/d") {
		t.Errorf("erwartete ein Lösch-sed im Deaktivierungs-Skript:\n%s", all)
	}
}
