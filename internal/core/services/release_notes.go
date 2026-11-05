package services

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"LCM/internal/appdocs"
	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"
)

// ReleaseNotesBaseURL ist der Ordner der Versionshinweise auf dem Doku-Server,
// je Version eine Datei <version>.md. Die CI schneidet sie beim Doku-Bau aus
// dem CHANGELOG. Bewusst vom Server statt im Binary: So lässt sich ein Text
// auch nach der Veröffentlichung noch ergänzen. Eine Variable, damit Tests sie
// auf einen lokalen Test-Server umbiegen können.
var ReleaseNotesBaseURL = "https://doc.techeve.de/lcm/static/release-notes/"

const (
	releaseNotesMaxBytes = 256 << 10
	// releaseNotesRetry bremst den Abruf, wenn der Doku-Server nicht antwortet
	// oder die Datei fehlt: Jede Seite eines Administrators fragt sonst erneut.
	releaseNotesRetry = 10 * time.Minute
)

// ReleaseNotes sind die gerenderten Hinweise einer Version.
type ReleaseNotes struct {
	Version string `json:"version"`
	HTML    string `json:"html"`
}

// ReleaseNotesService entscheidet, wem die Hinweise zu einem Update noch
// zustehen, und holt sie vom Doku-Server. Über ihn läuft der Abruf, weil die CSP
// der Oberfläche fremde Ziele sperrt.
type ReleaseNotesService struct {
	settings *repositories.SettingsRepository
	users    *repositories.UserRepository

	mu         sync.Mutex
	cached     *ReleaseNotes
	retryAfter time.Time
}

func NewReleaseNotesService(settings *repositories.SettingsRepository, users *repositories.UserRepository) *ReleaseNotesService {
	return &ReleaseNotesService{settings: settings, users: users}
}

// NoteUpdate merkt sich eine neu eingespielte Version. Nur ein Schritt nach
// oben zählt: Eine Erstinstallation hat nichts Neues zu melden, ein Downgrade
// auch nicht.
func (s *ReleaseNotesService) NoteUpdate(previous, current string) error {
	if previous == "" || version.Compare(current, previous) <= 0 {
		return nil
	}
	return s.settings.UpdateFields(map[string]any{"updated_to_version": current})
}

// Pending liefert die Hinweise, die der Benutzer noch nicht gesehen hat. nil
// heißt: nichts zu zeigen, auch wenn der Doku-Server gerade nicht antwortet. Die
// Hinweise bleiben dann offen und kommen beim nächsten Mal.
func (s *ReleaseNotesService) Pending(user *domain.User) (*ReleaseNotes, error) {
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	current := version.Version
	if settings.UpdatedToVersion != current || user.ReleaseNotesSeen == current {
		return nil, nil
	}
	return s.load(current), nil
}

// MarkSeen hält fest, dass der Benutzer die Hinweise der laufenden Version kennt.
func (s *ReleaseNotesService) MarkSeen(user *domain.User) error {
	return s.users.UpdateFields(user.ID, map[string]any{"release_notes_seen": version.Version})
}

func (s *ReleaseNotesService) load(current string) *ReleaseNotes {
	if notes, fetch := s.reserveFetch(current); !fetch {
		return notes
	}
	html, err := fetchReleaseNotes(ReleaseNotesBaseURL, current)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = &ReleaseNotes{Version: current, HTML: html}
	return s.cached
}

// reserveFetch liefert die gemerkten Hinweise oder gibt genau einem Aufrufer den
// Abruf frei. Die Wartezeit gilt schon ab hier: So fragt niemand parallel, und
// ein Doku-Server, der nicht antwortet, hält keine Anfrage an der Sperre auf.
func (s *ReleaseNotesService) reserveFetch(current string) (*ReleaseNotes, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.cached.Version == current {
		return s.cached, false
	}
	if time.Now().Before(s.retryAfter) {
		return nil, false
	}
	s.retryAfter = time.Now().Add(releaseNotesRetry)
	return nil, true
}

func fetchReleaseNotes(base, ver string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(base + url.PathEscape(ver) + ".md")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("doku-server antwortet mit %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, releaseNotesMaxBytes))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", errors.New("hinweisdatei ist leer")
	}
	return appdocs.Render(string(body)), nil
}
