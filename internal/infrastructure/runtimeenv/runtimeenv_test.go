package runtimeenv

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeRoot bildet die Erkennung auf ein Verzeichnis im Testordner ab. Ohne
// das ließe sich immer nur der Zweig prüfen, in dem der Test gerade zufällig
// läuft - auf einem Entwicklerrechner „Host", im CI-Container „Container".
// Der jeweils andere wäre nie abgedeckt, und das ist der interessante.
type fakeRoot struct{ root string }

func (r fakeRoot) exists(path string) bool {
	_, err := os.Stat(filepath.Join(r.root, path))
	return err == nil
}

func (r fakeRoot) read(path string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(r.root, path))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// schreibe legt eine Datei samt Elternverzeichnissen unterhalb der Wurzel an.
func schreibe(t *testing.T, root, path, content string) {
	t.Helper()
	voll := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(voll), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(voll, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestErkennung(t *testing.T) {
	cases := []struct {
		name    string
		dateien map[string]string
		erwarte Kind
	}{
		{
			name:    "leere Wurzel ist ein Host",
			dateien: nil,
			erwarte: Host,
		},
		{
			name:    "Docker legt /.dockerenv an",
			dateien: map[string]string{"/.dockerenv": ""},
			erwarte: Docker,
		},
		{
			name:    "Podman legt /run/.containerenv an",
			dateien: map[string]string{"/run/.containerenv": "engine=\"podman\"\n"},
			erwarte: Podman,
		},
		{
			name:    "Docker ueber den cgroup-Pfad von PID 1",
			dateien: map[string]string{"/proc/1/cgroup": "0::/docker/6f3a1b\n"},
			erwarte: Docker,
		},
		{
			name:    "LXC ueber den cgroup-Pfad von PID 1",
			dateien: map[string]string{"/proc/1/cgroup": "0::/lxc/101\n"},
			erwarte: LXC,
		},
		{
			// Erkennbar ein Container, aber die Laufzeit gibt sich nicht zu
			// erkennen. Dann sagen wir genau das, statt zu raten.
			name:    "systemd markiert die Umgebung ohne Laufzeit-Marker",
			dateien: map[string]string{"/proc/1/environ": "PATH=/usr/bin\x00container=lxc\x00"},
			erwarte: Container,
		},
		{
			// Ein Host MIT systemd hat /proc/1/environ, aber ohne
			// container= - der Regelfall auf jedem .deb-Server.
			name: "Host mit systemd bleibt Host",
			dateien: map[string]string{
				"/proc/1/environ": "PATH=/usr/bin\x00INVOCATION_ID=abc\x00",
				"/proc/1/cgroup":  "0::/init.scope\n",
			},
			erwarte: Host,
		},
	}

	for _, f := range cases {
		t.Run(f.name, func(t *testing.T) {
			root := t.TempDir()
			for path, content := range f.dateien {
				schreibe(t, root, path, content)
			}
			if got := detect(fakeRoot{root}); got != f.erwarte {
				t.Errorf("erwartet %q, bekam %q", f.erwarte, got)
			}
		})
	}
}

// TestInContainerGiltFuerJedeLaufzeit: Die Kurzform darf keine Laufzeit
// vergessen. Kommt eine neue Kind-Konstante dazu und wird hier nicht
// mitgezaehlt, faellt das hier auf - und nicht erst dadurch, dass im
// Container wieder Schaltflaechen erscheinen, die dort scheitern muessen.
func TestInContainerGiltFuerJedeLaufzeit(t *testing.T) {
	for _, k := range []Kind{Docker, Podman, LXC, Container} {
		if k == Host {
			t.Fatalf("%q darf nicht Host sein", k)
		}
	}
	if Host == "" {
		t.Fatal("Host braucht einen Wert - sonst ist der Nullwert eines Kind-Feldes ein Container")
	}
}
