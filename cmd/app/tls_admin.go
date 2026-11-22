package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"LCM/internal/api/controllers"
	"LCM/internal/infrastructure/tlsx"
)

// AddUpload prüft ein hochgeladenes Zertifikat samt Schlüssel, legt es ab und
// lädt den Pool neu. Es liefert die ID des Zertifikats im Pool. Ein Upload,
// den der Pool danach doch nicht annimmt, bleibt nicht liegen.
func (r *certReloader) AddUpload(certPEM, keyPEM []byte, actor string) (string, error) {
	id, err := tlsx.SaveUpload(r.dataDir, certPEM, keyPEM, time.Now())
	if err != nil {
		return "", err
	}
	poolID := "upload-" + id
	loaded := r.Reload("").result(poolID)
	if loaded == nil || loaded.Cert == nil {
		_ = tlsx.RemoveUpload(r.dataDir, id)
		r.Reload("")
		return "", fmt.Errorf("das hochgeladene zertifikat ließ sich nicht laden: %s", loaded.errorText())
	}
	r.audit(actor, auditTLSUpload, fmt.Sprintf("id=%s namen=%s gültig bis %s",
		poolID, strings.Join(loaded.Cert.Names, ","), loaded.Cert.NotAfter.UTC().Format(time.DateOnly)))
	return poolID, nil
}

// RemoveUpload löscht ein hochgeladenes Zertifikat (ID im Pool) und lädt den
// Pool neu. Zertifikate aus der config.json und von ACME bleiben unberührt.
func (r *certReloader) RemoveUpload(poolID, actor string) error {
	id, isUpload := tlsx.UploadIDOf(poolID)
	if !isUpload {
		if r.hasCertificate(poolID) {
			return tlsx.ErrNotUpload
		}
		return tlsx.ErrUploadNotFound
	}
	if err := tlsx.RemoveUpload(r.dataDir, id); err != nil {
		return err
	}
	r.Reload("")
	r.audit(actor, auditTLSRemove, "id="+poolID)
	return nil
}

// CertificateChain liefert die Zertifikatskette (PEM) eines Zertifikats im
// Pool, gleich aus welcher Quelle: aus der config.json, hochgeladen oder von
// ACME. Es kommen nur die Zertifikate heraus, nie ein Schlüssel (auch wenn die
// Datei einen enthält). Der Abruf steht im Audit-Log.
func (r *certReloader) CertificateChain(poolID, actor string) ([]byte, error) {
	r.mu.Lock()
	specs := slices.Concat(r.configSpecs, r.uploadSpecs, r.acmeSpecs)
	r.mu.Unlock()
	i := slices.IndexFunc(specs, func(spec tlsx.PoolSpec) bool { return spec.ID == poolID })
	if i < 0 {
		return nil, tlsx.ErrUploadNotFound
	}
	data, err := os.ReadFile(specs[i].CertPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, tlsx.ErrUploadNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("zertifikat lesen: %w", err)
	}
	chain, err := tlsx.CertificateChainPEM(data)
	if err != nil {
		return nil, err
	}
	r.audit(actor, auditTLSDownload, "id="+poolID)
	return chain, nil
}

func (r *certReloader) hasCertificate(id string) bool {
	return slices.ContainsFunc(r.pool.Status(), func(c tlsx.CertStatus) bool { return c.ID == id })
}

// result ist der Ausgang für das Zertifikat mit dieser ID, nil ohne einen.
func (r reloadReport) result(id string) *certResult {
	for i := range r.Certificates {
		if r.Certificates[i].ID == id {
			return &r.Certificates[i]
		}
	}
	return nil
}

func (c *certResult) errorText() string {
	if c == nil || c.Error == "" {
		return "unbekannter grund"
	}
	return c.Error
}

// tlsAPI gibt der REST-API Zugriff auf den Pool. Der Router entsteht vor dem
// Pool (und im Entwicklungsmodus gibt es ihn gar nicht), deshalb holt sich
// der Zugriff den Reloader erst beim Aufruf.
type tlsAPI struct{ reloader func() *certReloader }

var _ controllers.TLSManager = tlsAPI{}

func (a tlsAPI) current() (*certReloader, error) {
	if r := a.reloader(); r != nil {
		return r, nil
	}
	return nil, controllers.ErrTLSUnavailable
}

// Status: Zertifikate, Endpunkte und gemeinsamer Port wie bei `lcm tls status`.
func (a tlsAPI) Status() (any, error) {
	r, err := a.current()
	if err != nil {
		return nil, err
	}
	return r.Status(), nil
}

// AddUpload antwortet mit der ID des neuen Zertifikats und dem neuen Stand.
func (a tlsAPI) AddUpload(certPEM, keyPEM []byte, actor string) (any, error) {
	r, err := a.current()
	if err != nil {
		return nil, err
	}
	id, err := r.AddUpload(certPEM, keyPEM, actor)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "status": r.Status()}, nil
}

// RemoveUpload antwortet mit dem neuen Stand.
func (a tlsAPI) RemoveUpload(id, actor string) (any, error) {
	r, err := a.current()
	if err != nil {
		return nil, err
	}
	if err := r.RemoveUpload(id, actor); err != nil {
		return nil, err
	}
	return map[string]any{"status": r.Status()}, nil
}

// CertificateChain liefert die Kette zum Download.
func (a tlsAPI) CertificateChain(id, actor string) ([]byte, error) {
	r, err := a.current()
	if err != nil {
		return nil, err
	}
	return r.CertificateChain(id, actor)
}

// Reload antwortet mit dem Bericht des Neuladens und dem neuen Stand.
func (a tlsAPI) Reload(actor string) (any, error) {
	r, err := a.current()
	if err != nil {
		return nil, err
	}
	report := r.Reload(actor)
	return map[string]any{"report": report, "status": r.Status()}, nil
}
