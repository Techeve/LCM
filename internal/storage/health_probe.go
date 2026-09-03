package storage

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"LCM/internal/core/domain"
)

// ProbeWritable schreibt einen Zeitstempel in die Prüfzeile und meldet damit,
// ob die Datenbank Schreibvorgänge annimmt.
//
// Bewusst ein Schreibvorgang und kein Ping: Ein Ping beantwortet „ist die
// Verbindung offen?", nicht „kann ich arbeiten?". Im WAL-Modus sind das zwei
// verschiedene Fragen - Leser kommen durch, während die Schreibsperre bei
// einem anderen Vorgang liegt. Genau in dieser Lücke hat der Dienst im Test
// minutenlang „operational" gemeldet, ohne eine Zeile schreiben zu können
// (siehe domain.HealthProbe).
//
// Ein einziges Statement: Der Upsert legt die Zeile beim ersten Lauf an und
// aktualisiert sie danach. Kein Vorher-Lesen, kein Sonderfall.
//
// SkipDefaultTransaction, aus zwei Gründen. Der eine ist die Meldung: GORM
// wickelt jedes Schreiben in eine Transaktion, und scheitert die an einer
// gesperrten Datenbank, hängt der Rollback ein zweites „transaction has
// already been committed or rolled back" an den eigentlichen Fehler. Diese
// Meldung landet im Klartext in `systemctl status` - der einen Stelle, an der
// jemand um drei Uhr nachts schnell verstehen muss, was los ist. Dort gehört
// kein Rauschen hin. Der andere Grund ist der Preis: Die Prüfung läuft alle
// 45 Sekunden, für immer; ein einzelnes Upsert ist für sich atomar und
// braucht die Klammer nicht.
func ProbeWritable(ctx context.Context, db *gorm.DB) error {
	probe := domain.HealthProbe{ID: domain.HealthProbeID, CheckedAt: time.Now()}
	return db.WithContext(ctx).
		Session(&gorm.Session{SkipDefaultTransaction: true}).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"checked_at"}),
		}).Create(&probe).Error
}
