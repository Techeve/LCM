package host

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Die Testdaten sind eingefrorene Kopien von /proc und /sys aus CT 120
// (LXC mit lxcfs) und VM 150 auf pve-test, aufgenommen am 08.11.2026.

func TestQuellenImContainer(t *testing.T) {
	src := Detect("testdata/ct120")
	if !src.Container || !src.LXCFS {
		t.Fatalf("Container/lxcfs nicht erkannt: %+v", src)
	}
	want := map[string]string{
		"load":       ScopeHost,      // lxcfs ohne -l: 1.063 Tasks bei 20 im Container
		"cpu":        ScopeMixed,     // lxcfs /proc/stat
		"memory":     ScopeContainer, // lxcfs /proc/meminfo
		"mem_limit":  ScopeContainer, // memory.max steht auf max, Grenze aus MemTotal
		"psi_cpu":    ScopeContainer,
		"psi_memory": ScopeContainer,
		"psi_io":     ScopeContainer, // aus der cgroup, nicht aus lxcfs
		"disks":      ScopeHost,
		"cg_cpu":     ScopeContainer,
		"cg_io":      ScopeContainer,
	}
	for key, scope := range want {
		if got := src.Values[key].Scope; got != scope {
			t.Errorf("%s: %q, erwartet %q (%s)", key, got, scope, src.Values[key].Path)
		}
	}
	if src.Values["psi_io"].Path != "/sys/fs/cgroup/io.pressure" || src.Values["mem_limit"].Path != "/proc/meminfo" {
		t.Errorf("Pfade: %+v", src.Values)
	}
	if strings.Join(src.Disks, ",") != "sda" {
		t.Errorf("Platten %v, erwartet nur sda aus io.stat", src.Disks)
	}
}

func TestQuellenInDerVM(t *testing.T) {
	src := Detect("testdata/vm150")
	if src.Container {
		t.Fatal("VM als Container erkannt")
	}
	for _, key := range []string{"load", "cpu", "memory", "psi_cpu", "psi_io", "disks"} {
		if got := src.Values[key].Scope; got != ScopeSystem {
			t.Errorf("%s: %q, erwartet system", key, got)
		}
	}
	for _, key := range []string{"mem_limit", "cg_cpu", "cg_io"} {
		if _, ok := src.Values[key]; ok {
			t.Errorf("%s dürfte es in der VM nicht geben", key)
		}
	}
	if strings.Join(src.Disks, ",") != "sda" {
		t.Errorf("Platten %v, erwartet sda ohne Partitionen und sr0", src.Disks)
	}
}

// copyTree kopiert Testdaten, damit der Test Zähler fortschreiben kann.
func copyTree(t *testing.T, from string) string {
	t.Helper()
	to := t.TempDir()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return to
}

func write(t *testing.T, root, path, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMengenJeIntervallImContainer(t *testing.T) {
	root := copyTree(t, "testdata/ct120")
	s := NewSampler(root, Detect(root), []string{"/"})
	first := s.Sample(10 * time.Second)
	if first.CPU != nil || first.Mem == nil || len(first.Load) != 3 {
		t.Fatalf("erste Messung: %+v", first)
	}
	if first.Mem.Limit != 2048*1024*1024 || first.Mem.Total != first.Mem.Limit {
		t.Errorf("Grenze %d, erwartet 2 GiB aus lxcfs", first.Mem.Limit)
	}
	if len(first.Free) != 1 || first.Free[0].Total == 0 {
		t.Errorf("freier Platz fehlt: %+v", first.Free)
	}

	// Zwei Sekunden später: 200 Ticks, davon 50 iowait; 1 s I/O-Druck;
	// 1 MiB geschrieben in 10 Vorgängen à 4 ms.
	s.prev.cpu, _ = parseCPU("cpu  0 0 0 950 0 0 0 0 0 0\n")
	write(t, root, "proc/stat", "cpu  100 0 50 1000 50 0 0 0 0 0\n")
	write(t, root, "sys/fs/cgroup/io.pressure", "some avg10=0 avg60=0 avg300=0 total=1000000\nfull avg10=0 avg60=0 avg300=0 total=500000\n")
	s.prev.psi["psi_io"] = [2]uint64{0, 0}
	d := s.prev.disks["sda"]
	write(t, root, "proc/diskstats", diskLine(d, 10, 2048, 40))
	write(t, root, "sys/fs/cgroup/cpu.stat", "usage_usec 1000000\nthrottled_usec 3000\n")
	s.prev.cgUsage, s.prev.cgThrot = 0, 0

	got := s.Sample(12 * time.Second)
	if got.CPU == nil || got.CPU.IOWait != 20 || got.CPU.User != 40 {
		t.Errorf("CPU %+v, erwartet iowait 20 %%, user 40 %%", got.CPU)
	}
	if got.PSI == nil || got.PSI.IOSome != 50 || got.PSI.IOFull != 25 {
		t.Errorf("PSI %+v, erwartet io some 50 %%, full 25 %%", got.PSI)
	}
	if got.CgCPU == nil || got.CgCPU.Usage != 50 || got.CgCPU.ThrottledMS != 3 {
		t.Errorf("cgroup-CPU %+v, erwartet 50 %% eines Kerns", got.CgCPU)
	}
	if len(got.Disks) != 1 || got.Disks[0].WriteBytes != 1<<20 || got.Disks[0].Writes != 10 || got.Disks[0].AwaitMS != 4 {
		t.Errorf("Platte %+v", got.Disks)
	}
}

// diskLine schreibt die Zeile von sda mit zusätzlichen Schreibvorgängen.
func diskLine(d diskCounters, writes, sectors, ms uint64) string {
	f := []uint64{d.reads, 0, d.readSectors, d.readMS, d.writes + writes, 0, d.writeSectors + sectors, d.writeMS + ms, 0, d.ioMS + 20, 0}
	parts := []string{"8", "0", "sda"}
	for _, v := range f {
		parts = append(parts, itoa(v))
	}
	return "   " + strings.Join(append(parts, "0", "0", "0", "0", "0", "0"), " ") + "\n"
}

func itoa(v uint64) string { return strconv.FormatUint(v, 10) }

// Am 08.11.2026 um 20:32:09 lief auf lcm-prod die Summe von user und system
// unter lxcfs rückwärts; der Bericht zeigte danach iowait 457 %.
func TestCPUAnteileBeiRueckwaertsZaehler(t *testing.T) {
	a := cpuTicks{user: 5000, system: 3000, idle: 90000, iowait: 100}
	b := cpuTicks{user: 4000, system: 2500, idle: 90200, iowait: 300}
	cpu := cpuShares(a, b)
	if cpu == nil {
		t.Fatal("keine Anteile")
	}
	if cpu.User != 0 || cpu.System != 0 || cpu.IOWait != 50 || cpu.Idle != 50 {
		t.Errorf("Anteile %+v, erwartet iowait 50 und idle 50", *cpu)
	}
	if sum := cpu.User + cpu.System + cpu.IOWait + cpu.Steal + cpu.Idle; sum != 100 {
		t.Errorf("Summe %v statt 100", sum)
	}
}
