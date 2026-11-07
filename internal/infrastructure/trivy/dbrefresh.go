package trivy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/registry"
)

// dbRepositories sind die Quellen der Schwachstellen-Datenbank, in der
// Reihenfolge, in der auch Trivy sie versucht. Die Digests beider Quellen
// unterscheiden sich, verglichen wird also mit dem Stand der Quelle, von der
// Trivy tatsächlich lädt.
var dbRepositories = []string{
	"mirror.gcr.io/aquasec/trivy-db:2",
	"ghcr.io/aquasecurity/trivy-db:2",
}

const (
	dbDirName      = "db"
	dbStagingName  = "db-next"
	dbDigestName   = "db-digest"
	dbFileName     = "trivy.db"
	dbMetadataName = "metadata.json"

	// dbStaleAge: Ist der aktive Stand älter, traut der Abgleich dem Digest
	// nicht mehr. Hebt ein neues Trivy das Datenbank-Schema an, hat es einen
	// anderen Tag als dbRepositories, der Digest dort bleibt also stehen.
	dbStaleAge = 24 * time.Hour
	// dbBlindInterval begrenzt Downloads ohne Hinweis auf einen neuen Stand:
	// Sie kosten jedes Mal rund 140 MB.
	dbBlindInterval = 6 * time.Hour
)

// DBUpdate ist das Ergebnis eines Datenbank-Abgleichs.
type DBUpdate struct {
	// Output gehört ins Job-Protokoll und ist auch im Fehlerfall gefüllt.
	Output string
	// Updated meldet, ob tatsächlich ein neuerer Stand übernommen wurde.
	Updated bool
}

// DBDownload lädt die Datenbank in das angegebene Cache-Verzeichnis und liefert
// die Ausgabe des Werkzeugs.
type DBDownload func(ctx context.Context, cacheDir string) (string, error)

// DBRefresher hält die Schwachstellen-Datenbank aktuell, ohne laufende Scans zu
// stören.
//
// `trivy --download-db-only` taugt dafür allein nicht. Trivy hält seine
// Datenbank 24 Stunden lang für aktuell (NextUpdate) und lädt vorher nichts
// nach, obwohl der Hersteller alle sechs Stunden neu baut. Außerdem packt es
// die neue Datei über die alte aus, Scans müssten also warten.
//
// Deshalb lädt der Refresher in ein eigenes Cache-Verzeichnis, in dem es keine
// alte Datenbank und keine Sperre gibt. Die neue Datei ersetzt die alte erst,
// wenn sie vollständig ist. rename ist atomar, und ein laufender Scan liest bis
// zum Ende die Datei, die er geöffnet hat. Scheitert der Download, bleibt die
// aktive Datenbank unberührt.
type DBRefresher struct {
	cacheDir string
	download DBDownload
	registry registry.Checker

	// busy lässt nur einen Abgleich zu: Knopf, Stundentakt und der Scan-Lauf
	// teilen sich das Staging-Verzeichnis. Ein Kanal, damit das Warten den
	// Kontext des Aufrufers beachtet.
	busy chan struct{}
	// lastBlind ist der letzte Download ohne Hinweis auf einen neuen Stand.
	lastBlind time.Time
}

// NewDBRefresher erstellt den Abgleich für ein Trivy-Cache-Verzeichnis.
func NewDBRefresher(cacheDir string, download DBDownload) *DBRefresher {
	return &DBRefresher{cacheDir: cacheDir, download: download, registry: registry.New(), busy: make(chan struct{}, 1)}
}

// Refresh übernimmt einen neueren Stand, falls es einen gibt. Der Blick in die
// Registry kostet nur eine kleine Anfrage, der Download rund 140 MB.
func (r *DBRefresher) Refresh(ctx context.Context) (DBUpdate, error) {
	if r.cacheDir == "" {
		return DBUpdate{}, errors.New("trivy-datenbank-update: kein cache-verzeichnis gesetzt")
	}
	select {
	case r.busy <- struct{}{}:
		defer func() { <-r.busy }()
	case <-ctx.Done():
		return DBUpdate{}, ctx.Err()
	}

	digest, digestErr := r.remoteDigest(ctx)
	if digestErr != nil {
		slog.Warn("trivy db: stand der registry nicht abfragbar", "error", digestErr)
	}
	active, activeErr := readDBMetadata(r.path(dbDirName))
	if activeErr == nil && !r.shouldDownload(digest, digestErr, active) {
		if digestErr != nil {
			return DBUpdate{Output: "Der Stand der Registry ist nicht abfragbar: " + digestErr.Error()}, nil
		}
		return DBUpdate{Output: "Die Datenbank ist aktuell, es gibt keinen neueren Stand."}, nil
	}

	staging := r.path(dbStagingName)
	defer os.RemoveAll(staging)
	if err := os.RemoveAll(staging); err != nil {
		return DBUpdate{}, fmt.Errorf("trivy-datenbank-update: reste entfernen: %w", err)
	}
	output, err := r.download(ctx, staging)
	if err != nil {
		return DBUpdate{Output: output}, err
	}
	next, err := readDBMetadata(filepath.Join(staging, dbDirName))
	if err != nil {
		return DBUpdate{Output: output}, fmt.Errorf("trivy-datenbank-update: download unvollständig: %w", err)
	}

	if activeErr == nil && !next.UpdatedAt.After(active.UpdatedAt) {
		r.storeDigest(digest)
		return DBUpdate{Output: joinOutput(output, "Es gibt keinen neueren Stand, die aktive Datenbank bleibt.")}, nil
	}
	if err := r.activate(staging); err != nil {
		return DBUpdate{Output: output}, fmt.Errorf("trivy-datenbank-update: datenbank austauschen: %w", err)
	}
	r.storeDigest(digest)
	return DBUpdate{Output: joinOutput(output, "Neuer Stand übernommen."), Updated: true}, nil
}

// shouldDownload entscheidet bei vorhandener Datenbank. Ein neuer Digest ist
// der Normalfall. Ohne ihn lädt der Abgleich nur, wenn er dem Digest nicht
// trauen kann, und auch das nur selten: Der Download bringt dann entweder den
// Stand, der anschließend verglichen wird, oder die volle Trivy-Ausgabe.
func (r *DBRefresher) shouldDownload(digest string, digestErr error, active dbMetadata) bool {
	if digestErr == nil && digest != r.storedDigest() {
		return true
	}
	blind := digestErr != nil || time.Since(active.UpdatedAt) > dbStaleAge
	if !blind || time.Since(r.lastBlind) < dbBlindInterval {
		return false
	}
	r.lastBlind = time.Now()
	return true
}

// activate ersetzt die aktive Datenbank. Die Metadaten folgen als Letztes:
// Sie benennen den Stand, und ohne gespeicherten Digest prüft der nächste
// Abgleich ohnehin nach, falls hier etwas abbricht.
func (r *DBRefresher) activate(staging string) error {
	dir := r.path(dbDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{dbFileName, dbMetadataName} {
		if err := os.Rename(filepath.Join(staging, dbDirName, name), filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// remoteDigest liefert den Stand der ersten erreichbaren Quelle.
func (r *DBRefresher) remoteDigest(ctx context.Context) (string, error) {
	var failures []string
	for _, ref := range dbRepositories {
		res := r.registry.CheckDigest(ctx, ref)
		if res.Status == domain.DockerCheckOK {
			return res.Digest, nil
		}
		failures = append(failures, ref+": "+res.Err)
	}
	return "", errors.New(strings.Join(failures, "; "))
}

func (r *DBRefresher) storedDigest() string {
	stored, err := os.ReadFile(r.path(dbDigestName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(stored))
}

// storeDigest merkt sich den übernommenen Stand. Ein Fehler kostet nur einen
// überflüssigen Download beim nächsten Abgleich.
func (r *DBRefresher) storeDigest(digest string) {
	if digest == "" {
		return
	}
	if err := os.WriteFile(r.path(dbDigestName), []byte(digest+"\n"), 0o644); err != nil { //nolint:gosec // kein Geheimnis, liegt neben der Datenbank
		slog.Warn("trivy db: digest nicht gespeichert", "error", err)
	}
}

func (r *DBRefresher) path(name string) string { return filepath.Join(r.cacheDir, name) }

// readDBMetadata liest den Stand einer Datenbank und prüft, dass sie vollständig
// vorliegt.
func readDBMetadata(dir string) (dbMetadata, error) {
	var meta dbMetadata
	info, err := os.Stat(filepath.Join(dir, dbFileName))
	if err != nil {
		return meta, err
	}
	if info.Size() == 0 {
		return meta, errors.New(dbFileName + " ist leer")
	}
	raw, err := os.ReadFile(filepath.Join(dir, dbMetadataName))
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return meta, fmt.Errorf("%s nicht lesbar: %w", dbMetadataName, err)
	}
	if meta.UpdatedAt.IsZero() {
		return meta, errors.New(dbMetadataName + " nennt keinen stand")
	}
	return meta, nil
}

// dbMetadata ist der benötigte Ausschnitt aus Trivys metadata.json.
type dbMetadata struct {
	UpdatedAt time.Time `json:"UpdatedAt"`
}

func joinOutput(output, note string) string {
	return strings.TrimSpace(output + "\n" + note)
}
