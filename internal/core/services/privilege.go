package services

import (
	"encoding/base64"
	"fmt"
	"path"
	"strings"

	"LCM/internal/core/domain"
)

// restrictSelfTestExit ist der Exit-Code, mit dem das Umschalt-Skript meldet:
// „eingeschränkter Modus verworfen, Voll-Modus wiederhergestellt". Bewusst
// abseits der üblichen 1/2, damit er nicht mit einem Fehler der
// Einzelkommandos verwechselt wird.
const restrictSelfTestExit = 9

// Eingeschränkter Service-User (Server.RestrictedSudo).
//
// Im Voll-Modus (Default) hat der Management-Benutzer NOPASSWD:ALL und LCM
// führt Skripte als root über `sudo sh -c '<script>'` aus. Im eingeschränkten
// Modus darf der Benutzer per sudoers-Whitelist nur eine feste Auswahl an
// Binaries ausführen (Paketverwaltung, Docker, ufw und den validierenden
// LCM-Helper) - KEINE Root-Shell und keinen beliebigen Befehl.
//
// Damit die bestehenden (mehrschrittigen) Skript-Generatoren unverändert
// bleiben, nutzt der eingeschränkte Modus einen PATH-Shim: LCM legt im Home
// des Service-Users kleine Wrapper (`~/.lcm/sudo-bin/<binary>`) an, die das
// jeweilige Binary über `sudo -n` aufrufen. Restricted-Skripte laufen dann als
// der (nicht-root) Service-User mit diesem Verzeichnis vorne im PATH: die
// erlaubten Binaries gehen automatisch über sudo, alles andere läuft ohne
// Privilegien. Die eigentliche Sicherheitsgrenze ist die sudoers-Whitelist -
// die Wrapper sind nur Bequemlichkeit (der Service-User könnte sie ändern,
// gewönne dadurch aber nichts, weil sudo nur die Whitelist erlaubt).

// sudoBinary beschreibt ein im eingeschränkten Modus erlaubtes Kommando:
// den im Shim genutzten Namen und die absoluten Pfade für die sudoers-Regel
// (mehrere gängige Pfade je Distribution; nicht existente matchen nie).
type sudoBinary struct {
	name  string
	paths []string
}

// allowedSudoBinaries ist die Whitelist des eingeschränkten Service-Users:
// Paketverwaltung (Updates + Lese-Abfragen), Docker, ufw und der LCM-Helper
// (fest umrissene Verwaltungsaktionen wie Repositories, sshd-Drop-ins und
// Benutzer-Sync mit strenger Parameter-Validierung - siehe lcm_helper.go).
// Bewusst NICHT enthalten: sh/bash, tee, systemctl, beliebige Editoren.
//
// WAS DIESE LISTE NICHT LEISTET - bitte vor Änderungen lesen:
//
// Der eingeschränkte Modus verhindert KEINE Rechteausweitung durch einen
// Angreifer, der den Service-Schlüssel besitzt. apt-get, dpkg und docker
// führen konstruktionsbedingt Code als root aus - das ist ihr Zweck, und
// genau deshalb stehen sie hier. Beide Wege sind nachgewiesen:
//
//	sudo -n apt-get -o APT::Update::Pre-Invoke::="…" update   → beliebiger Code als root
//	sudo -n docker run --rm -v /:/host alpine …               → volles Wirts-Dateisystem
//
// sudo kann Argumente nicht zuverlässig filtern, ein Verbot von "-o" wäre
// also wirkungslos. Ohne diese drei Programme wäre der Modus funktionslos,
// denn Paket-Updates und Docker sind die beworbenen Kernaktionen, die auch
// eingeschränkt weiterlaufen sollen.
//
// Der reale Nutzen ist damit: kleinere Angriffsfläche gegen VERSEHENTLICHES
// Fehlverhalten und Bedienfehler, plus ein vollständiges Protokoll dessen,
// was LCM tut. Wer echten Schutz gegen einen kompromittierten Service-Zugang
// braucht, müsste die gefährlichen Kommandos hinter eng validierende
// lcm-helper-Unterkommandos legen (nur bestimmte apt-Transaktionen ohne
// -o-Overrides, Docker ohne Host-Mounts und ohne --privileged) - das
// beschneidet den Funktionsumfang erheblich und ist bewusst nicht der
// aktuelle Stand.
var allowedSudoBinaries = []sudoBinary{
	// Debian/Ubuntu.
	{"apt-get", []string{"/usr/bin/apt-get", "/bin/apt-get"}},
	{"apt", []string{"/usr/bin/apt", "/bin/apt"}},
	{"apt-cache", []string{"/usr/bin/apt-cache", "/bin/apt-cache"}},
	{"dpkg", []string{"/usr/bin/dpkg", "/bin/dpkg"}},
	{"dpkg-query", []string{"/usr/bin/dpkg-query", "/bin/dpkg-query"}},
	// RHEL-Familie.
	{"dnf", []string{"/usr/bin/dnf"}},
	{"yum", []string{"/usr/bin/yum", "/bin/yum"}},
	// SUSE.
	{"zypper", []string{"/usr/bin/zypper"}},
	// Arch. pacman-key gehört dazu, weil die Repository-Einrichtung fremde
	// Signaturschlüssel importiert (--add/--lsign-key). Ohne beide war der
	// eingeschränkte Modus auf Arch funktionslos: LCM meldete Erfolg, während
	// jeder Paketlauf an „you cannot perform this operation unless you are
	// root" scheiterte (R2-020).
	{"pacman", []string{"/usr/bin/pacman", "/bin/pacman", "/sbin/pacman"}},
	{"pacman-key", []string{"/usr/bin/pacman-key", "/bin/pacman-key", "/sbin/pacman-key"}},
	// Alpine. apk liegt dort unter /sbin (R2-020, zweite Hälfte).
	{"apk", []string{"/sbin/apk", "/usr/bin/apk", "/bin/apk", "/usr/sbin/apk"}},
	// Snap (zweite Paketverwaltung, v.a. Ubuntu).
	{"snap", []string{"/usr/bin/snap", "/snap/bin/snap"}},
	// Docker (Container/Compose steuern).
	{"docker", []string{"/usr/bin/docker", "/bin/docker"}},
	// Firewall.
	{"ufw", []string{"/usr/sbin/ufw", "/usr/bin/ufw", "/sbin/ufw"}},
	// Deep Scan: read-only Audit-Tools (needrestart liest Prozess-Maps, lynis
	// prüft die Systemhärtung). Beide verändern die Konfiguration nicht.
	{"needrestart", []string{"/usr/sbin/needrestart", "/usr/bin/needrestart", "/sbin/needrestart"}},
	{"lynis", []string{"/usr/bin/lynis", "/usr/sbin/lynis", "/usr/local/bin/lynis", "/sbin/lynis"}},
	// LCM-Helper: validierte privilegierte Verwaltungsaktionen (Repos,
	// apt-Cache, sshd-Drop-ins, Benutzer-Sync) - wird beim Einschränken
	// mitinstalliert (root:root, für den Service-User nicht beschreibbar).
	{"lcm-helper", []string{lcmHelperPath}},
}

// sudoShimDir ist das Verzeichnis der Shim-Wrapper im Home des Service-Users.
func sudoShimDir(svcUser string) string {
	return "/home/" + svcUser + "/.lcm/sudo-bin"
}

// restrictedPathPrelude ist der PATH, mit dem Restricted-Skripte laufen: der
// Shim zuerst (erlaubte Binaries → sudo), dann die üblichen Systempfade für
// alles Übrige (echo, grep, cd, …) - als nicht-privilegierter Service-User.
func restrictedPathPrelude(svcUser string) string {
	return "PATH=" + sudoShimDir(svcUser) + ":/usr/sbin:/usr/bin:/sbin:/bin"
}

// restrictedSecurePath ist der sudo-Suchpfad des eingeschränkten
// Service-Users. LCM setzt ihn selbst, statt sich auf den der Distribution zu
// verlassen: RHEL 10 und seine Klone liefern
// `secure_path = /sbin:/bin:/usr/sbin:/usr/bin` - OHNE `/usr/local/sbin`, wo
// der LCM-Helper liegt. Der Shim-Wrapper rief `sudo -n lcm-helper` also nach
// einem Namen auf, den sudo nie fand; die Whitelist-Regel (absoluter Pfad)
// wurde nie getroffen und JEDE Helper-Funktion scheiterte mit „sudo: a
// password is required" - SSH-Härtung, Root-Login, Repositories, apt-Cache,
// Benutzer-Sync (R2-019, belegt auf almalinux/centos/rockylinux/opensuse).
const restrictedSecurePath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// restrictedSudoersContent baut den Inhalt von /etc/sudoers.d/<svcUser> für den
// eingeschränkten Modus: NOPASSWD nur für die Whitelist-Binaries. `!requiretty`
// erlaubt sudo ohne TTY (SSH-Exec), `env_keep` reicht DEBIAN_FRONTEND für
// nicht-interaktive apt-Läufe durch.
func restrictedSudoersContent(svcUser string) string {
	var paths []string
	for _, b := range allowedSudoBinaries {
		paths = append(paths, b.paths...)
	}
	return strings.Join([]string{
		"# LCM - eingeschränkter Management-Benutzer (nur Whitelist, kein Root-Shell-Zugriff).",
		"# Von LCM verwaltet; nicht von Hand bearbeiten.",
		"Defaults:" + svcUser + " !requiretty",
		"Defaults:" + svcUser + " env_keep += \"DEBIAN_FRONTEND\"",
		// Eigener Suchpfad - siehe restrictedSecurePath (R2-019).
		"Defaults:" + svcUser + " secure_path=\"" + restrictedSecurePath + "\"",
		// openSUSE setzt `Defaults targetpw`: sudo verlangt dann das Passwort
		// des ZIEL-Benutzers (root), was einen passwortlosen Dienstzugang
		// grundsätzlich ausschließt. Für den Service-User abschalten - die
		// Rechte begrenzt hier die Whitelist, nicht eine Passwortabfrage.
		"Defaults:" + svcUser + " !targetpw",
		"Cmnd_Alias LCM_ALLOWED = " + strings.Join(paths, ", "),
		svcUser + " ALL=(root) NOPASSWD: LCM_ALLOWED",
		"",
	}, "\n")
}

// writeFileB64 liefert einen Shell-Schritt, der content (base64-kodiert, also
// frei von Sonderzeichen/Quoting-Fallen bei verschachteltem sudo sh -c) auf dem
// Server nach path schreibt.
func writeFileB64(content, path string) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(content))
	return fmt.Sprintf("printf '%%s' %s | base64 -d > %s", b64, path)
}

// restrictedProvisionScript liefert die Shell-Schritte, die beim Onboarding im
// eingeschränkten Modus zusätzlich zum User die sudoers-Whitelist und die
// Shim-Wrapper anlegen. Ersetzt die NOPASSWD:ALL-Zeile des Voll-Modus.
func restrictedProvisionScript(svcUser string) []string {
	shim := sudoShimDir(svcUser)
	steps := []string{
		// Zielverzeichnis des Helpers sicherstellen: Alpine liefert
		// /usr/local/sbin nicht mit, dort brach das Einschränken bisher mit
		// „can't create /usr/local/sbin/lcm-helper.tmp: nonexistent
		// directory" ab (R2-021).
		"install -d -m 755 " + path.Dir(lcmHelperPath),
		// LCM-Helper zuerst installieren (root:root, 755 - der Service-User
		// kann ihn nicht verändern), damit die sudoers-Whitelist nie auf ein
		// fehlendes Binary zeigt.
		writeFileB64(lcmHelperScript, lcmHelperPath+".tmp"),
		fmt.Sprintf("chmod 755 %s.tmp", lcmHelperPath),
		fmt.Sprintf("mv %s.tmp %s", lcmHelperPath, lcmHelperPath),
		// Eingeschränkte sudoers-Whitelist: erst in eine .tmp schreiben, per
		// visudo prüfen, dann atomar an ihren Platz verschieben (nie eine
		// kaputte sudoers-Datei hinterlassen).
		writeFileB64(restrictedSudoersContent(svcUser), fmt.Sprintf("/etc/sudoers.d/%s.tmp", svcUser)),
		fmt.Sprintf("visudo -cf /etc/sudoers.d/%s.tmp >/dev/null", svcUser),
		fmt.Sprintf("chmod 440 /etc/sudoers.d/%s.tmp", svcUser),
		fmt.Sprintf("mv /etc/sudoers.d/%s.tmp /etc/sudoers.d/%s", svcUser, svcUser),
		// Shim-Verzeichnis (gehört dem Service-User).
		fmt.Sprintf("install -d -m 755 -o %s -g %s %s", svcUser, svcUser, shim),
	}
	// Je erlaubtem Binary ein Wrapper `<shim>/<name>` → `sudo -n <name> "$@"`.
	for _, b := range allowedSudoBinaries {
		wrapper := shim + "/" + b.name
		// Der Helper wird von LCM selbst installiert, sein Pfad steht also
		// fest - ihn absolut aufzurufen macht den wichtigsten Wrapper von der
		// sudo-Pfadsuche unabhängig (zweite Absicherung neben secure_path,
		// R2-019). Distributionsbinaries bleiben beim Namen, weil ihr Ort je
		// System abweicht.
		target := b.name
		if len(b.paths) == 1 && b.paths[0] == lcmHelperPath {
			target = lcmHelperPath
		}
		content := "#!/bin/sh\nexec sudo -n " + target + " \"$@\"\n"
		steps = append(steps,
			writeFileB64(content, wrapper),
			fmt.Sprintf("chmod 755 %s", wrapper),
			fmt.Sprintf("chown %s:%s %s", svcUser, svcUser, wrapper),
		)
	}
	return steps
}

// restrictedSelfTestScript prüft ALS SERVICE-USER, ob der eingeschränkte Modus
// tatsächlich trägt: kommt der LCM-Helper über sudo an, und lässt sich die
// Paketverwaltung des Systems aufrufen? Genau das war der blinde Fleck - LCM
// meldete „eingeschränkt: eingerichtet", und erst der nächste echte Lauf fiel
// auf die Nase: auf Arch war die Paketverwaltung tot (pacman fehlte in der
// Whitelist, R2-020), auf RHEL-Klonen und openSUSE scheiterte jede
// Helper-Funktion am secure_path (R2-019). Beides ist behoben - die Probe
// sorgt dafür, dass ein künftiger Bruch derselben Art nicht wieder als Erfolg
// durchgeht, auch auf einer Distribution, die wir hier nie getestet haben.
//
// Der Paketmanager wird auf dem Zielsystem selbst gesucht, und zwar bewusst
// OHNE das Shim-Verzeichnis im PATH: dort liegt für jedes Whitelist-Binary ein
// Wrapper, `command -v apt-get` fände also auch auf einem Arch-System einen
// Treffer. Gesucht wird deshalb im System-PATH, geprüft wird über den Shim.
//
// Das Probe-Skript kommt base64-kodiert an `su -c`, damit die Quoting-Ebenen
// (sudo sh -c '…' → su -c "…") nicht kollidieren.
func restrictedSelfTestScript(svcUser string) string {
	probe := strings.Join([]string{
		restrictedPathPrelude(svcUser),
		"export PATH",
		`lcm-helper selftest >/dev/null 2>&1 || { echo "der LCM-Helper ist als eingeschraenkter Benutzer nicht ueber sudo erreichbar" >&2; exit 1; }`,
		`PM=""`,
		`for c in apt-get dnf yum zypper pacman apk; do ` +
			`if PATH=/usr/sbin:/usr/bin:/sbin:/bin command -v "$c" >/dev/null 2>&1; then PM="$c"; break; fi; done`,
		`[ -n "$PM" ] || { echo "keine bekannte Paketverwaltung gefunden" >&2; exit 1; }`,
		`"$PM" --version >/dev/null 2>&1 || ` +
			`{ echo "die Paketverwaltung $PM ist als eingeschraenkter Benutzer nicht ueber sudo erreichbar" >&2; exit 1; }`,
	}, "\n")
	b64 := base64.StdEncoding.EncodeToString([]byte(probe))
	return fmt.Sprintf(`su -s /bin/sh %s -c "$(printf '%%s' %s | base64 -d)" 2>&1`, svcUser, b64)
}

// restrictedRollbackScript stellt den Voll-Modus wieder her (identisch zu dem,
// was das Onboarding im Normalfall schreibt) und räumt die Shim-Wrapper ab.
// Gegenstück zur Selbstprobe: schlägt sie fehl, darf kein halb eingeschränkter
// Server zurückbleiben, dessen Rückweg nur noch über die Serverkonsole ginge.
func restrictedRollbackScript(svcUser string) string {
	return strings.Join([]string{
		fmt.Sprintf("printf '%%s ALL=(ALL) NOPASSWD:ALL\\n' %s > /etc/sudoers.d/%s", svcUser, svcUser),
		fmt.Sprintf("chmod 440 /etc/sudoers.d/%s", svcUser),
		fmt.Sprintf("rm -rf %s", sudoShimDir(svcUser)),
	}, "; ")
}

// privRun ist die server-bewusste Hülle um wrapSudo: es liest Service-User und
// Rechte-Modus aus dem Server. Der bevorzugte Weg an allen Aufrufstellen, die
// ein *domain.Server zur Hand haben.
func privRun(server *domain.Server, cmd string) string {
	return wrapSudo(server.ServiceUser, server.RestrictedSudo, cmd)
}

// restrictedAllowsRule meldet, ob ein Rule-Typ mit dem eingeschränkten
// Service-User ausführbar ist: Paketverwaltung, Docker-Prune, Firewall,
// Benutzer-Sync und apt-Cache-Anbindung (beide über den validierenden
// LCM-Helper) sowie der reine Verfügbarkeits-Check. Gesperrt bleiben
// script/custom (beliebige Kommandos) und reboot - sie brauchen eine
// Root-Shell bzw. volle Systemrechte.
func restrictedAllowsRule(ruleType string) bool {
	switch ruleType {
	case domain.RuleTypeUpdate, domain.RuleTypePackages, domain.RuleTypeSecurity,
		domain.RuleTypePackageScan, domain.RuleTypeAutoremove, domain.RuleTypeDockerPrune,
		domain.RuleTypeDockerUpdateUnused,
		domain.RuleTypeFirewall, domain.RuleTypeHealth,
		domain.RuleTypeSync, domain.RuleTypeAptProxy,
		domain.RuleTypeDNSTest, domain.RuleTypeDeepScan,
		// ACL-Einrichtung laeuft ueber die Paketverwaltung (Whitelist), der
		// Rechte-Abgleich ueber setfacl/chmod auf von LCM verwalteten Pfaden.
		domain.RuleTypeACLSetup, domain.RuleTypePermSync:
		return true
	}
	return false
}
