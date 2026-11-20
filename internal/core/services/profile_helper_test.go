package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestHelperProfilKommandosSchreibenKeinenFreitext: Im eingeschränkten Modus
// hat der Service-User keine Root-Shell. Die Profile laufen deshalb über den
// Helper - und der bekommt Slug und Inhalt als getrennte, geprüfte Parameter,
// nicht als zusammengebautes Shell-Kommando.
func TestHelperProfilKommandosSchreibenKeinenFreitext(t *testing.T) {
	profile := &domain.PrivilegeProfile{
		ID: 1, Slug: "webserver", Name: "Web",
		SudoRules: []domain.ProfileSudoRule{{Command: "/usr/bin/systemctl --no-pager restart nginx"}},
	}
	cmds := helperProfileApplyCmds([]*domain.PrivilegeProfile{profile})
	if len(cmds) != 3 {
		t.Fatalf("erwartet anwenden + aufräumen + sftp-drop-in, bekam %d: %v", len(cmds), cmds)
	}
	if !strings.Contains(cmds[0], "profile-apply 'webserver'") {
		t.Errorf("anwenden fehlt: %s", cmds[0])
	}
	// Der sudoers-Inhalt geht base64-kodiert - so gibt es über die
	// verschachtelten sudo/sh-Ebenen keine Quoting-Fallen.
	if strings.Contains(cmds[0], "systemctl") {
		t.Errorf("der inhalt darf nicht im klartext im kommando stehen: %s", cmds[0])
	}
	if !strings.Contains(cmds[1], "profile-prune 'webserver'") {
		t.Errorf("aufräumen nennt das behaltene profil nicht: %s", cmds[1])
	}
}

// TestHelperAufraeumenOhneProfile: Bleibt kein Profil übrig, muss der Helper
// trotzdem aufräumen - sonst behielte der Server Gruppe und sudoers-Datei
// eines längst abgezogenen Profils.
func TestHelperAufraeumenOhneProfile(t *testing.T) {
	cmds := helperProfileApplyCmds(nil)
	if len(cmds) != 2 || !strings.Contains(cmds[0], "profile-prune '-'") {
		t.Fatalf("erwartet aufräumen und sftp-drop-in, bekam %v", cmds)
	}
}

// TestHelperSFTPKontenOhneShell: Im eingeschränkten Modus legte der Helper
// SFTP-Konten mit /bin/bash und ohne ForceCommand an - „nur Dateizugriff"
// hieß dort volle Shell. Jetzt schreibt der Helper dasselbe Drop-in wie der
// Voll-Modus, und das Konto bekommt nologin.
func TestHelperSFTPKontenOhneShell(t *testing.T) {
	cmds := helperProfileApplyCmds([]*domain.PrivilegeProfile{sftpProfile(), {Slug: "web"}})
	if last := cmds[len(cmds)-1]; last != "lcm-helper profile-sftp 'datenpflege'" {
		t.Errorf("drop-in nennt nicht genau das sftp-profil: %s", last)
	}
	// Ohne SFTP-Profil nur aufräumen, wenn die Datei liegt - ein älterer
	// Helper kennt das Kommando nicht.
	if got := helperProfileSFTPCmd(nil); got != "[ ! -e "+profileSSHDPath+" ] || lcm-helper profile-sftp '-'" {
		t.Errorf("aufräumen ohne sftp-profil: %s", got)
	}
	effect := effectFor(&domain.LinuxUser{Username: "anna"}, sftpProfile())
	if got := helperUserEnsureCmd(&domain.LinuxUser{Username: "anna", Shell: "/bin/bash"}, "", effect); !strings.Contains(got, "user-ensure 'anna' 'nologin'") {
		t.Errorf("sftp-konto behält die shell: %s", got)
	}
}

// TestHelperSFTPDropinWieImVollModus: Der Helper baut das Drop-in selbst aus
// den Slugs. Bis auf die Kommentare muss es dem des Voll-Modus gleichen.
func TestHelperSFTPDropinWieImVollModus(t *testing.T) {
	second := sftpProfile()
	second.Slug = "upload"
	want := profileSSHDContent([]*domain.PrivilegeProfile{sftpProfile(), second})
	ok, got := helperFunktionAufrufen(t, `profile_sftp_content "$1"`, "datenpflege,upload")
	if !ok {
		t.Fatalf("profile_sftp_content: %s", got)
	}
	if ohneKommentare(got) != ohneKommentare(want) {
		t.Errorf("helper:\n%s\nvoll-modus:\n%s", got, want)
	}
	if ok, got := helperFunktionAufrufen(t, `profile_sftp_content -`); !ok || got != "" {
		t.Errorf("ohne sftp-profil muss das drop-in leer sein: %q", got)
	}
}

func ohneKommentare(s string) string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// TestHelperNologinPfad: nologin liegt je Distribution woanders, auf BusyBox
// gibt es nur /bin/false.
func TestHelperNologinPfad(t *testing.T) {
	ok, got := helperFunktionAufrufen(t, "nologin_shell")
	if !ok || !strings.HasPrefix(got, "/") {
		t.Errorf("kein nologin-pfad: %q", got)
	}
}

// TestHelperMitgliedschaft: Auch im eingeschränkten Modus gehört ein Konto in
// genau eine Profilgruppe.
func TestHelperMitgliedschaft(t *testing.T) {
	if got := helperProfileMemberCmd("anna", "webserver"); !strings.Contains(got, "profile-member 'anna' 'webserver'") {
		t.Errorf("mitgliedschaft falsch: %s", got)
	}
	// Ohne Profil wird die Mitgliedschaft gelöst - der Helper erwartet dafür
	// den Platzhalter.
	if got := helperProfileMemberCmd("anna", ""); !strings.Contains(got, "profile-member 'anna' '-'") {
		t.Errorf("lösen der mitgliedschaft falsch: %s", got)
	}
}

// TestHelperPruefungImSkript: Der Helper übernimmt den sudoers-Inhalt nicht
// blind. Ohne diese Prüfung könnte ein kompromittiertes LCM dem
// eingeschränkten Service-User über eine Profil-Datei volle Rechte
// zurückgeben - genau das, was der eingeschränkte Modus verhindern soll.
func TestHelperPruefungImSkript(t *testing.T) {
	for _, marker := range []string{
		"fremde gruppe in profil-regel",
		"uneingeschraenkte regel abgelehnt",
		"visudo -cf",
	} {
		if !strings.Contains(lcmHelperScript, marker) {
			t.Errorf("prüfung fehlt im helper: %s", marker)
		}
	}
	// Die neuen Unterkommandos müssen im Dispatcher stehen, sonst sind sie
	// unerreichbar.
	for _, cmd := range []string{"profile-apply)", "profile-member)", "profile-prune)"} {
		if !strings.Contains(lcmHelperScript, cmd) {
			t.Errorf("unterkommando fehlt im dispatcher: %s", cmd)
		}
	}
}

// helperDefinitionen liefert den Helper ohne den Aufruf am Ende - nur
// Variablen und Funktionen, damit ein Test eine einzelne Prüfung aufrufen kann.
func helperDefinitionen(t *testing.T) string {
	t.Helper()
	end := strings.Index(lcmHelperScript, "\ncmd=\"${1:-}\"")
	if end < 0 {
		t.Fatal("aufruf am ende des helpers nicht gefunden")
	}
	return lcmHelperScript[:end]
}

// helperFunktionAufrufen ruft eine Funktion des Helpers mit Argumenten auf.
func helperFunktionAufrufen(t *testing.T, call string, args ...string) (bool, string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	script := helperDefinitionen(t) + "\n" + call + "\n"
	out, err := exec.Command(sh, append([]string{"-c", script, "sh"}, args...)...).CombinedOutput()
	return err == nil, string(out)
}

// profilRegelGeprueft ruft profile_rule_ok des Helpers für eine Zeile auf.
func profilRegelGeprueft(t *testing.T, group, line string) (bool, string) {
	t.Helper()
	return helperFunktionAufrufen(t, `profile_rule_ok "$1" "$2"`, group, line)
}

// TestHelperLehntRootRegelnAb: profile-apply nimmt keine Regel an, die Root
// bedeutet - ALL in jeder Schreibweise, Shells und Programme, die beliebige
// Kommandos starten. Sonst gäbe ein kompromittiertes LCM dem eingeschränkten
// Modus über eine Profil-Datei die Root-Shell zurück.
func TestHelperLehntRootRegelnAb(t *testing.T) {
	const g = "%lcm-prof-web ALL="
	// Gegenprobe: Eine harmlose Regel kommt durch - die Ablehnungen unten
	// gelten der Regel, nicht einer fehlenden Prüfung.
	if ok, out := profilRegelGeprueft(t, "lcm-prof-web", g+"(root) NOPASSWD: /usr/bin/id"); !ok {
		t.Fatalf("harmlose regel abgelehnt:\n%s", out)
	}
	for _, line := range []string{
		g + "(ALL) NOPASSWD: /usr/bin/systemctl --no-pager restart nginx",
		g + "(ALL:ALL) NOPASSWD: /usr/bin/id",
		g + "(root:ALL) NOPASSWD: /usr/bin/id",
		g + "(root) NOPASSWD: ALL",
		g + "(root) NOPASSWD:ALL",
		g + "(root) NOPASSWD:\tALL",
		g + "(root)   NOPASSWD:   ALL  ",
		g + "(root) PASSWD: ALL",
		g + "(root) ALL",
		g + "(root) NOPASSWD: /usr/bin/id, ALL",
		g + "(root) NOPASSWD: /usr/bin/id,/bin/sh",
		g + "(root) NOPASSWD: !/usr/bin/id",
		// Kleingeschrieben ist all kein Schlüsselwort - als Kommando aber
		// ohne absoluten Pfad und damit ebenso abgelehnt.
		g + "(root) NOPASSWD: all",
		g + "(root) NOPASSWD: /bin/sh",
		g + "(root) NOPASSWD: /bin/bash",
		g + "(root) NOPASSWD: /usr/bin/dash",
		g + "(root) NOPASSWD: /usr/bin/zsh",
		g + "(root) NOPASSWD: /bin/ksh",
		g + "(root) NOPASSWD: /usr/bin/fish",
		g + "(root) NOPASSWD: /bin/su -",
		g + "(root) NOPASSWD: /usr/bin/sudo -i",
		g + "(root) NOPASSWD: /usr/bin/env id",
		g + "(root) NOPASSWD: /usr/bin/python3 /opt/x.py",
		g + "(root) NOPASSWD: /usr/bin/python3.11 /opt/x.py",
		g + "(root) NOPASSWD: /usr/bin/perl5.36.0 /opt/x.pl",
		g + "(root) NOPASSWD: /usr/bin/vim.basic /etc/hosts",
		g + "(root) NOPASSWD: /usr/bin/lua5.4 /opt/x.lua",
		g + "(root) NOPASSWD: /bin/rbash",
		g + "(root) NOPASSWD: /usr/bin/find /srv -exec id",
		g + "(root) SETENV: NOPASSWD: /usr/bin/id",
		g + "(root) NOPASSWD: /usr/bin/id \\",
		g + "(root) NOPASSWD: /usr/bin/apt-get install *",
		g + "(root) NOPASSWD: sudoedit",
		"%lcm-prof-andere ALL=(root) NOPASSWD: /usr/bin/id",
		"%lcm-prof-web\tALL=(root) NOPASSWD: /usr/bin/id",
		"Defaults env_keep+=LD_PRELOAD",
	} {
		if ok, out := profilRegelGeprueft(t, "lcm-prof-web", line); ok {
			t.Errorf("angenommen: %q\n%s", line, out)
		}
	}
}

// TestHelperNimmtLCMsEigeneRegelnAn: Was profileSudoersContent schreibt, muss
// die Prüfung des Helpers bestehen - sonst scheiterte jeder Abgleich im
// eingeschränkten Modus an LCMs eigener Ausgabe.
func TestHelperNimmtLCMsEigeneRegelnAn(t *testing.T) {
	profile := &domain.PrivilegeProfile{
		Slug: "web", Name: "Web",
		SudoRules: []domain.ProfileSudoRule{
			{Command: "/usr/bin/systemctl --no-pager restart nginx"},
			{Command: "/usr/bin/journalctl --no-pager -u nginx -n 200", RunAs: "www-data", RequirePassword: true},
			{Command: "/usr/sbin/nginx -t"},
		},
		EditRules: []domain.ProfileEditRule{{Path: "/etc/nginx/nginx.conf"}},
	}
	for _, line := range strings.Split(strings.TrimSpace(profileSudoersContent(profile)), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if ok, out := profilRegelGeprueft(t, "lcm-prof-web", line); !ok {
			t.Errorf("abgelehnt: %q\n%s", line, out)
		}
	}
}

// TestHelperProfilDateiOhneInclude: Kommentar ist nur "#" mit Leerzeichen.
// "#include" liest eine fremde Datei ein - etwa eine aus dem Home des
// Service-Users -, "#1000" ist für sudo eine Benutzer-ID.
func TestHelperProfilDateiOhneInclude(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	helper := helperTestScript(t)
	for _, body := range []string{
		"#include /home/lcm-svc/regeln\n",
		"#includedir /tmp\n",
		"@include /tmp/regeln\n",
		"#1000 ALL=(ALL) NOPASSWD: ALL\n",
		"# LCM\n%lcm-prof-web ALL=(root) NOPASSWD: /bin/sh\n",
	} {
		out, err := exec.Command(sh, helper, "profile-apply", "web", helperB64(body)).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "profil-regeln abgelehnt") {
			t.Errorf("angenommen: %q\n%s", body, out)
		}
	}
}

// TestHelperRepoOhneTrusted: repo-add schaltet die Signaturprüfung nicht ab -
// weder mit trusted= in der Einzeilen-Form noch mit Trusted: (deb822) oder
// allow-insecure.
func TestHelperRepoOhneTrusted(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	helper := helperTestScript(t)
	keyURL := helperB64("https://repo.example/key.asc")
	for _, line := range []string{
		"deb [trusted=yes] https://repo.example/debian stable main",
		"deb [arch=amd64 Trusted = yes] https://repo.example/debian stable main",
		"deb [TRUSTED=yes] https://repo.example/debian stable main",
		"deb https://repo.example/debian stable main Trusted: yes",
		"deb [allow-insecure=yes] https://repo.example/debian stable main",
		"deb [allow-weak=yes] https://repo.example/debian stable main",
	} {
		out, err := exec.Command(sh, helper, "repo-add", "beispiel", keyURL, helperB64(line)).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "apt-zeile abgelehnt") {
			t.Errorf("angenommen: %q\n%s", line, out)
		}
	}
}

// TestRootRegelnFallenImEingeschraenktenModusWeg: Eine bestätigte
// Root-gleichwertige Regel gilt im Voll-Modus, der Helper lehnt sie ab. Sie
// fällt deshalb vorher sichtbar weg, statt den ganzen Abgleich scheitern zu
// lassen; die übrigen Regeln bleiben.
func TestRootRegelnFallenImEingeschraenktenModusWeg(t *testing.T) {
	profile := &domain.PrivilegeProfile{Slug: "web", SudoRules: []domain.ProfileSudoRule{
		{Command: "/usr/bin/systemctl --no-pager restart nginx"},
		{Command: "/usr/bin/vim /etc/nginx/nginx.conf", AllowRootEquivalent: true},
	}}
	kept, notes := helperAcceptedRules(profile)
	if len(kept.SudoRules) != 1 || kept.SudoRules[0].Command != "/usr/bin/systemctl --no-pager restart nginx" {
		t.Errorf("falsche regeln behalten: %+v", kept.SudoRules)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "vim") {
		t.Errorf("wegfall nicht gemeldet: %v", notes)
	}
	if len(profile.SudoRules) != 2 {
		t.Error("das profil selbst darf nicht verändert werden")
	}
}

// TestHelperSperrtDieselbenPfadeWieDieDomaene: Der Helper wiederholt die
// Pfadprüfung auf dem Zielsystem. Er urteilt über jeden Pfad wie LCM beim
// Speichern - sonst lehnte er eine gespeicherte Regel ab oder ließe eine
// gesperrte durch.
func TestHelperSperrtDieselbenPfadeWieDieDomaene(t *testing.T) {
	paths := []string{
		"/", "/etc", "/etc/nginx", "/etc/systemd", "/etc/systemdx", "/etc/systemd/system",
		"/etc/crontab", "/etc/cron.d/job", "/var/spool/cron", "/var/spool/mail", "/var", "/var/lib",
		"/var/lib/lcm", "/var/www", "/home", "/home/anna", "/home/" + domain.DefaultServiceUser + "/.ssh",
		"/etc/ld.so.preload", "/etc/profile.d/x.sh", "/etc/environment", "/etc/sudoers.d/x",
		"/etc/sudo.conf", "/etc/pam.d", "/usr/local/bin", "/srv/www", "/root", "/run", "/run/systemd",
		"/etc/ssh", "/etc/apt/sources.list.d", "/etc/apt/apt.conf.d",
	}
	for _, p := range paths {
		for _, mode := range []string{"edit", domain.PathModeRead, domain.PathModeReadWrite, domain.PathModeDeny} {
			var goErr error
			if mode == "edit" {
				goErr = domain.ValidateEditPath(p)
			} else {
				goErr = domain.ValidatePathRule(p, mode)
			}
			ok, out := helperFunktionAufrufen(t, `rule_path_ok "$1" "$2"`, p, mode)
			if ok != (goErr == nil) {
				t.Errorf("%s (%s): helper ok=%v, domäne %v\n%s", p, mode, ok, goErr, out)
			}
		}
	}
	// Nur der Helper sieht die kurze Form: LCM speichert bereinigt.
	for _, p := range []string{"/etc/nginx/", "/etc//nginx", "/srv/./www", "/srv/../etc", "srv/www", "/srv/my dir"} {
		if ok, _ := helperFunktionAufrufen(t, `rule_path_ok "$1" read`, p); ok {
			t.Errorf("%q angenommen", p)
		}
	}
}

// TestHelperPrueftSudoeditPfade: Eine sudoedit-Regel auf eine gesperrte Datei
// lehnt der Helper ab - sudoedit auf /etc/cron.d schreibt als root.
func TestHelperPrueftSudoeditPfade(t *testing.T) {
	const g = "%lcm-prof-web ALL=(root) NOPASSWD: sudoedit "
	for _, p := range []string{"/etc/cron.d/job", "/etc/systemd/system/x.service", "/etc/sudoers", "/etc/../etc/crontab"} {
		if ok, out := profilRegelGeprueft(t, "lcm-prof-web", g+p); ok {
			t.Errorf("sudoedit %s angenommen\n%s", p, out)
		}
	}
	if ok, out := profilRegelGeprueft(t, "lcm-prof-web", g+"/etc/nginx/nginx.conf"); !ok {
		t.Errorf("sudoedit auf eine freie datei abgelehnt\n%s", out)
	}
}

// TestHelperNimmtAltesRechtAuchAufGesperrtemPfadZurueck: Ein vor der
// Verschärfung gesetztes Recht auf /etc/systemd muss sich zurücknehmen lassen -
// genau dort soll es verschwinden. Nur das Setzen ist gesperrt.
func TestHelperNimmtAltesRechtAuchAufGesperrtemPfadZurueck(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	helper := helperTestScript(t)
	out, err := exec.Command(sh, helper, "profile-acl", "remove", "web", "-", "/etc/systemd").CombinedOutput()
	if err != nil || strings.Contains(string(out), "abgelehnt") {
		t.Errorf("zurücknehmen abgelehnt: %v\n%s", err, out)
	}
	out, err = exec.Command(sh, helper, "profile-acl", "set", "web", "read", "/etc/systemd").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "verzeichnisrecht abgelehnt") {
		t.Errorf("setzen angenommen: %v\n%s", err, out)
	}
}

// TestHelperSperreLaeuftAbUndVerlaesstProfilgruppen: user-lock setzt im
// eingeschränkten Modus dasselbe wie der Voll-Modus - Ablaufdatum und
// Austritt aus den Profilgruppen -, user-ensure hebt LCMs Ablaufdatum auf.
func TestHelperSperreLaeuftAbUndVerlaesstProfilgruppen(t *testing.T) {
	lock := helperFunktion(t, "user_lock")
	for _, must := range []string{`usermod -e 1 "$name"`, `leave_profile_groups "$name"`} {
		if !strings.Contains(lock, must) {
			t.Errorf("user-lock ohne %s:\n%s", must, lock)
		}
	}
	if ensure := helperFunktion(t, "user_ensure"); !strings.Contains(ensure, `chage -E -1 "$name"`) {
		t.Errorf("user-ensure hebt das ablaufdatum nicht auf:\n%s", ensure)
	}
}

// TestHelperVerlaesstNurProfilgruppen: Das Konto verlässt alle
// lcm-prof-Gruppen außer der behaltenen - fremde Gruppen fasst LCM nicht an.
func TestHelperVerlaesstNurProfilgruppen(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "log")
	for name, body := range map[string]string{
		"id":      "#!/bin/sh\necho 'anna lcm-prof-web lcm-prof-db users'\n",
		"gpasswd": "#!/bin/sh\necho \"$*\" >> " + log + "\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := helperDefinitionen(t) + "\nleave_profile_groups anna lcm-prof-db\n"
	cmd := exec.Command(sh, "-c", script)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, _ := os.ReadFile(log)
	if strings.TrimSpace(string(got)) != "-d anna lcm-prof-web" {
		t.Errorf("ausgetreten: %q", got)
	}
}

// TestHelperKenntDieRootListeDerDomaene: Der Helper prüft gegen dieselben
// Programme wie LCM beim Speichern.
func TestHelperKenntDieRootListeDerDomaene(t *testing.T) {
	for _, name := range append(domain.RootEquivalentBinaries(), domain.EscapeArguments()...) {
		if !strings.Contains(lcmHelperScript, " "+name+" ") {
			t.Errorf("%s fehlt im helper", name)
		}
	}
	for _, name := range []string{"sh", "bash", "dash", "zsh", "ksh", "fish", "su", "sudo", "env"} {
		if !slices.Contains(domain.RootEquivalentBinaries(), name) {
			t.Errorf("%s fehlt in der liste", name)
		}
	}
}
