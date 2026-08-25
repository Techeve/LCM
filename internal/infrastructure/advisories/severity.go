package advisories

import (
	"math"
	"strings"

	"LCM/internal/core/domain"
)

// Ermittlung der Schwere eines OSV-Datensatzes.
//
// Der naheliegende Weg - das Feld database_specific.severity - trägt in der
// Praxis genau dort nichts, wo es gebraucht wird: Von 3643 geprüften
// Debian-Meldungen führte es KEINE. Ohne die beiden folgenden Quellen wären
// damit sämtliche Befunde zu Betriebssystempaketen „unbekannt" - und ein
// Alarm ab Schwere „hoch" könnte für sie prinzipiell nie auslösen.
//
// Ausgewertet wird deshalb in dieser Reihenfolge:
//
//  1. database_specific.severity - die ausdrückliche Angabe, wenn vorhanden
//     (so liefern es GitHub-Advisories).
//  2. Der CVSS-Vektor aus severity[] - bei Debian in rund 5 % der Fälle.
//  3. Die Dringlichkeit der Distribution (ecosystem_specific.urgency). Das
//     ist die Einschätzung des Sicherheitsteams selbst und die einzige
//     Angabe, die praktisch flächendeckend vorliegt.

// severityFrom bestimmt die Schwere aus den drei Quellen.
func severityFrom(explicit string, vectors []string, urgency string) string {
	if s := domain.NormalizeSeverity(explicit); s != domain.SeverityUnknown {
		return s
	}
	for _, vector := range vectors {
		if score, ok := cvssBaseScore(vector); ok {
			return severityForScore(score)
		}
	}
	return severityForUrgency(urgency)
}

// severityForScore bildet einen CVSS-Basiswert auf die interne Stufe ab
// (Grenzen nach der CVSS-Spezifikation).
func severityForScore(score float64) string {
	switch {
	case score >= 9.0:
		return domain.SeverityCritical
	case score >= 7.0:
		return domain.SeverityHigh
	case score >= 4.0:
		return domain.SeverityMedium
	case score > 0:
		return domain.SeverityLow
	default:
		return domain.SeverityUnknown
	}
}

// severityForUrgency übersetzt die Dringlichkeit der Distribution.
//
// „unimportant" ist Debians Kennzeichnung für Lücken ohne praktische
// Auswirkung im ausgelieferten Zustand - sie als „niedrig" zu führen ist
// ehrlicher als „unbekannt", weil eine Bewertung ja stattgefunden hat.
// „not yet assigned" und „end-of-life" bleiben dagegen unbekannt: Dort hat
// niemand bewertet, und eine erfundene Stufe wäre schlimmer als keine.
func severityForUrgency(urgency string) string {
	switch strings.ToLower(strings.TrimSpace(urgency)) {
	case "high":
		return domain.SeverityHigh
	case "medium":
		return domain.SeverityMedium
	case "low", "unimportant":
		return domain.SeverityLow
	default:
		return domain.SeverityUnknown
	}
}

// cvssBaseScore rechnet einen CVSS-3.x-Vektor in seinen Basiswert um.
//
// Andere Fassungen (CVSS 2, CVSS 4) werden bewusst nicht gerechnet: Ihre
// Formeln sind andere, und ein mit der falschen Formel ermittelter Wert wäre
// schlechter als gar keiner - er sähe genauso belastbar aus.
func cvssBaseScore(vector string) (float64, bool) {
	if !strings.HasPrefix(vector, "CVSS:3.") {
		return 0, false
	}
	metrics := map[string]string{}
	for _, part := range strings.Split(vector, "/")[1:] {
		if key, value, ok := strings.Cut(part, ":"); ok {
			metrics[key] = value
		}
	}

	changed := metrics["S"] == "C"
	av, ok1 := cvssValue(attackVector, metrics["AV"])
	ac, ok2 := cvssValue(attackComplexity, metrics["AC"])
	ui, ok3 := cvssValue(userInteraction, metrics["UI"])
	c, ok4 := cvssValue(impactMetric, metrics["C"])
	i, ok5 := cvssValue(impactMetric, metrics["I"])
	a, ok6 := cvssValue(impactMetric, metrics["A"])
	pr, ok7 := privilegesRequired(metrics["PR"], changed)
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7) {
		return 0, false
	}

	iss := 1 - (1-c)*(1-i)*(1-a)
	var impact float64
	if changed {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	} else {
		impact = 6.42 * iss
	}
	if impact <= 0 {
		return 0, true
	}
	exploitability := 8.22 * av * ac * pr * ui
	score := impact + exploitability
	if changed {
		score *= 1.08
	}
	return roundUp(math.Min(score, 10)), true
}

// roundUp rundet auf die nächste Zehntelstelle AUF - so schreibt es die
// CVSS-Spezifikation vor (der Umweg über ganze Zahlen vermeidet die
// bekannten Fließkomma-Fehler beim direkten Runden).
func roundUp(value float64) float64 {
	i := int(math.Round(value * 100000))
	if i%10000 == 0 {
		return float64(i) / 100000
	}
	return (math.Floor(float64(i)/10000) + 1) / 10
}

var (
	attackVector     = map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2}
	attackComplexity = map[string]float64{"L": 0.77, "H": 0.44}
	userInteraction  = map[string]float64{"N": 0.85, "R": 0.62}
	impactMetric     = map[string]float64{"H": 0.56, "L": 0.22, "N": 0}
	// Die benötigten Rechte zählen schwerer, wenn der Angriff den
	// Sicherheitsbereich wechselt - daher zwei Tabellen.
	prUnchanged = map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	prChanged   = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.50}
)

func cvssValue(table map[string]float64, key string) (float64, bool) {
	v, ok := table[key]
	return v, ok
}

func privilegesRequired(key string, changed bool) (float64, bool) {
	if changed {
		return cvssValue(prChanged, key)
	}
	return cvssValue(prUnchanged, key)
}
