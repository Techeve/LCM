package controllers

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Gemeinsame Bausteine der Listen-Endpunkte: Filter, Sortierung und Blättern
// laufen im Backend, die Antwort ist immer {"items": [...], "total": N}.

// listResponse ist die Antwort aller Listen-Endpunkte.
type listResponse[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// newListResponse macht aus nil eine leere Liste, damit items nie null ist.
func newListResponse[T any](items []T, total int) listResponse[T] {
	if items == nil {
		items = []T{}
	}
	return listResponse[T]{Items: items, Total: total}
}

// pagingOf liest limit und offset. Fehlendes oder nicht positives limit gibt
// die Vorgabe, ein größeres als max wird auf max gekürzt; ein negativer offset
// zählt als 0.
func pagingOf(c fiber.Ctx, defaultLimit, maxLimit int) (limit, offset int) {
	limit = fiber.Query[int](c, "limit")
	if limit <= 0 {
		limit = defaultLimit
	}
	return min(limit, maxLimit), max(fiber.Query[int](c, "offset"), 0)
}

// csvQuery liest einen Kommaliste-Parameter ohne Leerwerte.
func csvQuery(c fiber.Ctx, key string) []string {
	var out []string
	for _, part := range strings.Split(c.Query(key), ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// enumListQuery liest einen Kommaliste-Parameter und weist Werte außerhalb von
// allowed ab - ein Tippfehler soll nicht wie „keine Treffer" aussehen.
func enumListQuery(c fiber.Ctx, key string, allowed []string) ([]string, error) {
	values := csvQuery(c, key)
	for _, v := range values {
		if !slices.Contains(allowed, v) {
			return nil, errInvalidFilter.With("filter", key).With("allowed", strings.Join(allowed, ", "))
		}
	}
	return values, nil
}

// idListQuery liest eine Kommaliste von Kennungen.
func idListQuery(c fiber.Ctx, key string) ([]uint, error) {
	var ids []uint
	for _, part := range csvQuery(c, key) {
		id, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return nil, errInvalidFilter.With("filter", key).With("allowed", "number list")
		}
		ids = append(ids, uint(id))
	}
	return ids, nil
}

// idQuery liest eine einzelne Kennung (0, wenn der Parameter fehlt).
func idQuery(c fiber.Ctx, key string) (uint, error) {
	raw := c.Query(key)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, errInvalidFilter.With("filter", key).With("allowed", "number")
	}
	return uint(id), nil
}

// timeQuery liest einen RFC-3339-Zeitpunkt (nil, wenn der Parameter fehlt).
func timeQuery(c fiber.Ctx, key string) (*time.Time, error) {
	raw := c.Query(key)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errInvalidFilter.With("filter", key).With("allowed", "RFC 3339")
	}
	return &t, nil
}

// sortQuery liest den sort-Parameter und prüft ihn gegen allowed; fehlt er,
// gilt def.
func sortQuery(c fiber.Ctx, allowed []string, def string) (string, error) {
	sortBy := c.Query("sort")
	if sortBy == "" {
		return def, nil
	}
	if !slices.Contains(allowed, sortBy) {
		return "", errInvalidFilter.With("filter", "sort").With("allowed", strings.Join(allowed, ", "))
	}
	return sortBy, nil
}
