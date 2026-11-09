package router

import (
	"errors"
	"testing"
)

// Eine verletzte Eindeutigkeit der Datenbank ist ein Konflikt, kein interner Fehler.
func TestUniqueViolationErkanntMitSpalte(t *testing.T) {
	err := errors.New("constraint failed: UNIQUE constraint failed: server_groups.name (2067)")
	column, ok := uniqueViolation(err)
	if !ok || column != "name" {
		t.Fatalf("uniqueViolation = %q, %v", column, ok)
	}
	if _, ok := uniqueViolation(errors.New("irgendein anderer Fehler")); ok {
		t.Error("fremder Fehler wurde als Eindeutigkeit gedeutet")
	}
	if column, _ := uniqueViolation(errors.New("UNIQUE constraint failed: a.x, a.y")); column != "x" {
		t.Errorf("Spalte bei mehreren: %q", column)
	}
}
