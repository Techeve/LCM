package services_test

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/events"
	"LCM/internal/core/services"
)

// Ein ausgelöster Alarm geht auf den Bus - mit Kennungen, aber ohne die
// Beschreibung, die Rohausgaben enthalten kann.
func TestAlarmWirdGemeldetOhneBeschreibung(t *testing.T) {
	env := newAlertEnv(t)
	bus := events.NewBus()
	env.alerts.WithEvents(bus)
	sub := bus.Subscribe()
	defer sub.Close()

	stale := time.Now().Add(-48 * time.Hour)
	server := env.createServer(t, "old-01", func(s *domain.Server) { s.LastSeenAt = &stale })
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Heartbeat", Type: domain.AlertTypeHeartbeat, Enabled: true,
		HeartbeatHours: 24, Severity: domain.AlertSeverityCritical,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}

	select {
	case e := <-sub.Events():
		d, ok := e.Data.(events.AlertFiredData)
		if e.Type != events.TypeAlertFired || !ok {
			t.Fatalf("erwartet alert.fired, bekam %+v", e)
		}
		if e.ServerID != server.ID || d.ServerID == nil || *d.ServerID != server.ID {
			t.Errorf("Server: %+v", e)
		}
		if d.Type != domain.AlertTypeHeartbeat || d.Severity != domain.AlertSeverityCritical || d.ID == "" {
			t.Errorf("Nutzlast: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kein alert.fired angekommen")
	}
}
