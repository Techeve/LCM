package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"LCM/internal/remote/wire"
)

// Tests für die Korrekturen aus der Durchsicht von Paket B/C (23.11.).

// adoptNothing nimmt die Jobs der Ablage auf, ohne sie weiter zu begleiten.
func adoptNothing(r *Runner) {
	r.Adopt(func(string, func(func(int)) wire.Result) {})
}

// TestNeuerRunnerKenntFertigeJobsUndBestaetigteIDs: Nach einem Neustart gelten
// auch fertige Jobs mit unbestätigtem Ergebnis und bestätigte als bekannt - eine
// erneut zugestellte ID läuft nicht noch einmal.
func TestNeuerRunnerKenntFertigeJobsUndBestaetigteIDs(t *testing.T) {
	dir := t.TempDir()
	old := newTestRunner(t).WithJobsDir(dir)
	old.Run(wire.Command{ID: "fertig", Cmd: "true"}, nil)
	old.Run(wire.Command{ID: "bestaetigt", Cmd: "true"}, nil)
	old.Ack("bestaetigt")

	next := newTestRunner(t).WithJobsDir(dir)
	adoptNothing(next)
	for _, id := range []string{"fertig", "bestaetigt"} {
		if next.Claim(id) {
			t.Errorf("die ID %q gilt nach dem Neustart als neu - ein Doppel liefe noch einmal", id)
		}
	}
	if !next.Claim("neu") {
		t.Error("eine unbekannte ID wurde abgelehnt")
	}
}

// TestAckListeBleibtBegrenztUndUeberlebtPrune: Die Liste der bestätigten IDs hält
// nur die letzten keepAcked, und die Aufräumrunde der Ablage lässt sie stehen.
func TestAckListeBleibtBegrenztUndUeberlebtPrune(t *testing.T) {
	s := &jobStore{dir: t.TempDir()}
	for i := range keepAcked + 50 {
		s.ack("id-" + strconv.Itoa(i))
	}
	ids := s.ackedIDs()
	if len(ids) != keepAcked || ids[0] != "id-50" || ids[len(ids)-1] != "id-"+strconv.Itoa(keepAcked+49) {
		t.Fatalf("%d IDs, erste %q, letzte %q", len(ids), ids[0], ids[len(ids)-1])
	}
	old := time.Now().Add(-3 * orphanAfter)
	if err := os.Chtimes(filepath.Join(s.dir, ackedFile), old, old); err != nil {
		t.Fatal(err)
	}
	s.prune(time.Now())
	if len(s.ackedIDs()) != keepAcked {
		t.Fatal("prune hat die Liste der bestätigten IDs gelöscht")
	}
}

// TestNebenlaeufigeBestaetigungenGehenNichtVerloren: deliver und resendResults
// bestätigen gleichzeitig; keine ID darf beim Lesen-Ändern-Schreiben der Liste
// verloren gehen.
func TestNebenlaeufigeBestaetigungenGehenNichtVerloren(t *testing.T) {
	s := &jobStore{dir: t.TempDir()}
	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.ack("id-" + strconv.Itoa(i))
		}()
	}
	wg.Wait()

	if got := len(s.ackedIDs()); got != n {
		t.Fatalf("%d IDs in der Liste, want %d", got, n)
	}
	for i := range n {
		if !slices.Contains(s.ackedIDs(), "id-"+strconv.Itoa(i)) {
			t.Errorf("id-%d fehlt", i)
		}
	}
}

// TestErneutZugestelltesKommandoLiefertDasEchteErgebnis: Das Ergebnis eines
// Kommandos liegt unbestätigt in der Ablage, der Agent startet neu, und LCM stellt
// dasselbe Kommando noch einmal zu. Es läuft nicht noch einmal, und es entsteht
// kein Fehlerergebnis, das das echte überholt - das echte wird erneut gesendet.
func TestErneutZugestelltesKommandoLiefertDasEchteErgebnis(t *testing.T) {
	dir := t.TempDir()
	first := NewClient(&Config{AgentID: "agent-1"}, nil, "test").WithJobsDir(dir)
	first.runner.systemdRun = ""
	first.mqtt = &recordingMQTT{failing: true}
	marker := filepath.Join(t.TempDir(), "laeufe")
	cmd := wire.Command{ID: "doppelt", Cmd: "echo lauf >>" + marker + "; echo echt"}
	first.handleCommand(commandPayload(t, cmd))
	waitFor(t, 5*time.Second, "ergebnis in der ablage", func() bool { return len(first.runner.Pending()) == 1 })
	first.Stop()

	second := NewClient(&Config{AgentID: "agent-1"}, nil, "test").WithJobsDir(dir)
	second.runner.systemdRun = ""
	conn := &recordingMQTT{}
	second.mqtt = conn
	second.adoptJobs()
	second.handleCommand(commandPayload(t, cmd)) // erneut zugestellt
	waitFor(t, 5*time.Second, "echtes ergebnis", func() bool { return len(conn.resultList()) >= 1 })
	time.Sleep(200 * time.Millisecond)

	got := conn.resultList()
	if len(got) != 1 || got[0].Error != "" || got[0].Output != "echt\n" {
		t.Fatalf("gesendet wurde %+v - erwartet genau das echte Ergebnis", got)
	}
	if data, _ := os.ReadFile(marker); strings.Count(string(data), "lauf") != 1 {
		t.Fatalf("das kommando lief mehrfach: %q", data)
	}
}

// TestRunnerProbeLaeuftNichtUnterDerSperre: Die Probe von systemd-run darf
// Sekunden dauern; solange sie läuft, bleiben Claim, Cancel und Vitals ansprechbar
// (und mit ihnen der Nachrichtenweg des Agents).
func TestRunnerProbeLaeuftNichtUnterDerSperre(t *testing.T) {
	r := newTestRunner(t)
	r.systemdRun = scopeAuto
	entered, release := make(chan struct{}), make(chan struct{})
	r.scope.probe = func() (string, error) {
		close(entered)
		<-release
		return "", errNoSystemd
	}
	done := make(chan wire.Result, 1)
	go func() { done <- r.Run(wire.Command{ID: "probe", Cmd: "true"}, nil) }()
	<-entered

	free := make(chan struct{})
	go func() {
		r.Claim("anderes")
		r.Cancel("nochmal-anderes")
		r.Vitals("probe")
		close(free)
	}()
	select {
	case <-free:
	case <-time.After(2 * time.Second):
		t.Fatal("Claim, Cancel und Vitals warten auf die Probe von systemd-run")
	}
	close(release)
	if res := <-done; res.ExitCode != 0 {
		t.Fatalf("ergebnis = %+v", res)
	}
}

// TestGescheiterteProbeWirdNachDerWartezeitWiederholt: Ein vorübergehender Fehler
// von systemd-run schaltet die Scopes nicht bis zum nächsten Neustart ab - nach
// der Wartezeit probiert der Runner erneut, vorher nicht.
func TestGescheiterteProbeWirdNachDerWartezeitWiederholt(t *testing.T) {
	r := newTestRunner(t)
	r.systemdRun = scopeAuto
	r.scope.retryAfter = 150 * time.Millisecond
	calls, failing := 0, true
	r.scope.probe = func() (string, error) {
		calls++
		if failing {
			return "", os.ErrPermission
		}
		return "/usr/bin/systemd-run", nil
	}
	if r.useScope() {
		t.Fatal("scope trotz gescheiterter probe")
	}
	if _, degraded := r.scopeCached(); !degraded {
		t.Error("ein Lauf ohne Scope trotz systemd gilt nicht als eingeschränkt")
	}
	r.useScope()
	if calls != 1 {
		t.Fatalf("probe %d-mal in der Wartezeit", calls)
	}
	failing = false
	time.Sleep(200 * time.Millisecond)
	if !r.useScope() || calls != 2 {
		t.Fatalf("keine neue probe nach der wartezeit (scope=%v, aufrufe=%d)", r.useScope(), calls)
	}
	if _, degraded := r.scopeCached(); degraded {
		t.Error("nach gelungener Probe noch eingeschränkt")
	}
	r.useScope()
	if calls != 2 {
		t.Error("nach gelungener Probe wird weiter geprüft")
	}
}

// setBootID lässt den Runner glauben, der Server laufe seit der Kennung id.
func setBootID(t *testing.T, id string) {
	t.Helper()
	old := bootID
	bootID = func() string { return id }
	t.Cleanup(func() { bootID = old })
}

// TestJobAusFruehererBootGiltAlsBeendet: Nach einem Neustart des Servers ist ein
// Job tot, auch wenn seine PID inzwischen einem anderen Prozess gehört. LCM
// erfährt es mit klarem Text, statt dass der Agent einem fremden Prozess
// hinterherwartet.
func TestJobAusFruehererBootGiltAlsBeendet(t *testing.T) {
	dir := t.TempDir()
	setBootID(t, "boot-alt")
	s := &jobStore{dir: dir}
	j, err := s.create("neustart", "sleep 100")
	if err != nil {
		t.Fatal(err)
	}
	// Die PID gehört jetzt einem lebenden Prozess: diesem Test.
	if err := os.WriteFile(j.path(pidFile), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	setBootID(t, "boot-neu")

	r := newTestRunner(t).WithJobsDir(dir)
	got := make(chan wire.Result, 1)
	r.Adopt(func(_ string, follow func(func(int)) wire.Result) { go func() { got <- follow(nil) }() })
	select {
	case res := <-got:
		if res.ExitCode != -1 || !strings.Contains(res.Error, "Neustart") {
			t.Fatalf("ergebnis = %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("der Job aus der früheren Boot-Sitzung wurde nie als beendet gemeldet")
	}
}

// TestJobOhnePIDHatLangeGnadenfrist: Ein Job ohne pid-Datei beginnt womöglich
// noch (überlasteter Server, langsames systemd-run) und gilt nicht schon nach
// Sekunden als beendet.
func TestJobOhnePIDHatLangeGnadenfrist(t *testing.T) {
	s := &jobStore{dir: t.TempDir()}
	j, err := s.create("langsam", "true")
	if err != nil {
		t.Fatal(err)
	}
	j.meta.Started = time.Now().Add(-30 * time.Second)
	if j.gone() {
		t.Fatal("ein Job ohne pid-Datei gilt nach 30 s als beendet")
	}
	j.meta.Started = time.Now().Add(-2 * noPIDGrace)
	if !j.gone() {
		t.Fatal("ein Job, der nie eine pid-Datei bekam, gilt nie als beendet")
	}
}

// TestBestehendeAblageWirdAuf0700Gesetzt: Das Verzeichnis kann von früher (oder
// von Hand) mit weiten Rechten dastehen - Kommandos und Ausgaben können Geheimnisse
// enthalten.
func TestBestehendeAblageWirdAuf0700Gesetzt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jobs")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (&jobStore{dir: dir}).create("recht", "true"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("rechte der ablage: %v (%v)", info.Mode().Perm(), err)
	}
}

// TestKommandoBeimWechselWirdAbgesagt: Der Agent wechselt gerade aufs neue Binary
// und lehnt ein Kommando ab - LCM bekommt eine Antwort mit dem Grund, statt auf
// ein Ergebnis zu warten, das nie kommt.
func TestKommandoBeimWechselWirdAbgesagt(t *testing.T) {
	c, fake := testClient()
	c.WithJobsDir(t.TempDir())
	marker := filepath.Join(t.TempDir(), "gelaufen")
	if !c.work.claimIdle(0) {
		t.Fatal("wechsel nicht möglich")
	}
	c.handleCommand(commandPayload(t, wire.Command{ID: "abgesagt", Cmd: "touch " + marker}))
	waitResults(t, fake, 1)
	if res := fake.results[0]; res.ID != "abgesagt" || res.ExitCode != -1 || res.Error != errSwitching {
		t.Fatalf("antwort = %+v", res)
	}
	if fileExists(marker) {
		t.Fatal("das kommando lief trotz Wechsel")
	}
	if ids := c.runner.Known(); len(ids) != 0 {
		t.Fatalf("das abgesagte Kommando gilt als bekannt: %v", ids)
	}
}

// TestInventarMeldetBekannteKommandos: Auch ein wartendes Kommando, das noch
// keinen Job hat, und ein fertiges mit unbestätigtem Ergebnis stehen im Inventar;
// ein Agent ohne Kommandos meldet eine leere Liste (nicht "nichts").
func TestInventarMeldetBekannteKommandos(t *testing.T) {
	c, _ := testClient()
	c.runner.systemdRun = ""
	c.WithJobsDir(t.TempDir())
	if ids := c.inventory().KnownJobs; ids == nil || len(ids) != 0 {
		t.Fatalf("KnownJobs ohne Kommandos = %#v, erwartet eine leere Liste", ids)
	}
	if !c.runner.Claim("wartet") {
		t.Fatal("claim")
	}
	c.runner.Run(wire.Command{ID: "fertig", Cmd: "true"}, nil)
	got := c.inventory().KnownJobs
	if len(got) != 2 || got[0] != "fertig" || got[1] != "wartet" {
		t.Fatalf("KnownJobs = %v", got)
	}
	c.runner.Ack("fertig")
	if got := c.inventory().KnownJobs; len(got) != 1 || got[0] != "wartet" {
		t.Fatalf("KnownJobs nach der Bestätigung = %v", got)
	}
}

// blockingMQTT trennt nie: Disconnect hängt, wie bei einer toten Leitung.
type blockingMQTT struct {
	mqtt.Client
	release chan struct{}
}

func (b *blockingMQTT) Disconnect(uint) { <-b.release }

// TestTrennenVorDemExecGibtNachDerFristAuf: Hängt das Trennen, hängt der Wechsel
// aufs neue Binary nicht mit.
func TestTrennenVorDemExecGibtNachDerFristAuf(t *testing.T) {
	cl := &blockingMQTT{release: make(chan struct{})}
	defer close(cl.release)
	start := time.Now()
	disconnectWithin(cl, 150*time.Millisecond)
	if d := time.Since(start); d < 100*time.Millisecond || d > 2*time.Second {
		t.Fatalf("disconnectWithin dauerte %s", d)
	}
	disconnectWithin(nil, time.Second) // kein Client: kein Fehler
}
