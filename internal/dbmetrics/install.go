package dbmetrics

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

const startKey = "dbmetrics:start"

// Install hängt die Messung an eine Verbindung: den Wrapper um den Pool für
// Sperre, Transaktionen und Fehler, die Callbacks für die Dauer der
// Anweisungen. Ein Eingriff in die Repositories ist nicht nötig.
func Install(db *gorm.DB, c *Collector) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	wrapped := &pool{counting: counting{inner: db.ConnPool, c: c}, db: sqlDB}
	db.ConnPool = wrapped
	db.Statement.ConnPool = wrapped
	return registerCallbacks(db, c)
}

func registerCallbacks(db *gorm.DB, c *Collector) error {
	before := func(tx *gorm.DB) { tx.InstanceSet(startKey, time.Now()) }
	after := func(tx *gorm.DB) { c.afterStatement(tx) }
	cb := db.Callback()
	return errors.Join(
		cb.Create().Before("gorm:create").Register("dbmetrics:before_create", before),
		cb.Create().After("gorm:create").Register("dbmetrics:after_create", after),
		cb.Query().Before("gorm:query").Register("dbmetrics:before_query", before),
		cb.Query().After("gorm:query").Register("dbmetrics:after_query", after),
		cb.Update().Before("gorm:update").Register("dbmetrics:before_update", before),
		cb.Update().After("gorm:update").Register("dbmetrics:after_update", after),
		cb.Delete().Before("gorm:delete").Register("dbmetrics:before_delete", before),
		cb.Delete().After("gorm:delete").Register("dbmetrics:after_delete", after),
		cb.Row().Before("gorm:row").Register("dbmetrics:before_row", before),
		cb.Row().After("gorm:row").Register("dbmetrics:after_row", after),
		cb.Raw().Before("gorm:raw").Register("dbmetrics:before_raw", before),
		cb.Raw().After("gorm:raw").Register("dbmetrics:after_raw", after),
	)
}

// afterStatement trägt Dauer und geschriebene Zeilen einer Anweisung ein.
func (c *Collector) afterStatement(tx *gorm.DB) {
	v, ok := tx.InstanceGet(startKey)
	start, isTime := v.(time.Time)
	if !ok || !isTime {
		return
	}
	now := time.Now()
	sql := tx.Statement.SQL.String()
	if sql == "" {
		return // nichts ausgeführt, etwa nach einem Fehler davor
	}
	kind := statementKind(sql)
	table := tx.Statement.Table
	if table == "" {
		table = tableOf(sql)
	}
	c.observeStatement(table, kind, sql, now.Sub(start), now)
	if writes(kind) && tx.Error == nil && tx.RowsAffected > 0 {
		c.rowsWritten.Add(uint64(tx.RowsAffected))
	}
}
