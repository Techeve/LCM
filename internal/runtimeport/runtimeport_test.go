package runtimeport

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Read(dir); ok {
		t.Fatal("Port gelesen, obwohl nichts geschrieben wurde")
	}
	if err := Write(dir, 9400); err != nil {
		t.Fatal(err)
	}
	if port, ok := Read(dir); !ok || port != 9400 {
		t.Errorf("Read = %d, %v", port, ok)
	}
	if err := Write(dir, 9401); err != nil {
		t.Fatal(err)
	}
	if port, _ := Read(dir); port != 9401 {
		t.Errorf("nach dem Überschreiben: %d", port)
	}
}

func TestReadIgnoriertKaputteDatei(t *testing.T) {
	dir := t.TempDir()
	for _, content := range []string{"", "abc", "0", "70000", "-1"} {
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if port, ok := Read(dir); ok {
			t.Errorf("%q als Port gelesen: %d", content, port)
		}
	}
}
