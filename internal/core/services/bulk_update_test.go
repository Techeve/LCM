package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestBulkUpdateAllServers: das Sammel-Update aktualisiert alle erreichbaren
// Server der Reihe nach und meldet den Fortschritt bis zum Abschluss.
func TestBulkUpdateAllServers(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")
	joinTestServer(t, env, "web02")

	st, err := env.Servers.StartBulkUpdate(repositories.ScopeAll(), "admin")
	if err != nil {
		t.Fatalf("StartBulkUpdate: %v", err)
	}
	if !st.Running || st.Total != 2 {
		t.Fatalf("Start-Status unerwartet: %+v", st)
	}

	// Ein zweiter Start während des Laufs muss abgelehnt werden.
	if _, err := env.Servers.StartBulkUpdate(repositories.ScopeAll(), "admin"); !errors.Is(err, services.ErrBulkUpdateRunning) {
		t.Errorf("zweiter Start sollte ErrBulkUpdateRunning liefern, bekam %v", err)
	}

	waitFor(t, func() bool { return !env.Servers.BulkUpdateStatus().Running })

	fin := env.Servers.BulkUpdateStatus()
	if fin.Completed != 2 || fin.Failed != 0 {
		t.Fatalf("Ergebnis unerwartet: %+v", fin)
	}
	if fin.FinishedAt == nil {
		t.Error("FinishedAt sollte gesetzt sein")
	}
}
