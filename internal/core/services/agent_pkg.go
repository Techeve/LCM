package services

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Agent-Update für dnf, zypper, apk und pacman.
//
// Der Ablauf ist derselbe wie unter apt (siehe agent_update.go): Versionen der
// Paketquelle lesen, LCM wählt die Zielversion, genau diese Version
// installieren, die installierte Version zurücklesen. Hier stehen die Skripte
// und Auswertungen je Paketverwaltung.
//
// Die Skripte geben die Ausgabe der Paketverwaltung roh nach der Marke
// LCM_AGENT_VERSIONS aus; ausgewertet wird in Go (parseAgentList), damit sich
// die Auswertung gegen echte Ausgaben testen lässt.
//
// Halten, je Familie (der Agent bleibt bei einem Systemupdate stehen):
//
//	dnf     exclude=lcm-agent in der Repo-Datei techeve-lcm (setup.sh und
//	        Paketskript); LCM installiert mit --setopt=disable_excludes=techeve-lcm
//	        (dnf4 und dnf5 kennen das, --disableexcludes nur dnf4)
//	zypper  Schloss (zypper addlock). Es wird um die Installation herum gelöst
//	        und danach wieder gesetzt (EXIT-Falle, auch nach einem Fehlschlag).
//	apk     `apk add lcm-agent=<Version>` trägt die Version in /etc/apk/world ein
//	pacman  IgnorePkg = lcm-agent in /etc/pacman.conf (Paketskript); `pacman -U`
//	        setzt sich darüber hinweg
//
// yum ohne dnf (CentOS 7, Amazon Linux 2) ist nicht unterstützt: Das
// Versionsskript meldet dort agentMarkNoManager.
//
// Arch: Beim Agent-Update erneuert LCM die Paketlisten des Systems nie (pacman
// -Sy ohne -u ist ein nicht unterstütztes Teil-Update); die Datenbank des Repos
// techeve wird in ein Wegwerf-Verzeichnis geholt, und installiert wird immer
// mit pacman -U.

// Marke der Skripte, wenn die Paketverwaltung des Servers fehlt (apt hat
// agentMarkNoApt).
const agentMarkNoManager = "LCM_AGENT_NO_MANAGER"

const (
	// agentDnfRepo ist die Repo-Kennung, die setup.sh für dnf und zypper anlegt.
	agentDnfRepo = "techeve-lcm"
	// agentPacmanRepo ist der Repo-Name im Arch-Repository.
	agentPacmanRepo = "techeve"
	// agentPacmanRel ist die Paketrevision des Arch-Pakets (nfpm schreibt immer 1).
	agentPacmanRel = "1"
	// agentZyppLockWait ist die Zeit in Sekunden, die zypper auf eine fremde
	// Sperre wartet, bevor es aufgibt.
	agentZyppLockWait = "300"
)

// Rückgabe der installierten Version je Paketverwaltung (Teil der Skripte).
const (
	agentReadRpm    = `$(rpm -q --qf '%{VERSION}-%{RELEASE}' ` + agentPackage + ` 2>/dev/null)`
	agentReadApk    = `$(apk list -I ` + agentPackage + ` 2>/dev/null | sed -n 's/^` + agentPackage + `-\([0-9][^ ]*\) .*/\1/p')`
	agentReadPacman = `$(pacman -Q ` + agentPackage + ` 2>/dev/null | cut -d' ' -f2)`
)

// agentReadInstalled liefert den Shell-Ausdruck, der die installierte Version
// der Familie nennt.
func agentReadInstalled(family string) string {
	switch family {
	case pkgApk:
		return agentReadApk
	case pkgPacman:
		return agentReadPacman
	}
	return agentReadRpm
}

// agentVersionsScriptFor liest je Paketverwaltung, was installiert und zu haben
// ist (siehe agentVersionsScript für apt). refresh erneuert vorher die
// Paketlisten; scheitert das, gilt der Stand der Listen, und die Marke merkt es an.
func agentVersionsScriptFor(mgr string, refresh bool) string {
	family := pkgFamily(mgr)
	if family == pkgApt {
		return agentVersionsScript(refresh)
	}
	if mgr == pkgYum {
		return `echo ` + agentMarkNoManager + "\nexit 0"
	}
	lines := []string{
		`export LC_ALL=C`,
		agentPresentCheck(mgr),
		agentPackagedCheck(family),
	}
	if family == pkgPacman {
		lines = append(lines, `lcm_pdb=`, `lcm_popt=`)
		if refresh {
			lines = append(lines, agentPacmanFunc)
		}
	}
	if refresh {
		lines = append(lines, agentRefreshLine(mgr))
	}
	lines = append(lines,
		`echo `+agentMarkInstalled+agentReadInstalled(family),
		`echo `+agentMarkVersions,
		agentListCommand(mgr),
	)
	if family == pkgPacman && refresh {
		lines = append(lines, `[ -z "$lcm_pdb" ] || rm -rf "$lcm_pdb"`)
	}
	lines = append(lines, `exit 0`)
	return strings.Join(lines, "\n")
}

// agentPresentCheck endet das Skript mit der Marke, wenn die Paketverwaltung fehlt.
func agentPresentCheck(mgr string) string {
	var need string
	switch pkgFamily(mgr) {
	case pkgDnf:
		need = `command -v ` + dnfBin(mgr) + ` >/dev/null 2>&1 && command -v rpm >/dev/null 2>&1`
	case pkgZypper:
		need = `command -v zypper >/dev/null 2>&1 && command -v rpm >/dev/null 2>&1`
	case pkgApk:
		need = `command -v apk >/dev/null 2>&1`
	default:
		need = `command -v pacman >/dev/null 2>&1`
	}
	return need + ` || { echo ` + agentMarkNoManager + `; exit 0; }`
}

// agentPackagedCheck endet das Skript mit der Marke, wenn lcm-agent kein Paket ist.
func agentPackagedCheck(family string) string {
	var is string
	switch family {
	case pkgApk:
		is = `apk info -e ` + agentPackage
	case pkgPacman:
		is = `pacman -Q ` + agentPackage
	default:
		is = `rpm -q ` + agentPackage
	}
	return is + ` >/dev/null 2>&1 || { echo ` + agentMarkNotPackaged + `; exit 0; }`
}

// agentRefreshLine erneuert die Paketlisten; ein Fehler wird nur vermerkt.
func agentRefreshLine(mgr string) string {
	var cmd string
	switch pkgFamily(mgr) {
	case pkgDnf:
		cmd = dnfBin(mgr) + ` -q --refresh makecache --repo ` + agentDnfRepo
	case pkgZypper:
		cmd = `ZYPP_LOCK_TIMEOUT=` + agentZyppLockWait + ` zypper -n refresh ` + agentDnfRepo
	case pkgApk:
		cmd = `apk update`
	default:
		return agentPacmanRefresh
	}
	return cmd + ` >/dev/null 2>&1 || echo ` + agentMarkRefreshFail + `$?`
}

// agentPacmanRefresh holt die Datenbank des Repos techeve in ein
// Wegwerf-Verzeichnis (eigene Konfiguration nur mit diesem Repo, eigener
// dbpath): Die Datenbanken des Systems bleiben unberührt, denn ein
// `pacman -Sy` frischte alle auf, ohne dass das System dazu aktualisiert wird
// (nicht unterstütztes Teil-Update). Die Signatur der Datenbank prüft pacman
// nach dem SigLevel des Repos. pacman-conf nennt Server und SigLevel des
// Systems mit aufgelöstem $arch. Scheitert das, bleibt lcm_popt leer, und die
// Liste kommt aus dem Stand des Systems.
const agentPacmanRefresh = `lcm_pdb=$(mktemp -d 2>/dev/null) && mkdir -p "$lcm_pdb/db/sync" && ` +
	`{ echo '[options]'; echo 'LogFile = /dev/null'; echo '[` + agentPacmanRepo + `]'; pacman-conf -r ` + agentPacmanRepo + `; } >"$lcm_pdb/pacman.conf" && ` +
	`lcm_pacman --config "$lcm_pdb/pacman.conf" --dbpath "$lcm_pdb/db" -Sy >/dev/null 2>&1 && ` +
	`lcm_popt="--config $lcm_pdb/pacman.conf --dbpath $lcm_pdb/db" || echo ` + agentMarkRefreshFail + `$?`

// agentPacmanFunc definiert lcm_pacman, den Aufruf für alles, was pacman aus dem
// Netz lädt (-Sy, -U mit URL). pacman 7 lädt in einem Sandbox-Prozess und
// beschränkt ihn mit Landlock; Kernel ohne Landlock (Arch im LXC-Container,
// etwa auf Proxmox) lassen jeden Download mit „restricting filesystem access
// failed because Landlock is not supported by the kernel!“ scheitern, unter
// strengem seccomp mit „… because the Landlock ruleset could not be applied“. Nur dann,
// und nur einmal, läuft derselbe Aufruf mit --disable-sandbox wiederholt. Jeder
// andere Fehler bleibt unverändert, und pacman.conf bekommt nie DisableSandbox.
// Die Standardausgabe läuft durch, nur die Fehlerausgabe wird zwischengelagert;
// der Rückgabewert ist der von pacman. Ohne mktemp läuft pacman ganz normal.
const agentPacmanFunc = `lcm_pacman() {
lcm_pe=$(mktemp 2>/dev/null) || { pacman "$@"; return $?; }
pacman "$@" 2>"$lcm_pe"
lcm_prc=$?
if [ "$lcm_prc" -ne 0 ] && grep -q 'restricting filesystem access failed because' "$lcm_pe"; then
rm -f "$lcm_pe"
pacman --disable-sandbox "$@"
return $?
fi
cat "$lcm_pe" >&2
rm -f "$lcm_pe"
return "$lcm_prc"
}`

// agentListCommand listet die Versionen, die die Quellen führen. Bei dnf lässt
// disable_excludes das exclude der Techeve-Repo-Datei (das Halten) außen vor.
func agentListCommand(mgr string) string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		return dnfBin(mgr) + ` -q --setopt=disable_excludes=` + agentDnfRepo +
			` repoquery --available --qf '%{name} %{version}-%{release} %{repoid}\n' ` + agentPackage + ` 2>/dev/null`
	case pkgZypper:
		return `ZYPP_LOCK_TIMEOUT=` + agentZyppLockWait + ` zypper -n search -s --match-exact ` + agentPackage + ` 2>/dev/null`
	case pkgApk:
		return `apk policy ` + agentPackage + ` 2>/dev/null`
	default:
		return `pacman $lcm_popt -Si ` + agentPacmanRepo + `/` + agentPackage + ` 2>/dev/null`
	}
}

// agentInstallScriptFor installiert genau diese Version (SemVer-Schreibweise wie
// in agentVersions.Available) und meldet danach die installierte. Auf die Version
// des Servers wird nur gewechselt, weil LCM sie gewählt hat: Abwärts geht es
// nur beim Nachziehen nach einem Rollback.
//
// Die Version landet in einem Shell-Skript, das als root läuft. Sie stammt
// teils von den Servern selbst (Rückmeldung des Agents), deshalb lehnt die
// Funktion alles ab, was keine Paketversion ist.
func agentInstallScriptFor(mgr, ver string) (string, error) {
	if !rePackageVersion.MatchString(ver) {
		return "", fmt.Errorf("ungültige Agent-Version %q", ver)
	}
	family := pkgFamily(mgr)
	if family == pkgApt {
		return agentInstallScript(ver), nil
	}
	if mgr == pkgYum {
		return "", errors.New("yum ohne dnf wird für das Agent-Update nicht unterstützt")
	}
	native := agentNativeVersion(family, ver)
	lines := []string{`export LC_ALL=C`}
	switch family {
	case pkgDnf:
		// dnf4 und dnf5 tauschen bei install auch auf eine ältere Version.
		lines = append(lines,
			dnfBin(mgr)+` -y --setopt=disable_excludes=`+agentDnfRepo+` install `+agentPackage+`-`+native,
			`rc=$?`)
	case pkgZypper:
		// Ein Schloss lässt zypper nicht installieren: lösen und es über eine
		// EXIT-Falle wieder setzen, auch nach einem Fehlschlag. Exit-Code 102
		// (Neustart nötig), 103 (zypper selbst wurde aktualisiert) und 106
		// (Repository übersprungen) sind keine Fehler der Installation: Die
		// Rücklesung der Version entscheidet.
		lines = append(lines,
			`export ZYPP_LOCK_TIMEOUT=`+agentZyppLockWait,
			`zypper -n removelock `+agentPackage+` >/dev/null 2>&1`,
			`trap 'zypper -n addlock `+agentPackage+` >/dev/null 2>&1' EXIT`,
			`zypper -n install --oldpackage `+agentPackage+`=`+native,
			`rc=$?`,
			`case $rc in 102|103|106) rc=0;; esac`)
	case pkgApk:
		lines = append(lines, `apk add `+agentPackage+`=`+native, `rc=$?`)
	default:
		lines = append(lines, agentPacmanInstall(native), `rc=$?`)
	}
	lines = append(lines, `echo `+agentMarkInstalled+agentReadInstalled(family), `exit $rc`)
	return strings.Join(lines, "\n"), nil
}

// agentPacmanInstall installiert immer mit pacman -U aus dem Verzeichnis des
// Repos techeve: Dort liegen alle Dateien samt .sig, die Signatur prüft pacman
// nach dem SigLevel des Systems. `pacman -S` käme nur an die neueste Version,
// und es zöge die Paketlisten, die LCM nicht anrührt (Teil-Update).
func agentPacmanInstall(native string) string {
	return strings.Join([]string{
		agentPacmanFunc,
		`base=$(pacman-conf -r ` + agentPacmanRepo + ` Server | head -n 1)`,
		`lcm_pacman -U --noconfirm "$base/` + agentPackage + `-` + native + `-` + agentPacmanRel + `-$(uname -m).pkg.tar.zst"`,
	}, "\n")
}

// agentListing ist eine Version der Paketquelle in der Schreibweise der Verwaltung.
type agentListing struct {
	Native string
	Source string
}

// parseAgentList liest die Ausgabe nach LCM_AGENT_VERSIONS (Familie ohne apt).
func parseAgentList(family, out string) []agentListing {
	switch family {
	case pkgDnf:
		return parseDnfRepoqueryList(out)
	case pkgZypper:
		return parseZypperSearchList(out)
	case pkgApk:
		return parseApkPolicyList(out)
	default:
		return parsePacmanSiList(out)
	}
}

// parseDnfRepoqueryList liest `repoquery --qf '%{name} %{version}-%{release} %{repoid}'`.
// dnf4 hängt Leerzeilen dazwischen, dnf5 schreibt Statuszeilen davor.
func parseDnfRepoqueryList(out string) []agentListing {
	var list []agentListing
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == agentPackage {
			list = append(list, agentListing{Native: f[1], Source: f[2]})
		}
	}
	return list
}

// parseZypperSearchList liest die Tabelle von `zypper search -s`:
//
//	S  | Name      | Type    | Version        | Arch   | Repository
//
// Zeilen des installierten Stands (Repository „(System Packages)“) sind keine Quelle.
func parseZypperSearchList(out string) []agentListing {
	var list []agentListing
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "|")
		if len(f) < 6 {
			continue
		}
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		if f[1] != agentPackage || f[2] != "package" || strings.Contains(f[5], "System Packages") || f[5] == "@System" {
			continue
		}
		list = append(list, agentListing{Native: f[3], Source: f[5]})
	}
	return list
}

var reApkPolicyVersion = regexp.MustCompile(`^  (\S+):$`)

// parseApkPolicyList liest `apk policy lcm-agent`: je Version eine Zeile „  <Version>:“,
// darunter die Quellen. Eine Version zählt nur, wenn ein Repository sie führt
// (der installierte Stand steht unter lib/apk/db/installed).
func parseApkPolicyList(out string) []agentListing {
	var list []agentListing
	current, listed := "", false
	for _, line := range strings.Split(out, "\n") {
		if m := reApkPolicyVersion.FindStringSubmatch(line); m != nil {
			current, listed = m[1], false
			continue
		}
		src := strings.TrimSpace(line)
		if current == "" || listed || !strings.HasPrefix(line, "    ") || src == "lib/apk/db/installed" {
			continue
		}
		list = append(list, agentListing{Native: current, Source: src})
		listed = true
	}
	return list
}

// parsePacmanSiList liest `pacman -Si techeve/lcm-agent`: die Datenbank des Repos
// führt je Paket eine Version, die neueste.
func parsePacmanSiList(out string) []agentListing {
	var repo, ver string
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Repository":
			repo = strings.TrimSpace(val)
		case "Version":
			ver = strings.TrimSpace(val)
		}
	}
	if ver == "" {
		return nil
	}
	return []agentListing{{Native: ver, Source: repo}}
}

var (
	reRpmRelease     = regexp.MustCompile(`-[^-]+$`)
	reApkRelease     = regexp.MustCompile(`-r[0-9]+$`)
	reApkPre         = regexp.MustCompile(`_([a-z]+)([0-9]*)$`)
	reArchPre        = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+)([a-z].*)$`)
	reCanonPreDotted = regexp.MustCompile(`~([a-z]+)\.([0-9]+)$`)
)

// agentCanonVersion macht aus der Schreibweise der Paketverwaltung die von apt
// (2.4.0~beta.1), in der LCM Versionen vergleicht und anzeigt: Revision ab,
// Vorabkennung vereinheitlicht.
//
//	dnf, zypper  2.4.0~beta.1-1   →  2.4.0~beta.1
//	apk          2.4.0_beta1      →  2.4.0~beta.1
//	pacman       2.4.0beta.1-1    →  2.4.0~beta.1
func agentCanonVersion(family, native string) string {
	v := strings.TrimSpace(native)
	switch family {
	case pkgDnf, pkgZypper:
		return reRpmRelease.ReplaceAllString(v, "")
	case pkgApk:
		v = reApkRelease.ReplaceAllString(v, "")
		return reApkPre.ReplaceAllStringFunc(v, func(m string) string {
			p := reApkPre.FindStringSubmatch(m)
			if p[2] == "" {
				return "~" + p[1]
			}
			return "~" + p[1] + "." + p[2]
		})
	case pkgPacman:
		v = reRpmRelease.ReplaceAllString(v, "")
		return reArchPre.ReplaceAllString(v, "$1~$2")
	}
	return v
}

// agentNativeVersion ist die Umkehrung von agentCanonVersion, ohne Revision
// (so nehmen die Paketverwaltungen eine Version beim Installieren an).
func agentNativeVersion(family, canon string) string {
	switch family {
	case pkgApk:
		if reCanonPreDotted.MatchString(canon) {
			return reCanonPreDotted.ReplaceAllString(canon, "_$1$2")
		}
		return strings.Replace(canon, "~", "_", 1)
	case pkgPacman:
		return strings.Replace(canon, "~", "", 1)
	}
	return canon
}
