package agent

import (
	"os"
	"testing"
)

// TestMain lenkt die Job-Ablage in ein Wegwerf-Verzeichnis: Die Tests laufen
// nicht als root und dürfen /var/lib nicht anfassen.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lcm-agent-jobs-")
	if err != nil {
		panic(err)
	}
	JobsDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
