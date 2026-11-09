package host

import (
	"strconv"
	"strings"
)

// diskCounters ist eine Zeile aus /proc/diskstats.
type diskCounters struct {
	dev, name                  string // dev: „major:minor"
	reads, readSectors, readMS uint64
	writes, writeSectors       uint64
	writeMS, ioMS              uint64
}

func parseDiskstats(text string) []diskCounters {
	var out []diskCounters
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 14 {
			continue
		}
		n := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
		out = append(out, diskCounters{
			dev: f[0] + ":" + f[1], name: f[2],
			reads: n(3), readSectors: n(5), readMS: n(6),
			writes: n(7), writeSectors: n(9), writeMS: n(10), ioMS: n(12),
		})
	}
	return out
}

// ioCounters ist eine Zeile aus io.stat der cgroup.
type ioCounters struct{ rbytes, wbytes, rios, wios uint64 }

func parseIOStat(text string) map[string]ioCounters {
	out := map[string]ioCounters{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		var c ioCounters
		for _, kv := range f[1:] {
			k, v, _ := strings.Cut(kv, "=")
			n, _ := strconv.ParseUint(v, 10, 64)
			switch k {
			case "rbytes":
				c.rbytes = n
			case "wbytes":
				c.wbytes = n
			case "rios":
				c.rios = n
			case "wios":
				c.wios = n
			}
		}
		out[f[0]] = c
	}
	return out
}

// cpuTicks ist die Summenzeile aus /proc/stat.
type cpuTicks struct{ user, system, idle, iowait, steal uint64 }

func parseCPU(text string) (cpuTicks, bool) {
	line, _, _ := strings.Cut(text, "\n")
	f := strings.Fields(line)
	if len(f) < 9 || f[0] != "cpu" {
		return cpuTicks{}, false
	}
	n := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
	// user nice system idle iowait irq softirq steal; guest steckt in user.
	return cpuTicks{
		user: n(1) + n(2), system: n(3) + n(6) + n(7), idle: n(4), iowait: n(5), steal: n(8),
	}, true
}

// psiTotals liest die total-Werte (Mikrosekunden) aus einer pressure-Datei.
func psiTotals(text string) (some, full uint64, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		_, v, _ := strings.Cut(f[4], "=")
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "some":
			some, ok = n, true
		case "full":
			full = n
		}
	}
	return some, full, ok
}

func parseLoad(text string) []float64 {
	f := strings.Fields(text)
	if len(f) < 3 {
		return nil
	}
	out := make([]float64, 3)
	for i := range out {
		out[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return out
}
