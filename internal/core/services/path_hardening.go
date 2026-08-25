package services

import (
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Verzeichnisse abschotten.
//
// Ein Berechtigungsprofil GIBT Rechte - es nimmt keine weg. Der Grundzustand
// eines frischen Linux ist lesefreundlich: `/srv`, `/opt` und die
// Konfigurationsverzeichnisse einzelner Dienste stehen meist auf 755, weil das
// Paket sie so ausliefert. Wer will, dass ein Datenbereich nur den Berechtigten
// offensteht, muss deshalb ZUERST den Grundzustand härten und DANN über ein
// Profil gezielt freigeben.
//
// Das ist bewusst kein Profil-Bestandteil, sondern eine eigene Aktion je
// Verzeichnis: Sie gilt unabhängig davon, ob je ein Profil auf den Pfad zeigt.

// hardenScript baut das Skript zum Abschotten eines Verzeichnisses.
//
// Drei Vorkehrungen stecken darin, und jede hat einen Grund:
//
//   - Der VORZUSTAND wird ausgegeben, bevor etwas geändert wird. Ohne ihn gäbe
//     es keine Rücknahme - und eine Härtung, die man nicht zurücknehmen kann,
//     traut sich niemand anzuwenden.
//   - Die Gruppe wird gesetzt, BEVOR das Welt-Recht fällt. `/var/www` steht
//     typischerweise auf 755 root:root, und der nginx-Worker liest als
//     www-data über genau dieses Welt-Recht. 750 root:root legt die Seite
//     lahm; richtig ist 750 root:www-data.
//   - Ist eine Unit angegeben, wird danach geprüft, ob sie noch läuft - und
//     bei Fehlschlag alles zurückgenommen. Eine Härtung, die einen Dienst
//     abschießt, darf nicht bestehen bleiben.
//
// Nicht rekursiv: Fehlt am Kopf eines Baums das Durchgangsrecht, ist alles
// darunter unerreichbar. Ein `chmod -R` fasste stattdessen jede Datei an,
// dauerte auf großen Beständen ewig und wäre kaum zurückzunehmen.
func hardenScript(path, group, unit string) string {
	steps := []string{
		fmt.Sprintf("[ -d %s ] || { echo 'kein verzeichnis: %s' >&2; exit 1; }", path, path),
		fmt.Sprintf("[ -L %s ] && { echo 'pfad ist ein symlink: %s' >&2; exit 1; }", path, path),
		// Vorzustand ausgeben - die Grundlage der Rücknahme.
		fmt.Sprintf("echo \"LCM-VORHER: $(stat -c '%%a %%G' %s)\"", path),
	}
	if group != "" {
		steps = append(steps, fmt.Sprintf("chgrp %s %s", group, path))
	}
	steps = append(steps, fmt.Sprintf("chmod o-rwx %s", path))
	if unit != "" {
		// Wirkungsprobe: Läuft der Dienst danach nicht mehr, war die Härtung
		// zu eng - dann wird sie sofort zurückgenommen, statt ein kaputtes
		// System zu hinterlassen.
		// Nur prüfen, wo es systemctl GIBT: Auf Alpine und anderen
		// OpenRC-Systemen fehlt es, und ein „command not found" sähe aus wie
		// ein toter Dienst - die Härtung würde sofort und immer
		// zurückgenommen.
		steps = append(steps, strings.Join([]string{
			fmt.Sprintf("if command -v systemctl >/dev/null 2>&1 && ! systemctl is-active --quiet %s; then", unit),
			fmt.Sprintf("  echo 'dienst %s läuft nach dem abschotten nicht mehr - zurückgenommen' >&2;", unit),
			fmt.Sprintf("  chmod o+rX %s;", path),
			"  exit 2;",
			fmt.Sprintf("elif ! command -v systemctl >/dev/null 2>&1; then echo 'kein systemd - dienstprobe für %s übersprungen';", unit),
			"fi",
		}, " "))
	}
	steps = append(steps, fmt.Sprintf("echo \"LCM-NACHHER: $(stat -c '%%a %%G' %s)\"", path))
	return strings.Join(steps, "; ")
}

// restoreScript stellt den aufgezeichneten Vorzustand wieder her.
func restoreScript(hardened *domain.HardenedPath) string {
	steps := []string{fmt.Sprintf("[ -d %s ] || exit 0", hardened.Path)}
	if hardened.PrevGroup != "" {
		steps = append(steps, fmt.Sprintf("chgrp %s %s", hardened.PrevGroup, hardened.Path))
	}
	if hardened.PrevMode != "" {
		steps = append(steps, fmt.Sprintf("chmod %s %s", hardened.PrevMode, hardened.Path))
	}
	return strings.Join(steps, "; ")
}

// parseHardenState liest „LCM-VORHER: 755 root" aus der Skript-Ausgabe.
func parseHardenState(out, marker string) (mode, group string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		rest, found := strings.CutPrefix(line, marker)
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) >= 2 {
			return fields[0], fields[1]
		}
	}
	return "", ""
}

// HardenPath schottet ein Verzeichnis ab: Welt-Zugriff weg, optional die
// Gruppe des Diensts gesetzt, optional die Unit danach geprüft.
func (s *ServerService) HardenPath(scope repositories.AccessScope, id uint, path, group, unit, actor string) (*domain.HardenedPath, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	if err := domain.ValidateHardenTarget(path, group, unit); err != nil {
		return nil, err
	}
	conn, err := s.connectRec(server, "path-harden", actor)
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	out, code, runErr := conn.Run(privRun(server, hardenScript(path, group, unit)))
	if runErr != nil {
		return nil, runErr
	}
	if code != 0 {
		return nil, fmt.Errorf("abschotten fehlgeschlagen (exit %d): %s", code, summarize(out))
	}
	prevMode, prevGroup := parseHardenState(out, "LCM-VORHER:")
	mode, nowGroup := parseHardenState(out, "LCM-NACHHER:")
	hardened := &domain.HardenedPath{
		ServerID: server.ID, Path: path,
		PrevMode: prevMode, PrevGroup: prevGroup, Mode: mode, Group: nowGroup, Unit: unit,
	}
	if err := s.servers.SaveHardenedPath(hardened); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "server.path-harden", "server", server.ID,
		fmt.Sprintf("%s: %s - %s %s → %s %s", server.Name, path, prevMode, prevGroup, mode, nowGroup))
	return hardened, nil
}

// RestorePath nimmt eine Härtung zurück.
func (s *ServerService) RestorePath(scope repositories.AccessScope, id, hardenedID uint, actor string) error {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return err
	}
	hardened, err := s.servers.FindHardenedPath(server.ID, hardenedID)
	if err != nil {
		return err
	}
	conn, err := s.connectRec(server, "path-restore", actor)
	if err != nil {
		return fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	out, code, runErr := conn.Run(privRun(server, restoreScript(hardened)))
	if runErr != nil {
		return runErr
	}
	if code != 0 {
		return fmt.Errorf("rücknahme fehlgeschlagen (exit %d): %s", code, summarize(out))
	}
	if err := s.servers.DeleteHardenedPath(hardened.ID); err != nil {
		return err
	}
	s.audit.Log(actor, "server.path-restore", "server", server.ID,
		fmt.Sprintf("%s: %s → %s %s", server.Name, hardened.Path, hardened.PrevMode, hardened.PrevGroup))
	return nil
}

// HardenedPaths liefert die abgeschotteten Verzeichnisse eines Servers.
func (s *ServerService) HardenedPaths(scope repositories.AccessScope, id uint) ([]domain.HardenedPath, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	return s.servers.HardenedPaths(server.ID)
}

// ---- Generelle Härtung: Vorschläge --------------------------------------------

// hardenDataRoots sind die Wurzeln, unter denen Datenbereiche liegen. Geprüft
// werden nur die DIREKTEN Kinder - ein Verzeichnis je Anwendung, nicht der
// ganze Baum.
var hardenDataRoots = []string{"/srv", "/opt", "/var/www", "/data"}

// hardenConfigDirs ist die kuratierte Liste der Konfigurationsverzeichnisse,
// die LCM zum Abschotten VORSCHLÄGT.
//
// Bewusst eine Positivliste statt „alles unter /etc außer …": Unter /etc liegt
// auch, was world-readable bleiben MUSS - `profile.d`, `alternatives`,
// `ssl/certs`, die Init-Skripte. Eine Ausschlussliste wäre nie vollständig, und
// der erste vergessene Eintrag legt ein System lahm. Vorgeschlagen wird nur,
// wovon bekannt ist, dass der Dienst seine Konfiguration als root oder unter
// eigener Kennung liest.
var hardenConfigDirs = []string{
	"/etc/nginx", "/etc/apache2", "/etc/httpd", "/etc/lighttpd", "/etc/caddy",
	"/etc/postfix", "/etc/dovecot", "/etc/opendkim",
	"/etc/mysql", "/etc/my.cnf.d", "/etc/postgresql", "/etc/redis", "/etc/mongod.d",
	"/etc/grafana", "/etc/prometheus", "/etc/loki", "/etc/telegraf",
	"/etc/samba", "/etc/vsftpd", "/etc/proftpd",
	"/etc/letsencrypt", "/etc/dehydrated",
	"/etc/php", "/etc/rspamd", "/etc/unbound", "/etc/bind", "/etc/powerdns",
	"/etc/docker", "/etc/containerd", "/etc/wireguard", "/etc/openvpn",
}

// HardenSuggestion ist ein Verzeichnis, dessen Welt-Zugriff LCM zum Entfernen
// vorschlägt.
type HardenSuggestion struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	// Group ist die aktuelle Gruppe; ServiceGroup die Kennung, unter der
	// Dateien darin liegen - der Vorschlag für das Abschotten.
	Group        string `json:"group"`
	ServiceGroup string `json:"service_group"`
	// Kind unterscheidet Datenbereich und Konfigurationsverzeichnis.
	Kind string `json:"kind"`
}

// hardenSuggestScript sucht Verzeichnisse mit Welt-Rechten.
//
// Ermittelt wird zusätzlich die Gruppe, unter der die INHALTE liegen. Das ist
// der entscheidende Wert: `/var/www` gehört meist root:root, die Dateien darin
// aber www-data - und genau diese Gruppe muss das Verzeichnis bekommen, bevor
// das Welt-Recht fällt. Ohne sie verliert der Dienst den Zugriff.
func hardenSuggestScript() string {
	var steps []string
	report := `m=$(stat -c '%a' "$d"); [ $(( 0$m %% 8 )) -ne 0 ] || continue; ` +
		`g=$(stat -c '%G' "$d"); ` +
		// Gruppe der Inhalte: die häufigste unter den ersten Einträgen.
		`sg=$(find "$d" -mindepth 1 -maxdepth 1 -printf '%%g\n' 2>/dev/null | sort | uniq -c | sort -rn | head -1 | awk '{print $2}'); ` +
		`echo "LCM-KAND|%s|$d|$m|$g|${sg:-$g}"`
	for _, root := range hardenDataRoots {
		steps = append(steps, fmt.Sprintf(
			`[ -d %s ] && for d in %s/*; do [ -d "$d" ] || continue; [ -L "$d" ] && continue; %s; done`,
			root, root, fmt.Sprintf(report, "daten")))
	}
	steps = append(steps, fmt.Sprintf(
		`for d in %s; do [ -d "$d" ] || continue; [ -L "$d" ] && continue; %s; done`,
		strings.Join(hardenConfigDirs, " "), fmt.Sprintf(report, "konfig")))
	return strings.Join(steps, "\n")
}

// parseHardenSuggestions liest die Kandidatenzeilen des Skripts.
func parseHardenSuggestions(out string) []HardenSuggestion {
	var found []HardenSuggestion
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "LCM-KAND|")
		if !ok {
			continue
		}
		parts := strings.Split(rest, "|")
		if len(parts) != 5 {
			continue
		}
		found = append(found, HardenSuggestion{
			Kind: parts[0], Path: parts[1], Mode: parts[2], Group: parts[3], ServiceGroup: parts[4],
		})
	}
	return found
}

// HardenSuggestions sucht auf dem Server Verzeichnisse, deren Welt-Zugriff sich
// entfernen ließe - bereits gehärtete und gesperrte Pfade fallen heraus.
func (s *ServerService) HardenSuggestions(scope repositories.AccessScope, id uint, actor string) ([]HardenSuggestion, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	conn, err := s.connectRecRead(server, "harden-suggest", actor)
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	out, code, runErr := conn.Run(privRun(server, hardenSuggestScript()))
	if runErr != nil {
		return nil, runErr
	}
	if code != 0 && strings.TrimSpace(out) == "" {
		return nil, fmt.Errorf("suche fehlgeschlagen (exit %d)", code)
	}
	already, err := s.servers.HardenedPaths(server.ID)
	if err != nil {
		return nil, err
	}
	done := make(map[string]bool, len(already))
	for i := range already {
		done[already[i].Path] = true
	}
	var out2 []HardenSuggestion
	for _, sug := range parseHardenSuggestions(out) {
		if done[sug.Path] || domain.ValidateHardenTarget(sug.Path, "", "") != nil {
			continue
		}
		out2 = append(out2, sug)
	}
	return out2, nil
}

// HardenPathsBulk schottet mehrere Verzeichnisse in EINER Verbindung ab und
// liefert je Pfad ein Ergebnis. Ein Fehlschlag hält die übrigen nicht auf -
// ein Verzeichnis, das gerade nicht existiert, darf die Härtung der anderen
// nicht verhindern.
func (s *ServerService) HardenPathsBulk(scope repositories.AccessScope, id uint, targets []HardenTarget, actor string) ([]HardenResult, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	for _, target := range targets {
		if err := domain.ValidateHardenTarget(target.Path, target.Group, target.Unit); err != nil {
			return nil, err
		}
	}
	conn, err := s.connectRec(server, "path-harden", actor)
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	results := make([]HardenResult, 0, len(targets))
	for _, target := range targets {
		out, code, runErr := conn.Run(privRun(server, hardenScript(target.Path, target.Group, target.Unit)))
		res := HardenResult{Path: target.Path, OK: runErr == nil && code == 0}
		switch {
		case runErr != nil:
			res.Message = runErr.Error()
		case code != 0:
			res.Message = summarize(out)
		default:
			prevMode, prevGroup := parseHardenState(out, "LCM-VORHER:")
			mode, nowGroup := parseHardenState(out, "LCM-NACHHER:")
			hardened := &domain.HardenedPath{
				ServerID: server.ID, Path: target.Path,
				PrevMode: prevMode, PrevGroup: prevGroup, Mode: mode, Group: nowGroup, Unit: target.Unit,
			}
			if err := s.servers.SaveHardenedPath(hardened); err != nil {
				return nil, err
			}
			res.Message = fmt.Sprintf("%s %s → %s %s", prevMode, prevGroup, mode, nowGroup)
			s.audit.Log(actor, "server.path-harden", "server", server.ID,
				fmt.Sprintf("%s: %s - %s", server.Name, target.Path, res.Message))
		}
		results = append(results, res)
	}
	return results, nil
}

// HardenTarget ist ein Verzeichnis samt gewünschter Gruppe und optionaler
// Dienstprobe.
type HardenTarget struct {
	Path  string `json:"path"`
	Group string `json:"group"`
	Unit  string `json:"unit"`
}

// HardenResult protokolliert das Abschotten je Pfad.
type HardenResult struct {
	Path    string `json:"path"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}
