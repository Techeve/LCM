package controllers

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// ServerController bedient das Server-Onboarding und -Management.
type ServerController struct {
	servers *services.ServerService
}

func NewServerController(servers *services.ServerService) *ServerController {
	return &ServerController{servers: servers}
}

// mapServerError übersetzt Server-spezifische Fehler in HTTP-Status.
func mapServerError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "server nicht gefunden")
	case errors.Is(err, services.ErrServerNameTaken), errors.Is(err, services.ErrServerHostTaken):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case errors.Is(err, services.ErrFingerprintMismatch), errors.Is(err, sshx.ErrHostKeyMismatch),
		errors.Is(err, services.ErrLoopbackInContainer):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrServerBusy),
		errors.Is(err, services.ErrProxmoxRestricted),
		errors.Is(err, services.ErrPackagePinsUnavailable),
		errors.Is(err, services.ErrPackagePinsNotWired),
		errors.Is(err, services.ErrRouterOSUnsupported),
		errors.Is(err, services.ErrDSMUnsupported),
		errors.Is(err, services.ErrFirewallToolMissing),
		errors.Is(err, services.ErrRestrictedSudo),
		errors.Is(err, services.ErrAgentTransport),
		errors.Is(err, services.ErrNotAgentServer),
		errors.Is(err, services.ErrAgentOffline),
		errors.Is(err, services.ErrNoOldKernels),
		errors.Is(err, services.ErrNoSnapd),
		errors.Is(err, services.ErrNoRevertCandidates),
		errors.Is(err, services.ErrKernelCleanupUnsupported):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case errors.Is(err, sshx.ErrPasswordAuthUnavailable), errors.Is(err, sshx.ErrPasswordRejected),
		errors.Is(err, services.ErrNotRevertible),
		errors.Is(err, services.ErrNoOnboardingKey):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrNotLcmHost), errors.Is(err, services.ErrLcmHostNotApt),
		errors.Is(err, services.ErrLcmHostInContainer):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrUnknownSecurityTool), errors.Is(err, services.ErrInvalidAllowlistIP),
		errors.Is(err, services.ErrCrowdSecLapiMissing), errors.Is(err, services.ErrCrowdSecConsoleMissing),
		errors.Is(err, services.ErrInvalidLapiMode), errors.Is(err, services.ErrSecurityToolAction),
		errors.Is(err, services.ErrCrowdSecUnsupported):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrInvalidFirewallRules), errors.Is(err, services.ErrFirewallNeedsFullSudo):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrNoAptCacheURL),
		errors.Is(err, services.ErrTooManyDNSServers), errors.Is(err, services.ErrInvalidDNSServer),
		errors.Is(err, services.ErrInvalidTimezone), errors.Is(err, services.ErrInvalidNTPServer),
		errors.Is(err, services.ErrTooManyNTPServers):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrClockFromHost):
		// Kein Eingabefehler und kein Ziel-Fehler: die Aktion ergibt auf
		// diesem Server prinzipiell keinen Sinn - dasselbe Muster wie bei
		// gesperrten Aktionen auf Proxmox.
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case errors.Is(err, services.ErrNTPNotSynced):
		// Eigener Zustand, kein Eingabefehler: die Zeitserver stehen, nur der
		// Nachweis fehlt. 502 sagt „auf dem Zielsystem nicht erreicht" -
		// dasselbe Muster wie bei der unbelegten SSH-Härtung.
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	case errors.Is(err, services.ErrNoSnaps), errors.Is(err, services.ErrInvalidSnap),
		errors.Is(err, services.ErrProtectedSnap):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrNoPackages), errors.Is(err, services.ErrInvalidPackage),
		errors.Is(err, services.ErrInvalidVersion), errors.Is(err, services.ErrVersionOnePkg),
		errors.Is(err, services.ErrProtectedPackage),
		errors.Is(err, services.ErrPinnedPackage),
		errors.Is(err, services.ErrPackagePinName), errors.Is(err, services.ErrPackagePinEffect),
		errors.Is(err, services.ErrUnknownRepo),
		errors.Is(err, services.ErrRepoPackageManagerMismatch):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrInvalidComposeName), errors.Is(err, services.ErrInvalidDockerRef),
		errors.Is(err, services.ErrComposeUnavailable), errors.Is(err, services.ErrComposeProjectUnknown),
		errors.Is(err, services.ErrDockerImageUnknown), errors.Is(err, services.ErrDockerImageInUse),
		errors.Is(err, services.ErrDockerUnavailable), errors.Is(err, services.ErrComposePathMissing),
		errors.Is(err, services.ErrDockerContainerUnknown), errors.Is(err, services.ErrNoImagesToUpdate),
		errors.Is(err, services.ErrDockerUpdatesDisabled):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	default:
		return err
	}
}

// mapServerActionError mappt Fehler der SSH-Aktions-Handler: bekannte
// Service-Fehler (404, 409, 422, …) laufen über mapServerError; alles
// Übrige - typischerweise Verbindungs-/Zielsystem-Fehler - wird als
// 502 Bad Gateway gemeldet. Vorher gingen gemappte Fehler wie
// ErrProxmoxRestricted oder ErrServerBusy in diesen Handlern fälschlich
// als 502 raus.
func mapServerActionError(err error) error {
	if mapped := mapServerError(err); mapped != err { //nolint:errorlint - bewusster Identitätsvergleich (Default liefert err unverändert)
		return mapped
	}
	return fiber.NewError(fiber.StatusBadGateway, err.Error())
}

type probeRequest struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Probe - POST /api/v1/servers/probe (servers:write)
// Liest den Host-Key-Fingerprint für die Bestätigung im Join-Wizard.
func (ctrl *ServerController) Probe(c fiber.Ctx) error {
	var req probeRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Host == "" {
		return fiber.NewError(fiber.StatusBadRequest, "host ist erforderlich")
	}
	res, err := ctrl.servers.Probe(req.Host, req.Port)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.JSON(res)
}

type joinRequest struct {
	Name                 string `json:"name"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	LoginUser            string `json:"login_user"`
	LoginPassword        string `json:"login_password"`
	AuthMethod           string `json:"auth_method"` // "password" (Default) oder "key"
	ConfirmedFingerprint string `json:"confirmed_fingerprint"`
	RestrictedAccess     bool   `json:"restricted_access"` // eingeschränkter Service-User
}

// Join - POST /api/v1/servers/join (servers:write)
func (ctrl *ServerController) Join(c fiber.Ctx) error {
	var req joinRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Name == "" || req.Host == "" || req.LoginUser == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name, host und login_user sind erforderlich")
	}
	server, err := ctrl.servers.Join(services.JoinRequest{
		Name:                 req.Name,
		Host:                 req.Host,
		Port:                 req.Port,
		LoginUser:            req.LoginUser,
		LoginPassword:        req.LoginPassword,
		AuthMethod:           req.AuthMethod,
		ConfirmedFingerprint: req.ConfirmedFingerprint,
		RestrictedAccess:     req.RestrictedAccess,
		Actor:                actor(c),
	})
	if err != nil {
		return mapServerActionError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(server)
}

// CreateAgent - POST /api/v1/servers/agent (servers:write)
// Legt einen Agent-Server (LCM Remote) an und liefert das Enrollment-Token -
// EINMALIG, at rest liegt nur der Hash. Der Server ist zunächst offline und
// geht online, sobald sich der lcm-agent mit dem Token verbindet.
func (ctrl *ServerController) CreateAgent(c fiber.Ctx) error {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name ist erforderlich")
	}
	server, token, err := ctrl.servers.CreateAgentServer(req.Name, actor(c))
	if err != nil {
		return mapServerError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"server": server, "token": token})
}

// CreateRouterOS - POST /api/v1/servers/routeros (servers:write)
// Legt ein MikroTik-RouterOS-Gerät zur reinen Überwachung an. Passwort-Modus
// verbindet sofort und scannt; Key-Modus liefert einen Public Key zum Import.
func (ctrl *ServerController) CreateRouterOS(c fiber.Ctx) error {
	var req struct {
		Name                 string `json:"name"`
		Host                 string `json:"host"`
		Port                 int    `json:"port"`
		LoginUser            string `json:"login_user"`
		LoginPassword        string `json:"login_password"`
		AuthMethod           string `json:"auth_method"`
		ConfirmedFingerprint string `json:"confirmed_fingerprint"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	res, err := ctrl.servers.CreateRouterOSServer(services.RouterOSRequest{
		Name:                 req.Name,
		Host:                 req.Host,
		Port:                 req.Port,
		LoginUser:            req.LoginUser,
		LoginPassword:        req.LoginPassword,
		AuthMethod:           req.AuthMethod,
		ConfirmedFingerprint: req.ConfirmedFingerprint,
		Actor:                actor(c),
	})
	if err != nil {
		return mapServerError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(res)
}

// ProbeDSM - POST /api/v1/servers/dsm/probe (servers:write)
// Liest den TLS-Zertifikats-Fingerprint eines DSM-Geräts für die Bestätigung
// im Onboarding-Dialog - BEVOR Zugangsdaten übertragen werden (DSM liefert ab
// Werk ein selbstsigniertes Zertifikat, das Pinning ist hier der MitM-Schutz).
func (ctrl *ServerController) ProbeDSM(c fiber.Ctx) error {
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Host == "" {
		return fiber.NewError(fiber.StatusBadRequest, "host ist erforderlich")
	}
	fp, err := ctrl.servers.ProbeDSM(req.Host, req.Port)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.JSON(fiber.Map{"cert_fingerprint": fp})
}

// CreateDSM - POST /api/v1/servers/dsm (servers:write)
// Nimmt ein Synology-DSM-Gerät zur Überwachung über die DSM-Web-API auf.
func (ctrl *ServerController) CreateDSM(c fiber.Ctx) error {
	var req struct {
		Name                 string `json:"name"`
		Host                 string `json:"host"`
		Port                 int    `json:"port"`
		Account              string `json:"account"`
		Password             string `json:"password"`
		ConfirmedFingerprint string `json:"confirmed_fingerprint"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	server, err := ctrl.servers.CreateDSMServer(services.DSMRequest{
		Name: req.Name, Host: req.Host, Port: req.Port,
		Account: req.Account, Password: req.Password,
		ConfirmedFingerprint: req.ConfirmedFingerprint,
		Actor:                actor(c),
	})
	if err != nil {
		return mapServerError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(server)
}

// RegenerateAgentToken - POST /api/v1/servers/:id/agent-token (servers:write)
// Ersetzt das Agent-Token (Verlust/Kompromittierung). Der alte Token ist
// sofort ungültig, eine aktive Agent-Session wird getrennt.
func (ctrl *ServerController) RegenerateAgentToken(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	token, err := ctrl.servers.RegenerateAgentToken(scopeFor(c), id, actor(c))
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(fiber.Map{"token": token})
}

type reconnectRequest struct {
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	LoginUser            string `json:"login_user"`
	LoginPassword        string `json:"login_password"`
	AuthMethod           string `json:"auth_method"` // "password" (Default) oder "key"
	ConfirmedFingerprint string `json:"confirmed_fingerprint"`
	RestrictedAccess     bool   `json:"restricted_access"`
}

// Reconnect - POST /api/v1/servers/:id/reconnect (servers:write)
// Stellt die Verbindung zu einem bestehenden Server neu her und überschreibt
// dessen Credentials (geänderte Anmeldedaten oder ausgetauschter Server).
func (ctrl *ServerController) Reconnect(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	// Alle Felder sind optional (leer = bisherige Werte beibehalten), deshalb
	// ist auch ein leerer Rumpf zulässig - sonst verdeckte ein Parse-Fehler
	// die aussagekräftige Meldung des Services (BUG-029).
	var req reconnectRequest
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	server, err := ctrl.servers.Reconnect(scopeFor(c), services.ReconnectRequest{
		ID:                   id,
		Host:                 req.Host,
		Port:                 req.Port,
		LoginUser:            req.LoginUser,
		LoginPassword:        req.LoginPassword,
		AuthMethod:           req.AuthMethod,
		ConfirmedFingerprint: req.ConfirmedFingerprint,
		RestrictedAccess:     req.RestrictedAccess,
		Actor:                actor(c),
	})
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(server)
}

// List - GET /api/v1/servers (servers:read)
func (ctrl *ServerController) List(c fiber.Ctx) error {
	servers, err := ctrl.servers.List(scopeFor(c))
	if err != nil {
		return err
	}
	return c.JSON(servers)
}

// Get - GET /api/v1/servers/:id (servers:read)
func (ctrl *ServerController) Get(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	server, err := ctrl.servers.Get(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	// firewall_backend: das wirksame Firewall-Backend (erkanntes Werkzeug,
	// sonst das für die Distribution vorgesehene) - für den Badge im Dialog.
	return c.JSON(struct {
		*domain.Server
		FirewallBackend string `json:"firewall_backend"`
	}{server, services.FirewallBackendFor(server)})
}

// ActiveJob - GET /api/v1/servers/:id/active-job (servers:read)
// Liefert den aktuell laufenden Job des Servers (null, wenn keiner läuft) -
// die UI zeigt damit die Job-Sperre an und deaktiviert Aktions-Buttons.
func (ctrl *ServerController) ActiveJob(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	job, err := ctrl.servers.ActiveJob(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(fiber.Map{"job": job})
}

// Status - GET /api/v1/servers/:id/status (servers:read)
func (ctrl *ServerController) Status(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	status, insights, osSupport, err := ctrl.servers.Status(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	// docker_port_exposures qualifiziert die Firewall-Anzeige: Container-Ports,
	// die an ufw vorbei erreichbar sind (Docker filtert vor der INPUT-Kette).
	exposures, _ := ctrl.servers.DockerPortExposures(scopeFor(c), id)
	// kernel: laufender Kernel + installierte Kernel-Pakete. Zusammengesetzt
	// statt roh gespeichert, weil erst der Abgleich beider Angaben etwas
	// aussagt (welcher laeuft, gibt es eine Rueckfallebene, fehlt ein Neustart).
	kernel, _ := ctrl.servers.KernelInfo(scopeFor(c), id)
	return c.JSON(fiber.Map{
		"status": status, "insights": insights, "os_support": osSupport,
		"docker_port_exposures": exposures,
		"kernel":                kernel,
	})
}

// Hardware - GET /api/v1/servers/:id/hardware (servers:read)
func (ctrl *ServerController) Hardware(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	server, err := ctrl.servers.Get(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(fiber.Map{
		"cpu_model":     server.CPUModel,
		"cpu_cores":     server.CPUCores,
		"mem_total_mb":  server.MemTotalMB,
		"mem_used_mb":   server.MemUsedMB,
		"disk_total_mb": server.DiskTotalMB,
		"disk_used_mb":  server.DiskUsedMB,
		"disk_percent":  server.DiskUsagePercent(),
		"ip_addresses":  server.IPAddresses,
	})
}

// Packages - GET /api/v1/servers/:id/packages (servers:read)
func (ctrl *ServerController) Packages(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	pkgs, err := ctrl.servers.Packages(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(pkgs)
}

// Snaps - GET /api/v1/servers/:id/snaps (servers:read)
func (ctrl *ServerController) Snaps(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	snaps, err := ctrl.servers.Snaps(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(snaps)
}

// Vulnerabilities - GET /api/v1/servers/:id/vulnerabilities (servers:read)
func (ctrl *ServerController) Vulnerabilities(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	report, err := ctrl.servers.Vulnerabilities(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(report)
}

// StorageHistory - GET /api/v1/servers/:id/storage-history (servers:read)
// Täglicher Verlauf der Festplattenbelegung (Kapazität + Nutzung).
func (ctrl *ServerController) StorageHistory(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	report, err := ctrl.servers.StorageHistory(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(report)
}

// OutdatedPackages - GET /api/v1/servers/:id/outdated-packages (servers:read)
func (ctrl *ServerController) OutdatedPackages(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	pkgs, err := ctrl.servers.OutdatedPackages(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(pkgs)
}

// Repositories - GET /api/v1/servers/:id/repositories (servers:read)
func (ctrl *ServerController) Repositories(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	repos, err := ctrl.servers.Repositories(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(repos)
}

// KnownRepos - GET /api/v1/servers/known-repos (servers:read)
// Katalog der bekannten Paketquellen (Docker, PostgreSQL, …) - pflegbar
// unter Einstellungen → Repositories.
func (ctrl *ServerController) KnownRepos(c fiber.Ctx) error {
	repos, err := ctrl.servers.KnownRepoCatalog()
	if err != nil {
		return err
	}
	return c.JSON(repos)
}

// SecureRepositories - POST /api/v1/servers/:id/repositories/secure
// (servers:write). Stellt alle http-Quellen auf https um (mit Rollback).
func (ctrl *ServerController) SecureRepositories(c fiber.Ctx) error {
	return ctrl.runServerAction(c, "secured", ctrl.servers.SecureRepositories)
}

// RevertRepositoriesHTTPS - POST /api/v1/servers/:id/repositories/revert-https
// (servers:write). Stellt Paketquellen wieder auf http zurück - nur die, die
// vor der LCM-Umstellung http waren. Ohne "uris" im Body alle davon.
func (ctrl *ServerController) RevertRepositoriesHTTPS(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		URIs []string `json:"uris"`
	}
	// Leerer Body ist der Normalfall („alle Kandidaten") - nur ein
	// vorhandener, aber kaputter Body ist ein Fehler.
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
		}
	}
	output, err := ctrl.servers.RevertRepositoriesHTTPS(scopeFor(c), id, req.URIs, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "reverted", "output": output})
}

// AddRepository - POST /api/v1/servers/:id/repositories/add (servers:write)
// Richtet eine bekannte Paketquelle aus dem Katalog ein.
func (ctrl *ServerController) AddRepository(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	output, err := ctrl.servers.AddKnownRepository(scopeFor(c), id, req.Key, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "added", "output": output})
}

// PackageVersions - GET /api/v1/servers/:id/packages/:name/versions (servers:read)
// Installierbare Versionen eines Pakets (neueste zuerst) für die Auswahl.
func (ctrl *ServerController) PackageVersions(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	versions, err := ctrl.servers.PackageVersions(scopeFor(c), id, c.Params("name"))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(versions)
}

// RefreshPackages - POST /api/v1/servers/:id/packages/refresh (servers:write)
// Aktualisiert die Paketliste (apt-get update + Bestandsaufnahme).
func (ctrl *ServerController) RefreshPackages(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RefreshPackages(scopeFor(c), id, actor(c))
	})
}

// Reboot - POST /api/v1/servers/:id/reboot (servers:write)
// Startet den Server neu. Braucht vollen Root-Zugriff (im eingeschränkten
// Sudo-Modus gesperrt).
func (ctrl *ServerController) Reboot(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.Reboot(scopeFor(c), id, actor(c))
	})
}

// RefreshHardware - POST /api/v1/servers/:id/refresh-hardware (servers:write)
// Liest Hardware-/OS-Fakten neu ein (kein Upgrade, kein Bestand-Replace).
func (ctrl *ServerController) RefreshHardware(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RefreshHardware(scopeFor(c), id, actor(c))
	})
}

// RefreshAll - POST /api/v1/servers/:id/refresh-all (servers:write)
// Liest Hardware, Pakete, Snaps, Repos, Docker, Speicher und den
// Firewall-/SSH-Status neu ein (reine Datenerfassung).
func (ctrl *ServerController) RefreshAll(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RefreshAll(scopeFor(c), id, actor(c))
	})
}

// UpgradeAllPackages - POST /api/v1/servers/:id/packages/upgrade-all (servers:write)
func (ctrl *ServerController) UpgradeAllPackages(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.UpgradeAllPackages(scopeFor(c), id, actor(c))
	})
}

// LcmHostStatus - GET /api/v1/servers/:id/lcm-host/status (servers:read)
// Einrichtungszustand des LCM-Hosts (Trivy, apt-cacher-ng).
func (ctrl *ServerController) LcmHostStatus(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	status, err := ctrl.servers.LcmHostStatus(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(status)
}

// InstallSandbox - POST /api/v1/servers/:id/lcm-host/install-sandbox (servers:write)
// Rüstet die Sandbox des CVE-Scanners nach (bubblewrap) - für Hosts, auf denen
// Trivy vor der Sandbox eingerichtet wurde.
func (ctrl *ServerController) InstallSandbox(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.InstallSandbox(scopeFor(c), id, actor(c))
	})
}

// InstallTrivy - POST /api/v1/servers/:id/lcm-host/install-trivy (servers:write)
func (ctrl *ServerController) InstallTrivy(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.InstallTrivy(scopeFor(c), id, actor(c))
	})
}

// InstallAptCacher - POST /api/v1/servers/:id/lcm-host/install-apt-cacher (servers:write)
func (ctrl *ServerController) InstallAptCacher(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.InstallAptCacher(scopeFor(c), id, actor(c))
	})
}

// InstallCrowdSecLapi - POST /api/v1/servers/:id/lcm-host/install-crowdsec-lapi
// (servers:write). Body {bouncer:bool}. Richtet den CrowdSec-LAPI-Server auf
// dem LCM-Host ein und verdrahtet die Zugangsdaten automatisch.
func (ctrl *ServerController) InstallCrowdSecLapi(c fiber.Ctx) error {
	req := struct {
		Bouncer bool `json:"bouncer"`
	}{}
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.InstallCrowdSecLapi(scopeFor(c), id, services.LapiInstallInput{Bouncer: req.Bouncer}, actor(c))
	})
}

// AptCacherDetail - GET /api/v1/servers/:id/apt-cache/detail (servers:read)
// Statistik-/Einstellungsseite: Installationsstatus, Live-Erreichbarkeit,
// Transfer-Statistiken und der "permanentes Caching"-Schalter.
func (ctrl *ServerController) AptCacherDetail(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	detail, err := ctrl.servers.AptCacherDetail(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(detail)
}

// AptCacheOverview - GET /api/v1/settings/apt-cache/overview (settings:manage)
// Datengrundlage der zentralen APT-Cache-Seite: konfigurierte URL,
// Live-Erreichbarkeit + Transfer-Statistiken sowie - falls apt-cacher-ng auf dem
// LCM-Host läuft - dessen Verwaltbarkeit (Server-ID, permanentes Caching,
// Platten-Belegung). Nicht server-scoped: der LCM-Host wird intern aufgelöst.
func (ctrl *ServerController) AptCacheOverview(c fiber.Ctx) error {
	overview, err := ctrl.servers.AptCacheOverview(scopeFor(c))
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(overview)
}

// RestartAptCacher - POST /api/v1/servers/:id/apt-cache/restart (servers:write)
func (ctrl *ServerController) RestartAptCacher(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RestartAptCacher(scopeFor(c), id, actor(c))
	})
}

// SetAptCacherPermanentCache - POST /api/v1/servers/:id/apt-cache/permanent-cache
// (servers:write). Body: {enabled}.
func (ctrl *ServerController) SetAptCacherPermanentCache(c fiber.Ctx) error {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.SetAptCacherPermanentCache(scopeFor(c), id, req.Enabled, actor(c))
	})
}

// UpgradeSecurityPackages - POST /api/v1/servers/:id/packages/upgrade-security (servers:write)
func (ctrl *ServerController) UpgradeSecurityPackages(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.UpgradeSecurityPackages(scopeFor(c), id, actor(c))
	})
}

// UpdatePackages - POST /api/v1/servers/:id/packages/update (servers:write)
// Body: {names: ["htop", …], version: ""}. Ist version gesetzt, muss names
// genau ein Paket enthalten (exakte Version, Downgrade erlaubt).
func (ctrl *ServerController) UpdatePackages(c fiber.Ctx) error {
	var req struct {
		Names   []string `json:"names"`
		Version string   `json:"version"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.UpdatePackages(scopeFor(c), id, req.Names, req.Version, actor(c))
	})
}

// AutoremovePackages - POST /api/v1/servers/:id/packages/autoremove (servers:write)
// Entfernt nicht mehr benötigte Pakete (apt autoremove und Pendants).
func (ctrl *ServerController) AutoremovePackages(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.AutoremovePackages(scopeFor(c), id, actor(c))
	})
}

// RemovePackages - POST /api/v1/servers/:id/packages/remove (servers:write)
// Body: {names: ["altpaket", …]}. Deinstalliert gezielt die genannten Pakete;
// geschützte Systempakete werden mit 422 abgelehnt.
func (ctrl *ServerController) RemovePackages(c fiber.Ctx) error {
	var req struct {
		Names []string `json:"names"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RemovePackages(scopeFor(c), id, req.Names, actor(c))
	})
}

// RemoveOldKernels - POST /api/v1/servers/:id/kernels/cleanup (servers:write)
// Entfernt die nicht mehr benötigten Kernel samt ihrer Begleitpakete. Was
// stehen bleibt (laufender Kernel, neuere, eine Rückfallebene), entscheidet
// domain.RemovableKernels - der Aufruf braucht keine Liste.
func (ctrl *ServerController) RemoveOldKernels(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RemoveOldKernels(scopeFor(c), id, actor(c))
	})
}

// RefreshAllSnaps - POST /api/v1/servers/:id/snaps/refresh-all (servers:write)
func (ctrl *ServerController) RefreshAllSnaps(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RefreshAllSnaps(scopeFor(c), id, actor(c))
	})
}

// RefreshSnaps - POST /api/v1/servers/:id/snaps/refresh (servers:write)
// Body: {names: ["firefox", …]}.
func (ctrl *ServerController) RefreshSnaps(c fiber.Ctx) error {
	names, err := snapNamesFromBody(c)
	if err != nil {
		return err
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RefreshSnaps(scopeFor(c), id, names, actor(c))
	})
}

// RemoveSnaps - POST /api/v1/servers/:id/snaps/remove (servers:write)
// Body: {names: ["firefox", …]}. snapd und die core-Basen werden abgelehnt.
func (ctrl *ServerController) RemoveSnaps(c fiber.Ctx) error {
	names, err := snapNamesFromBody(c)
	if err != nil {
		return err
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RemoveSnaps(scopeFor(c), id, names, actor(c))
	})
}

func snapNamesFromBody(c fiber.Ctx) ([]string, error) {
	var req struct {
		Names []string `json:"names"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	return req.Names, nil
}

// ListPackagePins - GET /api/v1/servers/:id/packages/pins (servers:read)
// Globale und serverspezifische Pins, Verfügbarkeit (Proxmox ist ausgenommen)
// und der Kernel-Vorschlag für die Paketverwaltung dieses Servers.
func (ctrl *ServerController) ListPackagePins(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	view, err := ctrl.servers.ListPackagePins(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(view)
}

// CreatePackagePin - POST /api/v1/servers/:id/packages/pins (servers:write)
// Body {name, no_remove, hold, note, global}. Legt einen Pin an; ein
// vorhandener Pin gleichen Namens im selben Geltungsbereich wird aktualisiert.
func (ctrl *ServerController) CreatePackagePin(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Name     string `json:"name"`
		NoRemove *bool  `json:"no_remove"`
		Hold     bool   `json:"hold"`
		Note     string `json:"note"`
		Global   bool   `json:"global"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	// Ohne Angabe ist „nicht entfernen" gemeint - das ist der Zweck der Pins.
	noRemove := true
	if req.NoRemove != nil {
		noRemove = *req.NoRemove
	}
	pin, err := ctrl.servers.CreatePackagePin(scopeFor(c), id, services.PackagePinInput{
		Name: req.Name, NoRemove: noRemove, Hold: req.Hold, Note: req.Note, Global: req.Global,
	}, actor(c))
	if err != nil {
		return mapServerError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(pin)
}

// DeletePackagePin - DELETE /api/v1/servers/:id/packages/pins/:pinId (servers:write)
func (ctrl *ServerController) DeletePackagePin(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	pinID, err := paramNamedID(c, "pinId")
	if err != nil {
		return err
	}
	if err := ctrl.servers.DeletePackagePin(scopeFor(c), id, pinID, actor(c)); err != nil {
		return mapServerError(err)
	}
	return c.JSON(fiber.Map{"status": "removed"})
}

// PinKernelPreset - POST /api/v1/servers/:id/packages/pins/kernel (servers:write)
// Body {global}. Ein-Klick-Kernelschutz: legt die Kernel-Muster der
// Paketverwaltung dieses Servers als „nicht entfernen"-Pins an.
func (ctrl *ServerController) PinKernelPreset(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Global bool `json:"global"`
	}
	_ = c.Bind().Body(&req) // leerer Rumpf => je Server
	pins, err := ctrl.servers.PinKernelPreset(scopeFor(c), id, req.Global, actor(c))
	if err != nil {
		return mapServerError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"pins": pins})
}

// ApplyPackagePins - POST /api/v1/servers/:id/packages/pins/apply (servers:write)
// Schreibt die wirksamen Pins auf dem Server fest. Asynchroner Job.
func (ctrl *ServerController) ApplyPackagePins(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.ApplyPackagePins(scopeFor(c), id, actor(c))
	})
}

// Docker - GET /api/v1/servers/:id/docker (servers:read)
// Container (mit Compose-Zuordnung), Images und CVE-Zähler eines Servers.
func (ctrl *ServerController) Docker(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	report, err := ctrl.servers.Docker(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(report)
}

// RefreshDocker - POST /api/v1/servers/:id/docker/refresh (servers:write)
// Liest das Docker-Inventar neu ein (reiner Scan).
func (ctrl *ServerController) RefreshDocker(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RefreshDocker(scopeFor(c), id, actor(c))
	})
}

// UpdateCompose - POST /api/v1/servers/:id/docker/compose-update (servers:write)
// Body: {project, service?}. Projektname im Body (nie in der URL) und nur
// gegen das gespeicherte Inventar validiert ausführbar.
func (ctrl *ServerController) UpdateCompose(c fiber.Ctx) error {
	var req struct {
		Project string `json:"project"`
		Service string `json:"service"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.UpdateComposeProject(scopeFor(c), id, req.Project, req.Service, actor(c))
	})
}

// PullDockerImage - POST /api/v1/servers/:id/docker/pull (servers:write)
// Body: {image}. Zieht das neueste Image des Tags (ohne Container-Neustart).
func (ctrl *ServerController) PullDockerImage(c fiber.Ctx) error {
	var req struct {
		Image string `json:"image"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.PullDockerImage(scopeFor(c), id, req.Image, actor(c))
	})
}

// PullAllDockerImages - POST /api/v1/servers/:id/docker/pull-all (servers:write)
// Zieht die neueste Version aller genutzten, getaggten Registry-Images
// (ein Job, docker pull je Image; keine Container-Neustarts).
func (ctrl *ServerController) PullAllDockerImages(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.PullAllDockerImages(scopeFor(c), id, actor(c))
	})
}

// SetContainerCVERelevance - POST /api/v1/servers/:id/docker/cve-relevance
// (servers:write). Body: {name, relevant}. Markiert einen Container als
// CVE-relevant (seine Image-CVEs zählen in der Status-Bewertung) bzw. hebt
// die Markierung auf. Antwortet mit dem aktualisierten Server.
func (ctrl *ServerController) SetContainerCVERelevance(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Name     string `json:"name"`
		Relevant bool   `json:"relevant"`
	}
	if err := c.Bind().Body(&req); err != nil || req.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	server, err := ctrl.servers.SetContainerCVERelevance(scopeFor(c), id, req.Name, req.Relevant, actor(c))
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(server)
}

// RemoveDockerImage - POST /api/v1/servers/:id/docker/remove-image (servers:write)
// Body: {image}. Löscht ein ungenutztes Image (docker rmi).
func (ctrl *ServerController) RemoveDockerImage(c fiber.Ctx) error {
	var req struct {
		Image string `json:"image"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.DeleteDockerImage(scopeFor(c), id, req.Image, actor(c))
	})
}

// PruneDockerImages - POST /api/v1/servers/:id/docker/prune (servers:write)
// Entfernt alle ungenutzten Images (docker image prune -af) auf einen Schlag.
func (ctrl *ServerController) PruneDockerImages(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.PruneDockerImages(scopeFor(c), id, actor(c))
	})
}

// startPackageJob kapselt das gemeinsame Muster der Paket-Aktionen: ID lesen,
// Aktion asynchron starten, Job-Referenz zurückgeben.
func (ctrl *ServerController) startPackageJob(c fiber.Ctx, run func(id uint) (*domain.Job, error)) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	job, err := run(id)
	if err != nil {
		return mapServerError(err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status": "started", "job_id": job.ID, "job_name": job.Name,
	})
}

type updateServerRequest struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
	// Pointer: nil = Feld nicht im Request enthalten → unverändert lassen.
	UserSyncDisabled      *bool `json:"user_sync_disabled"`
	UnreachableUncritical *bool `json:"unreachable_uncritical"`
	UnreachableGraceDays  *int  `json:"unreachable_grace_days"`
	DockerUpdatesDisabled *bool `json:"docker_updates_disabled"`
	DockerCVEsIgnored     *bool `json:"docker_cves_ignored"`
}

// UpdateSettings - PATCH /api/v1/servers/:id/settings (servers:write)
func (ctrl *ServerController) UpdateSettings(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req updateServerRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	server, err := ctrl.servers.UpdateSettings(scopeFor(c), id, services.ServerSettingsInput{
		Name:                  req.Name,
		Host:                  req.Host,
		Port:                  req.Port,
		UserSyncDisabled:      req.UserSyncDisabled,
		UnreachableUncritical: req.UnreachableUncritical,
		UnreachableGraceDays:  req.UnreachableGraceDays,
		DockerUpdatesDisabled: req.DockerUpdatesDisabled,
		DockerCVEsIgnored:     req.DockerCVEsIgnored,
	}, actor(c))
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(server)
}

// RotateKey - POST /api/v1/servers/:id/rotate-key (servers:write)
func (ctrl *ServerController) RotateKey(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.servers.RotateKey(scopeFor(c), id, actor(c)); err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "rotated"})
}

// runServerAction bündelt das gemeinsame Muster der Ein-Klick-Aktionen
// (HTTPS-Umstellung, SSH-Härtung, …): ID parsen, Service-Aufruf mit
// Scope/Actor, Fehler mappen, Ergebnis als {status, output}.
func (ctrl *ServerController) runServerAction(c fiber.Ctx, status string,
	fn func(scope repositories.AccessScope, id uint, actor string) (string, error)) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	output, err := fn(scopeFor(c), id, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": status, "output": output})
}

// HardenSSH - POST /api/v1/servers/:id/harden-ssh (servers:write)
func (ctrl *ServerController) HardenSSH(c fiber.Ctx) error {
	return ctrl.runServerAction(c, "hardened", ctrl.servers.HardenSSH)
}

// UnhardenSSH - POST /api/v1/servers/:id/unharden-ssh (servers:write)
func (ctrl *ServerController) UnhardenSSH(c fiber.Ctx) error {
	return ctrl.runServerAction(c, "unhardened", ctrl.servers.UnhardenSSH)
}

// RestrictSudo - POST /api/v1/servers/:id/restrict-sudo (servers:write)
// Schränkt den LCM-Management-Benutzer nachträglich auf die sudo-Whitelist ein
// (Einweg-Operation). Ein bereits eingeschränkter Server liefert 409.
func (ctrl *ServerController) RestrictSudo(c fiber.Ctx) error {
	return ctrl.runServerAction(c, "restricted", ctrl.servers.RestrictSudo)
}

// SetSSHRootLogin - POST /api/v1/servers/:id/ssh-root-login (servers:write)
// Body {disabled:bool}: sperrt/erlaubt das direkte root-SSH-Login.
//
// Zeiger statt bool, und ein fehlendes Feld wird abgewiesen: der Go-Nullwert
// hieße hier „Root-Login wieder ERLAUBEN". Ein Tippfehler im Feldnamen nahm so
// still den Schutz von allen angesprochenen Servern und lieferte HTTP 200 -
// dieselbe Falle, die die Firewall mit ihrem Pflichtfeld `enable` und das
// Sicherheits-Werkzeug mit `Bouncer *bool` (R2-078) bereits geschlossen haben.
// Eine Vorgabe darf hier nicht in die unsichere Richtung zeigen.
func (ctrl *ServerController) SetSSHRootLogin(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	req := struct {
		Disabled *bool `json:"disabled"`
	}{}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Disabled == nil {
		return fiber.NewError(fiber.StatusBadRequest,
			"Feld 'disabled' ist erforderlich (true = Root-Login per SSH sperren, false = erlauben)")
	}
	output, err := ctrl.servers.SetSSHRootLogin(scopeFor(c), id, *req.Disabled, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "ok", "output": output})
}

// ChangeSSHPort - POST /api/v1/servers/:id/ssh-port (servers:write)
// Body {port:2222}: stellt den SSH-Port um (erst verifizieren, dann übernehmen).
func (ctrl *ServerController) ChangeSSHPort(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	req := struct {
		Port int `json:"port"`
	}{}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	output, err := ctrl.servers.ChangeSSHPort(scopeFor(c), id, req.Port, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "ok", "output": output})
}

// ScanListeningPorts - POST /api/v1/servers/:id/listening-ports/scan (servers:write)
// Liest das Inventar lauschender Dienste sofort vom Server (ss/netstat) und
// liefert es zurück - die Grundlage der Vorschläge im Firewall-Dialog.
func (ctrl *ServerController) ScanListeningPorts(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	ports, err := ctrl.servers.ScanListeningPortsNow(scopeFor(c), id, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"listening_ports": ports})
}

// ConfigureFirewall - POST /api/v1/servers/:id/firewall (servers:write)
// Body {enable:bool, rules:[{port,proto,ip_version,allowlist_ids,source_ips,
// comment}], ports:"80,443"}.
// enable=false deaktiviert die Firewall; rules sind die detaillierten
// Freigaben (ports ist der Legacy-CSV-Weg, nur TCP). Asynchroner Job - je
// nach Distribution muss das Firewall-Werkzeug erst installiert werden.
func (ctrl *ServerController) ConfigureFirewall(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	req := struct {
		Enable *bool                 `json:"enable"`
		Rules  []domain.FirewallRule `json:"rules"`
		Ports  string                `json:"ports"`
		// Quell-Einschränkung der SSH-Freigabe (benannte Allowlists und/oder
		// eigene IPs/CIDRs). Fehlt das Feld, bleibt SSH von überall offen.
		SSHSources *domain.FirewallSSHSources `json:"ssh_sources"`
	}{}
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	// Ein LEERER Body darf hier nicht mehr „aktivieren, nur SSH" bedeuten:
	// das schloss bei einem Client-Fehler (falscher Content-Type, leerer
	// Request) sämtliche Dienst-Ports des Zielservers. Die Absicht muss
	// ausdrücklich im Body stehen.
	if req.Enable == nil {
		return fiber.NewError(fiber.StatusBadRequest,
			"Feld 'enable' ist erforderlich (true = Firewall aktivieren, false = deaktivieren)")
	}
	enable := *req.Enable
	spec := req.Ports
	if req.Rules != nil {
		spec = firewallRulesSpecJSON(req.Rules)
	}
	var sshSources domain.FirewallSSHSources
	if req.SSHSources != nil {
		sshSources = *req.SSHSources
	}
	job, err := ctrl.servers.ConfigureFirewall(scopeFor(c), id, enable, spec, sshSources, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status": "started", "job_id": job.ID, "job_name": job.Name,
	})
}

// firewallRulesSpecJSON serialisiert die Regel-Eingabe für den Service (der
// validiert und normalisiert selbst; leere Liste = nur SSH freigeben).
func firewallRulesSpecJSON(rules []domain.FirewallRule) string {
	b, err := json.Marshal(rules)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ConfigureAptProxy - POST /api/v1/servers/:id/apt-proxy (servers:write)
// Bindet den Server an den zentralen APT-Cache an bzw. löst ihn davon.
func (ctrl *ServerController) ConfigureAptProxy(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	req := struct {
		Enable *bool `json:"enable"`
	}{}
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	enable := req.Enable == nil || *req.Enable
	output, err := ctrl.servers.ConfigureAptProxy(scopeFor(c), id, enable, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "ok", "output": output})
}

// ConfigureDNS - POST /api/v1/servers/:id/dns (servers:write)
// Setzt bis zu drei Nameserver auf dem Server (leere Liste = LCM gibt die
// DNS-Verwaltung frei). Synchron mit Rollback bei gebrochener Auflösung.
func (ctrl *ServerController) ConfigureDNS(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	req := struct {
		Servers []string `json:"servers"`
	}{}
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	output, err := ctrl.servers.ConfigureDNS(scopeFor(c), id, req.Servers, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "ok", "output": output})
}

// TimeState - POST /api/v1/servers/:id/time-check (servers:read)
// Liest Zeitzone, Zeitdienst und den Uhrenversatz gegenüber LCM frisch aus
// und speichert das Ergebnis. Rein lesend auf dem Ziel - POST, weil es eine
// Aktion mit Seitenwirkung in LCM ist (gespeicherter Zustand + SSH-Protokoll).
func (ctrl *ServerController) TimeState(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	server, err := ctrl.servers.TimeState(scopeFor(c), id, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(server)
}

// SetTimezone - POST /api/v1/servers/:id/timezone (servers:write)
// Setzt die Zeitzone des Servers und liest sie zur Bestätigung zurück.
func (ctrl *ServerController) SetTimezone(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	req := struct {
		Timezone string `json:"timezone"`
	}{}
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	output, err := ctrl.servers.SetTimezone(scopeFor(c), id, req.Timezone, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "ok", "output": output})
}

// ConfigureNTP - POST /api/v1/servers/:id/ntp (servers:write)
// Trägt Zeitserver ein, startet den Zeitdienst und belegt die
// Synchronisierung. Ist sie im Zeitfenster nicht nachweisbar, endet der
// Aufruf mit 502 und einer Meldung, die genau das sagt - die Konfiguration
// bleibt dabei bestehen.
func (ctrl *ServerController) ConfigureNTP(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	req := struct {
		Servers []string `json:"servers"`
	}{}
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	output, err := ctrl.servers.ConfigureNTP(scopeFor(c), id, req.Servers, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "ok", "output": output})
}

// DNSTest - POST /api/v1/servers/:id/dns-test (servers:write)
// Prüft die Auflösbarkeit der gepflegten Test-Domains auf dem Server und
// liefert das dreistufige Ergebnis (full/partial/none) + Detail + aktive Resolver.
func (ctrl *ServerController) DNSTest(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	result, err := ctrl.servers.DNSTest(scopeFor(c), id, actor(c))
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(result)
}

// DeepScanReport - GET /api/v1/servers/:id/deep-scan (servers:read)
// Lesesicht des letzten Deep Scans: Befunde, kernel-bezogene CVEs, Härtungs-Index.
func (ctrl *ServerController) DeepScanReport(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	report, err := ctrl.servers.DeepScanReport(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(report)
}

// DeepScanReportDetail - GET /api/v1/servers/:id/deep-scan/reports/:reportId
// (servers:read). Ein einzelner, datierter Lauf samt seiner Befunde.
func (ctrl *ServerController) DeepScanReportDetail(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	reportID := c.Params("reportId")
	if reportID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "report-id fehlt")
	}
	report, err := ctrl.servers.DeepScanReportDetail(scopeFor(c), id, reportID)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(report)
}

// HardenedPaths - GET /api/v1/servers/:id/hardened-paths (servers:read)
func (ctrl *ServerController) HardenedPaths(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	rows, err := ctrl.servers.HardenedPaths(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(rows)
}

// HardenSuggestions - GET /api/v1/servers/:id/harden-suggestions (servers:read).
// Sucht Verzeichnisse, deren Welt-Zugriff sich entfernen ließe.
func (ctrl *ServerController) HardenSuggestions(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	found, err := ctrl.servers.HardenSuggestions(scopeFor(c), id, actor(c))
	if err != nil {
		return mapHardenError(err)
	}
	return c.JSON(found)
}

// HardenPathsBulk - POST /api/v1/servers/:id/hardened-paths/bulk (servers:write)
func (ctrl *ServerController) HardenPathsBulk(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Targets []services.HardenTarget `json:"targets"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if len(req.Targets) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "keine verzeichnisse ausgewählt")
	}
	results, err := ctrl.servers.HardenPathsBulk(scopeFor(c), id, req.Targets, actor(c))
	if err != nil {
		return mapHardenError(err)
	}
	return c.JSON(results)
}

// HardenPath - POST /api/v1/servers/:id/hardened-paths (servers:write).
// Entfernt den Welt-Zugriff auf ein Verzeichnis; der Vorzustand wird
// aufgezeichnet, damit sich das zurücknehmen lässt.
func (ctrl *ServerController) HardenPath(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Path  string `json:"path"`
		Group string `json:"group"`
		Unit  string `json:"unit"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	hardened, err := ctrl.servers.HardenPath(scopeFor(c), id, req.Path, req.Group, req.Unit, actor(c))
	if err != nil {
		return mapHardenError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(hardened)
}

// RestorePath - DELETE /api/v1/servers/:id/hardened-paths/:pathId (servers:write)
func (ctrl *ServerController) RestorePath(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	hardenedID, err := paramNamedID(c, "pathId")
	if err != nil {
		return err
	}
	if err := ctrl.servers.RestorePath(scopeFor(c), id, hardenedID, actor(c)); err != nil {
		return mapHardenError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// mapHardenError bildet Eingabefehler der Härtung auf 422 ab.
func mapHardenError(err error) error {
	switch {
	case errors.Is(err, domain.ErrPathRelative), errors.Is(err, domain.ErrPathMeta),
		errors.Is(err, domain.ErrPathProtected):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	default:
		return mapServerError(err)
	}
}

// InstallACLSupport - POST /api/v1/servers/:id/acl/install (servers:write).
// Installiert das Paket „acl", ohne das die Verzeichnisrechte der
// Berechtigungsprofile auf diesem Server wirkungslos bleiben.
func (ctrl *ServerController) InstallACLSupport(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.InstallACLSupport(scopeFor(c), id, actor(c))
	})
}

// InstallDeepScanTools - POST /api/v1/servers/:id/deep-scan/install-tools
// (servers:write). Installiert needrestart + lynis (angeboten, nicht automatisch).
func (ctrl *ServerController) InstallDeepScanTools(c fiber.Ctx) error {
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.InstallDeepScanTools(scopeFor(c), id, actor(c))
	})
}

// ConfigureSecurityTool - POST /api/v1/servers/:id/security-tool (servers:write)
// Installiert/konfiguriert fail2ban bzw. CrowdSec. Asynchroner Job.
func (ctrl *ServerController) ConfigureSecurityTool(c fiber.Ctx) error {
	var req struct {
		Tool         string   `json:"tool"`
		AllowlistIPs []string `json:"allowlist_ips"`
		AllowlistIDs []uint   `json:"allowlist_ids"`
		Bouncer      *bool    `json:"bouncer"`
		Collections  []string `json:"collections"`
		LapiMode     string   `json:"lapi_mode"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	// Zeiger statt bool: fehlt das Feld, galt bisher der Go-Nullwert false,
	// während die Oberfläche das Kästchen vorbelegt anzeigt. Über die API
	// entstand so ein CrowdSec, das erkennt, aber nichts sperrt - von LCM
	// trotzdem als „aktiv" geführt (R2-078). Die Vorgabe ist jetzt in beiden
	// Wegen dieselbe; ausdrückliches false bleibt selbstverständlich false.
	bouncer := true
	if req.Bouncer != nil {
		bouncer = *req.Bouncer
	}
	in := services.SecurityToolInput{
		Tool: req.Tool, AllowlistIPs: req.AllowlistIPs, AllowlistIDs: req.AllowlistIDs, Bouncer: bouncer,
		Collections: req.Collections, LapiMode: req.LapiMode,
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.ConfigureSecurityTool(scopeFor(c), id, in, actor(c))
	})
}

// ConfigureSSH2FA - POST /api/v1/servers/:id/ssh-2fa (servers:write)
// Aktiviert bzw. entfernt SSH-2FA (TOTP neben dem SSH-Key) als Job.
func (ctrl *ServerController) ConfigureSSH2FA(c fiber.Ctx) error {
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.ConfigureSSH2FA(scopeFor(c), id, req.Enable, actor(c))
	})
}

// ManageSecurityTool - POST /api/v1/servers/:id/security-tool/manage (servers:write)
// Bedient ein BEREITS installiertes Werkzeug: Dienst steuern (start/stop/
// restart/enable/disable), deinstallieren, Allowlist nachziehen oder eine
// Sperre aufheben. Asynchroner Job.
func (ctrl *ServerController) ManageSecurityTool(c fiber.Ctx) error {
	var req struct {
		Tool         string   `json:"tool"`
		Action       string   `json:"action"`
		UnbanIP      string   `json:"unban_ip"`
		AllowlistIPs []string `json:"allowlist_ips"`
		AllowlistIDs []uint   `json:"allowlist_ids"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	in := services.SecurityToolManageInput{
		Tool: req.Tool, Action: req.Action, UnbanIP: req.UnbanIP,
		AllowlistIPs: req.AllowlistIPs, AllowlistIDs: req.AllowlistIDs,
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.ManageSecurityTool(scopeFor(c), id, in, actor(c))
	})
}

// SecurityToolBans - GET /api/v1/servers/:id/security-tool/bans?tool=… (servers:read)
// Liefert die aktuellen Sperren. Bewusst synchron: Für eine reine Abfrage wäre
// ein Job mit Polling unangemessen, und wer sich selbst ausgesperrt hat, will
// die Liste sofort sehen.
func (ctrl *ServerController) SecurityToolBans(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	bans, err := ctrl.servers.SecurityToolBans(scopeFor(c), id, c.Query("tool"), actor(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"bans": bans})
}

// Decommission - POST /api/v1/servers/:id/decommission (servers:write)
// Body {purge:true} bereinigt zusätzlich den Zielserver (User + LCM-Zugänge)
// vor dem Löschen. Ohne purge wird der Server nur aus dem LCM entfernt.
func (ctrl *ServerController) Decommission(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Purge bool `json:"purge"`
	}
	_ = c.Bind().Body(&req) // leerer Body => purge=false (einfaches Löschen)
	output, err := ctrl.servers.Decommission(scopeFor(c), id, actor(c), services.DecommissionOptions{
		PurgeTarget: req.Purge,
	})
	if err != nil {
		return mapServerActionError(err)
	}
	return c.JSON(fiber.Map{"status": "removed", "output": output})
}
