package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/advisories"
	"LCM/internal/storage/repositories"
)

// enableAdvisories schaltet die Frühwarnung ein (Standard ist aus).
func enableAdvisories(t *testing.T, env *testEnv, ttlMinutes int) {
	t.Helper()
	on := true
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{
		AdvisoryPollingEnabled:  &on,
		AdvisoryCacheTTLMinutes: &ttlMinutes,
	}, "admin"); err != nil {
		t.Fatalf("Einstellungen: %v", err)
	}
}

// verlaufsFilter liefert alle Befunde inklusive der behobenen.
func verlaufsFilter() repositories.AdvisoryFilter {
	return repositories.AdvisoryFilter{IncludeResolved: true, Page: 1, PageSize: 100}
}

// seedPackages legt einen Server mit Paketbestand an.
func seedPackages(t *testing.T, env *testEnv, name string, pkgs ...domain.Package) uint {
	t.Helper()
	id := joinTestServer(t, env, name)
	if err := repositories.NewServerRepository(env.DB()).ReplacePackages(id, pkgs); err != nil {
		t.Fatalf("Pakete setzen: %v", err)
	}
	return id
}

// TestAdvisoryPollStandardmaessigAus: Die Frühwarnung schickt den
// Paketbestand an einen fremden Dienst - ohne ausdrückliche Zustimmung des
// Betreibers darf keine einzige Abfrage rausgehen.
func TestAdvisoryPollStandardmaessigAus(t *testing.T) {
	env := newTestEnv(t)
	seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	out, err := env.Advisories.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if env.AdvSource.QueryCalls != 0 {
		t.Errorf("ohne Zustimmung darf nichts abgefragt werden, waren %d Aufrufe", env.AdvSource.QueryCalls)
	}
	if !strings.Contains(out, "ausgeschaltet") {
		t.Errorf("Zustand nicht benannt: %q", out)
	}
}

// TestAdvisoryPollLegtBefundAn prüft den Kernablauf: purl abfragen,
// Beschreibung nachholen, Befund je Server anlegen.
func TestAdvisoryPollLegtBefundAn(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 10)
	id := seedPackages(t, env, "web01",
		domain.Package{Name: "openssl", Version: "3.0.11"},
		domain.Package{Name: "bash", Version: "5.2"})

	env.AdvSource.QueryFunc = func(purls []string) (map[string][]advisories.Advisory, error) {
		out := map[string][]advisories.Advisory{}
		for _, p := range purls {
			if strings.Contains(p, "openssl") {
				out[p] = []advisories.Advisory{{ID: "CVE-2026-1"}}
			}
		}
		return out, nil
	}
	env.AdvSource.DetailsFunc = func(ids []string) (map[string]advisories.Detail, error) {
		return map[string]advisories.Detail{
			"CVE-2026-1": {
				ID: "CVE-2026-1", Severity: domain.SeverityCritical, Title: "Loch in openssl",
				FixedVersions: map[string]string{"openssl": "3.0.14"},
			},
		}, nil
	}

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	found, err := env.Advisories.ActiveForServer(id)
	if err != nil {
		t.Fatalf("ActiveForServer: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("erwartet 1 Befund, bekam %d", len(found))
	}
	f := found[0]
	if f.PackageName != "openssl" || f.InstalledVersion != "3.0.11" {
		t.Errorf("Paketbezug falsch: %+v", f)
	}
	if f.FixedVersion != "3.0.14" {
		t.Errorf("behebende Version fehlt: %q", f.FixedVersion)
	}
	if f.Severity != domain.SeverityCritical || f.Kind != domain.AdvisoryKindVulnerability {
		t.Errorf("Schwere/Art falsch: %+v", f)
	}
	if f.FirstSeenAt.IsZero() {
		t.Error("FirstSeenAt nicht gesetzt - ohne ihn ist kein Befund als „neu\" erkennbar")
	}
}

// TestAdvisoryPollErkenntSchadpaket: MAL-Kennungen sind Schadpakete und
// gelten immer als kritisch - auch wenn die Quelle gar keine Schwere führt.
func TestAdvisoryPollErkenntSchadpaket(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 10)
	id := seedPackages(t, env, "web01", domain.Package{Name: "leftpad", Version: "1.0.0"})

	env.AdvSource.QueryFunc = func(purls []string) (map[string][]advisories.Advisory, error) {
		out := map[string][]advisories.Advisory{}
		for _, p := range purls {
			out[p] = []advisories.Advisory{{ID: "MAL-2026-42"}}
		}
		return out, nil
	}
	// Bewusst OHNE Schwere-Angabe - genau der Fall, der ohne Sonderregel
	// unter jeder Schwelle durchrutschen würde.
	env.AdvSource.DetailsFunc = func([]string) (map[string]advisories.Detail, error) {
		return map[string]advisories.Detail{"MAL-2026-42": {ID: "MAL-2026-42", Title: "Backdoor"}}, nil
	}

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	found, _ := env.Advisories.ActiveForServer(id)
	if len(found) != 1 {
		t.Fatalf("erwartet 1 Befund, bekam %d", len(found))
	}
	if found[0].Kind != domain.AdvisoryKindMalware {
		t.Errorf("MAL-Kennung muss als Schadpaket gelten: %+v", found[0])
	}
	if got := found[0].EffectiveSeverity(); got != domain.SeverityCritical {
		t.Errorf("Schadpaket muss kritisch wirken, war %q", got)
	}
}

// TestAdvisoryPollNutztZwischenspeicher: Beim zweiten Durchgang innerhalb der
// TTL darf kein purl erneut nach außen gehen.
func TestAdvisoryPollNutztZwischenspeicher(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 30)
	seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll 1: %v", err)
	}
	first := len(env.AdvSource.QueriedPurls)
	if first == 0 {
		t.Fatal("der erste Durchgang muss abfragen")
	}
	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if len(env.AdvSource.QueriedPurls) != first {
		t.Errorf("zweiter Durchgang hat erneut abgefragt: %d → %d", first, len(env.AdvSource.QueriedPurls))
	}
}

// TestAdvisoryPollOhneZwischenspeicher: TTL 0 schaltet ihn ab - dann fragt
// jeder Durchgang wieder alles.
func TestAdvisoryPollOhneZwischenspeicher(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 0)
	seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll 1: %v", err)
	}
	first := len(env.AdvSource.QueriedPurls)
	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if len(env.AdvSource.QueriedPurls) != 2*first {
		t.Errorf("ohne Zwischenspeicher muss erneut abgefragt werden: %d → %d", first, len(env.AdvSource.QueriedPurls))
	}
}

// TestAdvisoryPollFragtGleichePaketeNurEinmal: Der eigentliche Spar-Hebel -
// eine Flotte mit gleichem Bestand ergibt eine einzige Abfrage je purl.
func TestAdvisoryPollFragtGleichePaketeNurEinmal(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 10)
	pkg := domain.Package{Name: "openssl", Version: "3.0.11"}
	seedPackages(t, env, "web01", pkg)
	seedPackages(t, env, "web02", pkg)
	seedPackages(t, env, "web03", pkg)

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(env.AdvSource.QueriedPurls) != 1 {
		t.Errorf("drei gleiche Server müssen einen purl ergeben, waren %d: %v",
			len(env.AdvSource.QueriedPurls), env.AdvSource.QueriedPurls)
	}
}

// TestAdvisoryPollBehebtUndOeffnetWieder: Verschwindet ein Befund, wird er
// als behoben markiert statt gelöscht; taucht er wieder auf, wird derselbe
// Eintrag wiedereröffnet - FirstSeenAt bleibt erhalten.
func TestAdvisoryPollBehebtUndOeffnetWieder(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 0) // ohne Zwischenspeicher, damit jeder Lauf zählt
	id := seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	betroffen := true
	env.AdvSource.QueryFunc = func(purls []string) (map[string][]advisories.Advisory, error) {
		out := map[string][]advisories.Advisory{}
		if betroffen {
			for _, p := range purls {
				out[p] = []advisories.Advisory{{ID: "CVE-2026-1"}}
			}
		}
		return out, nil
	}

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll 1: %v", err)
	}
	before, _ := env.Advisories.ActiveForServer(id)
	if len(before) != 1 {
		t.Fatalf("erwartet 1 offenen Befund, bekam %d", len(before))
	}
	firstSeen := before[0].FirstSeenAt

	betroffen = false
	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if open, _ := env.Advisories.ActiveForServer(id); len(open) != 0 {
		t.Errorf("Befund müsste behoben sein, sind noch %d offen", len(open))
	}
	// Der Eintrag bleibt als Verlauf stehen.
	page, err := env.Advisories.Global(repositories.ScopeAll(), verlaufsFilter())
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("Verlauf fehlt: %v / %d Zeilen", err, len(page.Items))
	}
	if page.Items[0].ResolvedAt == nil {
		t.Error("behobener Befund braucht ResolvedAt")
	}

	betroffen = true
	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll 3: %v", err)
	}
	again, _ := env.Advisories.ActiveForServer(id)
	if len(again) != 1 {
		t.Fatalf("Befund müsste wiedereröffnet sein, sind %d offen", len(again))
	}
	if !again[0].FirstSeenAt.Equal(firstSeen) {
		t.Error("Wiedereröffnung darf FirstSeenAt nicht überschreiben")
	}
	all, _ := env.Advisories.Global(repositories.ScopeAll(), verlaufsFilter())
	if len(all.Items) != 1 {
		t.Errorf("Wiedereröffnung darf keinen zweiten Eintrag anlegen, sind %d", len(all.Items))
	}
}

// TestAdvisoryPollFehlerErzeugtJob: Eine unerreichbare Quelle darf nicht
// stillschweigend verpuffen - sonst hielte man sich für gewarnt, ohne es zu
// sein. Der Erfolgsfall bleibt dagegen leise (96 Läufe pro Tag).
func TestAdvisoryPollFehlerErzeugtJob(t *testing.T) {
	env := newTestEnv(t)
	// Ohne Zwischenspeicher, sonst fragt der zweite Durchgang gar nicht erst
	// an (Treffer aus dem ersten) und koennte damit auch nicht scheitern.
	enableAdvisories(t, env, 0)
	seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	// Gezielt nach den Jobs der Fruehwarnung filtern: Das Aufnehmen des
	// Test-Servers legt selbst Jobs an.
	pollJobs := repositories.JobFilter{NameQuery: "Fruehwarnung", Limit: 10}

	env.Executor.RunAdvisoryPoll("scheduler")
	if _, total, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), pollJobs); total != 0 {
		t.Errorf("erfolgreicher Durchgang darf keinen Job erzeugen, fand %d", total)
	}

	env.AdvSource.QueryFunc = func([]string) (map[string][]advisories.Advisory, error) {
		return nil, errors.New("osv-abfrage fehlgeschlagen: http 503")
	}
	env.Executor.RunAdvisoryPoll("scheduler")

	jobs, total, err := env.Jobs.HistoryFiltered(repositories.ScopeAll(), pollJobs)
	if err != nil || total != 1 {
		t.Fatalf("erwartet genau 1 Fehler-Job, bekam %d (%v)", total, err)
	}
	if jobs[0].Status != "failed" {
		t.Errorf("Job sollte failed sein, war %q", jobs[0].Status)
	}
}

// TestAdvisoryCacheTTLBegrenzt: Ein zu hoher Wert wird auf das Maximum
// gekappt - der Zwischenspeicher darf die Frühwarnung nicht länger blind
// machen als vorgesehen.
func TestAdvisoryCacheTTLBegrenzt(t *testing.T) {
	env := newTestEnv(t)
	zuHoch := 240
	got, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AdvisoryCacheTTLMinutes: &zuHoch}, "admin")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.AdvisoryCacheTTLMinutes != domain.AdvisoryCacheTTLMax {
		t.Errorf("erwartet Deckelung auf %d, war %d", domain.AdvisoryCacheTTLMax, got.AdvisoryCacheTTLMinutes)
	}
}
