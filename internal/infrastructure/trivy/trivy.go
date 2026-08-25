// Package trivy kapselt den CVE-Scan des Paketbestands über das externe
// Werkzeug Trivy (github.com/aquasecurity/trivy). LCM erzeugt pro Server ein
// CycloneDX-SBOM aus dem bereits erfassten Paketbestand und lässt Trivy es
// gegen seine Schwachstellen-Datenbank prüfen - der Scan läuft zentral auf
// dem LCM-Host, ohne die verwalteten Server zu kontaktieren.
//
// Trivy ist eine WEICHE Abhängigkeit: fehlt das Binary, meldet Available()
// false und der Scan wird sauber übersprungen (graceful degrade).
package trivy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
)

// Scanner ist die Abstraktion des CVE-Scans (analog zu sshx.Dialer). Sie
// erlaubt einen Fake in Tests und den graceful degrade, wenn Trivy fehlt.
type Scanner interface {
	// Available meldet, ob der Scanner einsatzbereit ist (Trivy-Binary da).
	Available() bool
	// Scan prüft den Paketbestand eines Ziels und liefert die gefundenen
	// Schwachstellen (ServerID setzt der Aufrufer).
	Scan(ctx context.Context, target Target) ([]domain.Vulnerability, error)
	// ScanImage prüft ein Container-Image. Trivy zieht die Image-Layer aus
	// der Registry in seinen Cache - das läuft zentral auf dem LCM-Host,
	// die verwalteten Server werden nicht kontaktiert.
	ScanImage(ctx context.Context, ref string) ([]domain.Vulnerability, error)
	// Info liefert Scanner-Version und den Stand der Schwachstellen-Datenbank.
	// Ohne diese Angabe ist „keine Sicherheitslücken gefunden" nicht von
	// „seit Wochen nicht mehr nachgesehen" zu unterscheiden.
	Info(ctx context.Context) domain.CVEDBStatus
	// CacheStats liefert den Zustand des Ergebnis-Zwischenspeichers.
	CacheStats() CacheStats
	// UpdateDB lädt die Schwachstellen-Datenbank herunter, ohne zu scannen.
	// Liefert die Ausgabe des Werkzeugs (für das Job-Protokoll).
	UpdateDB(ctx context.Context) (string, error)
}

// Target bündelt die Eingaben eines Scans: Distribution, Paketverwaltung und
// der installierte Paketbestand.
type Target struct {
	OSID           string // /etc/os-release ID, z.B. "ubuntu"/"debian"/"rocky"
	OSVersionID    string // VERSION_ID, z.B. "22.04"/"12"/"9.3"
	PackageManager string // apt/dnf/yum/zypper
	Packages       []domain.Package
}

// Trivy ist die Scanner-Implementierung. Sie besitzt alles, was unabhängig
// von der Ausführungsart gilt - SBOM-Bau, Ergebnis-Cache, Auswertung der
// JSON-Ausgabe und den zwischengespeicherten DB-Stand. WIE Trivy ausgeführt
// wird, steckt hinter dem runner (siehe runner.go): lokal als Kindprozess
// oder über HTTP gegen einen Sidecar-Container.
type Trivy struct {
	run runner

	// Zwischenspeicher für Info(). Das Kommando ist billig und braucht kein
	// Netz, wird aber bei jedem Server-Status abgefragt - einmal pro Minute
	// genügt völlig.
	infoMu   sync.Mutex
	infoVal  domain.CVEDBStatus
	infoTime time.Time

	// scans ist der inhaltsadressierte Ergebnis-Cache (siehe scan_cache.go):
	// identischer Paketbestand + identische Datenbank ⇒ kein zweiter Lauf.
	scans scanCache
}

// infoTTL ist die Lebensdauer des zwischengespeicherten DB-Stands.
const infoTTL = time.Minute

// New erstellt einen Trivy-Scanner, der das Binary lokal startet. binary darf
// ein Name (via PATH) oder ein absoluter Pfad sein; cacheDir nimmt die
// heruntergeladene Vuln-DB auf.
func New(binary, cacheDir string) *Trivy {
	return &Trivy{run: newLocal(binary, cacheDir)}
}

// NewRemote erstellt einen Trivy-Scanner, der einen Sidecar über HTTP
// anspricht (Container-Betrieb, siehe cmd/trivyd). Alles jenseits der
// Ausführung - SBOM, Cache, Auswertung - ist identisch zum lokalen Weg.
func NewRemote(baseURL, token string) *Trivy {
	return &Trivy{run: newRemote(baseURL, token)}
}

// Available meldet, ob der Scanner einsatzbereit ist.
func (t *Trivy) Available() bool {
	return t != nil && t.run != nil && t.run.available()
}

// Scan baut aus dem Paketbestand ein SBOM und lässt Trivy es prüfen.
// Vorher fragt es den Ergebnis-Cache: identischer Paketbestand bei
// identischem Datenbank-Stand liefert zwangsläufig dasselbe Ergebnis - dann
// entfallen SBOM-Bau und Lauf komplett (scan_cache.go).
func (t *Trivy) Scan(ctx context.Context, target Target) ([]domain.Vulnerability, error) {
	if !t.Available() {
		return nil, fmt.Errorf("trivy nicht verfügbar")
	}
	// Gültigkeitsanker ist der Bau-Zeitstempel der Datenbank (Info ist für
	// eine Minute gecacht und billig). Ohne lesbaren Stand bleibt der Cache
	// stumm - dann wird schlicht jedes Mal gescannt.
	stamp := dbStamp(t.Info(ctx))
	key := scanCacheKey(target)
	if vulns, ok := t.scans.get(stamp, key); ok {
		return vulns, nil
	}
	sbom, err := BuildSBOM(target)
	if err != nil {
		return nil, fmt.Errorf("sbom erzeugen: %w", err)
	}
	out, err := t.run.scanSBOM(ctx, sbom)
	if err != nil {
		return nil, scanError(err)
	}
	vulns, err := parseReport(out, target.PackageManager)
	if err != nil {
		return nil, err
	}
	t.scans.put(stamp, key, vulns)
	return vulns, nil
}

// CacheStats liefert den Zustand des Ergebnis-Zwischenspeichers.
func (t *Trivy) CacheStats() CacheStats { return t.scans.stats() }

// dbStamp zieht den Bau-Zeitstempel der Datenbank aus dem Scanner-Status -
// der Gültigkeitsanker des Ergebnis-Caches. Zero heißt: kein verlässlicher
// Stand, nicht cachen.
func dbStamp(st domain.CVEDBStatus) time.Time {
	if st.UpdatedAt == nil {
		return time.Time{}
	}
	return *st.UpdatedAt
}

// scanError übersetzt bekannte Trivy-Fehlerbilder in einen Satz, der dem
// Betreiber sagt, was los ist - statt eines rohen Stacktraces in der Ampel
// (R2-005). Nur klar erkannte Muster werden übersetzt; alles Unbekannte
// behält den vollen Fehlertext als einzige Spur zur Ursache.
//
// Bewusst auf dem Fehlertext statt auf stderr: So gilt die Übersetzung auch
// für den Sidecar-Weg, der stderr nicht selbst in der Hand hat.
func scanError(err error) error {
	// trivy-db liefert zeitweise keine CPE-Indizes für die Red-Hat-Advisories -
	// dann schlägt die Bewertung für RHEL-nahe Systeme (v.a. CentOS Stream)
	// fehl, obwohl an LCM, Trivy und dem Server nichts kaputt ist.
	// Referenz: https://github.com/aquasecurity/trivy-db/issues/435
	if strings.Contains(err.Error(), "unable to find CPE indices") {
		return fmt.Errorf("die Trivy-Datenbank enthält derzeit keine verwertbaren Red-Hat-Advisories für dieses Betriebssystem (bekanntes Upstream-Problem trivy-db#435, betrifft v.a. CentOS Stream) - kein Fehler an Server oder LCM; die Bewertung gelingt wieder, sobald Upstream die Daten liefert")
	}
	return fmt.Errorf("trivy-scan fehlgeschlagen: %w", err)
}

// Info liefert Scanner-Version und DB-Stand (gecacht, siehe infoTTL).
// Die Frische-Bewertung macht der Aufrufer über EvaluateCVEDB - die Domain
// kennt die Schwellen, nicht diese Infrastruktur-Schicht.
func (t *Trivy) Info(ctx context.Context) domain.CVEDBStatus {
	t.infoMu.Lock()
	defer t.infoMu.Unlock()
	if !t.infoTime.IsZero() && time.Since(t.infoTime) < infoTTL {
		return t.infoVal
	}
	st := t.run.info(ctx)
	t.infoVal = st
	t.infoTime = time.Now()
	return st
}

// ForgetInfo verwirft den gemerkten Scanner-Status. Nötig, wenn sich am Host
// etwas ändert, das in den Status einfließt - etwa eine nachinstallierte
// Sandbox: sonst zeigte die Oberfläche bis zum Ablauf der Frist weiter „ohne
// Sandbox".
func (t *Trivy) ForgetInfo() {
	t.infoMu.Lock()
	t.infoTime = time.Time{}
	t.infoMu.Unlock()
}

// UpdateDB lädt die Schwachstellen-Datenbank herunter, ohne zu scannen
// (`--download-db-only`). Danach wird der Info-Cache verworfen, damit die
// Oberfläche sofort den neuen Stand zeigt statt bis zu einer Minute den alten.
func (t *Trivy) UpdateDB(ctx context.Context) (string, error) {
	if !t.Available() {
		return "", fmt.Errorf("trivy nicht verfügbar")
	}
	out, err := t.run.updateDB(ctx)
	// Danach den gemerkten Stand verwerfen, damit die Oberfläche sofort den
	// neuen zeigt statt bis zu einer Minute den alten.
	t.ForgetInfo()
	return out, err
}

// ScanImage lässt Trivy ein Container-Image aus der Registry prüfen
// (`trivy image <ref>`). Das JSON-Ausgabeformat ist identisch zum
// SBOM-Scan; als PkgManager-Kennung wird "docker" gespeichert.
func (t *Trivy) ScanImage(ctx context.Context, ref string) ([]domain.Vulnerability, error) {
	if !t.Available() {
		return nil, fmt.Errorf("trivy nicht verfügbar")
	}
	out, err := t.run.scanImage(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("trivy-image-scan fehlgeschlagen: %w", err)
	}
	return parseReport(out, "docker")
}

// trivyReport ist der für uns relevante Ausschnitt der Trivy-JSON-Ausgabe.
type trivyReport struct {
	Results []struct {
		Vulnerabilities []struct {
			VulnerabilityID  string `json:"VulnerabilityID"`
			PkgName          string `json:"PkgName"`
			InstalledVersion string `json:"InstalledVersion"`
			FixedVersion     string `json:"FixedVersion"`
			Severity         string `json:"Severity"`
			Title            string `json:"Title"`
			PrimaryURL       string `json:"PrimaryURL"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

// parseReport wandelt die Trivy-Ausgabe in Vulnerability-Records (ohne
// ServerID - die setzt der Aufrufer beim Speichern).
func parseReport(out []byte, mgr string) ([]domain.Vulnerability, error) {
	var report trivyReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("trivy-ausgabe parsen: %w", err)
	}
	var vulns []domain.Vulnerability
	for _, res := range report.Results {
		for _, v := range res.Vulnerabilities {
			if v.VulnerabilityID == "" {
				continue
			}
			vulns = append(vulns, domain.Vulnerability{
				CVEID:            v.VulnerabilityID,
				PackageName:      v.PkgName,
				InstalledVersion: v.InstalledVersion,
				FixedVersion:     v.FixedVersion,
				Severity:         domain.NormalizeSeverity(v.Severity),
				Title:            v.Title,
				PrimaryURL:       v.PrimaryURL,
				PkgManager:       mgr,
			})
		}
	}
	return vulns, nil
}
