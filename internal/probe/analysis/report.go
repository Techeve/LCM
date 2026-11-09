package analysis

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"time"

	"LCM/internal/probe/host"
	"LCM/internal/probe/journal"
)

const (
	defaultInterval = 2 * time.Second
	defaultDBEvery  = time.Minute
	// lookBack ist das Umfeld vor einer Lücke, das der Bericht zeigt.
	lookBack = 5 * time.Minute
	// clockSlack: So weit dürfen Wanduhr und monotone Uhr zwischen zwei
	// Zeilen auseinanderliegen, bevor es als gestellte Uhr gilt.
	clockSlack = 2 * time.Second
)

// Report ist das Ergebnis der Auswertung, Grundlage für Text und HTML.
type Report struct {
	From, To    time.Time
	Stats       journal.Stats
	HostLines   int
	DBLines     int
	Interval    time.Duration
	Coverage    float64 // Anteil des Zeitraums mit Hostzeilen
	Sources     *host.Sources
	Gaps        []Gap
	ClockJumps  []ClockJump
	LockWait    WaitSummary
	Costly      []StatementCost
	Slowest     []slowStatement
	LongestTx   []longTransaction
	Busy        uint64
	Locked      uint64
	RowsWritten uint64
	Operations  []operation
	Growth      Growth
	Peaks       []Peak
	Outages     []Outage
	LCMStarts   []time.Time
	Self        SelfSummary
}

// Gap ist eine Lücke zwischen zwei Hostzeilen.
type Gap struct {
	Start    time.Time
	Duration time.Duration
	Kind     string
	Before   []string // was in den fünf Minuten davor lief
}

// ClockJump: Die Wanduhr sprang, ohne dass Zeit verging.
type ClockJump struct {
	At   time.Time
	Jump time.Duration
}

// WaitSummary ist die Verteilung der Wartezeit auf die Schreibsperre.
type WaitSummary struct {
	Count       uint64
	MedianMS    float64
	MedianAbove bool
	P95MS       float64
	P95Above    bool
	MaxMS       float64
	MaxAt       time.Time
	TopMS       float64 // höchste belegte Stufe im Zeitraum
	TopAbove    bool
}

// StatementCost ist der Aufwand einer Tabelle und Art im Zeitraum.
type StatementCost struct {
	Table, Kind string
	Count       uint64
	SumMS       float64
	MaxMS       float64
	MaxAt       time.Time
}

// Peak sind die höchsten Werte einer Größe.
type Peak struct {
	Name   string
	Unit   string
	Values []PeakValue
}

// PeakValue ist ein Spitzenwert mit Uhrzeit.
type PeakValue struct {
	At    time.Time
	Value float64
}

// Outage ist eine Zeit, in der LCM nicht antwortete.
type Outage struct {
	Start, End time.Time
	Polls      int
	Error      string
}

// SelfSummary ist der Verbrauch von lcm-probe selbst.
type SelfSummary struct {
	CPUPercent float64 // eines Kerns
	MaxRSS     uint64
	SyncP50MS  float64
	SyncP95MS  float64
	SyncMaxMS  float64
}

// Build wertet die Daten aus.
func Build(d *Data) *Report {
	r := &Report{From: d.From, To: d.To, Stats: d.Stats, HostLines: len(d.Hosts), DBLines: len(d.DBs)}
	r.Interval = interval(d)
	r.effectiveRange(d)
	if n := len(d.Sources); n > 0 {
		r.Sources = &d.Sources[n-1].Sources
	}
	r.Coverage = coverage(d.Hosts, r.Interval, r.From, r.To)
	r.Gaps, r.ClockJumps = findGaps(d, r.Interval)
	r.summarizeDB(d)
	for i := range r.Gaps {
		r.Gaps[i].Before = describeBefore(d, r.Operations, r.Gaps[i])
	}
	r.Growth = growth(d)
	r.Peaks = peaks(d.Hosts)
	r.Outages = outages(d.Downs, dbEvery(d))
	r.Self = selfSummary(d.Hosts)
	return r
}

// effectiveRange setzt offene Ränder auf die erste und letzte Zeile.
func (r *Report) effectiveRange(d *Data) {
	if len(d.Hosts) == 0 {
		return
	}
	if r.From.IsZero() || r.From.Before(d.Hosts[0].Time()) {
		r.From = d.Hosts[0].Time()
	}
	if last := d.Hosts[len(d.Hosts)-1].Time(); r.To.IsZero() || r.To.After(last) {
		r.To = last
	}
}

func interval(d *Data) time.Duration {
	if n := len(d.Starts); n > 0 && d.Starts[n-1].IntervalMS > 0 {
		return time.Duration(d.Starts[n-1].IntervalMS) * time.Millisecond
	}
	return defaultInterval
}

func dbEvery(d *Data) time.Duration {
	if n := len(d.Starts); n > 0 && d.Starts[n-1].DBIntervalMS > 0 {
		return time.Duration(d.Starts[n-1].DBIntervalMS) * time.Millisecond
	}
	return defaultDBEvery
}

// gapThreshold: fünf ausgefallene Messungen, mindestens zehn Sekunden.
func gapThreshold(iv time.Duration) time.Duration { return max(5*iv, 10*time.Second) }

func coverage(hosts []journal.HostLine, iv time.Duration, from, to time.Time) float64 {
	span := to.Sub(from)
	if span <= 0 {
		return 0
	}
	return min(1, float64(len(hosts))*float64(iv)/float64(span))
}

// findGaps sucht Lücken zwischen aufeinanderfolgenden Hostzeilen. Liegt ein
// Start von lcm-probe dazwischen, war es ein Neustart des Werkzeugs (sauber
// mit Stop-Zeile, sonst abgebrochen) oder des Rechners (andere Boot-ID).
func findGaps(d *Data, iv time.Duration) ([]Gap, []ClockJump) {
	threshold := gapThreshold(iv)
	var gaps []Gap
	var jumps []ClockJump
	for i := 1; i < len(d.Hosts); i++ {
		a, b := d.Hosts[i-1].Head, d.Hosts[i].Head
		if start, ok := startBetween(d.Starts, a, b); ok {
			wall := time.Duration(b.Wall-a.Wall) * time.Millisecond
			kind, reboot := restartKind(d, a, start)
			// Ein neuer Systemstart zählt auch, wenn er schnell ging: Ein
			// Container, der auf einen anderen Host umzieht, ist nach
			// Sekunden wieder da.
			if wall > threshold || reboot {
				gaps = append(gaps, Gap{Start: a.Time(), Duration: wall, Kind: kind})
			}
			continue
		}
		mono := time.Duration(b.Mono-a.Mono) * time.Millisecond
		wall := time.Duration(b.Wall-a.Wall) * time.Millisecond
		switch diff := wall - mono; {
		case mono > threshold:
			gaps = append(gaps, Gap{Start: a.Time(), Duration: mono, Kind: "Stillstand"})
		case wall > threshold && diff > clockSlack:
			// In einer angehaltenen VM steht auch die monotone Uhr; nur die
			// Wanduhr zeigt, wie lange es dauerte. Genauso sähe eine vorgestellte
			// Uhr aus, der Bericht kann beides nicht trennen.
			gaps = append(gaps, Gap{Start: a.Time(), Duration: wall, Kind: "Pause der VM oder Uhr vorgestellt"})
		case diff > clockSlack || diff < -clockSlack:
			jumps = append(jumps, ClockJump{At: b.Time(), Jump: diff})
		}
	}
	return gaps, jumps
}

func startBetween(starts []journal.StartLine, a, b journal.Head) (journal.StartLine, bool) {
	for _, s := range starts {
		if s.Wall > a.Wall && s.Wall <= b.Wall {
			return s, true
		}
	}
	return journal.StartLine{}, false
}

// restartKind sagt, warum lcm-probe neu anfing. reboot ist wahr, wenn sich
// die boot_id geändert hat. Im Container ist das die des Hosts: Der Host
// startete neu, oder der Container zog auf einen anderen um.
func restartKind(d *Data, a journal.Head, start journal.StartLine) (kind string, reboot bool) {
	var prevBoot string
	for _, s := range d.Starts {
		if s.Wall <= a.Wall {
			prevBoot = s.BootID
		}
	}
	if prevBoot != "" && start.BootID != "" && prevBoot != start.BootID {
		if inContainer(d, start) {
			return "Container neu gestartet, Host neu gestartet oder gewechselt", true
		}
		return "Neustart des Rechners", true
	}
	for _, s := range d.Stops {
		if s.Wall >= a.Wall && s.Wall <= start.Wall {
			return "lcm-probe sauber neu gestartet", false
		}
	}
	return "lcm-probe abgebrochen", false
}

// inContainer liest die Quellen, die dem Start zeitlich am nächsten stehen.
// Eine Quellenzeile steht am Anfang jeder Tagesdatei, nicht bei jedem Start.
func inContainer(d *Data, start journal.StartLine) bool {
	container, best := false, int64(-1)
	for _, s := range d.Sources {
		dist := s.Wall - start.Wall
		if dist < 0 {
			dist = -dist
		}
		if best < 0 || dist < best {
			container, best = s.Sources.Container, dist
		}
	}
	return container
}

// describeBefore nennt, was in den fünf Minuten vor einer Lücke lief: Läufe
// von LCM, ob LCM antwortete, und die Spitzen des Hosts.
func describeBefore(d *Data, ops []operation, g Gap) []string {
	from, end := g.Start.Add(-lookBack), g.Start.Add(g.Duration)
	var out []string
	for _, op := range ops {
		if !op.Started.After(end) && !op.end().Before(from) {
			out = append(out, fmt.Sprintf("%s %s, %s%s", clock(op.Started), op.label(),
				duration(time.Duration(op.DurationMS*float64(time.Millisecond))), failed(op.Error)))
		}
	}
	downs := 0
	for _, dl := range d.Downs {
		if t := dl.Time(); !t.Before(from) && !t.After(end) {
			downs++
		}
	}
	if downs > 0 {
		out = append(out, fmt.Sprintf("LCM %d-mal nicht erreichbar", downs))
	}
	if m, ok := maxima(d.Hosts, from, g.Start); ok {
		out = append(out, m)
	}
	return out
}

func failed(err string) string {
	if err == "" {
		return ""
	}
	return ", fehlgeschlagen: " + err
}

// maxima fasst die Spitzen des Hosts in einem Zeitraum in einer Zeile.
func maxima(hosts []journal.HostLine, from, to time.Time) (string, bool) {
	var ioSome, iowait, load, sync, await float64
	var minAvail uint64
	found := false
	for _, h := range hosts {
		if t := h.Time(); t.Before(from) || t.After(to) {
			continue
		}
		found = true
		if h.PSI != nil {
			ioSome = max(ioSome, h.PSI.IOSome)
		}
		if h.CPU != nil {
			iowait = max(iowait, h.CPU.IOWait)
		}
		if len(h.Load) > 0 {
			load = max(load, h.Load[0])
		}
		if h.Mem != nil && (minAvail == 0 || h.Mem.Available < minAvail) {
			minAvail = h.Mem.Available
		}
		sync = max(sync, h.Self.SyncMS)
		for _, disk := range h.Disks {
			await = max(await, disk.AwaitMS)
		}
	}
	if !found {
		return "", false
	}
	return fmt.Sprintf("Spitzen davor: I/O-Druck %s %%, iowait %s %%, Last %s, Speicher frei %s, Plattenwartezeit %s ms, fdatasync %s ms",
		num(ioSome, 1), num(iowait, 1), num(load, 2), bytesText(minAvail), num(await, 1), num(sync, 1)), true
}

// summarizeDB wertet die Momentaufnahmen je Lauf von LCM aus.
func (r *Report) summarizeDB(d *Data) {
	var wait histogram
	costs := map[[2]string]*StatementCost{}
	slow := map[string]slowStatement{}
	longTx := map[string]longTransaction{}
	ops := map[string]operation{}
	for _, run := range d.runs() {
		first, last := run[0], run[len(run)-1]
		base := &first.Snap
		if !first.Snap.StartedAt.Before(r.From) {
			base = nil // Lauf beginnt im Zeitraum: alles zählt
			r.LCMStarts = append(r.LCMStarts, first.Snap.StartedAt)
		}
		wait.add(last.Snap.LockWait.delta(histOf(base, func(s *snapshot) histogram { return s.LockWait })))
		r.trackMax(run)
		addCosts(costs, last.Snap, base, r.From, r.To)
		r.Busy += last.Snap.Errors.Busy - baseOf(base, func(s *snapshot) uint64 { return s.Errors.Busy })
		r.Locked += last.Snap.Errors.Locked - baseOf(base, func(s *snapshot) uint64 { return s.Errors.Locked })
		r.RowsWritten += last.Snap.RowsWritten - baseOf(base, func(s *snapshot) uint64 { return s.RowsWritten })
		r.collectLists(run, slow, longTx, ops)
	}
	r.LockWait.Count = wait.Count
	r.LockWait.MedianMS, r.LockWait.MedianAbove = wait.quantile(0.5)
	r.LockWait.P95MS, r.LockWait.P95Above = wait.quantile(0.95)
	r.LockWait.TopMS, r.LockWait.TopAbove = wait.quantile(1)
	r.Costly = topCosts(costs, 10)
	r.Slowest = topBy(slices.Collect(maps.Values(slow)), 10, func(s slowStatement) float64 { return s.MaxMS })
	r.LongestTx = topBy(slices.Collect(maps.Values(longTx)), 5, func(t longTransaction) float64 { return t.MaxMS })
	r.Operations = slices.SortedFunc(maps.Values(ops), func(a, b operation) int { return a.Started.Compare(b.Started) })
}

func histOf(s *snapshot, f func(*snapshot) histogram) *histogram {
	if s == nil {
		return nil
	}
	h := f(s)
	return &h
}

func baseOf(s *snapshot, f func(*snapshot) uint64) uint64 {
	if s == nil {
		return 0
	}
	return f(s)
}

// trackMax: Das Maximum der Wartezeit kennt LCM samt Uhrzeit; es zählt, wenn
// es im Zeitraum liegt.
func (r *Report) trackMax(run []DBPoint) {
	for _, p := range run {
		lw := p.Snap.LockWait
		if lw.MaxMS > r.LockWait.MaxMS && r.inRange(lw.MaxAt) {
			r.LockWait.MaxMS, r.LockWait.MaxAt = lw.MaxMS, lw.MaxAt
		}
	}
}

func (r *Report) inRange(t time.Time) bool { return !t.Before(r.From) && !t.After(r.To) }

func addCosts(costs map[[2]string]*StatementCost, last snapshot, base *snapshot, from, to time.Time) {
	earlier := map[[2]string]histogram{}
	if base != nil {
		for _, s := range base.Statements {
			earlier[[2]string{s.Table, s.Kind}] = s.histogram
		}
	}
	for _, s := range last.Statements {
		key := [2]string{s.Table, s.Kind}
		var prev *histogram
		if h, ok := earlier[key]; ok {
			prev = &h
		}
		d := s.histogram.delta(prev)
		c := costs[key]
		if c == nil {
			c = &StatementCost{Table: s.Table, Kind: s.Kind}
			costs[key] = c
		}
		c.Count += d.Count
		c.SumMS += d.SumMS
		if s.MaxMS > c.MaxMS && !s.MaxAt.Before(from) && !s.MaxAt.After(to) {
			c.MaxMS, c.MaxAt = s.MaxMS, s.MaxAt
		}
	}
}

func topCosts(costs map[[2]string]*StatementCost, n int) []StatementCost {
	var list []StatementCost
	for _, c := range costs {
		if c.Count > 0 {
			list = append(list, *c)
		}
	}
	return topBy(list, n, func(c StatementCost) float64 { return c.SumMS })
}

// collectLists sammelt Bestenlisten und Läufe aus allen Momentaufnahmen.
func (r *Report) collectLists(run []DBPoint, slow map[string]slowStatement, longTx map[string]longTransaction, ops map[string]operation) {
	last := run[len(run)-1].Snap
	for _, s := range last.Slowest {
		if r.inRange(s.At) && s.MaxMS > slow[s.SQL].MaxMS {
			slow[s.SQL] = s
		}
	}
	for _, t := range last.LongestTransactions {
		if r.inRange(t.At) && t.MaxMS > longTx[t.Caller].MaxMS {
			longTx[t.Caller] = t
		}
	}
	for _, p := range run {
		for _, op := range p.Snap.Operations {
			if !op.end().Before(r.From.Add(-lookBack)) && !op.Started.After(r.To) {
				ops[op.Kind+"|"+op.Name+"|"+op.Started.String()] = op
			}
		}
	}
}

func topBy[T any](list []T, n int, value func(T) float64) []T {
	slices.SortFunc(list, func(a, b T) int { return cmp.Compare(value(b), value(a)) })
	if len(list) > n {
		list = list[:n]
	}
	return list
}
