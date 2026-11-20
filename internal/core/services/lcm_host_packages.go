package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"LCM/internal/core/domain"
)

// Paketläufe auf dem LCM-Host.
//
// Ein Update auf dem Rechner, auf dem LCM läuft, nimmt früher oder später das
// Paket lcm mit, und dessen Installation startet den Dienst neu. Liefe dpkg
// dabei als Kind der SSH-Sitzung, die LCM selbst offen hält, schnitte der
// Neustart den Lauf mitten im Einspielen ab - zurück bliebe ein halb
// konfiguriertes System. Das Selbst-Update löst das mit systemd-run
// (self_update_run.go); hier gilt dasselbe für jede Paket-Aktion und jede
// Paket-Regel auf dem LCM-Host:
//
//   - Voller sudo: Der Lauf geht in die transiente Unit lcm-host-packages.
//     Ausgabe und Exit-Code landen in hostPackageDir; die SSH-Sitzung liest
//     die Ausgabe laufend mit und endet mit dem Exit-Code der Unit. Reißt
//     sie ab, läuft apt in der Unit zu Ende, und der Wiederanlauf von LCM
//     holt das Ergebnis aus dem Verzeichnis (hostPackageOutcome).
//   - Eingeschränkter sudo: systemd-run steht nicht auf der Whitelist, und
//     ein Helper-Kommando, das beliebige Skripte abkoppelt, hebelte die
//     Whitelist aus. Dort bleibt der Lauf in der Sitzung, meldet aber keinen
//     Erfolg, solange `dpkg --audit` angebrochene Pakete findet.
//
// Beide Wege prüfen am Ende `dpkg --audit`: Ein Lauf, nach dem dpkg Pakete
// halb eingerichtet zurücklässt, ist kein Erfolg, auch wenn apt 0 meldet.

const hostPackageUnit = "lcm-host-packages"

// hostPackageDir hält Ausgabe (out) und Exit-Code (rc) des abgekoppelten
// Laufs. Unter /run, weil es den Neustart von LCM überdauern muss, aber
// nicht den des Rechners. Lesbar für den Dienst lcm, der das Ergebnis nach
// seinem Neustart abholt - apt-Ausgaben sind nicht geheim. Variable für
// Tests.
var hostPackageDir = "/run/" + hostPackageUnit

// lcmHostPackageScript koppelt einen Paketlauf auf dem LCM-Host ab. Andere
// Server, andere Paketverwaltungen als apt (das Paket lcm gibt es nur als
// .deb) und Job-Arten, die nichts einspielen, bleiben unverändert.
func lcmHostPackageScript(server *domain.Server, jobType, script string) string {
	if !server.IsLcmHost() || server.PackageManager != pkgApt || !selfUpdateJobTypes[jobType] {
		return script
	}
	if server.RestrictedSudo {
		return "( " + script + "\n); __rc=$?\n" + dpkgAuditCheck + "\nexit $__rc"
	}
	return detachedHostPackageScript(script)
}

// dpkgAuditCheck setzt __rc auf einen Fehler, wenn dpkg unvollständig
// eingerichtete Pakete meldet.
const dpkgAuditCheck = `__audit=$(dpkg --audit 2>&1)
if [ -n "$__audit" ]; then
  echo 'FEHLER: dpkg meldet unvollstaendig eingerichtete Pakete - der Lauf wurde unterbrochen:'
  echo "$__audit"
  echo 'Reparatur: Pakete > Reparieren > dpkg --configure -a'
  [ "$__rc" -eq 0 ] && __rc=1
fi`

// detachedHostPackageScript baut den abgekoppelten Lauf (voller sudo).
func detachedHostPackageScript(script string) string {
	d := hostPackageDir
	inner := "( " + script + "\n) >" + d + "/out 2>&1 </dev/null; __rc=$?\n" +
		dpkgAuditCheck + " >>" + d + "/out 2>&1\n" +
		"echo $__rc >" + d + "/rc.tmp && mv " + d + "/rc.tmp " + d + "/rc"
	return strings.Join([]string{
		"__d=" + d + "; __u=" + hostPackageUnit,
		"command -v systemd-run >/dev/null 2>&1 || " +
			"{ echo 'FEHLER: systemd-run nicht gefunden - Paketlaeufe auf dem LCM-Host brauchen systemd.' >&2; exit 1; }",
		`case $(systemctl show -p ActiveState --value "$__u" 2>/dev/null) in active|activating|deactivating|reloading) ` +
			`echo "FEHLER: Ein frueherer Paketlauf auf dem LCM-Host laeuft noch (Unit $__u). Protokoll: $__d/out" >&2; exit 1 ;; esac`,
		`rm -rf "$__d" && mkdir -m 0755 "$__d" && : >"$__d/out" || exit 1`,
		`systemd-run --collect --unit="$__u" --description='LCM-Paketlauf auf dem LCM-Host' /bin/sh -c ` +
			shellQuote(inner) + ` >/dev/null || exit 1`,
		`echo "Paketlauf als eigener Dienst ($__u) gestartet - er ueberdauert einen Neustart von LCM."`,
		hostPackageFollow,
	}, "\n")
}

// hostPackageFollow liest die Ausgabe der Unit mit, bis der Exit-Code
// vorliegt, und endet mit ihm. Endet die Unit ohne Exit-Code (abgestürzt,
// von Hand gestoppt), ist das ein Fehler.
const hostPackageFollow = `__s=0
__out() { __n=$(($(wc -c <"$__d/out"))); [ "$__n" -gt "$__s" ] || return 0; tail -c +$((__s + 1)) "$__d/out" | head -c $((__n - __s)); __s=$__n; }
while [ ! -f "$__d/rc" ]; do
  sleep 1
  __out
  case $(systemctl show -p ActiveState --value "$__u" 2>/dev/null) in active|activating|deactivating|reloading) continue ;; esac
  sleep 1
  [ -f "$__d/rc" ] && break
  __out
  echo "FEHLER: Der Paketlauf endete ohne Ergebnis. Protokoll: journalctl -u $__u" >&2
  exit 1
done
__out
exit $(cat "$__d/rc")`

// hostPackageOutcome holt nach einem Neustart von LCM das Ergebnis des
// abgekoppelten Laufs, der zum Job gehört. Ein Ergebnis zählt nur, wenn der
// Lauf nach dem Job begonnen hat - sonst stammt es von einem früheren.
// Leerer Text: nichts gefunden; exitCode nil: der Lauf ist noch nicht fertig.
func hostPackageOutcome(job *domain.Job) (text string, exitCode *int) {
	info, err := os.Stat(filepath.Join(hostPackageDir, "out"))
	// Eine Sekunde Spielraum: Die Uhr der Datei hat nur Sekunden.
	if err != nil || info.ModTime().Before(job.CreatedAt.Add(-time.Second)) {
		return "", nil
	}
	out := tailText(filepath.Join(hostPackageDir, "out"), 64<<10)
	raw, err := os.ReadFile(filepath.Join(hostPackageDir, "rc"))
	if err != nil {
		return fmt.Sprintf("Der Paketlauf läuft als Dienst %s weiter. Protokoll: %s/out\n%s",
			hostPackageUnit, hostPackageDir, out), nil
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return "", nil
	}
	return fmt.Sprintf("Ergebnis des Paketlaufs (Exit-Code %d):\n%s", code, out), &code
}

// tailText liest höchstens die letzten max Bytes einer Datei.
func tailText(path string, max int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > max {
		_, _ = f.Seek(-max, io.SeekEnd)
	}
	data, _ := io.ReadAll(io.LimitReader(f, max))
	return string(data)
}
