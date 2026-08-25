package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// ErrFirewallNeedsFullSudo: die Backends firewalld und nftables brauchen
// systemctl/rc-update und Datei-Schreibzugriffe unter /etc - das gibt die
// sudo-Whitelist des eingeschränkten Modus bewusst nicht her. Nur ufw ist
// dort verwaltbar (ufw steht auf der Whitelist).
var ErrFirewallNeedsFullSudo = errors.New("dieses Firewall-Backend braucht volle Sudo-Rechte - im eingeschränkten Modus ist nur ufw verwaltbar")

// ErrInvalidFirewallRules kennzeichnet Validierungsfehler der Regel-Eingabe
// (Portbereich, Protokoll, Bind-Adresse, IP-Version) für das 400-Mapping.
var ErrInvalidFirewallRules = errors.New("ungültige Firewall-Regeln")

// ConfigureFirewall aktiviert oder deaktiviert die Firewall eines Servers -
// als asynchroner Job, denn je nach Distribution muss das Firewall-Werkzeug
// (ufw/firewalld/nftables) erst installiert werden. rulesSpec ist ein
// JSON-Array von FirewallRule oder die Legacy-CSV-Portliste; der SSH-Port
// wird IMMER zusätzlich freigegeben. Ist bereits ein anderes als das für die
// Distribution vorgesehene Werkzeug installiert, wird DIESES verwendet -
// LCM installiert nie eine zweite Firewall neben einer vorhandenen.
func (s *ServerService) ConfigureFirewall(scope repositories.AccessScope, id uint, enable bool, rulesSpec string, sshSources domain.FirewallSSHSources, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	// Proxmox bringt seine eigene Firewall (pve-firewall) mit - ein zweiter
	// Paketfilter würde damit kollidieren und kann Cluster-/Gast-Traffic kappen.
	if server.IsProxmox() {
		return nil, ErrProxmoxRestricted
	}
	rules, err := parseFirewallRuleSpec(rulesSpec)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFirewallRules, err)
	}
	// Zur KONFIGURATIONSZEIT streng: Der Anwender sitzt davor und kann es
	// beheben. Eine unbekannte Allowlist-ID oder eine Einschränkung, die zu
	// 0 IPs auflöst, wird abgewiesen - früher lief das mit „success" durch
	// und die Regel verschwand wortlos vom Zielsystem (R2-071). Zur
	// ANWENDUNGSZEIT (Gruppen-Regeln, gespeicherter Stand) bleibt es beim
	// fail-closed mit lauter Warnung - dort kann niemand sofort eingreifen.
	if enable {
		if _, warnings, verr := expandRuleAllowlists(rules, s.ipAllowlistExpand); verr != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidFirewallRules, verr)
		} else if len(warnings) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidFirewallRules, strings.Join(warnings, " · "))
		}
	}
	// Quell-Einschränkung der SSH-Freigabe: dieselbe Prüfung wie bei jeder
	// anderen Regel. Wichtig ist die Auflösung - löst die Einschränkung zu
	// KEINER Adresse auf, spräche das Ergebnis eine Aussperrung aus, und die
	// Regel würde vom Zielsystem verschwinden.
	cleanSources, err := normalizeSourceIPs(sshSources.SourceIPs)
	if err != nil {
		return nil, fmt.Errorf("%w: SSH-Quellen: %v", ErrInvalidFirewallRules, err)
	}
	sshSources.SourceIPs = cleanSources
	server.FirewallSSHSources = sshSourcesJSON(sshSources)
	if enable {
		sshRule := serverSSHRule(server)
		if len(sshRule.AllowlistIDs) > 0 || len(sshRule.SourceIPs) > 0 {
			expanded, warnings, verr := expandRuleAllowlists([]domain.FirewallRule{sshRule}, s.ipAllowlistExpand)
			if verr != nil {
				return nil, fmt.Errorf("%w: SSH-Quellen: %v", ErrInvalidFirewallRules, verr)
			}
			if len(warnings) > 0 || len(expanded) == 0 || len(expanded[0].Sources) == 0 {
				return nil, fmt.Errorf("%w: die Quell-Einschränkung der SSH-Freigabe löst zu keiner Adresse auf - "+
					"das würde dich vom Server aussperren", ErrInvalidFirewallRules)
			}
		}
	}
	if server.RestrictedSudo && firewallBackendFor(server) != domain.FirewallToolUfw {
		return nil, ErrFirewallNeedsFullSudo
	}
	label := "Firewall deaktivieren @ " + server.Name
	if enable {
		label = "Firewall anwenden @ " + server.Name
	}
	job, err := s.jobs.Start(&server.ID, nil, domain.RuleTypeFirewall, label, actor)
	if err != nil {
		return nil, err
	}
	verb := "deaktivieren"
	if enable {
		// Der SSH-Port ist immer freigegeben - mit dem echten Port des
		// Servers protokollieren, nicht hartkodiert 22.
		sshPort := server.SSHPort
		if sshPort == 0 {
			sshPort = 22
		}
		verb = "anwenden (Ports: " + strings.TrimSuffix(fmt.Sprintf("%d,", sshPort)+firewallRulesPortsCSV(rules), ",") + ")"
	}
	s.audit.Log(actor, "server.firewall", "server", id, server.Name+": "+verb)
	safego.GoCleanup("job:firewall", jobPanicCleanup(s.jobs, job), func() {
		s.runFirewallJob(job, server, enable, rules, actor)
	})
	return job, nil
}

// runFirewallJob führt die Firewall-Aktion aus: Backend bestimmen (erkanntes
// Werkzeug gewinnt), bei Bedarf installieren, Regeln deklarativ anwenden,
// Ergebnis VERIFIZIEREN und persistieren.
func (s *ServerService) runFirewallJob(job *domain.Job, server *domain.Server, enable bool, rules []domain.FirewallRule, actor string) {
	if server.IsDemo {
		// Demo-Server werden nie per SSH kontaktiert - Zustand nur simulieren,
		// damit die UI das Ergebnis zeigt.
		fields := map[string]any{"firewall_active": enable}
		if enable {
			fields["firewall_tool"] = firewallDesignatedBackend(server.OSID)
			fields["firewall_rules"] = firewallRulesJSON(rules)
			fields["firewall_allowed_ports"] = firewallRulesPortsCSV(rules)
			fields["firewall_ssh_sources"] = server.FirewallSSHSources
		}
		_ = s.servers.UpdateFields(server.ID, fields)
		s.jobs.Complete(job, "Demo-Server - Firewall-Konfiguration simuliert (kein SSH-Kontakt).", ptrInt(0), nil)
		return
	}
	conn, err := s.connectRec(server, "firewall", actor)
	if err != nil {
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	defer conn.Close()

	if !enable {
		s.runFirewallDisable(job, conn, server)
		return
	}

	// Benannte Allowlists in konkrete Quell-CIDRs auflösen (persistiert werden
	// die Referenzen, angewendet die aufgelösten Quellen).
	ssh, applied, warnings, err := expandSSHAndRules(serverSSHRule(server), rules, s.ipAllowlistExpand)
	if err != nil {
		s.jobs.Complete(job, "", nil, fmt.Errorf("allowlists auflösen: %w", err))
		return
	}
	output, backend, err := applyFirewallRules(conn, server, ssh, applied, true)
	if len(warnings) > 0 {
		output += "\n\n" + strings.Join(warnings, "\n")
	}
	if err != nil {
		if strings.Contains(output, firewallToolMissingPrefix) {
			_ = s.servers.UpdateFields(server.ID, map[string]any{"firewall_active": false})
		}
		s.jobs.Complete(job, output, nil, err)
		return
	}
	_ = s.servers.UpdateFields(server.ID, map[string]any{
		"reachable":       true,
		"failed_checks":   0,    // erfolgreicher Kontakt
		"firewall_active": true, // durch applyFirewallRules verifiziert
		"firewall_tool":   backend,
		"firewall_rules":  firewallRulesJSON(rules),
		// Die Portliste gibt den TATSÄCHLICH angewandten Stand wieder -
		// übersprungene Regeln (Quell-Einschränkung löst zu 0 IPs auf)
		// zählen nicht als offen (R2-071: Anzeige behauptete Ports, die es
		// auf dem System nie gab).
		"firewall_allowed_ports": firewallRulesPortsCSV(applied),
		"firewall_ssh_sources":   server.FirewallSSHSources,
	})
	s.jobs.Complete(job, output, ptrInt(0), nil)
}

// runFirewallDisable deaktiviert die erkannte Firewall. Die Regel-
// Konfiguration bleibt erhalten (erneutes Aktivieren stellt sie wieder her).
func (s *ServerService) runFirewallDisable(job *domain.Job, conn sshx.Conn, server *domain.Server) {
	detected := ""
	if out, code, err := conn.Run(privRun(server, firewallDetectCmd)); err == nil && code == 0 {
		detected = parseFirewallDetect(out)
	}
	backend := detected
	if backend == "" {
		backend = firewallBackendFor(server)
	}
	be := firewallBackendByName(backend)
	output, code, runErr := conn.Run(privRun(server, be.disableScript()))
	if strings.Contains(output, firewallToolMissingPrefix) {
		// Kein Werkzeug installiert → es gibt nichts zu deaktivieren; der
		// Zustand ist faktisch "aus", der Fehler bleibt sichtbar (BUG-024).
		_ = s.servers.UpdateFields(server.ID, map[string]any{"firewall_active": false})
		s.jobs.Complete(job, output, ptrInt(code), ErrFirewallToolMissing)
		return
	}
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("firewall deaktivieren endete mit exit-code %d", code)
	}
	if runErr != nil {
		s.jobs.Complete(job, output, ptrInt(code), runErr)
		return
	}
	_ = s.servers.UpdateFields(server.ID, map[string]any{"reachable": true, "failed_checks": 0, "firewall_active": false, "firewall_tool": backend})
	s.jobs.Complete(job, output, ptrInt(0), nil)
}

// applyFirewallRules ist der gemeinsame Kern von Aktion und Gruppen-Regel:
//  1. installiertes Werkzeug erkennen (Konfliktprüfung - nie ein zweites
//     neben einem vorhandenen installieren),
//  2. fehlt eines und installIfMissing ist gesetzt: das vorgesehene Werkzeug
//     der Distribution installieren,
//  3. Regelsatz deklarativ anwenden (SSH-Port immer offen),
//  4. VERIFIZIEREN, dass die Firewall danach wirklich aktiv ist.
//
// Rückgabe: gesammelter Output, verwendetes Backend, Fehler.
func applyFirewallRules(conn sshx.Conn, server *domain.Server, ssh domain.FirewallRule, rules []domain.FirewallRule, installIfMissing bool) (string, string, error) {
	var log strings.Builder

	detected := ""
	if out, code, err := conn.Run(privRun(server, firewallDetectCmd)); err == nil && code == 0 {
		detected = parseFirewallDetect(out)
	}
	backend, installScript := resolveFirewallBackend(server, detected)
	if designated := firewallDesignatedBackend(server.OSID); detected != "" && detected != designated {
		fmt.Fprintf(&log, "Hinweis: %s ist bereits installiert - es wird %s statt %s verwendet (LCM installiert nie eine zweite Firewall).\n",
			detected, detected, designated)
	}
	if installScript != "" {
		if !installIfMissing {
			return log.String() + firewallToolMissingMarker(backend), backend, ErrFirewallToolMissing
		}
		fmt.Fprintf(&log, "Firewall-Werkzeug fehlt - %s wird installiert.\n", backend)
		out, code, err := conn.Run(privRun(server, installScript))
		log.WriteString(out + "\n")
		if err == nil && (code != 0 || strings.Contains(out, firewallInstallFailedMarker)) {
			err = fmt.Errorf("installation von %s fehlgeschlagen", backend)
		}
		if err != nil {
			return log.String(), backend, err
		}
	}

	be := firewallBackendByName(backend)
	out, code, err := conn.Run(privRun(server, be.enableScript(ssh, rules)))
	log.WriteString(out)
	if strings.Contains(out, firewallToolMissingPrefix) {
		return log.String(), backend, ErrFirewallToolMissing
	}
	if err == nil && code != 0 {
		err = fmt.Errorf("firewall-kommando endete mit exit-code %d", code)
	}
	if err != nil {
		return log.String(), backend, err
	}

	// Verifikation nach der zustandsändernden Aktion: der Status muss die
	// Firewall jetzt als aktiv zeigen - sonst ehrlicher Fehler statt "ok".
	statusOut, statusCode, statusErr := conn.Run(privRun(server, be.statusCmd))
	if statusErr != nil || statusCode != 0 || !be.activeFrom(statusOut) {
		log.WriteString("\n### Verifikation\n" + statusOut)
		return log.String(), backend, fmt.Errorf("verifikation fehlgeschlagen - %s meldet die firewall nach der anwendung nicht als aktiv", backend)
	}
	return log.String(), backend, nil
}
