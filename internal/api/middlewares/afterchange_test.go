package middlewares

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestAfterChange: Nur ändernde Anfragen lösen den Rückruf aus - auch eine
// gescheiterte, denn sie kann vor dem Fehler schon etwas geändert haben.
func TestAfterChange(t *testing.T) {
	tests := []struct {
		method string
		fail   bool
		want   int
	}{
		{"GET", false, 0},
		{"POST", false, 1},
		{"PATCH", false, 1},
		{"DELETE", true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			calls := 0
			app := fiber.New()
			app.Use(AfterChange(func() { calls++ }))
			app.All("/x", func(c fiber.Ctx) error {
				if tt.fail {
					return fiber.ErrBadRequest
				}
				return c.SendStatus(fiber.StatusNoContent)
			})
			if _, err := app.Test(httptest.NewRequest(tt.method, "/x", nil)); err != nil {
				t.Fatal(err)
			}
			if calls != tt.want {
				t.Fatalf("%d aufrufe, erwartet %d", calls, tt.want)
			}
		})
	}
}
