package services

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"LCM/internal/core/domain"
)

// Benutzer-Scan: erfasst die anmeldefähigen Linux-Konten eines Servers -
// Name, UID, Shell, Passwort-Status (aus /etc/shadow), Anzahl der
// authorized_keys, TOTP-Enrollment (~/.google_authenticator), Konto-Ablauf
// und letzter Login (lastlog/lastlog2, best effort).
//
// Das Skript existiert bewusst nur EINMAL: der Voll-Modus führt es als
// root-Skript aus, der eingeschränkte Modus über das Helper-Unterkommando
// users-scan (Einsetzung in lcm_helper.go) - zwei Kopien derselben Erfassung
// würden auseinanderlaufen, und dann zeigte der eingeschränkte Modus andere
// Konten als der Voll-Modus.
//
// POSIX sh, keine Bashismen (läuft auch unter BusyBox/dash im Helper).
func usersScanScript() string {
	return `UM=$(awk '$1=="UID_MIN"{print $2; exit}' /etc/login.defs 2>/dev/null)
case "$UM" in ''|*[!0-9]*) UM=1000 ;; esac
TODAY=$(( $(date +%s) / 86400 ))
{ getent passwd 2>/dev/null || cat /etc/passwd; } | while IFS=: read -r name _ uid gid gecos home shell; do
  case "$uid" in ''|*[!0-9]*) continue ;; esac
  [ "$uid" -eq 0 ] || [ "$uid" -ge "$UM" ] || continue
  [ "$uid" -ge 65534 ] && continue
  case "$shell" in ''|*/nologin|*/false|*/sync|*/shutdown|*/halt) continue ;; esac
  if [ -r /etc/shadow ]; then
    sh=$(awk -F: -v u="$name" '$1==u{print $2 "|" $8; exit}' /etc/shadow 2>/dev/null)
    hash=${sh%|*}; exp=${sh#*|}
    case "$hash" in
      ''|'!'|'!!'|'*') pst=none ;;
      '!'*) pst=locked ;;
      \**) pst=none ;;
      *) pst=set ;;
    esac
  else
    pst=unknown; exp=""
  fi
  dis=no
  case "$exp" in
    ''|*[!0-9]*) ;;
    *) [ "$exp" -le "$TODAY" ] && dis=yes ;;
  esac
  keys=0
  ak="$home/.ssh/authorized_keys"
  [ -s "$ak" ] && keys=$(grep -c -E '^[[:space:]]*(ssh-|ecdsa-|sk-)' "$ak" 2>/dev/null)
  tfa=no
  [ -f "$home/.google_authenticator" ] && tfa=yes
  ll=""
  if command -v lastlog2 >/dev/null 2>&1; then
    ll=$(LANG=C lastlog2 -u "$name" 2>/dev/null | sed -n 2p)
  elif command -v lastlog >/dev/null 2>&1; then
    ll=$(LANG=C lastlog -u "$name" 2>/dev/null | sed -n 2p)
  fi
  case "$ll" in *Never*) ll="" ;; esac
  echo "LCMUSER|$name|$uid|$shell|$pst|$keys|$tfa|$dis|$ll"
done
# Anmelde-Historie aus wtmp. -F liefert volle Zeitstempel, -w volle Namen.
# Die Tiefe ist durch die Rotation von wtmp begrenzt (meist ein Monat) - das
# ist eine Eigenschaft des Systems, nicht des Scans.
if command -v last >/dev/null 2>&1; then
  # ISO-Format bevorzugen: es enthaelt den Zeitzonen-Offset, der Zeitpunkt ist
  # damit eindeutig. Aeltere last-Fassungen kennen die Option nicht - dann
  # -F, dessen Wanduhrzeit ohne Zone auskommen muss.
  if LANG=C last --time-format iso -w -n 1 >/dev/null 2>&1; then
    LASTOUT=$(LANG=C last --time-format iso -w -n 300 2>/dev/null)
  else
    LASTOUT=$(LANG=C last -F -w -n 300 2>/dev/null)
  fi
  printf '%s\n' "$LASTOUT" | while IFS= read -r zeile; do
    case "$zeile" in
      ""|wtmp*|reboot*|shutdown*|runlevel*) continue ;;
    esac
    echo "LCMLOGIN|$zeile"
  done
fi`
}

// lastLoginLayouts sind die Datumsformate von lastlog (shadow-utils) und
// lastlog2 (util-linux), jeweils LANG=C: Wochentag, Monat, Tag, Zeit,
// optional Zeitzonen-Offset, Jahr.
var lastLoginLayouts = []string{
	"Mon Jan 2 15:04:05 -0700 2006",
	"Mon Jan 2 15:04:05 2006",
}

// weekdayNames erkennt den Beginn des Datums in einer lastlog-Zeile - davor
// stehen je nach System 1-3 Spalten (Name, Port, Herkunft), von denen Port
// und Herkunft auch fehlen können.
var weekdayNames = map[string]bool{
	"Mon": true, "Tue": true, "Wed": true, "Thu": true,
	"Fri": true, "Sat": true, "Sun": true,
}

// parseLastLogin zieht aus dem Rest einer Scan-Zeile (lastlog-Ausgabe hinter
// Name/Port/Herkunft) den Zeitpunkt des letzten Logins. nil = nicht ermittelbar.
func parseLastLogin(rest string) *time.Time {
	fields := strings.Fields(rest)
	start := -1
	for i, f := range fields {
		if weekdayNames[f] {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	candidate := strings.Join(fields[start:], " ")
	for _, layout := range lastLoginLayouts {
		if t, err := time.Parse(layout, candidate); err == nil {
			return &t
		}
	}
	return nil
}

// parseServerUsers wandelt die LCMUSER-Zeilen des Scan-Skripts in
// ServerUser-Einträge. Unlesbare Zeilen werden übergangen - ein einzelnes
// kaputtes Konto darf nicht die ganze Erfassung verwerfen.
func parseServerUsers(out string) []domain.ServerUser {
	var users []domain.ServerUser
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "LCMUSER|") {
			continue
		}
		// Name|UID|Shell|PwStatus|Keys|2FA|Disabled|LastLogin - der letzte
		// Teil ist die rohe lastlog-Zeile und darf selbst Trenner enthalten.
		parts := strings.SplitN(line[len("LCMUSER|"):], "|", 8)
		if len(parts) < 8 {
			continue
		}
		uid, err := strconv.Atoi(parts[1])
		if err != nil || parts[0] == "" {
			continue
		}
		users = append(users, domain.ServerUser{
			Username:          parts[0],
			UID:               uid,
			Shell:             parts[2],
			PasswordStatus:    parts[3],
			SSHKeyCount:       atoiSafe(parts[4]),
			TwoFactorEnrolled: parts[5] == "yes",
			Disabled:          parts[6] == "yes",
			LastLoginAt:       parseLastLogin(parts[7]),
		})
	}
	return users
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// lastLineRe zerlegt eine Zeile von `last`:
//
//	tony  pts/0  10.0.0.5  2026-08-18T09:12:33+02:00   still logged in
//	tony  pts/1  10.0.0.5  2026-08-17T14:02:11+02:00 - 2026-08-17T15:44:02+02:00  (01:41)
//	tony  tty1             Sat Aug 16 08:00:00 2026 - crash                       (02:11)
//
// Die Herkunftsspalte fehlt bei lokaler Anmeldung ganz - sie wird deshalb
// über das Datum abgegrenzt, nicht über Spaltenzählung (die je nach
// Distribution abweicht). Was NACH dem Datum steht, fängt eine eigene Gruppe
// als Ganzes ein und wird in Go gedeutet: „- <Ende>", „still logged in",
// „- crash" und die Dauer in Klammern sind zu viele Formen für ein Muster,
// das noch lesbar bleiben soll.
var lastLineRe = regexp.MustCompile(
	`^(\S+)\s+(\S+)\s+(.*?)\s*(` + isoStamp + `|` + textStamp + `)\s*(.*)$`)

const (
	isoStamp  = `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:?\d{2}`
	textStamp = `(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)\s+\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\s+\d{4}`
)

// lastStampLayouts sind die beiden Zeitformate von `last`. Das ISO-Format
// trägt den Zeitzonen-Offset und ergibt einen eindeutigen Zeitpunkt; die
// Wanduhr-Form von `-F` hat keine Zone und wird als UTC gelesen - auf
// Systemen mit altem util-linux kann die Anzeige daher um den Zonen-Versatz
// abweichen. Besser als die Historie ganz wegzulassen, und die Reihenfolge
// stimmt in jedem Fall.
var lastStampLayouts = []string{
	"2006-01-02T15:04:05-07:00",
	"2006-01-02T15:04:05-0700",
	"Mon Jan 2 15:04:05 2006",
}

// parseLastStamp liest einen Zeitstempel in einem der bekannten Formate.
func parseLastStamp(s string) (time.Time, bool) {
	s = normalizeSpaces(s)
	for _, layout := range lastStampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseLastLogins wandelt die LCMLOGIN-Zeilen in Anmelde-Einträge.
// Unlesbare Zeilen werden übergangen - `last` formatiert je nach Version
// leicht abweichend, und eine einzelne krumme Zeile darf nicht die ganze
// Historie verwerfen.
func parseLastLogins(out string) []domain.ServerUserLogin {
	var logins []domain.ServerUserLogin
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "LCMLOGIN|") {
			continue
		}
		m := lastLineRe.FindStringSubmatch(strings.TrimRight(line[len("LCMLOGIN|"):], " \r"))
		if m == nil {
			continue
		}
		start, ok := parseLastStamp(m[4])
		if !ok {
			continue
		}
		l := domain.ServerUserLogin{
			Username:  m[1],
			TTY:       m[2],
			FromHost:  strings.TrimSpace(m[3]),
			StartedAt: start,
		}
		// Der Rest der Zeile: erst die Dauer in Klammern abschneiden, dann
		// bleibt entweder „- <Ende>", „still logged in" oder „- crash".
		rest := strings.TrimSpace(m[5])
		if i := strings.LastIndex(rest, "("); i >= 0 {
			rest = strings.TrimSpace(rest[:i])
		}
		switch {
		case strings.Contains(rest, "still"):
			l.StillActive = true
		case strings.HasPrefix(rest, "-"):
			ende := strings.TrimSpace(strings.TrimPrefix(rest, "-"))
			// „crash"/„down": Der Rechner ging hart aus, das Ende ist
			// unbekannt. Lieber offen lassen als einen Zeitpunkt erfinden.
			if t, ok := parseLastStamp(ende); ok {
				l.EndedAt = &t
			}
		}
		logins = append(logins, l)
	}
	return logins
}

// normalizeSpaces macht aus mehrfachen Leerzeichen eines - `last` richtet
// Datumsfelder mit variabler Breite aus (etwa „Aug  1" gegenüber „Aug 18").
func normalizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// applyLoginHistory füllt „zuletzt angemeldet" aus der erhobenen Historie,
// wo lastlog nichts liefert.
//
// Nötig, weil moderne Systeme das Werkzeug schlicht nicht mehr mitbringen:
// Debian 13 hat weder lastlog noch lastlog2 im Standardumfang (util-linux hat
// lastlog abgelöst, der Nachfolger ist nicht überall da). Die Datei
// /var/log/lastlog wird zwar weiter beschrieben, aber niemand kann sie lesen.
// Ohne diese Ableitung stünde in der Übersicht „nie angemeldet" neben einer
// Historie mit Einträgen - ein Widerspruch, der wie ein Fehler aussieht.
//
// wtmp ist dabei die verlässlichere Quelle: Sie hält jede Sitzung fest, nicht
// nur die letzte.
func applyLoginHistory(users []domain.ServerUser, logins []domain.ServerUserLogin) {
	if len(logins) == 0 {
		return
	}
	neueste := map[string]time.Time{}
	for i := range logins {
		if t, ok := neueste[logins[i].Username]; !ok || logins[i].StartedAt.After(t) {
			neueste[logins[i].Username] = logins[i].StartedAt
		}
	}
	for i := range users {
		if users[i].LastLoginAt != nil {
			continue // lastlog hat geliefert - das bleibt maßgeblich
		}
		if t, ok := neueste[users[i].Username]; ok {
			zeit := t
			users[i].LastLoginAt = &zeit
		}
	}
}
