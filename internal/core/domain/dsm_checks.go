package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"slices"
)

// Firewall und Sicherheitseinstellungen von Synology DSM gegen die Sollwerte.
//
// LCM liest die Werte über die DSM-Web-API und meldet jede Abweichung als
// Befund, wie bei der SSH-Härtung eines Linux-Servers. Der Befund nennt den
// Weg in der DSM-Oberfläche. Auf ausdrücklichen Klick setzt LCM einen Wert
// auch selbst auf das Soll (DSMSettable), nie von sich aus. Die APIs sind
// von Synology nicht dokumentiert - was sich nicht deuten lässt, ist
// „unbekannt" und kein Befund (Nichtwissen ist kein Alarm).

// Zustände einer Prüfung.
const (
	DSMCheckOK        = "ok"
	DSMCheckDeviation = "deviation"
	DSMCheckUnknown   = "unknown"
)

// Die geprüften Einstellungen.
const (
	DSMCheckFirewall     = "firewall"
	DSMCheckSkipIPCheck  = "skipIpCheck"
	DSMCheckCSRF         = "csrf"
	DSMCheckCSP          = "csp"
	DSMCheckIframe       = "iframe"
	DSMCheckSessionClear = "sessionClear"
	DSMCheckAutoBlock    = "autoBlock"
	DSMCheckDoS          = "dos"
	DSMCheckTLSProfile   = "tlsProfile"
	DSMCheckSpectre      = "spectre"
)

// DSMSettable sind die Prüfungen, die LCM auf Klick auf den Sollwert setzt,
// in der Reihenfolge der DSM-Systemsteuerung. Die Firewall fehlt bewusst:
// Mit unpassenden Regeln eingeschaltet sperrt sie LCM und den Admin aus. Die
// Regeln bleiben Sache von DSM.
var DSMSettable = []string{
	DSMCheckSkipIPCheck, DSMCheckCSRF, DSMCheckCSP, DSMCheckIframe, DSMCheckSessionClear,
	DSMCheckAutoBlock, DSMCheckDoS, DSMCheckTLSProfile, DSMCheckSpectre,
}

// IsDSMSettable meldet, ob LCM die Prüfung auf den Sollwert setzen kann.
func IsDSMSettable(key string) bool {
	return slices.Contains(DSMSettable, key)
}

// DSMCheck ist das Ergebnis einer Prüfung.
type DSMCheck struct {
	Key   string `json:"key"`
	State string `json:"state"`
	// Value ist der gelesene Wert im Klartext ("aus", "20 Versuche in 5
	// Minuten", "intermediate"), Params dieselbe Angabe für die Übersetzung.
	Value  string            `json:"value,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// DSMChecks liegt als JSON in einer Textspalte - ein Scan-Ergebnis ohne
// eigene Tabelle, es wird bei jeder Erfassung ganz ersetzt.
type DSMChecks []DSMCheck

// Value schreibt die Liste als JSON (leer = NULL-freier Leerstring).
func (c DSMChecks) Value() (driver.Value, error) {
	if len(c) == 0 {
		return "", nil
	}
	b, err := json.Marshal([]DSMCheck(c))
	return string(b), err
}

// Scan liest die Liste zurück. Ein leerer Wert ist „nie geprüft".
func (c *DSMChecks) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		*c = nil
		return nil
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return errors.New("dsm_checks: unerwarteter Spaltentyp")
	}
	if len(raw) == 0 {
		*c = nil
		return nil
	}
	return json.Unmarshal(raw, (*[]DSMCheck)(c))
}

// dsmCheckTexts sind die deutschen Befundtexte samt Menüpfad in DSM.
// Schlüssel der Übersetzung: insights.<key>.
var dsmCheckTexts = map[string]struct{ key, text string }{
	DSMCheckFirewall: {"dsmFirewallOff",
		"DSM-Firewall ist ausgeschaltet - einschalten unter Systemsteuerung → Sicherheit → Firewall"},
	DSMCheckSkipIPCheck: {"dsmSkipIpCheck",
		"DSM verzichtet auf die IP-Prüfung der Sitzung - ausschalten unter Systemsteuerung → Sicherheit → Sicherheit („Browser-Kompatibilität durch Verzicht auf IP-Prüfung verbessern“)"},
	DSMCheckCSRF: {"dsmCsrfOff",
		"DSM-Schutz vor Cross-Site-Request-Forgery ist aus - einschalten unter Systemsteuerung → Sicherheit → Sicherheit"},
	DSMCheckCSP: {"dsmCspOff",
		"DSM sendet keine HTTP Content Security Policy - einschalten unter Systemsteuerung → Sicherheit → Sicherheit"},
	DSMCheckIframe: {"dsmIframeAllowed",
		"DSM lässt sich in fremde Seiten (iFrame) einbetten - „Einbetten in einen iFrame verhindern“ einschalten unter Systemsteuerung → Sicherheit → Sicherheit"},
	DSMCheckSessionClear: {"dsmSessionsKept",
		"DSM behält Anmeldesitzungen über einen Neustart - „Sitzungen beim Neustart löschen“ einschalten unter Systemsteuerung → Sicherheit → Sicherheit"},
	DSMCheckDoS: {"dsmDosOff",
		"DSM-DoS-Schutz ist aus - ist das Gerät aus dem Internet erreichbar, einschalten unter Systemsteuerung → Sicherheit → Schutz"},
	DSMCheckSpectre: {"dsmSpectreOff",
		"DSM-Schutz vor Spectre und Meltdown ist aus - einschalten unter Systemsteuerung → Sicherheit → Erweitert"},
}

// insights macht aus den Prüfungen Befunde (Abweichungen) und einen Hinweis
// auf nicht deutbare Werte. Alle Abweichungen sind Warnungen: Das Gerät ist
// damit nicht kaputt, aber schwächer geschützt als vorgesehen.
func (c DSMChecks) insights() (befunde, hinweise []StatusInsight) {
	unbekannt := 0
	for _, check := range c {
		switch check.State {
		case DSMCheckDeviation:
			// Ob das Gerät aus dem Internet erreichbar ist, weiß LCM nicht.
			// Ein fehlender DoS-Schutz ist deshalb nur ein Hinweis und färbt
			// die Ampel nicht: Für ein reines LAN-Gerät ist er kein Mangel.
			if check.Key == DSMCheckDoS {
				hinweise = append(hinweise, withSeverity(dsmCheckInsight(check), "info"))
				continue
			}
			befunde = append(befunde, dsmCheckInsight(check))
		case DSMCheckUnknown:
			unbekannt++
		}
	}
	if unbekannt > 0 {
		hinweise = append(hinweise, insight("info", "dsmChecksUnknown",
			formatCount(unbekannt,
				"DSM-Sicherheitseinstellung ließ sich nicht lesen - die Rohantwort steht im Job-Protokoll der Erfassung",
				"DSM-Sicherheitseinstellungen ließen sich nicht lesen - die Rohantwort steht im Job-Protokoll der Erfassung"),
			countParams(unbekannt)))
	}
	return befunde, hinweise
}

func dsmCheckInsight(check DSMCheck) StatusInsight {
	switch check.Key {
	case DSMCheckAutoBlock:
		return autoBlockInsight(check)
	case DSMCheckFirewall:
		if check.Params["profile"] != "" {
			return firewallInsight(check)
		}
	case DSMCheckTLSProfile:
		return insight("warning", "dsmTlsProfile",
			"DSM-TLS/SSL-Profilstufe ist „"+check.Value+"“ statt „moderne Kompatibilität“ - umstellen unter Systemsteuerung → Sicherheit → Erweitert",
			map[string]string{"level": check.Value})
	}
	text, ok := dsmCheckTexts[check.Key]
	if !ok {
		return insight("warning", "", "DSM-Sicherheitseinstellung "+check.Key+" weicht vom Sollwert ab", nil)
	}
	return insight("warning", text.key, text.text, nil)
}

// firewallInsight unterscheidet ein Profil ohne Regeln von einem, das den
// übrigen Verkehr nicht verweigert. Ältere Prüfungen kennen nur den ersten
// Fall und haben kein "rules".
func firewallInsight(check DSMCheck) StatusInsight {
	profile := check.Params["profile"]
	params := map[string]string{"profile": profile}
	if rules := check.Params["rules"]; rules == "" || rules == "0" {
		return insight("warning", "dsmFirewallNoRules",
			"DSM-Firewall ist an, das aktive Profil „"+profile+"“ hat aber keine Regeln und lässt alles durch - Regeln anlegen unter Systemsteuerung → Sicherheit → Firewall",
			params)
	}
	return insight("warning", "dsmFirewallNoDeny",
		"DSM-Firewall ist an, im aktiven Profil „"+profile+"“ verweigert aber keine Regel den übrigen Verkehr - am Ende eine Regel „alles verweigern“ anlegen oder für „Wenn keine Regel zutrifft“ den Zugriff verweigern, unter Systemsteuerung → Sicherheit → Firewall",
		params)
}

// autoBlockInsight unterscheidet „aus" von „zu großzügig eingestellt".
func autoBlockInsight(check DSMCheck) StatusInsight {
	attempts, minutes := check.Params["attempts"], check.Params["minutes"]
	if attempts == "" {
		return insight("warning", "dsmAutoBlockOff",
			"DSM-Funktion „automatisch blockieren“ ist aus - einschalten (höchstens 10 Versuche in 5 Minuten) unter Systemsteuerung → Sicherheit → Schutz", nil)
	}
	return insight("warning", "dsmAutoBlockWeak",
		"DSM blockiert erst nach "+attempts+" Fehlversuchen in "+minutes+" Minuten - höchstens 10 Versuche in 5 Minuten einstellen unter Systemsteuerung → Sicherheit → Schutz",
		map[string]string{"attempts": attempts, "minutes": minutes})
}

// withSeverity setzt die Schwere eines Befunds neu.
func withSeverity(in StatusInsight, severity string) StatusInsight {
	in.Severity = severity
	return in
}
