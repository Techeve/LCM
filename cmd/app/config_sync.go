package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/netfilter"
	"LCM/internal/storage/repositories"
)

// Die config.json und die Einstellungen in der Datenbank führen die Werte aus
// config.Mirror gemeinsam (Plan Adressmodus, Abschnitt 2):
//
//   - Speichern in der Oberfläche schreibt sie in die Datei (Sync).
//   - Beim Start übernimmt LCM, was jemand in der Datei von Hand geändert hat
//     (adoptFile); alles übrige gilt die Datenbank, und die Datei folgt ihr.
//
// Die Datenbank hält in config_synced fest, was die Datei zuletzt nachweislich
// enthielt. Nur daran ist eine Handänderung von einer Datei zu unterscheiden, die
// bloß hinterherhinkt (ein Schreibfehler, ein zurückgespielter Stand).
// Let's Encrypt steht nicht in der Datei.

// configEditor ist der Name, unter dem übernommene Werte im Audit-Log stehen.
const configEditor = "config.json"

// envLocks sind die Schlüssel, die eine Umgebungsvariable festlegt: LCM kann
// sie nicht ändern, also bleiben sie beim Abgleich außen vor.
type envLocks struct{ port, agentPort, agentDisabled bool }

func envLocksOf(ac services.AddressConfig) envLocks {
	return envLocks{port: ac.UI.Set, agentPort: ac.Agent.Set, agentDisabled: ac.Agent.Set && ac.Agent.Value == 0}
}

func (l envLocks) skips(key string) bool {
	switch key {
	case "port":
		return l.port
	case "agent_port":
		return l.agentPort
	case "agent_enabled":
		return l.agentDisabled
	}
	return false
}

// configSync gleicht die config.json mit den Einstellungen ab.
type configSync struct {
	path     string
	repo     *repositories.SettingsRepository
	settings *services.SettingsService
	locks    envLocks
	mu       sync.Mutex // eine Abgleichung zur Zeit
}

func newConfigSync(path string, repo *repositories.SettingsRepository, settings *services.SettingsService, locks envLocks) *configSync {
	return &configSync{path: path, repo: repo, settings: settings, locks: locks}
}

// Start läuft beim Start von LCM: Handänderungen der Datei übernehmen, dann die
// Datei auf den Stand der Datenbank bringen.
func (c *configSync) Start() {
	failed := c.adoptFile()
	if err := c.write(failed); err != nil {
		slog.Warn("config.json konnte nicht mit den Einstellungen abgeglichen werden", "error", err)
	}
}

// Sync schreibt die gespiegelten Einstellungen in die Datei, nach jedem Speichern.
func (c *configSync) Sync() error { return c.write(nil) }

// mirrorOf bildet die gespiegelten Werte aus den Einstellungen. listenHost ist
// der Bind-Host, den die Datei in shared_tls_listen hat: Er bleibt, auch wenn
// sich der Port ändert.
func mirrorOf(st *domain.GlobalSettings, listenHost string) config.Mirror {
	m := config.Mirror{
		AddressMode:      st.Mode(),
		PublicHost:       st.PublicHost,
		PublicBaseURL:    st.PublicBaseURL,
		PublicAgentURL:   st.PublicAgentURL,
		PublicMCPURL:     st.PublicMCPURL,
		Port:             orDefault(st.UIPort, domain.DefaultUIPort),
		AgentPort:        orDefault(st.AgentPort, domain.DefaultAgentPort),
		MCPPort:          orDefault(st.MCPPort, domain.DefaultMCPPort),
		AgentEnabled:     st.AgentEnabled,
		MCPEnabled:       st.MCPEnabled,
		TrustProxyHeader: st.TrustProxyHeader,
		TrustedProxies:   st.TrustedProxies,
		AllowedIPs:       services.StoredNets(st, netfilter.EndpointUI),
		AgentAllowedIPs:  services.StoredNets(st, netfilter.EndpointAgent),
		MCPAllowedIPs:    services.StoredNets(st, netfilter.EndpointMCP),
	}
	if st.Mode() == domain.AddressModeSharedPort {
		port := orDefault(st.SharedTLSPort, domain.DefaultSharedTLSPort)
		m.SharedTLSListen = net.JoinHostPort(listenHost, strconv.Itoa(port))
	}
	return m
}

func orDefault(value, def int) int {
	if value == 0 {
		return def
	}
	return value
}

// listenHostOf ist der Bind-Host einer Listen-Adresse wie "127.0.0.1:8443";
// leer ohne Host oder bei einer unlesbaren Adresse.
func listenHostOf(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return host
}

// canonical liefert jeden Schlüssel als kompaktes JSON, zum Vergleichen.
// shared_tls_listen zählt nur mit seinem Port: Der Host davor ist Sache der
// Datei, nicht der Einstellungen.
func canonical(m config.Mirror) map[string]string {
	if m.SharedTLSListen != "" && listenPort(m.SharedTLSListen) > 0 {
		m.SharedTLSListen = ":" + strconv.Itoa(listenPort(m.SharedTLSListen))
	}
	out := map[string]string{}
	for key, value := range m.Values() {
		out[key] = string(value)
	}
	return out
}

// parseState liest den gespeicherten Stand der Datei; nil, wenn es noch keinen
// gibt (die Installation stammt von vor dem Abgleich).
func parseState(stored string) map[string]string {
	if stored == "" {
		return nil
	}
	var state map[string]string
	if err := json.Unmarshal([]byte(stored), &state); err != nil {
		return nil
	}
	return state
}

// ---- Beim Start: Handänderungen übernehmen ------------------------------------

// adoptFile übernimmt, was in der Datei von Hand geändert wurde, in die
// Einstellungen. Die Rückgabe nennt die Schlüssel, die sich nicht übernehmen
// ließen: Sie bleiben in der Datei, wie sie sind, und LCM meldet sie bei jedem
// Start erneut.
func (c *configSync) adoptFile() map[string]bool {
	st, err := c.repo.Get()
	if err != nil {
		slog.Warn("config.json: settings not readable, nothing adopted", "error", err)
		return nil
	}
	file, present, err := config.ReadMirror(c.path)
	if err != nil {
		slog.Warn("config.json: not readable, nothing adopted", "error", err)
		return nil
	}
	db := mirrorOf(st, listenHostOf(file.SharedTLSListen))
	file, present = legacyAgentPort(file, present, db)
	edited := c.editedKeys(canonical(file), canonical(db), present, parseState(st.ConfigSynced))
	if len(edited) == 0 {
		return nil
	}
	return c.adopt(edited, file, st)
}

// legacyAgentPort liest ein altes "agent_port": 0 als "Agent aus": Der Port der
// Datenbank bleibt, agent_enabled wird false - es sei denn, die Datei sagt es
// selbst, oder der Agent läuft am gemeinsamen Port (dort hieß 0 nur: kein eigener
// Port daneben).
func legacyAgentPort(file config.Mirror, present map[string]bool, db config.Mirror) (config.Mirror, map[string]bool) {
	if !present["agent_port"] || file.AgentPort != 0 {
		return file, present
	}
	file.AgentPort = db.AgentPort
	sharedPort := file.SharedTLSListen != "" || file.AddressMode == domain.AddressModeSharedPort
	if !present["agent_enabled"] && !sharedPort {
		file.AgentEnabled = false
		present["agent_enabled"] = true
	}
	return file, present
}

// editedKeys nennt die Schlüssel, die jemand in der Datei geändert hat: Sie
// stehen in der Datei, weichen von der Datenbank ab und weichen vom Stand ab,
// den die Datei zuletzt hatte. Ein fehlender Schlüssel gilt nicht als Änderung,
// die Datenbank füllt ihn.
func (c *configSync) editedKeys(file, db map[string]string, present map[string]bool, last map[string]string) []string {
	var edited []string
	for _, key := range config.MirrorKeys {
		if !present[key] || c.locks.skips(key) || file[key] == db[key] {
			continue
		}
		if last == nil && isLegacyDefault(key, file[key]) {
			// Eine Datei von vor dem Abgleich: LCM schrieb diese Vorgaben selbst
			// hinein, ein Mensch hat sie nie gesetzt. Es gilt die Datenbank.
			continue
		}
		if last != nil && last[key] == file[key] {
			continue // die Datei hinkt nur hinterher, die Datenbank gilt
		}
		edited = append(edited, key)
	}
	return edited
}

// isLegacyDefault: der Wert, den ältere Fassungen von LCM ungefragt in die Datei
// schrieben, wenn niemand ihn gesetzt hatte.
func isLegacyDefault(key, value string) bool {
	switch key {
	case "port":
		return value == strconv.Itoa(domain.DefaultUIPort)
	case "agent_port":
		return value == strconv.Itoa(domain.DefaultAgentPort)
	case "shared_tls_listen":
		return value == `""`
	case "trust_proxy_header":
		return value == "false"
	case "trusted_proxies", "allowed_ips", "agent_allowed_ips", "mcp_allowed_ips":
		return value == "[]"
	}
	return false
}

// adopt übernimmt die geänderten Schlüssel. Die Einstellungen gehen zuerst als
// ein PATCH hinein, mit denselben Prüfungen wie über die Oberfläche; scheitert
// er, einzeln, damit ein Tippfehler die übrigen Werte nicht blockiert.
func (c *configSync) adopt(edited []string, file config.Mirror, st *domain.GlobalSettings) map[string]bool {
	failed := map[string]bool{}
	editedSet := map[string]bool{}
	for _, key := range edited {
		editedSet[key] = true
	}
	var settingKeys []string
	for _, key := range edited {
		if ep, ok := networkEndpointOf(key); ok {
			c.adoptNetworks(key, ep, file, failed)
			continue
		}
		settingKeys = append(settingKeys, key)
	}
	if len(settingKeys) == 0 {
		return failed
	}
	var batch services.GlobalSettingsInput
	for _, key := range settingKeys {
		fileInput(key, file, st, editedSet, &batch)
	}
	if _, err := c.settings.UpdateGlobal(batch, configEditor); err == nil {
		c.logAdopted(settingKeys, file)
		return failed
	}
	for _, key := range settingKeys {
		var one services.GlobalSettingsInput
		fileInput(key, file, st, editedSet, &one)
		if _, err := c.settings.UpdateGlobal(one, configEditor); err != nil {
			slog.Warn("config.json: value not adopted, the database value applies", "key", key, "error", err)
			failed[key] = true
			continue
		}
		c.logAdopted([]string{key}, file)
	}
	return failed
}

func (c *configSync) logAdopted(keys []string, file config.Mirror) {
	values := canonical(file)
	for _, key := range keys {
		slog.Info("config.json: value changed by hand, adopted into the settings", "key", key, "value", values[key])
	}
}

// fileInput trägt den Wert des Schlüssels aus der Datei in die PATCH-Eingabe ein.
func fileInput(key string, file config.Mirror, st *domain.GlobalSettings, edited map[string]bool, in *services.GlobalSettingsInput) {
	switch key {
	case "address_mode":
		in.AddressMode = ptrTo(file.AddressMode)
	case "public_host":
		in.PublicHost = ptrTo(file.PublicHost)
	case "public_base_url":
		in.PublicBaseURL = ptrTo(file.PublicBaseURL)
	case "public_agent_url":
		in.PublicAgentURL = ptrTo(file.PublicAgentURL)
	case "public_mcp_url":
		in.PublicMCPURL = ptrTo(file.PublicMCPURL)
	case "port":
		in.UIPort = ptrTo(file.Port)
	case "agent_port":
		in.AgentPort = ptrTo(file.AgentPort)
	case "mcp_port":
		in.MCPPort = ptrTo(file.MCPPort)
	case "shared_tls_listen":
		sharedListenInput(file.SharedTLSListen, st, edited["address_mode"], in)
	case "agent_enabled":
		in.AgentEnabled = ptrTo(file.AgentEnabled)
	case "mcp_enabled":
		in.MCPEnabled = ptrTo(file.MCPEnabled)
	case "trusted_proxies":
		in.TrustedProxies = ptrTo(file.TrustedProxies)
	case "trust_proxy_header":
		in.TrustProxyHeader = ptrTo(file.TrustProxyHeader)
	}
}

// sharedListenInput übersetzt shared_tls_listen: eine Adresse heißt Modus
// "ein Port" mit ihrem Port, leer heißt kein gemeinsamer Port. Steht address_mode
// selbst unter den Änderungen, entscheidet er über den Modus.
func sharedListenInput(listen string, st *domain.GlobalSettings, modeEdited bool, in *services.GlobalSettingsInput) {
	switch {
	case listen != "":
		in.SharedTLSPort = ptrTo(listenPort(listen))
		if !modeEdited {
			in.AddressMode = ptrTo(domain.AddressModeSharedPort)
		}
	case !modeEdited && st.Mode() == domain.AddressModeSharedPort:
		in.AddressMode = ptrTo(domain.AddressModeSeparate)
	}
}

func ptrTo[T any](v T) *T { return &v }

// networkEndpointOf ordnet einen Schlüssel der Netzbereiche seinem Endpunkt zu.
func networkEndpointOf(key string) (netfilter.Endpoint, bool) {
	for _, ep := range netfilter.Endpoints {
		if ep.ConfigKey() == key {
			return ep, true
		}
	}
	return "", false
}

// adoptNetworks übernimmt die Netzliste eines Endpunkts, wenn sie gültig ist.
func (c *configSync) adoptNetworks(key string, ep netfilter.Endpoint, file config.Mirror, failed map[string]bool) {
	entries := map[netfilter.Endpoint][]string{
		netfilter.EndpointUI: file.AllowedIPs, netfilter.EndpointAgent: file.AgentAllowedIPs, netfilter.EndpointMCP: file.MCPAllowedIPs,
	}[ep]
	normalized, err := netfilter.Normalize(entries)
	if err == nil {
		err = c.repo.UpdateFields(map[string]any{services.NetworkColumn(ep): strings.Join(normalized, ",")})
	}
	if err != nil {
		slog.Warn("config.json: value not adopted, the database value applies", "key", key, "error", err)
		failed[key] = true
		return
	}
	slog.Info("config.json: value changed by hand, adopted into the settings", "key", key, "value", strings.Join(normalized, ","))
}

// ---- Nach dem Speichern: die Datei schreiben -----------------------------------

// write bringt die Datei auf den Stand der Einstellungen. keep nennt Schlüssel,
// die unberührt bleiben (eine Handänderung, die sich nicht übernehmen ließ).
// Nur Schlüssel, die fehlen oder abweichen, werden geschrieben; alle anderen
// Schlüssel der Datei bleiben, wie sie sind.
func (c *configSync) write(keep map[string]bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := c.repo.Get()
	if err != nil {
		return err
	}
	file, present, err := config.ReadMirror(c.path)
	if err != nil {
		return err
	}
	want := mirrorOf(st, listenHostOf(file.SharedTLSListen))
	wantCanon, fileCanon, raw := canonical(want), canonical(file), want.Values()
	set := map[string]json.RawMessage{}
	for _, key := range config.MirrorKeys {
		if c.locks.skips(key) || keep[key] {
			continue
		}
		if !present[key] || wantCanon[key] != fileCanon[key] {
			set[key] = raw[key]
		}
	}
	if err := config.WriteMirror(c.path, set); err != nil {
		return err
	}
	return c.saveState(st, wantCanon, keep)
}

// saveState hält fest, was die Datei jetzt enthält. Schlüssel, die unberührt
// blieben, behalten ihren bisherigen Eintrag.
func (c *configSync) saveState(st *domain.GlobalSettings, written map[string]string, keep map[string]bool) error {
	state := parseState(st.ConfigSynced)
	if state == nil {
		state = map[string]string{}
	}
	for _, key := range config.MirrorKeys {
		if !c.locks.skips(key) && !keep[key] {
			state[key] = written[key]
		}
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if string(data) == st.ConfigSynced {
		return nil
	}
	if err := c.repo.UpdateFields(map[string]any{"config_synced": string(data)}); err != nil {
		return fmt.Errorf("stand der config.json merken: %w", err)
	}
	return nil
}
