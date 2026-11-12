package synology

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sollwerte setzen, nur auf ausdrücklichen Klick.
//
// Die DSM-Oberfläche ändert diese Einstellungen über dieselben APIs mit
// method=set und denselben Feldnamen wie get. Was set genau erwartet,
// dokumentiert Synology nicht. Deshalb gilt Lesen-Ändern-Zurückschreiben:
// Das gelesene Objekt geht vollständig zurück, nur die Zielfelder tragen
// einen neuen Wert. So fällt nichts Unbeteiligtes auf eine Vorgabe zurück.
// Jedes Feld ist ein eigener Formularparameter, sein Wert JSON-kodiert
// (requestFormat JSON: true/false, Zahlen bloß, Texte in Anführungszeichen).
//
// Der set-Aufruf geht immer raus, auch wenn der Wert schon stimmt. So lässt
// sich das Aufrufformat an einem Gerät prüfen, ohne etwas zu ändern.

// Sollwerte der automatischen Blockierung: höchstens so viele Fehlversuche,
// mindestens so lang das Zeitfenster.
const (
	AutoBlockMaxAttempts = 10
	AutoBlockMinMinutes  = 5
)

// errFieldMissing: Das Zielfeld steht nicht in der Antwort. Dann wird nichts
// gesendet - ein erfundener Feldname wäre geraten.
var errFieldMissing = errors.New("Feld nicht in der Antwort, nichts gesendet")

// reservedParams sind die Parameter des Aufrufs selbst. Ein gleichnamiges
// Feld würde sie im Formular überschreiben.
var reservedParams = map[string]bool{"api": true, "version": true, "method": true, "_sid": true}

// targets setzen je Prüfung den Sollwert. Die Schlüssel sind die der
// Prüfungen (domain.DSMCheck*). Die Firewall fehlt bewusst: Mit unpassenden
// Regeln eingeschaltet sperrt sie LCM und den Admin aus.
var targets = map[string]func(*Client) (string, error){
	"skipIpCheck":  func(c *Client) (string, error) { return c.setSwitch(apiSecurityDSM, keysSkipIPCheck, false) },
	"csrf":         func(c *Client) (string, error) { return c.setSwitch(apiSecurityDSM, keysCSRF, true) },
	"csp":          func(c *Client) (string, error) { return c.setSwitch(apiSecurityDSM, keysCSP, true) },
	"sessionClear": func(c *Client) (string, error) { return c.setSwitch(apiSecurityDSM, keysSessionsCleared, true) },
	"iframe":       (*Client).setIframeBlocked,
	"autoBlock":    (*Client).setAutoBlock,
	"dos":          (*Client).setDoS,
	"tlsProfile":   (*Client).setTLSModern,
	"spectre":      (*Client).setSpectre,
}

const apiSecurityDSM = "SYNO.Core.Security.DSM"

// HasTarget meldet, ob LCM die Prüfung auf ihren Sollwert setzen kann.
func HasTarget(key string) bool {
	_, ok := targets[key]
	return ok
}

// ApplyTarget setzt eine Einstellung auf ihren Sollwert. Die Notiz fürs
// Job-Protokoll sagt, was gesendet wurde.
func (c *Client) ApplyTarget(key string) (string, error) {
	set, ok := targets[key]
	if !ok {
		return "", fmt.Errorf("%s: kein Sollwert setzbar", key)
	}
	if c.apis == nil {
		c.discoverAPIs()
	}
	return set(c)
}

// change ist ein geändertes Feld fürs Protokoll.
type change struct {
	field         string
	before, after any
}

// readModifyWrite liest das Objekt einer API, lässt edit die Zielfelder
// ändern und schickt alle Felder mit method=set zurück. Geliefert werden
// die Notiz und die Antwort von DSM (oft leer).
func (c *Client) readModifyWrite(api string, edit func(map[string]any) ([]change, error)) (string, any, error) {
	obj, err := c.readObject(api)
	if err != nil {
		return "", nil, err
	}
	changes, err := edit(obj)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", api, err)
	}
	params, err := encodeParams(obj)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", api, err)
	}
	answer, err := c.set(api, params)
	if err != nil {
		return "", nil, err
	}
	return applyNote(api, changes, params, answer), answer, nil
}

// readObject liest die get-Antwort als Objekt. Zahlen bleiben json.Number,
// damit sie genau so zurückgehen, wie DSM sie geschickt hat.
func (c *Client) readObject(api string) (map[string]any, error) {
	var raw json.RawMessage
	if err := c.callWith(api, "get", "", nil, &raw); err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := decodeNumbers(raw, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("%s: Antwort ist kein Objekt, nichts gesendet", api)
	}
	return obj, nil
}

func decodeNumbers(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

// encodeParams kodiert jedes Feld als eigenen Formularparameter.
func encodeParams(obj map[string]any) (map[string]string, error) {
	params := make(map[string]string, len(obj))
	for k, v := range obj {
		if reservedParams[k] {
			return nil, fmt.Errorf("Feld %q kollidiert mit dem Aufruf, nichts gesendet", k)
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("Feld %q nicht kodierbar: %w", k, err)
		}
		params[k] = string(b)
	}
	return params, nil
}

// set schickt die Parameter mit method=set in der höchsten Version.
func (c *Client) set(api string, params map[string]string) (any, error) {
	var raw json.RawMessage
	if err := c.callWith(api, "set", "", params, &raw); err != nil {
		return nil, err
	}
	var answer any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &answer)
	}
	return answer, nil
}

// setField setzt das erste vorhandene Feld aus keys auf der obersten Ebene.
func setField(obj map[string]any, keys []string, value any) (change, bool) {
	name, ok := topField(obj, keys)
	if !ok {
		return change{}, false
	}
	ch := change{field: name, before: obj[name], after: value}
	obj[name] = value
	return ch, true
}

// topField sucht den Feldnamen, wie DSM ihn schreibt, zu den Schreibweisen.
func topField(obj map[string]any, keys []string) (string, bool) {
	for _, key := range keys {
		for name := range obj {
			if normKey(name) == normKey(key) {
				return name, true
			}
		}
	}
	return "", false
}

// setSwitch setzt einen Schalter auf want.
func (c *Client) setSwitch(api string, keys []string, want bool) (string, error) {
	note, _, err := c.readModifyWrite(api, func(obj map[string]any) ([]change, error) {
		ch, ok := setField(obj, keys, want)
		if !ok {
			return nil, errFieldMissing
		}
		return []change{ch}, nil
	})
	return note, err
}

// setIframeBlocked schaltet dort, wo das Lesen den Wert fand: in den
// Sicherheitseinstellungen oder, wie bei DSM 7.3, in einer eigenen API. Die
// Liste erlaubter Seiten (whitelist) geht unverändert mit.
func (c *Client) setIframeBlocked() (string, error) {
	note, err := c.setSwitch(apiSecurityDSM, keysIframeBlocked, true)
	if !errors.Is(err, errFieldMissing) {
		return note, err
	}
	return c.setSwitch("SYNO.Core.Security.DSM.Embed", keysEmbedBlocked, true)
}

// setAutoBlock schaltet ein und zieht die Grenzwerte auf das Soll: höchstens
// 10 Versuche (auch bei 0), mindestens 5 Minuten. Strengere Werte bleiben.
func (c *Client) setAutoBlock() (string, error) {
	note, _, err := c.readModifyWrite("SYNO.Core.Security.AutoBlock", func(obj map[string]any) ([]change, error) {
		on, ok := setField(obj, keysAutoBlockOn, true)
		if !ok {
			return nil, errFieldMissing
		}
		changes := []change{on}
		if ch, ok := adjustInt(obj, keysAutoBlockTries, capAttempts); ok {
			changes = append(changes, ch)
		}
		if ch, ok := adjustInt(obj, keysAutoBlockMins, func(n int) int { return max(n, AutoBlockMinMinutes) }); ok {
			changes = append(changes, ch)
		}
		return changes, nil
	})
	return note, err
}

func capAttempts(n int) int {
	if n <= 0 || n > AutoBlockMaxAttempts {
		return AutoBlockMaxAttempts
	}
	return n
}

// adjustInt rechnet eine Zahl um, wenn das Feld da und lesbar ist.
func adjustInt(obj map[string]any, keys []string, adjust func(int) int) (change, bool) {
	name, ok := topField(obj, keys)
	if !ok {
		return change{}, false
	}
	n, ok := asInt(obj[name])
	if !ok {
		return change{}, false
	}
	return setField(obj, []string{name}, adjust(n))
}

// setDoS schaltet den Schutz an allen gelesenen Netzwerkschnittstellen ein.
// DSM 7.3 nimmt die Schnittstellen als JSON-Liste im Parameter configs.
func (c *Client) setDoS() (string, error) {
	const api = "SYNO.Core.Security.DoS"
	adapters, err := c.readDoSAdapters()
	if err != nil {
		return "", err
	}
	var changes []change
	for _, a := range adapters {
		ch, ok := setField(a, keysDoS[:1], true)
		if !ok {
			return "", fmt.Errorf("%s: %w", api, errFieldMissing)
		}
		ch.field = fmt.Sprint(a["adapter"]) + "." + ch.field
		changes = append(changes, ch)
	}
	configs, err := json.Marshal(adapters)
	if err != nil {
		return "", fmt.Errorf("%s: configs nicht kodierbar: %w", api, err)
	}
	params := map[string]string{"configs": string(configs)}
	answer, err := c.set(api, params)
	if err != nil {
		return "", err
	}
	return applyNote(api, changes, params, answer), nil
}

// readDoSAdapters liest die Schnittstellen mit den Aufrufformen des Lesens.
func (c *Client) readDoSAdapters() ([]map[string]any, error) {
	const api = "SYNO.Core.Security.DoS"
	var failures []string
	for _, a := range dosAttempts {
		var raw json.RawMessage
		err := c.callWith(api, "get", "", a.params, &raw)
		if errors.Is(err, errAPIMissing) {
			return nil, fmt.Errorf("%s: %w", api, err)
		}
		if err != nil {
			failures = append(failures, a.label+": "+err.Error())
			continue
		}
		var adapters []map[string]any
		if decodeNumbers(raw, &adapters) != nil || len(adapters) == 0 || adapters[0]["adapter"] == nil {
			failures = append(failures, a.label+": keine Liste von Schnittstellen")
			continue
		}
		return adapters, nil
	}
	return nil, fmt.Errorf("%s: nichts gesendet - %s", api, strings.Join(failures, "; "))
}

// setTLSModern stellt die Standardstufe auf „moderne Kompatibilität“.
//
// Annahme: set nimmt dieselbe Form wie get. "services" geht deshalb so
// zurück, wie es gelesen wurde, samt der Anzeigenamen je Dienst. Hat der
// Dienst dsm eine eigene Stufe, bleibt sie; das Protokoll sagt es dann.
func (c *Client) setTLSModern() (string, error) {
	api := c.findAPI("SYNO.Core.Web.Security.TLSProfile", "tlsprofile")
	own := 0
	note, _, err := c.readModifyWrite(api, func(obj map[string]any) ([]change, error) {
		ch, ok := setField(obj, []string{"default_level"}, tlsModernLevel)
		if !ok {
			return nil, errFieldMissing
		}
		own = ownDSMLevel(obj)
		return []change{ch}, nil
	})
	if err == nil && own > 0 && own != tlsModernLevel {
		note += fmt.Sprintf("; der Dienst dsm hat die eigene Stufe %d, sie bleibt", own)
	}
	return note, err
}

// ownDSMLevel ist die eigene Stufe des Dienstes dsm (0 = folgt der Standardstufe).
func ownDSMLevel(obj map[string]any) int {
	services, _ := obj["services"].(map[string]any)
	dsm, _ := services["dsm"].(map[string]any)
	n, _ := asInt(dsm["current-level"])
	return n
}

// setSpectre schaltet den Schutz ein. Er wirkt erst nach einem Neustart.
// Ohne eigene API steht der Schalter bei den übrigen Sicherheitseinstellungen
// (wie beim Lesen, nur unter seinem eigenen Namen).
func (c *Client) setSpectre() (string, error) {
	api := c.findAPI("SYNO.Core.Hardware.SpectreMeltdown", "spectre")
	note, answer, err := c.readModifyWrite(api, func(obj map[string]any) ([]change, error) {
		ch, ok := setField(obj, keysSpectre, true)
		if !ok {
			return nil, errFieldMissing
		}
		return []change{ch}, nil
	})
	if errors.Is(err, errAPIMissing) {
		note, answer, err = c.readModifyWrite(apiSecurityDSM, func(obj map[string]any) ([]change, error) {
			ch, ok := setField(obj, keysSpectre[:2], true)
			if !ok {
				return nil, errFieldMissing
			}
			return []change{ch}, nil
		})
	}
	if err != nil {
		return "", err
	}
	note += "; wirkt erst nach einem Neustart des Geräts"
	if rebootRequested(answer) {
		note += " (DSM meldet: Neustart nötig)"
	}
	return note, nil
}

// rebootRequested sucht in der set-Antwort einen eingeschalteten Hinweis auf
// einen Neustart ("need_reboot", "reboot_required" …).
func rebootRequested(answer any) bool {
	obj, ok := answer.(map[string]any)
	if !ok {
		return false
	}
	for k, v := range obj {
		if on, ok := asBool(v); ok && on && strings.Contains(strings.ToLower(k), "reboot") {
			return true
		}
	}
	return false
}

// applyNote beschreibt einen set-Aufruf: die Zielfelder mit altem und neuem
// Wert, dann alle gesendeten Parameter (Geheimes geschwärzt) und die Antwort.
func applyNote(api string, changes []change, params map[string]string, answer any) string {
	var b strings.Builder
	b.WriteString(api + " set:")
	for _, ch := range changes {
		fmt.Fprintf(&b, " %s %s → %s;", ch.field, jsonText(ch.before), jsonText(ch.after))
	}
	b.WriteString(" gesendet:")
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		v := params[k]
		if isSecretKey(k) {
			v = "***"
		}
		fmt.Fprintf(&b, " %s=%s", k, v)
	}
	if answer != nil {
		b.WriteString("; Antwort: " + cleanRaw(answer))
	}
	return b.String()
}

func jsonText(v any) string {
	if v == nil {
		return "(fehlte)"
	}
	b, _ := json.Marshal(v)
	return string(b)
}
