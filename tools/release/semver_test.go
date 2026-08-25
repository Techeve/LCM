package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseCommit(t *testing.T) {
	cases := []struct {
		subject, body       string
		wantType, wantScope string
		wantSubject         string
		wantBreaking        bool
	}{
		{"feat: neue Suche", "", "feat", "", "neue Suche", false},
		{"feat(api): endpunkt für notes", "", "feat", "api", "endpunkt für notes", false},
		{"fix(ui): navbar umbruch", "", "fix", "ui", "navbar umbruch", false},
		{"feat!: token-format geändert", "", "feat", "", "token-format geändert", true},
		{"refactor(core): services entkoppelt", "", "refactor", "core", "services entkoppelt", false},
		{"fix: irgendwas", "Details...\n\nBREAKING CHANGE: config-Format neu", "fix", "", "irgendwas", true},
		{"Update readme", "", "", "", "Update readme", false}, // kein Conventional Commit
		{"ci: pipeline caching", "", "ci", "", "pipeline caching", false},
	}
	for _, c := range cases {
		got := ParseCommit("abcdef1234567890", c.subject, c.body)
		if got.Type != c.wantType || got.Scope != c.wantScope || got.Subject != c.wantSubject || got.Breaking != c.wantBreaking {
			t.Errorf("ParseCommit(%q): got %+v", c.subject, got)
		}
	}
}

func TestDecideBump(t *testing.T) {
	mk := func(subjects ...string) []Commit {
		var out []Commit
		for _, s := range subjects {
			out = append(out, ParseCommit("hash", s, ""))
		}
		return out
	}
	cases := []struct {
		commits []Commit
		want    Bump
	}{
		{mk("docs: readme", "chore: deps"), BumpNone},
		{mk("fix: bug"), BumpPatch},
		{mk("refactor: aufgeräumt"), BumpPatch},
		{mk("fix: bug", "feat: neu"), BumpMinor},
		{mk("feat: neu", "fix: bug", "feat!: breaking"), BumpMajor},
		{mk(), BumpNone},
		{mk("Update readme"), BumpNone}, // nicht-konforme Commits lösen nichts aus
	}
	for i, c := range cases {
		if got := DecideBump(c.commits); got != c.want {
			t.Errorf("case %d: got %s, want %s", i, got, c.want)
		}
	}
	// BREAKING CHANGE im Body
	commits := []Commit{ParseCommit("h", "fix: klein", "BREAKING CHANGE: api umgebaut")}
	if got := DecideBump(commits); got != BumpMajor {
		t.Errorf("BREAKING CHANGE im Body: got %s, want major", got)
	}
}

func TestNextVersion(t *testing.T) {
	cases := []struct {
		current string
		bump    Bump
		want    string
	}{
		{"1.0.0-beta.1", BumpMajor, "2.0.0"}, // Prerelease-Suffix am Patch wird toleriert
		{"1.1.0", BumpPatch, "1.1.1"},
		{"1.1.5", BumpMinor, "1.2.0"},
		{"1.9.3", BumpMajor, "2.0.0"},
		{"v2.0.0", BumpMinor, "2.1.0"},
		{"1.1.0", BumpNone, "1.1.0"},
		{"0.0.0", BumpMinor, "0.1.0"},
	}
	for _, c := range cases {
		got, err := NextVersion(c.current, c.bump)
		if err != nil || got != c.want {
			t.Errorf("NextVersion(%q, %s) = %q, %v - want %q", c.current, c.bump, got, err, c.want)
		}
	}
	if _, err := NextVersion("kaputt", BumpPatch); err == nil {
		t.Error("ungültige Version nicht abgelehnt")
	}
}

func TestRenderChangelog(t *testing.T) {
	commits := []Commit{
		ParseCommit("aaaa111122223333", "feat(api): notes-endpunkt", ""),
		ParseCommit("bbbb111122223333", "fix: token-ablauf korrigiert", ""),
		ParseCommit("cccc111122223333", "feat!: config-format v2", ""),
		ParseCommit("dddd111122223333", "ci: cache aktiviert", ""),
	}
	out := RenderChangelog("2.0.0", time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), commits)

	for _, want := range []string{
		"## v2.0.0 - 2026-07-04",
		"### 💥 Breaking Changes",
		"config-format v2 (cccc1111)",
		"### 🚀 Features",
		"**api**: notes-endpunkt (aaaa1111)",
		"### 🐛 Bugfixes",
		"token-ablauf korrigiert (bbbb1111)",
		"### 🔧 Sonstiges",
		"cache aktiviert (dddd1111)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("changelog enthält nicht %q:\n%s", want, out)
		}
	}
	// Reihenfolge der Rubriken: Breaking vor Features vor Bugfixes.
	if strings.Index(out, "Breaking") > strings.Index(out, "Features") {
		t.Error("Breaking Changes müssen vor Features stehen")
	}
}

func TestWithPrerelease(t *testing.T) {
	if got := withPrerelease("1.0.0", "beta.1"); got != "1.0.0-beta.1" {
		t.Errorf("withPrerelease: erwartet 1.0.0-beta.1, bekam %s", got)
	}
	if got := withPrerelease("1.2.3", ""); got != "1.2.3" {
		t.Errorf("withPrerelease leeres Suffix: erwartet 1.2.3, bekam %s", got)
	}
}
