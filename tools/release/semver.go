package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Bump ist die Art des Versionssprungs.
type Bump string

const (
	BumpNone  Bump = "none"
	BumpPatch Bump = "patch"
	BumpMinor Bump = "minor"
	BumpMajor Bump = "major"
)

// Commit ist ein geparster Conventional Commit.
type Commit struct {
	Hash     string
	Type     string // feat, fix, refactor, ... ("" = kein Conventional Commit)
	Scope    string // optionaler Scope aus feat(scope): ...
	Subject  string // Beschreibung ohne Typ-Präfix
	Breaking bool   // feat!: oder "BREAKING CHANGE" im Body
}

// conventionalRe: type(scope)!: subject
var conventionalRe = regexp.MustCompile(`^([a-z]+)(\(([^)]*)\))?(!)?:\s*(.+)$`)

// ParseCommit zerlegt eine Commit-Message nach Conventional Commits.
// Nicht-konforme Messages ergeben Type "" und laufen unter "Sonstiges".
func ParseCommit(hash, subject, body string) Commit {
	c := Commit{Hash: hash, Subject: strings.TrimSpace(subject)}
	if m := conventionalRe.FindStringSubmatch(c.Subject); m != nil {
		c.Type = m[1]
		c.Scope = m[3]
		c.Breaking = m[4] == "!"
		c.Subject = strings.TrimSpace(m[5])
	}
	if strings.Contains(body, "BREAKING CHANGE") || strings.Contains(body, "BREAKING-CHANGE") {
		c.Breaking = true
	}
	return c
}

// DecideBump leitet aus den Commits die Art des Versionssprungs ab:
// Breaking -> Major, feat -> Minor, fix/perf/refactor -> Patch,
// alles andere (docs, test, ci, chore, ...) löst kein Release aus.
func DecideBump(commits []Commit) Bump {
	bump := BumpNone
	for _, c := range commits {
		switch {
		case c.Breaking && c.Type != "":
			return BumpMajor
		case c.Type == "feat":
			bump = maxBump(bump, BumpMinor)
		case c.Type == "fix" || c.Type == "perf" || c.Type == "refactor":
			bump = maxBump(bump, BumpPatch)
		}
	}
	return bump
}

var bumpRank = map[Bump]int{BumpNone: 0, BumpPatch: 1, BumpMinor: 2, BumpMajor: 3}

func maxBump(a, b Bump) Bump {
	if bumpRank[b] > bumpRank[a] {
		return b
	}
	return a
}

// NextVersion berechnet aus "1.2.3" + Bump die nächste Version.
// Bei BumpNone bleibt die Version unverändert.
func NextVersion(current string, bump Bump) (string, error) {
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(current), "v"), ".", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("keine gültige SemVer: %q", current)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		// Prerelease-Suffix am Patch tolerieren ("3-dev" -> 3).
		p, _, _ = strings.Cut(p, "-")
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", fmt.Errorf("keine gültige SemVer: %q", current)
		}
		nums[i] = n
	}
	switch bump {
	case BumpMajor:
		return fmt.Sprintf("%d.0.0", nums[0]+1), nil
	case BumpMinor:
		return fmt.Sprintf("%d.%d.0", nums[0], nums[1]+1), nil
	case BumpPatch:
		return fmt.Sprintf("%d.%d.%d", nums[0], nums[1], nums[2]+1), nil
	default:
		return fmt.Sprintf("%d.%d.%d", nums[0], nums[1], nums[2]), nil
	}
}

// Changelog-Rubriken in Anzeige-Reihenfolge.
var sections = []struct {
	Title string
	Match func(Commit) bool
}{
	{"💥 Breaking Changes", func(c Commit) bool { return c.Breaking && c.Type != "" }},
	{"🚀 Features", func(c Commit) bool { return !c.Breaking && c.Type == "feat" }},
	{"🐛 Bugfixes", func(c Commit) bool { return !c.Breaking && c.Type == "fix" }},
	{"⚡ Performance", func(c Commit) bool { return !c.Breaking && c.Type == "perf" }},
	{"♻️ Refactoring", func(c Commit) bool { return !c.Breaking && c.Type == "refactor" }},
	{"🔧 Sonstiges", func(c Commit) bool {
		return !c.Breaking && c.Type != "feat" && c.Type != "fix" && c.Type != "perf" && c.Type != "refactor"
	}},
}

// RenderChangelog erzeugt den Markdown-Abschnitt einer Version, gruppiert
// nach Commit-Typ. Commits werden pro Rubrik alphabetisch nach Scope
// sortiert; der Kurz-Hash macht jeden Eintrag nachvollziehbar.
func RenderChangelog(version string, date time.Time, commits []Commit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## v%s - %s\n", version, date.Format("2006-01-02"))

	for _, section := range sections {
		var entries []string
		for _, c := range commits {
			// Release-Vorbereitungs-Commits ("release: v… - Version &
			// Changelog vorbereitet") sind Buchhaltung des Zugs selbst -
			// als Changelog-Eintrag wären sie nur Rauschen unter Sonstiges.
			if c.Type == "release" {
				continue
			}
			if !section.Match(c) {
				continue
			}
			entry := "- "
			if c.Scope != "" {
				entry += "**" + c.Scope + "**: "
			}
			entry += c.Subject
			if len(c.Hash) >= 8 {
				entry += " (" + c.Hash[:8] + ")"
			}
			entries = append(entries, entry)
		}
		if len(entries) == 0 {
			continue
		}
		sort.Strings(entries)
		fmt.Fprintf(&b, "\n### %s\n\n", section.Title)
		for _, e := range entries {
			b.WriteString(e + "\n")
		}
	}
	return b.String()
}
