// tools/release - automatische Versionierung & Changelog-Generierung
// nach Conventional Commits (https://www.conventionalcommits.org).
//
// Das Tool analysiert alle Commits seit dem letzten Release-Tag (v*),
// leitet daraus die Art des Versionssprungs ab und rendert den
// Changelog-Abschnitt:
//
//	feat!: / fix!: / "BREAKING CHANGE" im Body  ->  Major (2.0.0)
//	feat:                                        ->  Minor (1.2.0)
//	fix: / perf: / refactor:                     ->  Patch (1.1.1)
//	docs: / test: / ci: / chore: / build: ...    ->  kein Release-Auslöser
//
// Verwendung (lokal wie in der CI):
//
//	go run ./tools/release                  # Vorschau: nächste Version + Changelog
//	go run ./tools/release -env release.env -changelog changelog-snippet.md
//	go run ./tools/release -version 1.0.0   # explizite Version (z.B. Beta->Final)
//
// Release-Ablauf (siehe packaging/prepare-release.sh): Version und Changelog
// werden VOR dem Merge auf develop vorbereitet und committet - damit trägt der
// nach main überführte Commit bereits den passenden Changelog. Der release-Job
// auf main liest anschließend nur noch VERSION + CHANGELOG.md und erzeugt Tag
// und Release daraus (kein nachgelagerter Writeback mehr nötig).
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	envOut := flag.String("env", "", "dotenv-Ausgabedatei (NEXT_VERSION, RELEASE_NEEDED, ...)")
	changelogOut := flag.String("changelog", "", "Ausgabedatei für den Changelog-Abschnitt (Markdown)")
	versionOverride := flag.String("version", "", "explizite Version statt Berechnung aus Commits (z.B. beim Beta->Final-Wechsel)")
	flag.Parse()

	lastTag, err := latestReleaseTag()
	if err != nil {
		fatal("letztes Release-Tag ermitteln: %v", err)
	}

	commits, err := commitsSince(lastTag)
	if err != nil {
		fatal("commits lesen: %v", err)
	}

	bump := DecideBump(commits)
	lastVersion := strings.TrimPrefix(lastTag, "v")
	if lastTag == "" {
		lastVersion = "0.0.0"
	}

	// Version bestimmen: entweder explizit vorgegeben (-version) oder aus den
	// Conventional Commits seit dem letzten Tag berechnet.
	var next string
	releaseNeeded := false
	if *versionOverride != "" {
		next = strings.TrimPrefix(strings.TrimSpace(*versionOverride), "v")
		releaseNeeded = true
		fmt.Fprintf(os.Stderr, "Explizite Version: %s (letztes Release: %s, %d Commits)\n",
			next, orDash(lastTag), len(commits))
	} else {
		next, err = NextVersion(lastVersion, bump)
		if err != nil {
			fatal("version berechnen: %v", err)
		}
		// Prerelease-Kennzeichnung: Trägt die VERSION-Datei ein Suffix (z.B.
		// "1.0.0-beta.1"), wird es an die berechnete Version angehängt →
		// "<numerisch>-beta.1". Der numerische Teil kommt weiterhin aus den
		// Commits; das Suffix markiert die Release-Reihe bewusst als Vorabversion.
		// Für ein Finale einfach das Suffix aus VERSION entfernen (oder -version nutzen).
		next = withPrerelease(next, prereleaseSuffix())
		releaseNeeded = bump != BumpNone

		fmt.Fprintf(os.Stderr, "Letztes Release: %s | Commits seit dem Release: %d | Bump: %s\n",
			orDash(lastTag), len(commits), bump)
		if bump == BumpNone {
			fmt.Fprintln(os.Stderr, "Keine release-relevanten Commits (feat/fix/perf/refactor) - kein neues Release.")
		} else {
			fmt.Fprintf(os.Stderr, "Nächste Version: %s\n", next)
		}
	}

	snippet := RenderChangelog(next, time.Now(), commits)

	if *changelogOut != "" {
		if err := os.WriteFile(*changelogOut, []byte(snippet), 0o644); err != nil {
			fatal("changelog schreiben: %v", err)
		}
	}
	if *envOut != "" {
		env := fmt.Sprintf("NEXT_VERSION=%s\nRELEASE_NEEDED=%t\nLAST_TAG=%s\n",
			next, releaseNeeded, lastTag)
		if err := os.WriteFile(*envOut, []byte(env), 0o644); err != nil {
			fatal("env schreiben: %v", err)
		}
	}
	if *changelogOut == "" && *envOut == "" {
		// Vorschau-Modus: Changelog auf stdout.
		fmt.Println(snippet)
	}
}

// prereleaseSuffix liest das Prerelease-Suffix aus der VERSION-Datei
// ("1.0.0-beta.1" -> "beta.1"). Fehlt die Datei oder das Suffix, ist es "".
func prereleaseSuffix() string {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		return ""
	}
	_, suffix, found := strings.Cut(strings.TrimSpace(string(data)), "-")
	if found {
		return suffix
	}
	return ""
}

// withPrerelease hängt ein Prerelease-Suffix an eine Version an (leer = unverändert).
func withPrerelease(version, suffix string) string {
	if suffix == "" {
		return version
	}
	return version + "-" + suffix
}

// latestReleaseTag liefert das höchste v*-Tag (SemVer-sortiert) oder "".
func latestReleaseTag() (string, error) {
	out, err := gitOutput("tag", "--list", "v*", "--sort=-v:refname")
	if err != nil {
		return "", err
	}
	tags := strings.Fields(out)
	if len(tags) == 0 {
		return "", nil
	}
	return tags[0], nil
}

// commitsSince liest alle Nicht-Merge-Commits seit dem Tag (oder alle,
// wenn tag leer ist), älteste zuerst.
func commitsSince(tag string) ([]Commit, error) {
	// %x01/%x02 als Feld-/Satztrenner - kommt in Commit-Texten nicht vor.
	format := "--format=%H%x01%s%x01%b%x02"
	args := []string{"log", "--no-merges", "--reverse", format}
	if tag != "" {
		args = append(args, tag+"..HEAD")
	}
	out, err := gitOutput(args...)
	if err != nil {
		return nil, err
	}

	var commits []Commit
	for _, record := range strings.Split(out, "\x02") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		parts := strings.SplitN(record, "\x01", 3)
		if len(parts) < 2 {
			continue
		}
		body := ""
		if len(parts) == 3 {
			body = parts[2]
		}
		commits = append(commits, ParseCommit(parts[0], parts[1], body))
	}
	return commits, nil
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "release: "+format+"\n", args...)
	os.Exit(1)
}
