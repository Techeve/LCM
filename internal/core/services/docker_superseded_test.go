package services

import (
	"testing"

	"LCM/internal/core/domain"
)

// Ausgabe von Docker 26 nach einem Pull, der dem alten nginx den Tag nahm:
// Mit RepoDigest steht das Image als "nginx <none>" in der Liste, ohne als
// "<none> <none>". Beides sind überholte Fassungen.
const dockerSupersededOut = `{"Digest":"sha256:aaaa","ID":"sha256:img-new-latest","Repository":"nginx","Tag":"latest","Size":"68MB"}
{"Digest":"sha256:77e5","ID":"sha256:img-old-latest","Repository":"nginx","Tag":"<none>","Size":"142MB"}
{"Digest":"sha256:77e5","ID":"sha256:img-old-latest","Repository":"nginx","Tag":"<none>","Size":"142MB"}
{"Digest":"<none>","ID":"sha256:img-orphan","Repository":"<none>","Tag":"<none>","Size":"9MB"}
{"Digest":"<none>","ID":"sha256:img-named","Repository":"<none>","Tag":"<none>","Size":"20MB"}`

// Ausgabe von dockerUntaggedCmd: ID, Bytes, früherer RepoDigest.
const dockerUntaggedOut = "sha256:img-named\t20000000\tredis@sha256:bbbb \n" +
	"sha256:img-orphan\t9000000\t\n"

func TestParseDockerImagesUntaggedAreSuperseded(t *testing.T) {
	images := parseDockerImages(dockerSupersededOut, parseUntaggedImages(dockerUntaggedOut))
	// Doppelte Zeile (zweiter RepoDigest) zählt nur einmal.
	if len(images) != 4 {
		t.Fatalf("erwartete 4 images, bekam %d: %+v", len(images), images)
	}
	if images[0].Superseded || images[0].Tag != "latest" {
		t.Errorf("getaggtes image darf nicht überholt sein: %+v", images[0])
	}
	old := images[1]
	if !old.Superseded || old.Repository != "nginx" || old.Tag != "" || old.SizeText != "142MB" {
		t.Errorf("alte nginx-fassung falsch: %+v", old)
	}
	if images[2].Repository != "" || !images[2].Superseded {
		t.Errorf("verwaiste zeile ohne herkunft falsch: %+v", images[2])
	}
	if images[3].Repository != "redis" || !images[3].Superseded {
		t.Errorf("name aus dem RepoDigest fehlt: %+v", images[3])
	}
}

func TestParseUntaggedImagesFormat(t *testing.T) {
	got := parseUntaggedImages("sha256:x\t30400000\tghcr.io/foo/bar@sha256:1 ghcr.io/foo/bar@sha256:2 \nsha256:y\t5\t\nschrott")
	if x := got["sha256:x"]; x.Repository != "ghcr.io/foo/bar" || x.Bytes != 30400000 {
		t.Errorf("herkunft falsch: %+v", got)
	}
	if y, ok := got["sha256:y"]; !ok || y.Repository != "" {
		t.Errorf("zeile ohne digest fehlt oder hat einen namen: %+v", got)
	}
	if len(got) != 2 {
		t.Errorf("schrott darf keinen eintrag erzeugen: %+v", got)
	}
}

// Docker 29 (containerd-Image-Speicher) führt untagged Images nicht in
// `docker images`; nur der Dangling-Filter kennt sie. Sie müssen trotzdem
// erscheinen - einmal, auch wenn die Liste sie ebenfalls führt.
func TestUntaggedImageMissingFromListComesFromDanglingFilter(t *testing.T) {
	list := `{"Digest":"sha256:aaaa","ID":"sha256:img-new","Repository":"nginx","Tag":"stable-alpine","Size":"37.3MB"}`
	untagged := parseUntaggedImages("sha256:img-old\t30400000\tnginx@sha256:77e5 \n")
	images := parseDockerImages(list, untagged)
	if len(images) != 2 {
		t.Fatalf("erwartete 2 images, bekam %d: %+v", len(images), images)
	}
	old := images[1]
	if !old.Superseded || old.Repository != "nginx" || old.ImageID != "sha256:img-old" || old.SizeText != "30.4MB" {
		t.Errorf("fehlendes untagged image falsch: %+v", old)
	}
	// Führt die Liste es selbst, kommt es nicht doppelt.
	list += "\n" + `{"Digest":"sha256:77e5","ID":"sha256:img-old","Repository":"nginx","Tag":"<none>","Size":"30.4MB"}`
	if got := parseDockerImages(list, untagged); len(got) != 2 {
		t.Errorf("image doppelt geführt: %+v", got)
	}
}

func TestDockerSize(t *testing.T) {
	for in, want := range map[int64]string{999: "999B", 10100: "10.1kB", 41100000: "41.1MB", 1500000000: "1.5GB"} {
		if got := dockerSize(in); got != want {
			t.Errorf("dockerSize(%d) = %q, erwartet %q", in, got, want)
		}
	}
}

func TestSupersededImageIsNotInUseByRepoName(t *testing.T) {
	// Der Container läuft auf "nginx:latest" (neue Fassung). Die überholte
	// Fassung hat nur den Repo-Namen "nginx" - sie darf dadurch nicht als
	// genutzt gelten, nur die exakte Image-ID zählt.
	containers := []domain.DockerContainer{{Image: "nginx:latest", ImageID: "sha256:img-new-latest"}}
	images := parseDockerImages(dockerSupersededOut, nil)
	markImagesInUse(images, containers)
	if images[1].InUse {
		t.Errorf("überholtes image darf nicht über den Repo-Namen als genutzt gelten: %+v", images[1])
	}
	containers[0].ImageID = "sha256:img-old-latest"
	markImagesInUse(images, containers)
	if !images[1].InUse {
		t.Errorf("container auf der alten ID muss das überholte image als genutzt markieren")
	}
}
