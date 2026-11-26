package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// labelRelease kennzeichnet alles, was die Automatik anlegt (MRs und Issues).
const labelRelease = "release-automatik"

// GitLab ist der kleine Ausschnitt der REST-API, den die Automatik braucht.
// Aufgerufen wird mit dem Projekt-Token RENOVATE_TOKEN: Pushes und MRs mit dem
// CI_JOB_TOKEN lösen keine Pipelines aus, der MR bliebe ohne Prüfung.
type GitLab struct {
	BaseURL string // z.B. https://gitlab.techeve.de/api/v4
	Project string // ID oder Pfad (wird URL-kodiert)
	Token   string
	HTTP    *http.Client
}

// MergeRequest ist der Teil eines MR, der interessiert.
type MergeRequest struct {
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	WebURL       string `json:"web_url"`
}

// Issue ist der Teil eines Issues, der interessiert.
type Issue struct {
	IID    int    `json:"iid"`
	Title  string `json:"title"`
	WebURL string `json:"web_url"`
}

// apiError trägt den HTTP-Status, damit Aufrufer auf "noch nicht mergebar"
// (405/406/422) anders reagieren können als auf echte Fehler.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("GitLab-API: HTTP %d: %s", e.Status, strings.TrimSpace(e.Body))
}

func (g *GitLab) call(method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, g.BaseURL+"/projects/"+url.PathEscape(g.Project)+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", g.Token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := g.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &apiError{Status: resp.StatusCode, Body: string(raw)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// OpenMergeRequests listet die offenen MRs in einen Ziel-Branch.
func (g *GitLab) OpenMergeRequests(target string) ([]MergeRequest, error) {
	var all []MergeRequest
	for page := 1; ; page++ {
		var batch []MergeRequest
		q := url.Values{"state": {"opened"}, "target_branch": {target}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if err := g.call(http.MethodGet, "/merge_requests?"+q.Encode(), nil, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			return all, nil
		}
	}
}

// CreateMergeRequest legt den MR an; die Pipeline entsteht dabei mit.
func (g *GitLab) CreateMergeRequest(source, target, title, description string) (MergeRequest, error) {
	var mr MergeRequest
	err := g.call(http.MethodPost, "/merge_requests", map[string]any{
		"source_branch":        source,
		"target_branch":        target,
		"title":                title,
		"description":          description,
		"labels":               labelRelease,
		"remove_source_branch": removableBranch(source),
		"squash":               false,
	}, &mr)
	return mr, err
}

// EnableAutoMerge schaltet "Mergen, wenn die Pipeline erfolgreich ist" ein.
// Beide Parameternamen gehen mit: auto_merge löst merge_when_pipeline_succeeds
// in neueren GitLab-Ständen ab, ältere kennen nur den alten.
func (g *GitLab) EnableAutoMerge(mr MergeRequest) error {
	return g.call(http.MethodPut, fmt.Sprintf("/merge_requests/%d/merge", mr.IID), map[string]any{
		"auto_merge":                   true,
		"merge_when_pipeline_succeeds": true,
		"should_remove_source_branch":  removableBranch(mr.SourceBranch),
	}, nil)
}

// removableBranch: Nur Branches, die die Automatik selbst für einen MR anlegt,
// verschwinden nach dem Merge. Der MR community -> beta kommt direkt aus
// community, und das darf nie als "Quell-Branch löschen" enden.
func removableBranch(name string) bool {
	return strings.HasPrefix(name, patchBranchPrefix) || strings.HasPrefix(name, "merge/")
}

// OpenIssues listet die offenen Issues mit dem Automatik-Label.
func (g *GitLab) OpenIssues() ([]Issue, error) {
	var issues []Issue
	q := url.Values{"state": {"opened"}, "labels": {labelRelease}, "per_page": {"100"}}
	err := g.call(http.MethodGet, "/issues?"+q.Encode(), nil, &issues)
	return issues, err
}

// CreateIssue meldet, was die Automatik nicht allein lösen kann.
func (g *GitLab) CreateIssue(title, description string) (Issue, error) {
	var issue Issue
	err := g.call(http.MethodPost, "/issues", map[string]any{
		"title":       title,
		"description": description,
		"labels":      labelRelease,
	}, &issue)
	return issue, err
}
