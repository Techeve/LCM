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

// JobSort sind die erlaubten Sortierungen der Job-Liste.
const (
	JobSortStartedDesc  = "-started_at" // neueste zuerst (Vorgabe)
	JobSortStartedAsc   = "started_at"
	JobSortDurationDesc = "-duration" // längste Laufzeit zuerst, unfertige zuletzt
)

// jobTimeExpr ist der Zeitpunkt, an dem ein Job „stattfand": der Start, bei
// wartenden und abgewiesenen Jobs (nie gestartet) die Anlage. Dazu gibt es
// einen passenden Ausdrucks-Index (siehe storage.Migrate).
const jobTimeExpr = "COALESCE(jobs.started_at, jobs.created_at)"

// JobFilter bündelt die Filter-, Sortier- und Pagination-Parameter der Job-Liste.
type JobFilter struct {
	ServerID    uint
	Status      string     // Kommaliste von Job-Zuständen
	Type        string     // Kommaliste von Job-Typen
	NameQuery   string     // LIKE %…% auf den Job-Namen
	TriggeredBy string     // exakter Auslöser
	HideHealth  bool       // Health-Check-Jobs ausblenden
	From        *time.Time // Zeitpunkt >= From
	To          *time.Time // Zeitpunkt < To (exklusiv)
	Sort        string     // JobSort*, leer = JobSortStartedDesc
	Limit       int
	Offset      int
}

// HistoryFiltered liefert die gefilterte, seitenweise Job-Liste samt
// Gesamtanzahl (für die Pagination). Die Jobs tragen nur Kennung und Name ihres
// Servers.
func (r *JobRepository) HistoryFiltered(scope AccessScope, f JobFilter) ([]domain.Job, int64, error) {
	apply := func(q *gorm.DB) *gorm.DB {
		if f.ServerID > 0 {
			q = q.Where("jobs.server_id = ?", f.ServerID)
		}
		q = scope.scopeByServerColumn(q, "jobs.server_id")
		if statuses := splitList(f.Status); len(statuses) > 0 {
			q = q.Where("jobs.status IN ?", statuses)
		}
		if types := splitList(f.Type); len(types) > 0 {
			q = q.Where("jobs.type IN ?", types)
		}
		if f.HideHealth {
			q = q.Where("jobs.type <> ?", domain.RuleTypeHealth)
		}
		if f.NameQuery != "" {
			q = q.Where("jobs.name LIKE ? ESCAPE '\\'", likeContains(f.NameQuery))
		}
		if f.TriggeredBy != "" {
			q = q.Where("jobs.triggered_by = ?", f.TriggeredBy)
		}
		if f.From != nil {
			q = q.Where(jobTimeExpr+" >= ?", *f.From)
		}
		if f.To != nil {
			q = q.Where(jobTimeExpr+" < ?", *f.To)
		}
		return q
	}

	var total int64
	if err := apply(r.db.Model(&domain.Job{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	q := apply(r.db.Model(&domain.Job{})).Order(jobOrder(f.Sort)).
		Preload("Server", func(db *gorm.DB) *gorm.DB { return db.Select("id", "name") })
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

// jobOrder übersetzt eine Sortierung in ORDER BY; die Zeilenkennung hält die
// Reihenfolge beim Blättern stabil.
func jobOrder(sortBy string) string {
	switch sortBy {
	case JobSortStartedAsc:
		return jobTimeExpr + " ASC, jobs.rowid ASC"
	case JobSortDurationDesc:
		return "(julianday(jobs.finished_at) - julianday(jobs.started_at)) DESC, jobs.rowid DESC"
	default:
		return jobTimeExpr + " DESC, jobs.rowid DESC"
	}
}

// RecentFinished liefert die zuletzt beendeten Jobs (Erfolg, Fehler, Abbruch)
// ohne Health-Checks, neueste zuerst - für das Dashboard. Ohne Ausgabe: Die
// ist groß, verschlüsselt, und die Übersicht zeigt sie nicht. Der Server kommt
// per Preload mit, also ohne eine Abfrage je Zeile.
func (r *JobRepository) RecentFinished(scope AccessScope, limit int) ([]domain.Job, error) {
	q := scope.scopeByServerColumn(r.db.Omit("output").Preload("Server"), "jobs.server_id").
		Where("jobs.status IN ? AND jobs.type <> ?",
			[]string{domain.JobStatusSuccess, domain.JobStatusFailed, domain.JobStatusAborted},
			domain.RuleTypeHealth)
	var jobs []domain.Job
	err := q.Order("jobs.finished_at DESC, jobs.rowid DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
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

// ReplaceOutputOlderThan ersetzt die Ausgabe abgeschlossener Jobs der
// genannten Typen, die vor cutoff angelegt wurden, durch note. Der Eintrag
// selbst bleibt stehen.
//
// note wird bewusst UNVERSCHLÜSSELT geschrieben: Er ist ein fester Hinweis
// ohne Inhalt, den der Serializer als Klartext liest - und nur so lässt sich
// ein bereits ersetzter Eintrag im WHERE erkennen. Sonst schriebe jeder
// nächtliche Lauf alle Routine-Jobs der letzten Wochen erneut.
func (r *JobRepository) ReplaceOutputOlderThan(types []string, cutoff time.Time, note string) (int64, error) {
	res := r.db.Exec("UPDATE jobs SET output = ? WHERE type IN ? AND created_at < ? AND status NOT IN ? AND output != '' AND output != ?",
		note, types, cutoff, []string{domain.JobStatusPending, domain.JobStatusRunning}, note)
	return res.RowsAffected, res.Error
}

// DeleteOlderThan löscht Jobs, die vor dem Stichtag abgeschlossen wurden
// (Log Retention). Liefert die Anzahl gelöschter Einträge.
func (r *JobRepository) DeleteOlderThan(cutoff time.Time) (int64, error) {
	res := r.db.Where("created_at < ? AND status NOT IN ?", cutoff,
		[]string{domain.JobStatusPending, domain.JobStatusRunning}).
		Delete(&domain.Job{})
	return res.RowsAffected, res.Error
}
