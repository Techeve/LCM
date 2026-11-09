package host

import (
	"math"
	"time"

	"golang.org/x/sys/unix"
)

// sectorBytes: /proc/diskstats zählt immer in 512-Byte-Sektoren.
const sectorBytes = 512

// Sample ist eine Messung. Mengen (Bytes, Vorgänge, Wartezeit) gelten für
// das Intervall seit der vorigen Messung, Anteile in Prozent davon.
type Sample struct {
	Load  []float64  `json:"load,omitempty"` // 1, 5, 15 Minuten
	CPU   *CPU       `json:"cpu,omitempty"`
	CgCPU *CgroupCPU `json:"cg_cpu,omitempty"`
	Mem   *Memory    `json:"mem,omitempty"`
	PSI   *PSI       `json:"psi,omitempty"`
	Disks []Disk     `json:"disks,omitempty"`
	Free  []Free     `json:"free,omitempty"`
}

// CPU sind die Anteile aus /proc/stat in Prozent.
type CPU struct {
	User   float64 `json:"user"`
	System float64 `json:"sys"`
	IOWait float64 `json:"iowait"`
	Steal  float64 `json:"steal"`
	Idle   float64 `json:"idle"`
}

// CgroupCPU ist der Verbrauch des Containers: Usage in Prozent eines Kerns.
type CgroupCPU struct {
	Usage       float64 `json:"usage"`
	ThrottledMS float64 `json:"throttled_ms"`
}

// Memory in Bytes. Used ist Total minus Available.
type Memory struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"avail"`
	Used      uint64 `json:"used"`
	Cached    uint64 `json:"cached"`
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`
	CgCurrent uint64 `json:"cg_current,omitempty"`
	Limit     uint64 `json:"limit,omitempty"`
}

// PSI ist der Anteil des Intervalls in Prozent, in dem Prozesse warten
// mussten (some: mindestens einer, full: alle).
type PSI struct {
	CPUSome float64 `json:"cpu_some"`
	CPUFull float64 `json:"cpu_full"`
	MemSome float64 `json:"mem_some"`
	MemFull float64 `json:"mem_full"`
	IOSome  float64 `json:"io_some"`
	IOFull  float64 `json:"io_full"`
}

// Disk ist der Verkehr einer Platte im Intervall. AwaitMS ist die mittlere
// Dauer je Vorgang, Util der Anteil der Zeit mit Vorgängen in Prozent. Die
// Cg-Werte sind der Anteil des Containers aus io.stat.
type Disk struct {
	Name         string  `json:"name"`
	ReadBytes    uint64  `json:"rb"`
	WriteBytes   uint64  `json:"wb"`
	Reads        uint64  `json:"r"`
	Writes       uint64  `json:"w"`
	AwaitMS      float64 `json:"await_ms"`
	Util         float64 `json:"util"`
	CgReadBytes  uint64  `json:"cg_rb,omitempty"`
	CgWriteBytes uint64  `json:"cg_wb,omitempty"`
}

// Free ist der Platz eines Dateisystems in Bytes.
type Free struct {
	Path  string `json:"path"`
	Avail uint64 `json:"avail"`
	Total uint64 `json:"total"`
}

// Sampler misst wiederholt und rechnet Zähler in Mengen je Intervall um.
type Sampler struct {
	fs    files
	src   Sources
	paths []string // Dateisysteme für den freien Platz
	prev  *counters
}

// counters sind die Zählerstände einer Messung.
type counters struct {
	at      time.Duration
	cpu     cpuTicks
	cpuOK   bool
	cgUsage uint64
	cgThrot uint64
	psi     map[string][2]uint64
	disks   map[string]diskCounters
	cgIO    map[string]ioCounters
}

// NewSampler misst unter root ("" = echtes System) mit den erkannten Quellen.
func NewSampler(root string, src Sources, paths []string) *Sampler {
	return &Sampler{fs: files{root}, src: src, paths: paths}
}

// Sources liefert die Quellen, mit denen der Sampler misst.
func (s *Sampler) Sources() Sources { return s.src }

// Sample misst; mono ist die monotone Uhr. Die erste Messung hat noch keine
// Mengen je Intervall, nur Stände.
func (s *Sampler) Sample(mono time.Duration) Sample {
	cur := s.readCounters(mono)
	out := Sample{Load: parseLoad(s.fs.read(s.path("load"))), Mem: s.memory(), Free: s.free()}
	if s.prev != nil && cur.at > s.prev.at {
		s.fillDeltas(&out, s.prev, cur)
	}
	s.prev = cur
	return out
}

func (s *Sampler) path(key string) string { return s.src.Values[key].Path }

func (s *Sampler) has(key string) bool {
	_, ok := s.src.Values[key]
	return ok
}

func (s *Sampler) readCounters(mono time.Duration) *counters {
	c := &counters{at: mono, psi: map[string][2]uint64{}, disks: map[string]diskCounters{}}
	if s.has("cpu") {
		c.cpu, c.cpuOK = parseCPU(s.fs.read(s.path("cpu")))
	}
	if s.has("cg_cpu") {
		kv := keyValues(s.fs.read(s.path("cg_cpu")))
		c.cgUsage, c.cgThrot = kv["usage_usec"], kv["throttled_usec"]
	}
	for _, key := range []string{"psi_cpu", "psi_memory", "psi_io"} {
		if !s.has(key) {
			continue
		}
		if some, full, ok := psiTotals(s.fs.read(s.path(key))); ok {
			c.psi[key] = [2]uint64{some, full}
		}
	}
	for _, d := range parseDiskstats(s.fs.read(s.path("disks"))) {
		c.disks[d.name] = d
	}
	if s.has("cg_io") {
		c.cgIO = parseIOStat(s.fs.read(s.path("cg_io")))
	}
	return c
}

func (s *Sampler) fillDeltas(out *Sample, prev, cur *counters) {
	dt := cur.at - prev.at
	if cur.cpuOK && prev.cpuOK {
		out.CPU = cpuShares(prev.cpu, cur.cpu)
	}
	if s.has("cg_cpu") {
		out.CgCPU = &CgroupCPU{
			Usage:       round(pct(float64(sub(cur.cgUsage, prev.cgUsage)), float64(dt.Microseconds()))),
			ThrottledMS: round(float64(sub(cur.cgThrot, prev.cgThrot)) / 1000),
		}
	}
	out.PSI = psiShares(prev.psi, cur.psi, dt)
	for _, name := range s.src.Disks {
		if d, ok := diskDelta(prev, cur, name, dt); ok {
			out.Disks = append(out.Disks, d)
		}
	}
}

// cpuShares teilt die Ticks seit der vorigen Probe auf. Unter lxcfs laufen
// einzelne Zähler gelegentlich rückwärts, etwa wenn ein Prozess den Container
// verlässt. Ein solcher Zähler zählt als 0, und die Summe der übrigen ist der
// Nenner, damit die Anteile zusammen 100 % bleiben.
func cpuShares(a, b cpuTicks) *CPU {
	user, system := sub(b.user, a.user), sub(b.system, a.system)
	iowait, steal, idle := sub(b.iowait, a.iowait), sub(b.steal, a.steal), sub(b.idle, a.idle)
	total := float64(user + system + iowait + steal + idle)
	if total == 0 {
		return nil
	}
	share := func(x uint64) float64 { return round(pct(float64(x), total)) }
	return &CPU{
		User: share(user), System: share(system),
		IOWait: share(iowait), Steal: share(steal), Idle: share(idle),
	}
}

func psiShares(prev, cur map[string][2]uint64, dt time.Duration) *PSI {
	if len(cur) == 0 {
		return nil
	}
	us := float64(dt.Microseconds())
	share := func(key string, i int) float64 {
		return round(pct(float64(sub(cur[key][i], prev[key][i])), us))
	}
	return &PSI{
		CPUSome: share("psi_cpu", 0), CPUFull: share("psi_cpu", 1),
		MemSome: share("psi_memory", 0), MemFull: share("psi_memory", 1),
		IOSome: share("psi_io", 0), IOFull: share("psi_io", 1),
	}
}

func diskDelta(prev, cur *counters, name string, dt time.Duration) (Disk, bool) {
	a, okA := prev.disks[name]
	b, okB := cur.disks[name]
	if !okA || !okB {
		return Disk{}, false
	}
	d := Disk{
		Name:       name,
		ReadBytes:  sub(b.readSectors, a.readSectors) * sectorBytes,
		WriteBytes: sub(b.writeSectors, a.writeSectors) * sectorBytes,
		Reads:      sub(b.reads, a.reads),
		Writes:     sub(b.writes, a.writes),
		Util:       round(pct(float64(sub(b.ioMS, a.ioMS)), float64(dt.Milliseconds()))),
	}
	if ops := d.Reads + d.Writes; ops > 0 {
		d.AwaitMS = round(float64(sub(b.readMS, a.readMS)+sub(b.writeMS, a.writeMS)) / float64(ops))
	}
	if ca, cb := prev.cgIO[b.dev], cur.cgIO[b.dev]; cur.cgIO != nil {
		d.CgReadBytes, d.CgWriteBytes = sub(cb.rbytes, ca.rbytes), sub(cb.wbytes, ca.wbytes)
	}
	return d, true
}

// memory liest Speicher, Cache, Swap und Grenze.
func (s *Sampler) memory() *Memory {
	if !s.has("memory") {
		return nil
	}
	kv := keyValues(s.fs.read(s.path("memory")))
	m := &Memory{
		Total: kv["MemTotal"], Available: kv["MemAvailable"],
		Cached:    kv["Cached"] + kv["Buffers"],
		SwapTotal: kv["SwapTotal"], SwapUsed: sub(kv["SwapTotal"], kv["SwapFree"]),
	}
	m.Used = sub(m.Total, m.Available)
	if s.src.Container {
		m.CgCurrent, _ = s.fs.uint(cgroupRoot + "memory.current")
	}
	switch limit := s.src.Values["mem_limit"]; limit.Path {
	case "/proc/meminfo":
		m.Limit = m.Total
	case "":
	default:
		m.Limit, _ = s.fs.uint(limit.Path)
	}
	return m
}

// free liest den freien Platz der Dateisysteme. Pfade, die es nicht gibt,
// fehlen in der Zeile; liegen zwei auf demselben Dateisystem, steht es nur
// unter dem ersten.
func (s *Sampler) free() []Free {
	var out []Free
	seen := map[unix.Fsid]bool{}
	for _, path := range s.paths {
		var st unix.Statfs_t
		if unix.Statfs(s.fs.root+path, &st) != nil || seen[st.Fsid] {
			continue
		}
		seen[st.Fsid] = true
		bsize := uint64(st.Bsize) //nolint:gosec // Blockgröße ist nie negativ
		out = append(out, Free{Path: path, Avail: st.Bavail * bsize, Total: st.Blocks * bsize})
	}
	return out
}

// sub zieht ab und fängt zurückgesetzte Zähler ab (Neustart des Containers).
func sub(b, a uint64) uint64 {
	if b < a {
		return 0
	}
	return b - a
}

func pct(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return 100 * part / whole
}

// round hält die Zeilen kurz: eine Nachkommastelle reicht für Anteile.
func round(v float64) float64 { return math.Round(v*10) / 10 }
