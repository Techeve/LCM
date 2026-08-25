package repositories

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"LCM/internal/core/domain"
)

// ServerRepository verwaltet Server samt Paketen und Repositories.
// Alle lesenden/schreibenden Zugriffe laufen über einen AccessScope.
type ServerRepository struct {
	db *gorm.DB
}

func NewServerRepository(db *gorm.DB) *ServerRepository {
	return &ServerRepository{db: db}
}

func (r *ServerRepository) Create(server *domain.Server) error {
	// NameBIdx setzt der BeforeSave-Hook (domain.Server) - greift auf allen
	// Schreibpfaden, auch bei direktem db.Create in Seed/Tests.
	return r.db.Create(server).Error
}

func (r *ServerRepository) Update(server *domain.Server) error {
	return r.db.Save(server).Error
}

// UpdateFields aktualisiert einzelne Spalten (z.B. Health-Check-Ergebnis),
// ohne konkurrierende Änderungen anderer Felder zu überschreiben.
func (r *ServerRepository) UpdateFields(id uint, fields map[string]any) error {
	return r.db.Model(&domain.Server{}).Where("id = ?", id).Updates(fields).Error
}

func (r *ServerRepository) FindByID(scope AccessScope, id uint) (*domain.Server, error) {
	var server domain.Server
	q := scope.scopeServers(r.db.Preload("Groups"))
	if err := q.First(&server, id).Error; err != nil {
		return nil, translate(err)
	}
	return &server, nil
}

// FindByName sucht über den deterministischen Blindindex - der Name selbst
// liegt verschlüsselt (und pro Zeile zufällig) in der Spalte und ist nicht
// direkt abfragbar.
func (r *ServerRepository) FindByName(name string) (*domain.Server, error) {
	var server domain.Server
	if err := r.db.Where("name_bidx = ?", BlindIndex(name)).First(&server).Error; err != nil {
		return nil, translate(err)
	}
	return &server, nil
}

// HostExists meldet, ob bereits ein Server mit dieser Host/IP-Port-Kombination
// existiert. Eindeutig ist das PAAR aus Host und SSH-Port - hinter derselben
// IP können per Port-Forwarding/NAT mehrere Maschinen stehen (z. B.
// 203.0.113.7:2201 und 203.0.113.7:2202); nur der Host allein würde solche
// legitimen Setups fälschlich als Duplikat abweisen. Der Host liegt
// AES-verschlüsselt und ohne Blindindex in der DB und ist daher nicht per SQL
// abfragbar - deshalb werden alle Server geladen und in Go verglichen
// (getrimmt, case-insensitiv; Port 0 zählt als 22). excludeID > 0 schließt
// einen Server (z. B. den gerade bearbeiteten) von der Prüfung aus.
func (r *ServerRepository) HostExists(host string, port int, excludeID uint) (bool, error) {
	target := strings.ToLower(strings.TrimSpace(host))
	if target == "" {
		return false, nil
	}
	if port <= 0 {
		port = 22
	}
	var servers []domain.Server
	if err := r.db.Select("id", "host", "ssh_port").Find(&servers).Error; err != nil {
		return false, err
	}
	for i := range servers {
		if servers[i].ID == excludeID {
			continue
		}
		sPort := servers[i].SSHPort
		if sPort <= 0 {
			sPort = 22
		}
		if strings.ToLower(strings.TrimSpace(servers[i].Host)) == target && sPort == port {
			return true, nil
		}
	}
	return false, nil
}

func (r *ServerRepository) FindAll(scope AccessScope) ([]domain.Server, error) {
	var servers []domain.Server
	// Kein ORDER BY name: der Name ist verschlüsselt (Sortierung wäre
	// bedeutungslos) - nach dem Entschlüsseln in Go sortieren.
	q := scope.scopeServers(r.db.Preload("Groups"))
	if err := q.Find(&servers).Error; err != nil {
		return nil, err
	}
	sortByName(servers)
	return servers, nil
}

// FindAllUnscoped liefert alle Server - nur für interne Prozesse
// (Scheduler, System-Sync), nie für User-Requests verwenden.
func (r *ServerRepository) FindAllUnscoped() ([]domain.Server, error) {
	var servers []domain.Server
	if err := r.db.Find(&servers).Error; err != nil {
		return nil, err
	}
	sortByName(servers)
	return servers, nil
}

// sortByName sortiert Server nach dem (entschlüsselten) Namen - App-seitig,
// da der Name at rest verschlüsselt liegt und nicht per SQL sortierbar ist.
func sortByName(servers []domain.Server) {
	sort.Slice(servers, func(i, j int) bool {
		return strings.ToLower(servers[i].Name) < strings.ToLower(servers[j].Name)
	})
}

// FindByIDUnscoped liefert einen Server ohne Zugriffsprüfung - nur für interne
// Prozesse (Scheduler, CVE-Scan), nie für User-Requests.
func (r *ServerRepository) FindByIDUnscoped(id uint) (*domain.Server, error) {
	var server domain.Server
	if err := r.db.First(&server, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &server, nil
}

// FindByAgentID liefert den Server zu einer Agent-UUID (MQTT-Username) -
// für die Broker-Authentifizierung, daher ohne AccessScope. Die Spalte ist
// Klartext (zufällige UUID, nicht sensibel), ein Blindindex ist unnötig.
func (r *ServerRepository) FindByAgentID(agentID string) (*domain.Server, error) {
	if agentID == "" {
		return nil, ErrNotFound
	}
	var server domain.Server
	if err := r.db.First(&server, "agent_id = ?", agentID).Error; err != nil {
		return nil, translate(err)
	}
	return &server, nil
}

// Delete entfernt den Server und ALLE damit verbundenen Daten restlos:
// Pakete (apt + Snaps), CVE-Funde, Repos, Gruppen-/Benutzer-Zuordnungen, Jobs
// sowie die SSH-Protokolle (Sessions samt Kommandos). Der tamper-evidente
// Audit-Log bleibt als Compliance-Nachweis unberührt.
func (r *ServerRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_ref = ?", ServerRef(id)).Delete(&domain.Package{}).Error; err != nil {
			return err
		}
		if err := tx.Where("server_id = ?", id).Delete(&domain.SnapPackage{}).Error; err != nil {
			return err
		}
		if err := tx.Where("server_id = ?", id).Delete(&domain.DockerContainer{}).Error; err != nil {
			return err
		}
		if err := tx.Where("server_id = ?", id).Delete(&domain.DockerImage{}).Error; err != nil {
			return err
		}
		if err := tx.Where("server_ref = ?", ServerRef(id)).Delete(&domain.Vulnerability{}).Error; err != nil {
			return err
		}
		if err := tx.Where("server_ref = ?", ServerRef(id)).Delete(&domain.AdvisoryFinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("server_id = ?", id).Delete(&domain.StorageHistory{}).Error; err != nil {
			return err
		}
		if err := tx.Where("server_id = ?", id).Delete(&domain.AptRepository{}).Error; err != nil {
			return err
		}
		// Offene Benutzer-Abgleiche verfallen mit dem Server - sie hätten
		// sonst kein Ziel mehr.
		if err := tx.Where("server_id = ?", id).Delete(&domain.PendingUserSync{}).Error; err != nil {
			return err
		}
		server := domain.Server{ID: id}
		if err := tx.Model(&server).Association("Groups").Clear(); err != nil {
			return err
		}
		if err := tx.Model(&server).Association("LinuxUsers").Clear(); err != nil {
			return err
		}
		// SSH-Protokolle löschen: erst die Kommandos der zugehörigen Sessions,
		// dann die Sessions selbst.
		if err := tx.Where("ssh_session_id IN (SELECT id FROM ssh_sessions WHERE server_id = ?)", id).
			Delete(&domain.SSHCommand{}).Error; err != nil {
			return err
		}
		if err := tx.Where("server_id = ?", id).Delete(&domain.SSHSession{}).Error; err != nil {
			return err
		}
		// Job-Historie dieses Servers löschen.
		if err := tx.Where("server_id = ?", id).Delete(&domain.Job{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&domain.Server{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ---- Pakete & Repositories (Scan-Ergebnisse) --------------------------------

// ReplacePackages ersetzt den Paketbestand eines Servers atomar durch
// das Ergebnis eines neuen Scans.
func (r *ServerRepository) ReplacePackages(serverID uint, packages []domain.Package) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		ref := ServerRef(serverID)
		if err := tx.Where("server_ref = ?", ref).Delete(&domain.Package{}).Error; err != nil {
			return err
		}
		for i := range packages {
			packages[i].ID = "" // neue UUID vergibt BeforeCreate
			packages[i].ServerRef = ref
		}
		if len(packages) == 0 {
			return nil
		}
		return tx.CreateInBatches(packages, 500).Error
	})
}

func (r *ServerRepository) ReplaceRepositories(serverID uint, repos []domain.AptRepository) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.AptRepository{}).Error; err != nil {
			return err
		}
		for i := range repos {
			repos[i].ID = 0
			repos[i].ServerID = serverID
		}
		if len(repos) == 0 {
			return nil
		}
		return tx.Create(repos).Error
	})
}

func (r *ServerRepository) FindPackages(serverID uint) ([]domain.Package, error) {
	var pkgs []domain.Package
	if err := r.db.Where("server_ref = ?", ServerRef(serverID)).Order("name").Find(&pkgs).Error; err != nil {
		return nil, err
	}
	return pkgs, nil
}

func (r *ServerRepository) FindOutdatedPackages(serverID uint) ([]domain.Package, error) {
	var pkgs []domain.Package
	if err := r.db.Where("server_ref = ? AND candidate_version != '' AND candidate_version != version", ServerRef(serverID)).
		Order("name").Find(&pkgs).Error; err != nil {
		return nil, err
	}
	return pkgs, nil
}

func (r *ServerRepository) CountOutdatedPackages(serverID uint) (int64, error) {
	var n int64
	err := r.db.Model(&domain.Package{}).
		Where("server_ref = ? AND candidate_version != '' AND candidate_version != version", ServerRef(serverID)).
		Count(&n).Error
	return n, err
}

// CountPackages zählt den erfassten Paketbestand. Null bedeutet, dass die
// Bestandsaufnahme nie gelungen ist - für die Ampel ein eigener, kritischer
// Zustand, denn ohne Pakete melden alle abgeleiteten Kriterien "nichts
// gefunden" und der Server erschiene makellos (siehe TrafficLightInput).
func (r *ServerRepository) CountPackages(serverID uint) (int64, error) {
	var n int64
	err := r.db.Model(&domain.Package{}).Where("server_ref = ?", ServerRef(serverID)).Count(&n).Error
	return n, err
}

// ReplaceSnapPackages ersetzt den Snap-Bestand eines Servers atomar durch
// das Ergebnis eines neuen Scans (analog zu ReplacePackages für apt).
func (r *ServerRepository) ReplaceSnapPackages(serverID uint, snaps []domain.SnapPackage) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.SnapPackage{}).Error; err != nil {
			return err
		}
		for i := range snaps {
			snaps[i].ID = "" // neue UUID vergibt BeforeCreate
			snaps[i].ServerID = serverID
		}
		if len(snaps) == 0 {
			return nil
		}
		return tx.CreateInBatches(snaps, 500).Error
	})
}

// FindSnapPackages liefert die installierten Snaps eines Servers.
func (r *ServerRepository) FindSnapPackages(serverID uint) ([]domain.SnapPackage, error) {
	var snaps []domain.SnapPackage
	if err := r.db.Where("server_id = ?", serverID).Order("name").Find(&snaps).Error; err != nil {
		return nil, err
	}
	return snaps, nil
}

// ---- Docker (Container + Images) ---------------------------------------------

// ReplaceDockerContainers ersetzt den Container-Bestand eines Servers
// atomar durch das Ergebnis eines neuen Scans.
func (r *ServerRepository) ReplaceDockerContainers(serverID uint, containers []domain.DockerContainer) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.DockerContainer{}).Error; err != nil {
			return err
		}
		for i := range containers {
			containers[i].ID = "" // neue UUID vergibt BeforeCreate
			containers[i].ServerID = serverID
		}
		if len(containers) == 0 {
			return nil
		}
		return tx.CreateInBatches(containers, 500).Error
	})
}

// ReplaceDockerImages ersetzt den Image-Bestand eines Servers atomar.
// Die Ergebnisse des zentralen Update-Checks (CandidateDigest & Co.)
// werden dabei per (repository, tag) von den alten auf die neuen Zeilen
// übertragen - sonst würde jeder System-Sync das nächtliche
// Check-Ergebnis verwerfen. Ändert sich der lokale RepoDigest (Image
// wurde gezogen), verfällt das alte Ergebnis bewusst.
func (r *ServerRepository) ReplaceDockerImages(serverID uint, images []domain.DockerImage) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var old []domain.DockerImage
		if err := tx.Where("server_id = ?", serverID).Find(&old).Error; err != nil {
			return err
		}
		prev := make(map[string]*domain.DockerImage, len(old))
		for i := range old {
			prev[old[i].Repository+":"+old[i].Tag] = &old[i]
		}
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.DockerImage{}).Error; err != nil {
			return err
		}
		for i := range images {
			images[i].ID = "" // neue UUID vergibt BeforeCreate
			images[i].ServerID = serverID
			p, ok := prev[images[i].Repository+":"+images[i].Tag]
			if !ok || images[i].RepoDigest != p.RepoDigest || images[i].CheckStatus == domain.DockerCheckLocal {
				continue
			}
			images[i].CandidateDigest = p.CandidateDigest
			images[i].UpdateAvailable = p.UpdateAvailable
			images[i].CheckStatus = p.CheckStatus
			images[i].CheckError = p.CheckError
			images[i].LastCheckedAt = p.LastCheckedAt
		}
		if len(images) == 0 {
			return nil
		}
		return tx.CreateInBatches(images, 500).Error
	})
}

// FindDockerContainers liefert die Container eines Servers (Compose-
// Projekte zusammenhängend, darin nach Name).
func (r *ServerRepository) FindDockerContainers(serverID uint) ([]domain.DockerContainer, error) {
	var containers []domain.DockerContainer
	if err := r.db.Where("server_id = ?", serverID).
		Order("compose_project, name").Find(&containers).Error; err != nil {
		return nil, err
	}
	return containers, nil
}

// FindDockerImages liefert die Images eines Servers.
func (r *ServerRepository) FindDockerImages(serverID uint) ([]domain.DockerImage, error) {
	var images []domain.DockerImage
	if err := r.db.Where("server_id = ?", serverID).
		Order("repository, tag").Find(&images).Error; err != nil {
		return nil, err
	}
	return images, nil
}

// CountOutdatedDockerImages zählt genutzte Images mit verfügbarem Update
// (speist die Ampel-Bewertung).
func (r *ServerRepository) CountOutdatedDockerImages(serverID uint) (int64, error) {
	var n int64
	err := r.db.Model(&domain.DockerImage{}).
		Where("server_id = ? AND update_available = ? AND in_use = ?", serverID, true, true).
		Count(&n).Error
	return n, err
}

// FindAllDockerImagesUnscoped liefert die Images ALLER Server - für den
// zentralen Update-Check/CVE-Scan (System-Job, keine Mandantensicht).
func (r *ServerRepository) FindAllDockerImagesUnscoped() ([]domain.DockerImage, error) {
	var images []domain.DockerImage
	if err := r.db.Order("repository, tag").Find(&images).Error; err != nil {
		return nil, err
	}
	return images, nil
}

// UpdateDockerImageCheck schreibt das Ergebnis des zentralen Update-Checks
// feldweise auf eine Image-Zeile.
func (r *ServerRepository) UpdateDockerImageCheck(id string, fields map[string]any) error {
	return r.db.Model(&domain.DockerImage{}).Where("id = ?", id).Updates(fields).Error
}

// ---- Sicherheitslücken (CVE-Scan) -------------------------------------------

// severityOrder sortiert nach Schwere (kritisch zuerst). Wird in mehreren
// Abfragen als ORDER-BY-Ausdruck verwendet.
const severityOrder = "CASE severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1" +
	" WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END"

// ReplaceVulnerabilities ersetzt die gefundenen CVEs EINER QUELLE eines
// Servers atomar durch das Ergebnis eines neuen Scans. OS-Scan (Quelle
// "os") und Docker-Image-Scan (Quelle "docker") laufen getrennt und
// dürfen die Funde der jeweils anderen Quelle nicht löschen.
func (r *ServerRepository) ReplaceVulnerabilities(serverID uint, source string, vulns []domain.Vulnerability) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		ref := ServerRef(serverID)
		if err := tx.Where("server_ref = ? AND source = ?", ref, source).Delete(&domain.Vulnerability{}).Error; err != nil {
			return err
		}
		for i := range vulns {
			vulns[i].ID = "" // neue UUID vergibt BeforeCreate
			vulns[i].ServerRef = ref
			vulns[i].Source = source
		}
		if len(vulns) == 0 {
			return nil
		}
		return tx.CreateInBatches(vulns, 500).Error
	})
}

// FindVulnerabilities liefert die CVEs eines Servers (kritischste zuerst).
func (r *ServerRepository) FindVulnerabilities(serverID uint) ([]domain.Vulnerability, error) {
	var vulns []domain.Vulnerability
	if err := r.db.Where("server_ref = ?", ServerRef(serverID)).
		Order(severityOrder).Order("package_name").Order("cve_id").
		Find(&vulns).Error; err != nil {
		return nil, err
	}
	return vulns, nil
}

// deepScanSeverityOrder sortiert Befunde schwerste zuerst.
const deepScanSeverityOrder = "CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END"

// SaveDeepScanReport legt einen neuen Lauf samt seiner Befunde an und
// bestimmt dabei den Fortschritt gegenüber dem vorhergehenden Lauf: welche
// Befunde sind neu, welche seither verschwunden.
//
// Der Vergleich passiert HIER, in derselben Transaktion, in der der Report
// entsteht - nicht später beim Lesen. Sonst hinge die Aussage „3 behoben" an
// Daten, die inzwischen weggeräumt sein können (Retention), und ein alter
// Report würde seine eigene Geschichte je nach Aufbewahrung anders erzählen.
func (r *ServerRepository) SaveDeepScanReport(report *domain.DeepScanReport, findings []domain.DeepScanFinding) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Vorgänger laden (nur die Befund-Identitäten, nicht der ganze Bericht).
		var prev domain.DeepScanReport
		hasPrev := tx.Where("server_id = ?", report.ServerID).
			Order("created_at DESC").First(&prev).Error == nil

		prevKeys := map[string]string{} // Key → Titel
		if hasPrev {
			var prevFindings []domain.DeepScanFinding
			if err := tx.Where("report_id = ?", prev.ID).Find(&prevFindings).Error; err != nil {
				return err
			}
			for _, f := range prevFindings {
				prevKeys[domain.DeepScanFindingKey(f)] = f.Title
			}
		}

		report.ID = ""
		report.Critical, report.Warnings, report.Infos = 0, 0, 0
		seen := map[string]bool{}
		for i := range findings {
			switch findings[i].Severity {
			case domain.DeepScanCritical:
				report.Critical++
			case domain.DeepScanWarning:
				report.Warnings++
			default:
				report.Infos++
			}
			key := domain.DeepScanFindingKey(findings[i])
			seen[key] = true
			// Neu ist ein Befund nur im Vergleich zu einem Vorlauf. Ohne
			// Vorgänger wäre jeder Befund „neu" - das ist keine Information,
			// sondern nur der erste Stand.
			findings[i].IsNew = hasPrev && prevKeys[key] == ""
			if findings[i].IsNew {
				report.NewFindings++
			}
		}
		var resolved []string
		for key, title := range prevKeys {
			if !seen[key] {
				resolved = append(resolved, title)
			}
		}
		sort.Strings(resolved) // stabile Reihenfolge (Map-Iteration ist zufällig)
		report.ResolvedFindings = len(resolved)
		if len(resolved) > 0 {
			raw, err := json.Marshal(resolved)
			if err != nil {
				return err
			}
			report.ResolvedTitles = string(raw)
		}

		if err := tx.Omit(clause.Associations).Create(report).Error; err != nil {
			return err
		}
		for i := range findings {
			findings[i].ID = "" // neue UUID vergibt BeforeCreate
			findings[i].ServerID = report.ServerID
			findings[i].ReportID = report.ID
		}
		if len(findings) == 0 {
			return nil
		}
		return tx.CreateInBatches(findings, 500).Error
	})
}

// FindDeepScanReports liefert die Lauf-Historie eines Servers, neueste zuerst
// (ohne Befunde - die kommen erst beim Aufklappen eines Laufs).
func (r *ServerRepository) FindDeepScanReports(serverID uint, limit int) ([]domain.DeepScanReport, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var reports []domain.DeepScanReport
	if err := r.db.Where("server_id = ?", serverID).
		Order("created_at DESC").Limit(limit).Find(&reports).Error; err != nil {
		return nil, err
	}
	return reports, nil
}

// FindDeepScanReport liefert einen einzelnen Lauf samt seiner Befunde. Die
// Server-ID wird mitgeprüft, damit eine fremde Report-ID nicht die Befunde
// eines anderen Servers ausliefert.
func (r *ServerRepository) FindDeepScanReport(serverID uint, reportID string) (*domain.DeepScanReport, error) {
	var report domain.DeepScanReport
	if err := r.db.Where("id = ? AND server_id = ?", reportID, serverID).First(&report).Error; err != nil {
		return nil, err
	}
	findings, err := r.findDeepScanFindingsOfReport(report.ID)
	if err != nil {
		return nil, err
	}
	report.Findings = findings
	return &report, nil
}

func (r *ServerRepository) findDeepScanFindingsOfReport(reportID string) ([]domain.DeepScanFinding, error) {
	var findings []domain.DeepScanFinding
	if err := r.db.Where("report_id = ?", reportID).
		Order(deepScanSeverityOrder).Order("category").Order("title").
		Find(&findings).Error; err != nil {
		return nil, err
	}
	return findings, nil
}

// FindDeepScanFindings liefert die Befunde des JÜNGSTEN Laufs eines Servers,
// schwerste zuerst - der aktuelle Stand, auf den Ampel, Insights und
// Alarm-Auswertung schauen.
func (r *ServerRepository) FindDeepScanFindings(serverID uint) ([]domain.DeepScanFinding, error) {
	var latest domain.DeepScanReport
	err := r.db.Where("server_id = ?", serverID).Order("created_at DESC").First(&latest).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.findDeepScanFindingsOfReport(latest.ID)
}

// CleanupDeepScanReports hält die Historie je Server auf die jüngsten `keep`
// Läufe. Die Befunde der ausgemusterten Läufe gehen mit - ohne das wüchse die
// Tabelle bei täglichen Scans unbegrenzt.
func (r *ServerRepository) CleanupDeepScanReports(keep int) (int64, error) {
	if keep <= 0 {
		keep = 30
	}
	var serverIDs []uint
	if err := r.db.Model(&domain.DeepScanReport{}).Distinct().Pluck("server_id", &serverIDs).Error; err != nil {
		return 0, err
	}
	var deleted int64
	for _, sid := range serverIDs {
		var keepIDs []string
		if err := r.db.Model(&domain.DeepScanReport{}).Where("server_id = ?", sid).
			Order("created_at DESC").Limit(keep).Pluck("id", &keepIDs).Error; err != nil {
			return deleted, err
		}
		if len(keepIDs) == 0 {
			continue
		}
		err := r.db.Transaction(func(tx *gorm.DB) error {
			var oldIDs []string
			if err := tx.Model(&domain.DeepScanReport{}).
				Where("server_id = ? AND id NOT IN ?", sid, keepIDs).
				Pluck("id", &oldIDs).Error; err != nil {
				return err
			}
			if len(oldIDs) == 0 {
				return nil
			}
			if err := tx.Where("report_id IN ?", oldIDs).Delete(&domain.DeepScanFinding{}).Error; err != nil {
				return err
			}
			res := tx.Where("id IN ?", oldIDs).Delete(&domain.DeepScanReport{})
			deleted += res.RowsAffected
			return res.Error
		})
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// VulnFact ist der schlanke CVE-Auszug für die gewichtete Status-Bewertung:
// Schwere, Quelle (os/docker), Paketname, - bei Docker-Funden - die
// betroffene Image-Referenz, und ob ein Fix existiert (FixedVersion leer =
// der Hersteller hat noch nichts veröffentlicht; R2-056).
type VulnFact struct {
	Severity     string
	Source       string
	PackageName  string
	ImageRef     string
	FixedVersion string
}

// Fixable meldet, ob für den Fund eine behebende Version existiert - nur
// solche Funde fließen in Ampel und Alarme ein (R2-056: Unbehebbares darf
// ein voll gepflegtes System nicht dauerhaft rot halten).
func (f VulnFact) Fixable() bool { return f.FixedVersion != "" }

// VulnerabilityFacts liefert alle CVE-Fakten eines Servers (leichtgewichtig,
// ohne Titel/URLs) - Basis der gewichteten Ampel- und Alarm-Bewertung.
func (r *ServerRepository) VulnerabilityFacts(serverID uint) ([]VulnFact, error) {
	var facts []VulnFact
	err := r.db.Model(&domain.Vulnerability{}).
		Select("severity, source, package_name, image_ref, fixed_version").
		Where("server_ref = ?", ServerRef(serverID)).
		Scan(&facts).Error
	return facts, err
}

// DeleteDockerVulnerabilitiesExcept entfernt Docker-CVE-Funde des Servers,
// deren Image nicht (mehr) in der übergebenen Liste genutzter Images steht -
// Sofort-Bereinigung nach jedem Inventar-Rescan, damit CVEs ersetzter oder
// ungenutzter Images nicht bis zum nächsten täglichen Docker-Check stehen
// bleiben.
func (r *ServerRepository) DeleteDockerVulnerabilitiesExcept(serverID uint, inUseRefs []string) error {
	q := r.db.Where("server_ref = ? AND source = ?", ServerRef(serverID), domain.VulnSourceDocker)
	if len(inUseRefs) > 0 {
		q = q.Where("image_ref NOT IN ?", inUseRefs)
	}
	return q.Delete(&domain.Vulnerability{}).Error
}

func (r *ServerRepository) FindRepositories(serverID uint) ([]domain.AptRepository, error) {
	var repos []domain.AptRepository
	if err := r.db.Where("server_id = ?", serverID).Order("line").Find(&repos).Error; err != nil {
		return nil, err
	}
	return repos, nil
}

// ---- Speicher-Verlauf (Festplattenbelegung über die Zeit) -------------------

// RecordStorageSample fasst eine Messung (Kapazität + Nutzung) in den
// Tagesdurchschnitt eines Servers ein. Existiert für den Tag noch keine Zeile,
// wird sie angelegt; sonst wird der laufende Durchschnitt fortgeschrieben
// (avg' = (avg*n + wert)/(n+1)). Der Aufrufer serialisiert je Server über den
// Job-Lock, daher ist Read-modify-write hier unkritisch.
func (r *ServerRepository) RecordStorageSample(serverID uint, day string, totalMB, usedMB int64, at time.Time) error {
	var h domain.StorageHistory
	err := r.db.Where("server_id = ? AND day = ?", serverID, day).First(&h).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.Create(&domain.StorageHistory{
			ServerID: serverID, Day: day,
			DiskTotalMB: totalMB, DiskUsedMB: usedMB,
			Samples: 1, LastSampleAt: at,
		}).Error
	}
	if err != nil {
		return err
	}
	n := int64(h.Samples)
	h.DiskTotalMB = (h.DiskTotalMB*n + totalMB) / (n + 1)
	h.DiskUsedMB = (h.DiskUsedMB*n + usedMB) / (n + 1)
	h.Samples++
	h.LastSampleAt = at
	return r.db.Save(&h).Error
}

// LatestStorageSampleAt liefert den Zeitpunkt der jüngsten Messung eines
// Servers (Nullzeit, wenn noch keine existiert) - Grundlage der stündlichen
// Drosselung.
func (r *ServerRepository) LatestStorageSampleAt(serverID uint) (time.Time, error) {
	var h domain.StorageHistory
	err := r.db.Where("server_id = ?", serverID).Order("last_sample_at DESC").First(&h).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, nil
	}
	return h.LastSampleAt, err
}

// ReplaceDiskVolumes ersetzt den erfassten Volume-Bestand eines Servers
// atomar (analog zu ReplacePackages) - die eingehängten Dateisysteme aus dem
// letzten Full-Scan.
func (r *ServerRepository) ReplaceDiskVolumes(serverID uint, volumes []domain.DiskVolume) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.DiskVolume{}).Error; err != nil {
			return err
		}
		for i := range volumes {
			volumes[i].ID = 0
			volumes[i].ServerID = serverID
		}
		if len(volumes) == 0 {
			return nil
		}
		return tx.Create(volumes).Error
	})
}

// FindDiskVolumes liefert die eingehängten Dateisysteme eines Servers, Root „/"
// zuerst, danach alphabetisch nach Mountpoint.
func (r *ServerRepository) FindDiskVolumes(serverID uint) ([]domain.DiskVolume, error) {
	var vols []domain.DiskVolume
	if err := r.db.Where("server_id = ?", serverID).
		Order("mountpoint = '/' DESC, mountpoint").Find(&vols).Error; err != nil {
		return nil, err
	}
	return vols, nil
}

// ReplaceServerUsers ersetzt die beim Scan erfassten Linux-Konten eines
// Servers atomar (analog zu ReplaceDiskVolumes). Eine leere Liste heißt
// „Scan konnte die Konten nicht erheben" - dann bleibt der alte Bestand
// stehen (root ist immer anmeldefähig, eine echte leere Liste gibt es nicht).
func (r *ServerRepository) ReplaceServerUsers(serverID uint, users []domain.ServerUser) error {
	if len(users) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.ServerUser{}).Error; err != nil {
			return err
		}
		for i := range users {
			users[i].ID = 0
			users[i].ServerID = serverID
		}
		return tx.Create(users).Error
	})
}

// FindServerUsers liefert die erfassten Linux-Konten eines Servers,
// UID-aufsteigend (root zuerst).
func (r *ServerRepository) FindServerUsers(serverID uint) ([]domain.ServerUser, error) {
	var users []domain.ServerUser
	if err := r.db.Where("server_id = ?", serverID).Order("uid").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// ---- Gesperrte Konten je Server ---------------------------------------------

// BlockServerUser sperrt ein von LCM verwaltetes Konto auf EINEM Server.
// Idempotent: ein zweiter Aufruf ändert nichts.
func (r *ServerRepository) BlockServerUser(serverID uint, username, actor string) error {
	bidx := domain.UserBlindIndex(username)
	var existing domain.ServerUserBlock
	err := r.db.Where("server_id = ? AND username_bidx = ?", serverID, bidx).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return r.db.Create(&domain.ServerUserBlock{ServerID: serverID, Username: username, Actor: actor}).Error
}

// UnblockServerUser hebt die Sperre auf (kein Fehler, wenn keine bestand).
func (r *ServerRepository) UnblockServerUser(serverID uint, username string) error {
	return r.db.Where("server_id = ? AND username_bidx = ?", serverID, domain.UserBlindIndex(username)).
		Delete(&domain.ServerUserBlock{}).Error
}

// BlockedServerUsers liefert die gesperrten Kontonamen eines Servers.
func (r *ServerRepository) BlockedServerUsers(serverID uint) (map[string]bool, error) {
	var rows []domain.ServerUserBlock
	if err := r.db.Where("server_id = ?", serverID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for i := range rows {
		out[rows[i].Username] = true
	}
	return out, nil
}

// ---- Login-Historie ----------------------------------------------------------

// ReplaceServerUserLogins ersetzt die erfassten Anmeldungen eines Servers.
// Leere Liste heißt „nicht erhebbar" (kein wtmp, kein last) - dann bleibt der
// alte Bestand stehen, statt eine gefüllte Historie grundlos zu leeren.
func (r *ServerRepository) ReplaceServerUserLogins(serverID uint, logins []domain.ServerUserLogin) error {
	if len(logins) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.ServerUserLogin{}).Error; err != nil {
			return err
		}
		for i := range logins {
			logins[i].ID = 0
			logins[i].ServerID = serverID
		}
		return tx.Create(logins).Error
	})
}

// FindServerUserLogins liefert die Anmeldungen eines Kontos, neueste zuerst.
func (r *ServerRepository) FindServerUserLogins(serverID uint, username string, limit int) ([]domain.ServerUserLogin, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []domain.ServerUserLogin
	err := r.db.Where("server_id = ? AND username_bidx = ?", serverID, domain.UserBlindIndex(username)).
		Order("started_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// CountServerUserLogins zählt die erfassten Anmeldungen je Konto eines Servers.
func (r *ServerRepository) CountServerUserLogins(serverID uint) (map[string]int, error) {
	var rows []domain.ServerUserLogin
	if err := r.db.Where("server_id = ?", serverID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int{}
	for i := range rows {
		out[rows[i].Username]++
	}
	return out, nil
}

// FindStorageHistory liefert den Speicher-Verlauf eines Servers, chronologisch
// aufsteigend (ältester Tag zuerst) - passend für die grafische Darstellung.
func (r *ServerRepository) FindStorageHistory(serverID uint) ([]domain.StorageHistory, error) {
	var hist []domain.StorageHistory
	if err := r.db.Where("server_id = ?", serverID).Order("day").Find(&hist).Error; err != nil {
		return nil, err
	}
	return hist, nil
}

// DeleteStorageHistoryOlderThan entfernt Tages-Snapshots vor dem Stichtag
// (Tag im Format "2006-01-02"; die lexikografische Sortierung entspricht der
// chronologischen). Für die konfigurierbare Aufbewahrung (Log-Bereinigung).
func (r *ServerRepository) DeleteStorageHistoryOlderThan(cutoffDay string) (int64, error) {
	res := r.db.Where("day < ?", cutoffDay).Delete(&domain.StorageHistory{})
	return res.RowsAffected, res.Error
}

// ---- Globale Paket-Sichten ---------------------------------------------------

// PackageOverviewRow: ein Paket über alle (sichtbaren) Server hinweg.
type PackageOverviewRow struct {
	Name        string `json:"name"`
	ServerCount int    `json:"server_count"`
	Outdated    int    `json:"outdated"` // auf wie vielen Servern veraltet
}

// GlobalPackageOverview aggregiert den Paketbestand über alle (im Scope
// sichtbaren) Server: je Paketname die Anzahl betroffener Server und wie
// viele davon eine ältere als die Kandidat-Version fahren.
func (r *ServerRepository) GlobalPackageOverview(scope AccessScope) ([]PackageOverviewRow, error) {
	var rows []PackageOverviewRow
	q := scope.scopeServers(r.db.Table("packages").
		Select("packages.name AS name, COUNT(DISTINCT packages.server_ref) AS server_count," +
			" SUM(CASE WHEN packages.candidate_version != '' AND packages.candidate_version != packages.version THEN 1 ELSE 0 END) AS outdated").
		Joins("JOIN servers ON servers.ref = packages.server_ref")).
		Group("packages.name").Order("packages.name")
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// VulnerablePackageRow: kritisches (Security-)Update mit betroffenem Server.
type VulnerablePackageRow struct {
	ServerID         uint   `json:"server_id"`
	ServerName       string `json:"server_name"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	CandidateVersion string `json:"candidate_version"`
	Security         bool   `json:"security"`
}

// GlobalVulnerablePackages listet offene Security-Updates über alle (im
// Scope sichtbaren) Server - die Grundlage der globalen Sicherheitsseite.
func (r *ServerRepository) GlobalVulnerablePackages(scope AccessScope) ([]VulnerablePackageRow, error) {
	var rows []VulnerablePackageRow
	// Kein SQL-ORDER BY servers.name: der Name ist verschlüsselt. Nach dem
	// Entschlüsseln in Go sortieren.
	q := scope.scopeServers(r.db.Table("packages").
		Select("servers.id AS server_id, servers.name AS server_name, packages.name, packages.version,"+
			" packages.candidate_version, packages.security").
		Joins("JOIN servers ON servers.ref = packages.server_ref").
		Where("packages.security = ? AND packages.candidate_version != '' AND packages.candidate_version != packages.version", true)).
		Order("packages.name")
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].ServerName = decryptField(rows[i].ServerName)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if a, b := strings.ToLower(rows[i].ServerName), strings.ToLower(rows[j].ServerName); a != b {
			return a < b
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

// DockerImageOverviewRow: ein einzigartiges Docker-Image über alle
// sichtbaren Server hinweg (globale Docker-Übersicht).
// DockerServerRef ist ein Server, auf dem ein Image liegt - mit Namen, damit
// die Übersicht nicht nur eine Zahl zeigt. Wer wissen will, WO ein Image mit
// kritischen Lücken liegt, musste bisher jeden Server einzeln aufsuchen.
type DockerServerRef struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type DockerImageOverviewRow struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	// Servers sind die betroffenen Server, alphabetisch. ServerCount bleibt
	// als eigenes Feld erhalten: Die Oberfläche zeigt nur die ersten paar
	// Namen, die Gesamtzahl aber immer.
	Servers         []DockerServerRef `json:"servers"`
	ServerCount     int               `json:"server_count"`
	InUseCount      int               `json:"in_use_count"`
	UpdateAvailable bool              `json:"update_available"` // auf mind. einem Server
	Unverifiable    bool              `json:"unverifiable"`     // privates Image (anonym nicht prüfbar)
	LocalOnly       bool              `json:"local_only"`       // lokal gebaut (kein Registry-Digest)
	CriticalVulns   int               `json:"critical_vulns"`
	HighVulns       int               `json:"high_vulns"`
}

// GlobalDockerOverview aggregiert die Docker-Images aller sichtbaren
// Server pro (repository, tag) und reichert sie um die CVE-Zähler des
// Image-Scans an (distinct CVE-IDs - nicht über Server aufsummiert).
func (r *ServerRepository) GlobalDockerOverview(scope AccessScope) ([]DockerImageOverviewRow, error) {
	var raw []struct {
		Repository      string
		Tag             string
		ServerCount     int
		InUseCount      int
		UpdateAvailable int
		Unverifiable    int
		LocalOnly       int
	}
	q := scope.scopeServers(r.db.Table("docker_images").
		Select("docker_images.repository, docker_images.tag," +
			" COUNT(DISTINCT docker_images.server_id) AS server_count," +
			" SUM(CASE WHEN docker_images.in_use THEN 1 ELSE 0 END) AS in_use_count," +
			" MAX(CASE WHEN docker_images.update_available THEN 1 ELSE 0 END) AS update_available," +
			" MAX(CASE WHEN docker_images.check_status = 'unauthorized' THEN 1 ELSE 0 END) AS unverifiable," +
			" MIN(CASE WHEN docker_images.check_status = 'local' THEN 1 ELSE 0 END) AS local_only").
		Joins("JOIN servers ON servers.id = docker_images.server_id")).
		Group("docker_images.repository, docker_images.tag").
		Order("docker_images.repository, docker_images.tag")
	if err := q.Scan(&raw).Error; err != nil {
		return nil, err
	}

	// CVE-Zähler je Image-Referenz (distinct CVE-IDs über die sichtbaren
	// Server - dieselbe Lücke auf 12 Servern zählt einmal).
	var vulnRows []struct {
		ImageRef string
		Severity string
		N        int
	}
	vq := scope.scopeServers(r.db.Table("vulnerabilities").
		Select("vulnerabilities.image_ref, vulnerabilities.severity, COUNT(DISTINCT vulnerabilities.cve_id) AS n").
		Joins("JOIN servers ON servers.ref = vulnerabilities.server_ref").
		Where("vulnerabilities.source = ?", domain.VulnSourceDocker)).
		Group("vulnerabilities.image_ref, vulnerabilities.severity")
	if err := vq.Scan(&vulnRows).Error; err != nil {
		return nil, err
	}
	crit := map[string]int{}
	high := map[string]int{}
	for _, v := range vulnRows {
		switch v.Severity {
		case domain.SeverityCritical:
			crit[v.ImageRef] = v.N
		case domain.SeverityHigh:
			high[v.ImageRef] = v.N
		}
	}

	// Server je Image nachladen. Bewusst eine zweite Abfrage statt einer
	// String-Aggregation im SQL: Die Namen liegen verschlüsselt in der
	// Datenbank und müssen einzeln entschlüsselt werden - ein
	// zusammengeklebtes GROUP_CONCAT wäre nicht mehr aufzutrennen.
	serversByImage := map[string][]DockerServerRef{}
	var serverRows []struct {
		Repository string
		Tag        string
		ID         uint
		Name       string
	}
	if err := scope.scopeServers(r.db.Table("docker_images").
		Select("docker_images.repository, docker_images.tag, servers.id, servers.name").
		Joins("JOIN servers ON servers.id = docker_images.server_id")).
		Scan(&serverRows).Error; err != nil {
		return nil, err
	}
	for _, row := range serverRows {
		key := row.Repository + "\x00" + row.Tag
		serversByImage[key] = append(serversByImage[key], DockerServerRef{
			ID: row.ID, Name: decryptField(row.Name),
		})
	}
	for key := range serversByImage {
		list := serversByImage[key]
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		serversByImage[key] = list
	}

	rows := make([]DockerImageOverviewRow, 0, len(raw))
	for _, row := range raw {
		ref := row.Repository
		if row.Tag != "" {
			ref += ":" + row.Tag
		}
		rows = append(rows, DockerImageOverviewRow{
			Repository:      row.Repository,
			Tag:             row.Tag,
			Servers:         serversByImage[row.Repository+"\x00"+row.Tag],
			ServerCount:     row.ServerCount,
			InUseCount:      row.InUseCount,
			UpdateAvailable: row.UpdateAvailable > 0,
			Unverifiable:    row.Unverifiable > 0,
			LocalOnly:       row.LocalOnly > 0,
			CriticalVulns:   crit[ref],
			HighVulns:       high[ref],
		})
	}
	return rows, nil
}

// CVERow: eine gefundene Sicherheitslücke samt betroffenem Server (globale
// Sicht über alle sichtbaren Server).
type CVERow struct {
	ServerID         uint   `json:"server_id"`
	ServerName       string `json:"server_name"`
	CVEID            string `json:"cve_id"`
	PackageName      string `json:"package_name"`
	InstalledVersion string `json:"installed_version"`
	FixedVersion     string `json:"fixed_version"`
	Severity         string `json:"severity"`
	Title            string `json:"title"`
	PrimaryURL       string `json:"primary_url"`
	Source           string `json:"source"`    // "os" | "docker"
	ImageRef         string `json:"image_ref"` // nur bei source=docker
}

// GlobalVulnerabilities listet alle CVE-Funde über die sichtbaren Server
// hinweg, kritischste zuerst.
func (r *ServerRepository) GlobalVulnerabilities(scope AccessScope) ([]CVERow, error) {
	var rows []CVERow
	// Primär nach Schwere sortieren (SQL); der Servername ist verschlüsselt,
	// daher NICHT per SQL - nach dem Entschlüsseln in Go als Sekundärschlüssel.
	q := scope.scopeServers(r.db.Table("vulnerabilities").
		Select("servers.id AS server_id, servers.name AS server_name, vulnerabilities.cve_id,"+
			" vulnerabilities.package_name, vulnerabilities.installed_version, vulnerabilities.fixed_version,"+
			" vulnerabilities.severity, vulnerabilities.title, vulnerabilities.primary_url,"+
			" vulnerabilities.source, vulnerabilities.image_ref").
		Joins("JOIN servers ON servers.ref = vulnerabilities.server_ref")).
		// Wie in GlobalVulnerabilitiesPage: ausgenommene Server bringen ihre
		// Container-Funde hier nicht ein.
		Where("COALESCE(vulnerabilities.source, '') <> ? OR servers.docker_cves_ignored = ?", "docker", false).
		Order("vulnerabilities.package_name")
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].ServerName = decryptField(rows[i].ServerName)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		// Kleinerer SeverityRank = schwerer → kritischste zuerst.
		ri := (&domain.Vulnerability{Severity: rows[i].Severity}).SeverityRank()
		rj := (&domain.Vulnerability{Severity: rows[j].Severity}).SeverityRank()
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(rows[i].ServerName) < strings.ToLower(rows[j].ServerName)
	})
	return rows, nil
}

// VulnPage ist eine paginierte CVE-Sicht samt Gesamtzahl und Schwere-Summary
// (die Summary zählt über ALLE Treffer, nicht nur die aktuelle Seite).
type VulnPage struct {
	Items    []CVERow       `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Summary  map[string]int `json:"summary"`
}

// cveSeverityOrder sortiert per SQL nach Schwere (kritischste zuerst); der
// Paketname ist Sekundärschlüssel. Der (verschlüsselte) Servername kann nicht
// als SQL-Sortierschlüssel dienen und entfällt hier als Tiebreak.
const cveSeverityOrder = "CASE vulnerabilities.severity" +
	" WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END," +
	" vulnerabilities.package_name"

// GlobalVulnerabilitiesPage liefert eine Seite der CVEs (kritischste zuerst)
// plus Gesamtzahl und Schwere-Summary. Serverseitig paginiert, damit die
// Security-Seite bei großen Beständen nicht Tausende Zeilen auf einmal lädt.
// source filtert die Quelle: "os" = nur nativ installierte Pakete (Docker-CVEs
// ausgeblendet), "docker" = nur Container-Images, "" = alle.
func (r *ServerRepository) GlobalVulnerabilitiesPage(scope AccessScope, page, pageSize int, source string) (*VulnPage, error) {
	base := func() *gorm.DB {
		q := scope.scopeServers(r.db.Table("vulnerabilities").
			Joins("JOIN servers ON servers.ref = vulnerabilities.server_ref"))
		// Server, für die Docker-CVEs abgeschaltet sind, tauchen mit ihren
		// Container-Funden hier gar nicht auf - auch nicht über den
		// Quellen-Filter „docker". Der Schalter meint die Übersicht, sonst
		// bliebe die Ruhe auf einen Klick beschränkt.
		q = q.Where("COALESCE(vulnerabilities.source, '') <> ? OR servers.docker_cves_ignored = ?", "docker", false)
		switch source {
		case "os":
			// Nur nativ installierte Pakete - Docker-CVEs ausblenden. Alt-Zeilen
			// ohne gesetzte Quelle (leer/NULL) zählen als „os".
			q = q.Where("COALESCE(vulnerabilities.source, '') <> ?", "docker")
		case "docker":
			q = q.Where("vulnerabilities.source = ?", "docker")
		}
		return q
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, err
	}
	// Schwere-Summary über ALLE Treffer (für die Badges).
	summary := map[string]int{}
	var sumRows []struct {
		Severity string
		N        int
	}
	if err := base().Select("vulnerabilities.severity AS severity, COUNT(*) AS n").
		Group("vulnerabilities.severity").Scan(&sumRows).Error; err != nil {
		return nil, err
	}
	for _, s := range sumRows {
		summary[s.Severity] = s.N
	}
	// Die eigentliche Seite.
	var rows []CVERow
	q := base().
		Select("servers.id AS server_id, servers.name AS server_name, vulnerabilities.cve_id," +
			" vulnerabilities.package_name, vulnerabilities.installed_version, vulnerabilities.fixed_version," +
			" vulnerabilities.severity, vulnerabilities.title, vulnerabilities.primary_url," +
			" vulnerabilities.source, vulnerabilities.image_ref").
		Order(cveSeverityOrder).Limit(pageSize).Offset((page - 1) * pageSize)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].ServerName = decryptField(rows[i].ServerName)
	}
	return &VulnPage{Items: rows, Total: total, Page: page, PageSize: pageSize, Summary: summary}, nil
}

// ---- Abgeschottete Verzeichnisse --------------------------------------------

// HardenedPaths liefert die Verzeichnisse, deren Welt-Zugriff LCM auf diesem
// Server entfernt hat.
func (r *ServerRepository) HardenedPaths(serverID uint) ([]domain.HardenedPath, error) {
	var rows []domain.HardenedPath
	err := r.db.Where("server_id = ?", serverID).Order("path").Find(&rows).Error
	return rows, err
}

// FindHardenedPath lädt einen Eintrag; die Server-Zugehörigkeit wird geprüft.
func (r *ServerRepository) FindHardenedPath(serverID, id uint) (*domain.HardenedPath, error) {
	var row domain.HardenedPath
	if err := r.db.Where("server_id = ? AND id = ?", serverID, id).First(&row).Error; err != nil {
		return nil, translate(err)
	}
	return &row, nil
}

// SaveHardenedPath legt den Eintrag an oder frischt ihn auf - derselbe Pfad
// zweimal gehärtet ist ein Eintrag, kein zweiter.
func (r *ServerRepository) SaveHardenedPath(h *domain.HardenedPath) error {
	var existing domain.HardenedPath
	err := r.db.Where("server_id = ? AND path = ?", h.ServerID, h.Path).First(&existing).Error
	if err == nil {
		// Vorzustand des ERSTEN Eingriffs behalten: Sonst zeigte die Rücknahme
		// auf den bereits gehärteten Zustand und wäre wirkungslos.
		h.ID, h.PrevMode, h.PrevGroup = existing.ID, existing.PrevMode, existing.PrevGroup
		return r.db.Save(h).Error
	}
	return r.db.Create(h).Error
}

// DeleteHardenedPath entfernt den Eintrag nach der Rücknahme.
func (r *ServerRepository) DeleteHardenedPath(id uint) error {
	return r.db.Delete(&domain.HardenedPath{}, id).Error
}
