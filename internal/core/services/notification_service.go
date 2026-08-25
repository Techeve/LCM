package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/notify"
	"LCM/internal/storage/repositories"
)

var (
	ErrChannelNameRequired = errors.New("name des benachrichtigungskanals ist erforderlich")
	ErrChannelTypeInvalid  = errors.New("unbekannter benachrichtigungs-kanaltyp")
	ErrChannelInUse        = errors.New("der kanal wird von mindestens einer alarmregel verwendet und kann nicht gelöscht werden")
	ErrChannelDisabled     = errors.New("der benachrichtigungskanal ist deaktiviert")
	// ErrChannelManaged: der System-E-Mail-Kanal wird über die Checkbox in
	// Einstellungen → Allgemein verwaltet, nicht über die Kanal-Endpunkte.
	ErrChannelManaged = errors.New("der system-e-mail-kanal wird über die allgemeinen einstellungen verwaltet")
	// ErrChannelConfigInvalid umhüllt Validierungsfehler des Providers
	// (fehlender Host, ungültige Webhook-URL, …) - der Controller übersetzt
	// sie in ein 422 statt eines generischen Serverfehlers.
	ErrChannelConfigInvalid = errors.New("ungültige kanal-konfiguration")
)

// NotificationService verwaltet die Benachrichtigungskanäle (Provider-
// Instanzen) und versendet standardisierte Events über den jeweiligen
// Provider. Sensible Konfigurationswerte (SMTP-Passwort) werden AES-GCM-
// verschlüsselt gespeichert.
type NotificationService struct {
	repo   *repositories.NotificationRepository
	alerts *repositories.AlertRepository
	cipher *crypto.Cipher
	audit  *AuditService
	// systemMailer baut den Provider des Standard-E-Mail-Versands aus den
	// GlobalSettings (zur Sendezeit - kein Konfigurations-Drift). Gesetzt
	// über WithSystemMailer; nil, solange nicht verdrahtet.
	systemMailer func() (notify.Provider, error)
}

func NewNotificationService(repo *repositories.NotificationRepository, alerts *repositories.AlertRepository, cipher *crypto.Cipher, audit *AuditService) *NotificationService {
	return &NotificationService{repo: repo, alerts: alerts, cipher: cipher, audit: audit}
}

// WithSystemMailer verdrahtet den Standard-E-Mail-Versand als Provider-
// Fabrik für den verwalteten Kanal (Typ system_email).
func (s *NotificationService) WithSystemMailer(fn func() (notify.Provider, error)) *NotificationService {
	s.systemMailer = fn
	return s
}

// ChannelInput sind die über die UI/API änderbaren Felder eines Kanals.
type ChannelInput struct {
	Name    string
	Type    string
	Enabled bool
	Config  string // provider-spezifische JSON-Konfiguration (nicht-sensibel)
	Secret  string // sensibler Teil (z.B. SMTP-Passwort); "" = unverändert
}

func (s *NotificationService) List() ([]domain.NotificationChannel, error) {
	return s.repo.FindAll()
}

func (s *NotificationService) Get(id uint) (*domain.NotificationChannel, error) {
	return s.repo.FindByID(id)
}

// validChannelType prüft, ob der Kanal-Typ über die API anlegbar ist.
// system_email fehlt bewusst: der verwaltete Kanal entsteht nur über
// EnsureSystemChannel (Checkbox in den allgemeinen Einstellungen).
func validChannelType(t string) bool {
	return t == domain.ChannelTypeEmail || t == domain.ChannelTypeWebhook
}

// Create legt einen Kanal an. Die Provider-Konfiguration wird vor dem
// Speichern validiert (build + Validate), das Secret verschlüsselt.
func (s *NotificationService) Create(in ChannelInput, actor string) (*domain.NotificationChannel, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, ErrChannelNameRequired
	}
	if !validChannelType(in.Type) {
		return nil, ErrChannelTypeInvalid
	}
	channel := &domain.NotificationChannel{
		Name:    name,
		Type:    in.Type,
		Enabled: in.Enabled,
		Config:  in.Config,
	}
	if err := s.validateConfig(*channel, in.Secret); err != nil {
		return nil, err
	}
	if in.Secret != "" {
		enc, err := s.cipher.EncryptString(in.Secret)
		if err != nil {
			return nil, err
		}
		channel.SecretEnc = enc
	}
	if err := s.repo.Create(channel); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "notification-channel.create", "notification-channel", channel.ID, name)
	return channel, nil
}

// Update ändert einen Kanal. Ein leeres Secret lässt das gespeicherte
// Passwort unverändert.
func (s *NotificationService) Update(id uint, in ChannelInput, actor string) (*domain.NotificationChannel, error) {
	channel, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if channel.Type == domain.ChannelTypeSystemEmail {
		return nil, ErrChannelManaged
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, ErrChannelNameRequired
	}
	if !validChannelType(in.Type) {
		return nil, ErrChannelTypeInvalid
	}
	channel.Name = name
	channel.Type = in.Type
	channel.Enabled = in.Enabled
	channel.Config = in.Config
	if err := s.validateConfig(*channel, s.effectiveSecret(channel, in.Secret)); err != nil {
		return nil, err
	}
	if in.Secret != "" {
		enc, err := s.cipher.EncryptString(in.Secret)
		if err != nil {
			return nil, err
		}
		channel.SecretEnc = enc
	}
	if err := s.repo.Update(channel); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "notification-channel.update", "notification-channel", id, name)
	return channel, nil
}

// Delete entfernt einen Kanal, sofern ihn keine Alarmregel referenziert.
// Der verwaltete System-E-Mail-Kanal ist nicht löschbar (nur abschaltbar).
func (s *NotificationService) Delete(id uint, actor string) error {
	channel, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if channel.Type == domain.ChannelTypeSystemEmail {
		return ErrChannelManaged
	}
	if s.alerts != nil {
		n, err := s.alerts.CountRulesUsingChannel(id)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrChannelInUse
		}
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "notification-channel.delete", "notification-channel", id, channel.Name)
	return nil
}

// Test versendet ein Beispiel-Event über den Kanal (Konfigurations-Check).
func (s *NotificationService) Test(id uint, actor string) error {
	channel, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	event := domain.NotificationEvent{
		Timestamp:   time.Now(),
		Severity:    domain.AlertSeverityInfo,
		Code:        "test",
		Description: "LCM Test-Benachrichtigung",
		Details:     "Diese Nachricht bestätigt, dass der Benachrichtigungskanal korrekt konfiguriert ist.",
	}
	if err := s.Send(channel, event); err != nil {
		return err
	}
	s.audit.Log(actor, "notification-channel.test", "notification-channel", id, channel.Name)
	return nil
}

// Send versendet ein Event über einen bereits geladenen Kanal.
func (s *NotificationService) Send(channel *domain.NotificationChannel, event domain.NotificationEvent) error {
	if !channel.Enabled {
		return ErrChannelDisabled
	}
	provider, err := s.buildProvider(channel)
	if err != nil {
		return err
	}
	return provider.Send(event)
}

// buildProvider entschlüsselt das Secret und baut den passenden Provider.
// Der System-E-Mail-Kanal hat keine eigene Konfiguration - sein Provider
// kommt aus dem Standard-E-Mail-Versand (GlobalSettings, zur Sendezeit).
func (s *NotificationService) buildProvider(channel *domain.NotificationChannel) (notify.Provider, error) {
	if channel.Type == domain.ChannelTypeSystemEmail {
		if s.systemMailer == nil {
			return nil, ErrMailerDisabled
		}
		return s.systemMailer()
	}
	secret, err := s.decryptSecret(channel)
	if err != nil {
		return nil, err
	}
	return notify.New(*channel, secret)
}

// validateConfig baut den Provider und prüft seine Konfiguration. Fehler
// werden als ErrChannelConfigInvalid markiert (→ 422 statt 500).
func (s *NotificationService) validateConfig(channel domain.NotificationChannel, secret string) error {
	provider, err := notify.New(channel, secret)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelConfigInvalid, err)
	}
	if err := provider.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrChannelConfigInvalid, err)
	}
	return nil
}

// decryptSecret liefert das entschlüsselte Secret eines Kanals ("" wenn keins).
func (s *NotificationService) decryptSecret(channel *domain.NotificationChannel) (string, error) {
	if channel.SecretEnc == "" {
		return "", nil
	}
	return s.cipher.DecryptString(channel.SecretEnc)
}

// effectiveSecret liefert für die Validierung das neue Secret, sonst das
// bereits gespeicherte (entschlüsselt).
func (s *NotificationService) effectiveSecret(channel *domain.NotificationChannel, newSecret string) string {
	if newSecret != "" {
		return newSecret
	}
	secret, _ := s.decryptSecret(channel)
	return secret
}

// ---- Verwalteter System-E-Mail-Kanal ------------------------------------------

// SystemChannelEnabled liefert den Zustand der Checkbox „Standard-E-Mail-
// Versand als Benachrichtigungskanal": true, wenn der verwaltete Kanal
// existiert und aktiv ist.
func (s *NotificationService) SystemChannelEnabled() (bool, error) {
	channel, err := s.repo.FindByType(domain.ChannelTypeSystemEmail)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return channel.Enabled, nil
}

// EnsureSystemChannel schaltet den verwalteten Kanal des Standard-E-Mail-
// Versands: legt ihn beim ersten Aktivieren an, danach wird nur Enabled
// umgeschaltet. Abschalten DEAKTIVIERT nur (löscht nicht) - Alarmregeln,
// die den Kanal referenzieren, bleiben intakt.
func (s *NotificationService) EnsureSystemChannel(enabled bool, actor string) error {
	channel, err := s.repo.FindByType(domain.ChannelTypeSystemEmail)
	if err != nil {
		if !errors.Is(err, repositories.ErrNotFound) {
			return err
		}
		if !enabled {
			return nil // aus + nicht vorhanden: nichts zu tun
		}
		channel = &domain.NotificationChannel{
			Name:    domain.SystemEmailChannelName,
			Type:    domain.ChannelTypeSystemEmail,
			Enabled: true,
		}
		if err := s.repo.Create(channel); err != nil {
			return err
		}
		s.audit.Log(actor, "notification-channel.create", "notification-channel", channel.ID, channel.Name)
		return nil
	}
	if channel.Enabled == enabled {
		return nil
	}
	channel.Enabled = enabled
	if err := s.repo.Update(channel); err != nil {
		return err
	}
	s.audit.Log(actor, "notification-channel.update", "notification-channel", channel.ID, channel.Name)
	return nil
}
