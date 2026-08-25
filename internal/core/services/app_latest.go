package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"LCM/internal/core/domain"
)

// Abgleich mit der neuesten Version.
//
// Die Abfrage hängt an der ANWENDUNG, nicht am Server: Bei 40 Servern mit
// AdGuard Home wäre alles andere 40-mal dieselbe Anfrage an dieselbe API -
// und GitHub lässt unangemeldet 60 Anfragen pro Stunde und IP zu. Also
// einmal fragen, alle Funde dagegen vergleichen. Dieselbe Aufteilung wie beim
// Docker-Check, der die Registry auch zentral befragt.

// appLatestTimeout begrenzt eine einzelne Abfrage.
const appLatestTimeout = 20 * time.Second

// ErrAppLatestSource: die Quelle hat keine brauchbare Antwort geliefert.
var ErrAppLatestSource = errors.New("quelle lieferte keine version")

// LatestChecker fragt die neueste Version einer Anwendung ab.
type LatestChecker struct {
	http *http.Client
}

func NewLatestChecker() *LatestChecker {
	return &LatestChecker{http: &http.Client{Timeout: appLatestTimeout}}
}

// Check liefert die neueste Version zu einer Quellenangabe.
func (c *LatestChecker) Check(ctx context.Context, source, pattern string) (string, error) {
	kind, value, _ := strings.Cut(strings.TrimSpace(source), ":")
	switch kind {
	case "github":
		return c.checkGitHub(ctx, value)
	case "url":
		return c.checkURL(ctx, value, pattern)
	default:
		return "", fmt.Errorf("%w: unbekannte art %q", domain.ErrAppSource, kind)
	}
}

// checkGitHub liest den Tag der jüngsten Freigabe. Die Kurzform gibt es,
// weil sie der Regelfall ist - sonst müsste jeder die API-Adresse von Hand
// zusammensetzen und dabei auf /releases/latest kommen.
func (c *LatestChecker) checkGitHub(ctx context.Context, repo string) (string, error) {
	body, err := c.get(ctx, "https://api.github.com/repos/"+repo+"/releases/latest",
		map[string]string{"Accept": "application/vnd.github+json"})
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("antwort von github: %w", err)
	}
	if tag := strings.TrimSpace(release.TagName); tag != "" {
		return tag, nil
	}
	if name := strings.TrimSpace(release.Name); name != "" {
		return name, nil
	}
	return "", ErrAppLatestSource
}

// checkURL holt eine beliebige Seite und schneidet die Version mit dem
// hinterlegten Muster heraus.
func (c *LatestChecker) checkURL(ctx context.Context, url, pattern string) (string, error) {
	body, err := c.get(ctx, url, nil)
	if err != nil {
		return "", err
	}
	if pattern == "" {
		return domain.ExtractVersion(string(body), ""), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("%w: %v", domain.ErrAppPattern, err)
	}
	m := re.FindSubmatch(body)
	switch {
	case m == nil:
		return "", ErrAppLatestSource
	case len(m) > 1:
		return strings.TrimSpace(string(m[1])), nil
	default:
		return strings.TrimSpace(string(m[0])), nil
	}
}

// appLatestMaxBody deckelt die gelesene Antwort. Eine Versionsangabe braucht
// Kilobytes, keine Megabytes - und eine Quelle, die etwas anderes liefert,
// soll den LCM-Host nicht volllaufen lassen.
const appLatestMaxBody = 1 << 20

func (c *LatestChecker) get(ctx context.Context, url string, header map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LCM")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s antwortete mit %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, appLatestMaxBody))
}

// CheckLatestVersions gleicht alle Katalogeinträge mit einer hinterlegten
// Quelle ab. Fehler bleiben am Eintrag stehen, statt den ganzen Lauf zu
// beenden: Dass GitHub gerade drosselt, ist kein Grund, die übrigen
// Anwendungen ungeprüft zu lassen.
func (s *AppService) CheckLatestVersions(ctx context.Context) (string, error) {
	if s.latest == nil {
		return "", errors.New("kein prüfer verdrahtet")
	}
	entries, err := s.apps.FindEnabled()
	if err != nil {
		return "", err
	}
	checked, failed, changed := 0, 0, 0
	for _, entry := range entries {
		if strings.TrimSpace(entry.LatestSource) == "" {
			continue
		}
		checked++
		jetzt := time.Now()
		version, err := s.latest.Check(ctx, entry.LatestSource, entry.LatestPattern)
		fields := map[string]any{"latest_checked_at": jetzt}
		if err != nil {
			failed++
			fields["latest_error"] = kurz(err.Error())
		} else {
			if version != entry.LatestVersion {
				changed++
			}
			fields["latest_version"], fields["latest_error"] = version, ""
		}
		_ = s.apps.UpdateLatest(entry.ID, fields)
	}
	return fmt.Sprintf("%d Anwendungen abgefragt, %d mit neuer Angabe, %d Fehler", checked, changed, failed), nil
}

// kurz hält die Fehlermeldung auf Anzeigelänge.
func kurz(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// WithApps verdrahtet den Anwendungskatalog für den zentralen Lauf.
func (e *Executor) WithApps(a *AppService) *Executor {
	e.apps = a
	return e
}

// RunAppCheck ist der geplante Lauf: Er fragt für jede Anwendung im Katalog
// die neueste Version bei ihrer Quelle ab. Serverlos protokolliert, weil er
// keinen Server anfasst.
func (e *Executor) RunAppCheck(triggeredBy string) {
	e.runSystemJob(domain.RuleTypeAppCheck, triggeredBy, "Anwendungs-Check", func() (string, error) {
		if e.apps == nil {
			return "", errors.New("anwendungskatalog nicht verdrahtet")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		return e.apps.CheckLatestVersions(ctx)
	})
}
