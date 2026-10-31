package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// Docker-Inventar-Kommandos. `--format '{{json .}}'` liefert eine
// JSON-Zeile pro Eintrag (stabil seit Docker 17.06, geprüft bis 27.x).
// Podman ist bewusst außen vor - dessen JSON-Ausgabe weicht ab.
const (
	// dockerPSCmd listet ALLE Container (auch gestoppte) mit voller ID.
	dockerPSCmd = `docker ps -a --no-trunc --format '{{json .}}' 2>/dev/null`
	// dockerInspectCmd liefert Compose-Labels + Image-ID je Container als
	// Tab-Zeile. `docker ps` selbst gibt Labels nur komma-separiert aus -
	// bei config_files (selbst kommagetrennt!) nicht sicher parsebar,
	// daher der Umweg über inspect mit festem Template. `index` toleriert
	// fehlende Labels (leerer String statt Fehler), xargs -r toleriert
	// null Container.
	dockerInspectCmd = `docker ps -aq | xargs -r docker inspect --format '{{.Id}}{{"\t"}}{{index .Config.Labels "com.docker.compose.project"}}{{"\t"}}{{index .Config.Labels "com.docker.compose.service"}}{{"\t"}}{{index .Config.Labels "com.docker.compose.project.working_dir"}}{{"\t"}}{{index .Config.Labels "com.docker.compose.project.config_files"}}{{"\t"}}{{.Image}}' 2>/dev/null`
	// dockerImagesCmd listet Images mit Manifest-Digest - die
	// Vergleichsbasis für den zentralen Registry-Update-Check.
	dockerImagesCmd = `docker images --no-trunc --digests --format '{{json .}}' 2>/dev/null`
	// dockerUntaggedCmd liefert je Image ohne Tag ID, Größe und früheren
	// RepoDigest - daraus ergibt sich das Repository ("nginx@sha256:…" →
	// "nginx"). Ein Pull mit neuem Digest entzieht dem alten Image seinen Tag.
	// Je nach Docker steht es dann als "<none>" oder unter dem Repo-Namen ohne
	// Tag in `docker images` - oder fehlt dort ganz (Docker 29 mit containerd-
	// Image-Speicher); der Dangling-Filter kennt es immer. `range` statt `join`:
	// Docker liefert dort []interface{}.
	dockerUntaggedCmd = `docker images -qf dangling=true --no-trunc | xargs -r docker image inspect --format '{{.Id}}{{"\t"}}{{.Size}}{{"\t"}}{{range .RepoDigests}}{{.}} {{end}}' 2>/dev/null`
)

// scanDocker erfasst das Docker-Inventar (Container + Images) über eine
// bestehende SSH-Verbindung. Docker-Kommandos brauchen root - sie laufen
// via wrapSudo unter dem Login-User. Fehlt Docker, bleibt alles leer;
// einzelne fehlschlagende Kommandos brechen den Scan nicht ab (run liefert
// dann "" - wie beim übrigen System-Scan).
func scanDocker(res *scanResult, loginUser string, restricted bool, run func(label, cmd string) string) {
	res.HasDocker = firstLine(run("has-docker", "command -v docker 2>/dev/null || true")) != ""
	if !res.HasDocker {
		return
	}
	sudo := func(cmd string) string { return wrapSudo(loginUser, restricted, cmd) }
	// Compose v2 ist ein CLI-Plugin ("docker compose"); v1 (docker-compose)
	// ist EOL und außen vor - ohne Plugin gibt es keine Projekt-Updates.
	res.HasCompose = strings.TrimSpace(run("compose", sudo("docker compose version --short 2>/dev/null || true"))) != ""
	res.DockerContainers = parseDockerPS(run("docker-ps", sudo(dockerPSCmd)))
	applyComposeLabels(res.DockerContainers, run("docker-inspect", sudo(dockerInspectCmd)))
	untagged := parseUntaggedImages(run("docker-untagged", sudo(dockerUntaggedCmd)))
	res.DockerImages = parseDockerImages(run("docker-images", sudo(dockerImagesCmd)), untagged)
	markImagesInUse(res.DockerImages, res.DockerContainers)
}

// parseDockerPS parst die JSON-Zeilen von `docker ps -a --format '{{json .}}'`.
// Nicht parsebare Zeilen werden übersprungen (fehlertolerant wie parseSnapList).
func parseDockerPS(out string) []domain.DockerContainer {
	var containers []domain.DockerContainer
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var row struct {
			ID     string `json:"ID"`
			Names  string `json:"Names"`
			Image  string `json:"Image"`
			State  string `json:"State"`
			Status string `json:"Status"`
			Ports  string `json:"Ports"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil || row.ID == "" {
			continue
		}
		containers = append(containers, domain.DockerContainer{
			ContainerID: row.ID,
			// Bei verlinkten Containern listet Names mehrere Einträge -
			// der erste ist der eigentliche Container-Name.
			Name:   strings.SplitN(row.Names, ",", 2)[0],
			Image:  row.Image,
			State:  row.State,
			Status: row.Status,
			Ports:  row.Ports,
		})
	}
	return containers
}

// applyComposeLabels ergänzt Compose-Labels und Image-ID aus den
// Tab-Zeilen von dockerInspectCmd (Zuordnung über die volle Container-ID).
func applyComposeLabels(containers []domain.DockerContainer, out string) {
	byID := make(map[string]*domain.DockerContainer, len(containers))
	for i := range containers {
		byID[containers[i].ContainerID] = &containers[i]
	}
	for _, line := range strings.Split(out, "\n") {
		cols := strings.Split(line, "\t")
		if len(cols) < 6 {
			continue
		}
		c, ok := byID[strings.TrimSpace(cols[0])]
		if !ok {
			continue
		}
		c.ComposeProject = cleanTemplateValue(cols[1])
		c.ComposeService = cleanTemplateValue(cols[2])
		c.ComposeWorkingDir = cleanTemplateValue(cols[3])
		c.ComposeConfigFiles = cleanTemplateValue(cols[4])
		c.ImageID = strings.TrimSpace(cols[5])
	}
}

// cleanTemplateValue normalisiert eine Go-Template-Ausgabe: Container ganz
// OHNE Labels haben eine nil-Map, und `index` auf nil liefert den String
// "<no value>" statt eines leeren Strings (im Live-Test auf Ubuntu 24.04
// bei Standalone-Containern aufgefallen).
func cleanTemplateValue(s string) string {
	s = strings.TrimSpace(s)
	if s == "<no value>" {
		return ""
	}
	return s
}

// parseDockerImages parst die JSON-Zeilen von `docker images --digests`.
// Images ohne Tag sind überholte Fassungen (Superseded): Ein Pull mit neuem
// Digest hat ihnen den Tag entzogen. Ihr Repository steht in der Liste
// (mit Digest) oder fehlt ("<none>"); dann liefert untagged den früheren Namen,
// sonst bleibt es leer. Untagged Images, die die Liste gar nicht führt, kommen
// aus untagged dazu. Images ohne Registry-Digest (lokal gebaut oder ohne Tag)
// sind nicht gegen die Registry prüfbar → CheckStatus "local".
func parseDockerImages(out string, untagged map[string]untaggedImage) []domain.DockerImage {
	var images []domain.DockerImage
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var row struct {
			Repository string `json:"Repository"`
			Tag        string `json:"Tag"`
			ID         string `json:"ID"`
			Digest     string `json:"Digest"`
			Size       string `json:"Size"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil || row.Repository == "" {
			continue
		}
		img := domain.DockerImage{Repository: row.Repository, ImageID: row.ID, SizeText: row.Size}
		if row.Repository == "<none>" {
			img.Repository = untagged[row.ID].Repository
		}
		if row.Tag != "" && row.Tag != "<none>" {
			img.Tag = row.Tag
		} else {
			img.Superseded = true
		}
		if row.Digest != "" && row.Digest != "<none>" {
			img.RepoDigest = row.Digest
		}
		if img.RepoDigest == "" || img.Tag == "" {
			img.CheckStatus = domain.DockerCheckLocal
		}
		// Ein Image mit mehreren RepoDigests steht mehrfach in der Liste.
		if key := img.Repository + "|" + img.Tag + "|" + img.ImageID; !seen[key] {
			seen[key] = true
			images = append(images, img)
		}
	}
	return append(images, unlistedUntagged(images, untagged)...)
}

// unlistedUntagged liefert die untagged Images, die `docker images` nicht
// führt (Docker 29 mit containerd-Image-Speicher), in stabiler Reihenfolge.
func unlistedUntagged(listed []domain.DockerImage, untagged map[string]untaggedImage) []domain.DockerImage {
	known := map[string]bool{}
	for i := range listed {
		known[listed[i].ImageID] = true
	}
	var ids []string
	for id := range untagged {
		if !known[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]domain.DockerImage, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.DockerImage{
			Repository: untagged[id].Repository, ImageID: id, SizeText: dockerSize(untagged[id].Bytes),
			Superseded: true, CheckStatus: domain.DockerCheckLocal,
		})
	}
	return out
}

// untaggedImage ist, was `dockerUntaggedCmd` über ein Image ohne Tag weiß.
type untaggedImage struct {
	Repository string
	Bytes      int64
}

// parseUntaggedImages liest "<image-id>\t<bytes>\t<repodigest> …" je Zeile.
// Das Repository stammt aus dem ersten RepoDigest und bleibt ohne ihn leer.
func parseUntaggedImages(out string) map[string]untaggedImage {
	images := map[string]untaggedImage{}
	for _, line := range strings.Split(out, "\n") {
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) < 3 || strings.TrimSpace(cols[0]) == "" {
			continue
		}
		img := untaggedImage{}
		img.Bytes, _ = strconv.ParseInt(strings.TrimSpace(cols[1]), 10, 64)
		if digests := strings.Fields(cols[2]); len(digests) > 0 {
			img.Repository, _, _ = strings.Cut(digests[0], "@")
		}
		images[strings.TrimSpace(cols[0])] = img
	}
	return images
}

// dockerSize formatiert Bytes wie `docker images` (dezimal: "41.1MB").
func dockerSize(b int64) string {
	switch {
	case b >= 1_000_000_000:
		return fmt.Sprintf("%.3gGB", float64(b)/1e9)
	case b >= 1_000_000:
		return fmt.Sprintf("%.3gMB", float64(b)/1e6)
	case b >= 1_000:
		return fmt.Sprintf("%.3gkB", float64(b)/1e3)
	}
	return fmt.Sprintf("%dB", b)
}

// markImagesInUse markiert Images, die von mindestens einem Container
// referenziert werden (Zuordnung primär über die Image-ID aus inspect,
// zur Sicherheit auch über die Ref, falls inspect nichts lieferte). Eine
// überholte Fassung hat keinen Tag; ihre Ref wäre nur der Repo-Name und
// würde das aktuelle latest treffen - sie zählt daher nur über die ID.
func markImagesInUse(images []domain.DockerImage, containers []domain.DockerContainer) {
	usedID := make(map[string]bool, len(containers))
	usedRef := make(map[string]bool, len(containers))
	for _, c := range containers {
		if c.ImageID != "" {
			usedID[c.ImageID] = true
		}
		if c.Image != "" {
			usedRef[c.Image] = true
		}
	}
	for i := range images {
		images[i].InUse = usedID[images[i].ImageID] || (!images[i].Superseded && usedRef[images[i].Ref()])
	}
}

// rescanDockerInto liest das Docker-Inventar eines Servers über eine
// bestehende Verbindung neu ein und ersetzt Container + Images in der DB
// (best effort, nach Docker-Aktionen - Pendant zu rescanPackagesInto).
func rescanDockerInto(servers *repositories.ServerRepository, conn sshx.Conn, server *domain.Server) {
	res := &scanResult{}
	run := func(_, cmd string) string { out, _, _ := conn.Run(cmd); return out }
	scanDocker(res, server.ServiceUser, server.RestrictedSudo, run)
	// has_docker/has_compose IMMER mitschreiben - auch wenn Docker gerade
	// nicht (mehr) vorhanden ist. Der dedizierte Docker-Refresh ließ die
	// Flags sonst auf dem alten Stand, sodass er isoliert genutzt
	// unvollständig wirkte (R2-006); erst der volle Scan zog nach.
	_ = servers.UpdateFields(server.ID, map[string]any{
		"has_docker": res.HasDocker, "has_compose": res.HasCompose,
	})
	if !res.HasDocker {
		return
	}
	_ = servers.ReplaceDockerContainers(server.ID, res.DockerContainers)
	_ = servers.ReplaceDockerImages(server.ID, res.DockerImages)
	// CVE-Funde von Images, die jetzt nicht mehr genutzt werden (ersetzt,
	// entfernt, Container gestoppt), sofort abräumen - nicht erst beim
	// nächsten täglichen Docker-Check. Nur genutzte Images tragen CVEs.
	var inUse []string
	for i := range res.DockerImages {
		if img := &res.DockerImages[i]; img.InUse && !img.Superseded {
			inUse = append(inUse, img.Ref())
		}
	}
	_ = servers.DeleteDockerVulnerabilitiesExcept(server.ID, inUse)
}
