// Package services enthält die Business-Logik der Anwendung.
// Services sind unabhängig von HTTP (Fiber) und kennen nur Repositories
// und Domain-Structs - dadurch sind sie ohne Webserver testbar.
package services

import (
	"crypto/hmac"
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
	ErrInvalidToken       = errors.New("ungültiges oder abgelaufenes Token")
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

// Claims sind die JWT-Claims dieses Templates. Permissions werden NICHT
// ins Token geschrieben - die Rechteauflösung passiert pro Request gegen
// die DB, damit Rollenänderungen sofort greifen.
//
// TwoFAPending markiert ein Challenge-Token zwischen den beiden
// Login-Schritten: Es autorisiert AUSSCHLIESSLICH den 2FA-Verify-Schritt,
// nie reguläre API-Zugriffe.
type Claims struct {
	Username     string `json:"username"`
	TwoFAPending bool   `json:"twofa_pending,omitempty"`
	jwt.RegisteredClaims
}

// AuthService verantwortet Login, Passwort-Hashing und JWT-Lifecycle.
type AuthService struct {
	users *repositories.UserRepository
	// signingKey ist das effektive HS256-Signaturmaterial. Es wird NICHT
	// direkt aus dem jwt_secret gebildet, sondern an diesen Prozessstart
	// gebunden (siehe deriveSigningKey) - dadurch überlebt keine Session
	// einen Neustart.
	signingKey []byte
	// tokenTTL liefert die aktuelle Session-Lebensdauer. Als Funktion, damit die
	// UI-Einstellung (GlobalSettings.SessionTTLMinutes) zur Login-Zeit greift.
	tokenTTL func() time.Duration
	// revoked hält die SHA-256-Hashes ausgeloggter Tokens bis zu deren
	// regulärem Ablauf. Vorher war POST /auth/logout ein No-Op - der Token
	// blieb serverseitig gültig, ein „Abmelden" beendete die Sitzung nicht
	// (R2-059). In-Memory reicht: ein Neustart entwertet ohnehin ALLE
	// Sessions (prozessgebundener Signaturschlüssel).
	revokedMu sync.Mutex
	revoked   map[string]time.Time // Token-Hash -> Ablauf
}

func NewAuthService(users *repositories.UserRepository, jwtSecret string, tokenTTL time.Duration) *AuthService {
	return &AuthService{users: users, signingKey: deriveSigningKey(jwtSecret),
		tokenTTL: func() time.Duration { return tokenTTL }, revoked: map[string]time.Time{}}
}

// WithSessionTTL verdrahtet eine dynamische Session-Lebensdauer (z.B. aus den
// globalen Einstellungen). Die Funktion wird bei jeder Token-Ausstellung
// ausgewertet; sie sollte selbst auf die config-Vorgabe zurückfallen.
func (s *AuthService) WithSessionTTL(fn func() time.Duration) *AuthService {
	if fn != nil {
		s.tokenTTL = fn
	}
	return s
}

// deriveSigningKey bindet das JWT-Signaturmaterial an DIESEN Prozessstart:
// HMAC-SHA256(jwt_secret, zufälliges Instanz-Nonce). Das Nonce wird bei jedem
// Start neu aus crypto/rand gezogen und lebt ausschließlich im Arbeitsspeicher.
//
// Folge: Bei jedem (Neu-)Start entsteht ein anderer Signaturschlüssel, wodurch
// ALLE zuvor ausgestellten Tokens ihre Signaturprüfung nicht mehr bestehen -
// alle Sessions sind beendet, ein neues Login ist erforderlich. Das gilt auch,
// wenn das jwt_secret in der config.json unverändert bleibt, und deckt damit
// insbesondere ab: Rebuild, Prozess-Neustart und das Neuanlegen der Datenbank
// (ein frisches DB-Seeding erfordert stets einen Neustart - sonst würde eine
// alte, weiterhin signatur-gültige Session dem neu erzeugten Admin mit
// derselben ID vertrauen). Da LCM als Einzelinstanz läuft (eingebettetes
// SQLite), ist das gemeinsame Nutzen von Tokens über mehrere Prozesse hinweg
// ohnehin kein Anwendungsfall.
func deriveSigningKey(jwtSecret string) []byte {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		// crypto/rand darf praktisch nie fehlschlagen; wenn doch, ist ein
		// sicherer Betrieb unmöglich - hart abbrechen statt schwach signieren.
		panic("auth: instanz-nonce konnte nicht erzeugt werden: " + err.Error())
	}
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	mac.Write(nonce)
	return mac.Sum(nil)
}

// HashPassword hasht ein Klartext-Passwort mit argon2id.
func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2Params)
}

// VerifyPassword prüft ausschließlich die Credentials (ohne Token-
// Ausgabe) und liefert den User. Grundlage des zweistufigen Logins:
// der Controller entscheidet danach, ob 2FA nötig ist.
func (s *AuthService) VerifyPassword(username, password string) (*domain.User, error) {
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

// Login prüft Credentials und liefert bei Erfolg ein signiertes JWT
// samt User (Ein-Schritt-Login ohne 2FA).
func (s *AuthService) Login(username, password string) (string, *domain.User, error) {
	user, err := s.VerifyPassword(username, password)
	if err != nil {
		return "", nil, err
	}
	token, err := s.CompleteLogin(user)
	if err != nil {
		return "", nil, err
	}
	return token, user, nil
}

// CompleteLogin gibt das finale JWT aus und aktualisiert den
// Login-Zeitstempel. Nach erfolgreicher Passwort- (und ggf. 2FA-)Prüfung.
func (s *AuthService) CompleteLogin(user *domain.User) (string, error) {
	token, err := s.IssueToken(user)
	if err != nil {
		return "", err
	}
	now := time.Now()
	if err := s.users.UpdateFields(user.ID, map[string]any{"last_login_at": now}); err != nil {
		return "", err
	}
	return token, nil
}

// IssueToken erzeugt das reguläre Access-Token für einen User.
func (s *AuthService) IssueToken(user *domain.User) (string, error) {
	return s.sign(Claims{Username: user.Username}, user.ID, s.tokenTTL())
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
	if !claims.TwoFAPending {
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

// ValidateToken parst und validiert ein reguläres JWT und lädt den User
// frisch aus der DB (inkl. Rollen/Permissions für RBAC). 2FA-Challenge-
// Tokens (TwoFAPending) werden hier abgelehnt - sie dürfen keine
// regulären API-Zugriffe autorisieren.
// tokenHash ist der Schlüssel der Widerrufsliste (nie der Token selbst).
func tokenHash(tokenString string) string {
	sum := sha256.Sum256([]byte(tokenString))
	return hex.EncodeToString(sum[:])
}

// RevokeToken entwertet GENAU dieses Token bis zu seinem regulären Ablauf
// (Logout, R2-059). Abgelaufene Einträge werden bei jedem Aufruf
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

func (s *AuthService) ValidateToken(tokenString string) (*domain.User, error) {
	claims, err := s.parse(tokenString)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if s.tokenRevoked(tokenString) {
		return nil, ErrInvalidToken
	}
	if claims.TwoFAPending {
		return nil, ErrInvalidToken
	}
	return s.userFromClaims(claims)
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
