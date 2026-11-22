package controllers

import (
	"errors"
	"net/netip"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
	"LCM/internal/api/middlewares"
	"LCM/internal/core/services"
	"LCM/internal/netfilter"
)

// Netzbereiche je Endpunkt.
var (
	errEndpointUnknown       = apierr.Define(404, "endpoint.unknown", "unknown endpoint, allowed are ui, agent and mcp")
	errEndpointNetsInvalid   = apierr.Define(422, "endpoint.invalid_networks", "invalid network list")
	errEndpointWouldLockOut  = apierr.Define(409, "endpoint.would_lock_out", "your own address is not in the new list")
	errEndpointAgentsOutside = apierr.Define(409, "endpoint.agents_outside", "connected agents are outside the new list and would be disconnected")
)

// NetworkController bedient die erlaubten Netzbereiche der Endpunkte. Das
// Recht dazu (system:endpoints) hat allein admin.
type NetworkController struct {
	network *services.NetworkAccessService
}

func NewNetworkController(network *services.NetworkAccessService) *NetworkController {
	return &NetworkController{network: network}
}

// List - GET /api/v1/system/network (system:endpoints)
// Liefert je Endpunkt die Liste, dazu die eigene Adresse des
// Aufrufers: Sie muss in einer neuen Liste der Weboberfläche liegen.
func (ctrl *NetworkController) List(c fiber.Ctx) error {
	views, err := ctrl.network.View()
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"client_address": middlewares.ClientIP(c), "endpoints": views})
}

// Update - PUT /api/v1/system/network/:endpoint (system:endpoints)
// Body {"allowed": ["10.0.0.0/8"], "confirm": false}. Eine leere Liste erlaubt
// alle Netze. confirm bestätigt das Trennen verbundener Agenten außerhalb der
// neuen Agent-Liste.
func (ctrl *NetworkController) Update(c fiber.Ctx) error {
	endpoint, ok := netfilter.ParseEndpoint(c.Params("endpoint"))
	if !ok {
		return errEndpointUnknown
	}
	var in struct {
		Allowed *[]string `json:"allowed"`
		Confirm bool      `json:"confirm"`
	}
	if err := bindBody(c, &in); err != nil {
		return err
	}
	// Ein fehlendes Feld darf nicht wie eine leere Liste wirken: Das öffnete
	// den Endpunkt für alle Netze.
	if in.Allowed == nil {
		return missingFields("allowed")
	}
	client, _ := netip.ParseAddr(middlewares.ClientIP(c))
	result, err := ctrl.network.Update(endpoint, *in.Allowed,
		services.NetworkUpdate{ClientAddr: client.Unmap(), Confirm: in.Confirm}, actor(c))
	if err != nil {
		return mapNetworkError(err, middlewares.ClientIP(c))
	}
	return c.JSON(result)
}

func mapNetworkError(err error, clientAddr string) error {
	var invalid *services.NetworkInvalidError
	var outside *services.AgentsOutsideError
	switch {
	case errors.As(err, &invalid):
		return errEndpointNetsInvalid.With("detail", invalid.Detail)
	case errors.Is(err, services.ErrNetworkLockout):
		return errEndpointWouldLockOut.With("address", clientAddr)
	case errors.As(err, &outside):
		return errEndpointAgentsOutside.With("count", len(outside.Agents)).With("agents", outside.Agents)
	}
	return err
}
