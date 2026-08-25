package services_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestStorageReportIncludesVolumesRootFirst: der Speicher-Bericht enthält die
// erfassten Volumes, Root „/" zuerst; Verlauf/Prognose bleiben root-basiert.
func TestStorageReportIncludesVolumesRootFirst(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	if err := repo.ReplaceDiskVolumes(id, []domain.DiskVolume{
		{Mountpoint: "/data", Device: "/dev/sdb1", Fstype: "xfs", TotalMB: 512000, UsedMB: 384000},
		{Mountpoint: "/", Device: "/dev/sda1", Fstype: "ext4", TotalMB: 40960, UsedMB: 12800},
		{Mountpoint: "/boot", Device: "/dev/sda2", Fstype: "ext4", TotalMB: 976, UsedMB: 180},
	}); err != nil {
		t.Fatal(err)
	}

	report, err := env.Servers.StorageHistory(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Volumes) != 3 {
		t.Fatalf("erwartete 3 Volumes im Bericht, bekam %d", len(report.Volumes))
	}
	if !report.Volumes[0].IsRoot() {
		t.Errorf("Root-Volume sollte an erster Stelle stehen, bekam %q", report.Volumes[0].Mountpoint)
	}
}
