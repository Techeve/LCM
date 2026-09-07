package advisories

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/version"
)

// OSV fragt api.osv.dev ab - den Aggregator der Open Source Vulnerability
// Datenbank. Er deckt Debian, Ubuntu, Alpine und die Sprach-Ökosysteme über
// ein gemeinsames Format ab und enthält als einzige der genutzten Quellen die
// MAL-Einträge über bösartige Pakete (OpenSSF Malicious Packages), die in der
// Trivy-Datenbank gar nicht vorkommen.
//
// Die API ist kostenfrei und ohne Schlüssel nutzbar; es sind derzeit keine
// Ratenlimits dokumentiert. Trotzdem wird nicht paketweise angefragt, sondern
// über querybatch: Ein Durchgang über eine ganze Flotte kommt damit mit
// wenigen Aufrufen aus.
type OSV struct {
	baseURL string
	http    *http.Client
}

// osvBaseURL ist der öffentliche Endpunkt.
const osvBaseURL = "https://api.osv.dev"

// osvBatchSize ist die Zahl der purls je querybatch-Aufruf (API-Grenze 1000).
const osvBatchSize = 1000

// osvTimeout begrenzt einen einzelnen HTTP-Aufruf. Bewusst knapp: Der Poller
// läuft alle 15 Minuten, ein hängender Aufruf soll den Durchgang nicht
// blockieren - der nächste Takt holt es nach.
const osvTimeout = 30 * time.Second

// NewOSV erstellt den Client. baseURL leer = öffentlicher Endpunkt (Tests
// setzen hier ihren eigenen Server ein).
func NewOSV(baseURL string) *OSV {
	if baseURL == "" {
		baseURL = osvBaseURL
	}
	return &OSV{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: osvTimeout},
	}
}

func (o *OSV) Name() string { return domain.AdvisorySourceOSV }

// Local: Nein - die Abfrage geht an osv.dev.
func (o *OSV) Local() bool       { return false }
func (o *OSV) Available() bool   { return o != nil && o.baseURL != "" }
func (o *OSV) userAgent() string { return "LCM/" + version.Version }

// --- Drahtformat --------------------------------------------------------------

type osvBatchRequest struct {
	Queries []osvQuery `json:"queries"`
}

type osvQuery struct {
	Package osvPackage `json:"package"`
}

type osvPackage struct {
	Purl string `json:"purl"`
}

type osvBatchResponse struct {
	// Die Antwort ist positionsgleich zur Anfrage - Ergebnis i gehört zu
	// purl i. Deshalb darf die Reihenfolge nie umsortiert werden.
	Results []struct {
		Vulns []struct {
			ID       string    `json:"id"`
			Modified time.Time `json:"modified"`
		} `json:"vulns"`
	} `json:"results"`
}

// osvVuln ist der Ausschnitt eines OSV-Datensatzes, den LCM auswertet.
type osvVuln struct {
	ID       string    `json:"id"`
	Summary  string    `json:"summary"`
	Modified time.Time `json:"modified"`
	Aliases  []string  `json:"aliases"`
	Affected []struct {
		Package struct {
			Name string `json:"name"`
		} `json:"package"`
		Ranges []struct {
			Events []struct {
				Fixed string `json:"fixed"`
			} `json:"events"`
		} `json:"ranges"`
		// EcosystemSpecific traegt bei Distributionen die Dringlichkeit
		// ihres Sicherheitsteams - bei Debian die einzige Schwere-Angabe,
		// die flaechendeckend vorliegt.
		EcosystemSpecific struct {
			Urgency string `json:"urgency"`
		} `json:"ecosystem_specific"`
	} `json:"affected"`
	// Severity haelt CVSS-Vektoren.
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
}

// --- Abfragen -----------------------------------------------------------------

// Query fragt die purls in Blöcken ab und liefert je purl die Kennungen.
func (o *OSV) Query(ctx context.Context, purls []string) (map[string][]Advisory, error) {
	out := make(map[string][]Advisory, len(purls))
	for start := 0; start < len(purls); start += osvBatchSize {
		end := min(start+osvBatchSize, len(purls))
		chunk := purls[start:end]

		req := osvBatchRequest{Queries: make([]osvQuery, 0, len(chunk))}
		for _, p := range chunk {
			req.Queries = append(req.Queries, osvQuery{Package: osvPackage{Purl: queryPurl(p)}})
		}
		var res osvBatchResponse
		if err := o.post(ctx, "/v1/querybatch", req, &res); err != nil {
			return nil, err
		}
		// Positionsgleichheit prüfen statt annehmen: Käme eine kürzere Liste
		// zurück, würden Befunde stillschweigend dem falschen Paket
		// zugeordnet - ein Fehler, der niemandem auffiele.
		if len(res.Results) != len(chunk) {
			return nil, fmt.Errorf("osv querybatch: %d antworten auf %d anfragen", len(res.Results), len(chunk))
		}
		for i, r := range res.Results {
			if len(r.Vulns) == 0 {
				continue
			}
			list := make([]Advisory, 0, len(r.Vulns))
			for _, v := range r.Vulns {
				list = append(list, Advisory{ID: v.ID, Modified: v.Modified})
			}
			out[chunk[i]] = list
		}
	}
	return out, nil
}

// queryPurl bringt einen purl in die Form, die api.osv.dev tatsächlich
// auswertet.
//
// Der Anlass ist ein Fehlerbild, das ohne Prüfung gegen den echten Dienst
// unentdeckt geblieben wäre: Mit dem Qualifier "?distro=debian-12" - so baut
// ihn der SBOM-Pfad, und so versteht ihn Trivy - antwortet OSV mit NULL
// Treffern. Nicht mit einem Fehler, sondern mit einem sauberen Ergebnis.
// Erwartet wird dort der Codename der Veröffentlichung ("bookworm").
//
// Regel:
//   - Debian: Version auf den Codenamen abbilden.
//   - Unbekannte Debian-Version oder andere Distribution: Qualifier
//     weglassen. OSV wertet ihn dort ohnehin nicht aus; und wo doch, ist
//     eine Meldung zu viel allemal besser als eine zu wenig - ein
//     Frühwarnsystem darf sich irren, aber nicht schweigen.
func queryPurl(purl string) string {
	body, qualifiers, ok := strings.Cut(purl, "?")
	if !ok {
		return purl
	}
	var distro string
	for _, q := range strings.Split(qualifiers, "&") {
		if key, value, found := strings.Cut(q, "="); found && key == "distro" {
			distro = value
		}
	}
	name, versionID, ok := strings.Cut(distro, "-")
	if !ok || !strings.EqualFold(name, "debian") {
		return body
	}
	if codename := debianCodename(versionID); codename != "" {
		return body + "?distro=" + codename
	}
	return body
}

// debianCodename bildet die Hauptversion auf den Codenamen ab. Fehlt einer,
// wird der Qualifier weggelassen statt geraten.
func debianCodename(versionID string) string {
	switch versionID {
	case "7":
		return "wheezy"
	case "8":
		return "jessie"
	case "9":
		return "stretch"
	case "10":
		return "buster"
	case "11":
		return "bullseye"
	case "12":
		return "bookworm"
	case "13":
		return "trixie"
	case "14":
		return "forky"
	default:
		return ""
	}
}

// Details holt die Beschreibungen einzeln nach (/v1/vulns/{id}). Das ist
// vertretbar, weil es nur für Kennungen passiert, die der lokale Cache noch
// nicht kennt - im eingeschwungenen Betrieb sind das null bis eine Handvoll
// pro Durchgang.
//
// Ein Fehler bei einer einzelnen Kennung bricht den Durchgang NICHT ab: Der
// Befund selbst steht bereits fest, nur seine Beschreibung fehlt dann - und
// ein Befund ohne Titel ist immer noch besser als kein Befund.
func (o *OSV) Details(ctx context.Context, ids []string) (map[string]Detail, error) {
	out := make(map[string]Detail, len(ids))
	for _, id := range ids {
		var v osvVuln
		if err := o.get(ctx, "/v1/vulns/"+id, &v); err != nil {
			continue
		}
		out[id] = detailFrom(v)
	}
	return out, nil
}

// detailFrom übersetzt einen OSV-Datensatz in die Beschreibung.
func detailFrom(v osvVuln) Detail {
	d := Detail{
		ID:            v.ID,
		Title:         v.Summary,
		Modified:      v.Modified,
		URL:           "https://osv.dev/vulnerability/" + v.ID,
		FixedVersions: map[string]string{},
		Aliases:       v.Aliases,
	}
	vectors := make([]string, 0, len(v.Severity))
	for _, sev := range v.Severity {
		vectors = append(vectors, sev.Score)
	}
	urgency := ""
	for _, a := range v.Affected {
		if a.EcosystemSpecific.Urgency != "" {
			urgency = a.EcosystemSpecific.Urgency
			break
		}
	}
	d.Severity = severityFrom(v.DatabaseSpecific.Severity, vectors, urgency)
	for _, a := range v.Affected {
		name := a.Package.Name
		if name == "" {
			continue
		}
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				// Die erste behebende Version je Paket genügt: Mehrere
				// Einträge entstehen durch parallele Zweige, und die
				// Entscheidung „aktualisieren oder einfrieren" hängt nicht
				// daran, welcher Zweig zuerst geflickt wurde.
				if e.Fixed != "" && d.FixedVersions[name] == "" {
					d.FixedVersions[name] = e.Fixed
				}
			}
		}
	}
	return d
}

// --- HTTP ---------------------------------------------------------------------

func (o *OSV) post(ctx context.Context, path string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return o.do(req, out)
}

func (o *OSV) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+path, nil)
	if err != nil {
		return err
	}
	return o.do(req, out)
}

// do führt den Aufruf aus. Der eigene User-Agent ist kein Schmuck: Anonyme
// Standard-Agents laufen bei manchen Quellen in Sperren, und wer eine
// auffällige Last verursacht, soll erkennbar sein.
func (o *OSV) do(req *http.Request, out any) error {
	req.Header.Set("User-Agent", o.userAgent())
	req.Header.Set("Accept", "application/json")
	res, err := o.http.Do(req)
	if err != nil {
		return fmt.Errorf("osv-abfrage fehlgeschlagen: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("osv-abfrage fehlgeschlagen: http %d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
