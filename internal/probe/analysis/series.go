package analysis

import (
	"math"
	"time"

	"LCM/internal/probe/journal"
)

// maxPoints begrenzt die Punkte je Zeitreihe. Mehr zeigt kein Bildschirm,
// und die Datei bleibt auch bei 90 Tagen klein. Wer feiner sehen will, wählt
// einen kürzeren Zeitraum.
const maxPoints = 4000

// column ist eine Zeitreihe; NaN steht für „kein Wert".
type column struct {
	name string
	vals []float64
}

// chartData ist eine Gruppe von Zeitreihen auf einer Zeitachse (Sekunden).
type chartData struct {
	X      []float64        `json:"x"`
	Series map[string][]any `json:"series"`
}

// downsample fasst Punkte in Zeitscheiben zusammen. Liegen zwei Punkte
// weiter als gap auseinander, folgt ein leerer Punkt: Die Linie bricht dort
// ab, statt die Lücke zu überbrücken.
func downsample(ts []int64, cols []column, gap time.Duration) chartData {
	out := chartData{Series: map[string][]any{}}
	if len(ts) == 0 {
		return out
	}
	width := max(int64(1000), (ts[len(ts)-1]-ts[0])/maxPoints)
	acc := make([][]float64, len(cols))
	cur := make([]float64, len(cols)) // Aggregat der offenen Scheibe
	open, start := false, int64(0)
	push := func(at int64, vals []float64) {
		out.X = append(out.X, float64(at)/1000)
		for j := range cols {
			acc[j] = append(acc[j], vals[j])
		}
	}
	empty := make([]float64, len(cols))
	for j := range empty {
		empty[j] = math.NaN()
	}
	for i, t := range ts {
		if i > 0 && time.Duration(t-ts[i-1])*time.Millisecond > gap {
			if open {
				push(start, cur)
				open = false
			}
			push(ts[i-1]+1000, empty)
		}
		if open && t >= start+width {
			push(start, cur)
			open = false
		}
		if !open {
			open, start = true, t
			copy(cur, empty)
		}
		for j, c := range cols {
			cur[j] = combine(cur[j], c.vals[i])
		}
	}
	if open {
		push(start, cur)
	}
	for j, c := range cols {
		out.Series[c.name] = nullable(acc[j])
	}
	return out
}

// spanRange setzt leere Punkte an Anfang und Ende des Berichts. So zeigen
// alle Diagramme denselben Zeitraum, auch wenn die Momentaufnahmen erst
// später beginnen als die Hostwerte.
func spanRange(c chartData, from, to time.Time) chartData {
	if len(c.X) == 0 {
		return c
	}
	start, end := float64(from.UnixMilli())/1000, float64(to.UnixMilli())/1000
	if c.X[0] > start {
		c.X = append([]float64{start}, c.X...)
		for name, vals := range c.Series {
			c.Series[name] = append([]any{nil}, vals...)
		}
	}
	if c.X[len(c.X)-1] < end {
		c.X = append(c.X, end)
		for name, vals := range c.Series {
			c.Series[name] = append(vals, nil)
		}
	}
	return c
}

// combine nimmt einen Wert in das Aggregat auf: das Maximum der Scheibe,
// damit Spitzen sichtbar bleiben. NaN zählt nicht.
func combine(acc, v float64) float64 {
	switch {
	case math.IsNaN(v):
		return acc
	case math.IsNaN(acc):
		return v
	}
	return max(acc, v)
}

// nullable macht NaN zu null für JSON und rundet auf drei Stellen.
func nullable(vals []float64) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		if !math.IsNaN(v) {
			out[i] = math.Round(v*1000) / 1000
		}
	}
	return out
}

const mb = 1e6

// hostChart baut die Zeitreihen der Hostwerte.
func hostChart(hosts []journal.HostLine, gap time.Duration) chartData {
	ts := make([]int64, len(hosts))
	names := []string{"io_some", "io_full", "mem_some", "cpu_some", "iowait", "user", "sys", "steal", "load1",
		"mem_used", "mem_cached", "mem_limit", "swap_used", "disk_w", "disk_r", "await", "sync"}
	vals := map[string][]float64{}
	for _, n := range names {
		vals[n] = make([]float64, len(hosts))
	}
	for i, h := range hosts {
		ts[i] = h.Wall
		for _, n := range names {
			vals[n][i] = math.NaN()
		}
		fillHost(vals, i, h)
	}
	cols := make([]column, 0, len(names))
	for _, n := range names {
		cols = append(cols, column{name: n, vals: vals[n]})
	}
	return downsample(ts, cols, gap)
}

func fillHost(v map[string][]float64, i int, h journal.HostLine) {
	if p := h.PSI; p != nil {
		v["io_some"][i], v["io_full"][i], v["mem_some"][i], v["cpu_some"][i] = p.IOSome, p.IOFull, p.MemSome, p.CPUSome
	}
	if c := h.CPU; c != nil {
		v["iowait"][i], v["user"][i], v["sys"][i], v["steal"][i] = c.IOWait, c.User, c.System, c.Steal
	}
	if len(h.Load) > 0 {
		v["load1"][i] = h.Load[0]
	}
	if m := h.Mem; m != nil {
		v["mem_used"][i], v["mem_cached"][i], v["swap_used"][i] = float64(m.Used)/mb, float64(m.Cached)/mb, float64(m.SwapUsed)/mb
		limit := m.Total
		if m.Limit > 0 {
			limit = m.Limit
		}
		v["mem_limit"][i] = float64(limit) / mb
	}
	if h.DT > 0 && len(h.Disks) > 0 {
		seconds := float64(h.DT) / 1000
		var w, r, await float64
		for _, d := range h.Disks {
			w += float64(d.WriteBytes)
			r += float64(d.ReadBytes)
			await = max(await, d.AwaitMS)
		}
		v["disk_w"][i], v["disk_r"][i], v["await"][i] = w/mb/seconds, r/mb/seconds, await
	}
	v["sync"][i] = h.Self.SyncMS
}

// dbChart baut die Zeitreihen der Datenbank: Wartezeit je Minute aus den
// Unterschieden aufeinanderfolgender Momentaufnahmen desselben Laufs.
func dbChart(d *Data, gap time.Duration) chartData {
	var ts []int64
	names := []string{"wait_mean", "wait_p95", "waits", "db", "wal", "free"}
	vals := map[string][]float64{}
	for _, run := range d.runs() {
		for i, p := range run {
			ts = append(ts, p.Wall)
			f := p.Snap.File
			mean, p95, perMin := math.NaN(), math.NaN(), math.NaN()
			if i > 0 {
				mean, p95, perMin = waitBetween(run[i-1], p)
			}
			for name, v := range map[string]float64{
				"wait_mean": mean, "wait_p95": p95, "waits": perMin,
				"db": float64(f.DBBytes) / mb, "wal": float64(f.WALBytes) / mb, "free": float64(f.FreelistCount*f.PageSize) / mb,
			} {
				vals[name] = append(vals[name], v)
			}
		}
	}
	cols := make([]column, 0, len(names))
	for _, n := range names {
		cols = append(cols, column{name: n, vals: vals[n]})
	}
	return downsample(ts, cols, gap)
}

// waitBetween liefert mittlere Wartezeit, 95 %-Stufe und Transaktionen je
// Minute zwischen zwei Momentaufnahmen.
func waitBetween(a, b DBPoint) (mean, p95, perMinute float64) {
	h := b.Snap.LockWait.delta(&a.Snap.LockWait)
	minutes := float64(b.Wall-a.Wall) / 60000
	if h.Count == 0 || minutes <= 0 {
		return math.NaN(), math.NaN(), 0
	}
	bound, _ := h.quantile(0.95)
	return h.SumMS / float64(h.Count), bound, float64(h.Count) / minutes
}
