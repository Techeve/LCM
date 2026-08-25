package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestSpiegelKenntDieDistributionen: Der Spiegel leitet aus dem Paketbestand
// ab, welche Distributionen er holen muss. Ohne diesen Schritt laedt er
// nichts - und der Knopf tut scheinbar nichts.
func TestSpiegelKenntDieDistributionen(t *testing.T) {
	env := newTestEnv(t)
	id := seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	// Distribution setzen - ohne sie ist keine Zuordnung moeglich.
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.UpdateFields(id, map[string]any{
		"os_id": "debian", "os_version_id": "12", "package_manager": "apt",
	}); err != nil {
		t.Fatal(err)
	}

	got := env.Advisories.MirrorableEcosystems()
	if len(got) != 1 || got[0] != "Debian:12" {
		t.Fatalf("erwartet [Debian:12], bekam %v", got)
	}
}

// TestSpiegelOhneBestandBenenntDenGrund: Gibt es keinen Server mit
// Paketbestand, ist „nichts zu spiegeln" die richtige Antwort - sie muss
// aber ALS Grund ankommen und nicht als stiller Erfolg.
func TestSpiegelOhneBestandBenenntDenGrund(t *testing.T) {
	env := newTestEnv(t)
	setLocalCopy(t, env, true)

	if got := env.Advisories.MirrorableEcosystems(); len(got) != 0 {
		t.Errorf("ohne Bestand darf es nichts zu spiegeln geben, war %v", got)
	}

	env.Executor.RunAdvisoryMirror("test")
	jobs, total, err := env.Jobs.HistoryFiltered(repositories.ScopeAll(),
		repositories.JobFilter{NameQuery: "lokale Kopie", Limit: 5})
	if err != nil || total == 0 {
		t.Fatalf("Spiegellauf-Job fehlt: %v", err)
	}
	if !strings.Contains(jobs[0].Output, "nichts zu spiegeln") {
		t.Errorf("der Grund fehlt in der Ausgabe: %q", jobs[0].Output)
	}
}

// TestSpiegelLaufLiefertJob: Der manuelle Auslöser gibt den Job zurück -
// nur so kann die Oberfläche auf sein Ergebnis warten. Ohne ihn blieb es
// bei „gestartet", und ob etwas passierte, erfuhr niemand.
func TestSpiegelLaufLiefertJob(t *testing.T) {
	env := newTestEnv(t)
	setLocalCopy(t, env, true)

	job, err := env.Scheduler.TriggerAdvisoryMirror("admin")
	if err != nil {
		t.Fatalf("TriggerAdvisoryMirror: %v", err)
	}
	if job == nil || job.ID == "" {
		t.Fatal("ohne Job-Kennung kann die Oberflaeche nicht auf das Ergebnis warten")
	}
}
