package repositories

import (
	"errors"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// AgentUpdateRepository verwaltet die Agent-Update-Regeln der Gruppen: eine
// Zeile je Gruppe, ohne Zeile gilt „aus“.
type AgentUpdateRepository struct {
	db *gorm.DB
}

func NewAgentUpdateRepository(db *gorm.DB) *AgentUpdateRepository {
	return &AgentUpdateRepository{db: db}
}

// Get liefert die Regel der Gruppe, ohne gespeicherte die Vorgabe (aus).
func (r *AgentUpdateRepository) Get(groupID uint) (*domain.AgentUpdatePolicy, error) {
	var p domain.AgentUpdatePolicy
	err := r.db.Where("group_id = ?", groupID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.DefaultAgentUpdatePolicy(groupID), nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Enabled liefert alle eingeschalteten Regeln - der Zeitplaner legt daraus die
// Fenster an.
func (r *AgentUpdateRepository) Enabled() ([]domain.AgentUpdatePolicy, error) {
	var list []domain.AgentUpdatePolicy
	err := r.db.Where("mode = ?", domain.AgentUpdateAuto).Order("group_id").Find(&list).Error
	return list, err
}

// SaveSettings schreibt Schalter, Fenster und Staffelung der Regel. Den Stand
// des letzten Laufs lässt es unberührt - außer der Sperre: Wer die Regel
// bearbeitet, hat sich damit das Ergebnis angesehen, der Lauf darf es wieder
// versuchen.
func (r *AgentUpdateRepository) SaveSettings(p *domain.AgentUpdatePolicy) error {
	current, err := r.Get(p.GroupID)
	if err != nil {
		return err
	}
	current.Mode, current.Staged = p.Mode, p.Staged
	current.CronExpr, current.WindowMinutes = p.CronExpr, p.WindowMinutes
	current.BlockedFor = ""
	if err := r.db.Save(current).Error; err != nil {
		return err
	}
	*p = *current
	return nil
}

// RecordRun hält den Stand eines Laufs fest (Ergebnis, geprüfte Version, Sperre)
// und rührt die Einstellungen nicht an: Wer sie während des Laufs ändert, wird
// nicht mit dem alten Stand überschrieben. Fehlt die Zeile (Lauf von Hand für
// eine Gruppe ohne gespeicherte Regel), entsteht sie mit der Vorgabe „aus“.
func (r *AgentUpdateRepository) RecordRun(p *domain.AgentUpdatePolicy) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		row := domain.DefaultAgentUpdatePolicy(p.GroupID)
		if err := tx.Where("group_id = ?", p.GroupID).FirstOrCreate(row).Error; err != nil {
			return err
		}
		return tx.Model(row).Updates(map[string]any{
			"last_run_at": p.LastRunAt, "last_result": p.LastResult,
			"proven_for": p.ProvenFor, "proven_version": p.ProvenVersion, "blocked_for": p.BlockedFor,
		}).Error
	})
}

// DeleteForGroup entfernt die Regel einer aufgelösten Gruppe.
func (r *AgentUpdateRepository) DeleteForGroup(groupID uint) error {
	return r.db.Where("group_id = ?", groupID).Delete(&domain.AgentUpdatePolicy{}).Error
}
