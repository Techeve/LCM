package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// PrivilegeProfileController bedient die Berechtigungsprofile für
// Linux-Benutzer.
type PrivilegeProfileController struct {
	profiles *services.PrivilegeProfileService
}

func NewPrivilegeProfileController(profiles *services.PrivilegeProfileService) *PrivilegeProfileController {
	return &PrivilegeProfileController{profiles: profiles}
}

// mapProfileError bildet Eingabefehler auf 422 ab - sie sind Fehleingaben
// des Betreibers, keine Serverfehler, und ihre Meldung erklärt jeweils, warum
// die Regel abgelehnt wurde.
func mapProfileError(err error) error {
	if errors.Is(err, repositories.ErrNotFound) {
		return errProfileNotFound
	}
	if rootEquivalent, ok := rootEquivalentError(err); ok {
		return rootEquivalent
	}
	return mapSentinel(err, profileErrors)
}

// profileRequest ist die Eingabe für Anlegen und Ändern.
type profileRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"` // nur beim Anlegen ausgewertet
	Description string `json:"description"`
	AccountType string `json:"account_type"`

	SudoRules []struct {
		Command             string `json:"command"`
		RunAs               string `json:"run_as"`
		RequirePassword     bool   `json:"require_password"`
		AllowRootEquivalent bool   `json:"allow_root_equivalent"`
	} `json:"sudo_rules"`
	EditRules []struct {
		Path string `json:"path"`
	} `json:"edit_rules"`
	PathRules []struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
	} `json:"path_rules"`
	// BlockUses sind die eingesetzten Regelbausteine mit ihren Parametern.
	// Fehlten sie hier, verlöre jedes Speichern die Bausteine des Profils.
	BlockUses []struct {
		BlockID uint   `json:"block_id"`
		Values  string `json:"values"`
	} `json:"block_uses"`
}

// toInput überführt den Request in die Service-Eingabe.
func (r *profileRequest) toInput() services.ProfileInput {
	in := services.ProfileInput{
		Name: r.Name, Slug: r.Slug, Description: r.Description, AccountType: r.AccountType,
	}
	for _, rule := range r.SudoRules {
		in.SudoRules = append(in.SudoRules, domain.ProfileSudoRule{
			Command: rule.Command, RunAs: rule.RunAs,
			RequirePassword: rule.RequirePassword, AllowRootEquivalent: rule.AllowRootEquivalent,
		})
	}
	for _, rule := range r.EditRules {
		in.EditRules = append(in.EditRules, domain.ProfileEditRule{Path: rule.Path})
	}
	for _, rule := range r.PathRules {
		in.PathRules = append(in.PathRules, domain.ProfilePathRule{Path: rule.Path, Mode: rule.Mode})
	}
	for _, use := range r.BlockUses {
		in.BlockUses = append(in.BlockUses, domain.ProfileBlockUse{BlockID: use.BlockID, Values: use.Values})
	}
	return in
}

// List - GET /api/v1/privilege-profiles (profiles:read)
func (ctrl *PrivilegeProfileController) List(c fiber.Ctx) error {
	profiles, err := ctrl.profiles.List()
	if err != nil {
		return err
	}
	return c.JSON(profiles)
}

// Get - GET /api/v1/privilege-profiles/:id (profiles:read)
func (ctrl *PrivilegeProfileController) Get(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	profile, err := ctrl.profiles.Get(id)
	if err != nil {
		return mapProfileError(err)
	}
	return c.JSON(profile)
}

// Create - POST /api/v1/privilege-profiles (profiles:write)
func (ctrl *PrivilegeProfileController) Create(c fiber.Ctx) error {
	var req profileRequest
	if err := bindBody(c, &req); err != nil {
		return err
	}
	if err := checkProfileRequest(req); err != nil {
		return err
	}
	profile, err := ctrl.profiles.Create(req.toInput(), actor(c))
	if err != nil {
		return mapProfileError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(profile)
}

// Clone - POST /api/v1/privilege-profiles/:id/clone (profiles:write)
// Legt eine änderbare Kopie an - der Weg, ein mitgeliefertes Profil
// anzupassen.
func (ctrl *PrivilegeProfileController) Clone(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := bindBody(c, &req); err != nil {
		return err
	}
	if err := checkLines(lineField{"slug", req.Slug, maxNameLen}, lineField{"name", req.Name, maxNameLen}); err != nil {
		return err
	}
	profile, err := ctrl.profiles.Clone(id, req.Slug, req.Name, actor(c))
	if err != nil {
		return mapProfileError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(profile)
}

// Update - PATCH /api/v1/privilege-profiles/:id (profiles:write)
func (ctrl *PrivilegeProfileController) Update(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req profileRequest
	if err := bindBody(c, &req); err != nil {
		return err
	}
	if err := checkProfileRequest(req); err != nil {
		return err
	}
	profile, err := ctrl.profiles.Update(id, req.toInput(), actor(c))
	if err != nil {
		return mapProfileError(err)
	}
	return c.JSON(profile)
}

// Delete - DELETE /api/v1/privilege-profiles/:id (profiles:write)
func (ctrl *PrivilegeProfileController) Delete(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.profiles.Delete(id, actor(c)); err != nil {
		return mapProfileError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// checkProfileRequest prüft Namen, Beschreibung und jede Regelzeile. Die
// Regeln landen in sudoers - der Service prüft ihre Bedeutung, hier geht es um
// Länge und Steuerzeichen.
func checkProfileRequest(req profileRequest) error {
	if err := checkLines(lineField{"name", req.Name, maxNameLen}, lineField{"slug", req.Slug, maxNameLen}, lineField{"account_type", req.AccountType, maxNameLen}); err != nil {
		return err
	}
	if err := checkText("description", req.Description, maxDescriptionLen); err != nil {
		return err
	}
	if err := checkCount("sudo_rules", len(req.SudoRules), maxItems); err != nil {
		return err
	}
	if err := checkCount("edit_rules", len(req.EditRules), maxItems); err != nil {
		return err
	}
	if err := checkCount("path_rules", len(req.PathRules), maxItems); err != nil {
		return err
	}
	if err := checkCount("block_uses", len(req.BlockUses), maxItems); err != nil {
		return err
	}
	for _, r := range req.BlockUses {
		if err := checkText("block_uses.values", r.Values, maxDescriptionLen); err != nil {
			return err
		}
	}
	for _, r := range req.SudoRules {
		if err := checkLines(lineField{"sudo_rules.command", r.Command, maxCommandLen}, lineField{"sudo_rules.run_as", r.RunAs, maxNameLen}); err != nil {
			return err
		}
	}
	for _, r := range req.EditRules {
		if err := checkLine("edit_rules.path", r.Path, maxPathLen); err != nil {
			return err
		}
	}
	for _, r := range req.PathRules {
		if err := checkLines(lineField{"path_rules.path", r.Path, maxPathLen}, lineField{"path_rules.mode", r.Mode, maxNameLen}); err != nil {
			return err
		}
	}
	return nil
}
