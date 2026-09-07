package services

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/notify"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// SettingsService verwaltet die globalen Systemeinstellungen. Sensible
// Werte (Default-SSH-Passwort, TLS-Key) werden AES-GCM-verschlüsselt.
type SettingsService struct {
	settings     *repositories.SettingsRepository
	cipher       *crypto.Cipher
	audit        *AuditService
	reload       func() error
	knownRepos   *repositories.KnownRepoRepository   // Katalog bekannter Paketquellen, optional
	ipAllowlists *repositories.IPAllowlistRepository // benannte IP-Allowlists, optional
	// ipAllowlistUsage liefert alle Verweise auf eine Allowlist (Server-
	// Firewall-Regeln, Gruppen-Regeln) - Löschsperre R2-072. Optional.
	ipAllowlistUsage func(id uint) []string
	updateCache      updateStatusCache // Update-Prüfung: nur im Speicher, siehe update_check.go
	// mcpApply startet/stoppt den MCP-Listener zur Laufzeit, wenn sich die
	// MCP-Einstellungen ändern (enabled/host/port). Optional.
	mcpApply func(enabled bool, host string, port int)
	// defaultBackupDir ist das effektive Standard-Backup-Verzeichnis
	// (siehe WithDefaultBackupDir) - Rückfall, wenn das Formularfeld
	// geleert wird.
	defaultBackupDir string
	// fallbackBaseURL ist die aus der lokalen Konfiguration abgeleitete
	// Basis-Adresse für Mail-Links, wenn keine PublicBaseURL gesetzt ist.
	fallbackBaseURL string
	// roles dient der Validierung von require_2fa_roles gegen die real
	// existierenden Rollen. Optional (nil = keine Prüfung).
	roles *repositories.RoleRepository
}

// WithRoles verdrahtet den Rollen-Katalog für die Validierung der
// 2FA-Pflicht-Rollenliste.
func (s *SettingsService) WithRoles(repo *repositories.RoleRepository) *SettingsService {
	s.roles = repo
	return s
}

// WithMCPToggle verdrahtet die Laufzeit-Steuerung des MCP-Listeners: bei
// jeder Änderung der MCP-Einstellungen wird sie mit dem neuen Zustand
// aufgerufen (starten/stoppen/neu binden).
func (s *SettingsService) WithMCPToggle(apply func(enabled bool, host string, port int)) *SettingsService {
	s.mcpApply = apply
	return s
}

// SetMCP schaltet die MCP-Schnittstelle ein/aus und setzt Bind-Adresse/Port.
// Der Listener wird direkt zur Laufzeit entsprechend gestartet/gestoppt.
func (s *SettingsService) SetMCP(enabled bool, host string, port int, actor string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	// Die MCP-Schnittstelle spricht unverschlüsseltes HTTP und trägt den
	// API-Key als Bearer-Token. Auf einer nicht-lokalen Bind-Adresse liefe er
	// im Klartext über das Netz - zusammen mit der vollständigen
	// Flotteninventur (welcher Server ist ungepatcht, hat kritische CVEs,
	// keine Firewall). Deshalb sind hier ausschließlich Loopback-Adressen
	// zulässig; für Fernzugriff gehört ein TLS-Reverse-Proxy/Tunnel davor.
	if !isLoopbackHost(host) {
		return fmt.Errorf("%w: die MCP-Bind-Adresse muss eine lokale Adresse sein (127.0.0.1, ::1 oder localhost) - für Fernzugriff einen TLS-Reverse-Proxy davorsetzen, da MCP unverschlüsseltes HTTP spricht", ErrSettingInvalid)
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("ungültiger MCP-Port: %d", port)
	}
	if err := s.settings.UpdateFields(map[string]any{
		"mcp_enabled": enabled, "mcp_bind_host": host, "mcp_port": port,
	}); err != nil {
		return err
	}
	s.audit.Log(actor, "settings.mcp", "settings", 1, fmt.Sprintf("enabled=%v %s:%d", enabled, host, port))
	if s.mcpApply != nil {
		s.mcpApply(enabled, host, port)
	}
	return nil
}

func NewSettingsService(settings *repositories.SettingsRepository, cipher *crypto.Cipher, audit *AuditService, reload func() error) *SettingsService {
	return &SettingsService{settings: settings, cipher: cipher, audit: audit, reload: reload}
}

// WithKnownRepos verdrahtet den pflegbaren Katalog bekannter Paketquellen.
func (s *SettingsService) WithKnownRepos(repo *repositories.KnownRepoRepository) *SettingsService {
	s.knownRepos = repo
	return s
}

// WithIPAllowlistUsage verdrahtet die Verwendungsprüfung für die
// Allowlist-Löschsperre (R2-072).
func (s *SettingsService) WithIPAllowlistUsage(fn func(id uint) []string) *SettingsService {
	s.ipAllowlistUsage = fn
	return s
}

// WithDefaultBackupDir hinterlegt das effektive Standard-Backup-Verzeichnis
// (config.json-Vorgabe oder <data>/backups) und belegt ein leeres
// backup_dir in der DB sofort damit vor. So zeigt die UI immer den
// tatsächlich verwendeten Pfad - änderbar, aber nie leer.
func (s *SettingsService) WithDefaultBackupDir(dir string) *SettingsService {
	s.defaultBackupDir = dir
	if dir == "" {
		return s
	}
	if settings, err := s.settings.Get(); err == nil && settings.BackupDir == "" {
		if err := s.settings.UpdateFields(map[string]any{"backup_dir": dir}); err != nil {
			slog.Warn("backup directory could not be initialized", "error", err)
		}
	}
	return s
}

// Get liefert die globalen Einstellungen.
func (s *SettingsService) Get() (*domain.GlobalSettings, error) {
	return s.settings.Get()
}

// SetCVEScanEnabled schaltet den CVE-Scan (Trivy) global ein/aus und lädt den
// Scheduler neu. Wird u. a. genutzt, wenn LCM Trivy auf dem eigenen Host
// installiert und den Scan danach selbsttätig aktiviert.
func (s *SettingsService) SetCVEScanEnabled(enabled bool) error {
	if err := s.settings.UpdateFields(map[string]any{"cve_scan_enabled": enabled}); err != nil {
		return err
	}
	if s.reload != nil {
		return s.reload()
	}
	return nil
}

// DisableSelfRegistration hält fest, dass der LCM-Host nicht mehr automatisch
// in die Verwaltung aufgenommen werden soll. Wird beim Löschen genau dieses
// Servers gesetzt: Das Installationsskript legt die Übergabedatei bei jedem
// Lauf neu an, ohne diesen Vermerk käme der Eintrag beim nächsten
// Paket-Update also zurück.
//
// Rückgängig machen lässt sich das durch erneutes Aufnehmen des Hosts über den
// Join-Wizard - der Vermerk steuert nur die AUTOMATISCHE Aufnahme.
func (s *SettingsService) DisableSelfRegistration() error {
	return s.settings.UpdateFields(map[string]any{"self_server_disabled": true})
}

// SetAptCacheURL setzt die globale APT-Cache-URL (normalisiert/validiert) und
// lädt den Scheduler neu. Wird u. a. genutzt, wenn LCM apt-cacher-ng auf dem
// eigenen Host installiert und den Cache danach selbsttätig einträgt.
func (s *SettingsService) SetAptCacheURL(rawURL string) error {
	url, err := normalizeAptCacheURL(rawURL)
	if err != nil {
		return err
	}
	if err := s.settings.UpdateFields(map[string]any{"apt_cache_url": url}); err != nil {
		return err
	}
	if s.reload != nil {
		return s.reload()
	}
	return nil
}

// SetCrowdSecLapi trägt die (auf dem LCM-Host erzeugten) CrowdSec-LAPI-
// Zugangsdaten in die globalen Einstellungen ein - URL/Login im Klartext,
// das Passwort AES-verschlüsselt. Wird nach der LAPI-Installation auf dem
// LCM-Host aufgerufen, damit verwaltete Server sofort im Remote-Modus
// enrollen können.
func (s *SettingsService) SetCrowdSecLapi(rawURL, login, password string) error {
	enc, err := s.cipher.EncryptString(password)
	if err != nil {
		return err
	}
	settings, err := s.settings.Get()
	if err != nil {
		return err
	}
	settings.CrowdSecLapiURL = strings.TrimSpace(rawURL)
	settings.CrowdSecLapiLogin = strings.TrimSpace(login)
	settings.CrowdSecLapiPasswordEnc = enc
	if err := s.settings.Save(settings); err != nil {
		return err
	}
	if s.reload != nil {
		return s.reload()
	}
	return nil
}

// EnsureOnboardingKey erzeugt beim ersten Start ein Onboarding-Schlüsselpaar
// (falls noch keines existiert): Der Private Key wird AES-GCM-verschlüsselt
// gespeichert, der Public Key im Klartext (zur Anzeige). Idempotent - bei
// jedem weiteren Start passiert nichts.
func (s *SettingsService) EnsureOnboardingKey() error {
	settings, err := s.settings.Get()
	if err != nil {
		return err
	}
	if settings.OnboardingKeyEnc != "" {
		return nil
	}
	privPEM, pubLine, err := sshx.GenerateKeyPair("lcm-onboarding")
	if err != nil {
		return fmt.Errorf("onboarding-key erzeugen: %w", err)
	}
	enc, err := s.cipher.EncryptString(privPEM)
	if err != nil {
		return err
	}
	settings.OnboardingKeyEnc = enc
	settings.OnboardingPubKey = pubLine
	if err := s.settings.Save(settings); err != nil {
		return err
	}
	slog.Info("onboarding ssh key created - public key visible in settings")
	return nil
}

// OnboardingPrivateKey liefert den entschlüsselten Onboarding-Private-Key
// (PEM) für den Key-Login beim Join/Reconnect.
func (s *SettingsService) OnboardingPrivateKey() (string, error) {
	settings, err := s.settings.Get()
	if err != nil {
		return "", err
	}
	if settings.OnboardingKeyEnc == "" {
		return "", ErrNoOnboardingKey
	}
	return s.cipher.DecryptString(settings.OnboardingKeyEnc)
}

// ---- Katalog bekannter Paketquellen -----------------------------------------

// ErrKnownRepoInvalid signalisiert eine fehlgeschlagene Validierung eines
// Katalog-Eintrags; die konkrete Ursache steckt in der Fehlermeldung.
var ErrKnownRepoInvalid = errors.New("ungültiger katalog-eintrag")

// reKnownRepoKey: Slug, der auf dem Zielsystem Dateinamen bildet
// (/etc/apt/keyrings/<key>.asc, lcm-<key>.list) - daher strikt.
var reKnownRepoKey = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// validateRepoLine prüft die Quellen-Zeile passend zur Paketverwaltung:
// apt erwartet eine vollständige "deb …"-Zeile, alle anderen eine http(s)-URL
// (Repository- bzw. .repo-Datei-URL).
func validateRepoLine(mgr, line string) error {
	if line == "" {
		return fmt.Errorf("%w: die quelle darf nicht leer sein", ErrKnownRepoInvalid)
	}
	switch mgr {
	case "apt":
		if !strings.HasPrefix(line, "deb ") {
			return fmt.Errorf("%w: die apt-quelle muss mit 'deb ' beginnen", ErrKnownRepoInvalid)
		}
	default:
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
			return fmt.Errorf("%w: die %s-quelle muss eine http(s)-URL sein", ErrKnownRepoInvalid, mgr)
		}
	}
	return nil
}

// ListKnownRepos liefert den pflegbaren Katalog bekannter Paketquellen.
func (s *SettingsService) ListKnownRepos() ([]domain.KnownRepo, error) {
	return s.knownRepos.List()
}

// SaveKnownRepo legt einen Katalog-Eintrag an (ID 0) oder aktualisiert ihn.
// Key, KeyURL und Line landen später wörtlich in Shell-Skripten auf den
// Zielsystemen - die Validierung ist deshalb streng.
func (s *SettingsService) SaveKnownRepo(in domain.KnownRepo, actor string) (*domain.KnownRepo, error) {
	in.Key = strings.TrimSpace(in.Key)
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.KeyURL = strings.TrimSpace(in.KeyURL)
	in.Line = strings.TrimSpace(in.Line)
	in.PackageManager = strings.TrimSpace(in.PackageManager)
	if in.PackageManager == "" {
		in.PackageManager = "apt"
	}

	if !reKnownRepoKey.MatchString(in.Key) {
		return nil, fmt.Errorf("%w: key muss ein slug sein (a-z, 0-9, bindestrich)", ErrKnownRepoInvalid)
	}
	if in.Name == "" {
		return nil, fmt.Errorf("%w: name fehlt", ErrKnownRepoInvalid)
	}
	if !PackageManagerSupported(in.PackageManager) {
		return nil, fmt.Errorf("%w: unbekannte paketverwaltung %q (erlaubt: apt, dnf, yum, zypper, pacman, apk)", ErrKnownRepoInvalid, in.PackageManager)
	}
	if err := validateRepoLine(in.PackageManager, in.Line); err != nil {
		return nil, err
	}
	if in.KeyURL != "" && !strings.HasPrefix(in.KeyURL, "https://") {
		return nil, fmt.Errorf("%w: die key-url muss mit https:// beginnen", ErrKnownRepoInvalid)
	}
	// Die Werte werden in doppelte Anführungszeichen eingebettet bzw. bilden
	// Dateinamen - Quotes, Backslashes und Steuerzeichen sind tabu.
	for _, field := range []string{in.KeyURL, in.Line} {
		if strings.ContainsAny(field, "\"'`\\\n\r") || strings.Contains(field, "$(") {
			return nil, fmt.Errorf("%w: quotes, backslashes oder subshells sind nicht erlaubt", ErrKnownRepoInvalid)
		}
	}

	// Key-Eindeutigkeit vorab prüfen (klare Meldung statt DB-Constraint-Fehler).
	if existing, err := s.knownRepos.FindByKey(in.Key); err == nil && existing.ID != in.ID {
		return nil, fmt.Errorf("%w: key %q ist bereits vergeben", ErrKnownRepoInvalid, in.Key)
	}

	if in.ID == 0 {
		if err := s.knownRepos.Create(&in); err != nil {
			return nil, err
		}
		s.audit.Log(actor, "settings.known-repo.create", "known_repo", in.ID, in.Key)
		return &in, nil
	}
	existing, err := s.knownRepos.FindByID(in.ID)
	if err != nil {
		return nil, err
	}
	existing.Key = in.Key
	existing.Name = in.Name
	existing.Description = in.Description
	existing.KeyURL = in.KeyURL
	existing.PackageManager = in.PackageManager
	existing.Line = in.Line
	if err := s.knownRepos.Update(existing); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "settings.known-repo.update", "known_repo", existing.ID, existing.Key)
	return existing, nil
}

// DeleteKnownRepo entfernt einen Katalog-Eintrag. Bereits auf Servern
// eingerichtete Quellen bleiben davon unberührt (der Katalog ist nur die
// Vorlage fürs Einrichten).
func (s *SettingsService) DeleteKnownRepo(id uint, actor string) error {
	repo, err := s.knownRepos.FindByID(id)
	if err != nil {
		return err
	}
	if err := s.knownRepos.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "settings.known-repo.delete", "known_repo", id, repo.Key)
	return nil
}

// GlobalSettingsInput sind die über die UI änderbaren Felder.
// GlobalSettingsInput trägt die Felder eines Einstellungs-PATCH. Alle
// Felder sind Zeiger: nil = nicht mitgeschickt = unverändert (R2-029 -
// vorher wirkte der PATCH wie ein PUT und nullte fehlende Felder).
type GlobalSettingsInput struct {
	DefaultSSHUser              *string
	DefaultSSHPassword          *string // nil/leer = unverändert
	DefaultSSHPort              *int
	LogRetentionDays            *int
	StorageHistoryRetentionDays *int
	BackupEnabled               *bool
	BackupIntervalHours         *int
	BackupRetention             *int
	BackupTime                  *string
	BackupDir                   *string
	RestoreAutoRestart          *bool
	CVEScanEnabled              *bool
	CVEScanCron                 *string
	AdvisoryPollingEnabled      *bool
	AdvisoryLocalCopy           *bool
	AdvisoryCacheTTLMinutes     *int
	SessionTTLMinutes           *int
	JobIdleTimeoutMinutes       *int
	JobIdleTimeoutSlowMinutes   *int
	AptCacheURL                 *string
	Require2FARoles             *string
	PublicBaseURL               *string
	CVEHighWeightPackages       *string
	DNSServerPresets            *string
	NTPServerPresets            *string
	DefaultTimezone             *string
	DNSTestDomains              *string

	// Standard-E-Mail-Versand (System-Mailer).
	MailEnabled         *bool
	MailHost            *string
	MailPort            *int
	MailUsername        *string
	MailPassword        *string // nil/leer = unverändert
	MailFrom            *string
	MailUseTLS          *bool
	MailAdminRecipients *string

	// CrowdSec-Zugang (Passwort/Key nil/leer = unverändert).
	CrowdSecLapiURL      *string
	CrowdSecLapiLogin    *string
	CrowdSecLapiPassword *string
	CrowdSecConsoleKey   *string
}

// clampSessionTTL begrenzt die eingestellte Session-Dauer auf sinnvolle Werte:
// 0 bleibt 0 (config-Vorgabe), sonst zwischen 5 Minuten und 30 Tagen.
func clampSessionTTL(minutes int) int {
	if minutes <= 0 {
		return 0
	}
	const min, max = 5, 30 * 24 * 60
	if minutes < min {
		return min
	}
	if minutes > max {
		return max
	}
	return minutes
}

// UpdateGlobal speichert die globalen Einstellungen und lädt den
// Scheduler neu (Backup-Intervall/Retention können sich geändert haben).
//
// ECHTER PATCH: nil-Felder bleiben unangetastet. Früher wirkte der Endpunkt
// wie ein PUT - ein Aufruf mit nur einem Feld schaltete still Backup und
// CVE-Scan ab und verwarf die APT-Cache-URL (R2-029). Der Audit-Eintrag
// nennt jetzt die geänderten Felder (R2-062).
func (s *SettingsService) UpdateGlobal(in GlobalSettingsInput, actor string) (*domain.GlobalSettings, error) {
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	var changed []string
	touch := func(name string) { changed = append(changed, name) }

	if in.DefaultSSHUser != nil {
		settings.DefaultSSHUser = *in.DefaultSSHUser
		touch("default_ssh_user")
	}
	if in.DefaultSSHPassword != nil && *in.DefaultSSHPassword != "" {
		enc, err := s.cipher.EncryptString(*in.DefaultSSHPassword)
		if err != nil {
			return nil, err
		}
		settings.DefaultSSHPasswordEnc = enc
		touch("default_ssh_password")
	}
	if in.DefaultSSHPort != nil && *in.DefaultSSHPort > 0 {
		if err := validatePort(*in.DefaultSSHPort, "default_ssh_port"); err != nil {
			return nil, err
		}
		settings.DefaultSSHPort = *in.DefaultSSHPort
		touch("default_ssh_port")
	}
	if in.LogRetentionDays != nil {
		if err := validateRange(*in.LogRetentionDays, 0, 3650, "log_retention_days"); err != nil {
			return nil, err
		}
		settings.LogRetentionDays = *in.LogRetentionDays
		touch("log_retention_days")
	}
	if in.StorageHistoryRetentionDays != nil {
		settings.StorageHistoryRetentionDays = domain.ClampStorageHistoryRetention(*in.StorageHistoryRetentionDays)
		touch("storage_history_retention_days")
	}
	if in.BackupEnabled != nil {
		settings.BackupEnabled = *in.BackupEnabled
		touch("backup_enabled")
	}
	if in.BackupIntervalHours != nil && *in.BackupIntervalHours > 0 {
		if err := validateRange(*in.BackupIntervalHours, 1, 8760, "backup_interval_hours"); err != nil {
			return nil, err
		}
		settings.BackupIntervalHours = *in.BackupIntervalHours
		touch("backup_interval_hours")
	}
	if in.BackupRetention != nil {
		if err := validateRange(*in.BackupRetention, 0, 3650, "backup_retention"); err != nil {
			return nil, err
		}
		settings.BackupRetention = *in.BackupRetention
		touch("backup_retention")
	}
	if in.BackupTime != nil {
		normalized, err := normalizeBackupTime(*in.BackupTime)
		if err != nil {
			return nil, err
		}
		settings.BackupTime = normalized
		touch("backup_time")
	}
	if in.BackupDir != nil {
		backupDir, err := normalizeBackupDir(*in.BackupDir)
		if err != nil {
			return nil, err
		}
		settings.BackupDir = backupDir
		touch("backup_dir")
	}
	if in.RestoreAutoRestart != nil {
		settings.RestoreAutoRestart = *in.RestoreAutoRestart
		touch("restore_auto_restart")
	}
	if in.CVEScanEnabled != nil {
		settings.CVEScanEnabled = *in.CVEScanEnabled
		touch("cve_scan_enabled")
	}
	if in.CVEScanCron != nil && *in.CVEScanCron != "" {
		// Cron-Ausdruck sofort validieren - ein Tippfehler fiele sonst erst
		// als Log-Zeile beim Scheduler-Reload auf und der CVE-Scan liefe
		// still nie.
		if _, err := cronParser.Parse(*in.CVEScanCron); err != nil {
			return nil, fmt.Errorf("ungültiger CVE-Scan-Zeitplan %q: %w", *in.CVEScanCron, err)
		}
		settings.CVEScanCron = *in.CVEScanCron
		touch("cve_scan_cron")
	}
	if in.AdvisoryPollingEnabled != nil {
		settings.AdvisoryPollingEnabled = *in.AdvisoryPollingEnabled
		touch("advisory_polling_enabled")
	}
	if in.AdvisoryLocalCopy != nil {
		settings.AdvisoryLocalCopy = *in.AdvisoryLocalCopy
		touch("advisory_local_copy")
	}
	if in.AdvisoryCacheTTLMinutes != nil {
		// Begrenzen statt ablehnen: Ein zu hoher Wert ist keine Fehlbedienung,
		// aber er darf die Fruehwarnung nicht laenger blind machen als
		// vorgesehen (siehe AdvisoryCacheTTLMax).
		settings.AdvisoryCacheTTLMinutes = domain.ClampAdvisoryCacheTTL(*in.AdvisoryCacheTTLMinutes)
		touch("advisory_cache_ttl_minutes")
	}
	if in.CVEHighWeightPackages != nil {
		settings.CVEHighWeightPackages = strings.TrimSpace(*in.CVEHighWeightPackages)
		touch("cve_high_weight_packages")
	}
	if in.DNSServerPresets != nil {
		settings.DNSServerPresets = strings.TrimSpace(*in.DNSServerPresets)
		touch("dns_server_presets")
	}
	if in.DNSTestDomains != nil {
		settings.DNSTestDomains = strings.TrimSpace(*in.DNSTestDomains)
		touch("dns_test_domains")
	}
	if in.NTPServerPresets != nil {
		settings.NTPServerPresets = strings.TrimSpace(*in.NTPServerPresets)
		touch("ntp_server_presets")
	}
	if in.DefaultTimezone != nil {
		// Die Vorgabe wird hier geprüft und nicht erst auf dem Zielsystem:
		// eine unsinnige Zone würde sonst in jedem Aktionsformular
		// vorbelegt und erst beim Anwenden auf einem fremden Server auffallen.
		tz := strings.TrimSpace(*in.DefaultTimezone)
		if tz != "" {
			clean, err := validTimezone(tz)
			if err != nil {
				return nil, err
			}
			tz = clean
		}
		settings.DefaultTimezone = tz
		touch("default_timezone")
	}
	if in.SessionTTLMinutes != nil {
		// Session-Dauer: 0 = config-Vorgabe; sonst auf sinnvolle Grenzen
		// begrenzen (min. 5 Minuten, max. 30 Tage).
		settings.SessionTTLMinutes = clampSessionTTL(*in.SessionTTLMinutes)
		touch("session_ttl_minutes")
	}
	if in.JobIdleTimeoutMinutes != nil {
		// Erlaubte Stille (Watchdog): 0 = aus; sonst 1 Minute bis 24 Stunden.
		settings.JobIdleTimeoutMinutes = domain.ClampJobIdleTimeout(*in.JobIdleTimeoutMinutes)
		touch("job_idle_timeout_minutes")
	}
	if in.JobIdleTimeoutSlowMinutes != nil {
		settings.JobIdleTimeoutSlowMinutes = domain.ClampJobIdleTimeout(*in.JobIdleTimeoutSlowMinutes)
		touch("job_idle_timeout_slow_minutes")
	}
	if in.AptCacheURL != nil {
		aptCacheURL, err := normalizeAptCacheURL(*in.AptCacheURL)
		if err != nil {
			return nil, err
		}
		settings.AptCacheURL = aptCacheURL
		touch("apt_cache_url")
	}
	if in.Require2FARoles != nil {
		// Rollennamen normalisieren: ohne das machte ein „Admin" statt
		// „admin" die 2FA-Pflicht LAUTLOS wirkungslos.
		roles, err := s.normalizeRequire2FARoles(*in.Require2FARoles)
		if err != nil {
			return nil, err
		}
		settings.Require2FARoles = roles
		touch("require_2fa_roles")
	}
	if in.PublicBaseURL != nil {
		publicBase, err := normalizePublicBaseURL(*in.PublicBaseURL)
		if err != nil {
			return nil, err
		}
		settings.PublicBaseURL = publicBase
		touch("public_base_url")
	}

	// System-Mailer: Passwort ist write-only (nil/leer = unverändert).
	if in.MailEnabled != nil {
		settings.MailEnabled = *in.MailEnabled
		touch("mail_enabled")
	}
	if in.MailHost != nil {
		settings.MailHost = strings.TrimSpace(*in.MailHost)
		touch("mail_host")
	}
	if in.MailPort != nil && *in.MailPort > 0 {
		if err := validatePort(*in.MailPort, "mail_port"); err != nil {
			return nil, err
		}
		settings.MailPort = *in.MailPort
		touch("mail_port")
	}
	if in.MailUsername != nil {
		settings.MailUsername = strings.TrimSpace(*in.MailUsername)
		touch("mail_username")
	}
	if in.MailPassword != nil && *in.MailPassword != "" {
		enc, err := s.cipher.EncryptString(*in.MailPassword)
		if err != nil {
			return nil, err
		}
		settings.MailPasswordEnc = enc
		touch("mail_password")
	}
	if in.MailFrom != nil {
		settings.MailFrom = strings.TrimSpace(*in.MailFrom)
		touch("mail_from")
	}
	if in.MailUseTLS != nil {
		settings.MailUseTLS = *in.MailUseTLS
		touch("mail_use_tls")
	}
	if in.MailAdminRecipients != nil {
		settings.MailAdminRecipients = strings.TrimSpace(*in.MailAdminRecipients)
		touch("mail_admin_recipients")
	}

	// CrowdSec-Zugang: URL/Login Klartext, Passwort/Key write-only verschlüsselt.
	if in.CrowdSecLapiURL != nil {
		settings.CrowdSecLapiURL = strings.TrimSpace(*in.CrowdSecLapiURL)
		touch("crowdsec_lapi_url")
	}
	if in.CrowdSecLapiLogin != nil {
		settings.CrowdSecLapiLogin = strings.TrimSpace(*in.CrowdSecLapiLogin)
		touch("crowdsec_lapi_login")
	}
	if in.CrowdSecLapiPassword != nil && *in.CrowdSecLapiPassword != "" {
		enc, err := s.cipher.EncryptString(*in.CrowdSecLapiPassword)
		if err != nil {
			return nil, err
		}
		settings.CrowdSecLapiPasswordEnc = enc
		touch("crowdsec_lapi_password")
	}
	if in.CrowdSecConsoleKey != nil && *in.CrowdSecConsoleKey != "" {
		enc, err := s.cipher.EncryptString(*in.CrowdSecConsoleKey)
		if err != nil {
			return nil, err
		}
		settings.CrowdSecConsoleKeyEnc = enc
		touch("crowdsec_console_key")
	}
	// Konsistenz des ENDzustands prüfen (unabhängig davon, welche Felder
	// dieser PATCH anfasste).
	if settings.MailEnabled {
		if settings.MailHost == "" || settings.MailFrom == "" {
			return nil, ErrMailerIncomplete
		}
		if settings.MailPort <= 0 || settings.MailPort > 65535 {
			return nil, fmt.Errorf("%w: smtp-port ungültig", ErrMailerIncomplete)
		}
	}

	if err := s.settings.Save(settings); err != nil {
		return nil, err
	}
	// Der Audit-Eintrag benennt, WAS sich geändert hat - „settings.update"
	// ohne Details war für einen Prüfer wertlos (R2-062). Werte stehen
	// bewusst nicht drin (Geheimnisse).
	s.audit.Log(actor, "settings.update", "settings", 1, "Felder: "+strings.Join(changed, ", "))
	if s.reload != nil {
		_ = s.reload()
	}
	return settings, nil
}

// BackupSettingsInput sind AUSSCHLIESSLICH die Felder des Backup-Formulars.
type BackupSettingsInput struct {
	Enabled       bool
	IntervalHours int
	Retention     int
	// Time ist die Anker-Uhrzeit des Zeitplans (HH:MM, leer = Vorgabe) -
	// siehe GlobalSettings.BackupTime (R2-034).
	Time        string
	Dir         string
	AutoRestart bool
	// Passphrase für geplante Backups (write-only; leer = unverändert).
	Passphrase string
}

// ErrBackupNeedsPassphrase: automatische Backups lassen sich nicht
// aktivieren, solange keine Passphrase hinterlegt ist - sonst liefe der
// Zeitplan ins Leere und scheiterte still bei jedem Tick (R2-027).
var ErrBackupNeedsPassphrase = errors.New(
	"automatische Backups brauchen eine Passphrase - erst hinterlegen (Passphrase-Feld oder Umgebungsvariable " + EnvBackupPassphrase + "), dann aktivieren")

// BackupPassphraseStored meldet, ob in den Einstellungen eine
// Backup-Passphrase hinterlegt ist (nur das Flag, nie der Wert).
func (s *SettingsService) BackupPassphraseStored() bool {
	settings, err := s.settings.Get()
	return err == nil && settings.BackupPassphraseEnc != ""
}

// UpdateBackupSettings ändert nur die Backup-Felder der globalen
// Einstellungen. Bewusst KEIN UpdateGlobal: das Voll-Update setzt jedes
// nicht mitgereichte Formularfeld (Mailer, DNS, CrowdSec, …) auf seinen
// Nullwert zurück - genau die Falle, die früher schon die apt_cache_url
// über das Backup-Formular geleert hat. Teil-Formulare bekommen deshalb
// eigene, feld-scharfe Update-Methoden.
func (s *SettingsService) UpdateBackupSettings(in BackupSettingsInput, actor string) (*domain.GlobalSettings, error) {
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	settings.BackupEnabled = in.Enabled
	if in.IntervalHours > 0 {
		settings.BackupIntervalHours = in.IntervalHours
	}
	settings.BackupRetention = in.Retention
	backupTime, err := normalizeBackupTime(in.Time)
	if err != nil {
		return nil, err
	}
	settings.BackupTime = backupTime
	// Ein geleertes Verzeichnis-Feld fällt auf den effektiven Standard
	// zurück - das Backup-Ziel ist damit in der UI immer sichtbar gesetzt.
	settings.BackupDir = strings.TrimSpace(in.Dir)
	if settings.BackupDir == "" {
		settings.BackupDir = s.defaultBackupDir
	}
	settings.RestoreAutoRestart = in.AutoRestart
	// Passphrase für geplante Backups: write-only, verschlüsselt (leer =
	// unverändert). R2-027 - vorher gab es KEINEN Weg, sie über API/UI zu
	// setzen; das ab Werk aktive geplante Backup scheiterte still bei
	// jedem Tick.
	if p := strings.TrimSpace(in.Passphrase); p != "" {
		// Eine neu hinterlegte Passphrase für geplante Backups muss die
		// Stärke-Policy erfüllen - dieselbe Prüfung wie beim manuellen Backup.
		if err := EnforceBackupPassphrase(p); err != nil {
			return nil, err
		}
		enc, err := s.cipher.EncryptString(p)
		if err != nil {
			return nil, err
		}
		settings.BackupPassphraseEnc = enc
	}
	// Aktivieren ohne irgendeine Passphrase (gespeichert oder Umgebung)
	// wird abgelehnt - sonst wäre „eingeschaltet" eine leere Behauptung.
	if settings.BackupEnabled && settings.BackupPassphraseEnc == "" && !BackupPassphraseSet() {
		return nil, ErrBackupNeedsPassphrase
	}
	if err := s.settings.Save(settings); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "settings.backup.update", "settings", 1, "")
	// Scheduler neu laden - Intervall/Retention können sich geändert haben.
	if s.reload != nil {
		_ = s.reload()
	}
	return settings, nil
}

// ---- Standard-E-Mail-Versand (System-Mailer) ---------------------------------

var (
	// ErrMailerDisabled: der Standard-E-Mail-Versand ist nicht aktiviert.
	ErrMailerDisabled = errors.New("der standard-e-mail-versand ist nicht aktiviert")
	// ErrMailerIncomplete: Pflichtfelder (Host, Absender, Port) fehlen.
	ErrMailerIncomplete = errors.New("smtp-konfiguration unvollständig (host und absender sind erforderlich)")
	// ErrMailerNoRecipients: für Admin-Hinweise sind keine Empfänger hinterlegt.
	ErrMailerNoRecipients = errors.New("keine admin-empfänger für den standard-e-mail-versand hinterlegt")
)

// SystemMailProvider baut den E-Mail-Provider aus den GlobalSettings -
// derselbe Versandkern wie beim E-Mail-Benachrichtigungskanal. Als
// Empfänger sind die Admin-Adressen konfiguriert (für Kanal-Events);
// transaktionale Mails setzen ihre Empfänger explizit über SendRaw.
func (s *SettingsService) SystemMailProvider() (*notify.EmailProvider, error) {
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	if !settings.MailEnabled {
		return nil, ErrMailerDisabled
	}
	if settings.MailHost == "" || settings.MailFrom == "" {
		return nil, ErrMailerIncomplete
	}
	password := ""
	if settings.MailPasswordEnc != "" {
		if password, err = s.cipher.DecryptString(settings.MailPasswordEnc); err != nil {
			return nil, err
		}
	}
	cfg := notify.EmailConfig{
		Host:       settings.MailHost,
		Port:       settings.MailPort,
		Username:   settings.MailUsername,
		From:       settings.MailFrom,
		Recipients: splitRecipients(settings.MailAdminRecipients),
		UseTLS:     settings.MailUseTLS,
	}
	return notify.NewEmailProvider(cfg, password), nil
}

// SendSystemMail versendet eine transaktionale Mail (Passwort-Reset,
// Einladungslink) über den System-Mailer an explizite Empfänger.
func (s *SettingsService) SendSystemMail(subject, body string, to []string) error {
	provider, err := s.SystemMailProvider()
	if err != nil {
		return err
	}
	return provider.SendRaw(subject, body, to)
}

// NotifyAdmins schickt einen Hinweis an die konfigurierten Admin-Empfänger
// (derzeit: Testnachricht der Einstellungsseite). Ist der Mailer aus oder sind
// keine Empfänger hinterlegt, ist das ein stiller No-Op mit Fehler-Rückgabe -
// Aufrufer entscheiden, ob sie das loggen.
func (s *SettingsService) NotifyAdmins(subject, body string) error {
	settings, err := s.settings.Get()
	if err != nil {
		return err
	}
	recipients := splitRecipients(settings.MailAdminRecipients)
	if len(recipients) == 0 {
		return ErrMailerNoRecipients
	}
	return s.SendSystemMail(subject, body, recipients)
}

// TestSystemMail verschickt eine Testnachricht an die Admin-Empfänger -
// der Konfigurations-Check hinter dem „Test senden"-Button.
func (s *SettingsService) TestSystemMail(actor string) error {
	if err := s.NotifyAdmins("[LCM] Test des Standard-E-Mail-Versands",
		"Diese Nachricht bestätigt, dass der Standard-E-Mail-Versand von LCM korrekt konfiguriert ist."); err != nil {
		return err
	}
	s.audit.Log(actor, "settings.mail.test", "settings", 1, "")
	return nil
}

// splitRecipients zerlegt eine kommagetrennte Empfängerliste.
func splitRecipients(raw string) []string {
	var out []string
	for _, r := range strings.Split(raw, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}
