package synology

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
)

// Firewall und Sicherheitseinstellungen lesen. Das Setzen auf Klick steht
// in apply.go.
//
// Synology dokumentiert diese APIs nicht. Die Feldnamen stammen aus der
// DSM-Oberfläche und den Community-Bibliotheken (synology-api) und sind je
// DSM-Fassung verschieden geschrieben. Deshalb wird tolerant gelesen: Für
// jede Einstellung gibt es eine Liste bekannter Schreibweisen. Passt keine,
// gilt der Wert als unbekannt und die Rohantwort (bereinigt und gekürzt)
// wandert ins Job-Protokoll, damit sie sich am echten Gerät nachziehen lässt.

// Setting ist eine gelesene Einstellung.
type Setting struct {
	Known bool
	On    bool   // bei Schaltern: eingeschaltet
	Text  string // gelesener Wert im Klartext ("modern", Profilname …)
	// Reason sagt bei Known=false, warum: API fehlt, Abruf gescheitert oder
	// Antwort nicht deutbar. Raw ist dann die bereinigte Rohantwort.
	Reason string
	Raw    string
}

// Settings sind alle gelesenen Einstellungen eines Geräts.
type Settings struct {
	Firewall Setting // Text = aktives Profil
	// FirewallProfile sind die Regeln des aktiven Profils.
	FirewallProfile domain.DSMFirewall
	SkipIPCheck     Setting
	CSRFProtection  Setting
	CSP             Setting
	IframeBlocked   Setting
	SessionsCleared Setting // Sitzungen beim Neustart löschen
	AutoBlock       Setting
	// AutoBlockAttempts/-Minutes: Versuche innerhalb von Minuten (0 = nicht gelesen).
	AutoBlockAttempts int
	AutoBlockMinutes  int
	DoS               Setting
	// TLSProfile: Text = "modern", "intermediate" oder "old"; solange eine
	// Stufennummer noch keinem Namen zugeordnet ist, Known=false und Text
	// "Stufe <n>".
	TLSProfile      Setting
	SpectreMeltdown Setting
}

// Bekannte Schreibweisen je Einstellung. Verglichen wird ohne
// Groß-/Kleinschreibung, Unterstriche und Bindestriche.
var (
	keysFirewallOn      = []string{"enable_firewall", "firewall_enable", "enable", "enabled"}
	keysFirewallProfile = []string{"profile_name", "current_profile", "active_profile", "profile"}
	keysSkipIPCheck     = []string{"skip_ip_checking", "skip_ip_check", "enable_skip_ip_check", "skip_ipcheck", "allow_skip_ip_check"}
	keysIPCheck         = []string{"enable_ip_check", "ip_check", "check_ip"} // umgekehrte Bedeutung
	keysCSRF            = []string{"enable_csrf_protection", "csrf_protection", "enable_csrf"}
	keysCSP             = []string{"csp_header_option", "enable_csp_header", "csp_header", "enable_csp", "csp", "content_security_policy"}
	keysIframeBlocked   = []string{"prevent_iframe", "deny_iframe", "iframe_protect", "enable_x_frame", "x_frame_options", "xframe"}
	keysIframeAllowed   = []string{"allow_iframe", "allow_embed", "enable_embed"} // umgekehrte Bedeutung
	keysSessionsCleared = []string{"restart_clean_session", "restart_clear_session", "clear_session_on_restart", "restart_clear", "clean_session_on_reboot", "reboot_clear_session"}
	keysAutoBlockOn     = []string{"enable_autoblock", "autoblock_enable", "auto_block", "enable", "enabled"}
	keysAutoBlockTries  = []string{"attempts", "attempt", "login_attempts", "max_attempts", "max_fail"}
	keysAutoBlockMins   = []string{"within_mins", "within_min", "within_minutes", "within", "minutes"}
	keysDoS             = []string{"dos_protect_enable", "dos_protect", "enable_dos", "dos_enable", "dos_protection", "enable", "enabled"}
	keysTLSLevel        = []string{"default_level", "level", "tls_profile", "profile_level", "profile"}
	// keysEmbedBlocked gilt nur in der Antwort von SYNO.Core.Security.DSM.Embed;
	// in den übrigen Sicherheitseinstellungen hieße "enable_block" etwas anderes.
	keysEmbedBlocked = []string{"enable_block"}
	keysSpectre      = []string{"enable_spectre_meltdown_mitigation", "spectre_meltdown", "enable_spectre_meltdown", "mitigation", "enable", "enabled"}
)

// collectSettings liest Firewall und Sicherheitseinstellungen. Jede API für
// sich: Fehlt eine oder scheitert sie, bleiben die anderen gültig.
func (c *Client) collectSettings() Settings {
	var s Settings
	c.readFirewall(&s)
	dsm, dsmRaw := c.readDSMSecurity(&s)
	c.readAutoBlock(&s)
	s.DoS = c.readDoS()
	tls, raw := c.fetch(c.findAPI("SYNO.Core.Web.Security.TLSProfile", "tlsprofile"))
	s.TLSProfile = tlsLevel(tls, raw)
	spectre, raw := c.fetch(c.findAPI("SYNO.Core.Hardware.SpectreMeltdown", "spectre"))
	s.SpectreMeltdown = switchOf(spectre, raw, keysSpectre, nil)
	// Ohne eigene API steht der Schalter je nach Fassung bei den übrigen
	// Sicherheitseinstellungen - dort aber nur unter seinem eigenen Namen,
	// ein bloßes "enable" hieße dort etwas anderes.
	if spectre.reason != "" {
		if got := switchOf(dsm, dsmRaw, keysSpectre[:2], nil); got.Known {
			s.SpectreMeltdown = got
		}
	}
	return s
}

// fetched ist eine abgerufene, geparste Antwort oder der Grund, warum nicht.
type fetched struct {
	data   any
	reason string
	// missing: Das Gerät kennt die API nicht.
	missing bool
}

// fetch ruft die get-Methode einer API und liefert die geparste Antwort und
// ihre bereinigte Rohform fürs Protokoll.
func (c *Client) fetch(api string) (fetched, string) {
	return c.fetchWith(api, "", nil)
}

// fetchWith ist fetch mit Version und Parametern (siehe callWith).
func (c *Client) fetchWith(api, version string, params map[string]string) (fetched, string) {
	var raw json.RawMessage
	if err := c.callWith(api, "get", version, params, &raw); err != nil {
		return fetched{reason: api + ": " + err.Error(), missing: errors.Is(err, errAPIMissing)}, ""
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return fetched{reason: api + ": Antwort nicht lesbar"}, ""
	}
	return fetched{data: data}, api + " " + cleanRaw(data)
}

// readDSMSecurity liest Systemsteuerung → Sicherheit → Sicherheit. Das
// Einbetten in iFrames steht je nach Fassung dort oder in einer eigenen API.
func (c *Client) readDSMSecurity(s *Settings) (fetched, string) {
	dsm, raw := c.fetch("SYNO.Core.Security.DSM")
	s.SkipIPCheck = switchOf(dsm, raw, keysSkipIPCheck, keysIPCheck)
	s.CSRFProtection = switchOf(dsm, raw, keysCSRF, nil)
	s.CSP = switchOf(dsm, raw, keysCSP, nil)
	s.SessionsCleared = switchOf(dsm, raw, keysSessionsCleared, nil)
	s.IframeBlocked = switchOf(dsm, raw, keysIframeBlocked, keysIframeAllowed)
	if !s.IframeBlocked.Known {
		embed, embedRaw := c.fetch("SYNO.Core.Security.DSM.Embed")
		blocked := append(append([]string{}, keysEmbedBlocked...), keysIframeBlocked...)
		if got := switchOf(embed, embedRaw, blocked, keysIframeAllowed); got.Known || embed.reason == "" {
			s.IframeBlocked = got
		}
	}
	return dsm, raw
}

func (c *Client) readAutoBlock(s *Settings) {
	ab, raw := c.fetch("SYNO.Core.Security.AutoBlock")
	s.AutoBlock = switchOf(ab, raw, keysAutoBlockOn, nil)
	if v, ok := lookup(ab.data, keysAutoBlockTries); ok {
		s.AutoBlockAttempts, _ = asInt(v)
	}
	if v, ok := lookup(ab.data, keysAutoBlockMins); ok {
		s.AutoBlockMinutes, _ = asInt(v)
	}
}

// dosAttempts sind die Aufrufformen für den DoS-Schutz. DSM 7.3 lehnt get
// ohne Parameter mit Code 114 ab und will die Netzwerkschnittstellen als
// "configs" (an Tonys VirtualDSM am 09.11.2026 bestätigt). Abgefragt werden
// die üblichen Namen auf einmal; kennt das Gerät einen davon nicht, bleibt
// eth0 allein. Ältere Fassungen antworten auf get ohne Parameter.
var dosAttempts = []struct {
	label  string
	params map[string]string
}{
	{"get", nil},
	{"get configs=[eth0..eth3,bond0,ovs_eth0]", map[string]string{"configs": `[{"adapter":"eth0"},{"adapter":"eth1"},{"adapter":"eth2"},{"adapter":"eth3"},{"adapter":"bond0"},{"adapter":"ovs_eth0"}]`}},
	{"get configs=[eth0]", map[string]string{"configs": `[{"adapter":"eth0"}]`}},
}

// readDoS liest den DoS-Schutz mit der ersten Aufrufform, die DSM annimmt.
func (c *Client) readDoS() Setting {
	const api = "SYNO.Core.Security.DoS"
	var failures []string
	for _, a := range dosAttempts {
		f, raw := c.fetchWith(api, "", a.params)
		if f.missing {
			return Setting{Reason: f.reason}
		}
		if f.reason != "" {
			failures = append(failures, a.label+": "+f.reason)
			continue
		}
		return anySwitch(f, a.label+": "+raw, keysDoS)
	}
	return Setting{Reason: strings.Join(failures, "; ")}
}

// switchOf deutet einen Schalter: erst die Schreibweisen mit gleicher
// Bedeutung, dann die umgekehrten.
func switchOf(f fetched, raw string, keys, inverted []string) Setting {
	if f.reason != "" {
		return Setting{Reason: f.reason}
	}
	if v, ok := lookup(f.data, keys); ok {
		if on, ok := asBool(v); ok {
			return Setting{Known: true, On: on}
		}
	}
	if v, ok := lookup(f.data, inverted); ok {
		if on, ok := asBool(v); ok {
			return Setting{Known: true, On: !on}
		}
	}
	return Setting{Reason: "Wert nicht deutbar", Raw: raw}
}

// anySwitch gilt als an, wenn irgendeine Fundstelle an ist - der DoS-Schutz
// wird je Netzwerkschnittstelle geschaltet.
func anySwitch(f fetched, raw string, keys []string) Setting {
	if f.reason != "" {
		return Setting{Reason: f.reason}
	}
	found := false
	for _, v := range lookupAll(f.data, keys) {
		on, ok := asBool(v)
		if !ok {
			continue
		}
		found = true
		if on {
			return Setting{Known: true, On: true}
		}
	}
	if found {
		return Setting{Known: true}
	}
	return Setting{Reason: "Wert nicht deutbar", Raw: raw}
}

// tlsLevelNames ordnet die Stufennummern von SYNO.Core.Web.Security.TLSProfile
// den Profilstufen zu. DSM 7.3 liefert nur Zahlen, und welche Zahl welche
// Stufe ist, dokumentiert Synology nicht. Eine Nummer ohne Eintrag bleibt
// unbekannt und nennt ihre Nummer.
//
// 1 = modern: Tonys VirtualDSM mit DSM 7.3.2 steht auf „Moderne
// Kompatibilität“ und meldet "default-level": 1 (09.11.2026).
// 3 = old: dasselbe Gerät auf „Alte Kompatibilität“ meldet 3 (10.11.2026).
// 2 = intermediate ist nur erschlossen, nicht an einem Gerät abgelesen: DSM
// kennt genau drei Stufen, und 2 liegt zwischen den beiden abgelesenen.
var tlsLevelNames = map[int]string{tlsModernLevel: "modern", 2: "intermediate", 3: "old"}

const tlsModernLevel = 1

// tlsLevel deutet die Profilstufe: als Klartext (ältere Fassungen) oder als
// Stufennummer.
func tlsLevel(f fetched, raw string) Setting {
	if f.reason != "" {
		return Setting{Reason: f.reason}
	}
	if v, ok := lookup(f.data, keysTLSLevel); ok {
		if text, isText := v.(string); isText {
			return tlsLevelText(text, raw)
		}
	}
	n, ok := tlsLevelNumber(f.data)
	if !ok {
		return Setting{Reason: "Wert nicht deutbar", Raw: raw}
	}
	if name, known := tlsLevelNames[n]; known {
		return Setting{Known: true, On: name == "modern", Text: name}
	}
	level := "Stufe " + strconv.Itoa(n)
	return Setting{Reason: level + " ist noch keiner Profilstufe zugeordnet", Text: level, Raw: raw}
}

func tlsLevelText(text, raw string) Setting {
	low := strings.ToLower(text)
	for _, level := range []string{"modern", "intermediate", "old"} {
		if strings.Contains(low, level) {
			return Setting{Known: true, On: level == "modern", Text: level}
		}
	}
	return Setting{Reason: "Wert nicht deutbar", Raw: raw}
}

// tlsLevelNumber liest die Stufe, die für die DSM-Oberfläche gilt. Jeder Dienst
// hat eine eigene ("services" → "dsm" → "current-level"); 0 heißt dort
// „Standardprofil“, dann gilt "default-level".
func tlsLevelNumber(data any) (int, bool) {
	top, ok := data.(map[string]any)
	if !ok {
		return 0, false
	}
	if services, ok := top["services"].(map[string]any); ok {
		if dsm, ok := services["dsm"].(map[string]any); ok {
			if n, ok := asInt(dsm["current-level"]); ok && n > 0 {
				return n, true
			}
		}
	}
	return asInt(top["default-level"])
}

// normKey macht Schreibweisen vergleichbar: "enable-csp_Header" → "enablecspheader".
func normKey(k string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(k))
}

// lookup sucht den ersten Treffer, Schreibweisen in der Reihenfolge der
// Liste, die oberste Ebene vor verschachtelten - so gewinnt ein "enable" der
// Antwort selbst vor dem "enable" einer einzelnen Regel darin.
func lookup(data any, keys []string) (any, bool) {
	for _, key := range keys {
		if all := findKey(data, normKey(key), true); len(all) > 0 {
			return all[0], true
		}
	}
	return nil, false
}

// lookupAll liefert alle Fundstellen aller Schreibweisen.
func lookupAll(data any, keys []string) []any {
	var out []any
	for _, key := range keys {
		out = append(out, findKey(data, normKey(key), false)...)
	}
	return out
}

// findKey durchsucht die Antwort Ebene für Ebene (Breitensuche).
func findKey(data any, key string, first bool) []any {
	var out []any
	level := []any{data}
	for len(level) > 0 {
		var next []any
		for _, node := range level {
			switch n := node.(type) {
			case map[string]any:
				for k, v := range n {
					if normKey(k) == key {
						out = append(out, v)
					} else {
						next = append(next, v)
					}
				}
			case []any:
				next = append(next, n...)
			}
		}
		if first && len(out) > 0 {
			return out
		}
		level = next
	}
	return out
}

// asBool deutet true/false, 0/1 und "yes"/"no"/"on"/"off".
func asBool(v any) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	case float64:
		if b == 0 || b == 1 {
			return b == 1, true
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true", "yes", "on", "1", "enable", "enabled":
			return true, true
		case "false", "no", "off", "0", "disable", "disabled":
			return false, true
		}
	}
	return false, false
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	}
	return 0, false
}

func asText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// rawLimit begrenzt die Rohantwort im Protokoll.
const rawLimit = 600

// secretMarkers kennzeichnen Felder, deren Werte nie ins Protokoll gehören.
var secretMarkers = []string{"pass", "secret", "token", "sid", "key", "cert", "otp", "private", "cookie"}

// cleanRaw macht aus einer Antwort die Protokollform: geheime Werte
// geschwärzt, auf rawLimit Zeichen gekürzt.
func cleanRaw(data any) string {
	b, err := json.Marshal(redact(data))
	if err != nil {
		return ""
	}
	if len(b) > rawLimit {
		return string(b[:rawLimit]) + " …(gekürzt)"
	}
	return string(b)
}

func redact(data any) any {
	switch n := data.(type) {
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, v := range n {
			if isSecretKey(k) {
				out[k] = "***"
				continue
			}
			out[k] = redact(v)
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			out[i] = redact(v)
		}
		return out
	}
	return data
}

func isSecretKey(k string) bool {
	low := strings.ToLower(k)
	for _, m := range secretMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}
