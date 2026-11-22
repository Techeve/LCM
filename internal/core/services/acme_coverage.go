package services

import (
	"log/slog"
	"slices"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/safego"
)

// Ausstellen ohne Extra-Klick: Trägt kein gültiges Zertifikat alle Namen,
// stellt LCM von selbst aus - nach dem Speichern von Let's Encrypt, nach
// geänderten Adressen und nach dem Einschalten von Agent oder MCP. Dieselbe
// Prüfung läuft beim Start.

// acmeAutoBackoff: Nach einem gescheiterten Lauf von selbst startet LCM für
// dieselben Namen erst nach dieser Wartezeit neu. „Jetzt ausstellen“ geht immer.
const acmeAutoBackoff = time.Hour

// autoFailure merkt sich den letzten gescheiterten Lauf, den LCM selbst
// gestartet hat (nur im Speicher: Ein Neustart beginnt frisch).
type autoFailure struct {
	key string // CA und Namen
	at  time.Time
}

// WithAutoIssue schaltet das Ausstellen bei fehlender Abdeckung ein. Ohne
// diesen Aufruf (Tests) startet das Speichern der Einstellungen keinen Lauf.
func (s *ACMEService) WithAutoIssue() *ACMEService {
	s.autoIssue = true
	return s
}

// EnsureCoverage startet im Hintergrund einen Lauf, wenn ACME eingerichtet ist
// und kein gültiges Zertifikat alle Namen trägt. Es tut nichts, wenn schon ein
// Lauf aktiv ist oder ein früher Lauf von selbst für dieselben Namen vor
// weniger als einer Stunde scheiterte.
func (s *ACMEService) EnsureCoverage() {
	st, err := s.settings.Get()
	if err != nil {
		slog.Warn("acme settings not readable", "error", err)
		return
	}
	names, missing := s.missingCoverage(st)
	if !missing {
		return
	}
	key := autoRunKey(st.ACMEDirectoryURL, names)
	if s.inBackoff(key) {
		return
	}
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	safego.Go("acme:coverage", func() {
		defer s.running.Store(false)
		s.noteAutoRun(key, s.runSafe(acmeSystemActor))
	})
}

// missingCoverage nennt die Namen eines Laufs und ob ein Lauf nötig ist: ACME
// ist an und vollständig eingerichtet, und ein gültiges Zertifikat für diese
// CA und alle Namen fehlt.
func (s *ACMEService) missingCoverage(st *domain.GlobalSettings) (names []string, missing bool) {
	if !st.ACMEEnabled || !termsAccepted(st) {
		return nil, false
	}
	names = s.runNames(st)
	if len(names) == 0 {
		return nil, false
	}
	if _, err := s.solver(st); err != nil {
		return nil, false // der Anbieter ist nicht vollständig eingerichtet
	}
	return names, !s.manager.Covers(st.ACMEDirectoryURL, names)
}

func autoRunKey(directoryURL string, names []string) string {
	return directoryURL + "|" + strings.Join(slices.Sorted(slices.Values(names)), ",")
}

func (s *ACMEService) inBackoff(key string) bool {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	return s.autoFail.key == key && s.currentTime().Sub(s.autoFail.at) < acmeAutoBackoff
}

// noteAutoRun hält den Ausgang eines selbst gestarteten Laufs fest.
func (s *ACMEService) noteAutoRun(key string, err error) {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if err != nil {
		s.autoFail = autoFailure{key: key, at: s.currentTime()}
		return
	}
	if s.autoFail.key == key {
		s.autoFail = autoFailure{}
	}
}

// forgetAutoFailure verwirft die Wartezeit: Wer die Einstellungen speichert,
// ändert die Lage, und der nächste Versuch ist berechtigt.
func (s *ACMEService) forgetAutoFailure() {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	s.autoFail = autoFailure{}
}

func (s *ACMEService) currentTime() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}
