package services

import (
	"log/slog"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// AuditService schreibt und prüft das manipulationssichere Audit-Log.
// JEDE Änderung, jeder Cronjob und jede manuelle Aktion wird hier
// protokolliert (Protokollierungspflicht).
type AuditService struct {
	audit *repositories.AuditRepository
}

func NewAuditService(audit *repositories.AuditRepository) *AuditService {
	return &AuditService{audit: audit}
}

// Log protokolliert eine Aktion. Fehler werden geloggt, aber nicht
// zurückgegeben - ein Audit-Schreibfehler darf die eigentliche Aktion
// nicht rückwirkend scheitern lassen (sie ist bereits passiert).
func (s *AuditService) Log(actor, action, entity string, entityID uint, details string) {
	entry := &domain.AuditLog{
		Actor:    actor,
		Action:   action,
		Entity:   entity,
		EntityID: entityID,
		Details:  details,
	}
	if err := s.audit.Append(entry); err != nil {
		slog.Error("writing audit log failed",
			"action", action, "entity", entity, "error", err)
	}
}

// Recent liefert die neuesten Einträge für die Audit-Ansicht.
func (s *AuditService) Recent(limit int) ([]domain.AuditLog, error) {
	return s.audit.FindRecent(limit)
}

// VerifyChainOnStartup prüft die Hash-Kette beim Anwendungsstart.
// Eine gebrochene Kette bedeutet: Jemand hat Einträge direkt in der
// Datenbank gelöscht oder verändert - es wird laut gewarnt.
func (s *AuditService) VerifyChainOnStartup() {
	brokenID, err := s.audit.VerifyChain()
	switch {
	case err != nil:
		slog.Error("audit chain could not be verified", "error", err)
	case brokenID != "":
		slog.Error("WARNING: audit log was tampered with - hash chain broken",
			"first_tampered_entry", brokenID)
	default:
		slog.Info("audit log hash chain verified - no tampering detected")
	}
}
