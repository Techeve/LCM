package analysis

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"LCM/internal/probe/host"
)

// WriteText schreibt den Bericht für Menschen und Agenten.
func WriteText(w io.Writer, r *Report) error {
	t := &textWriter{w: w}
	t.overview(r)
	t.gaps(r)
	t.lockWait(r)
	t.statements(r)
	t.operations(r)
	t.growth(r)
	t.peaks(r)
	t.outages(r)
	t.self(r)
	return t.err
}

// textWriter merkt sich den ersten Schreibfehler.
type textWriter struct {
	w   io.Writer
	err error
}

func (t *textWriter) printf(format string, args ...any) {
	if t.err == nil {
		_, t.err = fmt.Fprintf(t.w, format, args...)
	}
}

func (t *textWriter) section(title string) { t.printf("\n== %s ==\n", title) }

// table schreibt Zeilen mit Tabulatoren als ausgerichtete Spalten.
func (t *textWriter) table(rows [][]string) {
	if t.err != nil || len(rows) == 0 {
		return
	}
	tw := tabwriter.NewWriter(t.w, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintln(tw, "  "+strings.Join(row, "\t"))
	}
	t.err = tw.Flush()
}

func (t *textWriter) overview(r *Report) {
	t.printf("lcm-probe Bericht: %s bis %s (%s)\n", clock(r.From), clock(r.To), duration(r.To.Sub(r.From)))
	t.printf("Hostzeilen %s, Abdeckung %s %%, Momentaufnahmen der Datenbank %s, Dateien %d",
		count(r.HostLines), num(100*r.Coverage, 1), count(r.DBLines), r.Stats.Files)
	if r.Stats.Skipped > 0 {
		t.printf(", übersprungene Zeilen %d", r.Stats.Skipped)
	}
	t.printf("\n")
	if r.Sources != nil {
		t.printf("Herkunft: %s\n", sourcesText(*r.Sources))
	}
}

// sourcesText nennt die Werte, die nicht vom eigenen System stammen.
func sourcesText(s host.Sources) string {
	if !s.Container {
		return "eigener Kernel, alle Werte vom System"
	}
	var parts []string
	for _, key := range host.SourceKeys {
		if o, ok := s.Values[key]; ok {
			parts = append(parts, key+"="+o.Scope)
		}
	}
	return "Container, " + strings.Join(parts, " ")
}

func (t *textWriter) gaps(r *Report) {
	t.section("Lücken")
	if len(r.Gaps) == 0 {
		t.printf("  keine Lücke über %s\n", duration(gapThreshold(r.Interval)))
	}
	for _, g := range r.Gaps {
		t.printf("  %s  %s  %s\n", clock(g.Start), duration(g.Duration), g.Kind)
		for _, b := range g.Before {
			t.printf("      %s\n", b)
		}
	}
	for _, j := range r.ClockJumps {
		t.printf("  Uhr gestellt %s um %s\n", clock(j.At), duration(absDuration(j.Jump)))
	}
}

func (t *textWriter) lockWait(r *Report) {
	t.section("Wartezeit auf die Schreibsperre")
	lw := r.LockWait
	if lw.Count == 0 {
		t.printf("  keine Transaktion im Zeitraum (oder keine Momentaufnahmen)\n")
		return
	}
	t.printf("  %s Transaktionen, Median %s, 95 %% %s, Maximum %s\n", count(lw.Count),
		waitText(lw.MedianMS, lw.MedianAbove), waitText(lw.P95MS, lw.P95Above), maxWaitText(lw))
	t.printf("  Fehler durch Sperren: database is locked %d, SQLITE_LOCKED %d\n", r.Busy, r.Locked)
	t.printf("  geschriebene Zeilen %s\n", count(r.RowsWritten))
	if r.DBLines == 0 {
		t.printf("  keine Momentaufnahmen: liegt ein API-Schlüssel in /etc/lcm-probe/key?\n")
	}
	if len(r.LongestTx) > 0 {
		t.printf("  längste Transaktionen:\n")
		var rows [][]string
		for _, tx := range r.LongestTx {
			rows = append(rows, []string{msText(tx.MaxMS), clock(tx.At), tx.Caller})
		}
		t.table(rows)
	}
}

func (t *textWriter) statements(r *Report) {
	t.section("Teuerste Anweisungen (Summe der Dauer)")
	rows := [][]string{{"Tabelle", "Art", "Anzahl", "Summe", "Mittel", "Maximum"}}
	for _, c := range r.Costly {
		rows = append(rows, []string{orDash(c.Table), c.Kind, count(c.Count), duration(ms(c.SumMS)),
			msText(c.SumMS / float64(c.Count)), maxText(c.MaxMS, c.MaxAt)})
	}
	if len(rows) == 1 {
		t.printf("  keine Anweisungen im Zeitraum\n")
	} else {
		t.table(rows)
	}
	if len(r.Slowest) == 0 && len(r.Costly) > 0 {
		t.printf("\n  keine der 50 langsamsten Anweisungen seit dem Start von LCM fällt in den Zeitraum\n")
	}
	if len(r.Slowest) > 0 {
		t.printf("\n  langsamste einzelne Anweisungen:\n")
		for _, s := range r.Slowest {
			t.printf("  %10s  %s  %s\n", msText(s.MaxMS), clock(s.At), s.SQL)
		}
	}
}

func (t *textWriter) operations(r *Report) {
	t.section("Läufe (Backup, VACUUM, Checkpoint, Jobs)")
	if len(r.Operations) == 0 {
		t.printf("  keine\n")
		return
	}
	var rows [][]string
	short := map[string]int{}
	for _, op := range r.Operations {
		if op.short() {
			short[op.label()]++
			continue
		}
		rows = append(rows, []string{clock(op.Started), op.label(), duration(ms(op.DurationMS)),
			count(op.RowsWritten) + " Zeilen", strings.TrimPrefix(failed(op.Error), ", ")})
	}
	t.table(rows)
	if len(short) > 0 {
		t.printf("  dazu unter einer Sekunde: %s\n", shortRuns(short))
	}
}

// shortRuns zählt die kurzen Läufe je Name, die häufigsten zuerst.
func shortRuns(short map[string]int) string {
	names := make([]string, 0, len(short))
	for name := range short {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		if c := cmp.Compare(short[b], short[a]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = fmt.Sprintf("%s %d-mal", name, short[name])
	}
	return strings.Join(parts, ", ")
}

func (t *textWriter) growth(r *Report) {
	t.section("Wachstum je Tag")
	g := r.Growth
	if len(g.Days) == 0 {
		t.printf("  keine Momentaufnahmen\n")
		return
	}
	rows := [][]string{{"Tag", "Datei", "WAL", "frei in der Datei"}}
	for _, f := range g.File {
		rows = append(rows, []string{f.Day, bytesText(f.DBBytes), bytesText(f.WALBytes), bytesText(f.FreeBytes)})
	}
	t.table(rows)
	if len(g.Tables) == 0 {
		t.printf("\n  Zeilen je Tabelle: noch nicht gezählt (LCM zählt stündlich)\n")
		return
	}
	header := []string{"Tabelle", "Zeilen", "Zuwachs"}
	for _, day := range g.Days {
		header = append(header, day[5:])
	}
	rows = [][]string{header}
	for _, tg := range g.Tables {
		row := []string{tg.Table, count(tg.Last), signed(tg.Last - tg.First)}
		for _, n := range tg.PerDay {
			row = append(row, signed(n))
		}
		rows = append(rows, row)
	}
	t.printf("\n")
	t.table(rows)
}

func (t *textWriter) peaks(r *Report) {
	t.section("Spitzen")
	if len(r.Peaks) == 0 {
		t.printf("  keine Hostwerte\n")
	}
	var rows [][]string
	for _, p := range r.Peaks {
		var parts []string
		for _, v := range p.Values {
			parts = append(parts, fmt.Sprintf("%s%s %s", num(v.Value, 1), unitSuffix(p.Unit), clock(v.At)))
		}
		rows = append(rows, []string{p.Name, strings.Join(parts, ", ")})
	}
	t.table(rows)
}

func (t *textWriter) outages(r *Report) {
	t.section("Zeiten ohne LCM")
	var total time.Duration
	for _, o := range r.Outages {
		total += o.End.Sub(o.Start)
	}
	if len(r.Outages) == 0 {
		t.printf("  LCM hat jede Abfrage beantwortet\n")
	} else {
		t.printf("  %d-mal, zusammen etwa %s\n", len(r.Outages), duration(total))
	}
	for _, o := range r.Outages {
		t.printf("  %s  %s  %d Abfragen  %s\n", clock(o.Start), duration(o.End.Sub(o.Start)), o.Polls, o.Error)
	}
	for _, s := range r.LCMStarts {
		t.printf("  LCM gestartet %s\n", clock(s))
	}
}

func (t *textWriter) self(r *Report) {
	t.section("lcm-probe selbst")
	s := r.Self
	t.printf("  CPU %s %% eines Kerns, Speicher bis %s, fdatasync Median %s ms, 95 %% %s ms, Maximum %s ms\n",
		num(s.CPUPercent, 2), bytesText(s.MaxRSS), num(s.SyncP50MS, 1), num(s.SyncP95MS, 1), num(s.SyncMaxMS, 1))
}

// maxWaitText nennt das Maximum mit Uhrzeit. LCM kennt nur das Maximum seit
// seinem Start; liegt es vor dem Zeitraum, bleibt für den Zeitraum die
// höchste belegte Stufe.
func maxWaitText(lw WaitSummary) string {
	if !lw.MaxAt.IsZero() {
		return msText(lw.MaxMS) + " um " + clock(lw.MaxAt)
	}
	return "unbekannt, höchste Stufe " + waitText(lw.TopMS, lw.TopAbove)
}

// maxText: Liegt das Maximum vor dem Zeitraum, ist es für den Zeitraum
// unbekannt - LCM kennt nur das Maximum seit dem Start.
func maxText(v float64, at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	return msText(v)
}

func ms(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func signed(n int64) string {
	if n > 0 {
		return "+" + count(n)
	}
	return count(n)
}

func unitSuffix(unit string) string {
	if unit == "" {
		return ""
	}
	return " " + unit
}
