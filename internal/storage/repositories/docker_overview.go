package repositories

import (
	"sort"
	"strconv"
	"strings"
)

// Globale Docker-Sichten über alle sichtbaren Server.
//
// Die Bilder-Übersicht (GlobalDockerOverview) beantwortet „welches Image
// liegt wo". Die beiden Sichten hier beantworten die zwei anderen Fragen,
// die man an eine Docker-Landschaft hat: „was läuft gerade" und „welche
// Compose-Projekte gibt es". Alle drei aus derselben Tabelle zu lesen wäre
// möglich, aber die Zuschnitte sind verschieden genug, dass eine gemeinsame
// Abfrage nur noch Sonderfälle enthielte.

// DockerContainerRow ist ein Container samt Server (globale Sicht).
type DockerContainerRow struct {
	ServerID       uint   `json:"server_id"`
	ServerName     string `json:"server_name"`
	Name           string `json:"name"`
	Image          string `json:"image"`
	State          string `json:"state"`
	Status         string `json:"status"`
	Ports          string `json:"ports"`
	ComposeProject string `json:"compose_project"`
	ComposeService string `json:"compose_service"`
}

// GlobalDockerContainers listet die Container aller sichtbaren Server.
// Laufende zuerst - sie sind das, wonach man zuerst sucht.
func (r *ServerRepository) GlobalDockerContainers(scope AccessScope) ([]DockerContainerRow, error) {
	var rows []DockerContainerRow
	q := scope.scopeServers(r.db.Table("docker_containers").
		Select("servers.id AS server_id, servers.name AS server_name," +
			" docker_containers.name, docker_containers.image, docker_containers.state," +
			" docker_containers.status, docker_containers.ports," +
			" docker_containers.compose_project, docker_containers.compose_service").
		Joins("JOIN servers ON servers.id = docker_containers.server_id")).
		Order("CASE WHEN docker_containers.state = 'running' THEN 0 ELSE 1 END").
		Order("docker_containers.name")
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].ServerName = decryptField(rows[i].ServerName)
	}
	return rows, nil
}

// DockerComposeRow ist ein Compose-Projekt über alle Server hinweg.
type DockerComposeRow struct {
	Project string `json:"project"`
	// Servers sind die Server, auf denen das Projekt läuft. Ein Projekt kann
	// auf mehreren liegen (gleicher Name, getrennte Installationen) - das
	// ist eine Information, keine Verwechslung, und wird deshalb gezeigt.
	Servers []DockerServerRef `json:"servers"`
	// Services sind die Dienstnamen des Projekts, alphabetisch.
	Services []string `json:"services"`
	// Containers/Running: Gesamtzahl und davon laufend. Die Differenz ist
	// das, was einen an einem Compose-Projekt interessiert.
	Containers int `json:"containers"`
	Running    int `json:"running"`
	// WorkingDir ist das Verzeichnis der Projektdefinition (aus den Labels);
	// leer, wenn Compose es nicht gemeldet hat.
	WorkingDir string `json:"working_dir"`
}

// GlobalDockerCompose gruppiert die Container zu Compose-Projekten.
// Container ohne Projekt-Label bleiben außen vor - sie sind keine.
func (r *ServerRepository) GlobalDockerCompose(scope AccessScope) ([]DockerComposeRow, error) {
	var raw []struct {
		Project    string
		Service    string
		State      string
		WorkingDir string
		ServerID   uint
		ServerName string
	}
	q := scope.scopeServers(r.db.Table("docker_containers").
		Select("docker_containers.compose_project AS project,"+
			" docker_containers.compose_service AS service, docker_containers.state,"+
			" docker_containers.compose_working_dir AS working_dir,"+
			" servers.id AS server_id, servers.name AS server_name").
		Joins("JOIN servers ON servers.id = docker_containers.server_id")).
		Where("COALESCE(docker_containers.compose_project, '') <> ?", "")
	if err := q.Scan(&raw).Error; err != nil {
		return nil, err
	}

	byProject := map[string]*DockerComposeRow{}
	seenServer := map[string]bool{}
	seenService := map[string]bool{}
	for _, row := range raw {
		p, ok := byProject[row.Project]
		if !ok {
			p = &DockerComposeRow{Project: row.Project, WorkingDir: row.WorkingDir}
			byProject[row.Project] = p
		}
		p.Containers++
		if row.State == "running" {
			p.Running++
		}
		if p.WorkingDir == "" {
			p.WorkingDir = row.WorkingDir
		}
		if key := row.Project + "\x00" + strconv.FormatUint(uint64(row.ServerID), 10); !seenServer[key] {
			seenServer[key] = true
			p.Servers = append(p.Servers, DockerServerRef{
				ID: row.ServerID, Name: decryptField(row.ServerName),
			})
		}
		if svc := strings.TrimSpace(row.Service); svc != "" {
			if key := row.Project + "\x00" + svc; !seenService[key] {
				seenService[key] = true
				p.Services = append(p.Services, svc)
			}
		}
	}

	out := make([]DockerComposeRow, 0, len(byProject))
	for _, p := range byProject {
		sort.Slice(p.Servers, func(i, j int) bool { return p.Servers[i].Name < p.Servers[j].Name })
		sort.Strings(p.Services)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return out, nil
}
