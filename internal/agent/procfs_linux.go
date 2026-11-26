//go:build linux

package agent

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Beobachtungen am Prozessbaum eines Jobs über /proc (C-4). Der Baum sind alle
// Prozesse der Sitzung des Kommandos: Der Wrapper ist ihr Führer (Setsid), und
// auch Hintergrundprozesse, die dort bleiben, gehören dazu.

// clockTick ist USER_HZ, die Einheit der CPU-Zeiten in /proc/<pid>/stat.
// Unter Linux fest 100.
const clockTick = 10 // Millisekunden je Tick

// procStat sind die Felder aus /proc/<pid>/stat, die der Agent braucht.
type procStat struct {
	state        byte
	session      int
	utime, stime uint64
	start        uint64
}

// readStat liest /proc/<pid>/stat. Der Name steht in Klammern und darf selbst
// Klammern und Leerzeichen enthalten: Maßgeblich ist die letzte ")".
func readStat(pid int) (procStat, bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return procStat{}, false
	}
	return parseStat(string(raw))
}

func parseStat(raw string) (procStat, bool) {
	i := strings.LastIndexByte(raw, ')')
	if i < 0 {
		return procStat{}, false
	}
	f := strings.Fields(raw[i+1:]) // f[0] ist das 3. Feld (state)
	if len(f) < 20 {
		return procStat{}, false
	}
	num := func(n int) uint64 { v, _ := strconv.ParseUint(f[n-3], 10, 64); return v }
	session, _ := strconv.Atoi(f[3])
	return procStat{state: f[0][0], session: session, utime: num(14), stime: num(15), start: num(22)}, true
}

// procStartTime ist die Startzeit des Prozesses (0, wenn nicht lesbar).
func procStartTime(pid int) uint64 {
	st, _ := readStat(pid)
	return st.start
}

// sameProcess prüft, dass die PID noch derselbe, lebende Prozess ist.
func sameProcess(pid int, start uint64) bool {
	st, ok := readStat(pid)
	return ok && st.state != 'Z' && (start == 0 || st.start == start)
}

// bootIDFile nennt die Kennung des laufenden Systemstarts.
const bootIDFile = "/proc/sys/kernel/random/boot_id"

// readBootID liest die Kennung des Systemstarts ("" wenn nicht lesbar). Nach
// einem Neustart des Servers ist sie eine andere: Jede PID aus der Zeit davor
// gehört dann zu keinem Prozess mehr, den der Agent kennt.
func readBootID() string {
	raw, err := os.ReadFile(bootIDFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// groupIsOurs prüft vor einem Signal an die Gruppe -pid, dass die PID nicht
// inzwischen einem fremden Prozess gehört. Gibt es zur PID einen Prozess, muss
// es der Wrapper sein (Startzeit). Gibt es keinen, kann die Gruppe nur noch aus
// Nachfahren des Wrappers bestehen: Eine PID, die eine Gruppe führt, vergibt der
// Kernel nicht neu, solange die Gruppe Mitglieder hat.
func groupIsOurs(pid int, start uint64) bool {
	st, ok := readStat(pid)
	return !ok || start == 0 || st.start == start
}

// reapWrapper sammelt den beendeten Wrapper eines Jobs ein, den ein früheres
// Abbild dieses Prozesses gestartet hat (exec behält die PID, und damit bleibt
// der Job ein Kind): Ohne das stünde er bis zum Ende des Agents als Zombie da.
// Der Wrapper schreibt seinen Exit-Code und endet gleich darauf, deshalb gibt
// es ihm einen Augenblick. Gesammelt wird nur, was nach Startzeit der Wrapper
// ist - Wait4 auf eine wiederverwendete PID risse einem fremden Kind den
// Exit-Status weg. Kein Kind dieses Prozesses (ECHILD) ist kein Fehler.
func reapWrapper(pid int, start uint64) {
	if pid <= 0 || start == 0 {
		return
	}
	for range 20 {
		st, ok := readStat(pid)
		if !ok || st.start != start {
			return
		}
		if st.state == 'Z' {
			var ws syscall.WaitStatus
			_, _ = syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// sampleSession summiert CPU-Zeit und E/A aller Prozesse der Sitzung sid und
// prüft, ob einer auf eine Dateisperre wartet. false: /proc ist nicht lesbar.
func sampleSession(sid int) (sample, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return sample{}, false
	}
	var s sample
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		st, ok := readStat(pid)
		if !ok || st.session != sid || st.state == 'Z' {
			continue
		}
		s.procs++
		s.cpuMillis += (st.utime + st.stime) * clockTick
		s.ioBytes += readIO(pid)
		pids = append(pids, pid)
	}
	s.lockWait = waitsOnLock(pids)
	return s, true
}

// readIO liest rchar und wchar aus /proc/<pid>/io: alles, was der Prozess
// gelesen und geschrieben hat, auch aus dem Seitencache und über Sockets.
func readIO(pid int) uint64 {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/io")
	if err != nil {
		return 0
	}
	var total uint64
	for _, line := range strings.Split(string(raw), "\n") {
		key, val, ok := strings.Cut(line, ":")
		if ok && (key == "rchar" || key == "wchar") {
			n, _ := strconv.ParseUint(strings.TrimSpace(val), 10, 64)
			total += n
		}
	}
	return total
}

// waitsOnLock meldet, ob einer der Prozesse in /proc/locks als Wartender
// steht (Zeilen mit "->"). Format: "ID: [->] TYP ADVISORY READ|WRITE PID …".
// Ein Prozess, der eine Sperre nur in einer Schleife wiederholt (wie
// DPkg::Lock::Timeout), steht dort nicht - die Angabe ist eine Näherung.
func waitsOnLock(pids []int) bool {
	if len(pids) == 0 {
		return false
	}
	raw, err := os.ReadFile("/proc/locks")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || f[1] != "->" {
			continue
		}
		waiter, _ := strconv.Atoi(f[5])
		for _, pid := range pids {
			if pid == waiter {
				return true
			}
		}
	}
	return false
}

// Linux-Konstanten für fallocate(2): Loch stanzen, Dateigröße behalten.
const (
	fallocKeepSize  = 0x01
	fallocPunchHole = 0x02
)

// punchHole gibt den Platz von [off, off+n) frei, ohne die Dateigröße zu ändern.
func punchHole(path string, off, n int64) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return syscall.Fallocate(int(f.Fd()), fallocPunchHole|fallocKeepSize, off, n)
}
