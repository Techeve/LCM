package services

import (
	"os"
	"testing"
)

// TestPrintPAMScriptsForSandbox schreibt die 2FA-Skripte in Dateien, wenn
// LCM_DUMP_2FA gesetzt ist - nur als Werkzeug für den Sandbox-Test auf einem
// echten Linux (läuft im normalen Testlauf als No-Op durch).
func TestPrintPAMScriptsForSandbox(t *testing.T) {
	dir := os.Getenv("LCM_DUMP_2FA")
	if dir == "" {
		t.Skip("LCM_DUMP_2FA nicht gesetzt")
	}
	if err := os.WriteFile(dir+"/pam_install.sh", []byte(ssh2faPAMScript()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/pam_restore.sh", []byte(ssh2faPAMRestoreScript()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
