package services

import (
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// newReachDB oeffnet eine schlanke In-Memory-Datenbank NUR mit der
// Server-Tabelle. Bewusst ohne das storage-Paket: das importiert seinerseits
// services (Seed) und wuerde einen Import-Zyklus erzeugen.
func newReachDB(t *testing.T) (*repositories.ServerRepository, uint) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("db oeffnen: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&domain.Server{}); err != nil {
		t.Fatalf("migrieren: %v", err)
	}
	// Wegwerf-Tabelle je Test - die geteilte In-Memory-DB lebt sonst ueber
	// Testgrenzen hinweg weiter.
	db.Exec("DELETE FROM servers")
	repo := repositories.NewServerRepository(db)
	srv := &domain.Server{
		Name: "test-01", Host: "10.0.0.1", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
		HostKeyFingerprint: "SHA256:TEST", PrivateKeyEnc: "x", Reachable: true,
	}
	if err := repo.Create(srv); err != nil {
		t.Fatalf("server anlegen: %v", err)
	}
	return repo, srv.ID
}

func loadServer(t *testing.T, repo *repositories.ServerRepository, id uint) *domain.Server {
	t.Helper()
	s, err := repo.FindByID(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatalf("laden: %v", err)
	}
	return s
}

// TestFehlgeschlageneKontakteZaehlenHoch: Der Zaehler muss sich ueber MEHRERE
// Fehlschlaege aufsummieren - sonst gaebe es keine Schwelle, sondern nur ein
// Ja/Nein, und genau diese Unterscheidung ist der Zweck. Der Test geht
// bewusst ueber die Datenbank: Der Zaehler wird per SQL-Ausdruck erhoeht, ein
// Tippfehler darin wuerde stillschweigend gar nichts tun.
func TestFehlgeschlageneKontakteZaehlenHoch(t *testing.T) {
	repo, id := newReachDB(t)

	// Erster Fehlschlag: nicht erreichbar, aber noch NICHT offline.
	if err := repo.UpdateFields(id, unreachableFields(&domain.Server{Name: "test"}, errors.New("timeout"))); err != nil {
		t.Fatal(err)
	}
	s := loadServer(t, repo, id)
	if s.Reachable || s.FailedChecks != 1 {
		t.Fatalf("nach 1. Fehlschlag: reachable=%v failed=%d", s.Reachable, s.FailedChecks)
	}
	if s.IsOffline() {
		t.Error("ein einzelner Fehlschlag darf noch nicht als offline gelten")
	}
	if s.LastError == "" {
		t.Error("die Ursache sollte festgehalten werden")
	}

	// Zweiter Fehlschlag in Folge: jetzt offline.
	if err := repo.UpdateFields(id, unreachableFields(&domain.Server{Name: "test"}, errors.New("timeout"))); err != nil {
		t.Fatal(err)
	}
	s = loadServer(t, repo, id)
	if s.FailedChecks != 2 {
		t.Fatalf("nach 2. Fehlschlag: failed=%d, erwartet 2", s.FailedChecks)
	}
	if !s.IsOffline() {
		t.Error("nach zwei Fehlschlaegen in Folge sollte der Server offline sein")
	}
}

// TestErfolgreicherKontaktSetztZaehlerZurueck: „In Folge" heisst in Folge -
// ein erfolgreicher Kontakt macht den Zaehler zunichte. Ohne das wuerde ein
// Server, der irgendwann einmal weg war, dauerhaft als offline gelten.
func TestErfolgreicherKontaktSetztZaehlerZurueck(t *testing.T) {
	repo, id := newReachDB(t)
	for i := 0; i < 5; i++ {
		if err := repo.UpdateFields(id, unreachableFields(&domain.Server{Name: "test"}, errors.New("weg"))); err != nil {
			t.Fatal(err)
		}
	}
	if s := loadServer(t, repo, id); !s.IsOffline() {
		t.Fatalf("Vorbedingung: Server sollte offline sein (failed=%d)", s.FailedChecks)
	}

	if err := repo.UpdateFields(id, reachableFields(time.Now())); err != nil {
		t.Fatal(err)
	}
	s := loadServer(t, repo, id)
	if s.FailedChecks != 0 {
		t.Errorf("nach erfolgreichem Kontakt: failed=%d, erwartet 0", s.FailedChecks)
	}
	if !s.Reachable || s.IsOffline() {
		t.Error("wer antwortet, ist nicht offline")
	}
	if s.LastError != "" {
		t.Errorf("die alte Fehlerursache sollte geloescht sein: %q", s.LastError)
	}
}

// TestNeustartZaehltNichtAlsFehlschlag: Der bewusst gesetzte
// Nicht-erreichbar-Zustand direkt nach einem ausgeloesten Neustart ist ein
// ERWARTETER Zustand, kein fehlgeschlagener Erreichbarkeits-Check. Wuerde er
// mitzaehlen, waere jeder planmaessige Neustart ein halber Offline-Befund.
func TestNeustartZaehltNichtAlsFehlschlag(t *testing.T) {
	repo, id := newReachDB(t)
	// Exakt das Feld-Set aus reboot.go.
	if err := repo.UpdateFields(id, map[string]any{
		"reachable": false, "last_error": "Neustart ausgelöst - sollte in Kürze wieder online sein.",
	}); err != nil {
		t.Fatal(err)
	}
	s := loadServer(t, repo, id)
	if s.FailedChecks != 0 {
		t.Errorf("ein ausgeloester Neustart darf den Zaehler nicht fuellen: failed=%d", s.FailedChecks)
	}
	if s.IsOffline() {
		t.Error("direkt nach einem Neustart ist der Server nicht 'offline'")
	}
}
