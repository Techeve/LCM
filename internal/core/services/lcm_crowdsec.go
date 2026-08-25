package services

import (
	"fmt"
	"regexp"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// LAPI-Server-Einrichtung auf dem LCM-Host: LCM installiert CrowdSec, öffnet
// die Local API auf allen Adressen, legt ein Maschinen-Konto für die
// verwalteten Server an und trägt dessen Zugangsdaten (URL/Login/Passwort)
// automatisch in die CrowdSec-Einstellungen ein. Danach können Server im
// Remote-Modus (Aktion „Sicherheit-Tools" → CrowdSec) sofort enrollen.

// crowdsecLapiMachine ist der Login des von LCM angelegten Maschinen-Kontos.
const crowdsecLapiMachine = "lcm-managed"

// crowdsecLapiListenPort ist der LAPI-Port (CrowdSec-Standard).
const crowdsecLapiListenPort = 8080

// reLapiPwMarker parst das vom Skript ausgegebene erzeugte Passwort.
var reLapiPwMarker = regexp.MustCompile(`LCM-LAPI-PW:\s*(\S+)`)

// LapiInstallInput bündelt die Optionen der LAPI-Einrichtung.
type LapiInstallInput struct {
	Bouncer bool // lokalen Firewall-Bouncer auf dem LCM-Host mitinstallieren
}

// WithCrowdSecLapiSetter verdrahtet das Eintragen der erzeugten LAPI-Zugangs-
// daten in die globalen Einstellungen (Auto-Verdrahtung nach der Installation).
func (s *ServerService) WithCrowdSecLapiSetter(fn func(url, login, password string) error) *ServerService {
	s.setCrowdSecLapi = fn
	return s
}

// crowdsecLapiInstallScript baut das Einrichtungs-Skript (apt/Debian-Ubuntu,
// LCM-Host ist immer apt). Es installiert CrowdSec, öffnet die LAPI auf
// 0.0.0.0, erzeugt ein Maschinen-Passwort, legt das Maschinen-Konto an,
// optional den lokalen Firewall-Bouncer, startet den Dienst neu und gibt das
// erzeugte Passwort über einen Marker aus (der Job-Runner liest ihn zurück).
func crowdsecLapiInstallScript(bouncer bool) string {
	lines := []string{
		"set -e",
		"export DEBIAN_FRONTEND=noninteractive",
		"echo 'LCM: CrowdSec-LAPI wird eingerichtet …'",
		"apt-get install -y curl gnupg >/dev/null 2>&1 || true",
		"curl -s https://packagecloud.io/install/repositories/crowdsec/crowdsec/script.deb.sh | bash || true",
		"apt-get install -y crowdsec",
		"command -v cscli >/dev/null 2>&1 || { echo 'LCM: CrowdSec-Installation fehlgeschlagen'; exit 1; }",
		// LAPI auf allen Adressen lauschen lassen (config.yaml.local-Override).
		"install -d -m 755 /etc/crowdsec",
		fmt.Sprintf("printf 'api:\\n  server:\\n    listen_uri: 0.0.0.0:%d\\n' > /etc/crowdsec/config.yaml.local", crowdsecLapiListenPort),
		// Maschinen-Passwort erzeugen (openssl, sonst /dev/urandom).
		"PW=$(openssl rand -hex 24 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \\n')",
		// Bestehendes Konto überschreiben (idempotent), Passwort setzen.
		fmt.Sprintf("cscli machines delete %s >/dev/null 2>&1 || true", crowdsecLapiMachine),
		fmt.Sprintf("cscli machines add %s --password \"$PW\" -f /dev/null >/dev/null 2>&1 || cscli machines add %s --password \"$PW\" >/dev/null 2>&1", crowdsecLapiMachine, crowdsecLapiMachine),
	}
	if bouncer {
		lines = append(lines,
			"if command -v nft >/dev/null 2>&1; then CSB=crowdsec-firewall-bouncer-nftables; else CSB=crowdsec-firewall-bouncer-iptables; fi",
			`apt-get install -y "$CSB" || echo 'LCM: Bouncer-Installation fehlgeschlagen (evtl. nicht im Repo)'`,
		)
	}
	lines = append(lines,
		"systemctl enable --now crowdsec >/dev/null 2>&1 || true",
		"systemctl restart crowdsec >/dev/null 2>&1 || true",
		"cscli lapi status 2>/dev/null || true",
		// Marker: der Job-Runner liest das Passwort zurück und trägt die
		// Zugangsdaten in die Einstellungen ein (Auto-Verdrahtung).
		"echo \"LCM-LAPI-PW: $PW\"",
	)
	return strings.Join(lines, "\n")
}

// InstallCrowdSecLapi richtet den CrowdSec-LAPI-Server auf dem LCM-Host ein
// (optional mit Firewall-Bouncer) und trägt die erzeugten Zugangsdaten
// automatisch in die CrowdSec-Einstellungen ein. Nur für den LCM-Host, nur
// auf apt-Systemen. Asynchroner Job.
func (s *ServerService) InstallCrowdSecLapi(scope repositories.AccessScope, id uint, in LapiInstallInput, actor string) (*domain.Job, error) {
	server, err := s.requireLcmHostApt(scope, id)
	if err != nil {
		return nil, err
	}
	job, err := s.jobs.Start(&server.ID, nil, domain.RuleTypeScript, "CrowdSec-LAPI einrichten", actor)
	if err != nil {
		return nil, err
	}
	s.audit.Log(actor, "server.crowdsec-lapi", "server", id, server.Name)
	safego.GoCleanup("job:crowdsec-lapi", jobPanicCleanup(s.jobs, job), func() {
		s.runCrowdSecLapiJob(job, server, in, actor)
	})
	return job, nil
}

func (s *ServerService) runCrowdSecLapiJob(job *domain.Job, server *domain.Server, in LapiInstallInput, actor string) {
	if server.IsDemo {
		s.jobs.Complete(job, "Demo-Server - CrowdSec-LAPI-Einrichtung simuliert (kein SSH-Kontakt).", ptrInt(0), nil)
		return
	}
	conn, err := s.connectRec(server, "crowdsec-lapi", actor)
	if err != nil {
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	defer conn.Close()

	output, code, runErr := conn.Run(privRun(server, crowdsecLapiInstallScript(in.Bouncer)))
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("einrichtung endete mit exit-code %d", code)
	}
	if runErr != nil {
		s.jobs.Complete(job, output, ptrInt(code), runErr)
		return
	}

	// Paketbestand neu erfassen, damit CrowdSec im Inventar erscheint
	// (LcmHostStatus.CrowdSecLapiInstalled).
	rescanPackagesInto(s.servers, conn, server)

	// Auto-Verdrahtung: erzeugtes Passwort aus dem Output lesen und die
	// LAPI-Zugangsdaten in die Einstellungen eintragen. Die URL zeigt auf die
	// erste Nicht-Loopback-IP des LCM-Hosts.
	log := output
	if m := reLapiPwMarker.FindStringSubmatch(output); m != nil && s.setCrowdSecLapi != nil {
		host := firstNonLoopbackIP(server.IPAddresses)
		if host != "" {
			url := fmt.Sprintf("http://%s:%d", bracketIPv6(host), crowdsecLapiListenPort)
			if e := s.setCrowdSecLapi(url, crowdsecLapiMachine, m[1]); e == nil {
				log += "\n\nLCM: LAPI-Zugangsdaten in die Einstellungen eingetragen (" + url + ", Login " + crowdsecLapiMachine + "). Verwaltete Server können jetzt im Remote-Modus enrollen."
			} else {
				log += "\n\nLCM-WARNUNG: LAPI eingerichtet, aber das Eintragen der Zugangsdaten schlug fehl: " + e.Error()
			}
		}
	}
	// Passwort nie im Klartext im Job-Protokoll hinterlassen.
	log = reLapiPwMarker.ReplaceAllString(log, "LCM-LAPI-PW: ********")
	s.jobs.Complete(job, log, ptrInt(0), nil)
}
