package services

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"LCM/internal/core/domain"
)

// Browser-Sitzung. Ablauf und Entwertung liegen serverseitig: Der Browser trägt
// nur einen zufälligen, undurchsichtigen Wert (Cookie oder Kopfzeile, je nach
// Einstellung browser_session_mode); alles andere (Nutzer, Beginn, letzte
// Aktivität) steht im Speicher des Prozesses.
//
// Im Speicher, nicht in der Datenbank: Wie schon der Signaturschlüssel der
// 2FA-Challenge (newSigningKey) überlebt eine Sitzung keinen Neustart - das ist
// gewollt (Neuanlage der DB, Rebuild) und braucht damit weder Tabelle noch Migration.

// SessionAbsoluteMax ist die längste Lebensdauer einer Sitzung, egal wie aktiv
// sie genutzt wird. Ist die Leerlauf-Zeit (SessionTTLMinutes) länger
// eingestellt, gilt diese.
const SessionAbsoluteMax = 12 * time.Hour

// Transport sagt, wie der Browser den Sitzungswert mitschickt.
type Transport string

const (
	// TransportCookie: Cookie __Host-lcm_session (nur über HTTPS).
	TransportCookie Transport = "cookie"
	// TransportHeader: Kopfzeile X-LCM-Session, der Wert liegt nur im
	// Arbeitsspeicher des Tabs.
	TransportHeader Transport = "header"
)

// sessionTokenBytes ist die Länge des Sitzungswerts vor der Kodierung.
const sessionTokenBytes = 32

var (
	// ErrInvalidSession deckt unbekannt, abgelaufen, widerrufen und "der
	// Nutzer hat sich seit Beginn verändert" gemeinsam ab.
	ErrInvalidSession = errors.New("ungültige oder abgelaufene Sitzung")
	// ErrInvalidLanguage meldet eine Sprachkennung, die kein BCP-47-Tag ist.
	ErrInvalidLanguage = errors.New("ungültige Sprachkennung")
)

// session ist der serverseitige Zustand einer Browser-Sitzung.
type session struct {
	userID uint
	// transport ist der Weg, für den der Wert ausgestellt wurde. Ein Wert
	// gilt nur über diesen Weg: ein Cookie, das zu einer Kopfzeilen-Sitzung
	// gehört, ist keine Anmeldung - und umgekehrt.
	transport Transport
	created   time.Time
	lastSeen  time.Time
	// rolesSig hält die Rollen-IDs bei Beginn fest. Ändern sich die Rollen
	// des Nutzers, endet die Sitzung - er meldet sich mit den neuen Rechten
	// frisch an.
	rolesSig string
}

// WithClock ersetzt die Uhr (Tests: Ablauf und Verlängerung der Sitzung).
func (s *AuthService) WithClock(now func() time.Time) *AuthService {
	s.now = now
	return s
}

// sessionKey ist der Schlüssel der Sitzungstabelle und zugleich die
// öffentliche Sitzungs-ID (Log, Verbindungslimit): der SHA-256 des
// Sitzungswerts. Aus ihm lässt sich der Wert nicht zurückrechnen.
func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func rolesSignature(user *domain.User) string {
	ids := make([]uint, 0, len(user.Roles))
	for _, r := range user.Roles {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	return fmt.Sprint(ids)
}

// sessionLimits liefert Leerlauf-Zeit (Einstellung "Sitzungsdauer") und
// absolute Obergrenze.
func (s *AuthService) sessionLimits() (idle, absolute time.Duration) {
	idle = s.sessionIdle()
	return idle, max(SessionAbsoluteMax, idle)
}

func (s *AuthService) sessionExpired(sess *session, now time.Time) bool {
	idle, absolute := s.sessionLimits()
	return now.Sub(sess.lastSeen) > idle || now.Sub(sess.created) > absolute
}

// StartSession eröffnet eine Sitzung für einen geprüften Nutzer und liefert
// den Sitzungswert, die öffentliche Sitzungs-ID und die Gesamtlaufzeit (für
// das Cookie-Ablaufdatum).
func (s *AuthService) StartSession(user *domain.User, transport Transport) (token, sessionID string, lifetime time.Duration, err error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", 0, fmt.Errorf("sitzungswert erzeugen: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	sessionID = sessionKey(token)
	now := s.now()
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.pruneSessionsLocked(now)
	s.sessions[sessionID] = &session{
		userID: user.ID, transport: transport, created: now, lastSeen: now, rolesSig: rolesSignature(user),
	}
	_, absolute := s.sessionLimits()
	return token, sessionID, absolute, nil
}

// pruneSessionsLocked räumt abgelaufene Sitzungen auf (Aufrufer hält die
// Sperre) - die Tabelle bleibt so höchstens aktivitätsgroß.
func (s *AuthService) pruneSessionsLocked(now time.Time) {
	for k, sess := range s.sessions {
		if s.sessionExpired(sess, now) {
			delete(s.sessions, k)
		}
	}
}

// ValidateSession prüft einen Sitzungswert, der über diesen Transport kam,
// lädt den Nutzer frisch aus der DB und verlängert die Leerlauf-Frist. Liefert
// die öffentliche Sitzungs-ID. Sitzungen, deren Nutzer sich verändert hat
// (deaktiviert, gelöscht, neues Passwort, andere Rollen), werden dabei beendet.
func (s *AuthService) ValidateSession(token string, transport Transport) (*domain.User, string, error) {
	key := sessionKey(token)
	user, err := s.checkSession(key, transport)
	if err != nil {
		return nil, "", err
	}
	s.touchSession(key)
	return user, key, nil
}

// CheckSession prüft eine Sitzung anhand ihrer öffentlichen ID wie
// ValidateSession, verlängert die Leerlauf-Frist aber nicht (Ereigniskanal:
// eine offene Verbindung ist keine Aktivität).
func (s *AuthService) CheckSession(sessionID string) (*domain.User, error) {
	return s.checkSession(sessionID, "")
}

// checkSession prüft die Sitzung zu dieser ID. Ein leerer Transport lässt
// beide gelten (nur für Aufrufer, die die ID schon über einen Transport
// bewiesen bekamen).
func (s *AuthService) checkSession(key string, transport Transport) (*domain.User, error) {
	sess, ok := s.liveSession(key)
	if !ok || (transport != "" && sess.transport != transport) {
		return nil, ErrInvalidSession
	}
	user, err := s.users.FindByID(sess.userID)
	if err != nil || !sessionMatchesUser(sess, user) {
		s.dropSession(key)
		return nil, ErrInvalidSession
	}
	return user, nil
}

// liveSession liefert eine Kopie der Sitzung, sofern sie nicht abgelaufen ist
// (abgelaufene werden gelöscht).
func (s *AuthService) liveSession(key string) (session, bool) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	sess, ok := s.sessions[key]
	if !ok {
		return session{}, false
	}
	if s.sessionExpired(sess, s.now()) {
		delete(s.sessions, key)
		return session{}, false
	}
	return *sess, true
}

func sessionMatchesUser(sess session, user *domain.User) bool {
	if !user.Active || user.IsSystem {
		return false
	}
	if user.PasswordChangedAt != nil && user.PasswordChangedAt.After(sess.created) {
		return false
	}
	return rolesSignature(user) == sess.rolesSig
}

func (s *AuthService) touchSession(key string) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if sess, ok := s.sessions[key]; ok {
		sess.lastSeen = s.now()
	}
}

func (s *AuthService) dropSession(key string) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	delete(s.sessions, key)
}

// RevokeSession beendet die Sitzung zu diesem Sitzungswert. Ein unbekannter
// Wert ist kein Fehler.
func (s *AuthService) RevokeSession(token string) {
	s.dropSession(sessionKey(token))
}

// RevokeSessionID beendet die Sitzung mit dieser öffentlichen ID (Logout).
func (s *AuthService) RevokeSessionID(sessionID string) {
	s.dropSession(sessionID)
}

// RevokeAllSessions beendet alle Browser-Sitzungen aller Benutzer (Wechsel von
// browser_session_mode). API-Keys sind keine Sitzungen und bleiben gültig.
func (s *AuthService) RevokeAllSessions() {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	clear(s.sessions)
}

// csrfKey leitet das CSRF-Geheimnis aus dem Signaturschlüssel ab (eigene
// Verwendung, damit das Token nicht als JWT-Signatur taugt).
func (s *AuthService) csrfKey() []byte {
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte("lcm-csrf-v1"))
	return mac.Sum(nil)
}

// CSRFToken liefert das zur Sitzung gehörende CSRF-Token: HMAC der
// Sitzungs-ID. Wer die Sitzung nicht besitzt, kann es nicht berechnen.
func (s *AuthService) CSRFToken(sessionID string) string {
	mac := hmac.New(sha256.New, s.csrfKey())
	mac.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// CSRFValid vergleicht ein eingereichtes Token in konstanter Zeit.
func (s *AuthService) CSRFValid(sessionID, got string) bool {
	want := s.CSRFToken(sessionID)
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// languageTag: BCP 47 in der hier benötigten Form, etwa "de", "en-GB".
var languageTag = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// ValidLanguage prüft eine Sprachkennung (2-10 Zeichen, BCP-47-Form).
func ValidLanguage(lang string) bool {
	return len(lang) >= 2 && len(lang) <= 10 && languageTag.MatchString(lang)
}

// SetLanguage speichert die Oberflächensprache am Benutzerkonto.
func (s *AuthService) SetLanguage(userID uint, lang string) error {
	if !ValidLanguage(lang) {
		return ErrInvalidLanguage
	}
	return s.users.UpdateFields(userID, map[string]any{"language": lang})
}
