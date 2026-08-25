package repositories

import (
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// AuditRepository verwaltet das manipulationssichere Audit-Log.
type AuditRepository struct {
	db *gorm.DB
	// appendMu serialisiert das Anhängen: Die Hash-Chain liest den letzten
	// Eintrag und schreibt darauf aufbauend - zwei gleichzeitige Append
	// würden dieselbe seq/prev_hash lesen und beim Upgrade auf den
	// Schreib-Lock mit SQLITE_BUSY kollidieren (genau der verlorene
	// Audit-Eintrag aus R2-011). Ein prozessweiter Mutex macht die Kette
	// konsistent UND vermeidet das Lock-Rennen; busy_timeout deckt den
	// (hier nicht auftretenden) Mehr-Prozess-Fall ab.
	appendMu sync.Mutex
}

func NewAuditRepository(db *gorm.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// Append hängt einen Eintrag an die Hash-Chain an. PrevHash/Hash werden
// hier gesetzt; die Serialisierung über den Mutex + eine Transaktion hält
// die Kette auch bei parallelen Writes intakt (R2-011).
func (r *AuditRepository) Append(entry *domain.AuditLog) error {
	r.appendMu.Lock()
	defer r.appendMu.Unlock()
	// Zusätzlicher Schutz gegen ein verlorenes Lock-Rennen: bei SQLITE_BUSY
	// kurz warten und erneut versuchen, statt den Eintrag zu verwerfen.
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = r.appendOnce(entry); err == nil || !isSQLiteBusy(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	return err
}

// isSQLiteBusy erkennt das „database is locked"-Rennen (SQLITE_BUSY).
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "sqlite_busy") || strings.Contains(m, "database is locked")
}

func (r *AuditRepository) appendOnce(entry *domain.AuditLog) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var last domain.AuditLog
		// Seq ist der monotone Ketten-Anker (UUID-PKs sind nicht sortierbar).
		err := tx.Order("seq DESC").First(&last).Error
		switch {
		case err == gorm.ErrRecordNotFound:
			entry.PrevHash = domain.AuditChainStart
			entry.Seq = 1
		case err != nil:
			return err
		default:
			entry.PrevHash = last.Hash
			entry.Seq = last.Seq + 1
		}
		if entry.CreatedAt.IsZero() {
			// CreatedAt fließt in den Hash ein - vor ComputeHash setzen.
			entry.CreatedAt = tx.NowFunc()
		}
		entry.Hash = entry.ComputeHash()
		return tx.Create(entry).Error
	})
}

// FindRecent liefert die neuesten Einträge (für die Audit-Ansicht).
func (r *AuditRepository) FindRecent(limit int) ([]domain.AuditLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var entries []domain.AuditLog
	if err := r.db.Order("seq DESC").Limit(limit).Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// VerifyChain prüft die komplette Hash-Kette in Batches und liefert die
// ID (UUID) des ersten manipulierten Eintrags ("" = Kette intakt). Die
// Kette wird über Seq (monotone Einfügereihenfolge) durchlaufen, da UUIDs
// nicht sortierbar sind.
func (r *AuditRepository) VerifyChain() (string, error) {
	prevHash := domain.AuditChainStart
	var lastSeq uint
	for {
		var batch []domain.AuditLog
		if err := r.db.Where("seq > ?", lastSeq).Order("seq").Limit(500).Find(&batch).Error; err != nil {
			return "", err
		}
		if len(batch) == 0 {
			return "", nil
		}
		for _, e := range batch {
			if e.PrevHash != prevHash || e.ComputeHash() != e.Hash {
				return e.ID, nil
			}
			prevHash = e.Hash
			lastSeq = e.Seq
		}
	}
}
