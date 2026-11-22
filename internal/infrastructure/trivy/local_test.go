package trivy

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestCommandStartetTrivyMitAbsolutemPfad: Der Landlock-Starter wird per
// execve zum Programm und sucht dabei nicht im PATH. Mit dem bloßen Namen
// „trivy" scheiterte dort jeder Aufruf - der Aufruf muss den Pfad tragen,
// den LookPath gefunden hat.
func TestCommandStartetTrivyMitAbsolutemPfad(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "trivy-test")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	cmd := newLocal("trivy-test", t.TempDir()).command(context.Background(), false, nil, "--version")
	if !slices.Contains(cmd.Args, bin) {
		t.Errorf("absoluter Pfad %s fehlt im Aufruf: %v", bin, cmd.Args)
	}
	if slices.Contains(cmd.Args, "trivy-test") {
		t.Errorf("der Aufruf nennt Trivy nur beim Namen: %v", cmd.Args)
	}
}

// TestImageScanTrenntDieReferenzAb: Die Referenz kommt vom verwalteten
// Server. Hinter „--" bleibt sie ein Ziel, auch wenn sie mit einem
// Bindestrich beginnt - davor läse Trivy sie als Option.
func TestImageScanTrenntDieReferenzAb(t *testing.T) {
	ref := "--config=/etc/lcm/config.json"
	args := imageScanArgs("/var/lib/lcm/trivy", ref)
	n := len(args)
	if n < 2 || args[n-2] != "--" || args[n-1] != ref {
		t.Errorf("die Referenz steht nicht hinter dem Trenner: %v", args)
	}
	if !slices.Contains(args, "--skip-db-update") || !slices.Contains(args, "/var/lib/lcm/trivy") {
		t.Errorf("Optionen des Image-Scans fehlen: %v", args)
	}
}

// TestCommandLegtDenCacheAn: Bubblewrap übergeht einen fehlenden Pfad
// (--bind-try). Gäbe es den Cache beim Start nicht, landete die geladene
// Datenbank im flüchtigen Dateisystem der Sandbox - der Download meldete
// Erfolg, und danach fehlte die Datei. Auf einer frischen Installation gibt es
// das Verzeichnis noch nicht.
func TestCommandLegtDenCacheAn(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "var", "lib", "lcm", "trivy")
	newLocal("trivy", cache).command(context.Background(), true, nil, "--version")
	info, err := os.Stat(cache)
	if err != nil || !info.IsDir() {
		t.Fatalf("Cache-Verzeichnis wurde nicht angelegt: %v", err)
	}
}
