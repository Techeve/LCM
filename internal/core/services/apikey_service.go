package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

var (
	ErrAPIKeyInvalid = errors.New("ungültiger API-Key")
	ErrInvalidScope  = errors.New("ungültiger Scope (erlaubt: read, readwrite, mcp)")
)

// APIKeyService erzeugt und validiert API-Keys für Service-Kommunikation.
// Der Klartext-Key wird nur bei der Erstellung zurückgegeben; gespeichert
// wird ausschließlich ein SHA-256-Hash.
type APIKeyService struct {
	keys *repositories.APIKeyRepository
	// audit protokolliert Erzeugung und Widerruf - API-Keys SIND vergebene
	// Rechte, ihre Historie war die einzige ohne Audit-Spur (R2-048).
	// Optional nil (Tests).
	audit *AuditService
}

func NewAPIKeyService(keys *repositories.APIKeyRepository) *APIKeyService {
	return &APIKeyService{keys: keys}
}

// WithAudit verdrahtet das Audit-Log (R2-048).
func (s *APIKeyService) WithAudit(audit *AuditService) *APIKeyService {
	s.audit = audit
	return s
}

const apiKeyPrefix = "lcm_" // erleichtert Secret-Scanning

// Create erzeugt einen neuen API-Key im Rechte-Kontext des angegebenen
// Users. scope: domain.APIKeyScopeRead (nur GET/HEAD),
// domain.APIKeyScopeReadWrite oder domain.APIKeyScopeMCP (nur für die
// MCP-Schnittstelle gültig, nie für die REST-API); "" fällt auf readwrite
// zurück. Rückgabe: Klartext-Key (einmalig!) und die gespeicherte Entität.
func (s *APIKeyService) Create(name string, userID uint, scope string, expiresAt *time.Time, actor string) (string, *domain.APIKey, error) {
	if scope == "" {
		scope = domain.APIKeyScopeReadWrite
	}
	if scope != domain.APIKeyScopeRead && scope != domain.APIKeyScopeReadWrite && scope != domain.APIKeyScopeMCP {
		return "", nil, ErrInvalidScope
	}
	plaintext := apiKeyPrefix + config.RandomSecret(32)
	key := &domain.APIKey{
		Name:      name,
		KeyHash:   hashKey(plaintext),
		Prefix:    plaintext[:12],
		UserID:    userID,
		Scope:     scope,
		ExpiresAt: expiresAt,
	}
	if err := s.keys.Create(key); err != nil {
		return "", nil, err
	}
	details := name + " - Scope " + scope
	if expiresAt != nil {
		details += ", läuft ab " + expiresAt.Format("2006-01-02")
	} else {
		details += ", unbefristet"
	}
	if s.audit != nil {
		s.audit.Log(actor, "apikey.create", "apikey", key.ID, details)
	}
	return plaintext, key, nil
}

// Validate prüft einen Klartext-Key und liefert den zugehörigen User
// (inkl. Rollen/Permissions) sowie den Key selbst (für Scope-Prüfung).
func (s *APIKeyService) Validate(plaintext string) (*domain.User, *domain.APIKey, error) {
	key, err := s.keys.FindActiveByHash(hashKey(plaintext))
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, nil, ErrAPIKeyInvalid
		}
		return nil, nil, err
	}
	// Deaktivierte Benutzer: ihre API-Keys müssen sofort ungültig sein -
	// sonst behielte ein ausgeschiedener Mitarbeiter über einen bestehenden
	// Key Zugriff, obwohl seine Browser-Sessions bereits tot sind (der
	// JWT-Pfad prüft user.Active frisch, dieser Pfad tat es bisher nicht).
	if !key.User.Active {
		return nil, nil, ErrAPIKeyInvalid
	}
	// Passwortwechsel entwertet auch API-Keys, die VOR dem Wechsel entstanden
	// sind. Ohne diese Prüfung überlebte ein still angelegter Key genau den
	// Vorgang, mit dem ein Nutzer auf eine Kompromittierung reagiert: alle
	// JWTs sterben, der Key des Angreifers bliebe unbefristet gültig.
	if key.User.PasswordChangedAt != nil && key.CreatedAt.Before(*key.User.PasswordChangedAt) {
		return nil, nil, ErrAPIKeyInvalid
	}
	// Nutzung protokollieren (best effort, Fehler nicht fatal).
	_ = s.keys.TouchLastUsed(key.ID)
	return &key.User, key, nil
}

func (s *APIKeyService) List() ([]domain.APIKey, error) {
	return s.keys.FindAll()
}

// Revoke setzt die zweistufige DELETE-Semantik um (R2-053): der erste Aufruf
// WIDERRUFT den Key (sofort ungültig, Zeile bleibt als Historie sichtbar),
// ein zweiter Aufruf auf dem bereits widerrufenen Key entfernt ihn endgültig.
// So heißt DELETE nie „weg ohne Spur", und Aufräumen bleibt trotzdem möglich.
func (s *APIKeyService) Revoke(id uint, actor string) error {
	key, err := s.keys.FindByID(id)
	if err != nil {
		return err
	}
	if key.Revoked {
		if err := s.keys.Delete(id); err != nil {
			return err
		}
		if s.audit != nil {
			s.audit.Log(actor, "apikey.purge", "apikey", id,
				fmt.Sprintf("%s (%s…) endgültig entfernt - war bereits widerrufen", key.Name, key.Prefix))
		}
		return nil
	}
	if err := s.keys.Revoke(id); err != nil {
		return err
	}
	if s.audit != nil {
		s.audit.Log(actor, "apikey.revoke", "apikey", id,
			fmt.Sprintf("%s (%s…) widerrufen - erneutes Löschen entfernt den Eintrag endgültig", key.Name, key.Prefix))
	}
	return nil
}

func hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
