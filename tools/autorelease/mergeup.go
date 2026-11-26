package main

import (
	"fmt"
	"strings"
)

// Aufwärtsmerge nach einem community-Release: community -> beta -> develop.
// Der Weg gehört zum Branch-Modell (docs/reference/ci-release.md): Ein Fix
// entsteht auf der ältesten Linie und fließt aufwärts, damit er beim nächsten
// Release-Zug nicht fehlt.

// step ist ein Schritt des Aufwärtsmerges.
type step struct {
	src, dst string
	// direct: Läuft der Merge ohne Konflikt, geht ein MR src -> dst. Sonst
	// (und immer bei develop) entsteht ein Branch merge/<src>-<version>-nach-<dst>.
	direct bool
}

type stepResult int

const (
	stepDone    stepResult = iota // nichts (mehr) zu tun
	stepPending                   // MR offen oder eben angelegt, Folgeschritt wartet
)

// mergeUp führt zuerst community nach beta und danach beta nach develop.
// Der zweite Schritt wartet, bis der erste gemergt ist: Der nächste tägliche
// Lauf setzt dort fort.
func (a *app) mergeUp() error {
	if _, err := a.git.run(a.repoDir, "fetch", "--quiet", "--tags", "origin", "develop", "beta", "community"); err != nil {
		return err
	}
	result, err := a.propagate(step{src: "community", dst: "beta", direct: true})
	if err != nil || result == stepPending {
		return err
	}
	_, err = a.propagate(step{src: "beta", dst: "develop"})
	return err
}

func (a *app) propagate(s step) (stepResult, error) {
	srcRef, dstRef := "origin/"+s.src, "origin/"+s.dst
	a.logf("== %s -> %s ==", s.src, s.dst)
	if a.git.isAncestor(a.repoDir, srcRef, dstRef) {
		a.logf("%s ist schon in %s enthalten.", s.src, s.dst)
		return stepDone, nil
	}

	for _, prefix := range []string{s.src, "merge/" + s.src + "-"} {
		mr, found, err := a.openMergeRequestFrom(s.dst, prefix)
		if err != nil {
			return stepDone, err
		}
		if found {
			a.logf("Offener MR %s - nichts Neues anzulegen.", mr.WebURL)
			a.healAutoMerge(mr)
			return stepPending, nil
		}
	}

	result := stepDone
	err := a.withWorktree(dstRef, func(dir string) error {
		var err error
		result, err = a.mergeInto(dir, s)
		return err
	})
	return result, err
}

// mergeInto versucht den Merge in einem Worktree auf dem Ziel-Branch und legt
// den MR an, wenn er Inhalt hat.
func (a *app) mergeInto(dir string, s step) (stepResult, error) {
	srcRef := "origin/" + s.src
	srcVersion, err := a.versionAt(srcRef)
	if err != nil {
		return stepDone, err
	}
	branch, err := a.freeMergeBranch(s, srcVersion)
	if err != nil {
		return stepDone, err
	}
	if _, err := a.git.run(dir, "switch", "--quiet", "-c", branch); err != nil {
		return stepDone, err
	}

	_, mergeErr := a.git.exec(dir, "merge", "--no-ff", "--no-commit", srcRef)
	conflicts, err := a.git.lines(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return stepDone, err
	}
	notes := ""
	switch {
	case mergeErr == nil:
		// sauber
	case len(conflicts) == 0:
		return stepDone, fmt.Errorf("merge %s -> %s: %w", s.src, s.dst, mergeErr)
	default:
		a.logf("Konflikte: %s", strings.Join(conflicts, ", "))
		unresolved, err := resolveConflicts(a.git, dir, a.tidy)
		if err != nil {
			a.abortMerge(dir)
			return stepDone, err
		}
		if len(unresolved) > 0 {
			a.abortMerge(dir)
			return stepDone, a.reportManual(s, unresolved)
		}
		notes = "Aufgelöst nach festen Regeln: " + strings.Join(conflicts, ", ") + "."
	}

	if a.git.succeeds(dir, "diff", "--cached", "--quiet") {
		a.abortMerge(dir)
		a.logf("Der Merge ändert keinen Inhalt von %s - kein MR nötig.", s.dst)
		return stepDone, nil
	}
	if mergeErr == nil && s.direct {
		// Ohne Konflikt braucht es keinen eigenen Branch: Der MR geht direkt
		// aus der Quelle, wie ihn check:mr-source für beta vorsieht.
		a.abortMerge(dir)
		return a.openMergeRequest(s.src, s.dst, srcVersion, "")
	}
	if _, err := a.git.run(dir, "commit", "--no-edit"); err != nil {
		return stepDone, err
	}
	if err := a.push(dir, branch); err != nil {
		return stepDone, err
	}
	return a.openMergeRequest(branch, s.dst, srcVersion, notes)
}

func (a *app) abortMerge(dir string) {
	_, _ = a.git.run(dir, "merge", "--abort")
}

func (a *app) versionAt(ref string) (string, error) {
	out, err := a.git.run(a.repoDir, "show", ref+":VERSION")
	return strings.TrimSpace(out), err
}

// freeMergeBranch wählt den Namen merge/<quelle>-<version>-nach-<ziel>. Gibt
// es ihn schon (eine Version, die mit neuem Inhalt wiederkehrt), hängt der
// Kurz-Hash der Quelle an.
func (a *app) freeMergeBranch(s step, version string) (string, error) {
	name := fmt.Sprintf("merge/%s-%s-nach-%s", s.src, version, s.dst)
	if !a.remoteBranchExists(name) {
		return name, nil
	}
	short, err := a.git.run(a.repoDir, "rev-parse", "--short=8", "origin/"+s.src)
	if err != nil {
		return "", err
	}
	name += "-" + short
	if a.remoteBranchExists(name) {
		return "", fmt.Errorf("branch %s gibt es schon auf dem Server - bitte aufräumen", name)
	}
	return name, nil
}

func (a *app) openMergeRequest(source, target, version, notes string) (stepResult, error) {
	title := fmt.Sprintf("Aufwärtsmerge: %s (v%s) nach %s", sourceLine(source), version, target)
	description := "Automatisch angelegt von der Release-Automatik (tools/autorelease, Job merge:up).\n\n" +
		"Führt den Stand von " + sourceLine(source) + " in " + target + " nach, damit Fixes beim nächsten Release-Zug nicht fehlen."
	if notes != "" {
		description += "\n\n" + notes
	}
	mr, err := a.api.CreateMergeRequest(source, target, title, description)
	if err != nil {
		return stepDone, err
	}
	a.logf("MR angelegt: %s", mr.WebURL)
	if err := a.enableAutoMerge(mr); err != nil {
		return stepPending, err
	}
	return stepPending, nil
}

// sourceLine macht aus merge/beta-2.3.3-nach-develop wieder "beta".
func sourceLine(branch string) string {
	if rest, ok := strings.CutPrefix(branch, "merge/"); ok {
		line, _, _ := strings.Cut(rest, "-")
		return line
	}
	return branch
}

// reportManual meldet einen Konflikt, den die Regeln nicht lösen, als Issue
// (einmal je Fall, nicht täglich neu) und beendet den Lauf mit Fehler.
func (a *app) reportManual(s step, unresolved []string) error {
	title := fmt.Sprintf("Aufwärtsmerge %s nach %s braucht Handarbeit", s.src, s.dst)
	problem := fmt.Errorf("%s: nicht automatisch lösbar: %s", title, strings.Join(unresolved, ", "))

	issues, err := a.api.OpenIssues()
	if err != nil {
		return fmt.Errorf("%w (Issue nicht prüfbar: %v)", problem, err)
	}
	for _, issue := range issues {
		if issue.Title == title {
			a.logf("Dazu gibt es schon ein offenes Issue: %s", issue.WebURL)
			return problem
		}
	}
	description := fmt.Sprintf(`Die Release-Automatik (tools/autorelease, Job merge:up) kann %s nicht nach %s führen.

Nicht automatisch lösbar:

%s

Von Hand:

    git switch %s && git pull
    git switch -c merge/%s-handarbeit-nach-%s
    git merge origin/%s
    # Konflikte lösen, committieren, pushen, MR nach %s

Die Automatik legt ihren MR danach nicht noch einmal an, sobald %s in %s enthalten ist. Dieses Issue schließt sich nicht selbst.`,
		s.src, s.dst, "- `"+strings.Join(unresolved, "`\n- `")+"`",
		s.dst, s.src, s.dst, s.src, s.dst, s.src, s.dst)
	issue, err := a.api.CreateIssue(title, description)
	if err != nil {
		return fmt.Errorf("%w (Issue nicht anlegbar: %v)", problem, err)
	}
	a.logf("Issue angelegt: %s", issue.WebURL)
	return problem
}
