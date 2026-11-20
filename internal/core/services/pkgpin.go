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
	// pinHoldState fuehrt die Pakete, die LCM selbst gehalten hat (eins je
	// Zeile). Nur diese gibt ein Abgleich wieder frei; Holds, die ein Admin
	// gesetzt hat, stehen nicht darin und bleiben. Bewusst nicht unter
	// /var/lib/lcm: dort liegen auf dem LCM-Host Datenbank und Master-Key.
	pinHoldState = "/var/lib/lcm-managed/package-holds"
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

// pkgPinScript schreibt die Pins auf dem Ziel fest. Braucht root (privRun).
//
// Das Skript ist idempotent: Es ersetzt die LCM-Dateien vollstaendig und
// gleicht die Holds gegen die Zustandsdatei ab (pinHoldSync). Wer einen Pin
// in LCM entfernt, bekommt so beim naechsten Anwenden auch auf dem Server
// wieder einen ungehaltenen Zustand - Holds, die ein Admin selbst gesetzt
// hat, bleiben dabei unberuehrt.
//
// restricted: Im eingeschraenkten Modus gibt es keine Root-Shell. Die Schritte,
// die unter /etc oder /var/lib schreiben oder Holds setzen (pinRootStep),
// laufen dann ueber das Helper-Unterkommando package-pins; alles uebrige geht
// ueber Paketverwaltungen, die auf der sudo-Whitelist stehen. Ein veralteter
// Helper bricht vorher ab: Seine Fassung gaebe noch alle Holds frei, auch
// die eines Admins.
func pkgPinScript(restricted bool, mgr string, pins []domain.PackagePin) string {
	script := pkgPinSteps(restricted, mgr, pinNames(pins, false), pinNames(pins, true))
	if !restricted {
		return script
	}
	return helperCurrentStep(true) + "\n" + script
}

// pkgPinSteps waehlt die Schritte der Paketverwaltung mgr.
func pkgPinSteps(restricted bool, mgr string, noRemove, hold []string) string {
	switch pkgFamily(mgr) {
	case pkgApt:
		return pinRootStep(restricted, pkgApt, noRemove, hold)
	case pkgDnf:
		return dnfPinScript(restricted, mgr, noRemove, hold)
	case pkgZypper:
		return zypperPinScript(restricted, noRemove, hold)
	case pkgPacman:
		return pinRootStep(restricted, pkgPacman, noRemove, hold)
	case pkgApk:
		return apkPinScript(restricted, noRemove, hold)
	}
	return "echo 'LCM: Paket-Pins werden fuer diese Paketverwaltung nicht unterstuetzt'"
}

// pinHoldSync gleicht die Holds ab, ohne fremde anzufassen. Vorher definiert
// der Rumpf vier Funktionen der Paketverwaltung: pin_installed (installierte
// Namen), pin_held (gehaltene Namen), pin_hold und pin_unhold (Namen als
// Argumente). Die Zustandsdatei fuehrt, was LCM selbst gehalten hat:
//
//   - freigeben = (Zustandsdatei) ohne (gewuenscht), nur was noch gehalten ist
//   - setzen    = gewuenscht und noch nicht gehalten
//   - gewuenscht und schon gehalten, aber nicht in der Zustandsdatei: ein
//     Admin-Hold. Er bleibt und wird NICHT als LCMs vermerkt - sonst gaebe ein
//     spaeteres Entfernen des Pins den Hold des Admins frei.
//
// Uebergang: Fehlt die Zustandsdatei (erster Abgleich nach dem Update), gibt
// LCM nichts frei und uebernimmt nur die Holds, die auf einen heutigen Pin
// passen - die hat die alte Fassung bei jedem Abgleich selbst gesetzt.
// Die Datei wird danach immer geschrieben, auch leer: Ihr Fehlen bedeutet
// "noch nie abgeglichen".
const pinHoldSync = `state=` + pinHoldState + `
rc=0
held=" $(pin_held 2>/dev/null | tr '\n' ' ') "
owned=; seed=
if [ -f "$state" ]; then owned=$(tr '\n' ' ' < "$state"); else seed=1; fi
want=
if [ -n "$HOLD" ]; then
  for p in $(pin_installed 2>/dev/null | sort -u); do
    # $pat ist bewusst ein Glob-Muster (Pins wie linux-image-*).
    # shellcheck disable=SC2254
    for pat in $HOLD; do case "$p" in $pat) want="$want $p"; break ;; esac; done
  done
  [ -n "$want" ] || echo 'LCM: keine installierten Pakete passen auf die Hold-Pins'
fi
mine=; add=; foreign=; release=
for p in $want; do
  case "$held" in
    *" $p "*) case " $owned " in
        *" $p "*) mine="$mine $p" ;;
        *) if [ -n "$seed" ]; then mine="$mine $p"; else foreign="$foreign $p"; fi ;;
      esac ;;
    *) add="$add $p" ;;
  esac
done
for p in $owned; do
  case "$p" in [A-Za-z0-9]*) ;; *) continue ;; esac
  case " $want " in *" $p "*) continue ;; esac
  case "$held" in *" $p "*) release="$release $p" ;; esac
done
if [ -n "$release" ]; then
  if pin_unhold $release; then echo "LCM: Holds geloest:$release"
  else rc=1; mine="$mine $release"; echo "LCM: Holds loesen fehlgeschlagen:$release" >&2; fi
fi
if [ -n "$add" ]; then
  mine="$mine $add"
  if pin_hold $add; then echo "LCM: Holds gesetzt:$add"
  else rc=1; echo "LCM: Holds setzen fehlgeschlagen:$add" >&2; fi
fi
[ -z "$foreign" ] || echo "LCM: schon von Hand gehalten, bleibt Sache des Admins:$foreign"
others=
for p in $held; do
  case " $mine $release $foreign " in *" $p "*) ;; *) others="$others $p" ;; esac
done
[ -z "$others" ] || echo "LCM: fremde Holds bleiben unberuehrt:$others"
install -d -m 755 "${state%/*}" &&
  { for p in $mine; do echo "$p"; done; } > "$state.tmp" &&
  mv -f "$state.tmp" "$state" || { rc=1; echo "LCM: $state nicht geschrieben" >&2; }
exit "$rc"`

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
pin_installed() { dpkg-query -W -f '${Package}\n'; }
pin_held() { apt-mark showhold; }
pin_hold() { apt-mark hold "$@"; }
pin_unhold() { apt-mark unhold "$@"; }
` + pinHoldSync + ` )`

	// /etc/dnf/protected.d: dnf lehnt das Entfernen der dort genannten Pakete
	// ab. Muster loesen wir gegen den installierten Bestand auf. `versionlock`
	// friert die Version ein; `versionlock list` nennt je nach Fassung
	// name-[epoch:]version-release.* (dnf4), epoch:name-version-release.*
	// (yum) oder "Package name: name" (dnf5) - pin_held macht daraus Namen.
	pinDnfBody = `( set -f
install -d -m 755 /etc/dnf/protected.d
if [ -z "$NOREMOVE" ]; then
  rm -f ` + dnfPinFile + `
else
  : > ` + dnfPinFile + `
  # $pat ist bewusst ein Glob-Muster (Pins wie kernel-*).
  # shellcheck disable=SC2254
  for p in $(rpm -qa --qf '%{NAME}\n' 2>/dev/null | sort -u); do for pat in $NOREMOVE; do case "$p" in $pat) echo "$p" >> ` + dnfPinFile + `; break ;; esac; done; done
  echo "LCM: $(wc -l < ` + dnfPinFile + `) Pakete vor dem Entfernen geschuetzt (` + dnfPinFile + `)"
fi
bin=dnf; command -v dnf >/dev/null 2>&1 || bin=yum
pin_installed() { rpm -qa --qf '%{NAME}\n'; }
pin_held() {
  LC_ALL=C "$bin" -q versionlock list | awk '/^Package name:/ {print $3; next}
    /^[#!]/ || / / || NF == 0 {next}
    {sub(/^[0-9]+:/, ""); sub(/\.\*$/, ""); sub(/-[^-]+-[^-]+$/, ""); print}'
}
pin_hold() { "$bin" versionlock add "$@"; }
pin_unhold() { "$bin" versionlock delete "$@"; }
` + pinHoldSync + ` )`

	// zypper: Hold-Pins sind echte Schloesser (addlock). `zypper locks`
	// liefert eine Tabelle; die Spalten Name und Type sucht pin_held ueber die
	// Kopfzeile. Schloesser mit Muster (kernel*) passen auf kein konkretes
	// Paket und bleiben damit fremd.
	pinZypperBody = `( set -f
pin_installed() { rpm -qa --qf '%{NAME}\n'; }
pin_held() {
  LC_ALL=C zypper --non-interactive locks | awk '
    {n = split($0, f, "|"); for (i = 1; i <= n; i++) gsub(/^[ \t]+|[ \t]+$/, "", f[i])}
    f[2] == "Name" {ni = 0; ti = 0; for (i = 1; i <= n; i++) {if (f[i] == "Name") ni = i; if (f[i] == "Type") ti = i}; next}
    ni && f[1] ~ /^[0-9]+$/ && (!ti || f[ti] == "package") {print f[ni]}'
}
pin_hold() { zypper --non-interactive addlock "$@"; }
pin_unhold() { zypper --non-interactive removelock "$@"; }
` + pinHoldSync + ` )`

	// apk: `apk hold` gibt es erst in neueren apk-tools, eine Liste der Holds
	// nicht. pin_held nennt deshalb die Eintraege der Zustandsdatei - fremde
	// Holds kann LCM hier nicht sehen und gibt sie damit auch nie frei.
	pinApkBody = `( set -f
if ! apk hold --help >/dev/null 2>&1; then
  [ -z "$HOLD" ] || echo 'LCM: dieses apk kennt kein hold - die Versions-Fixierung wurde NICHT gesetzt'
  exit 0
fi
pin_installed() { apk info; }
pin_held() { cat "$state"; }
pin_hold() { apk hold "$@"; }
pin_unhold() { apk unhold "$@"; }
` + pinHoldSync + ` )`

	// pacman trennt beides sauber (HoldPkg = Entfernen wird verweigert,
	// IgnorePkg = keine Upgrades). Beide stehen in einer eigenen LCM-Datei,
	// die per Include aus pacman.conf gezogen wird (einmalig, idempotent).
	// Fremde Holds gibt es hier nicht anzufassen: sie stehen in pacman.conf.
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
var pinRootBodies = map[string]string{
	pkgApt: pinAptBody, pkgDnf: pinDnfBody, pkgZypper: pinZypperBody,
	pkgApk: pinApkBody, pkgPacman: pinPacmanBody,
}

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

// dnfPinScript: Schutz vor dem Entfernen und Holds sind ein Root-Schritt
// (pinDnfBody). Davor kommt bei Bedarf das versionlock-Plugin ueber dnf
// selbst; der Root-Schritt steht zuletzt, sein Ergebnis ist das des Laufs.
//
// Hinweis: Wie viele Kernel dnf behaelt, steuert zusaetzlich installonly_limit
// in dnf.conf (Standard 3) - der Pin wirkt ergaenzend.
func dnfPinScript(restricted bool, mgr string, noRemove, hold []string) string {
	bin := dnfBin(mgr)
	var steps []string
	if len(hold) > 0 {
		steps = append(steps,
			bin+" -y install 'dnf-command(versionlock)' >/dev/null 2>&1 || "+bin+" -y install python3-dnf-plugin-versionlock >/dev/null 2>&1 || true")
	}
	steps = append(steps, pinRootStep(restricted, pkgDnf, noRemove, hold))
	return strings.Join(steps, "\n")
}

// zypperPinScript: zypper kennt nur EIN Schloss (`zypper addlock`). Es
// verhindert Entfernen UND Aktualisieren. Fuer NoRemove-Pins waere das zu
// scharf (keine Sicherheitsupdates mehr), deshalb:
//
//   - Hold-Pins  -> echtes Schloss (addlock, Root-Schritt pinZypperBody).
//   - NoRemove   -> kein Schloss; stattdessen wird der Autoremove-Lauf um
//     diese Pakete gekuerzt (siehe pkgAutoremoveScriptWithPins).
//
// Das ist die ehrliche Abbildung: Wir behaupten keinen Schutz, den zypper in
// dieser Form nicht kennt.
func zypperPinScript(restricted bool, noRemove, hold []string) string {
	var steps []string
	if len(noRemove) > 0 {
		steps = append(steps, fmt.Sprintf(
			"echo 'LCM: %d Schutz-Pins werden bei zypper beim Aufraeumen ausgespart (zypper kennt keine reine Entfernsperre)'", len(noRemove)))
	}
	steps = append(steps, pinRootStep(restricted, pkgZypper, noRemove, hold))
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
// dann sagt der Root-Schritt das offen, statt Schutz zu behaupten.
func apkPinScript(restricted bool, noRemove, hold []string) string {
	var steps []string
	if len(noRemove) > 0 {
		steps = append(steps, fmt.Sprintf(
			"echo 'LCM: apk kennt keinen Autoremove und damit keine Entfernsperre - die %d Schutz-Pins bleiben ohne Wirkung'", len(noRemove)))
	}
	steps = append(steps, pinRootStep(restricted, pkgApk, noRemove, hold))
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
