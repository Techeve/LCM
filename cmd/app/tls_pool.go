package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"LCM/internal/config"
	"LCM/internal/infrastructure/tlsx"
)

// configCertificateSpecs: die Einträge aus tls_certificates, mit der
// Position als Kennung.
func configCertificateSpecs(files []config.TLSCertificateFile) []tlsx.PoolSpec {
	specs := make([]tlsx.PoolSpec, 0, len(files))
	for i, f := range files {
		specs = append(specs, tlsx.PoolSpec{
			ID:       fmt.Sprintf("config-%d", i),
			Source:   tlsx.SourceConfig,
			CertPath: f.Cert,
			KeyPath:  f.Key,
		})
	}
	return specs
}

func logPoolReport(report tlsx.LoadReport) {
	for _, r := range report {
		if r.Err != nil {
			slog.Warn("tls certificate not loaded", "id", r.ID, "kept_previous", r.Kept, "error", r.Err)
		}
		if r.Cert == nil {
			continue
		}
		slog.Info("tls certificate in use", "id", r.ID, "source", r.Cert.Source,
			"names", strings.Join(r.Cert.Names, ","), "not_after", r.Cert.NotAfter.Format(time.DateOnly))
		for _, w := range r.Cert.Warnings {
			slog.Warn("tls certificate", "id", r.ID, "warning", w)
		}
	}
}
