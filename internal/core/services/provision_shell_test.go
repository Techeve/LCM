package services

import (
	"strings"
	"testing"
)

// TestCreateUserShellFallback (R2-003): Alpine/BusyBox hat kein /bin/bash -
// OpenSSH lehnt Logins zu Konten mit nicht existierender Shell schon im
// Preauth ab, der Join scheiterte. Das Anlege-Skript muss die Wunsch-Shell
// prüfen und auf /bin/sh ausweichen, und zwar in BEIDEN Zweigen
// (useradd UND adduser).
func TestCreateUserShellFallback(t *testing.T) {
	script := createUserScript("lcm-svc")

	if !strings.Contains(script, "[ -x \"$USHELL\" ] || USHELL=/bin/sh") {
		t.Error("kein /bin/sh-Fallback für fehlende Shells im Skript")
	}
	if !strings.Contains(script, "USHELL='/bin/bash'") {
		t.Error("die Wunsch-Shell /bin/bash sollte der Ausgangswert sein")
	}
	if strings.Count(script, "-s \"$USHELL\"") != 2 {
		t.Errorf("beide Zweige (useradd/adduser) müssen die geprüfte Shell verwenden:\n%s", script)
	}
	// Die fest verdrahtete Shell direkt am -s wäre genau der alte Fehler.
	if strings.Contains(script, "-s /bin/bash") {
		t.Error("fest verdrahtetes '-s /bin/bash' gefunden - der Fallback wäre wirkungslos")
	}

	// Provisionierte Linux-Benutzer: frei gewählte Shell, gleicher Schutz.
	custom := createUserWithShellScript("tomtest", "/bin/zsh")
	if !strings.Contains(custom, "USHELL='/bin/zsh'") || !strings.Contains(custom, "USHELL=/bin/sh") {
		t.Errorf("auch für Wunsch-Shells muss der Fallback greifen:\n%s", custom)
	}
}

// TestKontoanlageNormalisiertAlpine (R2-046): BusyBox-adduser hinterließ ein
// leeres Passwortfeld (statt "*"/"!") und ein gruppenlesbares Home (2755) -
// ein anderer Ausgangszustand als auf jeder useradd-Distribution. Das
// Verwaltungswerkzeug normalisiert beides.
func TestKontoanlageNormalisiertAlpine(t *testing.T) {
	script := createUserWithShellScript("tomtest", "/bin/sh")
	if !strings.Contains(script, "chmod 700 /home/tomtest") {
		t.Errorf("Home-Rechte werden nicht auf 700 normalisiert:\n%s", script)
	}

	unlock := unlockAccountScript("tomtest")
	// usermod-Systeme: unbrauchbares Passwort-Hash "*" statt Sperre.
	if !strings.Contains(unlock, "usermod -p '*' tomtest") {
		t.Errorf("usermod-Zweig setzt kein '*': %s", unlock)
	}
	// BusyBox-Systeme: chpasswd -e setzt denselben Wert - passwd -u allein
	// ließ das leere Feld leer.
	if !strings.Contains(unlock, "chpasswd -e") {
		t.Errorf("BusyBox-Zweig normalisiert das Passwortfeld nicht via chpasswd -e: %s", unlock)
	}
	if !strings.Contains(unlock, "passwd -u tomtest") {
		t.Errorf("der letzte Rückfall passwd -u fehlt: %s", unlock)
	}
}
