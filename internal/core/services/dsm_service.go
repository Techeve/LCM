package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/synology"
	"LCM/internal/storage/repositories"
)

// Synology DSM: Onboarding und Erfassung über die DSM-Web-API.
//
// DSM ist der zweite reine API-Gerätetyp neben MikroTik RouterOS - kein
// Linux-Server im Sinne von LCM: kein /etc/os-release, ein alter
// Synology-Kernel (der CVE-Scan liefe in Falschalarme), synopkg statt apt,
// und Benutzer/Dienste verwaltet DSM selbst. Statt einen Service-User mit
// sudo neben DSMs eigene Konfigurationsverwaltung zu setzen, spricht LCM die
// dokumentierte Web-API und überwacht: DSM-Version und verfügbare Updates,
// installierte Pakete, Volumes/Belegung, Zeit/NTP und den Security Advisor.

// ErrDSMUnsupported: die Aktion setzt eine POSIX-Shell oder Paketverwaltung
// voraus, die es auf DSM nicht gibt.
var ErrDSMUnsupported = errors.New("für Synology DSM nicht verfügbar - LCM überwacht dieses Gerät über die DSM-Web-API (keine Shell/Paketverwaltung für Firewall, CVE-Scan, Repos oder Benutzer)")

// DSMRequest bündelt die Eingaben des DSM-Onboarding-Formulars.
type DSMRequest struct {
	Name string
	Host string
	Port int
	// Account/Password sind die Zugangsdaten eines DSM-Kontos in der
	// Administratorgruppe. Empfehlung (Doku): ein eigenes Konto nur für LCM,
	// ohne erzwungene 2FA - ein unbeaufsichtigter Scan kann keinen OTP-Code
	// liefern; abgesichert über DSMs IP-Beschränkung auf den LCM-Host.
	Account  string
	Password string
	// ConfirmedFingerprint ist der in der Oberfläche bestätigte
	// SHA-256-Fingerprint des TLS-Zertifikats (Trust-on-First-Use wie beim
	// SSH-Host-Key). Leer = LCM übernimmt den vorgefundenen Fingerprint.
	ConfirmedFingerprint string
	Actor                string
}

// ProbeDSM liest den Zertifikats-Fingerprint eines DSM-Geräts - für die
// Bestätigung im Onboarding-Dialog, bevor Zugangsdaten übertragen werden.
func (s *ServerService) ProbeDSM(host string, port int) (string, error) {
	return synology.ProbeFingerprint(strings.TrimSpace(host), port)
}

// CreateDSMServer legt ein Synology-DSM-Gerät an (OSID=dsm): Zertifikat
// prüfen, anmelden, Zustand erheben, online anlegen.
func (s *ServerService) CreateDSMServer(req DSMRequest) (*domain.Server, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.Account = strings.TrimSpace(req.Account)
	if req.Name == "" || req.Host == "" || req.Account == "" || req.Password == "" {
		return nil, errors.New("name, host, konto und passwort sind erforderlich")
	}
	if req.Port < 0 || req.Port > 65535 {
		return nil, errors.New("ungültiger port: erwartet wird 1-65535")
	}
	if req.Port == 0 {
		req.Port = synology.DefaultPort
	}
	if _, err := s.servers.FindByName(req.Name); err == nil {
		return nil, ErrServerNameTaken
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, err
	}
	// Host-Eindeutigkeit über den DSM-Port: dasselbe Gerät soll nicht zweimal
	// im Bestand stehen.
	if taken, err := s.servers.HostExists(req.Host, req.Port, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrServerHostTaken
	}

	// Zertifikat: vorgefundenen Fingerprint holen und - falls der Admin einen
	// bestätigt hat - abgleichen. Danach ist er gepinnt.
	fp, err := synology.ProbeFingerprint(req.Host, req.Port)
	if err != nil {
		return nil, fmt.Errorf("DSM nicht erreichbar: %w", err)
	}
	if req.ConfirmedFingerprint != "" &&
		!strings.EqualFold(strings.ReplaceAll(req.ConfirmedFingerprint, ":", ""), fp) {
		return nil, ErrFingerprintMismatch
	}

	client := synology.New(synology.Config{Host: req.Host, Port: req.Port, CertFingerprint: fp})
	if err := client.Login(req.Account, req.Password); err != nil {
		return nil, err
	}
	defer client.Logout()
	info, err := client.Collect()
	if err != nil {
		return nil, fmt.Errorf("DSM-Zustand lesen: %w", err)
	}

	enc, err := s.cipher.EncryptString(req.Password)
	if err != nil {
		return nil, fmt.Errorf("passwort verschlüsseln: %w", err)
	}
	now := time.Now()
	server := &domain.Server{
		Name: req.Name, Host: req.Host,
		// SSHPort trägt den DSM-Port mit: er hält die Host-Eindeutigkeit und
		// die UI zeigt „Host:Port" überall gleich. DSMPort ist die
		// ausdrückliche, gleichlautende Angabe für den API-Client.
		SSHPort:          req.Port,
		DSMPort:          req.Port,
		ServiceUser:      req.Account,
		LoginPasswordEnc: enc,
		OSID:             domain.OSIDSynologyDSM,
		OSName:           "Synology DSM",
		Transport:        domain.TransportSSH,
		Reachable:        true,
		LastSeenAt:       &now,
	}
	server.DSMCertFingerprint = fp
	applyDSMInfo(server, info)
	if err := s.servers.Create(server); err != nil {
		return nil, err
	}
	s.assignToSystemGroup(server)
	s.replaceDSMPackages(server.ID, info)
	s.recordDSMJoin(server, req.Actor,
		fmt.Sprintf("DSM erkannt: %s auf %s (Seriennummer %s), %d Paket(e).",
			info.Version, info.Model, info.Serial, len(info.Packages)))
	return server, nil
}

// applyDSMInfo überträgt den erhobenen Zustand auf den Server-Datensatz.
// Bewusst dieselben Felder wie bei Linux-Servern, damit Dashboard, Ampel,
// Speicher-Verlauf und Zeit-Sicht ohne Sonderfälle funktionieren.
func applyDSMInfo(server *domain.Server, info *synology.Info) {
	server.OSVersion = info.Version
	server.DSMModel = info.Model
	server.DSMLatestVersion = info.LatestVersion
	server.DSMUpdateAvailable = info.UpdateAvailable
	server.DSMSecurityRisks = info.SecurityRisks
	server.DSMSecuritySummary = info.SecuritySummary
	server.CPUCores = info.CPUCores
	server.MemTotalMB = int64(info.RAMSizeMB)
	server.DiskTotalMB = int64(info.VolumeTotalMB)
	server.DiskUsedMB = int64(info.VolumeUsedMB)
	// Zeit-Sicht: DSM meldet Zeitzone und NTP-Zustand selbst. Der Zeitversatz
	// bleibt bewusst 0 - DSM liefert die Uhrzeit nur als lokale Zeichenkette
	// ohne Zonen-Offset, eine daraus errechnete Differenz wäre geraten.
	server.Timezone = info.Timezone
	server.NTPSynchronized = info.NTPEnabled
	server.NTPServers = info.NTPServer
	if info.NTPEnabled {
		server.NTPService = "dsm"
	} else {
		server.NTPService = ""
	}
}

// dsmFields sind die per Refresh aktualisierten Spalten.
func dsmFields(server *domain.Server) map[string]any {
	now := time.Now()
	return map[string]any{
		"os_version": server.OSVersion, "dsm_model": server.DSMModel,
		"dsm_latest_version": server.DSMLatestVersion, "dsm_update_available": server.DSMUpdateAvailable,
		"dsm_security_risks": server.DSMSecurityRisks, "dsm_security_summary": server.DSMSecuritySummary,
		"cpu_cores": server.CPUCores, "mem_total_mb": server.MemTotalMB,
		"disk_total_mb": server.DiskTotalMB, "disk_used_mb": server.DiskUsedMB,
		"timezone": server.Timezone, "ntp_service": server.NTPService,
		"ntp_synchronized": server.NTPSynchronized, "ntp_servers": server.NTPServers,
		"time_checked_at": now,
		"reachable":       true, "last_seen_at": now, "last_error": "", "failed_checks": 0,
	}
}

// dsmClient baut den API-Client eines Geräts aus den gespeicherten Daten.
func (s *ServerService) dsmClient(server *domain.Server) (*synology.Client, error) {
	password, err := s.cipher.DecryptString(server.LoginPasswordEnc)
	if err != nil {
		return nil, fmt.Errorf("passwort entschlüsseln: %w", err)
	}
	port := server.DSMPort
	if port <= 0 {
		port = server.SSHPort
	}
	client := synology.New(synology.Config{
		Host: server.Host, Port: port, CertFingerprint: server.DSMCertFingerprint,
	})
	if err := client.Login(server.ServiceUser, password); err != nil {
		return nil, err
	}
	return client, nil
}

// refreshDSM erhebt den Zustand eines DSM-Geräts neu und schreibt ihn zurück.
// Liefert die Zusammenfassung fürs Job-Protokoll.
func (s *ServerService) refreshDSM(server *domain.Server) (string, error) {
	client, err := s.dsmClient(server)
	if err != nil {
		return "", err
	}
	defer client.Logout()
	info, err := client.Collect()
	if err != nil {
		return "", err
	}
	fresh := *server
	applyDSMInfo(&fresh, info)
	if err := s.servers.UpdateFields(server.ID, dsmFields(&fresh)); err != nil {
		return "", err
	}
	s.replaceDSMPackages(server.ID, info)

	var b strings.Builder
	fmt.Fprintf(&b, "DSM %s auf %s\n", info.Version, info.Model)
	if info.UpdateAvailable {
		fmt.Fprintf(&b, "Update verfügbar: %s\n", firstNonEmpty(info.LatestVersion, "neuere Fassung"))
	} else {
		b.WriteString("DSM ist aktuell.\n")
	}
	fmt.Fprintf(&b, "Pakete: %d\n", len(info.Packages))
	if info.VolumeTotalMB > 0 {
		fmt.Fprintf(&b, "Speicher: %d von %d MB belegt (Zustand: %s)\n",
			info.VolumeUsedMB, info.VolumeTotalMB, firstNonEmpty(info.VolumeStatus, "unbekannt"))
	}
	if info.SecurityRisks > 0 {
		fmt.Fprintf(&b, "Security Advisor: %d Befund(e) - %s\n", info.SecurityRisks, info.SecuritySummary)
	} else {
		b.WriteString("Security Advisor: keine Befunde.\n")
	}
	return b.String(), nil
}

// replaceDSMPackages spiegelt die DSM-Pakete in den Paketbestand. Sie tragen
// KEINE Update-Kandidaten: DSM meldet über die API nur die installierte
// Fassung, und einen Update-Kandidaten zu behaupten, den niemand geprüft hat,
// wäre genau die Art Halbwahrheit, die LCM sonst vermeidet.
func (s *ServerService) replaceDSMPackages(id uint, info *synology.Info) {
	pkgs := make([]domain.Package, 0, len(info.Packages))
	for _, p := range info.Packages {
		name := firstNonEmpty(p.Name, p.ID)
		if name == "" {
			continue
		}
		// ServerRef setzt ReplacePackages selbst (aus id).
		pkgs = append(pkgs, domain.Package{
			Name: name, Version: p.Version,
		})
	}
	if err := s.servers.ReplacePackages(id, pkgs); err != nil {
		return
	}
}

// recordDSMJoin schreibt den Onboarding-Job und den Audit-Eintrag.
func (s *ServerService) recordDSMJoin(server *domain.Server, actor, detail string) {
	now := time.Now()
	job := &domain.Job{
		ServerID: &server.ID, Type: "join", Name: "DSM-Onboarding: " + server.Name,
		Status: domain.JobStatusSuccess, TriggeredBy: actor,
		StartedAt: &now, FinishedAt: &now,
		Output: "Synology-DSM-Gerät angelegt (Überwachung über die DSM-Web-API).\n" + detail,
	}
	_ = s.jobs.jobs.Create(job)
	s.audit.Log(actor, "server.dsm_create", "server", server.ID, server.Host)
}

// runDSMRule führt eine Gruppen-Regel auf einem DSM-Gerät aus.
//
// Auf einem reinen API-Gerät gibt es keine Shell: Health-Check und
// System-Sync erheben stattdessen den Gerätezustand neu - das ist dort die
// sachliche Entsprechung eines Verfügbarkeits-Pings, und es hält Ampel,
// Speicher-Verlauf und Update-Stand aktuell. Alles Shell-/Paket-Gestützte
// wird ausdrücklich benannt übersprungen (nicht still), damit ein gemischter
// Schedule nicht an einem Gerätetyp scheitert und im Protokoll steht, WARUM
// nichts passiert ist.
func (e *Executor) runDSMRule(job *domain.Job, server *domain.Server, rule *domain.Rule) {
	switch rule.Type {
	case domain.RuleTypeHealth, domain.RuleTypeSync, domain.RuleTypePackageScan:
		if e.dsmRefresh == nil {
			e.jobs.Complete(job, "", nil, errors.New("DSM-Erfassung nicht verdrahtet"))
			return
		}
		out, err := e.dsmRefresh(server)
		if err != nil {
			e.markUnreachable(server, err)
			e.jobs.Complete(job, "", nil, err)
			return
		}
		e.jobs.Complete(job, out, ptrInt(0), nil)
	default:
		e.jobs.Complete(job, "Synology DSM: Aktionstyp „"+rule.Type+"“ übersprungen - "+
			"LCM überwacht dieses Gerät über die DSM-Web-API; Paketverwaltung, Firewall, "+
			"Benutzer-Sync und Skripte gibt es dort nicht.", ptrInt(0), nil)
	}
}

// RefreshDSMState ist der exportierte Einstieg in die DSM-Erfassung - für die
// Verdrahtung des Executors (WithDSMRefresh), der den ServerService nicht
// kennt.
func (s *ServerService) RefreshDSMState(server *domain.Server) (string, error) {
	return s.refreshDSM(server)
}
