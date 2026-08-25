package services

import (
	"errors"
	"strings"
	"testing"
)

func TestComposeUpdateScript(t *testing.T) {
	// Bevorzugt: working_dir.
	s, err := composeUpdateScript("/opt/webshop", "/opt/webshop/compose.yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	if s != "cd '/opt/webshop' && docker compose pull && docker compose up -d" {
		t.Errorf("unerwartetes skript: %q", s)
	}

	// Mit Service-Einschränkung.
	s, err = composeUpdateScript("/opt/webshop", "", "web")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "pull 'web'") || !strings.Contains(s, "up -d 'web'") {
		t.Errorf("service-einschränkung fehlt: %q", s)
	}

	// Fallback: config_files (auch mehrere, kommagetrennt).
	s, err = composeUpdateScript("", "/a/compose.yaml, /a/override.yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "-f '/a/compose.yaml' -f '/a/override.yaml' pull") {
		t.Errorf("config-file-fallback falsch: %q", s)
	}

	// Beides leer → Fehler VOR Job-Start.
	if _, err := composeUpdateScript("", "", ""); !errors.Is(err, ErrComposePathMissing) {
		t.Errorf("erwartete ErrComposePathMissing, bekam %v", err)
	}

	// Injection im Service-Namen wird abgelehnt.
	if _, err := composeUpdateScript("/opt/x", "", "web; rm -rf /"); !errors.Is(err, ErrInvalidComposeName) {
		t.Errorf("injection im service-namen nicht abgefangen: %v", err)
	}

	// Gefährliches working_dir wird durch shellQuote neutralisiert.
	s, _ = composeUpdateScript("/opt/x'; rm -rf /; '", "", "")
	if strings.Contains(s, "; rm -rf /;") && !strings.Contains(s, `'\''`) {
		t.Errorf("working_dir nicht gequotet: %q", s)
	}
}

func TestDockerPullScript(t *testing.T) {
	if s := dockerPullScript("nginx:1.25"); s != "docker pull 'nginx:1.25'" {
		t.Errorf("unerwartetes skript: %q", s)
	}
	// Quoting neutralisiert Sonderzeichen (die Ref-Validierung läuft davor).
	if s := dockerPullScript("a'b"); !strings.Contains(s, `'\''`) {
		t.Errorf("ref nicht gequotet: %q", s)
	}
}

func TestDockerRemoveAndPruneScripts(t *testing.T) {
	if s := dockerRemoveImageScript("nginx:1.25"); s != "docker rmi 'nginx:1.25'" {
		t.Errorf("rmi-skript falsch: %q", s)
	}
	if s := dockerPruneScript(); s != "docker image prune -af" {
		t.Errorf("prune-skript falsch: %q", s)
	}
}

func TestDockerInputValidation(t *testing.T) {
	valid := []string{"nginx:1.25", "ghcr.io/org/app:v2", "registry.local:5000/a/b:t", "redis@sha256:abc"}
	for _, ref := range valid {
		if !reDockerImageRef.MatchString(ref) {
			t.Errorf("gültige ref abgelehnt: %q", ref)
		}
	}
	invalid := []string{"", "nginx; rm -rf /", "nginx'x", "a b", "-nginx", "$(reboot)"}
	for _, ref := range invalid {
		if reDockerImageRef.MatchString(ref) {
			t.Errorf("ungültige ref akzeptiert: %q", ref)
		}
	}
	if !reComposeName.MatchString("webshop") || !reComposeName.MatchString("web_shop-2.0") {
		t.Error("gültige projektnamen abgelehnt")
	}
	for _, name := range []string{"", "web shop", "web;x", "'web'", "-web"} {
		if reComposeName.MatchString(name) {
			t.Errorf("ungültiger projektname akzeptiert: %q", name)
		}
	}
}
