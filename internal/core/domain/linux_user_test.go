package domain

import "testing"

func TestValidLinuxShell(t *testing.T) {
	valid := []string{"/bin/bash", "/usr/bin/zsh", "/bin/sh", "/usr/sbin/nologin", "/bin/false"}
	for _, s := range valid {
		if !ValidLinuxShell(s) {
			t.Errorf("ValidLinuxShell(%q) = false, erwartet true", s)
		}
	}
	// Injection-Versuche und Unfug müssen abgelehnt werden - der Wert fließt
	// in ein `useradd -s <shell>`-Kommando als root.
	invalid := []string{
		"", "/", "bash", "bin/bash", // leer/relativ/kein absoluter Pfad
		"x$(id)", "/bin/sh; rm -rf /", "/bin/sh -c 'x'",
		"/bin/bash\nrm", "/bin/ba sh", "/bin/`id`", "/bin/sh|cat",
	}
	for _, s := range invalid {
		if ValidLinuxShell(s) {
			t.Errorf("ValidLinuxShell(%q) = true, erwartet false (Injection/ungültig)", s)
		}
	}
}
