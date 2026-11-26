package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitRunner ruft git auf. Die Identität kommt über die Umgebung, damit weder
// die Git-Konfiguration des Runners noch die des Repositorys berührt wird.
type gitRunner struct {
	name, email string
	// secrets werden aus allem entfernt, was in Fehlermeldungen und Logs landet
	// (der Token steckt in der Push-URL).
	secrets []string
}

func (g gitRunner) environ() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME="+g.name, "GIT_AUTHOR_EMAIL="+g.email,
		"GIT_COMMITTER_NAME="+g.name, "GIT_COMMITTER_EMAIL="+g.email,
		"GIT_TERMINAL_PROMPT=0",
	)
}

func (g gitRunner) scrub(s string) string {
	for _, secret := range g.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "***")
		}
	}
	return s
}

// run führt git in dir aus und liefert stdout ohne den abschließenden Umbruch.
func (g gitRunner) run(dir string, args ...string) (string, error) {
	out, err := g.exec(dir, args...)
	return strings.TrimRight(out.stdout, "\n"), err
}

type gitOutput struct{ stdout, stderr string }

func (g gitRunner) exec(dir string, args ...string) (gitOutput, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = g.environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := gitOutput{stdout: g.scrub(stdout.String()), stderr: g.scrub(stderr.String())}
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", g.scrub(strings.Join(args, " ")), err, strings.TrimSpace(out.stderr))
	}
	return out, nil
}

// succeeds fragt nur nach dem Exit-Code (z.B. merge-base --is-ancestor).
func (g gitRunner) succeeds(dir string, args ...string) bool {
	_, err := g.exec(dir, args...)
	return err == nil
}

// isAncestor sagt, ob a in b enthalten ist.
func (g gitRunner) isAncestor(dir, a, b string) bool {
	return g.succeeds(dir, "merge-base", "--is-ancestor", a, b)
}

func (g gitRunner) lines(dir string, args ...string) ([]string, error) {
	out, err := g.run(dir, args...)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}
