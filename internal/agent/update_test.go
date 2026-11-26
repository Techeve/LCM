package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"LCM/internal/remote/wire"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestUpdateStateZaehlerUndVermerke: Der Agent und das Startskript teilen sich
// einfache Dateien. Kaputte Inhalte gelten als „nichts".
func TestUpdateStateZaehlerUndVermerke(t *testing.T) {
	s := updateState{dir: filepath.Join(t.TempDir(), "update")}
	if s.starts() != 0 || s.mode() != "normal" || s.rolledBack() != nil {
		t.Fatal("leeres Verzeichnis muss neutral sein")
	}
	if err := s.failed(reasonNotReady); err != nil {
		t.Fatal(err)
	}
	if s.starts() != maxStarts {
		t.Errorf("starts nach failed = %d, erwartet %d (das Skript rollt beim nächsten Start zurück)", s.starts(), maxStarts)
	}
	if got := s.read("fail-reason"); got != "not-ready\n" {
		t.Errorf("fail-reason = %q", got)
	}
	if fi, err := os.Stat(s.dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("verzeichnis: %v %v, erwartet 0700", fi, err)
	}

	writeFile(t, s.path("rolled-back"), "from=2.4.0\nto=2.3.0\nreason=not-ready\nat=1700000000\n")
	writeFile(t, s.path("cycles"), "2\n")
	if err := s.healthy(false); err != nil {
		t.Fatal(err)
	}
	if s.starts() != 0 {
		t.Error("healthy setzt den Zähler nicht zurück")
	}
	for _, name := range []string{"fail-reason", "cycles", "rolled-back"} {
		if _, err := os.Stat(s.path(name)); err == nil {
			t.Errorf("healthy (normales Binary) lässt %s stehen", name)
		}
	}

	writeFile(t, s.path("rolled-back"), "from=2.4.0\nto=2.3.0\nreason=not-ready\nat=1700000000\n")
	if err := s.healthy(true); err != nil {
		t.Fatal(err)
	}
	rb := s.rolledBack()
	if rb == nil || *rb != (RolledBack{From: "2.4.0", To: "2.3.0", Reason: reasonNotReady, At: 1700000000}) {
		t.Errorf("Rückfall-Vermerk des vorigen Binarys bleibt stehen, ist aber %+v", rb)
	}

	writeFile(t, s.path("starts"), "kaputt")
	writeFile(t, s.path("mode"), "kaputt")
	writeFile(t, s.path("rolled-back"), "müll ohne gleichheitszeichen\n")
	if s.starts() != 0 || s.mode() != "normal" {
		t.Error("kaputter Zähler oder Modus muss als neutral gelten")
	}
	if rb := s.rolledBack(); rb == nil || rb.From != "unknown" || rb.Reason != reasonCrashLoop {
		t.Errorf("Vermerk ohne Felder: %+v", rb)
	}
}

// TestUpdateStateVoriges: Die Kopie entsteht nur, wenn dort nicht schon dasselbe
// liegt; „unbewährt" ist ein Binary nur, wenn es einen anderen Rückfall gibt.
func TestUpdateStateVoriges(t *testing.T) {
	dir := t.TempDir()
	s := updateState{dir: filepath.Join(dir, "update")}
	self := filepath.Join(dir, "lcm-agent")
	writeFile(t, self, "binary-B")

	if s.prevDiffers(self) {
		t.Fatal("ohne Rückfall ist das Binary nicht unbewährt")
	}
	copied, err := s.savePrev(self)
	if err != nil || !copied {
		t.Fatalf("erste Kopie: %v %v", copied, err)
	}
	if got, _ := os.ReadFile(s.prevPath()); string(got) != "binary-B" {
		t.Errorf("Kopie = %q", got)
	}
	if fi, _ := os.Stat(s.prevPath()); fi.Mode().Perm()&0o100 == 0 {
		t.Error("Kopie ist nicht ausführbar")
	}
	if copied, err := s.savePrev(self); err != nil || copied {
		t.Errorf("dieselbe Datei noch einmal kopiert: %v %v", copied, err)
	}
	if s.prevDiffers(self) {
		t.Error("gleiches Binary gilt als unbewährt")
	}
	writeFile(t, self, "binary-C")
	if !s.prevDiffers(self) {
		t.Error("anderes Binary gilt nicht als unbewährt")
	}
	if copied, err := s.savePrev(self); err != nil || !copied {
		t.Errorf("neues Binary nicht gesichert: %v %v", copied, err)
	}
	if _, err := os.Stat(s.prevPath() + ".tmp"); err == nil {
		t.Error("temporäre Datei liegen geblieben")
	}
}

// healthClient baut einen Agenten, der als Binary des Pakets läuft.
type healthRig struct {
	c     *Client
	self  string
	exits chan int
}

func newHealthRig(t *testing.T) *healthRig {
	t.Helper()
	c, _ := testClient()
	dir := t.TempDir()
	r := &healthRig{c: c, self: filepath.Join(dir, "lcm-agent"), exits: make(chan int, 4)}
	writeFile(t, r.self, "binary-B")
	c.update = updateState{dir: filepath.Join(dir, "update")}
	c.exePath, c.packagedPath, c.selfBinary = r.self, r.self, r.self
	c.timing = Timing{HealthWait: 60 * time.Millisecond, StableAfter: 20 * time.Millisecond}.withDefaults()
	c.exit = func(code int) { r.exits <- code }
	c.reachable = func() bool { return true }
	t.Cleanup(c.Stop)
	return r
}

func (r *healthRig) exited(d time.Duration) (int, bool) {
	select {
	case code := <-r.exits:
		return code, true
	case <-time.After(d):
		return 0, false
	}
}

// waitState wartet, bis cond wahr ist.
func waitState(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("zeitüberschreitung: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestGesundMeldetSichUndSichertDasBinary: Bereit und stabil heißt: Zähler 0,
// alter Rückfall-Vermerk weg, und dieses Binary ist der neue Rückfall.
func TestGesundMeldetSichUndSichertDasBinary(t *testing.T) {
	r := newHealthRig(t)
	writeFile(t, r.c.update.prevPath(), "binary-A")
	writeFile(t, r.c.update.path("starts"), "2\n")
	writeFile(t, r.c.update.path("rolled-back"), "from=2.9.0\n")

	go r.c.watchHealth()
	r.c.health.markReady()
	waitState(t, "starts = 0", func() bool { return r.c.update.starts() == 0 && r.c.update.read("starts") != "" })
	waitState(t, "Kopie", func() bool {
		b, _ := os.ReadFile(r.c.update.prevPath())
		return string(b) == "binary-B"
	})
	if _, err := os.Stat(r.c.update.path("rolled-back")); err == nil {
		t.Error("Rückfall-Vermerk eines früheren Updates bleibt stehen")
	}
	if _, ok := r.exited(100 * time.Millisecond); ok {
		t.Error("ein gesunder Agent beendet sich nicht")
	}
}

// TestGesundVorStabilitaetNichtEingetragen: Hält der Agent nach dem Bereitwerden
// nicht durch (Absturz), bleibt der Zähler stehen - das Startskript zählt weiter.
func TestGesundVorStabilitaetNichtEingetragen(t *testing.T) {
	r := newHealthRig(t)
	r.c.timing.StableAfter = time.Hour
	writeFile(t, r.c.update.path("starts"), "2\n")
	go r.c.watchHealth()
	r.c.health.markReady()
	time.Sleep(150 * time.Millisecond)
	if r.c.update.starts() != 2 {
		t.Errorf("starts = %d, bereit allein genügt nicht", r.c.update.starts())
	}
}

// TestNichtBereitGibtAufWennLCMErreichbar: Ein unbewährtes Binary, das trotz
// erreichbarem LCM nicht bereit wird, trägt „nicht bereit" ein und beendet sich.
func TestNichtBereitGibtAufWennLCMErreichbar(t *testing.T) {
	r := newHealthRig(t)
	writeFile(t, r.c.update.prevPath(), "binary-A")
	go r.c.watchHealth()
	if code, ok := r.exited(2 * time.Second); !ok || code != 1 {
		t.Fatalf("exit = %d %v, erwartet 1", code, ok)
	}
	if r.c.update.starts() != maxStarts || r.c.update.read("fail-reason") != "not-ready\n" {
		t.Errorf("verdikt: starts=%d reason=%q", r.c.update.starts(), r.c.update.read("fail-reason"))
	}
}

// TestNichtBereitOhneNetzGibtNichtAuf: Ohne Netz liegt es nicht am Binary - kein
// Rückfall, aber die Prüfung läuft weiter und greift, sobald LCM erreichbar ist.
func TestNichtBereitOhneNetzGibtNichtAuf(t *testing.T) {
	r := newHealthRig(t)
	writeFile(t, r.c.update.prevPath(), "binary-A")
	up := make(chan struct{})
	r.c.reachable = func() bool {
		select {
		case <-up:
			return true
		default:
			return false
		}
	}
	go r.c.watchHealth()
	if _, ok := r.exited(300 * time.Millisecond); ok {
		t.Fatal("Agent gibt ohne Netz auf")
	}
	if r.c.update.starts() != 0 {
		t.Error("Zähler ohne Netz verändert")
	}
	close(up)
	if code, ok := r.exited(2 * time.Second); !ok || code != 1 {
		t.Fatalf("nach der Rückkehr des Netzes: exit = %d %v", code, ok)
	}
}

// TestNichtBereitOhneRueckfallGibtNichtAuf: Ohne anderes vorheriges Binary ist
// dieses das bewährte - ein Ausfall von LCM darf nichts zurückrollen.
func TestNichtBereitOhneRueckfallGibtNichtAuf(t *testing.T) {
	for _, tc := range []struct{ name, prev string }{{"kein Rückfall", ""}, {"derselbe Rückfall", "binary-B"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newHealthRig(t)
			if tc.prev != "" {
				writeFile(t, r.c.update.prevPath(), tc.prev)
			}
			go r.c.watchHealth()
			if _, ok := r.exited(300 * time.Millisecond); ok {
				t.Fatal("Agent gibt auf, obwohl es nichts gibt, auf das er zurückfallen könnte")
			}
		})
	}
}

// TestRueckfallBinaerGibtNichtAufUndSichertSichNicht: Läuft der Agent als
// vorheriges Binary, bleibt der Vermerk stehen, und er überschreibt den Rückfall
// nicht mit sich selbst.
func TestRueckfallBinaerGibtNichtAufUndSichertSichNicht(t *testing.T) {
	r := newHealthRig(t)
	r.c.exePath = r.c.update.prevPath()
	r.c.asPrev = true
	writeFile(t, r.c.update.prevPath(), "binary-A")
	r.c.selfBinary = r.c.update.prevPath()
	writeFile(t, r.c.update.path("rolled-back"), "from=2.4.0\nto=2.3.0\nreason=crash-loop\nat=5\n")
	writeFile(t, r.c.update.path("starts"), "1\n")
	go r.c.watchHealth()
	if _, ok := r.exited(200 * time.Millisecond); ok {
		t.Fatal("der Rückfall beendet sich, wenn LCM nicht antwortet")
	}
	r.c.health.markReady()
	waitState(t, "starts = 0", func() bool { return r.c.update.starts() == 0 })
	if r.c.update.rolledBack() == nil {
		t.Error("Rückfall-Vermerk verschwunden, solange das vorige Binary läuft")
	}
	if got, _ := os.ReadFile(r.c.update.prevPath()); string(got) != "binary-A" {
		t.Errorf("der Rückfall wurde überschrieben: %q", got)
	}
}

// TestInventarMeldetRueckfall: LCM erfährt das Scheitern des Updates mit dem
// Inventar - nur vom vorigen Binary aus, und mit allen Einzelheiten.
func TestInventarMeldetRueckfall(t *testing.T) {
	r := newHealthRig(t)
	writeFile(t, r.c.update.path("rolled-back"), "from=2.4.0\nto=2.3.0\nreason=not-ready\nat=5\n")

	var got map[string]any
	decode := func() {
		got = nil
		b, err := json.Marshal(r.c.inventoryMessage())
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
	}
	decode()
	if _, ok := got["rolled_back"]; ok {
		t.Error("ein normales Binary meldet einen Rückfall")
	}
	r.c.asPrev = true
	decode()
	if got["rolled_back"] != "2.4.0" || got["rolled_back_to"] != "2.3.0" || got["rolled_back_reason"] != "not-ready" {
		t.Errorf("inventar = %v", got)
	}
	if got["agent_version"] != "test" || got["known_jobs"] == nil {
		t.Errorf("das übrige Inventar fehlt: %v", got)
	}
}

// TestFaehigkeitenSelfUpdate: Nur mit dem Startskript des Pakets kann LCM den
// Agenten gezielt aktualisieren.
func TestFaehigkeitenSelfUpdate(t *testing.T) {
	without := agentCapabilities([]string{wire.CapabilityTTY}, false)
	with := agentCapabilities([]string{wire.CapabilityTTY}, true)
	has := func(l []string, c string) bool {
		for _, x := range l {
			if x == c {
				return true
			}
		}
		return false
	}
	if has(without, capabilitySelfUpdate) || !has(with, capabilitySelfUpdate) {
		t.Errorf("without=%v with=%v", without, with)
	}
	for _, want := range []string{wire.CapabilityTTY, wire.CapabilityPing, wire.CapabilityJobs} {
		if !has(without, want) || !has(with, want) {
			t.Errorf("Fähigkeit %s fehlt", want)
		}
	}
}
