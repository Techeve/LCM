package services

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// Automatischer Benutzer-Abgleich.
//
// Jede Änderung, die die Berechtigung eines Linux-Benutzers auf einem Server
// betrifft - neuer Schlüssel, sudo, Passwort, Gruppen- oder Server-Zuordnung,
// oder ein Server, der eine Gruppe betritt oder verlässt - läuft hier durch.
// Der Ablauf ist immer derselbe:
//
//  1. Aufträge im Rückstand festhalten (überlebt einen Neustart von LCM),
//  2. sofort versuchen, sie abzuarbeiten,
//  3. gelingt das nicht (Server aus, keine Verbindung), bleiben sie liegen und
//     werden beim nächsten erfolgreichen Kontakt nachgeholt - der Health-Check
//     arbeitet den Rückstand auf seiner ohnehin offenen Verbindung ab.
//
// Schritt 3 ist der eigentliche Grund für den Rückstand: Ein ENTZOGENER Zugang,
// der erst „beim nächsten geplanten Sync" verschwindet, bleibt bis dahin nutzbar.

// ErrUserSyncPostponed meldet, dass ein Abgleich im Rückstand liegt, weil der
// Server nicht erreichbar war. Kein Fehler im eigentlichen Sinn - die Änderung
// ist gespeichert und wird nachgeholt.
var ErrUserSyncPostponed = errors.New("server nicht erreichbar - der benutzer-abgleich liegt im rückstand")

// WithPendingUserSyncs verdrahtet den Rückstand. Ohne ihn bleibt es beim
// sofortigen Versuch ohne Nachholen.
func (s *ProvisioningService) WithPendingUserSyncs(repo *repositories.PendingUserSyncRepository) *ProvisioningService {
	s.pending = repo
	return s
}

// userSyncSkipped meldet, ob auf diesem Server überhaupt Linux-Benutzer
// verwaltet werden - sonst gibt es nichts abzugleichen.
func userSyncSkipped(server *domain.Server) bool {
	return server.IsDemo || server.UserSyncDisabled ||
		server.IsProxmox() || server.IsRouterOS() || server.IsSynologyDSM()
}

// EntitledUsernames sind die Konten, zu denen ein Server einen Benutzer
// aktuell berechtigt (direkt oder über eine Gruppe). Vor einer Änderung
// erhoben, um danach zu wissen, wem der Zugang entzogen wurde.
func (s *ProvisioningService) EntitledUsernames(serverID uint) []string {
	users, err := s.linux.AssignedForServer(serverID)
	if err != nil {
		slog.Warn("user sync: entitled accounts not readable", "server", serverID, "error", err)
		return nil
	}
	names := make([]string, 0, len(users))
	for i := range users {
		names = append(names, users[i].Username)
	}
	return names
}

// ReconcileServer gleicht einen Server nach einer Änderung ab, ohne auf das
// Ergebnis zu warten - für Änderungen, die viele Server betreffen (Gruppen).
// before sind die vorher berechtigten Konten; wer darin steht und jetzt nicht
// mehr berechtigt ist, wird vom Server entfernt.
func (s *ProvisioningService) ReconcileServer(server *domain.Server, before []string, actor string) {
	tasks := s.planReconcile(server, before)
	if tasks == nil {
		return
	}
	go s.reconcileAsync(*server, tasks, actor)
}

// ReconcileServers ist ReconcileServer für mehrere Server; before ordnet je
// Server die vorher berechtigten Konten zu (nil = nichts entzogen).
func (s *ProvisioningService) ReconcileServers(servers []domain.Server, before map[uint][]string, actor string) {
	for i := range servers {
		s.ReconcileServer(&servers[i], before[servers[i].ID], actor)
	}
}

// ReconcileServerNow ist ReconcileServer für Aktionen an EINEM Server, bei
// denen der Anwender auf die Rückmeldung wartet. Ein nicht erreichbarer Server
// ist kein Fehler - der Auftrag liegt dann im Rückstand.
func (s *ProvisioningService) ReconcileServerNow(server *domain.Server, before []string, actor string) error {
	tasks := s.planReconcile(server, before)
	if tasks == nil {
		return nil
	}
	_, err := s.runTasks(server, tasks, actor)
	if errors.Is(err, ErrUserSyncPostponed) {
		return nil
	}
	return err
}

// planReconcile stellt die Aufträge zusammen und legt sie in den Rückstand.
// nil bedeutet: auf diesem Server gibt es nichts abzugleichen.
func (s *ProvisioningService) planReconcile(server *domain.Server, before []string) []domain.PendingUserSync {
	if userSyncSkipped(server) {
		return nil
	}
	var tasks []domain.PendingUserSync
	for _, name := range removedUsernames(before, s.EntitledUsernames(server.ID)) {
		tasks = append(tasks, domain.PendingUserSync{ServerID: server.ID, Username: name})
	}
	// Zum Schluss verteilen: erst die entzogenen Konten weg, dann die
	// verbliebenen auf den aktuellen Stand bringen.
	tasks = append(tasks, domain.PendingUserSync{ServerID: server.ID})
	if s.pending != nil {
		for i := range tasks {
			if err := s.pending.Queue(tasks[i].ServerID, tasks[i].Username); err != nil {
				slog.Error("user sync: backlog entry not stored", "server", server.ID, "error", err)
			}
		}
	}
	return tasks
}

func (s *ProvisioningService) reconcileAsync(server domain.Server, tasks []domain.PendingUserSync, actor string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("user sync: reconcile panicked", "server", server.Name, "panic", r)
		}
	}()
	if _, err := s.runTasks(&server, tasks, actor); err != nil {
		slog.Info("user sync: not completed", "server", server.Name, "error", err)
	}
}

// removedUsernames liefert die Konten aus before, die in after fehlen.
func removedUsernames(before, after []string) []string {
	if len(before) == 0 {
		return nil
	}
	keep := make(map[string]bool, len(after))
	for _, name := range after {
		keep[name] = true
	}
	var gone []string
	for _, name := range before {
		if !keep[name] {
			gone = append(gone, name)
		}
	}
	return gone
}

// runTasks öffnet eine Verbindung und arbeitet ab. Mit verdrahtetem Rückstand
// zählt dessen Inhalt (er enthält die eben eingestellten Aufträge und alles,
// was früher liegengeblieben ist); ohne ihn nur die übergebenen Aufträge.
func (s *ProvisioningService) runTasks(server *domain.Server, tasks []domain.PendingUserSync, actor string) (string, error) {
	todo, err := s.openTasks(server.ID, tasks)
	if err != nil || len(todo) == 0 {
		return "", err
	}
	conn, err := s.connectRec(server, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: "user-sync",
	})
	if err != nil {
		s.markAllFailed(todo, err)
		return "", fmt.Errorf("%w: %v", ErrUserSyncPostponed, err)
	}
	defer conn.Close()
	return s.drainEntries(conn, server, todo, actor)
}

// DrainServer arbeitet einen liegengebliebenen Rückstand über eine eigene
// Verbindung ab (z.B. auf Knopfdruck).
func (s *ProvisioningService) DrainServer(server *domain.Server, actor string) (string, error) {
	if userSyncSkipped(server) {
		return "", nil
	}
	return s.runTasks(server, nil, actor)
}

// DrainOnConn holt den Rückstand auf einer bereits offenen Verbindung nach -
// so braucht der Health-Check keine zweite Sitzung.
func (s *ProvisioningService) DrainOnConn(conn sshx.Conn, server *domain.Server, actor string) (string, error) {
	if userSyncSkipped(server) {
		return "", nil
	}
	todo, err := s.openTasks(server.ID, nil)
	if err != nil || len(todo) == 0 {
		return "", err
	}
	return s.drainEntries(conn, server, todo, actor)
}

func (s *ProvisioningService) openTasks(serverID uint, fallback []domain.PendingUserSync) ([]domain.PendingUserSync, error) {
	if s.pending == nil {
		return fallback, nil
	}
	return s.pending.ForServer(serverID)
}

// drainEntries führt die Aufträge aus. Ein gescheiterter Auftrag bleibt liegen
// (mit Ursache am Eintrag) und blockiert die übrigen nicht.
func (s *ProvisioningService) drainEntries(conn sshx.Conn, server *domain.Server, entries []domain.PendingUserSync, actor string) (string, error) {
	var log strings.Builder
	var firstErr error
	done := 0
	for i := range entries {
		entry := &entries[i]
		out, err := s.runEntry(conn, server, entry)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.markFailed(entry, err)
			fmt.Fprintf(&log, "%s - fehlgeschlagen: %v\n", entryLabel(entry), err)
			continue
		}
		s.clear(entry)
		done++
		fmt.Fprintf(&log, "%s - erledigt\n%s", entryLabel(entry), out)
	}
	if done > 0 {
		s.logAudit(actor, "server.user-sync", "server", server.ID,
			fmt.Sprintf("%d Benutzer-Abgleiche ausgeführt", done))
	}
	return log.String(), firstErr
}

func (s *ProvisioningService) runEntry(conn sshx.Conn, server *domain.Server, entry *domain.PendingUserSync) (string, error) {
	if entry.Removal() {
		return s.removeUserOnConn(conn, server, entry.Username)
	}
	return s.distributeUsers(conn, server)
}

func entryLabel(entry *domain.PendingUserSync) string {
	if entry.Removal() {
		return "Konto entfernen: " + entry.Username
	}
	return "Benutzer verteilen"
}

func (s *ProvisioningService) clear(entry *domain.PendingUserSync) {
	if s.pending == nil || entry.ID == 0 {
		return
	}
	if err := s.pending.Delete(entry.ID); err != nil {
		slog.Error("user sync: backlog entry not cleared", "id", entry.ID, "error", err)
	}
}

func (s *ProvisioningService) markFailed(entry *domain.PendingUserSync, cause error) {
	if s.pending == nil || entry.ID == 0 {
		return
	}
	if err := s.pending.MarkFailed(entry, cause.Error()); err != nil {
		slog.Error("user sync: failure not recorded", "id", entry.ID, "error", err)
	}
}

// markAllFailed hält am gesamten Rückstand fest, warum der Server nicht
// erreichbar war - sonst stünde er ohne Ursache da.
func (s *ProvisioningService) markAllFailed(entries []domain.PendingUserSync, cause error) {
	for i := range entries {
		s.markFailed(&entries[i], cause)
	}
}

// PendingUserSyncs liefert den Rückstand eines Servers für die Anzeige.
func (s *ProvisioningService) PendingUserSyncs(serverID uint) ([]domain.PendingUserSync, error) {
	if s.pending == nil {
		return nil, nil
	}
	return s.pending.ForServer(serverID)
}
