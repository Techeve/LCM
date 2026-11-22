package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/api/controllers"
	"LCM/internal/infrastructure/tlsx"
)

// uploadPEM liest Zertifikat und Schlüssel eines Testzertifikats.
func uploadPEM(t *testing.T, c testCert) (certPEM, keyPEM []byte) {
	t.Helper()
	certPEM, err := os.ReadFile(c.cert)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err = os.ReadFile(c.key)
	if err != nil {
		t.Fatal(err)
	}
	return certPEM, keyPEM
}

func uploadDirs(t *testing.T, f *reloadFixture) []string {
	t.Helper()
	dirs, _ := filepath.Glob(filepath.Join(f.dataDir, "tls", "uploads", "*"))
	return dirs
}

func TestAddUploadLiefertDasZertifikatAusUndProtokolliert(t *testing.T) {
	f := newReloadFixture(t)
	*f.addresses = publicAddresses{UI: "https://lcm.example:9310", Agent: "https://agent.other:9320"}
	up := newTestCert(t, time.Now().Add(30*24*time.Hour))
	certPEM, keyPEM := uploadPEM(t, up)

	poolID, err := f.reloader.AddUpload(certPEM, keyPEM, "tony")
	if err != nil {
		t.Fatal(err)
	}
	if serialOf(f.served("lcm.example")) != up.serial {
		t.Error("der Upload wird nicht ausgeliefert")
	}
	if len(*f.audit) != 1 || (*f.audit)[0].actor != "tony" || (*f.audit)[0].action != auditTLSUpload {
		t.Errorf("Audit = %+v, erwartet einen Eintrag %s", *f.audit, auditTLSUpload)
	}
	status := f.reloader.Status()
	if ui := status.Endpoints[0]; ui.Cert == nil || ui.Cert.ID != poolID || ui.Cert.Source != tlsx.SourceUpload {
		t.Errorf("UI = %+v", ui)
	}
	// Der Agent-Name steht nicht im Zertifikat: Die UI muss das aus dem Status erkennen können.
	if agent := status.Endpoints[1]; agent.Cert == nil || agent.Cert.Source != tlsx.SourceLCMCA {
		t.Errorf("Agent = %+v, erwartet das LCM-Zertifikat", agent)
	}
}

func TestAddUploadLegtNichtsAbBeiUngueltigemZertifikat(t *testing.T) {
	f := newReloadFixture(t)
	expired := newTestCert(t, time.Now().Add(-time.Hour))
	certPEM, keyPEM := uploadPEM(t, expired)

	_, err := f.reloader.AddUpload(certPEM, keyPEM, "tony")
	var invalid *tlsx.InvalidCertificateError
	if !errors.As(err, &invalid) {
		t.Fatalf("Fehler = %v, erwartet InvalidCertificateError", err)
	}
	if dirs := uploadDirs(t, f); len(dirs) != 0 || len(*f.audit) != 0 {
		t.Errorf("Uploads %v, Audit %+v", dirs, *f.audit)
	}
}

func TestRemoveUploadNurFuerUploads(t *testing.T) {
	config := newTestCert(t, time.Now().Add(30*24*time.Hour))
	f := newReloadFixture(t, config)
	f.reloader.Reload("")
	up := newTestCert(t, time.Now().Add(60*24*time.Hour))
	certPEM, keyPEM := uploadPEM(t, up)
	poolID, err := f.reloader.AddUpload(certPEM, keyPEM, "tony")
	if err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]error{
		"config-0":        tlsx.ErrNotUpload,
		"lcm-ca":          tlsx.ErrNotUpload,
		"nicht-vorhanden": tlsx.ErrUploadNotFound,
		"upload-00000000": tlsx.ErrUploadNotFound,
		"upload-../..":    tlsx.ErrUploadID,
	} {
		if err := f.reloader.RemoveUpload(id, "tony"); !errors.Is(err, want) {
			t.Errorf("%s: Fehler = %v, erwartet %v", id, err, want)
		}
	}
	if len(uploadDirs(t, f)) != 1 {
		t.Fatal("ein abgewiesenes Entfernen hat den Upload berührt")
	}

	if err := f.reloader.RemoveUpload(poolID, "tony"); err != nil {
		t.Fatal(err)
	}
	if len(uploadDirs(t, f)) != 0 {
		t.Error("der Upload liegt noch auf der Platte")
	}
	if serialOf(f.served("lcm.example")) != config.serial {
		t.Error("nach dem Entfernen gilt nicht mehr das Zertifikat der config.json")
	}
	last := (*f.audit)[len(*f.audit)-1]
	if last.action != auditTLSRemove || last.actor != "tony" {
		t.Errorf("Audit = %+v", last)
	}
}

func TestTLSAPIOhnePoolMeldetNichtVerfuegbar(t *testing.T) {
	api := tlsAPI{reloader: func() *certReloader { return nil }}
	if _, err := api.Status(); !errors.Is(err, controllers.ErrTLSUnavailable) {
		t.Errorf("Fehler = %v", err)
	}
}

// Die Kette zum Download enthält nur Zertifikate: auch dann kein Schlüssel,
// wenn die Zertifikatsdatei einen enthält (config.json darf eine Datei mit
// beidem nennen). Der Abruf steht im Audit-Log; eine fremde ID ist unbekannt.
func TestCertificateChainLiefertNurZertifikateUndProtokolliert(t *testing.T) {
	cfg := newTestCert(t, time.Now().Add(30*24*time.Hour))
	certPEM, keyPEM := uploadPEM(t, cfg)
	// Zertifikat und Schlüssel in einer Datei.
	combined := filepath.Join(t.TempDir(), "beides.pem")
	if err := os.WriteFile(combined, append(append([]byte{}, certPEM...), keyPEM...), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.cert = combined
	f := newReloadFixture(t, cfg)
	f.reloader.Reload("")
	up := newTestCert(t, time.Now().Add(60*24*time.Hour))
	upCert, upKey := uploadPEM(t, up)
	uploadID, err := f.reloader.AddUpload(upCert, upKey, "tony")
	if err != nil {
		t.Fatal(err)
	}
	*f.audit = nil

	for _, id := range []string{"config-0", uploadID} {
		chain, err := f.reloader.CertificateChain(id, "tony")
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !strings.Contains(string(chain), "BEGIN CERTIFICATE") || strings.Contains(string(chain), "PRIVATE KEY") {
			t.Errorf("%s: Kette %q", id, chain)
		}
	}
	if len(*f.audit) != 2 || (*f.audit)[0] != (auditEntry{"tony", auditTLSDownload, "id=config-0"}) {
		t.Errorf("Audit = %+v", *f.audit)
	}

	*f.audit = nil
	for _, id := range []string{"unbekannt", "upload-00000000", "lcm-ca", "../x"} {
		if _, err := f.reloader.CertificateChain(id, "tony"); !errors.Is(err, tlsx.ErrUploadNotFound) {
			t.Errorf("%s: Fehler %v, erwartet ErrUploadNotFound", id, err)
		}
	}
	if len(*f.audit) != 0 {
		t.Errorf("ein erfolgloser Abruf steht im Audit-Log: %+v", *f.audit)
	}
}
