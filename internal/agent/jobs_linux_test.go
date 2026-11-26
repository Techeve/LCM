//go:build linux

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"LCM/internal/remote/wire"
)

// Tests für die Teile, die /proc und Linux-Aufrufe brauchen (C-4, Platz der
// Ausgabedatei, Scope-Erkennung). Sie laufen in `docker run golang:1.27`.

func TestParseStatMitKlammernImNamen(t *testing.T) {
	// Name "(a) b" enthält Klammer und Leerzeichen.
	raw := "4242 ((a) b) S 1 4242 4242 0 -1 4194560 100 0 0 0 7 5 0 0 20 0 1 0 9876 1000 100 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0"
	st, ok := parseStat(raw)
	if !ok || st.state != 'S' || st.session != 4242 || st.utime != 7 || st.stime != 5 || st.start != 9876 {
		t.Fatalf("parseStat = %+v (%v)", st, ok)
	}
	if _, ok := parseStat("kaputt"); ok {
		t.Fatal("kaputte zeile gelesen")
	}
}

// collectVitals führt cmd aus und sammelt die Vitals jedes Lebenszeichens.
func collectVitals(t *testing.T, cmd string) []wire.Vitals {
	t.Helper()
	r := newTestRunner(t)
	r.progressEvery = 100 * time.Millisecond
	var mu sync.Mutex
	var seen []wire.Vitals
	res := r.Run(wire.Command{ID: "vitals", Cmd: cmd}, func(int) {
		if v, ok := r.Vitals("vitals"); ok {
			mu.Lock()
			seen = append(seen, v)
			mu.Unlock()
		}
	})
	if res.ExitCode != 0 {
		t.Fatalf("ergebnis = %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	return seen
}

// TestVitalsMelden... : Der Agent sieht Prozessbaum, CPU-Zeit und Stille.
func TestVitalsMeldenProzessbaumCPUUndStille(t *testing.T) {
	seen := collectVitals(t, `echo los; i=0; while [ $i -lt 400000 ]; do i=$((i+1)); done; sleep 1.5`)
	if len(seen) < 3 {
		t.Fatalf("%d lebenszeichen", len(seen))
	}
	var cpu int64
	var silent int
	for _, v := range seen {
		if !v.Alive || v.Procs < 1 {
			t.Errorf("prozessbaum nicht gesehen: %+v", v)
		}
		cpu += v.CPUMillis
		silent = max(silent, v.SilentSec)
	}
	if cpu <= 0 {
		t.Errorf("keine cpu-zeit gemessen: %+v", seen)
	}
	if silent < 1 {
		t.Errorf("stille nicht gemessen (nach der ersten zeile 1,5 s ruhe): %+v", seen)
	}
}

// TestVitalsMessenEAUndKinder: Die Messung umfasst auch Kindprozesse.
func TestVitalsMessenEAUndKinder(t *testing.T) {
	seen := collectVitals(t, `(dd if=/dev/zero of=/dev/null bs=1M count=3000 2>/dev/null; sleep 0.5) & wait`)
	var io int64
	procs := 0
	for _, v := range seen {
		io += v.IOBytes
		procs = max(procs, v.Procs)
	}
	if procs < 2 {
		t.Errorf("kinder nicht im prozessbaum (höchstens %d prozesse)", procs)
	}
	if len(seen) > 0 && io <= 0 {
		t.Errorf("keine e/a gemessen: %+v", seen)
	}
}

// TestVitalsErkennenWartenAufEineSperre: Ein Prozess, der auf eine flock-Sperre
// wartet, steht in /proc/locks - das Lebenszeichen sagt „wartet auf Sperre“.
func TestVitalsErkennenWartenAufEineSperre(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock fehlt")
	}
	// busybox-flock (Alpine, das CI-Image) kennt kein -w.
	if help, _ := exec.Command("flock", "--help").CombinedOutput(); !strings.Contains(string(help), "-w") {
		t.Skip("flock ohne -w (busybox)")
	}
	lock := filepath.Join(t.TempDir(), "sperre")
	holder := exec.Command("flock", lock, "sleep", "4")
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Process.Kill(); _ = holder.Wait() }()
	time.Sleep(300 * time.Millisecond) // der Halter hat die Sperre

	r := newTestRunner(t)
	r.progressEvery = 100 * time.Millisecond
	var mu sync.Mutex
	waiting := false
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(wire.Command{ID: "sperre", Cmd: "flock -w 3 " + lock + " true"}, func(int) {
			if v, ok := r.Vitals("sperre"); ok && v.Waiting == wire.WaitingLock {
				mu.Lock()
				waiting = true
				mu.Unlock()
			}
		})
	}()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if !waiting {
		t.Error("das warten auf die sperre wurde nicht gemeldet")
	}
}

// TestVitalsDesWiederaufgenommenenJobs: Auch ein Job, den ein neuer Runner
// übernimmt, wird beobachtet (die PID kommt aus der Ablage).
func TestVitalsDesWiederaufgenommenenJobs(t *testing.T) {
	dir := t.TempDir()
	old := newTestRunner(t).WithJobsDir(dir)
	go old.Run(wire.Command{ID: "alt", Cmd: "sleep 1.5"}, nil)
	waitRunning(t, old, "alt")
	waitFor(t, 5*time.Second, "pid-datei", func() bool { return fileExists(filepath.Join(dir, "alt", pidFile)) })
	old.Detach()

	next := newTestRunner(t).WithJobsDir(dir)
	next.progressEvery = 100 * time.Millisecond
	var mu sync.Mutex
	alive := false
	done := make(chan struct{})
	next.Adopt(func(id string, follow func(func(int)) wire.Result) {
		go func() {
			defer close(done)
			follow(func(int) {
				if v, ok := next.Vitals("alt"); ok && v.Alive && v.Procs > 0 {
					mu.Lock()
					alive = true
					mu.Unlock()
				}
			})
		}()
	})
	<-done
	mu.Lock()
	defer mu.Unlock()
	if !alive {
		t.Error("der übernommene job wurde nicht als lebend beobachtet")
	}
}

// TestAusgabedateiBleibtKlein: Ein Kommando, das viel ausgibt, füllt die Platte
// nicht: trim stanzt die Mitte frei, Anfang und Ende bleiben lesbar.
func TestAusgabedateiBleibtKlein(t *testing.T) {
	r := newTestRunner(t)
	cmd := `echo ANFANG; head -c 60000000 /dev/zero | tr '\0' 'x'; echo; echo ENDE; sleep 2`
	done := make(chan wire.Result, 1)
	go func() { done <- r.Run(wire.Command{ID: "gross", Cmd: cmd}, nil) }()
	waitRunning(t, r, "gross")
	out := filepath.Join(r.store.dir, "gross", outFile)

	var used, size int64
	waitFor(t, 10*time.Second, "ausgabe geschrieben", func() bool {
		info, err := os.Stat(out)
		if err != nil {
			return false
		}
		size = info.Size()
		return size > 60000000
	})
	time.Sleep(400 * time.Millisecond) // trim läuft im Takt des Pollens
	var st syscall.Stat_t
	if err := syscall.Stat(out, &st); err != nil {
		t.Fatal(err)
	}
	used = st.Blocks * 512
	res := <-done
	if used >= size/2 {
		r2 := newTestRunner(t)
		if err := os.WriteFile(filepath.Join(r2.store.dir, "probe"), nil, 0o600); err == nil {
			if punchHole(filepath.Join(r2.store.dir, "probe"), 0, 1) != nil {
				t.Skip("dateisystem unterstützt kein fallocate(PUNCH_HOLE)")
			}
		}
		t.Fatalf("ausgabedatei belegt %d von %d Bytes - die mitte wurde nicht freigegeben", used, size)
	}
	if !strings.HasPrefix(res.Output, "ANFANG\n") || !strings.HasSuffix(res.Output, "ENDE\n") || !res.Truncated {
		t.Fatalf("anfang oder ende fehlen (truncated=%v): %.40q … %.40q", res.Truncated, res.Output, res.Output[max(0, len(res.Output)-40):])
	}
}

// TestOhneSystemdLaufenKommandosInDerEigenenSitzung: Ohne systemd (Docker,
// OpenRC) erkennt der Runner das und startet direkt.
func TestOhneSystemdLaufenKommandosInDerEigenenSitzung(t *testing.T) {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		t.Skip("hier läuft systemd")
	}
	r := NewRunner().WithJobsDir(t.TempDir())
	if r.useScope() {
		t.Fatal("scope trotz fehlendem systemd")
	}
	res := r.Run(wire.Command{ID: "direkt", Cmd: "echo ok"}, nil)
	if res.Output != "ok\n" {
		t.Fatalf("ergebnis = %+v", res)
	}
	j, _ := r.store.load("direkt")
	if j.meta.Scope != "" {
		t.Errorf("scope vermerkt: %q", j.meta.Scope)
	}
}

// TestKommandoLaeuftInEigenerSitzung: Das Kommando hat eine eigene Sitzung und
// Prozessgruppe - ein Abbruch trifft die ganze Gruppe, und es hängt nicht am
// Terminal des Agents.
func TestKommandoLaeuftInEigenerSitzung(t *testing.T) {
	r := newTestRunner(t)
	res := r.Run(wire.Command{ID: "sitzung", Cmd: `awk '{print $5, $6}' /proc/$$/stat`}, nil)
	f := strings.Fields(res.Output)
	if len(f) != 2 || f[0] != f[1] {
		t.Fatalf("prozessgruppe und sitzung des kommandos: %q", res.Output)
	}
	own, _ := readStat(os.Getpid())
	if f[1] == strconv.Itoa(own.session) {
		t.Fatalf("das kommando läuft in der sitzung des agents: %q", res.Output)
	}
}

// ---- Korrekturen aus der Durchsicht von Paket B/C (23.11.) --------------------

// lebt meldet, ob der Prozess läuft - ein Zombie zählt als beendet.
func lebt(pid int) bool {
	st, ok := readStat(pid)
	return ok && st.state != 'Z'
}

// pidOfFile liest eine PID, die ein Kommando in eine Datei geschrieben hat.
func pidOfFile(t *testing.T, path string) int {
	t.Helper()
	var pid int
	waitFor(t, 5*time.Second, "pid in "+path, func() bool {
		raw, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(raw)))
		return err == nil && pid > 0
	})
	return pid
}

// startJob legt einen Job an und startet ihn von Hand (ohne ihn zu begleiten).
func startJob(t *testing.T, r *Runner, id, cmd string) *job {
	t.Helper()
	j, err := r.store.create(id, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.spawn(j, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "pid-datei", func() bool { return fileExists(j.path(pidFile)) })
	return j
}

func TestBootIDIstLesbar(t *testing.T) {
	if id := readBootID(); len(id) < 16 {
		t.Fatalf("boot-id = %q", id)
	}
}

// TestAdoptHaeltDieStartzeitDesWrappersFest: Fehlt sie in der Ablage, trägt die
// Wiederaufnahme sie nach - sonst ließe sich eine wiederverwendete PID nicht vom
// Wrapper unterscheiden.
func TestAdoptHaeltDieStartzeitDesWrappersFest(t *testing.T) {
	r := newTestRunner(t)
	j := startJob(t, r, "ohne-start", "sleep 30")
	defer killGroup(t, j.pid())
	if err := j.update(func(m *jobMeta) { m.PIDStart = 0 }); err != nil {
		t.Fatal(err)
	}

	next := newTestRunner(t).WithJobsDir(r.store.dir)
	next.Adopt(func(string, func(func(int)) wire.Result) {})
	loaded, err := next.store.load("ohne-start")
	if err != nil || loaded.meta.PIDStart == 0 {
		t.Fatalf("startzeit nach der Wiederaufnahme: %+v (%v)", loaded, err)
	}
	if loaded.meta.PIDStart != procStartTime(j.pid()) {
		t.Error("die nachgetragene Startzeit ist nicht die des Wrappers")
	}
}

// TestJobMitFremderPIDGiltAlsBeendet: Gehört die PID des Wrappers nach einem
// Absturz einem anderen Prozess (andere Startzeit), ist der Job beendet - der
// Agent wartet nicht auf den fremden Prozess.
func TestJobMitFremderPIDGiltAlsBeendet(t *testing.T) {
	s := &jobStore{dir: t.TempDir()}
	j, err := s.create("fremd", "true")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j.path(pidFile), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	j.meta.PIDStart = procStartTime(os.Getpid())
	if j.gone() {
		t.Fatal("der Prozess mit passender Startzeit gilt als beendet")
	}
	j.meta.PIDStart++
	if !j.gone() {
		t.Fatal("ein Prozess mit anderer Startzeit gilt als der Wrapper")
	}
}

// TestAbbruchPruefPIDUndStartzeit: Gehört die PID nicht mehr dem Wrapper, geht
// kein Signal an die Gruppe, die ihr gehört.
func TestAbbruchPruefPIDUndStartzeit(t *testing.T) {
	r := newTestRunner(t)
	r.abortGrace = 200 * time.Millisecond
	j := startJob(t, r, "fremd-abbruch", "sleep 30")
	pid := j.pid()
	defer killGroup(t, pid)
	if err := j.update(func(m *jobMeta) { m.PIDStart++ }); err != nil { // die PID "gehört einem anderen"
		t.Fatal(err)
	}
	r.abort(j)
	time.Sleep(600 * time.Millisecond) // länger als die Frist bis SIGKILL
	if !lebt(pid) {
		t.Fatal("der Abbruch traf einen Prozess, der nicht der Wrapper des Jobs ist")
	}
}

// TestAbbruchTrifftHinterbliebeneNachDerFrist: Endet der Wrapper, aber ein
// Hintergrundprozess des Kommandos läuft weiter (und ignoriert SIGTERM), trifft
// ihn nach der Frist das SIGKILL - nicht nur, solange der Wrapper lebt.
func TestAbbruchTrifftHinterbliebeneNachDerFrist(t *testing.T) {
	r := newTestRunner(t)
	r.abortGrace = 300 * time.Millisecond
	pidPath := filepath.Join(t.TempDir(), "hintergrund")
	j := startJob(t, r, "hinterbliebene",
		"trap '' TERM; sleep 60 & echo $! >"+pidPath+"; exit 0")
	bg := pidOfFile(t, pidPath)
	defer func() { _ = syscall.Kill(bg, syscall.SIGKILL) }()
	waitFor(t, 5*time.Second, "wrapper beendet", func() bool { return fileExists(j.path(exitFile)) })
	waitFor(t, 5*time.Second, "wrapper nicht mehr lebendig", func() bool { return !lebt(j.pid()) })

	r.abort(j)
	waitFor(t, 5*time.Second, "SIGKILL für den hinterbliebenen Prozess", func() bool { return !lebt(bg) })
}

// fakeSystemctl legt ein systemctl ab, das jeden Aufruf festhält und das Signal
// an die Prozessgruppe des Jobs schickt (der Scope ist hier nur die Gruppe).
func fakeSystemctl(t *testing.T, jobsDir string) (path, calls string) {
	t.Helper()
	bin := t.TempDir()
	calls = filepath.Join(bin, "aufrufe")
	script := "#!/bin/sh\necho \"$*\" >>" + calls + "\n" +
		"sig=${2#--signal=SIG}; id=${3#lcm-job-}; id=${id%.scope}\n" +
		"kill -s \"$sig\" -- -\"$(cat " + jobsDir + "/$id/pid)\"\n"
	path = filepath.Join(bin, "systemctl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, calls
}

// TestAbbruchNutztSystemctlKillMitScope: Läuft das Kommando als Scope, geht der
// Abbruch über `systemctl kill --signal=… <scope>` - erst SIGTERM, nach der Frist
// SIGKILL -, nicht über die rohe PID.
func TestAbbruchNutztSystemctlKillMitScope(t *testing.T) {
	r := newTestRunner(t)
	r.abortGrace = 300 * time.Millisecond
	var calls string
	r.systemctl, calls = fakeSystemctl(t, r.store.dir)
	j := startJob(t, r, "scope-abbruch", `trap '' TERM; sleep 60`)
	defer killGroup(t, j.pid())
	// Die PID-Prüfung greift hier nicht: Mit Scope entscheidet systemctl.
	err := j.update(func(m *jobMeta) { m.Scope, m.PIDStart = "lcm-job-scope-abbruch.scope", m.PIDStart+1 })
	if err != nil {
		t.Fatal(err)
	}
	pid := j.pid()
	r.abort(j)
	waitFor(t, 5*time.Second, "kommando beendet", func() bool { return !lebt(pid) })
	raw, _ := os.ReadFile(calls)
	for _, want := range []string{"kill --signal=SIGTERM lcm-job-scope-abbruch.scope", "kill --signal=SIGKILL lcm-job-scope-abbruch.scope"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("systemctl ohne %q aufgerufen: %s", want, raw)
		}
	}
}

// TestWiederaufgenommenerJobWirdEingesammelt: Der Wrapper ist ein Kind dieses
// Prozesses, das niemand mit Wait abholt (wie nach dem exec auf das neue Binary);
// endet er, bleibt er nicht als Zombie stehen.
func TestWiederaufgenommenerJobWirdEingesammelt(t *testing.T) {
	r := newTestRunner(t)
	j := startJob(t, r, "zombie", "sleep 0.3")
	pid := j.pid()
	adopted, err := r.store.load("zombie") // ein Job ohne proc, wie ihn Adopt führt
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.exited(adopted):
	case <-time.After(10 * time.Second):
		t.Fatal("das Ende des Jobs wurde nie erkannt")
	}
	waitFor(t, 5*time.Second, "wrapper eingesammelt", func() bool {
		_, exists := readStat(pid)
		return !exists
	})
}
