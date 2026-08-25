package services

import (
	"strings"
	"testing"
)

// Realistische Ausgabezeilen (Docker 24/27, --format '{{json .}}').
const dockerPSOut = `{"Command":"\"/docker-entrypoint.sh nginx\"","CreatedAt":"2026-06-01 12:00:00 +0200 CEST","ID":"aaa111bbb222ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee5","Image":"nginx:1.25","Labels":"com.docker.compose.project=webshop,com.docker.compose.service=web","LocalVolumes":"0","Mounts":"","Names":"webshop-web-1","Networks":"webshop_default","Ports":"0.0.0.0:80->80/tcp","RunningFor":"3 days ago","State":"running","Status":"Up 3 days"}
{"Command":"\"redis-server\"","CreatedAt":"2026-06-01 12:00:01 +0200 CEST","ID":"bbb222ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee555fff6","Image":"redis:7","Labels":"","LocalVolumes":"1","Mounts":"data","Names":"cache,extra-alias","Networks":"bridge","Ports":"","RunningFor":"2 weeks ago","State":"exited","Status":"Exited (0) 2 days ago"}
kein json hier`

// Der zweite Container hat GAR KEINE Labels - `index` auf der nil-Map
// liefert dann "<no value>" statt eines leeren Strings (Go-Template;
// im Live-Test bei Standalone-Containern aufgefallen).
const dockerInspectOut = "aaa111bbb222ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee5\twebshop\tweb\t/opt/webshop\t/opt/webshop/compose.yaml\tsha256:img-nginx\n" +
	"bbb222ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee555fff6\t<no value>\t<no value>\t<no value>\t<no value>\tsha256:img-redis\n"

const dockerImagesOut = `{"Containers":"N/A","CreatedAt":"2026-05-01 10:00:00 +0200 CEST","CreatedSince":"5 weeks ago","Digest":"sha256:feedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedface","ID":"sha256:img-nginx","Repository":"nginx","Tag":"1.25","SharedSize":"N/A","Size":"188MB","UniqueSize":"N/A","VirtualSize":"187.7MB"}
{"Containers":"N/A","CreatedAt":"2026-05-02 10:00:00 +0200 CEST","CreatedSince":"5 weeks ago","Digest":"<none>","ID":"sha256:img-local","Repository":"meinapp","Tag":"dev","SharedSize":"N/A","Size":"92MB","UniqueSize":"N/A","VirtualSize":"92MB"}
{"Containers":"N/A","CreatedAt":"2026-05-03 10:00:00 +0200 CEST","CreatedSince":"4 weeks ago","Digest":"<none>","ID":"sha256:img-dangling","Repository":"<none>","Tag":"<none>","SharedSize":"N/A","Size":"10MB","UniqueSize":"N/A","VirtualSize":"10MB"}`

func TestParseDockerPSAndComposeLabels(t *testing.T) {
	containers := parseDockerPS(dockerPSOut)
	if len(containers) != 2 {
		t.Fatalf("erwartete 2 container, bekam %d: %+v", len(containers), containers)
	}
	web := containers[0]
	if web.Name != "webshop-web-1" || web.Image != "nginx:1.25" || web.State != "running" {
		t.Errorf("web-container falsch geparst: %+v", web)
	}
	// Bei mehreren Namen (Links/Aliase) gewinnt der erste.
	if containers[1].Name != "cache" {
		t.Errorf("container-name sollte 'cache' sein, ist %q", containers[1].Name)
	}

	applyComposeLabels(containers, dockerInspectOut)
	if containers[0].ComposeProject != "webshop" || containers[0].ComposeService != "web" {
		t.Errorf("compose-labels fehlen: %+v", containers[0])
	}
	if containers[0].ComposeWorkingDir != "/opt/webshop" || containers[0].ComposeConfigFiles != "/opt/webshop/compose.yaml" {
		t.Errorf("compose-pfade fehlen: %+v", containers[0])
	}
	if containers[0].ImageID != "sha256:img-nginx" || containers[1].ImageID != "sha256:img-redis" {
		t.Errorf("image-ids fehlen: %+v", containers)
	}
	// Standalone-Container bleibt ohne Projekt.
	if containers[1].ComposeProject != "" {
		t.Errorf("cache sollte standalone sein: %+v", containers[1])
	}
}

func TestParseDockerImages(t *testing.T) {
	images := parseDockerImages(dockerImagesOut)
	// Das "<none>"-Repo (dangling layer) wird übersprungen.
	if len(images) != 2 {
		t.Fatalf("erwartete 2 images, bekam %d: %+v", len(images), images)
	}
	nginx := images[0]
	if nginx.Repository != "nginx" || nginx.Tag != "1.25" || nginx.SizeText != "188MB" {
		t.Errorf("nginx falsch geparst: %+v", nginx)
	}
	if !strings.HasPrefix(nginx.RepoDigest, "sha256:feedface") || nginx.CheckStatus != "" {
		t.Errorf("nginx sollte registry-digest und leeren check-status haben: %+v", nginx)
	}
	if nginx.Ref() != "nginx:1.25" {
		t.Errorf("ref sollte 'nginx:1.25' sein, ist %q", nginx.Ref())
	}
	// Lokal gebaut (kein Digest) → nicht gegen die Registry prüfbar.
	local := images[1]
	if local.Repository != "meinapp" || local.RepoDigest != "" || local.CheckStatus != "local" {
		t.Errorf("lokales image falsch: %+v", local)
	}

	if got := parseDockerImages(""); len(got) != 0 {
		t.Errorf("leere ausgabe sollte 0 images liefern, bekam %d", len(got))
	}
}

func TestMarkImagesInUse(t *testing.T) {
	containers := parseDockerPS(dockerPSOut)
	applyComposeLabels(containers, dockerInspectOut)
	images := parseDockerImages(dockerImagesOut)

	markImagesInUse(images, containers)
	if !images[0].InUse {
		t.Errorf("nginx:1.25 wird von einem container genutzt: %+v", images[0])
	}
	if images[1].InUse {
		t.Errorf("meinapp:dev wird von keinem container genutzt: %+v", images[1])
	}
}

func TestScanDockerAbsent(t *testing.T) {
	// Ohne Docker-Binary bleibt alles leer und es laufen keine weiteren
	// Docker-Kommandos.
	res := &scanResult{}
	var commands []string
	run := func(_, cmd string) string {
		commands = append(commands, cmd)
		return "" // command -v docker liefert nichts
	}
	scanDocker(res, "root", false, run)
	if res.HasDocker || res.HasCompose || len(res.DockerContainers) != 0 || len(res.DockerImages) != 0 {
		t.Errorf("ohne docker sollte alles leer bleiben: %+v", res)
	}
	if len(commands) != 1 {
		t.Errorf("nur die docker-probe sollte laufen, liefen: %v", commands)
	}
}

func TestScanDockerWithSudo(t *testing.T) {
	// Als Nicht-root laufen die Docker-Kommandos via sudo.
	res := &scanResult{}
	run := func(_, cmd string) string {
		switch {
		case strings.Contains(cmd, "command -v docker"):
			return "/usr/bin/docker\n"
		case strings.Contains(cmd, "compose version"):
			return "2.27.0\n"
		case strings.Contains(cmd, "docker ps -a --no-trunc"):
			if !strings.HasPrefix(cmd, "sudo sh -c ") {
				t.Errorf("docker ps sollte via sudo laufen: %q", cmd)
			}
			return dockerPSOut
		case strings.Contains(cmd, "docker inspect"):
			return dockerInspectOut
		case strings.Contains(cmd, "docker images"):
			return dockerImagesOut
		}
		return ""
	}
	scanDocker(res, "lcm-svc", false, run)
	if !res.HasDocker || !res.HasCompose {
		t.Fatalf("docker+compose sollten erkannt sein: %+v", res)
	}
	if len(res.DockerContainers) != 2 || len(res.DockerImages) != 2 {
		t.Fatalf("inventar unvollständig: %d container, %d images", len(res.DockerContainers), len(res.DockerImages))
	}
}
