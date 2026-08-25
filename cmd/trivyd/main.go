// Command trivyd ist der Trivy-Sidecar: ein winziger HTTP-Dienst, der im
// selben Container wie Trivy liegt und dessen Kommandozeile für LCM bedient.
//
// Warum es ihn gibt: LCMs Runtime-Image ist ein Scratch-Image - keine Shell,
// kein Trivy. Trivys eigener Client/Server-Modus hilft nicht, weil sein
// Client wieder das Trivy-Binary ist (~90 MB); es müsste also doch ins
// LCM-Image und läge dann doppelt vor. Trivys internes Server-Protokoll
// direkt zu sprechen wäre möglich, koppelt aber an ein versionsgebundenes
// Schema, das nicht als Schnittstelle gedacht ist. Dieser Dienst ruft statt
// dessen die DOKUMENTIERTE Kommandozeile auf und reicht deren JSON durch -
// unverändert, damit LCM es mit demselben Parser auswertet wie beim lokalen
// Lauf.
//
// Betrieb: im internen Container-Netz, ohne veröffentlichten Port, mit
// Pflicht-Token. Details: docs/reference/packaging.md
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"LCM/internal/infrastructure/trivy"
)

func main() {
	addr := flag.String("listen", ":9330", "Adresse, auf der gelauscht wird")
	binary := flag.String("trivy", "trivy", "Pfad zum Trivy-Binary")
	cacheDir := flag.String("cache", "/cache", "Verzeichnis für die Schwachstellen-Datenbank")
	healthcheck := flag.Bool("healthcheck", false, "Eigenen Health-Endpunkt prüfen und mit 0 bzw. 1 beenden")
	flag.Parse()

	if *healthcheck {
		if err := probeSelf(*addr); err != nil {
			fmt.Fprintln(os.Stderr, "nicht gesund:", err)
			os.Exit(1)
		}
		return
	}

	// Ohne Token gar nicht erst starten. Ein offener Endpunkt, der Prozesse
	// startet und Images aus fremden Registries zieht, ist kein Zustand, den
	// man versehentlich erreichen darf - und ein Standard-Token wäre keiner.
	token := strings.TrimSpace(os.Getenv("LCM_TRIVY_TOKEN"))
	if token == "" {
		fmt.Fprintln(os.Stderr,
			"FEHLER: LCM_TRIVY_TOKEN ist nicht gesetzt. Der Dienst startet ohne Token nicht - "+
				"er führt Trivy aus und lädt Images aus dem Netz.")
		os.Exit(2)
	}

	srv := &server{trivy: *binary, cache: *cacheDir, token: token}
	slog.Info("trivyd gestartet", "address", *addr, "cache", *cacheDir)
	if err := http.ListenAndServe(*addr, srv.routes()); err != nil { //nolint:gosec // Zeitlimits setzt der Aufrufer; hier zaehlt der lange DB-Download
		slog.Error("trivyd beendet", "error", err)
		os.Exit(1)
	}
}

type server struct {
	trivy string
	cache string
	token string
	// Ein Cache, ein Lauf: Trivy sperrt sein Cache-Verzeichnis, und zwei
	// gleichzeitige Läufe blockieren sich gegenseitig bis ins Zeitlimit.
	// Serialisieren ist ehrlicher als parallel scheitern.
	mu sync.Mutex
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	// Ohne Token: Der Healthcheck sagt nur, dass der Prozess lebt.
	mux.HandleFunc("GET "+trivy.PathHealth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("GET "+trivy.PathInfo, s.auth(s.handleInfo))
	mux.Handle("POST "+trivy.PathScanSBOM, s.auth(s.handleScanSBOM))
	mux.Handle("POST "+trivy.PathScanImage, s.auth(s.handleScanImage))
	mux.Handle("POST "+trivy.PathUpdateDB, s.auth(s.handleUpdateDB))
	return mux
}

// auth verlangt das Bearer-Token. Der Vergleich ist absichtlich schlicht:
// Der Dienst ist nur im internen Container-Netz erreichbar, und ein
// Zeitangriff über diese Strecke setzt bereits Zugang zu ebendiesem Netz
// voraus.
func (s *server) auth(next func(http.ResponseWriter, *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			writeError(w, http.StatusUnauthorized, "ungültiges oder fehlendes Token")
			return
		}
		next(w, r)
	})
}

// maxSBOM begrenzt die Anfrage. Ein SBOM mit einigen tausend Paketen liegt im
// niedrigen einstelligen MB-Bereich; alles darüber ist kein Paketbestand mehr.
const maxSBOM = 32 << 20 // 32 MiB

const (
	sbomTimeout     = 5 * time.Minute
	imageTimeout    = 15 * time.Minute
	dbUpdateTimeout = 10 * time.Minute
	versionTimeout  = 30 * time.Second
)

func (s *server) handleScanSBOM(w http.ResponseWriter, r *http.Request) {
	sbom, err := io.ReadAll(io.LimitReader(r.Body, maxSBOM))
	if err != nil || len(sbom) == 0 {
		writeError(w, http.StatusBadRequest, "kein lesbares SBOM im Anfragekörper")
		return
	}
	f, err := os.CreateTemp("", "lcm-sbom-*.json")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(sbom); err != nil {
		f.Close()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	f.Close()

	// --skip-db-update wie beim lokalen Lauf: Der Scan wertet aus, was da ist.
	// Nachgeladen wird ausschliesslich über /db/update - sonst bricht ein Scan
	// mitten im Betrieb ab, wenn Trivy seine Datenbank für abgelaufen hält.
	args := []string{"sbom", "--format", "json", "--quiet", "--scanners", "vuln",
		"--skip-db-update", "--cache-dir", s.cache, f.Name()}
	out, err := s.runTrivy(r, sbomTimeout, args...)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, out)
}

func (s *server) handleScanImage(w http.ResponseWriter, r *http.Request) {
	var req trivy.ImageRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "anfrage nicht lesbar: "+err.Error())
		return
	}
	ref := strings.TrimSpace(req.Ref)
	// Die Referenz landet als Argument in einem Prozessaufruf. Ohne Shell
	// dazwischen ist sie kein Einfallstor für Kommando-Verkettung - ein
	// führender Bindestrich würde aber als OPTION gelesen und könnte Trivys
	// Verhalten umstellen.
	if ref == "" || strings.HasPrefix(ref, "-") {
		writeError(w, http.StatusBadRequest, "ungültige image-referenz")
		return
	}
	args := []string{"image", "--format", "json", "--quiet", "--scanners", "vuln",
		"--cache-dir", s.cache, ref}
	out, err := s.runTrivy(r, imageTimeout, args...)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, out)
}

func (s *server) handleUpdateDB(w http.ResponseWriter, r *http.Request) {
	out, err := s.runTrivy(r, dbUpdateTimeout, "image", "--download-db-only", "--cache-dir", s.cache)
	if err != nil {
		// Die Ausgabe des Werkzeugs gehört auch im Fehlerfall zum Betreiber -
		// dort stehen Proxy-, Rate-Limit- und Netzfehler im Klartext.
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, mustJSON(trivy.UpdateResponse{Output: strings.TrimSpace(string(out))}))
}

func (s *server) handleInfo(w http.ResponseWriter, r *http.Request) {
	resp := trivy.InfoResponse{}
	out, err := s.runTrivy(r, versionTimeout, "--version", "--format", "json", "--cache-dir", s.cache)
	if err != nil {
		resp.Error = err.Error()
		writeJSON(w, mustJSON(resp))
		return
	}
	var v struct {
		Version         string `json:"Version"`
		VulnerabilityDB *struct {
			UpdatedAt    *time.Time `json:"UpdatedAt"`
			NextUpdate   *time.Time `json:"NextUpdate"`
			DownloadedAt *time.Time `json:"DownloadedAt"`
		} `json:"VulnerabilityDB"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		resp.Error = "trivy-versionsausgabe parsen: " + err.Error()
		writeJSON(w, mustJSON(resp))
		return
	}
	resp.Available = true
	resp.Version = v.Version
	if v.VulnerabilityDB != nil {
		resp.UpdatedAt = rfc3339(v.VulnerabilityDB.UpdatedAt)
		resp.NextUpdate = rfc3339(v.VulnerabilityDB.NextUpdate)
		resp.DownloadedAt = rfc3339(v.VulnerabilityDB.DownloadedAt)
	}
	// Kein Zeitstempel heißt: noch nie geladen. Das klar benennen, statt es
	// als fehlenden Wert durchgehen zu lassen - sonst sieht „keine Funde" aus
	// wie eine Entwarnung.
	if resp.UpdatedAt == nil {
		resp.Error = "keine Schwachstellen-Datenbank vorhanden - sie wurde noch nie geladen"
	}
	writeJSON(w, mustJSON(resp))
}

// runTrivy führt einen Trivy-Aufruf aus. Serialisiert (ein Cache, ein Lauf)
// und an das Zeitlimit sowie den Abbruch der Anfrage gebunden: Bricht LCM ab,
// läuft hier kein verwaister Scan weiter.
//
// Keine zusätzliche Sandbox: Der Container IST die Grenze. Er enthält weder
// LCMs Datenbank noch den Master-Key - es gibt schlicht nichts zu erreichen.
// Bubblewrap bräuchte obendrein Rechte, die ein gehärteter Container gerade
// nicht hat.
func (s *server) runTrivy(r *http.Request, timeout time.Duration, args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := contextWithTimeout(r, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.trivy, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	// Beim Datenbank-Update schreibt Trivy seinen Fortschritt auf stderr -
	// stdout ist dann leer, und der Fortschritt ist genau das, was der
	// Betreiber im Job-Protokoll sehen will.
	if stdout.Len() == 0 {
		return []byte(strings.TrimSpace(stderr.String())), nil
	}
	return []byte(stdout.String()), nil
}

func writeJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(mustJSON(trivy.ErrorResponse{Error: msg}))
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"error":"antwort konnte nicht serialisiert werden"}`)
	}
	return b
}

func rfc3339(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
