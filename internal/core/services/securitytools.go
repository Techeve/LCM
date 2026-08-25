package services

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// Sicherheits-Tools: fail2ban bzw. CrowdSec per Aktion installieren & einrichten.
// Beide sperren wiederholt auffällige IPs - deshalb wird LCMs eigene Quell-IP
// IMMER in die Allowlist gesetzt, damit LCM sich nicht selbst aussperrt. Die
// Aktion braucht vollen Root-Zugriff (ensureFullSudo); im eingeschränkten
// Sudo-Modus ist sie gesperrt.

// Sicherheits-Tool-Kennungen.
const (
	SecurityToolFail2ban = "fail2ban"
	SecurityToolCrowdSec = "crowdsec"
)

var (
	// ErrUnknownSecurityTool: unbekanntes Tool angefragt.
	ErrUnknownSecurityTool = errors.New("unbekanntes Sicherheits-Tool (erlaubt: fail2ban, crowdsec)")
	// ErrInvalidAllowlistIP: ein Allowlist-Eintrag ist keine gültige IP.
	ErrInvalidAllowlistIP = errors.New("ungültige Allowlist-IP")
	// ErrCrowdSecLapiMissing: Remote-LAPI gewählt, aber in den Einstellungen kein Zugang hinterlegt.
	ErrCrowdSecLapiMissing = errors.New("keine CrowdSec-LAPI-Zugangsdaten hinterlegt (Einstellungen → CrowdSec)")
	// ErrCrowdSecConsoleMissing: Console gewählt, aber kein Enrollment-Key hinterlegt.
	ErrCrowdSecConsoleMissing = errors.New("kein CrowdSec-Console-Key hinterlegt (Einstellungen → CrowdSec)")
	// ErrCrowdSecUnsupported: die Distribution bietet CrowdSec nicht als Paket.
	ErrCrowdSecUnsupported = errors.New("CrowdSec wird auf dieser Paketverwaltung nicht per Knopfdruck unterstützt")
)

// reCollection erlaubt nur unbedenkliche CrowdSec-Collection-Namen (z.B.
// crowdsecurity/sshd) - sie landen wörtlich in einem cscli-Aufruf.
var reCollection = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// reConsoleKey erlaubt nur alphanumerische Console-Enrollment-Keys.
var reConsoleKey = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// CrowdSecConfig ist der entschlüsselte CrowdSec-Zugang aus den globalen Einstellungen.
type CrowdSecConfig struct {
	LapiURL      string
	LapiLogin    string
	LapiPassword string
	ConsoleKey   string
}

// SecurityToolInput bündelt die Formular-Eingaben der Aktion.
type SecurityToolInput struct {
	Tool         string   // fail2ban | crowdsec
	AllowlistIPs []string // zusätzliche Allowlist-IPs (LCM-IP kommt automatisch dazu)
	AllowlistIDs []uint   // referenzierte benannte IP-Allowlists (gemeinsamer Pool)
	// Nur CrowdSec:
	Bouncer     bool     // Firewall-Bouncer mitinstallieren
	Collections []string // z.B. ["crowdsecurity/sshd"]
	LapiMode    string   // local | remote | console
}

// validateSecurityToolInput prüft Tool, IPs, Collections und LAPI-Modus.
func validateSecurityToolInput(in *SecurityToolInput) error {
	switch in.Tool {
	case SecurityToolFail2ban, SecurityToolCrowdSec:
	default:
		return ErrUnknownSecurityTool
	}
	// Ad-hoc-Allowlist-IPs kanonisieren - IP ODER CIDR (ignoreip/whitelist
	// vertragen beides; die aus benannten Allowlists aufgelösten Einträge sind
	// ohnehin bereits kanonisch).
	clean := make([]string, 0, len(in.AllowlistIPs))
	for _, raw := range in.AllowlistIPs {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		canon, _, err := canonicalAddr(ip)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrInvalidAllowlistIP, ip)
		}
		clean = append(clean, canon)
	}
	in.AllowlistIPs = clean
	if in.Tool == SecurityToolCrowdSec {
		colls := make([]string, 0, len(in.Collections))
		for _, c := range in.Collections {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if !reCollection.MatchString(c) {
				return fmt.Errorf("ungültiger Collection-Name: %q", c)
			}
			colls = append(colls, c)
		}
		if len(colls) == 0 {
			colls = []string{"crowdsecurity/sshd"}
		}
		in.Collections = colls
		switch in.LapiMode {
		case "", "local":
			in.LapiMode = "local"
		case "remote", "console":
		default:
			return fmt.Errorf("%w: %q (erlaubt: local, remote, console)", ErrInvalidLapiMode, in.LapiMode)
		}
	}
	return nil
}

// allowlistIPs baut die effektive Allowlist: Loopback + LCM-Quell-IP + Extras
// (dedupliziert, leere entfernt).
func allowlistIPs(srcIP string, extra []string) []string {
	out := []string{"127.0.0.1/8", "::1"}
	seen := map[string]bool{"127.0.0.1/8": true, "::1": true}
	for _, ip := range append([]string{srcIP}, extra...) {
		ip = strings.TrimSpace(ip)
		if ip != "" && !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out
}

// ---- fail2ban ---------------------------------------------------------------

// fail2banDropin ist LCMs eigene fail2ban-Konfiguration.
//
// Bewusst ein Drop-in in jail.d und NICHT jail.local: fail2ban liest
// jail.conf → jail.d/*.conf → jail.local → jail.d/*.local, die letzte Datei
// gewinnt. LCMs Werte setzen sich damit durch, ohne die Datei anzufassen, in
// der Administratoren ihre eigenen Jails und Verschärfungen pflegen. Bis
// hierher überschrieb LCM jail.local ersatzlos - eine gehärtete Einstellung
// (bantime, maxretry, zusätzliche Jails) war nach einem erneuten
// „Installieren" spurlos weg, ohne Sicherung und ohne Hinweis (R2-076).
const fail2banDropin = "/etc/fail2ban/jail.d/99-lcm.local"

// fail2banInstallScript installiert fail2ban und schreibt LCMs Drop-in mit der
// Allowlist (ignoreip) und aktivierter sshd-Jail. `backend = systemd` macht die
// Log-Auswertung distributionsübergreifend robust.
func fail2banInstallScript(mgr, srcIP string, extra []string) string {
	ignore := strings.Join(allowlistIPs(srcIP, extra), " ")
	return strings.Join([]string{
		pkgInstallScript(mgr, []string{"fail2ban"}),
		"install -d -m 755 /etc/fail2ban/jail.d",
		fmt.Sprintf("printf '# Von LCM verwaltet - eigene Jails und Verschaerfungen gehoeren in jail.local.\\n"+
			"[DEFAULT]\\nbackend = systemd\\nignoreip = %s\\n\\n[sshd]\\nenabled = true\\n' > %s", ignore, fail2banDropin),
		// Eine vorhandene eigene Konfiguration bleibt bestehen - aber der
		// Administrator soll wissen, dass LCMs Drop-in zuletzt gelesen wird.
		"if [ -f /etc/fail2ban/jail.local ]; then " +
			"echo 'LCM: vorhandene /etc/fail2ban/jail.local bleibt unveraendert bestehen; " +
			"LCMs Drop-in (99-lcm.local) wird danach gelesen und hat fuer ignoreip und [sshd] Vorrang'; fi",
		"systemctl enable --now fail2ban 2>/dev/null || service fail2ban restart 2>/dev/null || rc-service fail2ban restart 2>/dev/null || true",
		"fail2ban-client reload 2>/dev/null || true",
		fmt.Sprintf("echo 'LCM: fail2ban eingerichtet (ignoreip: %s)'", ignore),
		"fail2ban-client status sshd 2>/dev/null || true",
	}, "\n")
}

// ---- CrowdSec ---------------------------------------------------------------

// crowdsecRepoStep richtet das CrowdSec-Herstellerrepo ein und sagt, wenn das
// misslingt. Vorher endete der Schritt auf `|| true`: schlug die Einrichtung
// fehl, installierte LCM wortlos die (deutlich ältere) Fassung der
// Distribution und meldete vollen Erfolg (R2-077).
func crowdsecRepoStep(script string) string {
	return "if curl -fsS https://packagecloud.io/install/repositories/crowdsec/crowdsec/" + script +
		" | bash >/dev/null 2>&1; then echo 'LCM: CrowdSec-Herstellerrepo eingerichtet'; " +
		"else echo 'LCM-WARNUNG: das CrowdSec-Herstellerrepo liess sich nicht einrichten - " +
		"installiert wird die Fassung der Distribution, die meist deutlich aelter ist'; fi; "
}

// crowdsecOriginCheck belegt, WOHER das installierte CrowdSec stammt.
//
// Die Repo-Einrichtung zu prüfen genügt nicht: Das Skript von packagecloud
// trägt die laufende Distributionsfassung ein und meldet Erfolg, auch wenn es
// dafür dort gar keine Pakete gibt. Auf Debian 13 ist genau das der Fall - die
// Suite „trixie" antwortet mit 404, das Repo bleibt leer, und installiert wird
// die Fassung der Distribution: CrowdSec 1.4.6 von 2022 statt 1.7.x. Bisher
// quittierte LCM das als vollen Erfolg (R2-077). Maßgeblich ist deshalb, was
// am Ende tatsächlich installiert ist - und das steht jetzt im Job.
func crowdsecOriginCheck(mgr string) string {
	var probe string
	switch pkgFamily(mgr) {
	case pkgDnf:
		probe = "(" + dnfBin(mgr) + " info crowdsec 2>/dev/null || true) | grep -qi 'crowdsec/crowdsec\\|packagecloud'"
	case pkgZypper:
		probe = "(zypper info crowdsec 2>/dev/null || true) | grep -qi 'crowdsec\\|packagecloud'"
	case pkgApk:
		// Alpine liefert CrowdSec aus dem community-Repo - dort ist die
		// Distributionsfassung die vorgesehene Quelle, kein Fremdrepo.
		return "true"
	default:
		probe = "(apt-cache policy crowdsec 2>/dev/null || true) | grep -q packagecloud"
	}
	return "if " + probe + "; then echo 'LCM: CrowdSec stammt aus dem Herstellerrepo'; " +
		"else echo 'LCM-WARNUNG: das Herstellerrepo liefert fuer diese Distributionsfassung keine Pakete - " +
		"installiert ist die Fassung der Distribution (siehe Version oben), die deutlich aelter sein kann. " +
		"Fuer eine aktuelle Fassung die Installationsanleitung von CrowdSec nutzen'; fi"
}

// crowdsecInstallScript installiert CrowdSec (Agent) und optional Bouncer,
// Collections, LAPI-Anbindung und die Allowlist. Geheimnisse (LAPI-Passwort,
// Console-Key) kommen base64-kodiert und werden erst auf dem Ziel dekodiert.
func crowdsecInstallScript(mgr, srcIP string, extra []string, in SecurityToolInput, cfg CrowdSecConfig) (string, error) {
	if pkgFamily(mgr) == pkgPacman {
		return "", ErrCrowdSecUnsupported
	}
	inst := pkgInstallCmd(mgr)
	var repo string
	switch pkgFamily(mgr) {
	case pkgApk:
		repo = "" // CrowdSec liegt im Alpine-community-Repo, kein Extra-Repo nötig.
	case pkgDnf, pkgZypper:
		repo = crowdsecRepoStep("script.rpm.sh")
	default: // apt
		repo = crowdsecRepoStep("script.deb.sh")
	}

	lines := []string{
		"echo 'LCM: CrowdSec wird installiert …'",
		// curl/gnupg für das Repo-Skript sicherstellen (best-effort).
		pkgRefreshCmd(mgr) + inst + " curl >/dev/null 2>&1 || true",
		repo + inst + " crowdsec",
		"command -v cscli >/dev/null 2>&1 || { echo 'LCM: CrowdSec-Installation fehlgeschlagen'; exit 1; }",
		// Welche Version tatsächlich installiert wurde, gehört in den Job.
		// Zuvor richtete LCM eigens das Herstellerrepo ein, dessen HTTPS-Quelle
		// dann am eigenen APT-Proxy scheiterte (R2-038) - installiert wurde
		// stillschweigend die drei Jahre alte Distributionsfassung, quittiert
		// als voller Erfolg (R2-077). Die Version sichtbar zu machen ist die
		// Kontrolle, die das künftig auffliegen lässt.
		`echo "LCM: installierte CrowdSec-Version: $(cscli version 2>&1 | head -n 3 | tr '\n' ' ')"`,
		crowdsecOriginCheck(mgr),
	}

	// Collections.
	if len(in.Collections) > 0 {
		lines = append(lines, fmt.Sprintf("cscli collections install %s 2>/dev/null || true", strings.Join(in.Collections, " ")))
	}

	// Optionaler Firewall-Bouncer (nftables bevorzugt, sonst iptables).
	if in.Bouncer {
		lines = append(lines,
			"if command -v nft >/dev/null 2>&1; then CSB=crowdsec-firewall-bouncer-nftables; else CSB=crowdsec-firewall-bouncer-iptables; fi",
			inst+` "$CSB" || echo 'LCM: Bouncer-Installation fehlgeschlagen (evtl. nicht im Repo)'`,
		)
	}

	// LAPI-Anbindung.
	switch in.LapiMode {
	case "remote":
		if cfg.LapiURL == "" || cfg.LapiLogin == "" || cfg.LapiPassword == "" {
			return "", ErrCrowdSecLapiMissing
		}
		if u, err := url.Parse(cfg.LapiURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", fmt.Errorf("ungültige LAPI-URL")
		}
		lines = append(lines,
			"install -d -m 755 /etc/crowdsec",
			fmt.Sprintf("CSPW=$(printf '%%s' '%s' | base64 -d)", helperB64(cfg.LapiPassword)),
			fmt.Sprintf("printf 'url: %s\\nlogin: %s\\npassword: %%s\\n' \"$CSPW\" > /etc/crowdsec/local_api_credentials.yaml", shellSafe(cfg.LapiURL), shellSafe(cfg.LapiLogin)),
			"chmod 600 /etc/crowdsec/local_api_credentials.yaml",
			"echo 'LCM: an Remote-LAPI angebunden'",
		)
	case "console":
		if cfg.ConsoleKey == "" {
			return "", ErrCrowdSecConsoleMissing
		}
		if !reConsoleKey.MatchString(cfg.ConsoleKey) {
			return "", fmt.Errorf("ungültiger Console-Key")
		}
		lines = append(lines, fmt.Sprintf("cscli console enroll %s 2>/dev/null || echo 'LCM: Console-Enrollment fehlgeschlagen'", cfg.ConsoleKey))
	}

	// Allowlist als Parser-Whitelist (versionsrobust über alle CrowdSec-Stände).
	var ipYAML strings.Builder
	for _, ip := range allowlistIPs(srcIP, extra) {
		ipYAML.WriteString("    - " + ip + "\\n")
	}
	lines = append(lines,
		"install -d -m 755 /etc/crowdsec/parsers/s02-enrich",
		fmt.Sprintf("printf 'name: lcm/whitelist\\ndescription: \"LCM management allowlist\"\\nwhitelist:\\n  reason: \"LCM management\"\\n  ip:\\n%s' > /etc/crowdsec/parsers/s02-enrich/lcm-whitelist.yaml", ipYAML.String()),
	)

	// Dienst aktivieren/neu laden + verifizieren.
	lines = append(lines,
		"systemctl enable --now crowdsec 2>/dev/null || rc-service crowdsec start 2>/dev/null || true",
		"systemctl reload crowdsec 2>/dev/null || systemctl restart crowdsec 2>/dev/null || rc-service crowdsec restart 2>/dev/null || true",
		"echo 'LCM: CrowdSec eingerichtet'",
		"cscli lapi status 2>/dev/null || true",
	)
	return strings.Join(lines, "\n"), nil
}

// shellSafe entfernt einfache Anführungszeichen/Steuerzeichen aus admin-
// gepflegten Klartextwerten (URL/Login), die in ein printf-Format eingesetzt
// werden. Validierung erfolgt zusätzlich in crowdsecInstallScript.
func shellSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\'' || r == '\n' || r == '\r' || r == '`' || r == '$' {
			return -1
		}
		return r
	}, s)
}

// ---- Aktion (asynchroner Job) ----------------------------------------------

// ConfigureSecurityTool installiert und konfiguriert fail2ban bzw. CrowdSec auf
// dem Server. Braucht vollen Root-Zugriff; asynchroner Job. Muster Reboot.
func (s *ServerService) ConfigureSecurityTool(scope repositories.AccessScope, id uint, in SecurityToolInput, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	if err := ensureFullSudo(server); err != nil {
		return nil, err
	}
	if err := validateSecurityToolInput(&in); err != nil {
		return nil, err
	}
	// Remote/Console-Voraussetzungen früh prüfen (bevor ein Job startet).
	if in.Tool == SecurityToolCrowdSec && (in.LapiMode == "remote" || in.LapiMode == "console") {
		if s.crowdsecConfig == nil {
			return nil, ErrCrowdSecLapiMissing
		}
	}
	// Allowlist-Referenzen früh prüfen: eine unbekannte ID lief bisher mit
	// „success" durch, und ignoreip fiel wortlos auf die Standardbelegung
	// zurück - das Verwaltungsnetz war dann sperrbar (R2-075). Der Anwender
	// sitzt hier davor: 400 statt stiller Rückfall.
	if len(in.AllowlistIDs) > 0 && s.ipAllowlistExpand != nil {
		if _, err := s.ipAllowlistExpand(in.AllowlistIDs); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidAllowlistIP, err)
		}
	}
	job, err := s.jobs.Start(&server.ID, nil, domain.RuleTypeScript, "Sicherheit-Tools ("+in.Tool+") @ "+server.Name, actor)
	if err != nil {
		return nil, err
	}
	s.audit.Log(actor, "server.security-tool", "server", id, server.Name+": "+in.Tool)
	safego.GoCleanup("job:security-tool", jobPanicCleanup(s.jobs, job), func() {
		s.runSecurityToolJob(job, server, in, actor)
	})
	return job, nil
}

func (s *ServerService) runSecurityToolJob(job *domain.Job, server *domain.Server, in SecurityToolInput, actor string) {
	if server.IsDemo {
		s.jobs.Complete(job, "Demo-Server - Sicherheits-Tool-Installation simuliert (kein SSH-Kontakt).", ptrInt(0), nil)
		return
	}
	conn, err := s.connectRec(server, "security-tool", actor)
	if err != nil {
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	defer conn.Close()

	// LCM-Quell-IP OHNE sudo lesen (sudo würde $SSH_CONNECTION verwerfen).
	srcIP := server.LCMSourceIP
	if out, _, e := conn.Run(`echo "$SSH_CONNECTION" | awk '{print $1}'`); e == nil {
		if ip := strings.TrimSpace(out); ip != "" {
			srcIP = ip
		}
	}

	// Benannte Allowlists auflösen und den Ad-hoc-IPs voranstellen (Union in
	// ignoreip/Whitelist). Fehler bei der Auflösung sind nicht fatal - die
	// Ad-hoc-IPs und die LCM-Quell-IP greifen weiterhin -, aber sie dürfen
	// NICHT stumm bleiben: der Admin würde sonst Schutz vermuten, den es
	// nicht gibt. Daher Log + sichtbare Notiz im Job-Output.
	extra := in.AllowlistIPs
	allowlistWarn := ""
	if s.ipAllowlistExpand != nil && len(in.AllowlistIDs) > 0 {
		ips, e := s.ipAllowlistExpand(in.AllowlistIDs)
		switch {
		case e != nil:
			// Unbekannte IDs werden bereits VOR dem Job abgewiesen -
			// dieser Zweig fängt Änderungen zwischen Anfrage und
			// Ausführung (Liste zwischenzeitlich gelöscht).
			slog.Warn("allowlist resolution for security tool failed",
				"server", server.Name, "tool", in.Tool, "error", e)
			allowlistWarn = "\n\nLCM-WARNUNG: die gewählten IP-Allowlists konnten nicht aufgelöst werden (" + e.Error() +
				") - ignoreip/Whitelist enthält nur die LCM-Quell-IP und die manuell angegebenen IPs."
		case len(ips) == 0:
			// Referenzierte Listen existieren, sind aber leer: der
			// Aussperrschutz, den der Anwender AUSDRÜCKLICH wollte, greift
			// nicht. Bisher fiel ignoreip wortlos auf die Standardbelegung
			// zurück (R2-075) - jetzt steht es im Job.
			allowlistWarn = "\n\nLCM-WARNUNG: die gewählten IP-Allowlists lösten zu 0 IPs auf (Listen leer) - " +
				"ignoreip/Whitelist enthält nur die LCM-Quell-IP und die manuell angegebenen IPs."
		default:
			extra = append(append([]string{}, ips...), in.AllowlistIPs...)
		}
	}

	var script string
	if in.Tool == SecurityToolFail2ban {
		script = fail2banInstallScript(server.PackageManager, srcIP, extra)
	} else {
		cfg := CrowdSecConfig{}
		if s.crowdsecConfig != nil {
			if c, e := s.crowdsecConfig(); e == nil {
				cfg = c
			}
		}
		sc, e := crowdsecInstallScript(server.PackageManager, srcIP, extra, in, cfg)
		if e != nil {
			s.jobs.Complete(job, "", nil, e)
			return
		}
		script = sc
	}

	output, code, runErr := conn.Run(privRun(server, script))
	output += allowlistWarn
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("installation endete mit exit-code %d", code)
	}
	if runErr != nil {
		s.jobs.Complete(job, output, ptrInt(code), runErr)
		return
	}

	// Zustand frisch nachlesen (installiert + aktiv) und persistieren.
	st := s.readSecurityToolState(conn, server)
	fields := map[string]any{
		"reachable":          true,
		"failed_checks":      0, // erfolgreicher Kontakt
		"lcm_source_ip":      srcIP,
		"fail2ban_installed": st.fail2banInstalled,
		"fail2ban_active":    st.fail2banActive,
		"crowdsec_installed": st.crowdsecInstalled,
		"crowdsec_active":    st.crowdsecActive,
	}
	if st.crowdsecInstalled {
		// LAPI-Anbindung festhalten: gewählter Modus + die tatsächliche
		// LAPI-URL aus der Credentials-Datei (Grundlage der „Angebundene
		// Server"-Liste auf der CrowdSec-Einstellungsseite).
		if in.Tool == SecurityToolCrowdSec && in.LapiMode != "" {
			fields["crowdsec_lapi_mode"] = in.LapiMode
		}
		for k, v := range crowdsecLapiURLFields(conn, server) {
			fields[k] = v
		}
	}
	_ = s.servers.UpdateFields(server.ID, fields)
	s.jobs.Complete(job, output, ptrInt(0), nil)
}

type securityToolState struct {
	fail2banInstalled, fail2banActive bool
	crowdsecInstalled, crowdsecActive bool
}

// serviceActiveCommand baut den Shell-Einzeiler „läuft der Dienst?" für
// systemd UND OpenRC. Nur der Exit-Code zählt (--quiet bzw. stumme
// Umleitung) - is-active druckt bei gestopptem Dienst "inactive" auf stdout,
// ein naiver String-Match würde darin "active" finden. Ausgabe: genau
// "active" oder "inactive".
func serviceActiveCommand(name string) string {
	return "(systemctl is-active --quiet " + name + " 2>/dev/null || rc-service " + name + " status >/dev/null 2>&1) && echo active || echo inactive"
}

// crowdsecLapiCredFile ist die Credentials-Datei, über die ein CrowdSec-Agent
// seine LAPI kennt - die verlässliche Quelle dafür, WOHIN der Agent meldet
// (auch bei Installationen außerhalb von LCM).
const crowdsecLapiCredFile = "/etc/crowdsec/local_api_credentials.yaml"

// crowdsecLapiURLCommand liest die LAPI-URL aus der Credentials-Datei.
// Ist die Datei nicht lesbar (z. B. im eingeschränkten Modus ohne sudo),
// meldet __na__ - der gespeicherte Wert bleibt dann unangetastet, statt
// fälschlich geleert zu werden.
func crowdsecLapiURLCommand() string {
	return "if [ -r " + crowdsecLapiCredFile + " ]; then awk '/^url:/ {print $2; exit}' " + crowdsecLapiCredFile + "; else echo __na__; fi"
}

// crowdsecLapiURLFields liest die LAPI-URL des Servers (best effort) und
// liefert das zu persistierende Feld - leeres Ergebnis (Datei fehlt/ohne url)
// löscht bewusst einen veralteten Wert.
func crowdsecLapiURLFields(conn sshx.Conn, server *domain.Server) map[string]any {
	out, _, err := conn.Run(privRun(server, crowdsecLapiURLCommand()))
	if err != nil {
		return nil
	}
	if v := strings.TrimSpace(firstLine(out)); v != "__na__" {
		return map[string]any{"crowdsec_lapi_url": v}
	}
	return nil
}

// readSecurityToolState liest über die bestehende Verbindung, ob fail2ban/
// CrowdSec vorhanden und aktiv sind.
func (s *ServerService) readSecurityToolState(conn sshx.Conn, server *domain.Server) securityToolState {
	line := func(cmd string) string {
		out, _, err := conn.Run(privRun(server, cmd))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	st := securityToolState{}
	st.fail2banInstalled = line("command -v fail2ban-client >/dev/null 2>&1 && echo yes") == "yes"
	st.crowdsecInstalled = line("command -v cscli >/dev/null 2>&1 || command -v crowdsec >/dev/null 2>&1 && echo yes") == "yes"
	if st.fail2banInstalled {
		st.fail2banActive = line(serviceActiveCommand("fail2ban")) == "active"
	}
	if st.crowdsecInstalled {
		st.crowdsecActive = line(serviceActiveCommand("crowdsec")) == "active"
	}
	return st
}
