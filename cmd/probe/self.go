package main

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// monotonic liest CLOCK_MONOTONIC. Die Uhr gilt für den ganzen Kernel, im
// Container also die des Hosts: Sie läuft weiter, während der Container
// eingefroren ist, und eine Lücke zwischen zwei Zeilen wird sichtbar. In einer
// angehaltenen VM steht sie dagegen mit; dann zeigt nur die Wanduhr die Pause
// (siehe analysis.findGaps).
func monotonic() time.Duration {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return time.Duration(time.Now().UnixNano())
	}
	return time.Duration(ts.Nano())
}

// selfUsage rechnet die eigene CPU-Zeit in Mengen je Intervall um.
type selfUsage struct{ last time.Duration }

func (u *selfUsage) cpuSince(time.Duration) float64 {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	total := time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
	delta := total - u.last
	u.last = total
	return float64(delta.Microseconds()) / 1000
}

// rss liest den belegten Speicher aus /proc/self/statm (nur Linux).
func rss() uint64 {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0
	}
	pages, _ := strconv.ParseUint(f[1], 10, 64)
	return pages * uint64(os.Getpagesize())
}
