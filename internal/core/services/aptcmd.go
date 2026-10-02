package services

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Paket-Update-Kommandos. Alle apt-Aufrufe laufen nicht-interaktiv und
// werden über wrapSudo als root ausgeführt. Die Skripte sind bewusst
// eigenständig (mehrere Schritte in einer sh -c), damit Verbindung und
// Protokoll pro Aktion genau ein Kommando sehen.

var (
	ErrNoPackages     = errors.New("keine gültigen paketnamen angegeben")
	ErrInvalidPackage = errors.New("ungültiger paketname")
	ErrInvalidVersion = errors.New("ungültige paketversion")
	ErrVersionOnePkg  = errors.New("eine versionsgenaue installation ist nur für genau ein paket möglich")
	// ErrProtectedPackage: das gezielte Entfernen eines kritischen Systempakets
	// (SSH-Server, sudo, Paketverwaltung, Kernel, libc …) wurde abgelehnt, weil
	// es den Server oder den LCM-Zugang unbrauchbar machen würde.
	ErrProtectedPackage = errors.New("dieses paket ist geschützt und kann nicht über LCM entfernt werden (kritisch für system oder LCM-zugang)")
	// Paketnamen: apt (klein) wie auch RPM (auch "_"); alle Zeichen sind
	// shell-sicher (kein Whitespace/Metazeichen) - Schutz vor Injection.
	rePackageName      = regexp.MustCompile(`^[a-z0-9][a-z0-9+._-]*$`)
	rePackageVersion   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+.:~_-]*$`)
	reUpgradableSecure = "grep -iE '/[^ ]*(security|-security)'"
)

// parsePackageNames zerlegt eine durch Komma und/oder Whitespace getrennte
// Liste, validiert jeden Namen streng (Schutz vor Shell-Injection) und
// dedupliziert in Eingabereihenfolge.
func parsePackageNames(s string) ([]string, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == ';'
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		name := strings.ToLower(strings.TrimSpace(f))
		if name == "" {
			continue
		}
		if !rePackageName.MatchString(name) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPackage, f)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, ErrNoPackages
	}
	return out, nil
}

// aptUpgradeRounds ist die Zahl der Anläufe, die ein Upgrade bekommt.
//
// Ein einziger Durchgang genügt nicht immer: Bei großen Rückständen (ein
// System, das lange nicht aktualisiert wurde, oder ein Sprung über mehrere
// Distributionsstände) hängen Pakete voneinander ab und lassen sich erst in
// einer bestimmten Reihenfolge einspielen. apt bricht dann mit halb
// konfigurierten Paketen ab - der zweite Anlauf, dem `dpkg --configure -a`
// und `apt-get -f install` vorausgehen, bringt genau diese durch. Drei
// Anläufe decken den Fall ab, ohne bei echten Fehlern lange zu kreisen: Was
// dreimal am selben Punkt scheitert, braucht einen Menschen.
const aptUpgradeRounds = 3

// aptRetry legt den Wiederholungslauf um ein apt-Kommando: Vor jedem
// weiteren Anlauf werden angebrochene Installationen abgeschlossen
// (`dpkg --configure -a`) und fehlende Abhängigkeiten nachgezogen
// (`apt-get -f install`) - die zwei Handgriffe, mit denen man ein steckenes
// Upgrade auch von Hand weiterbringt. Der Exit-Code des letzten Anlaufs ist
// der des Skripts, ein Fehlschlag bleibt also ein Fehlschlag.
//
// Eine belegte Paketverwaltung (aptLockBusyExit) beendet den Lauf sofort:
// lcm_apt hat dann schon die ganze Frist gewartet, ein weiterer Anlauf
// änderte nichts. `apt-get update` steht bewusst VOR der Schleife - scheitert
// es an einer Paketquelle, hilft kein dpkg-Handgriff.
func aptRetry(cmd string) string {
	rounds := make([]string, 0, aptUpgradeRounds)
	for i := 1; i <= aptUpgradeRounds; i++ {
		rounds = append(rounds, strconv.Itoa(i))
	}
	busy := strconv.Itoa(aptLockBusyExit)
	return strings.Join([]string{
		`rc=0`,
		`for i in ` + strings.Join(rounds, " ") + `; do`,
		`  if [ "$i" -gt 1 ]; then`,
		`    echo "LCM: Anlauf $i - angebrochene Installationen abschliessen"`,
		`    lcm_apt_wait || { rc=$?; break; }`,
		`    DEBIAN_FRONTEND=noninteractive dpkg --configure -a || true`,
		`    lcm_apt -f install -y || true`,
		`  fi`,
		`  ` + cmd + `; rc=$?`,
		`  [ $rc -eq 0 ] && break`,
		`  [ $rc -eq ` + busy + ` ] && break`,
		`  echo "LCM: Anlauf $i endete mit Exit-Code $rc"`,
		`done`,
		`exit $rc`,
	}, "\n")
}

// aptScript setzt den Sperren-Vorspann (aptPrelude) vor ein apt-Skript.
func aptScript(lines ...string) string {
	return aptPrelude + strings.Join(lines, "\n")
}

// aptUpgradeAllScript aktualisiert alle Pakete.
//
// --with-new-pkgs entspricht `apt upgrade`: Updates, die ein neues Paket
// brauchen, kommen mit. Ohne die Option hält `apt-get upgrade` sie dauerhaft
// zurück - allen voran die Kernel (linux-image-generic braucht für jede neue
// Fassung ein neues linux-image-<version>-Paket). Entfernt wird weiterhin
// nichts; das bliebe `full-upgrade` vorbehalten.
//
// Danach werden gestaffelte Updates mit bekannter CVE vorgezogen
// (aptForceStep) und alles benannt, was liegen bleibt (lcm_apt_report) -
// auch nach einem Fehlschlag, nur nicht bei belegtem apt.
func aptUpgradeAllScript() string {
	return aptScript(
		aptHeldBackFunc+aptHeldBackReport+"lcm_apt_update || exit $?",
		"( "+aptRetry("lcm_apt -y --with-new-pkgs upgrade")+" )",
		"rc=$?",
		`[ $rc -eq 0 ] && { `+aptForceStep+`; }`,
		`[ $rc -ne `+strconv.Itoa(aptLockBusyExit)+` ] && lcm_apt_report`,
		"exit $rc",
	)
}

// aptRefreshScript aktualisiert nur die Paket-Metadaten (apt-get update) -
// es wird NICHTS installiert. In Kombination mit dem anschließenden Rescan
// (dpkg-query + apt list --upgradable) hält es den Paketbestand samt
// verfügbarer Updates aktuell.
func aptRefreshScript() string {
	return aptScript("lcm_apt_update")
}

// aptUpgradePackagesScript aktualisiert ausschließlich die genannten Pakete
// auf die neueste verfügbare Version (--only-upgrade installiert keine neuen
// Pakete, falls eines nicht installiert ist).
func aptUpgradePackagesScript(names []string) string {
	return aptScript("lcm_apt_update || exit $?",
		aptRetry("lcm_apt install --only-upgrade -y "+strings.Join(names, " ")))
}

// aptInstallVersionScript installiert ein Paket auf eine exakte Version
// (erlaubt bewusst auch Downgrades).
func aptInstallVersionScript(name, version string) string {
	return aptScript("lcm_apt_update && lcm_apt install -y --allow-downgrades " + name + "=" + version)
}

// aptAutoremoveScript entfernt automatisch installierte Pakete, die von
// keinem anderen Paket mehr benötigt werden (klassisches apt autoremove).
// Bewusst OHNE --purge: die Konfigurationsdateien bleiben erhalten, das
// Entfernen ist damit weniger einschneidend und im Zweifel umkehrbar.
func aptAutoremoveScript() string {
	return aptScript("lcm_apt -y autoremove")
}

// aptRemovePackagesScript deinstalliert gezielt die genannten Pakete
// (klassisches apt remove - Konfiguration bleibt, kein Purge). Die Namen
// sind vorvalidiert (rePackageName) und shell-sicher.
func aptRemovePackagesScript(names []string) string {
	return aptScript("lcm_apt -y remove " + strings.Join(names, " "))
}

// aptSecurityUpgradeScript aktualisiert nur Pakete aus Security-Quellen:
// die Kandidatenliste wird auf -security-Ursprünge gefiltert und exakt diese
// Pakete werden aktualisiert. Ohne Security-Updates endet das Skript sauber.
func aptSecurityUpgradeScript() string {
	return aptScript(
		"lcm_apt_update || exit $?",
		"pkgs=$(apt list --upgradable 2>/dev/null | "+reUpgradableSecure+" | cut -d/ -f1)",
		`if [ -z "$pkgs" ]; then echo 'LCM: keine security-updates verfuegbar'; exit 0; fi`,
		`echo "LCM: security-updates fuer: $pkgs"`,
		aptRetry("lcm_apt install --only-upgrade -y $pkgs"),
	)
}

// parseMadison extrahiert die verfügbaren Versionen aus `apt-cache madison`.
// Zeilenformat: " paket | 1.2.3-1 | http://… ". Reihenfolge (neueste zuerst)
// bleibt erhalten, Duplikate werden entfernt.
func parseMadison(out string) []string {
	seen := map[string]bool{}
	var versions []string
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		v := strings.TrimSpace(parts[1])
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		versions = append(versions, v)
	}
	return versions
}
