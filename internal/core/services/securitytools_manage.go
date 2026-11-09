package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// Verwaltung bereits installierter Sicherheits-Tools.
//
// Bis hierher konnte LCM fail2ban und CrowdSec nur EINRICHTEN und danach ihren
// Zustand anzeigen - Dienst steuern, Allowlist nachziehen oder eine Sperre
// aufheben ging nur per SSH von Hand. Das ist eine halbe Funktion: Wer ein
// Werkzeug per Klick installiert, erwartet es auch per Klick zu bedienen.
//
// Die Sperrliste ist dabei der Teil, der im Betrieb am dringendsten gebraucht
// wird: Wenn sich jemand selbst aussperrt, will er nicht erst per SSH auf die
// Maschine - die er ja womoeglich gerade nicht mehr erreicht.

const (
	SecurityToolActionStart     = "start"
	SecurityToolActionStop      = "stop"
	SecurityToolActionRestart   = "restart"
	SecurityToolActionEnable    = "enable"  // Autostart einschalten
	SecurityToolActionDisable   = "disable" // Autostart ausschalten
	SecurityToolActionUninstall = "uninstall"
	SecurityToolActionAllowlist = "allowlist" // Allowlist neu schreiben
	SecurityToolActionUnban     = "unban"
)

var (
	// ErrSecurityToolAction: unbekannte Aktion.
	ErrSecurityToolAction = errors.New("unbekannte Aktion für das Sicherheits-Tool")
	// ErrInvalidLapiMode: ungültiger CrowdSec-LAPI-Modus (R2-079 - vorher 500).
	ErrInvalidLapiMode = errors.New("ungültiger LAPI-Modus")
	// ErrSecurityToolNotInstalled: die Aktion setzt ein installiertes Tool voraus.
	ErrSecurityToolNotInstalled = errors.New("das sicherheits-tool ist auf diesem server nicht installiert")
	// ErrUnbanIPMissing: Entsperren ohne (gueltige) IP.
	ErrUnbanIPMissing = errors.New("zum entsperren wird eine gueltige IP-Adresse benoetigt")
)

// SecurityToolManageInput buendelt die Eingaben der Verwaltungs-Aktionen.
type SecurityToolManageInput struct {
	Tool   string // fail2ban | crowdsec
	Action string
	// Nur fuer "unban": die freizugebende Adresse.
	UnbanIP string
	// Nur fuer "allowlist": dieselben Quellen wie bei der Einrichtung.
	AllowlistIPs []string
	AllowlistIDs []uint
}

// SecurityToolBan ist ein Eintrag der Sperrliste - vereinheitlicht ueber beide
// Werkzeuge, damit die Oberflaeche nur EINE Darstellung braucht.
type SecurityToolBan struct {
	IP    string `json:"ip"`
	Scope string `json:"scope"`           // fail2ban: Jail | CrowdSec: Szenario
	Since string `json:"since,omitempty"` // CrowdSec: Restdauer
	Cause string `json:"cause,omitempty"` // CrowdSec: Grund der Entscheidung
	Tool  string `json:"tool"`
}

// serviceCommand baut ein Dienst-Kommando, das systemd, SysV und OpenRC
// abdeckt - dieselbe Reihenfolge wie bei der Einrichtung, damit sich Alpine
// und Debian nicht unterschiedlich verhalten.
func serviceCommand(action, name string) string {
	switch action {
	case SecurityToolActionEnable:
		return "systemctl enable " + name + " 2>/dev/null || rc-update add " + name + " default 2>/dev/null || true"
	case SecurityToolActionDisable:
		return "systemctl disable " + name + " 2>/dev/null || rc-update del " + name + " default 2>/dev/null || true"
	default: // start | stop | restart
		return "systemctl " + action + " " + name + " 2>/dev/null" +
			" || service " + name + " " + action + " 2>/dev/null" +
			" || rc-service " + name + " " + action + " 2>/dev/null || true"
	}
}

// serviceUnit liefert den Dienstnamen des Werkzeugs.
func serviceUnit(tool string) string {
	if tool == SecurityToolCrowdSec {
		return "crowdsec"
	}
	return "fail2ban"
}

// securityServiceBody ist das Dienst-Kommando einer Aktion samt Meldung; es
// erwartet den Dienstnamen in $UNIT. Voll-Modus und Helper
// (security-service) benutzen denselben Rumpf.
func securityServiceBody(action string) string {
	return serviceCommand(action, `"$UNIT"`) + "\necho \"LCM: $UNIT " + action + "\""
}

// securityServiceStep steuert den Dienst des Werkzeugs: der Rumpf im
// Voll-Modus, der Helper-Aufruf (security-service) im eingeschränkten.
func securityServiceStep(restricted bool, action, tool string) string {
	return rootStep(restricted, shellVars("UNIT", serviceUnit(tool))+securityServiceBody(action),
		"security-service", tool, action)
}

// Rümpfe des Entfernens: die Konfiguration, die LCM geschrieben hat. Der Helper
// (security-purge) trägt beide und wählt nach dem Werkzeug.
const (
	// NICHT `rm -rf /etc/fail2ban`: dort liegt neben LCMs Drop-in auch
	// jail.local, in der Administratoren ihre eigenen Jails und
	// Verschärfungen pflegen. Ein Rückbau, der fremde Konfiguration
	// mitreißt, ist dieselbe Fehlerklasse wie das stille Überschreiben
	// aus R2-076. Entfernt wird deshalb LCMs eigene Datei; das
	// Verzeichnis nur, wenn danach nichts Fremdes mehr darin steht.
	purgeFail2banBody = `rm -f ` + fail2banDropin + `
rmdir /etc/fail2ban/jail.d 2>/dev/null || true
if rmdir /etc/fail2ban 2>/dev/null; then echo 'LCM: /etc/fail2ban entfernt (war leer)'; ` +
		`else echo 'LCM: /etc/fail2ban bleibt bestehen - es enthaelt Konfiguration, die nicht von LCM stammt'; fi
echo 'LCM: fail2ban entfernt (Paket, Dienst und LCM-Konfiguration)'`
	// dpkg --purge vor dem Löschen: Ein nur entferntes Paket merkt sich seine
	// Konfigurationsdateien als vorhanden, und eine spätere Neuinstallation
	// scheiterte an der fehlenden config.yaml.
	purgeCrowdSecBody = `if command -v dpkg >/dev/null; then
  dpkg --purge crowdsec crowdsec-firewall-bouncer-iptables crowdsec-firewall-bouncer-nftables 2>/dev/null || true
fi
rm -rf /etc/crowdsec
echo 'LCM: crowdsec entfernt (Paket, Dienst und /etc/crowdsec)'`
)

func purgeBody(tool string) string {
	if tool == SecurityToolCrowdSec {
		return purgeCrowdSecBody
	}
	return purgeFail2banBody
}

// uninstallScript entfernt das Werkzeug samt Konfiguration. Der Dienst wird
// zuerst gestoppt und aus dem Autostart genommen - sonst bliebe je nach
// Paketverwaltung eine verwaiste Unit zurueck.
func uninstallScript(restricted bool, mgr, tool string) string {
	pkgs := []string{"fail2ban"}
	if tool == SecurityToolCrowdSec {
		// Der Bouncer wird mitentfernt, falls vorhanden - sonst blockiert er
		// weiter nach Regeln, die niemand mehr pflegt.
		pkgs = []string{"crowdsec", "crowdsec-firewall-bouncer-iptables", "crowdsec-firewall-bouncer-nftables"}
	}
	steps := []string{
		securityServiceStep(restricted, SecurityToolActionStop, tool),
		securityServiceStep(restricted, SecurityToolActionDisable, tool),
	}
	for _, p := range pkgs {
		// Fehlende Pakete duerfen den Lauf nicht abbrechen (der Bouncer ist
		// optional, und die Bouncer-Variante haengt an der Firewall).
		steps = append(steps, pkgRemovePackagesScript(mgr, []string{p})+" 2>/dev/null || true")
	}
	steps = append(steps, rootStep(restricted, purgeBody(tool), "security-purge", tool))
	return strings.Join(steps, "\n")
}

// allowlistUpdateScript schreibt NUR die Allowlist neu und laedt sie nach -
// ohne Neuinstallation. Es ist derselbe Schritt wie bei der Einrichtung, damit
// beide Wege denselben Zustand erzeugen.
func allowlistUpdateScript(restricted bool, tool, srcIP string, extra []string) string {
	ips := allowlistIPs(srcIP, extra)
	if tool == SecurityToolCrowdSec {
		step, _ := crowdsecConfigureStep(restricted, "", CrowdSecConfig{}, ips)
		return step
	}
	return fail2banConfigStep(restricted, ips)
}

// Rümpfe von Entsperren (erwarten die IP in $IP) und Sperrliste. Der Helper
// trägt dieselben (security-unban, security-bans).
const (
	// fail2ban: Die IP kann in mehreren Jails sitzen - alle durchgehen.
	unbanFail2banBody = `for j in $(fail2ban-client status | sed -n 's/.*Jail list:[[:space:]]*//p' | tr ',' ' '); do
  fail2ban-client set "$j" unbanip "$IP" >/dev/null 2>&1 && echo "LCM: $j -> $IP entsperrt"
done
fail2ban-client unban "$IP" >/dev/null 2>&1 || true`
	unbanCrowdSecBody = `cscli decisions delete --ip "$IP"
echo "LCM: CrowdSec-Entscheidungen fuer $IP aufgehoben"`

	banListCrowdSecBody = `cscli decisions list -o json 2>/dev/null || echo '[]'`
	// Je Zeile "jail|ip" - bewusst einfach, damit kein Parser-Format der
	// jeweiligen fail2ban-Version im Weg steht.
	banListFail2banBody = `for j in $(fail2ban-client status 2>/dev/null | sed -n 's/.*Jail list:[[:space:]]*//p' | tr ',' ' '); do
  for ip in $(fail2ban-client status "$j" 2>/dev/null | sed -n 's/.*Banned IP list:[[:space:]]*//p'); do
    [ -n "$ip" ] && echo "$j|$ip"
  done
done`
)

// unbanScript hebt eine Sperre auf. Die IP ist vorher kanonisiert worden.
func unbanScript(restricted bool, tool, ip string) string {
	body := unbanFail2banBody
	if tool == SecurityToolCrowdSec {
		body = unbanCrowdSecBody
	}
	return rootStep(restricted, shellVars("IP", ip)+body, "security-unban", tool, ip)
}

// banListCommand liefert die Sperrliste in einem maschinenlesbaren Format.
func banListCommand(restricted bool, tool string) string {
	body := banListFail2banBody
	if tool == SecurityToolCrowdSec {
		body = banListCrowdSecBody
	}
	return rootStep(restricted, body, "security-bans", tool)
}

// parseBans wandelt die Ziel-Ausgabe in die vereinheitlichte Liste.
func parseBans(tool, out string) []SecurityToolBan {
	bans := []SecurityToolBan{}
	if tool == SecurityToolCrowdSec {
		// cscli liefert je Entscheidung ein Objekt; die Felder sind ueber die
		// Versionen stabil genug, unbekannte werden ignoriert.
		var raw []struct {
			Value    string `json:"value"`
			Scenario string `json:"scenario"`
			Duration string `json:"duration"`
			Origin   string `json:"origin"`
			Scope    string `json:"scope"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
			return bans
		}
		for _, d := range raw {
			if strings.TrimSpace(d.Value) == "" {
				continue
			}
			bans = append(bans, SecurityToolBan{
				IP: d.Value, Scope: d.Scenario, Since: d.Duration,
				Cause: d.Origin, Tool: SecurityToolCrowdSec,
			})
		}
		return bans
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		bans = append(bans, SecurityToolBan{
			IP: parts[1], Scope: parts[0], Tool: SecurityToolFail2ban,
		})
	}
	return bans
}

// banListTimeout begrenzt die Abfrage der Sperrliste. Sie läuft synchron im
// Seitenaufruf: Antwortet der Server nicht, soll die Seite das schnell sagen,
// statt zu warten, bis der Proxy mit einem 504 aufgibt.
var banListTimeout = 30 * time.Second

// ErrBanListTimeout: Der Server hat die Sperrliste nicht rechtzeitig geliefert.
var ErrBanListTimeout = errors.New("der server hat die sperrliste nicht rechtzeitig geliefert")

// SecurityToolBans liest die aktuelle Sperrliste. Bewusst SYNCHRON und ohne
// Job: Die Oberflaeche zeigt damit eine Liste an, und ein Job-Umweg mit
// Polling waere fuer eine reine Abfrage unangemessen.
func (s *ServerService) SecurityToolBans(scope repositories.AccessScope, id uint, tool, actor string) ([]SecurityToolBan, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if tool != SecurityToolFail2ban && tool != SecurityToolCrowdSec {
		return nil, ErrUnknownSecurityTool
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	if server.IsDemo {
		return []SecurityToolBan{}, nil
	}
	conn, err := s.connectRecRead(server, "security-tool-bans", actor)
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	out, err := runBanList(conn, privRun(server, banListCommand(server.RestrictedSudo, tool)))
	if err != nil {
		return nil, err
	}
	return parseBans(tool, out), nil
}

// runBanList wartet höchstens banListTimeout auf das Kommando.
func runBanList(conn sshx.Conn, cmd string) (string, error) {
	return runBounded(conn, cmd, banListTimeout, ErrBanListTimeout)
}

// ManageSecurityTool fuehrt eine Verwaltungs-Aktion auf einem bereits
// installierten Werkzeug aus.
func (s *ServerService) ManageSecurityTool(scope repositories.AccessScope, id uint, in SecurityToolManageInput, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := planManageSecurityTool(server, &in); err != nil {
		return nil, err
	}

	job, err := s.jobs.Start(&server.ID, nil, domain.RuleTypeScript,
		"Sicherheit-Tools ("+in.Tool+" "+in.Action+") @ "+server.Name, actor)
	if err != nil {
		return nil, err
	}
	s.audit.Log(actor, "server.security-tool-manage", "server", id,
		server.Name+": "+in.Tool+" "+in.Action)
	safego.GoCleanup("job:security-tool-manage", jobPanicCleanup(s.jobs, job), func() {
		s.runSecurityToolManageJob(job, server, in, actor)
	})
	return job, nil
}

// planManageSecurityTool prüft Server und Eingabe einer Verwaltungs-Aktion -
// für den Lauf und die Befehlsvorschau gleich. Die IP zum Entsperren wird
// dabei kanonisiert.
func planManageSecurityTool(server *domain.Server, in *SecurityToolManageInput) error {
	if in.Tool != SecurityToolFail2ban && in.Tool != SecurityToolCrowdSec {
		return ErrUnknownSecurityTool
	}
	if err := ensureNotRouterOS(server); err != nil {
		return err
	}
	switch in.Action {
	case SecurityToolActionStart, SecurityToolActionStop, SecurityToolActionRestart,
		SecurityToolActionEnable, SecurityToolActionDisable,
		SecurityToolActionUninstall, SecurityToolActionAllowlist:
	case SecurityToolActionUnban:
		// Die IP landet woertlich in einem Kommando - hier wird sie streng
		// kanonisiert, nicht nur oberflaechlich geprueft.
		canon, _, err := canonicalAddr(strings.TrimSpace(in.UnbanIP))
		if err != nil || canon == "" {
			return ErrUnbanIPMissing
		}
		in.UnbanIP = canon
	default:
		return ErrSecurityToolAction
	}
	return nil
}

func (s *ServerService) runSecurityToolManageJob(job *domain.Job, server *domain.Server, in SecurityToolManageInput, actor string) {
	if server.IsDemo {
		s.jobs.CompleteSkipped(job, "Demo-Server - Aktion simuliert (kein SSH-Kontakt).")
		return
	}
	conn, err := s.connectRec(server, "security-tool-manage", actor)
	if err != nil {
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	defer conn.Close()

	// Nichts anfassen, was nicht da ist - sonst entstuenden irrefuehrende
	// Erfolgsmeldungen ("gestoppt", obwohl nie installiert).
	state := s.readSecurityToolState(conn, server)
	installed := state.fail2banInstalled
	if in.Tool == SecurityToolCrowdSec {
		installed = state.crowdsecInstalled
	}
	if !installed {
		s.jobs.Complete(job, "", nil, ErrSecurityToolNotInstalled)
		return
	}

	script, err := s.buildManageScript(conn, server, in)
	if err != nil {
		s.jobs.Complete(job, "", nil, err)
		return
	}

	out, code, err := conn.Run(privRun(server, script))
	if err != nil {
		s.jobs.Complete(job, out, &code, err)
		return
	}
	// Zustand nach der Aktion mitliefern - sonst muesste der Anwender raten,
	// ob der Dienst jetzt wirklich laeuft.
	after := s.readSecurityToolState(conn, server)
	active := after.fail2banActive
	if in.Tool == SecurityToolCrowdSec {
		active = after.crowdsecActive
	}
	status := "\n\nZustand danach: "
	switch {
	case in.Action == SecurityToolActionUninstall:
		status += in.Tool + " ist entfernt."
	case active:
		status += in.Tool + " laeuft."
	default:
		status += in.Tool + " laeuft NICHT."
	}
	s.jobs.Complete(job, out+status, &code, nil)
}

// buildManageScript waehlt das Skript zur Aktion. Nur das Neuschreiben der
// Allowlist braucht die LCM-Quell-IP vom Server.
func (s *ServerService) buildManageScript(conn sshx.Conn, server *domain.Server, in SecurityToolManageInput) (string, error) {
	srcIP := server.LCMSourceIP
	if in.Action == SecurityToolActionAllowlist {
		srcIP = readLCMSourceIP(conn, server)
	}
	return s.manageScript(server, in, srcIP)
}

// manageScript baut das Skript einer Verwaltungs-Aktion - der Lauf und die
// Befehlsvorschau rufen es gleich auf.
func (s *ServerService) manageScript(server *domain.Server, in SecurityToolManageInput, srcIP string) (string, error) {
	switch in.Action {
	case SecurityToolActionStart, SecurityToolActionStop, SecurityToolActionRestart,
		SecurityToolActionEnable, SecurityToolActionDisable:
		return securityServiceStep(server.RestrictedSudo, in.Action, in.Tool), nil

	case SecurityToolActionUninstall:
		return uninstallScript(server.RestrictedSudo, server.PackageManager, in.Tool), nil

	case SecurityToolActionUnban:
		return unbanScript(server.RestrictedSudo, in.Tool, in.UnbanIP), nil

	case SecurityToolActionAllowlist:
		extra := append([]string{}, in.AllowlistIPs...)
		if len(in.AllowlistIDs) > 0 && s.ipAllowlistExpand != nil {
			resolved, e := s.ipAllowlistExpand(in.AllowlistIDs)
			if e != nil {
				return "", fmt.Errorf("ip-allowlists aufloesen: %w", e)
			}
			extra = append(resolved, extra...)
		}
		return allowlistUpdateScript(server.RestrictedSudo, in.Tool, srcIP, extra), nil
	}
	return "", ErrSecurityToolAction
}
