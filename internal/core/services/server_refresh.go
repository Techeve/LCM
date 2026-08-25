package services

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// Job-Typen der reinen Daten-Neuerfassung (kein Upgrade, nur auslesen).
const (
	JobTypeHardwareRefresh = "hardware-refresh"
	JobTypeFullRefresh     = "full-refresh"
)

// RefreshHardware liest Hardware-/OS-Fakten eines Servers neu ein (CPU, RAM,
// Festplatte, Kernel, Virtualisierung, IPs, Paketverwaltung, Docker-Flags) -
// OHNE Paket-/Docker-Bestände zu ersetzen und ohne etwas zu installieren.
func (s *ServerService) RefreshHardware(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	job, err := s.jobs.Start(&server.ID, nil, JobTypeHardwareRefresh, "Hardware aktualisieren @ "+server.Name, actor)
	if err != nil {
		return nil, err // u.a. ErrServerBusy → 409
	}
	s.audit.Log(actor, "server.refresh-hardware", "server", id, "")
	safego.GoCleanup("job:hardware-refresh", jobPanicCleanup(s.jobs, job), func() {
		s.runRefreshJob(job, server, actor, false)
	})
	return job, nil
}

// RefreshAll liest ALLE erfassbaren Daten neu ein: Hardware, Pakete, Snaps,
// Repos, Docker-Inventar, einen Speicher-Snapshot sowie den Firewall- und
// SSH-Härtungs-Status (live vom Server). Reine Datenerfassung, kein Upgrade.
func (s *ServerService) RefreshAll(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	job, err := s.jobs.Start(&server.ID, nil, JobTypeFullRefresh, "Alles aktualisieren @ "+server.Name, actor)
	if err != nil {
		return nil, err
	}
	s.audit.Log(actor, "server.refresh-all", "server", id, "")
	safego.GoCleanup("job:full-refresh", jobPanicCleanup(s.jobs, job), func() {
		s.runRefreshJob(job, server, actor, true)
	})
	return job, nil
}

// runRefreshJob führt den Scan über eine frische Verbindung aus und schreibt
// die Ergebnisse zurück. full=false: nur Hardware-Felder; full=true:
// zusätzlich Paket-/Snap-/Repo-/Docker-Bestände, Speicher-Sample und der
// Live-Status von Firewall/SSH-Härtung.
func (s *ServerService) runRefreshJob(job *domain.Job, server *domain.Server, actor string, full bool) {
	if server.IsDemo {
		s.jobs.Complete(job, "demo-server: datenerfassung simuliert (kein ssh-kontakt)", ptrInt(0), nil)
		return
	}
	// Synology DSM: eigener Erfassungspfad über die DSM-Web-API - es gibt
	// hier keine SSH-Verbindung und keine Shell (siehe dsm_service.go).
	if server.IsSynologyDSM() {
		out, err := s.refreshDSM(server)
		if err != nil {
			_ = s.servers.UpdateFields(server.ID, unreachableFields(server, err))
			s.jobs.Complete(job, "", nil, err)
			return
		}
		s.jobs.Complete(job, out, ptrInt(0), nil)
		return
	}
	conn, err := s.connect(server)
	if err != nil {
		_ = s.servers.UpdateFields(server.ID, unreachableFields(server, err))
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = s.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: actor,
		Purpose: "refresh", Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	// MikroTik RouterOS: eigener Scan-Pfad (RouterOS-CLI statt POSIX-Shell,
	// keine Pakete/Docker/Firewall/Härtung). LCM erfasst nur Identität und
	// Versions-Aktualität.
	if server.IsRouterOS() {
		s.runRouterOSRefresh(job, server, conn)
		return
	}

	scan := scanServerMode(conn, server.ServiceUser, server.RestrictedSudo)
	fresh := *server
	applyScan(&fresh, scan)
	fields := map[string]any{
		"os_name": fresh.OSName, "os_version": fresh.OSVersion,
		"os_id": fresh.OSID, "os_version_id": fresh.OSVersionID,
		"proxmox_type": fresh.ProxmoxType, "proxmox_version": fresh.ProxmoxVersion,
		"virtualization": fresh.Virtualization, "package_manager": fresh.PackageManager,
		"has_snap": fresh.HasSnap, "has_docker": fresh.HasDocker, "has_compose": fresh.HasCompose,
		"has_acl": fresh.HasACL, "acl_usable": fresh.ACLUsable,
		"reboot_required": fresh.RebootRequired, "listening_packages": fresh.ListeningPackages,
		"kernel_version": fresh.KernelVersion, "installed_kernels": fresh.InstalledKernels,
		"cpu_model": fresh.CPUModel, "cpu_cores": fresh.CPUCores,
		"mem_total_mb": fresh.MemTotalMB, "mem_used_mb": fresh.MemUsedMB,
		"disk_total_mb": fresh.DiskTotalMB, "disk_used_mb": fresh.DiskUsedMB,
		"ip_addresses":       fresh.IPAddresses,
		"fail2ban_installed": fresh.Fail2banInstalled, "crowdsec_installed": fresh.CrowdSecInstalled,
		"ssh_2fa_enabled": fresh.SSH2FAEnabled,
		"firewall_tool":   fresh.FirewallTool, "listening_ports": fresh.ListeningPorts,
		// Zeit-Zustand gehört zur Grunderfassung: eine falsch gehende Uhr
		// fällt sonst nirgends auf, verdirbt aber TLS-Prüfungen, die
		// Protokoll-Reihenfolge über mehrere Server und signierte
		// Paket-Metadaten.
		"timezone": fresh.Timezone, "ntp_service": fresh.NTPService,
		"ntp_synchronized": fresh.NTPSynchronized, "ntp_servers": fresh.NTPServers,
		"clock_offset_seconds": fresh.ClockOffsetSeconds, "time_checked_at": time.Now(),
		"reachable": true, "last_seen_at": time.Now(), "last_error": "", "failed_checks": 0,
	}
	// Quell-IP nur überschreiben, wenn erkannt (leer = alten Wert behalten).
	if fresh.LCMSourceIP != "" {
		fields["lcm_source_ip"] = fresh.LCMSourceIP
	}
	// DNS gehört zur Grunderfassung: aktive Resolver + Auflösungstest der
	// gepflegten Test-Domains - rein lesend und sudo-frei, daher auch beim
	// reinen Hardware-Scan enthalten (nicht nur über die Aktion "DNS-Test").
	dnsFields := dnsScanFields(conn, sanitizeDNSDomains(s.dnsTestDomainList()))
	for k, v := range dnsFields {
		fields[k] = v
	}

	var log strings.Builder
	if full {
		_ = s.servers.ReplacePackages(server.ID, scan.Packages)
		_ = s.servers.ReplaceSnapPackages(server.ID, scan.Snaps)
		_ = s.servers.ReplaceRepositories(server.ID, scan.Repositories)
		s.updateHTTPSRevertURLs(conn, server, scan.Repositories)
		s.rescanApps(conn, server)
		_ = s.servers.ReplaceDiskVolumes(server.ID, scan.DiskVolumes)
		_ = s.servers.ReplaceServerUsers(server.ID, scan.Users)
		_ = s.servers.ReplaceServerUserLogins(server.ID, scan.UserLogins)
		_ = s.servers.ReplaceDockerContainers(server.ID, scan.DockerContainers)
		_ = s.servers.ReplaceDockerImages(server.ID, scan.DockerImages)
		// Speicher-Snapshot direkt festhalten (manuelle Aktion, nicht gedrosselt).
		if fresh.DiskTotalMB > 0 {
			now := time.Now()
			_ = s.servers.RecordStorageSample(server.ID, now.Format("2006-01-02"), fresh.DiskTotalMB, fresh.DiskUsedMB, now)
		}
		// Firewall- und SSH-Härtungs-Status live auslesen (read-only).
		for k, v := range readServerLiveStatus(conn, &fresh) {
			fields[k] = v
		}
		fmt.Fprintf(&log, "Alles aktualisiert: %d Pakete, %d Snaps, %d Container, %d Images, Disk %d%%\n",
			len(scan.Packages), len(scan.Snaps), len(scan.DockerContainers), len(scan.DockerImages), fresh.DiskUsagePercent())
	} else {
		fmt.Fprintf(&log, "Hardware aktualisiert: %s %s, CPU %s (%d Kerne), RAM %d MB, Disk %d%%\n",
			fresh.OSName, fresh.OSVersion, fresh.CPUModel, fresh.CPUCores, fresh.MemTotalMB, fresh.DiskUsagePercent())
	}
	_ = s.servers.UpdateFields(server.ID, fields)
	log.WriteString("\n### Scan-Ausgabe\n" + scan.Output)
	s.jobs.Complete(job, log.String(), ptrInt(0), nil)
}

// runRouterOSRefresh erfasst Identität und Versions-Aktualität eines
// MikroTik-RouterOS-Geräts über die RouterOS-CLI. Es gibt keine Pakete,
// Docker, Firewall- oder Härtungs-Erhebung - diese Bereiche sind auf RouterOS
// nicht anwendbar.
func (s *ServerService) runRouterOSRefresh(job *domain.Job, server *domain.Server, conn sshx.Conn) {
	scan := scanRouterOS(conn)
	fresh := *server
	applyScan(&fresh, scan)
	fields := map[string]any{
		"os_name": fresh.OSName, "os_version": fresh.OSVersion, "os_id": fresh.OSID,
		"kernel_version": fresh.KernelVersion, "cpu_model": fresh.CPUModel, "cpu_cores": fresh.CPUCores,
		"mem_total_mb": fresh.MemTotalMB, "mem_used_mb": fresh.MemUsedMB,
		"disk_total_mb": fresh.DiskTotalMB, "disk_used_mb": fresh.DiskUsedMB,
		"routerboard_model": fresh.RouterBoardModel, "routeros_channel": fresh.RouterOSChannel,
		"routeros_latest_version": fresh.RouterOSLatestVersion, "routeros_update_available": fresh.RouterOSUpdateAvailable,
		"reachable": true, "last_seen_at": time.Now(), "last_error": "", "failed_checks": 0,
	}
	_ = s.servers.UpdateFields(server.ID, fields)

	status := "aktuell"
	if fresh.RouterOSUpdateAvailable {
		status = "Update verfügbar: " + fresh.RouterOSLatestVersion
	}
	log := fmt.Sprintf("RouterOS aktualisiert: %s %s (%s), Board %s - %s\n\n### Scan-Ausgabe\n%s",
		fresh.OSName, fresh.OSVersion, fresh.RouterOSChannel, fresh.RouterBoardModel, status, scan.Output)
	s.jobs.Complete(job, log, ptrInt(0), nil)
}

// readServerLiveStatus liest den tatsächlichen Firewall- und SSH-Härtungs-
// Status vom Server (rein lesend) und liefert die zu aktualisierenden Felder.
// Fehler einzelner Prüfungen werden übersprungen (best effort) - so bleibt der
// gespeicherte Wert erhalten, statt fälschlich zurückgesetzt zu werden.
func readServerLiveStatus(conn sshx.Conn, server *domain.Server) map[string]any {
	fields := map[string]any{}

	// Firewall: installiertes Werkzeug erkennen (ufw/firewalld/nftables) und
	// den Aktiv-Status über das jeweilige Backend lesen. Ohne erkanntes
	// Werkzeug bleibt firewall_active unangetastet (best effort - z.B. im
	// eingeschränkten Modus ist die nft-Tabellen-Prüfung nicht möglich).
	if toolOut, code, err := conn.Run(privRun(server, firewallDetectCmd)); err == nil && code == 0 {
		tool := parseFirewallDetect(toolOut)
		fields["firewall_tool"] = tool
		if tool == "" {
			// Kein Firewall-Werkzeug (mehr) vorhanden: Aktiv-Flag und
			// Portliste zurücksetzen, statt den letzten Stand zu behalten
			// (R2-013).
			fields["firewall_active"] = false
			fields["firewall_allowed_ports"] = ""
		}
		if tool != "" {
			be := firewallBackendByName(tool)
			if out, c, e := conn.Run(privRun(server, be.statusCmd)); e == nil && c == 0 {
				active := be.activeFrom(out)
				fields["firewall_active"] = active
				// Die Portliste beschreibt, was GERADE freigegeben ist. Ist
				// keine Firewall aktiv, lautet die Antwort „nichts" - nicht
				// „der Stand von neulich". Das gilt unabhängig vom Werkzeug:
				// nach `apt-get purge ufw` erkennt LCM auf Debian das
				// vorinstallierte nftables, der ufw-Zweig unten läuft dann
				// nicht mehr, und die alten ufw-Ports blieben ohne diese
				// Zeile für immer stehen (R2-013, zweite Hälfte).
				if !active {
					fields["firewall_allowed_ports"] = ""
				}
				// Nur ufw: die Legacy-Portliste aus dem Status rekonstruieren
				// (Freigaben ohne den immer offenen SSH-Port, als CSV). Ist ufw
				// nicht (mehr) aktiv, wird die Liste GELEERT - sonst zeigte LCM
				// nach einer ufw-Deinstallation weiter die alten Ports an
				// (R2-013).
				if tool == domain.FirewallToolUfw {
					var extra []string
					if active {
						for p := range ufwPortsFromStatus(out) {
							if p != strconv.Itoa(server.SSHPort) {
								extra = append(extra, p)
							}
						}
						sort.Slice(extra, func(i, j int) bool {
							a, _ := strconv.Atoi(extra[i])
							b, _ := strconv.Atoi(extra[j])
							return a < b
						})
					}
					fields["firewall_allowed_ports"] = strings.Join(extra, ",")
				}
			}
		}
	}

	// SSH-Härtung: LCM setzt sie über das Drop-in
	// /etc/ssh/sshd_config.d/60-lcm-hardening.conf - dessen Vorhandensein ist
	// das maßgebliche Signal.
	if out, _, err := conn.Run(privRun(server,
		"test -f /etc/ssh/sshd_config.d/60-lcm-hardening.conf && echo yes || echo no")); err == nil {
		fields["ssh_hardened"] = strings.Contains(firstLine(out), "yes")
	}

	// Stand des LCM-Helpers. Nur im eingeschränkten Modus relevant - dort ist
	// er der einzige Weg zu den privilegierten Aktionen, wird aber seit dem
	// Einschränken nicht mehr erneuert (B17). Ein leerer Wert heißt „nicht
	// ermittelbar" und damit: veraltet, denn das Unterkommando gibt es erst
	// seit dieser Fassung.
	if server.RestrictedSudo {
		version := ""
		if out, _, err := conn.Run(privRun(server, helperCmd("version"))); err == nil {
			version = parseHelperVersion(out)
		}
		fields["helper_version"] = version
	}

	// Registrierung bei Red Hat. Nur dort, wo es subscription-manager
	// überhaupt gibt - auf allem anderen bleibt das Feld leer und der Befund
	// aus. Der Aufruf braucht Root; im eingeschränkten Modus bleibt als
	// Ersatz das Consumer-Zertifikat, das nur bei Registrierung existiert.
	if out, _, err := conn.Run(privRun(server, rhsmStatusScript)); err == nil {
		fields["rhsm_status"] = parseRHSMStatus(out)
	}

	// APT-Cache-Anbindung: analog über das LCM-Drop-in unter /etc/apt.
	if out, _, err := conn.Run(privRun(server, aptProxyStatusCommand())); err == nil {
		fields["apt_proxy_active"] = strings.Contains(firstLine(out), "yes")
	}

	// Sicherheits-Tools aktiv? (nur wenn im Scan als vorhanden erkannt.)
	// WICHTIG: is-active druckt bei gestopptem Dienst "inactive" auf stdout -
	// deshalb nur der Exit-Code zählen (--quiet) und exakt vergleichen; ein
	// Contains("active") würde auch "inactive" matchen.
	if server.Fail2banInstalled {
		if out, _, err := conn.Run(privRun(server, serviceActiveCommand("fail2ban"))); err == nil {
			fields["fail2ban_active"] = firstLine(out) == "active"
		}
	} else {
		// Nicht (mehr) installiert: Aktiv-Flag zurücksetzen, sonst blieb es
		// nach einer Deinstallation dauerhaft true stehen (R2-080).
		fields["fail2ban_active"] = false
	}
	if server.CrowdSecInstalled {
		if out, _, err := conn.Run(privRun(server, serviceActiveCommand("crowdsec"))); err == nil {
			fields["crowdsec_active"] = firstLine(out) == "active"
		}
		// An welche LAPI meldet der Agent tatsächlich? (Credentials-Datei -
		// speist die „Angebundene Server"-Liste der CrowdSec-Seite.)
		for k, v := range crowdsecLapiURLFields(conn, server) {
			fields[k] = v
		}
	} else {
		// Nicht (mehr) installiert: Aktiv-Flag zurücksetzen (R2-080).
		fields["crowdsec_active"] = false
	}

	return fields
}
