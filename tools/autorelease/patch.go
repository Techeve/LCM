package main

import (
	"fmt"
	"strings"
)

// patchBranchPrefix: Alle MRs dieser Automatik kommen aus hotfix/auto-<version>.
// hotfix/* ist der Weg, den check:mr-source nach community UND enterprise
// zulässt und den prepare-release.sh als Release-Branch kennt.
const patchBranchPrefix = "hotfix/auto-"

// patchRelease prüft, ob die Linie ein Patch-Release braucht, und legt es als
// MR mit Automerge an: Release-Commit (VERSION + CHANGELOG) auf einem Branch
// von der Linie, nach grüner Pipeline gemergt. Der Merge taggt und rollt aus
// wie jeder andere Release (Jobs version, release, deploy).
func (a *app) patchRelease(line string) error {
	a.logf("== Patch-Release für %s ==", line)

	open, found, err := a.openMergeRequestFrom(line, patchBranchPrefix)
	if err != nil {
		return err
	}
	if found {
		a.logf("Offener Release-MR %s - nichts Neues anzulegen.", open.WebURL)
		a.healAutoMerge(open)
		return nil
	}

	if _, err := a.git.run(a.repoDir, "fetch", "--quiet", "--tags", "origin", line); err != nil {
		return err
	}
	return a.withWorktree("origin/"+line, func(dir string) error {
		decision, err := a.decide(dir, line)
		if err != nil {
			return err
		}
		if !decision.Release {
			a.logf("Kein Release: %s", decision.Reason)
			return nil
		}
		a.logf("Release v%s: %s", decision.Version, decision.Reason)

		branch := patchBranchPrefix + decision.Version
		if _, err := a.git.run(dir, "switch", "--quiet", "-c", branch); err != nil {
			return err
		}
		if a.remoteBranchExists(branch) {
			// Rest eines früheren Laufs, der nach dem Push abbrach.
			a.logf("Branch %s liegt schon auf dem Server - er wird verwendet.", branch)
		} else {
			if err := a.prepare(dir, decision.Version); err != nil {
				return fmt.Errorf("prepare-release.sh %s: %w", decision.Version, err)
			}
			if err := a.push(dir, branch); err != nil {
				return err
			}
		}

		mr, err := a.api.CreateMergeRequest(branch, line,
			fmt.Sprintf("release: v%s - Patch-Release %s (automatisch)", decision.Version, line),
			patchDescription(line, decision))
		if err != nil {
			return err
		}
		a.logf("MR angelegt: %s", mr.WebURL)
		return a.enableAutoMerge(mr)
	})
}

// decide liest die Tatsachen der Linie aus dem Worktree und entscheidet.
func (a *app) decide(dir, line string) (Decision, error) {
	version, err := readVersion(dir)
	if err != nil {
		return Decision{}, err
	}
	lastTag := "v" + version
	if !a.git.succeeds(dir, "rev-parse", "-q", "--verify", "refs/tags/"+lastTag) {
		// VERSION steht auf der Linie für die zuletzt veröffentlichte Version.
		// Fehlt ihr Tag, läuft deren Release noch oder ist gescheitert - dann
		// bliebe jede Rechnung ab "letztem Tag" falsch.
		return Decision{Reason: fmt.Sprintf("Tag %s fehlt (Release läuft noch oder ist gescheitert)", lastTag)}, nil
	}
	relevant, err := a.relevant(dir)
	if err != nil {
		return Decision{}, err
	}
	subjects, err := a.git.lines(dir, "log", "--no-merges", "--format=%s", lastTag+"..HEAD")
	if err != nil {
		return Decision{}, err
	}
	tags, err := a.git.lines(a.repoDir, "tag", "--list", "v*")
	if err != nil {
		return Decision{}, err
	}
	return Decide(Facts{
		Line:     line,
		Now:      a.now(),
		LastTag:  lastTag,
		Version:  version,
		Relevant: relevant,
		Security: anySecurity(subjects),
		Tags:     tags,
	})
}

func anySecurity(subjects []string) bool {
	for _, s := range subjects {
		if strings.Contains(strings.ToLower(s), "[security]") {
			return true
		}
	}
	return false
}

func patchDescription(line string, d Decision) string {
	return fmt.Sprintf(`Automatisch angelegt von der Release-Automatik (tools/autorelease, Job release:patch).

- Linie: %s
- Version: v%s
- Anlass: %s

Der Merge taggt v%s und rollt aus wie jedes andere Release. Der MR mergt von selbst, sobald die Pipeline grün ist. Anhalten: Automerge am MR abbrechen (die Automatik legt keinen zweiten an, solange er offen ist) oder die Automatik pausieren (CI-Variable AUTO_RELEASE_PAUSE=true).`,
		line, d.Version, d.Reason, d.Version)
}
