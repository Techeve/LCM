package apierr

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestStatusOf: Das Zugriffsprotokoll muss denselben Status sehen, den der
// ErrorHandler schreibt - ein abgelehnter Login ist ein 401, kein 500.
func TestStatusOf(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{Unauthorized, 401},
		{NotFound.With("id", 3), 404},
		{fiber.NewError(413, "too large"), 413},
		{errors.New("kaputt"), 500},
	}
	for _, tc := range cases {
		if got := StatusOf(tc.err); got != tc.want {
			t.Errorf("StatusOf(%v) = %d, erwartet %d", tc.err, got, tc.want)
		}
	}
}
