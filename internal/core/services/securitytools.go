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
// IMMER in die Allowlist gesetzt, damit LCM sich nicht selbst aussperrt. Im
// eingeschränkten Sudo-Modus laufen die Root-Schritte (Herstellerrepo,
// Konfiguration, Dienststart) über den validierenden LCM-Helper, siehe
// rootStep.

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
	ErrCrowdSecLapiMissing = errors.New("keine CrowdSec-LAPI gewählt oder hinterlegt (Einstellungen → CrowdSec)")
	// ErrCrowdSecConsoleMissing: Console gewählt, aber kein Enrollment-Key hinterlegt.
	ErrCrowdSecConsoleMissing = errors.New("kein CrowdSec-Console-Key hinterlegt (Einstellungen → CrowdSec)")
	// ErrCrowdSecUnsupported: die Distribution bietet CrowdSec nicht als Paket.
	ErrCrowdSecUnsupported = errors.New("CrowdSec wird auf dieser Paketverwaltung nicht per Knopfdruck unterstützt")
)

// reCollection erlaubt nur unbedenkliche CrowdSec-Collection-Namen (z.B.
// crowdsecurity/sshd) - sie landen wörtlich in einem cscli-Aufruf.
var reCollection = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

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
	// LapiID wählt im Remote-Modus die LAPI (Einstellungen → CrowdSec).
	// 0 = die einzige, wenn es genau eine gibt.
	LapiID uint
}

// validateSecurityToolInput prüft Tool, IPs, Collections und LAPI-Modus.
func validateSecurityToolInput(in *SecurityToolInput) error {
	switch in.Tool {
	case SecurityToolFail2ban, SecurityToolCrowdSec:
	default:
		return ErrUnknownSecurityTool
	}
	// Ad-hoc-Allowlist-IPs kanonisieren - IP ODER CIDR (ignoreip nimmt beides,
	// crowdsecWhitelistBody trennt sie; die aus benannten Allowlists aufgelösten
	// Einträge sind ohnehin bereits kanonisch).
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
//
// restricted: ohne Root-Shell. Die Paketschritte laufen dann über die
// Paketverwaltung (sudo-Whitelist), das Schreiben der Konfiguration und der
// Dienststart über den Helper (fail2ban-config).
func fail2banInstallScript(restricted bool, mgr, srcIP string, extra []string) string {
	return strings.Join([]string{
		helperCurrentStep(restricted),
		fail2banRepoStep(mgr),
		pkgInstallScript(mgr, []string{"fail2ban"}),
		// Auch nach dem Schritt oben kann das Paket fehlen: Der Job darf dann nicht als erfolgreich enden.
		"command -v fail2ban-client >/dev/null 2>&1 || { echo 'LCM-FEHLER: fail2ban ist nicht installiert, es wurde nichts eingerichtet'; exit 1; }",
		fail2banConfigStep(restricted, allowlistIPs(srcIP, extra)),
	}, "\n")
}

// fail2banConfigBody schreibt LCMs Drop-in mit der Allowlist und lädt fail2ban
// neu; er erwartet die Allowlist (Leerzeichen-getrennt) in $IGNORE. Einrichtung
// und nachträgliches Ändern der Allowlist benutzen ihn gleichermaßen.
const fail2banConfigBody = `install -d -m 755 /etc/fail2ban/jail.d
printf '# Von LCM verwaltet - eigene Jails und Verschaerfungen gehoeren in jail.local.\n[DEFAULT]\nbackend = systemd\nignoreip = %s\n\n[sshd]\nenabled = true\n' "$IGNORE" > ` + fail2banDropin + `
if [ -f /etc/fail2ban/jail.local ]; then echo 'LCM: vorhandene /etc/fail2ban/jail.local bleibt unveraendert bestehen; LCMs Drop-in (99-lcm.local) wird danach gelesen und hat fuer ignoreip und [sshd] Vorrang'; fi
systemctl enable --now fail2ban 2>/dev/null || service fail2ban restart 2>/dev/null || rc-service fail2ban restart 2>/dev/null || true
fail2ban-client reload 2>/dev/null || true
echo "LCM: fail2ban-Konfiguration geschrieben (ignoreip: $IGNORE)"
fail2ban-client status sshd 2>/dev/null || true`

// fail2banConfigStep liefert den Root-Schritt: den Rumpf samt Variable im
// Voll-Modus, den Aufruf des Helpers (fail2ban-config) im eingeschränkten.
func fail2banConfigStep(restricted bool, ips []string) string {
	return rootStep(restricted, shellVars("IGNORE", strings.Join(ips, " "))+fail2banConfigBody, "fail2ban-config", ips...)
}

// fail2banRepoStep richtet EPEL ein, wenn fail2ban in den Paketquellen fehlt:
// Auf Rocky, Alma und RHEL liegt es nur dort. `epel-release` steckt bei den
// Klonen in „extras", RHEL kennt es nicht und bekommt das Paket direkt vom
// Fedora-Projekt. Fedora selbst führt fail2ban und läuft hier durch.
func fail2banRepoStep(mgr string) string {
	if pkgFamily(mgr) != pkgDnf {
		return ":"
	}
	dnf := dnfBin(mgr)
	return "if ! " + dnf + " -q info fail2ban >/dev/null 2>&1; then " +
		"if " + dnf + " install -y epel-release >/dev/null 2>&1 || " +
		dnf + " install -y \"https://dl.fedoraproject.org/pub/epel/epel-release-latest-$(rpm -E %rhel).noarch.rpm\" >/dev/null 2>&1; " +
		"then echo 'LCM: Paketquelle EPEL eingerichtet (dort liegt fail2ban)'; " +
		"else echo 'LCM-WARNUNG: die Paketquelle EPEL liess sich nicht einrichten'; fi; fi"
}

// ---- CrowdSec ---------------------------------------------------------------

// crowdsecRepoBody richtet das CrowdSec-Herstellerrepo ein und sagt, wenn das
// misslingt. Vorher endete der Schritt auf `|| true`: schlug die Einrichtung
// fehl, installierte LCM wortlos die (deutlich ältere) Fassung der
// Distribution und meldete vollen Erfolg (R2-077). Erwartet in
// $CS_REPO_SCRIPT den Namen des Installationsskripts (script.deb.sh bzw.
// script.rpm.sh).
const crowdsecRepoBody = `if curl -fsS "https://packagecloud.io/install/repositories/crowdsec/crowdsec/$CS_REPO_SCRIPT" | bash >/dev/null 2>&1; then echo 'LCM: CrowdSec-Herstellerrepo eingerichtet'; ` +
	`else echo 'LCM-WARNUNG: das CrowdSec-Herstellerrepo liess sich nicht einrichten - ` +
	`installiert wird die Fassung der Distribution, die meist deutlich aelter ist'; fi`

// crowdsecRepoStep ist der Root-Schritt für die Paketart kind ("deb" | "rpm"):
// der Rumpf im Voll-Modus, der Helper-Aufruf (crowdsec-repo) im eingeschränkten.
func crowdsecRepoStep(restricted bool, kind string) string {
	return rootStep(restricted, shellVars("CS_REPO_SCRIPT", "script."+kind+".sh")+crowdsecRepoBody, "crowdsec-repo", kind)
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
//
// restricted: ohne Root-Shell. Paketschritte laufen über die Paketverwaltung
// (sudo-Whitelist); Herstellerrepo, Collections und Konfiguration über die
// Helper-Unterkommandos crowdsec-repo, crowdsec-collections und
// crowdsec-configure - mit denselben Rümpfen wie im Voll-Modus.
func crowdsecInstallScript(restricted bool, mgr, srcIP string, extra []string, in SecurityToolInput, cfg CrowdSecConfig) (string, error) {
	if pkgFamily(mgr) == pkgPacman {
		return "", ErrCrowdSecUnsupported
	}
	configure, err := crowdsecConfigureStep(restricted, in.LapiMode, cfg, allowlistIPs(srcIP, extra))
	if err != nil {
		return "", err
	}
	inst := pkgInstallCmd(mgr)

	lines := []string{
		helperCurrentStep(restricted),
		"echo 'LCM: CrowdSec wird installiert …'",
		// curl/gnupg für das Repo-Skript sicherstellen (best-effort).
		pkgRefreshCmd(mgr) + inst + " curl >/dev/null 2>&1 || true",
	}
	switch pkgFamily(mgr) {
	case pkgApk:
		// CrowdSec liegt im Alpine-community-Repo, kein Extra-Repo nötig.
	case pkgDnf, pkgZypper:
		lines = append(lines, crowdsecRepoStep(restricted, "rpm"))
	default: // apt
		lines = append(lines, crowdsecRepoStep(restricted, "deb"))
	}
	lines = append(lines,
		inst+" crowdsec",
		"command -v cscli >/dev/null 2>&1 || { echo 'LCM: CrowdSec-Installation fehlgeschlagen'; exit 1; }",
		// Welche Version tatsächlich installiert wurde, gehört in den Job.
		// Zuvor richtete LCM eigens das Herstellerrepo ein, dessen HTTPS-Quelle
		// dann am eigenen APT-Proxy scheiterte (R2-038) - installiert wurde
		// stillschweigend die drei Jahre alte Distributionsfassung, quittiert
		// als voller Erfolg (R2-077). Die Version sichtbar zu machen ist die
		// Kontrolle, die das künftig auffliegen lässt.
		`echo "LCM: installierte CrowdSec-Version: $(cscli version 2>&1 | head -n 3 | tr '\n' ' ')"`,
		crowdsecOriginCheck(mgr),
	)

	// Collections.
	if len(in.Collections) > 0 {
		lines = append(lines, rootStep(restricted,
			shellVars("COLLS", strings.Join(in.Collections, " "))+crowdsecCollectionsBody,
			"crowdsec-collections", in.Collections...))
	}

	// Optionaler Firewall-Bouncer (nftables bevorzugt, sonst iptables).
	if in.Bouncer {
		lines = append(lines,
			"if command -v nft >/dev/null 2>&1; then CSB=crowdsec-firewall-bouncer-nftables; else CSB=crowdsec-firewall-bouncer-iptables; fi",
			inst+` "$CSB" || echo 'LCM: Bouncer-Installation fehlgeschlagen (evtl. nicht im Repo)'`,
		)
	}

	// LAPI-Anbindung, Allowlist und Dienst.
	return strings.Join(append(lines, configure), "\n"), nil
}

// crowdsecCollectionsBody installiert die Collections aus $COLLS (unquotiert:
// Leerzeichen-getrennte Namen).
const crowdsecCollectionsBody = `cscli collections install $COLLS 2>/dev/null || true`

// crowdsecWhitelistBody schreibt LCMs Allowlist aus $IPS. CrowdSec trennt streng:
// Unter `ip:` stehen nur einzelne Adressen, unter `cidr:` nur Netze. Ein Netz
// unter `ip:` lässt den Dienst gar nicht erst starten, und mit ihm scheitert
// jede Paket-Einrichtung, die ihn neu startet.
const crowdsecWhitelistBody = `CS_ADDRS=""; CS_NETS=""
for ip in $IPS; do
  case "$ip" in
    */*) CS_NETS="$CS_NETS $ip" ;;
    *) CS_ADDRS="$CS_ADDRS $ip" ;;
  esac
done
install -d -m 755 /etc/crowdsec/parsers/s02-enrich
{ printf 'name: lcm/whitelist\ndescription: "LCM management allowlist"\nwhitelist:\n  reason: "LCM management"\n'
  if [ -n "$CS_ADDRS" ]; then
    printf '  ip:\n'
    for ip in $CS_ADDRS; do printf '    - "%s"\n' "$ip"; done
  fi
  if [ -n "$CS_NETS" ]; then
    printf '  cidr:\n'
    for ip in $CS_NETS; do printf '    - "%s"\n' "$ip"; done
  fi
} > /etc/crowdsec/parsers/s02-enrich/lcm-whitelist.yaml`

// crowdsecConfigureBody bindet die LAPI an (remote/console), schreibt die
// Allowlist als Parser-Whitelist (versionsrobust über alle CrowdSec-Stände) und
// aktiviert/lädt den Dienst. Erwartet $LAPI_MODE (remote | console, sonst
// bleibt die LAPI unberührt), $LAPI_URL, $LAPI_LOGIN, $LAPI_PW_B64 (base64),
// $CONSOLE_KEY und die Allowlist in $IPS (Leerzeichen-getrennt).
const crowdsecConfigureBody = `case "$LAPI_MODE" in
  remote)
    install -d -m 755 /etc/crowdsec
    CSPW=$(printf '%s' "$LAPI_PW_B64" | base64 -d)
    printf 'url: %s\nlogin: %s\npassword: %s\n' "$LAPI_URL" "$LAPI_LOGIN" "$CSPW" > ` + crowdsecLapiCredFile + `
    chmod 600 ` + crowdsecLapiCredFile + `
    echo 'LCM: an Remote-LAPI angebunden' ;;
  console)
    cscli console enroll "$CONSOLE_KEY" 2>/dev/null || echo 'LCM: Console-Enrollment fehlgeschlagen' ;;
esac
` + crowdsecWhitelistBody + `
if ! crowdsec -c /etc/crowdsec/config.yaml -t -error; then
  echo 'LCM: CrowdSec lehnt die Konfiguration ab, der Dienst bleibt unveraendert' >&2
  exit 1
fi
systemctl enable --now crowdsec 2>/dev/null || rc-service crowdsec start 2>/dev/null || true
systemctl reload crowdsec 2>/dev/null || systemctl restart crowdsec 2>/dev/null || rc-service crowdsec restart 2>/dev/null || true
echo "LCM: CrowdSec eingerichtet (Allowlist: $IPS)"
cscli lapi status 2>/dev/null || true`

// crowdsecConfigureStep prüft die Zugangsdaten des LAPI-Modus und liefert den
// Root-Schritt crowdsecConfigureBody: samt Variablen im Voll-Modus, als Aufruf
// des Helpers (crowdsec-configure) im eingeschränkten. Die Allowlist-Änderung
// ohne Neueinrichtung ist derselbe Schritt ohne LAPI-Modus.
func crowdsecConfigureStep(restricted bool, mode string, cfg CrowdSecConfig, ips []string) (string, error) {
	pwB64 := ""
	switch mode {
	case "remote":
		if cfg.LapiURL == "" || cfg.LapiLogin == "" || cfg.LapiPassword == "" {
			return "", ErrCrowdSecLapiMissing
		}
		if u, err := url.Parse(cfg.LapiURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", fmt.Errorf("ungültige LAPI-URL")
		}
		pwB64 = helperB64(cfg.LapiPassword)
	case "console":
		if cfg.ConsoleKey == "" {
			return "", ErrCrowdSecConsoleMissing
		}
		if !reConsoleKey.MatchString(cfg.ConsoleKey) {
			return "", fmt.Errorf("ungültiger Console-Key")
		}
	default:
		mode = "keep"
	}
	vars := shellVars("LAPI_MODE", mode, "LAPI_URL", cfg.LapiURL, "LAPI_LOGIN", cfg.LapiLogin,
		"LAPI_PW_B64", pwB64, "CONSOLE_KEY", cfg.ConsoleKey, "IPS", strings.Join(ips, " "))
	args := append([]string{mode, dashIfEmpty(cfg.LapiURL), dashIfEmpty(cfg.LapiLogin), dashIfEmpty(pwB64), dashIfEmpty(cfg.ConsoleKey)}, ips...)
	return rootStep(restricted, vars+crowdsecConfigureBody, "crowdsec-configure", args...), nil
}

// dashIfEmpty ist die Helper-Schreibweise für „nicht gesetzt".
func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ---- Aktion (asynchroner Job) ----------------------------------------------

// ConfigureSecurityTool installiert und konfiguriert fail2ban bzw. CrowdSec auf
// dem Server. Im eingeschränkten Modus laufen die Root-Schritte über den Helper; asynchroner Job.
func (s *ServerService) ConfigureSecurityTool(scope repositories.AccessScope, id uint, in SecurityToolInput, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := s.planSecurityTool(server, &in); err != nil {
		return nil, err
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

// planSecurityTool prüft Server und Eingabe der Einrichtung - für den Lauf
// und die Befehlsvorschau gleich. in wird dabei normalisiert.
func (s *ServerService) planSecurityTool(server *domain.Server, in *SecurityToolInput) error {
	if err := ensureNotRouterOS(server); err != nil {
		return err
	}
	if err := validateSecurityToolInput(in); err != nil {
		return err
	}
	// Remote/Console-Voraussetzungen früh prüfen (bevor ein Job startet).
	if in.Tool == SecurityToolCrowdSec && (in.LapiMode == "remote" || in.LapiMode == "console") {
		if s.crowdsecConfig == nil {
			return ErrCrowdSecLapiMissing
		}
		if cfg, err := s.crowdsecConfig(in.LapiID); in.LapiMode == "remote" && (err != nil || cfg.LapiURL == "") {
			return ErrCrowdSecLapiMissing
		}
	}
	// Allowlist-Referenzen früh prüfen: eine unbekannte ID lief bisher mit
	// „success" durch, und ignoreip fiel wortlos auf die Standardbelegung
	// zurück - das Verwaltungsnetz war dann sperrbar (R2-075). Der Anwender
	// sitzt hier davor: 400 statt stiller Rückfall.
	if len(in.AllowlistIDs) > 0 && s.ipAllowlistExpand != nil {
		if _, err := s.ipAllowlistExpand(in.AllowlistIDs); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidAllowlistIP, err)
		}
	}
	return nil
}

func (s *ServerService) runSecurityToolJob(job *domain.Job, server *domain.Server, in SecurityToolInput, actor string) {
	if server.IsDemo {
		s.jobs.CompleteSkipped(job, "Demo-Server - Sicherheits-Tool-Installation simuliert (kein SSH-Kontakt).")
		return
	}
	conn, err := s.connectRec(server, "security-tool", actor)
	if err != nil {
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	defer conn.Close()

	srcIP := readLCMSourceIP(conn, server)
	script, allowlistWarn, err := s.securityToolScript(server, in, srcIP)
	if err != nil {
		s.jobs.Complete(job, "", nil, err)
		return
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

// securityToolScript baut das Einrichtungs-Skript für die gelesene
// LCM-Quell-IP - der Lauf und die Befehlsvorschau rufen es gleich auf. Der
// zweite Rückgabewert ist eine Warnung für den Job, wenn die benannten
// Allowlists nicht greifen.
func (s *ServerService) securityToolScript(server *domain.Server, in SecurityToolInput, srcIP string) (string, string, error) {
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
		script = fail2banInstallScript(server.RestrictedSudo, server.PackageManager, srcIP, extra)
	} else {
		cfg := CrowdSecConfig{}
		if s.crowdsecConfig != nil {
			if c, e := s.crowdsecConfig(in.LapiID); e == nil {
				cfg = c
			}
		}
		sc, e := crowdsecInstallScript(server.RestrictedSudo, server.PackageManager, srcIP, extra, in, cfg)
		if e != nil {
			return "", "", e
		}
		script = sc
	}
	return script, allowlistWarn, nil
}

// lcmSourceIPCmd liest die Adresse, von der LCM den Server erreicht. OHNE
// sudo: sudo würde $SSH_CONNECTION verwerfen.
const lcmSourceIPCmd = `echo "$SSH_CONNECTION" | awk '{print $1}'`

// readLCMSourceIP liest die LCM-Quell-IP auf dem Server; ohne Ergebnis gilt
// die zuletzt gespeicherte.
func readLCMSourceIP(conn sshx.Conn, server *domain.Server) string {
	if out, _, e := conn.Run(lcmSourceIPCmd); e == nil {
		if ip := strings.TrimSpace(out); ip != "" {
			return ip
		}
	}
	return server.LCMSourceIP
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

// crowdsecLapiURLBody liest die LAPI-URL aus der Credentials-Datei. Die Datei
// gehört root (0600): im eingeschränkten Modus liest sie der Helper
// (crowdsec-lapi-url, derselbe Rumpf). Ist sie nicht lesbar, meldet das
// Kommando __na__ - der gespeicherte Wert bleibt dann unangetastet, statt
// fälschlich geleert zu werden.
const crowdsecLapiURLBody = "if [ -r " + crowdsecLapiCredFile + " ]; then awk '/^url:/ {print $2; exit}' " + crowdsecLapiCredFile + "; else echo __na__; fi"

func crowdsecLapiURLCommand(restricted bool) string {
	return rootStep(restricted, crowdsecLapiURLBody, "crowdsec-lapi-url")
}

// crowdsecLapiURLFields liest die LAPI-URL des Servers (best effort) und
// liefert das zu persistierende Feld - leeres Ergebnis (Datei fehlt/ohne url)
// löscht bewusst einen veralteten Wert. Alles andere als eine URL bleibt liegen:
// conn.Run mischt stderr in die Ausgabe, und ein Helper ohne dieses Kommando
// meldete sich sonst als gespeicherte URL.
func crowdsecLapiURLFields(conn sshx.Conn, server *domain.Server) map[string]any {
	out, code, err := conn.Run(privRun(server, crowdsecLapiURLCommand(server.RestrictedSudo)))
	if err != nil || code != 0 {
		return nil
	}
	v := strings.TrimSpace(firstLine(out))
	if v != "" && !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
		return nil
	}
	return map[string]any{"crowdsec_lapi_url": v}
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
