package domain

import (
	"strings"
	"time"
)

// Befunde der Betriebsfähigkeit für die Ampel (siehe TrafficLight).
//
// Die Oberfläche setzt Dauer und Ursache aus den Parametern selbst in ihrer
// Sprache zusammen (frontend/src/lib/capability.js). Die deutschen Sätze hier
// sind für alles ohne Sprachkatalog: Benachrichtigungen, API-Antworten, Logs.

// Paketstand-Stufen für TrafficLightInput.PackagesStale.
const (
	PackagesStaleWarning  = "warning"
	PackagesStaleCritical = "critical"
)

// capabilityInsights bewertet alle Kategorien. critical ist wahr, wenn eine
// davon den Server rot färbt. Kategorien in Ordnung ergeben keinen Befund -
// außer ihrer Teilwarnung (kaputte Paketquelle bei sonst erfolgreichem Lauf).
func capabilityInsights(caps []ServerCapability, now time.Time) (insights, infos []StatusInsight, critical bool) {
	for _, c := range caps {
		level := c.Effective()
		if level != "" {
			insights = append(insights, actionFailedInsight(c, level, now))
			critical = critical || level == CapabilityCritical
		}
		// Die Warnung stammt aus dem letzten ERFOLG. Scheitert die Kategorie
		// inzwischen, sagt der Fehlschlag mehr - beides zu zeigen, doppelte
		// dieselbe Quelle.
		if c.Category != CapabilityPackages || level != "" {
			continue
		}
		switch c.WarningClass {
		case ErrClassRepoError:
			insights = append(insights, repoWarningInsight(c))
		case aptWarningClass:
			infos = append(infos, aptWarningInsight(c))
		}
	}
	return insights, infos, critical
}

// withoutConnection lässt die Verbindungs-Fähigkeit weg.
func withoutConnection(caps []ServerCapability) []ServerCapability {
	out := make([]ServerCapability, 0, len(caps))
	for _, c := range caps {
		if c.Category != CapabilityConnection {
			out = append(out, c)
		}
	}
	return out
}

// aptWarningClass: Hinweis aus `apt-get update`, der keine Quelle betrifft.
const aptWarningClass = "apt_warning"

func repoWarningInsight(c ServerCapability) StatusInsight {
	return insight("warning", "repoWarning", "Paketquelle fehlerhaft: "+c.WarningMessage,
		map[string]string{"repo": c.WarningMessage})
}

func aptWarningInsight(c ServerCapability) StatusInsight {
	return insight("info", "aptWarning", "Hinweis von apt: "+c.WarningMessage,
		map[string]string{"message": c.WarningMessage})
}

// packagesStaleInsight: der Paketstand ist zu alt, ob oder warum ein einzelner
// Lauf scheiterte, ist dabei gleich. Maßgeblich ist der letzte Erfolg.
func packagesStaleInsight(level string, since, now time.Time) StatusInsight {
	key, severity := "packagesStale", "warning"
	if level == PackagesStaleCritical {
		key, severity = "packagesStaleCritical", "critical"
	}
	msg := "Paketstand veraltet" + sinceClause(&since, now) +
		" - LCM hat die Paketliste nicht mehr erfolgreich aktualisiert"
	params := map[string]string{}
	if !since.IsZero() {
		params["since"] = since.UTC().Format(time.RFC3339)
	}
	return insight(severity, key, msg, params)
}

func actionFailedInsight(c ServerCapability, level string, now time.Time) StatusInsight {
	return insight(level, "actionFailed."+c.Category, actionFailedMessage(c, now), actionFailedParams(c))
}

// actionFailedParams: die Parameter der Fehlerklasse zuerst, damit die festen
// Namen (count, since, causeKey, job) nie von einem gleichnamigen Parameter
// des Servers überschrieben werden.
func actionFailedParams(c ServerCapability) map[string]string {
	params := map[string]string{}
	for k, v := range c.LastErrorParams {
		params[k] = v
	}
	params["count"] = itoa(c.ConsecutiveFailures)
	params["causeKey"] = c.LastErrorClass
	if c.FailingSince != nil {
		params["since"] = c.FailingSince.UTC().Format(time.RFC3339)
	}
	if c.Category == CapabilityRules && c.LastJobName != "" {
		params["job"] = c.LastJobName
	}
	return params
}

// actionFailedMessage: „Updates schlagen seit 2 Tagen fehl (3×): <Ursache>".
func actionFailedMessage(c ServerCapability, now time.Time) string {
	return actionFailedSentence(c, now, c.LastErrorMessage)
}

// ActionFailedAlertText ist derselbe Satz für Alarme und Benachrichtigungen,
// aber OHNE den Rohtext des Werkzeugs: Der kann Pfade, Hostnamen und
// Paketquellen enthalten und gehört nicht in Postfächer oder Chat-Kanäle.
// Eine unbekannte Fehlerklasse verweist stattdessen auf die Job-Ausgabe.
func ActionFailedAlertText(c ServerCapability, now time.Time) string {
	return actionFailedSentence(c, now, "")
}

// ActionRecoveredText: „Updates laufen wieder" - die Entwarnung zu
// ActionFailedAlertText.
func ActionRecoveredText(category string) string {
	switch category {
	case CapabilityConnection:
		return "Verbindung steht wieder"
	case CapabilityRules:
		return "Regeln und Aktionen laufen wieder"
	case CapabilityCVEScan:
		return "CVE-Scan läuft wieder"
	default:
		return "Updates laufen wieder"
	}
}

// PackagesStaleAlertText: der Satz des Paketstand-Befunds für Alarme.
func PackagesStaleAlertText(since, now time.Time) string {
	return packagesStaleInsight(PackagesStaleCritical, since, now).Message
}

func actionFailedSentence(c ServerCapability, now time.Time, raw string) string {
	subject, tail := actionFailedWording(c)
	msg := subject + sinceClause(c.FailingSince, now) + tail
	if c.ConsecutiveFailures > 0 {
		msg += " (" + itoa(c.ConsecutiveFailures) + "×)"
	}
	return msg + ": " + failureCause(c.LastErrorClass, c.LastErrorParams, raw)
}

// actionFailedWording liefert Satzanfang und -ende je Kategorie; dazwischen
// steht die Dauer.
func actionFailedWording(c ServerCapability) (subject, tail string) {
	switch c.Category {
	case CapabilityConnection:
		return "Verbindung scheitert", ""
	case CapabilityRules:
		if c.LastJobName != "" {
			return "Regel „" + c.LastJobName + "“ schlägt", " fehl"
		}
		return "Regeln und Aktionen schlagen", " fehl"
	case CapabilityCVEScan:
		return "CVE-Scan schlägt", " fehl"
	default:
		return "Updates schlagen", " fehl"
	}
}

// failureCause formuliert die Fehlerklasse als Ursache. Unbekanntes fällt auf
// den Rohtext des Werkzeugs zurück - besser ein ungeschliffener Beleg als gar
// keiner.
func failureCause(class string, params map[string]string, raw string) string {
	switch class {
	case ErrClassLockBusy:
		return lockBusyCause(params)
	case ErrClassDpkgInterrupted:
		return "dpkg wurde unterbrochen"
	case ErrClassBrokenDeps:
		return brokenDepsCause(params)
	case ErrClassDiskFull:
		return diskFullCause(params)
	case ErrClassRepoError:
		return repoErrorCause(params)
	case ErrClassNetwork:
		return "Netzwerkproblem - DNS, Proxy bzw. Paket-Cache prüfen"
	case ErrClassTimeout:
		return "Zeitüberschreitung - der Vorgang hing und wurde abgebrochen"
	case ErrClassScannerUnavailable:
		return "CVE-Scanner nicht verfügbar"
	}
	if raw != "" {
		return raw
	}
	return "Ursache unbekannt - Details stehen in der Job-Ausgabe"
}

func lockBusyCause(params map[string]string) string {
	pid := params["pid"]
	if pid == "" {
		return "dpkg-Sperre: ein anderer Prozess hält die Paketverwaltung fest"
	}
	holder := "einen anderen Prozess"
	if p := params["process"]; p != "" {
		holder = p
	}
	return "dpkg-Sperre durch " + holder + " (PID " + pid + ")"
}

func brokenDepsCause(params map[string]string) string {
	pkgs := strings.ReplaceAll(params["packages"], ",", ", ")
	if pkgs == "" {
		return "Abhängigkeiten sind nicht erfüllbar"
	}
	return "Abhängigkeiten nicht erfüllbar: " + pkgs
}

func diskFullCause(params map[string]string) string {
	if path := params["path"]; path != "" {
		return "Kein Speicherplatz mehr auf " + path
	}
	return "Kein Speicherplatz mehr auf dem Server"
}

func repoErrorCause(params map[string]string) string {
	repo := params["repo"]
	msg := "Eine Paketquelle ist fehlerhaft"
	if repo != "" {
		msg = "Paketquelle fehlerhaft: " + repo
	}
	// „Noch nicht gültig" liegt fast immer an der Uhr des Servers, nicht an
	// der Quelle.
	if params["not_valid_yet"] == "true" {
		msg += " (noch nicht gültig - Serverzeit prüfen)"
	}
	return msg
}

// sinceClause: „ seit 2 Tagen" - leer ohne Beginn oder ohne Zeitbezug, damit
// kein erfundener Zeitraum im Satz steht.
func sinceClause(since *time.Time, now time.Time) string {
	if since == nil || since.IsZero() || now.IsZero() {
		return ""
	}
	return " seit " + durationText(now.Sub(*since))
}

// durationText: Minuten, Stunden oder Tage, im Dativ wie nach „seit".
func durationText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "weniger als einer Minute"
	case d < time.Hour:
		return unitText(int(d/time.Minute), "Minute", "Minuten")
	case d < 24*time.Hour:
		return unitText(int(d/time.Hour), "Stunde", "Stunden")
	default:
		return unitText(int(d/(24*time.Hour)), "Tag", "Tagen")
	}
}

func unitText(n int, one, other string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + other
}
