// Package frontend bettet das gebaute Svelte-Frontend (Vite dist/) in das
// Go-Binary ein. Vor dem Go-Build muss `npm run build` (bzw. `make build`)
// gelaufen sein, damit dist/ existiert.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS liefert den Inhalt von dist/ als fs.FS (Wurzel = dist selbst).
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
