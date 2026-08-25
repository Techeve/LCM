package domain

import (
	"strings"
	"time"
)

// --- OS-Support: aktuelle LTS-Version & End-of-Life --------------------------

// OSSupportInfo bewertet die installierte Distribution: ist sie noch mit
// Sicherheitsupdates versorgt, und ist es die aktuell empfohlene (LTS-)Version?
type OSSupportInfo struct {
	Known       bool   `json:"known"`       // konnten wir die Version einordnen?
	Distro      string `json:"distro"`      // "Ubuntu" / "Debian"
	Release     string `json:"release"`     // "22.04" / "12"
	IsLTS       bool   `json:"is_lts"`      // (Ubuntu) LTS-Release?
	Supported   bool   `json:"supported"`   // noch im Sicherheits-Support?
	EOL         string `json:"eol"`         // "YYYY-MM" (Ende Support) oder ""
	Recommended string `json:"recommended"` // aktuell empfohlene Version
	UpToDate    bool   `json:"up_to_date"`  // installierte == empfohlene
	EOLSoon     bool   `json:"eol_soon"`    // Support endet in weniger als 1 Monat
	Severity    string `json:"severity"`    // "" | "warning" | "critical"
	Summary     string `json:"summary"`     // menschenlesbarer Satz
	// SummaryKey/SummaryParams sind derselbe Satz in übersetzbarer Form
	// (siehe StatusInsight) - die Oberfläche schlägt den Schlüssel nach.
	SummaryKey    string            `json:"summary_key,omitempty"`
	SummaryParams map[string]string `json:"summary_params,omitempty"`
}

// osRelease beschreibt einen Distributions-Release im Lebenszyklus.
type osRelease struct {
	eol string // "YYYY-MM": letzter Monat mit Sicherheitsupdates
	lts bool   // Ubuntu-LTS?
}

// Lebenszyklus-Tabellen (Stand: gepflegt im Code - EOL-Daten aus den
// offiziellen Release-Zyklen von Ubuntu und Debian). Bewusst konservativ:
// Ubuntu = 5 Jahre Standard-Support für LTS, 9 Monate für Interim-Releases;
// Debian = ~3 Jahre + LTS-Phase (~5 Jahre insgesamt).
var ubuntuReleases = map[string]osRelease{
	"16.04": {eol: "2021-04", lts: true},
	"18.04": {eol: "2023-05", lts: true},
	"20.04": {eol: "2025-05", lts: true},
	"22.04": {eol: "2027-06", lts: true},
	"24.04": {eol: "2029-06", lts: true},
	"24.10": {eol: "2025-07"},
	"25.04": {eol: "2026-01"},
	"25.10": {eol: "2026-07"},
}

var debianReleases = map[string]osRelease{
	"9":  {eol: "2022-06"},
	"10": {eol: "2024-06"},
	"11": {eol: "2026-08"},
	"12": {eol: "2028-06"},
	"13": {eol: "2030-06"},
}

// Die RHEL-Familie teilt sich einen Lebenszyklus: Rocky Linux und AlmaLinux
// sind binärkompatible Nachbauten und folgen den Daten von Red Hat. Maßgeblich
// ist hier das Ende des Maintenance Support - bis dahin liefert Red Hat
// Sicherheitsupdates, danach nur noch gegen Aufpreis (ELS).
//
// CentOS Stream läuft davor: Es ist der Vorlauf zur nächsten Minor-Version und
// endet mit dem Full Support der zugehörigen RHEL-Fassung.
var rhelReleases = map[string]osRelease{
	"7":  {eol: "2024-06"},
	"8":  {eol: "2029-05"},
	"9":  {eol: "2032-05"},
	"10": {eol: "2035-05"},
}

var centosStreamReleases = map[string]osRelease{
	"8":  {eol: "2024-05"},
	"9":  {eol: "2027-05"},
	"10": {eol: "2030-05"},
}

const (
	ubuntuRecommended = "24.04" // aktuellste LTS
	debianRecommended = "13"    // aktuelles Stable (trixie)
	rhelRecommended   = "10"    // aktuelle Hauptversion der RHEL-Familie
	centosRecommended = "10"    // aktueller CentOS-Stream
)

// OSSupportStatus bewertet die installierte Distribution zum Zeitpunkt now.
// osID/versionID stammen aus /etc/os-release (ID, VERSION_ID); ist osID leer,
// wird die Distribution aus osName erraten. now.IsZero() ⇒ keine Bewertung.
func OSSupportStatus(osID, versionID, osName string, now time.Time) OSSupportInfo {
	if now.IsZero() {
		return OSSupportInfo{}
	}
	distro := strings.ToLower(strings.TrimSpace(osID))
	if distro == "" {
		n := strings.ToLower(osName)
		switch {
		case strings.Contains(n, "ubuntu"):
			distro = "ubuntu"
		case strings.Contains(n, "debian"):
			distro = "debian"
		case strings.Contains(n, "red hat"):
			distro = "rhel"
		case strings.Contains(n, "rocky"):
			distro = "rocky"
		case strings.Contains(n, "almalinux"):
			distro = "almalinux"
		case strings.Contains(n, "centos"):
			distro = "centos"
		}
	}
	// Debian VERSION_ID ist die Major-Zahl ("12"); Ubuntu "22.04".
	rel := strings.TrimSpace(versionID)

	var table map[string]osRelease
	var recommended, distroName string
	switch distro {
	case "ubuntu":
		table, recommended, distroName = ubuntuReleases, ubuntuRecommended, "Ubuntu"
	case "debian":
		// Debian VERSION_ID ist die Major-Zahl ("12"); evtl. "12.5" → "12".
		rel = majorVersion(rel)
		table, recommended, distroName = debianReleases, debianRecommended, "Debian"
	// Die RHEL-Familie meldet die Minor-Version mit ("10.2", "9.5"); der
	// Lebenszyklus hängt aber an der Hauptversion.
	case "rhel":
		rel = majorVersion(rel)
		table, recommended, distroName = rhelReleases, rhelRecommended, "Red Hat Enterprise Linux"
	case "rocky":
		rel = majorVersion(rel)
		table, recommended, distroName = rhelReleases, rhelRecommended, "Rocky Linux"
	case "almalinux":
		rel = majorVersion(rel)
		table, recommended, distroName = rhelReleases, rhelRecommended, "AlmaLinux"
	case "centos":
		rel = majorVersion(rel)
		table, recommended, distroName = centosStreamReleases, centosRecommended, "CentOS Stream"
	default:
		return OSSupportInfo{} // unbekannte Distribution - keine Aussage
	}

	entry, ok := table[rel]
	if !ok {
		return OSSupportInfo{Distro: distroName, Release: rel} // Known bleibt false
	}

	supported := eolInFuture(entry.eol, now)
	info := OSSupportInfo{
		Known: true, Distro: distroName, Release: rel, IsLTS: entry.lts,
		Supported: supported, EOL: entry.eol, Recommended: recommended,
		UpToDate: rel == recommended,
	}

	ltsTag := ""
	if entry.lts {
		ltsTag = " LTS"
	}
	// Bausteine für die übersetzbare Fassung: das benannte System, das
	// Support-Ende und die empfohlene Version.
	info.SummaryParams = map[string]string{
		"os":          distroName + " " + rel + ltsTag,
		"eol":         entry.eol,
		"recommended": distroName + " " + recommended,
	}
	switch {
	case !supported:
		// Außerhalb des Herstellersupports - kritisch (macht den Server rot).
		info.Severity = "critical"
		info.SummaryKey = "osEol"
		info.Summary = distroName + " " + rel + ltsTag +
			" wird seit " + entry.eol + " nicht mehr mit Sicherheitsupdates versorgt (End-of-Life). Upgrade auf " +
			distroName + " " + recommended + " dringend empfohlen."
	case eolSoon(entry.eol, now):
		// Support endet in weniger als einem Monat - ebenfalls kritisch, damit
		// der Betreiber das Upgrade rechtzeitig VOR dem EOL einplant.
		info.Severity = "critical"
		info.EOLSoon = true
		info.SummaryKey = "osEolSoon"
		info.Summary = distroName + " " + rel + ltsTag +
			" erreicht in weniger als einem Monat das Support-Ende (" + entry.eol + "). Upgrade auf " +
			distroName + " " + recommended + " dringend empfohlen."
	case info.UpToDate:
		info.SummaryKey = "osCurrent"
		info.Summary = distroName + " " + rel + ltsTag +
			" - unterstützt bis " + entry.eol + " (aktuelle Version)."
	default:
		info.SummaryKey = "osNewerAvailable"
		info.Summary = distroName + " " + rel + ltsTag +
			" - unterstützt bis " + entry.eol + ". Neuere Version " + distroName + " " + recommended + " verfügbar."
	}
	return info
}

// eolInFuture meldet, ob das EOL-Datum ("YYYY-MM") noch in der Zukunft liegt
// (Support endet am Ende des genannten Monats).
func eolInFuture(eol string, now time.Time) bool {
	t, err := time.Parse("2006-01", eol)
	if err != nil {
		return true // unbekanntes Format ⇒ nicht als EOL werten
	}
	// Ende des Monats = erster Tag des Folgemonats.
	end := t.AddDate(0, 1, 0)
	return now.Before(end)
}

// eolSoon meldet, ob das Support-Ende ("YYYY-MM") in weniger als einem Monat
// bevorsteht (aber noch nicht überschritten ist): now liegt im Kalendermonat
// unmittelbar vor dem Support-Ende.
func eolSoon(eol string, now time.Time) bool {
	t, err := time.Parse("2006-01", eol)
	if err != nil {
		return false
	}
	end := t.AddDate(0, 1, 0)     // Support endet am Monatsende
	warn := end.AddDate(0, -1, 0) // ein Monat davor
	return !now.Before(warn) && now.Before(end)
}

// majorVersion schneidet die Hauptversion aus einer VERSION_ID: "10.2" → "10".
func majorVersion(v string) string {
	if f := strings.Fields(strings.ReplaceAll(v, ".", " ")); len(f) > 0 {
		return f[0]
	}
	return v
}
