// Package host liest die Umstände des Rechners, auf dem LCM läuft: Last,
// CPU, Speicher, Platten, Druck (PSI) und freien Platz. In einem Container
// liefern nicht alle Quellen Werte des Containers; jede Quelle bekommt
// deshalb eine Herkunft (siehe doc/plan-2026-11-08-monitoring-host-datenbank.md,
// Abschnitt „Befund in CT 120 und VM 150").
package host

import (
	"slices"
	"strconv"
	"strings"
)

// Herkunft eines Werts.
const (
	ScopeSystem    = "system"    // eigener Kernel: VM oder Blech
	ScopeContainer = "container" // der Container selbst
	ScopeHost      = "host"      // der Host unter dem Container, durchgereicht
	ScopeMixed     = "mixed"     // teils Container, teils Host
)

// Origin sagt, woher ein Wert kommt und wessen Wert er ist.
type Origin struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

// Sources beschreibt die lesbaren Quellen. Fehlt ein Schlüssel in Values,
// ist die Quelle nicht lesbar.
type Sources struct {
	Container bool              `json:"container"`
	LXCFS     bool              `json:"lxcfs"`
	Values    map[string]Origin `json:"values"`
	Disks     []string          `json:"disks"`
}

// Schlüssel in Sources.Values, zugleich die Reihenfolge der Ausgabe.
var SourceKeys = []string{"load", "cpu", "cg_cpu", "memory", "mem_limit", "psi_cpu", "psi_memory", "psi_io", "disks", "cg_io"}

const cgroupRoot = "/sys/fs/cgroup/"

// Detect prüft, welche Quellen unter root lesbar sind ("" = echtes System).
//
// Im Container ist /sys/fs/cgroup die Wurzel des cgroup-Namensraums, also
// der Container selbst. Erkennbar ist das an memory.current, das die echte
// Wurzel eines Kernels nicht hat.
func Detect(root string) Sources {
	fs := files{root}
	src := Sources{Container: fs.exists(cgroupRoot + "memory.current"), Values: map[string]Origin{}}
	lxcfs := fs.lxcfsMounts()
	src.LXCFS = len(lxcfs) > 0
	own := ScopeSystem
	if src.Container {
		own = ScopeContainer
	}
	// Was in einem Container ohne lxcfs aus /proc kommt, ist der Host.
	procScope := func(path string, withLXCFS string) string {
		switch {
		case !src.Container:
			return ScopeSystem
		case lxcfs[path]:
			return withLXCFS
		}
		return ScopeHost
	}
	set := func(key, path, scope string) {
		if fs.exists(path) {
			src.Values[key] = Origin{Scope: scope, Path: path}
		}
	}

	set("load", "/proc/loadavg", fs.loadScope(src.Container, lxcfs["/proc/loadavg"]))
	set("cpu", "/proc/stat", procScope("/proc/stat", ScopeMixed))
	set("memory", "/proc/meminfo", procScope("/proc/meminfo", ScopeContainer))
	for _, kind := range []string{"cpu", "memory", "io"} {
		key := "psi_" + kind
		set(key, cgroupRoot+kind+".pressure", own)
		if _, ok := src.Values[key]; !ok {
			// lxcfs mischt hier (CPU vom Container, I/O vom Host), deshalb
			// gilt /proc/pressure im Container immer als Hostwert.
			set(key, "/proc/pressure/"+kind, procScope("", ScopeHost))
		}
	}
	src.detectMemLimit(fs, lxcfs["/proc/meminfo"])
	if src.Container {
		set("cg_cpu", cgroupRoot+"cpu.stat", ScopeContainer)
		set("cg_io", cgroupRoot+"io.stat", ScopeContainer)
	}
	set("disks", "/proc/diskstats", procScope("", ScopeHost))
	src.Disks = fs.disks(src.Container)
	return src
}

// detectMemLimit findet die Speichergrenze: memory.max der cgroup, sonst im
// Container MemTotal aus lxcfs. Proxmox setzt die Grenze eine Stufe über der
// Wurzel des Namensraums, von innen steht dort „max".
func (src *Sources) detectMemLimit(fs files, lxcfsMeminfo bool) {
	if v, ok := fs.uint(cgroupRoot + "memory.max"); ok && v > 0 {
		src.Values["mem_limit"] = Origin{Scope: ScopeContainer, Path: cgroupRoot + "memory.max"}
		return
	}
	if src.Container && lxcfsMeminfo {
		src.Values["mem_limit"] = Origin{Scope: ScopeContainer, Path: "/proc/meminfo"}
	}
}

// loadScope: lxcfs liefert die Last des Containers nur mit -l, sonst reicht
// es die des Hosts durch. Erkennbar an der Zahl der Tasks in loadavg: Nennt
// sie deutlich mehr, als der Container hat, ist es der Host.
func (fs files) loadScope(container, lxcfs bool) string {
	if !container {
		return ScopeSystem
	}
	pids, ok := fs.uint(cgroupRoot + "pids.current")
	if !lxcfs || !ok {
		return ScopeHost
	}
	fields := strings.Fields(fs.read("/proc/loadavg"))
	if len(fields) < 4 {
		return ScopeHost
	}
	_, total, _ := strings.Cut(fields[3], "/")
	if n, err := strconv.ParseUint(total, 10, 64); err == nil && n <= 2*pids+16 {
		return ScopeContainer
	}
	return ScopeHost
}

// disks liefert die Platten, die gemessen werden: im Container die Geräte
// aus io.stat, sonst alle ganzen Platten mit Verkehr.
func (fs files) disks(container bool) []string {
	stats := parseDiskstats(fs.read("/proc/diskstats"))
	var names []string
	if container {
		used := parseIOStat(fs.read(cgroupRoot + "io.stat"))
		for _, d := range stats {
			if _, ok := used[d.dev]; ok {
				names = append(names, d.name)
			}
		}
		return names
	}
	for _, d := range stats {
		if d.reads+d.writes > 0 && !fs.isPartition(d.name) && !isPseudoDisk(d.name) {
			names = append(names, d.name)
		}
	}
	slices.Sort(names)
	return names
}

func (fs files) isPartition(name string) bool {
	return fs.exists("/sys/class/block/" + name + "/partition")
}

func isPseudoDisk(name string) bool {
	for _, prefix := range []string{"loop", "ram", "zram", "sr", "fd"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// lxcfsMounts liefert die Einhängepunkte von lxcfs.
func (fs files) lxcfsMounts() map[string]bool {
	mounts := map[string]bool{}
	for _, line := range strings.Split(fs.read("/proc/mounts"), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[2] == "fuse.lxcfs" {
			mounts[f[1]] = true
		}
	}
	return mounts
}
