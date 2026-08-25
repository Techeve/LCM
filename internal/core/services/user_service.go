package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

var (
	ErrUsernameTaken = errors.New("benutzername ist bereits vergeben")
	ErrEmailTaken    = errors.New("e-mail-adresse ist bereits einem anderen benutzer zugeordnet")
	// ErrWeakPassword ist der Sammelfehler der Passwort-Policy. Die konkreten
	// Regelverstöße reist ein *PasswordPolicyError mit (siehe
	// password_policy.go) - errors.Is(err, ErrWeakPassword) bleibt wahr.
	ErrWeakPassword     = errors.New("passwort erfüllt die sicherheitsanforderungen nicht")
	ErrProtectedUser    = errors.New("system-benutzer können nicht verändert oder gelöscht werden")
	ErrLastAdmin        = errors.New("der letzte aktive Administrator kann nicht gesperrt oder entrechtet werden - sonst sperrt sich die Verwaltung selbst aus")
	ErrUnknownRole      = errors.New("unbekannte Rolle")
	ErrInvalidUsername  = errors.New("benutzername muss 3-32 Zeichen lang sein (a-z, 0-9, _, -)")
	ErrReservedUsername = errors.New("dieser benutzername ist reserviert und kann nicht angelegt werden")
)

// checkEmailFree prüft, ob eine (nicht-leere) E-Mail-Adresse frei ist bzw.
// bereits dem User selbst gehört - klare Meldung statt DB-Constraint-Fehler.
func (s *UserService) checkEmailFree(email string, selfID uint) error {
	if strings.TrimSpace(email) == "" {
		return nil
	}
	existing, err := s.users.FindByEmail(email)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil
		}
		return err
	}
	if existing.ID != selfID {
		return ErrEmailTaken
	}
	return nil
}

// UserService enthält die Business-Logik rund um User und Rollen.
type UserService struct {
	users *repositories.UserRepository
	roles *repositories.RoleRepository
	// audit protokolliert JEDE Benutzer-, Rollen- und Passwort-Operation.
	// Genau die Operationen, die Rechte vergeben, waren die einzigen ohne
	// Audit-Spur (R2-048) - für ein System mit Root-Zugriff auf die ganze
	// Flotte ein Ausschlusskriterium. Optional nil (Tests).
	audit *AuditService
}

func NewUserService(users *repositories.UserRepository, roles *repositories.RoleRepository) *UserService {
	return &UserService{users: users, roles: roles}
}

// WithAudit verdrahtet das Audit-Log (R2-048).
func (s *UserService) WithAudit(audit *AuditService) *UserService {
	s.audit = audit
	return s
}

// logAudit schreibt einen Audit-Eintrag, sofern verdrahtet.
func (s *UserService) logAudit(actor, action string, id uint, details string) {
	if s.audit != nil {
		s.audit.Log(actor, action, "user", id, details)
	}
}

// CreateUser legt einen neuen User mit argon2id-gehashtem Passwort an
// und weist ihm die angegebenen Rollen zu.
func (s *UserService) CreateUser(username, email, password, firstName, lastName string, roleNames []string, actor string) (*domain.User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !validUsername(username) {
		return nil, ErrInvalidUsername
	}
	// Die geschützten Basis-Konten (system, admin) dürfen nicht als neue
	// Benutzer angelegt werden - sie werden ausschließlich beim Seeding erzeugt.
	if username == domain.SystemUsername || username == domain.AdminUsername {
		return nil, ErrReservedUsername
	}
	if err := EnforcePasswordPolicy(password, PasswordIdentity{
		Username: username, Email: email, FirstName: firstName, LastName: lastName,
	}); err != nil {
		return nil, err
	}
	if _, err := s.users.FindByUsername(username); err == nil {
		return nil, ErrUsernameTaken
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, err
	}
	email = strings.TrimSpace(email)
	if err := s.checkEmailFree(email, 0); err != nil {
		return nil, err
	}

	roles, err := s.resolveRoles(roleNames)
	if err != nil {
		return nil, err
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("passwort hashen: %w", err)
	}

	user := &domain.User{
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		FirstName:    firstName,
		LastName:     lastName,
		Active:       true,
		Roles:        roles,
	}
	if err := s.users.Create(user); err != nil {
		return nil, err
	}
	s.logAudit(actor, "user.create", user.ID, username+" - Rollen: "+strings.Join(roleNames, ","))
	return s.users.FindByID(user.ID)
}

// UpdateUserRoles ersetzt die Rollen eines Users. System-User sind geschützt.
func (s *UserService) UpdateUserRoles(userID uint, roleNames []string, actor string) (*domain.User, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if user.IsSystem {
		return nil, ErrProtectedUser
	}
	// Dem letzten aktiven Admin die admin-Rolle zu nehmen sperrt die
	// Verwaltung aus - abweisen, wenn die neue Rollenmenge kein admin mehr
	// enthält (R2-036-verwandt).
	hasAdmin := false
	for _, n := range roleNames {
		if n == domain.RoleAdmin {
			hasAdmin = true
		}
	}
	if !hasAdmin {
		if last, err := s.isLastActiveAdmin(user); err != nil {
			return nil, err
		} else if last {
			return nil, ErrLastAdmin
		}
	}
	roles, err := s.resolveRoles(roleNames)
	if err != nil {
		return nil, err
	}
	if err := s.users.SetRoles(user, roles); err != nil {
		return nil, err
	}
	s.logAudit(actor, "user.set-roles", userID, user.Username+" - Rollen: "+strings.Join(roleNames, ","))
	return s.users.FindByID(userID)
}

// UpdateProfile ändert E-Mail, Vor- und Nachnamen eines Users. System-User
// sind geschützt.
func (s *UserService) UpdateProfile(userID uint, email, firstName, lastName string, actor string) (*domain.User, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if user.IsSystem {
		return nil, ErrProtectedUser
	}
	email = strings.TrimSpace(email)
	if err := s.checkEmailFree(email, userID); err != nil {
		return nil, err
	}
	if err := s.users.UpdateFields(userID, map[string]any{
		"email":      email,
		"first_name": firstName,
		"last_name":  lastName,
	}); err != nil {
		return nil, err
	}
	s.logAudit(actor, "user.update-profile", userID, user.Username)
	return s.users.FindByID(userID)
}

// CheckPassword prüft ein Klartext-Passwort gegen den gespeicherten Hash
// eines Users - für die Re-Authentifizierung beim Self-Service-Passwortwechsel.
func (s *UserService) CheckPassword(userID uint, password string) (bool, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return false, err
	}
	return argon2id.ComparePasswordAndHash(password, user.PasswordHash)
}

// ResetPassword setzt ein neues Passwort (argon2id-gehasht). System-User
// sind geschützt.
//
// selfService unterscheidet die beiden Fälle: Setzt der Nutzer sein eigenes
// Passwort, ist der Wechselzwang erledigt. Setzt ein ADMIN das Passwort eines
// fremden Kontos (Helpdesk-Fall), kennt ein Dritter das Passwort - dann bleibt
// der Wechselzwang bestehen, damit der Kontoinhaber es beim nächsten Login
// selbst ersetzt (durchgesetzt von middlewares.AccountRemediation).
func (s *UserService) ResetPassword(userID uint, newPassword string, selfService bool, actor string) error {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if user.IsSystem {
		return ErrProtectedUser
	}
	if err := EnforcePasswordPolicy(newPassword, PasswordIdentity{
		Username: user.Username, Email: user.Email,
		FirstName: user.FirstName, LastName: user.LastName,
	}); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("passwort hashen: %w", err)
	}
	if err := s.users.UpdateFields(userID, map[string]any{
		"password_hash":        hash,
		"must_change_password": !selfService,
		// Entwertet alle bestehenden Sessions und API-Keys des Kontos.
		"password_changed_at": time.Now(),
	}); err != nil {
		return err
	}
	kind := "durch Administrator"
	if selfService {
		kind = "Self-Service"
	}
	s.logAudit(actor, "user.reset-password", userID, user.Username+" ("+kind+")")
	return nil
}

// SetActive sperrt (active=false) oder entsperrt einen Benutzer, ohne ihn
// zu löschen - der Normalfall beim Offboarding (R2-036: active war faktisch
// nur lesbar). System-User und der letzte aktive Admin sind geschützt:
// wer sich selbst oder den letzten Administrator sperrt, sperrt die
// Verwaltung aus.
func (s *UserService) SetActive(userID uint, active bool, actor string) (*domain.User, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if user.IsSystem {
		return nil, ErrProtectedUser
	}
	if !active {
		lastAdmin, err := s.isLastActiveAdmin(user)
		if err != nil {
			return nil, err
		}
		if lastAdmin {
			return nil, ErrLastAdmin
		}
	}
	if err := s.users.UpdateFields(userID, map[string]any{
		"active": active,
		// Sperren entwertet laufende Sessions und API-Keys sofort - sonst
		// bliebe ein gesperrtes Konto über sein noch gültiges Token aktiv.
		"password_changed_at": time.Now(),
	}); err != nil {
		return nil, err
	}
	verb := "gesperrt"
	if active {
		verb = "entsperrt"
	}
	s.logAudit(actor, "user.set-active", userID, user.Username+" ("+verb+")")
	return s.users.FindByID(userID)
}

// isLastActiveAdmin meldet, ob user der EINZIGE noch aktive Benutzer mit
// admin-Rolle ist - dann darf er nicht gesperrt/entrechtet werden.
func (s *UserService) isLastActiveAdmin(user *domain.User) (bool, error) {
	if !user.HasRole(domain.RoleAdmin) {
		return false, nil
	}
	all, err := s.users.FindAll()
	if err != nil {
		return false, err
	}
	for i := range all {
		other := &all[i]
		if other.ID != user.ID && other.Active && !other.IsSystem && other.HasRole(domain.RoleAdmin) {
			return false, nil
		}
	}
	return true, nil
}

func (s *UserService) DeleteUser(userID uint, actor string) error {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if user.IsSystem || user.Username == domain.AdminUsername {
		return ErrProtectedUser
	}
	if err := s.users.Delete(userID); err != nil {
		return err
	}
	s.logAudit(actor, "user.delete", userID, user.Username)
	return nil
}

func (s *UserService) GetUser(userID uint) (*domain.User, error) {
	return s.users.FindByID(userID)
}

func (s *UserService) ListUsers() ([]domain.User, error) {
	return s.users.FindAll()
}

func (s *UserService) ListRoles() ([]domain.Role, error) {
	return s.roles.FindAll()
}

func (s *UserService) resolveRoles(names []string) ([]domain.Role, error) {
	roles := make([]domain.Role, 0, len(names))
	for _, name := range names {
		role, err := s.roles.FindByName(name)
		if err != nil {
			if errors.Is(err, repositories.ErrNotFound) {
				return nil, fmt.Errorf("%w: %s", ErrUnknownRole, name)
			}
			return nil, err
		}
		roles = append(roles, *role)
	}
	return roles, nil
}

func validUsername(name string) bool {
	if len(name) < 3 || len(name) > 32 {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}
