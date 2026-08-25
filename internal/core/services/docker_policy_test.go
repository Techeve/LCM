package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// disableDockerUpdates legt den Schalter „keine Docker-Updates" für einen
// Server um - über denselben Weg wie die Einstellungsseite.
func disableDockerUpdates(t *testing.T, env *testEnv, id uint) {
	t.Helper()
	on := true
	if _, err := env.Servers.UpdateSettings(repositories.ScopeAll(), id,
		services.ServerSettingsInput{DockerUpdatesDisabled: &on}, "admin"); err != nil {
		t.Fatalf("einstellung setzen: %v", err)
	}
}

// TestDockerUpdatesDisabledBlocksManualActions: Ist der Schalter gesetzt,
// lehnt jede einspielende Aktion ab - egal ob Compose-Projekt, einzelnes
// Image oder alle Images auf einmal.
func TestDockerUpdatesDisabledBlocksManualActions(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker-locked")
	disableDockerUpdates(t, env, id)

	if _, err := env.Servers.UpdateComposeProject(repositories.ScopeAll(), id, "webshop", "", "admin"); !errors.Is(err, services.ErrDockerUpdatesDisabled) {
		t.Errorf("compose-update sollte abgelehnt werden, bekam: %v", err)
	}
	if _, err := env.Servers.PullDockerImage(repositories.ScopeAll(), id, "nginx:1.25", "admin"); !errors.Is(err, services.ErrDockerUpdatesDisabled) {
		t.Errorf("image-pull sollte abgelehnt werden, bekam: %v", err)
	}
	if _, err := env.Servers.PullAllDockerImages(repositories.ScopeAll(), id, "admin"); !errors.Is(err, services.ErrDockerUpdatesDisabled) {
		t.Errorf("pull-all sollte abgelehnt werden, bekam: %v", err)
	}
}

// TestDockerUpdatesDisabledKeepsMonitoring: Abgeschaltet ist das Einspielen,
// nicht das Hinsehen. Der Inventar-Rescan muss weiter laufen - sonst wüsste
// niemand mehr, was auf dem Server liegt und was es an Updates gäbe.
func TestDockerUpdatesDisabledKeepsMonitoring(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker-watched")
	disableDockerUpdates(t, env, id)

	if _, err := env.Servers.RefreshDocker(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("inventar-rescan muss weiterhin möglich sein: %v", err)
	}
	report, err := env.Servers.Docker(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatalf("docker-bericht: %v", err)
	}
	if len(report.Images) == 0 {
		t.Error("der bericht sollte weiterhin images ausweisen")
	}
}

// TestDockerUpdatesEnabledByDefault: Ohne den Schalter ändert sich nichts -
// die Sperre darf nicht versehentlich zum Normalzustand werden.
func TestDockerUpdatesEnabledByDefault(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker-open")

	if _, err := env.Servers.PullAllDockerImages(repositories.ScopeAll(), id, "admin"); errors.Is(err, services.ErrDockerUpdatesDisabled) {
		t.Error("ohne den schalter darf nichts gesperrt sein")
	}
}
