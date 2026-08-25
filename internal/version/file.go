package version

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Die Versionsdatei (version.json) liegt neben der Datenbank im
// Datenverzeichnis der Installation. Sie hält fest, welche Version der
// Anwendung dort zuletzt lief:
//
//   - Beim allerersten Start (keine DB, keine config.json) wird sie mit
//     der Version des Binaries angelegt.
//   - Bei jedem weiteren Start vergleicht die Anwendung die Datei mit der
//     eigenen Version. Ein Unterschied bedeutet: Das Binary wurde
//     aktualisiert -> die Update-Migrationen laufen (siehe
//     internal/storage/migrations.go), danach wird die Datei fortgeschrieben.
type FileInfo struct {
	Version   string    `json:"version"`
	Build     string    `json:"build"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FileName ist der Standardname der Versionsdatei.
const FileName = "version.json"

// ReadInstalled liest die Versionsdatei der Installation.
// Existiert sie nicht, ist die Rückgabe (nil, nil).
func ReadInstalled(path string) (*FileInfo, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("versionsdatei lesen: %w", err)
	}
	var info FileInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("versionsdatei parsen: %w", err)
	}
	return &info, nil
}

// WriteInstalled schreibt die aktuelle Binary-Version in die Versionsdatei.
func WriteInstalled(path string) error {
	info := FileInfo{
		Version:   Version,
		Build:     Build,
		UpdatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("versionsdatei schreiben: %w", err)
	}
	return nil
}
