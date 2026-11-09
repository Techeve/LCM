package router

import (
	"io/fs"
	"mime"
	"path"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// assetMaxAge: Die Dateien tragen ihren Inhalts-Hash im Namen, neue Inhalte
// heißen anders.
const assetMaxAge = "public, max-age=31536000"

// assetEncodings in der Reihenfolge der Vorliebe. Die Dateien legt der
// Vite-Build an (ui/vite-plugins/compress.ts).
var assetEncodings = []struct{ token, suffix string }{
	{"br", ".br"},
	{"gzip", ".gz"},
}

// precompressedAssets liefert die vorkomprimierte Fassung einer Datei, wenn der
// Client sie versteht und es sie gibt; sonst übernimmt next.
func precompressedAssets(frontend fs.FS, next fiber.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		name := strings.TrimPrefix(c.Path(), "/")
		accepted := c.Get(fiber.HeaderAcceptEncoding)
		c.Vary(fiber.HeaderAcceptEncoding)
		for _, encoding := range assetEncodings {
			if !acceptsEncoding(accepted, encoding.token) {
				continue
			}
			data, err := fs.ReadFile(frontend, name+encoding.suffix)
			if err != nil {
				continue
			}
			c.Set(fiber.HeaderContentEncoding, encoding.token)
			c.Set(fiber.HeaderContentType, mime.TypeByExtension(path.Ext(name)))
			c.Set(fiber.HeaderCacheControl, assetMaxAge)
			return c.Send(data)
		}
		return next(c)
	}
}

// acceptsEncoding liest Accept-Encoding: „br“ oder „br;q=1.0“ zählt, „br;q=0“
// heißt ausdrücklich nein.
func acceptsEncoding(header, token string) bool {
	for _, entry := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(entry), ";")
		if strings.EqualFold(strings.TrimSpace(name), token) {
			return strings.ReplaceAll(params, " ", "") != "q=0"
		}
	}
	return false
}
