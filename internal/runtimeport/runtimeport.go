// Package runtimeport merkt sich den Port, auf dem die Oberfläche gerade
// lauscht, in einer Datei im Datenverzeichnis. Der Port ist in den
// Einstellungen änderbar; wer von außen nach LCM fragt (lcm -healthcheck,
// lcm-probe), kennt nur die config.json und träfe sonst den alten Port.
package runtimeport

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileName ist die Datei im Datenverzeichnis.
const FileName = "ui-port"

// Write schreibt den Port der Oberfläche atomar. Die Datei hält nur eine Zahl
// und ist für alle lesbar (lcm-probe läuft unter einem anderen Benutzer).
func Write(dataDir string, port int) error {
	path := filepath.Join(dataDir, FileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(port)+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Read liest den gemerkten Port; false, wenn es ihn nicht gibt oder er keine
// gültige Zahl ist.
func Read(dataDir string) (int, bool) {
	raw, err := os.ReadFile(filepath.Join(dataDir, FileName))
	if err != nil {
		return 0, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}
