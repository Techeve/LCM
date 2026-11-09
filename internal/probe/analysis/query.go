package analysis

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"LCM/internal/probe/journal"
)

// QueryOptions wählt Zeilen und Felder für lcm-probe query.
type QueryOptions struct {
	Dir      string
	From, To time.Time
	Type     string   // Zeilentyp oder "all"
	Fields   []string // Punkt-Pfade; leer = alle Felder der ersten Zeile
	Format   string   // csv oder json
}

// Query gibt Zeilen als CSV oder JSON Lines aus. Felder stehen als
// Punkt-Pfad: psi.io_some, disks.sda.await_ms, db.lock_wait.max_ms.
// Platten stehen unter ihrem Namen, Anweisungen unter Tabelle und Art,
// alle anderen Listen unter der Stelle: db.slowest.0.sql.
func Query(w io.Writer, o QueryOptions) error {
	if o.Format != "csv" && o.Format != "json" {
		return fmt.Errorf("format %q: csv oder json", o.Format)
	}
	q := &query{opts: o, out: w, csv: csv.NewWriter(w)}
	_, err := journal.Read(o.Dir, o.From, o.To, q.line)
	q.csv.Flush()
	return errors.Join(err, q.csv.Error())
}

type query struct {
	opts     QueryOptions
	out      io.Writer
	csv      *csv.Writer
	header   []string
	expander journal.Expander
}

func (q *query) line(e journal.Entry) error {
	raw := e.Raw
	if e.Type == journal.TypeDB {
		expanded, err := q.expandDB(raw)
		if err != nil {
			return nil
		}
		raw = expanded
	}
	if !e.In(q.opts.From, q.opts.To) || (q.opts.Type != "all" && q.opts.Type != e.Type) {
		return nil
	}
	flat, err := flattenJSON(raw)
	if err != nil {
		return nil
	}
	flat["time"] = e.Time().Local().Format("2006-01-02T15:04:05.000")
	if q.opts.Format == "json" {
		return q.writeJSON(flat)
	}
	return q.writeCSV(flat)
}

// expandDB setzt die weggelassenen Abschnitte einer Momentaufnahme ein.
func (q *query) expandDB(raw []byte) ([]byte, error) {
	var line map[string]json.RawMessage
	if err := json.Unmarshal(raw, &line); err != nil {
		return nil, err
	}
	full, err := q.expander.Expand(line["db"])
	if err != nil {
		return nil, err
	}
	line["db"] = full
	return json.Marshal(line)
}

func (q *query) columns(flat map[string]any) []string {
	if q.header != nil {
		return q.header
	}
	if len(q.opts.Fields) > 0 {
		q.header = append([]string{"time"}, q.opts.Fields...)
		return q.header
	}
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range flat {
			if k != "time" && !yield(k) {
				return
			}
		}
	})
	q.header = append([]string{"time"}, keys...)
	return q.header
}

func (q *query) writeCSV(flat map[string]any) error {
	first := q.header == nil
	cols := q.columns(flat)
	if first {
		if err := q.csv.Write(cols); err != nil {
			return err
		}
	}
	row := make([]string, len(cols))
	for i, c := range cols {
		row[i] = cell(flat[c])
	}
	return q.csv.Write(row)
}

func (q *query) writeJSON(flat map[string]any) error {
	out := flat
	if len(q.opts.Fields) > 0 {
		out = map[string]any{"time": flat["time"]}
		for _, f := range q.opts.Fields {
			out[f] = flat[f]
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = q.out.Write(append(b, '\n'))
	return err
}

func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// flattenJSON macht aus verschachteltem JSON eine Ebene mit Punkt-Pfaden.
func flattenJSON(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	out := map[string]any{}
	flatten("", v, out)
	return out, nil
}

func flatten(prefix string, v any, out map[string]any) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			flatten(join(prefix, k), child, out)
		}
	case []any:
		for i, child := range x {
			flatten(join(prefix, elementKey(prefix, child, i)), child, out)
		}
	default:
		out[prefix] = v
	}
}

// elementKey adressiert Listenelemente über ihren Namen (Platten) und die
// Histogramme der Anweisungen über Tabelle und Art; alle anderen über die
// Stelle in der Liste.
func elementKey(prefix string, v any, i int) string {
	m, ok := v.(map[string]any)
	if !ok {
		return strconv.Itoa(i)
	}
	if name, ok := m["name"].(string); ok && name != "" {
		return name
	}
	table, okT := m["table"].(string)
	kind, okK := m["kind"].(string)
	if okT && okK && strings.HasSuffix(prefix, "statements") {
		return table + "." + kind
	}
	return strconv.Itoa(i)
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
