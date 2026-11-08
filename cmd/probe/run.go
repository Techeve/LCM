package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	"LCM/internal/probe/host"
	"LCM/internal/probe/journal"
	"LCM/internal/version"
)

// runConfig sind die Einstellungen des Messlaufs.
type runConfig struct {
	dir        string
	keepDays   int
	interval   time.Duration
	dbInterval time.Duration
	url        string
	keyFile    string
	lcmConfig  string
	lcmData    string
}

func runCommand(args []string) error {
	cfg := runConfig{}
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.StringVar(&cfg.dir, "dir", defaultDir, "Verzeichnis der Tagesdateien")
	fs.IntVar(&cfg.keepDays, "keep-days", 90, "Aufbewahrung in Tagen")
	fs.DurationVar(&cfg.interval, "interval", 2*time.Second, "Takt der Hostwerte")
	fs.DurationVar(&cfg.dbInterval, "db-interval", time.Minute, "Takt der Datenbank-Momentaufnahme")
	fs.StringVar(&cfg.url, "url", "", "Adresse von LCM (Vorgabe: Port aus der LCM-Konfiguration auf 127.0.0.1)")
	fs.StringVar(&cfg.keyFile, "key-file", "/etc/lcm-probe/key", "API-Schlüssel für GET /api/v1/system/db")
	fs.StringVar(&cfg.lcmConfig, "lcm-config", "/etc/lcm/config.json", "Konfiguration von LCM (für den Port)")
	fs.StringVar(&cfg.lcmData, "lcm-data", "/var/lib/lcm", "Datenverzeichnis von LCM (Zertifikat, freier Platz)")
	_ = fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return newRunner(cfg).run(ctx)
}

// runner misst Hostwerte im Takt und holt nebenher die Momentaufnahmen. Nur
// diese eine Goroutine schreibt; die Abfrage von LCM läuft in einer eigenen,
// damit ein hängendes LCM die Hostzeilen nicht aufhält.
type runner struct {
	cfg        runConfig
	sampler    *host.Sampler
	w          *journal.Writer
	compactor  journal.Compactor
	usage      selfUsage
	lastMono   time.Duration
	dbDay      string // Tag der letzten Momentaufnahme
	keyMissing bool
}

func newRunner(cfg runConfig) *runner {
	paths := []string{cfg.lcmData, "/"}
	return &runner{cfg: cfg, sampler: host.NewSampler("", host.Detect(""), paths)}
}

func (r *runner) run(ctx context.Context) error {
	w, err := journal.Open(r.cfg.dir, r.cfg.keepDays)
	if err != nil {
		return err
	}
	defer w.Close()
	r.w = w
	// Jede Tagesdatei beginnt mit der Herkunft, und ihre erste Momentaufnahme
	// steht vollständig da (writeSnapshot), damit sie für sich lesbar ist.
	w.OnNewFile = func(at time.Time) []any {
		return []any{journal.SourcesLine{Head: r.head(journal.TypeSources, at), Sources: r.sampler.Sources()}}
	}
	client := newLCMClient(r.cfg)
	r.write(journal.StartLine{
		Head: r.head(journal.TypeStart, time.Now()), Version: version.String(), PID: os.Getpid(),
		Hostname: hostname(), BootID: bootID(),
		IntervalMS: r.cfg.interval.Milliseconds(), DBIntervalMS: r.cfg.dbInterval.Milliseconds(), URL: client.base,
	})
	slog.Info("lcm-probe started", "dir", r.cfg.dir, "url", client.base, "interval", r.cfg.interval)

	snapshots := make(chan fetchResult, 1)
	go pollLCM(ctx, client, r.cfg.dbInterval, snapshots)
	hostTick := time.NewTicker(r.cfg.interval)
	defer hostTick.Stop()
	sourcesTick := time.NewTicker(time.Hour)
	defer sourcesTick.Stop()
	r.sampleHost()
	for {
		select {
		case <-ctx.Done():
			r.write(journal.StopLine{Head: r.head(journal.TypeStop, time.Now()), Signal: "SIGTERM"})
			return nil
		case <-hostTick.C:
			r.sampleHost()
		case res := <-snapshots:
			r.writeSnapshot(res)
		case <-sourcesTick.C:
			r.redetect()
		}
	}
}

func (r *runner) sampleHost() {
	now, mono := time.Now(), monotonic()
	sample := r.sampler.Sample(mono)
	line := journal.HostLine{Head: r.headAt(journal.TypeHost, now, mono), Sample: sample, Self: r.self(mono)}
	if r.lastMono > 0 {
		line.DT = (mono - r.lastMono).Milliseconds()
	}
	r.lastMono = mono
	r.write(line)
}

func (r *runner) self(mono time.Duration) journal.Self {
	return journal.Self{
		CPUMS: r.usage.cpuSince(mono), RSS: rss(),
		SyncMS:  float64(r.w.LastSync().Microseconds()) / 1000,
		Written: r.w.Written(),
	}
}

func (r *runner) writeSnapshot(res fetchResult) {
	if r.reportMissingKey(res.err) {
		return
	}
	if res.err != nil {
		r.write(journal.DownLine{Head: r.head(journal.TypeLCMDown, res.at), Error: res.err.Error()})
		return
	}
	// Die erste Momentaufnahme eines Tages steht vollständig da, auch wenn
	// sie die neue Datei eröffnet: Der Tag wird vor dem Verdichten geprüft.
	now := time.Now()
	if day := now.Format(time.DateOnly); day != r.dbDay {
		r.compactor.Reset()
		r.dbDay = day
	}
	compact, err := r.compactor.Compact(res.body)
	if err != nil {
		r.write(journal.DownLine{Head: r.head(journal.TypeLCMDown, res.at), Error: "antwort unlesbar: " + err.Error()})
		return
	}
	// Ging die Zeile verloren, darf die nächste nicht auf ihr aufbauen.
	if !r.writeAt(now, journal.DBLine{Head: r.head(journal.TypeDB, res.at), FetchMS: res.took, DB: compact}) {
		r.compactor.Reset()
	}
}

// reportMissingKey meldet einen fehlenden Schlüssel einmal im Protokoll des
// Dienstes statt jede Minute als Ausfall von LCM.
func (r *runner) reportMissingKey(err error) bool {
	missing := errors.Is(err, errNoKey)
	if missing && !r.keyMissing {
		slog.Warn("no API key, database snapshots paused", "key_file", r.cfg.keyFile)
	}
	if !missing && r.keyMissing {
		slog.Info("API key found, database snapshots resumed")
	}
	r.keyMissing = missing
	return missing
}

// redetect prüft die Quellen erneut; geänderte stehen als neue Zeile da.
func (r *runner) redetect() {
	src := host.Detect("")
	if sameSources(src, r.sampler.Sources()) {
		return
	}
	r.sampler = host.NewSampler("", src, []string{r.cfg.lcmData, "/"})
	r.write(journal.SourcesLine{Head: r.head(journal.TypeSources, time.Now()), Sources: src})
}

func (r *runner) head(typ string, at time.Time) journal.Head {
	return r.headAt(typ, at, monotonic())
}

func (r *runner) headAt(typ string, at time.Time, mono time.Duration) journal.Head {
	return journal.Head{Type: typ, Wall: at.UnixMilli(), Mono: mono.Milliseconds()}
}

// write schreibt eine Zeile. Ein Fehler beendet die Messung nicht: Die
// nächste Zeile versucht es wieder, etwa wenn die Platte voll war.
func (r *runner) write(line any) { r.writeAt(time.Now(), line) }

func (r *runner) writeAt(at time.Time, line any) bool {
	if err := r.w.Write(at, line); err != nil {
		slog.Error("writing line", "error", err)
		return false
	}
	return true
}

func hostname() string {
	name, _ := os.Hostname()
	return name
}

func bootID() string {
	b, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(b))
}

func sameSources(a, b host.Sources) bool { return reflect.DeepEqual(a, b) }
