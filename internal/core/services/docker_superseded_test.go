package services

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

// Der Kern des Pull-Falls: Der Tag liegt schon auf dem neuen Image, der
// Container läuft aber noch auf dem alten. Der Status darf nicht „aktuell" sagen.
func TestPullOhneNeustartLaesstContainerAufDerAltenFassung(t *testing.T) {
	images := []domain.DockerImage{
		{Repository: "nginx", Tag: "latest", ImageID: "sha256:neu"},
		{Repository: "nginx", ImageID: "sha256:alt", Superseded: true},
	}
	markImagesInUse(images, []domain.DockerContainer{{Image: "nginx", ImageID: "sha256:alt"}})

	if images[0].InUse {
		t.Error("das neue Image nutzt noch niemand")
	}
	if !images[0].RestartPending {
		t.Error("der Container wartet noch auf das neue Image")
	}
	if !images[1].InUse || images[1].RestartPending {
		t.Errorf("die alte Fassung läuft noch: %+v", images[1])
	}

	markImagesInUse(images, []domain.DockerContainer{{Image: "nginx:latest", ImageID: "sha256:neu"}})
	if !images[0].InUse || images[0].RestartPending || images[1].InUse {
		t.Errorf("nach dem Neuerstellen ist alles aktuell: %+v", images)
	}
}

func TestOhneImageIDBleibtDerNameAlsNotbehelf(t *testing.T) {
	images := []domain.DockerImage{{Repository: "redis", Tag: "7", ImageID: "sha256:r"}}
	markImagesInUse(images, []domain.DockerContainer{{Image: "redis:7"}})
	if !images[0].InUse || images[0].RestartPending {
		t.Errorf("Zuordnung über den Namen fehlt: %+v", images[0])
	}
}

func TestInspectLiefertDenNamenStattDerImageID(t *testing.T) {
	containers := []domain.DockerContainer{{ContainerID: "c1", Image: "4f2a9c1b77d0"}}
	applyComposeLabels(containers, "c1\t\t\t\t\tsha256:alt\tnginx:1.27\n")
	if containers[0].Image != "nginx:1.27" || containers[0].ImageID != "sha256:alt" {
		t.Errorf("inspect nicht übernommen: %+v", containers[0])
	}
}

func TestWithDefaultTag(t *testing.T) {
	for ref, want := range map[string]string{
		"nginx":                    "nginx:latest",
		"nginx:1.27":               "nginx:1.27",
		"registry:5000/foo":        "registry:5000/foo:latest",
		"registry:5000/foo:1":      "registry:5000/foo:1",
		"ghcr.io/foo/bar@sha256:a": "ghcr.io/foo/bar@sha256:a",
		"":                         "",
	} {
		if got := withDefaultTag(ref); got != want {
			t.Errorf("withDefaultTag(%q) = %q, erwartet %q", ref, got, want)
		}
	}
}

func TestImagesToScan(t *testing.T) {
	alt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	neu := alt.AddDate(0, 3, 0)
	images := []domain.DockerImage{
		{Repository: "nginx", Tag: "1.26", RepoDigest: "sha256:a", BuiltAt: &alt, InUse: true}, // genutzt, nicht das neueste
		{Repository: "nginx", Tag: "1.27", RepoDigest: "sha256:b", BuiltAt: &neu},              // ungenutzt, das neueste
		{Repository: "nginx", Tag: "1.25", RepoDigest: "sha256:c", BuiltAt: &alt},              // ungenutzt und alt
		{Repository: "redis", RepoDigest: "sha256:d", Superseded: true, InUse: true},           // überholt, läuft noch
		{Repository: "redis", RepoDigest: "sha256:e", Superseded: true},                        // überholt, ungenutzt
		{Repository: "postgres", Tag: "16", RepoDigest: "sha256:f"},                            // ohne Datum
		{Repository: "eigenbau", Tag: "1", InUse: true},                                        // lokal gebaut
		{ServerID: 2, Repository: "nginx", Tag: "1.25", RepoDigest: "sha256:c", BuiltAt: &alt}, // auf Server 2 das neueste
	}
	var got []string
	for _, img := range imagesToScan(images) {
		got = append(got, fmt.Sprintf("%d %s", img.ServerID, img.ScanRef()))
	}
	want := "0 nginx@sha256:a|0 nginx@sha256:b|0 redis@sha256:d|0 postgres@sha256:f|2 nginx@sha256:c"
	if strings.Join(got, "|") != want {
		t.Errorf("Auswahl = %v, erwartet %s", got, want)
	}
}

func TestUeberholteFassungBehaeltDigestAusDemDanglingFilter(t *testing.T) {
	list := `{"Digest":"<none>","ID":"sha256:img-old","Repository":"<none>","Tag":"<none>","Size":"30MB"}`
	images := parseDockerImages(list, parseUntaggedImages("sha256:img-old\t30000000\tnginx@sha256:77e5 \n"))
	if len(images) != 1 || images[0].VulnRef() != "nginx@sha256:77e5" {
		t.Errorf("Digest der überholten Fassung fehlt: %+v", images)
	}
}

func TestParseDockerTime(t *testing.T) {
	got := parseDockerTime("2026-05-01 10:00:00 +0200 CEST")
	if got == nil || got.UTC().Hour() != 8 {
		t.Errorf("Zeit nicht gelesen: %v", got)
	}
	if parseDockerTime("vor drei Tagen") != nil {
		t.Error("Unlesbares darf kein Datum ergeben")
	}
}
