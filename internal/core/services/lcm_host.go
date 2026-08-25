package services

import (
	"errors"
	"log/slog"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/runtimeenv"
	"LCM/internal/infrastructure/sandbox"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// Der LCM-Host ist der Server, auf dem LCM selbst läuft (Host localhost). Für
// ihn bietet LCM host-spezifische Einrichtungs-Aktionen an: den CVE-Scanner
// Trivy und den Paket-Cache apt-cacher-ng jeweils per Knopfdruck installieren.
// Beides läuft über den normalen SSH-Job-Mechanismus (privRun/sudo) auf dem
// LCM-Host und setzt eine Debian/Ubuntu-Basis (apt) voraus.

var (
	// ErrNotLcmHost: Die Aktion ist nur für den LCM-Host (localhost) erlaubt.
	ErrNotLcmHost = errors.New("aktion nur für den LCM-Host (localhost) verfügbar")
	// ErrLcmHostNotApt: Trivy/apt-cacher-ng werden nur auf Debian/Ubuntu (apt)
	// per Knopfdruck eingerichtet.
	ErrLcmHostNotApt = errors.New("automatische Einrichtung nur auf Debian/Ubuntu (apt) möglich")
	// ErrLcmHostInContainer: LCM läuft in einem Container. Alle Aktionen hier
	// richten etwas auf dem LCM-HOST ein - im Container gäbe es dafür weder
	// apt noch einen Dienst, der den Neustart überlebt.
	//
	// Wie ein LCM-Host-Eintrag dort überhaupt hinkommt: Die Selbstaufnahme
	// unterbleibt im Container (self_register.go), aber ein von Hand
	// angelegter Eintrag auf localhost oder ein in den Container
	// zurückgespieltes Backup einer Host-Installation bringt ihn mit.
	ErrLcmHostInContainer = errors.New("LCM läuft in einem Container - diese Einrichtung gilt dem LCM-Host und ist hier nicht möglich")
)

// trivyInstallScript bindet das offizielle Aqua-Trivy-APT-Repository ein und
// installiert Trivy. Läuft als root (privRun/sudo).
//
// bubblewrap kommt mit: es ist die Sandbox, in der LCM Trivy anschließend
// startet (siehe Paket sandbox). Trivy läuft als Kindprozess von LCM und käme
// sonst an dessen Datenverzeichnis - Master-Key und Datenbank inklusive.
// Genau darauf zielte die Trivy-Lieferkettenkompromittierung im März 2026.
// Anders als Trivy selbst stammt bubblewrap aus Debian/Ubuntu main, nicht aus
// einem Fremd-Repository, und braucht keine Sonderrechte.
const trivyInstallScript = "set -e; " +
	"export DEBIAN_FRONTEND=noninteractive; " +
	"apt-get update; " +
	"apt-get install -y wget gnupg apt-transport-https ca-certificates bubblewrap; " +
	"install -d -m 0755 /etc/apt/keyrings; " +
	"wget -qO- https://aquasecurity.github.io/trivy-repo/deb/public.key | gpg --dearmor -o /etc/apt/keyrings/trivy.gpg; " +
	"chmod 0644 /etc/apt/keyrings/trivy.gpg; " +
	"echo 'deb [signed-by=/etc/apt/keyrings/trivy.gpg] https://aquasecurity.github.io/trivy-repo/deb generic main' > /etc/apt/sources.list.d/trivy.list; " +
	"apt-get update; " +
	"apt-get install -y trivy; " +
	"trivy --version; " +
	"bwrap --version"

// sandboxInstallScript rüstet die Sandbox nach: bubblewrap aus Debian/Ubuntu
// main, sonst nichts. Trivy selbst bleibt unangetastet - LCM sperrt den
// Scanner beim Start in die Sandbox ein, das Binary ist dasselbe.
//
// Gebraucht wird das auf Hosts, auf denen Trivy VOR der Sandbox eingerichtet
// wurde: dort fehlt bubblewrap, und der Scanner läuft mit den Rechten von LCM.
const sandboxInstallScript = "set -e; " +
	"export DEBIAN_FRONTEND=noninteractive; " +
	"apt-get update; " +
	"apt-get install -y bubblewrap; " +
	"bwrap --version"

// aptCacherInstallScript installiert und aktiviert apt-cacher-ng (lauscht auf
// Port 3142). Läuft als root (privRun/sudo).
const aptCacherInstallScript = "set -e; " +
	"export DEBIAN_FRONTEND=noninteractive; " +
	"apt-get update; " +
	"apt-get install -y apt-cacher-ng; " +
	"systemctl enable --now apt-cacher-ng 2>/dev/null || service apt-cacher-ng start || true; " +
	"systemctl is-active apt-cacher-ng 2>/dev/null || true"

// LcmHostStatus beschreibt den Einrichtungszustand des LCM-Hosts.
type LcmHostStatus struct {
	IsLcmHost      bool   `json:"is_lcm_host"`
	PackageManager string `json:"package_manager"`
	// InContainer: LCM läuft in einem Container. Dann gibt es hier nichts
	// einzurichten - die Oberfläche erklärt das, statt Schaltflächen
	// anzubieten, die scheitern müssen.
	InContainer    bool `json:"in_container"`
	TrivyInstalled bool `json:"trivy_installed"` // Trivy auf dem LCM-Host verfügbar
	// SandboxRetrofit meldet, dass die Sandbox nachrüstbar ist: Der Scanner
	// läuft ungesperrt, obwohl der Host sie tragen könnte (apt-System).
	SandboxRetrofit       bool `json:"sandbox_retrofit"`
	AptCacherInstalled    bool `json:"apt_cacher_installed"`    // apt-cacher-ng im Paketbestand
	CrowdSecLapiInstalled bool `json:"crowdsec_lapi_installed"` // CrowdSec (LAPI-Server) im Paketbestand
}

// inContainer meldet die Betriebsart. Als Methode (nicht als direkter Aufruf
// von runtimeenv), damit Tests beide Zweige prüfen können: Ohne das hinge das
// Ergebnis daran, wo die Tests gerade laufen - auf einem Entwicklerrechner
// „Host", im CI-Container „Container", der jeweils andere nie.
func (s *ServerService) inContainer() bool {
	if s.containerCheck != nil {
		return s.containerCheck()
	}
	return runtimeenv.InContainer()
}

// LcmHostStatus liefert den Einrichtungszustand des LCM-Hosts (nur sinnvoll,
// wenn der Server der LCM-Host ist - sonst is_lcm_host=false).
func (s *ServerService) LcmHostStatus(scope repositories.AccessScope, id uint) (*LcmHostStatus, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	st := &LcmHostStatus{
		IsLcmHost:      server.IsLcmHost(),
		PackageManager: server.PackageManager,
		InContainer:    s.inContainer(),
	}
	if !st.IsLcmHost || st.InContainer {
		return st, nil
	}
	// Trivy: maßgeblich ist, ob der LCM-Prozess das Binary findet (== der
	// Zustand, den der CVE-Scan tatsächlich nutzt).
	st.TrivyInstalled = s.ScannerAvailable()
	// Nachrüsten anbieten, solange der Scanner ungesperrt läuft. Ob es dann
	// wirklich klappt, entscheidet der Host - die Schaltfläche verspricht
	// nichts, sie installiert bubblewrap und prüft neu.
	st.SandboxRetrofit = st.TrivyInstalled && !sandbox.Available().Active
	// apt-cacher-ng und crowdsec aus dem erfassten Paketbestand des Hosts ableiten.
	if pkgs, err := s.servers.FindPackages(id); err == nil {
		for i := range pkgs {
			switch pkgs[i].Name {
			case "apt-cacher-ng":
				st.AptCacherInstalled = true
			case "crowdsec":
				st.CrowdSecLapiInstalled = true
			}
		}
	}
	return st, nil
}

// InstallTrivy installiert Trivy auf dem LCM-Host (Aqua-APT-Repo + apt install).
// Nur für den LCM-Host und nur auf apt-Systemen. Nach dem Job wird der
// Paketbestand neu erfasst (Trivy erscheint dann im Inventar), und LCM aktiviert
// den CVE-Scan selbsttätig (Trivy ist dann verfügbar).
func (s *ServerService) InstallTrivy(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	if _, err := s.requireLcmHostApt(scope, id); err != nil {
		return nil, err
	}
	job, err := s.startPackageJob(scope, id, domain.RuleTypeScript,
		"Trivy installieren & einrichten", func(string) string { return trivyInstallScript }, actor)
	if err != nil {
		return nil, err
	}
	if s.enableCVEScan != nil {
		jobID := job.ID
		safego.Go("lcm-host:enable-cve-scan", func() {
			if s.waitJobTerminal(jobID) {
				_ = s.enableCVEScan()
			}
		})
	}
	return job, nil
}

// InstallSandbox rüstet die Sandbox des CVE-Scanners nach (bubblewrap) und
// wertet die Erkennung anschließend neu aus - ohne Neustart des Dienstes.
// Nur für den LCM-Host und nur auf apt-Systemen.
func (s *ServerService) InstallSandbox(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	if _, err := s.requireLcmHostApt(scope, id); err != nil {
		return nil, err
	}
	job, err := s.startPackageJob(scope, id, domain.RuleTypeScript,
		"Sandbox für den CVE-Scanner nachrüsten", func(string) string { return sandboxInstallScript }, actor)
	if err != nil {
		return nil, err
	}
	jobID := job.ID
	safego.Go("lcm-host:recheck-sandbox", func() {
		if !s.waitJobTerminal(jobID) {
			return
		}
		st := sandbox.Recheck()
		// Der Scanner merkt sich seinen Status ebenfalls - sonst zeigte die
		// Oberfläche trotz aktiver Sandbox weiter den alten Stand.
		if f, ok := s.scanner.(interface{ ForgetInfo() }); ok {
			f.ForgetInfo()
		}
		slog.Info("sandbox rechecked after retrofit", "active", st.Active, "backend", st.Backend, "reason", st.Reason)
	})
	return job, nil
}

// InstallAptCacher installiert und aktiviert apt-cacher-ng auf dem LCM-Host und
// trägt danach die lokale Cache-URL in die globalen Einstellungen ein.
func (s *ServerService) InstallAptCacher(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.requireLcmHostApt(scope, id)
	if err != nil {
		return nil, err
	}
	cacheURL := lcmCacheURL(server)
	job, err := s.startPackageJob(scope, id, domain.RuleTypeScript,
		"apt-cacher-ng installieren & einrichten", func(string) string { return aptCacherInstallScript }, actor)
	if err != nil {
		return nil, err
	}
	if s.setAptCacheURL != nil && cacheURL != "" {
		jobID, url := job.ID, cacheURL
		safego.Go("lcm-host:set-apt-cache-url", func() {
			if s.waitJobTerminal(jobID) {
				_ = s.setAptCacheURL(url)
			}
		})
	}
	return job, nil
}

// requireLcmHostApt stellt sicher, dass der Server der LCM-Host mit apt ist, und
// liefert ihn zurück.
func (s *ServerService) requireLcmHostApt(scope repositories.AccessScope, id uint) (*domain.Server, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if !server.IsLcmHost() {
		return nil, ErrNotLcmHost
	}
	// Eine Stelle für alle vier Einrichtungs-Aktionen: Was hier durchkommt,
	// läuft anschließend per SSH als root auf dem vermeintlich eigenen Host.
	if s.inContainer() {
		return nil, ErrLcmHostInContainer
	}
	if server.PackageManager != "apt" {
		return nil, ErrLcmHostNotApt
	}
	return server, nil
}

// lcmCacheURL leitet die von den verwalteten Servern erreichbare Cache-Adresse
// des LCM-Hosts ab: die erste Nicht-Loopback-IP des Hosts (Port 3142),
// ersatzweise sein Host-Name.
func lcmCacheURL(server *domain.Server) string {
	host := firstNonLoopbackIP(server.IPAddresses)
	if host == "" {
		host = strings.TrimSpace(server.Host)
	}
	if host == "" {
		host = "localhost"
	}
	return "http://" + bracketIPv6(host) + ":3142"
}

// bracketIPv6 fasst IPv6-Literale für URLs in eckige Klammern - sonst würde
// der erste Doppelpunkt als Port-Trenner gelesen.
func bracketIPv6(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func firstNonLoopbackIP(csv string) string {
	for _, part := range strings.Split(csv, ",") {
		ip := strings.TrimSpace(part)
		if ip == "" || ip == "::1" || strings.HasPrefix(ip, "127.") {
			continue
		}
		return ip
	}
	return ""
}

// ---- apt-cacher-ng: Statistik-/Einstellungsseite -----------------------

// aptCacherRestartScript startet den Dienst neu und meldet den neuen Zustand.
const aptCacherRestartScript = "systemctl restart apt-cacher-ng && systemctl is-active apt-cacher-ng"

// aptCacherPermanentCacheFile ist die von Debian vorgesehene Schalterdatei:
// /etc/cron.daily/apt-cacher-ng (der tägliche Wartungs-/Ablauf-Job) liest
// NO_CRON_RUN von hier und überspringt den kompletten Lauf, wenn gesetzt -
// der offizielle Weg, die automatische Cache-Bereinigung abzuschalten, ohne
// an der eigentlichen apt-cacher-ng-Konfiguration zu schrauben.
const aptCacherPermanentCacheFile = "/etc/default/apt-cacher-ng"

// aptCacherPermanentCacheScript setzt bzw. entfernt NO_CRON_RUN=1 in der
// Schalterdatei - idempotent, ersetzt eine vorhandene Zeile statt sie zu
// duplizieren.
func aptCacherPermanentCacheScript(enable bool) string {
	if enable {
		return strings.Join([]string{
			"touch " + aptCacherPermanentCacheFile,
			"grep -q '^NO_CRON_RUN=' " + aptCacherPermanentCacheFile +
				" && sed -i 's/^NO_CRON_RUN=.*/NO_CRON_RUN=1/' " + aptCacherPermanentCacheFile +
				" || echo 'NO_CRON_RUN=1' >> " + aptCacherPermanentCacheFile,
		}, " && ")
	}
	return "sed -i '/^NO_CRON_RUN=/d' " + aptCacherPermanentCacheFile + " 2>/dev/null || true"
}

// aptCacherPermanentCacheCheckCmd meldet "yes"/"no", ob die Schalterdatei den
// automatischen Wartungslauf aktuell deaktiviert.
const aptCacherPermanentCacheCheckCmd = "grep -qs '^NO_CRON_RUN=1' " + aptCacherPermanentCacheFile + " && echo yes || echo no"

// aptCacherDiskUsageCmd meldet die Gesamtgröße des Cache-Verzeichnisses
// menschenlesbar (z.B. "1,2G"); leer, wenn das Verzeichnis fehlt.
const aptCacherDiskUsageCmd = "du -sh /var/cache/apt-cacher-ng 2>/dev/null | cut -f1"

// AptCacherDetail bündelt Installationsstatus, Live-Erreichbarkeit,
// Transfer-Statistiken und die "permanentes Caching"-Einstellung für die
// apt-cacher-ng-Detailseite eines LCM-Hosts.
type AptCacherDetail struct {
	Installed      bool            `json:"installed"`
	PermanentCache bool            `json:"permanent_cache"`
	Status         *AptCacheStatus `json:"status,omitempty"`
	Stats          *AptCacheStats  `json:"stats,omitempty"`
}

// AptCacherDetail liefert die Datengrundlage der apt-cacher-ng-Detailseite:
// Installationsstatus aus dem erfassten Paketbestand, Live-Erreichbarkeit +
// Transfer-Statistiken per HTTP-Report (wie CheckAptCache) und den aktuellen
// Stand des "permanentes Caching"-Schalters per SSH-Lesezugriff.
func (s *ServerService) AptCacherDetail(scope repositories.AccessScope, id uint) (*AptCacherDetail, error) {
	server, err := s.requireLcmHostApt(scope, id)
	if err != nil {
		return nil, err
	}
	detail := &AptCacherDetail{}
	if pkgs, err := s.servers.FindPackages(id); err == nil {
		for i := range pkgs {
			if pkgs[i].Name == "apt-cacher-ng" {
				detail.Installed = true
				break
			}
		}
	}
	if !detail.Installed {
		return detail, nil
	}
	if s.aptCacheURL != nil {
		if cacheURL, err := s.aptCacheURL(); err == nil && cacheURL != "" {
			detail.Status, detail.Stats = fetchAcngReport(cacheURL)
		}
	}
	if conn, err := s.connectRead(server); err == nil {
		out, code, runErr := conn.Run(privRun(server, aptCacherPermanentCacheCheckCmd))
		if runErr == nil && code == 0 {
			detail.PermanentCache = strings.TrimSpace(out) == "yes"
		}
		conn.Close()
	}
	return detail, nil
}

// AptCacheOverview bündelt alles für die zentrale APT-Cache-Seite: die
// konfigurierte URL, Live-Erreichbarkeit + Transfer-Statistiken (für JEDE URL,
// egal wo der Cache läuft) und - falls apt-cacher-ng auf dem LCM-Host selbst
// betrieben wird - dessen Verwaltbarkeit (Server-ID für Neustart/Einstellungen,
// Installationsstatus, permanentes Caching, Platten-Belegung des Cache-Verzeichnisses).
type AptCacheOverview struct {
	URL            string          `json:"url"`
	Status         *AptCacheStatus `json:"status,omitempty"`
	Stats          *AptCacheStats  `json:"stats,omitempty"`
	Managed        bool            `json:"managed"` // acng läuft verwaltbar auf dem LCM-Host
	ServerID       uint            `json:"server_id,omitempty"`
	Installed      bool            `json:"installed"`
	PermanentCache bool            `json:"permanent_cache"`
	DiskUsage      string          `json:"disk_usage,omitempty"`
}

// AptCacheOverview liefert die Datengrundlage der zentralen APT-Cache-Seite.
// Erreichbarkeit und Statistiken werden per HTTP-Report für die konfigurierte
// URL ermittelt (funktioniert unabhängig davon, wo der Cache läuft). Die
// Verwaltungs-Aktionen (Neustart, permanentes Caching) sind nur möglich, wenn
// apt-cacher-ng auf dem LCM-Host selbst betrieben wird - nur dorthin hat LCM den
// nötigen SSH-Zugriff; das signalisiert Managed=true samt ServerID.
func (s *ServerService) AptCacheOverview(scope repositories.AccessScope) (*AptCacheOverview, error) {
	ov := &AptCacheOverview{}
	if s.aptCacheURL != nil {
		if url, err := s.aptCacheURL(); err == nil {
			ov.URL = url
		}
	}
	if ov.URL != "" {
		ov.Status, ov.Stats = fetchAcngReport(ov.URL)
	}
	host := s.findLcmHost(scope)
	if host == nil {
		return ov, nil
	}
	ov.ServerID = host.ID
	if pkgs, err := s.servers.FindPackages(host.ID); err == nil {
		for i := range pkgs {
			if pkgs[i].Name == "apt-cacher-ng" {
				ov.Installed = true
				break
			}
		}
	}
	if !ov.Installed {
		return ov, nil
	}
	ov.Managed = true
	if conn, err := s.connectRead(host); err == nil {
		if out, code, runErr := conn.Run(privRun(host, aptCacherPermanentCacheCheckCmd)); runErr == nil && code == 0 {
			ov.PermanentCache = strings.TrimSpace(out) == "yes"
		}
		if out, code, runErr := conn.Run(privRun(host, aptCacherDiskUsageCmd)); runErr == nil && code == 0 {
			ov.DiskUsage = strings.TrimSpace(out)
		}
		conn.Close()
	}
	return ov, nil
}

// findLcmHost sucht den LCM-Host (localhost, apt) unter den für den Aufrufer
// sichtbaren Servern - dort läuft und verwaltet LCM apt-cacher-ng ggf. selbst.
func (s *ServerService) findLcmHost(scope repositories.AccessScope) *domain.Server {
	servers, err := s.servers.FindAll(scope)
	if err != nil {
		return nil
	}
	for i := range servers {
		if servers[i].IsLcmHost() && servers[i].PackageManager == "apt" {
			return &servers[i]
		}
	}
	return nil
}

// RunLcmHostScript führt ein Skript als normalen Job auf dem LCM-Host aus
// (Subscription: apt-Kanal-Umstellung). after wird - sofern gesetzt - nach
// Job-Ende asynchron mit dem Erfolg gerufen; so setzen Aufrufer Folgezustand
// erst, wenn der Job WIRKLICH durch ist, nicht schon beim Start.
func (s *ServerService) RunLcmHostScript(name, script, actor string, after func(ok bool)) (*domain.Job, error) {
	return s.RunLcmHostJob(domain.RuleTypeScript, name, script, actor, after)
}

// RunLcmHostJob ist dasselbe mit wählbarer Job-Art. Die Art ist nicht
// kosmetisch: Sie entscheidet, ob der eingeschränkte Service-User den Lauf
// überhaupt darf (restrictedAllowsRule) und ob der Wiederanlauf einen vom
// Update abgeschnittenen Job als Erfolg wertet (selfUpdateJobTypes).
func (s *ServerService) RunLcmHostJob(jobType, name, script, actor string, after func(ok bool)) (*domain.Job, error) {
	host := s.findLcmHost(repositories.ScopeAll())
	if host == nil {
		return nil, ErrNotLcmHost
	}
	job, err := s.startPackageJob(repositories.ScopeAll(), host.ID, jobType,
		name, func(string) string { return script }, actor)
	if err != nil {
		return nil, err
	}
	if after != nil {
		jobID := job.ID
		safego.Go("lcm-host:script-after", func() { after(s.waitJobTerminal(jobID)) })
	}
	return job, nil
}

// RestartAptCacher startet den apt-cacher-ng-Dienst auf dem LCM-Host neu.
func (s *ServerService) RestartAptCacher(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	if _, err := s.requireLcmHostApt(scope, id); err != nil {
		return nil, err
	}
	return s.startPackageJob(scope, id, domain.RuleTypeScript,
		"apt-cacher-ng neu starten", func(string) string { return aptCacherRestartScript }, actor)
}

// SetAptCacherPermanentCache schaltet den automatischen nächtlichen
// Wartungs-/Ablauflauf von apt-cacher-ng ab (enabled=true → nichts läuft je
// ab, "permanentes Caching") bzw. wieder ein.
func (s *ServerService) SetAptCacherPermanentCache(scope repositories.AccessScope, id uint, enabled bool, actor string) (*domain.Job, error) {
	if _, err := s.requireLcmHostApt(scope, id); err != nil {
		return nil, err
	}
	name := "apt-cacher-ng: permanentes Caching aktivieren"
	if !enabled {
		name = "apt-cacher-ng: permanentes Caching deaktivieren"
	}
	script := aptCacherPermanentCacheScript(enabled)
	return s.startPackageJob(scope, id, domain.RuleTypeScript, name, func(string) string { return script }, actor)
}
