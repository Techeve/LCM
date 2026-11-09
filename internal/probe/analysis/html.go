package analysis

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html"
	"io"
	"text/template"
	"time"
)

// uPlot (MIT, Leon Sorokin) zeichnet die Zeitreihen. Es steckt im Binary und
// in der Datei, damit der Bericht ohne Netz läuft.
var (
	//go:embed assets/uPlot.iife.min.js
	uplotJS string
	//go:embed assets/uPlot.min.css
	uplotCSS string
	//go:embed assets/report.html.tmpl
	reportTemplate string
)

var reportTmpl = template.Must(template.New("report").Parse(reportTemplate))

// stat ist eine Kennzahl im Kopf des Berichts.
type stat struct {
	Label, Value, Note string
	Alert              bool
}

type htmlPage struct {
	Title      string
	Range      string
	Host       string
	Stats      []stat
	UplotCSS   string
	UplotJS    string
	DataJSON   string
	ReportText string
}

// payload sind die Daten der Diagramme.
type payload struct {
	Host    chartData  `json:"host"`
	DB      chartData  `json:"db"`
	Gaps    [][3]any   `json:"gaps"`    // Beginn, Ende (Sekunden), Art
	Outages [][2]int64 `json:"outages"` // Beginn, Ende
	Ops     [][3]any   `json:"ops"`     // Beginn, Ende, Name
}

// WriteHTML schreibt den Bericht als eine einzelne HTML-Datei mit Diagrammen.
func WriteHTML(w io.Writer, r *Report, d *Data) error {
	var text bytes.Buffer
	if err := WriteText(&text, r); err != nil {
		return err
	}
	gap := gapThreshold(r.Interval)
	data, err := json.Marshal(payload{
		Host: spanRange(hostChart(d.Hosts, gap), r.From, r.To),
		DB:   spanRange(dbChart(d, 3*dbEvery(d)), r.From, r.To),
		Gaps: gapBands(r.Gaps), Outages: outageBands(r.Outages, dbEvery(d)), Ops: opBands(r.Operations),
	})
	if err != nil {
		return err
	}
	return reportTmpl.Execute(w, htmlPage{
		Title:      "lcm-probe " + hostName(d),
		Range:      clock(r.From) + " bis " + clock(r.To) + " (" + duration(r.To.Sub(r.From)) + ")",
		Host:       hostName(d),
		Stats:      headlineStats(r),
		UplotCSS:   uplotCSS,
		UplotJS:    uplotJS,
		DataJSON:   string(data), // json.Marshal maskiert <, > und &
		ReportText: html.EscapeString(text.String()),
	})
}

func hostName(d *Data) string {
	if n := len(d.Starts); n > 0 && d.Starts[n-1].Hostname != "" {
		return d.Starts[n-1].Hostname
	}
	return "Bericht"
}

func headlineStats(r *Report) []stat {
	longest := time.Duration(0)
	for _, g := range r.Gaps {
		longest = max(longest, g.Duration)
	}
	gapNote := "keine über " + duration(gapThreshold(r.Interval))
	if len(r.Gaps) > 0 {
		gapNote = "längste " + duration(longest)
	}
	var down time.Duration
	for _, o := range r.Outages {
		down += o.End.Sub(o.Start)
	}
	stats := []stat{
		{Label: "Lücken", Value: count(len(r.Gaps)), Note: gapNote, Alert: len(r.Gaps) > 0},
		{Label: "Schreibwartezeit 95 %", Value: waitText(r.LockWait.P95MS, r.LockWait.P95Above), Note: "Median " + waitText(r.LockWait.MedianMS, r.LockWait.MedianAbove)},
		longestWait(r.LockWait),
		{Label: "database is locked", Value: count(r.Busy), Alert: r.Busy > 0},
		{Label: "ohne LCM", Value: count(len(r.Outages)) + "×", Note: "zusammen " + duration(down), Alert: len(r.Outages) > 0},
	}
	if n := len(r.Growth.File); n > 0 {
		f := r.Growth.File[n-1]
		stats = append(stats, stat{Label: "Datenbank", Value: bytesText(f.DBBytes), Note: "WAL " + bytesText(f.WALBytes)})
	}
	return stats
}

func longestWait(lw WaitSummary) stat {
	if lw.MaxAt.IsZero() {
		return stat{Label: "längste Wartezeit", Value: waitText(lw.TopMS, lw.TopAbove), Note: "Stufe, genauer Wert älter"}
	}
	return stat{Label: "längste Wartezeit", Value: msText(lw.MaxMS), Note: clock(lw.MaxAt), Alert: lw.MaxMS >= 1000}
}

func unix(t time.Time) int64 { return t.Unix() }

func gapBands(gaps []Gap) [][3]any {
	out := [][3]any{}
	for _, g := range gaps {
		out = append(out, [3]any{unix(g.Start), unix(g.Start.Add(g.Duration)), g.Kind})
	}
	return out
}

func outageBands(outages []Outage, every time.Duration) [][2]int64 {
	out := [][2]int64{}
	for _, o := range outages {
		out = append(out, [2]int64{unix(o.Start), unix(o.End.Add(every))})
	}
	return out
}

// opBands markiert die Läufe, die zählen; die kurzen ohne Fehler, etwa die
// Alarm-Auswertung alle 30 Minuten, verdecken sonst die Diagramme.
func opBands(ops []operation) [][3]any {
	out := [][3]any{}
	for _, op := range ops {
		if op.short() {
			continue
		}
		out = append(out, [3]any{unix(op.Started), unix(op.end()), op.label()})
	}
	return out
}
