package controllers

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/middlewares"
	"LCM/internal/core/services"
)

// APIKeyController verwaltet API-Keys (Permission: apikeys:manage).
type APIKeyController struct {
	apiKeys *services.APIKeyService
}

func NewAPIKeyController(apiKeys *services.APIKeyService) *APIKeyController {
	return &APIKeyController{apiKeys: apiKeys}
}

// List - GET /api/v1/apikeys
func (ctrl *APIKeyController) List(c fiber.Ctx) error {
	keys, err := ctrl.apiKeys.List()
	if err != nil {
		return err
	}
	return c.JSON(keys)
}

type createKeyRequest struct {
	Name  string `json:"name"`
	Scope string `json:"scope"` // "read" | "readwrite" | "mcp" (Default readwrite)
	// ExpiresInDays: Feld weglassen = unbefristet; Werte <= 0 werden
	// abgewiesen (R2-050 - negative Werte ergaben früher einen
	// UNBEFRISTETEN Key statt eines Fehlers).
	ExpiresInDays *int `json:"expires_in_days"`
}

// Create - POST /api/v1/apikeys
// Der Klartext-Key ist NUR in dieser Response enthalten.
//
// STRIKTER Body: unbekannte Felder werden abgewiesen (R2-050 - wer
// "expires_at" aus der API-Antwort abschrieb, bekam 201 und einen
// unbefristeten Admin-Key; das Feld fiel wortlos weg).
func (ctrl *APIKeyController) Create(c fiber.Ctx) error {
	var req createKeyRequest
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest,
			"ungültiger Request-Body (erlaubte Felder: name, scope, expires_in_days): "+err.Error())
	}
	if req.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name ist erforderlich")
	}

	var expiresAt *time.Time
	if req.ExpiresInDays != nil {
		if *req.ExpiresInDays <= 0 {
			return fiber.NewError(fiber.StatusBadRequest,
				"expires_in_days muss positiv sein - für einen unbefristeten Key das Feld weglassen")
		}
		t := time.Now().AddDate(0, 0, *req.ExpiresInDays)
		expiresAt = &t
	}

	// Der Key läuft im Rechte-Kontext des erstellenden Users.
	user := middlewares.CurrentUser(c)
	plaintext, key, err := ctrl.apiKeys.Create(req.Name, user.ID, req.Scope, expiresAt, actor(c))
	if err != nil {
		return mapServiceError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"key":     plaintext, // einmalige Anzeige!
		"api_key": key,
	})
}

// Revoke - DELETE /api/v1/apikeys/:id
func (ctrl *APIKeyController) Revoke(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.apiKeys.Revoke(id, actor(c)); err != nil {
		return mapServiceError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
