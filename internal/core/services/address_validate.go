package services

import (
	"errors"
	"fmt"

	"LCM/internal/core/domain"
)

// ErrPortUnavailable: ein neuer Port lässt sich nicht öffnen (belegt, keine
// Berechtigung). Der bisherige Port bleibt in Betrieb und nichts wird gespeichert.
var ErrPortUnavailable = fmt.Errorf("%w: port nicht verfügbar", ErrSettingInvalid)

// addressFields sind die Felder, deren Änderung die Adressprüfung auslöst.
// Ein älterer Stand soll das Speichern anderer Einstellungen nicht blockieren.
type addressFields struct {
	mode, host         string
	ui, agent, mcp, sp int
	urls               [3]string
}

func addressFieldsOf(st *domain.GlobalSettings) addressFields {
	return addressFields{
		mode: st.AddressMode, host: st.PublicHost,
		ui: st.UIPort, agent: st.AgentPort, mcp: st.MCPPort, sp: st.SharedTLSPort,
		urls: [3]string{st.PublicBaseURL, st.PublicAgentURL, st.PublicMCPURL},
	}
}

// validateAddressing prüft den Endzustand der Adressen: je nach Modus die
// Namen, in jedem Modus die Ports.
func (s *SettingsService) validateAddressing(st *domain.GlobalSettings) error {
	a := s.AddressesOf(st)
	if err := s.validatePorts(a); err != nil {
		return err
	}
	switch a.Mode {
	case domain.AddressModeOneName:
		if st.PublicHost == "" {
			return fmt.Errorf("%w: im Modus „ein Name“ fehlt public_host", ErrSettingInvalid)
		}
	case domain.AddressModeSharedPort:
		return s.checkSharedPortHosts(st)
	}
	return nil
}

// validatePorts prüft die Ports: gültiger Bereich, untereinander verschieden,
// und keiner auf dem Port des HTTP-01-Listeners. Der gemeinsame Port zählt nur
// im Modus C mit - sonst kann LCM auch selbst auf 443 lauschen.
func (s *SettingsService) validatePorts(a Addresses) error {
	taken := map[int]string{}
	if err := s.claimPort(taken, s.acmeHTTPPort(), "der Port der Let's-Encrypt-Prüfung (acme_http_listen)"); err != nil {
		return err
	}
	ports := []struct {
		name string
		port int
	}{{"ui_port", a.UIPort}, {"agent_port", a.AgentPort}, {"mcp_port", a.MCPPort}}
	if a.SharedPortActive() {
		ports = append(ports, struct {
			name string
			port int
		}{"shared_tls_port", a.SharedTLSPort})
	}
	for _, p := range ports {
		if p.name == "agent_port" && p.port == 0 {
			continue // Agent-Listener abgeschaltet (LCM_AGENT_PORT=0)
		}
		if err := validatePort(p.port, p.name); err != nil {
			return err
		}
		if err := s.claimPort(taken, p.port, p.name); err != nil {
			return err
		}
	}
	return nil
}

func (s *SettingsService) acmeHTTPPort() int {
	if s.addr.ACMEHTTPPort > 0 {
		return s.addr.ACMEHTTPPort
	}
	return domain.DefaultACMEHTTPPort
}

// claimPort merkt sich, wer port belegt, und lehnt einen zweiten Anspruch ab.
func (s *SettingsService) claimPort(taken map[int]string, port int, by string) error {
	if first, used := taken[port]; used {
		return fmt.Errorf("%w: Port %d ist doppelt vergeben (%s und %s)", ErrSettingInvalid, port, first, by)
	}
	taken[port] = by
	return nil
}

// applyPortInput übernimmt einen Port aus dem PATCH. Ein Port, den eine
// Umgebungsvariable festlegt, bleibt, wie er ist: Wer ihn ändern will, bekommt eine
// Antwort statt eines stillen Ignorierens - dieselbe Zahl wie gezeigt ist aber
// kein Fehler, denn die Oberfläche schickt das Formular als Ganzes.
func applyPortInput(in *int, locked bool, effective int, field string, target *int) (bool, error) {
	if in == nil {
		return false, nil
	}
	if locked {
		if *in != effective {
			return false, fmt.Errorf("%w: %s wird von einer Umgebungsvariable festgelegt (%d)", ErrSettingInvalid, field, effective)
		}
		return false, nil
	}
	if err := validatePort(*in, field); err != nil {
		return false, err
	}
	*target = *in
	return true, nil
}

// applyAddressMode übernimmt den Adressmodus.
func (s *SettingsService) applyAddressMode(mode string, st *domain.GlobalSettings) error {
	if !domain.ValidAddressMode(mode) {
		return fmt.Errorf("%w: address_mode muss separate, one_name oder shared_port sein", ErrSettingInvalid)
	}
	st.AddressMode = mode
	return nil
}

// applyServiceSwitches übernimmt agent_enabled und mcp_enabled aus dem PATCH.
// Den Agenten sperrt LCM_AGENT_PORT=0: Einschalten lehnt es ab.
func (s *SettingsService) applyServiceSwitches(in GlobalSettingsInput, st *domain.GlobalSettings, touch func(string)) error {
	if in.AgentEnabled != nil {
		if *in.AgentEnabled && s.AddressesOf(st).Locks.AgentEnabled {
			return fmt.Errorf("%w: agent_enabled wird von der Umgebung festgelegt (LCM_AGENT_PORT=0 schaltet den Agenten ab)", ErrSettingInvalid)
		}
		st.AgentEnabled = *in.AgentEnabled
		touch("agent_enabled")
	}
	if in.MCPEnabled != nil {
		st.MCPEnabled = *in.MCPEnabled
		touch("mcp_enabled")
	}
	return nil
}

// settleServiceNames wendet die Namensregel des Modus C auf die Schalter an: Ein
// Dienst ohne Namen lässt sich dort nicht einschalten, und wer seinen Namen leer
// speichert (oder in den Modus C wechselt), schaltet ihn dabei aus.
func (s *SettingsService) settleServiceNames(in GlobalSettingsInput, st *domain.GlobalSettings, modeSwitched bool, touch func(string)) error {
	if !s.AddressesOf(st).SharedPortActive() {
		return nil
	}
	services := []struct {
		name, url string
		urlSaved  bool  // dieser PATCH speichert die Adresse
		switchIn  *bool // der Schalter aus dem PATCH
		on        *bool
		field     string
	}{
		{"den Agenten", st.PublicAgentURL, in.PublicAgentURL != nil, in.AgentEnabled, &st.AgentEnabled, "agent_enabled"},
		{"MCP", st.PublicMCPURL, in.PublicMCPURL != nil, in.MCPEnabled, &st.MCPEnabled, "mcp_enabled"},
	}
	for _, svc := range services {
		if sniHost(svc.url) != "" {
			continue
		}
		if svc.switchIn != nil && *svc.switchIn {
			return errServiceNeedsName(svc.name)
		}
		if *svc.on && (svc.urlSaved || modeSwitched) {
			*svc.on = false
			touch(svc.field)
		}
	}
	return nil
}

// AddressPlan ist der Zustand der Listener, den die Einstellungen verlangen.
type AddressPlan struct {
	UIPort        int
	AgentPort     int // 0: kein Agent-Listener (auch bei ausgeschaltetem Agenten)
	MCPPort       int
	SharedTLSPort int // 0: kein gemeinsamer Port
	// AgentEnabled und MCPEnabled sind die Schalter der Dienste. Ist der Agent
	// aus, trennt LCM die verbundenen Agenten und weist neue ab.
	AgentEnabled bool
	MCPEnabled   bool
}

// planOf leitet den Listener-Zustand aus dem wirksamen Stand ab.
func planOf(a Addresses) AddressPlan {
	plan := AddressPlan{
		UIPort: a.UIPort, AgentPort: a.AgentListenPort(), MCPPort: a.MCPPort,
		AgentEnabled: a.AgentEnabled, MCPEnabled: a.MCPEnabled,
	}
	if a.SharedPortActive() {
		plan.SharedTLSPort = a.SharedTLSPort
	}
	return plan
}

// AddressChange ist eine vorbereitete Umschaltung der Listener: Die neuen
// Ports sind gebunden, die alten laufen noch.
type AddressChange interface {
	// Commit nimmt die neuen Listener in Betrieb und gibt die alten frei.
	Commit()
	// Rollback gibt die neuen Ports unbenutzt wieder frei.
	Rollback()
}

// AddressApplier setzt geänderte Ports und Modi zur Laufzeit um, ohne
// Neustart. Prepare bindet alles Neue, ohne das Laufende anzufassen; scheitert
// ein Bind, bleibt alles beim Alten und der Fehler kommt zurück.
type AddressApplier interface {
	Prepare(plan AddressPlan) (AddressChange, error)
}

// WithAddressApplier verdrahtet die Laufzeit-Umschaltung der Listener.
func (s *SettingsService) WithAddressApplier(a AddressApplier) *SettingsService {
	s.applier = a
	return s
}

// prepareListeners bindet die Listener, die der neue Stand braucht. nil, nil:
// nichts zu tun (nichts geändert oder keine Umschaltung verdrahtet).
func (s *SettingsService) prepareListeners(before, after Addresses) (AddressChange, error) {
	if s.applier == nil || !listenersChanged(before, after) {
		return nil, nil
	}
	if err := rejectPortSwap(before, after); err != nil {
		return nil, err
	}
	change, err := s.applier.Prepare(planOf(after))
	if err != nil {
		if errors.Is(err, ErrSettingInvalid) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v - die bisherigen Ports bleiben in Betrieb", ErrPortUnavailable, err)
	}
	return change, nil
}

// rejectPortSwap lehnt einen neuen Port ab, den ein anderer Dienst noch hält:
// Die neuen Ports werden gebunden, solange die alten noch laufen, also
// scheiterte das Binden mit einer Meldung, die niemand versteht. Gilt auch für
// einen Tausch (Oberfläche 9310 gegen Agent 9320) und für eine Kette.
func rejectPortSwap(before, after Addresses) error {
	services := []struct {
		name      string
		old, new  int
		wasBound  bool // der alte Port ist belegt
		wantBound bool // der neue Port wird gebraucht
	}{
		{"Oberfläche", before.UIPort, after.UIPort, true, true},
		{"Agent", before.AgentPort, after.AgentPort, before.AgentListenPort() > 0, after.AgentListenPort() > 0},
		{"MCP", before.MCPPort, after.MCPPort, before.MCPEnabled, after.MCPEnabled},
		{"gemeinsamer Port", before.SharedTLSPort, after.SharedTLSPort, before.SharedPortActive(), after.SharedPortActive()},
	}
	for i, moving := range services {
		if !moving.wantBound || moving.new == moving.old {
			continue
		}
		for j, holder := range services {
			if j != i && holder.wasBound && holder.old == moving.new {
				return fmt.Errorf("%w: Port %d ist noch von %s belegt - Ports nicht tauschen, erst auf einen freien Port wechseln",
					ErrSettingInvalid, moving.new, holder.name)
			}
		}
	}
	return nil
}

// listenersChanged meldet, ob sich etwas ändert, das die Listener betrifft.
func listenersChanged(before, after Addresses) bool {
	return before.UIPort != after.UIPort || before.AgentPort != after.AgentPort ||
		before.MCPPort != after.MCPPort || before.SharedPortActive() != after.SharedPortActive() ||
		before.AgentEnabled != after.AgentEnabled || before.MCPEnabled != after.MCPEnabled ||
		(after.SharedPortActive() && before.SharedTLSPort != after.SharedTLSPort)
}

// applySetupStep übernimmt den Schritt des Einrichtungsassistenten. Zwischen
// session und subscription geht er vor und zurück, aus intro nur auf leer (erledigt),
// aus leer gar nicht (siehe domain.SetupStepMoveAllowed). Derselbe Schritt noch
// einmal ist ein Nichts, alles andere ErrSettingInvalid.
func applySetupStep(in *string, st *domain.GlobalSettings, touch func(string)) error {
	if in == nil || *in == st.SetupStep {
		return nil
	}
	if !domain.ValidSetupStep(*in) {
		return fmt.Errorf("%w: setup_step muss session, addresses, services, access, certificate, onboarding, backup, intro oder leer sein", ErrSettingInvalid)
	}
	if !domain.SetupStepMoveAllowed(st.SetupStep, *in) {
		return fmt.Errorf("%w: setup_step darf von %q nicht auf %q wechseln", ErrSettingInvalid, st.SetupStep, *in)
	}
	st.SetupStep = *in
	touch("setup_step")
	return nil
}
