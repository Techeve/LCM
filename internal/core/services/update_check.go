package services

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/version"
)

// RepoBaseURL ist die Wurzel unseres öffentlichen apt-Repos. Community und
// Beta liegen dort als zwei Suites; der Enterprise-Kanal hat eine eigene,
// zugangsgeschützte Adresse (aus der Subscription, siehe repoIndexURL).
// Bewusst eine Variable (nicht const): Tests biegen sie auf einen lokalen
// Test-Server um, statt echten Netzwerkzugriff zu brauchen.
var RepoBaseURL = "https://repo.techeve.de"

// repoIndexURL baut den Paket-Index-Pfad einer Suite - dieselbe Datei, aus der
// `apt update` die verfügbare LCM-Version liest.
//
// Die Architektur ist fest amd64: Der Index nennt je Architektur dieselbe
// Paketversion, und geprüft wird hier nur die VERSION, nicht das Paket selbst.
func repoIndexURL(base, suite string) string {
	return strings.TrimSuffix(base, "/") + "/dists/" + suite + "/main/binary-amd64/Packages"
}

// aptSuiteForChannel übersetzt den Paketkanal des Hosts in die Suite, unter
// der seine Pakete liegen. Enterprise liegt unter einem eigenen Prefix, dort
// heißt die Suite wieder „stable" - die Trennung macht die Adresse, nicht der
// Suite-Name (siehe packaging/repo-server/ und deploy:apt in .gitlab-ci.yml).
func aptSuiteForChannel(channel string) string {
	if channel == domain.AptChannelBeta {
		return subAptSuiteBeta
	}
	return subAptSuite
}

// changelogURL ist die "Was gibt's Neues"-Ansicht für die Update-Anzeige (statt
// eines GitLab-Release-Links, den das apt-Repo nicht liefert).
const changelogURL = "https://doc.techeve.de/lcm/changelog/"

// reLCMVersion liest die Version-Zeile des lcm-Paket-Stanzas aus der
// Debian-Packages-Datei (RFC822-ähnliches Format, ein Stanza pro Paket).
var reLCMVersion = regexp.MustCompile(`(?ms)^Package:\s*lcm\s*$.*?^Version:\s*(\S+)\s*$`)

// updateStatusCache hält das Ergebnis der letzten Prüfung im Speicher - bewusst
// nicht in der Datenbank: die Prüfung läuft fest im Kern alle 3h plus einmal
// beim Start, ein Neustart holt den Stand ohnehin sofort neu.
type updateStatusCache struct {
	mu        sync.RWMutex
	latest    string
	checkedAt *time.Time
	err       string
	channel   string // Kanal, gegen den zuletzt geprüft wurde
}

// UpdateStatus ist die Update-Sicht für UI und Banner.
type UpdateStatus struct {
	CurrentVersion  string     `json:"current_version"`
	LatestVersion   string     `json:"latest_version"`
	UpdateAvailable bool       `json:"update_available"`
	LatestURL       string     `json:"latest_url"`
	CheckedAt       *time.Time `json:"checked_at"`
	Error           string     `json:"error"`
	// Channel ist der Paketkanal, gegen den geprüft wurde. Er gehört in die
	// Antwort: „aktuell" heißt je Kanal etwas anderes, und ohne die Angabe
	// wäre nicht erkennbar, worauf sich die gemeldete Version bezieht.
	Channel string `json:"channel"`
}

// UpdateStatus liefert den aktuellen Update-Zustand aus dem zwischengespeicherten
// Prüf-Ergebnis (ohne selbst zu prüfen). update_available = die zuletzt
// ermittelte Version ist höher als die installierte (SemVer).
func (s *SettingsService) UpdateStatus() (*UpdateStatus, error) {
	s.updateCache.mu.RLock()
	defer s.updateCache.mu.RUnlock()

	current := version.Version
	out := &UpdateStatus{
		CurrentVersion: current,
		LatestVersion:  s.updateCache.latest,
		LatestURL:      changelogURL,
		CheckedAt:      s.updateCache.checkedAt,
		Error:          s.updateCache.err,
		Channel:        s.updateCache.channel,
	}
	if s.updateCache.latest != "" {
		out.UpdateAvailable = version.Compare(current, s.updateCache.latest) < 0
	}
	return out, nil
}

// CheckForUpdate fragt den Paket-Index unseres apt-Repos ab (kein Token, kein
// GitLab-Zugriff nötig - dieselbe Datei, die `apt update` ausliest) und
// aktualisiert den zwischengespeicherten Stand. Fehler werden im Status
// hinterlegt, brechen den Aufrufer aber nicht hart ab.
func (s *SettingsService) CheckForUpdate() error {
	channel, url, login, secret, srcErr := s.updateSource()
	latest, checkErr := "", srcErr
	if srcErr == nil {
		latest, checkErr = fetchLatestRepoVersion(url, login, secret)
	}
	now := time.Now()

	s.updateCache.mu.Lock()
	s.updateCache.checkedAt = &now
	s.updateCache.channel = channel
	if checkErr != nil {
		s.updateCache.err = checkErr.Error()
	} else {
		s.updateCache.latest = latest
		s.updateCache.err = ""
	}
	s.updateCache.mu.Unlock()

	if checkErr != nil {
		return checkErr
	}
	return nil
}

// updateSource bestimmt, WO nach der neuesten Version gesucht wird: im
// Paketkanal, auf dem der Host tatsächlich steht. Vorher fragte die Prüfung
// immer den Community-Kanal ab - wer auf Beta oder Enterprise stand, bekam
// damit die Version eines fremden Kanals gemeldet und im Zweifel gar kein
// Update angezeigt, obwohl in seinem Kanal längst eins bereitlag.
//
// Community und Beta sind offen (kein Zugang). Enterprise liegt hinter dem
// instanzgebundenen Zugangsschlüssel - dieselben Daten, die auch der
// apt-Quelle des Hosts hinterlegt sind (siehe subscription_apt.go).
func (s *SettingsService) updateSource() (channel, url, login, secret string, err error) {
	settings, err := s.settings.Get()
	if err != nil {
		return "", "", "", "", err
	}
	channel = settings.AptChannel()
	if channel != domain.AptChannelEnterprise {
		return channel, repoIndexURL(RepoBaseURL, aptSuiteForChannel(channel)), "", "", nil
	}

	// Enterprise: eigene Adresse + Zugang. Fehlt beides, ist der gespeicherte
	// Kanal nicht mehr benutzbar - das wird gemeldet, statt still auf den
	// Community-Kanal auszuweichen und eine fremde Version als „neueste" zu
	// verkaufen.
	base := strings.TrimSpace(settings.SubscriptionRepoURL)
	if base == "" {
		return channel, "", "", "", fmt.Errorf("kein Enterprise-Repository hinterlegt - Subscription prüfen")
	}
	key, err := s.cipher.DecryptString(settings.SubscriptionAccessKeyEnc)
	if err != nil {
		return channel, "", "", "", fmt.Errorf("zugangsschlüssel entschlüsseln: %w", err)
	}
	return channel, repoIndexURL(base, aptSuiteForChannel(channel)), settings.InstanceID, key, nil
}

// fetchLatestRepoVersion liest die Version des lcm-Pakets aus dem Paket-Index
// der übergebenen Quelle. login/secret werden nur gesetzt, wenn die Quelle
// Zugang verlangt (Enterprise-Kanal).
func fetchLatestRepoVersion(url, login, secret string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("paket-index-adresse ungültig: %w", err)
	}
	if login != "" {
		req.SetBasicAuth(login, secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("paketquelle nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	// Ein abgelehnter Zugang ist der wahrscheinlichste Fehler im
	// Enterprise-Kanal und verdient deshalb eine eigene Ansage statt einer
	// nackten Statuszahl.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("paketquelle verweigert den Zugang (HTTP %d) - Subscription bzw. Zugangsschlüssel prüfen", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("paketquelle antwortet mit HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("paket-index lesen: %w", err)
	}
	// Der Index führt ALLE noch vorgehaltenen Fassungen des Pakets, je eine
	// pro Stanza. Die erste zu nehmen hieße, sich auf die Reihenfolge des
	// Repository-Servers zu verlassen - die höchste ist die Antwort auf die
	// Frage, und genau die nimmt auch apt.
	treffer := reLCMVersion.FindAllSubmatch(body, -1)
	if len(treffer) == 0 {
		return "", fmt.Errorf("paket 'lcm' nicht im Index gefunden")
	}
	latest := ""
	for _, m := range treffer {
		v := strings.TrimSpace(string(m[1]))
		if latest == "" || version.Compare(latest, v) < 0 {
			latest = v
		}
	}
	return latest, nil
}
