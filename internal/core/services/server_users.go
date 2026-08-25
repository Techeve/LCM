package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
)

// Benutzer-Übersicht eines Servers: die beim Scan erfassten anmeldefähigen
// Linux-Konten (IST-Zustand) samt Aktionen darauf - deaktivieren/aktivieren
// und endgültig entfernen. Das SOLL (von LCM verteilte LinuxUser) bleibt in
// der Provisionierung; verwaltete Konten sind hier bewusst schreibgeschützt,
// damit sich Übersicht und Sync nicht gegenseitig überschreiben.

var (
	// ErrServerUserProtected: root und der LCM-Service-User sind über die
	// Benutzer-Übersicht nicht veränderbar.
	ErrServerUserProtected = errors.New("dieses Konto ist geschützt und kann über die Benutzer-Übersicht nicht verändert werden")
	// ErrServerUserManaged: ein von LCM verteiltes Konto lässt sich auf einem
	// Server SPERREN (die Sperre überlebt den Sync), aber nicht endgültig
	// entfernen - das nächste Verteilen legte es ohnehin wieder an. Dafür ist
	// die Deprovisionierung über die Linux-Benutzer-Verwaltung da.
	ErrServerUserManaged = errors.New("dieses Konto wird von LCM verteilt - endgültig entfernen geht nur über die Linux-Benutzer-Verwaltung (sperren ist hier möglich)")
	// ErrInvalidServerUsername: der Name ist kein gültiger Linux-Account-Name.
	ErrInvalidServerUsername = errors.New("ungültiger Benutzername")
)

// disableUserScript sperrt ein beliebiges Konto vollständig: Passwort gesperrt
// UND Ablaufdatum gesetzt. Das Ablaufdatum ist der entscheidende Teil -
// usermod -L sperrt nur das Passwort, SSH-Key-Logins gehen weiter durch;
// erst ein abgelaufenes Konto blockiert jeden Login. (Gegenstück im
// eingeschränkten Modus: Helper-Unterkommando user-disable.)
func disableUserScript(username string) string {
	return strings.Join([]string{
		fmt.Sprintf("id -u %s >/dev/null 2>&1 || { echo 'unbekanntes konto: %s' >&2; exit 1; }", username, username),
		fmt.Sprintf("usermod -L %s 2>/dev/null || passwd -l %s 2>/dev/null || true", username, username),
		fmt.Sprintf("usermod -e 1970-01-02 %s 2>/dev/null || chage -E 1 %s 2>/dev/null || true", username, username),
	}, "; ")
}

// enableUserScript hebt Passwortsperre und Ablaufdatum wieder auf.
func enableUserScript(username string) string {
	return strings.Join([]string{
		fmt.Sprintf("id -u %s >/dev/null 2>&1 || { echo 'unbekanntes konto: %s' >&2; exit 1; }", username, username),
		fmt.Sprintf("usermod -U %s 2>/dev/null || passwd -u %s 2>/dev/null || true", username, username),
		fmt.Sprintf("usermod -e '' %s 2>/dev/null || chage -E -1 %s 2>/dev/null || true", username, username),
	}, "; ")
}

// ListServerUsers liefert die gespeicherten Konten des letzten Scans,
// LCM-verwaltete markiert (Managed).
func (s *ProvisioningService) ListServerUsers(server *domain.Server) ([]domain.ServerUser, error) {
	users, err := s.servers.FindServerUsers(server.ID)
	if err != nil {
		return nil, err
	}
	s.markManaged(server, users)
	return users, nil
}

// RefreshServerUsers erhebt die Konten jetzt neu über SSH, speichert sie und
// liefert den frischen Bestand.
func (s *ProvisioningService) RefreshServerUsers(server *domain.Server, actor string) ([]domain.ServerUser, error) {
	if server.IsDemo {
		return s.ListServerUsers(server)
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	conn, err := s.connectRec(server, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: "users-scan",
	})
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	users, logins, err := s.scanUsersAndLogins(conn, server)
	if err != nil {
		return nil, err
	}
	if err := s.servers.ReplaceServerUsers(server.ID, users); err != nil {
		return nil, err
	}
	_ = s.servers.ReplaceServerUserLogins(server.ID, logins)
	s.markManaged(server, users)
	return users, nil
}

// scanUsers führt den Benutzer-Scan über eine bestehende Verbindung aus.
func (s *ProvisioningService) scanUsers(conn sshx.Conn, server *domain.Server) ([]domain.ServerUser, error) {
	users, _, err := s.scanUsersAndLogins(conn, server)
	return users, err
}

// scanUsersAndLogins erhebt Konten UND Anmelde-Historie in einem Durchgang -
// beides steckt in derselben Skript-Ausgabe, eine zweite Verbindung wäre
// verschwendet.
func (s *ProvisioningService) scanUsersAndLogins(conn sshx.Conn, server *domain.Server) ([]domain.ServerUser, []domain.ServerUserLogin, error) {
	script := usersScanScript()
	if server.RestrictedSudo {
		script = helperCmd("users-scan")
	}
	out, code, err := conn.Run(privRun(server, script))
	if err != nil {
		return nil, nil, fmt.Errorf("benutzer-scan: %w", err)
	}
	if code != 0 {
		return nil, nil, fmt.Errorf("benutzer-scan: exit %d: %s", code, summarize(out))
	}
	users := parseServerUsers(out)
	if len(users) == 0 {
		// root ist immer anmeldefähig - eine leere Liste heißt, dass der
		// Scan nichts erheben konnte (z.B. veralteter Helper im
		// eingeschränkten Modus), nicht dass es keine Konten gibt.
		return nil, nil, errors.New("benutzer-scan lieferte keine Konten (eingeschränkter Modus mit veraltetem LCM-Helper?)")
	}
	logins := parseLastLogins(out)
	applyLoginHistory(users, logins)
	return users, logins, nil
}

// ServerUserLogins liefert die erfassten Anmeldungen eines Kontos. Für den
// LCM-Zugangsbenutzer stammt die Liste aus LCMs eigenem Sitzungsprotokoll
// (siehe applyServiceUserLogins) - in wtmp steht davon nichts.
func (s *ProvisioningService) ServerUserLogins(server *domain.Server, username string, limit int) ([]domain.ServerUserLogin, error) {
	if !domain.ValidLinuxUsername(username) {
		return nil, ErrInvalidServerUsername
	}
	if s.sshlog != nil && username == server.ServiceUser {
		sessions, err := s.sshlog.SessionsForUser(server.ID, username, limit)
		if err != nil {
			return nil, err
		}
		if len(sessions) > 0 {
			return sessionsAsLogins(server.ID, sessions), nil
		}
	}
	return s.servers.FindServerUserLogins(server.ID, username, limit)
}

// sessionsAsLogins bringt LCM-Sitzungen in die Form der übrigen Anmeldungen.
// TTY trägt den Zweck der Sitzung statt eines Terminals - den gibt es hier
// nicht, und der Zweck ist die nützlichere Information.
func sessionsAsLogins(serverID uint, sessions []domain.SSHSession) []domain.ServerUserLogin {
	out := make([]domain.ServerUserLogin, 0, len(sessions))
	for i := range sessions {
		s := &sessions[i]
		out = append(out, domain.ServerUserLogin{
			ServerID:    serverID,
			Username:    s.User,
			FromHost:    s.Host,
			TTY:         s.Purpose,
			StartedAt:   s.OpenedAt,
			EndedAt:     s.ClosedAt,
			StillActive: s.ClosedAt == nil,
		})
	}
	return out
}

// SetServerUserDisabled deaktiviert bzw. reaktiviert ein Konto auf dem
// Zielsystem. Geschützte und LCM-verwaltete Konten sind ausgenommen.
func (s *ProvisioningService) SetServerUserDisabled(server *domain.Server, username string, disabled bool, actor string) (string, error) {
	if err := s.guardServerUserAction(server, username, true); err != nil {
		return "", err
	}
	// Verteilte Konten: die Sperre MUSS gemerkt werden, bevor sie gesetzt
	// wird - sonst hebt der nächste Sync sie wieder auf (siehe
	// distributeUsers). Beim Freigeben entsprechend zurücknehmen.
	managed, err := s.isManaged(server.ID, username)
	if err != nil {
		return "", err
	}
	if managed {
		if disabled {
			err = s.servers.BlockServerUser(server.ID, username, actor)
		} else {
			err = s.servers.UnblockServerUser(server.ID, username)
		}
		if err != nil {
			return "", err
		}
	}
	script, helperSub := enableUserScript(username), "user-enable"
	action := "server.user-enable"
	if disabled {
		script, helperSub = disableUserScript(username), "user-disable"
		action = "server.user-disable"
	}
	if server.RestrictedSudo {
		script = helperCmd(helperSub, username)
	}
	out, err := s.runUserActionAndRescan(server, username, script, "user-toggle:"+username, actor)
	if err != nil {
		return out, err
	}
	s.logAudit(actor, action, "server", server.ID, username)
	return out, nil
}

// RemoveServerUser entfernt ein Konto ENDGÜLTIG vom Zielsystem (samt Home).
// Geschützte und LCM-verwaltete Konten sind ausgenommen - für verwaltete
// Konten ist die Deprovisionierung über die Linux-Benutzer-Verwaltung der Weg.
func (s *ProvisioningService) RemoveServerUser(server *domain.Server, username, actor string) (string, error) {
	if err := s.guardServerUserAction(server, username, false); err != nil {
		return "", err
	}
	script := removeUserScript(username)
	if server.RestrictedSudo {
		script = helperCmd("user-remove", username)
	}
	out, err := s.runUserActionAndRescan(server, username, script, "user-remove:"+username, actor)
	if err != nil {
		return out, err
	}
	s.logAudit(actor, "server.user-remove", "server", server.ID, username)
	return out, nil
}

// runUserActionAndRescan führt das Aktions-Skript aus und erhebt die Konten
// in derselben Verbindung neu - die Übersicht zeigt danach sofort den
// tatsächlichen Zustand des Zielsystems.
func (s *ProvisioningService) runUserActionAndRescan(server *domain.Server, username, script, purpose, actor string) (string, error) {
	if server.IsDemo {
		return "demo-server: übersprungen", nil
	}
	conn, err := s.connectRec(server, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: purpose,
	})
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	out, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return out, fmt.Errorf("%s: %w", username, runErr)
	}
	if code != 0 {
		return out, fmt.Errorf("%s: exit %d: %s", username, code, summarize(out))
	}
	// Bestand direkt nachziehen (best effort - der nächste Scan holt es sonst).
	if users, logins, scanErr := s.scanUsersAndLogins(conn, server); scanErr == nil {
		_ = s.servers.ReplaceServerUsers(server.ID, users)
		_ = s.servers.ReplaceServerUserLogins(server.ID, logins)
	}
	return out, nil
}

// guardServerUserAction prüft Name, Schutz- und Verwaltungsstatus vor jeder
// verändernden Aktion auf einem gescannten Konto.
// allowManaged unterscheidet die beiden Fälle: Sperren ist auch für verteilte
// Konten erlaubt (die Sperre wird gemerkt und überlebt den Sync), endgültiges
// Entfernen nicht - das nächste Verteilen legte das Konto ohnehin wieder an.
func (s *ProvisioningService) guardServerUserAction(server *domain.Server, username string, allowManaged bool) error {
	if !domain.ValidLinuxUsername(username) {
		return ErrInvalidServerUsername
	}
	// root und der Management-Zugang von LCM bleiben unantastbar: Über sie
	// verwaltet LCM den Server überhaupt erst.
	if username == "root" || username == server.ServiceUser {
		return ErrServerUserProtected
	}
	if server.IsProxmox() {
		return ErrProxmoxRestricted
	}
	if err := ensureNotRouterOS(server); err != nil {
		return err
	}
	if allowManaged {
		return nil
	}
	assigned, err := s.linux.AssignedForServer(server.ID)
	if err != nil {
		return err
	}
	for i := range assigned {
		if assigned[i].Username == username {
			return ErrServerUserManaged
		}
	}
	return nil
}

// isManaged meldet, ob das Konto von LCM auf diesen Server verteilt wird.
func (s *ProvisioningService) isManaged(serverID uint, username string) (bool, error) {
	assigned, err := s.linux.AssignedForServer(serverID)
	if err != nil {
		return false, err
	}
	for i := range assigned {
		if assigned[i].Username == username {
			return true, nil
		}
	}
	return false, nil
}

// markManaged setzt das Managed-Flag für Konten, die einem von LCM auf diesen
// Server verteilten LinuxUser entsprechen.
func (s *ProvisioningService) markManaged(server *domain.Server, users []domain.ServerUser) {
	assigned, err := s.linux.AssignedForServer(server.ID)
	if err != nil {
		return
	}
	names := make(map[string]bool, len(assigned))
	for i := range assigned {
		names[assigned[i].Username] = true
	}
	blocked, _ := s.servers.BlockedServerUsers(server.ID)
	logins, _ := s.servers.CountServerUserLogins(server.ID)
	for i := range users {
		users[i].Managed = names[users[i].Username]
		users[i].Blocked = blocked[users[i].Username]
		users[i].LoginCount = logins[users[i].Username]
	}
	s.applyServiceUserLogins(server, users)
}

// applyServiceUserLogins füllt Anzahl und Zeitpunkt der Anmeldungen des
// LCM-Zugangsbenutzers aus LCMs eigenem Sitzungsprotokoll.
//
// Warum eine zweite Quelle: LCM meldet sich ohne Terminal an (Kommando-
// Sitzung statt Login-Shell), und dafür schreibt sshd weder wtmp noch
// lastlog. Das Konto stand deshalb dauerhaft auf „nie angemeldet" - ausgerechnet
// das Konto, das den Server am häufigsten betritt. Die Sitzungen liegen in
// LCM ohnehin lückenlos vor (Protokoll-Tab), also werden sie hier genutzt.
func (s *ProvisioningService) applyServiceUserLogins(server *domain.Server, users []domain.ServerUser) {
	if s.sshlog == nil || server.ServiceUser == "" {
		return
	}
	count, last, err := s.sshlog.SessionStatsForUser(server.ID, server.ServiceUser)
	if err != nil || count == 0 {
		return
	}
	for i := range users {
		if users[i].Username != server.ServiceUser {
			continue
		}
		users[i].LoginCount = count
		users[i].LastLoginAt = last
		users[i].LoginsFromLCM = true
	}
}
