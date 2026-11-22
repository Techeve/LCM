package controllers

import (
	"errors"
	"io"
	"regexp"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
	"LCM/internal/infrastructure/tlsx"
)

// Zertifikate der Endpunkte.
var (
	errTLSUploadMissing = apierr.Define(400, "tls.upload_missing", "certificate and key are required as files in the fields certificate and key")
	errTLSFileTooLarge  = apierr.Define(422, "tls.file_too_large", "the certificate and key files must not be larger than 64 KiB each")
	errTLSInvalid       = apierr.Define(422, "tls.invalid_certificate", "the certificate or key cannot be used")
	errTLSNotFound      = apierr.Define(404, "tls.not_found", "certificate not found")
	errTLSNotRemovable  = apierr.Define(409, "tls.not_removable", "only uploaded certificates can be removed here")
	errTLSUnavailable   = apierr.Define(409, "tls.unavailable", "LCM runs without TLS (development mode), there are no certificates")
)

// maxTLSFile begrenzt Zertifikat- und Schlüsseldatei: Eine Kette mit drei
// Zertifikaten und ein RSA-Schlüssel bleiben weit darunter.
const maxTLSFile = 64 << 10

// ErrTLSUnavailable: LCM läuft ohne TLS (--dev), es gibt keinen Pool.
var ErrTLSUnavailable = errors.New("tls nicht verfügbar")

// TLSManager ist der Teil des laufenden Dienstes, dem der Zertifikatspool
// gehört. Die Antworten sind fertig für JSON.
type TLSManager interface {
	// Status: die Zertifikate im Pool und was jeder Endpunkt ausliefert.
	Status() (any, error)
	// AddUpload prüft und speichert ein Zertifikat samt Schlüssel und lädt den Pool neu.
	AddUpload(certPEM, keyPEM []byte, actor string) (any, error)
	// RemoveUpload entfernt ein hochgeladenes Zertifikat (ID im Pool) und lädt neu.
	RemoveUpload(id, actor string) (any, error)
	// Reload liest config.json, Uploads und ACME-Zertifikate neu ein.
	Reload(actor string) (any, error)
	// CertificateChain liefert die Zertifikatskette (PEM) des Zertifikats mit
	// dieser ID im Pool, nie den Schlüssel, und protokolliert den Abruf.
	CertificateChain(id, actor string) ([]byte, error)
}

// TLSController bedient die Zertifikate der Endpunkte. Das Recht dazu
// (system:endpoints) hat allein admin: Ein Zertifikat bestimmt, wem Browser
// und Agenten LCM abnehmen.
type TLSController struct {
	tls TLSManager
}

func NewTLSController(tls TLSManager) *TLSController {
	return &TLSController{tls: tls}
}

// Status - GET /api/v1/system/tls (system:endpoints)
func (ctrl *TLSController) Status(c fiber.Ctx) error {
	status, err := ctrl.tls.Status()
	if err != nil {
		return mapTLSError(err)
	}
	return c.JSON(status)
}

// Upload - POST /api/v1/system/tls/certificates (system:endpoints)
// Multipart mit den Dateien certificate (PEM, mit Kette) und key (PEM,
// unverschlüsselt). Was die Prüfung nicht besteht, wird nicht gespeichert.
func (ctrl *TLSController) Upload(c fiber.Ctx) error {
	certPEM, err := formPEM(c, "certificate")
	if err != nil {
		return err
	}
	keyPEM, err := formPEM(c, "key")
	if err != nil {
		return err
	}
	result, err := ctrl.tls.AddUpload(certPEM, keyPEM, actor(c))
	if err != nil {
		return mapTLSError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(result)
}

// Remove - DELETE /api/v1/system/tls/certificates/:id (system:endpoints)
// Entfernt ein hochgeladenes Zertifikat (ID wie im Status, upload-<id>).
func (ctrl *TLSController) Remove(c fiber.Ctx) error {
	result, err := ctrl.tls.RemoveUpload(c.Params("id"), actor(c))
	if err != nil {
		return mapTLSError(err)
	}
	return c.JSON(result)
}

// Download - GET /api/v1/system/tls/certificates/:id/download (system:endpoints)
// Die Zertifikatskette als PEM-Datei (ID wie im Status: upload-<id>,
// acme-<id>, config-<n>). Nur Zertifikate, nie der private Schlüssel.
func (ctrl *TLSController) Download(c fiber.Ctx) error {
	id := c.Params("id")
	chain, err := ctrl.tls.CertificateChain(id, actor(c))
	if err != nil {
		return mapTLSError(err)
	}
	c.Set(fiber.HeaderContentType, "application/x-pem-file")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="`+chainFileName(id)+`"`)
	return c.Send(chain)
}

// unsafeFileChars sind alle Zeichen, die in einem Dateinamen im Header stören.
var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// chainFileName macht aus der ID im Pool einen sicheren Dateinamen.
func chainFileName(id string) string {
	return "lcm-" + unsafeFileChars.ReplaceAllString(id, "_") + "-chain.pem"
}

// Reload - POST /api/v1/system/tls/reload (system:endpoints)
// Wie `lcm tls reload`: liest die Zertifikate neu ein, ohne Neustart.
func (ctrl *TLSController) Reload(c fiber.Ctx) error {
	result, err := ctrl.tls.Reload(actor(c))
	if err != nil {
		return mapTLSError(err)
	}
	return c.JSON(result)
}

// formPEM liest eine hochgeladene Datei des Formulars.
func formPEM(c fiber.Ctx, field string) ([]byte, error) {
	fh, err := c.FormFile(field)
	if err != nil {
		return nil, errTLSUploadMissing
	}
	if fh.Size > maxTLSFile {
		return nil, errTLSFileTooLarge.With("field", field)
	}
	f, err := fh.Open()
	if err != nil {
		return nil, errTLSUploadMissing
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTLSFile+1))
	if err != nil || len(data) == 0 {
		return nil, errTLSUploadMissing
	}
	if len(data) > maxTLSFile {
		return nil, errTLSFileTooLarge.With("field", field)
	}
	return data, nil
}

func mapTLSError(err error) error {
	var invalid *tlsx.InvalidCertificateError
	switch {
	case errors.As(err, &invalid):
		return errTLSInvalid.With("detail", invalid.Error())
	case errors.Is(err, tlsx.ErrUploadNotFound), errors.Is(err, tlsx.ErrUploadID), errors.Is(err, tlsx.ErrNoCertificate):
		return errTLSNotFound
	case errors.Is(err, tlsx.ErrNotUpload):
		return errTLSNotRemovable
	case errors.Is(err, ErrTLSUnavailable):
		return errTLSUnavailable
	}
	return err
}
