package repositories

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"LCM/internal/core/domain"
)

// AlertRepository verwaltet die Alarm-Regeln und die Alarm-Historie
// (ausgelöste Events).
type AlertRepository struct {
	db *gorm.DB
}

func NewAlertRepository(db *gorm.DB) *AlertRepository {
	return &AlertRepository{db: db}
}

// ---- Alarm-Regeln ------------------------------------------------------------

func (r *AlertRepository) Create(rule *domain.AlertRule) error {
	return r.db.Create(rule).Error
}

func (r *AlertRepository) Update(rule *domain.AlertRule) error {
	// Assoziationen beim Speichern AUSLASSEN und die Gruppenzuordnung danach
	// gezielt setzen: Ein Save mit Assoziationen schrieb die geladenen
	// Gruppen einfach zurück - die Zuordnung ließ sich dadurch weder ändern
	// noch lösen, der PATCH wurde mit 200 quittiert und still verworfen
	// (R2-030). Replace deckt beides ab, auch die leere Liste (= alle Server).
	if err := r.db.Omit(clause.Associations).Save(rule).Error; err != nil {
		return err
	}
	return r.db.Model(rule).Association("Groups").Replace(rule.Groups)
}

func (r *AlertRepository) Delete(id uint) error {
	// Gruppen-Zuordnung zuerst lösen: Die Verknüpfungstabelle hat einen
	// Fremdschlüssel auf die Regel - ohne das scheitert das Löschen jeder
	// Regel, die einer Gruppe zugeordnet ist.
	if err := r.db.Model(&domain.AlertRule{ID: id}).Association("Groups").Clear(); err != nil {
		return err
	}
	res := r.db.Delete(&domain.AlertRule{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *AlertRepository) FindByID(id uint) (*domain.AlertRule, error) {
	var rule domain.AlertRule
	if err := r.db.Preload("Groups").First(&rule, id).Error; err != nil {
		return nil, translate(err)
	}
	return &rule, nil
}

func (r *AlertRepository) FindAll() ([]domain.AlertRule, error) {
	var rules []domain.AlertRule
	if err := r.db.Preload("Groups").Order("name").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// FindEnabled liefert alle aktiven Alarm-Regeln (für die Auswertung).
func (r *AlertRepository) FindEnabled() ([]domain.AlertRule, error) {
	var rules []domain.AlertRule
	if err := r.db.Preload("Groups").Where("enabled = ?", true).Order("name").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// CountRulesUsingChannel zählt Regeln, die einen bestimmten Kanal referenzieren
// (für den Löschschutz eines Kanals).
func (r *AlertRepository) CountRulesUsingChannel(channelID uint) (int64, error) {
	var n int64
	err := r.db.Model(&domain.AlertRule{}).Where("channel_id = ?", channelID).Count(&n).Error
	return n, err
}

// ---- Alarm-Historie (Events) -------------------------------------------------

func (r *AlertRepository) CreateEvent(e *domain.AlertEvent) error {
	return r.db.Create(e).Error
}

// FindEvents liefert die jüngsten Alarm-Events (neueste zuerst).
// UpdateEventNotify trägt das Versand-Ergebnis eines Alarm-Events nach -
// der Versand läuft seit R2-033 asynchron, das Event existiert zu diesem
// Zeitpunkt bereits.
func (r *AlertRepository) UpdateEventNotify(id string, notified bool, notifyErr string) error {
	return r.db.Model(&domain.AlertEvent{}).Where("id = ?", id).
		Updates(map[string]any{"notified": notified, "notify_error": notifyErr}).Error
}

func (r *AlertRepository) FindEvents(limit int) ([]domain.AlertEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	var events []domain.AlertEvent
	if err := r.db.Order("created_at DESC").Limit(limit).Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// AlertEventFilter grenzt die Event-Historie ein (leer = alles).
type AlertEventFilter struct {
	Type     string
	Severity string
	ServerID uint
	Limit    int
	Offset   int
}

// FindEventsFiltered liefert Alarm-Events seitenweise mit Gesamtzahl.
// Vorher war die Historie auf die neuesten 200 gedeckelt und Paging/Filter
// wirkungslos - bei 677 Ereignissen waren 477 unerreichbar (R2-023).
func (r *AlertRepository) FindEventsFiltered(f AlertEventFilter) ([]domain.AlertEvent, int64, error) {
	q := r.db.Model(&domain.AlertEvent{})
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.Severity != "" {
		q = q.Where("severity = ?", f.Severity)
	}
	if f.ServerID != 0 {
		q = q.Where("server_id = ?", f.ServerID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	var events []domain.AlertEvent
	err := q.Order("created_at DESC").Limit(f.Limit).Offset(f.Offset).Find(&events).Error
	return events, total, err
}

// LastEventAt liefert den Zeitpunkt des jüngsten Events für (Regel, Server) -
// Grundlage der Cooldown-Entprellung. serverID darf nil sein (nicht
// serverbezogene Alarme). Nullzeit, wenn noch kein Event existiert.
func (r *AlertRepository) LastEventAt(ruleID uint, serverID *uint) (time.Time, error) {
	var e domain.AlertEvent
	q := r.db.Where("rule_id = ?", ruleID)
	if serverID == nil {
		q = q.Where("server_id IS NULL")
	} else {
		q = q.Where("server_id = ?", *serverID)
	}
	err := q.Order("created_at DESC").First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return e.CreatedAt, nil
}

// DeleteEventsOlderThan entfernt Alarm-Events vor dem Stichtag (Retention).
func (r *AlertRepository) DeleteEventsOlderThan(cutoff time.Time) (int64, error) {
	res := r.db.Where("created_at < ?", cutoff).Delete(&domain.AlertEvent{})
	return res.RowsAffected, res.Error
}
