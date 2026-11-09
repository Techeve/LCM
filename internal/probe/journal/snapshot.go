package journal

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

// Eine Momentaufnahme der Datenbank ist einige Kilobyte groß und ändert sich
// von Minute zu Minute nur in Teilen: Die Liste der langsamsten Anweisungen,
// die Zeilen je Tabelle und die Läufe bleiben meist gleich. Der Schreiber
// lässt Abschnitte weg, die gleich geblieben sind; der Leser setzt sie aus der
// vorigen Zeile wieder ein. Startet LCM neu (anderes started_at), beginnt
// beides von vorn.
//
// Die Histogramme je Tabelle und Art (statements) sind der größte Abschnitt,
// auf lcm-prod rund 20 KB, und ändern sich jede Minute, aber nur in wenigen
// Einträgen. Von ihnen schreibt der Schreiber nur die geänderten Einträge
// unter statements_changed.

// alwaysKept sind die Abschnitte, die in jeder Zeile stehen.
var alwaysKept = map[string]bool{"taken_at": true, "started_at": true}

const (
	statementsKey = "statements"
	changedKey    = "statements_changed"
)

// Compactor lässt unveränderte Abschnitte weg.
type Compactor struct{ prev map[string]json.RawMessage }

// Reset beginnt von vorn: Die nächste Momentaufnahme steht vollständig da.
// Der Schreiber ruft das am Anfang jeder Tagesdatei, damit jede Datei für
// sich lesbar bleibt.
func (c *Compactor) Reset() { c.prev = nil }

// Compact liefert die Momentaufnahme ohne die Abschnitte, die gleich der
// vorigen sind.
func (c *Compactor) Compact(snapshot []byte) (json.RawMessage, error) {
	var cur map[string]json.RawMessage
	if err := json.Unmarshal(snapshot, &cur); err != nil {
		return nil, err
	}
	if c.prev == nil || !bytes.Equal(c.prev["started_at"], cur["started_at"]) {
		c.prev = map[string]json.RawMessage{}
	}
	out := map[string]json.RawMessage{}
	for key, value := range cur {
		switch {
		case alwaysKept[key]:
			out[key] = value
		case bytes.Equal(c.prev[key], value):
		case key == statementsKey && c.prev[key] != nil:
			changed, ok, err := changedStatements(c.prev[key], value)
			if err != nil {
				return nil, err
			}
			if ok {
				out[changedKey] = changed
			} else {
				out[key] = value
			}
		default:
			out[key] = value
		}
	}
	c.prev = cur
	return json.Marshal(out)
}

// Expander setzt weggelassene Abschnitte wieder ein.
type Expander struct{ prev map[string]json.RawMessage }

// Expand liefert die vollständige Momentaufnahme.
func (e *Expander) Expand(compact json.RawMessage) (json.RawMessage, error) {
	var cur map[string]json.RawMessage
	if err := json.Unmarshal(compact, &cur); err != nil {
		return nil, err
	}
	if e.prev == nil || !bytes.Equal(e.prev["started_at"], cur["started_at"]) {
		e.prev = map[string]json.RawMessage{}
	}
	if changed, ok := cur[changedKey]; ok {
		merged, err := mergeStatements(e.prev[statementsKey], changed)
		if err != nil {
			return nil, err
		}
		delete(cur, changedKey)
		cur[statementsKey] = merged
	}
	for key, value := range e.prev {
		if _, ok := cur[key]; !ok {
			cur[key] = value
		}
	}
	e.prev = cur
	return json.Marshal(cur)
}

// changedStatements liefert die Einträge von cur, die in prev fehlen oder
// anders sind. ok ist false, wenn ein Eintrag aus prev fehlt; dann muss die
// ganze Liste in die Zeile, denn der Leser kann nichts entfernen.
func changedStatements(prev, cur json.RawMessage) (changed json.RawMessage, ok bool, err error) {
	before, err := statementsByKey(prev)
	if err != nil {
		return nil, false, err
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(cur, &entries); err != nil {
		return nil, false, err
	}
	diff := []json.RawMessage{}
	seen := 0
	for _, entry := range entries {
		old, found := before[statementKey(entry)]
		if found {
			seen++
		}
		if !found || !bytes.Equal(old, entry) {
			diff = append(diff, entry)
		}
	}
	if seen < len(before) {
		return nil, false, nil
	}
	changed, err = json.Marshal(diff)
	return changed, true, err
}

// mergeStatements setzt die geänderten Einträge in die vorige Liste ein, in
// der Reihenfolge, die LCM liefert: nach Tabelle und Art.
func mergeStatements(prev, changed json.RawMessage) (json.RawMessage, error) {
	merged, err := statementsByKey(prev)
	if err != nil {
		return nil, err
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(changed, &entries); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		merged[statementKey(entry)] = entry
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, strings.Compare)
	list := make([]json.RawMessage, len(keys))
	for i, key := range keys {
		list[i] = merged[key]
	}
	return json.Marshal(list)
}

func statementsByKey(list json.RawMessage) (map[string]json.RawMessage, error) {
	byKey := map[string]json.RawMessage{}
	if list == nil {
		return byKey, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(list, &entries); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		byKey[statementKey(entry)] = entry
	}
	return byKey, nil
}

// statementKey ist Tabelle und Art eines Eintrags, getrennt wie in LCM.
func statementKey(entry json.RawMessage) string {
	var id struct {
		Table string `json:"table"`
		Kind  string `json:"kind"`
	}
	_ = json.Unmarshal(entry, &id)
	return id.Table + " " + id.Kind
}
