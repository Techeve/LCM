package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"LCM/internal/infrastructure/tlsx"
)

// Ein von ACME ausgestelltes Zertifikat kommt beim Neuladen in den Pool, mit
// Quelle acme, und weicht einem Zertifikat aus der config.json mit demselben
// Namen.
func TestReloadLaedtAcmeZertifikateHinterConfig(t *testing.T) {
	own := newTestCert(t, time.Now().Add(30*24*time.Hour))
	f := newReloadFixture(t)

	issued := newTestCert(t, time.Now().Add(60*24*time.Hour))
	dir := filepath.Join(f.dataDir, "tls", "acme", "certs", "0a1b2c3d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for src, dst := range map[string]string{issued.cert: "cert.pem", issued.key: "key.pem"} {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, dst), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	report := f.reloader.Reload(reloadByCLI)
	if !report.OK() || len(report.Certificates) != 1 || report.Certificates[0].ID != "acme-0a1b2c3d" ||
		report.Certificates[0].Cert.Source != tlsx.SourceACME {
		t.Fatalf("Bericht = %+v", report)
	}
	if serialOf(f.served("lcm.example")) != issued.serial {
		t.Fatal("das ACME-Zertifikat wird nicht ausgeliefert")
	}

	writeConfig(t, f.configPath, own)
	f.reloader.Reload(reloadByCLI)
	if serialOf(f.served("lcm.example")) != own.serial {
		t.Error("ein Zertifikat aus der config.json muss dem von ACME vorgehen")
	}
}
