package advisories

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/version"
)

// LocalOSV beantwortet die Frühwarn-Abfragen aus einer lokalen Kopie der
// OSV-Datenbank statt über deren API.
//
// Der Zweck ist Vertraulichkeit: Im Online-Betrieb geht die Liste der
// installierten Pakete - wenn auch dedupliziert und ohne Serverbezug - an
// einen fremden Dienst. Wer das nicht will, spiegelt die Datenbank einmal
// täglich und fragt sie danach im Haus ab. Der Preis steht fest und wird
// nicht schöngeredet: Die Frühwarn-Latenz steigt vom Minutentakt auf den
// Rhythmus des Spiegels, also etwa einen Tag.
//
// Gespiegelt wird nur, was die eigene Flotte braucht. Der Debian-Dump allein
// umfasst über 64.000 Meldungen und rund 73 MB; ein Index über alle
// Distributionen und Versionen wäre um ein Vielfaches größer als der
// tatsächlich benötigte Ausschnitt.
type LocalOSV struct {
	dir     string
	baseURL string
	http    *http.Client

	mu    sync.RWMutex
	index map[string][]localEntry // Schlüssel: "<Ökosystem>\x00<Paket>"
	// loadedAt hält fest, welcher Indexstand geladen ist - nach einem
	// Spiegellauf wird neu eingelesen.
	loadedAt time.Time
}

// localEntry ist eine betroffene Versionsspanne eines Pakets.
type localEntry struct {
	AdvisoryID string   `json:"id"`
	Introduced string   `json:"introduced"`
	Fixed      string   `json:"fixed"`
	Severity   string   `json:"severity"`
	Title      string   `json:"title"`
	Aliases    []string `json:"aliases,omitempty"`
	Modified   string   `json:"modified,omitempty"`
}

const osvBucketURL = "https://storage.googleapis.com/osv-vulnerabilities"

// mirrorTimeout: Der Dump ist einige zehn MB groß und wird einmal täglich
// geholt - großzügig, aber nicht unbegrenzt.
const mirrorTimeout = 30 * time.Minute

// indexFile ist der Name der aufbereiteten Kopie im Datenverzeichnis.
const indexFile = "osv-index.json"

func NewLocalOSV(dir, baseURL string) *LocalOSV {
	if baseURL == "" {
		baseURL = osvBucketURL
	}
	return &LocalOSV{
		dir:     dir,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: mirrorTimeout},
	}
}

func (l *LocalOSV) Name() string { return domain.AdvisorySourceOSV }

// Available meldet, ob eine brauchbare Kopie vorliegt. Ohne sie darf die
// Quelle NICHT als verfügbar gelten: Sie würde sonst für jedes Paket „nichts
// gefunden" melden - also ein sauberes Ergebnis für etwas, das nie geprüft
// wurde.
func (l *LocalOSV) Available() bool {
	if l == nil || l.dir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(l.dir, indexFile))
	return err == nil && info.Size() > 0
}

// MirroredAt liefert den Stand der Kopie (Nullzeit = keine vorhanden).
func (l *LocalOSV) MirroredAt() time.Time {
	info, err := os.Stat(filepath.Join(l.dir, indexFile))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// Refresh holt die Dumps der benötigten Ökosysteme und baut daraus den
// Index. ecosystems sind OSV-Unterökosysteme wie "Debian:12" oder
// "Ubuntu:22.04" - heruntergeladen wird jeweils das Eltern-Ökosystem
// (OSV stellt die Unterökosysteme nicht mehr einzeln bereit), gefiltert
// wird lokal.
func (l *LocalOSV) Refresh(ctx context.Context, ecosystems []string) (string, error) {
	if l.dir == "" {
		return "", fmt.Errorf("kein verzeichnis für die lokale kopie gesetzt")
	}
	if len(ecosystems) == 0 {
		return "Keine Distributionen im Bestand - nichts zu spiegeln.", nil
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return "", err
	}

	wanted := make(map[string]bool, len(ecosystems))
	parents := map[string]bool{}
	for _, eco := range ecosystems {
		wanted[eco] = true
		parents[parentEcosystem(eco)] = true
	}

	index := map[string][]localEntry{}
	var advisories int
	for parent := range parents {
		n, err := l.mirrorEcosystem(ctx, parent, wanted, index)
		if err != nil {
			return "", fmt.Errorf("%s: %w", parent, err)
		}
		advisories += n
	}

	// Erst in eine Nebendatei schreiben, dann umbenennen: Ein Abbruch mitten
	// im Schreiben hinterlässt sonst einen halben Index, den die Abfrage für
	// vollständig hielte.
	tmp := filepath.Join(l.dir, indexFile+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if err := json.NewEncoder(f).Encode(index); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, filepath.Join(l.dir, indexFile)); err != nil {
		return "", err
	}

	l.mu.Lock()
	l.index = index
	l.loadedAt = time.Now()
	l.mu.Unlock()

	return fmt.Sprintf("%d meldung(en) für %s gespiegelt",
		advisories, strings.Join(sortedKeys(wanted), ", ")), nil
}

// mirrorEcosystem lädt den Dump eines Eltern-Ökosystems und übernimmt daraus
// die Einträge der gewünschten Unterökosysteme.
func (l *LocalOSV) mirrorEcosystem(ctx context.Context, parent string, wanted map[string]bool, index map[string][]localEntry) (int, error) {
	url := l.baseURL + "/" + parent + "/all.zip"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "LCM/"+version.Version)
	res, err := l.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("dump nicht abrufbar: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("dump nicht abrufbar: http %d", res.StatusCode)
	}

	// Der Dump geht in eine temporäre Datei: Für das Lesen des Zip-Archivs
	// wird wahlfreier Zugriff gebraucht, und zig MB im Speicher zu halten
	// wäre auf einer kleinen Maschine unhöflich.
	tmp, err := os.CreateTemp(l.dir, "osv-dump-*.zip")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	size, err := io.Copy(tmp, res.Body)
	if err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}

	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return 0, fmt.Errorf("dump nicht lesbar (%d bytes): %w", size, err)
	}
	defer zr.Close()

	count := 0
	for _, file := range zr.File {
		if !strings.HasSuffix(file.Name, ".json") {
			continue
		}
		rec, err := readOSVRecord(file)
		if err != nil {
			// Eine kaputte Einzeldatei darf den Spiegel nicht kippen.
			slog.Debug("osv mirror: record unreadable", "file", file.Name, "error", err)
			continue
		}
		if addRecord(rec, wanted, index) {
			count++
		}
	}
	return count, nil
}

// osvRecord ist der Ausschnitt eines OSV-Datensatzes, den der Index braucht.
type osvRecord struct {
	ID       string   `json:"id"`
	Summary  string   `json:"summary"`
	Modified string   `json:"modified"`
	Aliases  []string `json:"aliases"`
	Affected []struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		Ranges []struct {
			Type   string `json:"type"`
			Events []struct {
				Introduced string `json:"introduced"`
				Fixed      string `json:"fixed"`
			} `json:"events"`
		} `json:"ranges"`
		EcosystemSpecific struct {
			Urgency string `json:"urgency"`
		} `json:"ecosystem_specific"`
	} `json:"affected"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
}

func readOSVRecord(file *zip.File) (*osvRecord, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var rec osvRecord
	if err := json.NewDecoder(rc).Decode(&rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// addRecord übernimmt die passenden Versionsspannen eines Datensatzes in den
// Index. Rückgabe: ob überhaupt etwas übernommen wurde.
func addRecord(rec *osvRecord, wanted map[string]bool, index map[string][]localEntry) bool {
	used := false
	for _, aff := range rec.Affected {
		eco, name := aff.Package.Ecosystem, aff.Package.Name
		if name == "" || !wanted[eco] {
			continue
		}
		for _, rng := range aff.Ranges {
			introduced, fixed := "", ""
			for _, ev := range rng.Events {
				if ev.Introduced != "" {
					introduced = ev.Introduced
				}
				if ev.Fixed != "" {
					fixed = ev.Fixed
				}
			}
			key := indexKey(eco, name)
			index[key] = append(index[key], localEntry{
				AdvisoryID: rec.ID,
				Introduced: introduced,
				Fixed:      fixed,
				Severity:   recordSeverity(rec, aff.EcosystemSpecific.Urgency),
				Title:      rec.Summary,
				Aliases:    rec.Aliases,
				Modified:   rec.Modified,
			})
			used = true
		}
	}
	return used
}

// recordSeverity bestimmt die Schwere eines Datensatzes. Die Dringlichkeit
// kommt je Paket-Eintrag, weil dieselbe Meldung auf verschiedenen
// Distributionsstaenden verschieden dringend sein kann.
func recordSeverity(rec *osvRecord, urgency string) string {
	vectors := make([]string, 0, len(rec.Severity))
	for _, sev := range rec.Severity {
		vectors = append(vectors, sev.Score)
	}
	return severityFrom(rec.DatabaseSpecific.Severity, vectors, urgency)
}

// Query beantwortet die purl-Abfragen aus dem lokalen Index.
func (l *LocalOSV) Query(_ context.Context, purls []string) (map[string][]Advisory, error) {
	index, err := l.load()
	if err != nil {
		return nil, err
	}
	out := map[string][]Advisory{}
	for _, purl := range purls {
		p, ok := parsePurl(purl)
		if !ok {
			continue
		}
		for _, entry := range index[indexKey(p.ecosystem, p.name)] {
			if affected(p.pkgType, p.version, entry) {
				out[purl] = append(out[purl], Advisory{ID: entry.AdvisoryID})
			}
		}
	}
	return out, nil
}

// Details liest die Beschreibungen aus demselben Index - die lokale Kopie
// enthält sie bereits, ein zweiter Abruf wäre sinnlos.
func (l *LocalOSV) Details(_ context.Context, ids []string) (map[string]Detail, error) {
	index, err := l.load()
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	out := make(map[string]Detail, len(ids))
	for key, entries := range index {
		_, pkg, _ := strings.Cut(key, "\x00")
		for i := range entries {
			e := &entries[i]
			if !wanted[e.AdvisoryID] {
				continue
			}
			d, seen := out[e.AdvisoryID]
			if !seen {
				d = Detail{
					ID: e.AdvisoryID, Severity: e.Severity, Title: e.Title,
					URL:           "https://osv.dev/vulnerability/" + e.AdvisoryID,
					Aliases:       e.Aliases,
					FixedVersions: map[string]string{},
				}
				if t, err := time.Parse(time.RFC3339, e.Modified); err == nil {
					d.Modified = t
				}
			}
			if e.Fixed != "" && d.FixedVersions[pkg] == "" {
				d.FixedVersions[pkg] = e.Fixed
			}
			out[e.AdvisoryID] = d
		}
	}
	return out, nil
}

// affected entscheidet, ob eine installierte Version in die Spanne fällt.
//
// Die Auslegung folgt OSV: „introduced" ist einschließend, „fixed"
// ausschließend. Fehlt „fixed", ist die Lücke offen - dann gilt alles ab
// „introduced" als betroffen. Genau dieser Fall ist der wichtige: Ein Paket
// ohne verfügbaren Fix darf nicht durchrutschen, nur weil keine Obergrenze
// dasteht.
func affected(pkgType, installed string, e localEntry) bool {
	if installed == "" {
		return false
	}
	if e.Introduced != "" && e.Introduced != "0" {
		if CompareVersions(pkgType, installed, e.Introduced) < 0 {
			return false
		}
	}
	if e.Fixed == "" {
		return true
	}
	return CompareVersions(pkgType, installed, e.Fixed) < 0
}

// load liefert den Index und liest ihn bei Bedarf von der Platte.
func (l *LocalOSV) load() (map[string][]localEntry, error) {
	path := filepath.Join(l.dir, indexFile)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("keine lokale osv-kopie vorhanden - bitte zuerst spiegeln")
	}

	l.mu.RLock()
	if l.index != nil && !info.ModTime().After(l.loadedAt) {
		defer l.mu.RUnlock()
		return l.index, nil
	}
	l.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var index map[string][]localEntry
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("lokale osv-kopie nicht lesbar: %w", err)
	}

	l.mu.Lock()
	l.index = index
	l.loadedAt = time.Now()
	l.mu.Unlock()
	return index, nil
}

// --- Hilfsfunktionen ----------------------------------------------------------

// purlParts sind die für den Abgleich nötigen Bestandteile eines purl.
type purlParts struct {
	pkgType   string // deb | rpm
	name      string
	version   string
	ecosystem string // OSV-Unterökosystem, z.B. "Debian:12"
}

// parsePurl zerlegt "pkg:deb/debian/openssl@3.0.11-1?distro=debian-12".
func parsePurl(purl string) (purlParts, bool) {
	var p purlParts
	rest, ok := strings.CutPrefix(purl, "pkg:")
	if !ok {
		return p, false
	}
	body, qualifiers, _ := strings.Cut(rest, "?")
	segments := strings.Split(body, "/")
	if len(segments) < 2 {
		return p, false
	}
	p.pkgType = segments[0]
	last := segments[len(segments)-1]
	p.name, p.version, _ = strings.Cut(last, "@")
	if p.name == "" {
		return p, false
	}

	var distro string
	for _, q := range strings.Split(qualifiers, "&") {
		if key, value, found := strings.Cut(q, "="); found && key == "distro" {
			distro = value
		}
	}
	p.ecosystem = ecosystemFor(distro)
	return p, p.ecosystem != ""
}

// EcosystemForPurl liefert das OSV-Unterökosystem eines purl - der Aufrufer
// braucht das, um zu wissen, welche Dumps überhaupt gespiegelt werden müssen.
// Leer heißt: Für diese Distribution gibt es keinen passenden Bestand.
func EcosystemForPurl(purl string) string {
	p, ok := parsePurl(purl)
	if !ok {
		return ""
	}
	return p.ecosystem
}

// ecosystemFor bildet die Distro-Kennung eines purl ("debian-12") auf das
// OSV-Unterökosystem ab ("Debian:12").
func ecosystemFor(distro string) string {
	name, versionID, ok := strings.Cut(distro, "-")
	if !ok || name == "" || versionID == "" {
		return ""
	}
	switch strings.ToLower(name) {
	case "debian":
		return "Debian:" + versionID
	case "ubuntu":
		return "Ubuntu:" + versionID
	case "rocky":
		return "Rocky Linux:" + versionID
	case "almalinux", "alma":
		return "AlmaLinux:" + versionID
	case "rhel", "redhat":
		return "Red Hat:" + versionID
	case "opensuse.leap":
		return "openSUSE:Leap " + versionID
	case "sles":
		return "SUSE:SLES " + versionID
	default:
		// Unbekannte Distribution: kein Ökosystem, also keine Aussage. Das
		// ist ehrlicher, als sie einem falschen Bestand zuzuordnen.
		return ""
	}
}

// parentEcosystem liefert das Eltern-Ökosystem, unter dem OSV den Dump
// bereitstellt ("Debian:12" → "Debian").
func parentEcosystem(eco string) string {
	if base, _, ok := strings.Cut(eco, ":"); ok {
		return base
	}
	return eco
}

func indexKey(ecosystem, pkg string) string { return ecosystem + "\x00" + pkg }

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
