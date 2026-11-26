package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGitLab ist der Ausschnitt der API, den GitLab hier beantwortet.
type fakeGitLab struct {
	mu sync.Mutex

	mrs    []MergeRequest
	issues []Issue
	// autoMergeRefusals: So oft lehnt PUT .../merge ab (405), bevor es klappt.
	autoMergeRefusals int
	autoMergeCalls    map[int]int
	autoMerged        map[int]bool
}

func newFakeGitLab(t *testing.T) (*fakeGitLab, *GitLab) {
	f := &fakeGitLab{autoMergeCalls: map[int]int{}, autoMerged: map[int]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, &GitLab{BaseURL: srv.URL + "/api/v4", Project: "1", Token: "geheim"}
}

func (f *fakeGitLab) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if r.Header.Get("PRIVATE-TOKEN") != "geheim" {
		reply(http.StatusUnauthorized, map[string]string{"message": "401"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v4/projects/1")
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)

	switch {
	case r.Method == http.MethodGet && path == "/merge_requests":
		var out []MergeRequest
		for _, mr := range f.mrs {
			if mr.TargetBranch == r.URL.Query().Get("target_branch") {
				out = append(out, mr)
			}
		}
		reply(http.StatusOK, out)
	case r.Method == http.MethodPost && path == "/merge_requests":
		mr := MergeRequest{
			IID: len(f.mrs) + 1, Title: body["title"].(string),
			SourceBranch: body["source_branch"].(string), TargetBranch: body["target_branch"].(string),
		}
		mr.WebURL = fmt.Sprintf("http://gitlab.test/mr/%d", mr.IID)
		f.mrs = append(f.mrs, mr)
		reply(http.StatusCreated, mr)
	case r.Method == http.MethodPut && strings.HasSuffix(path, "/merge"):
		var iid int
		_, _ = fmt.Sscanf(path, "/merge_requests/%d/merge", &iid)
		f.autoMergeCalls[iid]++
		if f.autoMergeCalls[iid] <= f.autoMergeRefusals {
			reply(http.StatusMethodNotAllowed, map[string]string{"message": "405 Method Not Allowed"})
			return
		}
		f.autoMerged[iid] = true
		reply(http.StatusOK, map[string]any{"iid": iid})
	case r.Method == http.MethodGet && path == "/issues":
		reply(http.StatusOK, f.issues)
	case r.Method == http.MethodPost && path == "/issues":
		issue := Issue{IID: len(f.issues) + 1, Title: body["title"].(string), WebURL: fmt.Sprintf("http://gitlab.test/issues/%d", len(f.issues)+1)}
		f.issues = append(f.issues, issue)
		reply(http.StatusCreated, issue)
	default:
		reply(http.StatusNotFound, map[string]string{"message": r.Method + " " + path})
	}
}

// world ist ein "Server" (bare-Repository) mit einer Arbeitskopie als
// Pipeline-Checkout und der fake-API dazu.
type world struct {
	t        *testing.T
	bare     string
	seed     string // Arbeitskopie zum Vorbereiten des Servers
	pipeline string // Checkout der Pipeline (Quelle der Worktrees)
	gitlab   *fakeGitLab
	app      *app
	log      *strings.Builder
	slept    int
	prepared []string // Versionen, für die prepare aufgerufen wurde
}

func newWorld(t *testing.T) *world {
	requireTools(t)
	root := t.TempDir()
	w := &world{t: t, bare: filepath.Join(root, "origin.git"), seed: filepath.Join(root, "seed"), pipeline: filepath.Join(root, "pipeline"), log: &strings.Builder{}}
	mustGit(t, root, "init", "--quiet", "--bare", "-b", "develop", w.bare)
	mustGit(t, w.bare, "config", "receive.advertisePushOptions", "true")
	mustGit(t, root, "clone", "--quiet", w.bare, w.seed)
	mustGit(t, w.seed, "switch", "--quiet", "-c", "develop")
	mustGit(t, w.seed, "commit", "--quiet", "--allow-empty", "-m", "chore: Anfang")
	mustGit(t, w.seed, "push", "--quiet", "origin", "develop")

	gitlab, api := newFakeGitLab(t)
	w.gitlab = gitlab
	w.app = &app{
		git:     testGit(),
		api:     api,
		repoDir: w.pipeline,
		pushURL: w.bare,
		now:     func() time.Time { return time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC) }, // Dienstag
		sleep:   func(time.Duration) { w.slept++ },
		prepare: func(dir, version string) error {
			w.prepared = append(w.prepared, version)
			writeFiles(t, dir, map[string]string{
				"VERSION":      version + "\n",
				"CHANGELOG.md": "# Changelog\n\n## v" + version + " - 2026-12-01\n\n### 🐛 Bugfixes\n\n- Abhängigkeiten aktualisiert\n",
			})
			mustGit(t, dir, "add", "VERSION", "CHANGELOG.md")
			mustGit(t, dir, "commit", "--quiet", "-m", "release: v"+version+" - Version & Changelog vorbereitet")
			return nil
		},
		relevant: func(string) (bool, error) { return true, nil },
		out:      w.log,
	}
	return w
}

// commit schreibt Dateien auf einen Branch der Seed-Kopie und schiebt ihn.
func (w *world) commit(branch, message string, files map[string]string) {
	w.t.Helper()
	mustGit(w.t, w.seed, "switch", "--quiet", branch)
	commitFiles(w.t, w.seed, message, files)
	mustGit(w.t, w.seed, "push", "--quiet", "origin", branch)
}

func (w *world) branchFrom(branch, from string) {
	w.t.Helper()
	mustGit(w.t, w.seed, "switch", "--quiet", "-c", branch, from)
	mustGit(w.t, w.seed, "push", "--quiet", "origin", branch)
}

func (w *world) tag(name, branch string) {
	w.t.Helper()
	mustGit(w.t, w.seed, "tag", name, branch)
	mustGit(w.t, w.seed, "push", "--quiet", "origin", name)
}

// start klont den Server als Pipeline-Checkout; danach beginnen die Läufe.
func (w *world) start() {
	w.t.Helper()
	mustGit(w.t, filepath.Dir(w.pipeline), "clone", "--quiet", w.bare, w.pipeline)
}

func (w *world) remoteBranches() string {
	w.t.Helper()
	return mustGit(w.t, w.bare, "branch", "--list")
}

func (w *world) remoteFile(ref, name string) string {
	w.t.Helper()
	return mustGit(w.t, w.bare, "show", ref+":"+name)
}

func (w *world) logged() string { return w.log.String() }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
