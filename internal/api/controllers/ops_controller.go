package controllers

import (
	"errors"
	"io"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// MaxUploadBytes ist die Obergrenze für Request-Bodies der gesamten API und
// zugleich für ein hochgeladenes Backup-Archiv.
//
// Fiber kennt kein Body-Limit pro Route - dieser EINE Wert gilt überall.
// Deshalb ein Kompromiss: groß genug für den größten legitimen Upload (das
// Backup-Archiv beim Restore), aber klein genug, dass ein einzelner Request
// den Speicher nicht sprengt. Der Router setzt ihn als fiber.Config.BodyLimit;
// beide Stellen MÜSSEN denselben Wert verwenden, sonst lehnt fasthttp den
// Upload ab, bevor der Handler mit seiner eigenen Prüfung überhaupt läuft.
const MaxUploadBytes = 64 << 20 // 64 MiB

// maxRestoreUpload begrenzt eine hochgeladene Backup-Datei.
const maxRestoreUpload = MaxUploadBytes

// maxExtractedUpload begrenzt zusätzlich die Summe der daraus entpackten
// Dateien: Das Archiv ist komprimiert, ein präpariertes könnte sonst mit 64 MiB
// Upload die Platte füllen. Für Archive aus der eigenen Historie gilt die
// Grenze nicht - dort ist die Datenbank so groß, wie sie ist.
const maxExtractedUpload = 512 << 20 // 512 MiB

// OpsController bündelt system-nahe Endpunkte: globale Scheduler-Übersicht,
// manuelle Trigger, Backups, globale Paket-/Vulnerability-Sichten und
// die globalen Einstellungen.
type OpsController struct {
	scheduler *services.Scheduler
	groups    *services.GroupService
	backups   *services.BackupService
	packages  *services.PackageService
	settings  *services.SettingsService
	servers   *services.ServerService // für die Scope-Prüfung beim Einzel-CVE-Scan
	restart   func()                  // löst einen Prozess-Neustart aus (nil = nicht verfügbar)
	// selfUpdate spielt das eigene Debian-Paket auf dem LCM-Host ein.
	// Optional (nil = Endpunkte melden „nicht verfügbar").
	selfUpdate *services.SelfUpdateService
}

// WithSelfUpdate verdrahtet das Selbst-Update.
func (ctrl *OpsController) WithSelfUpdate(s *services.SelfUpdateService) *OpsController {
	ctrl.selfUpdate = s
	return ctrl
}

func NewOpsController(scheduler *services.Scheduler, groups *services.GroupService, backups *services.BackupService, packages *services.PackageService, settings *services.SettingsService, servers *services.ServerService, restart func()) *OpsController {
	return &OpsController{scheduler: scheduler, groups: groups, backups: backups, packages: packages, settings: settings, servers: servers, restart: restart}
}

// SchedulesOverview - GET /api/v1/system/schedules/overview (settings:manage)
func (ctrl *OpsController) SchedulesOverview(c fiber.Ctx) error {
	overview, err := ctrl.scheduler.Overview()
	if err != nil {
		return err
	}
	return c.JSON(overview)
}

// TriggerGlobalSchedule - POST /api/v1/system/schedules/:id/trigger-now (settings:manage)
// Triggert einen Gruppen-Schedule anhand seiner ID (global, ScopeAll).
func (ctrl *OpsController) TriggerGlobalSchedule(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	sched, err := ctrl.groups.FindSchedule(repositories.ScopeAll(), id)
	if err != nil {
		return mapGroupError(err)
	}
	ctrl.scheduler.TriggerScheduleNow(sched, actor(c))
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "triggered", "schedule": sched.Name})
}

// TriggerSystemSchedule - POST /api/v1/system/schedules/kind/:kind/trigger-now
// Triggert einen system-globalen Schedule sofort. Zulässig ist genau das, was
// die Schedule-Übersicht als System-Schedule ausweist (backup, cleanup,
// cve-scan, alert-check, update-check).
func (ctrl *OpsController) TriggerSystemSchedule(c fiber.Ctx) error {
	kind := c.Params("kind")
	if err := ctrl.scheduler.TriggerSystem(kind, actor(c)); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "triggered", "kind": kind})
}

// ---- Backups -----------------------------------------------------------------

// ListBackups - GET /api/v1/system/backups (backups:manage)
func (ctrl *OpsController) ListBackups(c fiber.Ctx) error {
	backups, err := ctrl.backups.List()
	if err != nil {
		return err
	}
	return c.JSON(backups)
}

// TriggerBackup - POST /api/v1/system/backups/trigger-now (backups:manage)
// Optionale Passphrase im Body; sonst greift LCM_BACKUP_PASSPHRASE.
func (ctrl *OpsController) TriggerBackup(c fiber.Ctx) error {
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	backup, err := ctrl.backups.Create(actor(c), req.Passphrase)
	if err != nil {
		if errors.Is(err, services.ErrBackupNoPassphrase) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		if errors.Is(err, services.ErrWeakBackupPassphrase) {
			return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, "backup fehlgeschlagen")
	}
	return c.Status(fiber.StatusCreated).JSON(backup)
}

// DownloadBackup - GET /api/v1/system/backups/:name/download (backups:manage)
// Liefert das verschlüsselte .lcmbak-Archiv zum Herunterladen.
func (ctrl *OpsController) DownloadBackup(c fiber.Ctx) error {
	name := c.Params("name")
	path, err := ctrl.backups.BackupPath(name)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "backup nicht gefunden")
	}
	return c.Download(path, name)
}

// DeleteBackup - DELETE /api/v1/system/backups/:name (backups:manage)
// Entfernt ein Backup dauerhaft (Datei + Metadaten). Verhindert, dass die
// Backup-Historie unbegrenzt anwächst.
func (ctrl *OpsController) DeleteBackup(c fiber.Ctx) error {
	name := c.Params("name")
	if err := ctrl.backups.Delete(name); err != nil {
		if errors.Is(err, services.ErrBackupNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "backup nicht gefunden")
		}
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// RestoreBackup - POST /api/v1/system/backups/:name/restore (backups:manage)
// Bereitet die Wiederherstellung aus einem Backup der Historie vor (Rollback
// auf einen früheren Stand). Passphrase im Body.
func (ctrl *OpsController) RestoreBackup(c fiber.Ctx) error {
	name := c.Params("name")
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	if err := ctrl.backups.StageRestoreFromHistory(name, req.Passphrase); err != nil {
		return mapRestoreError(err)
	}
	return ctrl.respondStaged(c)
}

// RestoreUpload - POST /api/v1/system/backups/restore-upload (backups:manage)
// Bereitet die Wiederherstellung aus einem HOCHGELADENEN Archiv vor. Damit
// lässt sich ein Backup auch auf einer frischen/leeren Instanz einspielen
// (nach Anmeldung mit dem Admin-Konto). Multipart: Feld "archive" + "passphrase".
func (ctrl *OpsController) RestoreUpload(c fiber.Ctx) error {
	fh, err := c.FormFile("archive")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "keine Backup-Datei (Feld 'archive')")
	}
	if fh.Size > maxRestoreUpload {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "Backup-Datei zu groß")
	}
	f, err := fh.Open()
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Backup-Datei nicht lesbar")
	}
	defer f.Close()
	// Streamend entpacken statt erst komplett in den Speicher zu laden: Ein
	// Backup-Archiv ist so groß wie die Datenbank, und der Prozess läuft
	// gewöhnlich auf einer kleinen VM. Das Größenlimit bleibt, es begrenzt
	// jetzt den Lesestrom.
	if err := ctrl.backups.StageRestoreReader(io.LimitReader(f, maxRestoreUpload), c.FormValue("passphrase"), maxExtractedUpload); err != nil {
		return mapRestoreError(err)
	}
	return ctrl.respondStaged(c)
}

// respondStaged beantwortet einen vorbereiteten Restore und löst - falls
// aktiviert und verfügbar - einen automatischen Neustart aus.
func (ctrl *OpsController) respondStaged(c fiber.Ctx) error {
	if ctrl.backups.AutoRestartEnabled() && ctrl.restart != nil {
		// Antwort zuerst rausgeben, dann mit kurzer Verzögerung neu starten,
		// damit der Client die Bestätigung noch erhält.
		r := ctrl.restart
		safego.Go("restore:restart", func() {
			time.Sleep(1500 * time.Millisecond)
			r()
		})
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"status":  "staged",
			"restart": "auto",
			"message": "Wiederherstellung vorbereitet - LCM startet automatisch neu, um sie anzuwenden.",
		})
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status":  "staged",
		"restart": "manual",
		"message": "Wiederherstellung vorbereitet - bitte LCM neu starten, um sie anzuwenden.",
	})
}

// mapRestoreError bildet Restore-Fehler auf HTTP-Status ab.
func mapRestoreError(err error) error {
	switch {
	case errors.Is(err, services.ErrBackupNotFound):
		return fiber.NewError(fiber.StatusNotFound, "backup nicht gefunden")
	case errors.Is(err, services.ErrBackupNoPassphrase):
		return fiber.NewError(fiber.StatusBadRequest, "Passphrase erforderlich")
	case errors.Is(err, services.ErrBackupPassphrase):
		return fiber.NewError(fiber.StatusBadRequest, "Passphrase falsch oder Datei beschädigt")
	case errors.Is(err, services.ErrBackupFormat):
		return fiber.NewError(fiber.StatusBadRequest, "kein gültiges LCM-Backup-Archiv")
	case errors.Is(err, services.ErrBackupIncomplete):
		return fiber.NewError(fiber.StatusBadRequest, "Archiv enthält keine Datenbank")
	default:
		return fiber.NewError(fiber.StatusInternalServerError, "wiederherstellung fehlgeschlagen")
	}
}

type backupSettingsRequest struct {
	Enabled       bool   `json:"enabled"`
	IntervalHours int    `json:"interval_hours"`
	Retention     int    `json:"retention"`
	Time          string `json:"time"` // Anker-Uhrzeit HH:MM (R2-034)
	Dir           string `json:"dir"`
	AutoRestart   bool   `json:"auto_restart"`
	// Passphrase für geplante Backups: write-only (leer = unverändert),
	// verschlüsselt abgelegt (R2-027).
	Passphrase string `json:"passphrase"`
}

// ConfigureBackups - PATCH /api/v1/system/backups/settings (backups:manage)
func (ctrl *OpsController) ConfigureBackups(c fiber.Ctx) error {
	var req backupSettingsRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	// Feld-scharfes Teil-Update: nur die Backup-Felder anfassen - das
	// Durchreichen an UpdateGlobal hat früher alle nicht mitgereichten
	// Formularfelder (Mailer, DNS, CrowdSec, …) auf Null zurückgesetzt.
	updated, err := ctrl.settings.UpdateBackupSettings(services.BackupSettingsInput{
		Enabled:       req.Enabled,
		IntervalHours: req.IntervalHours,
		Retention:     req.Retention,
		Time:          req.Time,
		Dir:           req.Dir,
		AutoRestart:   req.AutoRestart,
		Passphrase:    req.Passphrase,
	}, actor(c))
	if err != nil {
		if errors.Is(err, services.ErrBackupNeedsPassphrase) ||
			errors.Is(err, services.ErrSettingInvalid) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		if errors.Is(err, services.ErrWeakBackupPassphrase) {
			return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		}
		return err
	}
	return c.JSON(updated)
}

// ---- Globale Paket-Sichten ---------------------------------------------------

// PackageOverview - GET /api/v1/packages/overview (packages:read)
func (ctrl *OpsController) PackageOverview(c fiber.Ctx) error {
	rows, err := ctrl.packages.Overview(scopeFor(c))
	if err != nil {
		return err
	}
	return c.JSON(rows)
}

// VulnerablePackages - GET /api/v1/packages/vulnerable (packages:read)
func (ctrl *OpsController) VulnerablePackages(c fiber.Ctx) error {
	rows, err := ctrl.packages.Vulnerable(scopeFor(c))
	if err != nil {
		return err
	}
	return c.JSON(rows)
}

// DockerOverview - GET /api/v1/docker/overview (packages:read)
// Einzigartige Docker-Images über alle sichtbaren Server (Update-/CVE-Status).
func (ctrl *OpsController) DockerOverview(c fiber.Ctx) error {
	rows, err := ctrl.packages.DockerOverview(scopeFor(c))
	if err != nil {
		return err
	}
	return c.JSON(rows)
}

// DockerContainers - GET /api/v1/docker/containers (packages:read)
// Alle Container der sichtbaren Server, laufende zuerst.
func (ctrl *OpsController) DockerContainers(c fiber.Ctx) error {
	rows, err := ctrl.packages.DockerContainers(scopeFor(c))
	if err != nil {
		return err
	}
	return c.JSON(rows)
}

// DockerCompose - GET /api/v1/docker/compose (packages:read)
// Die Compose-Projekte über alle sichtbaren Server hinweg.
func (ctrl *OpsController) DockerCompose(c fiber.Ctx) error {
	rows, err := ctrl.packages.DockerCompose(scopeFor(c))
	if err != nil {
		return err
	}
	return c.JSON(rows)
}

// Vulnerabilities - GET /api/v1/security/vulnerabilities (packages:read)
// Globale CVE-Übersicht über alle sichtbaren Server (kritischste zuerst).
func (ctrl *OpsController) Vulnerabilities(c fiber.Ctx) error {
	page := fiber.Query[int](c, "page")
	if page < 1 {
		page = 1
	}
	pageSize := fiber.Query[int](c, "page_size")
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	// source filtert die Herkunft: "os" (nur nativ installiert, Docker-CVEs
	// ausgeblendet) oder "docker"; alles andere = keine Filterung.
	source := c.Query("source")
	if source != "os" && source != "docker" {
		source = ""
	}
	res, err := ctrl.packages.VulnerabilitiesPage(scopeFor(c), page, pageSize, source)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

// ScannerStatus - GET /api/v1/security/scanner (packages:read)
// Version des CVE-Scanners und Stand seiner Schwachstellen-Datenbank samt
// Frische-Bewertung.
//
// Warum das ein eigener Endpunkt ist: Trivy lädt die Datenbank beim Scan
// selbst nach, aber nur mit Netzzugang. Scheitert das, scannt Trivy mit der
// alten Datenbank weiter und meldet keinen Fehler - „keine Sicherheitslücken
// gefunden" wäre dann nicht von „seit Wochen nicht nachgesehen" zu
// unterscheiden.
func (ctrl *OpsController) ScannerStatus(c fiber.Ctx) error {
	return c.JSON(ctrl.servers.CVEDBStatus())
}

// UpdateCVEDB - POST /api/v1/security/scanner/update-db (settings:manage)
// Lädt die Schwachstellen-Datenbank herunter, ohne zu scannen. Asynchroner
// Job - damit ein Fehlschlag (Proxy, Rate-Limit, kein Netz) samt Ausgabe im
// Protokoll landet statt still zu verpuffen.
func (ctrl *OpsController) UpdateCVEDB(c fiber.Ctx) error {
	job, err := ctrl.servers.UpdateCVEDB(actor(c))
	if err != nil {
		if errors.Is(err, services.ErrScannerUnavailable) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return mapServerError(err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status": "job gestartet", "job_id": job.ID, "job_name": job.Name,
	})
}

// BulkUpdateStatus - GET /api/v1/security/update-all (packages:read)
// Liefert den Fortschritt eines laufenden „Alle VMs aktualisieren"-Laufs.
func (ctrl *OpsController) BulkUpdateStatus(c fiber.Ctx) error {
	return c.JSON(ctrl.servers.BulkUpdateStatus())
}

// StartBulkUpdate - POST /api/v1/security/update-all (servers:write)
// Stößt Security-Updates auf allen erreichbaren Servern im Scope an. Läuft
// bereits eines, antwortet der Endpunkt mit 409 samt aktuellem Stand.
func (ctrl *OpsController) StartBulkUpdate(c fiber.Ctx) error {
	status, err := ctrl.servers.StartBulkUpdate(scopeFor(c), actor(c))
	if err != nil {
		if errors.Is(err, services.ErrBulkUpdateRunning) {
			return c.Status(fiber.StatusConflict).JSON(status)
		}
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(status)
}

// ScanServerVulnerabilities - POST /api/v1/servers/:id/vulnerabilities/scan
// (servers:write). Stößt einen CVE-Scan für einen einzelnen Server an.
func (ctrl *OpsController) ScanServerVulnerabilities(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	// Mandantentrennung: nur Server im Scope des Aufrufers dürfen gescannt
	// werden. Fremde/unbekannte IDs liefern 404, bevor irgendein SSH-Kontakt
	// oder Job auf dem fremden Server ausgelöst wird.
	if _, err := ctrl.servers.Get(scopeFor(c), id); err != nil {
		return mapServerError(err)
	}
	ctrl.scheduler.TriggerCVEScanServer(id, actor(c))
	return c.JSON(fiber.Map{"status": "scan gestartet"})
}

// DeepScanServer - POST /api/v1/servers/:id/deep-scan (servers:write). Stößt den
// Deep Scan (Kernel-Reboot-Lücke, Kernel-CVEs, Härtungs-Audit) für einen
// einzelnen Server an. Läuft asynchron als Job.
func (ctrl *OpsController) DeepScanServer(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if _, err := ctrl.servers.Get(scopeFor(c), id); err != nil {
		return mapServerError(err)
	}
	ctrl.scheduler.TriggerDeepScanServer(id, actor(c))
	return c.JSON(fiber.Map{"status": "deep scan gestartet"})
}

// ---- Katalog bekannter Paketquellen -------------------------------------------

// ListKnownRepos - GET /api/v1/settings/known-repos (settings:manage)
// Der pflegbare Katalog; die servers:read-Sicht für das Server-Detail
// liegt unter /servers/known-repos.
func (ctrl *OpsController) ListKnownRepos(c fiber.Ctx) error {
	repos, err := ctrl.settings.ListKnownRepos()
	if err != nil {
		return err
	}
	return c.JSON(repos)
}

// SaveKnownRepo - POST /api/v1/settings/known-repos (settings:manage)
// Legt einen Katalog-Eintrag an (ohne id) oder aktualisiert ihn (mit id).
func (ctrl *OpsController) SaveKnownRepo(c fiber.Ctx) error {
	var req domain.KnownRepo
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	repo, err := ctrl.settings.SaveKnownRepo(req, actor(c))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrKnownRepoInvalid):
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		case errors.Is(err, repositories.ErrNotFound):
			return fiber.NewError(fiber.StatusNotFound, "katalog-eintrag nicht gefunden")
		}
		return err
	}
	return c.JSON(repo)
}

// DeleteKnownRepo - DELETE /api/v1/settings/known-repos/:id (settings:manage)
func (ctrl *OpsController) DeleteKnownRepo(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.settings.DeleteKnownRepo(id, actor(c)); err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "katalog-eintrag nicht gefunden")
		}
		return err
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}

// ---- IP-Allowlists (gemeinsamer Pool) ----------------------------------------

// ListIPAllowlists - GET /api/v1/settings/ip-allowlists (settings:manage) bzw.
// GET /api/v1/ip-allowlists (servers:read, für die Auswahl in Firewall-Regeln
// und der Security-Tool-Einrichtung).
func (ctrl *OpsController) ListIPAllowlists(c fiber.Ctx) error {
	lists, err := ctrl.settings.ListIPAllowlists()
	if err != nil {
		return err
	}
	return c.JSON(lists)
}

// SaveIPAllowlist - POST /api/v1/settings/ip-allowlists (settings:manage)
// Legt eine Allowlist an (ohne id) oder aktualisiert sie (mit id).
func (ctrl *OpsController) SaveIPAllowlist(c fiber.Ctx) error {
	var req domain.IPAllowlist
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	list, err := ctrl.settings.SaveIPAllowlist(req, actor(c))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrIPAllowlistInvalid):
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		case errors.Is(err, repositories.ErrNotFound):
			return fiber.NewError(fiber.StatusNotFound, "allowlist nicht gefunden")
		}
		return err
	}
	return c.JSON(list)
}

// DeleteIPAllowlist - DELETE /api/v1/settings/ip-allowlists/:id (settings:manage)
func (ctrl *OpsController) DeleteIPAllowlist(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.settings.DeleteIPAllowlist(id, actor(c)); err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "allowlist nicht gefunden")
		}
		if errors.Is(err, services.ErrAllowlistInUse) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return err
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}

// ---- Globale Einstellungen ---------------------------------------------------

// GetSettings - GET /api/v1/settings/global (settings:manage)
func (ctrl *OpsController) GetSettings(c fiber.Ctx) error {
	settings, err := ctrl.settings.Get()
	if err != nil {
		return err
	}
	// Geheimnisse bleiben verborgen (json:"-"); für die UI zusätzlich abgeleitete
	// „konfiguriert?"-Flags mitgeben, ohne die Werte preiszugeben.
	return c.JSON(struct {
		*domain.GlobalSettings
		CrowdSecLapiConfigured    bool `json:"crowdsec_lapi_configured"`
		CrowdSecConsoleConfigured bool `json:"crowdsec_console_configured"`
		BackupPassphraseSet       bool `json:"backup_passphrase_set"`
	}{settings, settings.CrowdSecLapiConfigured(), settings.CrowdSecConsoleConfigured(),
		services.BackupPassphraseSet() || ctrl.settings.BackupPassphraseStored()})
}

// SetMCP - PUT /api/v1/settings/mcp (settings:manage)
// Schaltet die MCP-Schnittstelle ein/aus und setzt Bind-Adresse/Port. Der
// separate MCP-Listener wird direkt zur Laufzeit gestartet/gestoppt.
func (ctrl *OpsController) SetMCP(c fiber.Ctx) error {
	var req struct {
		Enabled  bool   `json:"enabled"`
		BindHost string `json:"bind_host"`
		Port     int    `json:"port"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Port == 0 {
		req.Port = 9330
	}
	if err := ctrl.settings.SetMCP(req.Enabled, req.BindHost, req.Port, actor(c)); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	settings, err := ctrl.settings.Get()
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"mcp_enabled": settings.MCPEnabled, "mcp_bind_host": settings.MCPBindHost, "mcp_port": settings.MCPPort,
	})
}

// UpdateInfo - GET /api/v1/system/update-info (authentifiziert)
// Liefert den zwischengespeicherten Update-Status (installiert vs. neueste
// bekannte Version) fürs Banner und die Footer-Anzeige.
func (ctrl *OpsController) UpdateInfo(c fiber.Ctx) error {
	status, err := ctrl.settings.UpdateStatus()
	if err != nil {
		return err
	}
	return c.JSON(status)
}

// CheckUpdateNow - POST /api/v1/system/update-check (settings:manage)
// Löst die Update-Prüfung sofort aus (für den „Jetzt prüfen"-Button) und
// liefert den frischen Status zurück.
func (ctrl *OpsController) CheckUpdateNow(c fiber.Ctx) error {
	_ = ctrl.settings.CheckForUpdate() // Fehler stehen im Status (update_check_error)
	status, err := ctrl.settings.UpdateStatus()
	if err != nil {
		return err
	}
	return c.JSON(status)
}

// SelfUpdateStatus - GET /api/v1/system/self-update (settings:manage)
// Liefert, ob sich LCM hier selbst aktualisieren kann und wie weit eine
// angeforderte Aktualisierung ist.
func (ctrl *OpsController) SelfUpdateStatus(c fiber.Ctx) error {
	if ctrl.selfUpdate == nil {
		return c.JSON(services.SelfUpdateStatus{
			Phase:  services.SelfUpdateIdle,
			Reason: "Selbst-Update ist auf dieser Installation nicht eingerichtet.",
		})
	}
	return c.JSON(ctrl.selfUpdate.Status())
}

// StartSelfUpdate - POST /api/v1/system/self-update (settings:manage)
// Fordert das Selbst-Update an. Der Aufruf kehrt sofort zurück; LCM wartet
// danach, bis kein Job mehr läuft, sichert und spielt das Paket dann ein.
//
// Ohne "with_backup": false im Body wird gesichert - die vorsichtige Vorgabe
// ist hier die richtige: Wer sein eigenes Verwaltungssystem aktualisiert, hat
// im Fehlerfall kein zweites, das ihm hilft.
func (ctrl *OpsController) StartSelfUpdate(c fiber.Ctx) error {
	if ctrl.selfUpdate == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "Selbst-Update ist auf dieser Installation nicht eingerichtet.")
	}
	req := struct {
		WithBackup *bool `json:"with_backup"`
	}{}
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
		}
	}
	withBackup := req.WithBackup == nil || *req.WithBackup

	status, err := ctrl.selfUpdate.Start(actor(c), withBackup)
	if err != nil {
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	return c.JSON(status)
}

// globalSettingsRequest ist der PATCH-Rumpf für die globalen Einstellungen.
// ALLE Felder sind Zeiger: nur mitgeschickte Felder werden geändert. Früher
// verhielt sich der Endpunkt wie ein PUT - ein Aufruf mit einem einzigen
// Feld nullte zehn unbeteiligte Einstellungen (Backup aus, CVE-Scan aus,
// APT-Cache-URL weg), still und mit 200 (R2-029).
type globalSettingsRequest struct {
	DefaultSSHUser              *string `json:"default_ssh_user"`
	DefaultSSHPassword          *string `json:"default_ssh_password"`
	DefaultSSHPort              *int    `json:"default_ssh_port"`
	LogRetentionDays            *int    `json:"log_retention_days"`
	StorageHistoryRetentionDays *int    `json:"storage_history_retention_days"`
	BackupEnabled               *bool   `json:"backup_enabled"`
	BackupIntervalHours         *int    `json:"backup_interval_hours"`
	BackupRetention             *int    `json:"backup_retention"`
	BackupTime                  *string `json:"backup_time"`
	BackupDir                   *string `json:"backup_dir"`
	RestoreAutoRestart          *bool   `json:"restore_auto_restart"`
	CVEScanEnabled              *bool   `json:"cve_scan_enabled"`
	CVEScanCron                 *string `json:"cve_scan_cron"`
	AdvisoryPollingEnabled      *bool   `json:"advisory_polling_enabled"`
	AdvisoryLocalCopy           *bool   `json:"advisory_local_copy"`
	AdvisoryCacheTTLMinutes     *int    `json:"advisory_cache_ttl_minutes"`
	CVEHighWeightPackages       *string `json:"cve_high_weight_packages"`
	SessionTTLMinutes           *int    `json:"session_ttl_minutes"`
	JobIdleTimeoutMinutes       *int    `json:"job_idle_timeout_minutes"`
	JobIdleTimeoutSlowMinutes   *int    `json:"job_idle_timeout_slow_minutes"`
	AptCacheURL                 *string `json:"apt_cache_url"`
	Require2FARoles             *string `json:"require_2fa_roles"`
	PublicBaseURL               *string `json:"public_base_url"`
	DNSServerPresets            *string `json:"dns_server_presets"`
	DNSTestDomains              *string `json:"dns_test_domains"`
	NTPServerPresets            *string `json:"ntp_server_presets"`
	DefaultTimezone             *string `json:"default_timezone"`
	// CrowdSec-Zugang; Passwort/Key write-only (leer = unverändert).
	CrowdSecLapiURL      *string `json:"crowdsec_lapi_url"`
	CrowdSecLapiLogin    *string `json:"crowdsec_lapi_login"`
	CrowdSecLapiPassword *string `json:"crowdsec_lapi_password"`
	CrowdSecConsoleKey   *string `json:"crowdsec_console_key"`

	// Standard-E-Mail-Versand (System-Mailer); mail_password ist write-only
	// (leer = unverändert).
	MailEnabled         *bool   `json:"mail_enabled"`
	MailHost            *string `json:"mail_host"`
	MailPort            *int    `json:"mail_port"`
	MailUsername        *string `json:"mail_username"`
	MailPassword        *string `json:"mail_password"`
	MailFrom            *string `json:"mail_from"`
	MailUseTLS          *bool   `json:"mail_use_tls"`
	MailAdminRecipients *string `json:"mail_admin_recipients"`
}

// toInput übersetzt den Request in die Service-Eingabe. Bewusst eine eigene
// Funktion und nicht inline im Handler: So kann ein Test jedes Feld setzen und
// prüfen, dass keines auf dem Weg verlorengeht. Genau das war passiert -
// ntp_server_presets und default_timezone fehlten im Request-Rumpf, die
// Seite „Zeit & NTP" speicherte deshalb still ins Leere (200 ohne Wirkung).
func (req globalSettingsRequest) toInput() services.GlobalSettingsInput {
	return services.GlobalSettingsInput{
		DefaultSSHUser:              req.DefaultSSHUser,
		DefaultSSHPassword:          req.DefaultSSHPassword,
		DefaultSSHPort:              req.DefaultSSHPort,
		LogRetentionDays:            req.LogRetentionDays,
		StorageHistoryRetentionDays: req.StorageHistoryRetentionDays,
		BackupEnabled:               req.BackupEnabled,
		BackupIntervalHours:         req.BackupIntervalHours,
		BackupRetention:             req.BackupRetention,
		BackupTime:                  req.BackupTime,
		BackupDir:                   req.BackupDir,
		RestoreAutoRestart:          req.RestoreAutoRestart,
		CVEScanEnabled:              req.CVEScanEnabled,
		CVEHighWeightPackages:       req.CVEHighWeightPackages,
		CVEScanCron:                 req.CVEScanCron,
		AdvisoryPollingEnabled:      req.AdvisoryPollingEnabled,
		AdvisoryLocalCopy:           req.AdvisoryLocalCopy,
		AdvisoryCacheTTLMinutes:     req.AdvisoryCacheTTLMinutes,
		SessionTTLMinutes:           req.SessionTTLMinutes,
		JobIdleTimeoutMinutes:       req.JobIdleTimeoutMinutes,
		JobIdleTimeoutSlowMinutes:   req.JobIdleTimeoutSlowMinutes,
		AptCacheURL:                 req.AptCacheURL,
		Require2FARoles:             req.Require2FARoles,
		PublicBaseURL:               req.PublicBaseURL,
		DNSServerPresets:            req.DNSServerPresets,
		DNSTestDomains:              req.DNSTestDomains,
		NTPServerPresets:            req.NTPServerPresets,
		DefaultTimezone:             req.DefaultTimezone,
		CrowdSecLapiURL:             req.CrowdSecLapiURL,
		CrowdSecLapiLogin:           req.CrowdSecLapiLogin,
		CrowdSecLapiPassword:        req.CrowdSecLapiPassword,
		CrowdSecConsoleKey:          req.CrowdSecConsoleKey,
		MailEnabled:                 req.MailEnabled,
		MailHost:                    req.MailHost,
		MailPort:                    req.MailPort,
		MailUsername:                req.MailUsername,
		MailPassword:                req.MailPassword,
		MailFrom:                    req.MailFrom,
		MailUseTLS:                  req.MailUseTLS,
		MailAdminRecipients:         req.MailAdminRecipients,
	}
}

// UpdateSettings - PATCH /api/v1/settings/global (settings:manage)
func (ctrl *OpsController) UpdateSettings(c fiber.Ctx) error {
	var req globalSettingsRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	updated, err := ctrl.settings.UpdateGlobal(req.toInput(), actor(c))
	if err != nil {
		// Eingabevalidierung gehört als 4xx beantwortet - eine unbekannte
		// Rolle in require_2fa_roles ergab vorher HTTP 500 (R2-060).
		//
		// Geprüft wird die KATEGORIE, nicht eine Liste einzelner Fehler: Die
		// alte Positivliste musste bei jedem neuen Validierungsfehler
		// mitgepflegt werden, und wer das vergaß, lieferte für eine schlichte
		// Fehleingabe „interner Serverfehler" aus. Alle Validierungsfehler
		// wickeln jetzt ErrSettingInvalid ein.
		if errors.Is(err, services.ErrSettingInvalid) || errors.Is(err, services.ErrMailerIncomplete) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return err
	}
	return c.JSON(updated)
}

// TestSystemMail - POST /api/v1/settings/global/test-mail (settings:manage)
// Verschickt eine Testnachricht an die Admin-Empfänger des System-Mailers.
func (ctrl *OpsController) TestSystemMail(c fiber.Ctx) error {
	if err := ctrl.settings.TestSystemMail(actor(c)); err != nil {
		// Konfigurations- wie Versandfehler landen als 422 beim Aufrufer -
		// der Test-Button soll die Ursache anzeigen, nicht ein generisches 500.
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}
	return c.JSON(fiber.Map{"status": "sent"})
}

// CrowdSecLapiStatus - GET /api/v1/settings/crowdsec/status (settings:manage)
// Prüft vom LCM-Host aus, ob die konfigurierte CrowdSec-LAPI erreichbar ist
// und die hinterlegten Maschinen-Zugangsdaten akzeptiert.
func (ctrl *OpsController) CrowdSecLapiStatus(c fiber.Ctx) error {
	status, err := ctrl.settings.CheckCrowdSecLapi()
	if err != nil {
		return err
	}
	return c.JSON(status)
}

// AptCacheStatus - GET /api/v1/settings/apt-cache/status (settings:manage)
// Prüft vom LCM-Host aus, ob der konfigurierte apt-cacher-ng erreichbar ist.
func (ctrl *OpsController) AptCacheStatus(c fiber.Ctx) error {
	status, err := ctrl.settings.CheckAptCache()
	if err != nil {
		return err
	}
	return c.JSON(status)
}
