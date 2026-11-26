package services

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/remote/wire"
	"LCM/internal/storage/repositories"
)

// TestJobIdleLimitNurFuerAgentMitJobsAus: Auf einem Agent-Server mit dauerhaften
// Jobs (jobs-v2) beurteilt der Agent selbst, ob ein Kommando noch arbeitet; LCM
// bricht dort nie ab. SSH und ältere Agenten behalten den Job-Watchdog: Ihr
// hängendes Kommando hielte den Server sonst für immer fest.
func TestJobIdleLimitNurFuerAgentMitJobsAus(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.GlobalSettings{}, &domain.Server{}); err != nil {
		t.Fatal(err)
	}
	settings := repositories.NewSettingsRepository(db)
	if err := settings.Save(&domain.GlobalSettings{JobIdleTimeoutMinutes: 5, JobIdleTimeoutSlowMinutes: 30}); err != nil {
		t.Fatal(err)
	}
	ssh := domain.Server{Name: "ssh", Host: "10.0.0.1", Transport: domain.TransportSSH}
	durable := domain.Server{Name: "durable", Host: "10.0.0.2", Transport: domain.TransportAgent, AgentID: "a1",
		AgentVersion: "2.4.0", AgentCapabilities: "tty,ping," + wire.CapabilityJobs}
	legacy := domain.Server{Name: "legacy", Host: "10.0.0.3", Transport: domain.TransportAgent, AgentID: "a2",
		AgentVersion: "2.3.0", AgentCapabilities: "tty"}
	unreported := domain.Server{Name: "neu", Host: "10.0.0.4", Transport: domain.TransportAgent, AgentID: "a3"}
	for _, s := range []*domain.Server{&ssh, &durable, &legacy, &unreported} {
		if err := db.Create(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	limit := JobIdleLimit(settings, repositories.NewServerRepository(db))

	for _, tt := range []struct {
		name   string
		server *domain.Server
		want   time.Duration
	}{
		{"SSH-Server", &ssh, 5 * time.Minute},
		{"Agent mit Jobs", &durable, 0},
		{"älterer Agent", &legacy, 5 * time.Minute},
		{"Agent ohne Meldung", &unreported, 5 * time.Minute},
	} {
		if got := limit(&domain.Job{ServerID: &tt.server.ID}); got != tt.want {
			t.Errorf("%s: %v, erwartet %v", tt.name, got, tt.want)
		}
	}
}
