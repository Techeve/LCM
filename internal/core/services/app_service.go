package services

import (
	"errors"
	"fmt"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// AppService verwaltet den Anwendungskatalog und liefert die Funde je Server.
type AppService struct {
	apps    *repositories.AppRepository
	actions *repositories.CustomActionRepository
	audit   *AuditService
	// latest fragt die neueste Version bei der hinterlegten Quelle ab.
	// Optional - ohne ihn bleibt es beim reinen Bestand.
	latest *LatestChecker
}

// WithLatestChecker verdrahtet den Abgleich mit den Quellen im Netz.
func (s *AppService) WithLatestChecker(c *LatestChecker) *AppService {
	s.latest = c
	return s
}

func NewAppService(apps *repositories.AppRepository, actions *repositories.CustomActionRepository, audit *AuditService) *AppService {
	return &AppService{apps: apps, actions: actions, audit: audit}
}

var (
	// ErrAppSlugTaken: den technischen Namen gibt es schon.
	ErrAppSlugTaken = errors.New("eine anwendung mit diesem technischen namen existiert bereits")
	// ErrAppActionMissing: die verwiesene Eigene Aktion gibt es nicht.
	ErrAppActionMissing = errors.New("die gewählte aktion existiert nicht")
	// ErrAppBuiltinDelete: mitgelieferte Einträge lassen sich abschalten,
	// aber nicht löschen - das nächste Seeding legte sie ohnehin wieder an.
	ErrAppBuiltinDelete = errors.New("mitgelieferte anwendungen lassen sich abschalten, nicht löschen")
)

func (s *AppService) List() ([]domain.AppCatalogEntry, error) { return s.apps.FindAll() }

func (s *AppService) Get(id uint) (*domain.AppCatalogEntry, error) { return s.apps.FindByID(id) }

// Create legt einen eigenen Katalogeintrag an.
func (s *AppService) Create(e *domain.AppCatalogEntry, actor string) error {
	e.Builtin = false
	if err := s.validate(e); err != nil {
		return err
	}
	if _, err := s.apps.FindBySlug(e.Slug); err == nil {
		return ErrAppSlugTaken
	}
	if err := s.apps.Create(e); err != nil {
		return err
	}
	s.audit.Log(actor, "app.create", "app", e.ID, e.Name)
	return nil
}

// Update ändert einen Eintrag.
//
// Auch mitgelieferte Einträge sind änderbar - ihre Erkennungsfelder werden
// allerdings beim nächsten Start wieder auf den Auslieferungsstand gesetzt.
// Wer dauerhaft etwas anderes braucht, legt einen eigenen Eintrag an. Was
// bleibt: der Schalter und die Verweise auf die Aktionen.
func (s *AppService) Update(id uint, e *domain.AppCatalogEntry, actor string) error {
	vorhanden, err := s.apps.FindByID(id)
	if err != nil {
		return err
	}
	e.ID, e.Slug = vorhanden.ID, vorhanden.Slug
	if err := s.validate(e); err != nil {
		return err
	}
	if err := s.apps.Update(e); err != nil {
		return err
	}
	s.audit.Log(actor, "app.update", "app", id, e.Name)
	return nil
}

func (s *AppService) Delete(id uint, actor string) error {
	entry, err := s.apps.FindByID(id)
	if err != nil {
		return err
	}
	if entry.Builtin {
		return ErrAppBuiltinDelete
	}
	if err := s.apps.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "app.delete", "app", id, entry.Name)
	return nil
}

func (s *AppService) validate(e *domain.AppCatalogEntry) error {
	if err := domain.ValidateAppEntry(e); err != nil {
		return err
	}
	for _, ref := range []*uint{e.BackupActionID, e.UpdateActionID} {
		if ref == nil || *ref == 0 {
			continue
		}
		if s.actions == nil {
			return ErrAppActionMissing
		}
		if _, err := s.actions.FindByID(*ref); err != nil {
			return fmt.Errorf("%w (%d)", ErrAppActionMissing, *ref)
		}
	}
	// Ein Verweis auf 0 ist kein Verweis.
	e.BackupActionID, e.UpdateActionID = normalizeRef(e.BackupActionID), normalizeRef(e.UpdateActionID)
	return nil
}

func normalizeRef(id *uint) *uint {
	if id == nil || *id == 0 {
		return nil
	}
	return id
}

// ServerApps ist der Inhalt des Reiters „Anwendungen" eines Servers: die
// erkannten Katalog-Anwendungen und die Dienste, die zu keinem Paket gehören.
type ServerApps struct {
	Detected []DetectedAppView   `json:"detected"`
	Unknown  []domain.UnknownApp `json:"unknown"`
}

// DetectedAppView ist ein Fund samt dem, was der Katalog über ihn weiß -
// zusammengesetzt statt gespeichert, damit die neueste Version an genau einer
// Stelle steht und nicht je Server kopiert wird.
type DetectedAppView struct {
	domain.DetectedApp
	EntryID uint `json:"entry_id"`
	// Name/Beschreibung in beiden Sprachen - die Oberfläche wählt. NameEN
	// steht hier neben dem beim Fund mitgeschriebenen Namen, damit die Liste
	// auch dann etwas anzeigt, wenn der Katalogeintrag inzwischen fehlt.
	NameEN          string `json:"name_en"`
	Description     string `json:"description"`
	DescriptionEN   string `json:"description_en"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	CanBackup       bool   `json:"can_backup"`
	CanUpdate       bool   `json:"can_update"`
}

// ForServer stellt den Reiterinhalt zusammen.
func (s *AppService) ForServer(serverID uint) (*ServerApps, error) {
	detected, err := s.apps.DetectedFor(serverID)
	if err != nil {
		return nil, err
	}
	unknown, err := s.apps.UnknownFor(serverID)
	if err != nil {
		return nil, err
	}
	entries, err := s.apps.FindAll()
	if err != nil {
		return nil, err
	}
	bySlug := make(map[string]domain.AppCatalogEntry, len(entries))
	for _, e := range entries {
		bySlug[e.Slug] = e
	}
	out := &ServerApps{Detected: make([]DetectedAppView, 0, len(detected)), Unknown: unknown}
	for _, app := range detected {
		view := DetectedAppView{DetectedApp: app}
		if entry, ok := bySlug[app.Slug]; ok {
			view.EntryID = entry.ID
			view.NameEN, view.Description = entry.NameEN, entry.Description
			view.DescriptionEN = entry.DescriptionEN
			view.LatestVersion = entry.LatestVersion
			view.UpdateAvailable = domain.AppUpdateAvailable(app.Version, entry.LatestVersion, entry.Compare)
			view.CanBackup = entry.BackupActionID != nil
			view.CanUpdate = entry.UpdateActionID != nil
		}
		out.Detected = append(out.Detected, view)
	}
	return out, nil
}
