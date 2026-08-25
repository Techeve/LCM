package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

var (
	// ErrProfileBuiltin: Die mitgelieferten Profile bilden den Zustand ab,
	// den es vor den Profilen schon gab. Wären sie änderbar, änderte sich
	// unter der Hand, was „sudo" für alle bestehenden Benutzer bedeutet.
	ErrProfileBuiltin = errors.New("mitgelieferte profile lassen sich nicht ändern oder löschen - für eigene Regeln ein neues Profil anlegen")
	// ErrProfileSlugTaken meldet einen bereits vergebenen Slug.
	ErrProfileSlugTaken = errors.New("dieser slug ist bereits vergeben")
	// ErrProfileNameTaken meldet einen bereits vergebenen Namen.
	ErrProfileNameTaken = errors.New("dieser name ist bereits vergeben")
)

// PrivilegeProfileService verwaltet die Berechtigungsprofile.
type PrivilegeProfileService struct {
	repo  *repositories.PrivilegeProfileRepository
	audit *AuditService
}

func NewPrivilegeProfileService(repo *repositories.PrivilegeProfileRepository, audit *AuditService) *PrivilegeProfileService {
	return &PrivilegeProfileService{repo: repo, audit: audit}
}

// ProfileInput ist die Eingabe für Anlegen und Ändern eines Profils.
//
// GrantsFullRoot fehlt hier bewusst: Uneingeschränkte Root-Rechte gibt es nur
// im mitgelieferten Voll-Administrator. Wäre das Feld eingebbar, ließe sich
// die ganze Feinsteuerung mit einem Häkchen aushebeln.
type ProfileInput struct {
	Name        string
	Slug        string // nur beim Anlegen ausgewertet
	Description string
	AccountType string
	SudoRules   []domain.ProfileSudoRule
	EditRules   []domain.ProfileEditRule
	PathRules   []domain.ProfilePathRule
	BlockUses   []domain.ProfileBlockUse
}

func (s *PrivilegeProfileService) List() ([]domain.PrivilegeProfile, error) {
	return s.repo.FindAll()
}

func (s *PrivilegeProfileService) Get(id uint) (*domain.PrivilegeProfile, error) {
	return s.repo.FindByID(id)
}

func (s *PrivilegeProfileService) Create(in ProfileInput, actor string) (*domain.PrivilegeProfile, error) {
	if !domain.ValidProfileSlug(in.Slug) {
		return nil, domain.ErrProfileSlugInvalid
	}
	if err := s.checkUnique(in.Slug, in.Name, 0); err != nil {
		return nil, err
	}
	profile := &domain.PrivilegeProfile{Slug: in.Slug}
	if err := applyProfileInput(profile, in); err != nil {
		return nil, err
	}
	if err := s.repo.Create(profile); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "privilege-profile.create", "privilege-profile", profile.ID, auditProfileSummary(profile))
	return s.repo.FindByID(profile.ID)
}

func (s *PrivilegeProfileService) Update(id uint, in ProfileInput, actor string) (*domain.PrivilegeProfile, error) {
	profile, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if profile.Builtin {
		return nil, ErrProfileBuiltin
	}
	if err := s.checkUnique("", in.Name, id); err != nil {
		return nil, err
	}
	if err := applyProfileInput(profile, in); err != nil {
		return nil, err
	}
	if err := s.repo.Update(profile); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "privilege-profile.update", "privilege-profile", id, auditProfileSummary(profile))
	return s.repo.FindByID(id)
}

// Clone legt eine änderbare Kopie eines Profils an - der vorgesehene Weg,
// ein mitgeliefertes Profil anzupassen, und die Abkürzung für ein neues
// Profil, das einem vorhandenen ähnelt.
//
// Die Regeln werden ohne ihre Schlüssel übernommen: Sie hängen am Quellprofil,
// und mitkopiert würde die neue Kopie die Regeln des Originals überschreiben.
func (s *PrivilegeProfileService) Clone(id uint, slug, name, actor string) (*domain.PrivilegeProfile, error) {
	source, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	return s.Create(ProfileInput{
		Name: name, Slug: slug, Description: copyDescription(source),
		AccountType: source.AccountType,
		SudoRules:   freshSudoRules(source.SudoRules),
		EditRules:   freshEditRules(source.EditRules),
		PathRules:   freshPathRules(source.PathRules),
		BlockUses:   freshBlockUses(source.BlockUses),
	}, actor)
}

// copyDescription haelt die Beschreibung der Kopie ehrlich.
//
// Uneingeschraenkte Root-Rechte traegt nur das mitgelieferte Profil; die
// Kopie bekommt sie NICHT (GrantsFullRoot ist kein Eingabefeld, siehe
// ProfileInput). Bliebe die Beschreibung des Originals stehen, verspraeche
// die Kopie genau das, was sie nicht haelt.
func copyDescription(source *domain.PrivilegeProfile) string {
	if !source.GrantsFullRoot {
		return source.Description
	}
	return "Kopie von „" + source.Name + "“ - OHNE dessen uneingeschränkte Root-Rechte. " +
		"Die erlaubten Kommandos hier einzeln als Regeln eintragen."
}

// freshSudoRules & Co. lösen die Regeln vom Quellprofil: ohne eigene ID und
// ohne Verweis auf das Original legt GORM sie als neue Zeilen an.
func freshSudoRules(in []domain.ProfileSudoRule) []domain.ProfileSudoRule {
	out := make([]domain.ProfileSudoRule, 0, len(in))
	for _, r := range in {
		r.ID, r.ProfileID = 0, 0
		out = append(out, r)
	}
	return out
}

func freshEditRules(in []domain.ProfileEditRule) []domain.ProfileEditRule {
	out := make([]domain.ProfileEditRule, 0, len(in))
	for _, r := range in {
		r.ID, r.ProfileID = 0, 0
		out = append(out, r)
	}
	return out
}

func freshPathRules(in []domain.ProfilePathRule) []domain.ProfilePathRule {
	out := make([]domain.ProfilePathRule, 0, len(in))
	for _, r := range in {
		r.ID, r.ProfileID = 0, 0
		out = append(out, r)
	}
	return out
}

func freshBlockUses(in []domain.ProfileBlockUse) []domain.ProfileBlockUse {
	out := make([]domain.ProfileBlockUse, 0, len(in))
	for _, u := range in {
		u.ID, u.ProfileID = 0, 0
		out = append(out, u)
	}
	return out
}

func (s *PrivilegeProfileService) Delete(id uint, actor string) error {
	profile, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if profile.Builtin {
		return ErrProfileBuiltin
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "privilege-profile.delete", "privilege-profile", id, profile.Name)
	return nil
}

// checkUnique prüft Slug und Name auf Dopplung. Ein leerer Slug wird
// übersprungen (beim Ändern ist er unveränderlich), exceptID schließt das
// Profil aus, das gerade bearbeitet wird.
func (s *PrivilegeProfileService) checkUnique(slug, name string, exceptID uint) error {
	if slug != "" {
		if _, err := s.repo.FindBySlug(slug); err == nil {
			return ErrProfileSlugTaken
		} else if !errors.Is(err, repositories.ErrNotFound) {
			return err
		}
	}
	existing, err := s.repo.FindAll()
	if err != nil {
		return err
	}
	for i := range existing {
		if existing[i].ID != exceptID && strings.EqualFold(existing[i].Name, strings.TrimSpace(name)) {
			return ErrProfileNameTaken
		}
	}
	return nil
}

// applyProfileInput übernimmt die Eingabe nach Prüfung in das Profil.
func applyProfileInput(profile *domain.PrivilegeProfile, in ProfileInput) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return domain.ErrProfileNameEmpty
	}
	sudoRules, err := checkedSudoRules(in.SudoRules)
	if err != nil {
		return err
	}
	editRules, err := checkedEditRules(in.EditRules)
	if err != nil {
		return err
	}
	pathRules, err := checkedPathRules(in.PathRules)
	if err != nil {
		return err
	}
	blockUses, err := checkedBlockUses(in.BlockUses)
	if err != nil {
		return err
	}
	accountType := in.AccountType
	if accountType == "" {
		accountType = domain.AccountTypeShell
	}
	if !domain.ValidAccountType(accountType) {
		return fmt.Errorf("ungültiger kontotyp %q - erlaubt: shell, sftp", accountType)
	}
	profile.AccountType = accountType
	profile.Name = name
	profile.Description = strings.TrimSpace(in.Description)
	profile.SudoRules, profile.EditRules, profile.PathRules = sudoRules, editRules, pathRules
	profile.BlockUses = blockUses
	return nil
}

// checkedSudoRules prüft und normalisiert die Kommando-Regeln.
func checkedSudoRules(rules []domain.ProfileSudoRule) ([]domain.ProfileSudoRule, error) {
	out := make([]domain.ProfileSudoRule, 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		rule.Command = domain.NormalizeSudoCommand(rule.Command)
		if err := domain.ValidateSudoCommand(rule.Command, rule.AllowRootEquivalent); err != nil {
			return nil, err
		}
		rule.RunAs = strings.TrimSpace(rule.RunAs)
		if rule.RunAs == "" {
			rule.RunAs = "root"
		}
		if !domain.ValidLinuxUsername(rule.RunAs) {
			return nil, fmt.Errorf("ungültiger zielbenutzer %q", rule.RunAs)
		}
		if seen[rule.Command] {
			continue // dieselbe Zeile zweimal ist keine zweite Erlaubnis
		}
		seen[rule.Command] = true
		out = append(out, rule)
	}
	return out, nil
}

// checkedEditRules prüft die per sudoedit bearbeitbaren Dateien.
func checkedEditRules(rules []domain.ProfileEditRule) ([]domain.ProfileEditRule, error) {
	out := make([]domain.ProfileEditRule, 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		rule.Path = strings.TrimSpace(rule.Path)
		if err := domain.ValidateEditPath(rule.Path); err != nil {
			return nil, err
		}
		if seen[rule.Path] {
			continue
		}
		seen[rule.Path] = true
		out = append(out, rule)
	}
	return out, nil
}

// checkedPathRules prüft die Verzeichnisrechte. Derselbe Pfad zweimal wäre
// mehrdeutig - welcher Modus gälte dann?
func checkedPathRules(rules []domain.ProfilePathRule) ([]domain.ProfilePathRule, error) {
	out := make([]domain.ProfilePathRule, 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		rule.Path = strings.TrimSpace(rule.Path)
		if rule.Mode == "" {
			rule.Mode = domain.PathModeRead
		}
		if err := domain.ValidatePathRule(rule.Path, rule.Mode); err != nil {
			return nil, err
		}
		if seen[rule.Path] {
			return nil, fmt.Errorf("der pfad %q kommt mehrfach vor - je Pfad ist nur eine Regel möglich", rule.Path)
		}
		seen[rule.Path] = true
		out = append(out, rule)
	}
	return out, nil
}

// checkedBlockUses prüft die eingesetzten Parameterwerte. Sie landen in einer
// sudoers-Zeile - ein Leerzeichen erzeugte dort ein zusätzliches Argument, ein
// Sonderzeichen etwas ganz anderes.
func checkedBlockUses(uses []domain.ProfileBlockUse) ([]domain.ProfileBlockUse, error) {
	out := make([]domain.ProfileBlockUse, 0, len(uses))
	seen := make(map[uint]bool, len(uses))
	for _, use := range uses {
		if use.BlockID == 0 || seen[use.BlockID] {
			continue
		}
		for name, value := range domain.ParseBlockValues(use.Values) {
			if !domain.ValidBlockParamName(name) {
				return nil, domain.ErrBlockParamName
			}
			if !domain.ValidBlockParamValue(value) {
				return nil, fmt.Errorf("%w: %q", domain.ErrBlockParamValue, value)
			}
		}
		seen[use.BlockID] = true
		out = append(out, domain.ProfileBlockUse{BlockID: use.BlockID, Values: use.Values})
	}
	return out, nil
}

// auditProfileSummary beschreibt ein Profil fürs Audit-Log. Ein Profil vergibt
// Root-nahe Rechte - was es umfasst, gehört in die manipulationssichere Kette
// und nicht nur in die Datenbankzeile, die sich später ändern kann.
func auditProfileSummary(p *domain.PrivilegeProfile) string {
	return fmt.Sprintf("%s (%s): %d Kommandos, %d Dateien, %d Verzeichnisrechte, %d Bausteine",
		p.Name, p.Slug, len(p.SudoRules), len(p.EditRules), len(p.PathRules), len(p.BlockUses))
}
