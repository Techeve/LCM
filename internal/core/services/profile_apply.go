package services

import (
	"fmt"
	"sort"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// Anwenden der Berechtigungsprofile auf einem Zielsystem.
//
// Ein Profil wirkt über eine eigene Linux-Gruppe (`lcm-prof-<slug>`) und eine
// sudoers-Datei, die auf diese Gruppe ausgestellt ist. Damit hängen die Rechte
// am PROFIL statt am einzelnen Konto: Zuordnen heißt Gruppenmitgliedschaft
// setzen, entziehen heißt sie lösen - kein Aufräumen pro Benutzer.
//
// Die mitgelieferten Profile bleiben bewusst beim bisherigen Weg: Der
// Voll-Administrator schreibt weiterhin die per-Benutzer-Datei
// `/etc/sudoers.d/lcm-<username>`, der Standardbenutzer entfernt sie. Dadurch
// ändert das Update auf bestehenden Servern keine einzige Datei; die
// Gruppen-Mechanik entsteht erst dort, wo jemand ein eigenes Profil zuweist.

// profileSudoersContent baut den Inhalt der sudoers-Datei eines Profils.
//
// Je Regel eine eigene Zeile statt einer kommagetrennten Liste: In sudoers
// trennt das Komma die Kommandos, und eine Zeile je Regel ist auch beim
// Nachlesen auf dem Server die verständlichere Form.
func profileSudoersContent(profile *domain.PrivilegeProfile) string {
	group := "%" + domain.ProfileGroupName(profile.Slug)
	lines := []string{
		fmt.Sprintf("# LCM - Berechtigungsprofil %q (%s).", profile.Name, profile.Slug),
		"# Von LCM verwaltet; nicht von Hand bearbeiten.",
	}
	for _, rule := range profile.SudoRules {
		runAs := rule.RunAs
		if runAs == "" {
			runAs = "root"
		}
		tag := "NOPASSWD:"
		if rule.RequirePassword {
			tag = "PASSWD:"
		}
		lines = append(lines, fmt.Sprintf("%s ALL=(%s) %s %s", group, runAs, tag, rule.Command))
	}
	// sudoedit-Regeln laufen ohne Passwort: Der Editor startet als der
	// Benutzer, zurückgeschrieben wird als root - es gibt keine Root-Shell,
	// aus der jemand ausbrechen könnte.
	for _, rule := range profile.EditRules {
		lines = append(lines, fmt.Sprintf("%s ALL=(root) NOPASSWD: sudoedit %s", group, rule.Path))
	}
	return strings.Join(lines, "\n") + "\n"
}

// profileHasSudoers meldet, ob das Profil überhaupt eine sudoers-Datei braucht.
// Ein Profil nur mit Verzeichnisrechten braucht die Gruppe, aber keine Datei.
func profileHasSudoers(profile *domain.PrivilegeProfile) bool {
	return len(profile.SudoRules) > 0 || len(profile.EditRules) > 0
}

// ensureProfileGroupScript legt die Profilgruppe an - distributionsbewusst,
// weil BusyBox kein groupadd kennt (dasselbe Muster wie beim Anlegen der
// Benutzer). Idempotent.
func ensureProfileGroupScript(slug string) string {
	group := domain.ProfileGroupName(slug)
	return fmt.Sprintf(
		"if ! getent group %s >/dev/null 2>&1; then "+
			"if command -v groupadd >/dev/null 2>&1; then groupadd %s; "+
			"elif command -v addgroup >/dev/null 2>&1; then addgroup %s; "+
			"else echo 'weder groupadd noch addgroup vorhanden' >&2; exit 1; fi; fi",
		group, group, group)
}

// writeProfileSudoersScript schreibt die sudoers-Datei eines Profils -
// erst in eine .tmp, dann per visudo geprüft, dann atomar getauscht.
//
// Ohne die Prüfung genügt EIN Syntaxfehler in /etc/sudoers.d, um sudo auf dem
// gesamten System lahmzulegen - auch für LCMs eigenen Zugang. Dasselbe Muster
// nutzt bereits das Umschalten in den eingeschränkten Modus.
func writeProfileSudoersScript(profile *domain.PrivilegeProfile) string {
	path := domain.ProfileSudoersPath(profile.Slug)
	tmp := path + ".tmp"
	return strings.Join([]string{
		// Fehlt visudo, bricht die Kette sonst mit einem rohen
		// "visudo: command not found" ab - der Grund (sudo ist gar nicht
		// installiert) steht dann nirgends. Der Join meldet genau diesen Fall
		// seit jeher im Klartext; hier fehlte er (Etappe G, 20.08.2026).
		"command -v visudo >/dev/null 2>&1 || { " +
			"echo 'FEHLER: visudo nicht gefunden - ohne sudo lassen sich " +
			"Berechtigungsprofile nicht anwenden. Auf dem Zielsystem sudo " +
			"installieren (z.B. apt-get install sudo, pacman -S sudo).' >&2; exit 1; }",
		writeFileB64(profileSudoersContent(profile), tmp),
		fmt.Sprintf("chmod 440 %s", tmp),
		// Scheitert die Pruefung, muss die .tmp weg: Sie bliebe sonst bei
		// jedem Versuch erneut liegen. Harmlos (sudo ignoriert Dateinamen mit
		// Punkt), aber sie sammelt sich an und sieht nach Fehler aus.
		fmt.Sprintf("{ visudo -cf %s >/dev/null || { rm -f %s; false; }; }", tmp, tmp),
		fmt.Sprintf("mv %s %s", tmp, path),
	}, " && ")
}

// removeProfileSudoersScript entfernt die Datei eines Profils, das auf diesem
// Server keine Regeln (mehr) trägt.
func removeProfileSudoersScript(slug string) string {
	return fmt.Sprintf("rm -f %s", domain.ProfileSudoersPath(slug))
}

// setProfileMembershipScript setzt die Profil-Mitgliedschaft eines Kontos:
// Es gehört in GENAU EINE Profilgruppe. Übrige `lcm-prof-*`-Mitgliedschaften
// werden gelöst - sonst summierten sich Rechte auf, sobald jemand das Profil
// wechselt, und genau das soll dieses Feature abschaffen.
//
// slug leer = in keiner Profilgruppe (mitgelieferte Profile).
func setProfileMembershipScript(username, slug string) string {
	want := ""
	if slug != "" {
		want = domain.ProfileGroupName(slug)
	}
	steps := []string{
		// Fremde Profilgruppen verlassen.
		fmt.Sprintf("for g in $(id -nG %s 2>/dev/null); do "+
			"case \"$g\" in lcm-prof-*) [ \"$g\" = '%s' ] || "+
			// delgroup als dritter Weg ist fuer BusyBox noetig: Dort gibt es
			// kein gpasswd, und deluser kennt NUR die Form "deluser USER" --
			// mit zwei Argumenten bricht es mit Usage ab, ohne etwas zu tun.
			// Ohne diesen Zweig bliebe der Benutzer auf Alpine in der alten
			// Profilgruppe und behielte deren sudo-Rechte zusaetzlich zu den
			// neuen (im Langzeittest Etappe G auf Alpine 3.23 nachgewiesen).
			"{ gpasswd -d %s \"$g\" >/dev/null 2>&1 || deluser %s \"$g\" >/dev/null 2>&1 || "+
			"delgroup %s \"$g\" >/dev/null 2>&1 || true; };; esac; done",
			username, want, username, username, username),
	}
	if want != "" {
		steps = append(steps, fmt.Sprintf(
			"usermod -aG %s %s 2>/dev/null || addgroup %s %s 2>/dev/null || true", want, username, username, want))
	}
	return strings.Join(steps, "; ")
}

// pruneProfilesScript entfernt sudoers-Dateien und Gruppen von Profilen, die
// auf diesem Server nicht mehr gebraucht werden.
//
// Ermittelt wird das am Server selbst (Dateien mit dem Präfix `lcm-prof-`)
// statt aus einer Zustandstabelle in der Datenbank: Der Bestand auf dem
// System ist die Wahrheit, das ist selbstheilend, und es kann nichts
// zurückbleiben, weil ein Datenbankeintrag fehlte. Eine Gruppe wird nur
// gelöscht, wenn sie kein Mitglied mehr hat - fremde Mitglieder fasst LCM
// nicht an.
func pruneProfilesScript(wantedSlugs []string) string {
	wanted := make([]string, 0, len(wantedSlugs))
	for _, slug := range wantedSlugs {
		wanted = append(wanted, domain.ProfileGroupName(slug))
	}
	sort.Strings(wanted)
	keep := strings.Join(wanted, " ")
	return strings.Join([]string{
		fmt.Sprintf("KEEP=' %s '", keep),
		"for f in /etc/sudoers.d/lcm-prof-*; do",
		"  [ -e \"$f\" ] || continue",
		"  n=$(basename \"$f\")",
		"  case \"$KEEP\" in *\" $n \"*) continue;; esac",
		"  rm -f \"$f\"",
		"done",
		"for g in $(getent group | cut -d: -f1 | grep '^lcm-prof-' || true); do",
		"  case \"$KEEP\" in *\" $g \"*) continue;; esac",
		// Nur löschen, wenn niemand mehr drin ist - weder als Haupt- noch als
		// Nebengruppe.
		"  [ -z \"$(getent group \"$g\" | cut -d: -f4)\" ] || continue",
		"  groupdel \"$g\" >/dev/null 2>&1 || delgroup \"$g\" >/dev/null 2>&1 || true",
		"done",
	}, "\n")
}

// profileApplyScript baut den vollständigen Vorlauf eines Servers: Gruppen und
// sudoers-Dateien der dort wirksamen Profile, danach das Aufräumen.
// wanted enthält die Profile, die auf diesem Server tatsächlich gebraucht
// werden - mitgelieferte Profile gehören NICHT dazu (sie laufen über den
// bisherigen per-Benutzer-Weg).
func profileApplyScript(wanted []*domain.PrivilegeProfile) string {
	var steps []string
	slugs := make([]string, 0, len(wanted))
	for _, profile := range wanted {
		slugs = append(slugs, profile.Slug)
		steps = append(steps, ensureProfileGroupScript(profile.Slug))
		if profileHasSudoers(profile) {
			steps = append(steps, writeProfileSudoersScript(profile))
		} else {
			steps = append(steps, removeProfileSudoersScript(profile.Slug))
		}
	}
	steps = append(steps, pruneProfilesScript(slugs))
	// Kontotyp „nur Dateizugriff": Der sshd bekommt sein Drop-in immer neu
	// gerechnet - auch wenn KEIN Profil (mehr) sftp verlangt, denn dann muss
	// die Datei verschwinden.
	steps = append(steps, profileSSHDScript(wanted))
	steps = append(steps, checkBlanketRule)
	return strings.Join(steps, "\n")
}

// pauschalRegelPruefen meldet eine sudo-Regel, die JEDEM alles erlaubt.
//
// openSUSE liefert das ab Werk aus (/usr/etc/sudoers: "ALL ALL=(ALL) ALL"
// zusammen mit "Defaults targetpw"). Praktisch haelt die Beschraenkung - ohne
// root-Passwort geht nichts durch -, aber "sudo -l" zeigt fuer ein Profilkonto
// trotzdem "(ALL) ALL". Wer damit die Frage "was darf dieses Konto?"
// beantwortet, liest etwas anderes, als das Profil verspricht.
//
// LCM aendert die Zeile NICHT: Sie gehoert der Distribution, und sie
// stillschweigend zu entfernen waere ein Eingriff, den niemand angeordnet hat.
// Gemeldet wird sie aber - Verschweigen waere hier das Schlechtere.
// Gefunden im Langzeittest (Etappe G, 21.08.2026) auf openSUSE Leap 16.
const checkBlanketRule = `for f in /etc/sudoers /usr/etc/sudoers; do ` +
	`[ -f "$f" ] || continue; ` +
	`grep -qE '^[[:space:]]*ALL[[:space:]]+ALL=\(ALL\)' "$f" && ` +
	`echo "LCM-PAUSCHALREGEL: $f"; done; true`

// ---- Wirkung auf das einzelne Konto ------------------------------------------

// profileEffect beschreibt, wie ein Profil auf ein einzelnes Konto wirkt.
type profileEffect struct {
	// FullRoot schreibt die per-Benutzer-Datei mit NOPASSWD:ALL - der Weg,
	// den es vor den Profilen schon gab.
	FullRoot bool
	// GroupSlug ist die Profilgruppe, in die das Konto gehört ("" = keine).
	GroupSlug string
	// NoShell setzt die Login-Shell auf nologin - der Kontotyp „nur
	// Dateizugriff". Der sshd erzwingt zusätzlich internal-sftp; die Shell
	// wegzunehmen ist der zweite Riegel am Konto selbst.
	NoShell bool
}

// effectFor bestimmt die Wirkung des auf einem Server geltenden Profils.
//
// Die mitgelieferten Profile laufen bewusst über den bisherigen Weg: Sie
// bilden genau die zwei Zustände ab, die es vorher gab, und erzeugen dadurch
// auf bestehenden Servern keine einzige Änderung. Die Gruppen-Mechanik
// entsteht nur dort, wo jemand ein eigenes Profil zuweist.
//
// Ohne Profil (Altbestand, den die Migration nicht erfasst hat) entscheidet
// weiterhin das alte sudo-Bit - ein Konto darf durch eine fehlende Zuordnung
// weder Rechte verlieren noch welche dazubekommen.
func effectFor(user *domain.LinuxUser, profile *domain.PrivilegeProfile) profileEffect {
	switch {
	case profile == nil:
		return profileEffect{FullRoot: user.Sudo}
	case profile.GrantsFullRoot:
		return profileEffect{FullRoot: true}
	case profile.Builtin:
		return profileEffect{}
	default:
		return profileEffect{
			GroupSlug: profile.Slug,
			NoShell:   profile.AccountType == domain.AccountTypeSFTP,
		}
	}
}

// ownProfiles filtert die selbst angelegten Profile heraus - nur für sie
// entstehen Gruppe und sudoers-Datei auf dem Zielsystem.
func ownProfiles(byUser map[string]*domain.PrivilegeProfile) []*domain.PrivilegeProfile {
	seen := make(map[uint]bool, len(byUser))
	var out []*domain.PrivilegeProfile
	for _, profile := range byUser {
		if profile == nil || profile.Builtin || seen[profile.ID] {
			continue
		}
		seen[profile.ID] = true
		out = append(out, profile)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// profileLabel beschreibt das wirksame Profil für den Sync-Bericht.
func profileLabel(profile *domain.PrivilegeProfile) string {
	if profile == nil {
		return "ohne Profil"
	}
	return "Profil " + profile.Name
}

// applyProfiles ermittelt das auf diesem Server wirksame Profil je Konto und
// richtet die dafür nötigen Gruppen und sudoers-Dateien ein.
//
// Liefert die Zuordnung Benutzername → Profil (nil = keines) und den Bericht.
func (s *ProvisioningService) applyProfiles(conn sshx.Conn, server *domain.Server, users []domain.LinuxUser) (map[string]*domain.PrivilegeProfile, string, error) {
	byUser := make(map[string]*domain.PrivilegeProfile, len(users))
	if s.profiles == nil {
		return byUser, "", nil // ohne Profil-Repository gilt das alte sudo-Bit
	}
	effective, err := s.linux.EffectiveProfilesForServer(server.ID)
	if err != nil {
		return nil, "", fmt.Errorf("berechtigungsprofile ermitteln: %w", err)
	}
	cache := make(map[uint]*domain.PrivilegeProfile, len(effective))
	for i := range users {
		id, ok := effective[users[i].ID]
		if !ok {
			continue
		}
		if _, loaded := cache[id]; !loaded {
			profile, err := s.profiles.FindByID(id)
			if err != nil {
				return nil, "", fmt.Errorf("berechtigungsprofil %d laden: %w", id, err)
			}
			cache[id] = profile
		}
		byUser[users[i].Username] = cache[id]
	}

	wanted := ownProfiles(byUser)
	if len(wanted) == 0 {
		// Auch ohne eigene Profile aufräumen: Ein Server, von dem das letzte
		// Profil abgezogen wurde, darf weder dessen Gruppe und sudoers-Datei
		// noch dessen Verzeichnisrechte behalten.
		script := pruneProfilesScript(nil) + "\n" + profileSSHDScript(nil)
		if server.RestrictedSudo {
			script = helperCmd("profile-prune", "-")
		}
		out, code, runErr := conn.Run(privRun(server, script))
		if runErr != nil || code != 0 {
			return nil, "", profileScriptError(runErr, code, out)
		}
		pathReport, err := s.applyPathRules(conn, server, nil)
		return byUser, pathReport, err
	}
	// Regelbausteine für die Distributions-Familie DIESES Servers auflösen -
	// derselbe Baustein ergibt auf Debian und RHEL unterschiedliche Zeilen.
	family := pkgFamily(server.PackageManager)
	expanded := make([]*domain.PrivilegeProfile, 0, len(wanted))
	names := make([]string, 0, len(wanted))
	var notes []string
	for _, profile := range wanted {
		resolved, blockNotes := expandProfile(profile, family)
		expanded = append(expanded, resolved)
		names = append(names, profile.Name)
		notes = append(notes, blockNotes...)
	}
	// Eingeschränkter Modus: dieselbe Wirkung über den validierenden Helper
	// statt über ein Root-Shell-Skript.
	if server.RestrictedSudo {
		for _, cmd := range helperProfileApplyCmds(expanded) {
			out, code, runErr := conn.Run(privRun(server, cmd))
			if runErr != nil || code != 0 {
				return nil, "", profileScriptError(runErr, code, out)
			}
		}
	} else {
		out, code, runErr := conn.Run(privRun(server, profileApplyScript(expanded)))
		if runErr != nil || code != 0 {
			return nil, "", profileScriptError(runErr, code, out)
		}
		notes = append(notes, pauschalRegelHinweise(out)...)
	}
	report := "Berechtigungsprofile eingerichtet: " + strings.Join(names, ", ") + "\n"
	for _, note := range notes {
		report += "  " + note + "\n"
	}
	// Verzeichnisrechte danach: Sie hängen an der Profilgruppe, die im Schritt
	// davor entstanden ist.
	pathReport, err := s.applyPathRules(conn, server, expanded)
	if err != nil {
		return nil, report, err
	}
	return byUser, report + pathReport, nil
}

// profileScriptError macht aus Fehlschlag und Ausgabe eine sprechende Meldung.
// Die Ausgabe des Zielsystems gehört dazu: Scheitert visudo, steht dort der
// Grund - ohne ihn bliebe nur „exit 1".
func profileScriptError(runErr error, code int, out string) error {
	if runErr != nil {
		return fmt.Errorf("berechtigungsprofile einrichten: %w", runErr)
	}
	return fmt.Errorf("berechtigungsprofile einrichten: exit %d: %s", code, summarize(out))
}

// ---- Regelbausteine auflösen -------------------------------------------------

// expandProfile löst die Regelbausteine eines Profils für die
// Distributionsfamilie EINES Servers auf und mischt sie unter die eigenen
// Regeln. Liefert eine Kopie des Profils und die Hinweise, die in den
// Sync-Bericht gehören.
//
// Die Familie entscheidet, weil dieselbe Aufgabe je Distribution anders
// heißt: Die Unit ist auf Debian/Ubuntu `apache2`, auf RHEL und SUSE `httpd`.
// Fehlt für die Familie eines Servers eine Variante, gilt der Baustein dort
// NICHT - und das wird gemeldet. Eine fehlende Regel heißt fehlende Rechte;
// still übersprungen sucht jemand stundenlang, warum der Dienst-Neustart nur
// auf den Debian-Servern geht.
func expandProfile(profile *domain.PrivilegeProfile, family string) (*domain.PrivilegeProfile, []string) {
	expanded := *profile
	expanded.SudoRules = append([]domain.ProfileSudoRule(nil), profile.SudoRules...)
	expanded.EditRules = append([]domain.ProfileEditRule(nil), profile.EditRules...)
	expanded.PathRules = append([]domain.ProfilePathRule(nil), profile.PathRules...)

	var notes []string
	for i := range profile.BlockUses {
		use := &profile.BlockUses[i]
		if use.Block == nil {
			continue
		}
		variant := use.Block.VariantForFamily(family)
		if variant == nil {
			notes = append(notes, fmt.Sprintf(
				"Baustein %q gilt hier nicht: für die Distributions-Familie %q ist keine Variante hinterlegt",
				use.Block.Name, family))
			continue
		}
		values := domain.ParseBlockValues(use.Values)
		for _, cmd := range domain.BlockLines(variant.SudoCommands) {
			rendered := domain.NormalizeSudoCommand(domain.SubstituteBlockParams(cmd, values))
			// Erneut prüfen: Zwischen dem Speichern und dem Anwenden kann sich
			// der Baustein geändert haben, und ein nicht gefüllter Platzhalter
			// darf nie in einer sudoers-Datei landen.
			if err := domain.ValidateSudoCommand(rendered, false); err != nil {
				notes = append(notes, fmt.Sprintf("Regel aus Baustein %q verworfen (%v): %s", use.Block.Name, err, rendered))
				continue
			}
			runAs := variant.RunAs
			if runAs == "" {
				runAs = "root"
			}
			expanded.SudoRules = append(expanded.SudoRules, domain.ProfileSudoRule{Command: rendered, RunAs: runAs})
		}
		for _, path := range domain.BlockLines(variant.EditPaths) {
			rendered := domain.SubstituteBlockParams(path, values)
			if err := domain.ValidateEditPath(rendered); err != nil {
				notes = append(notes, fmt.Sprintf("Datei aus Baustein %q verworfen (%v): %s", use.Block.Name, err, rendered))
				continue
			}
			expanded.EditRules = append(expanded.EditRules, domain.ProfileEditRule{Path: rendered})
		}
		for _, line := range domain.BlockLines(variant.PathRules) {
			rendered := domain.SubstituteBlockParams(line, values)
			mode, dir, ok := domain.ParseBlockPathRule(rendered)
			if !ok {
				notes = append(notes, fmt.Sprintf("Verzeichnisrecht aus Baustein %q verworfen (%v): %s", use.Block.Name, domain.ErrBlockPathLine, rendered))
				continue
			}
			if err := domain.ValidatePathRule(dir, mode); err != nil {
				notes = append(notes, fmt.Sprintf("Verzeichnisrecht aus Baustein %q verworfen (%v): %s", use.Block.Name, err, rendered))
				continue
			}
			expanded.PathRules = append(expanded.PathRules, domain.ProfilePathRule{Path: dir, Mode: mode})
		}
	}
	return &expanded, notes
}

// ---- Verzeichnisrechte (POSIX-ACL) -------------------------------------------

// aclProbeScript prüft, ob ACLs auf diesem System TATSÄCHLICH wirken: Ein
// Eintrag wird auf einem Wegwerf-Verzeichnis gesetzt und sofort wieder
// entfernt.
//
// Das Werkzeug allein genügt nicht. Auf ZFS ohne `acltype=posixacl`, auf
// NFS-Mounts und in manchen Container-Overlays ist setfacl vorhanden und
// bewirkt nichts - LCM meldete dann „Rechte gesetzt", ohne dass jemand welche
// hätte.
const aclProbeScript = `d=$(mktemp -d 2>/dev/null) || exit 0; ` +
	`setfacl -m g:root:rx "$d" >/dev/null 2>&1 && getfacl -p "$d" 2>/dev/null | grep -q '^group:root:' && echo acl-ok; ` +
	`rm -rf "$d"`

// aclSpecFor übersetzt einen Regel-Modus in die ACL-Rechte.
//
// Großes X statt x: Es setzt das Ausführ-/Durchgangsrecht nur auf
// Verzeichnisse und auf Dateien, die es ohnehin schon tragen - sonst würde
// eine Freigabe jede Textdatei im Baum ausführbar machen.
func aclSpecFor(mode string) string {
	switch mode {
	case domain.PathModeReadWrite:
		return "rwX"
	case domain.PathModeDeny:
		return "---"
	default:
		return "rX"
	}
}

// profilePathScript setzt die Verzeichnisrechte eines Profils.
//
// Zwei Eigenheiten, die im Code stehen müssen:
//
//   - `-P` (physical): Ohne das folgt setfacl Symlinks. Ein Benutzer mit
//     Schreibrecht im freigegebenen Baum könnte vor dem nächsten Abgleich
//     einen Link auf /etc legen und bekäme dessen Ziel mitfreigegeben.
//   - Die Default-ACL (`-d`) ist die Vererbung: Was unter dem Pfad NEU
//     entsteht, erbt die Rechte automatisch. Das `-R` zieht einmalig den
//     Bestand nach. Für eine Verweigerung ergibt eine Vererbung keinen Sinn -
//     dort bleibt es beim einfachen Eintrag.
func profilePathScript(profile *domain.PrivilegeProfile) string {
	group := "g:" + domain.ProfileGroupName(profile.Slug)
	var steps []string
	for _, rule := range profile.PathRules {
		spec := group + ":" + aclSpecFor(rule.Mode)
		// Ein fehlender Pfad ist kein Fehler des Profils - er wird gemeldet
		// und übersprungen, damit ein einzelnes Verzeichnis nicht den ganzen
		// Abgleich anhält.
		steps = append(steps, fmt.Sprintf(
			"if [ ! -d %s ]; then echo 'pfad fehlt: %s'; elif [ -L %s ]; then echo 'pfad ist ein symlink, uebersprungen: %s'; else",
			rule.Path, rule.Path, rule.Path, rule.Path))
		steps = append(steps, fmt.Sprintf("  setfacl -RP -m %s %s", spec, rule.Path))
		if rule.Mode != domain.PathModeDeny {
			steps = append(steps, fmt.Sprintf("  setfacl -RP -d -m %s %s", spec, rule.Path))
		}
		steps = append(steps, "fi")
	}
	return strings.Join(steps, "\n")
}

// removeProfilePathScript nimmt die ACL-Einträge eines Profils auf einem Pfad
// zurück - für Pfade, die aus dem Profil verschwunden sind.
//
// Anders als bei den sudoers-Dateien lässt sich das NICHT am Server ablesen:
// ACL-Einträge tragen keinen Kommentar und keine Namenskonvention, über die
// man sie aufzählen könnte. Deshalb merkt sich LCM, welche Pfade es je Server
// gesetzt hat (AppliedProfilePath).
func removeProfilePathScript(slug, path string) string {
	group := "g:" + domain.ProfileGroupName(slug)
	return fmt.Sprintf("[ -d %s ] && { setfacl -RP -x %s %s 2>/dev/null; setfacl -RP -d -x %s %s 2>/dev/null; } || true",
		path, group, path, group, path)
}

// applyPathRules setzt die Verzeichnisrechte der auf diesem Server wirksamen
// Profile und nimmt zurück, was aus den Profilen verschwunden ist.
//
// Übersprungen wird sichtbar: Ohne nutzbare ACLs bleiben die Pfadregeln eines
// Profils wirkungslos - das gehört in den Bericht, damit sich niemand auf
// Rechte verlässt, die es dort nicht gibt.
func (s *ProvisioningService) applyPathRules(conn sshx.Conn, server *domain.Server, wanted []*domain.PrivilegeProfile) (string, error) {
	previous, err := s.profiles.AppliedPathsForServer(server.ID)
	if err != nil {
		return "", err
	}
	var soll []domain.AppliedProfilePath
	for _, profile := range wanted {
		for _, rule := range profile.PathRules {
			soll = append(soll, domain.AppliedProfilePath{
				ServerID: server.ID, ProfileID: profile.ID, Path: rule.Path, Mode: rule.Mode,
			})
		}
	}
	if len(soll) == 0 && len(previous) == 0 {
		return "", nil
	}
	if !server.ACLUsable {
		if len(soll) == 0 {
			return "", nil
		}
		hint := "das Paket acl fehlt"
		if server.HasACL {
			hint = "das Dateisystem trägt keine ACLs (z.B. ZFS ohne acltype=posixacl)"
		}
		return fmt.Sprintf("Verzeichnisrechte übersprungen - %s. Die Pfadregeln der Profile wirken auf diesem Server NICHT.\n", hint), nil
	}

	var steps []string
	// Zuerst zurücknehmen, was nicht mehr gefordert ist: Ein entfernter Pfad
	// darf keinen Zugriff überleben.
	for _, old := range previous {
		if !pathStillWanted(old, soll) {
			steps = append(steps, removeProfilePathScript(slugOf(wanted, old.ProfileID), old.Path))
		}
	}
	for _, profile := range wanted {
		if len(profile.PathRules) > 0 {
			steps = append(steps, profilePathScript(profile))
		}
	}
	if len(steps) == 0 {
		return "", nil
	}
	out, code, runErr := conn.Run(privRun(server, strings.Join(steps, "\n")))
	if runErr != nil || code != 0 {
		return "", profileScriptError(runErr, code, out)
	}
	if err := s.profiles.ReplaceAppliedPaths(server.ID, soll); err != nil {
		return "", err
	}
	report := fmt.Sprintf("Verzeichnisrechte gesetzt: %d Pfad(e)\n", len(soll))
	// Meldungen des Skripts (fehlender Pfad, Symlink) gehören in den Bericht.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			report += "  " + line + "\n"
		}
	}
	return report, nil
}

// pathStillWanted meldet, ob ein zuvor gesetzter Pfad weiterhin gefordert ist.
func pathStillWanted(old domain.AppliedProfilePath, soll []domain.AppliedProfilePath) bool {
	for _, want := range soll {
		if want.ProfileID == old.ProfileID && want.Path == old.Path && want.Mode == old.Mode {
			return true
		}
	}
	return false
}

// slugOf liefert den Slug eines Profils aus der Liste der wirksamen Profile.
// Ist es nicht mehr dabei (gelöscht oder abgezogen), bleibt nur die
// Zeichenfolge aus der Aufzeichnung - der Gruppenname folgt derselben Regel.
func slugOf(profiles []*domain.PrivilegeProfile, id uint) string {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile.Slug
		}
	}
	return ""
}

// InstallACLSupport installiert das Paket `acl` auf einem Server.
//
// Nie ungefragt: Der Server zeigt den Hinweis, der Betreiber drückt den Knopf.
// Der Paketname ist auf allen fünf Paketverwaltungen derselbe. Ob es danach
// wirklich trägt, entscheidet der nächste Scan über die ACL-Probe - auf ZFS
// ohne acltype=posixacl bleibt es auch mit installiertem Paket wirkungslos.
func (s *ServerService) InstallACLSupport(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	return s.startPackageJob(scope, id, domain.RuleTypeScript,
		"ACL-Unterstützung installieren (Paket acl)",
		func(mgr string) string { return pkgInstallScript(mgr, []string{"acl"}) }, actor)
}

// ---- Kontotyp „nur Dateizugriff" ---------------------------------------------

// profileSSHDPath ist das von LCM verwaltete sshd-Drop-in der Profile.
const profileSSHDPath = "/etc/ssh/sshd_config.d/70-lcm-profiles.conf"

// profileSSHDContent baut das Drop-in für die Profile mit Kontotyp „nur
// Dateizugriff".
//
// ForceCommand internal-sftp beantwortet die Frage „welche Programme darf
// jemand starten" endgültig mit: keine. internal-sftp steckt im sshd selbst -
// es muss nichts kopiert und nichts gepflegt werden. Sichtbar bleibt, was die
// Verzeichnisrechte hergeben.
//
// Das abschließende `Match all` ist Pflicht und kein Schönheitsfehler: Die
// Include-Zeile steht in sshd_config GANZ OBEN, und ein Match-Block gilt bis
// zum nächsten Match. Ohne den Abschluss rutschte die gesamte restliche
// Konfiguration der Hauptdatei ungewollt in diesen Block.
//
// Bewusst OHNE ChrootDirectory: Ein Chroot versteckt genau die Verzeichnisse,
// die das Profil per ACL freigibt - die Daten müssten dafür ins Jail gespiegelt
// oder hineingemountet werden. Ohne Chroot begrenzt das Dateisystem, und das
// ist derselbe Maßstab wie bei allen anderen Profilrechten.
func profileSSHDContent(profiles []*domain.PrivilegeProfile) string {
	var blocks []string
	for _, profile := range profiles {
		if profile.AccountType != domain.AccountTypeSFTP {
			continue
		}
		blocks = append(blocks, strings.Join([]string{
			"Match Group " + domain.ProfileGroupName(profile.Slug),
			"    ForceCommand internal-sftp",
			"    AllowTcpForwarding no",
			"    X11Forwarding no",
			"    PermitTTY no",
		}, "\n"))
	}
	if len(blocks) == 0 {
		return ""
	}
	head := "# LCM - Berechtigungsprofile mit Kontotyp „nur Dateizugriff\".\n" +
		"# Von LCM verwaltet; nicht von Hand bearbeiten.\n"
	// Match all schließt ab - siehe oben.
	return head + strings.Join(blocks, "\n\n") + "\n\nMatch all\n"
}

// profileSSHDScript schreibt oder entfernt das Drop-in - mit Sicherung,
// `sshd -t` und Rollback.
//
// Ein Fehler in der sshd-Konfiguration sperrt nicht nur die Benutzer aus,
// sondern auch LCMs eigenen Zugang. Deshalb dasselbe Muster wie beim
// SSH-Härten: erst sichern, prüfen, neu laden - und bei Fehlschlag zurück.
func profileSSHDScript(profiles []*domain.PrivilegeProfile) string {
	content := profileSSHDContent(profiles)
	steps := []string{
		fmt.Sprintf("cp -a %s %s.lcmbak 2>/dev/null || true", profileSSHDPath, profileSSHDPath),
	}
	if content == "" {
		steps = append(steps, fmt.Sprintf("rm -f %s", profileSSHDPath))
	} else {
		steps = append(steps,
			"install -d -m 755 /etc/ssh/sshd_config.d",
			writeFileB64(content, profileSSHDPath),
			fmt.Sprintf("chmod 644 %s", profileSSHDPath))
	}
	steps = append(steps, strings.Join([]string{
		"if sshd -t 2>/dev/null; then",
		fmt.Sprintf("  rm -f %s.lcmbak;", profileSSHDPath),
		"  systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null ||",
		"    rc-service sshd reload 2>/dev/null || service ssh reload 2>/dev/null ||",
		"    service sshd reload 2>/dev/null || true;",
		"else",
		fmt.Sprintf("  mv %s.lcmbak %s 2>/dev/null || rm -f %s;", profileSSHDPath, profileSSHDPath, profileSSHDPath),
		"  echo 'sshd lehnt die konfiguration ab - zurückgerollt' >&2;",
		"  exit 1;",
		"fi",
	}, " "))
	return strings.Join(steps, "\n")
}

// nologinShellStep setzt die Login-Shell eines Kontos auf nologin.
//
// Der Pfad weicht je Distribution ab (/usr/sbin/nologin, /sbin/nologin), und
// auf BusyBox gibt es ihn gar nicht - dann tut /bin/false denselben Dienst.
func nologinShellStep(username string) string {
	return fmt.Sprintf(
		"NOLOGIN=$(command -v nologin 2>/dev/null || true); "+
			"[ -n \"$NOLOGIN\" ] || { [ -x /usr/sbin/nologin ] && NOLOGIN=/usr/sbin/nologin; }; "+
			"[ -n \"$NOLOGIN\" ] || { [ -x /sbin/nologin ] && NOLOGIN=/sbin/nologin; }; "+
			"[ -n \"$NOLOGIN\" ] || NOLOGIN=/bin/false; "+
			"usermod -s \"$NOLOGIN\" %s 2>/dev/null || true", username)
}

// pauschalRegelHinweise wandelt die Marker des Anwenden-Skripts in Klartext.
func pauschalRegelHinweise(ausgabe string) []string {
	var hinweise []string
	for _, line := range strings.Split(ausgabe, "\n") {
		datei, found := strings.CutPrefix(strings.TrimSpace(line), "LCM-PAUSCHALREGEL: ")
		if !found {
			continue
		}
		hinweise = append(hinweise, "Achtung: "+datei+" erlaubt JEDEM Benutzer alles "+
			"(\"ALL ALL=(ALL) ALL\", Voreinstellung von openSUSE). Das Profil gilt "+
			"zusätzlich, begrenzt aber nicht - \"sudo -l\" zeigt deshalb auch \"(ALL) ALL\". "+
			"Ohne root-Passwort kommt dort niemand durch; wer das Konto wirklich "+
			"einschränken will, muss die Zeile auf dem Server selbst entfernen.")
	}
	return hinweise
}
