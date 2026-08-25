package services

import "strings"

// Multi-Distro-Paketverwaltung. LCM erkennt beim Scan die Paketverwaltung
// des Zielsystems (Server.PackageManager) und wählt hier die passenden
// Kommandos: Debian/Ubuntu über apt (siehe aptcmd.go), die RHEL-Familie
// (RHEL, CentOS Stream, Rocky, AlmaLinux, Fedora) über dnf/yum, SUSE über
// zypper, Arch über pacman und Alpine über apk. dnf und yum teilen sich die
// Syntax - es genügt, den erkannten Binärnamen einzusetzen.

// Paketverwaltungs-Kennungen (Wert von Server.PackageManager).
const (
	pkgApt    = "apt"
	pkgDnf    = "dnf"
	pkgYum    = "yum"
	pkgZypper = "zypper"
	pkgPacman = "pacman"
	pkgApk    = "apk"
)

// supportedPackageManagers sind die Paketverwaltungen, für die LCM vollständige
// Kommandos besitzt (Bestandsaufnahme, Updates, Security-Updates, Quellen).
//
// pacman und apk kennen KEINEN reinen Security-Kanal - dort ist ein
// Sicherheitsupdate technisch ein vollständiges Systemupdate (siehe
// pkgSecurityUpgradeScript). pacman kennt zudem keine Versions-Fixierung aus
// den Repos (rolling release); das bildet pkgInstallVersionScript ehrlich ab.
var supportedPackageManagers = map[string]bool{
	pkgApt: true, pkgDnf: true, pkgYum: true, pkgZypper: true,
	pkgPacman: true, pkgApk: true,
}

// PackageManagerSupported meldet, ob LCM diese Paketverwaltung bedienen kann.
// Alles andere - auch der leere Wert (nichts erkannt) - ist nicht unterstützt.
func PackageManagerSupported(mgr string) bool { return supportedPackageManagers[mgr] }

// PackageManagerLabel liefert eine anzeigbare Bezeichnung für Meldungen.
func PackageManagerLabel(mgr string) string {
	if mgr == "" {
		return "keine bekannte Paketverwaltung gefunden"
	}
	return mgr
}

// pkgFamily fasst die Binärvarianten zur bedienenden Familie zusammen
// (dnf/yum → dnf). apt ist der Default für den - nach dem Join-Guard
// (PackageManagerSupported) eigentlich unerreichbaren - Unbekannt-Fall.
//
// WICHTIG: Jeder switch auf pkgFamily MUSS pacman und apk explizit behandeln.
// Fiele einer davon auf `default: apt`, liefe auf einem Arch-/Alpine-System
// apt-get und jede Aktion endete mit exit 127 (der stille Fallback aus
// BUG-012).
func pkgFamily(mgr string) string {
	switch mgr {
	case pkgDnf, pkgYum:
		return pkgDnf
	case pkgZypper:
		return pkgZypper
	case pkgPacman:
		return pkgPacman
	case pkgApk:
		return pkgApk
	default:
		return pkgApt
	}
}

// dnfBin liefert das konkrete Binary der RHEL-Familie (dnf oder yum).
func dnfBin(mgr string) string {
	if mgr == pkgYum {
		return pkgYum
	}
	return pkgDnf
}

// pkgInstallCmd liefert das reine Installations-Kommando je Paketverwaltung
// (ohne Paketnamen) - z.B. "apt-get install -y". Basis für pkgInstallScript und
// für dynamische Paketnamen (Shell-Variablen).
func pkgInstallCmd(mgr string) string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		return dnfBin(mgr) + " install -y"
	case pkgZypper:
		return "zypper --non-interactive install"
	case pkgPacman:
		return "pacman -S --noconfirm --needed"
	case pkgApk:
		return "apk add"
	default:
		return "DEBIAN_FRONTEND=noninteractive apt-get install -y"
	}
}

// pkgRefreshCmd liefert das (best-effort) Metadaten-Refresh-Kommando je
// Paketverwaltung inklusive abschließendem "; " - leer für dnf (macht das selbst).
func pkgRefreshCmd(mgr string) string {
	switch pkgFamily(mgr) {
	case pkgZypper:
		return "zypper --non-interactive refresh >/dev/null 2>&1 || true; "
	case pkgPacman:
		return "pacman -Sy >/dev/null 2>&1 || true; "
	case pkgApk:
		return "apk update >/dev/null 2>&1 || true; "
	case pkgDnf:
		return ""
	default:
		return "apt-get update >/dev/null 2>&1 || true; "
	}
}

// pkgInstallScript installiert die angegebenen Pakete je Paketverwaltung
// best-effort - jedes Paket einzeln, damit ein auf der Distribution nicht
// verfügbares Paket (z. B. needrestart auf Alpine) die anderen nicht blockiert.
// Braucht root (läuft über privRun).
func pkgInstallScript(mgr string, pkgs []string) string {
	list := strings.Join(pkgs, " ")
	inst := pkgInstallCmd(mgr)
	return pkgRefreshCmd(mgr) + "for p in " + list + "; do " +
		"if " + inst + " \"$p\" >/dev/null 2>&1; then echo \"LCM: $p installiert\"; " +
		"else echo \"LCM: $p nicht verfuegbar oder Installation fehlgeschlagen (evtl. nicht im Repo dieser Distribution)\"; fi; done"
}

// pkgUpgradeAllScript aktualisiert alle Pakete.
func pkgUpgradeAllScript(mgr string) string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		return dnfBin(mgr) + " -y --refresh upgrade"
	case pkgZypper:
		return "zypper --non-interactive refresh && zypper --non-interactive update"
	case pkgPacman:
		return "pacman -Syu --noconfirm"
	case pkgApk:
		return "apk update && apk upgrade"
	default:
		return aptUpgradeAllScript()
	}
}

// pkgRefreshScript frischt nur die Metadaten auf (installiert nichts).
func pkgRefreshScript(mgr string) string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		return dnfBin(mgr) + " -y makecache"
	case pkgZypper:
		return "zypper --non-interactive refresh"
	case pkgPacman:
		return "pacman -Sy"
	case pkgApk:
		return "apk update"
	default:
		return aptRefreshScript()
	}
}

// pkgSecurityUpgradeScript spielt nur Security-Updates ein - soweit die
// Paketverwaltung einen eigenen Security-Kanal kennt. pacman und apk kennen
// keinen: dort ist die einzige Möglichkeit, Sicherheitslücken zu schließen,
// das vollständige Systemupdate (pkgUpgradeAllScript). Das ist bewusst so und
// wird in der UI/Doku als solches benannt - nicht als „nur Security".
func pkgSecurityUpgradeScript(mgr string) string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		return dnfBin(mgr) + " -y --refresh --security upgrade"
	case pkgZypper:
		// Erst auffrischen: Ohne das entscheidet zypper allein über seinen
		// Autorefresh, ob es die Repos neu liest - ein gerade veröffentlichter
		// Patch kann durchrutschen, während der Lauf „erfolgreich" meldet.
		// Das Voll-Update refresht ausdrücklich; der Security-Lauf tat es nicht.
		//
		// zypper patch nutzt Sonder-Exitcodes (100 = weitere Patches, 102/103
		// = Reboot/Restart nötig) - die werten wir als Erfolg.
		return "zypper --non-interactive refresh >/dev/null 2>&1 || true; " +
			"zypper --non-interactive patch --category security; rc=$?; " +
			"[ $rc -eq 0 -o $rc -eq 100 -o $rc -eq 101 -o $rc -eq 102 -o $rc -eq 103 ] && exit 0 || exit $rc"
	case pkgPacman, pkgApk:
		// Kein reiner Security-Kanal → vollständiges Update.
		return pkgUpgradeAllScript(mgr)
	default:
		return aptSecurityUpgradeScript()
	}
}

// pkgUpgradePackagesScript aktualisiert gezielt die genannten Pakete.
func pkgUpgradePackagesScript(mgr string, names []string) string {
	joined := strings.Join(names, " ")
	switch pkgFamily(mgr) {
	case pkgDnf:
		// --refresh wie beim Voll-Update: Ohne das arbeitet dnf innerhalb von
		// `metadata_expire` (je nach Repo Stunden bis Tage) aus dem
		// Zwischenspeicher und meldet „nichts zu tun", obwohl ein Update
		// bereitsteht.
		return dnfBin(mgr) + " -y --refresh upgrade " + joined
	case pkgZypper:
		return "zypper --non-interactive refresh >/dev/null 2>&1 || true; " +
			"zypper --non-interactive update " + joined
	case pkgPacman:
		// BEWUSST ohne `-y`: Auf einem Rolling Release ist `pacman -Sy <paket>`
		// der klassische Weg in ein halb aktualisiertes System - das Paket
		// käme aus der frischen Datenbank, seine Abhängigkeiten blieben alt.
		// Wer auf Arch gezielt aktualisieren will, aktualisiert alles; dafür
		// gibt es die Regel „Update (alle Pakete)".
		return "pacman -S --noconfirm " + joined
	case pkgApk:
		return "apk update && apk add --upgrade " + joined
	default:
		return aptUpgradePackagesScript(names)
	}
}

// pkgAutoremoveScript entfernt nicht mehr benötigte Pakete (verwaiste
// Abhängigkeiten) je Paketverwaltung.
//
//   - apt/dnf/yum kennen einen echten `autoremove`-Befehl.
//   - zypper hat keinen: die Kandidaten liefert `zypper packages --unneeded`,
//     die wir auslesen und mit `remove --clean-deps` entfernen. Ohne Treffer
//     ist es ein sauberer No-op.
//   - pacman entfernt „orphans" (nicht mehr benötigt UND nicht explizit
//     installiert): `pacman -Qdtq` liefert sie, `-Rns` entfernt sie samt ihrer
//     eigenen nicht mehr gebrauchten Abhängigkeiten. Ohne Treffer No-op.
//   - apk hat KEINEN autoremove-Begriff: Abhängigkeiten werden beim
//     Deinstallieren automatisch mit entfernt. Ehrliche No-op-Meldung.
func pkgAutoremoveScript(mgr string) string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		return dnfBin(mgr) + " -y autoremove"
	case pkgZypper:
		return "pkgs=$(zypper --non-interactive packages --unneeded 2>/dev/null | " +
			"awk -F'|' 'NR>2 && $1 ~ /i/ {gsub(/^[ \\t]+|[ \\t]+$/,\"\",$3); if ($3!=\"\" && $3!=\"Name\") print $3}' | sort -u); " +
			"if [ -n \"$pkgs\" ]; then zypper --non-interactive remove --clean-deps $pkgs; " +
			"else echo 'LCM: keine nicht mehr benötigten Pakete gefunden'; fi"
	case pkgPacman:
		return "orphans=$(pacman -Qdtq 2>/dev/null); " +
			"if [ -n \"$orphans\" ]; then pacman -Rns --noconfirm $orphans; " +
			"else echo 'LCM: keine verwaisten Pakete'; fi"
	case pkgApk:
		return "echo 'LCM: apk entfernt nicht mehr benötigte Abhängigkeiten automatisch beim Deinstallieren - einen separaten autoremove-Befehl gibt es nicht'"
	default:
		return aptAutoremoveScript()
	}
}

// protectedPackageExact sind Paketnamen, deren gezieltes Entfernen LCM
// grundsätzlich ablehnt: das SSH-Login (Aussperren), sudo/die Init (LCM
// verlöre die Rechte bzw. das System bootet nicht mehr) und die
// Paketverwaltung selbst (danach wäre der Server über LCM nicht mehr
// reparierbar). Die Liste ist distributionsübergreifend gehalten.
var protectedPackageExact = map[string]bool{
	// SSH-Server (Verlust = Aussperren).
	"openssh-server": true, "openssh": true, "openssh-server-common": true,
	// Rechte & Init.
	"sudo": true, "systemd": true, "systemd-sysv": true, "init": true,
	"sysvinit-core": true, "openrc": true, "busybox": true,
	// Paketverwaltung selbst.
	"apt": true, "apt-utils": true, "dpkg": true,
	"dnf": true, "yum": true, "rpm": true, "libdnf": true,
	"zypper": true, "libzypp": true,
	"pacman": true, "apk-tools": true,
	// Basis-Bibliotheken & Shell (Entfernen zerlegt das System).
	"libc6": true, "glibc": true, "musl": true,
	"bash": true, "dash": true, "coreutils": true,
}

// protectedPackagePrefixes fängt Kernel-Pakete (und ihre versionierten
// Varianten) über den Präfix ab - sie versioniert einzeln zu listen wäre
// unmöglich (linux-image-6.1.0-…). Ein entfernter laufender Kernel macht den
// Server beim nächsten Boot unbrauchbar.
var protectedPackagePrefixes = []string{
	"linux-image", "linux-headers", "linux-generic", "linux-kernel",
	"kernel-core", "kernel-default", "kernel-modules", "linux-lts",
}

// isProtectedPackage meldet, ob ein Paket geschützt ist (siehe die beiden
// Listen). Der Name ist bereits kleingeschrieben und validiert.
func isProtectedPackage(name string) bool {
	if protectedPackageExact[name] {
		return true
	}
	for _, p := range protectedPackagePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// pkgRemovePackagesScript deinstalliert gezielt die genannten Pakete je
// Paketverwaltung (die Namen sind vorvalidiert und shell-sicher). Bewusst
// ohne Kaskaden-/Purge-Optionen - es werden GENAU die genannten Pakete
// entfernt, damit die Aktion vorhersehbar bleibt (verwaiste Abhängigkeiten
// räumt danach separat der Autoremove-Lauf ab).
func pkgRemovePackagesScript(mgr string, names []string) string {
	joined := strings.Join(names, " ")
	switch pkgFamily(mgr) {
	case pkgDnf:
		return dnfBin(mgr) + " -y remove " + joined
	case pkgZypper:
		return "zypper --non-interactive remove " + joined
	case pkgPacman:
		return "pacman -R --noconfirm " + joined
	case pkgApk:
		return "apk del " + joined
	default:
		return aptRemovePackagesScript(names)
	}
}

// pkgInstallVersionScript installiert ein Paket auf eine exakte Version
// (Downgrades erlaubt, soweit die Paketverwaltung das kann).
//
// pacman kann das NICHT: ein Rolling-Release-Repo führt immer nur die eine
// aktuelle Version, ältere sind ohne Archiv-Tooling nicht erreichbar. Statt
// stillschweigend die neueste Version zu ziehen (und eine Fixierung
// vorzutäuschen), bricht LCM hier mit einer klaren Meldung ab.
func pkgInstallVersionScript(mgr, name, version string) string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		spec := name + "-" + version
		return dnfBin(mgr) + " -y install " + spec + " || " + dnfBin(mgr) + " -y downgrade " + spec
	case pkgZypper:
		return "zypper --non-interactive install --oldpackage " + name + "-" + version
	case pkgPacman:
		return "echo 'LCM: pacman kennt keine Versions-Fixierung aus den Repos (Rolling Release) - bitte das gesamte System aktualisieren'; exit 1"
	case pkgApk:
		return "apk update && apk add " + name + "=" + version
	default:
		return aptInstallVersionScript(name, version)
	}
}

// scriptForRule liefert das Update-Skript eines paketbezogenen Rule-Typs für
// die Paketverwaltung mgr. Der zweite Rückgabewert ist false für
// nicht-paketbezogene Typen.
func scriptForRule(mgr, ruleType, command string) (string, bool) {
	switch ruleType {
	case "update":
		return pkgUpgradeAllScript(mgr), true
	case "package-scan":
		return pkgRefreshScript(mgr), true
	case "security":
		return pkgSecurityUpgradeScript(mgr), true
	case "packages":
		names, err := parsePackageNames(command)
		if err != nil {
			return "echo 'LCM: keine gültigen paketnamen konfiguriert'; exit 1", true
		}
		return pkgUpgradePackagesScript(mgr, names), true
	case "autoremove":
		return pkgAutoremoveScript(mgr), true
	}
	return "", false
}

// pkgVersionsCommand liefert das Kommando zum Auflisten der installierbaren
// Versionen eines Pakets (für die versionsgenaue Auswahl in der UI).
//
// pacman liefert nur die eine aktuelle Repo-Version (rolling) - daher `-Si`.
func pkgVersionsCommand(mgr, name string) string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		return dnfBin(mgr) + " -q --showduplicates list " + name
	case pkgZypper:
		return "zypper --non-interactive search -s " + name
	case pkgPacman:
		return "pacman -Si " + name + " 2>/dev/null"
	case pkgApk:
		return "apk policy " + name + " 2>/dev/null"
	default:
		return "apt-cache madison " + name
	}
}

// parsePkgVersions extrahiert die Versionen aus der Ausgabe von
// pkgVersionsCommand (neueste zuerst, dedupliziert).
func parsePkgVersions(mgr, name, out string) []string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		return parseDnfVersions(out)
	case pkgZypper:
		return parseZypperVersions(name, out)
	case pkgPacman:
		return parsePacmanVersions(out)
	case pkgApk:
		return parseApkVersions(out)
	default:
		return parseMadison(out)
	}
}

// parseDnfVersions parst `dnf --showduplicates list`. Zeilenformat:
// "name.arch   [epoch:]version-release   repo". Die mittlere Spalte ist die
// Version; Kopf-/Abschnittszeilen ("Installed Packages", …) werden übersprungen.
func parseDnfVersions(out string) []string {
	seen := map[string]bool{}
	var versions []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// Erste Spalte muss "name.arch" sein (enthält einen Punkt), sonst
		// ist es eine Abschnittsüberschrift.
		if !strings.Contains(fields[0], ".") {
			continue
		}
		v := fields[1]
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		versions = append(versions, v)
	}
	return versions
}

// parseZypperVersions parst `zypper search -s`. Pipe-getrennte Tabelle:
// "S | Name | Type | Version | Arch | Repository". Nur package-Zeilen des
// gesuchten Pakets zählen; die Version ist Spalte 3 (0-basiert).
func parseZypperVersions(name, out string) []string {
	seen := map[string]bool{}
	var versions []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "|") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 4 {
			continue
		}
		if strings.TrimSpace(cols[1]) != name {
			continue
		}
		if t := strings.TrimSpace(cols[2]); t != "" && t != "package" {
			continue
		}
		v := strings.TrimSpace(cols[3])
		if v == "" || v == "Version" || seen[v] {
			continue
		}
		seen[v] = true
		versions = append(versions, v)
	}
	return versions
}

// parsePacmanVersions liest die eine verfügbare Version aus `pacman -Si`
// (Zeile "Version         : 1.2.3-1"). Rolling Release → in aller Regel genau
// ein Eintrag.
func parsePacmanVersions(out string) []string {
	var versions []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "Version" {
			continue
		}
		v := strings.TrimSpace(val)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		versions = append(versions, v)
	}
	return versions
}

// parseApkVersions liest die verfügbaren Versionen aus `apk policy <name>`.
// Ausgabe: Paketname, dann je Version eine eingerückte Zeile "X.Y.Z-rN:"
// gefolgt von den Quellen. apk listet aufsteigend - wir drehen auf
// „neueste zuerst".
func parseApkVersions(out string) []string {
	var versions []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		// Versionszeilen sind eingerückt (führendes Leerzeichen) und enden auf
		// ":". Die Paket-Kopfzeile ("name policy:") ist nicht eingerückt.
		if line == t || !strings.HasSuffix(t, ":") {
			continue
		}
		v := strings.TrimSuffix(t, ":")
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		versions = append(versions, v)
	}
	// aufsteigend → absteigend
	for i, j := 0, len(versions)-1; i < j; i, j = i+1, j-1 {
		versions[i], versions[j] = versions[j], versions[i]
	}
	return versions
}
