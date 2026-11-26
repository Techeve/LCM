package main

import (
	"strings"
	"testing"
	"time"
)

// patchWorld: community auf 2.3.2 mit einem Renovate-Commit seit dem Tag,
// enterprise auf 2.2.3.
func patchWorld(t *testing.T) *world {
	w := newWorld(t)
	w.commit("develop", "chore: Start", map[string]string{"VERSION": "2.3.2\n", "CHANGELOG.md": "# Changelog\n\n## v2.3.2 - 2026-11-25\n\n- alt\n"})
	w.branchFrom("community", "develop")
	w.tag("v2.3.2", "community")
	w.commit("community", "fix(deps): update module github.com/a/b to v1.2.3", map[string]string{"go.sum": "x\n"})
	w.branchFrom("enterprise", "develop")
	w.commit("enterprise", "chore: Wartungsstand", map[string]string{"VERSION": "2.2.3\n"})
	w.tag("v2.2.3", "enterprise")
	w.start()
	return w
}

func TestPatchReleaseLegtBranchUndMRMitAutomergeAn(t *testing.T) {
	w := patchWorld(t)
	w.gitlab.autoMergeRefusals = 2 // die Pipeline des MR entsteht erst einen Moment später

	if err := w.app.patchRelease("community"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.remoteBranches(), "hotfix/auto-2.3.3") {
		t.Fatalf("Branch fehlt auf dem Server:\n%s", w.remoteBranches())
	}
	if got := w.remoteFile("hotfix/auto-2.3.3", "VERSION"); got != "2.3.3" {
		t.Errorf("VERSION auf dem Branch = %q", got)
	}
	if len(w.gitlab.mrs) != 1 || w.gitlab.mrs[0].TargetBranch != "community" || w.gitlab.mrs[0].SourceBranch != "hotfix/auto-2.3.3" {
		t.Fatalf("MRs: %+v", w.gitlab.mrs)
	}
	if !w.gitlab.autoMerged[1] || w.gitlab.autoMergeCalls[1] != 3 || w.slept != 2 {
		t.Errorf("Automerge: gesetzt=%v, Aufrufe=%d, gewartet=%d (erwartet true/3/2)", w.gitlab.autoMerged[1], w.gitlab.autoMergeCalls[1], w.slept)
	}
}

func TestPatchReleaseEnterpriseZaehltUnabhaengig(t *testing.T) {
	w := patchWorld(t)
	if err := w.app.patchRelease("enterprise"); err != nil {
		t.Fatal(err)
	}
	if len(w.prepared) != 1 || w.prepared[0] != "2.2.4" {
		t.Errorf("prepare für %v, erwartet [2.2.4]", w.prepared)
	}
	if w.gitlab.mrs[0].TargetBranch != "enterprise" {
		t.Errorf("MR-Ziel %q", w.gitlab.mrs[0].TargetBranch)
	}
}

func TestPatchReleaseNurDienstags(t *testing.T) {
	w := patchWorld(t)
	w.app.now = func() time.Time { return time.Date(2026, 11, 30, 5, 0, 0, 0, time.UTC) } // Montag
	if err := w.app.patchRelease("community"); err != nil {
		t.Fatal(err)
	}
	if len(w.gitlab.mrs) != 0 || len(w.prepared) != 0 {
		t.Errorf("Montag darf nichts entstehen: MRs=%v prepare=%v", w.gitlab.mrs, w.prepared)
	}
	if !strings.Contains(w.logged(), "nicht Dienstag") {
		t.Errorf("Log nennt den Grund nicht:\n%s", w.logged())
	}
}

func TestPatchReleaseSicherheitDarfSofort(t *testing.T) {
	w := patchWorld(t)
	w.commit("community", "fix(deps): update module golang.org/x/net to v0.99.0 [security]", map[string]string{"go.sum": "y\n"})
	w.app.now = func() time.Time { return time.Date(2026, 12, 2, 5, 0, 0, 0, time.UTC) } // Mittwoch
	mustGit(t, w.pipeline, "fetch", "--quiet", "origin")
	if err := w.app.patchRelease("community"); err != nil {
		t.Fatal(err)
	}
	if len(w.gitlab.mrs) != 1 {
		t.Fatalf("erwartet ein Release am Mittwoch, MRs: %+v", w.gitlab.mrs)
	}
}

func TestPatchReleaseOhneAnlassKeinMR(t *testing.T) {
	w := patchWorld(t)
	w.app.relevant = func(string) (bool, error) { return false, nil }
	if err := w.app.patchRelease("community"); err != nil {
		t.Fatal(err)
	}
	if len(w.gitlab.mrs) != 0 {
		t.Errorf("MRs ohne Anlass: %+v", w.gitlab.mrs)
	}
}

func TestPatchReleaseLegtKeinZweitesAnWennEinMROffenIst(t *testing.T) {
	w := patchWorld(t)
	w.gitlab.mrs = []MergeRequest{{IID: 7, SourceBranch: "hotfix/auto-2.3.3", TargetBranch: "community", WebURL: "http://gitlab.test/mr/7"}}
	if err := w.app.patchRelease("community"); err != nil {
		t.Fatal(err)
	}
	if len(w.gitlab.mrs) != 1 || len(w.prepared) != 0 {
		t.Errorf("zweiter MR entstanden: %+v, prepare=%v", w.gitlab.mrs, w.prepared)
	}
	if !w.gitlab.autoMerged[7] {
		t.Error("Automerge des offenen MR wurde nicht nachgeholt")
	}
}

func TestPatchReleaseWartetAufDasTagDerLinie(t *testing.T) {
	w := patchWorld(t)
	mustGit(t, w.seed, "push", "--quiet", "origin", ":refs/tags/v2.3.2")
	mustGit(t, w.pipeline, "tag", "-d", "v2.3.2")
	if err := w.app.patchRelease("community"); err != nil {
		t.Fatal(err)
	}
	if len(w.gitlab.mrs) != 0 || !strings.Contains(w.logged(), "Tag v2.3.2 fehlt") {
		t.Errorf("ohne Tag darf nichts entstehen:\n%s", w.logged())
	}
}

func TestPatchReleaseBrichtAbWennAutomergeNichtGeht(t *testing.T) {
	w := patchWorld(t)
	w.gitlab.autoMergeRefusals = 1000
	if err := w.app.patchRelease("community"); err == nil {
		t.Fatal("Fehler erwartet, wenn Automerge nie einschaltbar ist")
	}
	if w.gitlab.autoMergeCalls[1] != autoMergeAttempts {
		t.Errorf("Versuche = %d, erwartet %d", w.gitlab.autoMergeCalls[1], autoMergeAttempts)
	}
}
