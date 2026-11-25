package perms

import (
	"os"
	"path/filepath"
)

// SystemDirs sind Verzeichnisse des Betriebssystems. Weder darf LCM dort Daten
// ablegen noch deren Rechte verändern: Ein chmod 0750 auf /usr/bin sperrt alle
// Benutzer außer root aus dem System aus.
var SystemDirs = []string{
	"/", "/etc", "/usr",
	"/bin", "/sbin", "/lib", "/lib32", "/lib64",
	"/usr/bin", "/usr/sbin", "/usr/lib", "/usr/lib64", "/usr/libexec",
	"/usr/local", "/usr/local/bin", "/usr/local/sbin", "/usr/local/lib",
}

// IsSystemDir sagt, ob path nach Auflösen von Symlinks eines der
// Systemverzeichnisse ist (/bin ist meist ein Symlink auf /usr/bin).
func IsSystemDir(path string) bool {
	return isSystemDirIn(path, SystemDirs)
}

func isSystemDirIn(path string, denied []string) bool {
	resolved := resolvePath(path)
	for _, d := range denied {
		if resolved == resolvePath(d) {
			return true
		}
	}
	return false
}

// resolvePath macht den Pfad absolut und löst Symlinks auf. Existiert er noch
// nicht, bleibt der bereinigte absolute Pfad.
func resolvePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// lcmMarkers sind Dateien und Verzeichnisse, die nur ein LCM-Datenverzeichnis
// enthält. Ein nicht leeres Verzeichnis ohne eines davon ist kein
// Datenverzeichnis von LCM - der Wächter fasst es nicht an.
var lcmMarkers = []string{
	"lcm.key", "app.db", "config.json", "version.json",
	"lcm-cert.pem", "lcm-key.pem", "backups", "tls",
}

// looksLikeDataDir: leer, nicht vorhanden oder mit mindestens einem Marker.
func looksLikeDataDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return true
	}
	for _, e := range entries {
		for _, m := range lcmMarkers {
			if e.Name() == m {
				return true
			}
		}
	}
	return false
}
