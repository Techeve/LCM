package journal

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Writer hängt Zeilen an die Datei des Tages.
type Writer struct {
	dir      string
	keepDays int
	// OnNewFile liefert Zeilen, die am Anfang jeder neuen Tagesdatei stehen
	// (die Herkunft der Werte), damit jede Datei für sich lesbar ist.
	OnNewFile func(at time.Time) []any

	f        *os.File
	day      string
	lastSync time.Duration
	written  uint64
	cleaning sync.WaitGroup
}

// Open bereitet das Verzeichnis vor. Ältere Tage packt und löscht es sofort,
// nicht erst um Mitternacht.
func Open(dir string, keepDays int) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, keepDays: keepDays}
	w.housekeeping(time.Now())
	return w, nil
}

// Write hängt eine Zeile an und bringt sie auf die Platte.
func (w *Writer) Write(at time.Time, line any) error {
	if err := w.ensureFile(at); err != nil {
		return err
	}
	return w.append(line)
}

// LastSync ist die Dauer des letzten fdatasync - ein direktes Maß dafür,
// wie schnell die Platte gerade schreibt.
func (w *Writer) LastSync() time.Duration { return w.lastSync }

// Written zählt die geschriebenen Bytes.
func (w *Writer) Written() uint64 { return w.written }

// Close schließt die offene Datei und wartet auf ein laufendes Packen.
func (w *Writer) Close() error {
	w.cleaning.Wait()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

func (w *Writer) append(line any) error {
	b, err := json.Marshal(line)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := w.f.Write(b); err != nil {
		return err
	}
	w.written += uint64(len(b))
	started := time.Now()
	err = datasync(w.f)
	w.lastSync = time.Since(started)
	return err
}

// ensureFile wechselt bei einem neuen Tag die Datei.
func (w *Writer) ensureFile(at time.Time) error {
	day := at.Format(dayLayout)
	if w.f != nil && day == w.day {
		return nil
	}
	if err := w.Close(); err != nil {
		slog.Warn("closing day file", "error", err)
	}
	f, err := openAppend(filepath.Join(w.dir, fileName(day)))
	if err != nil {
		return err
	}
	w.f, w.day = f, day
	// Den Vortag zu packen dauert auf einer langsamen Platte Sekunden. Das
	// läuft neben dem Messtakt, sonst sähe der Bericht um Mitternacht eine
	// Lücke, die lcm-probe selbst verursacht hat.
	w.cleaning.Add(1)
	go func() {
		defer w.cleaning.Done()
		w.housekeeping(at)
	}()
	if w.OnNewFile == nil {
		return nil
	}
	for _, line := range w.OnNewFile(at) {
		if err := w.append(line); err != nil {
			return err
		}
	}
	return nil
}

// openAppend öffnet die Datei zum Anhängen. Endet sie mitten in einer Zeile
// (Absturz beim Schreiben), beginnt die nächste Zeile auf einer neuen Zeile -
// sonst klebte sie an der angefangenen und wäre mit ihr verloren.
func openAppend(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o640)
	if err != nil {
		return nil, err
	}
	if endsMidLine(path) {
		if _, err := f.Write([]byte{'\n'}); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

func endsMidLine(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return false
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil {
		return false
	}
	return last[0] != '\n'
}

// housekeeping packt fertige Tage und löscht, was älter ist als keepDays.
func (w *Writer) housekeeping(now time.Time) {
	today := now.Format(dayLayout)
	oldest := now.AddDate(0, 0, -w.keepDays).Format(dayLayout)
	removeLeftovers(w.dir)
	for _, f := range listFiles(w.dir) {
		switch {
		case f.day < oldest:
			if err := os.Remove(f.path); err != nil {
				slog.Warn("removing old day file", "file", f.path, "error", err)
			}
		case f.day < today && !f.compressed:
			if err := compress(f.path); err != nil {
				slog.Warn("compressing day file", "file", f.path, "error", err)
			}
		}
	}
}

// removeLeftovers räumt auf, was ein Absturz beim Packen hinterlässt: eine
// Zwischendatei oder das Original neben der fertigen gepackten Datei.
func removeLeftovers(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		_, err := os.Stat(filepath.Join(dir, name+gzSuffix))
		packedExists := err == nil
		if strings.HasSuffix(name, gzSuffix+".tmp") || (strings.HasSuffix(name, fileSuffix) && packedExists) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				slog.Warn("removing leftover", "file", name, "error", err)
			}
		}
	}
}

// compress packt eine Tagesdatei: erst vollständig unter Zwischennamen
// schreiben und sichern, dann umbenennen, dann das Original löschen. Bricht
// das mittendrin ab, liegt höchstens eine Zwischendatei herum.
func compress(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := path + gzSuffix + ".tmp"
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(dst)
	_, err = io.Copy(zw, src)
	err = errors.Join(err, zw.Close(), dst.Sync(), dst.Close())
	if err == nil {
		err = os.Rename(tmp, path+gzSuffix)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// syncDir sichert Umbenennen und Löschen im Verzeichnis, damit nach einem
// harten Neustart nicht beide Fassungen oder keine dastehen.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

type dayFile struct {
	path       string
	day        string
	compressed bool
}

// listFiles liefert die Tagesdateien nach Tag sortiert. Liegt ein Tag
// gepackt und ungepackt vor, gilt die gepackte Datei.
func listFiles(dir string) []dayFile {
	entries, _ := os.ReadDir(dir)
	byDay := map[string]dayFile{}
	for _, e := range entries {
		day, compressed, ok := dayOf(e.Name())
		if !ok {
			continue
		}
		if prev, seen := byDay[day]; seen && prev.compressed {
			continue
		}
		byDay[day] = dayFile{path: filepath.Join(dir, e.Name()), day: day, compressed: compressed}
	}
	files := make([]dayFile, 0, len(byDay))
	for _, f := range byDay {
		files = append(files, f)
	}
	slices.SortFunc(files, func(a, b dayFile) int { return strings.Compare(a.day, b.day) })
	return files
}
