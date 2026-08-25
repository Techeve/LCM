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
	var rootEquivalent *domain.ErrRootEquivalent
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "berechtigungsprofil nicht gefunden")
	case errors.Is(err, services.ErrProfileBuiltin):
		return fiber.NewError(fiber.StatusForbidden, err.Error())
	case errors.Is(err, services.ErrProfileSlugTaken), errors.Is(err, services.ErrProfileNameTaken):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case errors.As(err, &rootEquivalent):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrProfileSlugInvalid), errors.Is(err, domain.ErrProfileNameEmpty),
		errors.Is(err, domain.ErrSudoCommandEmpty), errors.Is(err, domain.ErrSudoCommandRelative),
		errors.Is(err, domain.ErrSudoCommandWildcard), errors.Is(err, domain.ErrSudoCommandMeta),
		errors.Is(err, domain.ErrSudoCommandNoArgs), errors.Is(err, domain.ErrPathRelative),
		errors.Is(err, domain.ErrPathMeta), errors.Is(err, domain.ErrPathProtected),
		errors.Is(err, domain.ErrPathModeInvalid):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	default:
		return err
	}
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
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
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
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
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
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
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
