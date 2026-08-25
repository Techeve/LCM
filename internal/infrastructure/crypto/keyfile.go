package crypto

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KeyFileName ist der Dateiname des Master-Keys im Datenverzeichnis.
const KeyFileName = "lcm.key"

// EnvKeyName ist die Umgebungsvariable, die den Master-Key (base64)
// alternativ zur Key-Datei bereitstellen kann.
const EnvKeyName = "LCM_ENCRYPTION_KEY"

// LoadOrCreateMasterKey liefert den Master-Key der Installation.
//
// Reihenfolge: 1. LCM_ENCRYPTION_KEY (base64), 2. lcm.key im
// Datenverzeichnis, 3. neu generieren und mit 0600 speichern.
// created meldet, ob der Key in diesem Aufruf neu erzeugt wurde.
func LoadOrCreateMasterKey(dataDir string) (key []byte, created bool, err error) {
	if env := os.Getenv(EnvKeyName); env != "" {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(env))
		if err != nil {
			return nil, false, fmt.Errorf("%s ist kein gültiges base64: %w", EnvKeyName, err)
		}
		if len(key) != KeySize {
			return nil, false, fmt.Errorf("%s: %w", EnvKeyName, ErrInvalidKeySize)
		}
		return key, false, nil
	}

	path := filepath.Join(dataDir, KeyFileName)
	data, err := os.ReadFile(path)
	if err == nil {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, false, fmt.Errorf("%s ist kein gültiges base64: %w", path, err)
		}
		if len(key) != KeySize {
			return nil, false, fmt.Errorf("%s: %w", path, ErrInvalidKeySize)
		}
		return key, false, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("%s lesen: %w", path, err)
	}

	key = GenerateKey()
	if err := WriteKeyFile(path, key); err != nil {
		return nil, false, err
	}
	return key, true, nil
}

// WriteKeyFile schreibt einen Master-Key base64-kodiert mit strikten
// Rechten (0600) - die Datei gehört ausschließlich dem Service-User.
func WriteKeyFile(path string, key []byte) error {
	encoded := base64.StdEncoding.EncodeToString(key) + "\n"
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return fmt.Errorf("master-key speichern: %w", err)
	}
	return nil
}
