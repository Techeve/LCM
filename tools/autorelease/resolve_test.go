package main

import (
	"strings"
	"testing"
)

const goModBase = `module example.com/app

go 1.27.1

require (
	github.com/a/a v1.0.0
	github.com/b/b v1.0.0
	github.com/c/c v1.0.0
	github.com/gone/gone v1.0.0
)
`

func goModWith(goVersion string, requires ...string) string {
	return "module example.com/app\n\ngo " + goVersion + "\n\nrequire (\n\t" + strings.Join(requires, "\n\t") + "\n)\n"
}

func assertResolved(t *testing.T, dir string) {
	t.Helper()
	left, err := testGit().lines(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil || len(left) > 0 {
		t.Fatalf("ungelöste Dateien: %v, %v", left, err)
	}
}

func TestVersionDieHoehereGewinnt(t *testing.T) {
	for name, c := range map[string]struct{ ours, theirs, want string }{
		"Ziel hat die Beta":         {"2.4.0-beta.3", "2.3.3", "2.4.0-beta.3"},
		"Gegenseite ist höher":      {"2.3.3", "2.3.4", "2.3.4"},
		"Finale schlägt Vorabstand": {"2.4.0-beta.9", "2.4.0", "2.4.0"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := mergeFixture(t,
				map[string]string{"VERSION": "2.3.2\n"},
				map[string]string{"VERSION": c.ours + "\n"},
				map[string]string{"VERSION": c.theirs + "\n"})
			unresolved, err := resolveConflicts(testGit(), dir, nil)
			if err != nil || len(unresolved) > 0 {
				t.Fatalf("unresolved=%v err=%v", unresolved, err)
			}
			assertResolved(t, dir)
			if got := readFile(t, dir, "VERSION"); got != c.want+"\n" {
				t.Errorf("VERSION = %q, erwartet %q", got, c.want)
			}
		})
	}
}

const changelogOurs = `# Changelog

## v2.4.0-beta.1 - 2026-11-28

### 🚀 Features

- **ui**: Neue Suche (aaaa1111)

## v2.3.2 - 2026-11-25

### 🐛 Bugfixes

- **agent**: Der Agent bleibt stehen (bbbb2222)
`

const changelogTheirs = `# Changelog

## v2.3.3 - 2026-12-01

### 🐛 Bugfixes

- Abhängigkeiten aktualisiert

## v2.3.2 - 2026-11-25

### 🐛 Bugfixes

- **agent**: Der Agent bleibt stehen (bbbb2222)
`

func TestChangelogBeideAbschnitteOhneDoppel(t *testing.T) {
	merged, err := mergeChangelogs(changelogOurs, changelogTheirs)
	if err != nil {
		t.Fatal(err)
	}
	want := `# Changelog

## v2.3.3 - 2026-12-01

### 🐛 Bugfixes

- Abhängigkeiten aktualisiert

## v2.4.0-beta.1 - 2026-11-28

### 🚀 Features

- **ui**: Neue Suche (aaaa1111)

## v2.3.2 - 2026-11-25

### 🐛 Bugfixes

- **agent**: Der Agent bleibt stehen (bbbb2222)
`
	if merged != want {
		t.Errorf("Changelog:\n%s\nerwartet:\n%s", merged, want)
	}
}

// Dasselbe Changelog auf beiden Seiten darf nichts verändern - auch nicht
// die Reihenfolge oder die Leerzeilen, die tools/release schreibt.
func TestChangelogMitSichSelbstBleibtGleich(t *testing.T) {
	for _, changelog := range []string{changelogOurs, changelogTheirs} {
		merged, err := mergeChangelogs(changelog, changelog)
		if err != nil {
			t.Fatal(err)
		}
		if merged != changelog {
			t.Errorf("Vereinigung mit sich selbst verändert den Text:\n%s\nstatt:\n%s", merged, changelog)
		}
	}
}

func TestChangelogVerschiedenerTextDerselbenVersionBleibtHandarbeit(t *testing.T) {
	ours := "# Changelog\n\n## v2.3.2 - 2026-11-25\n\n- eins\n"
	theirs := "# Changelog\n\n## v2.3.2 - 2026-11-25\n\n- zwei\n"
	if _, err := mergeChangelogs(ours, theirs); err == nil {
		t.Error("abweichender Text derselben Version wurde stillschweigend vereinigt")
	}
}

func TestResolveChangelogImMerge(t *testing.T) {
	dir := mergeFixture(t,
		map[string]string{"CHANGELOG.md": "# Changelog\n\n## v2.3.2 - 2026-11-25\n\n- alt\n"},
		map[string]string{"CHANGELOG.md": changelogOurs},
		map[string]string{"CHANGELOG.md": changelogTheirs})
	unresolved, err := resolveConflicts(testGit(), dir, nil)
	if err != nil || len(unresolved) > 0 {
		t.Fatalf("unresolved=%v err=%v", unresolved, err)
	}
	assertResolved(t, dir)
	got := readFile(t, dir, "CHANGELOG.md")
	if !strings.Contains(got, "v2.3.3") || !strings.Contains(got, "v2.4.0-beta.1") || strings.Contains(got, "<<<<") {
		t.Errorf("Changelog nach dem Merge:\n%s", got)
	}
}

func TestGoModHoehereVersionenUndNeueModule(t *testing.T) {
	ours := goModWith("1.27.1",
		"github.com/a/a v1.5.0", "github.com/b/b v1.0.0", "github.com/c/c v1.0.0", "github.com/gone/gone v1.0.0",
		"github.com/neu/ours v1.0.0")
	theirs := goModWith("1.27.2",
		"github.com/a/a v1.2.0", "github.com/b/b v1.3.0", "github.com/c/c v1.0.0",
		"github.com/neu/theirs v2.0.0")
	sumOurs := "github.com/a/a v1.5.0 h1:ours\ngithub.com/b/b v1.0.0 h1:b1\n"
	sumTheirs := "github.com/a/a v1.2.0 h1:theirs\ngithub.com/b/b v1.3.0 h1:b3\n"
	dir := mergeFixture(t,
		map[string]string{"go.mod": goModBase, "go.sum": "github.com/a/a v1.0.0 h1:base\n"},
		map[string]string{"go.mod": ours, "go.sum": sumOurs},
		map[string]string{"go.mod": theirs, "go.sum": sumTheirs})

	tidied := false
	unresolved, err := resolveConflicts(testGit(), dir, func(string) error { tidied = true; return nil })
	if err != nil || len(unresolved) > 0 {
		t.Fatalf("unresolved=%v err=%v", unresolved, err)
	}
	assertResolved(t, dir)
	if !tidied {
		t.Error("go mod tidy wurde nicht aufgerufen")
	}
	mod := readFile(t, dir, "go.mod")
	for _, want := range []string{
		"github.com/a/a v1.5.0",      // Ziel höher
		"github.com/b/b v1.3.0",      // Gegenseite höher
		"github.com/neu/ours v1.0.0", // nur im Ziel
		"github.com/neu/theirs v2.0.0",
		"go 1.27.2",
	} {
		if !strings.Contains(mod, want) {
			t.Errorf("go.mod ohne %q:\n%s", want, mod)
		}
	}
	if strings.Contains(mod, "gone/gone") {
		t.Errorf("go.mod trägt das von der Gegenseite entfernte Modul noch:\n%s", mod)
	}
	sum := readFile(t, dir, "go.sum")
	for _, want := range []string{"a/a v1.5.0 h1:ours", "a/a v1.2.0 h1:theirs", "b/b v1.0.0 h1:b1", "b/b v1.3.0 h1:b3"} {
		if !strings.Contains(sum, want) {
			t.Errorf("go.sum ohne %q:\n%s", want, sum)
		}
	}
}

func TestAndereKonflikteBleibenUnangetastet(t *testing.T) {
	dir := mergeFixture(t,
		map[string]string{"VERSION": "2.3.2\n", "ui/package.json": "{\"v\": 1}\n"},
		map[string]string{"VERSION": "2.3.3\n", "ui/package.json": "{\"v\": 2}\n"},
		map[string]string{"VERSION": "2.3.4\n", "ui/package.json": "{\"v\": 3}\n"})
	unresolved, err := resolveConflicts(testGit(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unresolved) != 1 || unresolved[0] != "ui/package.json" {
		t.Errorf("unresolved = %v, erwartet [ui/package.json]", unresolved)
	}
	// Nichts darf halb gelöst zurückbleiben: VERSION trägt noch Konfliktmarken.
	if !strings.Contains(readFile(t, dir, "VERSION"), "<<<<<<<") {
		t.Error("VERSION wurde trotz ungelöstem Konflikt in einer anderen Datei angefasst")
	}
}
