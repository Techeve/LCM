package services_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestSnapUpdateLiestSnapsNeuEin: Nach einem Snap-Update muss der Reiter
// „Snaps" den neuen Stand zeigen. Bisher las LCM nach dem Lauf nur die
// apt-Pakete neu ein - das Update stand bis zum nächsten vollen Scan weiter
// als offen da, und es sah aus, als hätte es nicht geklappt.
func TestSnapUpdateLiestSnapsNeuEin(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "ubu01")
	setHasSnap(t, env, id)
	old := []domain.SnapPackage{{ServerID: id, Name: "yq", Version: "v4.49.2", Revision: "2759", CandidateVersion: "v4.54.3"}}
	if err := env.DB().Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	env.Dialer.Responses["snap list"] = sshx.FakeResponse{Output: "Name Version Rev Tracking Publisher Notes\nyq v4.54.3 3003 latest/stable mikefarah -\n"}
	env.Dialer.Responses["snap refresh --list"] = sshx.FakeResponse{Output: "All snaps up to date.\n"}

	job, err := env.Servers.RefreshSnaps(repositories.ScopeAll(), id, []string{"yq"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("Job %s: %s", done.Status, done.Output)
	}
	var snaps []domain.SnapPackage
	if err := env.DB().Where("server_id = ?", id).Find(&snaps).Error; err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].Version != "v4.54.3" || snaps[0].Outdated() {
		t.Errorf("Snap-Bestand nach dem Update nicht neu eingelesen: %+v", snaps)
	}
}
