package services

import (
	"errors"
	"strings"
	"testing"
)

// TestSnapNamenWerdenGeprueft: Die Namen landen in einer Kommandozeile, die
// als root läuft. Was hier durchkommt, ist shell-sicher.
func TestSnapNamenWerdenGeprueft(t *testing.T) {
	gut, err := parseSnapNames("firefox, chromium  lxd")
	if err != nil {
		t.Fatalf("gültige Namen abgelehnt: %v", err)
	}
	if strings.Join(gut, " ") != "firefox chromium lxd" {
		t.Errorf("zerlegt zu %v", gut)
	}

	// Leerzeichen und Komma trennen die Liste - „fire fox" sind zwei Namen,
	// kein Fehler. Abgewiesen wird alles, was in einer Kommandozeile etwas
	// anderes bewirken würde als ein Name.
	for _, schlecht := range []string{"firefox; rm -rf /", "$(id)", "Firefox!", "../etc"} {
		if _, err := parseSnapNames(schlecht); !errors.Is(err, ErrInvalidSnap) && !errors.Is(err, ErrNoSnaps) {
			t.Errorf("%q wurde nicht abgewiesen (%v)", schlecht, err)
		}
	}
	if _, err := parseSnapNames("   "); !errors.Is(err, ErrNoSnaps) {
		t.Errorf("leere Liste sollte ErrNoSnaps liefern, war %v", err)
	}
}

// TestSnapdBleibtGeschuetzt: `snap remove snapd` nimmt die Snap-Verwaltung
// mit, `snap remove core22` allen darauf aufbauenden Snaps die Laufzeit.
func TestSnapdBleibtGeschuetzt(t *testing.T) {
	for _, name := range []string{"snapd", "core", "core18", "core22", "core24", "bare", "lxd", "snap-store"} {
		if !isProtectedSnap(name) {
			t.Errorf("%s müsste geschützt sein", name)
		}
	}
	for _, name := range []string{"firefox", "chromium", "coreutils-snap", "microk8s"} {
		if isProtectedSnap(name) {
			t.Errorf("%s ist fälschlich geschützt", name)
		}
	}
}

// TestSnapEntfernenBehaeltDieDaten: Ohne --purge legt snapd vor dem Entfernen
// eine Momentaufnahme an. Genau die will man, wenn sich das Entfernen als
// Irrtum herausstellt.
func TestSnapEntfernenBehaeltDieDaten(t *testing.T) {
	script := snapRemoveScript([]string{"firefox"})
	if strings.Contains(script, "--purge") {
		t.Errorf("--purge vernichtet die Momentaufnahme: %q", script)
	}
	if script != "snap remove firefox" {
		t.Errorf("unerwartetes Kommando: %q", script)
	}
	if got := snapRefreshScript([]string{"firefox", "chromium"}); got != "snap refresh firefox chromium" {
		t.Errorf("unerwartetes Kommando: %q", got)
	}
	if got := snapRefreshAllScript(); got != "snap refresh" {
		t.Errorf("unerwartetes Kommando: %q", got)
	}
}
