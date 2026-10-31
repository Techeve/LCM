package middlewares

import "github.com/gofiber/fiber/v3"

// AfterChange ruft fn nach jeder ändernden Anfrage (POST, PUT, PATCH,
// DELETE) auf - auch nach einer gescheiterten: Ob sie etwas angefasst hat,
// bevor sie scheiterte, lässt sich von hier aus nicht sagen.
func AfterChange(fn func()) fiber.Handler {
	return func(c fiber.Ctx) error {
		err := c.Next()
		if isMutating(c.Method()) {
			fn()
		}
		return err
	}
}
