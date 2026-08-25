package advisories

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"LCM/internal/version"
)

// ExploitSource liefert die Kennungen der Schwachstellen, für die eine aktive
// Ausnutzung belegt ist.
//
// Bewusst NICHT Teil von Source: Diese Quelle beantwortet keine Frage nach
// Paketen, sondern liefert ein Dringlichkeitssignal zu bereits bekannten
// Befunden. Sie in dieselbe Schnittstelle zu zwängen hieße, eine Methode
// mitzuschleppen, die sie nie sinnvoll beantworten kann.
type ExploitSource interface {
	Available() bool
	// ExploitedCVEs liefert die Menge der CVE-Kennungen mit belegter
	// Ausnutzung.
	ExploitedCVEs(ctx context.Context) (map[string]bool, error)
}

// EUVD ist die EU-Schwachstellendatenbank der ENISA.
//
// Genutzt wird ausschließlich ihr KEV-Dump: eine vollständige Liste der
// nachweislich ausgenutzten Schwachstellen (rund 1700 Einträge, gut 240 KB).
// Der naheliegendere Endpunkt /api/exploitedvulnerabilities liefert nur eine
// Handvoll der jüngsten Einträge - als Grundlage für eine Bewertung wäre er
// irreführend, weil ein fehlender Treffer dort nichts bedeutet.
//
// Für den Betreiber ist das ein reiner Download: Über den eigenen Bestand
// verlässt dabei nichts das Haus. Deshalb braucht diese Quelle auch keine
// eigene Zustimmung - anders als die purl-Abfrage bei OSV.
type EUVD struct {
	baseURL string
	http    *http.Client
}

const euvdBaseURL = "https://euvdservices.enisa.europa.eu"

// euvdTimeout ist großzügiger als bei OSV: Der Dump ist einige hundert
// Kilobyte groß und die API gilt als leistungsschwach. Der Lauf ist täglich,
// ein langsamer Abruf stört niemanden.
const euvdTimeout = 2 * time.Minute

func NewEUVD(baseURL string) *EUVD {
	if baseURL == "" {
		baseURL = euvdBaseURL
	}
	return &EUVD{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: euvdTimeout},
	}
}

func (e *EUVD) Available() bool { return e != nil && e.baseURL != "" }

// euvdKevEntry ist eine Zeile des KEV-Dumps.
type euvdKevEntry struct {
	CVEID  string `json:"cveId"`
	EUVDID string `json:"euvdId"`
}

// ExploitedCVEs lädt den KEV-Dump und liefert die enthaltenen CVE-Kennungen.
func (e *EUVD) ExploitedCVEs(ctx context.Context) (map[string]bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+"/api/kev/dump", nil)
	if err != nil {
		return nil, err
	}
	// Eigener User-Agent ist hier zwingend, nicht nur höflich: Vor der API
	// steht ein Gateway, das gängige Standard-Agents (auch den von Go) mit
	// HTTP 403 abweist.
	req.Header.Set("User-Agent", "LCM/"+version.Version)
	req.Header.Set("Accept", "application/json")

	res, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("euvd-abfrage fehlgeschlagen: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("euvd-abfrage fehlgeschlagen: http %d", res.StatusCode)
	}

	var entries []euvdKevEntry
	if err := json.NewDecoder(res.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("euvd-antwort nicht lesbar: %w", err)
	}
	// Ein leerer Dump wäre kein gültiges Ergebnis, sondern ein Ausfall der
	// Gegenstelle: Würde er durchgereicht, setzte die Anreicherung reihenweise
	// „wird ausgenutzt"-Markierungen zurück und die Lage sähe schlagartig
	// harmloser aus, als sie ist.
	if len(entries) == 0 {
		return nil, fmt.Errorf("euvd lieferte einen leeren datenbestand")
	}
	out := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if id := strings.ToUpper(strings.TrimSpace(entry.CVEID)); id != "" {
			out[id] = true
		}
	}
	return out, nil
}
