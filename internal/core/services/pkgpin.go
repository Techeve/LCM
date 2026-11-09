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
		// Ältere Einträge können Namen tragen, die heute abgelehnt werden: Sie
		// sind kein Paket, und der Helper verwürfe ihretwegen alle Pins.
		if _, err := validPinName(name); err != nil {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
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
//
// restricted: Im eingeschraenkten Modus gibt es keine Root-Shell. Die Schritte,
// die unter /etc schreiben oder apt-mark brauchen (pinRootStep), laufen dann
// ueber das Helper-Unterkommando package-pins; alles uebrige geht ueber
// Paketverwaltungen, die auf der sudo-Whitelist stehen.
func pkgPinScript(restricted bool, mgr string, pins []domain.PackagePin) string {
	noRemove := pinNames(pins, false)
	hold := pinNames(pins, true)

	switch pkgFamily(mgr) {
	case pkgApt:
		return pinRootStep(restricted, pkgApt, noRemove, hold)
	case pkgDnf:
		return dnfPinScript(restricted, mgr, noRemove, hold)
	case pkgZypper:
		return zypperPinScript(noRemove, hold)
	case pkgPacman:
		return pinRootStep(restricted, pkgPacman, noRemove, hold)
	case pkgApk:
		return apkPinScript(noRemove, hold)
	}
	return "echo 'LCM: Paket-Pins werden fuer diese Paketverwaltung nicht unterstuetzt'"
}

// Root-Schritte der Pin-Skripte. Es gibt sie einmal, mit den Pin-Listen als
// Shell-Variablen $NOREMOVE und $HOLD (Leerzeichen-getrennte Muster):
// im Voll-Modus setzt LCM die Variablen vor den Rumpf, im eingeschraenkten Modus
// traegt der Helper (package-pins) denselben Rumpf und setzt sie aus seinen
// geprueften Parametern. Die Muster duerfen `*` enthalten - set -f verhindert,
// dass die Shell sie gegen Dateinamen aufloest; die Unterschale haelt das
// aus dem restlichen Skript heraus.
const (
	// APT::NeverAutoRemove schuetzt vor autoremove (Updates laufen weiter),
	// `apt-mark hold` friert die Version ein. Die Muster werden zu verankerten
	// Regexen: Namen duerfen nur [a-z0-9._+-] enthalten, also
	// sind `.` und `+` die einzigen zu maskierenden Zeichen.
	pinAptBody = `( set -f
install -d -m 755 /etc/apt/apt.conf.d
if [ -z "$NOREMOVE" ]; then
  rm -f ` + aptPinFile + `
else
  { printf '// von LCM verwaltet - nicht von Hand aendern\nAPT::NeverAutoRemove\n{\n'
    for p in $NOREMOVE; do
      esc=$(printf '%s' "${p%\*}" | sed 's/[.+]/\\&/g')
      case "$p" in *\*) printf '  "^%s.*$";\n' "$esc" ;; *) printf '  "^%s$";\n' "$esc" ;; esac
    done
    printf '};\n'
  } > ` + aptPinFile + `
  set -- $NOREMOVE
  echo "LCM: $# Paket-Muster vor autoremove geschuetzt (` + aptPinFile + `)"
fi
apt-mark unhold $(apt-mark showhold) >/dev/null 2>&1 || true
if [ -n "$HOLD" ]; then
  hold=
  for p in $(dpkg-query -W -f '${Package}\n' 2>/dev/null); do
    for pat in $HOLD; do case "$p" in $pat) hold="$hold $p"; break ;; esac; done
  done
  [ -n "$hold" ] && apt-mark hold $hold || echo 'LCM: keine installierten Pakete passen auf die Hold-Pins'
fi )`

	// /etc/dnf/protected.d: dnf lehnt das Entfernen der dort genannten Pakete
	// ab. Muster loesen wir gegen den installierten Bestand auf.
	pinDnfBody = `( set -f
install -d -m 755 /etc/dnf/protected.d
if [ -z "$NOREMOVE" ]; then
  rm -f ` + dnfPinFile + `
else
  : > ` + dnfPinFile + `
  for p in $(rpm -qa --qf '%{NAME}\n' 2>/dev/null | sort -u); do for pat in $NOREMOVE; do case "$p" in $pat) echo "$p" >> ` + dnfPinFile + `; break ;; esac; done; done
  echo "LCM: $(wc -l < ` + dnfPinFile + `) Pakete vor dem Entfernen geschuetzt (` + dnfPinFile + `)"
fi )`

	// pacman trennt beides sauber (HoldPkg = Entfernen wird verweigert,
	// IgnorePkg = keine Upgrades). Beide stehen in einer eigenen LCM-Datei,
	// die per Include aus pacman.conf gezogen wird (einmalig, idempotent).
	pinPacmanBody = `( set -f
install -d -m 755 /etc/pacman.d
if [ -z "$NOREMOVE" ] && [ -z "$HOLD" ]; then
  rm -f ` + pacmanPinFile + `
  echo 'LCM: keine Paket-Pins gesetzt'
else
  { echo '# von LCM verwaltet - nicht von Hand aendern'; echo '[options]'
    [ -z "$NOREMOVE" ] || echo "HoldPkg = $NOREMOVE"
    [ -z "$HOLD" ] || echo "IgnorePkg = $HOLD"
  } > ` + pacmanPinFile + `
  grep -q '` + pacmanPinFile + `' /etc/pacman.conf 2>/dev/null || printf '\nInclude = ` + pacmanPinFile + `\n' >> /etc/pacman.conf
  echo 'LCM: Paket-Pins geschrieben (` + pacmanPinFile + `)'
fi )`
)

// pinRootBodies ordnet dem Wert von package-pins den Rumpf zu.
var pinRootBodies = map[string]string{pkgApt: pinAptBody, pkgDnf: pinDnfBody, pkgPacman: pinPacmanBody}

// pinRootStep liefert den Root-Schritt der Paketverwaltung kind: im Voll-Modus
// den Rumpf samt Variablen, im eingeschraenkten Modus den Helper-Aufruf.
func pinRootStep(restricted bool, kind string, noRemove, hold []string) string {
	if restricted {
		return helperCmd("package-pins", kind, pinCSV(noRemove), pinCSV(hold))
	}
	return shellVars("NOREMOVE", strings.Join(noRemove, " "), "HOLD", strings.Join(hold, " ")) + pinRootBodies[kind]
}

// pinCSV ist die Helper-Schreibweise einer Pin-Liste ("-" = leer).
func pinCSV(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ",")
}

// dnfPinScript: der Schutz vor dem Entfernen ist ein Root-Schritt (siehe
// pinDnfBody), `versionlock` friert die Version ein (Plugin, wird bei Bedarf
// nachinstalliert) und laeuft ueber dnf selbst.
//
// Hinweis: Wie viele Kernel dnf behaelt, steuert zusaetzlich installonly_limit
// in dnf.conf (Standard 3) - der Pin wirkt ergaenzend.
func dnfPinScript(restricted bool, mgr string, noRemove, hold []string) string {
	bin := dnfBin(mgr)
	steps := []string{pinRootStep(restricted, pkgDnf, noRemove, hold)}
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
