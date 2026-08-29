package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type config struct {
	GitHubRepo    string // "Techeve/LCM"
	GitLabAPI     string // "https://gitlab.techeve.de/api/v4"
	GitLabProject string // "techeve/lcm" (wird URL-kodiert)
	Token         string
}

func configFromEnv() (config, error) {
	cfg := config{
		GitHubRepo:    os.Getenv("GITHUB_REPO"),
		GitLabAPI:     os.Getenv("CI_API_V4_URL"),
		GitLabProject: os.Getenv("GITLAB_MIRROR_PROJECT"),
		Token:         os.Getenv("ISSUE_SYNC_TOKEN"),
	}
	for name, v := range map[string]string{
		"GITHUB_REPO": cfg.GitHubRepo, "CI_API_V4_URL": cfg.GitLabAPI,
		"GITLAB_MIRROR_PROJECT": cfg.GitLabProject, "ISSUE_SYNC_TOKEN": cfg.Token,
	} {
		if v == "" {
			return cfg, fmt.Errorf("umgebungsvariable %s fehlt", name)
		}
	}
	return cfg, nil
}

// githubIssue ist der Ausschnitt der GitHub-Antwort, den der Abgleich braucht.
// PullRequest dient nur der Aussonderung: GitHubs Issue-API liefert auch
// Pull Requests, die hier nichts verloren haben.
type githubIssue struct {
	Number      int                    `json:"number"`
	Title       string                 `json:"title"`
	Body        string                 `json:"body"`
	State       string                 `json:"state"` // "open" | "closed"
	HTMLURL     string                 `json:"html_url"`
	User        struct{ Login string } `json:"user"`
	PullRequest *struct{}              `json:"pull_request"`
}

type gitlabIssue struct {
	IID         int    `json:"iid"`
	State       string `json:"state"` // "opened" | "closed"
	Description string `json:"description"`
}

type summary struct{ Seen, Created, Closed, Reopened, Unchanged int }

// markerLine ist die Zuordnungszeile in der GitLab-Beschreibung. Sie steht am
// Anfang, damit sie auch dann erhalten bleibt, wenn jemand die Beschreibung
// unten ergänzt.
func markerLine(htmlURL string) string {
	return "GitHub-Issue: " + htmlURL
}

// markerURL liest die zugeordnete GitHub-URL aus einer GitLab-Beschreibung -
// oder "", wenn das Issue nicht aus dem Sync stammt.
func markerURL(description string) string {
	for _, line := range strings.Split(description, "\n") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), "GitHub-Issue: "); found {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// gitlabDescription baut die Beschreibung des GitLab-Zwillings: Zuordnung und
// Herkunft zuerst, dann der GitHub-Text unverändert (beides ist Markdown).
func gitlabDescription(gh githubIssue) string {
	head := markerLine(gh.HTMLURL) + "\n\n_Von @" + gh.User.Login + " auf GitHub gemeldet - Diskussion bitte dort._\n"
	if strings.TrimSpace(gh.Body) == "" {
		return head
	}
	return head + "\n---\n\n" + gh.Body
}

func sync(cfg config) (summary, error) {
	var sum summary
	ghIssues, err := fetchGitHubIssues(cfg)
	if err != nil {
		return sum, err
	}
	known, err := fetchSyncedGitLabIssues(cfg)
	if err != nil {
		return sum, err
	}

	for _, gh := range ghIssues {
		if gh.PullRequest != nil {
			continue // Pull Requests sind keine Issues
		}
		sum.Seen++
		twin, exists := known[gh.HTMLURL]
		switch {
		case !exists:
			if err := createGitLabIssue(cfg, gh); err != nil {
				return sum, fmt.Errorf("issue #%d anlegen: %w", gh.Number, err)
			}
			sum.Created++
		case gh.State == "closed" && twin.State == "opened":
			if err := setGitLabIssueState(cfg, twin.IID, "close"); err != nil {
				return sum, fmt.Errorf("issue #%d schließen: %w", gh.Number, err)
			}
			sum.Closed++
		case gh.State == "open" && twin.State == "closed":
			if err := setGitLabIssueState(cfg, twin.IID, "reopen"); err != nil {
				return sum, fmt.Errorf("issue #%d wieder öffnen: %w", gh.Number, err)
			}
			sum.Reopened++
		default:
			sum.Unchanged++
		}
	}
	return sum, nil
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// fetchGitHubIssues liest alle Issues (offen und geschlossen) des öffentlichen
// Repos - ohne Token; die Rate von 60 Anfragen/Stunde reicht für den
// Zeitplan-Takt bei weitem.
func fetchGitHubIssues(cfg config) ([]githubIssue, error) {
	var all []githubIssue
	for page := 1; page <= 10; page++ {
		u := fmt.Sprintf("https://api.github.com/repos/%s/issues?state=all&per_page=100&page=%d", cfg.GitHubRepo, page)
		var batch []githubIssue
		if err := getJSON(u, "", &batch); err != nil {
			return nil, fmt.Errorf("github-issues laden: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			return all, nil
		}
	}
	return all, fmt.Errorf("mehr als 1000 GitHub-Issues - Seitengrenze erreicht, Abgleich wäre unvollständig")
}

// fetchSyncedGitLabIssues liefert die bereits synchronisierten Zwillinge,
// adressiert über die GitHub-URL aus der Markierungszeile.
func fetchSyncedGitLabIssues(cfg config) (map[string]gitlabIssue, error) {
	known := map[string]gitlabIssue{}
	project := url.PathEscape(cfg.GitLabProject)
	for page := 1; page <= 50; page++ {
		u := fmt.Sprintf("%s/projects/%s/issues?labels=github&state=all&per_page=100&page=%d", cfg.GitLabAPI, project, page)
		var batch []gitlabIssue
		if err := getJSON(u, cfg.Token, &batch); err != nil {
			return nil, fmt.Errorf("gitlab-issues laden: %w", err)
		}
		for _, issue := range batch {
			if u := markerURL(issue.Description); u != "" {
				known[u] = issue
			}
		}
		if len(batch) < 100 {
			return known, nil
		}
	}
	return known, nil
}

func createGitLabIssue(cfg config, gh githubIssue) error {
	payload := map[string]any{
		"title":       gh.Title,
		"description": gitlabDescription(gh),
		"labels":      "github",
	}
	iid, err := postJSON(cfg, fmt.Sprintf("%s/projects/%s/issues", cfg.GitLabAPI, url.PathEscape(cfg.GitLabProject)), payload)
	if err != nil {
		return err
	}
	// Ein auf GitHub bereits geschlossenes Issue kommt geschlossen an - sonst
	// stünde es in GitLab offen, obwohl es nie offen zu bearbeiten war.
	if gh.State == "closed" {
		return setGitLabIssueState(cfg, iid, "close")
	}
	return nil
}

func setGitLabIssueState(cfg config, iid int, event string) error {
	u := fmt.Sprintf("%s/projects/%s/issues/%d?state_event=%s", cfg.GitLabAPI, url.PathEscape(cfg.GitLabProject), iid, event)
	req, err := http.NewRequest(http.MethodPut, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", cfg.Token)
	return doExpectOK(req)
}

func getJSON(u, token string, into any) error {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s: HTTP %d: %s", u, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// postJSON legt an und liefert die IID des erzeugten Issues.
func postJSON(cfg config, u string, payload map[string]any) (int, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("PRIVATE-TOKEN", cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return 0, fmt.Errorf("%s: HTTP %d: %s", u, resp.StatusCode, body)
	}
	var created struct {
		IID int `json:"iid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return 0, err
	}
	return created.IID, nil
}

func doExpectOK(req *http.Request) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s: HTTP %d: %s", req.URL, resp.StatusCode, body)
	}
	return nil
}
