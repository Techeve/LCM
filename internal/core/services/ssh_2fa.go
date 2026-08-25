package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// SSH-2FA: TOTP als zweiter Faktor NEBEN dem SSH-Key, umgesetzt mit
// google-authenticator-libpam - dem meistgenutzten und in allen relevanten
// Distributionen paketierten PAM-Modul für TOTP (RFC 6238, funktioniert mit
// jeder Authenticator-App).
//
// Wirkung: sshd verlangt publickey UND keyboard-interactive:pam; den
// PAM-Teil beantwortet ausschließlich pam_google_authenticator (der
// Passwort-Stack ist im auth-Bereich stillgelegt - sonst käme nach dem Code
// auch noch eine Passwortabfrage). `nullok` hält den Rollout sanft: Benutzer
// OHNE eingerichtetes TOTP kommen weiter mit ihrem Key herein; wer enrolled
// ist, braucht den Code. Das Enrollment macht jeder Benutzer selbst auf dem
// Server (`google-authenticator`) - die Benutzer-Übersicht zeigt, wer schon
// so weit ist.
//
// Der LCM-Service-User ist per Match-Block ausgenommen (reine Key-Auth):
// LCMs SSH-Client beantwortet keine keyboard-interactive-Abfragen - ohne
// die Ausnahme sperrte sich LCM mit dem Aktivieren selbst aus. Zusätzlich
// beweist eine frische Verbindung NACH dem Umbau, dass der Zugang noch
// trägt; scheitert sie, wird alles zurückgerollt (fail-closed).

// ssh2faDropinPath: alphabetisch VOR 60-lcm-hardening.conf - OpenSSH nimmt
// je Option den ZUERST gefundenen Wert, und das Härtungs-Drop-in setzt
// ChallengeResponseAuthentication no.
const ssh2faDropinPath = "/etc/ssh/sshd_config.d/55-lcm-2fa.conf"

const pamSSHDPath = "/etc/pam.d/sshd"

// ErrSSH2FAUnsupported: keine Paketquelle für das PAM-Modul (z.B. Alpine,
// dessen sshd standardmäßig ohne PAM gebaut ist).
var ErrSSH2FAUnsupported = errors.New("SSH-2FA wird auf dieser Paketverwaltung nicht unterstützt")

// ssh2faPackageName liefert den Paketnamen des PAM-Moduls je Paketverwaltung.
func ssh2faPackageName(mgr string) (string, error) {
	switch pkgFamily(mgr) {
	case pkgApt, pkgPacman:
		return "libpam-google-authenticator", nil
	case pkgDnf:
		return "google-authenticator", nil // Fedora direkt, RHEL-Klone via EPEL
	case pkgZypper:
		return "google-authenticator-libpam", nil
	default:
		return "", ErrSSH2FAUnsupported
	}
}

// ssh2faPAMScript legt den Passwort-Stack im auth-Bereich von /etc/pam.d/sshd
// still und setzt den TOTP-Block ganz nach oben. Idempotent: ein vorhandener
// LCM-Block wird erst entfernt, alte Kommentierungen werden zurückgenommen.
// Das pam_permit am Blockende ist Absicht: mit nullok liefert
// pam_google_authenticator für nicht-enrollte Benutzer IGNORE - bestünde der
// Stack nur aus diesem Modul, wäre das Ergebnis undefiniert und die
// Anmeldung schlüge fehl.
//
// Zurückgeschrieben wird über `cat >` (nicht mv) - erhält Eigentümer, Rechte
// und SELinux-Kontext der Originaldatei.
func ssh2faPAMScript() string {
	// Die ||-Guards stehen in { }-Gruppen: ohne sie verketteten sich die
	// ||-Zweige mit den umliegenden &&-Schritten (R2-014 - dieselbe Falle
	// wie beim sshd-Reload).
	return strings.Join([]string{
		fmt.Sprintf(`{ [ -f %s ] || { echo "PAM-Datei %s fehlt" >&2; exit 1; }; }`, pamSSHDPath, pamSSHDPath),
		fmt.Sprintf(`{ [ -f %s.lcm-backup ] || cp -p %s %s.lcm-backup; }`, pamSSHDPath, pamSSHDPath, pamSSHDPath),
		fmt.Sprintf(`sed -i '/^# >>> LCM 2FA >>>/,/^# <<< LCM 2FA <<</d' %s`, pamSSHDPath),
		fmt.Sprintf(`sed -i 's/^#LCM-2FA# //' %s`, pamSSHDPath),
		// Debian: "@include common-auth" - RHEL: "auth substack password-auth"
		// - openSUSE: "auth include common-auth".
		fmt.Sprintf(`sed -i -E 's/^(@include[[:space:]]+common-auth)/#LCM-2FA# \1/; s/^(auth[[:space:]]+(include|substack)[[:space:]]+(common-auth|system-auth|password-auth))/#LCM-2FA# \1/' %s`, pamSSHDPath),
		`PAMTMP=$(mktemp)`,
		strings.Join([]string{
			`{ echo '# >>> LCM 2FA >>>'`,
			`echo 'auth required pam_google_authenticator.so nullok'`,
			`echo 'auth required pam_permit.so'`,
			`echo '# <<< LCM 2FA <<<'`,
			fmt.Sprintf(`cat %s; } > "$PAMTMP"`, pamSSHDPath),
		}, "; "),
		fmt.Sprintf(`cat "$PAMTMP" > %s`, pamSSHDPath),
		`rm -f "$PAMTMP"`,
	}, " && ")
}

// ssh2faPAMRestoreScript nimmt die PAM-Änderungen zurück.
func ssh2faPAMRestoreScript() string {
	return strings.Join([]string{
		fmt.Sprintf(`if [ -f %s ]; then sed -i '/^# >>> LCM 2FA >>>/,/^# <<< LCM 2FA <<</d' %s; sed -i 's/^#LCM-2FA# //' %s; fi`,
			pamSSHDPath, pamSSHDPath, pamSSHDPath),
	}, " && ")
}

// ssh2faDropinScript schreibt das sshd-Drop-in: Key + TOTP als Pflicht,
// der LCM-Service-User bleibt bei reiner Key-Auth.
//
// Bewusst die alte Schreibweise ChallengeResponseAuthentication (nicht das
// Alias KbdInteractiveAuthentication, das erst OpenSSH 8.7 kennt - ältere
// sshd verweigerten mit unbekannter Option den Start). `Match all` am Ende
// ist Pflicht: ohne die Zeile fielen alle später eingelesenen Drop-ins und
// die Hauptdatei in den Match-User-Block.
func ssh2faDropinScript(serviceUser string) string {
	content := strings.Join([]string{
		"# von LCM verwaltet: SSH-2FA (TOTP) - Anmeldung mit Key + Einmalcode",
		"UsePAM yes",
		"ChallengeResponseAuthentication yes",
		"AuthenticationMethods publickey,keyboard-interactive:pam",
		"# LCM-Service-User ausgenommen (maschineller Zugang, reine Key-Auth)",
		"Match User " + serviceUser,
		"    AuthenticationMethods publickey",
		"Match all",
		"",
	}, "\n")
	return strings.Join([]string{
		sshdEnsureIncludeScript,
		"install -d -m 755 /etc/ssh/sshd_config.d",
		"printf '%s' " + shellQuote(content) + " > " + ssh2faDropinPath,
		"sshd -t",
		sshdReloadScript,
	}, " && ")
}

// ssh2faInstallScript: Paket installieren (mit Nachweis - pkgInstallScript
// meldet fehlende Pakete nur, bricht aber nicht ab), PAM umbauen, Drop-in
// schreiben, sshd prüfen und neu laden.
func ssh2faInstallScript(mgr, serviceUser string) (string, error) {
	pkg, err := ssh2faPackageName(mgr)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		pkgInstallScript(mgr, []string{pkg}),
		`command -v google-authenticator >/dev/null 2>&1 || { echo "das Paket ` + pkg + ` konnte nicht installiert werden (Repo fehlt? RHEL braucht EPEL)" >&2; exit 1; }`,
		ssh2faPAMScript(),
		ssh2faDropinScript(serviceUser),
		`echo "LCM: SSH-2FA aktiv - Benutzer richten ihr TOTP selbst mit google-authenticator ein"`,
	}, " && "), nil
}

// ssh2faRollbackScript nimmt alles zurück (Drop-in weg, PAM wiederhergestellt,
// sshd neu geladen). Das Paket bleibt beim Rollback stehen - es stört nicht
// und der nächste Versuch spart den Download.
func ssh2faRollbackScript() string {
	return strings.Join([]string{
		"rm -f " + ssh2faDropinPath,
		ssh2faPAMRestoreScript(),
		"sshd -t",
		sshdReloadScript,
	}, " && ")
}

// ssh2faUninstallScript baut die Funktion vollständig zurück; die
// TOTP-Secrets der Benutzer (~/.google_authenticator) bleiben liegen -
// beim erneuten Aktivieren gelten sie sofort wieder.
func ssh2faUninstallScript(mgr string) string {
	pkg, err := ssh2faPackageName(mgr)
	steps := []string{ssh2faRollbackScript()}
	if err == nil {
		steps = append(steps, pkgRemovePackagesScript(mgr, []string{pkg})+" 2>/dev/null || true")
	}
	steps = append(steps, `echo "LCM: SSH-2FA entfernt - vorhandene TOTP-Secrets der Benutzer bleiben unangetastet"`)
	return strings.Join(steps, " && ")
}

// ConfigureSSH2FA aktiviert bzw. deaktiviert SSH-2FA auf dem Server -
// asynchron als Job (Paketinstallation dauert).
func (s *ServerService) ConfigureSSH2FA(scope repositories.AccessScope, id uint, enable bool, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	if err := ensureFullSudo(server); err != nil {
		return nil, err
	}
	if enable {
		if _, err := ssh2faPackageName(server.PackageManager); err != nil {
			return nil, err
		}
	}
	title := "SSH-2FA aktivieren @ " + server.Name
	if !enable {
		title = "SSH-2FA entfernen @ " + server.Name
	}
	job, err := s.jobs.Start(&server.ID, nil, domain.RuleTypeScript, title, actor)
	if err != nil {
		return nil, err
	}
	s.audit.Log(actor, "server.ssh-2fa", "server", id, fmt.Sprintf("%s: enable=%t", server.Name, enable))
	safego.GoCleanup("job:ssh-2fa", jobPanicCleanup(s.jobs, job), func() {
		s.runSSH2FAJob(job, server, enable, actor)
	})
	return job, nil
}

func (s *ServerService) runSSH2FAJob(job *domain.Job, server *domain.Server, enable bool, actor string) {
	if server.IsDemo {
		s.jobs.Complete(job, "Demo-Server - SSH-2FA simuliert (kein SSH-Kontakt).", ptrInt(0), nil)
		_ = s.servers.UpdateFields(server.ID, map[string]any{"ssh_2fa_enabled": enable})
		return
	}
	conn, err := s.connectRec(server, "ssh-2fa", actor)
	if err != nil {
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	defer conn.Close()

	script := ssh2faUninstallScript(server.PackageManager)
	if enable {
		script, err = ssh2faInstallScript(server.PackageManager, server.ServiceUser)
		if err != nil {
			s.jobs.Complete(job, "", nil, err)
			return
		}
	}
	output, code, runErr := conn.Run(privRun(server, script))
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("ssh-2fa endete mit exit-code %d", code)
	}
	if runErr != nil {
		s.jobs.Complete(job, output, ptrInt(code), runErr)
		return
	}

	if enable {
		// Aussperr-Probe: eine FRISCHE Verbindung muss gelingen, solange die
		// bestehende noch offen ist. Scheitert sie, hat die Match-Ausnahme
		// nicht gegriffen - dann wird sofort zurückgerollt statt LCM (und
		// damit jede Verwaltung dieses Servers) auszusperren.
		//
		// Über den LESE-Slot: die Job-Verbindung hält den Exec-Slot des
		// ConnLimiters, eine zweite Exec-Verbindung ist also nie zu
		// bekommen - die Probe schlüge sonst IMMER fehl und rollte jede
		// Aktivierung zurück (im Langzeittest genau so aufgetreten).
		if probe, probeErr := s.connectRecRead(server, "ssh-2fa-verify", actor); probeErr != nil {
			rbOut, _, _ := conn.Run(privRun(server, ssh2faRollbackScript()))
			s.jobs.Complete(job, output+"\n\nROLLBACK:\n"+rbOut, ptrInt(1),
				fmt.Errorf("aussperr-probe fehlgeschlagen (%v) - SSH-2FA wurde zurückgerollt", probeErr))
			return
		} else {
			probe.Close()
		}
	}

	_ = s.servers.UpdateFields(server.ID, map[string]any{"ssh_2fa_enabled": enable})
	s.jobs.Complete(job, output, ptrInt(0), nil)
}
