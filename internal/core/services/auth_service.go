// Package services enthält die Business-Logik der Anwendung.
// Services sind unabhängig von HTTP (Fiber) und kennen nur Repositories
// und Domain-Structs - dadurch sind sie ohne Webserver testbar.
package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

var (
	// ErrInvalidCredentials deckt "User existiert nicht" und "Passwort
	// falsch" bewusst gemeinsam ab (kein User-Enumeration-Leak).
	ErrInvalidCredentials = errors.New("ungültiger Benutzername oder ungültiges Passwort")
	ErrUserInactive       = errors.New("benutzer ist deaktiviert")
	ErrInvalidToken       = errors.New("ungültiges oder abgelaufenes Token") // 2FA-Challenge
)

// argon2Params: OWASP-empfohlene Parameter für argon2id
// (64 MiB Speicher, 2 Iterationen, 4 Threads, 32-Byte-Key). Zwei Iterationen
// liegen sicher über dem OWASP-Minimum für die 64-MiB-Klasse; ältere Hashes
// mit t=1 bleiben verifizierbar (der Kostenfaktor steckt im Hash-String).
var argon2Params = &argon2id.Params{
	Memory:      64 * 1024,
	Iterations:  2,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

// Claims sind die JWT-Claims der 2FA-Challenge. Ein JWT als Anmeldung gibt es
// nicht mehr: Der Browser meldet sich über eine Server-Sitzung an (auth_session.go),
// Skripte über X-API-Key.
//
// TwoFAPending markiert die Challenge zwischen den beiden Login-Schritten:
// Sie autorisiert AUSSCHLIESSLICH den 2FA-Verify-Schritt, nie API-Zugriffe.
type Claims struct {
	Username     string `json:"username"`
	TwoFAPending bool   `json:"twofa_pending,omitempty"`
	jwt.RegisteredClaims
}

// AuthService verantwortet Login, Passwort-Hashing, Browser-Sitzungen und die 2FA-Challenge.
type AuthService struct {
	users *repositories.UserRepository
	// signingKey ist das HS256-Signaturmaterial dieses Prozesses - bei jedem
	// Start neu aus crypto/rand (siehe newSigningKey). Es gibt kein
	// gespeichertes Geheimnis dahinter: Eine Challenge überlebt keinen Neustart,
	// also braucht auch nichts den Neustart zu überleben. Daraus leitet sich
	// auch das CSRF-Geheimnis ab.
	signingKey []byte
	// sessionIdle liefert die aktuelle Leerlauf-Frist einer Sitzung. Als Funktion,
	// damit die UI-Einstellung (GlobalSettings.SessionTTLMinutes) greift.
	sessionIdle func() time.Duration
	// revoked hält die SHA-256-Hashes eingelöster 2FA-Challenges bis zu deren
	// regulärem Ablauf: Eine Challenge eröffnet nur eine Sitzung.
	revokedMu sync.Mutex
	revoked   map[string]time.Time // Token-Hash -> Ablauf

	// Browser-Sitzungen (siehe auth_session.go).
	sessionsMu sync.Mutex
	sessions   map[string]*session // Sitzungs-ID -> Zustand
	// now ist die Uhr der Sitzungen; Tests ersetzen sie (WithClock).
	now func() time.Time
}

func NewAuthService(users *repositories.UserRepository, sessionIdle time.Duration) *AuthService {
	return &AuthService{users: users, signingKey: newSigningKey(),
		sessionIdle: func() time.Duration { return sessionIdle }, revoked: map[string]time.Time{},
		sessions: map[string]*session{}, now: time.Now}
}

// WithSessionTTL verdrahtet eine dynamische Leerlauf-Frist (z.B. aus den
// globalen Einstellungen). Die Funktion wird bei jeder Prüfung ausgewertet;
// sie sollte selbst auf die config-Vorgabe zurückfallen.
func (s *AuthService) WithSessionTTL(fn func() time.Duration) *AuthService {
	if fn != nil {
		s.sessionIdle = fn
	}
	return s
}

// newSigningKey zieht das Signaturmaterial der 2FA-Challenge für DIESEN Prozessstart aus
// crypto/rand. Es lebt ausschließlich im Arbeitsspeicher.
//
// Folge: Bei jedem (Neu-)Start entsteht ein anderer Signaturschlüssel, wodurch
// ALLE zuvor ausgestellten Tokens ihre Signaturprüfung nicht mehr bestehen -
// alle Sessions sind beendet, ein neues Login ist erforderlich. Das deckt
// insbesondere ab: Rebuild, Prozess-Neustart und das Neuanlegen der Datenbank
// (ein frisches DB-Seeding erfordert stets einen Neustart - sonst würde eine
// alte, weiterhin signatur-gültige Session dem neu erzeugten Admin mit
// derselben ID vertrauen). Da LCM als Einzelinstanz läuft (eingebettetes
// SQLite), ist das gemeinsame Nutzen von Tokens über mehrere Prozesse hinweg
// kein Anwendungsfall - und damit gibt es keinen Grund, ein Geheimnis dafür
// auf der Platte zu halten. Frühere Fassungen mischten ein jwt_secret aus der
// config.json hinein; das war ein Geheimnis ohne Zweck.
func newSigningKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// crypto/rand darf praktisch nie fehlschlagen; wenn doch, ist ein
		// sicherer Betrieb unmöglich - hart abbrechen statt schwach signieren.
		panic("auth: signaturschlüssel konnte nicht erzeugt werden: " + err.Error())
	}
	return key
}

// HashPassword hasht ein Klartext-Passwort mit argon2id.
func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2Params)
}

// MaxLoginPasswordBytes deckelt, was der Login überhaupt an argon2id
// weiterreicht. Die Policy erlaubt beim Setzen 200 Zeichen (PasswordMaxLength);
// die Grenze hier liegt bewusst darüber, damit kein gültiges Passwort daran
// scheitert - aber weit unter dem Rumpf-Limit. Ohne sie hashte der Dienst für
// jeden anonymen Versuch beliebig lange Eingaben, und das Hashing ist absichtlich
// teuer.
const MaxLoginPasswordBytes = 1024

// VerifyPassword prüft ausschließlich die Credentials (ohne Token-
// Ausgabe) und liefert den User. Grundlage des zweistufigen Logins:
// der Controller entscheidet danach, ob 2FA nötig ist.
func (s *AuthService) VerifyPassword(username, password string) (*domain.User, error) {
	if len(password) > MaxLoginPasswordBytes {
		return nil, ErrInvalidCredentials
	}
	user, err := s.users.FindByUsername(username)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			// Dummy-Vergleich gegen Timing-Unterschiede zwischen
			// "User unbekannt" und "Passwort falsch".
			_, _ = argon2id.ComparePasswordAndHash(password, dummyHash)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if user.IsSystem {
		return nil, ErrInvalidCredentials
	}
	match, err := argon2id.ComparePasswordAndHash(password, user.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("passwort prüfen: %w", err)
	}
	if !match {
		return nil, ErrInvalidCredentials
	}
	if !user.Active {
		return nil, ErrUserInactive
	}
	return user, nil
}

// CompleteLogin hält den Login-Zeitstempel fest. Nach erfolgreicher Passwort-
// (und ggf. 2FA-)Prüfung; die Sitzung eröffnet StartSession.
func (s *AuthService) CompleteLogin(user *domain.User) error {
	return s.users.UpdateFields(user.ID, map[string]any{"last_login_at": time.Now()})
}

// IssueChallenge erzeugt ein kurzlebiges Token für den 2FA-Zwischenschritt.
// Es trägt TwoFAPending und autorisiert nur den Verify-Endpunkt.
func (s *AuthService) IssueChallenge(user *domain.User) (string, error) {
	return s.sign(Claims{Username: user.Username, TwoFAPending: true}, user.ID, challengeTTL)
}

// ValidateChallenge prüft ein 2FA-Challenge-Token und liefert den User.
func (s *AuthService) ValidateChallenge(tokenString string) (*domain.User, error) {
	claims, err := s.parse(tokenString)
	if err != nil {
		return nil, ErrChallengeInvalid
	}
	// Eine eingelöste Challenge wird widerrufen (LoginTOTP) - sie darf keine
	// zweite Sitzung eröffnen.
	if !claims.TwoFAPending || s.tokenRevoked(tokenString) {
		return nil, ErrChallengeInvalid
	}
	return s.userFromClaims(claims)
}

func (s *AuthService) sign(claims Claims, userID uint, ttl time.Duration) (string, error) {
	now := time.Now()
	claims.RegisteredClaims = jwt.RegisteredClaims{
		Subject:   fmt.Sprintf("%d", userID),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		Issuer:    "lcm",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.signingKey)
}

// tokenHash ist der Schlüssel der Widerrufsliste (nie der Token selbst).
func tokenHash(tokenString string) string {
	sum := sha256.Sum256([]byte(tokenString))
	return hex.EncodeToString(sum[:])
}

// RevokeToken entwertet GENAU diese Challenge bis zu ihrem regulären Ablauf
// (eingelöst, R2-059). Abgelaufene Einträge werden bei jedem Aufruf
// mitbereinigt - die Liste bleibt so höchstens sitzungsgroß.
func (s *AuthService) RevokeToken(tokenString string) {
	claims, err := s.parse(tokenString)
	if err != nil {
		return // ungültig/abgelaufen - es gibt nichts zu widerrufen
	}
	exp := time.Now().Add(24 * time.Hour)
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Time
	}
	now := time.Now()
	s.revokedMu.Lock()
	defer s.revokedMu.Unlock()
	for h, e := range s.revoked {
		if e.Before(now) {
			delete(s.revoked, h)
		}
	}
	s.revoked[tokenHash(tokenString)] = exp
}

// tokenRevoked meldet, ob dieses Token per Logout entwertet wurde.
func (s *AuthService) tokenRevoked(tokenString string) bool {
	s.revokedMu.Lock()
	defer s.revokedMu.Unlock()
	exp, ok := s.revoked[tokenHash(tokenString)]
	return ok && time.Now().Before(exp)
}

// parse validiert Signatur und Ablauf eines Tokens und liefert die Claims.
func (s *AuthService) parse(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		// Algorithmus explizit festnageln - verhindert alg-Confusion.
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unerwartete Signaturmethode: %v", t.Header["alg"])
		}
		return s.signingKey, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// userFromClaims lädt den User aus dem Subject-Claim (frisch für RBAC).
func (s *AuthService) userFromClaims(claims *Claims) (*domain.User, error) {
	var userID uint
	if _, err := fmt.Sscanf(claims.Subject, "%d", &userID); err != nil {
		return nil, ErrInvalidToken
	}
	user, err := s.users.FindByID(userID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if !user.Active {
		return nil, ErrUserInactive
	}
	// Token-Invalidierung bei Passwortänderung: vor der letzten Änderung
	// ausgestellte Tokens werden abgelehnt (alle Alt-Sessions sind damit tot).
	if user.PasswordChangedAt != nil && claims.IssuedAt != nil &&
		claims.IssuedAt.Time.Before(*user.PasswordChangedAt) {
		return nil, ErrInvalidToken
	}
	return user, nil
}

// dummyHash für konstante Login-Dauer bei unbekanntem Usernamen.
var dummyHash = func() string {
	h, err := argon2id.CreateHash("dummy-timing-equalizer", argon2Params)
	if err != nil {
		panic(err)
	}
	return h
}()
