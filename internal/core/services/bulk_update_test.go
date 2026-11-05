package services_test

import (
	"errors"
	"testing"
	"time"

	"LCM/internal/core/events"
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

// Das Sammel-Update meldet jeden Schritt als bulk.progress - nur mit Zählern,
// denn die Meldung geht auch an Benutzer, die einen Teil der Server nicht sehen.
func TestBulkUpdateMeldetFortschritt(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")
	bus := events.NewBus()
	env.Servers.WithEvents(bus)
	sub := bus.Subscribe()
	defer sub.Close()

	if _, err := env.Servers.StartBulkUpdate(repositories.ScopeAll(), "admin"); err != nil {
		t.Fatal(err)
	}

	var last events.BulkProgressData
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e := <-sub.Events():
			if e.Type != events.TypeBulkProgress {
				continue // Job-Ereignisse des Updates selbst
			}
			last = e.Data.(events.BulkProgressData)
			if !last.Running {
				if last.Total != 1 || last.Completed != 1 {
					t.Errorf("Endstand: %+v", last)
				}
				return
			}
		case <-deadline:
			t.Fatalf("kein Endstand, zuletzt %+v", last)
		}
	}
}

// Das Sammel-Update-Status zeigt Namen und Zähler nur dem Admin und dem, dessen
// Sichtbereich der Lauf betrifft (Mandantentrennung).
func TestBulkUpdateStatusNurImEigenenSichtbereich(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")
	if _, err := env.Servers.StartBulkUpdate(repositories.ScopeAll(), "admin"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !env.Servers.BulkUpdateStatus().Running })
	st := env.Servers.BulkUpdateStatus()
	if st.Total != 1 {
		t.Fatalf("Admin-Lauf: %+v", st)
	}
	if got := st.VisibleTo(repositories.ScopeAll()); got.Total != 1 {
		t.Errorf("Admin sieht %+v", got)
	}
	if got := st.VisibleTo(repositories.ScopeManager(7)); got.Total != 0 || got.Actor != "" || got.Completed != 0 {
		t.Errorf("fremder Manager sieht %+v", got)
	}

	if _, err := env.Servers.StartBulkUpdate(repositories.ScopeManager(7), "mia"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !env.Servers.BulkUpdateStatus().Running })
	own := env.Servers.BulkUpdateStatus()
	if got := own.VisibleTo(repositories.ScopeManager(7)); got.Actor != "mia" {
		t.Errorf("der Startende sieht %+v", got)
	}
	if got := own.VisibleTo(repositories.ScopeManager(8)); got.Actor != "" {
		t.Errorf("anderer Manager sieht %+v", got)
	}
}
