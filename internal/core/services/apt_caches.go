package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Zentrale APT-Caches (Einstellungen → APT-Cache).
//
// Früher gab es genau eine Cache-URL in den globalen Einstellungen. Wer
// mehrere Standorte betreut, braucht aber je Standort einen Cache - die Regel
// „APT-Cache anbinden" und die Server-Aktion wählen deshalb einen aus der
// Liste.

// ErrAptCacheInvalid signalisiert eine fehlgeschlagene Validierung; die
// konkrete Ursache steckt in der Meldung.
var ErrAptCacheInvalid = errors.New("ungültiger apt-cache")

// AptCacheInput sind die Formularwerte eines Caches.
type AptCacheInput struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// WithAptCaches verdrahtet die Cache-Verwaltung.
func (s *SettingsService) WithAptCaches(repo *repositories.AptCacheRepository) *SettingsService {
	s.aptCaches = repo
	return s
}

// ListAptCaches liefert alle Caches.
func (s *SettingsService) ListAptCaches() ([]domain.AptCache, error) {
	return s.aptCaches.List()
}

// SaveAptCache legt einen Cache an (ID 0) oder aktualisiert ihn.
func (s *SettingsService) SaveAptCache(in AptCacheInput, actor string) (*domain.AptCache, error) {
	cache := &domain.AptCache{}
	if in.ID != 0 {
		existing, err := s.aptCaches.FindByID(in.ID)
		if err != nil {
			return nil, err
		}
		cache = existing
	}
	name := strings.TrimSpace(in.Name)
	if !validAllowlistName(name) {
		return nil, fmt.Errorf("%w: name fehlt, ist länger als 64 Zeichen oder enthält Anführungszeichen/Steuerzeichen", ErrAptCacheInvalid)
	}
	cacheURL, err := normalizeAptCacheURL(in.URL)
	if err != nil {
		return nil, err
	}
	if cacheURL == "" {
		return nil, fmt.Errorf("%w: url fehlt", ErrAptCacheInvalid)
	}
	if other, err := s.aptCaches.FindByName(name); err == nil && other.ID != cache.ID {
		return nil, fmt.Errorf("%w: name %q ist bereits vergeben", ErrAptCacheInvalid, name)
	}
	cache.Name, cache.URL = name, cacheURL
	if err := s.aptCaches.Save(cache); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "apt-cache.save", "apt_cache", cache.ID, cache.Name+" ("+cache.URL+")")
	return cache, nil
}

// DeleteAptCache entfernt einen Cache aus LCM. Angebundene Server bleiben
// angebunden - LCM verwaltet den Cache nur nicht mehr.
func (s *SettingsService) DeleteAptCache(id uint, actor string) error {
	cache, err := s.aptCaches.FindByID(id)
	if err != nil {
		return err
	}
	if err := s.aptCaches.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "apt-cache.delete", "apt_cache", id, cache.Name)
	return nil
}

// AptCacheURL liefert die Adresse des gewählten Caches. id 0 nimmt den
// einzigen, wenn es genau einen gibt - so funktionieren Regeln und
// API-Aufrufe aus der Zeit, als es nur einen gab, unverändert weiter.
func (s *SettingsService) AptCacheURL(id uint) (string, error) {
	cache, err := s.resolveAptCache(id)
	if err != nil {
		return "", ErrNoAptCacheURL
	}
	return cache.URL, nil
}

func (s *SettingsService) resolveAptCache(id uint) (*domain.AptCache, error) {
	if id != 0 {
		return s.aptCaches.FindByID(id)
	}
	all, err := s.aptCaches.List()
	if err != nil {
		return nil, err
	}
	if len(all) != 1 {
		return nil, repositories.ErrNotFound
	}
	return &all[0], nil
}

// CheckAptCaches prüft vom LCM-Host aus jeden Cache (Report-Seite). Speist
// die APT-Cache-Seite und den Alarm apt_cacher_down.
func (s *SettingsService) CheckAptCaches() ([]AptCacheStatus, error) {
	caches, err := s.aptCaches.List()
	if err != nil {
		return nil, err
	}
	out := make([]AptCacheStatus, 0, len(caches))
	for i := range caches {
		out = append(out, checkAptCache(&caches[i]))
	}
	return out, nil
}

// CheckAptCache prüft einen einzelnen Cache.
func (s *SettingsService) CheckAptCache(id uint) (*AptCacheStatus, error) {
	cache, err := s.aptCaches.FindByID(id)
	if err != nil {
		return nil, err
	}
	status := checkAptCache(cache)
	return &status, nil
}

func checkAptCache(cache *domain.AptCache) AptCacheStatus {
	status, _ := fetchAcngReport(cache.URL)
	status.ID, status.Name = cache.ID, cache.Name
	return *status
}

// lcmHostAptCacheName ist der Name, unter dem LCM den auf dem eigenen Host
// eingerichteten apt-cacher-ng einträgt.
const lcmHostAptCacheName = "LCM-Host"

// AdoptHostAptCache trägt den auf dem LCM-Host eingerichteten Cache ein - es
// sei denn, die Adresse steht schon in der Liste.
func (s *SettingsService) AdoptHostAptCache(cacheURL string) error {
	if _, err := s.aptCaches.FindByURL(strings.TrimRight(cacheURL, "/")); err == nil {
		return nil
	}
	_, err := s.SaveAptCache(AptCacheInput{Name: lcmHostAptCacheName, URL: cacheURL}, "lcm-host")
	return err
}

// legacyAptCacheName ist der Name des aus den alten Einstellungen
// übernommenen Caches.
const legacyAptCacheName = "Standard-Cache"

// MigrateLegacyAptCache übernimmt die eine Cache-URL aus den globalen
// Einstellungen (bis 1.46) in die Liste und leert das alte Feld. Läuft bei
// jedem Start, wirkt aber nur, solange dort noch etwas steht.
func (s *SettingsService) MigrateLegacyAptCache() error {
	st, err := s.settings.Get()
	if err != nil || st.AptCacheURL == "" {
		return err
	}
	if _, err := s.aptCaches.FindByURL(st.AptCacheURL); errors.Is(err, repositories.ErrNotFound) {
		if err := s.aptCaches.Save(&domain.AptCache{Name: legacyAptCacheName, URL: st.AptCacheURL}); err != nil {
			return err
		}
	}
	return s.settings.UpdateFields(map[string]any{"apt_cache_url": ""})
}
