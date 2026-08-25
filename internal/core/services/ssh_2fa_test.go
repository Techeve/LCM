package services

import (
	"errors"
	"strings"
	"testing"
)

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
