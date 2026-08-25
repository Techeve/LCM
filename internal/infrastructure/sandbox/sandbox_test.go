package sandbox

import (
	"strings"
	"testing"
)

// TestBaseSystemSpecSperrtDasDatenverzeichnis: der Kern der Sache - die
// Grundausstattung darf unter keinen Umständen das LCM-Datenverzeichnis
// enthalten. Dort liegen Master-Key und Datenbank nebeneinander; stünde einer
// dieser Pfade in der Liste, wäre die ganze Sandbox wertlos.
func TestBaseSystemSpecSperrtDasDatenverzeichnis(t *testing.T) {
	spec := BaseSystemSpec()
	verboten := []string{"/var/lib/lcm", "/etc/lcm", "/root", "/home", "/var/lib"}
	for _, p := range append(append([]string{}, spec.ReadDirs...), append(spec.ReadFiles, spec.WriteDirs...)...) {
		for _, v := range verboten {
			if p == v || strings.HasPrefix(p, v+"/") {
				t.Errorf("Grundausstattung enthält %q - damit käme der Kindprozess an %q", p, v)
			}
		}
	}
	if len(spec.WriteDirs) != 0 {
		t.Errorf("Grundausstattung darf nichts beschreibbar machen, hat aber: %v", spec.WriteDirs)
	}
	if spec.AllowNet {
		t.Error("Grundausstattung darf das Netz nicht von sich aus freigeben")
	}
}

// TestWithPathsLaesstDieBasisUnberuehrt: WithPaths darf die gemeinsam genutzte
// Basis nicht verändern - sonst würde ein Aufruf die Rechte des nächsten
// aufweichen (append kann auf ein geteiltes Array schreiben).
func TestWithPathsLaesstDieBasisUnberuehrt(t *testing.T) {
	basis := BaseSystemSpec()
	vorher := len(basis.ReadDirs)

	a := basis.WithPaths([]string{"/opt/a"}, nil, []string{"/var/cache/a"})
	b := basis.WithPaths([]string{"/opt/b"}, nil, []string{"/var/cache/b"})

	if len(basis.ReadDirs) != vorher {
		t.Errorf("die Basis wurde verändert (%d statt %d Einträge)", len(basis.ReadDirs), vorher)
	}
	if containsPath(a.ReadDirs, "/opt/b") || containsPath(b.ReadDirs, "/opt/a") {
		t.Error("zwei Specs teilen sich denselben Speicher - Rechte des einen landen beim anderen")
	}
	if !containsPath(a.WriteDirs, "/var/cache/a") || !containsPath(b.WriteDirs, "/var/cache/b") {
		t.Error("WithPaths hat die Schreibpfade nicht übernommen")
	}
}

// TestWithNet: die Netzfreigabe ist ein bewusster Schalter je Aufruf.
func TestWithNet(t *testing.T) {
	if BaseSystemSpec().WithNet(true).AllowNet != true {
		t.Error("WithNet(true) greift nicht")
	}
	if BaseSystemSpec().WithNet(true).WithNet(false).AllowNet {
		t.Error("WithNet(false) hebt die Freigabe nicht wieder auf")
	}
}

func containsPath(list []string, want string) bool {
	for _, p := range list {
		if p == want {
			return true
		}
	}
	return false
}
