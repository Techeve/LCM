package services

import (
	"errors"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/totp"
	"LCM/internal/storage/repositories"
)

var (
	ErrTOTPRequired     = errors.New("2fa-code erforderlich")
	ErrTOTPInvalid      = errors.New("ungültiger 2fa-code")
	ErrTOTPNotSetup     = errors.New("2fa wurde noch nicht eingerichtet")
	ErrTOTPAlreadyOn    = errors.New("2fa ist bereits aktiviert")
	ErrChallengeInvalid = errors.New("ungültige oder abgelaufene 2fa-anmeldung")
)

// totpIssuer erscheint im Authenticator als Aussteller-Name.
const totpIssuer = "LCM"

// TOTPService verwaltet Einrichtung, Aktivierung und Prüfung der
// Zwei-Faktor-Authentifizierung. Das Secret wird AES-GCM-verschlüsselt
// gespeichert.
type TOTPService struct {
	users  *repositories.UserRepository
	cipher *crypto.Cipher
	audit  *AuditService
	// used merkt sich je Benutzer den zuletzt angenommenen Code samt Ablauf.
	// Ein TOTP-Code ist im Fenster von ±30 s mehrfach „richtig" - wer ihn
	// mitliest (Schulterblick, Phishing-Proxy), könnte ihn in derselben
	// Minute noch einmal verwenden. RFC 6238 verlangt deshalb, einen
	// angenommenen Code nicht ein zweites Mal anzunehmen. In-Memory reicht:
	// LCM läuft als eine Instanz, und ein Neustart dauert länger als das Fenster.
	usedMu sync.Mutex
	used   map[uint]usedCode
}

type usedCode struct {
	code  string
	until time.Time
}

// usedCodeTTL überspannt das Prüffenster von Validate (±1 Periode) mit Reserve.
const usedCodeTTL = 2 * time.Minute

func NewTOTPService(users *repositories.UserRepository, cipher *crypto.Cipher, audit *AuditService) *TOTPService {
	return &TOTPService{users: users, cipher: cipher, audit: audit, used: map[uint]usedCode{}}
}

// consume nimmt einen bereits als gültig erkannten Code für den Benutzer an -
// oder lehnt ihn ab, wenn genau dieser Code gerade schon angenommen wurde.
func (s *TOTPService) consume(userID uint, code string) bool {
	now := time.Now()
	s.usedMu.Lock()
	defer s.usedMu.Unlock()
	for id, u := range s.used {
		if now.After(u.until) {
			delete(s.used, id)
		}
	}
	if u, ok := s.used[userID]; ok && u.code == code {
		return false
	}
	s.used[userID] = usedCode{code: code, until: now.Add(usedCodeTTL)}
	return true
}

// SetupResult liefert Secret, QR-Provisioning-URI und ein scanbares
// QR-Code-Bild (PNG als data:-URI) für die Authenticator-App.
type SetupResult struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
	QRCodeDataURI   string `json:"qr_code"`
}

// Setup generiert ein neues TOTP-Secret für den User (noch nicht aktiv)
// und liefert die Daten für die Authenticator-App inkl. QR-Code.
// Wiederholtes Setup überschreibt ein noch nicht aktiviertes Secret.
func (s *TOTPService) Setup(userID uint) (*SetupResult, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if user.TOTPEnabled {
		return nil, ErrTOTPAlreadyOn
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		return nil, err
	}
	enc, err := s.cipher.EncryptString(secret)
	if err != nil {
		return nil, err
	}
	if err := s.users.UpdateFields(userID, map[string]any{"totp_secret_enc": enc}); err != nil {
		return nil, err
	}
	s.audit.Log(user.Username, "2fa.setup", "user", userID, "TOTP-Secret erzeugt (noch nicht aktiviert)")
	uri := totp.ProvisioningURI(secret, user.Username, totpIssuer)
	qr, err := totp.QRCodeDataURI(uri)
	if err != nil {
		return nil, err
	}
	return &SetupResult{
		Secret:          secret,
		ProvisioningURI: uri,
		QRCodeDataURI:   qr,
	}, nil
}

// Enable aktiviert 2FA, nachdem der User einen gültigen Code aus seiner
// App bestätigt hat.
func (s *TOTPService) Enable(userID uint, code string) error {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if user.TOTPEnabled {
		return ErrTOTPAlreadyOn
	}
	secret, err := s.cipher.DecryptString(user.TOTPSecretEnc)
	if err != nil || secret == "" {
		return ErrTOTPNotSetup
	}
	if !totp.Validate(secret, code) || !s.consume(userID, strings.TrimSpace(code)) {
		return ErrTOTPInvalid
	}
	if err := s.users.UpdateFields(userID, map[string]any{"totp_enabled": true}); err != nil {
		return err
	}
	s.audit.Log(user.Username, "2fa.enable", "user", userID, "")
	return nil
}

// Disable schaltet 2FA wieder ab (nur mit gültigem Code).
func (s *TOTPService) Disable(userID uint, code string) error {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if !user.TOTPEnabled {
		// Nicht aktiviert, aber ein anstehendes Secret aus einem
		// abgebrochenen setup vorhanden: OHNE Code abräumen - es ist
		// wirkungslos und sollte den Datensatz nicht dauerhaft belasten
		// (R2-061). Ein Code lässt sich hier auch gar nicht verlangen, weil
		// der Nutzer die Einrichtung ja abgebrochen hat.
		if user.TOTPSecretEnc != "" {
			if err := s.users.UpdateFields(userID, map[string]any{"totp_secret_enc": ""}); err != nil {
				return err
			}
			s.audit.Log(user.Username, "2fa.setup-abort", "user", userID, "anstehendes TOTP-Secret verworfen")
			return nil
		}
		return ErrTOTPNotSetup
	}
	if err := s.verify(user, code); err != nil {
		return err
	}
	if err := s.users.UpdateFields(userID, map[string]any{"totp_enabled": false, "totp_secret_enc": ""}); err != nil {
		return err
	}
	s.audit.Log(user.Username, "2fa.disable", "user", userID, "")
	return nil
}

// verify prüft einen Code gegen das gespeicherte Secret des Users. Ein
// angenommener Code gilt genau einmal (siehe consume).
func (s *TOTPService) verify(user *domain.User, code string) error {
	secret, err := s.cipher.DecryptString(user.TOTPSecretEnc)
	if err != nil || secret == "" {
		return ErrTOTPNotSetup
	}
	if !totp.Validate(secret, code) || !s.consume(user.ID, strings.TrimSpace(code)) {
		return ErrTOTPInvalid
	}
	return nil
}

// Verify prüft einen Code für einen User (öffentliche API für den
// zweiten Login-Schritt).
func (s *TOTPService) Verify(userID uint, code string) error {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	return s.verify(user, code)
}

// challengeTTL begrenzt die Gültigkeit einer 2FA-Anmeldung zwischen den
// beiden Login-Schritten.
const challengeTTL = 5 * time.Minute
