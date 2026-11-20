package logging

import (
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"LCM/internal/safego"
)

// Rotierende Logdatei - selbst gebaut statt als Abhängigkeit: keine
// Kryptographie, keine fremde Eingabe, harmloses Fehlerbild. Begründung und
// Regel: docs/reference/dependencies.md.
//
// Ablauf: Überschreitet ein Schreibvorgang die Höchstgröße, wandert der
// aktuelle Stand mit Zeitstempel im Namen zur Seite und die Datei beginnt von
// vorn. Im Hintergrund wird der Altstand gzip-komprimiert, danach fliegen
// Altstände raus, die zu alt oder zu zahlreich sind. Im Hintergrund, weil der
// Schreiber dabei die Sperre hält: Zehn Megabyte zu komprimieren hielt sonst
// jede Logzeile des Prozesses auf, auch die der laufenden Anfragen.
//
// Aufräumen und Komprimieren sind bewusst fehlertolerant: ein Logger darf die
// Anwendung nicht anhalten, nur weil ein Altstand klemmt.

// backupTimeFormat steht im Dateinamen der Altstände. Ohne Doppelpunkte, damit
// die Namen auch unter Windows gültig sind; die lexikografische Sortierung
// entspricht der zeitlichen.
const backupTimeFormat = "2006-01-02T15-04-05.000"

type rotatingFile struct {
	path       string
	maxSize    int64
	maxBackups int
	maxAge     time.Duration
	now        func() time.Time // in Tests überschrieben

	mu   sync.Mutex
	file *os.File
	size int64

	// compress ist im Test ersetzbar. bgMu reiht Komprimieren und Aufräumen
	// der Hintergrundläufe hintereinander, bg zählt die offenen (Close wartet).
	compress func(path string)
	bgMu     sync.Mutex
	bg       sync.WaitGroup
}

func newRotatingFile(path string, maxSizeMB, maxBackups, maxAgeDays int) *rotatingFile {
	return &rotatingFile{
		path:       path,
		maxSize:    int64(maxSizeMB) * 1024 * 1024,
		maxBackups: maxBackups,
		maxAge:     time.Duration(maxAgeDays) * 24 * time.Hour,
		now:        time.Now,
		compress:   compress,
	}
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

// Close schließt die Datei und wartet, bis die Altstände komprimiert sind.
func (r *rotatingFile) Close() error {
	defer r.bg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

// open hängt an eine bestehende Datei an oder legt sie an. Die Rechte sind
// restriktiv: Logs können Details aus Fehlermeldungen enthalten.
func (r *rotatingFile) open() error {
	if err := os.MkdirAll(filepath.Dir(r.path), logDirFilePerm); err != nil {
		return err
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFilePerm)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.file, r.size = f, info.Size()
	return nil
}

// rotate legt den aktuellen Stand zur Seite und beginnt eine neue Datei.
func (r *rotatingFile) rotate() error {
	if err := r.file.Close(); err != nil {
		return err
	}
	r.file = nil

	backup := r.backupPath(r.now())
	if err := os.Rename(r.path, backup); err != nil {
		return err
	}
	if err := r.open(); err != nil {
		return err
	}
	r.compressInBackground(backup, r.now())
	return nil
}

// compressInBackground komprimiert den Altstand und räumt danach auf, ohne
// den Schreiber aufzuhalten. now kommt vom Schreiber mit, damit die Uhr nur
// unter dessen Sperre gelesen wird.
func (r *rotatingFile) compressInBackground(backup string, now time.Time) {
	r.bg.Add(1)
	safego.Go("log-rotate", func() {
		defer r.bg.Done()
		r.bgMu.Lock()
		defer r.bgMu.Unlock()
		r.compress(backup)
		r.prune(now)
	})
}

// backupPath baut "lcm-2026-08-18T14-05-09.123.log" aus "lcm.log".
func (r *rotatingFile) backupPath(t time.Time) string {
	ext := filepath.Ext(r.path)
	base := strings.TrimSuffix(r.path, ext)
	return base + "-" + t.Format(backupTimeFormat) + ext
}

// compress schreibt die Datei gzip-komprimiert daneben und entfernt das
// Original. Schlägt etwas fehl, bleibt der unkomprimierte Stand liegen - das
// ist immer noch besser als ein verlorener Altstand.
func compress(path string) {
	src, err := os.Open(path)
	if err != nil {
		return
	}
	dst, err := os.OpenFile(path+".gz", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, logFilePerm)
	if err != nil {
		_ = src.Close()
		return
	}
	zw := gzip.NewWriter(dst)
	_, copyErr := io.Copy(zw, src)
	if err := errors.Join(copyErr, zw.Close(), dst.Close(), src.Close()); err != nil {
		_ = os.Remove(path + ".gz")
		return
	}
	_ = os.Remove(path)
}

// prune entfernt Altstände, die zu alt sind oder über die erlaubte Anzahl
// hinausgehen. Fehler werden verschluckt: der nächste Durchlauf versucht es
// erneut.
func (r *rotatingFile) prune(now time.Time) {
	backups := r.backups()
	cutoff := now.Add(-r.maxAge)
	for i, b := range backups {
		tooMany := r.maxBackups > 0 && i >= r.maxBackups
		tooOld := r.maxAge > 0 && b.stamp.Before(cutoff)
		if tooMany || tooOld {
			_ = os.Remove(b.path)
		}
	}
}

type backup struct {
	path  string
	stamp time.Time
}

// backups sammelt die Altstände neben der Logdatei, jüngster zuerst. Der
// Zeitstempel kommt aus dem Dateinamen, nicht aus den Dateizeiten - kopierte
// oder wiederhergestellte Dateien sollen dieselbe Reihenfolge behalten.
func (r *rotatingFile) backups() []backup {
	ext := filepath.Ext(r.path)
	prefix := strings.TrimSuffix(filepath.Base(r.path), ext) + "-"

	entries, err := os.ReadDir(filepath.Dir(r.path))
	if err != nil {
		return nil
	}
	var found []backup
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		stamp, ok := parseBackupTime(e.Name(), prefix, ext)
		if !ok {
			continue
		}
		found = append(found, backup{filepath.Join(filepath.Dir(r.path), e.Name()), stamp})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].stamp.After(found[j].stamp) })
	return found
}

// parseBackupTime liest den Zeitstempel aus einem Altstand-Namen; die Endung
// .gz der komprimierten Stände wird dabei ignoriert.
func parseBackupTime(name, prefix, ext string) (time.Time, bool) {
	if !strings.HasPrefix(name, prefix) {
		return time.Time{}, false
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".gz")
	rest = strings.TrimSuffix(rest, ext)
	stamp, err := time.Parse(backupTimeFormat, rest)
	if err != nil {
		return time.Time{}, false
	}
	return stamp, true
}
