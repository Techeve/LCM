package creds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadKenntNurGesetzteCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DirEnv, dir)
	if err := os.WriteFile(filepath.Join(dir, TrivyToken), []byte("  geheim\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, ok := Read(TrivyToken); !ok || v != "geheim" {
		t.Errorf("Read = %q, %v", v, ok)
	}
	if _, ok := Read(MasterKey); ok {
		t.Error("nicht abgelegtes Credential darf nicht gefunden werden")
	}
}

func TestOhneSystemdGibtEsNichts(t *testing.T) {
	t.Setenv(DirEnv, "")
	if Dir() != "" || Path(MasterKey) != "" {
		t.Error("ohne CREDENTIALS_DIRECTORY darf nichts gefunden werden")
	}
	if _, ok := Read(MasterKey); ok {
		t.Error("ohne Verzeichnis kein Credential")
	}
}
