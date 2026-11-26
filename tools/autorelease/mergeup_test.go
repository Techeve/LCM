package main

import (
	"strings"
	"testing"
)

const startChangelog = "# Changelog\n\n## v2.3.2 - 2026-11-25\n\n### 🐛 Bugfixes\n\n- alt (aaaa1111)\n"

// mergeWorld: develop und beta stehen auf 2.4.0-beta.2, community hat ein
// Patch-Release 2.3.3 mit eigenem Release-Commit - VERSION und CHANGELOG.md
// kollidieren also.
func mergeWorld(t *testing.T) *world {
	w := newWorld(t)
	w.commit("develop", "chore: Start", map[string]string{"VERSION": "2.3.2\n", "CHANGELOG.md": startChangelog, "datei.txt": "a\n"})
	w.branchFrom("community", "develop")
	w.branchFrom("beta", "develop")
	w.commit("develop", "release: v2.4.0-beta.2", map[string]string{
		"VERSION":      "2.4.0-beta.2\n",
		"CHANGELOG.md": "# Changelog\n\n## v2.4.0-beta.2 - 2026-11-28\n\n### 🚀 Features\n\n- Neue Suche (bbbb2222)\n\n" + strings.TrimPrefix(startChangelog, "# Changelog\n\n"),
	})
	mustGit(t, w.seed, "push", "--quiet", "origin", "develop:beta")
	w.commit("community", "fix(deps): update module x to v1.2.3", map[string]string{"go.sum": "x v1\n"})
	w.commit("community", "release: v2.3.3", map[string]string{
		"VERSION":      "2.3.3\n",
		"CHANGELOG.md": "# Changelog\n\n## v2.3.3 - 2026-12-01\n\n### 🐛 Bugfixes\n\n- Abhängigkeiten aktualisiert\n\n" + strings.TrimPrefix(startChangelog, "# Changelog\n\n"),
	})
	w.start()
	return w
}

// mergeMR spielt nach, was GitLab beim Merge des MR tut: Branch in das Ziel.
func (w *world) mergeMR(source, target string) {
	w.t.Helper()
	mustGit(w.t, w.seed, "fetch", "--quiet", "origin")
	mustGit(w.t, w.seed, "push", "--quiet", "origin", "origin/"+source+":refs/heads/"+target)
	mustGit(w.t, w.pipeline, "fetch", "--quiet", "origin")
}

func TestMergeUpKetteCommunityBetaDevelop(t *testing.T) {
	w := mergeWorld(t)

	// Lauf 1: community -> beta. VERSION und CHANGELOG kollidieren, die
	// Regeln lösen sie; der Folgeschritt wartet auf den Merge dieses MR.
	if err := w.app.mergeUp(); err != nil {
		t.Fatalf("Lauf 1: %v\n%s", err, w.logged())
	}
	if len(w.gitlab.mrs) != 1 {
		t.Fatalf("nach Lauf 1: MRs %+v\n%s", w.gitlab.mrs, w.logged())
	}
	first := w.gitlab.mrs[0]
	if first.SourceBranch != "merge/community-2.3.3-nach-beta" || first.TargetBranch != "beta" || !w.gitlab.autoMerged[first.IID] {
		t.Errorf("MR 1: %+v, Automerge %v", first, w.gitlab.autoMerged[first.IID])
	}
	if got := w.remoteFile(first.SourceBranch, "VERSION"); got != "2.4.0-beta.2" {
		t.Errorf("VERSION auf dem Merge-Branch = %q, erwartet die höhere 2.4.0-beta.2", got)
	}
	changelog := w.remoteFile(first.SourceBranch, "CHANGELOG.md")
	if !strings.Contains(changelog, "v2.3.3") || !strings.Contains(changelog, "v2.4.0-beta.2") || strings.Count(changelog, "v2.3.2") != 1 {
		t.Errorf("CHANGELOG.md auf dem Merge-Branch:\n%s", changelog)
	}

	// Lauf 2 vor dem Merge: derselbe MR ist offen, es kommt kein zweiter.
	if err := w.app.mergeUp(); err != nil {
		t.Fatal(err)
	}
	if len(w.gitlab.mrs) != 1 {
		t.Errorf("zweiter MR trotz offenem: %+v", w.gitlab.mrs)
	}

	// Lauf 3 nach dem Merge: beta -> develop über einen Merge-Branch.
	w.gitlab.mrs = nil
	w.mergeMR(first.SourceBranch, "beta")
	if err := w.app.mergeUp(); err != nil {
		t.Fatalf("Lauf 3: %v\n%s", err, w.logged())
	}
	if len(w.gitlab.mrs) != 1 || w.gitlab.mrs[0].SourceBranch != "merge/beta-2.4.0-beta.2-nach-develop" || w.gitlab.mrs[0].TargetBranch != "develop" {
		t.Fatalf("nach Lauf 3: MRs %+v\n%s", w.gitlab.mrs, w.logged())
	}
	if w.remoteFile("merge/beta-2.4.0-beta.2-nach-develop", "go.sum") != "x v1" {
		t.Error("der Fix aus community fehlt auf dem Branch nach develop")
	}

	// Lauf 4 nach dem Merge: nichts mehr zu tun.
	w.gitlab.mrs = nil
	w.mergeMR("merge/beta-2.4.0-beta.2-nach-develop", "develop")
	if err := w.app.mergeUp(); err != nil {
		t.Fatal(err)
	}
	if len(w.gitlab.mrs) != 0 {
		t.Errorf("MR obwohl alles enthalten ist: %+v", w.gitlab.mrs)
	}
}

func TestMergeUpOhneKonfliktGehtDerMRDirektAusCommunity(t *testing.T) {
	w := newWorld(t)
	w.commit("develop", "chore: Start", map[string]string{"VERSION": "2.3.2\n", "datei.txt": "a\n"})
	w.branchFrom("community", "develop")
	w.branchFrom("beta", "develop")
	w.commit("community", "fix: etwas", map[string]string{"neu.txt": "b\n"})
	w.start()

	if err := w.app.mergeUp(); err != nil {
		t.Fatal(err)
	}
	if len(w.gitlab.mrs) != 1 || w.gitlab.mrs[0].SourceBranch != "community" || w.gitlab.mrs[0].TargetBranch != "beta" {
		t.Errorf("MRs: %+v", w.gitlab.mrs)
	}
}

func TestMergeUpMeldetUngeloesteKonflikteAlsIssue(t *testing.T) {
	w := newWorld(t)
	w.commit("develop", "chore: Start", map[string]string{"VERSION": "2.3.2\n", "ui/package.json": "{\"v\": 1}\n"})
	w.branchFrom("community", "develop")
	w.branchFrom("beta", "develop")
	w.commit("beta", "feat: neu", map[string]string{"ui/package.json": "{\"v\": 2}\n"})
	w.commit("community", "fix(deps): update dependency lit to v3.3.4", map[string]string{"ui/package.json": "{\"v\": 3}\n"})
	w.start()

	err := w.app.mergeUp()
	if err == nil || !strings.Contains(err.Error(), "ui/package.json") {
		t.Fatalf("Fehler mit der Datei erwartet, bekam %v", err)
	}
	if len(w.gitlab.mrs) != 0 {
		t.Errorf("MR trotz Konflikt: %+v", w.gitlab.mrs)
	}
	if len(w.gitlab.issues) != 1 || !strings.Contains(w.gitlab.issues[0].Title, "community nach beta") {
		t.Fatalf("Issues: %+v", w.gitlab.issues)
	}
	if strings.Contains(w.remoteBranches(), "merge/") {
		t.Errorf("Merge-Branch trotz Abbruch gepusht:\n%s", w.remoteBranches())
	}

	// Am nächsten Tag: derselbe Fall, kein zweites Issue.
	if err := w.app.mergeUp(); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if len(w.gitlab.issues) != 1 {
		t.Errorf("doppeltes Issue: %+v", w.gitlab.issues)
	}
}

func TestMergeUpUeberspringtMergesOhneInhalt(t *testing.T) {
	w := newWorld(t)
	w.commit("develop", "chore: Start", map[string]string{"VERSION": "2.3.2\n"})
	w.branchFrom("beta", "develop")
	w.branchFrom("community", "develop")
	w.commit("beta", "feat: neu", map[string]string{"neu.txt": "b\n"})
	// community nimmt beta auf (Release-Zug): Der Merge-Commit steht nur dort.
	mustGit(t, w.seed, "switch", "--quiet", "community")
	mustGit(t, w.seed, "merge", "--quiet", "--no-ff", "-m", "Merge branch 'beta' into 'community'", "beta")
	mustGit(t, w.seed, "push", "--quiet", "origin", "community")
	w.start()

	if err := w.app.mergeUp(); err != nil {
		t.Fatalf("%v\n%s", err, w.logged())
	}
	// community -> beta: Inhalt gleich, kein MR. beta -> develop: develop fehlt
	// der feat-Commit, dort entsteht der MR.
	if len(w.gitlab.mrs) != 1 || w.gitlab.mrs[0].TargetBranch != "develop" {
		t.Errorf("MRs: %+v\n%s", w.gitlab.mrs, w.logged())
	}
}

func TestMergeUpBranchNameKollidiert(t *testing.T) {
	w := mergeWorld(t)
	w.app.api.Project = "1"
	// Der Name ist von einem früheren, längst gemergten MR belegt.
	mustGit(t, w.seed, "push", "--quiet", "origin", "origin/community:refs/heads/merge/community-2.3.3-nach-beta")
	if err := w.app.mergeUp(); err != nil {
		t.Fatalf("%v\n%s", err, w.logged())
	}
	if len(w.gitlab.mrs) != 1 || !strings.HasPrefix(w.gitlab.mrs[0].SourceBranch, "merge/community-2.3.3-nach-beta-") {
		t.Errorf("MRs: %+v", w.gitlab.mrs)
	}
}
