// Package perms wacht über die Dateirechte des Datenverzeichnisses.
//
// Datenbank, Master-Key, TLS-Schlüssel und Konfiguration liegen auf der
// Platte - und ein einziges `chmod 777`, ein falsches Backup-Restore oder
// ein zu weiter umask machen sie für jeden Benutzer des Hosts lesbar. Das
// Paketskript setzt die Rechte einmal beim Installieren; dieser Wächter
// prüft sie beim Start und danach regelmäßig, zieht sie bei eigenen Dateien
// selbst wieder fest und meldet, was er nicht richten kann.
package perms

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Interval ist der Prüfabstand nach dem Start.
const Interval = 15 * time.Minute

// Finding ist eine Abweichung - behoben oder nicht.
type Finding struct {
	Path   string
	Detail string
	Fixed  bool
}

func (f Finding) String() string {
	state := "NICHT behoben"
	if f.Fixed {
		state = "behoben"
	}
	return fmt.Sprintf("%s: %s (%s)", f.Path, f.Detail, state)
}

// Guard kennt die Orte, an denen Geheimnisse liegen.
type Guard struct {
	dataDir    string
	configPath string
	uid        int
	now        func() time.Time
}

// New baut den Wächter für ein Datenverzeichnis und die Konfigurationsdatei.
func New(dataDir, configPath string) *Guard {
	return &Guard{dataDir: dataDir, configPath: configPath, uid: os.Getuid(), now: time.Now}
}

// secretFiles sind die Dateien, die niemand außer dem Dienst lesen darf.
var secretFiles = map[string]bool{
	"lcm.key": true, "lcm-key.pem": true, "self-onboard.json": true,
	"app.db": true, "app.db-wal": true, "app.db-shm": true,
}

// Check prüft einmal und richtet, was dem Dienst gehört. Rückgabe: alle
// Abweichungen, auch die behobenen - der Aufrufer protokolliert sie.
func (g *Guard) Check() []Finding {
	var out []Finding
	// Das Datenverzeichnis gehört dem Dienst allein: 0700.
	out = append(out, g.checkDir(g.dataDir, 0o700)...)
	entries, err := os.ReadDir(g.dataDir)
	if err == nil {
		for _, e := range entries {
			p := filepath.Join(g.dataDir, e.Name())
			switch {
			case e.IsDir():
				// Unterverzeichnisse (backups, logs, trivy, restore-staging):
				// ebenfalls nur der Dienst.
				out = append(out, g.checkDir(p, 0o700)...)
			case secretFiles[e.Name()] || strings.HasSuffix(e.Name(), ".lcmbak") || strings.HasSuffix(e.Name(), ".db"):
				out = append(out, g.checkFile(p, 0o600)...)
			default:
				// Zertifikat, version.json, Logs: kein Geheimnis, aber
				// niemand außer dem Dienst muss sie schreiben oder lesen.
				out = append(out, g.checkOtherBits(p)...)
			}
		}
		out = append(out, g.checkTree(filepath.Join(g.dataDir, "backups"), 0o600)...)
	}
	// Konfiguration: gehört root oder dem Dienst, Gruppe darf lesen/schreiben
	// (Restore), sonst niemand. Sie hält das JWT-Secret.
	if g.configPath != "" {
		out = append(out, g.checkOtherBits(filepath.Dir(g.configPath))...)
		out = append(out, g.checkOtherBits(g.configPath)...)
	}
	return out
}

// checkDir verlangt genau want und richtet eigene Verzeichnisse.
func (g *Guard) checkDir(path string, want fs.FileMode) []Finding {
	return g.checkMode(path, want, func(mode fs.FileMode) bool { return mode.Perm() != want })
}

// checkFile verlangt genau want und richtet eigene Dateien.
func (g *Guard) checkFile(path string, want fs.FileMode) []Finding {
	return g.checkMode(path, want, func(mode fs.FileMode) bool { return mode.Perm() != want })
}

// checkOtherBits verlangt nur, dass „andere" nichts dürfen; richtet eigene
// Dateien auf mode&^0o007.
func (g *Guard) checkOtherBits(path string) []Finding {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	want := info.Mode().Perm() &^ 0o007
	return g.checkMode(path, want, func(mode fs.FileMode) bool { return mode.Perm()&0o007 != 0 })
}

// checkTree prüft alle Dateien unter dir (Backups) auf want.
func (g *Guard) checkTree(dir string, want fs.FileMode) []Finding {
	var out []Finding
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, g.checkFile(filepath.Join(dir, e.Name()), want)...)
		}
	}
	return out
}

func (g *Guard) checkMode(path string, want fs.FileMode, bad func(fs.FileMode) bool) []Finding {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	var out []Finding
	owner, ok := ownerUID(info)
	// Fremder Eigentümer im Datenverzeichnis: kann der Dienst nicht richten,
	// und es gehört gemeldet - so kommt ein Angreifer an Dateien, die der
	// Dienst danach nicht mehr schreiben kann, oder er hat sie untergeschoben.
	foreign := ok && owner != g.uid && owner != 0
	if foreign {
		out = append(out, Finding{Path: path, Detail: fmt.Sprintf("gehört uid %d, nicht dem Dienst", owner)})
	}
	if !bad(info.Mode()) {
		return out
	}
	f := Finding{Path: path, Detail: fmt.Sprintf("rechte %04o, erwartet %04o", info.Mode().Perm(), want)}
	if ok && owner == g.uid {
		f.Fixed = os.Chmod(path, want) == nil
	}
	return append(out, f)
}

func ownerUID(info fs.FileInfo) (int, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}

// Run prüft jetzt und protokolliert. Behobenes ist eine Warnung (jemand hat
// die Rechte verändert), Unbehobenes ein Fehler mit festem Text „security",
// damit es dieselbe Auswertung erreicht wie Anmeldeereignisse.
func (g *Guard) Run() []Finding {
	findings := g.Check()
	for _, f := range findings {
		if f.Fixed {
			slog.Warn("security", "event", "permissions.fixed", "path", f.Path, "detail", f.Detail)
		} else {
			slog.Error("security", "event", "permissions.insecure", "path", f.Path, "detail", f.Detail)
		}
	}
	return findings
}

// Loop prüft im Abstand von Interval, bis stop geschlossen wird.
func (g *Guard) Loop(stop <-chan struct{}) {
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			g.Run()
		}
	}
}
