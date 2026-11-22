package main

import (
	"path/filepath"
	"testing"
	"time"

	"LCM/internal/config"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
)

// TestKaputtesConfigZertifikatLaesstDenStartZu: Der Pool liefert dann weiter
// das Zertifikat der LCM-CA, und die Einträge tragen config-<index> als ID.
func TestKaputtesConfigZertifikatLaesstDenStartZu(t *testing.T) {
	dataDir := t.TempDir()
	tlsxtest.ServerCert(t, dataDir, time.Now().Add(-time.Hour))
	store, err := tlsx.NewCertStore(filepath.Join(dataDir, tlsx.CertFileName), filepath.Join(dataDir, tlsx.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{TLSCertificates: []config.TLSCertificateFile{{Cert: "/gibt/es/nicht.pem", Key: "/gibt/es/nicht.key"}}}

	specs := configCertificateSpecs(cfg.TLSCertificates)
	if len(specs) != 1 || specs[0].ID != "config-0" || specs[0].Source != tlsx.SourceConfig {
		t.Fatalf("specs = %+v", specs)
	}
	pool := tlsx.NewPool(store)
	pool.Load(specs)

	status := pool.Status()
	if len(status) != 1 || status[0].Source != tlsx.SourceLCMCA {
		t.Errorf("Status = %+v, erwartet nur den Rückfall", status)
	}
}
