package services

import (
	"fmt"
	"sort"
	"strings"

	"LCM/internal/core/domain"
)

// Paket-Pins auf dem Zielsystem durchsetzen.
//
// Der Autoremove-Lauf entfernt alles, was keine andere Abhaengigkeit mehr
// braucht - darunter regelmaessig aeltere Kernel. Wer (wie Proxmox es
// vormacht) mehrere Kernel als Rueckfallebene behalten will, braucht dafuer
// eine ausdrueckliche Schutzliste. Genau die schreibt LCM hier.
//
// Zwei Wirkungen, bewusst getrennt (siehe domain.PackagePin):
//
//   - NoRemove: darf nicht entfernt werden, bekommt aber weiter Updates.
//   - Hold: die installierte Version wird eingefroren (keine Updates mehr).
//
// Je Paketverwaltung sind das UNTERSCHIEDLICHE Mechanismen - und nicht jede
// kennt beide. Wo eine Wirkung fehlt, sagt LCM das im Job-Output, statt
// stillschweigend Schutz vorzutaeuschen.

const (
	// aptPinFile ist das LCM-Drop-in fuer APT::NeverAutoRemove. Eigene Datei,
	// damit ein erneutes Anwenden sie vollstaendig ersetzen kann, ohne fremde
	// Konfiguration anzufassen.
	aptPinFile = "/etc/apt/apt.conf.d/99lcm-package-pins"
	// dnfPinFile nutzt die protected_packages-Mechanik von dnf/yum: Pakete in
	// /etc/dnf/protected.d/*.conf lehnt dnf beim Entfernen ab.
	dnfPinFile = "/etc/dnf/protected.d/lcm-package-pins.conf"
	// pacmanPinFile wird von /etc/pacman.conf per Include eingebunden.
	pacmanPinFile = "/etc/pacman.d/99-lcm-package-pins.conf"
)

// pinNames liefert die Namen der Pins mit der gewuenschten Wirkung, sortiert
// und ohne Dubletten (globale und serverspezifische Pins koennen sich
// ueberschneiden).
func pinNames(pins []domain.PackagePin, hold bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range pins {
		name := strings.TrimSpace(p.Name)
		if name == "" || seen[name] {
			continue
		}
		if (hold && !p.Hold) || (!hold && !p.NoRemove) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// pinRegex uebersetzt einen Pin-Namen in einen verankerten regulaeren
// Ausdruck, wie ihn APT::NeverAutoRemove erwartet. Ein Praefix-Muster
// ("linux-image-*") wird zu "^linux-image-.*$", ein exakter Name zu
// "^name$". Regex-Sonderzeichen im Namen werden maskiert - Paketnamen duerfen
// Punkte und Pluszeichen enthalten (g++, libstdc++6, python3.11).
func pinRegex(name string) string {
	base := strings.TrimSuffix(name, "*")
	var b strings.Builder
	for _, r := range base {
		if strings.ContainsRune(`.+*?()[]{}^$|\`, r) {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	if strings.HasSuffix(name, "*") {
		return "^" + b.String() + ".*$"
	}
	return "^" + b.String() + "$"
}

// pinShellPattern liefert das Glob-Muster fuer die Shell-Auswertung auf dem
// Ziel (dort, wo konkrete Paketnamen aus dem Bestand gefiltert werden).
func pinShellPattern(name string) string {
	if strings.HasSuffix(name, "*") {
		return strings.TrimSuffix(name, "*") + "*"
	}
	return name
}

// pinExpandCmd baut einen Shell-Ausdruck, der die Muster gegen die tatsaechlich
// installierten Pakete aufloest. Braucht es ueberall dort, wo die
// Paketverwaltung nur konkrete Namen akzeptiert (apt-mark, zypper, apk).
//
// listCmd muss die installierten Paketnamen zeilenweise ausgeben.
func pinExpandCmd(listCmd string, names []string) string {
	var cases []string
	for _, n := range names {
		cases = append(cases, pinShellPattern(n))
	}
	// case-Muster mit | verknuepft: ein Durchlauf ueber den Bestand genuegt.
	return "for p in $(" + listCmd + "); do case \"$p\" in " +
		strings.Join(cases, "|") + ") echo \"$p\";; esac; done"
}

// pkgPinScript schreibt die Pins auf dem Ziel fest. Braucht root (privRun).
//
// Das Skript ist idempotent: Es ersetzt die LCM-Dateien vollstaendig und
// setzt vorher die von LCM gesetzten Holds zurueck. Wer einen Pin in LCM
// entfernt, bekommt so beim naechsten Anwenden auch auf dem Server wieder
// einen ungeschuetzten Zustand - sonst blieben Leichen zurueck, die niemand
// mehr zuordnen kann.
func pkgPinScript(mgr string, pins []domain.PackagePin) string {
	noRemove := pinNames(pins, false)
	hold := pinNames(pins, true)

	switch pkgFamily(mgr) {
	case pkgApt:
		return aptPinScript(noRemove, hold)
	case pkgDnf:
		return dnfPinScript(mgr, noRemove, hold)
	case pkgZypper:
		return zypperPinScript(noRemove, hold)
	case pkgPacman:
		return pacmanPinScript(noRemove, hold)
	case pkgApk:
		return apkPinScript(noRemove, hold)
	}
	return "echo 'LCM: Paket-Pins werden fuer diese Paketverwaltung nicht unterstuetzt'"
}

// aptPinScript: APT::NeverAutoRemove schuetzt vor autoremove (Updates laufen
// weiter), `apt-mark hold` friert die Version ein.
func aptPinScript(noRemove, hold []string) string {
	steps := []string{"install -d -m 755 /etc/apt/apt.conf.d"}
	if len(noRemove) == 0 {
		steps = append(steps, "rm -f "+aptPinFile)
	} else {
		var entries strings.Builder
		for _, n := range noRemove {
			entries.WriteString("  \"" + pinRegex(n) + "\";\\n")
		}
		steps = append(steps,
			fmt.Sprintf("printf '// von LCM verwaltet - nicht von Hand aendern\\nAPT::NeverAutoRemove\\n{\\n%s};\\n' > %s", entries.String(), aptPinFile),
			fmt.Sprintf("echo 'LCM: %d Paket-Muster vor autoremove geschuetzt (%s)'", len(noRemove), aptPinFile),
		)
	}
	// Holds: erst ALLE bestehenden loesen, dann die gewuenschten setzen -
	// sonst bliebe ein entfernter Pin auf dem Server aktiv.
	steps = append(steps, "apt-mark unhold $(apt-mark showhold) >/dev/null 2>&1 || true")
	if len(hold) > 0 {
		steps = append(steps,
			"hold=$("+pinExpandCmd("dpkg-query -W -f '${Package}\\n' 2>/dev/null", hold)+")",
			"[ -n \"$hold\" ] && apt-mark hold $hold || echo 'LCM: keine installierten Pakete passen auf die Hold-Pins'",
		)
	}
	return strings.Join(steps, "\n")
}

// dnfPinScript: /etc/dnf/protected.d schuetzt vor dem Entfernen,
// `versionlock` friert die Version ein (Plugin, wird bei Bedarf nachinstalliert).
//
// Hinweis: Wie viele Kernel dnf behaelt, steuert zusaetzlich installonly_limit
// in dnf.conf (Standard 3) - der Pin wirkt ergaenzend.
func dnfPinScript(mgr string, noRemove, hold []string) string {
	bin := dnfBin(mgr)
	steps := []string{"install -d -m 755 /etc/dnf/protected.d"}
	if len(noRemove) == 0 {
		steps = append(steps, "rm -f "+dnfPinFile)
	} else {
		// protected.d nimmt je Zeile einen Paketnamen; Muster loesen wir
		// gegen den installierten Bestand auf.
		steps = append(steps,
			": > "+dnfPinFile,
			pinExpandCmd("rpm -qa --qf '%{NAME}\\n' 2>/dev/null | sort -u", noRemove)+" >> "+dnfPinFile,
			fmt.Sprintf("echo \"LCM: $(wc -l < %s) Pakete vor dem Entfernen geschuetzt (%s)\"", dnfPinFile, dnfPinFile),
		)
	}
	if len(hold) > 0 {
		steps = append(steps,
			bin+" -y install 'dnf-command(versionlock)' >/dev/null 2>&1 || "+bin+" -y install python3-dnf-plugin-versionlock >/dev/null 2>&1 || true",
			bin+" versionlock clear >/dev/null 2>&1 || true",
			"lock=$("+pinExpandCmd("rpm -qa --qf '%{NAME}\\n' 2>/dev/null | sort -u", hold)+")",
			"[ -n \"$lock\" ] && "+bin+" versionlock add $lock || echo 'LCM: keine installierten Pakete passen auf die Hold-Pins'",
		)
	} else {
		steps = append(steps, bin+" versionlock clear >/dev/null 2>&1 || true")
	}
	return strings.Join(steps, "\n")
}

// zypperPinScript: zypper kennt nur EIN Schloss (`zypper addlock`). Es
// verhindert Entfernen UND Aktualisieren. Fuer NoRemove-Pins waere das zu
// scharf (keine Sicherheitsupdates mehr), deshalb:
//
//   - Hold-Pins  -> echtes Schloss (addlock).
//   - NoRemove   -> kein Schloss; stattdessen wird der Autoremove-Lauf um
//     diese Pakete gekuerzt (siehe pkgAutoremoveScriptWithPins).
//
// Das ist die ehrliche Abbildung: Wir behaupten keinen Schutz, den zypper in
// dieser Form nicht kennt.
func zypperPinScript(noRemove, hold []string) string {
	steps := []string{"zypper --non-interactive removelock '*' >/dev/null 2>&1 || true"}
	if len(hold) > 0 {
		steps = append(steps,
			"lock=$("+pinExpandCmd("rpm -qa --qf '%{NAME}\\n' 2>/dev/null | sort -u", hold)+")",
			"[ -n \"$lock\" ] && zypper --non-interactive addlock $lock || echo 'LCM: keine installierten Pakete passen auf die Hold-Pins'",
		)
	}
	if len(noRemove) > 0 {
		steps = append(steps, fmt.Sprintf(
			"echo 'LCM: %d Schutz-Pins werden bei zypper beim Aufraeumen ausgespart (zypper kennt keine reine Entfernsperre)'", len(noRemove)))
	}
	return strings.Join(steps, "\n")
}

// pacmanPinScript: pacman trennt beides sauber -
//
//	HoldPkg   = Entfernen wird verweigert  -> NoRemove
//	IgnorePkg = keine Upgrades             -> Hold
//
// Beide stehen in einer eigenen LCM-Datei, die per Include aus pacman.conf
// gezogen wird. Das Include wird bei Bedarf einmalig ergaenzt.
func pacmanPinScript(noRemove, hold []string) string {
	steps := []string{"install -d -m 755 /etc/pacman.d"}
	if len(noRemove) == 0 && len(hold) == 0 {
		return strings.Join(append(steps, "rm -f "+pacmanPinFile,
			"echo 'LCM: keine Paket-Pins gesetzt'"), "\n")
	}
	lines := []string{"# von LCM verwaltet - nicht von Hand aendern", "[options]"}
	if len(noRemove) > 0 {
		lines = append(lines, "HoldPkg = "+strings.Join(expandStars(noRemove), " "))
	}
	if len(hold) > 0 {
		lines = append(lines, "IgnorePkg = "+strings.Join(expandStars(hold), " "))
	}
	steps = append(steps,
		fmt.Sprintf("printf '%s\\n' > %s", strings.Join(lines, "\\n"), pacmanPinFile),
		// Include nur einmal anhaengen (idempotent).
		fmt.Sprintf("grep -q '%s' /etc/pacman.conf 2>/dev/null || printf '\\nInclude = %s\\n' >> /etc/pacman.conf", pacmanPinFile, pacmanPinFile),
		fmt.Sprintf("echo 'LCM: Paket-Pins geschrieben (%s)'", pacmanPinFile),
	)
	return strings.Join(steps, "\n")
}

// expandStars macht aus LCM-Mustern pacman-Glob-Muster (pacman versteht in
// HoldPkg/IgnorePkg Shell-Globs).
func expandStars(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, pinShellPattern(n))
	}
	return out
}

// apkPinScript: apk kennt kein Autoremove (Abhaengigkeiten verschwinden nur
// beim gezielten Deinstallieren), eine reine Entfernsperre gibt es daher nicht.
// Fuer Hold kennt apk-tools ab 2.14 `apk hold`; aeltere Versionen nicht -
// dann sagt LCM das offen, statt Schutz zu behaupten.
func apkPinScript(noRemove, hold []string) string {
	steps := []string{}
	if len(noRemove) > 0 {
		steps = append(steps, fmt.Sprintf(
			"echo 'LCM: apk kennt keinen Autoremove und damit keine Entfernsperre - die %d Schutz-Pins bleiben ohne Wirkung'", len(noRemove)))
	}
	if len(hold) > 0 {
		steps = append(steps,
			"if apk hold --help >/dev/null 2>&1; then",
			"  apk unhold $(apk list --installed 2>/dev/null | awk '{print $1}') >/dev/null 2>&1 || true",
			"  h=$("+pinExpandCmd("apk info 2>/dev/null", hold)+")",
			"  [ -n \"$h\" ] && apk hold $h || echo 'LCM: keine installierten Pakete passen auf die Hold-Pins'",
			"else",
			"  echo 'LCM: dieses apk kennt kein hold - die Versions-Fixierung wurde NICHT gesetzt'",
			"fi",
		)
	}
	if len(steps) == 0 {
		steps = append(steps, "echo 'LCM: keine Paket-Pins fuer apk'")
	}
	return strings.Join(steps, "\n")
}

// pkgAutoremoveScriptWithPins baut den Autoremove-Lauf inklusive Pin-Schutz.
//
// Bei apt und dnf reicht die zuvor geschriebene Schutzdatei - die
// Paketverwaltung respektiert sie selbst. zypper und pacman kennen keine
// solche Datei fuer den Aufraeum-Lauf, dort wird die Kandidatenliste
// stattdessen um die geschuetzten Pakete gekuerzt.
func pkgAutoremoveScriptWithPins(mgr string, pins []domain.PackagePin) string {
	noRemove := pinNames(pins, false)
	base := pkgAutoremoveScript(mgr)
	if len(noRemove) == 0 {
		return base
	}
	switch pkgFamily(mgr) {
	case pkgZypper:
		return "pkgs=$(zypper --non-interactive packages --unneeded 2>/dev/null | " +
			"awk -F'|' 'NR>2 && $1 ~ /i/ {gsub(/^[ \\t]+|[ \\t]+$/,\"\",$3); if ($3!=\"\" && $3!=\"Name\") print $3}' | sort -u); " +
			"keep=''; for p in $pkgs; do case \"$p\" in " + strings.Join(expandStars(noRemove), "|") +
			") echo \"LCM: $p ist gepinnt - bleibt erhalten\";; *) keep=\"$keep $p\";; esac; done; " +
			"if [ -n \"$keep\" ]; then zypper --non-interactive remove --clean-deps $keep; " +
			"else echo 'LCM: keine nicht mehr benoetigten Pakete zum Entfernen'; fi"
	case pkgPacman:
		return "orphans=$(pacman -Qdtq 2>/dev/null); " +
			"keep=''; for p in $orphans; do case \"$p\" in " + strings.Join(expandStars(noRemove), "|") +
			") echo \"LCM: $p ist gepinnt - bleibt erhalten\";; *) keep=\"$keep $p\";; esac; done; " +
			"if [ -n \"$keep\" ]; then pacman -Rns --noconfirm $keep; " +
			"else echo 'LCM: keine verwaisten Pakete zum Entfernen'; fi"
	}
	return base
}
