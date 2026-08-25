package trivy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"LCM/internal/core/domain"
)

// remoteRunner spricht mit dem Trivy-Sidecar (cmd/trivyd) - der Weg im
// Container-Betrieb, wo LCMs Image weder Shell noch Trivy-Binary enthält.
type remoteRunner struct {
	base   string // z.B. http://trivy:9330
	token  string
	client *http.Client
}

// maxSidecarResponse begrenzt, wie viel wir aus einer Antwort lesen. Ein
// Trivy-Bericht ist groß, aber nicht beliebig groß; ohne Grenze zöge eine
// defekte oder feindliche Gegenstelle LCM den Speicher leer.
const maxSidecarResponse = 64 << 20 // 64 MiB

// newRemote erstellt den Sidecar-Weg. Das Zeitlimit deckt den längsten Fall
// ab (Datenbank-Download); die kürzeren Fristen setzt der Sidecar selbst.
func newRemote(baseURL, token string) *remoteRunner {
	return &remoteRunner{
		base:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:  strings.TrimSpace(token),
		client: &http.Client{Timeout: dbUpdateTimeout},
	}
}

// available meldet, ob ein Sidecar KONFIGURIERT ist - nicht, ob er antwortet.
//
// Das ist wichtig und bewusst so: Available()==false heißt an allen
// Aufrufstellen „Feature nicht in Benutzung" und lässt Ampel, Docker-Prüfung
// und den cve_db_stale-Alarm schweigen. Wäre ein toter Sidecar „nicht
// verfügbar", sähe ein Ausfall exakt aus wie ein abgeschalteter CVE-Scan -
// also wie „alles in Ordnung". Nichterreichbarkeit ist deshalb ein FEHLER,
// der in Info() und beim Scan mit Klartext ankommt.
func (r *remoteRunner) available() bool { return r != nil && r.base != "" }

func (r *remoteRunner) scanSBOM(ctx context.Context, sbom []byte) ([]byte, error) {
	// Kein Umweg über eine Datei: Das SBOM geht direkt über die Leitung.
	return r.post(ctx, PathScanSBOM, "application/json", sbom)
}

func (r *remoteRunner) scanImage(ctx context.Context, ref string) ([]byte, error) {
	body, err := json.Marshal(ImageRequest{Ref: ref})
	if err != nil {
		return nil, err
	}
	return r.post(ctx, PathScanImage, "application/json", body)
}

func (r *remoteRunner) info(ctx context.Context) domain.CVEDBStatus {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := r.get(ctx, PathInfo)
	if err != nil {
		// Ein unerreichbarer Sidecar ist ein Missstand, kein „aus".
		return domain.CVEDBStatus{
			Available: true, Freshness: domain.CVEDBUnknown,
			SandboxBackend: SidecarBackend,
			Error:          fmt.Sprintf("Trivy-Sidecar unter %s nicht erreichbar: %v", r.base, err),
		}
	}
	var resp InfoResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return domain.CVEDBStatus{
			Available: true, Freshness: domain.CVEDBUnknown,
			SandboxBackend: SidecarBackend,
			Error:          fmt.Sprintf("Antwort des Trivy-Sidecars nicht lesbar: %v", err),
		}
	}
	return infoFromSidecar(resp)
}

func (r *remoteRunner) updateDB(ctx context.Context) (string, error) {
	out, err := r.post(ctx, PathUpdateDB, "application/json", []byte("{}"))
	if err != nil {
		return "", err
	}
	var resp UpdateResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("antwort des trivy-sidecars nicht lesbar: %w", err)
	}
	return resp.Output, nil
}

func (r *remoteRunner) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+path, nil)
	if err != nil {
		return nil, err
	}
	return r.do(req)
}

func (r *remoteRunner) post(ctx context.Context, path, contentType string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return r.do(req)
}

func (r *remoteRunner) do(req *http.Request) ([]byte, error) {
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trivy-sidecar %s: %w", r.base, err)
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(io.LimitReader(resp.Body, maxSidecarResponse))
	if err != nil {
		return nil, fmt.Errorf("antwort des trivy-sidecars lesen: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, sidecarError(resp.StatusCode, out)
	}
	return out, nil
}

// sidecarError macht aus einem Fehlerstatus einen Satz, mit dem der Betreiber
// etwas anfangen kann. 401 ist der häufigste Einrichtungsfehler und bekommt
// deshalb einen eigenen Hinweis statt einer nackten Zahl.
func sidecarError(status int, body []byte) error {
	var e ErrorResponse
	if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
		if status == http.StatusUnauthorized {
			return fmt.Errorf("der Trivy-Sidecar weist den Zugang ab (%s) - stimmt das Token auf beiden Seiten (LCM_TRIVY_TOKEN)?", e.Error)
		}
		return fmt.Errorf("trivy-sidecar meldet HTTP %d: %s", status, e.Error)
	}
	if status == http.StatusUnauthorized {
		return fmt.Errorf("der Trivy-Sidecar weist den Zugang ab (HTTP 401) - stimmt das Token auf beiden Seiten (LCM_TRIVY_TOKEN)?")
	}
	return fmt.Errorf("trivy-sidecar meldet HTTP %d: %s", status, strings.TrimSpace(string(body)))
}

// parseTime liest einen RFC-3339-Zeitpunkt; nil und Unlesbares ergeben nil.
func parseTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	return &t
}
