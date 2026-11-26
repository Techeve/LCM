// tools/autorelease - Release-Automatik für das Wochen-Release.
//
// Renovate hält die Abhängigkeiten auf develop, community und enterprise
// aktuell (renovate.json). Dieses Werkzeug macht daraus Releases und führt sie
// zurück:
//
//	autorelease patch -line community|enterprise
//	    Entscheidet (Dienstag in Europe/Berlin mit Release-Anlass seit dem
//	    letzten Tag der Linie, oder sofort bei einem Sicherheitsupdate), nimmt
//	    die nächste freie Patch-Nummer, bereitet das Release auf
//	    hotfix/auto-<version> vor (packaging/prepare-release.sh), pusht und
//	    öffnet den MR in die Linie mit Automerge. Der Merge taggt und rollt aus.
//
//	autorelease merge-up
//	    Führt community nach beta und beta nach develop zurück (MR mit
//	    Automerge). VERSION, CHANGELOG.md, go.mod und go.sum löst es nach festen
//	    Regeln, bei jedem anderen Konflikt legt es ein Issue mit dem Label
//	    release-automatik an und endet mit Fehler.
//
// Läuft in der CI (Jobs release:patch und merge:up in .gitlab-ci.yml) mit
// RENOVATE_TOKEN; Pausieren siehe docs/reference/ci-release.md.
package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fatal("Aufruf: autorelease patch -line <community|enterprise> | merge-up")
	}
	if os.Getenv("AUTO_RELEASE_PAUSE") == "true" {
		fmt.Println("AUTO_RELEASE_PAUSE=true - Release-Automatik pausiert, nichts zu tun.")
		return
	}

	var err error
	switch os.Args[1] {
	case "patch":
		flags := flag.NewFlagSet("patch", flag.ExitOnError)
		line := flags.String("line", "", "Linie: community oder enterprise")
		_ = flags.Parse(os.Args[2:])
		if *line != "community" && *line != "enterprise" {
			fatal("-line muss community oder enterprise sein")
		}
		var a *app
		if a, err = appFromEnv(); err == nil {
			err = a.patchRelease(*line)
		}
	case "merge-up":
		var a *app
		if a, err = appFromEnv(); err == nil {
			err = a.mergeUp()
		}
	default:
		fatal("unbekannter Befehl %q", os.Args[1])
	}
	if err != nil {
		fatal("%v", err)
	}
}

// appFromEnv verdrahtet die echten Abhängigkeiten aus der CI-Umgebung.
func appFromEnv() (*app, error) {
	token := os.Getenv("RENOVATE_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("CI-Variable RENOVATE_TOKEN fehlt (Projekt-Access-Token mit api + write_repository, siehe docs/reference/ci-release.md)")
	}
	server, project := os.Getenv("CI_SERVER_URL"), os.Getenv("CI_PROJECT_PATH")
	apiURL, projectID := os.Getenv("CI_API_V4_URL"), os.Getenv("CI_PROJECT_ID")
	if server == "" || project == "" || apiURL == "" || projectID == "" {
		return nil, fmt.Errorf("CI_SERVER_URL, CI_PROJECT_PATH, CI_API_V4_URL und CI_PROJECT_ID erwartet - das Werkzeug läuft in der CI")
	}
	pushURL, err := authenticatedURL(server, project, token)
	if err != nil {
		return nil, err
	}
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	g := gitRunner{
		name:    envOr("RELEASE_BOT_NAME", "Release-Automatik"),
		email:   envOr("RELEASE_BOT_EMAIL", "release-automatik@techeve.de"),
		secrets: []string{token},
	}
	return &app{
		git:      g,
		api:      &GitLab{BaseURL: apiURL, Project: projectID, Token: token},
		repoDir:  repoDir,
		pushURL:  pushURL,
		now:      time.Now,
		sleep:    time.Sleep,
		prepare:  func(dir, version string) error { return runPrepareRelease(g, dir, version) },
		relevant: releaseRelevant,
		tidy:     goModTidy,
		out:      os.Stdout,
	}, nil
}

func authenticatedURL(server, project, token string) (string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword("oauth2", token)
	u.Path = "/" + project + ".git"
	return u.String(), nil
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// runPrepareRelease ruft das Skript der Linie in deren Worktree auf, auf dem
// Branch hotfix/auto-<version> (das Skript prüft den Branch-Namen).
func runPrepareRelease(g gitRunner, dir, version string) error {
	cmd := exec.Command("bash", "packaging/prepare-release.sh", version)
	cmd.Dir = dir
	cmd.Env = g.environ()
	out, err := cmd.CombinedOutput()
	fmt.Print(g.scrub(string(out)))
	return err
}

// releaseRelevant fragt tools/release der Linie, ob seit ihrem letzten Tag
// Commits mit Release-Anlass liegen - dieselbe Regel, nach der
// prepare-release.sh später entscheidet. So gibt es keine zweite Auslegung von
// "fix/perf/refactor/feat".
func releaseRelevant(dir string) (bool, error) {
	envFile, err := os.CreateTemp("", "release-env-")
	if err != nil {
		return false, err
	}
	defer os.Remove(envFile.Name())
	_ = envFile.Close()

	cmd := exec.Command("go", "run", "./tools/release", "-env", envFile.Name())
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("tools/release: %w: %s", err, strings.TrimSpace(string(out)))
	}
	raw, err := os.ReadFile(envFile.Name())
	if err != nil {
		return false, err
	}
	return strings.Contains(string(raw), "RELEASE_NEEDED=true"), nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "autorelease: "+format+"\n", args...)
	os.Exit(1)
}
