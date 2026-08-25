package services

import (
	"encoding/base64"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
)

// Erkennung der Anwendungen, die nicht aus der Paketverwaltung stammen.
//
// Der ganze Katalog wird in EIN Skript übersetzt und in einem Durchlauf
// ausgeführt. Der Grund ist Betriebspraxis: 60 Katalogeinträge einzeln über
// die Verbindung zu schicken wären 60 Runden je Server - bei 40 Servern eine
// Viertelstunde für eine Bestandsaufnahme, die in Sekunden erledigt sein kann.
//
// Das Skript meldet zeilenweise, mit Tabulator getrennt:
//
//	APP\t<slug>\t<merkmal>\t<pfad>\t<version-base64>
//	UNKNOWN\t<unit>\t<unit-datei>\t<programm>
//
// Die Version wird base64-kodiert übertragen, weil ihre Ausgabe mehrzeilig
// sein darf - das Muster, das sie zerlegt, läuft in Go und braucht den
// Originaltext. Fehlt base64 auf dem Zielsystem, bleibt die Version leer;
// erkannt wird die Anwendung trotzdem.

// appVersionTimeout begrenzt jedes einzelne Versionskommando. Ein Kommando,
// das hängt (Netzzugriff, wartende Sperre), darf nicht den ganzen Scan
// aufhalten. Ohne `timeout` auf dem Zielsystem läuft es ungebremst - dann ist
// die Begrenzung Sache des Verbindungs-Timeouts.
const appVersionTimeout = 15

// appScanScript baut das Erkennungsskript für den gesamten Katalog.
func appScanScript(entries []domain.AppCatalogEntry, mgr string) string {
	var b strings.Builder
	b.WriteString("TO=''\ncommand -v timeout >/dev/null 2>&1 && TO='timeout " +
		strconv.Itoa(appVersionTimeout) + "'\n")
	for _, entry := range entries {
		markers, err := domain.ParseAppMarkers(entry.Markers)
		if err != nil {
			// Ein kaputter Eintrag darf den Scan der übrigen nicht verhindern.
			continue
		}
		b.WriteString(entryScript(entry, markers, mgr))
	}
	b.WriteString(unknownServicesScript(mgr))
	return b.String()
}

// entryScript prüft die Merkmale eines Eintrags - erster Treffer gewinnt - und
// ermittelt bei einem Treffer die Version.
func entryScript(entry domain.AppCatalogEntry, markers []domain.AppMarker, mgr string) string {
	var b strings.Builder
	b.WriteString("p=''; m=''\n")
	for _, marker := range markers {
		b.WriteString(`if [ -z "$p" ]; then `)
		switch marker.Kind {
		case domain.MarkerPath:
			b.WriteString(`[ -e ` + marker.Value + ` ] && { p=` + marker.Value + `; m=path; }`)
		case domain.MarkerUnit:
			b.WriteString(`systemctl cat ` + marker.Value + ` >/dev/null 2>&1 && ` +
				`{ p=$(systemctl show -p FragmentPath --value ` + marker.Value + ` 2>/dev/null); m=unit; }`)
		case domain.MarkerBin:
			b.WriteString(`q=$(command -v ` + marker.Value + ` 2>/dev/null) && [ -n "$q" ] && { p="$q"; m=bin; }`)
		case domain.MarkerProc:
			b.WriteString(`pgrep -x ` + marker.Value + ` >/dev/null 2>&1 && { p=` + marker.Value + `; m=proc; }`)
		}
		b.WriteString("; fi\n")
	}
	// Was der Paketverwaltung gehört, gehört nicht hierher: Diese Anwendung
	// steht dann im Paket-Reiter und wird dort auch aktualisiert. Der Katalog
	// darf denselben Eintrag trotzdem führen - ob er greift, entscheidet der
	// einzelne Server.
	if owner := packageOwnerCheck(mgr); owner != "" {
		b.WriteString(`if [ -n "$p" ] && [ "$m" != proc ] && [ -e "$p" ]; then f="$p"; ` +
			owner + ` && p=''; fi` + "\n")
	}
	b.WriteString(`if [ -n "$p" ]; then` + "\n")
	if cmd := strings.TrimSpace(entry.VersionCommand); cmd != "" {
		// {path} ist der Fundort. Ohne Treffer eines path-Merkmals steht dort
		// die Unit-Datei bzw. das Binary - beides brauchbare Bezugspunkte.
		//
		// Ausgeführt in einer eigenen Shell mit dem Fundort als Argument: So
		// wirkt $TO auf das GANZE Kommando und nicht nur auf dessen erstes
		// Glied, und der Pfad wird übergeben statt in den Text geklebt.
		inner := strings.ReplaceAll(cmd, "{path}", `"$1"`)
		b.WriteString("  v=$($TO sh -c " + shellQuote(inner) + ` _ "$p" 2>/dev/null | head -c 4000)` + "\n")
		b.WriteString(`  v=$(printf '%s' "$v" | base64 2>/dev/null | tr -d '\n')` + "\n")
	} else {
		b.WriteString("  v=''\n")
	}
	b.WriteString(`  printf 'APP\t%s\t%s\t%s\t%s\n' ` + shellQuote(entry.Slug) + ` "$m" "$p" "$v"` + "\n")
	b.WriteString("fi\n")
	return b.String()
}

// unknownServicesScript ist das Netz für alles, was im Katalog fehlt:
// laufende Dienste, deren Unit-Datei keinem Paket gehört.
//
// Der Dienst ist dabei der verlässlichere Anker als das Dateisystem - was
// läuft, ist in Betrieb und damit interessant, während unter /opt auch
// Karteileichen liegen. Ohne systemd (Alpine/OpenRC) entfällt der Teil.
func unknownServicesScript(mgr string) string {
	owner := packageOwnerCheck(mgr)
	if owner == "" {
		return ""
	}
	return `command -v systemctl >/dev/null 2>&1 || exit 0
systemctl list-units --type=service --state=running --no-legend --plain 2>/dev/null |
  awk '{print $1}' | head -200 | while read -r u; do
    case "$u" in lcm-agent.service|user@*|systemd-*|dbus*|getty@*) continue ;; esac
    f=$(systemctl show -p FragmentPath --value "$u" 2>/dev/null)
    [ -n "$f" ] && [ -f "$f" ] || continue
    ` + owner + ` && continue
    e=$(systemctl show -p ExecStart --value "$u" 2>/dev/null | sed -n 's/.*path=\([^ ;]*\).*/\1/p' | head -1)
    printf 'UNKNOWN\t%s\t%s\t%s\n' "$u" "$f" "$e"
  done
`
}

// packageOwnerCheck liefert das Kommando, das mit Erfolg antwortet, wenn die
// Datei "$f" einem Paket gehört. Leer, wenn die Paketverwaltung unbekannt ist.
//
// Hier wird bewusst NICHT über pkgFamily gegangen, das Unbekanntes auf apt
// abbildet: Ein `dpkg -S` auf einem System ohne dpkg schlägt fehl, und
// „schlägt fehl" heißt in dieser Prüfung „gehört keinem Paket". Auf einem
// frisch aufgenommenen oder exotischen Server wäre dann jeder laufende Dienst
// ein Fund. Lieber gar keine Aussage als eine Liste voller Fehlalarme.
func packageOwnerCheck(mgr string) string {
	switch mgr {
	case pkgApt:
		return `dpkg -S "$f" >/dev/null 2>&1`
	case pkgDnf, pkgYum, pkgZypper:
		return `rpm -qf "$f" >/dev/null 2>&1`
	case pkgPacman:
		return `pacman -Qo "$f" >/dev/null 2>&1`
	case pkgApk:
		return `apk info --who-owns "$f" >/dev/null 2>&1`
	default:
		return ""
	}
}

// parseAppScan wertet die Ausgabe des Erkennungsskripts aus.
func parseAppScan(out string, entries []domain.AppCatalogEntry) ([]domain.DetectedApp, []domain.UnknownApp) {
	bySlug := make(map[string]domain.AppCatalogEntry, len(entries))
	for _, e := range entries {
		bySlug[e.Slug] = e
	}
	var apps []domain.DetectedApp
	var unknown []domain.UnknownApp
	for _, line := range strings.Split(out, "\n") {
		cols := strings.Split(strings.TrimRight(line, "\r"), "\t")
		switch {
		case cols[0] == "APP" && len(cols) >= 5:
			entry, ok := bySlug[cols[1]]
			if !ok {
				continue
			}
			apps = append(apps, domain.DetectedApp{
				Slug: entry.Slug, Name: entry.Name,
				Marker: cols[2], Path: strings.TrimSpace(cols[3]),
				Version: domain.ExtractVersion(decodeVersion(cols[4]), entry.VersionPattern),
			})
		case cols[0] == "UNKNOWN" && len(cols) >= 3:
			unit := strings.TrimSpace(cols[1])
			if unit == "" {
				continue
			}
			u := domain.UnknownApp{Unit: unit, FragmentPath: strings.TrimSpace(cols[2])}
			if len(cols) >= 4 {
				u.ExecPath = strings.TrimSpace(cols[3])
			}
			unknown = append(unknown, u)
		}
	}
	return apps, unknown
}

func decodeVersion(b64 string) string {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return ""
	}
	return string(raw)
}

// rescanApps nimmt die nicht paketverwalteten Anwendungen eines Servers auf.
// Best effort wie der übrige Bestand: Was hier schiefgeht, darf den Scan
// nicht scheitern lassen.
//
// Im eingeschränkten Modus läuft das Skript OHNE sudo. Die Merkmale (Pfad,
// Unit, Binary, Prozess) beantwortet das System auch unprivilegiert; ein
// Versionskommando, das Root braucht, bleibt dort ohne Antwort. Der Weg über
// den LCM-Helper stünde offen, hieße aber, beliebige Kommandos durch die
// Whitelist zu lassen - genau das, was der eingeschränkte Modus verhindert.
func (s *ServerService) rescanApps(conn sshx.Conn, server *domain.Server) {
	if s.apps == nil {
		return
	}
	entries, err := s.apps.FindEnabled()
	if err != nil || len(entries) == 0 {
		return
	}
	script := appScanScript(entries, server.PackageManager)
	cmd := script
	if !server.RestrictedSudo {
		cmd = privRun(server, script)
	}
	out, _, err := conn.Run(cmd)
	if err != nil {
		return
	}
	// Der Exit-Code zählt hier bewusst nicht: Das Skript endet mit dem Status
	// des letzten Merkmals, und ein nicht gefundener Pfad ist kein Fehler.
	apps, unknown := parseAppScan(out, entries)
	_ = s.apps.ReplaceDetected(server.ID, apps)
	_ = s.apps.ReplaceUnknown(server.ID, unknown)
}
