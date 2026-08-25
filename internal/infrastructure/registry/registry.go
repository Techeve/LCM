// Package registry prüft über die Docker-Registry-HTTP-API v2, welcher
// Manifest-Digest aktuell hinter einem Image-Tag steht - die Basis des
// zentralen Update-Checks (Vergleich mit dem RepoDigest auf dem Server).
// Bewusst reine Standardbibliothek (Minimal Dependency Policy): HEAD auf
// das Manifest + anonymer Bearer-Token-Flow decken Docker Hub, ghcr.io,
// quay.io und generische v2-Registries ab. Private Images sind in v1
// nicht prüfbar (Status "unauthorized").
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
)

// Checker abstrahiert den Update-Check (Fake in Tests, analog trivy.Scanner).
type Checker interface {
	// CheckDigest ermittelt den aktuellen Manifest-Digest des Tags einer
	// Image-Referenz (z.B. "nginx:1.25"). Fehler landen im Result -
	// ein einzelnes unprüfbares Image bricht den Lauf nie ab.
	CheckDigest(ctx context.Context, ref string) Result
}

// Result ist das Ergebnis eines Registry-Checks.
type Result struct {
	Digest string // Manifest-Digest des Tags ("sha256:…"), leer bei Fehler
	Status string // domain.DockerCheckOK / Unauthorized / NotFound / Error
	Err    string // Fehlertext für die UI (nur bei Status != ok)
}

// acceptManifests priorisiert Multi-Arch-Manifest-List/OCI-Index - deren
// Digest ist der RepoDigest, den Docker beim Pull speichert. Ein einzelnes
// Arch-Manifest hätte einen ABWEICHENDEN Digest und meldete fälschlich
// "Update verfügbar" (häufigste Fehlerquelle solcher Checks).
const acceptManifests = "application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.index.v1+json, " +
	"application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json"

// Client ist die produktive Checker-Implementierung mit anonymem
// Token-Cache (Docker Hub vergibt ein Token pro Repository-Scope).
type Client struct {
	http *http.Client

	mu     sync.Mutex
	tokens map[string]cachedToken

	// baseURL baut die Basis-URL einer Registry - in Tests durch den
	// httptest-Server ersetzt, produktiv immer https.
	baseURL func(host string) string
}

type cachedToken struct {
	token   string
	expires time.Time
}

// New erstellt einen Registry-Client (15 s Timeout pro Request).
func New() *Client {
	return &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		tokens:  map[string]cachedToken{},
		baseURL: func(host string) string { return "https://" + apiHost(host) },
	}
}

// CheckDigest implementiert Checker.
func (c *Client) CheckDigest(ctx context.Context, ref string) Result {
	host, repo, tag, err := ParseImageRef(ref)
	if err != nil {
		return Result{Status: domain.DockerCheckError, Err: err.Error()}
	}
	manifestURL := c.baseURL(host) + "/v2/" + repo + "/manifests/" + url.PathEscape(tag)

	// 1. Versuch ohne Token - offene Registries antworten direkt, alle
	// anderen mit 401 + WWW-Authenticate (Bearer-Realm für anonyme Token).
	resp, err := c.do(ctx, http.MethodHead, manifestURL, "")
	if err != nil {
		return Result{Status: domain.DockerCheckError, Err: err.Error()}
	}
	token := ""
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		closeBody(resp)
		token, err = c.anonymousToken(ctx, challenge, repo)
		if err != nil {
			return Result{Status: domain.DockerCheckUnauthorized, Err: "anonym nicht prüfbar: " + err.Error()}
		}
		resp, err = c.do(ctx, http.MethodHead, manifestURL, token)
		if err != nil {
			return Result{Status: domain.DockerCheckError, Err: err.Error()}
		}
	}
	defer closeBody(resp)

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return Result{Status: domain.DockerCheckUnauthorized, Err: "privates image - anonym nicht prüfbar"}
	case resp.StatusCode == http.StatusNotFound:
		return Result{Status: domain.DockerCheckNotFound, Err: "repository oder tag nicht in der registry"}
	case resp.StatusCode != http.StatusOK:
		return Result{Status: domain.DockerCheckError, Err: fmt.Sprintf("registry antwortete mit HTTP %d", resp.StatusCode)}
	}

	if digest := resp.Header.Get("Docker-Content-Digest"); digest != "" {
		return Result{Digest: digest, Status: domain.DockerCheckOK}
	}
	// Manche Registries liefern den Digest-Header bei HEAD nicht - dann
	// das Manifest laden und selbst hashen (der Manifest-Digest ist per
	// Definition der sha256 des Manifest-Bodys).
	return c.digestViaGet(ctx, manifestURL, token)
}

// digestViaGet lädt das Manifest und berechnet den Digest aus dem Body.
func (c *Client) digestViaGet(ctx context.Context, manifestURL, token string) Result {
	resp, err := c.do(ctx, http.MethodGet, manifestURL, token)
	if err != nil {
		return Result{Status: domain.DockerCheckError, Err: err.Error()}
	}
	defer closeBody(resp)
	if resp.StatusCode != http.StatusOK {
		return Result{Status: domain.DockerCheckError, Err: fmt.Sprintf("manifest-abruf: HTTP %d", resp.StatusCode)}
	}
	if digest := resp.Header.Get("Docker-Content-Digest"); digest != "" {
		return Result{Digest: digest, Status: domain.DockerCheckOK}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // Manifeste sind wenige KB
	if err != nil {
		return Result{Status: domain.DockerCheckError, Err: err.Error()}
	}
	sum := sha256.Sum256(body)
	return Result{Digest: "sha256:" + hex.EncodeToString(sum[:]), Status: domain.DockerCheckOK}
}

// do führt einen Request mit Manifest-Accept-Headern (+ optional Token) aus.
func (c *Client) do(ctx context.Context, method, rawURL, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", acceptManifests)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return c.http.Do(req)
}

// anonymousToken holt ein anonymes Bearer-Token gemäß der
// WWW-Authenticate-Challenge (Docker Registry Token Authentication).
func (c *Client) anonymousToken(ctx context.Context, challenge, repo string) (string, error) {
	params := parseWWWAuthenticate(challenge)
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("keine bearer-challenge (WWW-Authenticate: %q)", challenge)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + repo + ":pull"
	}
	cacheKey := realm + "|" + params["service"] + "|" + scope

	c.mu.Lock()
	if t, ok := c.tokens[cacheKey]; ok && time.Now().Before(t.expires) {
		c.mu.Unlock()
		return t.token, nil
	}
	c.mu.Unlock()

	q := url.Values{}
	if params["service"] != "" {
		q.Set("service", params["service"])
	}
	q.Set("scope", scope)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer closeBody(resp)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token-endpunkt antwortete mit HTTP %d", resp.StatusCode)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	token := body.Token
	if token == "" {
		token = body.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("token-endpunkt lieferte kein token")
	}
	ttl := time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 60 * time.Second // Spec-Default
	}
	c.mu.Lock()
	c.tokens[cacheKey] = cachedToken{token: token, expires: time.Now().Add(ttl - 5*time.Second)}
	c.mu.Unlock()
	return token, nil
}

// parseWWWAuthenticate zerlegt eine Bearer-Challenge wie
// `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`
// in ihre Parameter (minimaler Hand-Parser, Zero-Bloat).
func parseWWWAuthenticate(h string) map[string]string {
	params := map[string]string{}
	rest, ok := strings.CutPrefix(strings.TrimSpace(h), "Bearer ")
	if !ok {
		return params
	}
	for _, part := range strings.Split(rest, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		params[strings.ToLower(key)] = strings.Trim(value, `"`)
	}
	return params
}

func closeBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

var _ Checker = (*Client)(nil)
