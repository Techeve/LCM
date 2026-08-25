package services

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestRemoveKernelsScriptGuardsRunning: Die Auswahl stammt aus dem zuletzt
// erfassten Inventar. Zwischen Erfassung und Ausfuehrung kann ein Neustart
// den laufenden Kernel gewechselt haben - deshalb sitzt die Sicherung auch
// im Skript, nicht nur davor.
func TestRemoveKernelsScriptGuardsRunning(t *testing.T) {
	script := removeKernelsScript([]string{"6.1.0-10-amd64", "6.1.0-11-amd64"})

	if !strings.Contains(script, "RUNNING=$(uname -r)") {
		t.Errorf("das Skript fragt den laufenden Kernel nicht ab:\n%s", script)
	}
	if !strings.Contains(script, `grep -v -F "$RUNNING"`) {
		t.Errorf("der laufende Kernel wird nicht aus der Entfernungsliste gefiltert:\n%s", script)
	}
	if !strings.Contains(script, "purge") {
		t.Errorf("es wird nichts entfernt:\n%s", script)
	}
	for _, rel := range []string{"6.1.0-10-amd64", "6.1.0-11-amd64"} {
		if !strings.Contains(script, rel) {
			t.Errorf("Fassung %q fehlt im Skript:\n%s", rel, script)
		}
	}
	// Begleitpakete unter dem verkuerzten Namen (Ubuntu: linux-headers-6.8.0-40)
	// muessen mitgehen - sonst bleibt der groesste Teil liegen.
	if !strings.Contains(script, "BASE=${REL%-*}") {
		t.Errorf("die verkuerzte Fassung fuer Header/Tools fehlt:\n%s", script)
	}
}

// TestCleanupReleasesRejectsUnsafe: Die Fassungen gehen in ein Shell-Skript.
// Was nicht wie eine Kernel-Fassung aussieht, wird gar nicht erst
// eingesetzt - und bleibt nur das uebrig, ist der Lauf hinfaellig.
func TestCleanupReleasesRejectsUnsafe(t *testing.T) {
	info := domain.KernelInfo{
		Managed: true,
		Removable: []domain.KernelPackage{
			{Name: "linux-image-boese", Release: "6.1.0-10-amd64; rm -rf /"},
			{Name: "linux-image-leer", Release: ""},
		},
	}
	if _, err := cleanupReleases(info); !errors.Is(err, ErrNoOldKernels) {
		t.Errorf("erwartet ErrNoOldKernels, bekam %v", err)
	}

	info.Removable = append(info.Removable,
		domain.KernelPackage{Name: "linux-image-6.1.0-11-amd64", Release: "6.1.0-11-amd64"})
	got, err := cleanupReleases(info)
	if err != nil {
		t.Fatalf("gueltige Fassung sollte durchkommen: %v", err)
	}
	if len(got) != 1 || got[0] != "6.1.0-11-amd64" {
		t.Errorf("uebernommen wurde %v, erwartet nur die gueltige Fassung", got)
	}
}

// TestCleanupReleasesNeedsManagedKernel: Im Container kommt der Kernel vom
// Host - dort gibt es nichts aufzuraeumen.
func TestCleanupReleasesNeedsManagedKernel(t *testing.T) {
	info := domain.KernelInfo{Managed: false, Removable: []domain.KernelPackage{
		{Name: "linux-image-6.1.0-10-amd64", Release: "6.1.0-10-amd64"},
	}}
	if _, err := cleanupReleases(info); !errors.Is(err, ErrKernelCleanupUnsupported) {
		t.Errorf("erwartet ErrKernelCleanupUnsupported, bekam %v", err)
	}
}
