package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// Ein laufender Standalone-Container auf nginx:1.25 (dasselbe Image wie das
// Compose-Projekt "webshop"). Die Antwort auf `docker inspect <id>` ist die
// Konfiguration, aus der die Neuerstellung den Create-Request baut.
const standalonePS = `{"Command":"\"nginx -g daemon off\"","ID":"ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee555fff666aaa1","Image":"nginx:1.25","Labels":"","Names":"edge","Networks":"bridge","Ports":"0.0.0.0:8080->80/tcp","State":"running","Status":"Up 2 days"}`

const standaloneInspect = `[{"Id":"ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee555fff666aaa1","Config":{"Image":"nginx:1.25","Cmd":["nginx","-g","daemon off;"]},"HostConfig":{"RestartPolicy":{"Name":"always"}},"Mounts":[],"NetworkSettings":{"Networks":{"bridge":{"Aliases":null}}}}]`

// standaloneID ist die volle ID des Standalone-Containers. Der Schlüssel
// nutzt sie statt „docker inspect <id>", weil die Shell-Quotierung des
// Befehls den Schlüssel mit Anführungszeichen unauffindbar machte.
const standaloneID = "ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee555fff666aaa1"

// standaloneResponses ergänzt das Standard-Inventar um den Standalone-Container.
// Die längere ID-Antwort gewinnt gegen das allgemeine „docker inspect".
func standaloneResponses() map[string]sshx.FakeResponse {
	return map[string]sshx.FakeResponse{
		"docker ps -a --no-trunc": {Output: dockerPSOutStandalone()},
		standaloneID:              {Output: standaloneInspect},
		"docker inspect --format": {Output: "aaa111bbb222ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee5\twebshop\tweb\t/opt/webshop\t/opt/webshop/compose.yaml\tsha256:img-nginx\n" + "ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee555fff666aaa1\t\t\t\t\tsha256:img-nginx\n"},
	}
}

func dockerPSOutStandalone() string {
	return `{"Command":"\"/docker-entrypoint.sh nginx\"","ID":"aaa111bbb222ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee5","Image":"nginx:1.25","Labels":"com.docker.compose.project=webshop,com.docker.compose.service=web","Names":"webshop-web-1","Networks":"webshop_default","Ports":"","State":"running","Status":"Up 3 days"}` + "\n" + standalonePS + "\n"
}

// updateNginx markiert das nginx-Image (ID sha256:img-nginx) als veraltet.
func updateNginx(t *testing.T, env *testEnv, serverID uint) {
	t.Helper()
	repo := repositories.NewServerRepository(env.DB())
	images, err := repo.FindDockerImages(serverID)
	if err != nil {
		t.Fatal(err)
	}
	for _, img := range images {
		if img.ImageID == "sha256:img-nginx" {
			_ = repo.UpdateDockerImageCheck(img.ID, map[string]any{"update_available": true, "check_status": domain.DockerCheckOK})
		}
	}
}

func scheduledRule(t *testing.T, env *testEnv, serverID uint, ruleType, command string) *domain.Rule {
	t.Helper()
	group, err := env.Groups.Create("Gruppe "+ruleType+" "+command, "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "nachts", "0 3 * * *", 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "update", ruleType, command, &sched.ID, false, "admin")
	if err != nil {
		t.Fatalf("regel definieren: %v", err)
	}
	return rule
}

// TestDockerComposeUpdateRuleRunsPullUpAndPrunes: das betroffene Projekt wird
// gezogen und neu erstellt; danach räumt prune die untagged Images weg. Ohne
// Update passiert nichts.
func TestDockerComposeUpdateRuleRunsPullUpAndPrunes(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServerWith(t, env, "docker01", standaloneResponses())
	updateNginx(t, env, id)

	rule := scheduledRule(t, env, id, domain.RuleTypeDockerComposeUpdate, "")
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "docker compose pull && docker compose up -d") {
		t.Errorf("compose-update fehlt:\n%s", all)
	}
	if !strings.Contains(all, "docker image prune -f") {
		t.Errorf("aufräumen nach dem Update fehlt:\n%s", all)
	}
}

// TestDockerComposeUpdateRuleNothingToDo: ohne verfügbares Update läuft kein
// Projekt-Update an.
func TestDockerComposeUpdateRuleNothingToDo(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServerWith(t, env, "docker01", standaloneResponses())

	rule := scheduledRule(t, env, id, domain.RuleTypeDockerComposeUpdate, "")
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	if strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "docker compose up") {
		t.Error("ohne update darf kein compose-projekt neu erstellt werden")
	}
}

// TestDockerContainerUpdateRuleRecreatesStandalone: der Standalone-Container
// wird gezogen, neu erstellt und das alte Image entfernt. Das Compose-
// Projekt bleibt außen vor.
func TestDockerContainerUpdateRuleRecreatesStandalone(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServerWith(t, env, "docker01", standaloneResponses())
	updateNginx(t, env, id)

	rule := scheduledRule(t, env, id, domain.RuleTypeDockerContainerUpdate, "")
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	for _, want := range []string{"docker pull", "nginx:1.25", "docker stop", "edge.lcm-old", "--unix-socket", "docker start", "docker image rm"} {
		if !strings.Contains(all, want) {
			t.Errorf("%q fehlt:\n%s", want, all)
		}
	}
	if strings.Contains(all, "docker compose up") {
		t.Errorf("container-regel darf keine compose-projekte anfassen:\n%s", all)
	}
}

// TestDockerContainerUpdateRuleScopeAndDisabled: Auswahl per Name und
// abgeschaltete Docker-Updates am Server.
func TestDockerContainerUpdateRuleScopeAndDisabled(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServerWith(t, env, "docker01", standaloneResponses())
	updateNginx(t, env, id)

	other := scheduledRule(t, env, id, domain.RuleTypeDockerContainerUpdate, "andere")
	env.Dialer.Commands = nil
	env.Executor.RunRule(other, "admin")
	if strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "docker stop") {
		t.Error("ein anderer containername darf edge nicht anfassen")
	}

	off := true
	if _, err := env.Servers.UpdateSettings(repositories.ScopeAll(), id, services.ServerSettingsInput{DockerUpdatesDisabled: &off}, "admin"); err != nil {
		t.Fatalf("docker-updates abschalten: %v", err)
	}
	rule := scheduledRule(t, env, id, domain.RuleTypeDockerContainerUpdate, "")
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")
	if strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "docker stop") {
		t.Error("bei abgeschalteten docker-updates darf nichts umgebaut werden")
	}
}

// TestDockerUpdateRuleValidatesScope: Auswahl muss ein Name oder leer sein.
func TestDockerUpdateRuleValidatesScope(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServerWith(t, env, "docker01", standaloneResponses())
	group, err := env.Groups.Create("Docker-Hosts", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "nachts", "0 3 * * *", 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "bösartig", domain.RuleTypeDockerContainerUpdate, "x;rm -rf /", &sched.ID, false, "admin")
	if !errors.Is(err, services.ErrInvalidDockerScope) {
		t.Errorf("erwartet ErrInvalidDockerScope, bekam %v", err)
	}
}

// TestDockerContainerUpdateRuleUsesImageNameFromInspect: Nach einem Pull zeigt
// die Containerliste statt des Namens nur noch die Image-ID. Die Regel zieht
// und erstellt trotzdem mit dem Namen aus docker inspect.
func TestDockerContainerUpdateRuleUsesImageNameFromInspect(t *testing.T) {
	env := newTestEnv(t)
	resp := standaloneResponses()
	resp["docker ps -a --no-trunc"] = sshx.FakeResponse{Output: strings.ReplaceAll(dockerPSOutStandalone(),
		`"Image":"nginx:1.25","Labels":"","Names":"edge"`, `"Image":"sha256:img-nginx","Labels":"","Names":"edge"`)}
	id := joinDockerServerWith(t, env, "docker01", resp)
	updateNginx(t, env, id)

	rule := scheduledRule(t, env, id, domain.RuleTypeDockerContainerUpdate, "")
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "docker pull") || !strings.Contains(all, "nginx:1.25") {
		t.Errorf("pull mit dem Namen aus inspect fehlt:\n%s", all)
	}
	if strings.Contains(all, "docker pull 'sha256") || strings.Contains(all, "docker pull '\\''sha256") {
		t.Errorf("die Image-ID darf nicht als Name gezogen werden:\n%s", all)
	}
}

// TestDockerContainerUpdateRuleFailureIsReported: Scheitert das Erstellen
// (der Container ist dann zurückgebaut), endet der Job mit Fehler.
func TestDockerContainerUpdateRuleFailureIsReported(t *testing.T) {
	env := newTestEnv(t)
	resp := standaloneResponses()
	resp["--unix-socket"] = sshx.FakeResponse{Output: "curl: (22) The requested URL returned error: 409", ExitCode: 1}
	id := joinDockerServerWith(t, env, "docker01", resp)
	updateNginx(t, env, id)

	rule := scheduledRule(t, env, id, domain.RuleTypeDockerContainerUpdate, "")
	env.Executor.RunRule(rule, "admin")

	var out struct {
		Status, Output string
	}
	env.DB().Raw("select status, output from jobs where type = ? order by created_at desc limit 1", domain.RuleTypeDockerContainerUpdate).Scan(&out)
	if out.Status != string(domain.JobStatusFailed) || !strings.Contains(out.Output, "edge") {
		t.Errorf("fehlschlag muss gemeldet werden: status=%q output=%q", out.Status, out.Output)
	}
}
