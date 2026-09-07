package storage

import (
	"context"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// TestProbeWritableSchreibtWirklich: Die Selbstprüfung muss einen echten
// Schreibvorgang machen. Ein Ping beantwortet nur „ist die Verbindung offen?"
// - und genau diese Lücke hat im Betrieb dazu geführt, dass der Dienst
// minutenlang „operational" meldete, während keine einzige Zeile schreibbar
// war.
func TestProbeWritableSchreibtWirklich(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	// Erster Lauf legt die Zeile an.
	if err := ProbeWritable(context.Background(), db); err != nil {
		t.Fatalf("erste Prüfung: %v", err)
	}
	var probe domain.HealthProbe
	if err := db.First(&probe, domain.HealthProbeID).Error; err != nil {
		t.Fatalf("Prüfzeile fehlt: %v", err)
	}
	if probe.CheckedAt.IsZero() {
		t.Error("Zeitstempel wurde nicht gesetzt")
	}
	erster := probe.CheckedAt

	// Zweiter Lauf aktualisiert sie - und legt keine zweite an.
	time.Sleep(2 * time.Millisecond)
	if err := ProbeWritable(context.Background(), db); err != nil {
		t.Fatalf("zweite Prüfung: %v", err)
	}
	var n int64
	if err := db.Model(&domain.HealthProbe{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("die Prüfung darf genau EINE Zeile führen, waren %d", n)
	}
	if err := db.First(&probe, domain.HealthProbeID).Error; err != nil {
		t.Fatal(err)
	}
	if !probe.CheckedAt.After(erster) {
		t.Errorf("Zeitstempel nicht fortgeschrieben: %v → %v", erster, probe.CheckedAt)
	}
}

// TestProbeWritableAchtetAufDenKontext: Die Prüfung darf nicht länger dauern
// als ihr Zeitlimit - sonst wäre sie selbst die Störung, die sie melden soll.
func TestProbeWritableAchtetAufDenKontext(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ProbeWritable(ctx, db); err == nil {
		t.Error("abgebrochener Kontext muss einen Fehler liefern")
	}
}
