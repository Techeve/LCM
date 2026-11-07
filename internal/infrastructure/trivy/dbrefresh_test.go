package trivy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/registry"
)

// Beide Stände liegen relativ zu jetzt: Über dbStaleAge hinaus verhält sich der
// Abgleich anders.
var (
	dbStandAlt = time.Now().Add(-7 * time.Hour)
	dbStandNeu = time.Now().Add(-time.Hour)
)

// dbFixture ist ein Cache-Verzeichnis samt Registry-Attrappe. Der Download
// schreibt den Stand aus stand in das übergebene Verzeichnis.
type dbFixture struct {
	dir       string
	stand     time.Time
	downloads int
	registry  *registry.Fake
	refresher *DBRefresher
}

func newDBFixture(t *testing.T) *dbFixture {
	t.Helper()
	f := &dbFixture{dir: t.TempDir(), stand: dbStandNeu, registry: &registry.Fake{Results: map[string]registry.Result{}}}
	f.setDigest("sha256:neu")
	f.refresher = NewDBRefresher(f.dir, func(_ context.Context, cacheDir string) (string, error) {
		f.downloads++
		writeTestDB(t, filepath.Join(cacheDir, dbDirName), "inhalt "+f.stand.Format(time.RFC3339), f.stand)
		return "download", nil
	})
	f.refresher.registry = f.registry
	return f
}

func (f *dbFixture) setDigest(digest string) {
	f.registry.Results[dbRepositories[0]] = registry.Result{Digest: digest, Status: domain.DockerCheckOK}
}

func (f *dbFixture) activeContent(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(f.dir, dbDirName, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// assertNoStaging: Das Staging-Verzeichnis belegt 1,7 GB und darf nicht liegen
// bleiben.
func (f *dbFixture) assertNoStaging(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(f.dir, dbStagingName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staging-verzeichnis ist liegen geblieben: %v", err)
	}
}

func writeTestDB(t *testing.T, dir, content string, stand time.Time) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, dbFileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(dbMetadata{UpdatedAt: stand})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, dbMetadataName), meta, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDBRefreshErsterLaufUebernimmtDenStand(t *testing.T) {
	f := newDBFixture(t)

	update, err := f.refresher.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !update.Updated {
		t.Errorf("neue datenbank nicht gemeldet: %+v", update)
	}
	if got := f.refresher.storedDigest(); got != "sha256:neu" {
		t.Errorf("digest %q", got)
	}
	f.assertNoStaging(t)
}

// Das ist der teure Fall, der jede Stunde eintritt: Ohne neuen Stand darf kein
// Download laufen.
func TestDBRefreshOhneNeuenStandLaedtNichts(t *testing.T) {
	f := newDBFixture(t)
	if _, err := f.refresher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	update, err := f.refresher.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if update.Updated || f.downloads != 1 {
		t.Errorf("updated %v, %d downloads", update.Updated, f.downloads)
	}
}

// Trivy selbst liefe hier nicht los: Sein NextUpdate liegt 24 Stunden in der
// Zukunft. Der Refresher lädt, sobald die Registry einen neuen Stand hat.
func TestDBRefreshHoltNeuenStandVorTrivysFrist(t *testing.T) {
	f := newDBFixture(t)
	writeTestDB(t, filepath.Join(f.dir, dbDirName), "alt", dbStandAlt)
	f.refresher.storeDigest("sha256:alt")

	update, err := f.refresher.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !update.Updated || f.activeContent(t) == "alt" {
		t.Errorf("neuer stand nicht übernommen: %+v, inhalt %q", update, f.activeContent(t))
	}
}

// Ein neuer Digest mit altem Inhalt darf nicht jede Stunde neu laden.
func TestDBRefreshBehaeltGleichenStand(t *testing.T) {
	f := newDBFixture(t)
	f.stand = dbStandAlt
	writeTestDB(t, filepath.Join(f.dir, dbDirName), "aktiv", dbStandAlt)

	update, err := f.refresher.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if update.Updated || f.activeContent(t) != "aktiv" {
		t.Errorf("gleicher stand wurde getauscht: %+v", update)
	}
	if _, err := f.refresher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.downloads != 1 {
		t.Errorf("%d downloads, erwartet 1", f.downloads)
	}
	f.assertNoStaging(t)
}

func TestDBRefreshFehlschlagLaesstAktiveDatenbankUnberuehrt(t *testing.T) {
	cases := []struct {
		name     string
		download DBDownload
	}{
		{"Download scheitert", func(context.Context, string) (string, error) {
			return "rate limit", errors.New("trivy-datenbank-update fehlgeschlagen")
		}},
		{"Download unvollständig", func(_ context.Context, cacheDir string) (string, error) {
			return "rate limit", os.MkdirAll(filepath.Join(cacheDir, dbDirName), 0o755)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDBFixture(t)
			writeTestDB(t, filepath.Join(f.dir, dbDirName), "aktiv", dbStandAlt)
			f.refresher.download = tc.download

			update, err := f.refresher.Refresh(context.Background())
			if err == nil {
				t.Fatal("fehlschlag nicht gemeldet")
			}
			if update.Output != "rate limit" {
				t.Errorf("ausgabe fehlt: %q", update.Output)
			}
			if f.activeContent(t) != "aktiv" || f.refresher.storedDigest() != "" {
				t.Errorf("zustand verändert: inhalt %q, digest %q", f.activeContent(t), f.refresher.storedDigest())
			}
			f.assertNoStaging(t)
		})
	}
}

// Ist die Registry nicht abfragbar, entscheidet der Download. Ein Stand ohne
// bekannten Digest darf den gemerkten nicht überschreiben.
func TestDBRefreshOhneRegistryLaedtDirekt(t *testing.T) {
	f := newDBFixture(t)
	failed := registry.Result{Status: domain.DockerCheckError, Err: "kein netz"}
	for _, ref := range dbRepositories {
		f.registry.Results[ref] = failed
	}

	update, err := f.refresher.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !update.Updated || f.refresher.storedDigest() != "" {
		t.Errorf("updated %v, digest %q", update.Updated, f.refresher.storedDigest())
	}
}

// Ohne Registry darf die Stunde nicht jedes Mal 140 MB kosten.
func TestDBRefreshOhneRegistryDrosseltDownloads(t *testing.T) {
	f := newDBFixture(t)
	writeTestDB(t, filepath.Join(f.dir, dbDirName), "aktiv", dbStandAlt)
	for _, ref := range dbRepositories {
		f.registry.Results[ref] = registry.Result{Status: domain.DockerCheckError, Err: "kein netz"}
	}

	for range 3 {
		if _, err := f.refresher.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if f.downloads != 1 {
		t.Errorf("%d downloads, erwartet 1", f.downloads)
	}
}

// Hebt ein neues Trivy das Schema an, bleibt der beobachtete Digest stehen.
// Der Abgleich darf sich davon nicht dauerhaft täuschen lassen.
func TestDBRefreshHoltAltenStandTrotzGleichemDigest(t *testing.T) {
	f := newDBFixture(t)
	writeTestDB(t, filepath.Join(f.dir, dbDirName), "alt", time.Now().Add(-2*dbStaleAge))
	f.refresher.storeDigest("sha256:neu")

	update, err := f.refresher.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !update.Updated {
		t.Errorf("alter stand wurde nicht ersetzt: %+v", update)
	}
}

func TestDBRefreshWartenBeachtetDenKontext(t *testing.T) {
	f := newDBFixture(t)
	f.refresher.busy <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := f.refresher.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("fehler %v, erwartet context.Canceled", err)
	}
}

func TestDBRefreshFaelltAufZweiteQuelleZurueck(t *testing.T) {
	f := newDBFixture(t)
	f.registry.Results[dbRepositories[0]] = registry.Result{Status: domain.DockerCheckError, Err: "gestört"}
	f.registry.Results[dbRepositories[1]] = registry.Result{Digest: "sha256:zweite", Status: domain.DockerCheckOK}

	if _, err := f.refresher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.refresher.storedDigest(); got != "sha256:zweite" {
		t.Errorf("digest %q", got)
	}
}

// Das ist der Kern des Tauschs: Ein laufender Scan liest bis zum Ende die alte
// Datei, der nächste bekommt die neue.
func TestDBRefreshLaesstOffeneDateiUnberuehrt(t *testing.T) {
	f := newDBFixture(t)
	writeTestDB(t, filepath.Join(f.dir, dbDirName), "alt", dbStandAlt)
	open, err := os.Open(filepath.Join(f.dir, dbDirName, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()

	if _, err := f.refresher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 3)
	if _, err := open.Read(buf); err != nil || string(buf) != "alt" {
		t.Errorf("offene datei liefert %q (%v)", buf, err)
	}
	if f.activeContent(t) == "alt" {
		t.Error("neuer scan bekäme die alte datei")
	}
}

func TestDBRefreshOhneCacheVerzeichnis(t *testing.T) {
	if _, err := NewDBRefresher("", nil).Refresh(context.Background()); err == nil {
		t.Fatal("fehler erwartet")
	}
}

type dbRunnerStub struct{ blockingRunner }

func (*dbRunnerStub) updateDB(context.Context) (DBUpdate, error) {
	return DBUpdate{Updated: true}, nil
}

// Scans warten nicht auf den Datenbank-Abgleich, und er nicht auf sie.
func TestDatenbankAbgleichBrauchtKeinenScanPlatz(t *testing.T) {
	tv := newTrivy(&dbRunnerStub{})
	release, err := tv.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	update, err := tv.UpdateDB(ctx)
	if err != nil || !update.Updated {
		t.Errorf("update %+v, fehler %v", update, err)
	}
}
