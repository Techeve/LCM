package services

import (
	"fmt"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
)

// Rechte-Soll gegen Drift.
//
// Einmal setzen genügt nicht: Paketaktualisierungen bringen Eigentümer und
// Rechte der ausgelieferten Dateien mit und setzen eine Härtung zurück,
// systemd-tmpfiles stellt Modi verwalteter Pfade beim Booten wieder her, und
// eine beim Update ERSETZTE Datei ist ein neuer Inode - ihre ACL ist weg.
//
// Der Soll-Zustand liegt in LCM ohnehin vor. Also derselbe Weg wie bei
// Firewall und apt-Cache: vergleichen, nur bei Abweichung eingreifen, den
// Eingriff protokollieren. Nebeneffekt: Handarbeit am System fällt auf.

// hardeningDriftScript prüft die gehärteten Verzeichnisse eines Servers und
// stellt den Soll-Zustand nur dort wieder her, wo er nicht mehr gilt.
//
// Geprüft wird über die Welt-Rechte im Modus: Sobald sie wieder gesetzt sind,
// ist die Härtung aufgegangen. Die Ausgabe nennt jeden Eingriff, damit der
// Bericht nicht nur „geprüft" sagt.
func hardeningDriftScript(paths []domain.HardenedPath) string {
	var steps []string
	for i := range paths {
		p := &paths[i]
		fix := fmt.Sprintf("chmod o-rwx %s", p.Path)
		if p.Group != "" {
			fix = fmt.Sprintf("chgrp %s %s; %s", p.Group, p.Path, fix)
		}
		// Modulo 8, nicht 10: `stat -c %a` liefert die Rechte OKTAL, und die
		// Shell liest „0750" mit führender Null auch als Oktalzahl. Die
		// Welt-Bits sind damit der Rest modulo 8 - mit modulo 10 wäre ein
		// korrekt gehärtetes 750 (dezimal 488, Rest 8) fälschlich als Drift
		// gewertet worden, und die Regel hätte bei JEDEM Health-Ping
		// „eingegriffen".
		steps = append(steps, strings.Join([]string{
			fmt.Sprintf("if [ -d %s ] && [ $(( 0$(stat -c '%%a' %s) %% 8 )) -ne 0 ]; then", p.Path, p.Path),
			fmt.Sprintf("  %s;", fix),
			fmt.Sprintf("  echo 'LCM-DRIFT: %s neu abgeschottet';", p.Path),
			"fi",
		}, " "))
	}
	return strings.Join(steps, "\n")
}

// aclDriftScript prüft stichprobenartig, ob die ACL-Einträge der Profile noch
// stehen - auf dem WURZELPFAD je Regel, nicht rekursiv.
//
// Der Voll-Abgleich läuft bei jedem Benutzer-Sync; ihn zusätzlich bei jedem
// Health-Ping über große Bestände zu jagen, wäre reine Last. Fehlt der Eintrag
// an der Wurzel, ist etwas passiert - dann meldet die Regel es, und der
// nächste Sync zieht den ganzen Baum nach.
func aclDriftScript(paths []domain.AppliedProfilePath, slugs map[uint]string) string {
	var steps []string
	for i := range paths {
		slug := slugs[paths[i].ProfileID]
		if slug == "" {
			continue
		}
		group := domain.ProfileGroupName(slug)
		steps = append(steps, fmt.Sprintf(
			"[ -d %s ] && { getfacl -p %s 2>/dev/null | grep -q '^group:%s:' || echo 'LCM-DRIFT: ACL fehlt auf %s'; }",
			paths[i].Path, paths[i].Path, group, paths[i].Path))
	}
	return strings.Join(steps, "\n")
}

// enforcePermSync setzt die Grundsatz-Regel „Rechte-Soll" um.
func (e *Executor) enforcePermSync(conn sshx.Conn, server *domain.Server, rule *domain.Rule) (string, error) {
	if e.profiles == nil {
		return fmt.Sprintf("  [%s] übersprungen: Berechtigungsprofile nicht verdrahtet", rule.Name), nil
	}
	hardened, err := e.servers.HardenedPaths(server.ID)
	if err != nil {
		return fmt.Sprintf("  [%s] gehärtete Pfade nicht lesbar: %v", rule.Name, err), nil
	}
	applied, err := e.profiles.AppliedPathsForServer(server.ID)
	if err != nil {
		return fmt.Sprintf("  [%s] gesetzte Verzeichnisrechte nicht lesbar: %v", rule.Name, err), nil
	}
	if len(hardened) == 0 && len(applied) == 0 {
		return fmt.Sprintf("  [%s] nichts zu prüfen - weder gehärtete Pfade noch Verzeichnisrechte", rule.Name), nil
	}

	slugs := map[uint]string{}
	for i := range applied {
		if _, known := slugs[applied[i].ProfileID]; known {
			continue
		}
		if profile, err := e.profiles.FindByID(applied[i].ProfileID); err == nil {
			slugs[applied[i].ProfileID] = profile.Slug
		}
	}

	script := strings.TrimSpace(hardeningDriftScript(hardened) + "\n" + aclDriftScript(applied, slugs))
	if script == "" {
		return fmt.Sprintf("  [%s] nichts zu prüfen", rule.Name), nil
	}
	out, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return fmt.Sprintf("  [%s] ABWEICHUNG - prüfung fehlgeschlagen: %v", rule.Name, runErr), runErr
	}
	if code != 0 {
		return fmt.Sprintf("  [%s] prüfung endete mit exit %d: %s", rule.Name, code, summarize(out)), nil
	}
	drifts := driftLines(out)
	if len(drifts) == 0 {
		return fmt.Sprintf("  [%s] rechte-soll ok - %d Pfad(e) geprüft", rule.Name, len(hardened)+len(applied)), nil
	}
	// Ein tatsächlicher Eingriff ändert den Zustand eines Produktivsystems -
	// das gehört ins Audit, nicht nur in den Output eines fremden Jobs.
	e.recordEnforcement(server, rule, "Rechte-Soll: "+strings.Join(drifts, "; "))
	return fmt.Sprintf("  [%s] abweichung erkannt:\n    %s", rule.Name, strings.Join(drifts, "\n    ")), nil
}

// driftLines sammelt die Meldungen des Prüf-Skripts.
func driftLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), "LCM-DRIFT: "); found {
			lines = append(lines, rest)
		}
	}
	return lines
}

// enforceACLSetup setzt die Grundsatz-Regel „ACL einrichten" um: Fehlt die
// ACL-Unterstützung, wird das Paket nachinstalliert - sonst passiert nichts.
//
// Kein Fehlschlag in Schleife: Scheitert die Installation (kein Repo-Zugang),
// darf das nicht bei jedem Health-Ping erneut versucht und jedes Mal als
// Eingriff protokolliert werden. Der Fehlversuch wird am Server vermerkt und
// frühestens nach der Sperrfrist wiederholt.
func (e *Executor) enforceACLSetup(conn sshx.Conn, server *domain.Server, rule *domain.Rule) (string, error) {
	if server.ACLUsable {
		return fmt.Sprintf("  [%s] acl ok - verzeichnisrechte sind nutzbar", rule.Name), nil
	}
	if wait := aclRetryWait(server); wait != "" {
		return fmt.Sprintf("  [%s] übersprungen: letzter Versuch schlug fehl, nächster %s", rule.Name, wait), nil
	}
	out, code, runErr := conn.Run(privRun(server, pkgInstallScript(server.PackageManager, []string{"acl"})))
	probe := ""
	if runErr == nil && code == 0 {
		probe, _, _ = conn.Run(privRun(server, aclProbeScript))
	}
	if !strings.Contains(probe, "acl-ok") {
		_ = e.servers.UpdateFields(server.ID, map[string]any{"acl_retry_after": aclRetryDeadline()})
		return fmt.Sprintf("  [%s] ACL-Unterstützung konnte nicht eingerichtet werden: %s",
			rule.Name, summarize(out)), nil
	}
	_ = e.servers.UpdateFields(server.ID, map[string]any{
		"has_acl": true, "acl_usable": true, "acl_retry_after": nil,
	})
	e.recordEnforcement(server, rule, "ACL-Unterstützung eingerichtet (Paket acl)")
	return fmt.Sprintf("  [%s] abweichung erkannt - acl-unterstützung eingerichtet", rule.Name), nil
}

// aclRetryInterval ist die Sperrfrist nach einem gescheiterten Versuch, die
// ACL-Unterstützung einzurichten.
const aclRetryInterval = 24 * time.Hour

// aclRetryDeadline liefert den Zeitpunkt, ab dem erneut versucht werden darf.
func aclRetryDeadline() time.Time { return time.Now().Add(aclRetryInterval) }

// aclRetryWait meldet, wie lange die Sperrfrist noch läuft ("" = kein Halt).
func aclRetryWait(server *domain.Server) string {
	if server.ACLRetryAfter == nil || time.Now().After(*server.ACLRetryAfter) {
		return ""
	}
	return "frühestens " + server.ACLRetryAfter.Format("15:04 02.01.2006")
}
