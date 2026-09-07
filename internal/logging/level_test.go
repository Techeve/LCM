package logging

import (
	"testing"
	"time"
)

// TestDebugAufZeitKehrtZurueck: Debug schreibt jedes SSH-Kommando samt Ausgabe
// mit. Bleibt es an, wächst die Logdatei auf einer kleinen Maschine schneller,
// als die Rotation sie wegräumt - und niemand erinnert sich nach drei Wochen
// daran. Deshalb kehrt es von selbst zurück.
func TestDebugAufZeitKehrtZurueck(t *testing.T) {
	Setup("info", false, "")
	if got := Level(); got != "info" {
		t.Fatalf("Ausgangslevel %q, erwartet info", got)
	}

	EnableDebugFor(60 * time.Millisecond)
	if got := Level(); got != "debug" {
		t.Errorf("nach dem Einschalten %q, erwartet debug", got)
	}
	if DebugUntil().IsZero() {
		t.Error("das Ende der Frist muss abfragbar sein")
	}

	time.Sleep(120 * time.Millisecond)
	if got := Level(); got != "info" {
		t.Errorf("nach Ablauf %q, erwartet die Rückkehr auf info", got)
	}
	if !DebugUntil().IsZero() {
		t.Error("nach Ablauf darf keine Frist mehr gemeldet werden")
	}
}

// TestDebugLaesstSichSofortBeenden: Wer es angeschaltet hat, muss es auch
// wieder loswerden, ohne die Frist abzuwarten.
func TestDebugLaesstSichSofortBeenden(t *testing.T) {
	Setup("warn", false, "")

	EnableDebugFor(time.Hour)
	if got := Level(); got != "debug" {
		t.Fatalf("nach dem Einschalten %q, erwartet debug", got)
	}
	DisableDebug()
	if got := Level(); got != "warn" {
		t.Errorf("nach dem Ausschalten %q - es muss auf das KONFIGURIERTE Level zurück, nicht auf info", got)
	}
	if !DebugUntil().IsZero() {
		t.Error("nach dem Ausschalten darf keine Frist mehr laufen")
	}
}

// TestZweitesEinschaltenVerlaengert: Ein zweiter Aufruf stellt keinen zweiten
// Wecker, sondern schiebt den vorhandenen - sonst würde der erste das Debug
// mitten in der Fehlersuche abdrehen.
func TestZweitesEinschaltenVerlaengert(t *testing.T) {
	Setup("info", false, "")

	EnableDebugFor(40 * time.Millisecond)
	erstes := DebugUntil()
	EnableDebugFor(2 * time.Second)
	if !DebugUntil().After(erstes) {
		t.Error("das zweite Einschalten muss die Frist verlängern")
	}

	time.Sleep(120 * time.Millisecond)
	if got := Level(); got != "debug" {
		t.Errorf("die alte Frist hat abgedreht: %q", got)
	}
	DisableDebug()
}

// TestFlagHebtDasLevel: Das CLI-Flag -debug sticht die Config - und bleibt
// auch der Stand, auf den ein befristetes Debug zurückfällt.
func TestFlagHebtDasLevel(t *testing.T) {
	Setup("info", true, "")
	if got := Level(); got != "debug" {
		t.Fatalf("mit -debug %q, erwartet debug", got)
	}
	EnableDebugFor(30 * time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	if got := Level(); got != "debug" {
		t.Errorf("nach Ablauf %q - mit -debug muss debug der Ruhezustand bleiben", got)
	}
}
