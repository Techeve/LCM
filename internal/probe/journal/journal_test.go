package journal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testLine struct {
	Head
	N int `json:"n"`
}

func line(at time.Time, n int) testLine {
	return testLine{Head: Head{Type: TypeHost, Wall: at.UnixMilli(), Mono: int64(n)}, N: n}
}

func readAll(t *testing.T, dir string) ([]int, Stats) {
	t.Helper()
	var got []int
	st, err := Read(dir, time.Time{}, time.Time{}, func(e Entry) error {
		var l testLine
		if err := e.Decode(&l); err != nil {
			return err
		}
		got = append(got, l.N)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got, st
}

func TestAbgeschnitteneLetzteZeileWirdUebersprungen(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	w, err := Open(dir, 90)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if err := w.Write(now, line(now, i)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	// Absturz mitten im Schreiben der vierten Zeile.
	path := filepath.Join(dir, fileName(now.Format(dayLayout)))
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	f.WriteString(`{"t":"host","ts":1,"mono":4,"n":`)
	f.Close()

	got, st := readAll(t, dir)
	if len(got) != 3 || st.Skipped != 1 {
		t.Fatalf("gelesen %v, übersprungen %d", got, st.Skipped)
	}

	// Nach dem Neustart beginnt die nächste Zeile auf einer eigenen Zeile.
	w, _ = Open(dir, 90)
	w.Write(now, line(now, 5))
	w.Close()
	got, st = readAll(t, dir)
	if jsonOf(got) != "[1,2,3,5]" || st.Skipped != 1 {
		t.Fatalf("nach Neustart %v, übersprungen %d", got, st.Skipped)
	}
}

func TestTageWerdenGepacktUndNachFristGeloescht(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.AddDate(0, 0, -100)
	yesterday := now.AddDate(0, 0, -1)
	for _, at := range []time.Time{old, yesterday} {
		w, _ := Open(dir, 1000)
		w.Write(at, line(at, at.Day()))
		w.Close()
	}
	w, err := Open(dir, 90)
	if err != nil {
		t.Fatal(err)
	}
	w.OnNewFile = func(at time.Time) []any {
		return []any{testLine{Head: Head{Type: TypeSources, Wall: at.UnixMilli()}}}
	}
	w.Write(now, line(now, 99))
	w.Close()

	names := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		names[e.Name()] = true
	}
	if names[fileName(old.Format(dayLayout))] || names[fileName(old.Format(dayLayout))+gzSuffix] {
		t.Error("Tag vor 100 Tagen hätte gelöscht sein müssen")
	}
	if !names[fileName(yesterday.Format(dayLayout))+gzSuffix] || names[fileName(yesterday.Format(dayLayout))] {
		t.Errorf("gestern hätte gepackt sein müssen: %v", names)
	}
	got, _ := readAll(t, dir)
	if len(got) != 3 || got[0] != yesterday.Day() || got[2] != 99 {
		t.Fatalf("gelesen %v, erwartet gestern, Quellenzeile, heute", got)
	}
}

func TestResteNachAbsturzBeimPacken(t *testing.T) {
	dir := t.TempDir()
	day := time.Now().AddDate(0, 0, -2).Format(dayLayout)
	for _, name := range []string{fileName(day), fileName(day) + gzSuffix, fileName(day) + gzSuffix + ".tmp"} {
		os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o640)
	}
	w, err := Open(dir, 90)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != fileName(day)+gzSuffix {
		t.Fatalf("übrig: %v", entries)
	}
}

func TestMomentaufnahmeOhneUnveraenderteAbschnitte(t *testing.T) {
	var c Compactor
	var e Expander
	snaps := []string{
		`{"started_at":"a","taken_at":"1","slowest":[1,2],"rows":5}`,
		`{"started_at":"a","taken_at":"2","slowest":[1,2],"rows":6}`,
		`{"started_at":"b","taken_at":"3","slowest":[1,2],"rows":6}`,
	}
	wantCompact := []string{
		`{"rows":5,"slowest":[1,2],"started_at":"a","taken_at":"1"}`,
		`{"rows":6,"started_at":"a","taken_at":"2"}`,
		`{"rows":6,"slowest":[1,2],"started_at":"b","taken_at":"3"}`,
	}
	for i, s := range snaps {
		compact, err := c.Compact([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		if string(compact) != wantCompact[i] {
			t.Errorf("%d: %s, erwartet %s", i, compact, wantCompact[i])
		}
		full, _ := e.Expand(compact)
		var a, b map[string]any
		json.Unmarshal(full, &a)
		json.Unmarshal([]byte(s), &b)
		if jsonOf(a) != jsonOf(b) {
			t.Errorf("%d: wiederhergestellt %s, erwartet %s", i, full, s)
		}
	}
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
