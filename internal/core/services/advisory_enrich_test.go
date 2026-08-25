package services_test

import (
	"context"
	"errors"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/advisories"
	"LCM/internal/storage/repositories"
)

// pollWithFinding lässt einen Durchgang laufen, der genau einen Befund mit
// der gegebenen Kennung und den gegebenen Aliasen anlegt.
func pollWithFinding(t *testing.T, env *testEnv, advisoryID string, aliases ...string) uint {
	t.Helper()
	enableAdvisories(t, env, 0)
	id := seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	env.AdvSource.QueryFunc = func(purls []string) (map[string][]advisories.Advisory, error) {
		out := map[string][]advisories.Advisory{}
		for _, p := range purls {
			out[p] = []advisories.Advisory{{ID: advisoryID}}
		}
		return out, nil
	}
	env.AdvSource.DetailsFunc = func([]string) (map[string]advisories.Detail, error) {
		return map[string]advisories.Detail{
			advisoryID: {ID: advisoryID, Severity: domain.SeverityHigh, Aliases: aliases},
		}, nil
	}
	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	return id
}

func exploitedFlag(t *testing.T, env *testEnv, serverID uint) bool {
	t.Helper()
	found, err := env.Advisories.ActiveForServer(serverID)
	if err != nil || len(found) != 1 {
		t.Fatalf("erwartet 1 Befund, bekam %d (%v)", len(found), err)
	}
	return found[0].Exploited
}

// TestEnrichMarkiertUeberDieKennung ist der einfache Fall: Der Befund trägt
// selbst eine CVE-Kennung, die in der Ausnutzungsliste steht.
func TestEnrichMarkiertUeberDieKennung(t *testing.T) {
	env := newTestEnv(t)
	id := pollWithFinding(t, env, "CVE-2026-1")
	env.Advisories.WithExploitSource(&advisories.FakeExploits{
		IsAvailable: true, CVEs: map[string]bool{"CVE-2026-1": true},
	})

	if _, err := env.Advisories.EnrichExploited(context.Background()); err != nil {
		t.Fatalf("EnrichExploited: %v", err)
	}
	if !exploitedFlag(t, env, id) {
		t.Error("Befund müsste als ausgenutzt markiert sein")
	}
}

// TestEnrichMarkiertUeberDenAlias ist der eigentlich wichtige Fall: Bei
// Betriebssystempaketen trägt der Befund die Kennung der Distribution
// (DSA-…), die Ausnutzungsliste dagegen CVE-Kennungen. Ohne Alias-Abgleich
// liefe die Anreicherung für genau diese Pakete ins Leere.
func TestEnrichMarkiertUeberDenAlias(t *testing.T) {
	env := newTestEnv(t)
	id := pollWithFinding(t, env, "DSA-5973-1", "CVE-2026-9999")
	env.Advisories.WithExploitSource(&advisories.FakeExploits{
		IsAvailable: true, CVEs: map[string]bool{"CVE-2026-9999": true},
	})

	if _, err := env.Advisories.EnrichExploited(context.Background()); err != nil {
		t.Fatalf("EnrichExploited: %v", err)
	}
	if !exploitedFlag(t, env, id) {
		t.Error("Befund müsste über seinen Alias markiert sein")
	}
}

// TestEnrichNimmtMarkierungZurueck: Steht eine Lücke nicht mehr in der Liste,
// muss die Markierung verschwinden - sonst bliebe eine Dringlichkeit stehen,
// die niemand mehr belegen kann.
func TestEnrichNimmtMarkierungZurueck(t *testing.T) {
	env := newTestEnv(t)
	id := pollWithFinding(t, env, "CVE-2026-1")
	src := &advisories.FakeExploits{IsAvailable: true, CVEs: map[string]bool{"CVE-2026-1": true}}
	env.Advisories.WithExploitSource(src)

	if _, err := env.Advisories.EnrichExploited(context.Background()); err != nil {
		t.Fatalf("EnrichExploited: %v", err)
	}
	if !exploitedFlag(t, env, id) {
		t.Fatal("Vorbedingung: Befund sollte markiert sein")
	}

	src.CVEs = map[string]bool{"CVE-2000-1": true} // andere Lücke, unsere nicht mehr dabei
	if _, err := env.Advisories.EnrichExploited(context.Background()); err != nil {
		t.Fatalf("EnrichExploited: %v", err)
	}
	if exploitedFlag(t, env, id) {
		t.Error("Markierung müsste zurückgenommen sein")
	}
}

// TestEnrichFehlerLaesstMarkierungenStehen: Fällt die Quelle aus, darf der
// vorhandene Stand NICHT angetastet werden. Ein Ausfall, der die Lage
// harmloser aussehen lässt, wäre schlimmer als gar keine Anreicherung.
func TestEnrichFehlerLaesstMarkierungenStehen(t *testing.T) {
	env := newTestEnv(t)
	id := pollWithFinding(t, env, "CVE-2026-1")
	src := &advisories.FakeExploits{IsAvailable: true, CVEs: map[string]bool{"CVE-2026-1": true}}
	env.Advisories.WithExploitSource(src)
	if _, err := env.Advisories.EnrichExploited(context.Background()); err != nil {
		t.Fatalf("EnrichExploited: %v", err)
	}

	src.Err = errors.New("euvd nicht erreichbar")
	if _, err := env.Advisories.EnrichExploited(context.Background()); err == nil {
		t.Fatal("Ausfall der Quelle muss einen Fehler ergeben")
	}
	if !exploitedFlag(t, env, id) {
		t.Error("ein Quellen-Ausfall darf die Markierung nicht zurücknehmen")
	}
}

// TestEnrichFehlerErzeugtJob: Der tägliche Lauf ist leise; nur ein
// Fehlschlag landet als Job im Protokoll.
func TestEnrichFehlerErzeugtJob(t *testing.T) {
	env := newTestEnv(t)
	pollWithFinding(t, env, "CVE-2026-1")
	src := &advisories.FakeExploits{IsAvailable: true, CVEs: map[string]bool{"CVE-2026-1": true}}
	env.Advisories.WithExploitSource(src)

	filter := repositories.JobFilter{NameQuery: "Ausnutzungs-Signal", Limit: 10}
	env.Executor.RunAdvisoryEnrich("scheduler")
	if _, total, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), filter); total != 0 {
		t.Errorf("erfolgreicher Lauf darf keinen Job erzeugen, fand %d", total)
	}

	src.Err = errors.New("http 403")
	env.Executor.RunAdvisoryEnrich("scheduler")
	jobs, total, err := env.Jobs.HistoryFiltered(repositories.ScopeAll(), filter)
	if err != nil || total != 1 {
		t.Fatalf("erwartet genau 1 Fehler-Job, bekam %d (%v)", total, err)
	}
	if jobs[0].Status != "failed" {
		t.Errorf("Job sollte failed sein, war %q", jobs[0].Status)
	}
}
