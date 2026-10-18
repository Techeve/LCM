package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEntsperrenLaesstServerPasswortStehen: Das Entsperr-Skript darf nur einen
// fehlenden Passwort-Hash ersetzen. Ein Passwort, das jemand am Server gesetzt
// hat, übersteht den Abgleich; ein gesperrter Hash wird nur entsperrt.
func TestEntsperrenLaesstServerPasswortStehen(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	cases := []struct {
		name, field  string
		wantSperre   bool // usermod -p '*' (Hash ersetzt)
		wantEntsperr bool // usermod -U (Hash bleibt, Sperre fällt)
	}{
		{"echtes Passwort bleibt", "$6$salt$hash", false, false},
		{"gesperrtes Passwort wird entsperrt", "!$6$salt$hash", false, true},
		{"Sperre ohne Hash", "!", true, false},
		{"leeres Feld", "", true, false},
		{"Stern-Hash", "*", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			shadow := filepath.Join(dir, "shadow")
			if err := os.WriteFile(shadow, []byte("alice:"+tc.field+":19000:0:99999:7:::\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(dir, "aufrufe")
			// Attrappen: protokollieren nur, was aufgerufen wurde.
			for _, name := range []string{"usermod", "passwd", "chpasswd"} {
				fake := "#!/bin/sh\necho \"$(basename \"$0\") $*\" >> " + log + "\ncat >/dev/null\n"
				if err := os.WriteFile(filepath.Join(bin, name), []byte(fake), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			// Das Skript liest /etc/shadow; im Test zeigt es auf die Kopie.
			script := strings.ReplaceAll(unlockAccountScript("alice"), "/etc/shadow", shadow)
			cmd := exec.Command(sh, "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("skript: %v\n%s", err, out)
			}
			calls, _ := os.ReadFile(log)
			gesetzt := strings.Contains(string(calls), "usermod -p *")
			entsperrt := strings.Contains(string(calls), "usermod -U alice")
			if gesetzt != tc.wantSperre {
				t.Errorf("Hash ersetzt = %v, erwartet %v (aufrufe: %q)", gesetzt, tc.wantSperre, calls)
			}
			if entsperrt != tc.wantEntsperr {
				t.Errorf("entsperrt = %v, erwartet %v (aufrufe: %q)", entsperrt, tc.wantEntsperr, calls)
			}
		})
	}
}

// TestHelperEntsperrtWieVollModus: Der eingeschränkte Modus nutzt dieselbe
// Regel - der Platzhalter ist ersetzt, und das Helper-Skript enthält die
// Prüfung des Passwort-Hashes.
func TestHelperEntsperrtWieVollModus(t *testing.T) {
	if strings.Contains(lcmHelperScript, "@@ACCOUNT_UNLOCK@@") {
		t.Fatal("platzhalter für die entsperrung ist nicht ersetzt")
	}
	if !strings.Contains(lcmHelperScript, `lcm_hash=$(awk -F: -v u="$name"`) {
		t.Error("user-ensure prüft den Passwort-Hash nicht vor dem Ersetzen")
	}
}
