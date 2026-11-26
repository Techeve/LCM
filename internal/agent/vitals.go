package agent

import (
	"syscall"
	"time"

	"LCM/internal/remote/wire"
)

// sample ist eine Messung am Prozessbaum eines Jobs (siehe procfs_linux.go).
type sample struct {
	procs     int
	cpuMillis uint64 // CPU-Zeit aller Prozesse zusammen
	ioBytes   uint64 // gelesene und geschriebene Bytes aller Prozesse zusammen
	lockWait  bool   // ein Prozess wartet auf eine Dateisperre
}

// processAlive meldet, ob der Prozess pid noch läuft - derselbe, der bei start
// begann; ein Zombie zählt als beendet. Einsammeln tut es nicht (reapWrapper).
func processAlive(pid int, start uint64) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return err == syscall.EPERM
	}
	return sameProcess(pid, start)
}

// meter bildet aus zwei Messungen die Beobachtungen des Lebenszeichens: Die
// CPU-Zeit und die E/A-Bytes sind Unterschiede seit der vorigen Messung.
type meter struct{ prev sample }

// vitals misst den Job jetzt.
func (m *meter) vitals(j *job, now time.Time) wire.Vitals {
	v := wire.Vitals{SilentSec: int(now.Sub(j.lastOutput()).Seconds())}
	pid := j.pid()
	cur, ok := sample{}, false
	if pid > 0 {
		cur, ok = sampleSession(pid)
	}
	if !ok {
		v.Alive = !j.gone()
		return v
	}
	v.Alive = cur.procs > 0
	v.Procs = cur.procs
	// Beendete Prozesse nehmen ihre Zeiten mit: Fällt die Summe, gilt der
	// Unterschied als 0.
	v.CPUMillis = int64(max(cur.cpuMillis, m.prev.cpuMillis) - m.prev.cpuMillis)
	v.IOBytes = int64(max(cur.ioBytes, m.prev.ioBytes) - m.prev.ioBytes)
	if cur.lockWait {
		v.Waiting = wire.WaitingLock
	}
	m.prev = cur
	return v
}
