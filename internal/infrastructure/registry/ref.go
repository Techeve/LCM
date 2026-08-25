package registry

import (
	"errors"
	"strings"
)

// ParseImageRef zerlegt eine Docker-Image-Referenz in Registry-Host,
// Repository-Pfad und Tag - nach den Docker-Namenskonventionen:
//
//	nginx                  → docker.io  library/nginx      latest
//	nginx:1.25             → docker.io  library/nginx      1.25
//	grafana/grafana:10     → docker.io  grafana/grafana    10
//	ghcr.io/org/app:v2     → ghcr.io    org/app            v2
//	reg.local:5000/a/b:t   → reg.local:5000  a/b           t
//
// Ein angehängter Digest ("@sha256:…") wird entfernt - geprüft wird
// immer der Tag. Enthält die erste Pfadkomponente einen Punkt oder
// Doppelpunkt (oder ist "localhost"), ist sie ein Registry-Host; sonst
// gilt Docker Hub mit implizitem "library/"-Namespace für einteilige
// Repos.
func ParseImageRef(ref string) (host, repo, tag string, err error) {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	if ref == "" {
		return "", "", "", errors.New("leere image-referenz")
	}

	host = "docker.io"
	rest := ref
	if first, remainder, ok := strings.Cut(ref, "/"); ok &&
		(strings.ContainsAny(first, ".:") || first == "localhost") {
		host, rest = first, remainder
	}

	// Tag abtrennen: letzter ':' NACH dem letzten '/' (Port-Doppelpunkte
	// stecken im Host, der ist hier schon abgetrennt).
	tag = "latest"
	if i := strings.LastIndex(rest, ":"); i > strings.LastIndex(rest, "/") {
		rest, tag = rest[:i], rest[i+1:]
	}
	if rest == "" || tag == "" {
		return "", "", "", errors.New("ungültige image-referenz: " + ref)
	}

	if host == "docker.io" && !strings.Contains(rest, "/") {
		rest = "library/" + rest // offizielle Images liegen unter library/
	}
	return host, rest, tag, nil
}

// apiHost liefert den API-Endpunkt einer Registry - Docker Hub nutzt
// registry-1.docker.io, alle anderen den Host aus der Referenz.
func apiHost(host string) string {
	if host == "docker.io" {
		return "registry-1.docker.io"
	}
	return host
}
