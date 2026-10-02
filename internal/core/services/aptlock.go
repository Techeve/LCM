package services

import "strconv"

// Sperren der Paketverwaltung.
//
// apt und dpkg schützen ihren Zustand mit fcntl-Sperren auf vier Dateien. Wer
// eine davon hält, blockiert jeden weiteren Lauf - `apt-get update` scheitert
// dann sofort mit „Could not get lock /var/lib/apt/lists/lock". Die
// eingebaute Wartefunktion hilft dagegen nicht: DPkg::Lock::Timeout gilt nur
// für die dpkg-Sperre, an der Listen-Sperre bricht `apt-get update` weiterhin
// sofort ab (belegt mit apt 2.8).
//
// Halter sind meist harmlos (apt-daily, unattended-upgrades, ein Mensch auf
// der Konsole) - dann genügt Warten. Es gibt aber einen Halter, den LCM selbst
// erzeugt: Bricht ein Job ab, schließt LCM die SSH-Verbindung, der Prozess auf
// dem Server läuft aber weiter (ohne Terminal kein SIGHUP, die Ausgabe geht in
// eine Datei, also auch kein SIGPIPE). Hängt dieses verwaiste
// `apt-get update` an einem Cache, der nicht antwortet, hält es die Sperre
// unbegrenzt - und jeder folgende Lauf scheitert.
//
// Deshalb setzt jedes apt-Skript den Vorspann aptPrelude voran:
//
//   - lcm_apt_wait wartet, bis keine der Sperren mehr gehalten wird, und
//     meldet dabei jede Minute, wer sie hält. Die Zeile ist zugleich das
//     Lebenszeichen für den Job-Watchdog.
//   - Ist der Halter nach Ablauf der Frist ein verwaistes `apt-get update`
//     eines früheren LCM-Laufs, wird es beendet. Ein Update abzubrechen ist
//     gefahrlos; alles andere (ein laufendes Upgrade, fremde Prozesse) bleibt
//     unangetastet, der Lauf endet dann mit aptLockBusyExit.
//   - lcm_apt_update begrenzt `apt-get update` zeitlich, damit es gar nicht
//     erst zum dauerhaften Sperrenhalter werden kann.

// aptLockWaitSeconds ist die Frist, die ein Lauf auf belegte Sperren wartet.
const aptLockWaitSeconds = 600

// aptUpdateTimeoutSeconds begrenzt `apt-get update`. Auch über einen
// langsamen Cache ist ein Update nach wenigen Minuten durch; wer länger
// braucht, hängt.
const aptUpdateTimeoutSeconds = 900

// aptLockBusyExit ist der Exit-Code „apt ist belegt" (EX_TEMPFAIL). Er
// unterscheidet sich bewusst von apts eigener 100: Gegen eine fremde Sperre
// hilft kein weiterer Anlauf, gegen einen dpkg-Fehler schon.
const aptLockBusyExit = 75

// aptLocks sind die Sperrdateien von dpkg und apt.
const aptLocks = "/var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/lib/apt/lists/lock /var/cache/apt/archives/lock"

// aptPrelude definiert die Shell-Funktionen lcm_apt, lcm_apt_update und
// lcm_apt_wait (siehe oben).
//
// lcm_apt_holder liest den Halter aus /proc/locks statt über fuser: psmisc
// fehlt auf Minimal-Installationen, /proc/locks gibt es immer. Gesucht wird
// nach der Inode der Sperrdatei; Zeilen mit "->" sind Wartende, keine Halter.
//
// lcm_apt_orphan erkennt LCMs eigene Läufe am Ausgabe-Wrapper (__lf=, siehe
// detachBackgroundFDs) unter den Vorfahren des Halters. Ein laufender
// LCM-Lauf auf demselben Server kann es nicht sein: Jobs eines Servers laufen
// nacheinander.
var aptPrelude = `LCM_APT_LOCKS="` + aptLocks + `"
lcm_apt_holder() {
  for f in "$@"; do [ -e "$f" ] && stat -c %i "$f"; done |
    awk 'NR == FNR { ino[$1] = 1; next } $2 != "->" { n = split($6, a, ":"); if (a[n] in ino) { print $5; exit } }' - /proc/locks 2>/dev/null
}
lcm_apt_who() {
  printf 'PID %s, laeuft seit %s: %s' "$1" "$(ps -o etime= -p "$1" 2>/dev/null | tr -d ' ')" "$(tr '\0' ' ' <"/proc/$1/cmdline" 2>/dev/null | cut -c1-160)"
}
lcm_apt_orphan() {
  case "$(tr '\0' ' ' <"/proc/$1/cmdline" 2>/dev/null)" in *apt-get*" update "*) ;; *) return 1 ;; esac
  p=$1
  while [ "$p" -gt 1 ]; do
    p=$(sed 's/.*) //' "/proc/$p/stat" 2>/dev/null | cut -d' ' -f2)
    [ -n "$p" ] || return 1
    tr '\0' ' ' <"/proc/$p/cmdline" 2>/dev/null | grep -q '__lf=' && return 0
  done
  return 1
}
lcm_apt_reap() {
  lcm_apt_orphan "$1" || return 1
  echo "LCM: beende verwaistes apt-get update eines abgebrochenen LCM-Laufs ($(lcm_apt_who "$1"))"
  kill "$1" 2>/dev/null || return 1
  sleep 5
  kill -9 "$1" 2>/dev/null
  return 0
}
lcm_apt_wait() {
  w=0
  while h=$(lcm_apt_holder $LCM_APT_LOCKS); [ -n "$h" ]; do
    if [ "$w" -ge ` + strconv.Itoa(aptLockWaitSeconds) + ` ]; then
      lcm_apt_reap "$h" && continue
      echo "LCM: apt ist seit ` + strconv.Itoa(aptLockWaitSeconds/60) + ` Minuten belegt ($(lcm_apt_who "$h")) - Lauf abgebrochen"
      return ` + strconv.Itoa(aptLockBusyExit) + `
    fi
    [ $((w % 60)) -eq 0 ] && echo "LCM: apt ist belegt ($(lcm_apt_who "$h")) - warte"
    sleep 5
    w=$((w + 5))
  done
}
lcm_apt() {
  lcm_apt_wait || return
  DEBIAN_FRONTEND=noninteractive apt-get -o Dpkg::Options::=--force-confold -o DPkg::Lock::Timeout=60 \
    ${LCM_APT_PHASED:+-o APT::Get::Always-Include-Phased-Updates=true} "$@"
}
lcm_apt_update() {
  lcm_apt_wait || return
  DEBIAN_FRONTEND=noninteractive timeout -k 30 ` + strconv.Itoa(aptUpdateTimeoutSeconds) + ` apt-get update
  __urc=$?
  [ $__urc -eq 124 ] && echo "LCM: apt-get update nach ` + strconv.Itoa(aptUpdateTimeoutSeconds/60) + ` Minuten abgebrochen - antwortet eine Paketquelle oder der APT-Cache nicht?"
  return $__urc
}
`
