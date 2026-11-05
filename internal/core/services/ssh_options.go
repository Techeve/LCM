package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Per-Server-SSH-Optionen (Root-Login sperren, eigener Port). Beide werden
// über EIN gemeinsames Drop-in erzwungen. Bewusst niedrig nummeriert
// (10-…): sshd nimmt für jede Direktive den ZUERST gefundenen Wert - ein
// früh geladenes Drop-in setzt sich damit gegen andere Drop-ins und die
// Härtung (60-lcm-hardening.conf) durch.
const sshOptionsDropinPath = "/etc/ssh/sshd_config.d/10-lcm-ssh.conf"

// sshdReloadStep prüft die Konfiguration und lädt sshd neu. Der Reload
// selbst kommt aus sshdReloadScript (server_service.go) - inklusive der
// OpenRC-Fallbacks für Alpine (BUG-027); eine eigene Kurzfassung ohne
// rc-service hatte den Alpine-Fix hier still ausgelassen.
const sshdReloadStep = "{ mkdir -p /run/sshd 2>/dev/null; sshd -t; } && (" + sshdReloadScript + ")"

// sshOptionsBody baut die Direktiven-Zeilen des per-Server-Drop-ins aus dem
// Soll-Zustand: die Ports und optional PermitRootLogin no. Leeres Ergebnis
// ("") bedeutet: das Drop-in wird entfernt.
//
// Ein EINZELNER Port 22 braucht keine Zeile - das ist die Vorgabe. Sobald
// aber mehrere Ports gewünscht sind (die Übergangsphase des Portwechsels),
// muss auch die 22 dastehen: Debian und Ubuntu leiten die Ports der
// Socket-Unit per sshd-socket-generator AUS dieser Datei ab, und der
// Generator schreibt eine vollständige Liste. Fehlte die 22 dort, öffnete
// der Übergang nur den neuen Port - und der Wechsel wäre kein Übergang mehr,
// sondern ein Sprung ohne Rückweg.
func sshOptionsBody(disableRoot bool, ports []int) string {
	var wanted []int
	for _, p := range ports {
		if p > 0 {
			wanted = append(wanted, p)
		}
	}
	var b strings.Builder
	if !(len(wanted) == 1 && wanted[0] == 22) {
		for _, p := range wanted {
			fmt.Fprintf(&b, "Port %d\n", p)
		}
	}
	if disableRoot {
		b.WriteString("PermitRootLogin no\n")
	}
	return b.String()
}

// sshSocketDropinPath ist das Drop-in der Socket-Unit (Pfad ohne Unit-Namen).
const sshSocketDropinName = "10-lcm-port.conf"

// applySSHSocketPortsScript zieht einen Portwechsel bei socket-aktiviertem
// sshd nach - dort lauscht `ssh.socket`, nicht der sshd.
//
// Genau daran scheiterte der Wechsel: LCM schrieb sein sshd-Drop-in und ließ
// den Reload bewusst aus (bei Socket-Aktivierung ist er weder nötig noch
// harmlos), der Socket lauschte aber unverändert weiter auf dem alten Port.
// Die Verifikation schlug fehl, der Wechsel wurde zurückgerollt.
//
// Zwei Wege führen zum Ziel, und beide werden gegangen:
//
//  1. Debian und Ubuntu erzeugen die Ports der Socket-Unit per
//     `sshd-socket-generator` AUS sshd_config. Dort ist `systemctl
//     daemon-reload` der entscheidende Schritt - er lässt den Generator neu
//     laufen -, gefolgt vom Neustart der Socket-Unit. Sein Ergebnis
//     (/run/systemd/generator/…/addresses.conf) beginnt mit einem
//     zurücksetzenden `ListenStream=` und schlägt damit jedes eigene Drop-in.
//  2. Wo es keinen solchen Generator gibt, kommen die Ports aus der Unit
//     selbst. Dafür schreibt LCM ein eigenes Drop-in mit derselben Liste.
//
// Der Neustart der Socket-Unit trennt bestehende Sitzungen NICHT: sshd läuft
// pro Verbindung als eigener Prozess und hängt nicht am Socket.
//
// Der Dienst muss dafür aber stehen: Läuft ssh.service (nach der ersten
// Verbindung immer), verweigert systemd der Socket-Unit den Start („already
// active, refusing“) und lässt sie im Fehlerzustand. sshd lauschte dann nur
// noch über den geerbten Socket und beendete sich beim nächsten SIGHUP mit
// „Cannot bind any address“ - danach war der Server per SSH nicht mehr
// erreichbar. Die nächste Verbindung startet den Dienst wieder.
//
// Neu gestartet wird nur, wenn sich die Ports geändert haben: Jeder Start
// zählt gegen das Startlimit von ssh.service (5 in 10 s). Wird es erreicht,
// fällt auch die Socket-Unit aus und niemand kommt mehr auf den Server.
// `reset-failed` setzt den Zähler zurück.
func applySSHSocketPortsScript(ports []int) string {
	var lines []string
	for _, p := range ports {
		if p > 0 {
			lines = append(lines, fmt.Sprintf("ListenStream=%d", p))
		}
	}
	// Nur Port 22 (oder nichts) = Vorgabe der Distribution, kein Drop-in.
	desired := ""
	if len(lines) > 0 && !(len(lines) == 1 && lines[0] == "ListenStream=22") {
		desired = "# LCM - SSH-Port der Socket-Unit (von LCM verwaltet).\n[Socket]\nListenStream=\n" +
			strings.Join(lines, "\n") + "\n"
	}
	return fmt.Sprintf(`u=""; for c in ssh.socket sshd.socket; do systemctl is-active --quiet "$c" 2>/dev/null && { u="$c"; break; }; done
if [ -n "$u" ]; then
  d=/etc/systemd/system/$u.d; f=$d/%s; want=%s; s="${u%%.socket}.service"; before=$(systemctl show -p Listen --value "$u")
  if [ -z "$want" ]; then rm -f "$f"; else install -d -m 755 "$d" && printf '%%s' "$want" > "$f"; fi
  # daemon-reload lässt den sshd-socket-generator neu laufen (Debian/Ubuntu);
  # erst der Neustart der Unit legt die neuen Ports auf.
  systemctl daemon-reload || exit 1
  if [ "$(systemctl show -p Listen --value "$u")" != "$before" ]; then
    systemctl stop "$s" 2>/dev/null; systemctl reset-failed "$s" 2>/dev/null
    systemctl restart "$u" || exit 1
  fi
fi`, sshSocketDropinName, shellQuote(desired))
}

// applySSHOptionsScript schreibt (oder entfernt) das Drop-in sicher: der
// bisherige Stand wird gesichert, der neue geschrieben und `sshd -t` geprüft;
// scheitert die Prüfung, wird der alte Stand zurückgerollt - es bleibt nie
// eine kaputte sshd-Konfiguration zurück.
func applySSHOptionsScript(body string) string {
	path := sshOptionsDropinPath
	bak := path + ".lcmbak"
	backup := fmt.Sprintf("cp -a %s %s 2>/dev/null || true", path, bak)
	restore := fmt.Sprintf("(mv %s %s 2>/dev/null || rm -f %s)", bak, path, path)
	cleanup := fmt.Sprintf("rm -f %s", bak)
	var write string
	if body == "" {
		write = fmt.Sprintf("rm -f %s", path)
	} else {
		header := "# LCM - per-Server SSH-Optionen (von LCM verwaltet, nicht von Hand ändern).\n"
		write = fmt.Sprintf("install -d -m 755 /etc/ssh/sshd_config.d && printf '%%s' %s > %s",
			shellQuote(header+body), path)
	}
	// backup → write → (test+reload OK: cleanup) ODER (fehler: restore, reload, exit 1)
	return fmt.Sprintf("%s; %s && %s && %s || { %s; %s; exit 1; }",
		backup, write, sshdReloadStep, cleanup, restore, sshdReloadStep)
}

// sshdMainConfigPath ist die Hauptdatei. Sie wird den Skripten als Parameter
// durchgereicht, damit sie in einer Sandbox gegen eine echte Datei geprüft
// werden können (Muster wie bei den apt-Skripten).
const sshdMainConfigPath = "/etc/ssh/sshd_config"

// rootLoginMarker kennzeichnet eine Zeile, die LCM in der HAUPTdatei
// stillgelegt hat. Die Markierung ist der einzige Grund, warum das rückgängig
// gemacht werden kann: Ohne sie wäre nicht mehr zu unterscheiden, was LCM
// auskommentiert hat und was schon vorher auskommentiert war.
const rootLoginMarker = "#LCM-STILLGELEGT# "

// neutralizeRootLoginScript legt die PermitRootLogin-Zeilen der HAUPTdatei
// stumm - mit Sicherung, sshd-Prüfung und Rollback.
//
// Nötig ist das, weil sshd bei mehrfacher Definition die ERSTE nimmt: Steht
// in /etc/ssh/sshd_config ein PermitRootLogin VOR dem Include der Drop-ins,
// bleibt LCMs Drop-in wirkungslos. Viele Cloud- und Hoster-Images liefern
// genau diese Zeile mit. Nur die Hauptdatei wird angefasst; die Drop-ins
// gehören LCM ohnehin oder sind bewusst gesetzt.
//
// Angewandt wird das NUR, wenn die Prüfung vorher ergeben hat, dass die
// Sperre nicht greift - steht die Zeile hinter dem Include, ist sie
// wirkungslos und wird in Ruhe gelassen.
func neutralizeRootLoginScript() string { return neutralizeRootLoginScriptIn(sshdMainConfigPath) }

func neutralizeRootLoginScriptIn(cfg string) string {
	return `CFG=` + cfg + `
[ -f "$CFG" ] || { echo 'keine sshd_config gefunden'; exit 1; }
cp -a "$CFG" "$CFG.lcmbak"
# Umweg über eine Zwischendatei statt "sed -i": Das Ergebnis wird per cat in
# die BESTEHENDE Datei zurückgeschrieben, sodass Besitzer, Rechte und
# Sicherheitskontext der sshd_config unangetastet bleiben. Nebenbei ist es das
# einzige Vorgehen, das mit BSD- und GNU-sed gleichermaßen funktioniert.
if ! sed 's/^\([[:space:]]*PermitRootLogin[[:space:]]\)/` + rootLoginMarker + `\1/' "$CFG.lcmbak" > "$CFG.lcmneu"; then
  rm -f "$CFG.lcmneu" "$CFG.lcmbak"
  echo 'die sshd_config konnte nicht bearbeitet werden.'
  exit 1
fi
cat "$CFG.lcmneu" > "$CFG"
rm -f "$CFG.lcmneu"
# Gegenprobe VOR dem Reload: Hat die Ersetzung nichts bewirkt, war die Annahme
# falsch - dann wird zurückgerollt, statt einen Erfolg zu melden, den niemand
# geprüft hat.
if ! grep -q '^` + rootLoginMarker + `' "$CFG"; then
  mv "$CFG.lcmbak" "$CFG"
  echo 'keine aktive PermitRootLogin-Zeile in der sshd_config gefunden - nichts stillgelegt.'
  exit 1
fi
if ` + sshdReloadStep + `; then
  echo "Vorrangige PermitRootLogin-Zeilen in $CFG stillgelegt:"
  grep -n '^` + rootLoginMarker + `' "$CFG" || true
  rm -f "$CFG.lcmbak"
else
  cat "$CFG.lcmbak" > "$CFG"
  rm -f "$CFG.lcmbak"
  echo 'sshd lehnte die geänderte sshd_config ab - zurückgerollt.'
  exit 1
fi`
}

// restoreRootLoginScript nimmt die Stilllegung zurück. Beim Freigeben des
// Root-Logins muss das passieren: Sonst hinterließe LCM eine dauerhafte
// Änderung an einer fremden Datei für einen Zustand, den es gerade wieder
// aufhebt.
func restoreRootLoginScript() string { return restoreRootLoginScriptIn(sshdMainConfigPath) }

func restoreRootLoginScriptIn(cfg string) string {
	return `CFG=` + cfg + `
if [ -f "$CFG" ] && grep -q '^` + rootLoginMarker + `' "$CFG"; then
  cp -a "$CFG" "$CFG.lcmbak"
  # Wie beim Stilllegen: über eine Zwischendatei, damit Besitzer und Rechte
  # der sshd_config erhalten bleiben und BSD- wie GNU-sed mitspielen.
  if ! sed 's/^` + rootLoginMarker + `//' "$CFG.lcmbak" > "$CFG.lcmneu"; then
    rm -f "$CFG.lcmneu" "$CFG.lcmbak"
    echo 'die sshd_config konnte nicht bearbeitet werden.'
    exit 1
  fi
  cat "$CFG.lcmneu" > "$CFG"
  rm -f "$CFG.lcmneu"
  if ` + sshdReloadStep + `; then
    echo "Zuvor stillgelegte PermitRootLogin-Zeilen in $CFG wiederhergestellt."
    rm -f "$CFG.lcmbak"
  else
    cat "$CFG.lcmbak" > "$CFG"
    rm -f "$CFG.lcmbak"
    echo 'Wiederherstellung von sshd abgelehnt - zurückgerollt.'
    exit 1
  fi
fi`
}

// sshRootLoginVerifyScript liest den TATSÄCHLICH wirksamen Wert von
// PermitRootLogin (`sshd -T` gibt die effektive Konfiguration aus, nicht den
// Dateiinhalt) und dazu alle Fundstellen der Direktive.
//
// Die Fundstellen sind der Kern: sshd nimmt bei mehrfacher Definition die
// ERSTE - anders als fast jede andere Konfiguration. Steht in
// /etc/ssh/sshd_config ein `PermitRootLogin yes` VOR dem Include-Eintrag,
// bleibt das LCM-Drop-in wirkungslos, obwohl es fehlerfrei geschrieben und
// von `sshd -t` abgenommen wurde. Ohne diese Ausgabe sucht man den Grund in
// LCM statt in der Datei, die ihn enthält.
const sshRootLoginVerifyScript = `mkdir -p /run/sshd 2>/dev/null
echo "LCMEFFECTIVE=$(sshd -T 2>/dev/null | sed -n 's/^[Pp]ermit[Rr]oot[Ll]ogin[[:space:]]\+//p' | head -1)"
echo "LCMSOURCES"
grep -rniE '^[[:space:]]*PermitRootLogin' /etc/ssh/sshd_config /etc/ssh/sshd_config.d/ 2>/dev/null || true`

// parseRootLoginVerification liest die Ausgabe von sshRootLoginVerifyScript:
// den effektiven Wert (leer = nicht ermittelbar) und die Fundstellen.
func parseRootLoginVerification(out string) (effective string, sources []string) {
	inSources := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "LCMEFFECTIVE="):
			effective = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "LCMEFFECTIVE=")))
		case line == "LCMSOURCES":
			inSources = true
		case inSources && line != "":
			sources = append(sources, line)
		}
	}
	return effective, sources
}

// rootLoginMismatch prüft, ob der gewünschte Zustand wirklich greift, und
// baut andernfalls eine Meldung, die den Grund benennt. Ein leerer effektiver
// Wert heißt „nicht ermittelbar" (z. B. `sshd -T` ohne die nötigen Rechte) -
// das ist kein Widerspruch und wird nicht als Fehlschlag gewertet.
func rootLoginMismatch(disabled bool, effective string, sources []string) error {
	if effective == "" {
		return nil
	}
	// „no" sperrt vollständig, „prohibit-password"/„without-password" lassen
	// nur noch den Schlüssel zu. Gefordert ist hier die vollständige Sperre.
	if disabled && effective == "no" {
		return nil
	}
	if !disabled && effective != "no" {
		return nil
	}
	want := "no"
	if !disabled {
		want = "yes bzw. prohibit-password"
	}
	msg := fmt.Sprintf("die Einstellung greift nicht: sshd meldet weiterhin PermitRootLogin %s (erwartet: %s)", effective, want)
	if len(sources) > 0 {
		msg += ". sshd nimmt bei mehrfacher Definition die ERSTE - vermutlich steht eine davon vor dem Include der Drop-ins: " +
			strings.Join(sources, " | ")
	}
	return errors.New(msg)
}

// errOrCode liefert den Fehler, oder - wenn keiner vorliegt - einen aus dem
// Exit-Code gebildeten. Damit steht in beiden Fällen etwas Aussagekräftiges
// in der Meldung.
func errOrCode(err error, code int) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("exit-code %d", code)
}

// rootLoginPlan sind die Skripte von SetSSHRootLogin. Lauf und
// Befehlsvorschau bauen sie mit planSSHRootLogin.
type rootLoginPlan struct {
	restore    string // nur beim Freigeben: eine frühere Stilllegung zurücknehmen
	apply      string // Drop-in schreiben, sshd prüfen und neu laden
	verify     string // wirksamen Wert lesen (vor und ggf. nach dem Stilllegen)
	neutralize string // nur beim Sperren und nur, wenn die Prüfung zeigt, dass die Sperre nicht greift
}

// planSSHRootLogin prüft den Server und baut die Skripte. Nur klassische
// SSH-Server: auf RouterOS gibt es kein sshd_config-Drop-in, auf
// Agent-Servern keinen direkten sshd-Zugriff.
func planSSHRootLogin(server *domain.Server, disabled bool) (rootLoginPlan, error) {
	if err := ensureSSHTransport(server); err != nil {
		return rootLoginPlan{}, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return rootLoginPlan{}, err
	}
	plan := rootLoginPlan{
		apply:  sshOptionsApplyCmd(server, disabled, []int{server.SSHPort}),
		verify: sshRootLoginVerifyScript,
	}
	if disabled {
		plan.neutralize = neutralizeRootLoginScript()
	} else {
		plan.restore = restoreRootLoginScript()
	}
	return plan, nil
}

// SetSSHRootLogin schaltet das direkte SSH-Login von root für einen Server an
// oder aus (PermitRootLogin no). Ein bereits gesetzter eigener Port bleibt
// erhalten. Risikolos für LCM selbst - der Management-Benutzer meldet sich per
// Zertifikat an, nicht als root.
func (s *ServerService) SetSSHRootLogin(scope repositories.AccessScope, id uint, disabled bool, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	plan, err := planSSHRootLogin(server, disabled)
	if err != nil {
		return "", err
	}
	conn, err := s.connectRec(server, "ssh-root-login", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	// Beim Freigeben ZUERST eine frühere Stilllegung zurücknehmen: Sonst
	// bliebe die Zeile der Hauptdatei stumm, obwohl der Zustand, für den LCM
	// sie stillgelegt hat, gerade aufgehoben wird.
	var out string
	if plan.restore != "" {
		restoreOut, _, restoreErr := conn.Run(privRun(server, plan.restore))
		out += restoreOut
		if restoreErr != nil {
			out += "\nHinweis: Die Wiederherstellung der sshd_config schlug fehl (" + restoreErr.Error() + ")."
		}
	}

	applyOut, code, runErr := conn.Run(privRun(server, plan.apply))
	out += applyOut
	if runErr != nil {
		return out, runErr
	}
	if code != 0 {
		return out, fmt.Errorf("root-login-einstellung fehlgeschlagen (exit %d) - sshd-Konfiguration unverändert", code)
	}

	// Nachweis statt Annahme: Dass das Drop-in geschrieben wurde und `sshd -t`
	// es abgenommen hat, heißt nicht, dass es auch WIRKT. Ohne diese Prüfung
	// meldete LCM „gesperrt" und merkte sich das, während der Deep Scan
	// (der `sshd -T` liest) unverändert „Root-Login erlaubt" fand - zwei
	// Ansichten desselben Servers, von denen nur eine stimmte.
	verifyOut, _, verifyErr := conn.Run(privRun(server, plan.verify))
	if verifyErr != nil {
		// Die Prüfung selbst ist fehlgeschlagen - der gesetzte Zustand bleibt
		// bestehen, aber unbestätigt. Das wird gesagt, nicht verschwiegen.
		out += "\nHinweis: Die Wirkung konnte nicht überprüft werden (" + verifyErr.Error() + ")."
	} else {
		out += "\n" + verifyOut
		effective, sources := parseRootLoginVerification(verifyOut)
		mismatch := rootLoginMismatch(disabled, effective, sources)

		// Greift die Sperre nicht, liegt es an einer vorrangigen Zeile in der
		// Hauptdatei. Die wird jetzt stillgelegt und erneut geprüft - erst
		// dieser zweite Anlauf entscheidet. Angefasst wird sie also nur, wenn
		// sie nachweislich im Weg steht; eine Zeile hinter dem Include ist
		// wirkungslos und bleibt unberührt.
		if mismatch != nil && disabled {
			out += "\n" + mismatch.Error()
			fixOut, fixCode, fixErr := conn.Run(privRun(server, plan.neutralize))
			out += "\n" + fixOut
			if fixErr != nil || fixCode != 0 {
				return out, fmt.Errorf("die vorrangige PermitRootLogin-Zeile konnte nicht stillgelegt werden: %w", errOrCode(fixErr, fixCode))
			}
			verifyOut, _, verifyErr = conn.Run(privRun(server, plan.verify))
			if verifyErr != nil {
				return out, fmt.Errorf("nach dem Stilllegen war die Wirkung nicht mehr überprüfbar: %w", verifyErr)
			}
			out += "\n" + verifyOut
			effective, sources = parseRootLoginVerification(verifyOut)
			mismatch = rootLoginMismatch(disabled, effective, sources)
		}

		if mismatch != nil {
			// Der DB-Stand bleibt unverändert: Er soll den Server beschreiben,
			// nicht die Absicht.
			return out, mismatch
		}
		if effective == "" {
			out += "\nHinweis: `sshd -T` lieferte keinen Wert - die Wirkung ist auf diesem Server nicht überprüfbar."
		}
	}

	_ = s.servers.UpdateFields(id, map[string]any{"ssh_root_login_disabled": disabled})
	verb := "erlaubt"
	if disabled {
		verb = "gesperrt"
	}
	s.audit.Log(actor, "server.ssh-root-login", "server", id, server.Name+": root-SSH "+verb)
	return out, nil
}

// ChangeSSHPort stellt den SSH-Port eines Servers sicher um - mit
// „erst verifizieren, dann übernehmen", damit man sich nie aussperrt:
//
//  1. Firewall (falls aktiv) für den NEUEN Port öffnen, alten Port noch offen.
//  2. sshd auf BEIDE Ports legen (alt + neu), Konfig prüfen, neu laden.
//  3. Testverbindung auf dem NEUEN Port aufbauen - schlägt sie fehl, wird der
//     alte Zustand zurückgerollt und der alte Port bleibt aktiv.
//  4. Erst nach erfolgreicher Verifikation: sshd endgültig auf den neuen Port,
//     Port in der DB speichern und die Firewall auf den neuen Port verengen.
func (s *ServerService) ChangeSSHPort(scope repositories.AccessScope, id uint, newPort int, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	plan, err := planSSHPort(server, newPort)
	if err != nil {
		return "", err
	}
	oldPort := plan.oldPort
	if plan.unchanged() {
		return fmt.Sprintf("SSH-Port ist bereits %d - keine Änderung nötig.", newPort), nil
	}

	conn, err := s.connectRec(server, "ssh-port-change", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	var log strings.Builder
	run := func(label, cmd string) (int, error) {
		out, code, runErr := conn.Run(privRun(server, cmd))
		fmt.Fprintf(&log, "### %s\n%s\n", label, out)
		return code, runErr
	}

	// 1. Neuen Port in der Firewall öffnen (alter bleibt vorerst offen).
	if plan.firewallActive {
		if _, err := run("Firewall: neuen Port öffnen", plan.firewallOpen); err != nil {
			return log.String(), fmt.Errorf("firewall (neuer port): %w", err)
		}
	}

	// 2. sshd auf beide Ports legen (Übergang), Root-Login-Zustand erhalten.
	if code, err := run("sshd: Übergang (alt+neu)", plan.transition); err != nil || code != 0 {
		return log.String(), fmt.Errorf("sshd-übergangskonfiguration fehlgeschlagen (exit %d): %w", code, err)
	}

	// 3. Verifizieren: erreicht LCM den Server auf dem neuen Port?
	if err := s.verifySSHPort(server, newPort); err != nil {
		// Rollback: sshd zurück auf den alten Stand, Firewall-Öffnung zurück.
		_, _ = run("sshd: Rollback auf alten Port", plan.rollback)
		if plan.firewallActive {
			_, _ = run("Firewall: neuen Port wieder schließen", plan.firewallClose)
		}
		return log.String(), fmt.Errorf("portwechsel abgebrochen - der neue Port %d ist nicht erreichbar, der alte Port %d bleibt aktiv: %w", newPort, oldPort, err)
	}

	// 4. Übernehmen: sshd endgültig auf den neuen Port, DB aktualisieren.
	if code, err := run("sshd: endgültig auf neuen Port", plan.final); err != nil || code != 0 {
		return log.String(), fmt.Errorf("sshd-endkonfiguration fehlgeschlagen (exit %d): %w", code, err)
	}
	_ = s.servers.UpdateFields(id, map[string]any{"ssh_port": newPort})
	// Firewall auf den neuen Port verengen (alten Port schließen) - volle
	// Neu-Anwendung des Regelsatzes mit dem neuen SSH-Port; benannte
	// Allowlists werden dabei aufgelöst.
	if plan.firewallActive {
		script, fwWarnings := plan.firewallNarrow(server, s.ipAllowlistExpand)
		for _, w := range fwWarnings {
			fmt.Fprintf(&log, "%s\n", w)
		}
		_, _ = run("Firewall: auf neuen Port verengen", script)
	}
	s.audit.Log(actor, "server.ssh-port", "server", id, fmt.Sprintf("%s: SSH-Port %d → %d", server.Name, oldPort, newPort))
	fmt.Fprintf(&log, "\nSSH-Port erfolgreich auf %d umgestellt.\n", newPort)
	return log.String(), nil
}

// sshPortPlan sind die Schritte von ChangeSSHPort. Lauf und Befehlsvorschau
// bauen sie mit planSSHPort. Die Firewall-Schritte sind leer, wenn auf dem
// Server keine (von LCM verwaltbare) Firewall aktiv ist.
type sshPortPlan struct {
	oldPort, newPort int
	firewallActive   bool
	firewallOpen     string // 1. neuen Port öffnen
	transition       string // 2. sshd auf alt+neu
	rollback         string // nur wenn die Testverbindung scheitert: sshd zurück auf alt
	firewallClose    string // nur wenn die Testverbindung scheitert: neuen Port wieder schließen
	final            string // 4. sshd nur noch auf neu
}

func (p sshPortPlan) unchanged() bool { return p.newPort == p.oldPort }

// firewallNarrow baut die Neu-Anwendung des Regelsatzes mit dem neuen
// SSH-Port. Die Allowlists werden erst hier aufgelöst - im Lauf nach der
// erfolgreichen Umstellung, so wie bisher.
func (p sshPortPlan) firewallNarrow(server *domain.Server, expand func([]uint) ([]string, error)) (string, []string) {
	sshRule := sshFirewallRule(p.newPort, parseSSHSources(server.FirewallSSHSources))
	sshRule, applied, warnings, _ := expandSSHAndRules(sshRule, firewallRulesFromServer(server), expand)
	return firewallBackendByName(firewallBackendFor(server)).enableScript(sshRule, applied), warnings
}

// planSSHPort prüft Server und Port und baut die Skripte des Wechsels.
// Nur klassische SSH-Server (siehe planSSHRootLogin).
func planSSHPort(server *domain.Server, newPort int) (sshPortPlan, error) {
	if err := ensureSSHTransport(server); err != nil {
		return sshPortPlan{}, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return sshPortPlan{}, err
	}
	if newPort < 1 || newPort > 65535 {
		return sshPortPlan{}, fmt.Errorf("ungültiger SSH-Port %d (erlaubt: 1-65535)", newPort)
	}
	oldPort := server.SSHPort
	if oldPort == 0 {
		oldPort = 22
	}
	plan := sshPortPlan{oldPort: oldPort, newPort: newPort}
	if newPort == oldPort {
		return plan, nil
	}
	root := server.SSHRootLoginDisabled
	plan.transition = sshOptionsApplyCmd(server, root, []int{oldPort, newPort})
	plan.rollback = sshOptionsApplyCmd(server, root, []int{oldPort})
	plan.final = sshOptionsApplyCmd(server, root, []int{newPort})
	plan.firewallActive = server.FirewallActive && !server.IsProxmox()
	if plan.firewallActive {
		// Firewall-Backend des Servers (ufw/firewalld/nftables) - die Kommandos
		// je Backend sind gegen ein fehlendes Werkzeug abgesichert (|| true).
		fw := firewallBackendByName(firewallBackendFor(server))
		plan.firewallOpen = fw.allowPortCmd(newPort)
		plan.firewallClose = fw.removePortCmd(newPort)
	}
	return plan, nil
}

// verifySSHPort baut eine kurze Test-Key-Verbindung auf dem angegebenen Port
// auf (striktes Host-Key-Checking) und führt ein harmloses Kommando aus - der
// Beweis, dass sich LCM nach dem Portwechsel weiter anmelden kann.
func (s *ServerService) verifySSHPort(server *domain.Server, port int) error {
	privPEM, err := s.cipher.DecryptString(server.PrivateKeyEnc)
	if err != nil {
		return fmt.Errorf("private key entschlüsseln: %w", err)
	}
	conn, err := s.dialer.DialKey(server.Host, port, server.ServiceUser, privPEM, server.HostKeyFingerprint)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, code, err := conn.Run("true"); err != nil || code != 0 {
		return fmt.Errorf("testkommando fehlgeschlagen (exit %d): %w", code, err)
	}
	return nil
}
