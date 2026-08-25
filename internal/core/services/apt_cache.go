package services

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ErrAptCacheURLInvalid signalisiert eine ungültige APT-Cache-URL in den
// globalen Einstellungen.
var ErrAptCacheURLInvalid = fmt.Errorf("%w: ungültige apt-cache-url", ErrSettingInvalid)

// normalizeAptCacheURL validiert und normalisiert die APT-Cache-URL
// (leer = Feature aus). Die URL landet später wörtlich in apt-Drop-ins auf
// den Zielsystemen - daher dieselben strikten Zeichenregeln wie beim
// Repository-Katalog.
func normalizeAptCacheURL(raw string) (string, error) {
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "/"))
	if raw == "" {
		return "", nil
	}
	if strings.ContainsAny(raw, "\"'`\\\n\r ") || strings.Contains(raw, "$(") {
		return "", fmt.Errorf("%w: quotes, leerzeichen oder subshells sind nicht erlaubt", ErrAptCacheURLInvalid)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%w: erwartet wird http(s)://host[:port], z.B. http://192.168.1.10:3142", ErrAptCacheURLInvalid)
	}
	return raw, nil
}

// AptCacheStatus ist das Ergebnis des Erreichbarkeits-Checks.
type AptCacheStatus struct {
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	Running    bool   `json:"running"` // Report-Seite antwortet mit 200
	HTTPStatus int    `json:"http_status,omitempty"`
	Message    string `json:"message"`
}

// AptCacheStats sind die Transfer-Statistiken von der apt-cacher-ng-eigenen
// Report-Seite ("Transfer statistics") - menschenlesbar direkt übernommen
// (z.B. "1.2 GB"), keine Umrechnung nötig.
type AptCacheStats struct {
	DataFetched       string `json:"data_fetched"`        // seit Dienst-Start
	DataFetchedRecent string `json:"data_fetched_recent"` // jüngste Historie
	DataServed        string `json:"data_served"`
	DataServedRecent  string `json:"data_served_recent"`
}

// aptCacheHTTPTimeout: der Cache steht im lokalen Netz - antwortet er nicht
// binnen weniger Sekunden, ist er faktisch nicht nutzbar.
const aptCacheHTTPTimeout = 5 * time.Second

// reAcngFetched/reAcngServed lesen die "Transfer statistics"-Tabelle der
// apt-cacher-ng-Report-Seite (conf/report.html im Quellcode): pro Zeile zwei
// Zellen ("Since startup", "Recent history"), jeweils ein Bargraph-<img> mit
// dem menschenlesbaren Wert direkt danach, vor </td>.
var (
	reAcngFetched = regexp.MustCompile(`(?s)Data fetched:.*?height=11>\s*([^<]+?)\s*</td>.*?height=11>\s*([^<]+?)\s*</td>`)
	reAcngServed  = regexp.MustCompile(`(?s)Data served:.*?height=11>\s*([^<]+?)\s*</td>.*?height=11>\s*([^<]+?)\s*</td>`)
)

// parseAcngStats liest die Transfer-Statistiken aus dem HTML der Report-Seite.
// Passt das Format nicht (andere apt-cacher-ng-Version, angepasstes Theme),
// liefert es nil statt eines Fehlers - die Statistiken sind eine Zusatzinfo,
// kein kritischer Pfad.
func parseAcngStats(html string) *AptCacheStats {
	fm := reAcngFetched.FindStringSubmatch(html)
	sm := reAcngServed.FindStringSubmatch(html)
	if fm == nil && sm == nil {
		return nil
	}
	stats := &AptCacheStats{}
	if fm != nil {
		stats.DataFetched, stats.DataFetchedRecent = fm[1], fm[2]
	}
	if sm != nil {
		stats.DataServed, stats.DataServedRecent = sm[1], sm[2]
	}
	return stats
}

// fetchAcngReport holt die apt-cacher-ng-Report-Seite (/acng-report.html)
// unter baseURL und liefert sowohl den Erreichbarkeits-Status als auch (bei
// Erfolg, soweit das HTML-Format passt) die Transfer-Statistiken.
func fetchAcngReport(baseURL string) (*AptCacheStatus, *AptCacheStats) {
	status := &AptCacheStatus{Configured: true}
	client := &http.Client{Timeout: aptCacheHTTPTimeout}
	resp, err := client.Get(baseURL + "/acng-report.html")
	if err != nil {
		status.Message = fmt.Sprintf("nicht erreichbar: %v", err)
		return status, nil
	}
	defer resp.Body.Close()
	status.Reachable = true
	status.HTTPStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		status.Message = fmt.Sprintf("erreichbar, aber unerwartete antwort (HTTP %d) - läuft dort apt-cacher-ng?", resp.StatusCode)
		return status, nil
	}
	// HTTP 200 allein genügt nicht: unter der URL könnte auch ein anderer
	// Dienst antworten (fremder Webserver, Reverse-Proxy-Fehlerseite, ein an den
	// Port gebundener Dashboard-Login). Erst wenn der Inhalt erkennbar von
	// apt-cacher-ng stammt, gilt der Cache wirklich als „läuft" - so schlägt
	// auch der apt_cacher_down-Alarm bei einem stillen Fehlstart des Dienstes an.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		// Body nicht lesbar - 200 von der Report-URL ist ein brauchbares Signal;
		// im Zweifel „läuft", um keine Fehlalarme zu erzeugen.
		status.Running = true
		status.Message = "apt-cacher-ng läuft"
		return status, nil
	}
	if !isAcngReport(string(body)) {
		status.Message = "erreichbar, aber die Antwort ist keine apt-cacher-ng-Report-Seite - läuft dort ein anderer Dienst?"
		return status, nil
	}
	status.Running = true
	status.Message = "apt-cacher-ng läuft"
	return status, parseAcngStats(string(body))
}

// isAcngReport prüft, ob der Seiteninhalt tatsächlich von apt-cacher-ng stammt.
// Die Report-Seite führt „Apt-Cacher" durchgängig im Titel/Kopf - ein stabiler,
// versionsübergreifender Marker, der einen fremden Dienst auf demselben Port
// sicher aussortiert (case-insensitive, deckt „Apt-Cacher NG" wie „apt-cacher-ng" ab).
func isAcngReport(html string) bool {
	return strings.Contains(strings.ToLower(html), "apt-cacher")
}

// CheckAptCache prüft vom LCM-Host aus, ob unter der konfigurierten URL ein
// apt-cacher-ng läuft: GET auf dessen Report-Seite (/acng-report.html).
func (s *SettingsService) CheckAptCache() (*AptCacheStatus, error) {
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	if settings.AptCacheURL == "" {
		return &AptCacheStatus{Message: "kein APT-Cache konfiguriert"}, nil
	}
	status, _ := fetchAcngReport(settings.AptCacheURL)
	return status, nil
}
