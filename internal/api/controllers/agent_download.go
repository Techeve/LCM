package controllers

import (
	"os"
	"path/filepath"

	"github.com/gofiber/fiber/v3"
)

// AgentDownload liefert das lcm-agent-Binary für die manuelle Installation
// (LCM Remote) - GET /api/v1/agent/download/:arch (amd64|arm64).
//
// Bewusst ÖFFENTLICH (kein Login, von der IP-Allowlist ausgenommen): das
// Binary ist ein veröffentlichtes Artefakt ohne Geheimnisse, und der
// Ziel-Server hat beim Einrichten noch keine Zugangsdaten. searchDirs wird
// der Reihe nach durchsucht (Paket: /usr/share/lcm/agent, Dev: ./bin).
func AgentDownload(searchDirs []string) fiber.Handler {
	return func(c fiber.Ctx) error {
		arch := c.Params("arch")
		if arch != "amd64" && arch != "arm64" {
			return fiber.NewError(fiber.StatusBadRequest, "arch muss amd64 oder arm64 sein")
		}
		name := "lcm-agent-linux-" + arch
		for _, dir := range searchDirs {
			path := filepath.Join(dir, name)
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				c.Set(fiber.HeaderContentDisposition, `attachment; filename="lcm-agent"`)
				c.Set(fiber.HeaderContentType, "application/octet-stream")
				return c.SendFile(path)
			}
		}
		return fiber.NewError(fiber.StatusNotFound,
			"agent-binary nicht vorhanden - es wird mit dem lcm-Debian-Paket installiert (/usr/share/lcm/agent); alternativ das lcm-agent-Paket aus dem Repository verwenden")
	}
}
