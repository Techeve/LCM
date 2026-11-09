package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// ProfileBlockController bedient die Regelbausteine, aus denen sich
// Berechtigungsprofile zusammensetzen lassen.
type ProfileBlockController struct {
	blocks *services.ProfileBlockService
}

func NewProfileBlockController(blocks *services.ProfileBlockService) *ProfileBlockController {
	return &ProfileBlockController{blocks: blocks}
}

func mapBlockError(err error) error {
	if errors.Is(err, repositories.ErrNotFound) {
		return errBlockNotFound
	}
	if rootEquivalent, ok := rootEquivalentError(err); ok {
		return rootEquivalent
	}
	if mapped := matchSentinel(err, blockErrors); mapped != nil {
		return mapped
	}
	// Die Prüfung einer Variante liefert einen umschließenden Fehler mit
	// Fundstelle („variante apt, …") - der gehört als Fehleingabe an den
	// Client, nicht als Serverfehler.
	if mapped := matchSentinel(err, variantErrors); mapped != nil {
		return mapped.With("detail", err.Error())
	}
	return err
}

type blockRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	// Englische Fassung - optional; leer heißt Rückfall auf die deutsche.
	NameEN        string `json:"name_en"`
	DescriptionEN string `json:"description_en"`
	Params        string `json:"params"`
	Variants      []struct {
		Family       string `json:"family"`
		SudoCommands string `json:"sudo_commands"`
		RunAs        string `json:"run_as"`
		EditPaths    string `json:"edit_paths"`
		PathRules    string `json:"path_rules"`
	} `json:"variants"`
}

func (r *blockRequest) toInput() services.BlockInput {
	in := services.BlockInput{
		Name: r.Name, Slug: r.Slug, Description: r.Description, Params: r.Params,
		NameEN: r.NameEN, DescriptionEN: r.DescriptionEN,
	}
	for _, v := range r.Variants {
		in.Variants = append(in.Variants, domain.ProfileBlockVariant{
			Family: v.Family, SudoCommands: v.SudoCommands, RunAs: v.RunAs,
			EditPaths: v.EditPaths, PathRules: v.PathRules,
		})
	}
	return in
}

// List - GET /api/v1/profile-blocks (profiles:read)
func (ctrl *ProfileBlockController) List(c fiber.Ctx) error {
	blocks, err := ctrl.blocks.List()
	if err != nil {
		return err
	}
	return c.JSON(blocks)
}

// Usage - GET /api/v1/profile-blocks/:id/usage (profiles:read)
func (ctrl *ProfileBlockController) Usage(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	names, err := ctrl.blocks.Usage(id)
	if err != nil {
		return mapBlockError(err)
	}
	return c.JSON(fiber.Map{"profiles": names})
}

// Preview - POST /api/v1/profile-blocks/:id/preview (profiles:read)
//
// Zeigt die Zeilen, die der Baustein für eine Distributions-Familie ergibt -
// bevor sie auf einem Server landen.
func (ctrl *ProfileBlockController) Preview(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Family string `json:"family"`
		Values string `json:"values"`
	}
	if err := bindBody(c, &req); err != nil {
		return err
	}
	if err := checkLine("family", req.Family, maxNameLen); err != nil {
		return err
	}
	if err := checkText("values", req.Values, maxDescriptionLen); err != nil {
		return err
	}
	if req.Family == "" {
		req.Family = domain.BlockFamilyAll
	}
	lines, err := ctrl.blocks.RenderPreview(id, req.Family, req.Values)
	if err != nil {
		if mapped := matchSentinel(err, blockErrors); mapped != nil {
			return mapped
		}
		return errBlockRenderFailed.With("detail", err.Error())
	}
	return c.JSON(fiber.Map{"lines": lines})
}

// Create - POST /api/v1/profile-blocks (profiles:write)
func (ctrl *ProfileBlockController) Create(c fiber.Ctx) error {
	var req blockRequest
	if err := bindBody(c, &req); err != nil {
		return err
	}
	if err := checkBlockRequest(req); err != nil {
		return err
	}
	block, err := ctrl.blocks.Create(req.toInput(), actor(c))
	if err != nil {
		return mapBlockError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(block)
}

// Clone - POST /api/v1/profile-blocks/:id/clone (profiles:write)
func (ctrl *ProfileBlockController) Clone(c fiber.Ctx) error {
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
	block, err := ctrl.blocks.Clone(id, req.Slug, req.Name, actor(c))
	if err != nil {
		return mapBlockError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(block)
}

// Update - PATCH /api/v1/profile-blocks/:id (profiles:write)
func (ctrl *ProfileBlockController) Update(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req blockRequest
	if err := bindBody(c, &req); err != nil {
		return err
	}
	if err := checkBlockRequest(req); err != nil {
		return err
	}
	block, err := ctrl.blocks.Update(id, req.toInput(), actor(c))
	if err != nil {
		return mapBlockError(err)
	}
	return c.JSON(block)
}

// Delete - DELETE /api/v1/profile-blocks/:id (profiles:write)
func (ctrl *ProfileBlockController) Delete(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.blocks.Delete(id, actor(c)); err != nil {
		return mapBlockError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// checkBlockRequest prüft Namen, Beschreibungen, Parameter und jede Variante.
// Die Variantentexte sind sudoers-Zeilen und Pfade: der Service prüft ihre
// Bedeutung (domain.ValidateBlockVariant), hier geht es um Länge und
// Steuerzeichen.
func checkBlockRequest(req blockRequest) error {
	if err := checkLines(lineField{"name", req.Name, maxNameLen}, lineField{"slug", req.Slug, maxNameLen}, lineField{"name_en", req.NameEN, maxNameLen}, lineField{"params", req.Params, maxCommandLen}); err != nil {
		return err
	}
	if err := checkText("description", req.Description, maxDescriptionLen); err != nil {
		return err
	}
	if err := checkText("description_en", req.DescriptionEN, maxDescriptionLen); err != nil {
		return err
	}
	if err := checkCount("variants", len(req.Variants), maxItems); err != nil {
		return err
	}
	for _, v := range req.Variants {
		if err := checkLines(lineField{"variants.family", v.Family, maxNameLen}, lineField{"variants.run_as", v.RunAs, maxNameLen}); err != nil {
			return err
		}
		for _, f := range []lineField{
			{"variants.sudo_commands", v.SudoCommands, maxScriptLen},
			{"variants.edit_paths", v.EditPaths, maxScriptLen},
			{"variants.path_rules", v.PathRules, maxScriptLen},
		} {
			if err := checkText(f.name, f.value, f.max); err != nil {
				return err
			}
		}
	}
	return nil
}
