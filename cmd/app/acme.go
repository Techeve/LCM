package main

import (
	"LCM/internal/acmehttp"
	"LCM/internal/config"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/acmex"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// newACMEService baut den ACME-Dienst samt Listener für HTTP-01. Der Listener
// ist nur während einer Prüfung offen und beantwortet dort allein das Token
// der CA (siehe acmehttp). Die Namen im Zertifikat kommen aus den öffentlichen
// Adressen (settings.ACMENames). reload lädt neu ausgestellte Zertifikate in
// den Pool.
func newACMEService(cfg *config.Config, dataDir string, settings *repositories.SettingsRepository,
	audit *services.AuditService, settingsService *services.SettingsService,
	cipher *crypto.Cipher, reload func()) *services.ACMEService {
	tokens := acmex.NewChallengeStore()
	listener := acmehttp.New(tokens)
	return services.NewACMEService(settings, audit, acmex.NewManager(dataDir), tokens, listener, cfg.ACMEHTTPListen, reload,
		cipher, acmex.DNSEnv{DataDir: dataDir, ExecPath: cfg.ACMEDNSExec}).
		WithNames(settingsService.ACMENames)
}
