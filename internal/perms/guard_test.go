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

func TestIsSystemDirLoestSymlinksAuf(t *testing.T) {
	root := t.TempDir()
	fakeBin := filepath.Join(root, "usr-bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Wie /bin -> /usr/bin auf Debian.
	link := filepath.Join(root, "bin")
	if err := os.Symlink(fakeBin, link); err != nil {
		t.Fatal(err)
	}
	denied := []string{link}
	for _, p := range []string{fakeBin, link, fakeBin + "/", filepath.Join(link, "..", "usr-bin")} {
		if !isSystemDirIn(p, denied) {
			t.Errorf("%s müsste als Systemverzeichnis gelten", p)
		}
	}
	other := filepath.Join(root, "lcm")
	if isSystemDirIn(other, denied) || isSystemDirIn(filepath.Join(fakeBin, "unter"), denied) {
		t.Error("Unterverzeichnis oder fremdes Verzeichnis darf nicht gelten")
	}
	for _, p := range []string{"/", "/usr/bin", "/bin", "/etc", "/usr/local/bin"} {
		if !IsSystemDir(p) {
			t.Errorf("%s müsste als Systemverzeichnis gelten", p)
		}
	}
	if IsSystemDir("/var/lib/lcm") {
		t.Error("/var/lib/lcm ist kein Systemverzeichnis")
	}
}

// Der Fall aus dem Fehlerbild: Rechte 0755 auf einem Verzeichnis voller
// Programme - der Wächter darf sie weder verschärfen noch hineingreifen.
func TestGuardFasstSystemverzeichnisNichtAn(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "ls")
	if err := os.WriteFile(prog, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	g := New(dir, filepath.Join(dir, "config.json"))
	g.denied = []string{dir}
	findings := g.Check()
	if len(findings) != 1 || findings[0].Fixed {
		t.Fatalf("erwartet eine unbehobene Meldung, bekam %v", findings)
	}
	assertMode(t, dir, 0o755)
	assertMode(t, prog, 0o755)
}

func TestGuardFasstFremdesVerzeichnisNichtAn(t *testing.T) {
	dir := t.TempDir()
	foreign := filepath.Join(dir, "dokument.txt")
	if err := os.WriteFile(foreign, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if findings := New(dir, "").Check(); len(findings) != 1 || findings[0].Fixed {
		t.Fatalf("erwartet eine unbehobene Meldung, bekam %v", findings)
	}
	assertMode(t, dir, 0o755)
	assertMode(t, foreign, 0o644)
}

func TestGuardFolgtKeinemSymlink(t *testing.T) {
	outside := t.TempDir()
	if err := os.Chmod(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lcm.key"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "verweis")); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(dir, 0o700)
	New(dir, "").Check()
	assertMode(t, outside, 0o755)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Errorf("%s: %04o, erwartet %04o", path, info.Mode().Perm(), want)
	}
}
