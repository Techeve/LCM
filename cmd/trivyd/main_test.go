package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/infrastructure/trivy"
)

// Der Sidecar fuehrt Prozesse aus und laedt Images aus fremden Registries.
// Die Tests halten deshalb vor allem fest, was er NICHT tut: ohne Token
// antworten, beliebig grosse Koerper lesen, Argumente ungeprueft
// weiterreichen.

// fakeTrivy legt ein Skript an, das sich wie Trivy verhaelt: Es schreibt die
// uebergebenen Argumente nach args.txt und gibt die vorgegebene Antwort aus.
func fakeTrivy(t *testing.T, stdout string, exitCode int) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	bin = filepath.Join(dir, "trivy")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\n" +
		"cat <<'ENDE'\n" + stdout + "\nENDE\n" +
		"exit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	return string(rune('0' + i))
}

func testServer(t *testing.T, bin string) *httptest.Server {
	t.Helper()
	s := &server{trivy: bin, cache: t.TempDir(), token: "test-token"}
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv
}

func request(t *testing.T, srv *httptest.Server, method, path, token, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	buf := make([]byte, 64<<10)
	n, _ := resp.Body.Read(buf)
	return resp, string(buf[:n])
}

// TestOhneTokenKeineAntwort ist die wichtigste Zusage: Der Dienst startet
// Prozesse. Ein offener Endpunkt dafuer ist kein Zustand, den man
// versehentlich erreichen darf.
func TestOhneTokenKeineAntwort(t *testing.T) {
	bin, _ := fakeTrivy(t, `{"Results":[]}`, 0)
	srv := testServer(t, bin)

	for _, fall := range []struct{ method, path string }{
		{http.MethodGet, trivy.PathInfo},
		{http.MethodPost, trivy.PathScanSBOM},
		{http.MethodPost, trivy.PathScanImage},
		{http.MethodPost, trivy.PathUpdateDB},
	} {
		resp, _ := request(t, srv, fall.method, fall.path, "", "{}")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s ohne Token: erwartete 401, bekam %d", fall.method, fall.path, resp.StatusCode)
		}
		resp, _ = request(t, srv, fall.method, fall.path, "falsch", "{}")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s mit falschem Token: erwartete 401, bekam %d", fall.method, fall.path, resp.StatusCode)
		}
	}
}

// TestHealthzOhneToken: Die Lebendpruefung laeuft bewusst ohne Token - sonst
// stuende es im Klartext in der Container-Zeile, sichtbar in jedem
// `docker inspect`. Sie verraet auch nichts ausser „ich lebe".
func TestHealthzOhneToken(t *testing.T) {
	bin, _ := fakeTrivy(t, "", 0)
	srv := testServer(t, bin)

	resp, body := request(t, srv, http.MethodGet, trivy.PathHealth, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz muss ohne Token antworten, bekam %d", resp.StatusCode)
	}
	if strings.Contains(body, "test-token") {
		t.Error("die Antwort darf nichts ueber den Zugang verraten")
	}
}

// TestSbomScanLaedtNichtNach: Trivy haelt seine Datenbank nur 24 Stunden fuer
// gueltig und will danach mitten im Scan nachladen. In der Testumgebung fiel
// die CVE-Bewertung dadurch taeglich aus. --skip-db-update ist die Zusage,
// dass ein Scan auswertet, was da ist - nachgeladen wird nur ueber /db/update.
func TestSbomScanLaedtNichtNach(t *testing.T) {
	bin, argsFile := fakeTrivy(t, `{"Results":[]}`, 0)
	srv := testServer(t, bin)

	resp, _ := request(t, srv, http.MethodPost, trivy.PathScanSBOM, "test-token", `{"bomFormat":"CycloneDX"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("erwartete 200, bekam %d", resp.StatusCode)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--skip-db-update") {
		t.Errorf("der SBOM-Scan darf nicht nachladen duerfen:\n%s", args)
	}
	if !strings.Contains(string(args), "sbom") {
		t.Errorf("erwartete einen sbom-Aufruf:\n%s", args)
	}
}

// TestImageReferenzWirdGeprueft: Die Referenz landet als Argument in einem
// Prozessaufruf. Eine Shell gibt es dazwischen nicht - ein fuehrender
// Bindestrich wuerde aber als OPTION gelesen und koennte Trivys Verhalten
// umstellen.
func TestImageReferenzWirdGeprueft(t *testing.T) {
	bin, _ := fakeTrivy(t, `{"Results":[]}`, 0)
	srv := testServer(t, bin)

	for _, ref := range []string{"", "  ", "--cache-dir=/etc"} {
		body, _ := json.Marshal(trivy.ImageRequest{Ref: ref})
		resp, _ := request(t, srv, http.MethodPost, trivy.PathScanImage, "test-token", string(body))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Referenz %q: erwartete 400, bekam %d", ref, resp.StatusCode)
		}
	}
	body, _ := json.Marshal(trivy.ImageRequest{Ref: "nginx:1.25"})
	resp, _ := request(t, srv, http.MethodPost, trivy.PathScanImage, "test-token", string(body))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("eine gueltige Referenz muss durchgehen, bekam %d", resp.StatusCode)
	}
}

// TestInfoOhneDatenbankSagtEsDeutlich: „Noch nie geladen" ist der
// gefaehrlichste Zustand - dann hat kein Scan echte Daten gesehen, und „keine
// Funde" saehe aus wie Entwarnung.
func TestInfoOhneDatenbankSagtEsDeutlich(t *testing.T) {
	bin, _ := fakeTrivy(t, `{"Version":"0.74.0"}`, 0)
	srv := testServer(t, bin)

	_, body := request(t, srv, http.MethodGet, trivy.PathInfo, "test-token", "")
	var resp trivy.InfoResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("Antwort nicht lesbar: %v (%s)", err, body)
	}
	if !resp.Available || resp.Version != "0.74.0" {
		t.Errorf("Version fehlt: %+v", resp)
	}
	if resp.UpdatedAt != nil {
		t.Errorf("ohne DB-Block darf kein Zeitpunkt entstehen: %v", *resp.UpdatedAt)
	}
	if !strings.Contains(resp.Error, "nie geladen") {
		t.Errorf("der Zustand muss benannt werden, bekam %q", resp.Error)
	}
}

// TestScanFehlerBehaeltDieUrsache: Trivy schreibt seine Diagnose auf stderr.
// Geht die verloren, steht beim Betreiber nur „exit status 1".
func TestScanFehlerBehaeltDieUrsache(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "trivy")
	script := "#!/bin/sh\necho 'FATAL unable to find CPE indices' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := testServer(t, bin)

	resp, body := request(t, srv, http.MethodPost, trivy.PathScanSBOM, "test-token", `{"bomFormat":"CycloneDX"}`)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("erwartete 502, bekam %d", resp.StatusCode)
	}
	if !strings.Contains(body, "CPE indices") {
		t.Errorf("die Ursache ging verloren: %s", body)
	}
}

// TestLoopbackAdresse: Die Lausch-Adresse ist keine Zieladresse. „:9330" und
// „0.0.0.0:9330" muessen fuer den eigenen Healthcheck auf den Loopback zeigen.
func TestLoopbackAdresse(t *testing.T) {
	cases := map[string]string{
		":9330":          "127.0.0.1:9330",
		"0.0.0.0:9330":   "127.0.0.1:9330",
		"[::]:9330":      "127.0.0.1:9330",
		"127.0.0.1:9999": "127.0.0.1:9999",
		"kaputt":         "127.0.0.1:9330",
	}
	for ein, expected := range cases {
		if got := loopbackAddr(ein); got != expected {
			t.Errorf("%q -> %q, erwartet %q", ein, got, expected)
		}
	}
}
