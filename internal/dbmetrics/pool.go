package dbmetrics

import (
	"context"
	"database/sql"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// callerDepth begrenzt den Aufrufstapel, der je Transaktion gemerkt wird.
// Aufgelöst wird er nur, wenn die Transaktion in die Bestenliste kommt.
const callerDepth = 16

// counting reicht Anweisungen durch und zählt die Fehler durch Sperren. Hier
// und nicht in den GORM-Callbacks, weil auch BEGIN und COMMIT scheitern
// können und jeder Fehler genau einmal zählen soll.
type counting struct {
	inner gorm.ConnPool
	c     *Collector
}

func (w counting) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	stmt, err := w.inner.PrepareContext(ctx, query)
	w.c.countError(err)
	return stmt, err
}

func (w counting) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	res, err := w.inner.ExecContext(ctx, query, args...)
	w.c.countError(err)
	return res, err
}

func (w counting) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := w.inner.QueryContext(ctx, query, args...)
	w.c.countError(err)
	return rows, err
}

func (w counting) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	row := w.inner.QueryRowContext(ctx, query, args...)
	w.c.countError(row.Err())
	return row
}

// pool umhüllt den Verbindungspool. BeginTx misst die Wartezeit: Mit
// _txlock=immediate holt BEGIN die Schreibsperre, die Dauer ist also genau
// die Zeit, die ein Schreiber auf andere gewartet hat (busy_timeout).
type pool struct {
	counting
	db *sql.DB
}

// BeginTx erfüllt gorm.ConnPoolBeginner.
func (p *pool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	var pcs [callerDepth]uintptr
	n := runtime.Callers(2, pcs[:])
	start := time.Now()
	tx, err := p.db.BeginTx(ctx, opts)
	begun := time.Now()
	p.c.observeLockWait(begun.Sub(start), begun)
	if err != nil {
		p.c.countError(err)
		return nil, err
	}
	return &txConn{counting: counting{inner: tx, c: p.c}, tx: tx, db: p.db, begun: begun, pcs: pcs[:n]}, nil
}

// GetDBConn erfüllt gorm.GetDBConnector, damit db.DB() weiter funktioniert.
func (p *pool) GetDBConn() (*sql.DB, error) { return p.db, nil }

// txConn ist eine laufende Transaktion. Commit und Rollback messen, wie lange
// sie die Sperre hielt - nur einmal: Scheitert Commit, ruft GORM danach noch
// Rollback auf.
type txConn struct {
	counting
	tx       *sql.Tx
	db       *sql.DB
	begun    time.Time
	pcs      []uintptr
	finished bool
}

// Commit erfüllt gorm.TxCommitter.
func (t *txConn) Commit() error {
	err := t.tx.Commit()
	t.c.countError(err)
	t.finish()
	return err
}

// Rollback erfüllt gorm.TxCommitter.
func (t *txConn) Rollback() error {
	err := t.tx.Rollback()
	t.finish()
	return err
}

// GetDBConn erfüllt gorm.GetDBConnector.
func (t *txConn) GetDBConn() (*sql.DB, error) { return t.db, nil }

func (t *txConn) finish() {
	if t.finished {
		return
	}
	t.finished = true
	now := time.Now()
	t.c.observeTransaction(now.Sub(t.begun), now, func() string { return callerOf(t.pcs) })
}

// callerOf liefert die erste Aufrufstelle außerhalb von GORM, database/sql
// und diesem Paket - dort, wo die Transaktion im LCM-Code beginnt.
func callerOf(pcs []uintptr) string {
	frames := runtime.CallersFrames(pcs)
	for {
		f, more := frames.Next()
		if !isPlumbing(f) {
			return shortFunc(f.Function) + " (" + shortFile(f.File) + ":" + strconv.Itoa(f.Line) + ")"
		}
		if !more {
			return "unbekannt"
		}
	}
}

func isPlumbing(f runtime.Frame) bool {
	if strings.HasSuffix(f.File, "_test.go") {
		return false // Tests dieses Pakets sind Aufrufer wie jeder andere
	}
	for _, prefix := range []string{"gorm.io/", "database/sql.", "LCM/internal/dbmetrics.", "runtime."} {
		if strings.HasPrefix(f.Function, prefix) {
			return true
		}
	}
	return false
}

func shortFunc(fn string) string { return fn[strings.LastIndex(fn, "/")+1:] }

// shortFile kürzt den Pfad auf Paketordner und Datei.
func shortFile(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}
