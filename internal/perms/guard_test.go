package perms

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGuardRichtetEigeneDateien(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o777)
	key := filepath.Join(dir, "lcm.key")
	if err := os.WriteFile(key, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	cert := filepath.Join(dir, "lcm-cert.pem")
	if err := os.WriteFile(cert, []byte("c"), 0o646); err != nil {
		t.Fatal(err)
	}
	confDir := t.TempDir()
	_ = os.Chmod(confDir, 0o770)
	conf := filepath.Join(confDir, "config.json")
	if err := os.WriteFile(conf, []byte("{}"), 0o664); err != nil {
		t.Fatal(err)
	}
	// umask hat mitgeredet - die Ausgangslage ausdrücklich setzen.
	for path, mode := range map[string]os.FileMode{key: 0o644, cert: 0o646, conf: 0o664} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}

	findings := New(dir, conf).Check()
	if len(findings) != 4 {
		t.Fatalf("erwartet 4 Abweichungen, bekam %d: %v", len(findings), findings)
	}
	for _, f := range findings {
		if !f.Fixed {
			t.Errorf("eigene Datei nicht gerichtet: %s", f)
		}
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, key: 0o600, cert: 0o640, conf: 0o660} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s: %04o, erwartet %04o", path, info.Mode().Perm(), want)
		}
	}
	// Danach ist Ruhe.
	if again := New(dir, conf).Check(); len(again) != 0 {
		t.Errorf("zweiter Lauf muss leer sein, bekam %v", again)
	}
}

func TestGuardBackupsUndUnterverzeichnisse(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o700)
	backups := filepath.Join(dir, "backups")
	if err := os.Mkdir(backups, 0o755); err != nil {
		t.Fatal(err)
	}
	bak := filepath.Join(backups, "lcm-backup-1.lcmbak")
	if err := os.WriteFile(bak, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(backups, 0o755)
	_ = os.Chmod(bak, 0o644)
	findings := New(dir, "").Check()
	if len(findings) != 2 {
		t.Fatalf("erwartet 2 Abweichungen (Verzeichnis, Archiv), bekam %v", findings)
	}
	info, _ := os.Stat(bak)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("Archiv: %04o", info.Mode().Perm())
	}
}
