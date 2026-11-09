package dbmetrics

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// WritePrometheus schreibt die Momentaufnahme im Prometheus-Textformat.
// Dauern stehen dort nach Konvention in Sekunden.
func WritePrometheus(w io.Writer, s Snapshot) error {
	p := &promWriter{w: w}
	p.gauge("lcm_db_file_bytes", "Groesse der Datenbankdatei.", float64(s.File.DBBytes))
	p.gauge("lcm_db_wal_bytes", "Groesse des WAL.", float64(s.File.WALBytes))
	p.gauge("lcm_db_page_count", "Seiten der Datenbank.", float64(s.File.PageCount))
	p.gauge("lcm_db_freelist_count", "Freie Seiten der Datenbank.", float64(s.File.FreelistCount))
	p.gauge("lcm_db_page_size_bytes", "Seitengroesse.", float64(s.File.PageSize))
	p.histogram("lcm_db_lock_wait_seconds", "Wartezeit auf die Schreibsperre (BEGIN IMMEDIATE).", s.LockWait, s.LockWait.Bounds, "")
	p.histogram("lcm_db_transaction_seconds", "Dauer der Transaktionen.", s.Transactions, s.Transactions.Bounds, "")
	p.header("lcm_db_statement_seconds", "Dauer der Anweisungen je Tabelle und Art.", "histogram")
	for _, st := range s.Statements {
		p.histogramSeries("lcm_db_statement_seconds", st.Histogram, s.StatementBoundsMS,
			fmt.Sprintf(`table=%q,kind=%q`, st.Table, st.Kind))
	}
	p.header("lcm_db_errors_total", "Fehler durch Sperren.", "counter")
	p.sample("lcm_db_errors_total", `code="busy"`, float64(s.Errors.Busy))
	p.sample("lcm_db_errors_total", `code="locked"`, float64(s.Errors.Locked))
	p.counter("lcm_db_rows_written_total", "Geschriebene Zeilen.", float64(s.RowsWritten))
	p.header("lcm_db_table_rows", "Zeilen je Tabelle (stuendlich gezaehlt).", "gauge")
	for _, table := range slices.Sorted(maps.Keys(s.TableRows.Tables)) {
		p.sample("lcm_db_table_rows", fmt.Sprintf(`table=%q`, table), float64(s.TableRows.Tables[table]))
	}
	p.gauge("lcm_process_heap_alloc_bytes", "Belegter Heap.", float64(s.Process.HeapAllocBytes))
	p.gauge("lcm_process_heap_sys_bytes", "Vom System geholter Heap.", float64(s.Process.HeapSysBytes))
	p.gauge("lcm_process_goroutines", "Goroutines.", float64(s.Process.Goroutines))
	p.gauge("lcm_process_open_files", "Offene Dateien.", float64(s.Process.OpenFiles))
	p.gauge("lcm_db_pool_open", "Offene Verbindungen im Pool.", float64(s.Pool.Open))
	p.gauge("lcm_db_pool_in_use", "Benutzte Verbindungen im Pool.", float64(s.Pool.InUse))
	p.counter("lcm_db_pool_wait_total", "Wartevorgaenge auf eine Verbindung.", float64(s.Pool.WaitCount))
	p.gauge("lcm_process_start_time_seconds", "Startzeit des Prozesses.", float64(s.StartedAt.Unix()))
	return p.err
}

// promWriter merkt sich den ersten Schreibfehler, damit die Aufrufe oben
// ohne Fehlerprüfung je Zeile auskommen.
type promWriter struct {
	w   io.Writer
	err error
}

func (p *promWriter) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}

func (p *promWriter) header(name, help, typ string) {
	p.printf("# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func (p *promWriter) sample(name, labels string, v float64) {
	if labels != "" {
		labels = "{" + labels + "}"
	}
	p.printf("%s%s %s\n", name, labels, formatFloat(v))
}

func (p *promWriter) gauge(name, help string, v float64) {
	p.header(name, help, "gauge")
	p.sample(name, "", v)
}

func (p *promWriter) counter(name, help string, v float64) {
	p.header(name, help, "counter")
	p.sample(name, "", v)
}

func (p *promWriter) histogram(name, help string, h Histogram, bounds []float64, labels string) {
	p.header(name, help, "histogram")
	p.histogramSeries(name, h, bounds, labels)
}

// histogramSeries schreibt die Eimer kumuliert, wie Prometheus sie erwartet.
func (p *promWriter) histogramSeries(name string, h Histogram, bounds []float64, labels string) {
	var cumulative uint64
	for i, b := range bounds {
		cumulative += h.Counts[i]
		p.sample(name+"_bucket", joinLabels(labels, `le="`+formatFloat(b/1000)+`"`), float64(cumulative))
	}
	p.sample(name+"_bucket", joinLabels(labels, `le="+Inf"`), float64(h.Count))
	p.sample(name+"_sum", labels, h.SumMS/1000)
	p.sample(name+"_count", labels, float64(h.Count))
}

func joinLabels(a, b string) string {
	return strings.Trim(a+","+b, ",")
}

func formatFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
