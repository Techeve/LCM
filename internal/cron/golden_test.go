package cron

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	// Die Zeitzonen-Daten mitbringen: Das Alpine-Image der CI hat keine.
	_ "time/tzdata"
)

// goldenCase ist eine Zeile aus testdata/golden.jsonl, erzeugt von
// testdata/gen/main.go mit der früheren Cron-Bibliothek. Error gesetzt heißt:
// sie lehnte den Ausdruck ab; Panic: sie lief dabei in einen Panic.
type goldenCase struct {
	Spec  string   `json:"spec"`
	Error string   `json:"error"`
	Panic bool     `json:"panic"`
	Loc   string   `json:"loc"`
	Start string   `json:"start"`
	Next  []string `json:"next"`
}

// TestGolden vergleicht Parse und Next mit dem festgehaltenen Verhalten der
// früheren Cron-Bibliothek: dieselben Ausdrücke angenommen, dieselben mit
// demselben Text abgelehnt, dieselben fünf Folgetermine je Startzeit.
func TestGolden(t *testing.T) {
	cases := readGolden(t)
	if len(cases) < 3000 {
		t.Fatalf("nur %d Fälle in der Golden-Datei", len(cases))
	}
	for _, c := range cases {
		sched, err := Parse(c.Spec)
		switch {
		case c.Panic:
			if err == nil {
				t.Errorf("%q: bisher ein Panic, hier angenommen", c.Spec)
			}
		case c.Error != "":
			if err == nil || err.Error() != c.Error {
				t.Errorf("%q: Fehler %v, bisher: %s", c.Spec, err, c.Error)
			}
		case err != nil:
			t.Errorf("%q: abgelehnt (%v), bisher angenommen", c.Spec, err)
		default:
			checkChain(t, c, sched)
		}
	}
}

// checkChain rechnet die Folgetermine eines Falls nach.
func checkChain(t *testing.T, c goldenCase, sched Schedule) {
	t.Helper()
	loc, err := time.LoadLocation(c.Loc)
	if err != nil {
		t.Fatalf("zone %s: %v", c.Loc, err)
	}
	start, err := time.Parse(time.RFC3339Nano, c.Start)
	if err != nil {
		t.Fatalf("start %s: %v", c.Start, err)
	}
	next := start.In(loc)
	for i, want := range c.Next {
		next = sched.Next(next)
		got := ""
		if !next.IsZero() {
			got = next.Format(time.RFC3339Nano)
		}
		if got != want || (!next.IsZero() && next.Location() != loc) {
			t.Errorf("%q ab %s (%s), Termin %d: %s in %s, bisher: %s", c.Spec, c.Start, c.Loc, i+1, got, next.Location(), want)
			return
		}
	}
}

func readGolden(t *testing.T) []goldenCase {
	t.Helper()
	f, err := os.Open("testdata/golden.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cases []goldenCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var c goldenCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatalf("golden-zeile %q: %v", sc.Text(), err)
		}
		cases = append(cases, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}
