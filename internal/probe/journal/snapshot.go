package journal

import (
	"bytes"
	"encoding/json"
)

// Eine Momentaufnahme der Datenbank ist einige Kilobyte groß und ändert sich
// von Minute zu Minute nur in Teilen: Die Liste der langsamsten Anweisungen,
// die Zeilen je Tabelle und die Läufe bleiben meist gleich. Der Schreiber
// lässt Abschnitte weg, die gleich geblieben sind; der Leser setzt sie aus der
// vorigen Zeile wieder ein. Startet LCM neu (anderes started_at), beginnt
// beides von vorn.

// alwaysKept sind die Abschnitte, die in jeder Zeile stehen.
var alwaysKept = map[string]bool{"taken_at": true, "started_at": true}

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
		if alwaysKept[key] || !bytes.Equal(c.prev[key], value) {
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
	for key, value := range e.prev {
		if _, ok := cur[key]; !ok {
			cur[key] = value
		}
	}
	e.prev = cur
	return json.Marshal(cur)
}
