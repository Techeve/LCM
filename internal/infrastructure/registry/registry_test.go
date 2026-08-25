package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"LCM/internal/core/domain"
)

func TestParseImageRef(t *testing.T) {
	cases := []struct {
		ref             string
		host, repo, tag string
		wantErr         bool
	}{
		{ref: "nginx", host: "docker.io", repo: "library/nginx", tag: "latest"},
		{ref: "nginx:1.25", host: "docker.io", repo: "library/nginx", tag: "1.25"},
		{ref: "grafana/grafana:10.4", host: "docker.io", repo: "grafana/grafana", tag: "10.4"},
		{ref: "ghcr.io/org/app:v2", host: "ghcr.io", repo: "org/app", tag: "v2"},
		{ref: "quay.io/prometheus/node-exporter", host: "quay.io", repo: "prometheus/node-exporter", tag: "latest"},
		{ref: "registry.local:5000/a/b:t1", host: "registry.local:5000", repo: "a/b", tag: "t1"},
		{ref: "localhost/foo:dev", host: "localhost", repo: "foo", tag: "dev"},
		{ref: "redis@sha256:abcdef", host: "docker.io", repo: "library/redis", tag: "latest"},
		{ref: "redis:7@sha256:abcdef", host: "docker.io", repo: "library/redis", tag: "7"},
		{ref: "", wantErr: true},
		{ref: "@sha256:abc", wantErr: true},
	}
	for _, c := range cases {
		host, repo, tag, err := ParseImageRef(c.ref)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: fehler erwartet", c.ref)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.ref, err)
			continue
		}
		if host != c.host || repo != c.repo || tag != c.tag {
			t.Errorf("%q: bekam (%s, %s, %s), erwartet (%s, %s, %s)", c.ref, host, repo, tag, c.host, c.repo, c.tag)
		}
	}
}

// testClient baut einen Client, der alle Registry-Hosts auf den
// httptest-Server umleitet.
func testClient(srv *httptest.Server) *Client {
	c := New()
	c.http = srv.Client()
	c.baseURL = func(string) string { return srv.URL }
	return c
}

// TestCheckDigestWithTokenFlow: 401 → anonymes Token → HEAD mit
// Docker-Content-Digest (der Docker-Hub-Standardfall).
func TestCheckDigestWithTokenFlow(t *testing.T) {
	const digest = "sha256:feedface"
	tokenCalls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls++
			if r.URL.Query().Get("scope") != "repository:library/nginx:pull" {
				t.Errorf("falscher scope: %q", r.URL.Query().Get("scope"))
			}
			fmt.Fprintf(w, `{"token":"tok123","expires_in":300}`)
		case "/v2/library/nginx/manifests/1.25":
			if r.Header.Get("Authorization") != "Bearer tok123" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="test-registry",scope="repository:library/nginx:pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Method != http.MethodHead {
				t.Errorf("erwartete HEAD, bekam %s", r.Method)
			}
			w.Header().Set("Docker-Content-Digest", digest)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := testClient(srv)
	res := c.CheckDigest(context.Background(), "nginx:1.25")
	if res.Status != domain.DockerCheckOK || res.Digest != digest {
		t.Fatalf("erwartete ok/%s, bekam %+v", digest, res)
	}

	// Zweiter Check desselben Repos nutzt das gecachte Token.
	_ = c.CheckDigest(context.Background(), "nginx:1.25")
	if tokenCalls != 1 {
		t.Errorf("token sollte gecacht sein, wurde %d-mal geholt", tokenCalls)
	}
}

// TestCheckDigestGETFallback: Registry ohne Digest-Header bei HEAD -
// der Digest wird aus dem Manifest-Body berechnet.
func TestCheckDigestGETFallback(t *testing.T) {
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json"}`)
	sum := sha256.Sum256(manifest)
	want := "sha256:" + hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/org/app/manifests/v2" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Kein Docker-Content-Digest-Header; Body nur bei GET.
		if r.Method == http.MethodGet {
			_, _ = w.Write(manifest)
		}
	}))
	defer srv.Close()

	res := testClient(srv).CheckDigest(context.Background(), "ghcr.io/org/app:v2")
	if res.Status != domain.DockerCheckOK || res.Digest != want {
		t.Fatalf("erwartete ok/%s, bekam %+v", want, res)
	}
}

// TestCheckDigestUnauthorized: privates Image - Token wird vergeben, aber
// der Manifest-Zugriff bleibt verboten → "nicht prüfbar".
func TestCheckDigestUnauthorized(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			fmt.Fprintf(w, `{"token":"anon"}`)
		default:
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="t"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusForbidden) // anonym: kein Zugriff
		}
	}))
	defer srv.Close()

	res := testClient(srv).CheckDigest(context.Background(), "acme/privat:1.0")
	if res.Status != domain.DockerCheckUnauthorized {
		t.Fatalf("erwartete unauthorized, bekam %+v", res)
	}
}

// TestCheckDigestNotFound: unbekanntes Repo/Tag.
func TestCheckDigestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	res := testClient(srv).CheckDigest(context.Background(), "gibtsnicht:latest")
	if res.Status != domain.DockerCheckNotFound {
		t.Fatalf("erwartete not_found, bekam %+v", res)
	}
}

func TestParseWWWAuthenticate(t *testing.T) {
	p := parseWWWAuthenticate(`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`)
	if p["realm"] != "https://auth.docker.io/token" || p["service"] != "registry.docker.io" ||
		p["scope"] != "repository:library/nginx:pull" {
		t.Errorf("challenge falsch geparst: %v", p)
	}
	if len(parseWWWAuthenticate("Basic realm=x")) != 0 {
		t.Error("basic-challenge sollte leer parsen")
	}
}
