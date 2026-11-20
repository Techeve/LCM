//go:build linux

package agent

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Echtes Pseudo-Terminal: Die Shell rechnet, und Close beendet sie samt einem
// hängenden Kindprozess. Braucht root (Arbeitsverzeichnis /root, wie im
// Dienst).
func TestPTYShellEcht(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	shell, err := startPTYShell("xterm", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shell.Write([]byte("stty size; echo lcm-$((40+2)); sleep 600\n")); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "lcm-42") && time.Now().Before(deadline) {
		n, err := shell.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(out.String(), "30 100") || !strings.Contains(out.String(), "lcm-42") {
		t.Fatalf("ausgabe ohne fenstergröße oder rechnung: %q", out.String())
	}
	done := make(chan error, 1)
	go func() { done <- shell.Close() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("close hängt am sleep")
	}
}
