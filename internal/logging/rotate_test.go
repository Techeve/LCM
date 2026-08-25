package logging

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestFile baut einen Rotator mit fester Uhr, damit die Zeitstempel der
// Altstände vorhersagbar sind.
func newTestFile(t *testing.T, maxSizeBytes int64, maxBackups, maxAgeDays int) (*rotatingFile, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lcm.log")
	r := newRotatingFile(path, 0, maxBackups, maxAgeDays)
	r.maxSize = maxSizeBytes
	clock := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, dir
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestWriteWithoutRotation(t *testing.T) {
	r, dir := newTestFile(t, 100, 5, 7)
	for i := 0; i < 3; i++ {
		if _, err := r.Write([]byte("zeile\n")); err != nil {
			t.Fatal(err)
		}
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("erwartet nur die aktive datei, gefunden: %v", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "lcm.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "zeile\nzeile\nzeile\n" {
		t.Errorf("inhalt = %q", data)
	}
}

func TestRotationCompressesOldFile(t *testing.T) {
	r, dir := newTestFile(t, 20, 5, 7)
	if _, err := r.Write([]byte("erster stand xxxx\n")); err != nil { // 18 Bytes
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("nach rotation\n")); err != nil {
		t.Fatal(err)
	}

	active, err := os.ReadFile(filepath.Join(dir, "lcm.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "nach rotation\n" {
		t.Errorf("aktive datei = %q, erwartet nur den neuen eintrag", active)
	}

	var archive string
	for _, n := range names(t, dir) {
		if strings.HasSuffix(n, ".gz") {
			archive = filepath.Join(dir, n)
		}
	}
	if archive == "" {
		t.Fatalf("kein komprimierter altstand, gefunden: %v", names(t, dir))
	}
	if got := readGzip(t, archive); got != "erster stand xxxx\n" {
		t.Errorf("altstand = %q", got)
	}
}

func readGzip(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPruneKeepsOnlyMaxBackups(t *testing.T) {
	r, dir := newTestFile(t, 10, 2, 7)
	for i := 0; i < 5; i++ {
		if _, err := r.Write([]byte("0123456789ab\n")); err != nil {
			t.Fatal(err)
		}
	}
	archives := 0
	for _, n := range names(t, dir) {
		if strings.HasSuffix(n, ".gz") {
			archives++
		}
	}
	if archives != 2 {
		t.Errorf("altstände = %d, erwartet 2 - gefunden: %v", archives, names(t, dir))
	}
}

func TestPruneDropsExpiredBackups(t *testing.T) {
	r, dir := newTestFile(t, 10, 10, 7)

	// Ein Altstand, dessen Zeitstempel im Namen älter als die Aufbewahrung ist.
	stale := filepath.Join(dir, "lcm-2026-08-01T09-00-00.000.log.gz")
	if err := os.WriteFile(stale, []byte("alt"), 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Write([]byte("0123456789ab\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("loest rotation aus\n")); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("abgelaufener altstand wurde nicht entfernt: %v", err)
	}
}

func TestReopenAppendsToExistingFile(t *testing.T) {
	r, dir := newTestFile(t, 1000, 5, 7)
	if _, err := r.Write([]byte("vorher\n")); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	again := newRotatingFile(filepath.Join(dir, "lcm.log"), 0, 5, 7)
	again.maxSize = 1000
	defer func() { _ = again.Close() }()
	if _, err := again.Write([]byte("nachher\n")); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "lcm.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "vorher\nnachher\n" {
		t.Errorf("inhalt = %q, erwartet angehängt", data)
	}
}

func TestFilePermissions(t *testing.T) {
	r, dir := newTestFile(t, 1000, 5, 7)
	if _, err := r.Write([]byte("x\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "lcm.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != logFilePerm {
		t.Errorf("rechte = %o, erwartet %o", got, logFilePerm)
	}
}

func TestParseBackupTime(t *testing.T) {
	tests := []struct {
		name string
		file string
		ok   bool
	}{
		{"unkomprimiert", "lcm-2026-08-18T12-00-01.000.log", true},
		{"komprimiert", "lcm-2026-08-18T12-00-01.000.log.gz", true},
		{"aktive datei", "lcm.log", false},
		{"fremde datei", "andere-2026-08-18T12-00-01.000.log", false},
		{"kaputter stempel", "lcm-kein-datum.log", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := parseBackupTime(tt.file, "lcm-", ".log"); ok != tt.ok {
				t.Errorf("ok = %v, erwartet %v", ok, tt.ok)
			}
		})
	}
}
