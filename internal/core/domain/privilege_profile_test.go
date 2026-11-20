package domain

import (
	"errors"
	"strings"
	"testing"
)

// TestSudoKommandoBrauchtAbsolutenPfad: Ohne absoluten Pfad entscheidet der
// Suchpfad des Benutzers, welches Programm als root läuft.
func TestSudoKommandoBrauchtAbsolutenPfad(t *testing.T) {
	if err := ValidateSudoCommand("systemctl restart nginx", false); !errors.Is(err, ErrSudoCommandRelative) {
		t.Errorf("relativer pfad muss abgewiesen werden, bekam %v", err)
	}
	if err := ValidateSudoCommand("/usr/bin/systemctl --no-pager restart nginx", false); err != nil {
		t.Errorf("gültiges kommando abgewiesen: %v", err)
	}
}

// TestSudoKommandoOhneArgumenteIstVollzugriff: Genau hier kippen sudo-Regeln
// in der Praxis - „/usr/bin/systemctl" erlaubt auch `systemctl edit`, und das
// öffnet einen Editor als root.
func TestSudoKommandoOhneArgumenteIstVollzugriff(t *testing.T) {
	for _, cmd := range []string{"/usr/bin/systemctl", "/usr/bin/apt-get", "/usr/bin/docker"} {
		if err := ValidateSudoCommand(cmd, false); !errors.Is(err, ErrSudoCommandNoArgs) {
			t.Errorf("%s ohne argumente muss abgewiesen werden, bekam %v", cmd, err)
		}
	}
	// Mit festen Argumenten ist dasselbe Programm in Ordnung.
	if err := ValidateSudoCommand("/usr/bin/apt-get update", false); err != nil {
		t.Errorf("apt-get mit festem argument abgewiesen: %v", err)
	}
	// Das von LCM selbst ergänzte --no-pager darf die Prüfung nicht
	// aushebeln: `systemctl --no-pager` erlaubt weiterhin jede Unteraktion.
	if err := ValidateSudoCommand(NormalizeSudoCommand("/usr/bin/systemctl"), false); !errors.Is(err, ErrSudoCommandNoArgs) {
		t.Errorf("systemctl --no-pager ohne unteraktion muss abgewiesen werden, bekam %v", err)
	}
}

// TestSudoKommandoOhnePlatzhalter: Ein Stern öffnet die Regel für alles, was
// darauf passt - „apt-get install *" erlaubt jedes Paket, und Paketskripte
// laufen als root.
func TestSudoKommandoOhnePlatzhalter(t *testing.T) {
	for _, cmd := range []string{
		"/usr/bin/apt-get install *",
		"/usr/bin/systemctl restart nginx?",
		"/usr/bin/systemctl restart [an]ginx",
	} {
		if err := ValidateSudoCommand(cmd, false); !errors.Is(err, ErrSudoCommandWildcard) {
			t.Errorf("%q muss wegen platzhalter abgewiesen werden, bekam %v", cmd, err)
		}
	}
}

// TestSudoKommandoOhneShellZeichen: Das Komma ist dabei kein
// Schönheitsfehler - in sudoers TRENNT es Kommandos, ein Komma schmuggelte
// also ein zweites Kommando in dieselbe Regel.
func TestSudoKommandoOhneShellZeichen(t *testing.T) {
	for _, cmd := range []string{
		"/usr/bin/systemctl restart nginx; /bin/sh",
		"/usr/bin/systemctl restart nginx, /bin/sh",
		"/usr/bin/systemctl restart $(id)",
		"/usr/bin/systemctl restart nginx && /bin/sh",
	} {
		if err := ValidateSudoCommand(cmd, false); !errors.Is(err, ErrSudoCommandMeta) {
			t.Errorf("%q muss wegen sonderzeichen abgewiesen werden, bekam %v", cmd, err)
		}
	}
}

// TestRootGleichwertigeKommandosBrauchenBestaetigung: Shells, Interpreter,
// Editoren und Pager sind unabhängig von den Argumenten ein Rechteaufstieg.
// Verboten sind sie nicht - aber nur mit ausdrücklicher Bestätigung.
func TestRootGleichwertigeKommandosBrauchenBestaetigung(t *testing.T) {
	cases := []string{
		"/bin/bash", "/usr/bin/vi /etc/hosts", "/usr/bin/less /var/log/syslog",
		"/usr/bin/python3 /opt/tool.py", "/bin/dd if=/dev/zero of=/tmp/x",
		"/usr/bin/tee /etc/motd", "/usr/bin/chmod 777 /srv",
		// Nebennamen derselben Programme
		"/usr/bin/python3.11 /opt/tool.py", "/usr/bin/perl5.36.0 /opt/x.pl",
		"/usr/bin/vim.basic /etc/hosts", "/usr/bin/lua5.4 /opt/x.lua", "/bin/rbash",
	}
	for _, cmd := range cases {
		var rootEquivalent *ErrRootEquivalent
		err := ValidateSudoCommand(cmd, false)
		if !errors.As(err, &rootEquivalent) {
			t.Errorf("%q muss als root-gleichwertig gemeldet werden, bekam %v", cmd, err)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), "bestätigung") {
			t.Errorf("meldung nennt die bestätigung nicht: %s", err)
		}
		if err := ValidateSudoCommand(cmd, true); err != nil {
			t.Errorf("%q muss mit bestätigung durchgehen, bekam %v", cmd, err)
		}
	}
}

// TestAusbruchsArgumenteBrauchenBestaetigung: `find` ist mit festen
// Argumenten harmlos - mit `-exec` startet es beliebige Kommandos als root.
func TestAusbruchsArgumenteBrauchenBestaetigung(t *testing.T) {
	if err := ValidateSudoCommand("/usr/bin/find /srv -name core.log", false); err != nil {
		t.Errorf("find mit harmlosen argumenten abgewiesen: %v", err)
	}
	var rootEquivalent *ErrRootEquivalent
	if err := ValidateSudoCommand("/usr/bin/find /srv -exec /bin/sh", false); !errors.As(err, &rootEquivalent) {
		t.Errorf("find -exec muss als root-gleichwertig gemeldet werden, bekam %v", err)
	}
}

// TestPagerBekommtNoPager: `systemctl status` schickt seine Ausgabe durch
// einen Pager, der dann ALS ROOT läuft - in `less` genügt `!sh` für eine
// Root-Shell. Ein vermeintlich lesendes Kommando wäre damit ein voller
// Rechteaufstieg.
func TestPagerBekommtNoPager(t *testing.T) {
	got := NormalizeSudoCommand("/usr/bin/systemctl status nginx")
	if got != "/usr/bin/systemctl --no-pager status nginx" {
		t.Errorf("--no-pager wurde nicht ergänzt: %q", got)
	}
	// Schon vorhanden: nicht doppelt einsetzen.
	already := "/usr/bin/journalctl --no-pager -u nginx"
	if got := NormalizeSudoCommand(already); got != already {
		t.Errorf("--no-pager doppelt gesetzt: %q", got)
	}
	// Andere Programme bleiben unangetastet.
	if got := NormalizeSudoCommand("/usr/bin/apt-get update"); got != "/usr/bin/apt-get update" {
		t.Errorf("fremdes kommando verändert: %q", got)
	}
}

// TestGesperrtePfade: Auf diese Pfade darf weder eine Pfadregel noch eine
// sudoedit-Regel zeigen - ein Schreibrecht auf /etc/sudoers.d wäre ein
// Selbstbedienungsladen für Root-Rechte.
func TestGesperrtePfade(t *testing.T) {
	gesperrt := []string{
		"/", "/etc", "/etc/sudoers", "/etc/sudoers.d/lcm-prof-web", "/etc/shadow",
		"/usr", "/usr/bin", "/bin", "/boot", "/root", "/var/lib/lcm", "/etc/ssh/sshd_config",
	}
	for _, p := range gesperrt {
		if err := ValidateEditPath(p); !errors.Is(err, ErrPathProtected) {
			t.Errorf("%q muss gesperrt sein, bekam %v", p, err)
		}
	}
	// Konfigurationsverzeichnisse einzelner Dienste sind ausdrücklich erlaubt.
	for _, p := range []string{"/etc/nginx", "/etc/nginx/sites-available/kunde.conf", "/srv/www", "/opt/app/data"} {
		if err := ValidateEditPath(p); err != nil {
			t.Errorf("%q sollte erlaubt sein, bekam %v", p, err)
		}
	}
}

// TestPfadregelPruefungen deckt Form und Modus ab.
func TestPfadregelPruefungen(t *testing.T) {
	if err := ValidatePathRule("srv/www", PathModeRead); !errors.Is(err, ErrPathRelative) {
		t.Errorf("relativer pfad muss abgewiesen werden, bekam %v", err)
	}
	if err := ValidatePathRule("/srv/*", PathModeRead); !errors.Is(err, ErrPathMeta) {
		t.Errorf("platzhalter im pfad muss abgewiesen werden, bekam %v", err)
	}
	if err := ValidatePathRule("/srv/www", "vielleicht"); !errors.Is(err, ErrPathModeInvalid) {
		t.Errorf("ungültiger modus muss abgewiesen werden, bekam %v", err)
	}
	for _, mode := range []string{PathModeRead, PathModeReadWrite, PathModeDeny} {
		if err := ValidatePathRule("/srv/www", mode); err != nil {
			t.Errorf("modus %q abgewiesen: %v", mode, err)
		}
	}
}

// TestRootGleichwertigePfade: Was unter diesen Pfaden liegt, führt das System
// als root aus - ein Schreibrecht dort ist eine Root-Shell auf Umwegen. Das
// gilt auch für eine Schreibweise mit "//" oder "/" am Ende und für eine
// rekursive Freigabe darüber. Ein Name, der nur gleich beginnt, ist frei.
func TestRootGleichwertigePfade(t *testing.T) {
	for _, p := range []string{
		"/etc/crontab", "/etc/cron.d", "/etc/cron.daily/logrotate", "/etc/anacrontab",
		"/var/spool/cron", "/var/spool/cron/crontabs/root",
		"/etc/systemd", "/etc/systemd/system/x.service", "/etc//systemd/", "/run/systemd/system",
		"/etc/pam.d/sshd", "/etc/ld.so.preload", "/etc/ld.so.conf.d/x.conf",
		"/etc/profile", "/etc/profile.d/x.sh", "/etc/environment", "/etc/bash.bashrc",
		"/etc/sudo.conf", "/etc/sudoers.lcm", "/etc/logrotate.d/nginx", "/etc/init.d/nginx",
		"/etc/apt/apt.conf.d/99hook", "/home/" + DefaultServiceUser, "/home/" + DefaultServiceUser + "/.ssh",
		// Eine eigene Paketquelle installiert beim nächsten Update als root,
		// core_pattern in sysctl startet ein Programm als root, ebenso
		// Netzwerk-Hooks und Paketskripte.
		"/etc/apt/sources.list.d", "/etc/sysctl.d/99-x.conf", "/etc/rc2.d", "/etc/network/if-up.d",
		"/var/lib/dpkg/info", "/var/lib/docker",
	} {
		for _, mode := range []string{PathModeRead, PathModeReadWrite, PathModeDeny} {
			if err := ValidatePathRule(p, mode); !errors.Is(err, ErrPathRootEquivalent) {
				t.Errorf("%s (%s) muss gesperrt sein, bekam %v", p, mode, err)
			}
		}
		if err := ValidateEditPath(p); !errors.Is(err, ErrPathRootEquivalent) {
			t.Errorf("sudoedit %s muss gesperrt sein, bekam %v", p, err)
		}
	}
	// Darüber: setfacl -R gäbe den gesperrten Bereich mit frei. Eine
	// Verweigerung gibt nichts frei.
	for _, p := range []string{"/var", "/var/spool", "/var/lib", "/home", "/run"} {
		if err := ValidatePathRule(p, PathModeRead); err == nil {
			t.Errorf("read %s gibt einen gesperrten Bereich mit frei", p)
		}
		if err := ValidatePathRule(p, PathModeDeny); err != nil {
			t.Errorf("deny %s sollte erlaubt sein, bekam %v", p, err)
		}
	}
	for _, p := range []string{"/etc/systemdx", "/etc/nginx", "/var/www", "/var/spool/mail", "/home/anna", "/etc/resolv.conf"} {
		if err := ValidatePathRule(p, PathModeReadWrite); err != nil {
			t.Errorf("%s sollte erlaubt sein, bekam %v", p, err)
		}
	}
}

// TestPfadOhneElternverweis: Ein Pfad steht so da, wie er wirkt.
func TestPfadOhneElternverweis(t *testing.T) {
	for _, p := range []string{"/srv/../etc/nginx", "/srv/www/..", "/srv/a/../b"} {
		if err := ValidatePathRule(p, PathModeRead); !errors.Is(err, ErrPathParentRef) {
			t.Errorf("%s: erwartet ErrPathParentRef, bekam %v", p, err)
		}
		if err := ValidateEditPath(p); !errors.Is(err, ErrPathParentRef) {
			t.Errorf("sudoedit %s: erwartet ErrPathParentRef, bekam %v", p, err)
		}
	}
}

// TestProfilSlug: Aus dem Slug entsteht der Gruppenname auf dem Zielsystem.
func TestProfilSlug(t *testing.T) {
	gueltig := []string{"web", "webserver-betrieb", "db2", "a"}
	for _, s := range gueltig {
		if !ValidProfileSlug(s) {
			t.Errorf("%q sollte gültig sein", s)
		}
	}
	ungueltig := []string{"", "Web", "web_server", "-web", "9web", "web-", "mit leerzeichen",
		strings.Repeat("x", MaxProfileSlugLen+1)}
	for _, s := range ungueltig {
		if ValidProfileSlug(s) {
			t.Errorf("%q sollte ungültig sein", s)
		}
	}
}

// TestNebennamenTreffenNurListenProgramme: Die Suche nach Nebennamen kappt
// Versionsnummer und Variante, macht aber aus einem harmlosen Programm kein
// gelistetes.
func TestNebennamenTreffenNurListenProgramme(t *testing.T) {
	for _, name := range []string{"id", "systemctl", "sha256sum", "x86_64-linux-gnu-gcc-13", "journalctl"} {
		if listed, _, blocked := rootEquivalentBinary(name); blocked {
			t.Errorf("%s als %s gemeldet", name, listed)
		}
	}
}

// TestNurZeichenDesHelpers: Pfade und Kommandos mit Leerzeichen oder Umlauten
// nähme der Helper im eingeschränkten Modus nicht an - sie scheitern schon
// beim Speichern, nicht erst bei jedem Abgleich.
func TestNurZeichenDesHelpers(t *testing.T) {
	for _, p := range []string{"/srv/my dir", "/srv/größe", "/srv/tab\there"} {
		if err := ValidatePathRule(p, PathModeRead); !errors.Is(err, ErrPathMeta) {
			t.Errorf("%q: erwartet ErrPathMeta, bekam %v", p, err)
		}
	}
	if err := ValidateSudoCommand("/usr/bin/systemctl --no-pager restart dienst-ä", false); !errors.Is(err, ErrSudoCommandMeta) {
		t.Errorf("Umlaut im Kommando durchgelassen: %v", err)
	}
	if err := ValidatePathRule("/srv/app_1.2-x@y", PathModeRead); err != nil {
		t.Errorf("erlaubte Zeichen abgelehnt: %v", err)
	}
}
