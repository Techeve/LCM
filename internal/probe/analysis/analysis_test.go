package analysis

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"LCM/internal/dbmetrics"
	"LCM/internal/probe/journal"
)

// Die Beispieldatei testdata/lcm-probe-2026-11-07.jsonl zeigt eine
// Viertelstunde auf lcm-test: um 04:03 steht der Container 100 Sekunden,
// davor laufen CVE-Scan und VACUUM INTO, der I/O-Druck steigt; danach ist LCM
// zwei Minuten nicht erreichbar. Die letzte Zeile ist abgeschnitten.

func TestMain(m *testing.M) {
	time.Local = time.FixedZone("CET", 3600) // Uhrzeiten wie auf lcm-test
	os.Exit(m.Run())
}

func loadExample(t *testing.T) (*Data, *Report) {
	t.Helper()
	d, err := Load("testdata", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return d, Build(d)
}

func TestBerichtFindetDenStillstandUndWasDavorLief(t *testing.T) {
	d, r := loadExample(t)
	if d.Stats.Skipped != 1 {
		t.Errorf("übersprungene Zeilen %d, erwartet die abgeschnittene letzte", d.Stats.Skipped)
	}
	if len(r.Gaps) != 1 {
		t.Fatalf("Lücken %+v, erwartet eine", r.Gaps)
	}
	g := r.Gaps[0]
	if g.Kind != "Stillstand" || g.Duration != 100*time.Second || clock(g.Start) != "Sa 07.11. 04:03:00" {
		t.Errorf("Lücke %+v", g)
	}
	before := strings.Join(g.Before, "\n")
	for _, want := range []string{"CVE-Scan", "VACUUM INTO (Backup), 95,0 s", "I/O-Druck 35,5 %", "fdatasync 180,0 ms"} {
		if !strings.Contains(before, want) {
			t.Errorf("vor der Lücke fehlt %q:\n%s", want, before)
		}
	}
	if r.LockWait.MaxMS != 1800 || clock(r.LockWait.MaxAt) != "Sa 07.11. 04:03:00" {
		t.Errorf("längste Wartezeit %v um %v", r.LockWait.MaxMS, r.LockWait.MaxAt)
	}
	if len(r.Outages) != 1 || r.Outages[0].Polls != 2 {
		t.Errorf("Zeiten ohne LCM %+v", r.Outages)
	}
	if len(r.Growth.Tables) == 0 || r.Growth.Tables[0].Table != "jobs" || r.Growth.Tables[0].Last-r.Growth.Tables[0].First != 20 {
		t.Errorf("Wachstum %+v", r.Growth.Tables)
	}
	if len(r.Slowest) != 0 {
		t.Errorf("die langsamste Anweisung lag vor dem Zeitraum: %+v", r.Slowest)
	}
}

func TestPauseDerVM(t *testing.T) {
	at := time.Date(2026, 11, 8, 4, 0, 0, 0, time.Local)
	host := func(wall time.Time, mono int64) journal.HostLine {
		return journal.HostLine{Head: journal.Head{Type: journal.TypeHost, Wall: wall.UnixMilli(), Mono: mono}}
	}
	d := &Data{Hosts: []journal.HostLine{host(at, 1000), host(at.Add(2*time.Second), 3000), host(at.Add(92*time.Second), 5000)}}
	gaps, jumps := findGaps(d, 2*time.Second)
	if len(gaps) != 1 || gaps[0].Duration != 90*time.Second || len(jumps) != 0 {
		t.Fatalf("Lücken %+v, Sprünge %+v", gaps, jumps)
	}
}

func TestTextbericht(t *testing.T) {
	_, r := loadExample(t)
	var buf bytes.Buffer
	if err := WriteText(&buf, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"lcm-probe Bericht: Sa 07.11. 03:55:00 bis Sa 07.11. 04:10:00",
		"Sa 07.11. 04:03:00  1 min 40 s  Stillstand",
		"Maximum 1,8 s um Sa 07.11. 04:03:00",
		"jobs          SELECT",
		"Sa 07.11. 04:05:00  60,0 s  2 Abfragen",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("fehlt: %q\n%s", want, buf.String())
		}
	}
}

func TestHTMLBerichtIstEineDateiOhneNetz(t *testing.T) {
	d, r := loadExample(t)
	var buf bytes.Buffer
	if err := WriteHTML(&buf, r, d); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	for _, banned := range []string{"<script src", "<link ", "http://", "https://cdn"} {
		if strings.Contains(page, banned) {
			t.Errorf("lädt aus dem Netz: %q", banned)
		}
	}
	if !strings.Contains(page, "var uPlot=") || !strings.Contains(page, `id="data"`) {
		t.Fatal("uPlot oder Daten fehlen")
	}
	start := strings.Index(page, `<script type="application/json" id="data">`) + len(`<script type="application/json" id="data">`)
	end := strings.Index(page[start:], "</script>")
	var p payload
	if err := json.Unmarshal([]byte(page[start:start+end]), &p); err != nil {
		t.Fatalf("Daten unlesbar: %v", err)
	}
	if len(p.Host.X) < 300 || len(p.Gaps) != 1 || len(p.Ops) != 2 || len(p.Outages) != 1 {
		t.Errorf("Punkte %d, Lücken %d, Läufe %d, ohne LCM %d", len(p.Host.X), len(p.Gaps), len(p.Ops), len(p.Outages))
	}
	// Die Linie bricht an der Lücke ab, statt sie zu überbrücken.
	ioSome := p.Host.Series["io_some"].([]any)
	nulls := 0
	for _, v := range ioSome {
		if v == nil {
			nulls++
		}
	}
	if nulls != 1 {
		t.Errorf("%d leere Punkte in io_some, erwartet einen an der Lücke", nulls)
	}
}

func TestQueryCSV(t *testing.T) {
	from := time.Date(2026, 11, 7, 4, 2, 50, 0, time.Local)
	to := time.Date(2026, 11, 7, 4, 3, 0, 0, time.Local)
	var buf bytes.Buffer
	err := Query(&buf, QueryOptions{Dir: "testdata", From: from, To: to, Type: "host",
		Fields: []string{"psi.io_some", "disks.sda.await_ms", "self.sync_ms"}, Format: "csv"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if lines[0] != "time,psi.io_some,disks.sda.await_ms,self.sync_ms" || len(lines) != 7 {
		t.Fatalf("CSV:\n%s", buf.String())
	}
	if lines[4] != "2026-11-07T04:02:56.000,35.5,48.0,0.8" {
		t.Errorf("Zeile um 04:02:56: %s", lines[4])
	}
}

func TestQueryDBMitWeggelassenenAbschnitten(t *testing.T) {
	from := time.Date(2026, 11, 7, 3, 58, 0, 0, time.Local)
	var buf bytes.Buffer
	err := Query(&buf, QueryOptions{Dir: "testdata", From: from, To: from, Type: "db",
		Fields: []string{"db.slowest.0.max_ms", "db.statements.jobs.SELECT.count"}, Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	// Um 03:58 fehlt slowest in der Datei; der Leser setzt es aus 03:57 ein.
	if !strings.Contains(buf.String(), `"db.slowest.0.max_ms":8`) || !strings.Contains(buf.String(), `"db.statements.jobs.SELECT.count":420`) {
		t.Errorf("JSON: %s", buf.String())
	}
}

func TestParseTime(t *testing.T) {
	anchor := time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)
	cases := map[string]time.Time{
		"7d":               anchor.AddDate(0, 0, -7),
		"90m":              anchor.Add(-90 * time.Minute),
		"04:00":            time.Date(2026, 11, 8, 4, 0, 0, 0, time.Local),
		"2026-11-07 04:30": time.Date(2026, 11, 7, 4, 30, 0, 0, time.Local),
		"2026-11-07T04:30": time.Date(2026, 11, 7, 4, 30, 0, 0, time.Local),
	}
	for in, want := range cases {
		got, err := ParseTime(in, anchor)
		if err != nil || !got.Equal(want) {
			t.Errorf("%s: %v %v, erwartet %v", in, got, err, want)
		}
	}
	if _, err := ParseTime("gestern", anchor); err == nil {
		t.Error("unlesbare Angabe ohne Fehler")
	}
}

// TestSnapshotPasstZuDbmetrics hält die eigenen Typen der Auswertung im
// Gleichschritt mit dem, was LCM ausliefert.
func TestSnapshotPasstZuDbmetrics(t *testing.T) {
	at := time.Date(2026, 11, 8, 4, 0, 0, 0, time.UTC)
	src := dbmetrics.Snapshot{
		StartedAt: at, LockWait: dbmetrics.Histogram{Bounds: dbmetrics.LockWaitBounds, Counts: make([]uint64, 16), Count: 3, MaxMS: 12, MaxAt: at},
		Statements:  []dbmetrics.StatementStats{{Table: "jobs", Kind: "SELECT", Histogram: dbmetrics.Histogram{Count: 7, SumMS: 1.5}}},
		Slowest:     []dbmetrics.SlowStatement{{SQL: "SELECT ?", MaxMS: 3, At: at}},
		Errors:      dbmetrics.ErrorCounts{Busy: 2},
		Operations:  []dbmetrics.Operation{{Kind: "vacuum", Started: at, DurationMS: 5}},
		TableRows:   dbmetrics.TableRows{CountedAt: at, Tables: map[string]int64{"jobs": 9}},
		File:        dbmetrics.FileStats{DBBytes: 100, FreelistCount: 2, PageSize: 4096},
		RowsWritten: 11,
	}
	b, _ := json.Marshal(src)
	var got snapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !got.StartedAt.Equal(at) || got.LockWait.Count != 3 || len(got.LockWait.Bounds) != 15 || got.LockWait.MaxMS != 12 ||
		got.Statements[0].Count != 7 || got.Statements[0].Table != "jobs" || got.Slowest[0].SQL != "SELECT ?" ||
		got.Errors.Busy != 2 || got.Operations[0].Kind != "vacuum" || got.TableRows.Tables["jobs"] != 9 ||
		got.File.FreelistCount != 2 || got.RowsWritten != 11 {
		t.Errorf("Felder gehen verloren: %+v", got)
	}
}
