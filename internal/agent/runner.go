package agent

import (
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"LCM/internal/remote/wire"
)

// commandIDWindow ist die Zahl der Befehls-IDs, die sich der Runner merkt -
// für erkannte Doppel und für Abbrüche, die vor ihrem Kommando eintreffen.
// LCM schickt einem Agenten ein Kommando nach dem anderen; 1024 reichen
// weit über jede erneute Zustellung hinaus und kosten unter 100 KB.
const commandIDWindow = 1024

const (
	// abortGrace ist die Frist zwischen SIGTERM und SIGKILL bei einem
	// ausdrücklichen Abbruch.
	abortGrace = 30 * time.Second
	// jobPollEvery ist der Takt, in dem der Runner einen Job prüft: ob er noch
	// da ist (wieder aufgenommene Jobs) und ob seine Ausgabedatei Platz braucht.
	jobPollEvery = 500 * time.Millisecond
)

// errCancelledBeforeStart meldet ein Kommando, dessen Abbruch schon da war,
// als es an die Reihe kam.
var errCancelledBeforeStart = errors.New("kommando abgebrochen, bevor es startete")

// errDetached: Der Agent wird beendet. Das Kommando läuft ohne ihn weiter, sein
// Ergebnis stellt der nächste Agent zu.
var errDetached = errors.New("agent wird beendet - das kommando läuft weiter")

// Runner führt Kommandos des LCM-Servers aus - als Jobs, die den Agent
// überleben (jobrun.go) und in der Job-Ablage liegen (jobs.go): /bin/sh in
// einer eigenen Sitzung, Ausgabe in einer Datei, Abbruch per SIGTERM an die
// ganze Gruppe und nach einer Frist SIGKILL. Es läuft höchstens ein Kommando
// gleichzeitig - dieselbe Serialisierung, die serverseitig der ConnLimiter
// erzwingt. Von sich aus hält der Runner nie ein Kommando an: weder bei Stille
// noch nach Zeit (Entscheidung 1 vom 23.11.2026).
type Runner struct {
	log   *slog.Logger
	store *jobStore
	// slot serialisiert die Ausführung: Wer ihn hält, führt gerade ein Kommando
	// aus.
	slot chan struct{}

	progressEvery time.Duration // Takt der Lebenszeichen
	pollEvery     time.Duration
	abortGrace    time.Duration

	// systemdRun ist der Pfad von systemd-run, "" für den Lauf in der eigenen
	// Sitzung, scopeAuto für die Erkennung (probeScope, jobrun.go). systemctl
	// schickt Signale an einen Scope.
	systemdRun string
	systemctl  string
	scope      scopeState

	// quit schließt mit Detach: Der Agent wird beendet.
	quit     chan struct{}
	quitOnce sync.Once

	mu      sync.Mutex
	running map[string]*job // Request-ID → laufender Job
	// claimed sind die angenommenen Kommandos, die noch nicht zu Ende gelaufen
	// sind - auch solche, die hinter einem anderen warten und noch keinen Job haben.
	claimed   map[string]struct{}
	seen      *idWindow // angenommene Befehls-IDs
	cancelled *idWindow // abgebrochene Befehls-IDs
	vitals    map[string]wire.Vitals
}

// NewRunner baut den Runner mit der Job-Ablage JobsDir.
func NewRunner() *Runner {
	return &Runner{
		log:           slog.Default(),
		store:         &jobStore{dir: JobsDir},
		slot:          make(chan struct{}, 1),
		progressEvery: wire.ProgressInterval,
		pollEvery:     jobPollEvery,
		abortGrace:    abortGrace,
		systemdRun:    scopeAuto,
		systemctl:     "systemctl",
		scope:         scopeState{retryAfter: scopeRetryAfter},
		quit:          make(chan struct{}),
		running:       map[string]*job{},
		claimed:       map[string]struct{}{},
		seen:          newIDWindow(commandIDWindow),
		cancelled:     newIDWindow(commandIDWindow),
		vitals:        map[string]wire.Vitals{},
	}
}

// WithJobsDir legt die Job-Ablage fest (Tests).
func (r *Runner) WithJobsDir(dir string) *Runner {
	r.store = &jobStore{dir: dir}
	return r
}

// Claim nimmt ein Kommando an und meldet false, wenn seine ID schon einmal
// da war. MQTT mit QoS 1 stellt mindestens einmal zu - eine zweite Zustellung
// darf das Kommando nicht ein zweites Mal ausführen.
func (r *Runner) Claim(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.seen.add(id) {
		return false
	}
	r.claimed[id] = struct{}{}
	return true
}

// Unclaim nimmt ein angenommenes Kommando zurück, das nie bis Run kam (der Agent
// wechselt gerade aufs neue Binary).
func (r *Runner) Unclaim(id string) {
	r.mu.Lock()
	delete(r.claimed, id)
	r.mu.Unlock()
}

// Known liefert die IDs aller Kommandos, die der Agent kennt: angenommene,
// laufende und fertige, deren Ergebnis noch nicht bestätigt ist. LCM schließt
// daraus, dass ein Kommando, das hier fehlt, den Agent nie erreicht hat.
func (r *Runner) Known() []string {
	ids := []string{}
	for _, j := range r.store.all() {
		ids = append(ids, j.id)
	}
	r.mu.Lock()
	for id := range r.claimed {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	slices.Sort(ids)
	return slices.Compact(ids)
}

// Jobs liefert die Jobs der Ablage für die Zustandsdatei: laufende und fertige, deren
// Ergebnis LCM noch nicht bestätigt hat, die ältesten zuerst.
func (r *Runner) Jobs() []StatusJob {
	jobs := []StatusJob{}
	for _, j := range r.store.all() {
		jobs = append(jobs, StatusJob{ID: j.id, State: j.meta.State, Started: j.meta.Started})
	}
	return jobs
}

// Run führt das Kommando aus und liefert das Ergebnis (blockiert). progress
// wird - sofern gesetzt - im Takt von wire.ProgressInterval mit dem bisher
// gesammelten Ausgabe-Umfang aufgerufen, solange das Kommando wartet oder läuft;
// darüber erfährt der Server, dass es noch lebt. Die Beobachtungen dazu liefert
// Vitals.
func (r *Runner) Run(cmd wire.Command, progress func(outputBytes int)) wire.Result {
	defer r.Unclaim(cmd.ID)
	if !r.acquire(cmd.ID, progress) {
		return wire.Result{ID: cmd.ID, ExitCode: -1, Error: errDetached.Error()}
	}
	defer r.release()
	// Die Probe von systemd-run darf Sekunden dauern: nicht unter der Sperre, die
	// Cancel und Vitals brauchen (launch).
	r.useScope()
	j, err := r.launch(cmd)
	if err != nil {
		return wire.Result{ID: cmd.ID, ExitCode: -1, Error: err.Error()}
	}
	return r.follow(j, progress)
}

// acquire wartet, bis kein anderes Kommando mehr läuft, und meldet sich dabei
// im Takt der Lebenszeichen (C-5): Ein wartendes Kommando ist nicht stumm. false,
// wenn der Agent beendet wird.
func (r *Runner) acquire(id string, progress func(int)) bool {
	select {
	case r.slot <- struct{}{}:
		return true
	default:
	}
	r.setVitals(id, wire.Vitals{Alive: true, Waiting: wire.WaitingPrevious})
	defer r.dropVitals(id)
	beat := time.NewTicker(r.progressEvery)
	defer beat.Stop()
	if progress != nil {
		progress(0)
	}
	for {
		select {
		case r.slot <- struct{}{}:
			return true
		case <-beat.C:
			if progress != nil {
				progress(0)
			}
		case <-r.quit:
			return false
		}
	}
}

func (r *Runner) release() { <-r.slot }

// launch legt den Job an und startet ihn - unter derselben Sperre, die Cancel
// nimmt. So geht kein Abbruch verloren: Er kam entweder vorher (dann startet das
// Kommando gar nicht, auch wenn es hinter einem anderen wartete oder der
// Abbruch es auf dem Weg überholt hat) oder er trifft den Job.
func (r *Runner) launch(cmd wire.Command) (*job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelled.has(cmd.ID) {
		return nil, errCancelledBeforeStart
	}
	j, err := r.store.create(cmd.ID, cmd.Cmd)
	if err != nil {
		return nil, errors.New("job anlegen: " + err.Error())
	}
	if err := r.spawn(j, cmd.Stdin); err != nil {
		r.store.ack(cmd.ID)
		return nil, err
	}
	r.running[cmd.ID] = j
	return j, nil
}

// follow begleitet den Job bis zu seinem Ende: Es hält seine Ausgabedatei klein,
// meldet im Takt der Lebenszeichen, was es am Job sieht, und liefert danach das
// Ergebnis.
func (r *Runner) follow(j *job, progress func(int)) wire.Result {
	defer r.forget(j.id)
	exited := r.exited(j)
	poll := time.NewTicker(r.pollEvery)
	defer poll.Stop()
	var m meter
	next := time.Now().Add(r.progressEvery)
	for {
		select {
		case <-exited:
			if r.detached() {
				return wire.Result{ID: j.id, ExitCode: -1, Error: errDetached.Error()}
			}
			return r.finish(j)
		case <-r.quit:
			return wire.Result{ID: j.id, ExitCode: -1, Error: errDetached.Error()}
		case <-poll.C:
			j.trim()
			if now := time.Now(); !now.Before(next) {
				next = now.Add(r.progressEvery)
				r.setVitals(j.id, m.vitals(j, now))
				if progress != nil {
					progress(clampInt(j.outputSize()))
				}
			}
		}
	}
}

func (r *Runner) detached() bool {
	select {
	case <-r.quit:
		return true
	default:
		return false
	}
}

// finish liest Exit-Code und Ausgabe des beendeten Jobs und hält das Ergebnis in
// der Ablage fest, bis LCM es bestätigt.
func (r *Runner) finish(j *job) wire.Result {
	res := wire.Result{ID: j.id, ExitCode: -1}
	if code, ok := j.readExit(); ok {
		res.ExitCode = code
	} else if j.aborted() {
		res.Error = "kommando abgebrochen"
	} else if j.rebooted() {
		res.Error = "kommando durch einen Neustart des Servers beendet"
	} else {
		res.Error = "kommando ohne exit-code beendet (Prozess von außen getötet?)"
	}
	head, tail, omitted, err := readOutput(j.path(outFile), wire.MaxOutputBytes)
	if err != nil {
		r.log.Warn("job output could not be read", "id", j.id, "error", err)
	}
	// Zuletzt, wenn alle übrigen Felder stehen: Gemessen wird das ganze
	// serialisierte Ergebnis.
	fitOutput(&res, head, tail, omitted, wire.MaxResultBytes)
	if err := j.finish(res); err != nil {
		r.log.Warn("job result could not be stored", "id", j.id, "error", err)
	}
	r.store.prune(time.Now())
	return res
}

// forget trägt ein beendetes Kommando aus.
func (r *Runner) forget(id string) {
	r.mu.Lock()
	delete(r.running, id)
	delete(r.vitals, id)
	r.mu.Unlock()
}

// Adopt nimmt die Jobs der Ablage wieder auf, die der vorige Agent hinterlassen
// hat (Start des Agents). Für jeden laufenden ruft es spawn mit einer
// Beobachtungsfunktion auf, die der Aufrufer in einer eigenen Goroutine
// ausführt: Sie liefert das Ergebnis, sobald der Job zu Ende ist. Solange ein
// solcher Job läuft, hält der Runner den Platz für Kommandos besetzt. Außerdem
// räumt es die Ablage auf.
func (r *Runner) Adopt(spawn func(id string, follow func(progress func(int)) wire.Result)) {
	r.restoreSeen()
	holding := false
	for _, j := range r.store.withState(jobRunning) {
		j.pinProcess()
		r.mu.Lock()
		r.running[j.id] = j
		r.mu.Unlock()
		own := false
		select {
		case r.slot <- struct{}{}:
			own, holding = !holding, true
		default:
		}
		r.log.Info("job taken over from the job store", "id", j.id, "started", j.meta.Started)
		spawn(j.id, func(progress func(int)) wire.Result {
			if own {
				defer r.release()
			}
			return r.follow(j, progress)
		})
	}
	r.store.prune(time.Now())
}

// restoreSeen trägt alle Befehls-IDs ein, die der vorige Agent kannte: die
// bestätigten (Liste der Ablage) und jeden Job der Ablage, auch den fertigen mit
// unbestätigtem Ergebnis. Eine erneut zugestellte ID läuft so nicht noch einmal
// und überholt mit einem Fehler auch nicht das echte Ergebnis.
func (r *Runner) restoreSeen() {
	ids := r.store.ackedIDs()
	for _, j := range r.store.all() {
		ids = append(ids, j.id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range ids {
		r.seen.add(id)
	}
}

// Pending liefert die IDs der fertigen Jobs, deren Ergebnis LCM noch nicht
// bestätigt hat, die ältesten zuerst.
func (r *Runner) Pending() []string {
	var ids []string
	for _, j := range r.store.withState(jobFinished) {
		ids = append(ids, j.id)
	}
	return ids
}

// Result liefert das gespeicherte Ergebnis eines fertigen Jobs.
func (r *Runner) Result(id string) (wire.Result, bool) {
	j, err := r.store.load(safeID(id))
	if err != nil {
		return wire.Result{}, false
	}
	res, err := j.result()
	return res, err == nil
}

// Ack nimmt den Job aus der Ablage: LCM hat sein Ergebnis bestätigt.
func (r *Runner) Ack(id string) { r.store.ack(id) }

// Vitals liefert, was der Runner zuletzt am Kommando gesehen hat (ok=false,
// wenn keins läuft oder wartet).
func (r *Runner) Vitals(id string) (wire.Vitals, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.vitals[id]
	return v, ok
}

func (r *Runner) setVitals(id string, v wire.Vitals) {
	r.mu.Lock()
	r.vitals[id] = v
	r.mu.Unlock()
}

func (r *Runner) dropVitals(id string) {
	r.mu.Lock()
	delete(r.vitals, id)
	r.mu.Unlock()
}

// Detach löst den Runner von seinen Jobs: Der Agent wird beendet, und die Jobs
// laufen ohne ihn weiter. Wartende Kommandos kehren mit einem Fehler zurück.
func (r *Runner) Detach() { r.quitOnce.Do(func() { close(r.quit) }) }

// Cancel bricht ein Kommando ab (SIGTERM an die Prozessgruppe, nach der Frist
// SIGKILL). Läuft es noch nicht, merkt sich der Runner den Abbruch, und es
// startet gar nicht erst.
func (r *Runner) Cancel(reqID string) {
	r.mu.Lock()
	r.cancelled.add(reqID)
	j := r.running[reqID]
	r.mu.Unlock()
	if j != nil {
		r.abort(j)
	}
}

// idWindow merkt sich die zuletzt eingetragenen IDs, höchstens max Stück;
// die älteste fällt zuerst heraus. Nicht threadsicher - der Runner hält mu.
type idWindow struct {
	max   int
	order []string
	set   map[string]struct{}
}

func newIDWindow(max int) *idWindow {
	return &idWindow{max: max, set: map[string]struct{}{}}
}

// add trägt id ein und meldet false, wenn sie schon drin war.
func (w *idWindow) add(id string) bool {
	if w.has(id) {
		return false
	}
	w.set[id] = struct{}{}
	w.order = append(w.order, id)
	if len(w.order) > w.max {
		delete(w.set, w.order[0])
		w.order = w.order[1:]
	}
	return true
}

func (w *idWindow) has(id string) bool {
	_, ok := w.set[id]
	return ok
}
