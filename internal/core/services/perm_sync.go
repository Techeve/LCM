package services

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
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
//
// Zwei Schritte: Ein rein lesendes Skript liefert den Ist-Zustand,
// verglichen wird in Go, und nur für abweichende Pfade läuft ein zweites
// Skript, das korrigiert. Der Vergleich gehört nicht in die Shell: Trägt ein
// gehärteter Pfad zugleich eine Profil-ACL, zeigt `stat` in den Gruppen-Bits
// die ACL-Maske statt der Rechte der Gruppe - das in sh auseinanderzuhalten,
// wäre kaum noch lesbar.

// hardenedProbeScript liest den Ist-Zustand der gehärteten Pfade, ohne etwas
// zu ändern - Dateien wie Verzeichnisse.
//
// Je Pfad eine Zeile `LCM-IST|<nr>|<modus>|<gruppe>|<group::>|<mask::>`. Ein
// Symlink wird NICHT verfolgt: `[ -L ]` steht vor jeder anderen Prüfung, denn
// `[ -e ]`, `stat` und `getfacl` lösen ihn alle auf. Ein Symlink an einem
// gehärteten Pfad ist selbst die Abweichung (`LCM-IST|<nr>|link`). Ein
// fehlender Pfad ergibt keine Zeile - dort steht nichts offen.
func hardenedProbeScript(paths []domain.HardenedPath) string {
	var steps []string
	for i := range paths {
		p := shellQuote(paths[i].Path)
		steps = append(steps, strings.Join([]string{
			fmt.Sprintf("if [ -L %s ]; then echo 'LCM-IST|%d|link';", p, i),
			fmt.Sprintf("elif [ -e %s ]; then a=$(getfacl -p %s 2>/dev/null);", p, p),
			fmt.Sprintf(`echo "LCM-IST|%d|$(stat -c '%%a|%%G' %s)|$(aclField group "$a")|$(aclField mask "$a")";`, i, p),
			"fi",
		}, " "))
	}
	if len(steps) == 0 {
		return ""
	}
	// aclField zieht die Rechte eines Basis-Eintrags (group::, mask::) aus
	// der getfacl-Ausgabe; leer, wenn es ihn nicht gibt.
	helper := `aclField() { printf '%s\n' "$2" | sed -n "s/^$1::\([-rwx]*\).*/\1/p"; }`
	return helper + "\n" + strings.Join(steps, "\n")
}

// pathState ist der Ist-Zustand eines gehärteten Pfads.
type pathState struct {
	link  bool
	mode  uint64
	group string
	// groupEntry ist der ACL-Eintrag der besitzenden Gruppe (`group::r-x`),
	// mask die ACL-Maske - leer, wenn der Pfad keine erweiterte ACL trägt.
	groupEntry string
	mask       string
}

// parseHardenedProbe liest die Zeilen des Prüf-Skripts, je Pfadnummer ein
// Zustand. Eine unlesbare Zeile (stat ohne Zugriff) bleibt als nil drin,
// damit sie gemeldet statt stillschweigend als „in Ordnung" gezählt wird.
func parseHardenedProbe(out string) map[int]*pathState {
	states := map[int]*pathState{}
	for _, line := range strings.Split(out, "\n") {
		rest, found := strings.CutPrefix(strings.TrimSpace(line), "LCM-IST|")
		if !found {
			continue
		}
		fields := strings.Split(rest, "|")
		nr, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		states[nr] = probeState(fields[1:])
	}
	return states
}

// probeState wertet die Felder einer Prüfzeile aus; nil = nicht lesbar.
func probeState(fields []string) *pathState {
	if len(fields) == 1 && fields[0] == "link" {
		return &pathState{link: true}
	}
	if len(fields) != 4 {
		return nil
	}
	mode, ok := parseMode(fields[0])
	if !ok || fields[1] == "" {
		return nil
	}
	return &pathState{mode: mode, group: fields[1], groupEntry: fields[2], mask: fields[3]}
}

// parseMode liest einen oktalen Modus aus `stat -c %a` (mit Sonderbits).
func parseMode(s string) (uint64, bool) {
	mode, err := strconv.ParseUint(s, 8, 32)
	if err != nil || mode > 0o7777 {
		return 0, false
	}
	return mode, true
}

// hardenedFix vergleicht Ist und Soll eines gehärteten Pfads und liefert die
// Befehle, die das Soll wiederherstellen - leer, wenn alles stimmt.
//
// Verglichen wird VOLLSTÄNDIG: alle Rechte-Bits samt setuid/setgid/sticky und
// die Gruppe. Nur die Welt-Bits zu prüfen, ließ ein von 0700 auf 0770
// geöffnetes Verzeichnis als „in Ordnung" durchgehen. chmod bekommt den Modus
// fünfstellig: Nur so löscht GNU chmod setuid/setgid auf einem Verzeichnis,
// statt sie stillschweigend zu behalten.
//
// Trägt der Pfad eine erweiterte ACL, sind die Gruppen-Bits im Modus die
// ACL-MASKE - sie wächst mit jeder Profil-Freigabe auf demselben Pfad, und
// die Härtung hätte sonst bei jedem Lauf die Profilrechte gekappt. Dann gilt:
// Besitzer-, Welt- und Sonderbits exakt, die Maske bleibt, wie sie ist, und
// der Eintrag `group::` darf nicht MEHR erlauben als das Soll. Gleichheit
// lässt sich hier nicht verlangen: Wurde nach der Profil-Freigabe gehärtet,
// ist schon der gespeicherte Soll-Modus die Maske - die Gruppe würde
// aufgeweitet.
func hardenedFix(p *domain.HardenedPath, is *pathState) []string {
	want, ok := parseMode(p.Mode)
	if !ok {
		// Ohne bekannten Soll-Modus bleibt die alte Mindestgarantie: kein
		// Welt-Zugriff.
		want = is.mode &^ 0o007
	}
	path := shellQuote(p.Path)
	groupDrift := p.Group != "" && is.group != p.Group
	if is.mask == "" {
		if !groupDrift && is.mode == want {
			return nil
		}
		return withChgrp(p, []string{fmt.Sprintf("chmod %05o %s", want, path)})
	}
	keep := (want &^ 0o070) | (is.mode & 0o070)
	allowed := aclBits(is.groupEntry) & (want >> 3 & 0o7)
	entryDrift := aclBits(is.groupEntry) != allowed
	if !groupDrift && is.mode == keep && !entryDrift {
		return nil
	}
	cmds := []string{fmt.Sprintf("chmod %05o %s", keep, path)}
	if entryDrift {
		cmds = append(cmds, fmt.Sprintf("setfacl -m g::%s %s", aclPerms(allowed), path))
	}
	return withChgrp(p, cmds)
}

// withChgrp stellt den Gruppenwechsel VOR die übrigen Befehle: chgrp löscht
// auf ausführbaren Dateien setuid/setgid, das chmod danach setzt sie wieder.
// `-h` fasst einen inzwischen untergeschobenen Symlink selbst an, nicht sein
// Ziel.
func withChgrp(p *domain.HardenedPath, cmds []string) []string {
	if p.Group == "" {
		return cmds
	}
	chgrp := fmt.Sprintf("chgrp -h %s %s", shellQuote(p.Group), shellQuote(p.Path))
	return append([]string{chgrp}, cmds...)
}

// aclBits wandelt „r-x" in die Bits 5.
func aclBits(perms string) uint64 {
	var bits uint64
	for i, c := range "rwx" {
		if strings.ContainsRune(perms, c) {
			bits |= 4 >> i
		}
	}
	return bits
}

// aclPerms wandelt die Bits 5 in „r-x".
func aclPerms(bits uint64) string {
	out := []byte("---")
	for i, c := range "rwx" {
		if bits&(4>>i) != 0 {
			out[i] = byte(c)
		}
	}
	return string(out)
}

// hardenedFixScript stellt das Soll der abweichenden Pfade her. Vor dem
// Eingriff wird der Symlink-Test wiederholt: Zwischen Prüfung und Korrektur
// liegt ein Rundweg, und chmod folgt einem Symlink immer. Je Pfad meldet eine
// Zeile `LCM-FIX|<nr>|ok|fehler|link`, ob der Eingriff gelang - im
// eingeschränkten Modus laufen chmod und chgrp ohne root und scheitern an
// fremden Pfaden. Das darf nicht als „neu abgeschottet" durchgehen.
func hardenedFixScript(paths []domain.HardenedPath, fixes map[int][]string) string {
	var steps []string
	for i := range paths {
		cmds, found := fixes[i]
		if !found {
			continue
		}
		steps = append(steps, fmt.Sprintf(
			"if [ -L %s ]; then echo 'LCM-FIX|%d|link'; elif %s; then echo 'LCM-FIX|%d|ok'; else echo 'LCM-FIX|%d|fehler'; fi",
			shellQuote(paths[i].Path), i, strings.Join(cmds, " && "), i, i))
	}
	return strings.Join(steps, "\n")
}

// markedResults liest Zeilen der Form `<marke><nr>|<ergebnis>`.
func markedResults(out, marker string) map[int]string {
	results := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		rest, found := strings.CutPrefix(strings.TrimSpace(line), marker)
		if !found {
			continue
		}
		nr, result, _ := strings.Cut(rest, "|")
		if n, err := strconv.Atoi(nr); err == nil {
			results[n] = result
		}
	}
	return results
}

// hardenedDrift wertet die Prüfung der gehärteten Pfade aus, korrigiert die
// abweichenden in EINEM weiteren Lauf und liefert je Pfad eine Meldung:
// getrennt nach nur gemeldet (reported) und tatsächlich korrigiert (fixed).
func hardenedDrift(conn sshx.Conn, server *domain.Server, paths []domain.HardenedPath, probeOut string) (reported, fixed []string) {
	states := parseHardenedProbe(probeOut)
	var drifts []string
	fixes := map[int][]string{}
	for i := range paths {
		state, found := states[i]
		switch {
		case !found:
			continue
		case state == nil:
			drifts = append(drifts, paths[i].Path+" - Ist-Zustand nicht lesbar")
		case state.link:
			drifts = append(drifts, paths[i].Path+" ist ein Symlink - nicht angefasst")
		default:
			if cmds := hardenedFix(&paths[i], state); len(cmds) > 0 {
				fixes[i] = cmds
			}
		}
	}
	if len(fixes) == 0 {
		return drifts, nil
	}
	out, _, runErr := conn.Run(privRun(server, hardenedFixScript(paths, fixes)))
	results := markedResults(out, "LCM-FIX|")
	for i := range paths {
		if _, found := fixes[i]; !found {
			continue
		}
		was := fmt.Sprintf("%o %s", states[i].mode, states[i].group)
		switch {
		case runErr == nil && results[i] == "ok":
			fixed = append(fixed, fmt.Sprintf("%s neu abgeschottet (war %s)", paths[i].Path, was))
		case results[i] == "link":
			drifts = append(drifts, paths[i].Path+" ist ein Symlink - nicht angefasst")
		default:
			drifts = append(drifts, fmt.Sprintf("%s weicht ab (ist %s) - Korrektur fehlgeschlagen", paths[i].Path, was))
		}
	}
	return drifts, fixed
}

// aclDriftScript prüft stichprobenartig, ob die ACL-Einträge der Profile noch
// stehen - auf dem WURZELPFAD je Regel, nicht rekursiv.
//
// Der Voll-Abgleich läuft bei jedem Benutzer-Sync; ihn zusätzlich bei jedem
// Health-Ping über große Bestände zu jagen, wäre reine Last. Fehlt der Eintrag
// an der Wurzel, ist etwas passiert - dann meldet die Regel es, und der
// nächste Sync zieht den ganzen Baum nach.
//
// Geprüft wird der Eintrag SAMT Rechten als ganze Zeile: Dass für die Gruppe
// irgendein Eintrag existiert, besagt nichts - `group:lcm-prof-web:---`
// existiert auch. Die ganze Zeile fängt zugleich eine gekappte Maske ab:
// Wirkt ein Eintrag schwächer als gesetzt, hängt getfacl `#effective:` an,
// und die Zeile stimmt nicht mehr. Bei Freigaben gehört der vererbende
// Default-Eintrag dazu. Ein Symlink an der Wurzel wird nicht verfolgt
// (getfacl löst ihn auf) und ist selbst die Abweichung.
func aclDriftScript(paths []domain.AppliedProfilePath, slugs map[uint]string) string {
	var steps []string
	for i := range paths {
		slug := slugs[paths[i].ProfileID]
		if slug == "" {
			continue
		}
		p := shellQuote(paths[i].Path)
		entry := "group:" + domain.ProfileGroupName(slug) + ":" + aclEntryPerms(paths[i].Mode)
		check := fmt.Sprintf(`printf '%%s\n' "$a" | grep -qxF %s`, shellQuote(entry))
		if paths[i].Mode != domain.PathModeDeny {
			check += fmt.Sprintf(` && printf '%%s\n' "$a" | grep -qxF %s`, shellQuote("default:"+entry))
		}
		steps = append(steps, fmt.Sprintf(
			"if [ -L %s ]; then echo 'LCM-ACL|%d|link'; elif [ -d %s ]; then a=$(getfacl -p %s 2>/dev/null); %s || echo 'LCM-ACL|%d|abweichung'; fi",
			p, i, p, p, check, i))
	}
	return strings.Join(steps, "\n")
}

// aclEntryPerms ist die Rechte-Spalte, die getfacl für den Eintrag eines
// Profils auf einem Verzeichnis zeigt - abgeleitet aus der setfacl-Angabe
// (`rX` wird auf einem Verzeichnis zu `r-x`).
func aclEntryPerms(mode string) string {
	return aclPerms(aclBits(strings.ToLower(aclSpecFor(mode))))
}

// aclDrifts übersetzt die Ergebniszeilen der ACL-Prüfung in Meldungen.
func aclDrifts(out string, paths []domain.AppliedProfilePath) []string {
	results := markedResults(out, "LCM-ACL|")
	var drifts []string
	for i := range paths {
		switch results[i] {
		case "link":
			drifts = append(drifts, paths[i].Path+" ist ein Symlink - ACL nicht geprüft")
		case "abweichung":
			drifts = append(drifts, fmt.Sprintf("ACL weicht ab auf %s (soll group:%s)", paths[i].Path, aclEntryPerms(paths[i].Mode)))
		}
	}
	return drifts
}

// enforcePermSync setzt die Grundsatz-Regel „Rechte-Soll" um.
//
// Gehärtete Pfade werden korrigiert, ACL-Abweichungen nur gemeldet: Die ACL
// zieht der nächste Benutzer-Sync über den ganzen Baum nach - die Stichprobe
// an der Wurzel reicht zum Erkennen, nicht zum Reparieren.
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

	script := strings.TrimSpace(hardenedProbeScript(hardened) + "\n" + aclDriftScript(applied, slugs))
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
	reported, fixed := hardenedDrift(conn, server, hardened, out)
	reported = append(reported, aclDrifts(out, applied)...)
	drifts := append(fixed, reported...)
	key := permDriftKey{server.ID, rule.ID}
	if len(drifts) == 0 {
		e.permDrifts.forget(key)
		return fmt.Sprintf("  [%s] rechte-soll ok - %d Pfad(e) geprüft", rule.Name, len(hardened)+len(applied)), nil
	}
	// Ein tatsächlicher Eingriff ändert den Zustand eines Produktivsystems -
	// das gehört ins Audit, nicht nur in den Output eines fremden Jobs. Eine
	// Abweichung, die LCM nur meldet (ACL, eingeschränkter Modus), steht
	// dagegen nur beim ersten Mal und bei jeder Änderung im Audit - sonst
	// stünde sie dort alle fünfzehn Minuten aufs Neue.
	if e.permDrifts.changed(key, strings.Join(reported, "; ")) || len(fixed) > 0 {
		e.recordEnforcement(server, rule, "Rechte-Soll: "+strings.Join(drifts, "; "))
	}
	return fmt.Sprintf("  [%s] abweichung erkannt:\n    %s", rule.Name, strings.Join(drifts, "\n    ")), nil
}

// permDriftKey benennt eine Rechte-Soll-Regel auf einem Server.
type permDriftKey struct{ server, rule uint }

// driftMemory hält je Server und Regel die zuletzt gemeldete Abweichung - nur
// im Speicher: Nach einem Neustart von LCM steht sie einmal mehr im Audit.
// Der Nullwert ist benutzbar.
type driftMemory struct {
	mu   sync.Mutex
	last map[permDriftKey]string
}

// changed merkt sich msg und meldet, ob sie von der vorigen abweicht. Eine
// leere Meldung ist keine Änderung.
func (m *driftMemory) changed(key permDriftKey, msg string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.last == nil {
		m.last = map[permDriftKey]string{}
	}
	prev := m.last[key]
	m.last[key] = msg
	return msg != "" && msg != prev
}

// forget vergisst die Abweichung: Kehrt sie zurück, ist sie wieder neu.
func (m *driftMemory) forget(key permDriftKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.last, key)
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
