package synology

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// errAPIMissing: das Gerät führt die API nicht in seinem Verzeichnis - etwa
// eine ältere DSM-Fassung oder ein Modell ohne die Funktion.
var errAPIMissing = errors.New("API auf diesem Gerät nicht vorhanden")

// apiInfo ist ein Eintrag aus SYNO.API.Info.
type apiInfo struct {
	MinVersion int    `json:"minVersion"`
	MaxVersion int    `json:"maxVersion"`
	Path       string `json:"path"`
}

// discoverAPIs liest das API-Verzeichnis des Geräts. Die Sicherheits-APIs
// sind von Synology nicht dokumentiert und wechseln Name und Version mit der
// DSM-Fassung - das Verzeichnis sagt, was es auf DIESEM Gerät gibt. Scheitert
// die Abfrage, bleibt apis nil und callLatest versucht es mit Version 1.
func (c *Client) discoverAPIs() {
	var apis map[string]apiInfo
	err := c.callAt("query.cgi", "SYNO.API.Info", "1", "query", map[string]string{"query": "all"}, &apis)
	if err != nil || len(apis) == 0 {
		return
	}
	c.apis = apis
}

// callLatest ruft eine API in der höchsten Version auf, die das Gerät
// anbietet (wie die DSM-Oberfläche selbst), über den Pfad aus dem Verzeichnis.
func (c *Client) callLatest(api, method string, out any) error {
	return c.callWith(api, method, "", nil, out)
}

// callWith ruft eine API mit Parametern auf, in der angegebenen Version oder
// (leer) in der höchsten, die das Gerät anbietet.
func (c *Client) callWith(api, method, version string, params map[string]string, out any) error {
	latest, path, err := c.endpoint(api)
	if err != nil {
		return err
	}
	if version == "" {
		version = latest
	}
	return c.callAt(path, api, version, method, params, out)
}

// endpoint liefert höchste Version und CGI-Pfad einer API laut Verzeichnis.
func (c *Client) endpoint(api string) (version, path string, err error) {
	version, path = "1", "entry.cgi"
	if c.apis == nil {
		return version, path, nil
	}
	info, ok := c.apis[api]
	if !ok {
		return "", "", errAPIMissing
	}
	if info.MaxVersion > 0 {
		version = strconv.Itoa(info.MaxVersion)
	}
	if info.Path != "" {
		path = info.Path
	}
	return version, path, nil
}

// findAPI sucht im Verzeichnis die erste API, deren Name alle Bestandteile
// enthält (ohne Groß-/Kleinschreibung). Für Einstellungen, deren API-Name je
// DSM-Fassung anders lautet (TLS-Profil, Spectre/Meltdown).
func (c *Client) findAPI(fallback string, parts ...string) string {
	if _, ok := c.apis[fallback]; ok || c.apis == nil {
		return fallback
	}
	for name := range c.apis {
		if containsAll(strings.ToLower(name), parts) {
			return name
		}
	}
	return fallback
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, strings.ToLower(p)) {
			return false
		}
	}
	return true
}

// flexInt liest Zahlen, die DSM mal als Zahl, mal als Zeichenkette liefert
// ("cpu_cores":"2", "size":{"total":"129876824064"}, "percent":"-1").
// Unlesbares ergibt 0 statt eines Fehlers für die ganze Antwort.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		if fl, ferr := strconv.ParseFloat(s, 64); ferr == nil {
			n = int64(fl)
		}
	}
	*f = flexInt(n)
	return nil
}

// flexBool liest Wahrheitswerte als true/false, 0/1 oder "yes"/"no".
// Fehlt das Feld, bleibt Set false - VirtualDSM meldet manche Felder gar nicht.
type flexBool struct {
	Set   bool
	Value bool
}

func (f *flexBool) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil
	}
	f.Value, f.Set = asBool(v)
	return nil
}
