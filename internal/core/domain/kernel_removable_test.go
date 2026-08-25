package domain_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// kernelInv baut ein Inventar aus Debian-Kernel-Fassungen.
func kernelInv(releases ...string) []domain.KernelPackage {
	pkgs := make([]domain.KernelPackage, 0, len(releases))
	for _, r := range releases {
		pkgs = append(pkgs, domain.KernelPackage{Name: "linux-image-" + r, Release: r})
	}
	return pkgs
}

func releases(pkgs []domain.KernelPackage) string {
	var out []string
	for _, p := range pkgs {
		out = append(out, p.Release)
	}
	return strings.Join(out, ", ")
}

// TestRemovableKeepsRunningAndFallback ist die Zusage der Aufräum-Aktion: Der
// laufende Kernel und die nächstältere Rückfallebene bleiben immer stehen -
// erst darunter beginnt der Ballast.
func TestRemovableKeepsRunningAndFallback(t *testing.T) {
	info := domain.BuildKernelInfo("6.1.0-13-amd64", "kvm",
		kernelInv("6.1.0-10-amd64", "6.1.0-13-amd64", "6.1.0-11-amd64", "6.1.0-12-amd64"))

	got := releases(info.Removable)
	if got != "6.1.0-11-amd64, 6.1.0-10-amd64" {
		t.Errorf("entfernbar = %q, erwartet die beiden ältesten", got)
	}
	for _, p := range info.Removable {
		if p.Running {
			t.Fatal("der laufende Kernel darf nie zum Entfernen vorgeschlagen werden")
		}
	}
}

// TestRemovableKeepsNewerThanRunning: Ein installierter, aber noch nicht
// gebooteter Kernel ist genau der, den der nächste Neustart aktiviert - er
// zählt nicht als Ballast, und er verschiebt die Rückfallebene nicht.
func TestRemovableKeepsNewerThanRunning(t *testing.T) {
	info := domain.BuildKernelInfo("6.1.0-12-amd64", "kvm",
		kernelInv("6.1.0-13-amd64", "6.1.0-12-amd64", "6.1.0-11-amd64", "6.1.0-10-amd64"))

	if !info.RebootPending {
		t.Error("ein neuerer installierter Kernel sollte einen Neustart ankündigen")
	}
	if got := releases(info.Removable); got != "6.1.0-10-amd64" {
		t.Errorf("entfernbar = %q, erwartet nur den ältesten (11 bleibt Rückfallebene)", got)
	}
}

// TestRemovableNeedsARunningKernel: Steht der laufende Kernel nicht im
// Inventar (Eigenbau, Fremdquelle), fehlt der Bezugspunkt für „älter als der
// laufende". Dann wird nichts vorgeschlagen - geraten wird hier nicht.
func TestRemovableNeedsARunningKernel(t *testing.T) {
	info := domain.BuildKernelInfo("6.9.9-eigenbau", "kvm",
		kernelInv("6.1.0-13-amd64", "6.1.0-12-amd64", "6.1.0-11-amd64"))

	if len(info.Removable) != 0 {
		t.Errorf("ohne erkannten laufenden Kernel darf nichts entfernbar sein, ist %q", releases(info.Removable))
	}
}

// TestRemovableEmptyWithTwoKernels: Zwei Kernel sind laufender plus
// Rückfallebene - da ist nichts übrig.
func TestRemovableEmptyWithTwoKernels(t *testing.T) {
	info := domain.BuildKernelInfo("6.1.0-13-amd64", "kvm",
		kernelInv("6.1.0-13-amd64", "6.1.0-12-amd64"))

	if len(info.Removable) != 0 {
		t.Errorf("bei zwei Kerneln darf nichts entfernbar sein, ist %q", releases(info.Removable))
	}
}

// TestRemovableEmptyInContainer: Im Container kommt der Kernel vom Host -
// eine Aufräum-Schaltfläche wäre dort eine Behauptung ohne Grundlage.
func TestRemovableEmptyInContainer(t *testing.T) {
	info := domain.BuildKernelInfo("6.1.0-13-amd64", "lxc",
		kernelInv("6.1.0-13-amd64", "6.1.0-12-amd64", "6.1.0-11-amd64"))

	if len(info.Removable) != 0 {
		t.Errorf("im Container darf nichts entfernbar sein, ist %q", releases(info.Removable))
	}
}
