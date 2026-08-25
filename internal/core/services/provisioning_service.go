package services

import (
	"fmt"
	"log/slog"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// managedMarker grenzt den von LCM verwalteten Block in authorized_keys
// ab - so bleiben manuell hinterlegte Keys unangetastet.
const (
	managedBegin = "# >>> LCM managed keys >>>"
	managedEnd   = "# <<< LCM managed keys <<<"
)

// ProvisioningService synchronisiert Server: aktualisiert Hardware-/Paket-
// Daten und richtet die zugeordneten Linux-Benutzer (Accounts + SSH-Keys +
// optionales sudo) auf den Zielsystemen ein.
type ProvisioningService struct {
	linux    *repositories.LinuxUserRepository
	servers  *repositories.ServerRepository
	cipher   *crypto.Cipher
	audit    *AuditService
	recorder *SSHRecorder
	connect  func(*domain.Server) (sshx.Conn, error)
	// dnsDomains liefert die zu prüfenden DNS-Test-Domains (globale
	// Einstellung). Optional; nil = eingebaute Standardliste.
	dnsDomains func() []string
	// pending ist der Rückstand offener Benutzer-Abgleiche (siehe
	// user_sync_backlog.go). Optional; ohne ihn entfällt das Nachholen.
	pending *repositories.PendingUserSyncRepository
	// sshlog ist LCMs eigenes Sitzungsprotokoll - die einzige Quelle für die
	// Anmeldungen des Zugangsbenutzers (siehe applyServiceUserLogins).
	sshlog *repositories.SSHLogRepository
	// profiles liefert die Berechtigungsprofile. Optional; ohne sie fällt der
	// Sync auf das alte sudo-Bit zurück.
	profiles *repositories.PrivilegeProfileRepository
}

// WithProfiles verdrahtet die Berechtigungsprofile. Optional, damit schlanke
// Tests ohne sie auskommen.
func (s *ProvisioningService) WithProfiles(repo *repositories.PrivilegeProfileRepository) *ProvisioningService {
	s.profiles = repo
	return s
}

// WithSSHLog verdrahtet das Sitzungsprotokoll. Optional; ohne es bleibt der
// Zugangsbenutzer in der Übersicht ohne Anmeldungen.
func (s *ProvisioningService) WithSSHLog(repo *repositories.SSHLogRepository) *ProvisioningService {
	s.sshlog = repo
	return s
}

// WithDNSTestDomains verdrahtet die DNS-Test-Domains, damit der Sync denselben
// DNS-Befund erhebt wie der Hardware-Refresh. Optional.
func (s *ProvisioningService) WithDNSTestDomains(fn func() []string) *ProvisioningService {
	s.dnsDomains = fn
	return s
}

// dnsTestDomainList liefert die effektive Liste (Standard, wenn nicht verdrahtet).
func (s *ProvisioningService) dnsTestDomainList() []string {
	if s.dnsDomains != nil {
		return s.dnsDomains()
	}
	return (&domain.GlobalSettings{}).DNSTestDomainList()
}

func NewProvisioningService(linux *repositories.LinuxUserRepository, servers *repositories.ServerRepository, cipher *crypto.Cipher, connect func(*domain.Server) (sshx.Conn, error)) *ProvisioningService {
	return &ProvisioningService{linux: linux, servers: servers, cipher: cipher, connect: connect}
}

// WithAudit hängt einen AuditService an (optionale Verdrahtung, da der
// Executor den Service ohne Audit-Kontext erzeugt).
func (s *ProvisioningService) WithAudit(audit *AuditService) *ProvisioningService {
	s.audit = audit
	return s
}

// WithRecorder verdrahtet die SSH-Protokollierung.
func (s *ProvisioningService) WithRecorder(rec *SSHRecorder) *ProvisioningService {
	s.recorder = rec
	return s
}

// connectRec verbindet und legt den Protokoll-Decorator um die Verbindung.
func (s *ProvisioningService) connectRec(server *domain.Server, ctx SessionContext) (sshx.Conn, error) {
	conn, err := s.connect(server)
	if err != nil {
		return nil, err
	}
	if ctx.Host == "" {
		ctx.Host = server.Host
	}
	if ctx.User == "" {
		ctx.User = server.ServiceUser
	}
	return s.recorder.Record(conn, ctx), nil
}

func (s *ProvisioningService) logAudit(actor, action, entity string, id uint, details string) {
	if s.audit != nil {
		s.audit.Log(actor, action, entity, id, details)
	}
}

// SyncServer synchronisiert einen Server: aktualisiert Hardware-/Paket-
// Daten via Scan und verteilt die Linux-Benutzer. Liefert den
// Konsolen-Output fürs Job-Protokoll.
func (s *ProvisioningService) SyncServer(server *domain.Server, ctx SessionContext) (string, error) {
	conn, err := s.connectRec(server, ctx)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	var log strings.Builder

	// 1. Hardware-/Paket-Scan aktualisieren.
	scan := scanServerMode(conn, server.ServiceUser, server.RestrictedSudo)
	fresh := *server
	applyScan(&fresh, scan)
	fields := map[string]any{
		"os_name": fresh.OSName, "os_version": fresh.OSVersion, "kernel_version": fresh.KernelVersion,
		"installed_kernels": fresh.InstalledKernels,
		"proxmox_type":      fresh.ProxmoxType, "proxmox_version": fresh.ProxmoxVersion,
		"cpu_model": fresh.CPUModel, "cpu_cores": fresh.CPUCores,
		"mem_total_mb": fresh.MemTotalMB, "mem_used_mb": fresh.MemUsedMB,
		"disk_total_mb": fresh.DiskTotalMB, "disk_used_mb": fresh.DiskUsedMB,
		"ip_addresses": fresh.IPAddresses,
		"has_docker":   fresh.HasDocker, "has_compose": fresh.HasCompose,
	}
	// DNS gehört zur Grunderfassung - und zwar in JEDEM Scan-Weg. Lief der
	// Befund nur beim manuellen Hardware-Refresh, blieben die DNS-Daten auf
	// allen Servern leer, die ausschließlich der geplante Sync anfasst.
	// Rein lesend und sudo-frei, kostet also nichts.
	if !server.IsRouterOS() {
		for k, v := range dnsScanFields(conn, sanitizeDNSDomains(s.dnsTestDomainList())) {
			fields[k] = v
		}
	}
	if err := s.servers.UpdateFields(server.ID, fields); err != nil {
		return log.String(), err
	}
	_ = s.servers.ReplacePackages(server.ID, scan.Packages)
	_ = s.servers.ReplaceSnapPackages(server.ID, scan.Snaps)
	_ = s.servers.ReplaceRepositories(server.ID, scan.Repositories)
	_ = s.servers.ReplaceDiskVolumes(server.ID, scan.DiskVolumes)
	_ = s.servers.ReplaceServerUsers(server.ID, scan.Users)
	_ = s.servers.ReplaceServerUserLogins(server.ID, scan.UserLogins)
	_ = s.servers.ReplaceDockerContainers(server.ID, scan.DockerContainers)
	_ = s.servers.ReplaceDockerImages(server.ID, scan.DockerImages)
	log.WriteString(fmt.Sprintf("Hardware-Sync: %d Pakete, %d Snaps, %d Container, %d Images, Disk %d%%\n",
		len(scan.Packages), len(scan.Snaps), len(scan.DockerContainers), len(scan.DockerImages), fresh.DiskUsagePercent()))

	// 2. Linux-Benutzer verteilen.
	out, err := s.distributeUsers(conn, server)
	log.WriteString(out)
	return log.String(), err
}

// distributeUsers legt für jeden zugeordneten Linux-Benutzer den Account
// an (idempotent) und schreibt dessen Public-Keys in einen von LCM
// verwalteten Block der authorized_keys - so kann sich der Benutzer direkt
// mit Zertifikat anmelden.
func (s *ProvisioningService) distributeUsers(conn sshx.Conn, server *domain.Server) (string, error) {
	// Proxmox verwaltet seine Benutzer selbst (PAM/PVE-Realm) - LCM legt
	// dort keine Linux-Accounts an und verteilt keine authorized_keys.
	// Zentraler Sperrpunkt: deckt Gruppen-Sync, Sync-Rules und alle
	// automatischen Verteilwege ab.
	if server.IsProxmox() {
		return "Proxmox-System: Linux-Benutzer-Sync übersprungen (von LCM gesperrt).\n", nil
	}
	if server.IsRouterOS() {
		return "MikroTik RouterOS: Linux-Benutzer-Sync übersprungen (keine Linux-Benutzerverwaltung).\n", nil
	}
	if server.IsSynologyDSM() {
		return "Synology DSM: Linux-Benutzer-Sync übersprungen - DSM verwaltet seine Benutzer selbst.\n", nil
	}
	// In den Server-Einstellungen abgeschaltet: Zuweisungen bleiben bestehen,
	// aber der Server wird nicht angefasst.
	if server.UserSyncDisabled {
		return "Benutzer-Sync für diesen Server deaktiviert - übersprungen.\n", nil
	}
	// ALLE zugeordneten Benutzer - auch deaktivierte. Ein deaktivierter
	// Benutzer wird auf dem Zielsystem GESPERRT (Passwort gesperrt,
	// LCM-Key-Block raus, sudo weg); ihn nur aus der Soll-Liste zu filtern
	// ließ Konto, Schlüssel und Root-Rechte unangetastet, während LCM
	// „deaktiviert" anzeigte (R2-039 - der Notaus wirkte nicht).
	users, err := s.linux.AssignedForServer(server.ID)
	if err != nil {
		return "", err
	}
	// Auf DIESEM Server gesperrte Konten: Der Betreiber hat sie über die
	// Benutzer-Übersicht stillgelegt. Ohne diese Abfrage würde der Sync sie
	// gleich wieder provisionieren und entsperren - die Sperre wäre ein
	// Knopf ohne Wirkung, und niemand bekäme mit, dass LCM sie aufhebt.
	blocked, err := s.servers.BlockedServerUsers(server.ID)
	if err != nil {
		return "", err
	}
	// Wirksames Profil je Konto ermitteln und die dafür nötigen Gruppen und
	// sudoers-Dateien auf dem Server einrichten - VOR den Konten, damit die
	// Gruppe existiert, wenn das Konto ihr zugeordnet wird.
	profiles, profileLog, err := s.applyProfiles(conn, server, users)
	if err != nil {
		return profileLog, err
	}

	var log strings.Builder
	log.WriteString(profileLog)
	for i := range users {
		u := &users[i]
		if blocked[u.Username] {
			script := lockUserScript(u.Username)
			if server.RestrictedSudo {
				script = helperCmd("user-lock", u.Username)
			}
			out, code, runErr := conn.Run(privRun(server, script))
			if runErr != nil {
				return log.String(), fmt.Errorf("linux-user %s sperren: %w", u.Username, runErr)
			}
			if code != 0 {
				return log.String(), fmt.Errorf("linux-user %s sperren: exit %d: %s", u.Username, code, summarize(out))
			}
			log.WriteString("gesperrt (auf diesem Server blockiert): " + u.Username + "\n")
			continue
		}
		if !u.Active {
			script := lockUserScript(u.Username)
			if server.RestrictedSudo {
				script = helperCmd("user-lock", u.Username)
			}
			out, code, runErr := conn.Run(privRun(server, script))
			if runErr != nil {
				return log.String(), fmt.Errorf("linux-user %s sperren: %w", u.Username, runErr)
			}
			if code != 0 {
				return log.String(), fmt.Errorf("linux-user %s sperren: exit %d: %s", u.Username, code, summarize(out))
			}
			log.WriteString("gesperrt (deaktiviert): " + u.Username + " - Passwort gesperrt, LCM-Schlüssel und sudo entfernt\n")
			continue
		}
		// Passwort (falls per Aktivierung gesetzt) entschlüsseln, damit es
		// via chpasswd auf dem Server gesetzt werden kann. Ein Entschlüsselungs-
		// Fehler darf nicht stumm bleiben - der Benutzer würde sonst ohne
		// Passwort provisioniert und niemand wüsste warum.
		password := ""
		if u.PasswordEnc != "" && s.cipher != nil {
			if pw, err := s.cipher.DecryptString(u.PasswordEnc); err == nil {
				password = pw
			} else {
				slog.Warn("linux user: password not decryptable - user provisioned without password",
					"user", u.Username, "error", err)
				fmt.Fprintf(&log, "WARNUNG: Passwort von %s nicht entschlüsselbar - ohne Passwort provisioniert.\n", u.Username)
			}
		}
		effect := effectFor(u, profiles[u.Username])
		script := provisionScript(u, password, effect)
		// Eingeschränkter Modus: die gleiche Wirkung über den validierenden
		// LCM-Helper (kein Root-Shell-Skript).
		if server.RestrictedSudo {
			script = helperUserEnsureCmd(u, password, effect) +
				" && " + helperProfileMemberCmd(u.Username, effect.GroupSlug)
		}
		out, code, runErr := conn.Run(privRun(server, script))
		if runErr != nil {
			return log.String(), fmt.Errorf("linux-user %s provisionieren: %w", u.Username, runErr)
		}
		if code != 0 {
			return log.String(), fmt.Errorf("linux-user %s provisionieren: exit %d: %s", u.Username, code, summarize(out))
		}
		log.WriteString("provisioniert: " + u.Username + " (" + itoaKeys(len(u.SSHKeys)) +
			", " + profileLabel(profiles[u.Username]) + ")\n")
	}
	if log.Len() == 0 {
		return "keine linux-benutzer für diesen server\n", nil
	}
	return log.String(), nil
}

// provisionScript baut das idempotente Shell-Skript, das einen Linux-User
// anlegt/aktualisiert, dessen authorized_keys-Block setzt, sudo regelt und
// - falls ein Passwort übergeben wird - dieses via chpasswd setzt.
func provisionScript(u *domain.LinuxUser, password string, effect profileEffect) string {
	shell := u.Shell
	if shell == "" {
		shell = "/bin/bash"
	}

	var block strings.Builder
	block.WriteString(managedBegin + "\n")
	for _, k := range u.SSHKeys {
		block.WriteString(strings.TrimSpace(k.PublicKey) + "\n")
	}
	block.WriteString(managedEnd + "\n")

	steps := []string{
		// Werkzeug nach Verfügbarkeit wählen und die Passwortsperre des neuen
		// Kontos aufheben - dieselben zwei Gründe, aus denen der Service-User
		// auf Alpine und openSUSE scheiterte, betreffen das gesamte
		// Benutzer-Provisioning: LCM meldete für jede Zuweisung "ok", die
		// Konten konnten sich aber nicht anmelden (BUG-028).
		createUserWithShellScript(u.Username, shell),
		unlockAccountScript(u.Username),
		fmt.Sprintf("usermod -s %s %s 2>/dev/null || true", shell, u.Username),
		fmt.Sprintf("install -d -m 700 -o %s -g %s /home/%s/.ssh", u.Username, u.Username, u.Username),
		fmt.Sprintf("touch /home/%s/.ssh/authorized_keys", u.Username),
		// vorhandenen LCM-Block entfernen ...
		fmt.Sprintf("sed -i '/%s/,/%s/d' /home/%s/.ssh/authorized_keys",
			escapeSed(managedBegin), escapeSed(managedEnd), u.Username),
		// ... und neu anhängen
		fmt.Sprintf("printf '%%s' %s >> /home/%s/.ssh/authorized_keys", shellQuote(block.String()), u.Username),
		fmt.Sprintf("chown -R %s:%s /home/%s/.ssh", u.Username, u.Username, u.Username),
		fmt.Sprintf("chmod 600 /home/%s/.ssh/authorized_keys", u.Username),
	}
	if u.FullName != "" {
		steps = append(steps, fmt.Sprintf("chfn -f %s %s 2>/dev/null || true", shellQuote(u.FullName), u.Username))
	}
	// Passwort setzen (falls per Aktivierungslink hinterlegt) und Account
	// entsperren, damit Passwort-Login möglich ist.
	if password != "" {
		steps = append(steps,
			fmt.Sprintf("printf '%%s:%%s' %s %s | chpasswd", u.Username, shellQuote(password)),
			fmt.Sprintf("usermod -U %s 2>/dev/null || true", u.Username))
	}
	// Rechte aus dem Berechtigungsprofil: entweder der per-Benutzer-Grant mit
	// vollen Root-Rechten (mitgeliefertes Voll-Profil) oder die Mitgliedschaft
	// in genau einer Profilgruppe. Beides schließt einander aus - sonst
	// summierten sich Rechte beim Profilwechsel auf.
	if effect.FullRoot {
		steps = append(steps,
			fmt.Sprintf("printf '%%s ALL=(ALL) NOPASSWD:ALL\\n' %s > /etc/sudoers.d/lcm-%s", u.Username, u.Username),
			fmt.Sprintf("chmod 440 /etc/sudoers.d/lcm-%s", u.Username))
	} else {
		steps = append(steps, fmt.Sprintf("rm -f /etc/sudoers.d/lcm-%s", u.Username))
	}
	steps = append(steps, setProfileMembershipScript(u.Username, effect.GroupSlug))
	if effect.NoShell {
		// Kontotyp „nur Dateizugriff": Die Shell wird weggenommen, damit auch
		// ein Zugang an sshd vorbei (su, cron) keine bekommt.
		steps = append(steps, nologinShellStep(u.Username))
	}
	return strings.Join(steps, " && ")
}

// lockUserScript sperrt einen deaktivierten Benutzer auf dem Zielsystem:
// Passwort-Login gesperrt (usermod -L, BusyBox-Fallback passwd -l),
// LCM-Schlüsselblock aus den authorized_keys entfernt, sudo-Grant weg.
// Konto und Home bleiben - Deaktivierung ist umkehrbar; endgültiges
// Entfernen ist die getrennte Deprovisionierung. Idempotent; ein fehlendes
// Konto ist kein Fehler (der Benutzer war dort nie provisioniert).
func lockUserScript(username string) string {
	return strings.Join([]string{
		fmt.Sprintf("if id -u %s >/dev/null 2>&1; then", username),
		fmt.Sprintf("usermod -L %s 2>/dev/null || passwd -l %s 2>/dev/null || true;", username, username),
		fmt.Sprintf("if [ -f /home/%s/.ssh/authorized_keys ]; then sed -i '/%s/,/%s/d' /home/%s/.ssh/authorized_keys; fi;",
			username, escapeSed(managedBegin), escapeSed(managedEnd), username),
		fmt.Sprintf("rm -f /etc/sudoers.d/lcm-%s;", username),
		"fi",
	}, " ")
}

// removeUserScript entfernt einen Benutzer ENDGÜLTIG vom Zielsystem -
// distributionsbewusst und ehrlich: BusyBox hat kein userdel (nur deluser);
// die alte Fassung unterdrückte den Fehlschlag doppelt und meldete
// „entfernt", während das Konto samt Schlüsseln weiter nutzbar war
// (R2-040). Deshalb: Werkzeug nach Verfügbarkeit wählen und am Ende
// NACHWEISEN, dass das Konto weg ist - sonst exit != 0.
func removeUserScript(username string) string {
	return strings.Join([]string{
		fmt.Sprintf("if id -u %s >/dev/null 2>&1; then", username),
		fmt.Sprintf("if command -v userdel >/dev/null 2>&1; then userdel -r %s;", username),
		fmt.Sprintf("elif command -v deluser >/dev/null 2>&1; then deluser --remove-home %s;", username),
		"else echo 'weder userdel noch deluser vorhanden' >&2; exit 1; fi;",
		"fi;",
		fmt.Sprintf("if id -u %s >/dev/null 2>&1; then echo 'konto %s besteht weiterhin - entfernen fehlgeschlagen' >&2; exit 1; fi;",
			username, username),
		fmt.Sprintf("rm -f /etc/sudoers.d/lcm-%s", username),
	}, " ")
}

// escapeSed maskiert einen Kommentar-Marker für die sed-Adressierung.
func escapeSed(s string) string {
	s = strings.ReplaceAll(s, "/", `\/`)
	return s
}

func itoaKeys(n int) string {
	if n == 1 {
		return "1 key"
	}
	return fmt.Sprintf("%d keys", n)
}

// ---- Server ↔ Linux-User-Zuordnung ------------------------------------------

// AssignLinuxUserToServer ordnet einen Linux-Benutzer einem Server zu und
// löst sofort die Verteilung aus (best effort - die Zuordnung bleibt bei
// Verbindungsfehler bestehen und wird beim nächsten Sync erneut versucht).
func (s *ProvisioningService) AssignLinuxUserToServer(server *domain.Server, linuxUserID uint, actor string) error {
	// Auf Proxmox-Systemen ist die Benutzer-Provisionierung gesperrt -
	// gar nicht erst zuordnen (klare Meldung statt stiller Skip).
	if server.IsProxmox() {
		return ErrProxmoxRestricted
	}
	if err := ensureNotRouterOS(server); err != nil {
		return err
	}
	// Erst prüfen, ob es den Benutzer überhaupt gibt: ohne diese Prüfung
	// schlug der rohe Datenbankfehler bis zur API durch und der Anwender sah
	// "interner Serverfehler" - das sieht nach einem Programmfehler aus,
	// obwohl bloß eine ID falsch war (BUG-017).
	if _, err := s.linux.FindByID(linuxUserID); err != nil {
		return err
	}
	if err := s.linux.AssignToServer(linuxUserID, server.ID); err != nil {
		return err
	}
	s.logAudit(actor, "server.assign-user", "server", server.ID, "")
	return s.syncUsersOnly(server, actor)
}

// RemoveLinuxUserFromServer entzieht die Zuordnung und entfernt das Konto
// vom Server - sofern der Benutzer nicht über eine Gruppe weiterhin dorthin
// berechtigt ist. Ist der Server gerade nicht erreichbar, bleibt der Auftrag
// im Rückstand und wird nachgeholt.
func (s *ProvisioningService) RemoveLinuxUserFromServer(server *domain.Server, linuxUserID uint, actor string) error {
	before := s.EntitledUsernames(server.ID)
	if err := s.linux.RemoveFromServer(linuxUserID, server.ID); err != nil {
		return err
	}
	s.logAudit(actor, "server.remove-user", "server", server.ID, "")
	return s.ReconcileServerNow(server, before, actor)
}

// DeprovisionUser entfernt einen Linux-Benutzer AKTIV vom Zielserver:
// löscht den Account samt Home-Verzeichnis (userdel -r) und die
// zugehörige sudoers-Datei. Idempotent (fehlt der Account, kein Fehler).
// Demo-Server werden nicht kontaktiert.
func (s *ProvisioningService) DeprovisionUser(server *domain.Server, username, actor string) (string, error) {
	if server.IsDemo {
		return "demo-server: übersprungen", nil
	}
	conn, err := s.connectRec(server, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: "deprovision-user:" + username,
	})
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	return s.removeUserOnConn(conn, server, username)
}

// removeUserOnConn löscht das Konto über eine bereits offene Verbindung.
// Distributionsbewusst (userdel/deluser) und mit Nachweis, dass das Konto
// wirklich weg ist - die alte Fassung unterdrückte den userdel-Fehlschlag auf
// BusyBox doppelt und meldete „entfernt", während der Zugang bestehen blieb
// (R2-040).
func (s *ProvisioningService) removeUserOnConn(conn sshx.Conn, server *domain.Server, username string) (string, error) {
	script := removeUserScript(username)
	if server.RestrictedSudo {
		script = helperCmd("user-remove", username)
	}
	out, code, runErr := conn.Run(privRun(server, script))
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("entfernen von %s fehlgeschlagen (exit %d): %s", username, code, summarize(out))
	}
	return out, runErr
}

// SyncUsers forciert die Linux-Benutzer-Verteilung auf einem Server.
func (s *ProvisioningService) SyncUsers(server *domain.Server, actor string) (string, error) {
	// Manueller Sync auf Proxmox: klare Fehlermeldung statt No-Op -
	// der Aufrufer soll wissen, dass hier nichts verteilt wird.
	if server.IsProxmox() {
		return "", ErrProxmoxRestricted
	}
	if err := ensureNotRouterOS(server); err != nil {
		return "", err
	}
	// Manueller Sync bei deaktiviertem Benutzer-Sync: klare Fehlermeldung
	// statt No-Op - der Aufrufer soll wissen, dass hier nichts verteilt wird.
	if server.UserSyncDisabled {
		return "", ErrUserSyncDisabled
	}
	s.logAudit(actor, "server.sync-users", "server", server.ID, "")
	conn, err := s.connectRec(server, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: "sync-users",
	})
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	return s.distributeUsers(conn, server)
}

// syncUsersOnly verteilt nur die Benutzer (ohne Hardware-Scan). Bei nicht
// erreichbarem Server kein harter Fehler - der reguläre Sync-Schedule
// holt es nach.
func (s *ProvisioningService) syncUsersOnly(server *domain.Server, actor string) error {
	if server.IsDemo {
		return nil
	}
	// Benutzer-Sync deaktiviert: Zuordnung nur speichern, Server nicht anfassen.
	if server.UserSyncDisabled {
		return nil
	}
	conn, err := s.connectRec(server, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: "provision-users",
	})
	if err != nil {
		// Nicht erreichbar: Auftrag in den Rückstand, damit er beim nächsten
		// Kontakt nachgeholt wird statt bis zum nächsten geplanten Sync zu warten.
		if s.pending != nil {
			if qErr := s.pending.Queue(server.ID, ""); qErr != nil {
				slog.Error("user sync: backlog entry not stored", "server", server.ID, "error", qErr)
			}
		}
		return nil
	}
	defer conn.Close()
	_, err = s.distributeUsers(conn, server)
	return err
}
