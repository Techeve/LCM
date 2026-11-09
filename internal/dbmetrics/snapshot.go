package dbmetrics

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"LCM/internal/version"
)

// Snapshot ist die Momentaufnahme hinter GET /api/v1/system/db und /metrics.
type Snapshot struct {
	TakenAt             time.Time         `json:"taken_at"`
	StartedAt           time.Time         `json:"started_at"`
	Version             string            `json:"version"`
	File                FileStats         `json:"file"`
	LockWait            Histogram         `json:"lock_wait"`
	Transactions        Histogram         `json:"transactions"`
	StatementBoundsMS   []float64         `json:"statement_bounds_ms"`
	Statements          []StatementStats  `json:"statements"`
	Slowest             []SlowStatement   `json:"slowest"`
	LongestTransactions []LongTransaction `json:"longest_transactions"`
	Errors              ErrorCounts       `json:"errors"`
	RowsWritten         uint64            `json:"rows_written"`
	Operations          []Operation       `json:"operations"`
	TableRows           TableRows         `json:"table_rows"`
	Process             ProcessStats      `json:"process"`
	Pool                PoolStats         `json:"pool"`
}

// FileStats beschreibt Datei und WAL.
type FileStats struct {
	DBBytes       int64  `json:"db_bytes"`
	WALBytes      int64  `json:"wal_bytes"`
	PageSize      int64  `json:"page_size"`
	PageCount     int64  `json:"page_count"`
	FreelistCount int64  `json:"freelist_count"`
	Error         string `json:"error,omitempty"`
}

// StatementStats ist das Histogramm einer Tabelle und Art.
type StatementStats struct {
	Table string `json:"table"`
	Kind  string `json:"kind"`
	Histogram
}

// ErrorCounts zählt Fehler durch Sperren.
type ErrorCounts struct {
	Busy   uint64 `json:"busy"`   // SQLITE_BUSY, „database is locked"
	Locked uint64 `json:"locked"` // SQLITE_LOCKED, „database table is locked"
}

// ProcessStats beschreibt den LCM-Prozess.
type ProcessStats struct {
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64 `json:"heap_sys_bytes"`
	Goroutines     int    `json:"goroutines"`
	OpenFiles      int    `json:"open_files"` // -1: nicht ermittelbar
	GCRuns         uint32 `json:"gc_runs"`
}

// PoolStats ist die Statistik des Verbindungspools.
type PoolStats struct {
	Open      int     `json:"open"`
	InUse     int     `json:"in_use"`
	Idle      int     `json:"idle"`
	WaitCount int64   `json:"wait_count"`
	WaitMS    float64 `json:"wait_ms"`
	MaxOpen   int     `json:"max_open"`
}

// Reporter stellt Momentaufnahmen einer Datenbank zusammen.
type Reporter struct {
	c    *Collector
	db   *gorm.DB
	path string // Datei der Datenbank, leer bei :memory:
}

// NewReporter verbindet eine Sammelstelle mit ihrer Datenbank.
func NewReporter(c *Collector, db *gorm.DB, path string) *Reporter {
	return &Reporter{c: c, db: db, path: path}
}

// Snapshot liest alle Zahlen. Die PRAGMAs lesen nur den Kopf der Datei und
// kosten nichts.
func (r *Reporter) Snapshot() Snapshot {
	snap := r.c.snapshot()
	snap.Version = version.Version
	snap.File = r.fileStats()
	snap.Process = processStats()
	if sqlDB, err := r.db.DB(); err == nil {
		s := sqlDB.Stats()
		snap.Pool = PoolStats{
			Open: s.OpenConnections, InUse: s.InUse, Idle: s.Idle,
			WaitCount: s.WaitCount, WaitMS: msOf(s.WaitDuration), MaxOpen: s.MaxOpenConnections,
		}
	}
	return snap
}

func (r *Reporter) fileStats() FileStats {
	var fs FileStats
	var err error
	fs.PageCount, fs.FreelistCount, fs.PageSize, err = PageStats(r.db)
	if r.path != "" {
		fs.DBBytes = fileSize(r.path)
		fs.WALBytes = fileSize(r.path + "-wal")
	}
	if err != nil {
		fs.Error = err.Error()
	}
	return fs
}

// PageStats liest Seitenzahl, freie Seiten und Seitengröße.
func PageStats(db *gorm.DB) (pages, freePages, pageSize int64, err error) {
	err = errors.Join(
		db.Raw("PRAGMA page_count").Scan(&pages).Error,
		db.Raw("PRAGMA freelist_count").Scan(&freePages).Error,
		db.Raw("PRAGMA page_size").Scan(&pageSize).Error,
	)
	return pages, freePages, pageSize, err
}

// countDelay hält die erste Zählung aus dem Start heraus, wo LCM ohnehin
// viel liest.
const countDelay = 10 * time.Minute

// countTimeout begrenzt count(*) je Tabelle.
const countTimeout = 30 * time.Second

// CountTablesEvery zählt die Zeilen aller Tabellen, zuerst nach zehn
// Minuten, dann im Takt every. count(*) liest den kleinsten Index einer
// Tabelle ganz, das ist auf großen Tabellen spürbar: Es läuft je Tabelle für
// sich mit Zeitgrenze, am rohen Pool vorbei, damit die Zählung nicht in den
// eigenen Histogrammen landet. Ihre Dauer steht in TableRows.
func (r *Reporter) CountTablesEvery(ctx context.Context, every time.Duration) {
	timer := time.NewTimer(countDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if sqlDB, err := r.db.DB(); err == nil {
			r.c.setTableRows(countTables(ctx, sqlDB))
		}
		timer.Reset(every)
	}
}

func countTables(ctx context.Context, db *sql.DB) TableRows {
	started := time.Now()
	tables := map[string]int64{}
	for _, name := range tableNames(ctx, db) {
		cctx, cancel := context.WithTimeout(ctx, countTimeout)
		var n int64
		err := db.QueryRowContext(cctx, `SELECT count(*) FROM "`+strings.ReplaceAll(name, `"`, `""`)+`"`).Scan(&n)
		cancel()
		if err == nil {
			tables[name] = n
		}
	}
	return TableRows{CountedAt: started, TookMS: msOf(time.Since(started)), Tables: tables}
}

func tableNames(ctx context.Context, db *sql.DB) []string {
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			names = append(names, name)
		}
	}
	return names
}

// snapshot kopiert die Zahlen der Sammelstelle.
func (c *Collector) snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap := Snapshot{
		TakenAt:             time.Now(),
		StartedAt:           c.started,
		LockWait:            c.lockWait.clone(),
		Transactions:        c.txHold.clone(),
		StatementBoundsMS:   StatementBounds,
		Slowest:             slices.Clone(c.slowest),
		LongestTransactions: slices.Clone(c.longestTx),
		Errors:              ErrorCounts{Busy: c.busy.Load(), Locked: c.locked.Load()},
		RowsWritten:         c.rowsWritten.Load(),
		Operations:          slices.Clone(c.operations),
		TableRows:           c.tableRows,
	}
	for key, h := range c.statements {
		stats := StatementStats{Table: key.table, Kind: key.kind, Histogram: h.clone()}
		stats.Bounds = nil // stehen einmal in statement_bounds_ms
		snap.Statements = append(snap.Statements, stats)
	}
	slices.SortFunc(snap.Statements, func(a, b StatementStats) int {
		return strings.Compare(a.Table+" "+a.Kind, b.Table+" "+b.Kind)
	})
	return snap
}

func processStats() ProcessStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return ProcessStats{
		HeapAllocBytes: m.HeapAlloc, HeapSysBytes: m.HeapSys,
		Goroutines: runtime.NumGoroutine(), OpenFiles: openFiles(), GCRuns: m.NumGC,
	}
}

// openFiles zählt die offenen Dateien des Prozesses (Linux: /proc, sonst
// /dev/fd).
func openFiles() int {
	for _, dir := range []string{"/proc/self/fd", "/dev/fd"} {
		if entries, err := os.ReadDir(dir); err == nil {
			return len(entries)
		}
	}
	return -1
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
