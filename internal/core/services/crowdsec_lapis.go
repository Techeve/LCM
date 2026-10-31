package services

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Zentrale CrowdSec-LAPIs (Einstellungen → CrowdSec).
//
// Früher gab es genau eine LAPI, als drei Felder der globalen Einstellungen.
// Wer mehrere Standorte oder Kunden betreut, braucht aber mehrere - jede mit
// eigenem Namen, eigener Adresse und eigenem Maschinenkonto. Bei der
// Einrichtung von CrowdSec auf einem Server wird eine davon gewählt.

// ErrCrowdSecLapiInvalid signalisiert eine fehlgeschlagene Validierung; die
// konkrete Ursache steckt in der Meldung.
var ErrCrowdSecLapiInvalid = errors.New("ungültige crowdsec-lapi")

// reLapiLogin: Maschinen-Logins von CrowdSec (cscli machines add). Der Login
// landet in der Credentials-Datei des Servers, also streng.
var reLapiLogin = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)

// CrowdSecLapiInput sind die Formularwerte einer LAPI. Password leer heißt
// beim Bearbeiten „unverändert".
type CrowdSecLapiInput struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

// WithCrowdSecLapis verdrahtet die LAPI-Verwaltung.
func (s *SettingsService) WithCrowdSecLapis(repo *repositories.CrowdSecLapiRepository) *SettingsService {
	s.crowdsecLapis = repo
	return s
}

// ListCrowdSecLapis liefert alle LAPIs (ohne Passwörter).
func (s *SettingsService) ListCrowdSecLapis() ([]domain.CrowdSecLapi, error) {
	return s.crowdsecLapis.List()
}

// SaveCrowdSecLapi legt eine LAPI an (ID 0) oder aktualisiert sie.
func (s *SettingsService) SaveCrowdSecLapi(in CrowdSecLapiInput, actor string) (*domain.CrowdSecLapi, error) {
	lapi, err := s.lapiForSave(in)
	if err != nil {
		return nil, err
	}
	if err := normalizeLapiInput(&in, lapi.ID == 0); err != nil {
		return nil, err
	}
	if other, err := s.crowdsecLapis.FindByName(in.Name); err == nil && other.ID != lapi.ID {
		return nil, fmt.Errorf("%w: name %q ist bereits vergeben", ErrCrowdSecLapiInvalid, in.Name)
	}
	lapi.Name, lapi.URL, lapi.Login = in.Name, in.URL, in.Login
	if in.Password != "" {
		if lapi.PasswordEnc, err = s.cipher.EncryptString(in.Password); err != nil {
			return nil, err
		}
	}
	if err := s.crowdsecLapis.Save(lapi); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "crowdsec.lapi.save", "crowdsec_lapi", lapi.ID, lapi.Name+" ("+lapi.URL+", Login "+lapi.Login+")")
	return lapi, nil
}

// lapiForSave lädt die zu bearbeitende LAPI bzw. liefert eine neue.
func (s *SettingsService) lapiForSave(in CrowdSecLapiInput) (*domain.CrowdSecLapi, error) {
	if in.ID == 0 {
		return &domain.CrowdSecLapi{}, nil
	}
	return s.crowdsecLapis.FindByID(in.ID)
}

// normalizeLapiInput säubert und prüft die Formularwerte. URL und Login
// landen in der Credentials-Datei der Server - daher streng.
func normalizeLapiInput(in *CrowdSecLapiInput, isNew bool) error {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	in.Login = strings.TrimSpace(in.Login)
	if !validAllowlistName(in.Name) {
		return fmt.Errorf("%w: name fehlt, ist länger als 64 Zeichen oder enthält Anführungszeichen/Steuerzeichen", ErrCrowdSecLapiInvalid)
	}
	if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		strings.ContainsAny(in.URL, " '\"`\\") {
		return fmt.Errorf("%w: url muss mit http:// oder https:// beginnen und einen host enthalten", ErrCrowdSecLapiInvalid)
	}
	if !reLapiLogin.MatchString(in.Login) {
		return fmt.Errorf("%w: login fehlt oder enthält unerlaubte zeichen (erlaubt: a-z, 0-9, . _ @ -)", ErrCrowdSecLapiInvalid)
	}
	if isNew && in.Password == "" {
		return fmt.Errorf("%w: passwort fehlt", ErrCrowdSecLapiInvalid)
	}
	return nil
}

// DeleteCrowdSecLapi entfernt eine LAPI aus LCM. Die angebundenen Server
// bleiben angebunden - LCM verwaltet die LAPI nur nicht mehr.
func (s *SettingsService) DeleteCrowdSecLapi(id uint, actor string) error {
	lapi, err := s.crowdsecLapis.FindByID(id)
	if err != nil {
		return err
	}
	if err := s.crowdsecLapis.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "crowdsec.lapi.delete", "crowdsec_lapi", id, lapi.Name)
	return nil
}

// CrowdSecConfig liefert den entschlüsselten Zugang für die Installation:
// die gewählte LAPI und den Console-Key. lapiID 0 nimmt die einzige LAPI,
// wenn es genau eine gibt - so funktionieren API-Aufrufe aus der Zeit, als
// es nur eine gab, unverändert weiter.
func (s *SettingsService) CrowdSecConfig(lapiID uint) (CrowdSecConfig, error) {
	cfg := CrowdSecConfig{}
	if st, err := s.settings.Get(); err == nil && st.CrowdSecConsoleKeyEnc != "" {
		if key, err := s.cipher.DecryptString(st.CrowdSecConsoleKeyEnc); err == nil {
			cfg.ConsoleKey = key
		}
	}
	lapi, err := s.resolveLapi(lapiID)
	if err != nil {
		return cfg, nil // keine LAPI gewählt - die Installation meldet das selbst
	}
	password, err := s.cipher.DecryptString(lapi.PasswordEnc)
	if err != nil {
		return cfg, err
	}
	cfg.LapiURL, cfg.LapiLogin, cfg.LapiPassword = lapi.URL, lapi.Login, password
	return cfg, nil
}

func (s *SettingsService) resolveLapi(id uint) (*domain.CrowdSecLapi, error) {
	if id != 0 {
		return s.crowdsecLapis.FindByID(id)
	}
	all, err := s.crowdsecLapis.List()
	if err != nil {
		return nil, err
	}
	if len(all) != 1 {
		return nil, repositories.ErrNotFound
	}
	return &all[0], nil
}

// CheckCrowdSecLapis prüft vom LCM-Host aus jede LAPI (Login-Probe). Speist
// die CrowdSec-Seite und den Alarm crowdsec_lapi_down.
func (s *SettingsService) CheckCrowdSecLapis() ([]CrowdSecLapiStatus, error) {
	lapis, err := s.crowdsecLapis.List()
	if err != nil {
		return nil, err
	}
	out := make([]CrowdSecLapiStatus, 0, len(lapis))
	for i := range lapis {
		out = append(out, s.checkLapi(&lapis[i]))
	}
	return out, nil
}

// CheckCrowdSecLapi prüft eine einzelne LAPI.
func (s *SettingsService) CheckCrowdSecLapi(id uint) (*CrowdSecLapiStatus, error) {
	lapi, err := s.crowdsecLapis.FindByID(id)
	if err != nil {
		return nil, err
	}
	status := s.checkLapi(lapi)
	return &status, nil
}

func (s *SettingsService) checkLapi(lapi *domain.CrowdSecLapi) CrowdSecLapiStatus {
	status := CrowdSecLapiStatus{Configured: true, Message: "passwort nicht lesbar"}
	if password, err := s.cipher.DecryptString(lapi.PasswordEnc); err == nil {
		status = *probeCrowdSecLapi(lapi.URL, lapi.Login, password)
	}
	status.ID, status.Name = lapi.ID, lapi.Name
	return status
}

// lcmHostLapiName ist der Name, unter dem LCM die auf dem eigenen Host
// eingerichtete LAPI einträgt.
const lcmHostLapiName = "LCM-Host"

// AdoptHostCrowdSecLapi trägt die auf dem LCM-Host eingerichtete LAPI ein:
// Gibt es die Adresse schon, bekommt sie die neuen Zugangsdaten, sonst
// entsteht ein Eintrag „LCM-Host".
func (s *SettingsService) AdoptHostCrowdSecLapi(lapiURL, login, password string) error {
	in := CrowdSecLapiInput{Name: lcmHostLapiName, URL: lapiURL, Login: login, Password: password}
	if existing, err := s.crowdsecLapis.FindByURL(strings.TrimRight(lapiURL, "/")); err == nil {
		in.ID, in.Name = existing.ID, existing.Name
	}
	_, err := s.SaveCrowdSecLapi(in, "lcm-host")
	return err
}

// legacyLapiName ist der Name der aus den alten Einstellungen übernommenen LAPI.
const legacyLapiName = "Standard-LAPI"

// MigrateLegacyCrowdSecLapi übernimmt die einzelne LAPI aus den globalen
// Einstellungen (bis 1.45) in die Liste und leert die alten Felder. Läuft bei
// jedem Start, wirkt aber nur, solange dort noch etwas steht.
func (s *SettingsService) MigrateLegacyCrowdSecLapi() error {
	st, err := s.settings.Get()
	if err != nil || st.CrowdSecLapiURL == "" {
		return err
	}
	if st.CrowdSecLapiConfigured() {
		lapi := &domain.CrowdSecLapi{Name: legacyLapiName, URL: strings.TrimRight(st.CrowdSecLapiURL, "/"),
			Login: st.CrowdSecLapiLogin, PasswordEnc: st.CrowdSecLapiPasswordEnc}
		if _, err := s.crowdsecLapis.FindByURL(lapi.URL); errors.Is(err, repositories.ErrNotFound) {
			if err := s.crowdsecLapis.Save(lapi); err != nil {
				return err
			}
		}
	}
	return s.settings.UpdateFields(map[string]any{
		"crowd_sec_lapi_url": "", "crowd_sec_lapi_login": "", "crowd_sec_lapi_password_enc": "",
	})
}
