package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// LCMs eigener APT-Cache leitete bis hierher auch HTTPS über den Proxy.
// apt-cacher-ng lässt CONNECT-Tunnel aber nur mit gesetztem
// PassThroughPattern zu - in der Standardkonfiguration ist die Direktive
// auskommentiert. Jede HTTPS-Paketquelle starb dann an „403 CONNECT denied",
// und weil `apt-get update` auch bei kaputten Quellen mit exit 0 endet, blieb
// der Job grün: Docker, Trivy und sogar LCMs eigenes Repo fielen still aus
// Inventar, Update- und CVE-Sicht (R2-038).

// TestAptProxyErkenntFehlendenHTTPSTunnel: meldet der Cache „CONNECT denied",
// muss LCM auf „nur HTTP" umstellen, statt die HTTPS-Quellen kaputt zu lassen.
func TestAptProxyErkenntFehlendenHTTPSTunnel(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{
		AptCacheURL: sp("http://cache.local:3142")}, "admin"); err != nil {
		t.Fatal(err)
	}
	env.Dialer.Commands = nil

	if _, err := env.Servers.ConfigureAptProxy(repositories.ScopeAll(), id, true, "admin"); err != nil {
		t.Fatalf("apt-proxy aktivieren: %v", err)
	}
	all := strings.Join(env.Dialer.Commands, "\n")

	if !strings.Contains(all, "CONNECT denied") {
		t.Error("das Skript prüft nicht, ob der Cache HTTPS-Tunnel beherrscht")
	}
	if !strings.Contains(all, `Acquire::https::Proxy "DIRECT"`) {
		t.Error("es gibt keinen Rückfall auf HTTPS-am-Proxy-vorbei - HTTPS-Quellen blieben unerreichbar")
	}
}

// TestAptProxyWertetDieAusgabeAus: `apt-get update` endet auch bei kaputten
// Paketquellen mit exit 0. Wer nur den Exit-Code prüft, meldet eine halb
// kaputte Anbindung als Erfolg - genau das war der stille Teil von R2-038.
func TestAptProxyWertetDieAusgabeAus(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{
		AptCacheURL: sp("http://cache.local:3142")}, "admin"); err != nil {
		t.Fatal(err)
	}
	env.Dialer.Commands = nil

	if _, err := env.Servers.ConfigureAptProxy(repositories.ScopeAll(), id, true, "admin"); err != nil {
		t.Fatalf("apt-proxy aktivieren: %v", err)
	}
	all := strings.Join(env.Dialer.Commands, "\n")

	// Ohne die Quotes: wrapSudo escaped einfache Anführungszeichen, der
	// Klartext des Musters bleibt aber erhalten.
	if !strings.Contains(all, `^(Err|E):`) {
		t.Errorf("die Ausgabe von apt-get update wird nicht auf Err-Zeilen geprüft:\n%s", all)
	}
	// Und bei Fehlern muss das Drop-in wieder verschwinden.
	if !strings.Contains(all, "rm -f /etc/apt/apt.conf.d/02lcm-apt-cache") {
		t.Error("kein Rückbau des Drop-ins bei fehlerhaften Paketquellen")
	}
}

// TestAptProxyMeldetDenGewaehltenModus: welcher Weg genommen wurde, gehört in
// die Job-Ausgabe - sonst bleibt unsichtbar, dass HTTPS ungecacht läuft.
func TestAptProxyMeldetDenGewaehltenModus(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{
		AptCacheURL: sp("http://cache.local:3142")}, "admin"); err != nil {
		t.Fatal(err)
	}
	env.Dialer.Commands = nil

	if _, err := env.Servers.ConfigureAptProxy(repositories.ScopeAll(), id, true, "admin"); err != nil {
		t.Fatalf("apt-proxy aktivieren: %v", err)
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "$LCM_MODE") {
		t.Error("die Erfolgsmeldung nennt den gewählten Modus nicht")
	}
}

// TestFail2banLaesstJailLocalInRuhe: LCM schrieb seine Vorlage unbedingt nach
// jail.local und überschrieb damit ersatzlos die Datei, in der
// Administratoren ihre eigenen Jails und Verschärfungen pflegen - ohne
// Sicherung, ohne Hinweis (R2-076). LCMs Werte gehören in ein eigenes
// Drop-in, das fail2ban zuletzt liest.
func TestFail2banLaesstJailLocalInRuhe(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Commands = nil

	job, err := env.Servers.ConfigureSecurityTool(repositories.ScopeAll(), id,
		services.SecurityToolInput{Tool: "fail2ban"}, "admin")
	if err != nil {
		t.Fatalf("fail2ban einrichten: %v", err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}
	all := strings.Join(env.Dialer.Commands, "\n")

	if !strings.Contains(all, "/etc/fail2ban/jail.d/99-lcm.local") {
		t.Error("LCM schreibt kein eigenes Drop-in")
	}
	if strings.Contains(all, "> /etc/fail2ban/jail.local") {
		t.Error("jail.local wird weiterhin überschrieben - die Konfiguration des Administrators ginge verloren")
	}
}

// TestCrowdSecWeistDieHerkunftAus: dass das Herstellerrepo eingerichtet
// wurde, sagt nichts darüber, ob es auch Pakete liefert. Auf Debian 13
// antwortet die Suite „trixie" mit 404, das Repo bleibt leer, und installiert
// wird die Fassung der Distribution - CrowdSec 1.4.6 von 2022 statt 1.7.x,
// von LCM als voller Erfolg quittiert (R2-077).
func TestCrowdSecWeistDieHerkunftAus(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Commands = nil

	job, err := env.Servers.ConfigureSecurityTool(repositories.ScopeAll(), id,
		services.SecurityToolInput{Tool: "crowdsec"}, "admin")
	if err != nil {
		t.Fatalf("crowdsec einrichten: %v", err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}
	all := strings.Join(env.Dialer.Commands, "\n")

	if !strings.Contains(all, "cscli version") {
		t.Error("die installierte Version wird nicht ausgewiesen")
	}
	if !strings.Contains(all, "packagecloud") || !strings.Contains(all, "LCM-WARNUNG") {
		t.Error("die Herkunft wird nicht geprüft - eine leere Fremdquelle bliebe unbemerkt")
	}
}
