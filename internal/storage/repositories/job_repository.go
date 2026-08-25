package repositories

import (
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// JobRepository verwaltet die Job-Historie (Protokollierungspflicht).
type JobRepository struct {
	db *gorm.DB
}

func NewJobRepository(db *gorm.DB) *JobRepository {
	return &JobRepository{db: db}
}

func (r *JobRepository) Create(job *domain.Job) error {
	return r.db.Create(job).Error
}

func (r *JobRepository) Update(job *domain.Job) error {
	return r.db.Save(job).Error
}

func (r *JobRepository) FindByID(scope AccessScope, id string) (*domain.Job, error) {
	var job domain.Job
	q := r.db.Preload("Server")
	if !scope.All {
		// Jobs ohne Server (System-Jobs) sind nur für Admins sichtbar.
		q = q.Where("jobs.server_id IN (SELECT sgs.server_id FROM server_group_servers sgs"+
			" JOIN server_group_managers sgm ON sgm.server_group_id = sgs.server_group_id"+
			" WHERE sgm.user_id = ?)", scope.UserID)
	}
	if err := q.First(&job, "jobs.id = ?", id).Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}

// JobFilter bündelt die Filter- und Pagination-Parameter der Job-Historie.
type JobFilter struct {
	ServerID    uint
	Status      string
	Type        string
	NameQuery   string     // LIKE %…% auf den Job-Namen
	TriggeredBy string     // exakter Auslöser
	HideHealth  bool       // Health-Check-Jobs ausblenden
	From        *time.Time // started_at >= From
	To          *time.Time // started_at < To (exklusiv)
	Limit       int
	Offset      int
}

// HistoryFiltered liefert die gefilterte, seitenweise Job-Historie samt
// Gesamtanzahl (für die Pagination). Neueste zuerst.
func (r *JobRepository) HistoryFiltered(scope AccessScope, f JobFilter) ([]domain.Job, int64, error) {
	apply := func(q *gorm.DB) *gorm.DB {
		if f.ServerID > 0 {
			q = q.Where("jobs.server_id = ?", f.ServerID)
		}
		if !scope.All {
			q = q.Where("jobs.server_id IN (SELECT sgs.server_id FROM server_group_servers sgs"+
				" JOIN server_group_managers sgm ON sgm.server_group_id = sgs.server_group_id"+
				" WHERE sgm.user_id = ?)", scope.UserID)
		}
		if f.Status != "" {
			q = q.Where("jobs.status = ?", f.Status)
		}
		if f.Type != "" {
			q = q.Where("jobs.type = ?", f.Type)
		}
		if f.HideHealth {
			q = q.Where("jobs.type <> ?", domain.RuleTypeHealth)
		}
		if f.NameQuery != "" {
			q = q.Where("jobs.name LIKE ?", "%"+f.NameQuery+"%")
		}
		if f.TriggeredBy != "" {
			q = q.Where("jobs.triggered_by = ?", f.TriggeredBy)
		}
		if f.From != nil {
			q = q.Where("jobs.started_at >= ?", *f.From)
		}
		if f.To != nil {
			q = q.Where("jobs.started_at < ?", *f.To)
		}
		return q
	}

	var total int64
	if err := apply(r.db.Model(&domain.Job{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	q := apply(r.db.Model(&domain.Job{})).Preload("Server").Order("jobs.created_at DESC, jobs.rowid DESC")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	var jobs []domain.Job
	if err := q.Find(&jobs).Error; err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}

// FilterOptions liefert die distinkten Typen und Auslöser der (sichtbaren)
// Jobs - zum Befüllen der Filter-Dropdowns.
func (r *JobRepository) FilterOptions(scope AccessScope, serverID uint) (types, triggeredBy []string, err error) {
	base := func() *gorm.DB {
		q := r.db.Model(&domain.Job{})
		if serverID > 0 {
			q = q.Where("jobs.server_id = ?", serverID)
		}
		if !scope.All {
			q = q.Where("jobs.server_id IN (SELECT sgs.server_id FROM server_group_servers sgs"+
				" JOIN server_group_managers sgm ON sgm.server_group_id = sgs.server_group_id"+
				" WHERE sgm.user_id = ?)", scope.UserID)
		}
		return q
	}
	if err = base().Distinct().Order("type").Pluck("type", &types).Error; err != nil {
		return nil, nil, err
	}
	if err = base().Where("triggered_by <> ''").Distinct().Order("triggered_by").
		Pluck("triggered_by", &triggeredBy).Error; err != nil {
		return nil, nil, err
	}
	return types, triggeredBy, nil
}

// LastFinishedForServer liefert den letzten abgeschlossenen Job eines
// Servers (für die Ampel-Bewertung); nil wenn keiner existiert.
func (r *JobRepository) LastFinishedForServer(serverID uint) (*domain.Job, error) {
	var job domain.Job
	err := r.db.Where("server_id = ? AND status IN ?", serverID,
		[]string{domain.JobStatusSuccess, domain.JobStatusFailed, domain.JobStatusAborted}).
		Order("created_at DESC, rowid DESC").First(&job).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}

// HasRunningForServer meldet, ob auf dem Server aktuell ein Job läuft
// (Concurrency Control: überlappende Jobs werden blockiert).
func (r *JobRepository) HasRunningForServer(serverID uint) (bool, error) {
	var n int64
	err := r.db.Model(&domain.Job{}).
		Where("server_id = ? AND status = ?", serverID, domain.JobStatusRunning).
		Count(&n).Error
	return n > 0, err
}

// RunningForServer liefert den aktuell laufenden Job eines Servers -
// nil, wenn keiner läuft (für die Laufender-Job-Anzeige im Server-Detail).
func (r *JobRepository) RunningForServer(serverID uint) (*domain.Job, error) {
	var job domain.Job
	err := r.db.Where("server_id = ? AND status = ?", serverID, domain.JobStatusRunning).
		Order("created_at DESC, rowid DESC").First(&job).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}

// FindUnfinished liefert alle Jobs, die noch als laufend/wartend markiert
// sind - nach einem Dienst-Neustart sind das verwaiste Einträge (die
// zugehörigen Goroutinen existieren nicht mehr).
func (r *JobRepository) FindUnfinished() ([]domain.Job, error) {
	var jobs []domain.Job
	err := r.db.Where("status IN ?",
		[]string{domain.JobStatusPending, domain.JobStatusRunning}).Find(&jobs).Error
	return jobs, err
}

// FindRunning liefert alle aktuell laufenden Jobs (Job-Watchdog-Kandidaten).
// Der Laufzeit-Vergleich passiert bewusst in Go: ein SQL-Stringvergleich auf
// started_at wäre bei gemischten Zeitzonen-Offsets (Sommer-/Winterzeit)
// nicht verlässlich.
func (r *JobRepository) FindRunning() ([]domain.Job, error) {
	var jobs []domain.Job
	err := r.db.Where("status = ?", domain.JobStatusRunning).Find(&jobs).Error
	return jobs, err
}

// DeleteOlderThan löscht Jobs, die vor dem Stichtag abgeschlossen wurden
// (Log Retention). Liefert die Anzahl gelöschter Einträge.
func (r *JobRepository) DeleteOlderThan(cutoff time.Time) (int64, error) {
	res := r.db.Where("created_at < ? AND status NOT IN ?", cutoff,
		[]string{domain.JobStatusPending, domain.JobStatusRunning}).
		Delete(&domain.Job{})
	return res.RowsAffected, res.Error
}
