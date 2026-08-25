package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func sftpProfile() *domain.PrivilegeProfile {
	return &domain.PrivilegeProfile{
		ID: 4, Slug: "datenpflege", Name: "Datenpflege",
		AccountType: domain.AccountTypeSFTP,
	}
}

// TestSFTPDropinSchliesstMitMatchAll: Die Include-Zeile steht in sshd_config
// GANZ OBEN, und ein Match-Block gilt bis zum nächsten Match. Ohne den
// Abschluss rutschte die gesamte restliche Konfiguration ungewollt in den
// Block - ein Fehler, der erst auffällt, wenn sich jemand nicht mehr anmelden
// kann.
func TestSFTPDropinSchliesstMitMatchAll(t *testing.T) {
	content := profileSSHDContent([]*domain.PrivilegeProfile{sftpProfile()})
	if !strings.Contains(content, "Match Group lcm-prof-datenpflege") {
		t.Errorf("match-block fehlt:\n%s", content)
	}
	if !strings.Contains(content, "ForceCommand internal-sftp") {
		t.Errorf("internal-sftp fehlt:\n%s", content)
	}
	if !strings.HasSuffix(strings.TrimSpace(content), "Match all") {
		t.Errorf("das drop-in muss mit „Match all\" enden:\n%s", content)
	}
}

// TestSFTPDropinNurFuerDenKontotyp: Profile mit Shell-Zugang erzeugen keinen
// Block - sonst verlöre ein normaler Benutzer seine Shell.
func TestSFTPDropinNurFuerDenKontotyp(t *testing.T) {
	shell := &domain.PrivilegeProfile{ID: 1, Slug: "web", AccountType: domain.AccountTypeShell}
	if got := profileSSHDContent([]*domain.PrivilegeProfile{shell}); got != "" {
		t.Errorf("shell-profil darf kein drop-in erzeugen:\n%s", got)
	}
	// Ohne sftp-Profil wird die Datei entfernt, nicht leer geschrieben.
	script := profileSSHDScript([]*domain.PrivilegeProfile{shell})
	if !strings.Contains(script, "rm -f "+profileSSHDPath) {
		t.Errorf("drop-in wird nicht entfernt: %s", script)
	}
}

// TestSFTPDropinWirdGeprueftUndZurueckgerollt: Ein Fehler in der
// sshd-Konfiguration sperrt nicht nur die Benutzer aus, sondern auch LCMs
// eigenen Zugang.
func TestSFTPDropinWirdGeprueftUndZurueckgerollt(t *testing.T) {
	script := profileSSHDScript([]*domain.PrivilegeProfile{sftpProfile()})
	for _, marker := range []string{".lcmbak", "sshd -t", "mv " + profileSSHDPath + ".lcmbak"} {
		if !strings.Contains(script, marker) {
			t.Errorf("schritt fehlt (%s): %s", marker, script)
		}
	}
	// Die Prüfung muss VOR dem Neuladen stehen.
	if strings.Index(script, "sshd -t") > strings.Index(script, "systemctl reload sshd") {
		t.Errorf("sshd -t läuft erst nach dem reload: %s", script)
	}
}

// TestSFTPKontoVerliertDieShell: Der sshd erzwingt internal-sftp - die Shell
// wegzunehmen ist der zweite Riegel, damit auch ein Zugang an sshd vorbei
// (su, cron) keine bekommt.
func TestSFTPKontoVerliertDieShell(t *testing.T) {
	effect := effectFor(&domain.LinuxUser{Username: "anna"}, sftpProfile())
	if !effect.NoShell {
		t.Fatalf("sftp-profil muss die shell entziehen: %+v", effect)
	}
	step := nologinShellStep("anna")
	// Der Pfad weicht je Distribution ab, auf BusyBox fehlt nologin ganz.
	for _, marker := range []string{"/usr/sbin/nologin", "/sbin/nologin", "/bin/false", "usermod -s"} {
		if !strings.Contains(step, marker) {
			t.Errorf("rückfall %s fehlt: %s", marker, step)
		}
	}
	// Ein Shell-Profil lässt die Shell in Ruhe.
	if effectFor(&domain.LinuxUser{Username: "bert"}, &domain.PrivilegeProfile{Slug: "web"}).NoShell {
		t.Error("ein shell-profil darf die shell nicht entziehen")
	}
}
