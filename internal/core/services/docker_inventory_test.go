package services_test

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// dockerResponses sind FakeDialer-Antworten für einen Server mit Docker
// (ein Compose-Container, ein genutztes Registry-Image, ein lokales Image).
func dockerResponses() map[string]sshx.FakeResponse {
	return map[string]sshx.FakeResponse{
		"command -v docker":       {Output: "/usr/bin/docker\n"},
		"compose version":         {Output: "2.27.0\n"},
		"docker ps -a --no-trunc": {Output: `{"ID":"aaa111bbb222ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee5","Names":"webshop-web-1","Image":"nginx:1.25","State":"running","Status":"Up 3 days","Ports":"80/tcp"}` + "\n"},
		"docker inspect":          {Output: "aaa111bbb222ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee5\twebshop\tweb\t/opt/webshop\t/opt/webshop/compose.yaml\tsha256:img-nginx\n"},
		"docker images":           {Output: `{"Repository":"nginx","Tag":"1.25","ID":"sha256:img-nginx","Digest":"sha256:feedface","Size":"188MB"}` + "\n" + `{"Repository":"meinapp","Tag":"dev","ID":"sha256:img-local","Digest":"<none>","Size":"92MB"}` + "\n"},
	}
}

// TestJoinCollectsDockerInventory: Der Join-Scan erfasst Docker-Container
// und -Images und setzt die Server-Flags.
func TestJoinCollectsDockerInventory(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses = map[string]sshx.FakeResponse{
		"apt-get dnf zypper": {Output: "apt-get\n"}, // Debian → apt (Join prüft das)
		"sudo -n id -u":      {Output: "0\n"},       // Service-User erreicht root
		"os-release":         {Output: "NAME=\"Debian GNU/Linux\"\nVERSION=\"12 (bookworm)\"\n"},
		"dpkg-query":         {Output: "nginx 1.22.1\n"},
	}
	for k, v := range dockerResponses() {
		env.Dialer.Responses[k] = v
	}
	server, err := env.Servers.Join(services.JoinRequest{
		Name: "docker01", Host: "10.0.0.9", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join fehlgeschlagen: %v", err)
	}
	if !server.HasDocker || !server.HasCompose {
		t.Errorf("has_docker/has_compose sollten gesetzt sein: %+v", server)
	}

	repo := repositories.NewServerRepository(env.DB())
	containers, err := repo.FindDockerContainers(server.ID)
	if err != nil || len(containers) != 1 {
		t.Fatalf("erwartete 1 container, bekam %d (err=%v)", len(containers), err)
	}
	if containers[0].ComposeProject != "webshop" || containers[0].ComposeService != "web" {
		t.Errorf("compose-zuordnung fehlt: %+v", containers[0])
	}
	images, err := repo.FindDockerImages(server.ID)
	if err != nil || len(images) != 2 {
		t.Fatalf("erwartete 2 images, bekam %d (err=%v)", len(images), err)
	}
	// Sortierung: meinapp vor nginx.
	if !images[1].InUse || images[0].InUse {
		t.Errorf("in_use falsch: %+v", images)
	}
}

// TestReplaceDockerImagesPreservesCheck: Der System-Sync ersetzt den
// Image-Bestand - die Ergebnisse des nächtlichen Update-Checks müssen
// dabei erhalten bleiben, solange sich der lokale Digest nicht ändert.
func TestReplaceDockerImagesPreservesCheck(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	base := []domain.DockerImage{
		{Repository: "nginx", Tag: "1.25", ImageID: "sha256:a", RepoDigest: "sha256:alt", InUse: true},
	}
	if err := repo.ReplaceDockerImages(id, base); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.FindDockerImages(id)
	now := time.Now()
	if err := repo.UpdateDockerImageCheck(stored[0].ID, map[string]any{
		"candidate_digest": "sha256:neu", "update_available": true,
		"check_status": domain.DockerCheckOK, "last_checked_at": &now,
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Sync mit unverändertem Digest → Check-Ergebnis bleibt.
	if err := repo.ReplaceDockerImages(id, []domain.DockerImage{
		{Repository: "nginx", Tag: "1.25", ImageID: "sha256:a", RepoDigest: "sha256:alt", InUse: true},
	}); err != nil {
		t.Fatal(err)
	}
	after, _ := repo.FindDockerImages(id)
	if len(after) != 1 || !after[0].UpdateAvailable || after[0].CandidateDigest != "sha256:neu" || after[0].CheckStatus != domain.DockerCheckOK {
		t.Fatalf("check-ergebnis ging beim sync verloren: %+v", after)
	}
	if n, _ := repo.CountOutdatedDockerImages(id); n != 1 {
		t.Errorf("erwartete 1 veraltetes image, bekam %d", n)
	}

	// 2. Image wurde gezogen (neuer Digest) → altes Ergebnis verfällt.
	if err := repo.ReplaceDockerImages(id, []domain.DockerImage{
		{Repository: "nginx", Tag: "1.25", ImageID: "sha256:b", RepoDigest: "sha256:neu", InUse: true},
	}); err != nil {
		t.Fatal(err)
	}
	after, _ = repo.FindDockerImages(id)
	if after[0].UpdateAvailable || after[0].CheckStatus != "" {
		t.Errorf("nach image-pull sollte das check-ergebnis zurückgesetzt sein: %+v", after[0])
	}
	if n, _ := repo.CountOutdatedDockerImages(id); n != 0 {
		t.Errorf("erwartete 0 veraltete images, bekam %d", n)
	}
}

// TestDeleteServerRemovesDockerInventory: Server-Löschung räumt auch das
// Docker-Inventar mit ab.
func TestDeleteServerRemovesDockerInventory(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses = map[string]sshx.FakeResponse{
		"apt-get dnf zypper": {Output: "apt-get\n"}, // Debian → apt (Join prüft das)
		"sudo -n id -u":      {Output: "0\n"},       // Service-User erreicht root
		"os-release":         {Output: "NAME=\"Debian GNU/Linux\"\n"},
	}
	for k, v := range dockerResponses() {
		env.Dialer.Responses[k] = v
	}
	server, err := env.Servers.Join(services.JoinRequest{
		Name: "docker02", Host: "10.0.0.10", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.Delete(server.ID); err != nil {
		t.Fatal(err)
	}
	var n int64
	env.DB().Model(&domain.DockerContainer{}).Where("server_id = ?", server.ID).Count(&n)
	if n != 0 {
		t.Errorf("container nicht mitgelöscht: %d übrig", n)
	}
	env.DB().Model(&domain.DockerImage{}).Where("server_id = ?", server.ID).Count(&n)
	if n != 0 {
		t.Errorf("images nicht mitgelöscht: %d übrig", n)
	}
}
