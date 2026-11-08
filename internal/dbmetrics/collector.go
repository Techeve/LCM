package dbmetrics

import (
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	slowestKept    = 50  // langsamste Anweisungen, je Text einmal
	longestTxKept  = 10  // längste Transaktionen, je Aufrufstelle einmal
	operationsKept = 100 // letzte Läufe (Checkpoint, VACUUM, Jobs ...)
)

// Default ist die Sammelstelle des Prozesses. storage.Open hängt sie an jede
// Verbindung; die Endpunkte lesen sie aus.
var Default = New()

// Collector sammelt alle Zahlen eines Prozesses.
type Collector struct {
	started     time.Time
	busy        atomic.Uint64
	locked      atomic.Uint64
	rowsWritten atomic.Uint64

	mu         sync.Mutex
	lockWait   Histogram
	txHold     Histogram
	statements map[statementKey]*Histogram
	slowest    []SlowStatement
	longestTx  []LongTransaction
	operations []Operation
	tableRows  TableRows
}

type statementKey struct{ table, kind string }

// SlowStatement ist eine der langsamsten Anweisungen, ohne Werte.
type SlowStatement struct {
	SQL   string    `json:"sql"`
	Table string    `json:"table"`
	Kind  string    `json:"kind"`
	MaxMS float64   `json:"max_ms"`
	At    time.Time `json:"at"`
}

// LongTransaction ist eine der längsten Transaktionen mit ihrer Aufrufstelle.
type LongTransaction struct {
	Caller string    `json:"caller"`
	MaxMS  float64   `json:"max_ms"`
	At     time.Time `json:"at"`
}

// Operation ist ein Lauf, der die Datenbank treibt oder anhält. RowsWritten
// zählt alle Zeilen, die während des Laufs geschrieben wurden, auch von
// anderen Schreibern - je Lauf trennen lässt sich das ohne Eingriff in die
// Repositories nicht.
type Operation struct {
	Kind        string    `json:"kind"` // checkpoint, vacuum, vacuum_into, job
	Name        string    `json:"name,omitempty"`
	Started     time.Time `json:"started"`
	DurationMS  float64   `json:"duration_ms"`
	RowsWritten uint64    `json:"rows_written"`
	Error       string    `json:"error,omitempty"`
}

// TableRows ist die letzte Zählung der Zeilen je Tabelle.
type TableRows struct {
	CountedAt time.Time        `json:"counted_at,omitzero"`
	TookMS    float64          `json:"took_ms"`
	Tables    map[string]int64 `json:"tables,omitempty"`
}

// New liefert eine leere Sammelstelle.
func New() *Collector {
	return &Collector{
		started:    time.Now(),
		lockWait:   newHistogram(LockWaitBounds),
		txHold:     newHistogram(StatementBounds),
		statements: map[statementKey]*Histogram{},
	}
}

// observeLockWait trägt die Dauer von BEGIN IMMEDIATE ein.
func (c *Collector) observeLockWait(d time.Duration, at time.Time) {
	c.mu.Lock()
	c.lockWait.observe(d, at)
	c.mu.Unlock()
}

// observeTransaction trägt ein, wie lange eine Transaktion die Sperre hielt.
func (c *Collector) observeTransaction(d time.Duration, at time.Time, caller func() string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.txHold.observe(d, at)
	ms := msOf(d)
	if !qualifies(len(c.longestTx), longestTxKept, func() float64 { return c.longestTx[len(c.longestTx)-1].MaxMS }, ms) {
		return
	}
	entry := LongTransaction{Caller: caller(), MaxMS: ms, At: at}
	c.longestTx = keepTop(c.longestTx, entry, longestTxKept,
		func(e LongTransaction) bool { return e.Caller == entry.Caller },
		func(e LongTransaction) float64 { return e.MaxMS })
}

// observeStatement trägt die Dauer einer Anweisung ein.
func (c *Collector) observeStatement(table, kind, sql string, d time.Duration, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := statementKey{table, kind}
	h := c.statements[key]
	if h == nil {
		fresh := newHistogram(StatementBounds)
		h = &fresh
		c.statements[key] = h
	}
	h.observe(d, at)
	ms := msOf(d)
	if !qualifies(len(c.slowest), slowestKept, func() float64 { return c.slowest[len(c.slowest)-1].MaxMS }, ms) {
		return
	}
	entry := SlowStatement{SQL: normalizeSQL(sql), Table: table, Kind: kind, MaxMS: ms, At: at}
	c.slowest = keepTop(c.slowest, entry, slowestKept,
		func(e SlowStatement) bool { return e.SQL == entry.SQL },
		func(e SlowStatement) float64 { return e.MaxMS })
}

// countError zählt Fehler, die von der Sperre kommen.
func (c *Collector) countError(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked") {
		c.busy.Add(1)
	}
	if strings.Contains(msg, "SQLITE_LOCKED") || strings.Contains(msg, "database table is locked") {
		c.locked.Add(1)
	}
}

// Track beginnt die Messung eines Laufs. Der Rückgabewert schließt sie ab und
// nimmt das Ergebnis des Laufs entgegen.
func (c *Collector) Track(kind, name string) func(error) {
	started := time.Now()
	rowsBefore := c.rowsWritten.Load()
	return func(err error) {
		op := Operation{
			Kind: kind, Name: name, Started: started,
			DurationMS:  msOf(time.Since(started)),
			RowsWritten: c.rowsWritten.Load() - rowsBefore,
		}
		if err != nil {
			op.Error = err.Error()
		}
		c.mu.Lock()
		c.operations = append(c.operations, op)
		if len(c.operations) > operationsKept {
			c.operations = slices.Delete(c.operations, 0, len(c.operations)-operationsKept)
		}
		c.mu.Unlock()
	}
}

// Track misst einen Lauf in der Sammelstelle des Prozesses.
func Track(kind, name string) func(error) { return Default.Track(kind, name) }

func (c *Collector) setTableRows(rows TableRows) {
	c.mu.Lock()
	c.tableRows = rows
	c.mu.Unlock()
}

// qualifies sagt, ob ein Wert in eine Bestenliste gehört, ohne sie zu
// verändern: Ist sie voll, muss er den bisher kleinsten übertreffen.
func qualifies(n, limit int, smallest func() float64, ms float64) bool {
	return n < limit || ms > smallest()
}

// keepTop trägt entry in die absteigend sortierte Liste ein. Gibt es schon
// einen Eintrag mit demselben Schlüssel, bleibt nur der größere.
func keepTop[T any](list []T, entry T, limit int, same func(T) bool, value func(T) float64) []T {
	if i := slices.IndexFunc(list, same); i >= 0 {
		if value(list[i]) >= value(entry) {
			return list
		}
		list = slices.Delete(list, i, i+1)
	}
	at, _ := slices.BinarySearchFunc(list, value(entry), func(e T, v float64) int {
		switch {
		case value(e) > v:
			return -1
		case value(e) < v:
			return 1
		}
		return 0
	})
	list = slices.Insert(list, at, entry)
	if len(list) > limit {
		list = list[:limit]
	}
	return list
}

func msOf(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
