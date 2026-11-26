package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"LCM/internal/remote/wire"
)

// Die Job-Ablage (C-2 aus doc/plan-2026-11-23-agent-robustheit.md): Jedes
// Kommando bekommt ein Verzeichnis, in dem sein ganzer Lauf liegt - unabhängig
// vom Agent-Prozess. Stirbt oder wechselt der Agent, läuft das Kommando weiter
// (jobrun.go), und der nächste Agent findet hier alles wieder:
//
//	meta.json    ID, Zustand, Start, Scope
//	cmd.sh       das Kommando (nur, solange es läuft)
//	pid          PID des Wrappers, schreibt der Wrapper selbst als Erstes
//	out          Stdout und Stderr des Kommandos, angehängt
//	exit         Exit-Code, schreibt der Wrapper als Letztes
//	result.json  das Ergebnis, bis LCM den Empfang bestätigt hat
//
// Der Zustand ist "running" bis das Ergebnis steht, dann "finished". Ein
// bestätigtes Ergebnis (PUBACK des Brokers) löscht das Verzeichnis. Das
// Verzeichnis ist 0700 und gehört root: Ausgabe und Kommando können Geheimnisse
// enthalten (stdin dagegen läuft über eine Pipe und berührt nie die Platte).

// JobsDir ist die Ablage der Kommando-Läufe. Eine Variable, damit Tests sie
// umlenken können.
var JobsDir = "/var/lib/lcm-agent/jobs"

// Dateien eines Jobs.
const (
	metaFile   = "meta.json"
	cmdFile    = "cmd.sh"
	pidFile    = "pid"
	outFile    = "out"
	exitFile   = "exit"
	resultFile = "result.json"
)

// Zustände eines Jobs.
const (
	jobRunning  = "running"
	jobFinished = "finished"
)

const (
	// keepResults und keepResultsFor begrenzen die Ergebnisse, die LCM nicht
	// bestätigt hat (etwa weil es dauerhaft weg ist): höchstens so viele, höchstens
	// so lange. Ein Ergebnis ist höchstens wire.MaxResultBytes groß, die Ablage
	// bleibt also unter 32 · 8 MiB.
	keepResults    = 32
	keepResultsFor = 7 * 24 * time.Hour
	// orphanAfter: Ein Verzeichnis ohne lesbare meta.json ist nach so langer Zeit
	// Müll eines abgebrochenen Anlegens.
	orphanAfter = time.Hour
	// noPIDGrace: So lange darf ein Job ohne pid-Datei noch beim Starten sein.
	// Großzügig: Ein überlasteter Server oder ein langsames systemd-run brauchen
	// länger, und ein zu früh als beendet gemeldeter Job liefe doppelt.
	noPIDGrace = 60 * time.Second
	// ackedFile hält die zuletzt bestätigten Befehls-IDs, damit eine erneut
	// zugestellte nach einem Neustart nicht noch einmal läuft (keepAcked Stück).
	ackedFile = "acked"
	keepAcked = commandIDWindow
)

// jobMeta ist der Inhalt von meta.json.
type jobMeta struct {
	ID       string    `json:"id"`
	State    string    `json:"state"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	// Scope ist der systemd-Scope des Kommandos (leer ohne systemd).
	Scope string `json:"scope,omitempty"`
	// PIDStart ist die Startzeit des Wrappers (Linux, /proc/<pid>/stat): Sie
	// unterscheidet ihn von einem Prozess, der die PID später wiederverwendet.
	PIDStart uint64 `json:"pid_start,omitempty"`
	// BootID ist die Kennung des Systemstarts, in dem der Job begann. Nach einem
	// Neustart des Servers ist er beendet, auch wenn die PID wieder vergeben ist.
	BootID string `json:"boot_id,omitempty"`
	// Aborted: LCM hat das Kommando ausdrücklich abgebrochen.
	Aborted bool `json:"aborted,omitempty"`
}

// job ist ein Kommando-Lauf in der Ablage.
type job struct {
	id  string
	dir string

	mu   sync.Mutex
	meta jobMeta

	// proc ist der selbst gestartete Prozess, nil bei einem wieder
	// aufgenommenen Job. waited meldet, dass er beendet und eingesammelt ist.
	proc   *exec.Cmd
	waited atomic.Bool

	// punched ist der Teil der Ausgabedatei, den trim schon freigegeben hat.
	punched int64
	noPunch bool
}

// jobStore ist die Ablage auf der Platte. Immer als Zeiger benutzen (der Mutex).
type jobStore struct {
	dir string
	// ackMu schützt die Liste der bestätigten IDs: lesen, ergänzen und
	// schreiben ist ein Schritt, und deliver und resendResults bestätigen
	// nebenläufig.
	ackMu sync.Mutex
}

var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)

// safeID macht aus einer Befehls-ID einen Verzeichnis- und Unit-Namen. IDs
// kommen vom Server und sind UUIDs; alles andere wird zu einem Hash, damit
// keine ID aus der Ablage herausführt. Auch der Name der Datei mit den
// bestätigten IDs (ackedFile) ist vergeben: Eine ID "acked" machte aus ihr ein
// Verzeichnis, und ack löschte beim Bestätigen die Liste.
func safeID(id string) string {
	if safeIDPattern.MatchString(id) && id != ackedFile {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return "x" + hex.EncodeToString(sum[:16])
}

// bootID liefert die Kennung des Systemstarts (eine Variable für Tests).
var bootID = readBootID

// create legt den Job an. Existiert er schon, ist die ID doppelt vergeben.
func (s *jobStore) create(id, command string) (*job, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	// MkdirAll lässt die Rechte eines vorhandenen Verzeichnisses, wie es sie vorfand.
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return nil, err
	}
	j := &job{id: id, dir: filepath.Join(s.dir, safeID(id))}
	if err := os.Mkdir(j.dir, 0o700); err != nil {
		return nil, err
	}
	j.meta = jobMeta{ID: id, State: jobRunning, Started: time.Now(), BootID: bootID()}
	if err := os.WriteFile(j.path(cmdFile), []byte(command), 0o600); err != nil {
		_ = os.RemoveAll(j.dir)
		return nil, err
	}
	if err := j.save(); err != nil {
		_ = os.RemoveAll(j.dir)
		return nil, err
	}
	return j, nil
}

// load liest einen Job aus seinem Verzeichnis.
func (s *jobStore) load(name string) (*job, error) {
	dir := filepath.Join(s.dir, name)
	raw, err := os.ReadFile(filepath.Join(dir, metaFile))
	if err != nil {
		return nil, err
	}
	j := &job{dir: dir}
	if err := json.Unmarshal(raw, &j.meta); err != nil {
		return nil, err
	}
	j.id = j.meta.ID
	return j, nil
}

// all liefert alle lesbaren Jobs, die ältesten zuerst.
func (s *jobStore) all() []*job {
	entries, _ := os.ReadDir(s.dir)
	var jobs []*job
	for _, e := range entries {
		if j, err := s.load(e.Name()); err == nil && e.IsDir() {
			jobs = append(jobs, j)
		}
	}
	slices.SortFunc(jobs, func(a, b *job) int { return a.meta.Started.Compare(b.meta.Started) })
	return jobs
}

// withState liefert die Jobs in einem Zustand.
func (s *jobStore) withState(state string) []*job {
	return slices.DeleteFunc(s.all(), func(j *job) bool { return j.meta.State != state })
}

// ack löscht den Job: LCM hat sein Ergebnis bestätigt. Die ID bleibt in der
// Liste der bestätigten (ackedIDs).
func (s *jobStore) ack(id string) {
	_ = os.RemoveAll(filepath.Join(s.dir, safeID(id)))
	s.rememberAcked(id)
}

// ackedIDs liefert die zuletzt bestätigten Befehls-IDs, die älteste zuerst.
func (s *jobStore) ackedIDs() []string {
	raw, err := os.ReadFile(filepath.Join(s.dir, ackedFile))
	if err != nil {
		return nil
	}
	return strings.Fields(string(raw))
}

// rememberAcked hängt die ID an die Liste der bestätigten an und kürzt sie auf
// die letzten keepAcked. IDs mit Leerraum bleiben außen vor (UUIDs haben keinen).
func (s *jobStore) rememberAcked(id string) {
	if id == "" || strings.ContainsAny(id, " \t\r\n") {
		return
	}
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	ids := append(s.ackedIDs(), id)
	if len(ids) > keepAcked {
		ids = ids[len(ids)-keepAcked:]
	}
	if err := writeFileAtomic(filepath.Join(s.dir, ackedFile), []byte(strings.Join(ids, "\n")+"\n")); err != nil {
		slog.Warn("acknowledged command ids could not be stored", "error", err)
	}
}

// prune räumt auf: Verzeichnisse ohne lesbare meta.json nach orphanAfter und
// unbestätigte Ergebnisse über keepResults bzw. keepResultsFor hinaus. Laufende
// Jobs bleiben immer stehen.
func (s *jobStore) prune(now time.Time) {
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if !e.IsDir() { // die Liste der bestätigten IDs
			continue
		}
		if _, err := s.load(e.Name()); err == nil {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > orphanAfter {
			_ = os.RemoveAll(filepath.Join(s.dir, e.Name()))
		}
	}
	finished := s.withState(jobFinished)
	for i, j := range finished {
		newer := len(finished) - 1 - i // so viele Ergebnisse sind jünger
		if newer >= keepResults || now.Sub(j.meta.Finished) > keepResultsFor {
			_ = os.RemoveAll(j.dir)
		}
	}
}

func (j *job) path(name string) string { return filepath.Join(j.dir, name) }

// save schreibt meta.json, über eine Zwischendatei, damit nie eine halbe
// Datei liegt.
func (j *job) save() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.saveLocked()
}

func (j *job) saveLocked() error {
	raw, err := json.Marshal(j.meta)
	if err != nil {
		return err
	}
	return writeFileAtomic(j.path(metaFile), raw)
}

// writeFileAtomic schreibt über eine Zwischendatei und benennt sie um.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// update ändert die Metadaten und schreibt sie.
func (j *job) update(fn func(*jobMeta)) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.meta)
	return j.saveLocked()
}

func (j *job) aborted() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.meta.Aborted
}

// rebooted meldet, dass der Server seit dem Start des Jobs neu gestartet ist.
func (j *job) rebooted() bool {
	j.mu.Lock()
	started := j.meta.BootID
	j.mu.Unlock()
	now := bootID()
	return started != "" && now != "" && started != now
}

// pinProcess hält die Startzeit des Wrappers fest, falls noch keine vermerkt ist
// (der Agent konnte sie beim Start nicht lesen): Ohne sie ließe sich eine
// wiederverwendete PID nicht von ihm unterscheiden.
func (j *job) pinProcess() {
	pid := j.pid()
	if pid <= 0 || j.meta.PIDStart != 0 {
		return
	}
	if start := procStartTime(pid); start != 0 {
		_ = j.update(func(m *jobMeta) { m.PIDStart = start })
	}
}

// pid ist die PID des Wrappers, 0 solange sie nicht bekannt ist. Sie ist die
// der Sitzung und der Prozessgruppe des Kommandos.
func (j *job) pid() int {
	if j.proc != nil && j.proc.Process != nil {
		return j.proc.Process.Pid
	}
	raw, err := os.ReadFile(j.path(pidFile))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	return pid
}

// readExit liest den Exit-Code, den der Wrapper nach dem Kommando schreibt.
func (j *job) readExit() (int, bool) {
	raw, err := os.ReadFile(j.path(exitFile))
	if err != nil {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	return code, err == nil
}

// gone meldet, dass das Kommando zu Ende ist: Der Wrapper hat den Exit-Code
// geschrieben, oder sein Prozess ist weg (getötet, bevor er ihn schreiben
// konnte).
func (j *job) gone() bool {
	if _, ok := j.readExit(); ok {
		return true
	}
	if j.rebooted() {
		return true
	}
	if j.proc != nil {
		return j.waited.Load()
	}
	pid := j.pid()
	if pid == 0 {
		return time.Since(j.meta.Started) > noPIDGrace
	}
	return !processAlive(pid, j.meta.PIDStart)
}

// outputSize ist die Zahl aller Bytes, die das Kommando bisher geschrieben hat.
func (j *job) outputSize() int64 {
	info, err := os.Stat(j.path(outFile))
	if err != nil {
		return 0
	}
	return info.Size()
}

// lastOutput ist der Zeitpunkt der letzten Ausgabe, ohne Ausgabe der Start.
func (j *job) lastOutput() time.Time {
	if info, err := os.Stat(j.path(outFile)); err == nil && info.Size() > 0 {
		return info.ModTime()
	}
	return j.meta.Started
}

// finish hält das Ergebnis fest und gibt Platz frei: Aus dem laufenden Job wird
// ein fertiger, der nur noch sein Ergebnis trägt.
func (j *job) finish(res wire.Result) error {
	raw, err := json.Marshal(res)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(j.path(resultFile), raw); err != nil {
		return err
	}
	err = j.update(func(m *jobMeta) { m.State, m.Finished = jobFinished, time.Now() })
	for _, name := range []string{outFile, cmdFile, pidFile, exitFile} {
		_ = os.Remove(j.path(name))
	}
	return err
}

// result liest das Ergebnis eines fertigen Jobs.
func (j *job) result() (wire.Result, error) {
	var res wire.Result
	raw, err := os.ReadFile(j.path(resultFile))
	if err != nil {
		return res, err
	}
	return res, json.Unmarshal(raw, &res)
}

// readOutput liest Anfang und Ende der Ausgabedatei mit derselben Aufteilung wie
// ein Puffer mit Limit max: die erste Hälfte vom Anfang, die zweite vom Ende,
// dazwischen fehlt, was omitted zählt. Bei apt steht das Wichtige vorn (was
// ansteht) und hinten (Fehler, Zusammenfassung).
func readOutput(path string, max int) (head, tail []byte, omitted int, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, 0, nil // das Kommando hat nichts geschrieben
	}
	if err != nil {
		return nil, nil, 0, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, 0, err
	}
	// int64: Die Datei darf auch auf 32-Bit-Systemen über 2 GiB groß werden.
	size := info.Size()
	headN := min(size, int64(max/2))
	tailN := min(size-headN, int64(max-max/2))
	if head, err = readAt(f, 0, headN); err != nil {
		return nil, nil, 0, err
	}
	if tail, err = readAt(f, size-tailN, tailN); err != nil {
		return nil, nil, 0, err
	}
	return head, tail, clampInt(size - headN - tailN), nil
}

func readAt(f *os.File, off, n int64) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(io.NewSectionReader(f, off, n), buf)
	return buf, err
}

// clampInt wandelt in int, ohne auf 32-Bit-Systemen überzulaufen.
func clampInt(n int64) int { return int(min(n, math.MaxInt32)) }

// Platz der Ausgabedatei. Sie wächst mit dem Kommando (der Wrapper hängt an),
// und ein Upgrade kann Gigabytes ausgeben. Ausgewertet werden aber nur Anfang
// und Ende (readOutput). trim gibt deshalb die Mitte frei (unter Linux als
// Loch in der Datei, ohne ihre Größe oder Lage zu ändern), sobald die Datei
// outputSlack über dem hält, was gelesen wird. Das Ende bleibt dabei für
// ein späteres Lesen unangetastet: freigegeben wird nur bis zwei Ende-Längen
// vor dem aktuellen Dateiende.
const outputSlack = 3 * wire.MaxOutputBytes

// trim begrenzt den Plattenbedarf der Ausgabedatei (siehe oben). Ohne Unterstützung
// des Dateisystems bleibt es beim Wachsen.
func (j *job) trim() {
	if j.noPunch {
		return
	}
	size := j.outputSize()
	headN := int64(wire.MaxOutputBytes / 2)
	tailN := int64(wire.MaxOutputBytes - wire.MaxOutputBytes/2)
	if size < outputSlack {
		return
	}
	from, to := max(headN, j.punched), size-2*tailN
	if to <= from {
		return
	}
	if err := punchHole(j.path(outFile), from, to-from); err != nil {
		j.noPunch = true
		return
	}
	j.punched = to
}
