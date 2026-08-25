package services

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Zeitzone, Zeitabgleich und NTP. Aufbau bewusst analog zu DNS (dns.go) und
// APT-Cache: ein Inline-Skript für den Voll-Sudo-Modus, ein validierendes
// Helper-Unterkommando für den eingeschränkten Modus, jeweils mit
// Wirkungsnachweis statt einer Erfolgsmeldung auf Verdacht.
//
// Warum das kein Nebenschauplatz ist: eine falsch gehende Uhr lässt
// TLS-Zertifikate als „noch nicht gültig" oder „abgelaufen" erscheinen,
// verdirbt die Reihenfolge in Protokollen über mehrere Server hinweg, bricht
// zeitbasierte Einmalpasswörter (TOTP) und lässt Paketquellen mit signierten,
// zeitlich begrenzten Metadaten fehlschlagen. Der Fehler ist dabei
// unauffällig: das System läuft weiter und meldet nichts.

var (
	// ErrInvalidTimezone: die angefragte Zeitzone ist nicht plausibel.
	ErrInvalidTimezone = errors.New("ungültige Zeitzone")
	// ErrInvalidNTPServer: ein NTP-Server ist weder Hostname noch IP.
	ErrInvalidNTPServer = errors.New("ungültiger NTP-Server")
	// ErrTooManyNTPServers: mehr als domain.MaxNTPServers angefragt.
	ErrTooManyNTPServers = fmt.Errorf("höchstens %d NTP-Server erlaubt", domain.MaxNTPServers)
	// ErrClockFromHost: In Containern kommt die Uhr vom Host - ein Zeitdienst
	// im Container startet dort nicht und könnte die Zeit auch gar nicht
	// stellen. Der Weg führt über den Host.
	ErrClockFromHost = errors.New("die Systemuhr kommt in einem Container vom Host und ist hier nicht einstellbar - " +
		"den Zeitabgleich auf dem Virtualisierungs-Host einrichten")
)

// reTimezone lässt nur Zeitzonen im IANA-Format zu (Region/Ort, optional mit
// Unterebene). Der Wert landet in einem als root ausgeführten Kommando -
// ohne diese Schranke wäre er ein Einfallstor.
var reTimezone = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+_-]*(/[A-Za-z0-9+._-]+){0,2}$`)

// reNTPServer erlaubt Hostnamen und IP-Adressen (Pool-Adressen wie
// 0.debian.pool.ntp.org sind der Normalfall).
var reNTPServer = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._:-]{0,251}[A-Za-z0-9])?$`)

// validTimezone prüft und normalisiert eine Zeitzone.
func validTimezone(tz string) (string, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" || len(tz) > 64 || !reTimezone.MatchString(tz) {
		return "", fmt.Errorf("%w: %q", ErrInvalidTimezone, tz)
	}
	return tz, nil
}

// validNTPServers prüft die gewünschten Zeitserver.
func validNTPServers(servers []string) ([]string, error) {
	var out []string
	for _, raw := range servers {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if !reNTPServer.MatchString(s) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidNTPServer, s)
		}
		out = append(out, s)
	}
	if len(out) > domain.MaxNTPServers {
		return nil, ErrTooManyNTPServers
	}
	return out, nil
}

// timeStateScript liest den Zeit-Zustand eines Servers, rein lesend und ohne
// Sonderrechte. Ausgabe als feste key=value-Zeilen (robuster zu parsen als
// die je nach Distribution unterschiedliche timedatectl-Prosa):
//
//	tz=Europe/Berlin
//	epoch=1785941882
//	ntp_service=systemd-timesyncd
//	ntp_sync=yes
//	ntp_servers=0.debian.pool.ntp.org 1.debian.pool.ntp.org
//
// Die Zeitzone kommt bevorzugt aus timedatectl; fehlt systemd (Alpine mit
// OpenRC, Container ohne systemd), wird /etc/timezone bzw. das Ziel des
// /etc/localtime-Symlinks gelesen.
const timeStateScript = `echo "epoch=$(date -u +%s)"
TZ_VAL=$(timedatectl show -p Timezone --value 2>/dev/null)
[ -n "$TZ_VAL" ] || TZ_VAL=$(cat /etc/timezone 2>/dev/null)
# readlink -f liefert den Pfad auch dann zurueck, wenn /etc/localtime GAR
# NICHT existiert - deshalb nur uebernehmen, wenn wirklich ein zoneinfo-Pfad
# dahintersteht (auf Alpine ohne tzdata meldete LCM sonst "/etc/localtime"
# als Zeitzone).
if [ -z "$TZ_VAL" ]; then
  LINK=$(readlink -f /etc/localtime 2>/dev/null)
  case "$LINK" in */zoneinfo/*) TZ_VAL=${LINK##*/zoneinfo/} ;; esac
fi
# Letzter Rueckfall: die Abkuerzung, unter der das System tatsaechlich laeuft
# (ohne tzdata ist das UTC). Weniger genau als eine IANA-Zone, aber wahr.
[ -n "$TZ_VAL" ] || TZ_VAL=$(date +%Z 2>/dev/null)
echo "tz=$TZ_VAL"
NTP_SVC=""; NTP_SYNC="no"; NTP_SRV=""
if systemctl is-active --quiet chronyd 2>/dev/null || systemctl is-active --quiet chrony 2>/dev/null; then
  NTP_SVC=chrony
  chronyc -n tracking 2>/dev/null | grep -qiE '^Leap status +: +Normal' && NTP_SYNC=yes
  NTP_SRV=$(awk '/^(server|pool)[ \t]/ {print $2}' /etc/chrony/chrony.conf /etc/chrony.conf 2>/dev/null | tr '\n' ' ')
elif systemctl is-active --quiet systemd-timesyncd 2>/dev/null; then
  NTP_SVC=systemd-timesyncd
  [ "$(timedatectl show -p NTPSynchronized --value 2>/dev/null)" = "yes" ] && NTP_SYNC=yes
  NTP_SRV=$(awk -F= '/^[ \t]*(NTP|FallbackNTP)=/ {print $2}' /etc/systemd/timesyncd.conf /etc/systemd/timesyncd.conf.d/*.conf 2>/dev/null | tr '\n' ' ')
elif systemctl is-active --quiet ntpd 2>/dev/null || rc-service ntpd status >/dev/null 2>&1 || rc-service chronyd status >/dev/null 2>&1; then
  NTP_SVC=ntpd
  NTP_SRV=$(awk '/^(server|pool)[ \t]/ {print $2}' /etc/ntp.conf 2>/dev/null | tr '\n' ' ')
  ntpq -pn 2>/dev/null | grep -q '^\*' && NTP_SYNC=yes
elif rc-service busybox-ntpd status >/dev/null 2>&1 || pgrep -x ntpd >/dev/null 2>&1; then
  NTP_SVC=busybox-ntpd
  NTP_SRV=$(awk '/^NTPD_OPTS/ {print}' /etc/conf.d/ntpd 2>/dev/null | grep -oE '\-p [^ "]+' | cut -d' ' -f2 | tr '\n' ' ')
fi
echo "ntp_service=$NTP_SVC"
echo "ntp_sync=$NTP_SYNC"
echo "ntp_servers=$NTP_SRV"`

// timeState ist der geparste Zeit-Zustand eines Servers.
type timeState struct {
	Timezone   string
	Epoch      int64
	NTPService string
	NTPSync    bool
	NTPServers string
}

// parseTimeState liest die key=value-Ausgabe von timeStateScript.
func parseTimeState(out string) timeState {
	var st timeState
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "tz":
			st.Timezone = val
		case "epoch":
			st.Epoch, _ = strconv.ParseInt(val, 10, 64)
		case "ntp_service":
			st.NTPService = val
		case "ntp_sync":
			st.NTPSync = val == "yes"
		case "ntp_servers":
			// Doppelte und leere Einträge raus, Reihenfolge erhalten.
			seen := map[string]bool{}
			var srv []string
			for _, s := range strings.Fields(val) {
				if s != "" && !seen[s] {
					seen[s] = true
					srv = append(srv, s)
				}
			}
			st.NTPServers = strings.Join(srv, ",")
		}
	}
	return st
}

// clockOffsetSeconds ist der Versatz der Server-Uhr gegenüber LCM, in
// Sekunden (positiv = der Server geht vor).
//
// Die Messung ist bewusst grob: Zwischen dem `date`-Aufruf und dem Vergleich
// liegt eine SSH-Runde, ein bis zwei Sekunden Ungenauigkeit sind normal. Für
// die Frage, um die es geht - läuft die Uhr überhaupt richtig? - reicht das
// völlig; deshalb schlägt die Bewertung auch erst bei einer deutlich
// größeren Abweichung an (domain.ClockOffsetWarnSeconds).
func clockOffsetSeconds(serverEpoch int64, now time.Time) int {
	if serverEpoch <= 0 {
		return 0
	}
	return int(serverEpoch - now.Unix())
}

// scanTimeState erfasst den Zeit-Zustand über eine bestehende Verbindung.
func scanTimeState(res *scanResult, run func(label, cmd string) string) {
	st := parseTimeState(run("time-state", timeStateScript))
	res.Timezone = st.Timezone
	res.NTPService = st.NTPService
	res.NTPSynchronized = st.NTPSync
	res.NTPServers = st.NTPServers
	res.ClockOffsetSeconds = clockOffsetSeconds(st.Epoch, time.Now())
}

// timeStateFields bündelt die zu persistierenden Felder - eine Stelle, damit
// Voll-Scan, Refresh und die Zeit-Aktionen denselben Satz schreiben.
func timeStateFields(st timeState, now time.Time) map[string]any {
	return map[string]any{
		"timezone":             st.Timezone,
		"ntp_service":          st.NTPService,
		"ntp_synchronized":     st.NTPSync,
		"ntp_servers":          st.NTPServers,
		"clock_offset_seconds": clockOffsetSeconds(st.Epoch, now),
		"time_checked_at":      now,
	}
}

// ---- Aktionen ---------------------------------------------------------------

// timezoneApplyScript setzt die Zeitzone und LIEST SIE ZURÜCK. Eine
// geschriebene Datei ist kein Beleg dafür, dass das System sie auch verwendet:
// ohne systemd greift `timedatectl` nicht, und ein bloßes Überschreiben von
// /etc/timezone bleibt ohne den passenden /etc/localtime-Symlink wirkungslos.
func timezoneApplyScript(tz string) string {
	return fmt.Sprintf(`if command -v timedatectl >/dev/null 2>&1 && timedatectl set-timezone '%[1]s' 2>/dev/null; then
  :
else
  [ -f /usr/share/zoneinfo/'%[1]s' ] || { echo "LCM: Zeitzone '%[1]s' ist auf diesem System nicht vorhanden (Paket tzdata fehlt?)" >&2; exit 1; }
  ln -sf /usr/share/zoneinfo/'%[1]s' /etc/localtime
  printf '%%s\n' '%[1]s' > /etc/timezone 2>/dev/null || true
fi
NOW_TZ=$(timedatectl show -p Timezone --value 2>/dev/null)
[ -n "$NOW_TZ" ] || NOW_TZ=$(readlink -f /etc/localtime 2>/dev/null | sed 's#.*/zoneinfo/##')
if [ "$NOW_TZ" = '%[1]s' ]; then
  echo "LCM: Zeitzone gesetzt und geprueft: $NOW_TZ ($(date))"
else
  echo "LCM: Zeitzone wurde geschrieben, das System meldet aber weiterhin '$NOW_TZ' - nicht als gesetzt gewertet" >&2
  exit 1
fi`, tz)
}

// ntpApplyScript trägt die Zeitserver ein, startet den Dienst und BELEGT die
// Synchronisierung. Unterstützt chrony, systemd-timesyncd und den klassischen
// ntpd; welcher vorhanden ist, entscheidet das Zielsystem.
//
// Der Nachweis ist der Punkt: „Dienst gestartet" heißt nicht „Uhr geht
// richtig". Gelingt die Synchronisierung im Zeitfenster nicht, meldet die
// Aktion das ehrlich - die Konfiguration bleibt liegen, denn sie ist ja nicht
// falsch, sie hat nur noch nicht gegriffen.
func ntpApplyScript(servers []string) string {
	list := strings.Join(servers, " ")
	var chronyLines, ntpLines strings.Builder
	for _, s := range servers {
		chronyLines.WriteString("server " + s + " iburst\n")
		ntpLines.WriteString("server " + s + " iburst\n")
	}
	return fmt.Sprintf(`SYNCED=no; SVC=""
if command -v chronyc >/dev/null 2>&1; then
  SVC=chrony
  CONF=/etc/chrony/chrony.conf; [ -f "$CONF" ] || CONF=/etc/chrony.conf
  [ -f "$CONF" ] && { [ -f "$CONF.lcm-bak" ] || cp -f "$CONF" "$CONF.lcm-bak"; }
  # Nur die von LCM verwalteten Zeitserver ersetzen, den Rest der Datei lassen.
  { grep -vE '^[ \t]*(server|pool)[ \t]' "$CONF" 2>/dev/null; printf '%[2]s'; } > "$CONF.lcm-new" && mv "$CONF.lcm-new" "$CONF"
  systemctl restart chronyd 2>/dev/null || systemctl restart chrony 2>/dev/null || rc-service chronyd restart 2>/dev/null || true
  chronyc -n makestep >/dev/null 2>&1 || true
  for i in 1 2 3 4 5 6 7 8 9 10; do
    chronyc -n tracking 2>/dev/null | grep -qiE '^Leap status +: +Normal' && { SYNCED=yes; break; }
    sleep 2
  done
elif command -v timedatectl >/dev/null 2>&1 && [ -d /etc/systemd ]; then
  SVC=systemd-timesyncd
  install -d -m 755 /etc/systemd/timesyncd.conf.d
  printf '[Time]\nNTP=%[1]s\n' > /etc/systemd/timesyncd.conf.d/lcm-ntp.conf
  timedatectl set-ntp true 2>/dev/null || true
  systemctl restart systemd-timesyncd 2>/dev/null || true
  for i in 1 2 3 4 5 6 7 8 9 10; do
    [ "$(timedatectl show -p NTPSynchronized --value 2>/dev/null)" = "yes" ] && { SYNCED=yes; break; }
    sleep 2
  done
elif [ -f /etc/ntp.conf ]; then
  SVC=ntpd
  [ -f /etc/ntp.conf.lcm-bak ] || cp -f /etc/ntp.conf /etc/ntp.conf.lcm-bak
  { grep -vE '^[ \t]*(server|pool)[ \t]' /etc/ntp.conf; printf '%[3]s'; } > /etc/ntp.conf.lcm-new && mv /etc/ntp.conf.lcm-new /etc/ntp.conf
  systemctl restart ntpd 2>/dev/null || rc-service ntpd restart 2>/dev/null || true
  for i in 1 2 3 4 5; do
    ntpq -pn 2>/dev/null | grep -q '^\*' && { SYNCED=yes; break; }
    sleep 2
  done
else
  echo "LCM: kein unterstuetzter Zeitdienst gefunden (chrony, systemd-timesyncd oder ntpd installieren)" >&2
  exit 1
fi
echo "LCM: Zeitserver gesetzt ($SVC): %[1]s"
if [ "$SYNCED" = yes ]; then
  echo "LCM: Uhr ist synchronisiert - $(date -u '+%%Y-%%m-%%d %%H:%%M:%%S UTC')"
else
  echo "LCM: Zeitserver eingetragen, eine Synchronisierung war im Zeitfenster aber nicht nachweisbar. Die Konfiguration bleibt bestehen; Erreichbarkeit der Zeitserver pruefen." >&2
  exit 2
fi`, list, chronyLines.String(), ntpLines.String())
}

// ---- Service-Methoden -------------------------------------------------------

// TimeState liest den aktuellen Zeit-Zustand eines Servers frisch aus und
// speichert ihn. Rein lesend - läuft daher auch im eingeschränkten Modus.
func (s *ServerService) TimeState(scope repositories.AccessScope, id uint, actor string) (*domain.Server, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	conn, err := s.connectRec(server, "time-state", actor)
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	out, _, runErr := conn.Run(timeStateScript)
	if runErr != nil {
		return nil, runErr
	}
	_ = s.servers.UpdateFields(id, timeStateFields(parseTimeState(out), time.Now()))
	return s.servers.FindByID(scope, id)
}

// SetTimezone setzt die Zeitzone eines Servers (mit Rücklesen) und
// aktualisiert danach den gespeicherten Zeit-Zustand.
func (s *ServerService) SetTimezone(scope repositories.AccessScope, id uint, tz, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return "", err
	}
	clean, err := validTimezone(tz)
	if err != nil {
		return "", err
	}
	conn, err := s.connectRec(server, "set-timezone", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	script := timezoneApplyScript(clean)
	if server.RestrictedSudo {
		script = helperCmd("timezone", clean)
	}
	output, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return output, runErr
	}
	if code != 0 {
		return output, withProvisionLog(fmt.Errorf("zeitzone setzen fehlgeschlagen (exit %d)", code), output)
	}
	s.refreshTimeState(conn, server)
	s.audit.Log(actor, "server.set-timezone", "server", id, server.Name+": "+clean)
	return output, nil
}

// ConfigureNTP trägt Zeitserver ein und belegt die Synchronisierung.
func (s *ServerService) ConfigureNTP(scope repositories.AccessScope, id uint, servers []string, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return "", err
	}
	// In einem Container ist die Systemuhr GAR NICHT verwaltbar: sie kommt vom
	// Host, und systemd-timesyncd startet dort aus genau diesem Grund nicht
	// (ConditionVirtualization=!container). Eine Aktion anzubieten, die dort
	// nie greifen kann, wäre dasselbe leere Versprechen wie beim Kernel eines
	// Containers - deshalb hier ein klarer Riegel mit dem Hinweis, wo die Uhr
	// tatsächlich zu richten ist.
	if domain.IsContainerVirt(server.Virtualization) {
		return "", fmt.Errorf("%w (%s)", ErrClockFromHost, server.Virtualization)
	}
	clean, err := validNTPServers(servers)
	if err != nil {
		return "", err
	}
	if len(clean) == 0 {
		return "", fmt.Errorf("%w: keine Zeitserver angegeben", ErrInvalidNTPServer)
	}
	conn, err := s.connectRec(server, "configure-ntp", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	script := ntpApplyScript(clean)
	if server.RestrictedSudo {
		script = helperCmd("ntp", strings.Join(clean, ","))
	}
	output, code, runErr := conn.Run(privRun(server, script))
	// Der Zustand wird IMMER nachgelesen - auch wenn die Synchronisierung
	// (noch) nicht belegt ist, sind die Zeitserver jetzt eingetragen, und der
	// Server soll das anzeigen.
	s.refreshTimeState(conn, server)
	if runErr != nil {
		return output, runErr
	}
	s.audit.Log(actor, "server.configure-ntp", "server", id, server.Name+": "+strings.Join(clean, ","))
	switch code {
	case 0:
		return output, nil
	case ntpNotSyncedExit:
		// Eigener Zustand: eingetragen, aber unbelegt. Kein Erfolg, aber auch
		// kein Grund, die Konfiguration wieder abzuräumen.
		return output, withProvisionLog(ErrNTPNotSynced, output)
	default:
		return output, withProvisionLog(fmt.Errorf("ntp einrichten fehlgeschlagen (exit %d)", code), output)
	}
}

// ntpNotSyncedExit meldet „Zeitserver eingetragen, Synchronisierung nicht
// nachweisbar" - abseits der üblichen 1, damit er nicht mit einem Fehler der
// Einzelkommandos verwechselt wird.
const ntpNotSyncedExit = 2

// ErrNTPNotSynced: die Zeitserver stehen, die Uhr ist aber (noch) nicht
// nachweislich synchron.
var ErrNTPNotSynced = errors.New("zeitserver eingetragen, eine synchronisierung war im zeitfenster aber nicht nachweisbar - " +
	"die konfiguration bleibt bestehen; erreichbarkeit der zeitserver prüfen")

// refreshTimeState liest den Zeit-Zustand über eine bestehende Verbindung neu
// ein (best effort - ein Fehler hier darf die Aktion nicht scheitern lassen).
func (s *ServerService) refreshTimeState(conn interface {
	Run(string) (string, int, error)
}, server *domain.Server) {
	out, _, err := conn.Run(timeStateScript)
	if err != nil {
		return
	}
	_ = s.servers.UpdateFields(server.ID, timeStateFields(parseTimeState(out), time.Now()))
}
