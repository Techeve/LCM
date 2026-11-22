package services

import (
	"os"
	"path/filepath"
	"testing"
)

// Konto und Zertifikate von ACME kommen aus dem Staging an ihren Platz zurück;
// fremde Dateien im Staging und Namen mit Pfadanteilen bleiben liegen.
func TestRestoreACMEFilesPutsThemBack(t *testing.T) {
	staging, dataDir := t.TempDir(), t.TempDir()
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(staging, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{
		"tls-acme-account.key", "tls-acme-registration-0a1b2c3d.json",
		"tls-acme-cert-0a1b2c3d-cert.pem", "tls-acme-cert-0a1b2c3d-key.pem",
		"app.db", "tls-acme-cert-..-cert.pem",
	} {
		write(name)
	}

	if err := restoreACMEFiles(staging, dataDir); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"tls/acme/account.key", "tls/acme/accounts/0a1b2c3d.json",
		"tls/acme/certs/0a1b2c3d/cert.pem", "tls/acme/certs/0a1b2c3d/key.pem",
	} {
		info, err := os.Stat(filepath.Join(dataDir, rel))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v, %v", rel, info, err)
		}
	}
	for _, name := range []string{"app.db", "tls-acme-cert-..-cert.pem"} {
		if _, err := os.Stat(filepath.Join(staging, name)); err != nil {
			t.Errorf("%s wurde angefasst: %v", name, err)
		}
	}
}
