package services

import (
	"fmt"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// DockerImageView ist ein Image samt CVE-Zählern für die UI.
type DockerImageView struct {
	domain.DockerImage
	CriticalVulns int `json:"critical_vulns"`
	HighVulns     int `json:"high_vulns"`
}

// DockerReport ist das Read-Modell des Docker-Tabs eines Servers.
type DockerReport struct {
	HasDocker  bool                     `json:"has_docker"`
	HasCompose bool                     `json:"has_compose"`
	Containers []domain.DockerContainer `json:"containers"`
	Images     []DockerImageView        `json:"images"`
}

// Docker liefert Container, Images und CVE-Zähler eines Servers.
func (s *ServerService) Docker(scope repositories.AccessScope, id uint) (*DockerReport, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	containers, err := s.servers.FindDockerContainers(id)
	if err != nil {
		return nil, err
	}
	images, err := s.servers.FindDockerImages(id)
	if err != nil {
		return nil, err
	}
	vulns, err := s.servers.FindVulnerabilities(id)
	if err != nil {
		return nil, err
	}
	crit := map[string]int{}
	high := map[string]int{}
	for _, v := range vulns {
		if v.Source != domain.VulnSourceDocker {
			continue
		}
		switch v.Severity {
		case domain.SeverityCritical:
			crit[v.ImageRef]++
		case domain.SeverityHigh:
			high[v.ImageRef]++
		}
	}
	report := &DockerReport{
		HasDocker:  server.HasDocker,
		HasCompose: server.HasCompose,
		Containers: containers,
		Images:     make([]DockerImageView, 0, len(images)),
	}
	for _, img := range images {
		report.Images = append(report.Images, DockerImageView{
			DockerImage:   img,
			CriticalVulns: crit[img.Ref()],
			HighVulns:     high[img.Ref()],
		})
	}
	return report, nil
}

// UpdateComposeProject aktualisiert ein Compose-Projekt (optional einen
// einzelnen Service): `docker compose pull && up -d` am Ort des Projekts.
// Projekt/Service werden gegen das gespeicherte Inventar validiert -
// nur dort bekannte Projekte sind ausführbar.
func (s *ServerService) UpdateComposeProject(scope repositories.AccessScope, id uint, project, service, actor string) (*domain.Job, error) {
	if !reComposeName.MatchString(project) {
		return nil, ErrInvalidComposeName
	}
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if !server.HasCompose && !server.IsDemo {
		return nil, ErrComposeUnavailable
	}
	if server.DockerUpdatesDisabled {
		return nil, ErrDockerUpdatesDisabled
	}
	containers, err := s.servers.FindDockerContainers(id)
	if err != nil {
		return nil, err
	}
	var match *domain.DockerContainer
	for i := range containers {
		c := &containers[i]
		if c.ComposeProject != project {
			continue
		}
		if service != "" && c.ComposeService != service {
			continue
		}
		match = c
		break
	}
	if match == nil {
		return nil, ErrComposeProjectUnknown
	}
	script, err := composeUpdateScript(match.ComposeWorkingDir, match.ComposeConfigFiles, service)
	if err != nil {
		return nil, err
	}
	name := "Compose-Update " + project
	if service != "" {
		name += "/" + service
	}
	return s.startDockerJob(server, name, script, actor, true)
}

// PullDockerImage zieht die neueste Version eines Image-Tags auf den
// Server. Der laufende Container wird NICHT neu erstellt (Standalone -
// das Neuerstellen übernimmt der Betreiber bzw. compose up).
func (s *ServerService) PullDockerImage(scope repositories.AccessScope, id uint, image, actor string) (*domain.Job, error) {
	if !reDockerImageRef.MatchString(image) {
		return nil, ErrInvalidDockerRef
	}
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if server.DockerUpdatesDisabled {
		return nil, ErrDockerUpdatesDisabled
	}
	images, err := s.servers.FindDockerImages(id)
	if err != nil {
		return nil, err
	}
	known := false
	for i := range images {
		if images[i].Ref() == image {
			known = true
			break
		}
	}
	if !known {
		return nil, ErrDockerImageUnknown
	}
	return s.startDockerJob(server, "Image aktualisieren: "+image, dockerPullScript(image), actor, true)
}

// PullAllDockerImages zieht die neueste Version ALLER genutzten, getaggten
// Registry-Images des Servers (docker pull je Image, ein Job). Container
// werden - wie beim Einzel-Pull - nicht neu erstellt; lokal gebaute Images
// (ohne RepoDigest) bleiben außen vor.
func (s *ServerService) PullAllDockerImages(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if !server.HasDocker && !server.IsDemo {
		return nil, ErrDockerUnavailable
	}
	if server.DockerUpdatesDisabled {
		return nil, ErrDockerUpdatesDisabled
	}
	images, err := s.servers.FindDockerImages(id)
	if err != nil {
		return nil, err
	}
	var refs []string
	for i := range images {
		img := &images[i]
		if img.InUse && img.Tag != "" && img.RepoDigest != "" {
			refs = append(refs, img.Ref())
		}
	}
	if len(refs) == 0 {
		return nil, ErrNoImagesToUpdate
	}
	name := fmt.Sprintf("Alle Images aktualisieren (%d)", len(refs))
	return s.startDockerJob(server, name, dockerPullAllScript(refs), actor, true)
}

// SetContainerCVERelevance markiert einen Container als CVE-relevant - seine
// Image-CVEs fließen dann mit voller Schwere in die Status-Bewertung ein -
// oder nimmt die Markierung zurück. Die Auswahl hängt am Container-NAMEN
// (Server.CVERelevantContainers) und übersteht damit Image-Updates und
// Inventar-Rescans.
func (s *ServerService) SetContainerCVERelevance(scope repositories.AccessScope, id uint, containerName string, relevant bool, actor string) (*domain.Server, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	containers, err := s.servers.FindDockerContainers(id)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(strings.TrimSpace(containerName))
	known := false
	for i := range containers {
		if strings.ToLower(containers[i].Name) == lower {
			known = true
			break
		}
	}
	if !known {
		return nil, ErrDockerContainerUnknown
	}
	// CSV neu aufbauen: bestehende Auswahl ohne den Container, bei
	// relevant=true wieder anhängen (dedupliziert, case-insensitiv).
	next := make([]string, 0, 4)
	for _, n := range splitCSVList(server.CVERelevantContainers) {
		if n != lower {
			next = append(next, n)
		}
	}
	if relevant {
		next = append(next, lower)
	}
	csv := strings.Join(next, ",")
	if err := s.servers.UpdateFields(id, map[string]any{"cve_relevant_containers": csv}); err != nil {
		return nil, err
	}
	server.CVERelevantContainers = csv
	verb := "nicht CVE-relevant"
	if relevant {
		verb = "CVE-relevant"
	}
	s.audit.Log(actor, "server.docker-cve-relevance", "server", id, server.Name+": "+containerName+" → "+verb)
	return server, nil
}

// DeleteDockerImage löscht ein UNGENUTZTES Image (docker rmi). Nur Images,
// die von keinem Container referenziert werden, sind löschbar - laufende
// Container werden so nie beschädigt.
func (s *ServerService) DeleteDockerImage(scope repositories.AccessScope, id uint, image, actor string) (*domain.Job, error) {
	if !reDockerImageRef.MatchString(image) {
		return nil, ErrInvalidDockerRef
	}
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	images, err := s.servers.FindDockerImages(id)
	if err != nil {
		return nil, err
	}
	var match *domain.DockerImage
	for i := range images {
		if images[i].Ref() == image {
			match = &images[i]
			break
		}
	}
	if match == nil {
		return nil, ErrDockerImageUnknown
	}
	if match.InUse {
		return nil, ErrDockerImageInUse
	}
	return s.startDockerJob(server, "Image löschen: "+image, dockerRemoveImageScript(image), actor, false)
}

// PruneDockerImages entfernt ALLE ungenutzten Images auf einen Schlag
// (docker image prune -af) - von keinem Container referenzierte Images.
// Bequemer als das einzelne Löschen, wenn viele Altbestände anfallen.
func (s *ServerService) PruneDockerImages(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if !server.HasDocker && !server.IsDemo {
		return nil, ErrDockerUnavailable
	}
	return s.startDockerJob(server, "Ungenutzte Images aufräumen", dockerPruneScript(), actor, false)
}

// RefreshDocker liest das Docker-Inventar eines Servers neu ein (reiner
// Scan, ändert nichts am Server).
func (s *ServerService) RefreshDocker(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	return s.startDockerJob(server, "Docker-Inventar aktualisieren", "", actor, false)
}

// startDockerJob legt den Job an (Concurrency-Lock pro Server) und führt
// die Docker-Aktion asynchron aus (Muster startPackageJob). rescanCVEs stößt
// nach erfolgreichem Lauf den zentralen Docker-Check an (Update-Aktionen:
// Pull/Compose - die CVE-Bewertung soll dem neuen Stand sofort folgen).
func (s *ServerService) startDockerJob(server *domain.Server, name, script, actor string, rescanCVEs bool) (*domain.Job, error) {
	job, err := s.jobs.Start(&server.ID, nil, domain.RuleTypeDockerUpdate, name+" @ "+server.Name, actor)
	if err != nil {
		return nil, err // u.a. ErrServerBusy → der Controller mappt auf 409
	}
	s.audit.Log(actor, "server.docker-update", "server", server.ID, name)
	safego.GoCleanup("job:docker", jobPanicCleanup(s.jobs, job), func() {
		s.runDockerJob(job, server, script, actor, rescanCVEs)
	})
	return job, nil
}

// runDockerJob führt das Docker-Skript auf dem Server aus (protokolliert,
// mit dem Job verknüpft) und liest danach das Docker-Inventar neu ein.
// Ein leeres Skript bedeutet: nur Inventar-Rescan.
func (s *ServerService) runDockerJob(job *domain.Job, server *domain.Server, script, actor string, rescanCVEs bool) {
	if server.IsDemo {
		output := "demo-server: docker-inventar aktualisiert (kein ssh-kontakt)"
		if script != "" {
			output = demoSimulateDockerUpdate(s.servers, server)
		}
		s.jobs.Complete(job, output, ptrInt(0), nil)
		return
	}
	conn, err := s.connect(server)
	if err != nil {
		_ = s.servers.UpdateFields(server.ID, unreachableFields(server, err))
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = s.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: actor,
		Purpose: "docker-update", Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	output, code := "", 0
	var runErr error
	if script != "" {
		output, code, runErr = conn.Run(privRun(server, script))
		if runErr == nil && code != 0 {
			runErr = fmt.Errorf("docker-aktion endete mit exit-code %d", code)
		}
	}
	if runErr == nil {
		rescanDockerInto(s.servers, conn, server)
		_ = s.servers.UpdateFields(server.ID, map[string]any{
			"reachable": true, "last_seen_at": time.Now(), "last_error": "", "failed_checks": 0,
		})
		if script == "" {
			output = "Docker-Inventar neu eingelesen."
		}
		// Nach Update-Aktionen die CVE-Bewertung sofort auffrischen: der
		// zentrale Docker-Check gleicht Digests ab und scannt die genutzten
		// Images neu (asynchron, eigener System-Job - best effort).
		if rescanCVEs && s.dockerCheckTrigger != nil {
			output += "\n\nDocker-Check (Registry & Image-CVEs) angestoßen."
			safego.Go("docker-check-trigger", func() { s.dockerCheckTrigger(actor) })
		}
	}
	s.jobs.Complete(job, output, ptrInt(code), runErr)
}
