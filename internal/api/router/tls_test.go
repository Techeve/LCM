package router_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/api/controllers"
	"LCM/internal/api/router"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
	"LCM/internal/storage/repositories"
)

// tlsFake ist der Dienst hinter den Routen: Er nutzt die echte Prüfung und
// Ablage der Uploads, hat aber keinen Pool.
type tlsFake struct {
	dataDir string
	actors  []string
	reloads int
	// downloads nennt die Upload-IDs, deren Kette abgerufen wurde.
	downloads []string
	// config nennt Zertifikate, die der Pool aus der config.json hat.
	config []string
}

func (f *tlsFake) status() map[string]any {
	specs, _ := tlsx.UploadSpecs(f.dataDir)
	certs := []map[string]any{}
	for _, id := range f.config {
		certs = append(certs, map[string]any{"id": id, "source": "config"})
	}
	for _, spec := range specs {
		certs = append(certs, map[string]any{"id": spec.ID, "source": "upload"})
	}
	return map[string]any{"certificates": certs, "endpoints": []any{}}
}

func (f *tlsFake) Status() (any, error) { return f.status(), nil }

func (f *tlsFake) AddUpload(certPEM, keyPEM []byte, actor string) (any, error) {
	id, err := tlsx.SaveUpload(f.dataDir, certPEM, keyPEM, time.Now())
	if err != nil {
		return nil, err
	}
	f.actors = append(f.actors, actor)
	return map[string]any{"id": "upload-" + id, "status": f.status()}, nil
}

func (f *tlsFake) RemoveUpload(poolID, actor string) (any, error) {
	id, ok := tlsx.UploadIDOf(poolID)
	if !ok {
		if poolID == "config-0" {
			return nil, tlsx.ErrNotUpload
		}
		return nil, tlsx.ErrUploadNotFound
	}
	if err := tlsx.RemoveUpload(f.dataDir, id); err != nil {
		return nil, err
	}
	f.actors = append(f.actors, actor)
	return map[string]any{"status": f.status()}, nil
}

func (f *tlsFake) Reload(actor string) (any, error) {
	f.reloads++
	f.actors = append(f.actors, actor)
	return map[string]any{"report": map[string]any{"certificates": []any{}}, "status": f.status()}, nil
}

// CertificateChain liest wie der Dienst die Zertifikatsdatei des Uploads und
// gibt nur die Zertifikate aus.
func (f *tlsFake) CertificateChain(poolID, actor string) ([]byte, error) {
	id, ok := tlsx.UploadIDOf(poolID)
	if !ok {
		return nil, tlsx.ErrUploadNotFound
	}
	specs, _ := tlsx.UploadSpecs(f.dataDir)
	for _, spec := range specs {
		if spec.ID != poolID {
			continue
		}
		data, err := os.ReadFile(spec.CertPath)
		if err != nil {
			return nil, err
		}
		f.actors = append(f.actors, actor)
		f.downloads = append(f.downloads, id)
		return tlsx.CertificateChainPEM(data)
	}
	return nil, tlsx.ErrUploadNotFound
}

func tlsApp(t *testing.T, manager controllers.TLSManager) (*fiber.App, *gorm.DB) {
	t.Helper()
	return buildTestAppDB(t, 0, nil, func(d *router.Deps, _ *gorm.DB) { d.TLS = manager })
}

// pemFiles liefert ein Zertifikat samt Schlüssel für lcm.example.
func pemFiles(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	dir := t.TempDir()
	tlsxtest.ForeignCert(t, dir, time.Now().Add(60*24*time.Hour))
	certPEM, err := os.ReadFile(filepath.Join(dir, tlsx.CertFileName))
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err = os.ReadFile(filepath.Join(dir, tlsx.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	return certPEM, keyPEM
}

// uploadForm baut den Multipart-Körper; ein nil-Inhalt lässt das Feld weg.
func uploadForm(t *testing.T, certPEM, keyPEM []byte) (body string, headers map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for name, data := range map[string][]byte{"certificate": certPEM, "key": keyPEM} {
		if data == nil {
			continue
		}
		part, err := w.CreateFormFile(name, name+".pem")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String(), map[string]string{"Content-Type": w.FormDataContentType()}
}

func postUpload(t *testing.T, app *fiber.App, token string, certPEM, keyPEM []byte) *http.Response {
	t.Helper()
	body, headers := uploadForm(t, certPEM, keyPEM)
	return doRequestWithHeaders(t, app, "POST", "/api/v1/system/tls/certificates", token, body, headers)
}

func uploadedIDs(t *testing.T, f *tlsFake) []string {
	t.Helper()
	specs, err := tlsx.UploadSpecs(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, s := range specs {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestTLSStatusListsCertificates(t *testing.T) {
	f := &tlsFake{dataDir: t.TempDir(), config: []string{"config-0"}}
	app, _ := tlsApp(t, f)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	resp := doRequest(t, app, "GET", "/api/v1/system/tls", admin, "")
	var out struct {
		Certificates []struct{ ID, Source string } `json:"certificates"`
		Endpoints    []any                         `json:"endpoints"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&out) != nil {
		t.Fatalf("Status %d", resp.StatusCode)
	}
	if len(out.Certificates) != 1 || out.Certificates[0].Source != "config" || out.Endpoints == nil {
		t.Errorf("Antwort: %+v", out)
	}
}

func TestTLSUploadStoresValidatedPair(t *testing.T) {
	f := &tlsFake{dataDir: t.TempDir()}
	app, _ := tlsApp(t, f)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	certPEM, keyPEM := pemFiles(t)

	resp := postUpload(t, app, admin, certPEM, keyPEM)
	if resp.StatusCode != 201 {
		t.Fatalf("Upload: %d", resp.StatusCode)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.ID == "" {
		t.Fatalf("Antwort: %+v, %v", out, err)
	}
	if ids := uploadedIDs(t, f); len(ids) != 1 || ids[0] != out.ID {
		t.Errorf("abgelegt: %v, erwartet %s", ids, out.ID)
	}
	if len(f.actors) != 1 || f.actors[0] != "admin" {
		t.Errorf("Actor = %v, erwartet den Benutzernamen", f.actors)
	}
}

func TestTLSUploadRejectsBadFiles(t *testing.T) {
	f := &tlsFake{dataDir: t.TempDir()}
	app, _ := tlsApp(t, f)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	certPEM, keyPEM := pemFiles(t)
	_, otherKey := pemFiles(t)

	cases := []struct {
		name      string
		cert, key []byte
		status    int
		code      string
	}{
		{"Schlüssel fehlt", certPEM, nil, 400, "tls.upload_missing"},
		{"Zertifikat fehlt", nil, keyPEM, 400, "tls.upload_missing"},
		{"kein PEM", []byte("kaputt"), keyPEM, 422, "tls.invalid_certificate"},
		{"falscher Schlüssel", certPEM, otherKey, 422, "tls.invalid_certificate"},
		{"vertauscht", keyPEM, certPEM, 422, "tls.invalid_certificate"},
		{"zu groß", bytes.Repeat([]byte("A"), 64<<10+1), keyPEM, 422, "tls.file_too_large"},
	}
	for _, tc := range cases {
		resp := postUpload(t, app, admin, tc.cert, tc.key)
		if resp.StatusCode != tc.status {
			t.Errorf("%s: %d, erwartet %d", tc.name, resp.StatusCode, tc.status)
			continue
		}
		assertErrorCode(t, resp, tc.code)
	}
	if ids := uploadedIDs(t, f); len(ids) != 0 {
		t.Errorf("abgelehnte Uploads liegen auf der Platte: %v", ids)
	}
}

func TestTLSInvalidCertificateExplainsTheProblem(t *testing.T) {
	app, _ := tlsApp(t, &tlsFake{dataDir: t.TempDir()})
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	certPEM, _ := pemFiles(t)
	_, otherKey := pemFiles(t)

	resp := postUpload(t, app, admin, certPEM, otherKey)
	var out struct {
		Params struct{ Detail string } `json:"params"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if want := "zertifikat laden: der Schlüssel gehört nicht zu diesem Zertifikat"; out.Params.Detail != want {
		t.Errorf("detail = %q, erwartet %q", out.Params.Detail, want)
	}
}

func TestTLSRemoveOnlyUploads(t *testing.T) {
	f := &tlsFake{dataDir: t.TempDir(), config: []string{"config-0"}}
	app, _ := tlsApp(t, f)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	certPEM, keyPEM := pemFiles(t)
	var created struct{ ID string }
	if err := json.NewDecoder(postUpload(t, app, admin, certPEM, keyPEM).Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		id, code string
		status   int
	}{
		{"config-0", "tls.not_removable", 409},
		{"upload-00000000", "tls.not_found", 404},
		{"upload-..%2Fx", "tls.not_found", 404},
		{"unbekannt", "tls.not_found", 404},
	} {
		resp := doRequest(t, app, "DELETE", "/api/v1/system/tls/certificates/"+tc.id, admin, "")
		if resp.StatusCode != tc.status {
			t.Errorf("%s: %d, erwartet %d", tc.id, resp.StatusCode, tc.status)
			continue
		}
		assertErrorCode(t, resp, tc.code)
	}
	if ids := uploadedIDs(t, f); len(ids) != 1 {
		t.Fatalf("ein abgewiesenes Entfernen hat Uploads berührt: %v", ids)
	}
	if resp := doRequest(t, app, "DELETE", "/api/v1/system/tls/certificates/"+created.ID, admin, ""); resp.StatusCode != 200 {
		t.Fatalf("Entfernen: %d", resp.StatusCode)
	}
	if ids := uploadedIDs(t, f); len(ids) != 0 {
		t.Errorf("Upload ist noch da: %v", ids)
	}
}

func TestTLSReloadCallsManager(t *testing.T) {
	f := &tlsFake{dataDir: t.TempDir()}
	app, _ := tlsApp(t, f)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	if resp := doRequest(t, app, "POST", "/api/v1/system/tls/reload", admin, ""); resp.StatusCode != 200 {
		t.Fatalf("Reload: %d", resp.StatusCode)
	}
	if f.reloads != 1 || f.actors[0] != "admin" {
		t.Errorf("reloads=%d actors=%v", f.reloads, f.actors)
	}
}

func TestTLSDownloadReturnsOnlyTheChain(t *testing.T) {
	f := &tlsFake{dataDir: t.TempDir()}
	app, _ := tlsApp(t, f)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	certPEM, keyPEM := pemFiles(t)
	var created struct{ ID string }
	if err := json.NewDecoder(postUpload(t, app, admin, certPEM, keyPEM).Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	resp := doRequest(t, app, "GET", "/api/v1/system/tls/certificates/"+created.ID+"/download", admin, "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("Download: %d %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-pem-file" {
		t.Errorf("Content-Type %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="lcm-`+created.ID+`-chain.pem"` {
		t.Errorf("Content-Disposition %q", cd)
	}
	if !bytes.Contains(body, []byte("BEGIN CERTIFICATE")) || bytes.Contains(body, []byte("PRIVATE KEY")) {
		t.Errorf("die Antwort ist keine reine Zertifikatskette: %s", body)
	}
	if bytes.Contains(body, keyPEM) {
		t.Error("der private Schlüssel steckt in der Antwort")
	}
	if len(f.actors) < 2 || f.actors[len(f.actors)-1] != "admin" {
		t.Errorf("Actor = %v, erwartet den Benutzernamen (für das Audit-Log)", f.actors)
	}
}

func TestTLSDownloadUnknownAndUnsafeIDs(t *testing.T) {
	app, _ := tlsApp(t, &tlsFake{dataDir: t.TempDir()})
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	for _, id := range []string{"upload-00000000", "config-0", "unbekannt", "..%2Fx"} {
		resp := doRequest(t, app, "GET", "/api/v1/system/tls/certificates/"+id+"/download", admin, "")
		if resp.StatusCode != 404 {
			t.Errorf("%s: %d, erwartet 404", id, resp.StatusCode)
			continue
		}
		assertErrorCode(t, resp, "tls.not_found")
	}
}

// system:endpoints gehört allein admin: Ein Manager mit settings:manage kommt
// nicht durch, und es ändert sich nichts.
func TestTLSNeedsSystemEndpointsPermission(t *testing.T) {
	f := &tlsFake{dataDir: t.TempDir()}
	app, db := tlsApp(t, f)
	operator := operatorToken(t, app, db)
	certPEM, keyPEM := pemFiles(t)
	body, headers := uploadForm(t, certPEM, keyPEM)

	for _, call := range []struct{ method, path, body string }{
		{"GET", "/api/v1/system/tls", ""},
		{"POST", "/api/v1/system/tls/certificates", body},
		{"DELETE", "/api/v1/system/tls/certificates/upload-00000000", ""},
		{"POST", "/api/v1/system/tls/reload", ""},
		{"GET", "/api/v1/system/tls/certificates/upload-00000000/download", ""},
	} {
		h := map[string]string{}
		if call.body != "" {
			h = headers
		}
		if r := doRequestWithHeaders(t, app, call.method, call.path, operator, call.body, h); r.StatusCode != 403 {
			t.Errorf("%s %s als Betreiber: %d, erwartet 403", call.method, call.path, r.StatusCode)
		}
	}
	if ids := uploadedIDs(t, f); len(ids) != 0 || f.reloads != 0 {
		t.Errorf("ein abgewiesener Aufruf hat etwas bewirkt: %v, %d", ids, f.reloads)
	}
}

// Ohne TLS (--dev) gibt es keinen Pool: Die Routen melden das klar.
func TestTLSUnavailableWithoutPool(t *testing.T) {
	app, _ := tlsApp(t, unavailableTLS{})
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	resp := doRequest(t, app, "GET", "/api/v1/system/tls", admin, "")
	if resp.StatusCode != 409 {
		t.Fatalf("Status %d, erwartet 409", resp.StatusCode)
	}
	assertErrorCode(t, resp, "tls.unavailable")
}

type unavailableTLS struct{}

func (unavailableTLS) Status() (any, error) { return nil, controllers.ErrTLSUnavailable }
func (unavailableTLS) AddUpload([]byte, []byte, string) (any, error) {
	return nil, controllers.ErrTLSUnavailable
}
func (unavailableTLS) RemoveUpload(string, string) (any, error) {
	return nil, controllers.ErrTLSUnavailable
}
func (unavailableTLS) Reload(string) (any, error) { return nil, controllers.ErrTLSUnavailable }
func (unavailableTLS) CertificateChain(string, string) ([]byte, error) {
	return nil, controllers.ErrTLSUnavailable
}

// operatorToken legt einen Benutzer mit settings:manage an (ohne
// system:endpoints) und meldet ihn an.
func operatorToken(t *testing.T, app *fiber.App, db *gorm.DB) string {
	t.Helper()
	roles := repositories.NewRoleRepository(db)
	role := &domain.Role{Name: "betreiber", Description: "Einstellungen ohne Endpunkte"}
	if err := roles.Create(role); err != nil {
		t.Fatal(err)
	}
	perm, err := roles.EnsurePermission(domain.PermSettingsManage, "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.SetPermissions(role, []domain.Permission{*perm}); err != nil {
		t.Fatal(err)
	}
	users := services.NewUserService(repositories.NewUserRepository(db), roles)
	if _, err := users.CreateUser("betreiber", "", "Regen9-Amsel!Turmfalk", "", "", []string{"betreiber"}, "test"); err != nil {
		t.Fatal(err)
	}
	return loginToken(t, app, "betreiber", "Regen9-Amsel!Turmfalk")
}
