package trivy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sandbox"
)

// localRunner startet Trivy als Kindprozess auf dem LCM-Host - der Weg für
// eine normale Installation (.deb/systemd). Im Container-Betrieb tritt an
// seine Stelle der remoteRunner.
type localRunner struct {
	binary   string        // Pfad/Name des Trivy-Binaries (z.B. "trivy")
	cacheDir string        // Vuln-DB-Cache (z.B. <state>/trivy)
	timeout  time.Duration // Zeitlimit je SBOM-Scan
}

// sbomTimeout ist das Zeitlimit eines SBOM-Scans (lokale Datenbank, kein Netz).
const sbomTimeout = 5 * time.Minute

// imageTimeout ist das Zeitlimit eines Image-Scans - deutlich großzügiger als
// beim SBOM-Scan, weil Trivy die Image-Layer erst herunterladen muss.
const imageTimeout = 15 * time.Minute

// dbUpdateTimeout: der Download der Datenbank geht über das Netz und ist
// mehrere hundert MB groß.
const dbUpdateTimeout = 10 * time.Minute

func newLocal(binary, cacheDir string) *localRunner {
	if binary == "" {
		binary = "trivy"
	}
	return &localRunner{binary: binary, cacheDir: cacheDir, timeout: sbomTimeout}
}

// available prüft, ob das Trivy-Binary auffindbar ist.
func (l *localRunner) available() bool {
	if l == nil || l.binary == "" {
		return false
	}
	if strings.ContainsRune(l.binary, os.PathSeparator) {
		info, err := os.Stat(l.binary)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(l.binary)
	return err == nil
}

// command baut den Trivy-Aufruf - eingesperrt, soweit das System es hergibt
// (siehe Paket sandbox). Trivy darf damit nur sein eigenes Binary, seinen
// Cache, die übergebene SBOM-Datei und die Systemgrundlagen sehen; das
// LCM-Datenverzeichnis mit Datenbank und Master-Key ist für den Prozess
// nicht vorhanden.
//
// allowNet trennt die beiden Betriebsarten sauber: der SBOM-Scan wertet nur
// die lokale Datenbank aus und braucht KEIN Netz - ein manipuliertes Trivy
// könnte dabei also nicht einmal etwas abfließen lassen. Netz bekommen nur
// der Datenbank-Download und der Image-Scan, die ohne nicht arbeiten können.
func (l *localRunner) command(ctx context.Context, allowNet bool, readFiles []string, args ...string) *exec.Cmd {
	var read []string
	var write []string
	if l.cacheDir != "" {
		write = append(write, l.cacheDir)
	}
	// Absoluter Binärpfad: liegt Trivy außerhalb der Standardverzeichnisse,
	// muss sein Verzeichnis ausdrücklich erlaubt werden.
	if bin, err := exec.LookPath(l.binary); err == nil {
		read = append(read, filepath.Dir(bin))
	}
	spec := sandbox.BaseSystemSpec().WithPaths(read, readFiles, write).WithNet(allowNet)
	// Trivy legt beim Auswerten Zwischendateien an - es bekommt dafür ein
	// eigenes /tmp, nicht das des Hosts.
	spec.ScratchTmp = true
	return sandbox.Command(ctx, spec, l.binary, args...)
}

// scanSBOM schreibt das SBOM in eine temporäre Datei und lässt Trivy es prüfen.
// Die Datei ist nötig, weil das Werkzeug einen Pfad erwartet - beim
// remoteRunner entfällt sie, dort geht das SBOM direkt über die Leitung.
func (l *localRunner) scanSBOM(ctx context.Context, sbom []byte) ([]byte, error) {
	f, err := os.CreateTemp("", "lcm-sbom-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(sbom); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()

	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	// Kein Netz: der SBOM-Scan wertet nur die lokale Datenbank aus. Von den
	// Dateien des Hosts bekommt Trivy nur diese eine SBOM zu sehen.
	return l.run(ctx, false, []string{f.Name()}, sbomScanArgs(l.cacheDir, f.Name())...)
}

// scanImage lässt Trivy ein Container-Image aus der Registry prüfen.
func (l *localRunner) scanImage(ctx context.Context, ref string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, imageTimeout)
	defer cancel()

	args := []string{"image", "--format", "json", "--quiet", "--scanners", "vuln"}
	if l.cacheDir != "" {
		args = append(args, "--cache-dir", l.cacheDir)
	}
	args = append(args, ref)
	// Netz nötig: zieht die Image-Layer aus der Registry.
	return l.run(ctx, true, nil, args...)
}

// run führt den Aufruf aus und liefert stdout; stderr landet im Fehlertext.
func (l *localRunner) run(ctx context.Context, allowNet bool, readFiles []string, args ...string) ([]byte, error) {
	cmd := l.command(ctx, allowNet, readFiles, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return []byte(stdout.String()), nil
}

// sbomScanArgs baut die Aufrufzeile des SBOM-Scans.
//
// --skip-db-update ist der wichtige Teil: Der Scan wertet aus, was da ist, und
// lädt NICHTS nach. Trivy hält seine Datenbank nur 24 Stunden für gültig und
// will danach mitten im Scan nachladen - in der Sandbox gibt es dafür kein
// Netz, und der Scan endete mit einem Fatal-Fehler. Die CVE-Bewertung fiel
// damit täglich zwischen Ablauf der Datenbank und dem nächsten Datenbank-Lauf
// komplett aus (in der Testumgebung beobachtet). Fürs Nachladen ist UpdateDB
// da - mit Netz und als eigener Job; dass die Datenbank alt ist, meldet die
// Ampel ohnehin als Hinweis.
func sbomScanArgs(cacheDir, sbomFile string) []string {
	args := []string{"sbom", "--format", "json", "--quiet", "--scanners", "vuln", "--skip-db-update"}
	if cacheDir != "" {
		args = append(args, "--cache-dir", cacheDir)
	}
	return append(args, sbomFile)
}

// versionOutput ist die JSON-Ausgabe von `trivy --version --format json`.
// Der Aufruf braucht KEIN Netz - er liest nur die Metadaten der lokalen
// Datenbank; damit ist er auch auf abgeschotteten Hosts aussagekräftig.
type versionOutput struct {
	Version         string `json:"Version"`
	VulnerabilityDB *struct {
		Version      int        `json:"Version"`
		UpdatedAt    *time.Time `json:"UpdatedAt"`
		NextUpdate   *time.Time `json:"NextUpdate"`
		DownloadedAt *time.Time `json:"DownloadedAt"`
	} `json:"VulnerabilityDB"`
}

// info liefert Scanner-Version, DB-Stand und den Zustand der Abschottung.
// Die Frische-Bewertung macht der Aufrufer über EvaluateCVEDB - die Domain
// kennt die Schwellen, nicht diese Infrastruktur-Schicht.
func (l *localRunner) info(ctx context.Context) domain.CVEDBStatus {
	sb := sandbox.Available()
	st := domain.CVEDBStatus{
		Freshness: domain.CVEDBUnknown,
		Sandboxed: sb.Active, SandboxBackend: sb.Backend, SandboxNote: sb.Reason,
	}
	if !l.available() {
		return st
	}
	st.Available = true

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []string{"--version", "--format", "json"}
	if l.cacheDir != "" {
		args = append(args, "--cache-dir", l.cacheDir)
	}
	out, err := l.command(ctx, false, nil, args...).Output()
	if err != nil {
		st.Error = fmt.Sprintf("trivy --version fehlgeschlagen: %v", err)
		return st
	}
	var v versionOutput
	if err := json.Unmarshal(out, &v); err != nil {
		st.Error = fmt.Sprintf("trivy-versionsausgabe parsen: %v", err)
		return st
	}
	st.Version = v.Version
	if v.VulnerabilityDB != nil {
		st.UpdatedAt = v.VulnerabilityDB.UpdatedAt
		st.NextUpdate = v.VulnerabilityDB.NextUpdate
		st.DownloadedAt = v.VulnerabilityDB.DownloadedAt
	}
	// Kein DB-Block bzw. kein Zeitstempel heißt: noch nie geladen. Das ist
	// keine Kleinigkeit - dann hat noch kein Scan echte Daten gesehen, also
	// klar benennen statt still „unbekannt" zu lassen.
	if st.UpdatedAt == nil {
		st.Error = "keine Schwachstellen-Datenbank vorhanden - sie wurde noch nie geladen"
	}
	return st
}

// updateDB lädt die Schwachstellen-Datenbank herunter, ohne zu scannen
// (`--download-db-only`).
func (l *localRunner) updateDB(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dbUpdateTimeout)
	defer cancel()

	args := []string{"image", "--download-db-only"}
	if l.cacheDir != "" {
		args = append(args, "--cache-dir", l.cacheDir)
	}
	cmd := l.command(ctx, true, nil, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// Trivy schreibt seinen Fortschritt auf stderr - der gehört ins
	// Job-Protokoll, gerade im Fehlerfall (Proxy, Rate-Limit, kein Netz).
	output := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if err != nil {
		return output, fmt.Errorf("trivy-datenbank-update fehlgeschlagen: %w", err)
	}
	return output, nil
}
