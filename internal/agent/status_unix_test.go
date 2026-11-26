//go:build unix

package agent

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Die Datei ist für alle lesbar, auch wenn der Dienst mit strenger umask läuft.
func TestWriteStatusRechteUnabhaengigVonUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })
	dir := t.TempDir()
	if err := WriteStatus(dir, Status{Version: "x", Updated: time.Now()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, StatusFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("Rechte = %v, want 0644", info.Mode().Perm())
	}
}
