package services

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// runPAMScript führt ein PAM-Skript gegen eine Kopie der Fixture aus und
// liefert den Inhalt danach. `sed -i.x` verstehen GNU- und BSD-sed gleich.
func runPAMScript(t *testing.T, script, pamFile string) string {
	t.Helper()
	script = strings.ReplaceAll(script, "sed -i ", "sed -i.sedbak ")
	script = strings.ReplaceAll(script, pamSSHDPath, pamFile)
	if out, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("pam-skript scheitert: %v\n%s", err, out)
	}
	got, err := os.ReadFile(pamFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

// TestSSH2FAPAMLegtPasswortStackStill läuft gegen die echten
// /etc/pam.d/sshd der vier Familien (aus den Paketen von Arch, Debian 12,
// Rocky 9 und openSUSE Leap 15.6). Bleibt die Zeile mit dem Passwort-Stack
// aktiv, fragt PAM nach dem Code auch das Passwort ab - ein Konto ohne
// Passwort (nur Key) wäre ausgesperrt. Auf Arch war genau das der Fall:
// system-remote-login fehlte im Muster.
func TestSSH2FAPAMLegtPasswortStackStill(t *testing.T) {
	stacks := map[string]string{
		"arch":   "auth      include   system-remote-login",
		"debian": "@include common-auth",
		"rhel":   "auth       substack     password-auth",
		"suse":   "auth        include     common-auth",
	}
	for distro, stack := range stacks {
		t.Run(distro, func(t *testing.T) {
			orig, err := os.ReadFile(filepath.Join("testdata", "pam", distro+".sshd"))
			if err != nil {
				t.Fatal(err)
			}
			pamFile := filepath.Join(t.TempDir(), "sshd")
			if err := os.WriteFile(pamFile, orig, 0o644); err != nil {
				t.Fatal(err)
			}
			got := runPAMScript(t, ssh2faPAMScript(), pamFile)
			assertPAM2FA(t, string(orig), got, stack)

			// Ein zweiter Lauf ändert nichts mehr.
			if again := runPAMScript(t, ssh2faPAMScript(), pamFile); again != got {
				t.Errorf("zweiter lauf ändert die datei:\n%s", again)
			}
			// Der Rückweg stellt die Datei Zeichen für Zeichen wieder her.
			if restored := runPAMScript(t, ssh2faPAMRestoreScript(), pamFile); restored != string(orig) {
				t.Errorf("rückweg stellt das original nicht her:\n%s", restored)
			}
		})
	}
}

// assertPAM2FA prüft das Ergebnis: TOTP-Block oben, der Passwort-Stack nur
// noch auskommentiert, account/password/session unangetastet.
func assertPAM2FA(t *testing.T, orig, got, stack string) {
	t.Helper()
	if !strings.HasPrefix(got, "# >>> LCM 2FA >>>\nauth required pam_google_authenticator.so nullok\nauth required pam_permit.so\n") {
		t.Errorf("totp-block steht nicht oben:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	if !slices.Contains(lines, "#LCM-2FA# "+stack) {
		t.Errorf("passwort-stack %q nicht auskommentiert:\n%s", stack, got)
	}
	if slices.Contains(lines, stack) {
		t.Errorf("passwort-stack %q ist weiter aktiv:\n%s", stack, got)
	}
	for _, line := range strings.Split(orig, "\n") {
		if line != stack && !slices.Contains(lines, line) {
			t.Errorf("zeile %q ging verloren oder wurde verändert", line)
		}
	}
}

// TestSSH2FAPackageNames: je Paketverwaltung der richtige Paketname; Alpine
// (apk) ist ehrlich unsupported statt still zu scheitern.
func TestSSH2FAPackageNames(t *testing.T) {
	cases := map[string]string{
		"apt":    "libpam-google-authenticator",
		"pacman": "libpam-google-authenticator",
		"dnf":    "google-authenticator",
		"yum":    "google-authenticator",
		"zypper": "google-authenticator-libpam",
	}
	for mgr, want := range cases {
		got, err := ssh2faPackageName(mgr)
		if err != nil || got != want {
			t.Errorf("%s: erwartet %q, bekam %q (%v)", mgr, want, got, err)
		}
	}
	if _, err := ssh2faPackageName("apk"); !errors.Is(err, ErrSSH2FAUnsupported) {
		t.Errorf("apk sollte unsupported sein, bekam %v", err)
	}
}

// TestSSH2FADropinScript: die drei Stolperfallen des Drop-ins -
//  1. alte Direktiven-Schreibweise (OpenSSH < 8.7 kennt das neue Alias nicht
//     und verweigert mit unbekannter Option den START des Dienstes),
//  2. Match-Ausnahme für den LCM-Service-User (sonst sperrt sich LCM aus),
//  3. `Match all` als Abschluss (sonst fallen alle SPÄTER eingelesenen
//     Konfigurationsdateien in den Match-User-Block).
func TestSSH2FADropinScript(t *testing.T) {
	script := ssh2faDropinScript("lcm-svc")
	for _, want := range []string{
		"ChallengeResponseAuthentication yes",
		"UsePAM yes",
		"AuthenticationMethods publickey,keyboard-interactive:pam",
		"Match User lcm-svc",
		"Match all",
		"sshd -t",
		ssh2faDropinPath,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("drop-in-skript ohne %q", want)
		}
	}
	if strings.Contains(script, "KbdInteractiveAuthentication") {
		t.Error("neues Alias im Drop-in - ältere sshd starten damit nicht mehr")
	}
	// Der Match-Block muss VOR "Match all" stehen (Reihenfolge im Inhalt).
	if strings.Index(script, "Match User lcm-svc") > strings.Index(script, "Match all") {
		t.Error("Match all steht vor der Service-User-Ausnahme")
	}
}

// TestSSH2FADropinSortiertVorHardening: first-match-wins - das 2FA-Drop-in
// muss alphabetisch VOR 60-lcm-hardening.conf liegen, sonst gewinnt dessen
// ChallengeResponseAuthentication no und der Code-Prompt bleibt aus.
func TestSSH2FADropinSortiertVorHardening(t *testing.T) {
	if ssh2faDropinPath >= "/etc/ssh/sshd_config.d/60-lcm-hardening.conf" {
		t.Fatalf("2FA-drop-in %q sortiert nicht vor dem hardening-drop-in", ssh2faDropinPath)
	}
	// RHEL 9 und Klone: 50-redhat.conf setzt ChallengeResponseAuthentication no.
	if ssh2faDropinPath >= "/etc/ssh/sshd_config.d/50-redhat.conf" {
		t.Fatalf("2FA-drop-in %q sortiert nicht vor 50-redhat.conf", ssh2faDropinPath)
	}
	// Ein altes 55-Drop-in räumt das Einrichten ab, der Rückbau ebenso.
	if !strings.Contains(ssh2faDropinScript("lcm-svc"), "rm -f "+ssh2faLegacyDropinPath) {
		t.Error("einrichten räumt das alte drop-in nicht ab")
	}
	if !strings.Contains(ssh2faRollbackScript(), ssh2faLegacyDropinPath) {
		t.Error("rückbau räumt das alte drop-in nicht ab")
	}
}

// TestSSH2FAPAMScript: PAM-Umbau ist idempotent aufgebaut (alten Block erst
// entfernen), legt den Passwort-Stack still und enthält das pam_permit -
// ohne das wäre der Stack für nicht-enrollte Benutzer leer (nullok liefert
// IGNORE) und die Anmeldung schlüge fehl.
func TestSSH2FAPAMScript(t *testing.T) {
	script := ssh2faPAMScript()
	for _, want := range []string{
		"pam_google_authenticator.so nullok",
		"pam_permit.so",
		"# >>> LCM 2FA >>>",
		"#LCM-2FA# ",
		"common-auth",
		"password-auth",
		"lcm-backup",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("pam-skript ohne %q", want)
		}
	}
	// Rückweg nimmt Block UND Kommentierungen zurück.
	restore := ssh2faPAMRestoreScript()
	for _, want := range []string{"LCM 2FA >>>", "s/^#LCM-2FA# //"} {
		if !strings.Contains(restore, want) {
			t.Errorf("pam-restore ohne %q", want)
		}
	}
}

// TestSSH2FAUninstallOhnePaketquelle: auch wenn die Paketverwaltung kein
// 2FA-Paket kennt, muss der Rückbau der Konfiguration möglich sein.
func TestSSH2FAUninstallOhnePaketquelle(t *testing.T) {
	script := ssh2faUninstallScript("apk")
	if !strings.Contains(script, "rm -f "+ssh2faDropinPath) {
		t.Error("uninstall räumt das drop-in nicht ab")
	}
	if strings.Contains(script, "apk del") {
		t.Error("uninstall versucht paketentfernung auf nicht unterstützter paketverwaltung")
	}
}
