// Package router definiert alle API-Routen und verdrahtet Controller
// mit Middlewares. Neue Feature-Routen werden hier registriert.
package router

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"

	"LCM/internal/api/controllers"
	"LCM/internal/api/middlewares"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/health"
	"LCM/internal/netfilter"
	"LCM/internal/remote/wire"
	"LCM/internal/version"
)

// Deps bündelt alles, was der Router zum Verdrahten braucht.
type Deps struct {
	Auth          *services.AuthService
	APIKeys       *services.APIKeyService
	Users         *services.UserService
	Servers       *services.ServerService
	Jobs          *services.JobService
	Audit         *services.AuditService
	Groups        *services.GroupService
	Scheduler     *services.Scheduler
	Backups       *services.BackupService
	Packages      *services.PackageService
	Settings      *services.SettingsService
	Provisioning  *services.ProvisioningService
	Activation    *services.ActivationService
	LinuxUsers    *services.LinuxUserService
	SSHLogs       *services.SSHLogService
	TOTP          *services.TOTPService
	System        *services.SystemService
	CustomActions *services.CustomActionService
	Profiles      *services.PrivilegeProfileService
	ProfileBlocks *services.ProfileBlockService
	Apps          *services.AppService
	// RunAppAction löst Sicherung/Update einer erkannten Anwendung aus
	// (Executor). Optional - ohne sie bleibt der Reiter rein lesend.
	RunAppAction  func(server *domain.Server, slug, kind string, withBackup bool, actor string) (*domain.Job, error)
	Notifications *services.NotificationService
	Alerts        *services.AlertService
	Subscription  *services.SubscriptionService
	Advisories    *services.AdvisoryService
	// SelfUpdate spielt das eigene Debian-Paket auf dem LCM-Host ein
	// (nil in Tests => die Endpunkte melden „nicht eingerichtet").
	SelfUpdate *services.SelfUpdateService
	// FrontendFS ist der eingebettete Vite-dist-Ordner (nil in Tests).
	FrontendFS fs.FS
	// Logger für das Access-Log; nil => slog.Default().
	Logger *slog.Logger
	// AccessLog aktiviert das Request-Logging.
	AccessLog bool
	// APIKeyRateLimitPerMinute: Requests pro API-Key und Minute (0 = aus).
	APIKeyRateLimitPerMinute int
	// IPAllowlist beschränkt den Zugriff auf zugelassene Client-Adressen
	// (leerer Matcher = keine Einschränkung, bisheriges Verhalten).
	IPAllowlist netfilter.Allowlist
	// TrustProxyHeader lässt den IP-Filter die Client-IP aus X-Forwarded-For
	// nehmen statt aus der direkten Verbindung (nur hinter vertrauenswürdigem
	// Reverse-Proxy sinnvoll).
	TrustProxyHeader bool
	// Restart löst einen Prozess-Neustart aus (für Auto-Apply eines Restores).
	// nil in Tests / wenn kein Supervisor den Prozess neu startet.
	Restart func()
	// Health ist die Laufzeit-Selbstüberwachung; speist den /health-Endpunkt.
	// nil in Tests => der Endpunkt antwortet unbesehen mit "ok".
	Health *health.Monitor
	// AgentBinDirs sind die Suchpfade für die auslieferbaren lcm-agent-
	// Binaries (Download-Endpunkt). leer/nil => Route liefert 404.
	AgentBinDirs []string
	// DemoPublic aktiviert die Sperren der öffentlichen Demo (DemoGuard).
	DemoPublic bool
}

// New erstellt die Fiber-App mit allen Routen und Middlewares.
func New(deps Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName: "LCM",
		// Fiber-Fehler (fiber.NewError) werden als JSON serialisiert.
		ErrorHandler: jsonErrorHandler,
		// ReadTimeout begrenzt das Lesen von Request-Header/-Body (Schutz
		// gegen Slowloris); IdleTimeout schließt untätige Keep-Alive-
		// Verbindungen.
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
		// WriteTimeout großzügig, aber NICHT unbegrenzt: einige Handler führen
		// synchron SSH-Onboarding/Härtung aus und dürfen legitim lange laufen.
		// Ohne jede Grenze könnte ein Client, der Antworten nur sehr langsam
		// liest (Slow-Read), Verbindungen und Goroutinen dauerhaft binden.
		WriteTimeout: 15 * time.Minute,
		// Obergrenze gleichzeitiger Verbindungen. Der Fiber-Default (262144)
		// überlässt die Grenze faktisch dem Datei-Deskriptor-Limit des
		// Prozesses; ein realistischer Wert macht die Ressourcenobergrenze
		// vorhersagbar.
		Concurrency: 4096,
		// Request-Body-Limit. Fiber kennt kein Limit pro Route, deshalb muss
		// dieser eine Wert auch den größten legitimen Upload abdecken: das
		// Backup-Archiv beim Restore (siehe controllers.maxRestoreUpload -
		// die beiden Werte MÜSSEN zusammenpassen). Der Fiber-Default von
		// 4 MiB hatte den Restore in der Praxis unmöglich gemacht: fasthttp
		// wies jedes reale Archiv mit 413 ab, bevor der Handler lief.
		BodyLimit: controllers.MaxUploadBytes,
	})

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	app.Use(recover.New())
	// IP-Allowlist so früh wie möglich (nach recover): nicht zugelassene
	// Clients werden mit 403 abgewiesen, bevor Logging, Auth oder Controller
	// laufen. Nur registrieren, wenn konfiguriert - sonst kein Overhead.
	if !deps.IPAllowlist.IsEmpty() {
		app.Use(middlewares.IPAllowlist(deps.IPAllowlist, deps.TrustProxyHeader, logger))
	}
	if deps.AccessLog {
		app.Use(middlewares.AccessLog(logger))
	}
	app.Use(middlewares.SecurityHeaders())
	// Rate-Limit VOR der Key-Validierung: drosselt auch Brute-Force
	// mit ungültigen Keys. Betrifft nur Requests mit X-API-Key-Header.
	app.Use(middlewares.APIKeyRateLimit(deps.APIKeyRateLimitPerMinute))
	// Authenticate hängt den User an den Kontext, bricht aber nie ab -
	// Zugriffskontrolle machen RequireAuth/RequirePermission pro Route.
	// (Ausnahme: read-only API-Keys mit Schreibzugriff => sofort 403.)
	app.Use(middlewares.Authenticate(deps.Auth, deps.APIKeys))
	// Erzwingt Passwortänderung bzw. 2FA-Einrichtung, bevor ein Konto in
	// diesem Zustand andere Endpunkte nutzen darf (server-seitig, nicht nur
	// als Frontend-Hinweis). Läuft nach Authenticate.
	app.Use(middlewares.AccountRemediation(deps.Settings))

	// EIN Brute-Force-Zähler für alle Controller: Anmelde- und TOTP-Sperren
	// müssen endpunktübergreifend greifen (Login, 2FA-Deaktivierung,
	// Passwortwechsel prüfen alle denselben zweiten Faktor).
	loginGuard := controllers.NewLoginGuard()
	authCtrl := controllers.NewAuthController(deps.Auth, deps.TOTP, deps.Settings, deps.Activation, loginGuard, deps.TrustProxyHeader)
	userCtrl := controllers.NewUserController(deps.Users, deps.TOTP, loginGuard)
	apiKeyCtrl := controllers.NewAPIKeyController(deps.APIKeys)
	systemCtrl := controllers.NewSystemController(deps.System)
	serverCtrl := controllers.NewServerController(deps.Servers)
	jobCtrl := controllers.NewJobController(deps.Jobs, deps.Audit)
	groupCtrl := controllers.NewGroupController(deps.Groups, deps.Scheduler)
	opsCtrl := controllers.NewOpsController(deps.Scheduler, deps.Groups, deps.Backups, deps.Packages, deps.Settings, deps.Servers, deps.Restart).
		WithSelfUpdate(deps.SelfUpdate)
	provCtrl := controllers.NewProvisioningController(deps.Provisioning, deps.Activation, deps.Servers)
	linuxCtrl := controllers.NewLinuxUserController(deps.LinuxUsers)
	sshLogCtrl := controllers.NewSSHLogController(deps.SSHLogs)
	customActionCtrl := controllers.NewCustomActionController(deps.CustomActions)
	profileCtrl := controllers.NewPrivilegeProfileController(deps.Profiles)
	blockCtrl := controllers.NewProfileBlockController(deps.ProfileBlocks)
	appCtrl := controllers.NewAppController(deps.Apps).WithActions(deps.Servers, deps.RunAppAction)
	notificationCtrl := controllers.NewNotificationController(deps.Notifications)
	alertCtrl := controllers.NewAlertController(deps.Alerts)

	// LCM Remote: Die MQTT-über-WebSocket-Schnittstelle der Agents liegt
	// BEWUSST NICHT auf diesem Listener, sondern auf einem eigenen, dedizierten
	// Agent-Port (siehe NewAgentGateway). So bietet der UI/REST-Port keine
	// Agent-Schnittstelle und der Agent-Port keine UI/REST.

	api := app.Group("/api/v1")

	// Mitgelieferte Anwender-Doku (öffentlich): Die Anleitung zum Einrichten
	// des SSH-Schlüssels braucht man, BEVOR man einen Zugang hat - etwa aus
	// der Aktivierungs-Mail heraus. Die Seiten stecken im Binary und
	// enthalten nichts Vertrauliches.
	docsCtrl := controllers.NewDocsController()
	api.Get("/docs", docsCtrl.List)
	api.Get("/docs/:slug", docsCtrl.Get)

	// Health-Check (öffentlich, z.B. für Service-Monitoring, den
	// Docker-HEALTHCHECK und Reverse-Proxy-Prüfungen).
	//
	// Die Prüfung fasst hier tatsächlich an, was der Dienst zum Arbeiten
	// braucht (Datenbank-Ping). Eine reine „ok"-Antwort wäre wertlos: Ein
	// Prozess mit unerreichbarer Datenbank würde weiterhin als gesund gelten
	// und niemals neu gestartet.
	//
	// Detailtiefe wie bei /system/info: Anonyme Aufrufer bekommen nur den
	// Zustand, angemeldete zusätzlich die Diagnosewerte. Fehlertexte können
	// Pfade oder Datenbank-Interna enthalten und gehören nicht nach außen.
	api.Get("/health", func(c fiber.Ctx) error {
		if deps.Health == nil {
			return c.JSON(fiber.Map{"status": "ok"})
		}
		healthy := deps.Health.Probe() == nil
		status := "ok"
		code := fiber.StatusOK
		if !healthy {
			status, code = "unhealthy", fiber.StatusServiceUnavailable
		}
		if middlewares.CurrentUser(c) == nil {
			return c.Status(code).JSON(fiber.Map{"status": status})
		}
		return c.Status(code).JSON(fiber.Map{"status": status, "details": deps.Health.Status()})
	})

	// LCM Remote: lcm-agent-Binary für die manuelle Installation (öffentlich,
	// von der IP-Allowlist ausgenommen - siehe controllers.AgentDownload).
	api.Get("/agent/download/:arch", controllers.AgentDownload(deps.AgentBinDirs))

	// Öffentliche Demo: Sperren für Zugangs-Manipulation, Daten-Abfluss und
	// ausgehende Verbindungen (siehe middlewares.DemoGuard).
	if deps.DemoPublic {
		api.Use(middlewares.DemoGuard())
	}

	// System-Info: Version, Build, Uptime (Controller ohne DB-Schicht).
	api.Get("/system/info", systemCtrl.Info)
	// Update-Status (installiert vs. neueste bekannte Version) fürs Banner -
	// jeder eingeloggte Nutzer; die manuelle Prüfung ist Admin-Sache.
	api.Get("/system/update-info", middlewares.RequireAuth(), opsCtrl.UpdateInfo)
	api.Post("/system/update-check", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.CheckUpdateNow)
	// Selbst-Update des LCM-Hosts: Zustand lesen und anstoßen. Beides
	// Admin-Sache - der Vorgang startet den Dienst neu.
	api.Get("/system/self-update", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.SelfUpdateStatus)
	api.Post("/system/self-update", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.StartSelfUpdate)

	// Hinweis zu Fiber v3: Handler laufen in Angabe-Reihenfolge -
	// Middlewares stehen deshalb VOR dem Controller-Handler.

	// Auth (Login öffentlich, 2FA-Verwaltung authentifiziert). Der
	// Brute-Force-Schutz (IP-Fehlversuchs-Sperre + TOTP-Konto-Sperre) sitzt
	// im AuthController (login_guard.go).
	auth := api.Group("/auth")
	auth.Post("/login", authCtrl.Login)
	auth.Post("/login/2fa", authCtrl.LoginTOTP)
	// Self-Service-Passwort-Reset (öffentlich, pro IP gedrosselt): mailt
	// einen kurzlebigen Aktivierungslink über den Standard-E-Mail-Versand.
	auth.Post("/password-reset", authCtrl.RequestPasswordReset)
	auth.Post("/logout", middlewares.RequireAuth(), authCtrl.Logout)
	auth.Post("/2fa/setup", middlewares.RequireAuth(), authCtrl.SetupTOTP)
	auth.Post("/2fa/enable", middlewares.RequireAuth(), authCtrl.EnableTOTP)
	auth.Post("/2fa/disable", middlewares.RequireAuth(), authCtrl.DisableTOTP)
	auth.Get("/me", middlewares.RequireAuth(), authCtrl.Me)

	// LCM-User-Management (Login-Benutzer). Profil-Edit und Passwort-Reset
	// prüfen zusätzlich Self-Edit im Controller (nicht nur users:write).
	users := api.Group("/users")
	users.Get("/", middlewares.RequirePermission(domain.PermUsersRead), userCtrl.List)
	// Öffentliche Aktivierung (kein Login) - VOR der :id-Route registrieren.
	users.Post("/activation-links/consume", provCtrl.ConsumeActivationLink)
	users.Post("/activation-links/generate", middlewares.RequirePermission(domain.PermUsersWrite), provCtrl.GenerateActivationLink)
	users.Get("/:id", middlewares.RequirePermission(domain.PermUsersRead), userCtrl.Get)
	users.Post("/", middlewares.RequirePermission(domain.PermUsersWrite), userCtrl.Create)
	users.Patch("/:id/profile", middlewares.RequireAuth(), userCtrl.UpdateProfile)
	users.Post("/:id/reset-password", middlewares.RequireAuth(), userCtrl.ResetPassword)
	users.Put("/:id/roles", middlewares.RequirePermission(domain.PermRolesWrite), userCtrl.UpdateRoles)
	users.Patch("/:id/active", middlewares.RequirePermission(domain.PermUsersWrite), userCtrl.SetActive)
	users.Delete("/:id", middlewares.RequirePermission(domain.PermUsersWrite), userCtrl.Delete)
	api.Get("/roles", middlewares.RequirePermission(domain.PermRolesRead), userCtrl.ListRoles)

	// API-Keys (Service-Kommunikation)
	apiKeys := api.Group("/apikeys", middlewares.RequirePermission(domain.PermAPIKeysManage))
	apiKeys.Get("/", apiKeyCtrl.List)
	apiKeys.Post("/", apiKeyCtrl.Create)
	apiKeys.Delete("/:id", apiKeyCtrl.Revoke)

	// Server-Management (Onboarding, Monitoring, Härtung). Queries mit
	// servers:read, Commands mit servers:write; die Daten-Sichtbarkeit
	// wird zusätzlich pro Query auf die Gruppen des Users eingeschränkt.
	servers := api.Group("/servers")
	servers.Get("/", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.List)
	servers.Post("/probe", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.Probe)
	servers.Post("/join", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.Join)
	// LCM Remote: Agent-Server anlegen (Enrollment-Token) + Token erneuern.
	servers.Post("/agent", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.CreateAgent)
	servers.Post("/routeros", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.CreateRouterOS)
	// Synology DSM: Zertifikat bestätigen, dann per Web-API aufnehmen.
	servers.Post("/dsm/probe", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ProbeDSM)
	servers.Post("/dsm", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.CreateDSM)
	servers.Post("/:id/agent-token", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RegenerateAgentToken)
	// Statischer Katalog - VOR der :id-Route registrieren.
	servers.Get("/known-repos", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.KnownRepos)
	servers.Get("/:id", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.Get)
	servers.Get("/:id/status", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.Status)
	servers.Get("/:id/active-job", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.ActiveJob)
	servers.Get("/:id/hardware", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.Hardware)
	servers.Get("/:id/packages", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.Packages)
	servers.Get("/:id/snaps", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.Snaps)
	servers.Post("/:id/snaps/refresh-all", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RefreshAllSnaps)
	servers.Post("/:id/snaps/refresh", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RefreshSnaps)
	servers.Post("/:id/snaps/remove", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RemoveSnaps)
	servers.Get("/:id/vulnerabilities", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.Vulnerabilities)
	servers.Post("/:id/vulnerabilities/scan", middlewares.RequirePermission(domain.PermServersWrite), opsCtrl.ScanServerVulnerabilities)
	servers.Get("/:id/outdated-packages", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.OutdatedPackages)
	servers.Get("/:id/storage-history", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.StorageHistory)
	servers.Get("/:id/packages/:name/versions", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.PackageVersions)
	servers.Post("/:id/refresh-hardware", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RefreshHardware)
	servers.Post("/:id/refresh-all", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RefreshAll)
	servers.Post("/:id/packages/refresh", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RefreshPackages)
	servers.Post("/:id/packages/upgrade-all", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.UpgradeAllPackages)
	servers.Post("/:id/packages/upgrade-security", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.UpgradeSecurityPackages)
	servers.Post("/:id/packages/update", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.UpdatePackages)
	servers.Post("/:id/packages/autoremove", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.AutoremovePackages)
	servers.Post("/:id/packages/remove", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RemovePackages)
	servers.Post("/:id/kernels/cleanup", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RemoveOldKernels)
	// Paket-Pins: Schutz vor dem Aufräumen (Autoremove) und optionale
	// Versions-Fixierung. Lesen darf, wer den Server sieht; Ändern und das
	// Anwenden auf dem Ziel brauchen Schreibrecht.
	servers.Get("/:id/packages/pins", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.ListPackagePins)
	servers.Post("/:id/packages/pins", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.CreatePackagePin)
	servers.Delete("/:id/packages/pins/:pinId", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.DeletePackagePin)
	servers.Post("/:id/packages/pins/kernel", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.PinKernelPreset)
	servers.Post("/:id/packages/pins/apply", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ApplyPackagePins)
	// LCM-Host (localhost): Einrichtungsstatus + Ein-Klick-Installation von
	// Trivy bzw. apt-cacher-ng.
	servers.Get("/:id/lcm-host/status", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.LcmHostStatus)
	servers.Post("/:id/lcm-host/install-trivy", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.InstallTrivy)
	servers.Post("/:id/lcm-host/install-sandbox", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.InstallSandbox)
	servers.Post("/:id/lcm-host/install-apt-cacher", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.InstallAptCacher)
	servers.Post("/:id/lcm-host/install-crowdsec-lapi", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.InstallCrowdSecLapi)
	servers.Get("/:id/apt-cache/detail", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.AptCacherDetail)
	servers.Post("/:id/apt-cache/restart", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RestartAptCacher)
	servers.Post("/:id/apt-cache/permanent-cache", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.SetAptCacherPermanentCache)
	servers.Get("/:id/docker", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.Docker)
	servers.Post("/:id/docker/refresh", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RefreshDocker)
	servers.Post("/:id/docker/compose-update", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.UpdateCompose)
	servers.Post("/:id/docker/pull", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.PullDockerImage)
	servers.Post("/:id/docker/pull-all", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.PullAllDockerImages)
	servers.Post("/:id/docker/cve-relevance", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.SetContainerCVERelevance)
	servers.Post("/:id/docker/remove-image", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RemoveDockerImage)
	servers.Post("/:id/docker/prune", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.PruneDockerImages)
	servers.Get("/:id/repositories", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.Repositories)
	servers.Get("/:id/ssh-sessions", middlewares.RequirePermission(domain.PermJobsRead), sshLogCtrl.ServerSessions)
	servers.Patch("/:id/settings", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.UpdateSettings)
	servers.Post("/:id/assign-user", middlewares.RequirePermission(domain.PermServersWrite), provCtrl.AssignUser)
	servers.Post("/:id/remove-user", middlewares.RequirePermission(domain.PermServersWrite), provCtrl.RemoveUser)
	servers.Post("/:id/sync-users", middlewares.RequirePermission(domain.PermServersWrite), provCtrl.SyncUsers)
	// Benutzer-Übersicht: die gescannten Linux-Konten des Zielsystems.
	servers.Get("/:id/users", middlewares.RequirePermission(domain.PermServersRead), provCtrl.ServerUsers)
	servers.Get("/:id/users/:username/logins", middlewares.RequirePermission(domain.PermServersRead), provCtrl.ServerUserLogins)
	servers.Get("/:id/users/pending", middlewares.RequirePermission(domain.PermServersRead), provCtrl.PendingUserSyncs)
	servers.Post("/:id/users/refresh", middlewares.RequirePermission(domain.PermServersWrite), provCtrl.RefreshServerUsers)
	servers.Post("/:id/users/disable", middlewares.RequirePermission(domain.PermServersWrite), provCtrl.DisableServerUser)
	servers.Post("/:id/users/enable", middlewares.RequirePermission(domain.PermServersWrite), provCtrl.EnableServerUser)
	servers.Post("/:id/users/remove", middlewares.RequirePermission(domain.PermServersWrite), provCtrl.RemoveServerUser)
	servers.Post("/:id/repositories/secure", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.SecureRepositories)
	servers.Post("/:id/repositories/revert-https", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RevertRepositoriesHTTPS)
	servers.Get("/:id/apps", middlewares.RequirePermission(domain.PermServersRead), appCtrl.ServerApps)
	servers.Post("/:id/apps/:slug/:action", middlewares.RequirePermission(domain.PermServersWrite), appCtrl.RunAppAction)
	servers.Post("/:id/repositories/add", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.AddRepository)
	servers.Post("/:id/harden-ssh", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.HardenSSH)
	servers.Post("/:id/unharden-ssh", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.UnhardenSSH)
	servers.Post("/:id/restrict-sudo", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RestrictSudo)
	servers.Post("/:id/ssh-root-login", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.SetSSHRootLogin)
	servers.Post("/:id/ssh-port", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ChangeSSHPort)
	servers.Post("/:id/firewall", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ConfigureFirewall)
	servers.Post("/:id/listening-ports/scan", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ScanListeningPorts)
	servers.Post("/:id/apt-proxy", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ConfigureAptProxy)
	servers.Post("/:id/dns", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ConfigureDNS)
	servers.Post("/:id/dns-test", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.DNSTest)
	// Zeit: Prüfen ist lesend (servers:read), Setzen verändert (servers:write).
	servers.Post("/:id/time-check", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.TimeState)
	servers.Post("/:id/timezone", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.SetTimezone)
	servers.Post("/:id/ntp", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ConfigureNTP)
	servers.Get("/:id/deep-scan", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.DeepScanReport)
	servers.Post("/:id/deep-scan", middlewares.RequirePermission(domain.PermServersWrite), opsCtrl.DeepScanServer)
	servers.Get("/:id/deep-scan/reports/:reportId", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.DeepScanReportDetail)
	servers.Post("/:id/deep-scan/install-tools", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.InstallDeepScanTools)
	servers.Post("/:id/acl/install", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.InstallACLSupport)
	servers.Get("/:id/hardened-paths", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.HardenedPaths)
	servers.Get("/:id/harden-suggestions", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.HardenSuggestions)
	servers.Post("/:id/hardened-paths", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.HardenPath)
	servers.Post("/:id/hardened-paths/bulk", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.HardenPathsBulk)
	servers.Delete("/:id/hardened-paths/:pathId", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RestorePath)
	servers.Post("/:id/security-tool", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ConfigureSecurityTool)
	servers.Post("/:id/ssh-2fa", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ConfigureSSH2FA)
	// Verwaltung bereits installierter Werkzeuge. Die Sperrliste ist bewusst
	// nur servers:read - sie zu SEHEN ist harmlos, das Entsperren geht über
	// die schreibende Route.
	servers.Post("/:id/security-tool/manage", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.ManageSecurityTool)
	servers.Get("/:id/security-tool/bans", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.SecurityToolBans)
	servers.Post("/:id/reconnect", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.Reconnect)
	servers.Post("/:id/rotate-key", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.RotateKey)
	servers.Post("/:id/reboot", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.Reboot)
	servers.Post("/:id/decommission", middlewares.RequirePermission(domain.PermServersWrite), serverCtrl.Decommission)

	// Jobs & Audit
	api.Get("/jobs/history", middlewares.RequirePermission(domain.PermJobsRead), jobCtrl.History)
	api.Get("/jobs/filter-options", middlewares.RequirePermission(domain.PermJobsRead), jobCtrl.FilterOptions)
	api.Get("/jobs/:id/ssh-output", middlewares.RequirePermission(domain.PermJobsRead), jobCtrl.ConsoleOutput)
	// Abbrechen eines laufenden Jobs = Eingriff in den Server-Betrieb - daher
	// servers:write statt jobs:read.
	api.Post("/jobs/:id/abort", middlewares.RequirePermission(domain.PermServersWrite), jobCtrl.Abort)
	api.Get("/jobs/:id/ssh-sessions", middlewares.RequirePermission(domain.PermJobsRead), sshLogCtrl.JobSessions)
	api.Get("/ssh-sessions/:id", middlewares.RequirePermission(domain.PermJobsRead), sshLogCtrl.Session)
	api.Get("/audit", middlewares.RequirePermission(domain.PermAuditRead), jobCtrl.AuditLog)

	// Servergruppen
	groups := api.Group("/server-groups")
	groups.Get("/", middlewares.RequirePermission(domain.PermGroupsRead), groupCtrl.List)
	groups.Post("/create", middlewares.RequirePermission(domain.PermGroupsWrite), groupCtrl.Create)
	groups.Get("/:id", middlewares.RequirePermission(domain.PermGroupsRead), groupCtrl.Get)
	groups.Patch("/:id/settings", middlewares.RequirePermission(domain.PermGroupsWrite), groupCtrl.UpdateSettings)
	groups.Post("/:id/assign-server", middlewares.RequirePermission(domain.PermGroupsWrite), groupCtrl.AssignServer)
	groups.Post("/:id/remove-server", middlewares.RequirePermission(domain.PermGroupsWrite), groupCtrl.RemoveServer)
	// Betreuer der Gruppe - Gegenstück zu assign-/remove-server. Erst darüber
	// sieht ein Benutzer der Manager-Rolle überhaupt Server (Tenant Isolation).
	groups.Post("/:id/assign-manager", middlewares.RequirePermission(domain.PermGroupsWrite), groupCtrl.AssignManager)
	groups.Post("/:id/remove-manager", middlewares.RequirePermission(domain.PermGroupsWrite), groupCtrl.RemoveManager)
	groups.Post("/:id/disband", middlewares.RequirePermission(domain.PermGroupsWrite), groupCtrl.Disband)
	// Schedules & Rules einer Gruppe
	groups.Get("/:id/schedules", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.ListSchedules)
	groups.Post("/:id/schedules/define", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.DefineSchedule)
	groups.Get("/:id/rules", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.ListRules)
	groups.Post("/:id/rules/define", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.DefineRule)

	// Linux-Benutzer (Betriebssystem-Accounts der verwalteten Server).
	linux := api.Group("/linux-users")
	linux.Get("/", middlewares.RequirePermission(domain.PermLinuxUsersRead), linuxCtrl.List)
	// Öffentliche Aktivierung (kein Login) - VOR der :id-Route registrieren.
	linux.Post("/activation-links/consume", linuxCtrl.ConsumeActivation)
	linux.Post("/", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.Create)
	linux.Delete("/keys/:keyId", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.RemoveKey)
	linux.Get("/:id", middlewares.RequirePermission(domain.PermLinuxUsersRead), linuxCtrl.Get)
	linux.Patch("/:id", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.Update)
	linux.Delete("/:id", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.Delete)
	linux.Post("/:id/keys", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.AddKey)
	linux.Post("/:id/keys/generate", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.GenerateKey)
	linux.Post("/:id/activation-links/generate", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.GenerateActivation)
	linux.Post("/:id/remove-from-servers", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.RemoveFromServers)
	linux.Post("/:id/assign-group", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.AssignGroup)
	linux.Post("/:id/group-profile", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.SetGroupProfile)
	linux.Get("/:id/group-assignments", middlewares.RequirePermission(domain.PermLinuxUsersRead), linuxCtrl.GroupAssignments)
	linux.Post("/:id/remove-group", middlewares.RequirePermission(domain.PermLinuxUsersWrite), linuxCtrl.RemoveGroup)

	// Rules (rule-ID adressiert)
	// Custom-Aktionen (wiederverwendbare Command-Listen für Rules)
	// Custom-Aktionen enthalten beliebige Shell-Kommandos, die der Executor
	// als ROOT auf den Zielservern ausführt. Sie sind global (nicht einer
	// Gruppe zugeordnet) und damit NICHT mandantengetrennt.
	//
	// Deshalb: Lesen darf jeder mit rules:manage (Manager brauchen die Liste,
	// um eine Aktion für ihre Gruppen-Regel auszuwählen), ÄNDERN aber nur
	// settings:manage (Admin). Sonst könnte ein Manager von Mandant A den
	// Inhalt einer Aktion überschreiben, die der Zeitplan von Mandant B als
	// root auf dessen Servern ausführt - eine mandantenübergreifende
	// Rechteausweitung bis zu Root-Codeausführung.
	custom := api.Group("/custom-actions")
	custom.Get("/", middlewares.RequirePermission(domain.PermRulesManage), customActionCtrl.List)
	custom.Post("/", middlewares.RequirePermission(domain.PermSettingsManage), customActionCtrl.Create)
	custom.Patch("/:id", middlewares.RequirePermission(domain.PermSettingsManage), customActionCtrl.Update)
	custom.Delete("/:id", middlewares.RequirePermission(domain.PermSettingsManage), customActionCtrl.Delete)

	// Berechtigungsprofile: benannte Rechtebündel für Linux-Benutzer.
	// Lesen darf, wer Linux-Benutzer sieht; definieren nur admin - ein Profil
	// beschreibt Root-Rechte auf Zielsystemen und gilt mandantenübergreifend.
	profiles := api.Group("/privilege-profiles")
	profiles.Get("/", middlewares.RequirePermission(domain.PermProfilesRead), profileCtrl.List)
	profiles.Get("/:id", middlewares.RequirePermission(domain.PermProfilesRead), profileCtrl.Get)
	profiles.Post("/", middlewares.RequirePermission(domain.PermProfilesWrite), profileCtrl.Create)
	profiles.Post("/:id/clone", middlewares.RequirePermission(domain.PermProfilesWrite), profileCtrl.Clone)
	profiles.Patch("/:id", middlewares.RequirePermission(domain.PermProfilesWrite), profileCtrl.Update)
	profiles.Delete("/:id", middlewares.RequirePermission(domain.PermProfilesWrite), profileCtrl.Delete)

	// Regelbausteine: wiederverwendbare Rechte-Vorlagen für die Profile.
	blocks := api.Group("/profile-blocks")
	blocks.Get("/", middlewares.RequirePermission(domain.PermProfilesRead), blockCtrl.List)
	blocks.Get("/:id/usage", middlewares.RequirePermission(domain.PermProfilesRead), blockCtrl.Usage)
	blocks.Post("/:id/preview", middlewares.RequirePermission(domain.PermProfilesRead), blockCtrl.Preview)
	blocks.Post("/", middlewares.RequirePermission(domain.PermProfilesWrite), blockCtrl.Create)
	blocks.Post("/:id/clone", middlewares.RequirePermission(domain.PermProfilesWrite), blockCtrl.Clone)
	blocks.Patch("/:id", middlewares.RequirePermission(domain.PermProfilesWrite), blockCtrl.Update)
	blocks.Delete("/:id", middlewares.RequirePermission(domain.PermProfilesWrite), blockCtrl.Delete)

	// Anwendungskatalog: Software, die nicht aus der Paketverwaltung stammt.
	// Die Einträge tragen Kommandos, die auf jedem passenden Server als root
	// laufen - deshalb dieselbe Hürde wie die globalen Einstellungen.
	apps := api.Group("/apps", middlewares.RequirePermission(domain.PermSettingsManage))
	apps.Get("/", appCtrl.ListApps)
	apps.Post("/", appCtrl.CreateApp)
	apps.Put("/:id", appCtrl.UpdateApp)
	apps.Delete("/:id", appCtrl.DeleteApp)

	// Benachrichtigungskanäle (Notification-Service, Provider-Instanzen)
	notifications := api.Group("/notification-channels", middlewares.RequirePermission(domain.PermNotificationsManage))
	notifications.Get("/", notificationCtrl.List)
	notifications.Post("/", notificationCtrl.Create)
	notifications.Patch("/:id", notificationCtrl.Update)
	notifications.Delete("/:id", notificationCtrl.Delete)
	notifications.Post("/:id/test", notificationCtrl.Test)

	// Alarmregeln, Alarm-Historie und manuelle Auswertung
	alertRules := api.Group("/alert-rules", middlewares.RequirePermission(domain.PermAlertsManage))
	alertRules.Get("/", alertCtrl.List)
	alertRules.Post("/", alertCtrl.Create)
	alertRules.Patch("/:id", alertCtrl.Update)
	alertRules.Delete("/:id", alertCtrl.Delete)
	api.Get("/alert-events", middlewares.RequirePermission(domain.PermAlertsManage), alertCtrl.Events)
	api.Post("/alerts/evaluate", middlewares.RequirePermission(domain.PermAlertsManage), alertCtrl.Evaluate)

	// Globale Regel-Sicht (R2-085): alle Regeln aller sichtbaren Gruppen.
	api.Get("/rules", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.ListAllRules)
	api.Patch("/rules/:id/update", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.UpdateRule)
	api.Delete("/rules/:id/remove", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.RemoveRule)
	api.Post("/rules/:id/enable", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.EnableRule)
	api.Post("/rules/:id/disable", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.DisableRule)
	api.Post("/rules/:id/trigger-now", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.TriggerRule)

	// Schedules (schedule-ID adressiert)
	api.Patch("/schedules/:id/update", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.UpdateSchedule)
	api.Delete("/schedules/:id/remove", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.RemoveSchedule)
	api.Post("/schedules/:id/enable", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.EnableSchedule)
	api.Post("/schedules/:id/disable", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.DisableSchedule)
	api.Post("/schedules/:id/trigger-now", middlewares.RequirePermission(domain.PermRulesManage), groupCtrl.TriggerSchedule)

	// Globale Paket-/Vulnerability-Übersicht
	api.Get("/packages/overview", middlewares.RequirePermission(domain.PermPackagesRead), opsCtrl.PackageOverview)
	api.Get("/packages/vulnerable", middlewares.RequirePermission(domain.PermPackagesRead), opsCtrl.VulnerablePackages)
	api.Get("/security/vulnerabilities", middlewares.RequirePermission(domain.PermPackagesRead), opsCtrl.Vulnerabilities)
	// Sammel-Update aller VMs (Security-Seite): Status abrufen + starten.
	api.Get("/security/update-all", middlewares.RequirePermission(domain.PermPackagesRead), opsCtrl.BulkUpdateStatus)
	api.Post("/security/update-all", middlewares.RequirePermission(domain.PermServersWrite), opsCtrl.StartBulkUpdate)
	api.Get("/docker/overview", middlewares.RequirePermission(domain.PermPackagesRead), opsCtrl.DockerOverview)
	api.Get("/docker/containers", middlewares.RequirePermission(domain.PermPackagesRead), opsCtrl.DockerContainers)
	api.Get("/docker/compose", middlewares.RequirePermission(domain.PermPackagesRead), opsCtrl.DockerCompose)
	// Stand des CVE-Scanners (Version + Schwachstellen-Datenbank). Lesen darf,
	// wer CVEs sehen darf; das Nachladen der Datenbank greift in den LCM-Host
	// ein und braucht deshalb das Einstellungs-Recht.
	api.Get("/security/scanner", middlewares.RequirePermission(domain.PermPackagesRead), opsCtrl.ScannerStatus)
	api.Post("/security/scanner/update-db", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.UpdateCVEDB)

	// Fruehwarnung (OSV): Befunde lesen darf, wer CVEs sehen darf; das
	// Quittieren aendert den Alarm-Zustand und braucht Schreibrecht. Nur
	// registriert, wenn der Dienst verdrahtet ist (nil in schlanken Tests).
	if deps.Advisories != nil {
		advCtrl := controllers.NewAdvisoryController(deps.Advisories,
			deps.Scheduler.TriggerAdvisoryPoll, deps.Scheduler.TriggerAdvisoryMirror)
		api.Get("/security/advisories", middlewares.RequirePermission(domain.PermPackagesRead), advCtrl.List)
		api.Get("/security/advisories/status", middlewares.RequirePermission(domain.PermPackagesRead), advCtrl.Status)
		api.Get("/security/caches", middlewares.RequirePermission(domain.PermPackagesRead), advCtrl.Caches)
		api.Post("/security/advisories/poll", middlewares.RequirePermission(domain.PermServersWrite), advCtrl.Poll)
		// Der Spiegellauf laedt zig Megabyte auf den LCM-Host - Betreiber-Sache.
		api.Post("/security/advisories/mirror", middlewares.RequirePermission(domain.PermSettingsManage), advCtrl.Mirror)
		api.Post("/security/advisories/:id/acknowledge", middlewares.RequirePermission(domain.PermServersWrite), advCtrl.Acknowledge)
	}

	// Enterprise-Subscription (Betreiber-Sache → settings:manage). Nur
	// registriert, wenn der Service verdrahtet ist (nil in schlanken Tests).
	if deps.Subscription != nil {
		subCtrl := controllers.NewSubscriptionController(deps.Subscription)
		api.Get("/subscription", middlewares.RequirePermission(domain.PermSettingsManage), subCtrl.Status)
		api.Post("/subscription/activate", middlewares.RequirePermission(domain.PermSettingsManage), subCtrl.Activate)
		api.Post("/subscription/verify", middlewares.RequirePermission(domain.PermSettingsManage), subCtrl.Verify)
		api.Post("/subscription/apt", middlewares.RequirePermission(domain.PermSettingsManage), subCtrl.SetAptChannel)
		api.Delete("/subscription", middlewares.RequirePermission(domain.PermSettingsManage), subCtrl.Remove)
	}

	// Globale Einstellungen
	api.Get("/settings/global", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.GetSettings)
	api.Patch("/settings/global", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.UpdateSettings)
	// Standard-E-Mail-Versand: Testnachricht + verwalteter Benachrichtigungs-
	// kanal (Checkbox in Einstellungen → Allgemein).
	api.Post("/settings/global/test-mail", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.TestSystemMail)
	api.Put("/settings/mcp", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.SetMCP)
	api.Get("/settings/mail-channel", middlewares.RequirePermission(domain.PermSettingsManage), notificationCtrl.GetSystemChannel)
	api.Put("/settings/mail-channel", middlewares.RequirePermission(domain.PermSettingsManage), notificationCtrl.SetSystemChannel)
	// Katalog bekannter Paketquellen (pflegbar; Lese-Sicht für das
	// Server-Detail: GET /servers/known-repos)
	api.Get("/settings/apt-cache/status", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.AptCacheStatus)
	// Zentrale CrowdSec-Seite: LAPI-Erreichbarkeits-Check (Login-Probe).
	api.Get("/settings/crowdsec/status", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.CrowdSecLapiStatus)
	// Zentrale APT-Cache-Seite: Übersicht (URL, Erreichbarkeit, Statistik,
	// Verwaltbarkeit auf dem LCM-Host). Neustart/permanentes Caching laufen über
	// die server-scoped /servers/:id/apt-cache/*-Endpunkte mit der zurückgegebenen server_id.
	api.Get("/settings/apt-cache/overview", middlewares.RequirePermission(domain.PermSettingsManage), serverCtrl.AptCacheOverview)
	api.Get("/settings/known-repos", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.ListKnownRepos)
	api.Post("/settings/known-repos", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.SaveKnownRepo)
	api.Delete("/settings/known-repos/:id", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.DeleteKnownRepo)

	// IP-Allowlists (gemeinsamer Pool): Verwaltung unter settings:manage, plus
	// eine servers:read-Leseroute für die Auswahl in Firewall-/Security-Tools.
	api.Get("/settings/ip-allowlists", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.ListIPAllowlists)
	api.Post("/settings/ip-allowlists", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.SaveIPAllowlist)
	api.Delete("/settings/ip-allowlists/:id", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.DeleteIPAllowlist)
	api.Get("/ip-allowlists", middlewares.RequirePermission(domain.PermServersRead), opsCtrl.ListIPAllowlists)

	// System-Scheduler & Backups. Diese /system/*-Endpunkte wirken
	// system-/mandantenübergreifend (Gesamt-Übersicht, globale/fremde
	// Schedules, systemweite Backup-/Cleanup-/CVE-/Alarm-Läufe) und sind
	// daher admin-only (settings:manage). Manager triggern die Schedules
	// IHRER Gruppen über den scope-geprüften Pfad /schedules/:id/trigger-now.
	api.Get("/system/schedules/overview", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.SchedulesOverview)
	api.Post("/system/schedules/kind/:kind/trigger-now", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.TriggerSystemSchedule)
	api.Post("/system/schedules/:id/trigger-now", middlewares.RequirePermission(domain.PermSettingsManage), opsCtrl.TriggerGlobalSchedule)
	api.Get("/system/backups", middlewares.RequirePermission(domain.PermBackupsManage), opsCtrl.ListBackups)
	api.Post("/system/backups/trigger-now", middlewares.RequirePermission(domain.PermBackupsManage), opsCtrl.TriggerBackup)
	api.Patch("/system/backups/settings", middlewares.RequirePermission(domain.PermBackupsManage), opsCtrl.ConfigureBackups)
	// Restore: hochgeladenes Archiv (auch auf frischer Instanz) oder aus der
	// Historie (Rollback). Download eines vorhandenen Backups.
	api.Post("/system/backups/restore-upload", middlewares.RequirePermission(domain.PermBackupsManage), opsCtrl.RestoreUpload)
	api.Get("/system/backups/:name/download", middlewares.RequirePermission(domain.PermBackupsManage), opsCtrl.DownloadBackup)
	api.Delete("/system/backups/:name", middlewares.RequirePermission(domain.PermBackupsManage), opsCtrl.DeleteBackup)
	api.Post("/system/backups/:name/restore", middlewares.RequirePermission(domain.PermBackupsManage), opsCtrl.RestoreBackup)

	// Unbekannte API-Pfade => 404 als JSON (nicht die SPA).
	api.Use(func(c fiber.Ctx) error {
		return fiber.NewError(fiber.StatusNotFound, "unbekannter API-Endpunkt")
	})

	// SPA: eingebettetes Frontend ausliefern; unbekannte Pfade fallen auf
	// index.html zurück (Client-Side Routing).
	//
	// Cache-Strategie (entscheidend, damit Updates ankommen):
	//   - /assets/* (content-gehashte Vite-Bundles) → 1 Jahr, unveränderlich.
	//   - ALLES andere (index.html, SPA-Routen, favicon/logo) → no-cache.
	// Der no-cache-Header MUSS auf demselben Handler sitzen, der die Datei
	// ausliefert: eine globale MaxAge auf dem Static-Handler überschreibt sonst
	// den no-cache, sodass der Browser die index.html ein Jahr festhält und
	// nach einem Update weiter die ALTEN Asset-Referenzen lädt (alte UI).
	if deps.FrontendFS != nil {
		assetHandler := static.New("", static.Config{FS: deps.FrontendFS, MaxAge: 31536000})
		indexHandler := static.New("index.html", static.Config{FS: deps.FrontendFS})
		fileHandler := static.New("", static.Config{FS: deps.FrontendFS, NotFoundHandler: indexHandler})
		// Der ETag hängt am INHALT der index.html, nicht nur an der
		// Build-Kennung. Version und Build stehen weiterhin darin - sie machen
		// den Wert lesbar -, entscheidend ist aber der Inhalts-Hash.
		//
		// Grund: In einem Entwicklungsbau bleibt die Kennung dauerhaft
		// „0.0.0-dev-0". Der Browser revalidierte brav, bekam auf seinen
		// bedingten Request ein 304 und behielt die ALTE index.html mit den
		// alten Asset-Referenzen - obwohl das Binary längst eine neue
		// Oberfläche mitbrachte. Auch „Neu laden" half nicht, weil der Reload
		// wieder im 304 endete. Genau der Fehler, den der Kommentar unten
		// schon einmal für die ModTime beschreibt, nur eine Ebene höher.
		buildETag := `"` + version.Version + "-" + version.Build + "-" + indexHash(deps.FrontendFS) + `"`
		app.Get("/*", func(c fiber.Ctx) error {
			if strings.HasPrefix(c.Path(), "/assets/") {
				return assetHandler(c)
			}
			// Der eingebettete FS trägt für JEDE Datei den Nullzeitpunkt als
			// ModTime; Fiber macht daraus ein Last-Modified, das über alle
			// Versionen hinweg identisch bleibt. Der Browser revalidierte
			// also brav (no-cache), bekam auf seinen bedingten Request ein
			// 304 und behielt die ALTE index.html mit den alten
			// Asset-Referenzen - nach einem Update lief die alte Oberfläche
			// weiter, und auch „Neu laden" änderte daran nichts, weil der
			// Reload wieder im 304 endete.
			//
			// Deshalb: die auf dem Nullzeitpunkt beruhende Aushandlung
			// abschalten und durch die Build-Kennung ersetzen. Gleicher Build
			// → 304 (spart die Übertragung), neuer Build → 200 mit der neuen
			// Datei.
			if c.Get(fiber.HeaderIfNoneMatch) == buildETag {
				return c.SendStatus(fiber.StatusNotModified)
			}
			c.Request().Header.Del(fiber.HeaderIfModifiedSince)
			c.Request().Header.Del(fiber.HeaderIfNoneMatch)
			// KEINE MaxAge auf diesem Handler → der no-cache bleibt erhalten.
			c.Set("Cache-Control", "no-cache")
			err := fileHandler(c)
			c.Response().Header.Del(fiber.HeaderLastModified)
			c.Set(fiber.HeaderETag, buildETag)
			return err
		})
	}

	return app
}

// NewAgentGateway baut die schlanke Fiber-App für den DEDIZIERTEN Agent-Port
// (LCM Remote). Sie bietet AUSSCHLIESSLICH den MQTT-über-WebSocket-Endpunkt
// (GET /mqtt) - keine UI, keine REST-API, keine statischen Assets. Damit ist
// die Agent-Schnittstelle vollständig vom UI/REST-Listener getrennt.
//
// Kein RequireAuth und keine IP-Allowlist: Agents haben keine JWTs (die
// Authentifizierung per AgentID + Token macht der Broker selbst im MQTT-
// CONNECT) und verbinden sich roamend von wechselnden IPs. Alles andere auf
// diesem Port liefert 404.
func NewAgentGateway(ws fiber.Handler, logger *slog.Logger) *fiber.App {
	if logger == nil {
		logger = slog.Default()
	}
	app := fiber.New(fiber.Config{
		AppName:      "LCM-Agent-Gateway",
		ErrorHandler: jsonErrorHandler,
		ReadTimeout:  30 * time.Second,
		// Kein IdleTimeout: die WebSocket-Verbindung eines Agents ist
		// langlebig und wird von der gekaperten Verbindung selbst gehalten.
	})
	app.Use(recover.New())
	app.Use(middlewares.SecurityHeaders())
	if ws != nil {
		app.Get(wire.WSPath, ws)
	}
	return app
}

// jsonErrorHandler serialisiert alle Fehler als {"error": "..."}.
// Interne Fehler werden nicht an den Client durchgereicht.
func jsonErrorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	message := "interner Serverfehler"
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
		message = e.Message
	}
	return c.Status(code).JSON(fiber.Map{"error": message})
}

// indexHash liefert einen kurzen Hash der eingebetteten index.html. Sie
// referenziert die Asset-Dateien mit ihrem Inhalts-Hash im Namen - ändert sich
// irgendetwas an der Oberfläche, ändert sich damit auch dieser Wert.
func indexHash(fsys fs.FS) string {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		// Ohne lesbare index.html gibt es nichts auszuhandeln; ein fester
		// Platzhalter ist besser als ein Absturz beim Start.
		return "noindex"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}
