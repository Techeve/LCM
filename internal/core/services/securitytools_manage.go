package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	ErrSecurityToolAction = errors.New("unbekannte aktion fuer das sicherheits-tool")
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

// uninstallScript entfernt das Werkzeug samt Konfiguration. Der Dienst wird
// zuerst gestoppt und aus dem Autostart genommen - sonst bliebe je nach
// Paketverwaltung eine verwaiste Unit zurueck.
func uninstallScript(mgr, tool string) string {
	unit := serviceUnit(tool)
	pkgs := []string{"fail2ban"}
	conf := "/etc/fail2ban"
	if tool == SecurityToolCrowdSec {
		// Der Bouncer wird mitentfernt, falls vorhanden - sonst blockiert er
		// weiter nach Regeln, die niemand mehr pflegt.
		pkgs = []string{"crowdsec", "crowdsec-firewall-bouncer-iptables", "crowdsec-firewall-bouncer-nftables"}
		conf = "/etc/crowdsec"
	}
	steps := []string{
		serviceCommand(SecurityToolActionStop, unit),
		serviceCommand(SecurityToolActionDisable, unit),
	}
	for _, p := range pkgs {
		// Fehlende Pakete duerfen den Lauf nicht abbrechen (der Bouncer ist
		// optional, und die Bouncer-Variante haengt an der Firewall).
		steps = append(steps, pkgRemovePackagesScript(mgr, []string{p})+" 2>/dev/null || true")
	}
	if tool == SecurityToolFail2ban {
		// NICHT `rm -rf /etc/fail2ban`: dort liegt neben LCMs Drop-in auch
		// jail.local, in der Administratoren ihre eigenen Jails und
		// Verschärfungen pflegen. Ein Rückbau, der fremde Konfiguration
		// mitreißt, ist dieselbe Fehlerklasse wie das stille Überschreiben
		// aus R2-076. Entfernt wird deshalb LCMs eigene Datei; das
		// Verzeichnis nur, wenn danach nichts Fremdes mehr darin steht.
		steps = append(steps,
			"rm -f "+fail2banDropin,
			"rmdir /etc/fail2ban/jail.d 2>/dev/null || true",
			"if rmdir "+conf+" 2>/dev/null; then echo 'LCM: "+conf+" entfernt (war leer)'; "+
				"else echo 'LCM: "+conf+" bleibt bestehen - es enthaelt Konfiguration, die nicht von LCM stammt'; fi",
			fmt.Sprintf("echo 'LCM: %s entfernt (Paket, Dienst und LCM-Konfiguration)'", tool),
		)
		return strings.Join(steps, "\n")
	}
	steps = append(steps,
		"rm -rf "+conf,
		fmt.Sprintf("echo 'LCM: %s entfernt (Paket, Dienst und %s)'", tool, conf),
	)
	return strings.Join(steps, "\n")
}

// allowlistUpdateScript schreibt NUR die Allowlist neu und laedt sie nach -
// ohne Neuinstallation. Der Aufbau der Datei ist identisch zur Einrichtung,
// damit beide Wege denselben Zustand erzeugen.
func allowlistUpdateScript(tool, srcIP string, extra []string) string {
	ips := allowlistIPs(srcIP, extra)
	if tool == SecurityToolCrowdSec {
		var ipYAML strings.Builder
		for _, ip := range ips {
			ipYAML.WriteString("    - \"" + ip + "\"\\n")
		}
		return strings.Join([]string{
			"install -d -m 755 /etc/crowdsec/parsers/s02-enrich",
			fmt.Sprintf("printf 'name: lcm/whitelist\\ndescription: \"LCM management allowlist\"\\nwhitelist:\\n  reason: \"LCM management\"\\n  ip:\\n%s' > /etc/crowdsec/parsers/s02-enrich/lcm-whitelist.yaml", ipYAML.String()),
			serviceCommand(SecurityToolActionRestart, "crowdsec"),
			fmt.Sprintf("echo 'LCM: CrowdSec-Allowlist aktualisiert (%s)'", strings.Join(ips, " ")),
		}, "\n")
	}
	ignore := strings.Join(ips, " ")
	return strings.Join([]string{
		"install -d -m 755 /etc/fail2ban",
		// jail.local wird komplett neu geschrieben - wie bei der Einrichtung.
		fmt.Sprintf("printf '[DEFAULT]\\nbackend = systemd\\nignoreip = %s\\n\\n[sshd]\\nenabled = true\\n' > /etc/fail2ban/jail.local", ignore),
		"fail2ban-client reload 2>/dev/null || " + serviceCommand(SecurityToolActionRestart, "fail2ban"),
		fmt.Sprintf("echo 'LCM: fail2ban-Allowlist aktualisiert (ignoreip: %s)'", ignore),
	}, "\n")
}

// unbanScript hebt eine Sperre auf. Die IP ist vorher kanonisiert worden - sie
// landet woertlich in einem Kommando.
func unbanScript(tool, ip string) string {
	if tool == SecurityToolCrowdSec {
		return strings.Join([]string{
			"cscli decisions delete --ip " + ip,
			fmt.Sprintf("echo 'LCM: CrowdSec-Entscheidungen fuer %s aufgehoben'", ip),
		}, "\n")
	}
	// fail2ban: Die IP kann in mehreren Jails sitzen - alle durchgehen.
	return strings.Join([]string{
		"for j in $(fail2ban-client status | sed -n 's/.*Jail list:[[:space:]]*//p' | tr ',' ' '); do",
		"  fail2ban-client set \"$j\" unbanip " + ip + " >/dev/null 2>&1 && echo \"LCM: $j -> " + ip + " entsperrt\"",
		"done",
		"fail2ban-client unban " + ip + " >/dev/null 2>&1 || true",
	}, "\n")
}

// banListCommand liefert die Sperrliste in einem maschinenlesbaren Format.
func banListCommand(tool string) string {
	if tool == SecurityToolCrowdSec {
		return "cscli decisions list -o json 2>/dev/null || echo '[]'"
	}
	// Je Zeile "jail|ip" - bewusst einfach, damit kein Parser-Format der
	// jeweiligen fail2ban-Version im Weg steht.
	return strings.Join([]string{
		"for j in $(fail2ban-client status 2>/dev/null | sed -n 's/.*Jail list:[[:space:]]*//p' | tr ',' ' '); do",
		"  for ip in $(fail2ban-client status \"$j\" 2>/dev/null | sed -n 's/.*Banned IP list:[[:space:]]*//p'); do",
		"    [ -n \"$ip\" ] && echo \"$j|$ip\"",
		"  done",
		"done",
	}, "\n")
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

	out, _, err := conn.Run(privRun(server, banListCommand(tool)))
	if err != nil {
		return nil, err
	}
	return parseBans(tool, out), nil
}

// ManageSecurityTool fuehrt eine Verwaltungs-Aktion auf einem bereits
// installierten Werkzeug aus.
func (s *ServerService) ManageSecurityTool(scope repositories.AccessScope, id uint, in SecurityToolManageInput, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if in.Tool != SecurityToolFail2ban && in.Tool != SecurityToolCrowdSec {
		return nil, ErrUnknownSecurityTool
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	// Alle Aktionen greifen in Dienste und Systemkonfiguration ein - im
	// eingeschraenkten Modus ist das bewusst gesperrt, wie bei der Einrichtung.
	if err := ensureFullSudo(server); err != nil {
		return nil, err
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
			return nil, ErrUnbanIPMissing
		}
		in.UnbanIP = canon
	default:
		return nil, ErrSecurityToolAction
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

func (s *ServerService) runSecurityToolManageJob(job *domain.Job, server *domain.Server, in SecurityToolManageInput, actor string) {
	if server.IsDemo {
		s.jobs.Complete(job, "Demo-Server - Aktion simuliert (kein SSH-Kontakt).", ptrInt(0), nil)
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

// buildManageScript waehlt das Skript zur Aktion.
func (s *ServerService) buildManageScript(conn sshx.Conn, server *domain.Server, in SecurityToolManageInput) (string, error) {
	unit := serviceUnit(in.Tool)
	switch in.Action {
	case SecurityToolActionStart, SecurityToolActionStop, SecurityToolActionRestart,
		SecurityToolActionEnable, SecurityToolActionDisable:
		return serviceCommand(in.Action, unit) + "\necho 'LCM: " + unit + " " + in.Action + "'", nil

	case SecurityToolActionUninstall:
		return uninstallScript(server.PackageManager, in.Tool), nil

	case SecurityToolActionUnban:
		return unbanScript(in.Tool, in.UnbanIP), nil

	case SecurityToolActionAllowlist:
		// Quell-IP wie bei der Einrichtung OHNE sudo lesen - sudo verwirft
		// $SSH_CONNECTION.
		srcIP := server.LCMSourceIP
		if out, _, e := conn.Run(`echo "$SSH_CONNECTION" | awk '{print $1}'`); e == nil {
			if ip := strings.TrimSpace(out); ip != "" {
				srcIP = ip
			}
		}
		extra := append([]string{}, in.AllowlistIPs...)
		if len(in.AllowlistIDs) > 0 && s.ipAllowlistExpand != nil {
			resolved, e := s.ipAllowlistExpand(in.AllowlistIDs)
			if e != nil {
				return "", fmt.Errorf("ip-allowlists aufloesen: %w", e)
			}
			extra = append(resolved, extra...)
		}
		return allowlistUpdateScript(in.Tool, srcIP, extra), nil
	}
	return "", ErrSecurityToolAction
}
