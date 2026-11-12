package services

import (
	"fmt"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/synology"
)

// dsmChecks prüft die gelesenen DSM-Einstellungen gegen die Sollwerte.
func dsmChecks(s synology.Settings) domain.DSMChecks {
	return domain.DSMChecks{
		firewallCheck(s),
		switchCheck(domain.DSMCheckSkipIPCheck, s.SkipIPCheck, false),
		switchCheck(domain.DSMCheckCSRF, s.CSRFProtection, true),
		switchCheck(domain.DSMCheckCSP, s.CSP, true),
		switchCheck(domain.DSMCheckIframe, s.IframeBlocked, true),
		switchCheck(domain.DSMCheckSessionClear, s.SessionsCleared, true),
		autoBlockCheck(s),
		switchCheck(domain.DSMCheckDoS, s.DoS, true),
		tlsCheck(s.TLSProfile),
		switchCheck(domain.DSMCheckSpectre, s.SpectreMeltdown, true),
	}
}

// switchCheck vergleicht einen Schalter mit seinem Soll.
func switchCheck(key string, got synology.Setting, want bool) domain.DSMCheck {
	if !got.Known {
		return domain.DSMCheck{Key: key, State: domain.DSMCheckUnknown}
	}
	check := domain.DSMCheck{Key: key, State: domain.DSMCheckOK, Value: onOff(got.On)}
	if got.Text != "" {
		check.Value += " (" + got.Text + ")"
	}
	if got.On != want {
		check.State = domain.DSMCheckDeviation
	}
	return check
}

// firewallCheck: an, und das aktive Profil verweigert, was keine Regel
// erlaubt. Ohne Regeln oder ohne „alles verweigern“ lässt eine eingeschaltete
// Firewall den übrigen Verkehr durch; das ist eine Abweichung wie eine
// abgeschaltete. Params "rules" trennt im Befund „keine Regeln“ von „nichts
// verweigert“.
func firewallCheck(s synology.Settings) domain.DSMCheck {
	check := switchCheck(domain.DSMCheckFirewall, s.Firewall, true)
	p := s.FirewallProfile
	if check.State != domain.DSMCheckOK || !p.Read() {
		return check
	}
	rules := p.EnabledRules()
	check.Value = fmt.Sprintf("an (%s, %d %s)", p.Profile, rules, pluralRule(rules))
	if rules == 0 {
		check.Value = "an (" + p.Profile + ", keine Regeln)"
	}
	if !p.DenyByDefault() {
		check.State = domain.DSMCheckDeviation
		check.Params = map[string]string{"profile": p.Profile, "rules": strconv.Itoa(rules)}
	}
	return check
}

// autoBlockCheck: an, höchstens 10 Versuche in mindestens 5 Minuten. Ein
// nicht gelesener Grenzwert zählt nicht als Abweichung.
func autoBlockCheck(s synology.Settings) domain.DSMCheck {
	check := switchCheck(domain.DSMCheckAutoBlock, s.AutoBlock, true)
	if check.State != domain.DSMCheckOK || s.AutoBlockAttempts == 0 {
		return check
	}
	check.Value = fmt.Sprintf("%d Versuche in %d Minuten", s.AutoBlockAttempts, s.AutoBlockMinutes)
	tooMany := s.AutoBlockAttempts > synology.AutoBlockMaxAttempts
	tooShort := s.AutoBlockMinutes > 0 && s.AutoBlockMinutes < synology.AutoBlockMinMinutes
	if tooMany || tooShort {
		check.State = domain.DSMCheckDeviation
		check.Params = map[string]string{
			"attempts": strconv.Itoa(s.AutoBlockAttempts), "minutes": strconv.Itoa(s.AutoBlockMinutes),
		}
	}
	return check
}

// tlsCheck: Soll ist „moderne Kompatibilität“. Eine noch nicht zugeordnete
// Stufennummer bleibt unbekannt, steht aber als Wert da.
func tlsCheck(got synology.Setting) domain.DSMCheck {
	if !got.Known {
		return domain.DSMCheck{Key: domain.DSMCheckTLSProfile, State: domain.DSMCheckUnknown, Value: got.Text}
	}
	check := domain.DSMCheck{Key: domain.DSMCheckTLSProfile, State: domain.DSMCheckOK, Value: got.Text}
	if got.Text != "modern" {
		check.State = domain.DSMCheckDeviation
	}
	return check
}

func onOff(on bool) string {
	if on {
		return "an"
	}
	return "aus"
}

// dsmSettingsLog beschreibt die Prüfungen fürs Job-Protokoll. Unbekanntes
// steht mit Grund und Rohantwort da - daran lassen sich die Feldnamen an
// einem echten Gerät nachziehen.
func dsmSettingsLog(s synology.Settings, checks domain.DSMChecks) string {
	raw := map[string]synology.Setting{
		domain.DSMCheckFirewall: s.Firewall, domain.DSMCheckSkipIPCheck: s.SkipIPCheck,
		domain.DSMCheckCSRF: s.CSRFProtection, domain.DSMCheckCSP: s.CSP,
		domain.DSMCheckIframe: s.IframeBlocked, domain.DSMCheckSessionClear: s.SessionsCleared,
		domain.DSMCheckAutoBlock: s.AutoBlock, domain.DSMCheckDoS: s.DoS,
		domain.DSMCheckTLSProfile: s.TLSProfile, domain.DSMCheckSpectre: s.SpectreMeltdown,
	}
	var b strings.Builder
	b.WriteString("Sicherheitseinstellungen:\n")
	for _, c := range checks {
		switch c.State {
		case domain.DSMCheckUnknown:
			got := raw[c.Key]
			fmt.Fprintf(&b, "  %-12s unbekannt - %s\n", c.Key, got.Reason)
			if got.Raw != "" {
				fmt.Fprintf(&b, "  %-12s Rohantwort: %s\n", "", got.Raw)
			}
		case domain.DSMCheckDeviation:
			fmt.Fprintf(&b, "  %-12s %s - weicht vom Soll ab\n", c.Key, c.Value)
		default:
			fmt.Fprintf(&b, "  %-12s %s\n", c.Key, c.Value)
		}
	}
	return b.String()
}

// pluralRule: „1 Regel“, sonst „Regeln“.
func pluralRule(n int) string {
	if n == 1 {
		return "Regel"
	}
	return "Regeln"
}
