package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// app bündelt, was die beiden Abläufe (patch, merge-up) von außen brauchen.
// Die Funktionsfelder sind die Nahtstellen, die Tests ersetzen.
type app struct {
	git     gitRunner
	api     *GitLab
	repoDir string // Arbeitskopie der Pipeline, Ausgangspunkt der Worktrees
	pushURL string // Ziel der Pushes, mit Token (in Tests ein lokales Repository)

	now      func() time.Time
	sleep    func(time.Duration)
	prepare  func(dir, version string) error // packaging/prepare-release.sh
	relevant func(dir string) (bool, error)  // Release-Anlass laut tools/release
	tidy     func(dir string) error          // go mod tidy

	out io.Writer
}

func (a *app) logf(format string, args ...any) {
	fmt.Fprintf(a.out, format+"\n", args...)
}

// withWorktree legt einen frischen Worktree auf ref an und räumt ihn danach
// weg. So bleibt die Arbeitskopie der Pipeline unberührt.
func (a *app) withWorktree(ref string, fn func(dir string) error) error {
	dir, err := os.MkdirTemp("", "autorelease-")
	if err != nil {
		return err
	}
	if _, err := a.git.run(a.repoDir, "worktree", "add", "--detach", dir, ref); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	defer func() {
		_, _ = a.git.run(a.repoDir, "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(dir)
	}()
	return fn(dir)
}

// push schiebt HEAD des Worktrees als Branch ohne Branch-Pipeline: Die
// Pipeline entsteht mit dem MR. Ohne ci.skip liefe dieselbe Suite vorher
// einmal als Branch-Pipeline ins Leere.
func (a *app) push(dir, branch string) error {
	_, err := a.git.run(dir, "push", "--push-option=ci.skip", a.pushURL, "HEAD:refs/heads/"+branch)
	return err
}

func (a *app) remoteBranchExists(branch string) bool {
	out, err := a.git.run(a.repoDir, "ls-remote", "--heads", a.pushURL, "refs/heads/"+branch)
	return err == nil && out != ""
}

// openMergeRequestFrom sucht einen offenen MR in target, dessen Quell-Branch
// mit prefix beginnt (oder genau so heißt).
func (a *app) openMergeRequestFrom(target, prefix string) (MergeRequest, bool, error) {
	mrs, err := a.api.OpenMergeRequests(target)
	if err != nil {
		return MergeRequest{}, false, err
	}
	for _, mr := range mrs {
		if strings.HasPrefix(mr.SourceBranch, prefix) {
			return mr, true, nil
		}
	}
	return MergeRequest{}, false, nil
}

// autoMergeRetries: Der MR bekommt seine Pipeline erst einen Moment nach dem
// Anlegen; vorher lehnt GitLab "mergen, wenn die Pipeline grün ist" ab.
const (
	autoMergeAttempts = 30
	autoMergeWait     = 10 * time.Second
)

func (a *app) enableAutoMerge(mr MergeRequest) error {
	var err error
	for attempt := 1; attempt <= autoMergeAttempts; attempt++ {
		err = a.api.EnableAutoMerge(mr)
		var apiErr *apiError
		if err == nil || !errors.As(err, &apiErr) || !retryable(apiErr.Status) {
			return err
		}
		a.logf("Automerge für %s noch nicht möglich (Versuch %d/%d): %v", mr.WebURL, attempt, autoMergeAttempts, err)
		a.sleep(autoMergeWait)
	}
	return fmt.Errorf("automerge für %s nach %d Versuchen nicht einschaltbar: %w", mr.WebURL, autoMergeAttempts, err)
}

// retryable: 405/406/422 heißen bei GitLab "jetzt noch nicht mergebar", 409 ein
// Wettlauf mit einer Änderung am MR.
func retryable(status int) bool {
	switch status {
	case 405, 406, 409, 422:
		return true
	}
	return false
}

// healAutoMerge schaltet Automerge an einem schon offenen MR noch einmal ein,
// ohne zu warten: Ist ein früherer Lauf davor gescheitert, bliebe der MR sonst
// ewig offen. Ein Fehler ist hier nur eine Meldung wert.
func (a *app) healAutoMerge(mr MergeRequest) {
	if err := a.api.EnableAutoMerge(mr); err != nil {
		a.logf("Automerge an %s nicht (erneut) einschaltbar: %v", mr.WebURL, err)
	}
}

func readVersion(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}
