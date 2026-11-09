package analysis

import "time"

// snapshot ist der Teil von dbmetrics.Snapshot, den die Auswertung liest.
// Eigene Typen statt des Pakets dbmetrics, damit lcm-probe weder GORM noch
// die Datenbankschicht mitbringt; ein Test hält beide im Gleichschritt.
type snapshot struct {
	TakenAt             time.Time         `json:"taken_at"`
	StartedAt           time.Time         `json:"started_at"`
	Version             string            `json:"version"`
	File                fileStats         `json:"file"`
	LockWait            histogram         `json:"lock_wait"`
	Transactions        histogram         `json:"transactions"`
	StatementBoundsMS   []float64         `json:"statement_bounds_ms"`
	Statements          []statementStats  `json:"statements"`
	Slowest             []slowStatement   `json:"slowest"`
	LongestTransactions []longTransaction `json:"longest_transactions"`
	Errors              struct {
		Busy   uint64 `json:"busy"`
		Locked uint64 `json:"locked"`
	} `json:"errors"`
	RowsWritten uint64      `json:"rows_written"`
	Operations  []operation `json:"operations"`
	TableRows   tableRows   `json:"table_rows"`
	Process     struct {
		HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
		Goroutines     int    `json:"goroutines"`
		OpenFiles      int    `json:"open_files"`
	} `json:"process"`
}

type fileStats struct {
	DBBytes       int64 `json:"db_bytes"`
	WALBytes      int64 `json:"wal_bytes"`
	PageSize      int64 `json:"page_size"`
	PageCount     int64 `json:"page_count"`
	FreelistCount int64 `json:"freelist_count"`
}

type histogram struct {
	Bounds []float64 `json:"bounds_ms"`
	Counts []uint64  `json:"counts"`
	Count  uint64    `json:"count"`
	SumMS  float64   `json:"sum_ms"`
	MaxMS  float64   `json:"max_ms"`
	MaxAt  time.Time `json:"max_at"`
}

type statementStats struct {
	Table string `json:"table"`
	Kind  string `json:"kind"`
	histogram
}

type slowStatement struct {
	SQL   string    `json:"sql"`
	Table string    `json:"table"`
	Kind  string    `json:"kind"`
	MaxMS float64   `json:"max_ms"`
	At    time.Time `json:"at"`
}

type longTransaction struct {
	Caller string    `json:"caller"`
	MaxMS  float64   `json:"max_ms"`
	At     time.Time `json:"at"`
}

type operation struct {
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Started     time.Time `json:"started"`
	DurationMS  float64   `json:"duration_ms"`
	RowsWritten uint64    `json:"rows_written"`
	Error       string    `json:"error"`
}

// shortRun ist die Dauer, unter der ein Lauf ohne Fehler nur gezählt wird.
const shortRun = time.Second

// short sagt, ob der Lauf kurz und fehlerfrei war.
func (o operation) short() bool { return ms(o.DurationMS) < shortRun && o.Error == "" }

// end ist das Ende des Laufs.
func (o operation) end() time.Time {
	return o.Started.Add(time.Duration(o.DurationMS * float64(time.Millisecond)))
}

// operationNames benennt die Läufe ohne eigenen Namen.
var operationNames = map[string]string{
	"checkpoint":  "Checkpoint",
	"vacuum":      "VACUUM",
	"vacuum_into": "VACUUM INTO (Backup)",
}

// label benennt den Lauf für Bericht und Diagramm.
func (o operation) label() string {
	if o.Name != "" {
		return o.Name
	}
	if name, ok := operationNames[o.Kind]; ok {
		return name
	}
	return o.Kind
}

type tableRows struct {
	CountedAt time.Time        `json:"counted_at"`
	TookMS    float64          `json:"took_ms"`
	Tables    map[string]int64 `json:"tables"`
}

// delta zieht ein früheres Histogramm desselben Laufs ab. Ohne früheres
// (Lauf beginnt im Zeitraum) zählt alles.
func (h histogram) delta(earlier *histogram) histogram {
	out := h
	out.Counts = append([]uint64(nil), h.Counts...)
	if earlier == nil || len(earlier.Counts) != len(h.Counts) {
		return out
	}
	for i := range out.Counts {
		out.Counts[i] -= min(out.Counts[i], earlier.Counts[i])
	}
	out.Count -= min(out.Count, earlier.Count)
	out.SumMS -= min(out.SumMS, earlier.SumMS)
	return out
}

// add zählt ein Histogramm mit denselben Stufen hinzu.
func (h *histogram) add(o histogram) {
	if h.Counts == nil {
		h.Bounds, h.Counts = o.Bounds, make([]uint64, len(o.Counts))
	}
	if len(o.Counts) != len(h.Counts) {
		return
	}
	for i, n := range o.Counts {
		h.Counts[i] += n
	}
	h.Count += o.Count
	h.SumMS += o.SumMS
}

// quantile liefert die Obergrenze der Stufe, in der das Quantil q liegt;
// above=true heißt: über der höchsten Stufe.
func (h histogram) quantile(q float64) (bound float64, above bool) {
	if h.Count == 0 {
		return 0, false
	}
	target := q * float64(h.Count)
	var cumulative uint64
	for i, n := range h.Counts {
		cumulative += n
		if float64(cumulative) >= target {
			if i >= len(h.Bounds) {
				return h.Bounds[len(h.Bounds)-1], true
			}
			return h.Bounds[i], false
		}
	}
	return h.Bounds[len(h.Bounds)-1], true
}
