package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"LCM/internal/probe/analysis"
	"LCM/internal/probe/journal"
)

// window liest --since, --from und --to. Relative Angaben zählen ab der
// jüngsten Zeile im Verzeichnis.
type window struct{ since, from, to string }

func (w *window) flags(fs *flag.FlagSet, since string) {
	fs.StringVar(&w.since, "since", since, "Zeitraum bis zur jüngsten Zeile, etwa 6h oder 7d")
	fs.StringVar(&w.from, "from", "", "Beginn (überschreibt --since)")
	fs.StringVar(&w.to, "to", "", "Ende (Vorgabe: jüngste Zeile)")
}

func (w *window) resolve(dir string) (from, to time.Time, err error) {
	anchor, ok := journal.Latest(dir)
	if !ok {
		return from, to, fmt.Errorf("keine Tagesdateien in %s", dir)
	}
	start := w.from
	if start == "" {
		start = w.since
	}
	if start != "" {
		if from, err = analysis.ParseTime(start, anchor); err != nil {
			return from, to, err
		}
	}
	if w.to != "" {
		to, err = analysis.ParseTime(w.to, anchor)
	}
	return from, to, err
}

func reportCommand(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	dir := fs.String("dir", defaultDir, "Verzeichnis der Tagesdateien")
	htmlFile := fs.String("html", "", "HTML-Bericht mit Diagrammen in diese Datei schreiben")
	var w window
	w.flags(fs, "7d")
	_ = fs.Parse(args)

	from, to, err := w.resolve(*dir)
	if err != nil {
		return err
	}
	data, err := analysis.Load(*dir, from, to)
	if err != nil {
		return err
	}
	report := analysis.Build(data)
	if *htmlFile == "" {
		return analysis.WriteText(os.Stdout, report)
	}
	f, err := os.Create(*htmlFile)
	if err != nil {
		return err
	}
	err = analysis.WriteHTML(f, report, data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		fmt.Fprintf(os.Stderr, "Bericht geschrieben: %s\n", *htmlFile)
	}
	return err
}

func queryCommand(args []string) error {
	fs := flag.NewFlagSet("query", flag.ExitOnError)
	dir := fs.String("dir", defaultDir, "Verzeichnis der Tagesdateien")
	typ := fs.String("type", journal.TypeHost, "Zeilentyp: host, db, lcm_down, start, stop, sources oder all")
	fields := fs.String("fields", "", "Felder mit Punkt-Pfad, etwa psi.io_some,cpu.iowait,disks.sda.await_ms (leer: alle)")
	format := fs.String("format", "csv", "csv oder json")
	var w window
	w.flags(fs, "")
	_ = fs.Parse(args)
	if w.from == "" && w.since == "" {
		return errors.New("--from oder --since fehlt")
	}
	from, to, err := w.resolve(*dir)
	if err != nil {
		return err
	}
	var names []string
	if *fields != "" {
		names = strings.Split(*fields, ",")
	}
	return analysis.Query(os.Stdout, analysis.QueryOptions{
		Dir: *dir, From: from, To: to, Type: *typ, Fields: names, Format: *format,
	})
}
