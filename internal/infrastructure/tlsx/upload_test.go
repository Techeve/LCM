package tlsx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func saveTestUpload(t *testing.T, dataDir string) string {
	t.Helper()
	c := newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}})
	id, err := SaveUpload(dataDir, c.certPEM, c.keyPEM, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSaveUploadLegtDateienMitRechtenAb(t *testing.T) {
	dataDir := t.TempDir()
	c := newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}})
	id, err := SaveUpload(dataDir, c.certPEM, c.keyPEM, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !uploadIDPattern.MatchString(id) {
		t.Errorf("id %q hat nicht die erwartete Form", id)
	}

	dir := filepath.Join(dataDir, "tls", "uploads", id)
	for name, want := range map[string][]byte{"cert.pem": c.certPEM, "key.pem": c.keyPEM} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s: Inhalt stimmt nicht (%v)", name, err)
		}
		if info, _ := os.Stat(filepath.Join(dir, name)); info.Mode().Perm() != 0o600 {
			t.Errorf("%s: Rechte %v, erwartet 0600", name, info.Mode().Perm())
		}
	}
	for _, d := range []string{dir, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))} {
		if info, _ := os.Stat(d); info.Mode().Perm() != 0o700 {
			t.Errorf("%s: Rechte %v, erwartet 0700", d, info.Mode().Perm())
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.new")); len(left) != 0 {
		t.Errorf("Zwischendateien übrig: %v", left)
	}
}

func TestSaveUploadSchreibtNichtsBeiUngueltigemZertifikat(t *testing.T) {
	dataDir := t.TempDir()
	c := newTestCert(t, leafOpts{dns: []string{"a.example.org"},
		notBefore: time.Now().Add(-48 * time.Hour), notAfter: time.Now().Add(-time.Hour)})
	if _, err := SaveUpload(dataDir, c.certPEM, c.keyPEM, time.Now()); err == nil {
		t.Fatal("abgelaufenes Zertifikat wurde angenommen")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "tls")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("trotz Fehler wurde etwas geschrieben (%v)", err)
	}
}

func TestUploadSpecsUndPoolLaden(t *testing.T) {
	dataDir := t.TempDir()
	if specs, err := UploadSpecs(dataDir); err != nil || len(specs) != 0 {
		t.Fatalf("ohne Uploads: %v, %v", specs, err)
	}
	id := saveTestUpload(t, dataDir)
	// Fremdes im Verzeichnis stört nicht.
	if err := os.Mkdir(filepath.Join(dataDir, "tls", "uploads", "fremd"), 0o700); err != nil {
		t.Fatal(err)
	}

	specs, err := UploadSpecs(dataDir)
	if err != nil || len(specs) != 1 {
		t.Fatalf("UploadSpecs = %v, %v", specs, err)
	}
	if specs[0].ID != "upload-"+id || specs[0].Source != SourceUpload {
		t.Errorf("spec = %+v", specs[0])
	}
	p := newTestPool(t)
	mustLoad(t, p, specs...)
	if got := pick(t, p, "lcm.example.org"); got.Subject.CommonName != "test" {
		t.Errorf("Upload wird nicht ausgeliefert: %s", got.Subject.CommonName)
	}
}

func TestRemoveUpload(t *testing.T) {
	dataDir := t.TempDir()
	id := saveTestUpload(t, dataDir)
	// Eine Datei neben dem Upload-Verzeichnis, die ein Traversal treffen würde.
	keep := filepath.Join(dataDir, "tls", "wichtig.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"", "../", "../..", "../uploads/" + id, id + "/..", ".", "ABCDEF12", "abc", id + "x", strings.Repeat("a", 64)} {
		if err := RemoveUpload(dataDir, bad); !errors.Is(err, ErrUploadID) {
			t.Errorf("id %q: Fehler = %v, erwartet ErrUploadID", bad, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("Datei außerhalb wurde berührt: %v", err)
	}
	if err := RemoveUpload(dataDir, "00000000"); !errors.Is(err, ErrUploadNotFound) {
		t.Errorf("unbekannte id: %v", err)
	}
	if err := RemoveUpload(dataDir, id); err != nil {
		t.Fatal(err)
	}
	if specs, _ := UploadSpecs(dataDir); len(specs) != 0 {
		t.Errorf("Upload nicht entfernt: %v", specs)
	}
}

func TestUploadSicherungUndWiederherstellung(t *testing.T) {
	dataDir := t.TempDir()
	id := saveTestUpload(t, dataDir)

	files, err := UploadBackupFiles(dataDir)
	if err != nil || len(files) != 2 {
		t.Fatalf("UploadBackupFiles = %v, %v", files, err)
	}
	for name, path := range files {
		target, ok := UploadRestoreTarget(dataDir, name)
		if !ok || target != path {
			t.Errorf("%s: Ziel %q (ok=%v), erwartet %q", name, target, ok, path)
		}
	}
	for _, bad := range []string{
		"lcm-cert.pem", "tls-upload-" + id + "-andere.pem", "tls-upload-..-cert.pem",
		"tls-upload-" + id + "/../x-cert.pem", "tls-upload--cert.pem", "tls-upload-" + id + "-cert.pem/..",
	} {
		if target, ok := UploadRestoreTarget(dataDir, bad); ok {
			t.Errorf("%q wurde als Upload-Datei akzeptiert: %s", bad, target)
		}
	}
}

func TestPruneUploadsBehaeltNurDieGenanntenUndFremdes(t *testing.T) {
	dataDir := t.TempDir()
	keep, drop := saveTestUpload(t, dataDir), saveTestUpload(t, dataDir)
	foreign := filepath.Join(uploadsDir(dataDir), "kein-upload")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := PruneUploads(dataDir, map[string]bool{keep: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(uploadsDir(dataDir), drop)); !os.IsNotExist(err) {
		t.Errorf("%s ist noch da: %v", drop, err)
	}
	for _, path := range []string{filepath.Join(uploadsDir(dataDir), keep), foreign} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s fehlt: %v", path, err)
		}
	}
	if err := PruneUploads(t.TempDir(), nil); err != nil {
		t.Errorf("ohne Upload-Verzeichnis: %v", err)
	}
}
