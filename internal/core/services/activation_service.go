package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

var (
	ErrActivationInvalid = errors.New("aktivierungslink ist ungültig")
	ErrActivationExpired = errors.New("aktivierungslink ist abgelaufen oder bereits verwendet")
	// ErrUserNoEmail: für den Mail-Versand fehlt die E-Mail-Adresse am User.
	ErrUserNoEmail = errors.New("der benutzer hat keine e-mail-adresse hinterlegt")
)

// ActivationService erstellt und löst zeitlich begrenzte Aktivierungslinks
// ein, über die neue User ihre Credentials selbst setzen. Gespeichert wird
// nur der SHA-256-Hash des Tokens; der Klartext ist einmalig sichtbar.
type ActivationService struct {
	links *repositories.ActivationRepository
	users *repositories.UserRepository
	audit *AuditService
	// mailer versendet transaktionale Mails über den Standard-E-Mail-Versand
	// (SettingsService.SendSystemMail); nil, solange nicht verdrahtet.
	mailer func(subject, body string, to []string) error
	// linkBase liefert die Basis-Adresse für Links in Mails. BEWUSST eine
	// Closure auf die Konfiguration und NICHT der Host-Header des Requests:
	// sonst könnte ein Angreifer per gefälschtem Host einen echten
	// Reset-Link mit gültigem Token auf seine eigene Domain ausstellen
	// lassen (Kontoübernahme). Siehe domain.GlobalSettings.PublicBaseURL.
	linkBase func() string
}

func NewActivationService(links *repositories.ActivationRepository, users *repositories.UserRepository, audit *AuditService) *ActivationService {
	return &ActivationService{links: links, users: users, audit: audit}
}

// WithMailer verdrahtet den Standard-E-Mail-Versand für Einladungs- und
// Passwort-Reset-Mails.
func (s *ActivationService) WithMailer(fn func(subject, body string, to []string) error) *ActivationService {
	s.mailer = fn
	return s
}

// WithLinkBase verdrahtet die Basis-Adresse für Mail-Links (siehe linkBase).
func (s *ActivationService) WithLinkBase(fn func() string) *ActivationService {
	s.linkBase = fn
	return s
}

// base liefert die konfigurierte Basis-Adresse; ohne Verdrahtung bleibt der
// Link relativ (kein Fallback auf Request-Daten - lieber ein unvollständiger
// Link als einer auf eine fremde Domain).
func (s *ActivationService) base() string {
	if s.linkBase == nil {
		return ""
	}
	return s.linkBase()
}

// DefaultActivationTTL: Standard-Gültigkeit eines Aktivierungslinks.
const DefaultActivationTTL = 48 * time.Hour

// MaxActivationTTL: Obergrenze eines Aktivierungslinks (30 Tage). Ein Link
// ist ein temporärer Zugang - er darf nicht faktisch unbefristet sein
// (R2-044: ohne Grenze war Ablauf 2140 möglich).
const MaxActivationTTL = 30 * 24 * time.Hour

// clampActivationTTL: 0/negativ → Standard, über der Grenze → Grenze.
func clampActivationTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return DefaultActivationTTL
	}
	if ttl > MaxActivationTTL {
		return MaxActivationTTL
	}
	return ttl
}

// PasswordResetTTL: Gültigkeit eines Self-Service-Passwort-Reset-Links -
// bewusst kurz, der Link kommt per Mail.
const PasswordResetTTL = time.Hour

// Generate erstellt einen Aktivierungslink für einen User und liefert das
// Klartext-Token (einmalig - nur der Hash wird gespeichert).
func (s *ActivationService) Generate(userID uint, ttl time.Duration, actor string) (string, *domain.ActivationLink, error) {
	if _, err := s.users.FindByID(userID); err != nil {
		return "", nil, err
	}
	// TTL begrenzen: 0/negativ → Standard; über der Obergrenze → Obergrenze.
	// Vorher gab es KEINE Obergrenze - ein Link mit Ablauf im Jahr 2140 war
	// möglich (R2-044).
	ttl = clampActivationTTL(ttl)
	token := config.RandomSecret(32)
	link := &domain.ActivationLink{
		UserID:    userID,
		TokenHash: hashToken(token),
		ExpiresAt: time.Now().Add(ttl),
	}
	if err := s.links.Create(link); err != nil {
		return "", nil, err
	}
	s.audit.Log(actor, "user.activation-link", "user", userID, "")
	return token, link, nil
}

// Consume löst einen Aktivierungslink ein: setzt das Passwort des Users
// und aktiviert ihn. Öffentlicher Endpunkt (kein Login nötig).
func (s *ActivationService) Consume(token, newPassword string) (*domain.User, error) {
	link, err := s.links.FindByTokenHash(hashToken(token))
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, ErrActivationInvalid
		}
		return nil, err
	}
	if !link.Usable(time.Now()) {
		return nil, ErrActivationExpired
	}
	// Policy erst NACH der Token-Prüfung: sonst verriete die Fehlermeldung,
	// ob ein Token gültig ist, ohne dass der Aufrufer eines besitzt.
	// Der Kontoinhaber ist über den Link bekannt - damit greifen auch die
	// personenbezogenen Prüfungen (kein eigener Name im Passwort).
	owner, err := s.users.FindByID(link.UserID)
	if err != nil {
		return nil, err
	}
	if err := EnforcePasswordPolicy(newPassword, PasswordIdentity{
		Username: owner.Username, Email: owner.Email,
		FirstName: owner.FirstName, LastName: owner.LastName,
	}); err != nil {
		return nil, err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	if err := s.users.UpdateFields(link.UserID, map[string]any{
		"password_hash":        hash,
		"active":               true,
		"must_change_password": false,
		// KRITISCH: entwertet alle bestehenden Sessions und API-Keys. Fehlte
		// dieser Zeitstempel, überlebte ein gestohlenes Token genau den
		// Vorgang, der die Kompromittierung beenden soll.
		"password_changed_at": time.Now(),
	}); err != nil {
		return nil, err
	}
	now := time.Now()
	link.ConsumedAt = &now
	if err := s.links.MarkConsumed(link); err != nil {
		return nil, err
	}
	s.audit.Log("self-service", "user.activate", "user", link.UserID, "")
	return s.users.FindByID(link.UserID)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// activationURL baut aus Basis-URL und Token den Link auf die öffentliche
// Aktivierungsseite (Hash-Routing des Single-Binary-Frontends).
func activationURL(linkBase, token string) string {
	return strings.TrimRight(linkBase, "/") + "/#/aktivierung?token=" + token
}

// MailInvitation verschickt einen Aktivierungslink als Einladung an die
// E-Mail-Adresse des Users - setzt den verdrahteten Standard-E-Mail-Versand
// voraus.
func (s *ActivationService) MailInvitation(userID uint, token string, expiresAt time.Time) error {
	if s.mailer == nil {
		return ErrMailerDisabled
	}
	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(user.Email) == "" {
		return ErrUserNoEmail
	}
	body := fmt.Sprintf(`Hallo %s,

für Sie wurde ein Zugang zu LCM (Linux Centralized Management) angelegt.

Benutzername: %s

Über den folgenden Link setzen Sie Ihr Passwort und aktivieren den Zugang:

%s

Der Link ist gültig bis %s und kann nur einmal verwendet werden.
`, displayName(user), user.Username, activationURL(s.base(), token),
		expiresAt.Format("02.01.2006 15:04 MST"))
	if err := s.mailer("[LCM] Ihr Zugang - Passwort festlegen", body, []string{user.Email}); err != nil {
		return err
	}
	s.audit.Log("system", "user.activation-mail", "user", userID, "")
	return nil
}

// RequestPasswordReset stößt den Self-Service-Passwort-Reset an: existiert
// ein aktiver User mit dieser E-Mail-Adresse, bekommt er einen kurzlebigen
// Aktivierungslink gemailt. Der Rückgabewert verrät BEWUSST nicht, ob die
// Adresse existiert (keine User-Enumeration) - Fehler gibt es nur bei
// internen Problemen, nicht bei unbekannter Adresse.
func (s *ActivationService) RequestPasswordReset(email string) error {
	email = strings.TrimSpace(email)
	if email == "" || s.mailer == nil {
		return nil
	}
	user, err := s.users.FindByEmail(email)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil // unbekannte Adresse: nach außen identisch zum Erfolgsfall
		}
		return err
	}
	if !user.Active || user.IsSystem {
		return nil
	}
	token := config.RandomSecret(32)
	link := &domain.ActivationLink{
		UserID:    user.ID,
		TokenHash: hashToken(token),
		ExpiresAt: time.Now().Add(PasswordResetTTL),
	}
	if err := s.links.Create(link); err != nil {
		return err
	}
	body := fmt.Sprintf(`Hallo %s,

für Ihren LCM-Zugang (Benutzername: %s) wurde ein Passwort-Reset angefordert.

Über den folgenden Link setzen Sie ein neues Passwort:

%s

Der Link ist gültig bis %s und kann nur einmal verwendet werden.
Falls Sie das nicht angefordert haben, können Sie diese Mail ignorieren -
Ihr Passwort bleibt unverändert.
`, displayName(user), user.Username, activationURL(s.base(), token),
		link.ExpiresAt.Format("02.01.2006 15:04 MST"))
	if err := s.mailer("[LCM] Passwort zurücksetzen", body, []string{user.Email}); err != nil {
		// Mailer-Fehler nicht an den (anonymen) Aufrufer durchreichen -
		// das wäre ein Enumerations-Seitenkanal. Loggen reicht.
		slog.Error("password reset mail failed", "error", err)
		return nil
	}
	s.audit.Log("self-service", "user.password-reset-request", "user", user.ID, "")
	return nil
}

// displayName liefert den Anzeigenamen für die Mail-Anrede.
func displayName(user *domain.User) string {
	if name := user.FullName(); name != "" {
		return name
	}
	return user.Username
}
