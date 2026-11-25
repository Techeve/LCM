// LCM - Einstiegspunkt.
//
// Startablauf:
//  1. config.json laden (oder mit sicheren Zufallswerten erzeugen)
//  2. SQLite öffnen, GORM-Migrationen ausführen
//  3. Erststart-Seeding (Rollen, Permissions, system/admin, System-Gruppe)
//  4. Fiber-App mit eingebettetem Frontend starten
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	// Zeitzonendaten ins Binary einbetten: Die TZ-Umgebungsvariable
	// (z.B. TZ=Europe/Berlin) funktioniert damit überall - auch in
	// minimalen Containern ohne tzdata-Paket und unter Windows.
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/middlewares"
	"LCM/internal/api/router"
	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/core/events"
	"LCM/internal/core/services"
	"LCM/internal/dbmetrics"
	"LCM/internal/health"
	"LCM/internal/i18n"
	"LCM/internal/infrastructure/advisories"
	"LCM/internal/infrastructure/creds"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/notify"
	"LCM/internal/infrastructure/registry"
	"LCM/internal/infrastructure/sandbox"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/trivy"
	"LCM/internal/logging"
	"LCM/internal/mcp"
	"LCM/internal/netfilter"
	"LCM/internal/perms"
	"LCM/internal/remote"
	"LCM/internal/safego"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"
	"LCM/ui"
)

// restoreRestartExitCode ist der Exit-Code, mit dem sich LCM nach einem
// vorbereiteten Restore beendet, damit ein Supervisor neu startet (Nicht-Null,
// passt zu systemd Restart=on-failure).
const restoreRestartExitCode = 42

func main() {
	// Vor jedem Kindprozess: Kein Programm, das LCM startet, erbt die
	// Port-Capability des Dienstes (siehe caps_linux.go).
	dropAmbientCaps()

	// Sandbox-Starter: LCM ruft sich selbst so auf, um ein externes Programm
	// (Trivy) unter Landlock-Regeln zu starten. Muss VOR flag.Parse() und vor
	// jeder Initialisierung stehen - dieser Prozess ist kein Server, er setzt
	// nur die Regeln auf sich selbst und wird dann zum Zielprogramm.
	if len(os.Args) > 1 && os.Args[1] == sandbox.ExecArg {
		os.Exit(sandbox.RunExec(os.Args[2:]))
	}

	configPath := flag.String("config", "", "Pfad zur config.json (Default: im Datenverzeichnis)")
	dataDir := flag.String("data", "", "Datenverzeichnis für config.json, Datenbank und version.json (Default: Verzeichnis des Binaries; für Container z.B. /data)")
	debug := flag.Bool("debug", false, "Debug-Modus: hebt das Log-Level auf debug an")
	demo := flag.Bool("demo", false, "Demo-Modus: initialisiert mit Testdaten (Server, Pakete, Job-Historien)")
	demoPublic := flag.Bool("demo-public", false, "Öffentliche Demo-Instanz (impliziert --demo): zeigt Demo-Zugänge auf der Login-Seite und sperrt Zugangs-Änderungen, Backups-Export und ausgehende Verbindungen")
	dev := flag.Bool("dev", false, "Entwicklungsmodus: erlaubt unverschlüsseltes HTTP")
	showVersion := flag.Bool("version", false, "Version ausgeben und beenden")
	healthcheck := flag.Bool("healthcheck", false, "Eigenen Health-Endpunkt prüfen und mit 0 (gesund) bzw. 1 beenden - für den Docker-HEALTHCHECK")
	flag.Parse()

	if *showVersion {
		fmt.Println("LCM", version.String())
		return
	}

	// Healthcheck: prüft den laufenden Dienst und beendet sich sofort. Das
	// Runtime-Image ist ein Scratch-Image ohne Shell und ohne wget - die
	// Prüfung muss also aus dem Binary selbst kommen.
	if *healthcheck {
		if err := runHealthcheck(*configPath, *dataDir); err != nil {
			fmt.Fprintln(os.Stderr, "nicht gesund:", err)
			os.Exit(1)
		}
		return
	}

	// Unterkommandos (rotate-db-key, tls, network, credentials, help ...).
	// Ein unbekanntes Wort startet den Server nicht, siehe runSubcommand.
	opts := cliOptions{configPath: *configPath, dataDir: *dataDir}
	if code, handled := runSubcommand(flag.Args(), opts, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}

	if err := run(*configPath, *dataDir, *debug, *demo || *demoPublic, *dev, *demoPublic); err != nil {
		log.Fatal(err)
	}
}

// resolveDataDir bestimmt das Datenverzeichnis: -data Flag oder LCM_DATA
// (env), sonst das Verzeichnis des Binaries. Systemverzeichnisse (/usr/bin
// bei einer Paket-Installation ohne -data) werden abgelehnt, bevor etwas
// angelegt oder verändert wird.
func resolveDataDir(dataDir string) (string, error) {
	if dataDir == "" {
		dataDir = os.Getenv("LCM_DATA")
	}
	if dataDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		dir := filepath.Dir(exe)
		if err := checkDataDir(dir); err != nil {
			return "", err
		}
		return dir, nil
	}
	if err := checkDataDir(dataDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", fmt.Errorf("datenverzeichnis anlegen: %w", err)
	}
	return dataDir, nil
}

// rotateDBKey entschlüsselt die Datenbank mit dem aktuellen Master-Key
// und verschlüsselt sie mit einem neu generierten - danach wird die
// lcm.key-Datei atomar ersetzt.
func rotateDBKey(configPath, dataDir string) error {
	dataDir, err := resolveDataDir(dataDir)
	if err != nil {
		return err
	}
	if configPath == "" {
		configPath = filepath.Join(dataDir, config.DefaultFileName)
	}
	cfg, err := config.LoadFrom(configPath)
	if err != nil {
		return err
	}

	oldKey, source, err := crypto.LoadOrCreateMasterKey(dataDir)
	if err != nil {
		return err
	}
	if source == crypto.SourceGenerated {
		_ = crypto.ShredKeyFile(filepath.Join(dataDir, crypto.KeyFileName))
		return fmt.Errorf("kein bestehender master-key gefunden - rotation nicht möglich")
	}
	oldCipher, err := crypto.NewCipher(oldKey)
	if err != nil {
		return err
	}
	newKey := crypto.GenerateKey()
	newCipher, err := crypto.NewCipher(newKey)
	if err != nil {
		return err
	}

	dbPath := cfg.DatabasePath
	if !filepath.IsAbs(dbPath) && dbPath != ":memory:" {
		dbPath = filepath.Join(dataDir, dbPath)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		return err
	}
	if err := storage.RotateEncryptedFields(db, oldCipher, newCipher); err != nil {
		return err
	}
	keyPath := filepath.Join(dataDir, crypto.KeyFileName)
	if err := crypto.WriteKeyFile(keyPath, newKey); err != nil {
		return fmt.Errorf("ACHTUNG: DB bereits mit neuem Key verschlüsselt, aber %s konnte nicht geschrieben werden: %w", keyPath, err)
	}
	fmt.Println(i18n.Tf(
		"Master key rotated - all encrypted fields were re-encrypted with the new key from %s.",
		"Master-Key rotiert - alle verschlüsselten Felder wurden mit dem neuen Key aus %s verschlüsselt.",
		keyPath))
	credentialHint(defaultCredstore)
	return nil
}

func run(configPath, dataDir string, debug, demo, dev, demoPublic bool) error {
	// Datenverzeichnis: dorthin kommen config.json, Datenbank, lcm.key
	// und version.json - im Container ist das der Volume-Mountpoint.
	dataDir, err := resolveDataDir(dataDir)
	if err != nil {
		return err
	}

	// 1. Konfiguration
	if configPath == "" {
		configPath = filepath.Join(dataDir, config.DefaultFileName)
	}
	// Ein vorbereitetes Backup-Restore JETZT anwenden - vor dem Laden von
	// config.json/lcm.key und dem Öffnen der DB (die getauscht werden).
	if applied, err := services.ApplyStagedRestore(dataDir, configPath); err != nil {
		return fmt.Errorf("restore anwenden: %w", err)
	} else if applied {
		slog.Info("backup restored - starting with the restored data")
	}
	cfg, err := config.LoadFrom(configPath)
	if err != nil {
		return err
	}
	// Dateirechte: beim Start prüfen und richten, danach regelmäßig. Ein
	// einziges chmod 777 auf dem Datenverzeichnis macht Datenbank und
	// Master-Key sonst für jeden Benutzer des Hosts lesbar - und bliebe
	// unbemerkt.
	permGuard := perms.New(dataDir, configPath)
	permGuard.Run()
	safego.Go("perms-guard", func() { permGuard.Loop(make(chan struct{})) })
	if demo {
		cfg.DemoMode = true
	}
	cfg.DemoPublic = demoPublic
	cfg.DevMode = dev

	// Master-Key für die At-Rest-Verschlüsselung (AES-256-GCM) laden
	// oder beim Erststart erzeugen (lcm.key, chmod 600).
	masterKey, keySource, err := crypto.LoadOrCreateMasterKey(dataDir)
	if err != nil {
		return err
	}
	cipher, err := crypto.NewCipher(masterKey)
	if err != nil {
		return err
	}
	keyCreated := keySource == crypto.SourceGenerated
	if keyCreated {
		fmt.Println(i18n.Tf(
			"crypto: new master key created (%s) - keep it safe!",
			"crypto: neuer Master-Key erzeugt (%s) - sicher aufbewahren!",
			filepath.Join(dataDir, crypto.KeyFileName)))
	}

	// Container-freundliche Overrides: Host/Port per Umgebungsvariable,
	// ohne die gemountete config.json anfassen zu müssen.
	if host := os.Getenv("LCM_HOST"); host != "" {
		cfg.Host = host
	}
	if port := os.Getenv("LCM_PORT"); port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p <= 0 || p > 65535 {
			return fmt.Errorf("ungültiger LCM_PORT: %q", port)
		}
		cfg.Port = p
	}
	// Dedizierter Agent-Listener (LCM Remote) - ebenfalls per ENV übersteuerbar.
	// LCM_AGENT_PORT=0 schaltet den Agent-Listener ab.
	if host := os.Getenv("LCM_AGENT_HOST"); host != "" {
		cfg.AgentHost = host
	}
	if port := os.Getenv("LCM_AGENT_PORT"); port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 0 || p > 65535 {
			return fmt.Errorf("ungültiger LCM_AGENT_PORT: %q", port)
		}
		cfg.AgentPort = p
	}
	// Trivy-Sidecar (Container-Betrieb): Adresse und Token kommen üblicherweise
	// aus der Compose-Umgebung. Die Umgebung gewinnt gegen die config.json -
	// sie gehört zum Deployment, während die config.json aus dem gemounteten
	// Datenverzeichnis stammt und den Container überlebt.
	if url := os.Getenv("LCM_TRIVY_URL"); url != "" {
		cfg.TrivyURL = url
	}
	if token := os.Getenv("LCM_TRIVY_TOKEN"); token != "" {
		cfg.TrivyToken = token
	}
	// Ein systemd-Credential (SetCredentialEncrypted=trivy_token:…) schlägt
	// beides: Es steht weder in der config.json noch in der Umgebung.
	if token, ok := creds.Read(creds.TrivyToken); ok {
		cfg.TrivyToken = token
	}
	// Nach den Overrides erneut prüfen: Beim Laden lag das Token womöglich
	// noch nicht vor, und ein Sidecar ohne Token bekäme auf jede Anfrage
	// eine 401 - der CVE-Scan wäre still tot.
	if err := cfg.ValidateTrivy(); err != nil {
		return err
	}

	// Log-Service: Level aus config.json, -debug hebt auf Debug an. Zusätzlich
	// zu stdout wird in eine rotierende Datei geschrieben (Standard:
	// <Datenverzeichnis>/logs/lcm.log) - dort lassen sich Dienst-Starts/-Stopps,
	// Backups und andere Aktionen dauerhaft nachvollziehen.
	logFile := cfg.LogFile
	if logFile == "" {
		logFile = filepath.Join(dataDir, "logs", "lcm.log")
	}
	logger := logging.Setup(cfg.LogLevel, debug, logFile)
	// Klarer, greppbarer Lifecycle-Marker: JEDER (Neu-)Start erzeugt diese Zeile.
	// Fehlt davor ein "Dienst wird beendet", war es ein Absturz/harter Kill.
	slog.Info("=== LCM service started ===",
		"version", version.Version, "build", version.Build,
		"pid", os.Getpid(), "data_dir", dataDir, "log_file", logFile)
	// Woher der Master-Key kam, gehört ins Protokoll: Nur so sieht ein
	// Betreiber, ob die Umstellung auf ein systemd-Credential greift - und ob
	// nach einem Restore oder einer Rotation wieder eine Klartext-Datei liegt.
	slog.Info("master key", "source", keySource)
	if crypto.CredentialStale(keySource) {
		slog.Warn("security", "event", "masterkey.file-overrides-credential",
			"detail", "lcm.key lies next to the database although a systemd credential exists - "+
				"the file wins (restore or rotation); run `lcm credentials init` to move it back")
	}

	// 2. Datenbank: relativer Pfad wird im Datenverzeichnis aufgelöst,
	// damit sich der Service unabhängig vom Arbeitsverzeichnis verhält.
	dbPath := cfg.DatabasePath
	if !filepath.IsAbs(dbPath) && dbPath != ":memory:" {
		dbPath = filepath.Join(dataDir, dbPath)
	}
	// Erststart-Erkennung: Existierte die DB-Datei vor diesem Start?
	freshInstall := false
	if dbPath != ":memory:" {
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			freshInstall = true
		}
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		return err
	}
	// Cipher für die feldweise At-Rest-Verschlüsselung (SSH-/Job-Output)
	// registrieren, BEVOR geschrieben/gelesen wird (u.a. Demo-Seed).
	storage.SetFieldCipher(cipher)
	if err := storage.Migrate(db); err != nil {
		return err
	}

	// 3. Update-Prozess: Versionsdatei (version.json neben der DB) mit
	//    der Binary-Version abgleichen; bei einem Update laufen die
	//    ausstehenden Migrationen. Danach Erststart-Seeding (idempotent).
	updateResult, err := storage.RunUpdateMigrations(db, storage.UpdateOptions{
		VersionFilePath: filepath.Join(filepath.Dir(dbPath), version.FileName),
		FreshInstall:    freshInstall,
	})
	if err != nil {
		return err
	}
	// OS-/Kernel-/CPU-Profilfelder erst JETZT verschlüsseln - nach den
	// versionierten Migrationen, deren v0.3.0-Schritt os_name/os_id noch im
	// Klartext per LIKE auswertet (Paketverwaltung-Backfill).
	if err := storage.EncryptServerProfileFields(db); err != nil {
		return err
	}
	if err := storage.Seed(db, cfg); err != nil {
		return err
	}

	// 4. Dependency-Wiring: Repository -> Service -> Router
	userRepo := repositories.NewUserRepository(db)
	roleRepo := repositories.NewRoleRepository(db)
	apiKeyRepo := repositories.NewAPIKeyRepository(db)
	serverRepo := repositories.NewServerRepository(db)
	jobRepo := repositories.NewJobRepository(db)

	// Diagnose: Wurde ein NEUER Master-Key erzeugt, obwohl bereits
	// verschlüsselte Server-Zugänge in der DB liegen, ist die lcm.key-Datei
	// zwischen zwei Starts verloren gegangen (typisch: nicht persistentes
	// Datenverzeichnis / fehlendes Volume). Die gespeicherten SSH-Credentials
	// sind dann nicht mehr entschlüsselbar - ohne diese Warnung äußert sich
	// das erst später als kryptischer Verbindungsfehler.
	if keyCreated {
		if existing, err := serverRepo.FindAllUnscoped(); err == nil {
			encrypted := 0
			for i := range existing {
				if existing[i].PrivateKeyEnc != "" {
					encrypted++
				}
			}
			if encrypted > 0 {
				slog.Warn("WARNING: new master key created although encrypted server credentials already exist - "+
					"the lcm.key file was lost between two starts (data directory not persistent?). "+
					"the stored ssh credentials can no longer be decrypted; the affected servers must be reconnected",
					"affected_servers", encrypted,
					"keyfile", filepath.Join(dataDir, crypto.KeyFileName))
			}
		}
	}
	auditRepo := repositories.NewAuditRepository(db)
	groupRepo := repositories.NewGroupRepository(db)
	settingsRepo := repositories.NewSettingsRepository(db)
	linuxRepo := repositories.NewLinuxUserRepository(db)
	profileRepo := repositories.NewPrivilegeProfileRepository(db)

	authService := services.NewAuthService(userRepo, cfg.AccessTokenTTL()).
		WithSessionTTL(func() time.Duration {
			// Session-Dauer aus den globalen Einstellungen (0 = config-Vorgabe).
			if st, err := settingsRepo.Get(); err == nil && st.SessionTTLMinutes > 0 {
				return time.Duration(st.SessionTTLMinutes) * time.Minute
			}
			return cfg.AccessTokenTTL()
		})
	userService := services.NewUserService(userRepo, roleRepo)
	apiKeyService := services.NewAPIKeyService(apiKeyRepo)
	systemService := services.NewSystemService()
	// Ereignisbus: Die Dienste melden darauf, was sich ändert; der WebSocket
	// GET /api/v1/events reicht es an die Tabs weiter.
	eventBus := events.NewBus()
	jobService := services.NewJobService(jobRepo).WithEvents(eventBus)
	// Betriebsfähigkeit der Server: jedes Jobende (und jeder Watchdog-Abbruch)
	// schreibt den Zustand seiner Kategorie fort. Vor dem Watchdog verdrahten,
	// damit dessen Goroutine den Hook nicht mitten im Setzen sieht.
	capabilityRepo := repositories.NewCapabilityRepository(db)
	capabilityService := services.NewCapabilityService(capabilityRepo).WithNothingPending(serverRepo.NothingPending)
	jobService.OnOutcome(capabilityService.RecordJob)
	// Verwaiste Jobs des letzten Laufs aufräumen: nach einem Neustart kann
	// kein Job mehr legitim "running" sein - hängengebliebene Einträge würden
	// die Server-Sperre sonst für immer halten. VOR Scheduler-/API-Start.
	//
	// Ausgenommen ist der Lauf, der LCM selbst aktualisiert hat: Er hat sein
	// Ziel erreicht (deshalb läuft jetzt die neue Version) und wird als
	// erfolgreich abgeschlossen statt als Fehler gemeldet.
	selfUpdate := services.SelfUpdateOnRestart(serverRepo, updateResult.PreviousVersion, version.Version)
	jobService.FailInterruptedOnStartup(selfUpdate)
	// Job-Watchdog: bricht Jobs ab, die keine Lebenszeichen mehr geben
	// (hängende Remote-Kommandos geben die Server-Sperre damit selbst frei).
	safego.Go("job-watchdog", func() {
		jobService.RunWatchdog(services.JobIdleLimit(settingsRepo, serverRepo))
	})
	auditService := services.NewAuditService(auditRepo)
	jobService.WithAudit(auditService) // R2-067: Abbrüche/Watchdog auditieren
	// Benutzer-, Rollen- und API-Key-Operationen gehören in die Audit-Kette -
	// genau die Rechte-vergebenden Operationen waren die einzigen ohne Spur
	// (R2-048).
	userService.WithAudit(auditService)
	apiKeyService.WithAudit(auditService)
	sshLogRepo := repositories.NewSSHLogRepository(db)
	// Job-Verbindungen beim JobService registrieren: beim Abbruch (manuell
	// oder Watchdog) werden sie zwangsweise geschlossen.
	sshRecorder := services.NewSSHRecorder(sshLogRepo).WithJobs(jobService)
	// CVE-Scanner (Trivy): zentral, ohne die verwalteten Server zu kontaktieren.
	// Zwei Wege, dieselbe Auswertung - die Wahl trifft die Konfiguration:
	//   trivy_url gesetzt  → Sidecar-Container (cmd/trivyd), Container-Betrieb
	//   sonst              → Binary auf dem LCM-Host, in der Sandbox
	// Weiche Abhängigkeit: Fehlt Trivy lokal, deaktiviert sich der CVE-Scan
	// sauber. Ein KONFIGURIERTER, aber unerreichbarer Sidecar ist dagegen ein
	// Fehler und kein „aus" - sonst sähe ein Ausfall aus wie „abgeschaltet".
	cveScanner := trivy.New(cfg.TrivyPath, filepath.Join(filepath.Dir(dbPath), "trivy"))
	if cfg.TrivyURL != "" {
		slog.Info("CVE-Scanner: Sidecar", "url", cfg.TrivyURL)
		cveScanner = trivy.NewRemote(cfg.TrivyURL, cfg.TrivyToken)
	}
	knownRepoRepo := repositories.NewKnownRepoRepository(db)
	appRepo := repositories.NewAppRepository(db)
	ipAllowlistRepo := repositories.NewIPAllowlistRepository(db)
	packagePinRepo := repositories.NewPackagePinRepository(db)
	// Steuert die automatische CVE-Neubewertung nach Paket-Updates (lazy gelesen;
	// bei Lesefehler konservativ „an", passend zum gorm-Default).
	cveScanEnabled := func() bool {
		st, err := settingsRepo.Get()
		return err != nil || st.CVEScanEnabled
	}
	// CVE-Hochgewichtungs-Liste aus den globalen Einstellungen (leer =
	// eingebaute Standardliste) - für Status-Ampel und CVE-Alarme.
	cveWeightList := func() []string {
		st, err := settingsRepo.Get()
		if err != nil {
			return (&domain.GlobalSettings{}).CVEHighWeightList()
		}
		return st.CVEHighWeightList()
	}
	// DNS-Test-Domains aus den globalen Einstellungen (leer = eingebaute
	// Standardliste) - für die DNS-Test-Aktion und die Gruppen-Regel.
	dnsTestDomains := func() []string {
		st, err := settingsRepo.Get()
		if err != nil {
			return (&domain.GlobalSettings{}).DNSTestDomainList()
		}
		return st.DNSTestDomainList()
	}
	serverService := services.NewServerService(serverRepo, jobService, auditService, cipher, sshx.NewClient()).
		WithRecorder(sshRecorder).WithLinux(linuxRepo).WithGroups(groupRepo).WithScanner(cveScanner).
		WithSettings(settingsRepo).
		WithKnownRepos(knownRepoRepo).WithCVERescanEnabled(cveScanEnabled).
		WithCVEWeightList(cveWeightList).WithDNSTestDomains(dnsTestDomains).
		WithPackagePins(packagePinRepo).WithApps(appRepo).WithCapabilities(capabilityRepo).
		WithEvents(eventBus).
		WithCustomActions(repositories.NewCustomActionRepository(db))
	// Endet ein Job, kann sich die Ampel seines Servers geändert haben.
	jobService.OnFinished(serverService.ForgetStatus)

	// LCM Remote: eingebetteter MQTT-Broker + AgentHub für Server, die sich
	// per lcm-agent AUSGEHEND verbinden (NAT/Roaming). Kommandos laufen dort
	// über MQTT statt SSH - mit derselben erlaubten Stille wie der
	// Job-Watchdog (plus Puffer, damit immer zuerst der Watchdog abbricht).
	// Maßgeblich ist der größere der beiden Werte: Auf welcher Hardware das
	// Kommando landet, weiß erst der Watchdog anhand des Ziel-Servers.
	agentHub := remote.New(serverRepo, slog.Default()).
		WithIdleTimeout(func() time.Duration {
			st, err := settingsRepo.Get()
			if err != nil {
				return services.DefaultJobIdleTimeout + 10*time.Minute
			}
			minutes := max(st.JobIdleTimeoutMinutes, st.JobIdleTimeoutSlowMinutes)
			return time.Duration(domain.ClampJobIdleTimeout(minutes))*time.Minute + 10*time.Minute
		}).
		WithOnAgentOnline(func(server *domain.Server) {
			// Erst-Scan beim allerersten Kontakt eines frisch enrollten
			// Servers (noch kein OS erfasst) - danach übernehmen die
			// regulären Schedules (Health-Check, System-Sync).
			if server.OSName != "" {
				return
			}
			if _, err := serverService.RefreshAll(repositories.ScopeAll(), server.ID, "system:agent"); err != nil {
				slog.Warn("initial scan after agent connect failed", "server", server.Name, "error", err)
			}
		})
	if err := agentHub.Start(); err != nil {
		return fmt.Errorf("mqtt-broker: %w", err)
	}
	defer agentHub.Close()
	serverService.WithAgentHub(agentHub)
	packageService := services.NewPackageService(serverRepo)
	backupService := services.NewBackupService(repositories.NewDatabaseRepository(db), settingsRepo, dataDir, dbPath, configPath).
		WithConfigDir(cfg.BackupDir).WithCipher(cipher).WithMasterKey(masterKey)
	// Reste eines abgebrochenen Sicherungslaufs jetzt wegräumen: Ein neuer
	// Prozess hat keinen laufenden Lauf, den er stören könnte, und eine
	// liegengebliebene Momentaufnahme ist eine unverschlüsselte Kopie der
	// Datenbank. Danach übernimmt Prune nach jeder Sicherung.
	backupService.CleanStaleTemp(0)
	// R2-027: Das geplante Backup war ab Werk aktiv, konnte aber ohne
	// Passphrase PRINZIPBEDINGT nie laufen - 13 stille Fehlversuche im
	// Langzeittest. Ist es aktiviert und existiert nirgends eine Passphrase
	// (weder Umgebung noch Einstellungen), wird es EINMALIG deaktiviert und
	// das laut gesagt, statt Nacht für Nacht still zu scheitern.
	if st, err := settingsRepo.Get(); err == nil &&
		st.BackupEnabled && st.BackupPassphraseEnc == "" && !services.BackupPassphraseSet() && st.BackupRecipients == "" {
		if err := settingsRepo.UpdateFields(map[string]any{"backup_enabled": false}); err == nil {
			slog.Warn("automatic backups disabled: neither recipient keys nor a passphrase configured - " +
				"set one under Einstellungen → Backups (or LCM_BACKUP_PASSPHRASE) and re-enable")
		}
	}
	provService := services.NewProvisioningService(linuxRepo, serverRepo, cipher, serverService.Connect).
		WithAudit(auditService).WithRecorder(sshRecorder).WithDNSTestDomains(dnsTestDomains).
		WithPendingUserSyncs(repositories.NewPendingUserSyncRepository(db)).
		WithSSHLog(sshLogRepo).WithProfiles(profileRepo)
	serverService.WithServerUserPlans(provService) // Befehlsvorschau der Konten-Aktionen
	sshLogService := services.NewSSHLogService(sshLogRepo, serverRepo, jobRepo)
	linuxUserService := services.NewLinuxUserService(linuxRepo, groupRepo, auditService, cipher, provService).
		WithProfiles(profileRepo)
	activationService := services.NewActivationService(repositories.NewActivationRepository(db), userRepo, auditService)

	// Scheduler + Executor: der Executor braucht den Scheduler nicht, der
	// Scheduler den Executor - daher Executor zuerst, Scheduler danach.
	customActionRepo := repositories.NewCustomActionRepository(db)
	customActionService := services.NewCustomActionService(customActionRepo, auditService)
	profileService := services.NewPrivilegeProfileService(profileRepo, auditService)
	blockService := services.NewProfileBlockService(repositories.NewProfileBlockRepository(db), auditService)
	appService := services.NewAppService(appRepo, customActionRepo, auditService).
		WithLatestChecker(services.NewLatestChecker())
	// Notification-Engine + Alert-Management: der Alert-Service wertet die
	// Monitoring-/Trigger-Kriterien aus und benachrichtigt über die Kanäle.
	notificationRepo := repositories.NewNotificationRepository(db)
	alertRepo := repositories.NewAlertRepository(db)
	notificationService := services.NewNotificationService(notificationRepo, alertRepo, cipher, auditService)
	alertService := services.NewAlertService(alertRepo, serverRepo, groupRepo, notificationService, auditService).
		WithCVEWeightList(cveWeightList).WithCapabilities(capabilityRepo).WithEvents(eventBus)
	// action_failed: Wechsel der Betriebsfähigkeit lösen den Alarm sofort aus,
	// nicht erst im 30-Minuten-Lauf.
	capabilityService.OnTransition(alertService.OnCapabilityTransition)
	executor := services.NewExecutor(serverRepo, groupRepo, jobService, auditService,
		provService, backupService, settingsRepo, serverService.Connect).
		WithRecorder(sshRecorder).WithCustomActions(customActionRepo).WithScanner(cveScanner).
		WithRegistry(registry.New()).
		WithAlerts(alertService).WithProfiles(profileRepo).WithApps(appService).
		WithCapabilities(capabilityService).
		WithCompactor(func() (string, error) {
			res, err := storage.CompactDatabase(db, func() (uint64, bool) { return storage.FreeDiskBytes(dataDir) })
			return res.String(), err
		})
	scheduler := services.NewScheduler(groupRepo, settingsRepo, executor)
	// Nach Container-/Image-Updates die Docker-CVE-Bewertung sofort
	// auffrischen: der zentrale Docker-Check läuft als eigener System-Job.
	serverService.WithDockerCheckTrigger(executor.RunDockerCheck)
	// „Erneut versuchen" der Betriebsfähigkeit läuft über den Executor.
	serverService.WithRetryRunner(executor)
	groupService := services.NewGroupService(groupRepo, serverRepo, auditService, scheduler.Reload).
		WithCustomActions(customActionRepo).WithUsers(userRepo).WithProvisioning(provService)
	// Grundsatz-Regeln gleichen Typs aus mehreren Gruppen entscheidet seit
	// der Einführung des Gruppen-Vorrangs die stärkere Gruppe. Wo der Vorrang
	// gleich ist, entscheidet die Gruppen-ID - das ist im Altbestand der
	// Normalfall und soll dem Betreiber auffallen, bevor er es an geänderten
	// Ports merkt.
	groupService.ReportEnforceOverlaps()
	// Effektives Standard-Backup-Verzeichnis (config.json-Vorgabe oder
	// <data>/backups) - belegt ein leeres backup_dir in den Einstellungen vor,
	// damit die UI immer das tatsächliche Ziel zeigt.
	//
	// Der Pfad MUSS absolut sein: Die Validierung der Einstellungen lehnt
	// relative Pfade ab (sie haengen am Arbeitsverzeichnis des Dienstes und
	// wandern damit unbemerkt). Wird LCM mit einem relativen -data gestartet,
	// entstuende hier ein relativer Vorgabewert - und die Seite
	// „Einstellungen" liesse sich danach GAR NICHT mehr speichern, weil die
	// UI ihn unveraendert zurueckschickt.
	defaultBackupDir := cfg.BackupDir
	if defaultBackupDir == "" {
		defaultBackupDir = filepath.Join(dataDir, "backups")
	}
	if abs, err := filepath.Abs(defaultBackupDir); err == nil {
		defaultBackupDir = abs
	}
	releaseNotes := services.NewReleaseNotesService(settingsRepo, userRepo)
	if updateResult.Updated {
		if err := releaseNotes.NoteUpdate(updateResult.PreviousVersion, version.Version); err != nil {
			slog.Warn("could not note the update for the release notes", "error", err)
		}
	}
	settingsService := services.NewSettingsService(settingsRepo, cipher, auditService, scheduler.Reload).
		WithKnownRepos(knownRepoRepo).WithIPAllowlists(ipAllowlistRepo).
		WithCrowdSecLapis(repositories.NewCrowdSecLapiRepository(db)).
		WithAptCaches(repositories.NewAptCacheRepository(db)).
		WithDefaultBackupDir(defaultBackupDir).
		WithRoles(roleRepo).
		WithAddressConfig(addressConfig(cfg, dev))
	// config.json und Einstellungen führen Adressen, Ports, Proxys und Netzbereiche
	// gemeinsam: Was von Hand in der Datei geändert wurde, geht in die Einstellungen,
	// danach folgt die Datei der Datenbank. Auch im Entwicklungsmodus: Die
	// E2E-Tests legen ihre Ports in der config.json fest.
	mirror := newConfigSync(configPath, settingsRepo, settingsService, envLocksOf(addressConfig(cfg, dev)))
	mirror.Start()
	settingsService.WithConfigSync(mirror.Sync)
	configSyncFn := mirror.Sync
	systemService.WithAgentPort(settingsService.AgentPort).WithAgentURL(settingsService.PublicAgentURL).
		WithAgentProxyHint(settingsService.AgentURLNeedsProxy)
	serverService.WithAgentURL(settingsService.PublicAgentURL).WithLcmListeners(settingsService.LcmListeners)

	// Wird der LCM-Host aus der Verwaltung entfernt, darf er sich nicht beim
	// nächsten Paket-Update erneut selbst aufnehmen. Erst hier verdrahtbar:
	// settingsService entsteht nach serverService.
	serverService.WithSelfRegisterOff(settingsService.DisableSelfRegistration)

	totpService := services.NewTOTPService(userRepo, cipher, auditService)
	// CrowdSec-LAPI-Überwachung: der crowdsec_lapi_down-Alarm nutzt denselben
	// Login-Check wie die CrowdSec-Einstellungsseite. (Nachträglich verdrahtet,
	// weil der SettingsService erst nach dem AlertService entsteht.)
	alertService.WithCrowdSecLapiChecker(settingsService.CheckCrowdSecLapis)
	// Frist des Stillstands (action_failed) aus den globalen Einstellungen.
	alertService.WithSettings(settingsService.Get)
	// Stand der CVE-Datenbank fuer den cve_db_stale-Alarm (derselbe Wert, den
	// auch Ampel und Sicherheitsseite zeigen).
	alertService.WithCVEDBChecker(serverService.CVEDBStatus)
	// Erneuerung des eigenen TLS-Zertifikats vor Ablauf (services.CertRenewal).
	// Das Laden in die Listener verdrahtet der HTTPS-Start unten; im
	// --dev-Modus gibt es kein Zertifikat, und die Prüfung bleibt still.
	var certStore *tlsx.CertStore
	certRenewal := services.NewCertRenewal(dataDir, serverRepo, func() error {
		if certStore == nil {
			return nil
		}
		return certStore.Reload()
	}).WithOnline(agentHub.Online)
	// Pins fürs Enrollment-Token und den Download-Befehl: die des Zertifikats,
	// das der Pool für den Namen der Adresse ausliefert. Neue Agenten folgen
	// damit von Anfang an den Erneuerungen (CA-Pin). Der Pool entsteht erst mit
	// den Listenern, im --dev-Modus nie - dann bleiben Token und Befehl ohne Pins.
	var certPool *tlsx.Pool
	serverService.WithAgentTrust(func(host string) services.AgentTrust {
		if certPool == nil {
			return services.AgentTrust{}
		}
		lcmCA, _ := certRenewal.TokenCAFingerprint()
		return services.AgentTrustFor(certPool, lcmCA, host)
	})
	alertService.WithCertWarning(func() string {
		st, err := certRenewal.State()
		if err != nil {
			return ""
		}
		return certRenewal.Warning(st)
	})
	// Backup-Zustand fuer den backup_stale-Alarm: Aktivierung/Intervall aus
	// den Einstellungen, Alter des juengsten Backups aus der Historie
	// (neueste-zuerst; auch manuelle Backups zaehlen - derselbe Stand).
	alertService.WithBackupStatus(func() (bool, int, *time.Time, error) {
		gs, err := settingsService.Get()
		if err != nil {
			return false, 0, nil, err
		}
		backups, err := backupService.List()
		if err != nil {
			return false, 0, nil, err
		}
		var newest *time.Time
		if len(backups) > 0 {
			newest = &backups[0].CreatedAt
		}
		return gs.BackupEnabled, gs.BackupIntervalHours, newest, nil
	})

	// Fruehwarnung (Etappe B): fragt alle 15 Minuten die Online-Quelle OSV
	// nach Befunden zum installierten Paketbestand - die schnelle Spur neben
	// dem taeglichen Trivy-Scan. Standardmaessig AUS; der Poller prueft die
	// Einstellung bei jedem Lauf selbst, das Umlegen des Schalters wirkt
	// also sofort.
	advisoryService := services.NewAdvisoryService(
		serverRepo,
		repositories.NewAdvisoryRepository(db),
		repositories.NewAdvisoryCacheRepository(db),
		settingsRepo,
		advisories.NewOSV(""),
	).WithExploitSource(advisories.NewEUVD("")).
		WithLocalSource(advisories.NewLocalOSV(filepath.Join(dataDir, "osv"), "")).
		WithScanCacheStats(cveScanner.CacheStats)
	executor.WithAdvisories(advisoryService)
	// Befunde der Fruehwarnung fuer den advisory-Alarm.
	alertService.WithAdvisoryFindings(advisoryService.ActiveForServer)

	// Standard-E-Mail-Versand verdrahten: der verwaltete Kanal (system_email)
	// liest seine SMTP-Konfiguration zur Sendezeit aus den GlobalSettings;
	// Einladungs- und Passwort-Reset-Mails laufen über denselben Versandkern.
	notificationService.WithSystemMailer(func() (notify.Provider, error) {
		return settingsService.SystemMailProvider()
	})
	activationService.WithMailer(settingsService.SendSystemMail)
	// Aktivierungslinks für LINUX-Benutzer über denselben Postausgang: Das
	// Feld für die Adresse gab es immer, den Weg dorthin nicht.
	linuxUserService.WithMailer(settingsService.SendSystemMail)
	// Basis-Adresse für Links in Mails: AUSSCHLIESSLICH aus der Konfiguration,
	// nie aus dem Host-Header des Requests (sonst könnte ein Angreifer sich
	// einen gültigen Passwort-Reset-Link auf die eigene Domain ausstellen
	// lassen - siehe domain.GlobalSettings.PublicBaseURL).
	activationService.WithLinkBase(settingsService.LinkBaseURL)
	linuxUserService.WithLinkBase(settingsService.LinkBaseURL)
	// Onboarding-Key für den Key-Login beim Join/Reconnect (lazy entschlüsselt).
	// Nachgelagert verdrahtet, weil der SettingsService erst nach dem
	// ServerService entsteht (er braucht scheduler.Reload).
	serverService.WithOnboardingKey(settingsService.OnboardingPrivateKey)
	// Benannte IP-Allowlists auflösen (Firewall-Quell-Einschränkung + Security-
	// Tools) - sowohl für Server-Aktionen als auch für Grundsatz-Regeln.
	serverService.WithIPAllowlists(settingsService.ExpandIPAllowlists)
	serverService.WithCrowdSecConfig(settingsService.CrowdSecConfig)
	serverService.WithAptCacheURL(settingsService.AptCacheURL)
	executor.WithAptCaches(settingsService.AptCacheURL)
	alertService.WithAptCacheChecker(settingsService.CheckAptCaches)
	// Bis 1.46 gab es genau eine APT-Cache-URL in den Einstellungen - sie
	// wandert einmalig in die Liste.
	if err := settingsService.MigrateLegacyAptCache(); err != nil {
		slog.Warn("apt cache migration failed", "error", err)
	}
	// Bis 1.45 gab es genau eine LAPI in den Einstellungen - sie wandert
	// einmalig in die Liste.
	if err := settingsService.MigrateLegacyCrowdSecLapi(); err != nil {
		slog.Warn("crowdsec lapi migration failed", "error", err)
	}
	executor.WithIPAllowlists(settingsService.ExpandIPAllowlists)
	// Synology DSM: Health-Check und System-Sync erheben den Zustand ueber die
	// DSM-Web-API - der Executor kennt den ServerService nicht, daher als
	// Closure (dasselbe Muster wie die SSH-connect-Funktion).
	executor.WithDSMRefresh(serverService.RefreshDSMState)
	// Löschsperre: eine referenzierte Allowlist ist nicht löschbar (R2-072).
	settingsService.WithIPAllowlistUsage(func(id uint) []string {
		return services.IPAllowlistUsage(serverRepo, groupRepo, id)
	})
	// LCM-Host-Selbst-Einrichtung: nach der Installation von Trivy/apt-cacher-ng
	// den CVE-Scan aktivieren bzw. die lokale APT-Cache-URL eintragen.
	serverService.WithLcmHostConfig(
		// Die Datenbank zuerst: Der Scan lädt nichts nach und bricht ohne sie ab.
		func() error {
			executor.RefreshCVEDB("trivy-install")
			return settingsService.SetCVEScanEnabled(true)
		},
		settingsService.AdoptHostAptCache,
	)
	// CrowdSec-LAPI auf dem LCM-Host: nach der Installation die erzeugten
	// Maschinen-Zugangsdaten in die CrowdSec-Einstellungen eintragen.
	serverService.WithCrowdSecLapiSetter(settingsService.AdoptHostCrowdSecLapi)

	// Client-Adresse und Netzbereiche: wessen X-Forwarded-For gilt, und welche
	// Netze je Endpunkt (Weboberfläche, Agent, MCP) zugelassen sind. Beides
	// steht in den Einstellungen (die config.json spiegelt es) und ändert sich
	// zur Laufzeit über die Oberfläche (services.SettingsService,
	// services.NetworkAccessService).
	proxyTrust := netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{})
	if _, err := settingsService.WithProxyTrust(proxyTrust); err != nil {
		return err
	}
	networkAccess, err := services.NewNetworkAccessService(settingsRepo, auditService)
	if err != nil {
		return err
	}
	networkAccess.WithConfigSync(configSyncFn).WithAgentPeers(agentHub)
	logActiveNetworkLists(networkAccess)

	// Zertifikate von einer ACME-CA. Den Pool gibt es erst mit den Listenern;
	// bis dahin (und im --dev-Modus) lädt ein neues Zertifikat nirgends hin.
	var reloader *certReloader
	acmeService := newACMEService(cfg, dataDir, settingsRepo, auditService, settingsService, cipher,
		func() {
			if reloader != nil {
				reloader.Reload("")
			}
		})
	alertService.WithRenewalFailure(acmeService.RenewalFailure)
	if !dev {
		// Fehlt ein Zertifikat für alle Namen, stellt LCM nach dem Speichern selbst aus.
		acmeService.WithAutoIssue()
		settingsService.WithAddressesSaved(acmeService.EnsureCoverage)
	}

	// MCP-Schnittstelle: separater HTTP-Listener, über den KI-Agenten read-only
	// Server-Eigenschaften abrufen. Authentifizierung per Bearer-API-Key mit
	// MCP-Scope; komplett über die LCM-Einstellungen an-/abschaltbar (der
	// Manager startet/stoppt den Listener zur Laufzeit).
	mcpHandler := mcp.NewHandler(
		serverService.MCPProvider(),
		func(token string) bool {
			_, key, err := apiKeyService.Validate(token)
			return err == nil && key != nil && key.IsMCP()
		},
		"LCM", version.String(),
	)
	mcpServer := mcp.NewServer(mcpHandler, slog.Default()).WithGuards(
		middlewares.ResolveClientIP(proxyTrust),
		middlewares.IPAllowlist(networkAccess.Holder(netfilter.EndpointMCP), netfilter.EndpointMCP, slog.Default()),
	)
	// Gestartet wird MCP erst, wenn die Listener der übrigen Dienste vorbereitet
	// sind (startMCP): Mit TLS braucht es den Zertifikatspool. Es bindet immer mit
	// TLS und an den Host der Oberfläche.
	startMCP := func() {
		if addr, err := settingsService.Addresses(); err == nil && addr.MCPEnabled {
			_ = mcpServer.Apply(true, mcpBindHost(cfg.Host), addr.MCPPort, true) // Apply meldet Fehler selbst im Log
		}
	}

	// Update-Prüfung: Scheduler-Callback verdrahten (fragt periodisch die
	// GitLab-Releases ab) und einmal beim Start prüfen, damit das Banner ohne
	// Wartezeit auf den ersten Cron-Tick erscheint.
	scheduler.WithUpdateCheck(func() {
		if err := settingsService.CheckForUpdate(); err != nil {
			slog.Warn("update check failed", "error", err)
		}
	})
	safego.Go("update-check:initial", func() {
		if err := settingsService.CheckForUpdate(); err != nil {
			slog.Warn("initial update check failed", "error", err)
		}
	})

	// Onboarding-SSH-Key beim ersten Start erzeugen (idempotent) - der
	// Public Key wird in den Einstellungen angezeigt.
	if err := settingsService.EnsureOnboardingKey(); err != nil {
		slog.Warn("onboarding ssh key could not be created", "error", err)
	}

	// Enterprise-Subscription: Instanz-ID beim ersten Start erzeugen,
	// tägliches Lebenszeichen im Scheduler und ein Nachhol-Check beim Start
	// (der Cron zählt ab Prozessstart - häufig neu startende Instanzen
	// kämen sonst nie zu einer Prüfung).
	subscriptionService := services.NewSubscriptionService(settingsRepo, serverRepo, cipher, auditService).
		WithLcmHostRunner(serverService.RunLcmHostScript)
	if err := subscriptionService.EnsureInstanceID(); err != nil {
		slog.Warn("instance id could not be created", "error", err)
	}
	scheduler.WithSubscriptionCheck(subscriptionService.RunDailyCheck)

	// Selbst-Update: LCM spielt sein eigenes Paket auf dem LCM-Host ein,
	// sobald kein Job mehr läuft.
	selfUpdateService := services.NewSelfUpdateService(
		jobRepo, serverRepo, auditService, serverService.RunLcmHostJob, settingsService.UpdateStatus).
		WithBackup(func(actor string) (string, error) {
			// Passphrase aus Einstellungen/Umgebung - dieselbe Auflösung wie
			// beim geplanten Backup.
			b, err := backupService.Create(actor, "")
			if err != nil {
				return "", err
			}
			return b.FileName, nil
		}).WithMaintenanceGate(jobService).WithEvents(eventBus)
	safego.Go("subscription-check:initial", subscriptionService.CheckOnStartupIfStale)

	// Den eigenen Rechner in die Verwaltung aufnehmen, sofern das
	// Installationsskript die Zugangsdaten dafür hinterlegt hat. Läuft
	// bewusst synchron vor dem Start der Listener: Der Eintrag soll stehen,
	// bevor die erste Anfrage die Serverliste liest - sonst fehlte er beim
	// ersten Aufruf der Oberfläche noch. Der Vorgang dauert nur so lange wie
	// ein Host-Key-Probe auf localhost.
	//
	// Im Demo-Modus abgeschaltet: Dort steht bereits ein LCM-Host in den
	// Testdaten, und ein echter SSH-Zugriff auf die Entwicklungsmaschine wäre
	// unerwünscht.
	if !demo {
		selfReg := services.NewSelfRegisterService(serverRepo, settingsRepo, sshx.NewClient(), cipher, dataDir).
			WithRestrict(func(id uint) error {
				_, err := serverService.RestrictSudo(repositories.ScopeAll(), id, "system")
				return err
			})
		selfReg.Run()
		// Und danach ansagen, woran man ist: Eine bestehende Installation
		// bleibt im Voll-Modus, bis jemand sie umstellt - das gehoert ins
		// Protokoll, nicht in eine Annahme.
		selfReg.ReportHostMode()
	}

	// Nach einem Update den eigenen Host neu erfassen. Das neue Paket ist
	// installiert, die Verwaltung kennt aber noch den Bestand von vorher -
	// ohne diesen Lauf zeigte die Übersicht weiter die alte LCM-Version, bis
	// irgendwann der planmäßige Scan kommt. Ausgerechnet nach dem Update ist
	// das die Angabe, auf die jemand schaut.
	//
	// Asynchron und bewusst nach der Selbst-Registrierung: Der Scan spricht
	// über SSH mit dem Host und darf den Start nicht aufhalten.
	if updateResult.Updated && !demo {
		safego.Go("self-update:rescan", func() {
			host := services.LcmHostServer(serverRepo)
			if host == nil {
				return
			}
			if _, err := serverService.RefreshPackages(repositories.ScopeAll(), host.ID, "self-update"); err != nil {
				slog.Warn("package scan of the LCM host after self-update failed", "error", err)
			}
		})
	}

	// Erst-Erfassung nach einer NEUINSTALLATION. Die Selbst-Registrierung legt
	// den eigenen Host an, scannt ihn aber nicht - bis dahin steht er ohne
	// Betriebssystem, ohne Hardware und mit null Kernen in der Übersicht.
	// Bisher füllte ihn erst der nächtliche System-Sync um vier Uhr, also
	// ausgerechnet nicht in der Stunde nach der Installation, in der jemand
	// zum ersten Mal hinschaut.
	//
	// Dieselbe Bedingung wie bei frisch enrollten Agent-Servern (siehe
	// WithOnAgentOnline): noch kein OS erfasst ⇒ einmal alles erheben.
	// Asynchron, damit der Start nicht auf eine SSH-Sitzung wartet.
	if !demo {
		safego.Go("self-register:initial-scan", func() {
			host := services.LcmHostServer(serverRepo)
			if host == nil || host.OSName != "" {
				return
			}
			slog.Info("initial scan of the LCM host (not yet recorded)", "server", host.Name)
			if _, err := serverService.RefreshAll(repositories.ScopeAll(), host.ID, "system:self-register"); err != nil {
				slog.Warn("initial scan of the LCM host failed", "error", err)
			}
		})
	}

	// Beim Start die Integrität des Audit-Logs prüfen (Hash-Chain).
	auditService.VerifyChainOnStartup()

	// Interner Cronjob starten (Health-Check, System-Sync, Backup, Rules).
	if err := scheduler.Start(); err != nil {
		return fmt.Errorf("scheduler starten: %w", err)
	}
	defer scheduler.Stop()

	frontendFS, err := ui.FS()
	if err != nil {
		return err
	}

	// restart wird nach einem vorbereiteten Restore aufgerufen: der HTTP-Server
	// wird sauber gestoppt und der Prozess mit einem Nicht-Null-Code beendet,
	// damit ein Supervisor (systemd Restart=on-failure, Docker) neu startet und
	// ApplyStagedRestore beim Start greift. Ohne Supervisor bleibt der Restore
	// vorbereitet und wird beim nächsten manuellen Start angewendet.
	var app *fiber.App
	restart := func() {
		slog.Warn("restore prepared - restart LCM to apply it")
		if app != nil {
			_ = app.ShutdownWithTimeout(shutdownTimeout)
		}
		os.Exit(restoreRestartExitCode)
	}

	if trust := proxyTrust.Load(); trust.Enabled && trust.Proxies.IsEmpty() {
		// Ohne Liste vertraut LCM niemandem (netfilter.ProxyTrust). Wer das
		// früher als „jeder Peer" eingerichtet hat, sieht sonst nur, dass
		// hinter dem Proxy plötzlich alle Clients dieselbe Adresse haben.
		slog.Warn("trust_proxy_header ist an, aber trusted_proxies ist leer - LCM vertraut damit keinem Proxy " +
			"und ignoriert X-Forwarded-For und X-Forwarded-Proto: Als Client-Adresse gilt die direkte Verbindung " +
			"(für IP-Filter, Anmeldesperre und Protokolle), HTTPS nur bei TLS direkt an LCM. Abhilfe: die " +
			"Adressen des Reverse-Proxys in trusted_proxies eintragen (z.B. [\"127.0.0.1\"] oder [\"10.0.0.5/32\"]) " +
			"oder trust_proxy_header ausschalten")
	}

	// healthCheckTimeout begrenzt eine einzelne Selbstprüfung. Deutlich unter der
	// Frist, die der Monitor selbst darüberlegt (health.checkTimeout) - so meldet
	// die Prüfung ihr eigenes Scheitern, statt von außen abgeschnitten zu werden.
	const healthCheckTimeout = 5 * time.Second

	// Laufzeit-Selbstüberwachung. Sie stellt ZWEI Fragen, weil es zwei
	// verschiedene Störungen gibt - und nur auf eine davon ein Neustart die
	// richtige Antwort ist:
	//
	//  1. Ist die Datenbank erreichbar? Wenn nein, kann LCM gar nichts mehr
	//     und nimmt trotzdem weiter HTTP an - dann ist der Neustart richtig.
	//  2. Nimmt sie Schreibvorgänge an? Ein Ping allein beantwortet das
	//     NICHT: Im WAL-Modus kommen Leser durch, während die Schreibsperre
	//     bei einem fremden Vorgang liegt. In genau dieser Lücke meldete der
	//     Dienst im Test minutenlang „operational" und konnte dabei keine
	//     einzige Zeile schreiben - zwölf geplante Läufe fielen spurlos aus.
	//     Hier meldet er jetzt „degraded", startet aber NICHT neu: Die Sperre
	//     liegt außerhalb, ein neuer Prozess stünde vor derselben.
	healthMonitor := health.NewMonitor(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
		defer cancel()
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			return err
		}
		if err := storage.ProbeWritable(ctx, db); err != nil {
			return fmt.Errorf("%w: %v", health.ErrNotWritable, err)
		}
		return nil
	})

	dashboardService := services.NewDashboardService(serverService, jobRepo, alertRepo, groupRepo, eventBus)
	portScanService := services.NewPortScanService(serverRepo, repositories.NewPortScanRepository(db), jobService, auditService)

	// Messung der Datenbank (GET /api/v1/system/db, /metrics). Die Zeilen je
	// Tabelle zählt sie stündlich selbst, alles andere fällt nebenbei an.
	dbReporter := dbmetrics.NewReporter(dbmetrics.Default, db, dbPath)
	safego.Go("db-table-counter", func() { dbReporter.CountTablesEvery(context.Background(), time.Hour) })

	app = router.New(router.Deps{
		LogFile:                  logFile,
		Restart:                  restart,
		Health:                   healthMonitor,
		DemoPublic:               cfg.DemoPublic,
		UIAccess:                 networkAccess.Holder(netfilter.EndpointUI),
		Network:                  networkAccess,
		ACME:                     acmeService,
		TLS:                      tlsAPI{reloader: func() *certReloader { return reloader }},
		ProxyTrust:               proxyTrust,
		Auth:                     authService,
		APIKeys:                  apiKeyService,
		Users:                    userService,
		Servers:                  serverService,
		Jobs:                     jobService,
		Audit:                    auditService,
		Groups:                   groupService,
		Scheduler:                scheduler,
		Backups:                  backupService,
		Packages:                 packageService,
		Settings:                 settingsService,
		Provisioning:             provService,
		Activation:               activationService,
		LinuxUsers:               linuxUserService,
		SSHLogs:                  sshLogService,
		TOTP:                     totpService,
		System:                   systemService,
		CustomActions:            customActionService,
		Profiles:                 profileService,
		ProfileBlocks:            blockService,
		Apps:                     appService,
		RunAppAction:             executor.RunAppAction,
		Notifications:            notificationService,
		Alerts:                   alertService,
		Subscription:             subscriptionService,
		Advisories:               advisoryService,
		SelfUpdate:               selfUpdateService,
		FrontendFS:               frontendFS,
		Logger:                   logger,
		AccessLog:                cfg.AccessLog,
		APIKeyRateLimitPerMinute: cfg.APIKeyRateLimitPerMinute,
		AgentBinDirs:             []string{"/usr/share/lcm/agent", "bin"},
		Events:                   eventBus,
		Dashboard:                dashboardService,
		ReleaseNotes:             releaseNotes,
		DBMetrics:                dbReporter,
		TLSCACert:                certRenewal.CAPEM,
		PortScans:                portScanService,
	})

	// LCM Remote: Der Agent-WebSocket (MQTT) läuft auf einem EIGENEN,
	// dedizierten Listener - nicht auf dem UI/REST-Port. Das Gateway gibt es,
	// sobald der Hub existiert; ob es auf einem Port lauscht (agent_port != 0)
	// oder am gemeinsamen TLS-Port, entscheiden die Einstellungen zur Laufzeit.
	var agentApp *fiber.App
	if agentHub != nil {
		agentApp = router.NewAgentGateway(remote.WSHandler(agentHub, proxyTrust), proxyTrust,
			networkAccess.Holder(netfilter.EndpointAgent), slog.Default())
	}

	// Graceful Shutdown bei SIGINT/SIGTERM (wichtig für Service-Betrieb). Der
	// Hauptstrang wartet auf shutdownDone: Die Listener laufen in eigenen
	// Goroutinen, weil ihre Ports zur Laufzeit wechseln.
	shutdownDone := make(chan struct{})
	var portsToClose portCloser
	safego.Go("signal-handler", func() {
		defer close(shutdownDone)
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		sig := <-quit
		// Gegenstück zum Start-Marker: ein sauberes Beenden (Signal) erzeugt
		// diese Zeile. Ein Absturz tut das NICHT - daran erkennt man ihn.
		slog.Info("=== LCM service stopping ===", "signal", sig.String())
		health.NotifyStopping()
		// Erst die Ports freigeben, dann die Apps mit Frist herunterfahren: Ein
		// hängender Client hält den Stopp nicht bis zum Timeout von systemd auf.
		portsToClose.CloseAll()
		mcpServer.Shutdown()
		acmeService.Stop()
		shutdownApps(shutdownTimeout, agentApp, app)
	})

	// Selbstüberwachung starten: meldet systemd die Betriebsbereitschaft
	// (Type=notify), sendet danach regelmäßig Lebenszeichen - aber nur solange
	// die Datenbank erreichbar ist - und erzwingt einen Neustart, wenn der
	// Prozess dauerhaft gestört oder durch gehäufte Panics instabil ist.
	healthMonitor.Start("LCM v" + version.String())

	// HTTPS by default: Beim ersten Start entsteht eine eigene CA, die das
	// Server-Zertifikat ausstellt. Eigene Zertifikate (tls_certificates in der
	// config.json, Uploads) kommen über den Pool dazu. Unverschlüsseltes HTTP
	// ist nur im Entwicklungsmodus (--dev) erlaubt.
	if dev {
		slog.Warn("DEVELOPMENT MODE: unencrypted HTTP active (--dev)",
			"version", version.String(), "address", "http://"+cfg.Address())
		if cfg.SharedTLSListen != "" {
			slog.Warn("shared_tls_listen is ignored in development mode (--dev): there is no TLS")
		}
		closeControl := startControlSocket(dataDir, nil, networkAccess)
		defer closeControl()
		startMCP()
		listeners, err := startListeners(listenerSetup{
			cfg: cfg, dataDir: dataDir, settings: settingsService, app: app, agentApp: agentApp, mcp: mcpServer,
			agentGate: agentHub.SetAccepting,
		})
		if err != nil {
			return err
		}
		defer listeners.Close()
		portsToClose.Add(listeners.Close)
		<-shutdownDone
		return nil
	}

	// Die CA stellt das Server-Zertifikat aus: die des Betreibers
	// (tls_ca_cert/tls_ca_key) oder die eigene, beim ersten Start erzeugt.
	ca, err := tlsx.OpenCA(dataDir, cfg.TLSCACert, cfg.TLSCAKey, buildTime())
	if err != nil {
		return fmt.Errorf("tls-ca vorbereiten: %w", err)
	}
	certRenewal.SetCA(ca)
	certPath, keyPath, err := tlsx.EnsureServerCert(dataDir, ca, buildTime())
	if err != nil {
		return fmt.Errorf("tls-zertifikat vorbereiten: %w", err)
	}
	// Das Zertifikat der CA ist der Rückfall im Pool. Beide Listener holen
	// ihr Zertifikat bei jedem Handshake aus dem Pool - so nimmt die
	// Erneuerung ein neues ohne Neustart in Betrieb, ebenso ein Neuladen
	// (`lcm tls reload`, SIGHUP).
	certStore, err = tlsx.NewCertStore(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("tls-zertifikat laden: %w", err)
	}
	certPool = tlsx.NewPool(certStore)
	publicAddrs := func() publicAddresses { return storedPublicAddresses(settingsService) }
	reloader = newCertReloader(certPool, cfg, configPath, dataDir, func(actor, action, details string) {
		auditService.Log(actor, action, "settings", 1, details)
	}, publicAddrs)
	mcpServer.WithTLS(certPool.TLSConfig)
	sharedPort := newSharedPort(certPool, publicAddrs)
	sharedPort.AttachMCP(mcpServer)
	reloader.shared = sharedPort.Status
	defer sharedPort.Close()
	portsToClose.Add(sharedPort.Close)
	reloader.Reload("")
	closeControl := startControlSocket(dataDir, reloader, networkAccess)
	defer closeControl()
	reloadOnSIGHUP(reloader)
	startMCP()
	acmeService.MigrateNames()
	acmeService.EnsureCoverage()
	safego.Go("acme-renewal", acmeService.RunDaily)
	// Erste Prüfung vor den Listenern: Steht der Umstieg auf die CA an,
	// verbinden sich die Agenten nach dem Neustart gleich mit dem neuen
	// Zertifikat und lernen die CA (services.CertRenewal).
	certRenewal.Run()
	safego.Go("tls-cert-renewal", certRenewal.RunDaily)
	// LCM Remote: der dedizierte Agent-Listener nutzt DEMSELBEN Zertifikatspool
	// wie die UI - ohne passenden Namen liefert er das Zertifikat der CA, die
	// der Agent pinnt. Er läuft nebenläufig zum UI/REST-Listener; ein Fehler
	// dort beendet nicht den Hauptdienst.
	listeners, err := startListeners(listenerSetup{
		cfg: cfg, dataDir: dataDir, settings: settingsService, app: app, agentApp: agentApp,
		mcp: mcpServer, shared: sharedPort, tlsConfig: certPool.TLSConfig, agentGate: agentHub.SetAccepting,
	})
	if err != nil {
		return err
	}
	defer listeners.Close()
	portsToClose.Add(listeners.Close)
	slog.Info("LCM started (HTTPS)",
		"version", version.String(),
		"address", "https://"+listeners.ui.current(),
		"log_level", cfg.LogLevel,
	)
	<-shutdownDone
	return nil
}

// buildTime liefert einen stabilen NotBefore-Zeitpunkt für CA und
// Server-Zertifikat (aktuelle Zeit, minus Puffer gegen Uhr-Skew).
func buildTime() time.Time {
	return time.Now().Add(-1 * time.Hour)
}
