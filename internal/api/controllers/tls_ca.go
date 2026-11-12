package controllers

import (
	"github.com/gofiber/fiber/v3"
)

// TLSCACert liefert das Zertifikat der CA, die das Server-Zertifikat von LCM
// ausstellt (PEM, samt Kette dahinter) - GET /api/v1/system/tls/ca.pem
// (settings:manage). Gebraucht wird es für einen Reverse-Proxy vor dem
// Agent-Port, der TLS selbst beendet, und für den Import in den Browser.
// Ohne CA (--dev) antwortet die Route mit 404.
func TLSCACert(caPEM func() ([]byte, error)) fiber.Handler {
	return func(c fiber.Ctx) error {
		pemData, err := caPEM()
		if err != nil {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderContentDisposition, `attachment; filename="lcm-ca.pem"`)
		c.Set(fiber.HeaderContentType, "application/x-pem-file")
		return c.Send(pemData)
	}
}
