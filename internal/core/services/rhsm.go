package services

import (
	"strings"

	"LCM/internal/core/domain"
)

// Registrierung bei Red Hats Subscription Management.
//
// Warum LCM das überhaupt anschaut: Ein nicht registriertes RHEL hat keine
// Paketquellen. `dnf check-update` findet dort nichts - nicht, weil alles
// aktuell wäre, sondern weil niemand nachschauen konnte. Ohne diesen Befund
// stünde so ein Server mit "0 überfällige Updates" da und sähe damit besser
// aus als ein gepflegter, der ehrlich drei offene Updates meldet.
//
// Rocky, AlmaLinux und CentOS kennen subscription-manager nicht; dort liefert
// das Skript nichts und das Feld bleibt leer.

// rhsmStatusScript meldet den Registrierungsstand in einem Wort.
//
// Zwei Wege, weil der erste Root braucht: `subscription-manager status` ist
// die verlässliche Auskunft; wo sie nicht läuft, bleibt das Consumer-
// Zertifikat, das es nur bei einer Registrierung gibt.
const rhsmStatusScript = `command -v subscription-manager >/dev/null 2>&1 || exit 0
out=$(subscription-manager status 2>/dev/null | grep -i '^Overall Status:')
if [ -n "$out" ]; then printf '%s\n' "$out"; exit 0; fi
[ -s /etc/pki/consumer/cert.pem ] && echo 'Overall Status: Current' || echo 'Overall Status: Not registered'
`

// parseRHSMStatus übersetzt die Ausgabe in einen der RHSM-Zustände.
//
// „Disabled" gilt als registriert: Bei Simple Content Access - seit Jahren
// der Normalfall - prüft Red Hat keine Berechtigungen mehr und meldet genau
// das. Es als Mangel zu werten hieße, den Regelfall anzumahnen.
func parseRHSMStatus(out string) string {
	_, wert, ok := strings.Cut(strings.ToLower(out), "overall status:")
	if !ok {
		return ""
	}
	switch strings.TrimSpace(strings.Split(wert, "\n")[0]) {
	case "current", "disabled":
		return domain.RHSMRegistered
	case "insufficient", "invalid", "unknown":
		return domain.RHSMInvalid
	case "not registered":
		return domain.RHSMUnregistered
	default:
		return ""
	}
}
