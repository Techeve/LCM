package services

import (
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// RedactForTest macht die interne Redaction für Black-Box-Tests im
// services_test-Paket zugänglich.
func RedactForTest(s string) string { return redactSecrets(s) }

// RemainingVulnsSentenceForTest macht den Abschlusssatz der CVE-Neubewertung
// für Black-Box-Tests zugänglich (BUG-022: er muss seinen Bezugsrahmen nennen).
func RemainingVulnsSentenceForTest(servers *repositories.ServerRepository, server *domain.Server, crit, high int) string {
	return remainingVulnsSentence(servers, server, crit, high)
}

// SetContainerCheckForTest nagelt die Betriebsart fest. Ohne das hinge sie
// daran, WO die Tests gerade laufen - auf einem Entwicklerrechner „Host", im
// CI-Container „Container" - und die Tests der LCM-Host-Aktionen (Trivy,
// Sandbox, apt-cacher) schlügen ausgerechnet in der CI fehl.
func (s *ServerService) SetContainerCheckForTest(f func() bool) { s.containerCheck = f }

// SelfUpdateScriptForTest macht den Update-Lauf für Black-Box-Tests lesbar.
func SelfUpdateScriptForTest(restricted bool) string { return selfUpdateScript(restricted) }

// SetSelfUpdatePathsForTest nagelt fest, woran LCM eine Paket-Installation
// erkennt. Ohne das hinge das Ergebnis daran, WO die Tests laufen - ein
// Test-Binary liegt nie unter /usr/bin/lcm.
func SetSelfUpdatePathsForTest(binary, unit string) {
	selfBinaryPath, selfUnitPath = binary, unit
}

// SetContainerCheckForTest nagelt die Betriebsart des Selbst-Updates fest -
// aus demselben Grund wie bei ServerService.
func (s *SelfUpdateService) SetContainerCheckForTest(f func() bool) { s.containerCheck = f }

// FetchLatestRepoVersionForTest öffnet das Auslesen des Paket-Index für den
// Test - geprüft wird die Auswahl der höchsten Fassung, nicht der HTTP-Teil.
func FetchLatestRepoVersionForTest(url, login, secret string) (string, error) {
	return fetchLatestRepoVersion(url, login, secret)
}

// AbortIfStalledForTest führt genau EINEN Watchdog-Durchgang für einen Job
// aus - RunWatchdog selbst läuft endlos im Minutentakt und ist als Ganzes
// nicht prüfbar.
func (s *JobService) AbortIfStalledForTest(job *domain.Job, limit time.Duration) {
	s.abortIfStalled(job, func(*domain.Job) time.Duration { return limit })
}

// SetLastActivityForTest datiert das letzte Lebenszeichen eines überwachten
// Jobs zurück, damit die Stille ohne echtes Warten prüfbar ist.
func (s *JobService) SetLastActivityForTest(jobID string, at time.Time) {
	s.activityMu.Lock()
	if _, ok := s.activity[jobID]; ok {
		s.activity[jobID] = at
	}
	s.activityMu.Unlock()
}
