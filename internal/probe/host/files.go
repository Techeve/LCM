package host

import (
	"os"
	"strconv"
	"strings"
)

// files liest Dateien unterhalb einer Wurzel; Tests geben eine eingefrorene
// Kopie von /proc und /sys an.
type files struct{ root string }

func (fs files) read(path string) string {
	b, err := os.ReadFile(fs.root + path)
	if err != nil {
		return ""
	}
	return string(b)
}

func (fs files) exists(path string) bool {
	_, err := os.Stat(fs.root + path)
	return err == nil
}

// uint liest eine Datei mit einer Zahl; „max" und Unlesbares sind ok=false.
func (fs files) uint(path string) (uint64, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(fs.read(path)), 10, 64)
	return v, err == nil
}

// keyValues liest Zeilen der Form „name wert" (cpu.stat, memory.stat) oder
// „Name: wert kB" (meminfo).
func keyValues(text string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 2 && f[2] == "kB" {
			v *= 1024
		}
		out[strings.TrimSuffix(f[0], ":")] = v
	}
	return out
}
