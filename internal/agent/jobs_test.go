package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"LCM/internal/remote/wire"
)

// newTestRunner baut einen Runner mit eigener Ablage und kurzen Zeiten.
func newTestRunner(t *testing.T) *Runner {
	t.Helper()
	r := NewRunner().WithJobsDir(t.TempDir())
	r.pollEvery = 20 * time.Millisecond
	r.systemdRun = "" // ohne systemd: eigene Sitzung
	return r
}

// killGroup beendet die Prozessgruppe des Kommandos hart, wie ein OOM-Kill.
func killGroup(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
}

// waitFor wartet höchstens d auf die Bedingung.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("zeitüberschreitung (%s): %s", d, what)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestWrapperSchreibtAusgabeUndExitCodeInDateien: Das Kommando hängt an keiner
// Pipe des Agents - Ausgabe und Exit-Code liegen in Dateien der Ablage, auch
// wenn der Agent nie wieder hinsieht.
func TestWrapperSchreibtAusgabeUndExitCodeInDateien(t *testing.T) {
	r := newTestRunner(t)
	j, err := r.store.create("wrapper", "echo eins; echo zwei >&2; exit 7")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.spawn(j, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "exit-datei", func() bool { return fileExists(j.path(exitFile)) })

	if out, _ := os.ReadFile(j.path(outFile)); string(out) != "eins\nzwei\n" {
		t.Errorf("ausgabedatei = %q", out)
	}
	if code, ok := j.readExit(); !ok || code != 7 {
		t.Errorf("exit-datei = (%d, %v), erwartet 7", code, ok)
	}
	if j.pid() == 0 {
		t.Error("der wrapper hat seine pid nicht geschrieben")
	}
}

// TestKommandoHatKeineStdoutPipeZumAgent: Das Kommando schreibt noch, nachdem
// der Agent sein Ende der Pipe längst geschlossen hätte - ein SIGPIPE träfe es.
// Hier gibt es keine Pipe: Es schreibt in die Datei, ganz gleich, ob jemand liest.
func TestKommandoHatKeineStdoutPipeZumAgent(t *testing.T) {
	r := newTestRunner(t)
	res := r.Run(wire.Command{ID: "pipe", Cmd: "sleep 0.3; echo spaet; echo $?"}, nil)
	if res.ExitCode != 0 || !strings.HasPrefix(res.Output, "spaet\n") {
		t.Fatalf("ergebnis = %+v", res)
	}
	// stdin ist /dev/null, nicht der Agent.
	res = r.Run(wire.Command{ID: "stdin", Cmd: "cat; echo ende"}, nil)
	if res.Output != "ende\n" {
		t.Fatalf("stdin ohne Eingabe: %q", res.Output)
	}
}

// TestStdinBerueHrtDiePlatteNicht: Das Passwort für `sudo -S` liegt weder in der
// Kommandodatei noch sonst in der Ablage.
func TestStdinBeruehrtDiePlatteNicht(t *testing.T) {
	r := newTestRunner(t)
	done := make(chan wire.Result, 1)
	go func() {
		done <- r.Run(wire.Command{ID: "geheim", Cmd: "cat >/dev/null; sleep 1", Stdin: "GEHEIMES-PASSWORT"}, nil)
	}()
	waitRunning(t, r, "geheim")
	time.Sleep(200 * time.Millisecond)
	_ = filepath.Walk(r.store.dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if data, _ := os.ReadFile(path); strings.Contains(string(data), "GEHEIMES-PASSWORT") {
				t.Errorf("stdin steht in %s", path)
			}
		}
		return nil
	})
	<-done
}

// TestErgebnisBleibtBisZurBestaetigungInDerAblage: Ein fertiger Job liegt mit
// seinem Ergebnis in der Ablage, bis es bestätigt ist; Ausgabe und
// Kommandodatei sind dann weg.
func TestErgebnisBleibtBisZurBestaetigungInDerAblage(t *testing.T) {
	r := newTestRunner(t)
	res := r.Run(wire.Command{ID: "bleibt", Cmd: "echo hallo"}, nil)
	if res.Output != "hallo\n" {
		t.Fatal(res)
	}
	if ids := r.Pending(); len(ids) != 1 || ids[0] != "bleibt" {
		t.Fatalf("pending = %v", ids)
	}
	stored, ok := r.Result("bleibt")
	if !ok || stored.Output != "hallo\n" {
		t.Fatalf("gespeichertes ergebnis = %+v (%v)", stored, ok)
	}
	dir := filepath.Join(r.store.dir, "bleibt")
	for _, name := range []string{outFile, cmdFile} {
		if fileExists(filepath.Join(dir, name)) {
			t.Errorf("%s liegt nach dem Ende noch da", name)
		}
	}
	r.Ack("bleibt")
	if len(r.Pending()) != 0 || fileExists(dir) {
		t.Fatal("bestätigter job liegt noch in der Ablage")
	}
}

// TestJobUeberlebtAgentNeustart: Der Agent geht (Detach, wie ein Stopp oder
// Absturz), das Kommando läuft weiter, und ein neuer Runner auf derselben Ablage
// nimmt es wieder auf und liefert sein Ergebnis.
func TestJobUeberlebtAgentNeustart(t *testing.T) {
	dir := t.TempDir()
	old := newTestRunner(t).WithJobsDir(dir)
	marker := filepath.Join(t.TempDir(), "lief-weiter")
	first := make(chan wire.Result, 1)
	go func() {
		first <- old.Run(wire.Command{ID: "ueberlebt", Cmd: "sleep 1; echo fertig; touch " + marker}, nil)
	}()
	waitRunning(t, old, "ueberlebt")
	old.Detach() // der alte Agent endet

	select {
	case res := <-first:
		if !strings.Contains(res.Error, "beendet") {
			t.Fatalf("der alte runner liefert ein ergebnis statt abzutreten: %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run kehrte nach Detach nicht zurück")
	}
	if fileExists(marker) {
		t.Fatal("das kommando war schon fertig - der test prüft nichts")
	}

	// Der neue Agent.
	next := newTestRunner(t).WithJobsDir(dir)
	got := make(chan wire.Result, 1)
	next.Adopt(func(id string, follow func(func(int)) wire.Result) {
		go func() { got <- follow(nil) }()
	})
	select {
	case res := <-got:
		if res.ID != "ueberlebt" || res.ExitCode != 0 || res.Output != "fertig\n" || res.Error != "" {
			t.Fatalf("wieder aufgenommener job: %+v", res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("kein ergebnis vom wieder aufgenommenen job")
	}
	if !fileExists(marker) {
		t.Fatal("das kommando lief nicht zu Ende")
	}
	if next.Claim("ueberlebt") {
		t.Fatal("die ID des wieder aufgenommenen jobs gilt als neu - ein Doppel liefe noch einmal")
	}
}

// TestAdoptHaeltDenPlatzBesetzt: Solange ein wieder aufgenommener Job läuft,
// startet kein zweites Kommando daneben.
func TestAdoptHaeltDenPlatzBesetzt(t *testing.T) {
	dir := t.TempDir()
	old := newTestRunner(t).WithJobsDir(dir)
	go old.Run(wire.Command{ID: "alt", Cmd: "sleep 2"}, nil)
	waitRunning(t, old, "alt")
	old.Detach()

	next := newTestRunner(t).WithJobsDir(dir)
	var wg sync.WaitGroup
	wg.Add(1)
	next.Adopt(func(id string, follow func(func(int)) wire.Result) {
		go func() { defer wg.Done(); follow(nil) }()
	})
	started := make(chan time.Time, 1)
	begin := time.Now()
	go func() {
		next.Run(wire.Command{ID: "neu", Cmd: "true"}, nil)
		started <- time.Now()
	}()
	select {
	case at := <-started:
		if at.Sub(begin) < time.Second {
			t.Fatalf("neues kommando lief schon nach %s neben dem alten Job", at.Sub(begin))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("neues kommando kam nie an die Reihe")
	}
	wg.Wait()
}

// TestAdoptMeldetEinenGetoetetenJob: Ist der Wrapper tot, ohne den Exit-Code zu
// schreiben (SIGKILL), erfährt LCM das - es wartet nicht ewig.
func TestAdoptMeldetEinenGetoetetenJob(t *testing.T) {
	dir := t.TempDir()
	old := newTestRunner(t).WithJobsDir(dir)
	go old.Run(wire.Command{ID: "tot", Cmd: "sleep 30"}, nil)
	waitRunning(t, old, "tot")
	old.mu.Lock()
	pid := old.running["tot"].pid()
	old.mu.Unlock()
	waitFor(t, 5*time.Second, "pid-datei", func() bool { return fileExists(filepath.Join(dir, "tot", pidFile)) })
	old.Detach()
	killGroup(t, pid)
	// Im Betrieb sammelt init das verwaiste Kind ein. Hier ist es noch Kind
	// des Testprozesses, und ohne /proc (macOS) gälte der Zombie als lebendig.
	_, _ = syscall.Wait4(pid, nil, 0, nil)

	next := newTestRunner(t).WithJobsDir(dir)
	got := make(chan wire.Result, 1)
	next.Adopt(func(id string, follow func(func(int)) wire.Result) { go func() { got <- follow(nil) }() })
	select {
	case res := <-got:
		if res.ExitCode == 0 || !strings.Contains(res.Error, "ohne exit-code") {
			t.Fatalf("ergebnis = %+v", res)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("der tote job wurde nie gemeldet")
	}
}

// TestPruneRaeumtAufUndLaesstLaufendeStehen: Unbestätigte Ergebnisse sind nach
// Anzahl und Alter begrenzt, Müll ohne meta.json verschwindet, laufende Jobs
// bleiben immer.
func TestPruneRaeumtAufUndLaesstLaufendeStehen(t *testing.T) {
	s := &jobStore{dir: t.TempDir()}
	now := time.Now()
	finish := func(id string, age time.Duration) {
		j, err := s.create(id, "true")
		if err != nil {
			t.Fatal(err)
		}
		if err := j.finish(wire.Result{ID: id}); err != nil {
			t.Fatal(err)
		}
		_ = j.update(func(m *jobMeta) { m.Started, m.Finished = now.Add(-age), now.Add(-age) })
	}
	finish("alt", keepResultsFor+time.Hour)
	for i := 0; i < keepResults+5; i++ {
		finish("neu"+string(rune('a'+i%26))+string(rune('a'+i/26)), time.Duration(i)*time.Minute)
	}
	running, _ := s.create("laeuft", "sleep 1")
	_ = running.update(func(m *jobMeta) { m.Started = now.Add(-30 * 24 * time.Hour) })
	orphan := filepath.Join(s.dir, "muell")
	if err := os.Mkdir(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * orphanAfter)
	_ = os.Chtimes(orphan, old, old)

	s.prune(now)

	if fileExists(filepath.Join(s.dir, "alt")) {
		t.Error("zu altes ergebnis blieb")
	}
	if fileExists(orphan) {
		t.Error("verzeichnis ohne meta.json blieb")
	}
	if !fileExists(filepath.Join(s.dir, "laeuft")) {
		t.Error("laufender job wurde aufgeräumt")
	}
	if n := len(s.withState(jobFinished)); n != keepResults {
		t.Errorf("%d unbestätigte ergebnisse, erlaubt %d", n, keepResults)
	}
}

// TestSafeIDFuehrtNieAusDerAblageHeraus: Eine ID vom Server wird nie zum Pfad.
func TestSafeIDFuehrtNieAusDerAblageHeraus(t *testing.T) {
	for _, id := range []string{"../../etc", "a/b", "", ".", "..", "x y", strings.Repeat("a", 200), "ä"} {
		got := safeID(id)
		if got == "" || got != filepath.Base(got) || strings.ContainsAny(got, "/. ") {
			t.Errorf("safeID(%q) = %q", id, got)
		}
	}
	if got := safeID("0e0a6cbb-aaaa-bbbb-cccc-1234567890ab"); got != "0e0a6cbb-aaaa-bbbb-cccc-1234567890ab" {
		t.Errorf("eine UUID bleibt, wie sie ist: %q", got)
	}
	r := newTestRunner(t)
	if res := r.Run(wire.Command{ID: "../../ausbruch", Cmd: "echo ok"}, nil); res.Output != "ok\n" {
		t.Fatalf("eine fremde ID ließ das kommando scheitern: %+v", res)
	}
}

// TestAbbruchSendetErstSIGTERM: Ein Kommando, das SIGTERM versteht, räumt auf
// und endet von selbst - lange vor der Frist bis zum SIGKILL.
func TestAbbruchSendetErstSIGTERM(t *testing.T) {
	r := newTestRunner(t)
	r.abortGrace = 20 * time.Second
	done := make(chan wire.Result, 1)
	go func() {
		done <- r.Run(wire.Command{ID: "sanft", Cmd: `trap 'echo aufgeraeumt; exit 3' TERM; sleep 60 & wait`}, nil)
	}()
	waitRunning(t, r, "sanft")
	waitFor(t, 5*time.Second, "pid-datei", func() bool { return fileExists(filepath.Join(r.store.dir, "sanft", pidFile)) })
	time.Sleep(200 * time.Millisecond) // die Shell hat ihr trap gesetzt
	start := time.Now()
	r.Cancel("sanft")
	select {
	case res := <-done:
		if res.ExitCode != 3 || !strings.Contains(res.Output, "aufgeraeumt") {
			t.Fatalf("ergebnis = %+v", res)
		}
		if time.Since(start) > 10*time.Second {
			t.Fatal("das kommando endete erst durch SIGKILL")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("abbruch hat das kommando nicht beendet")
	}
}

// TestAbbruchSendetNachFristSIGKILL: Ignoriert das Kommando SIGTERM, trifft es
// nach der Frist das SIGKILL.
func TestAbbruchSendetNachFristSIGKILL(t *testing.T) {
	r := newTestRunner(t)
	r.abortGrace = 400 * time.Millisecond
	done := make(chan wire.Result, 1)
	go func() { done <- r.Run(wire.Command{ID: "stur", Cmd: `trap '' TERM; sleep 60`}, nil) }()
	waitRunning(t, r, "stur")
	waitFor(t, 5*time.Second, "pid-datei", func() bool { return fileExists(filepath.Join(r.store.dir, "stur", pidFile)) })
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	r.Cancel("stur")
	select {
	case res := <-done:
		if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
			t.Fatalf("das kommando endete nach %s - SIGTERM hätte es nicht beenden dürfen", elapsed)
		}
		if res.ExitCode == 0 || !strings.Contains(res.Error, "abgebrochen") {
			t.Fatalf("ergebnis = %+v", res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("auch das SIGKILL kam nicht an")
	}
}

// TestKeinAbbruchDurchStille: Ein Kommando ohne jede Ausgabe läuft, bis es fertig
// ist - der Runner hat keine Stille-Notbremse mehr, auch dann nicht, wenn der
// (veraltete) Server eine kurze Frist mitschickt.
func TestKeinAbbruchDurchStille(t *testing.T) {
	r := newTestRunner(t)
	r.progressEvery = 50 * time.Millisecond
	res := r.Run(wire.Command{ID: "still", Cmd: "sleep 1; echo geschafft", IdleTimeoutSec: 1}, nil)
	if res.ExitCode != 0 || res.Output != "geschafft\n" {
		t.Fatalf("ergebnis = %+v", res)
	}
}

// TestWartendesKommandoMeldetSichMitVitals: Es wartet auf das vorige und sagt es
// (C-5), samt der Angabe, worauf.
func TestWartendesKommandoMeldetSichMitVitals(t *testing.T) {
	r := newTestRunner(t)
	r.progressEvery = 50 * time.Millisecond
	go r.Run(wire.Command{ID: "vorn", Cmd: "sleep 1"}, nil)
	waitRunning(t, r, "vorn")

	var mu sync.Mutex
	var seen []wire.Vitals
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(wire.Command{ID: "hinten", Cmd: "true"}, func(int) {
			if v, ok := r.Vitals("hinten"); ok {
				mu.Lock()
				seen = append(seen, v)
				mu.Unlock()
			}
		})
	}()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 || seen[0].Waiting != wire.WaitingPrevious || !seen[0].Alive {
		t.Fatalf("lebenszeichen des wartenden kommandos: %+v", seen)
	}
}

// TestSystemdRunStartetJedesKommandoAlsScope: Mit systemd-run (hier ein Ersatz,
// der die Optionen festhält) läuft der Wrapper als Scope mit eigener Unit.
func TestSystemdRunStartetJedesKommandoAlsScope(t *testing.T) {
	bin := t.TempDir()
	calls := filepath.Join(bin, "aufrufe")
	script := "#!/bin/sh\necho \"$*\" >>" + calls + "\n" +
		"while [ \"$1\" != -- ]; do shift; done; shift\nexec \"$@\"\n"
	fake := filepath.Join(bin, "systemd-run")
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestRunner(t)
	r.systemdRun = fake

	res := r.Run(wire.Command{ID: "scope-1", Cmd: "echo im scope"}, nil)
	if res.Output != "im scope\n" || res.ExitCode != 0 {
		t.Fatalf("ergebnis = %+v", res)
	}
	raw, _ := os.ReadFile(calls)
	for _, want := range []string{"--scope", "--collect", "--unit=lcm-job-scope-1", "/bin/sh -c"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("systemd-run ohne %q aufgerufen: %s", want, raw)
		}
	}
	j, err := r.store.load("scope-1")
	if err != nil || j.meta.Scope != "lcm-job-scope-1.scope" {
		t.Errorf("scope nicht in der ablage vermerkt: %+v (%v)", j, err)
	}
}

// TestPanicErgebnisKommtAn: Die Ablage ändert nichts am Panic-Schutz (B17):
// Ein Panic im Runner liefert ein Fehlerergebnis.
func TestPanicErgebnisKommtAn(t *testing.T) {
	c, fake := testClient()
	c.run = func(wire.Command, func(int)) wire.Result { panic("kaputt") }
	c.handleCommand(commandPayload(t, wire.Command{ID: "panik", Cmd: "true"}))
	waitResults(t, fake, 1)
	if got := fake.results[0]; got.ExitCode != -1 || !strings.Contains(got.Error, "panic") {
		t.Fatalf("ergebnis = %+v", got)
	}
}

// ---- Client: Zustellung, Wiederaufnahme --------------------------------------

// failingToken ist ein Token, das mit einem Fehler endet.
type failingToken struct{ mqtt.DummyToken }

func (failingToken) Error() error { return errNotConnected }

// recordingMQTT hält alles fest, was der Agent sendet, und kann Zustellungen
// scheitern lassen.
type recordingMQTT struct {
	mqtt.Client
	mu       sync.Mutex
	failing  bool
	results  []wire.Result
	progress []wire.Result
}

func (f *recordingMQTT) Publish(_ string, _ byte, _ bool, payload any) mqtt.Token {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return &failingToken{}
	}
	var res wire.Result
	if b, ok := payload.([]byte); ok && json.Unmarshal(b, &res) == nil {
		if res.Progress {
			f.progress = append(f.progress, res)
		} else {
			f.results = append(f.results, res)
		}
	}
	return &mqtt.DummyToken{}
}

func (f *recordingMQTT) setFailing(on bool) {
	f.mu.Lock()
	f.failing = on
	f.mu.Unlock()
}

func (f *recordingMQTT) resultList() []wire.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wire.Result(nil), f.results...)
}

func (f *recordingMQTT) progressList() []wire.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wire.Result(nil), f.progress...)
}

// TestErgebnisWirdNachDerWiederverbindungZugestellt: Scheitert die Zustellung,
// bleibt der Job in der Ablage (C-6). Nach der nächsten Verbindung geht das
// Ergebnis genau einmal hinaus - ohne Wiederholungsschleife -, wird bestätigt
// und verschwindet aus der Ablage.
func TestErgebnisWirdNachDerWiederverbindungZugestellt(t *testing.T) {
	c, _ := testClient()
	c.runner.systemdRun = ""
	conn := &recordingMQTT{failing: true}
	c.mqtt = conn

	c.handleCommand(commandPayload(t, wire.Command{ID: "zustellung", Cmd: "echo hallo"}))
	waitFor(t, 5*time.Second, "ergebnis in der ablage", func() bool { return len(c.runner.Pending()) == 1 })
	time.Sleep(100 * time.Millisecond)
	if len(conn.resultList()) != 0 {
		t.Fatal("ergebnis trotz scheiternder leitung zugestellt")
	}

	conn.setFailing(false)
	c.resendResults()
	got := conn.resultList()
	if len(got) != 1 || got[0].Output != "hallo\n" || !got[0].Recovered {
		t.Fatalf("nachgereichtes ergebnis = %+v", got)
	}
	if len(c.runner.Pending()) != 0 {
		t.Fatal("bestätigtes ergebnis liegt noch in der ablage")
	}
	c.resendResults()
	if len(conn.resultList()) != 1 {
		t.Fatal("ein bestätigtes ergebnis wurde noch einmal gesendet")
	}
}

// TestErgebnisUeberlebtAgentNeustartInDerAblage: Der Agent stirbt mit einem
// ungesendeten Ergebnis; der nächste stellt es nach seiner Verbindung zu.
func TestErgebnisUeberlebtAgentNeustartInDerAblage(t *testing.T) {
	dir := t.TempDir()
	first := NewClient(&Config{AgentID: "agent-1"}, nil, "test").WithJobsDir(dir)
	first.runner.systemdRun = ""
	first.mqtt = &recordingMQTT{failing: true}
	first.handleCommand(commandPayload(t, wire.Command{ID: "ueberlebt", Cmd: "echo gesichert"}))
	waitFor(t, 5*time.Second, "ergebnis in der ablage", func() bool { return len(first.runner.Pending()) == 1 })
	first.Stop()

	second := NewClient(&Config{AgentID: "agent-1"}, nil, "test").WithJobsDir(dir)
	conn := &recordingMQTT{}
	second.mqtt = conn
	second.adoptJobs()
	second.resendResults()
	got := conn.resultList()
	if len(got) != 1 || got[0].ID != "ueberlebt" || got[0].Output != "gesichert\n" {
		t.Fatalf("zugestelltes ergebnis = %+v", got)
	}
}

// TestJobLaeuftNachClientNeustartWeiterUndLiefert: Wie die Probe im Plan: Ein
// Client geht (Stop), während das Kommando läuft; ein neuer auf derselben
// Ablage nimmt es auf, meldet Lebenszeichen mit Vitals und stellt das Ergebnis zu.
func TestJobLaeuftNachClientNeustartWeiterUndLiefert(t *testing.T) {
	dir := t.TempDir()
	first := NewClient(&Config{AgentID: "agent-1"}, nil, "test").WithJobsDir(dir)
	first.runner.systemdRun = ""
	first.mqtt = &recordingMQTT{}
	first.handleCommand(commandPayload(t, wire.Command{ID: "weiter", Cmd: "sleep 1; echo fertig"}))
	waitRunning(t, first.runner, "weiter")
	first.Stop()

	second := NewClient(&Config{AgentID: "agent-1"}, nil, "test").WithJobsDir(dir)
	second.runner.systemdRun = ""
	second.runner.progressEvery = 100 * time.Millisecond
	second.runner.pollEvery = 20 * time.Millisecond
	conn := &recordingMQTT{}
	second.mqtt = conn
	second.adoptJobs()
	waitFor(t, 10*time.Second, "ergebnis", func() bool { return len(conn.resultList()) == 1 })

	res := conn.resultList()[0]
	if res.ID != "weiter" || res.Output != "fertig\n" || res.ExitCode != 0 || !res.Recovered {
		t.Fatalf("ergebnis = %+v", res)
	}
	progress := conn.progressList()
	if len(progress) == 0 || progress[0].Vitals == nil {
		t.Fatalf("keine lebenszeichen mit vitals nach der wiederaufnahme: %+v", progress)
	}
	// Die Ablage räumt deliver erst nach dem Senden (Ack nach dem PUBACK), und
	// Ack schreibt danach noch die Liste der bestätigten IDs.
	waitFor(t, 5*time.Second, "zugestelltes ergebnis aus der ablage", func() bool {
		return len(second.runner.Pending()) == 0 && slices.Contains(second.runner.store.ackedIDs(), "weiter")
	})
}

// TestSelbstwechselWartetNichtAufDieZustellung: Ein Ergebnis, das nicht zugestellt
// werden kann, hält den Wechsel aufs neue Binary nicht mehr auf (C-6): Es liegt
// in der Ablage, und das neue Binary stellt es zu.
func TestSelbstwechselWartetNichtAufDieZustellung(t *testing.T) {
	c, _ := testClient()
	c.runner.systemdRun = ""
	c.mqtt = &recordingMQTT{failing: true}
	c.handleCommand(commandPayload(t, wire.Command{ID: "stau", Cmd: "true"}))
	waitFor(t, 5*time.Second, "ergebnis in der ablage", func() bool { return len(c.runner.Pending()) == 1 })
	waitFor(t, 5*time.Second, "wechsel möglich", func() bool { return c.work.claimIdle(0) })
}
