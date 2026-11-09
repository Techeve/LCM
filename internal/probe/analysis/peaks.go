package analysis

import (
	"cmp"
	"slices"
	"time"

	"LCM/internal/probe/host"
	"LCM/internal/probe/journal"
)

// peaksKept je Größe, mindestens fünf Minuten auseinander, damit eine
// einzige Spitze nicht alle Plätze belegt.
const (
	peaksKept    = 3
	peakDistance = 5 * time.Minute
	outageSlack  = 10 * time.Second
)

type peakDef struct {
	name, unit string
	value      func(journal.HostLine) (float64, bool)
}

var peakDefs = []peakDef{
	{"I/O-Druck some", "%", func(h journal.HostLine) (float64, bool) { return psi(h, func(p *host.PSI) float64 { return p.IOSome }) }},
	{"I/O-Druck full", "%", func(h journal.HostLine) (float64, bool) { return psi(h, func(p *host.PSI) float64 { return p.IOFull }) }},
	{"Speicherdruck some", "%", func(h journal.HostLine) (float64, bool) {
		return psi(h, func(p *host.PSI) float64 { return p.MemSome })
	}},
	{"iowait", "%", func(h journal.HostLine) (float64, bool) {
		if h.CPU == nil {
			return 0, false
		}
		return h.CPU.IOWait, true
	}},
	{"steal", "%", func(h journal.HostLine) (float64, bool) {
		if h.CPU == nil {
			return 0, false
		}
		return h.CPU.Steal, true
	}},
	{"Last 1 min", "", func(h journal.HostLine) (float64, bool) {
		if len(h.Load) == 0 {
			return 0, false
		}
		return h.Load[0], true
	}},
	{"Speicher belegt", "%", memUsedShare},
	{"Plattenwartezeit", "ms", func(h journal.HostLine) (float64, bool) {
		v := 0.0
		for _, d := range h.Disks {
			v = max(v, d.AwaitMS)
		}
		return v, len(h.Disks) > 0
	}},
	{"fdatasync von lcm-probe", "ms", func(h journal.HostLine) (float64, bool) { return h.Self.SyncMS, true }},
}

func psi(h journal.HostLine, f func(*host.PSI) float64) (float64, bool) {
	if h.PSI == nil {
		return 0, false
	}
	return f(h.PSI), true
}

// memUsedShare bezieht den belegten Speicher auf die Grenze des Containers,
// sonst auf den ganzen Speicher.
func memUsedShare(h journal.HostLine) (float64, bool) {
	if h.Mem == nil {
		return 0, false
	}
	total := h.Mem.Total
	if h.Mem.Limit > 0 {
		total = h.Mem.Limit
	}
	if total == 0 {
		return 0, false
	}
	return 100 * float64(h.Mem.Used) / float64(total), true
}

func peaks(hosts []journal.HostLine) []Peak {
	var out []Peak
	for _, def := range peakDefs {
		var values []PeakValue
		for _, h := range hosts {
			if v, ok := def.value(h); ok && v > 0 {
				values = append(values, PeakValue{At: h.Time(), Value: v})
			}
		}
		if len(values) > 0 {
			out = append(out, Peak{Name: def.name, Unit: def.unit, Values: distinctTop(values)})
		}
	}
	return out
}

func distinctTop(values []PeakValue) []PeakValue {
	slices.SortFunc(values, func(a, b PeakValue) int { return cmp.Compare(b.Value, a.Value) })
	var picked []PeakValue
	for _, v := range values {
		if len(picked) == peaksKept {
			break
		}
		if !slices.ContainsFunc(picked, func(p PeakValue) bool { return absDuration(p.At.Sub(v.At)) < peakDistance }) {
			picked = append(picked, v)
		}
	}
	return picked
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// outages fasst aufeinanderfolgende Fehlschläge zu Zeiten ohne LCM zusammen.
func outages(downs []journal.DownLine, every time.Duration) []Outage {
	var out []Outage
	for _, d := range downs {
		t := d.Time()
		if n := len(out); n > 0 && t.Sub(out[n-1].End) <= every+outageSlack {
			out[n-1].End = t
			out[n-1].Polls++
			continue
		}
		out = append(out, Outage{Start: t, End: t, Polls: 1, Error: d.Error})
	}
	return out
}

func selfSummary(hosts []journal.HostLine) SelfSummary {
	var s SelfSummary
	var cpuMS, dtMS float64
	syncs := make([]float64, 0, len(hosts))
	for _, h := range hosts {
		if h.DT > 0 {
			cpuMS += h.Self.CPUMS
			dtMS += float64(h.DT)
		}
		s.MaxRSS = max(s.MaxRSS, h.Self.RSS)
		syncs = append(syncs, h.Self.SyncMS)
	}
	if dtMS > 0 {
		s.CPUPercent = 100 * cpuMS / dtMS
	}
	slices.Sort(syncs)
	s.SyncP50MS, s.SyncP95MS = percentile(syncs, 0.5), percentile(syncs, 0.95)
	if n := len(syncs); n > 0 {
		s.SyncMaxMS = syncs[n-1]
	}
	return s
}

func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min(len(sorted)-1, int(q*float64(len(sorted))))]
}
