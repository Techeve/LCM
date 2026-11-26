package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Aufwärtsmerges (community -> beta -> develop) laufen in vier Dateien fast
// immer in Konflikte, weil jede Linie ihre eigene Version und ihr eigenes
// Changelog führt. Diese vier löst die Automatik nach festen Regeln, alles
// andere bleibt bei einem Menschen:
//
//	VERSION       die höhere Version
//	CHANGELOG.md  die Abschnitte beider Seiten, ohne Doppel, in Zeitfolge
//	go.mod        je Modul die höhere Version, dann go mod tidy
//	go.sum        die Vereinigung beider Seiten, dann go mod tidy
//
// Gelesen wird aus den Index-Ständen des Merges (:2: = Ziel-Branch, :3: =
// hochgeführte Seite, :1: = gemeinsamer Vorfahr), nie aus den
// Konfliktmarkierungen.

// resolveConflicts löst die bekannten Dateien eines laufenden Merges in dir.
// Zurück kommen die Dateien, die es nicht lösen darf oder kann; dann ist
// nichts angefasst. tidy führt "go mod tidy" aus (in Tests nil).
func resolveConflicts(g gitRunner, dir string, tidy func(dir string) error) ([]string, error) {
	conflicted, err := g.lines(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	var unresolved []string
	for _, file := range conflicted {
		switch file {
		case "VERSION", "CHANGELOG.md", "go.mod", "go.sum":
		default:
			unresolved = append(unresolved, file)
		}
	}
	if len(unresolved) > 0 {
		return unresolved, nil
	}

	goModTouched := false
	for _, file := range conflicted {
		ours, okOurs := stage(g, dir, 2, file)
		theirs, okTheirs := stage(g, dir, 3, file)
		if !okOurs || !okTheirs {
			return []string{file + " (auf einer Seite gelöscht)"}, nil
		}
		var merged string
		switch file {
		case "VERSION":
			merged, err = mergeVersionFiles(ours, theirs)
		case "CHANGELOG.md":
			merged, err = mergeChangelogs(ours, theirs)
		case "go.sum":
			merged = mergeGoSums(ours, theirs)
			goModTouched = true
		case "go.mod":
			base, _ := stage(g, dir, 1, file)
			merged, err = mergeGoMod(dir, base, ours, theirs)
			goModTouched = true
		}
		if err != nil {
			return []string{fmt.Sprintf("%s (%v)", file, err)}, nil
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(merged), 0o644); err != nil {
			return nil, err
		}
	}
	if goModTouched && tidy != nil {
		if err := tidy(dir); err != nil {
			return []string{fmt.Sprintf("go.mod (go mod tidy: %v)", err)}, nil
		}
	}
	for _, file := range conflicted {
		if _, err := g.run(dir, "add", "--", file); err != nil {
			return nil, err
		}
	}
	if goModTouched {
		// tidy darf go.mod und go.sum auch dann verändert haben, wenn nur eine
		// der beiden Dateien im Konflikt lag.
		if _, err := g.run(dir, "add", "--", "go.mod", "go.sum"); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// stage liest eine Datei aus dem Index-Stand n des Merges.
func stage(g gitRunner, dir string, n int, file string) (string, bool) {
	out, err := g.exec(dir, "show", fmt.Sprintf(":%d:%s", n, file))
	if err != nil {
		return "", false
	}
	return out.stdout, true
}

func mergeVersionFiles(ours, theirs string) (string, error) {
	o, t := strings.TrimSpace(ours), strings.TrimSpace(theirs)
	cmp, err := compareVersions(o, t)
	if err != nil {
		return "", err
	}
	if cmp < 0 {
		return t + "\n", nil
	}
	return o + "\n", nil
}

// ---- CHANGELOG.md ---------------------------------------------------------

var changelogHeader = regexp.MustCompile(`^## v(\S+) - (\S+)\s*$`)

type changelogSection struct {
	version, date, text string
}

// parseChangelog trennt den Kopf vor dem ersten Abschnitt von den Abschnitten.
func parseChangelog(content string) (preamble string, sections []changelogSection) {
	var head []string
	var current *changelogSection
	var body []string
	flush := func() {
		if current != nil {
			current.text = strings.TrimRight(strings.Join(body, "\n"), "\n ")
			sections = append(sections, *current)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if m := changelogHeader.FindStringSubmatch(line); m != nil {
			flush()
			current = &changelogSection{version: m[1], date: m[2]}
			body = []string{line}
			continue
		}
		if current == nil {
			head = append(head, line)
		} else {
			body = append(body, line)
		}
	}
	flush()
	return strings.TrimRight(strings.Join(head, "\n"), "\n"), sections
}

// mergeChangelogs vereinigt zwei Changelogs: jeder Abschnitt einmal, neueste
// zuerst (Datum, bei gleichem Datum die höhere Version). Trägt dieselbe
// Version auf beiden Seiten verschiedenen Text, ist das kein Fall für eine
// Regel.
func mergeChangelogs(ours, theirs string) (string, error) {
	preamble, a := parseChangelog(ours)
	theirPreamble, b := parseChangelog(theirs)
	if preamble == "" {
		preamble = theirPreamble
	}
	byVersion := map[string]changelogSection{}
	var all []changelogSection
	for _, s := range append(a, b...) {
		seen, ok := byVersion[s.version]
		if !ok {
			byVersion[s.version] = s
			all = append(all, s)
		} else if seen.text != s.text {
			return "", fmt.Errorf("abschnitt v%s lautet auf den Seiten verschieden", s.version)
		}
	}
	kept := all
	var sortErr error
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].date != kept[j].date {
			return kept[i].date > kept[j].date
		}
		cmp, err := compareVersions(kept[i].version, kept[j].version)
		if err != nil {
			sortErr = err
		}
		return cmp > 0
	})
	if sortErr != nil {
		return "", sortErr
	}
	var out strings.Builder
	if preamble != "" {
		out.WriteString(preamble + "\n\n")
	}
	texts := make([]string, len(kept))
	for i, s := range kept {
		texts[i] = s.text
	}
	out.WriteString(strings.Join(texts, "\n\n") + "\n")
	return out.String(), nil
}

// ---- go.mod und go.sum ----------------------------------------------------

type goModInfo struct {
	requires  map[string]string
	goVersion string
	toolchain string
}

// parseGoMod liest require-Zeilen und die go-/toolchain-Direktive. Replace-
// und exclude-Zeilen bleiben beim Ziel-Branch, wie er ist.
func parseGoMod(content string) goModInfo {
	info := goModInfo{requires: map[string]string{}}
	inRequire := false
	for _, line := range strings.Split(content, "\n") {
		line, _, _ = strings.Cut(line, "//")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch {
		case fields[0] == "require" && len(fields) == 2 && fields[1] == "(":
			inRequire = true
		case fields[0] == ")":
			inRequire = false
		case fields[0] == "require" && len(fields) >= 3:
			info.requires[fields[1]] = fields[2]
		case inRequire && len(fields) >= 2:
			info.requires[fields[0]] = fields[1]
		case fields[0] == "go" && len(fields) == 2:
			info.goVersion = fields[1]
		case fields[0] == "toolchain" && len(fields) == 2:
			info.toolchain = fields[1]
		}
	}
	return info
}

// goModEdits plant die Änderungen, die aus der Fassung des Ziel-Branches das
// Ergebnis machen: je Modul die höhere Version, neue Module der Gegenseite
// dazu, Module, die eine Seite gegenüber dem Vorfahren entfernt hat, bleiben
// entfernt.
func goModEdits(base, ours, theirs goModInfo) ([]string, error) {
	var edits []string
	modules := make([]string, 0, len(theirs.requires))
	for m := range theirs.requires {
		modules = append(modules, m)
	}
	sort.Strings(modules)
	for _, m := range modules {
		tv := theirs.requires[m]
		ov, inOurs := ours.requires[m]
		_, inBase := base.requires[m]
		switch {
		case inOurs:
			cmp, err := compareVersions(tv, ov)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", m, err)
			}
			if cmp > 0 {
				edits = append(edits, "-require="+m+"@"+tv)
			}
		case !inBase:
			edits = append(edits, "-require="+m+"@"+tv)
		}
	}
	var dropped []string
	for m := range ours.requires {
		_, inTheirs := theirs.requires[m]
		_, inBase := base.requires[m]
		if !inTheirs && inBase {
			dropped = append(dropped, m)
		}
	}
	sort.Strings(dropped)
	for _, m := range dropped {
		edits = append(edits, "-droprequire="+m)
	}
	if higher(normalizeGoVersion(theirs.goVersion), normalizeGoVersion(ours.goVersion)) {
		edits = append(edits, "-go="+theirs.goVersion)
	}
	if higher(normalizeGoVersion(strings.TrimPrefix(theirs.toolchain, "go")), normalizeGoVersion(strings.TrimPrefix(ours.toolchain, "go"))) {
		edits = append(edits, "-toolchain="+theirs.toolchain)
	}
	return edits, nil
}

// normalizeGoVersion macht aus "1.27" die Form "1.27.0" für den Vergleich.
func normalizeGoVersion(v string) string {
	if v != "" && strings.Count(v, ".") == 1 {
		return v + ".0"
	}
	return v
}

func higher(a, b string) bool {
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	cmp, err := compareVersions(a, b)
	return err == nil && cmp > 0
}

// mergeGoMod schreibt die Fassung des Ziel-Branches nach dir/go.mod, wendet
// die geplanten Änderungen mit "go mod edit" an und liefert das Ergebnis.
func mergeGoMod(dir, base, ours, theirs string) (string, error) {
	edits, err := goModEdits(parseGoMod(base), parseGoMod(ours), parseGoMod(theirs))
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(ours), 0o644); err != nil {
		return "", err
	}
	if len(edits) > 0 {
		cmd := exec.Command("go", append([]string{"mod", "edit"}, edits...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("go mod edit: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	merged, err := os.ReadFile(path)
	return string(merged), err
}

// mergeGoSums vereinigt beide Seiten Zeile für Zeile; go mod tidy räumt auf,
// was danach nicht mehr gebraucht wird.
func mergeGoSums(ours, theirs string) string {
	set := map[string]bool{}
	for _, line := range strings.Split(ours+"\n"+theirs, "\n") {
		if strings.TrimSpace(line) != "" {
			set[line] = true
		}
	}
	lines := make([]string, 0, len(set))
	for line := range set {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// goModTidy ist der echte tidy-Aufruf der Automatik.
func goModTidy(dir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
