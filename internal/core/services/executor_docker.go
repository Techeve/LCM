package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
)

// Docker-Update-Regeln (Watchtower-Art): neue Images ziehen, Container mit
// neuem Image erstellen, überholte Images entfernen. Die Auswahl (Command) ist
// optional: leer heißt alle betroffenen Projekte bzw. Container.

// ErrInvalidDockerScope: Die Auswahl einer Docker-Update-Regel ist kein
// Projekt- bzw. Containername.
var ErrInvalidDockerScope = errors.New("ungültige auswahl - erwartet wird ein compose-projekt- bzw. containername oder leer")

// validDockerScope prüft die Auswahl: leer oder ein gültiger Name.
func validDockerScope(command string) bool {
	c := strings.TrimSpace(command)
	return c == "" || reComposeName.MatchString(c)
}

// isDockerUpdateRule meldet, ob der Regeltyp die Docker-Update-Regeln sind.
func isDockerUpdateRule(ruleType string) bool {
	return ruleType == domain.RuleTypeDockerComposeUpdate || ruleType == domain.RuleTypeDockerContainerUpdate
}

// dockerUpdateSkip beantwortet die Vorbedingungen. Ohne Docker oder bei
// abgeschalteten Docker-Updates ist der Lauf ein sauberer No-Op, kein Fehler -
// eine Ausnahme soll einen Gruppenlauf nicht rot färben.
func (e *Executor) dockerUpdateSkip(job *domain.Job, server *domain.Server) bool {
	switch {
	case !server.HasDocker:
		e.jobs.CompleteSkipped(job, "docker nicht vorhanden - nichts zu aktualisieren")
	case server.DockerUpdatesDisabled:
		e.jobs.CompleteSkipped(job, "docker-updates sind für diesen server abgeschaltet - übersprungen")
	default:
		return false
	}
	return true
}

// dockerUpdateConn öffnet die protokollierte Verbindung eines Laufs. Der
// Aufrufer schließt sie.
func (e *Executor) dockerUpdateConn(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) (sshx.Conn, bool) {
	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return nil, false
	}
	return e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
	}), true
}

// staleImageIDs liefert die Image-IDs, die nicht mehr aktuell sind: Der
// Registry-Check meldet ein Update, oder das Image ist eine überholte Fassung
// (ein Pull hat ihm den Tag entzogen, ein Container läuft noch darauf).
func (e *Executor) staleImageIDs(serverID uint) (map[string]bool, error) {
	images, err := e.servers.FindDockerImages(serverID)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for i := range images {
		if images[i].UpdateAvailable || images[i].Superseded {
			ids[images[i].ImageID] = true
		}
	}
	return ids, nil
}

// composeTarget ist ein Compose-Projekt, dessen Container ein Update haben.
type composeTarget struct {
	Project, WorkingDir, ConfigFiles string
}

// composeUpdateTargets wählt die Projekte: mindestens ein Container nutzt ein
// veraltetes Image. scope schränkt auf ein Projekt ein.
func (e *Executor) composeUpdateTargets(serverID uint, scope string) ([]composeTarget, error) {
	updatable, err := e.staleImageIDs(serverID)
	if err != nil {
		return nil, err
	}
	containers, err := e.servers.FindDockerContainers(serverID)
	if err != nil {
		return nil, err
	}
	var out []composeTarget
	seen := map[string]bool{}
	for i := range containers {
		c := &containers[i]
		if c.ComposeProject == "" || seen[c.ComposeProject] || !updatable[c.ImageID] {
			continue
		}
		if scope != "" && c.ComposeProject != scope {
			continue
		}
		seen[c.ComposeProject] = true
		out = append(out, composeTarget{Project: c.ComposeProject, WorkingDir: c.ComposeWorkingDir, ConfigFiles: c.ComposeConfigFiles})
	}
	return out, nil
}

// runDockerComposeUpdateRule zieht die Images betroffener Compose-Projekte und
// erstellt deren Container neu (`pull && up -d`). Danach entfernt `image prune
// -f` die überholten Images - nur ungetaggte, getaggte bleiben.
func (e *Executor) runDockerComposeUpdateRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
	if e.dockerUpdateSkip(job, server) {
		return
	}
	targets, err := e.composeUpdateTargets(server.ID, rule.Command)
	if err != nil {
		e.jobs.Complete(job, "", nil, fmt.Errorf("docker-inventar laden: %w", err))
		return
	}
	if len(targets) == 0 {
		e.jobs.CompleteSkipped(job, "kein compose-projekt mit verfügbarem update")
		return
	}
	conn, ok := e.dockerUpdateConn(job, server, rule, triggeredBy)
	if !ok {
		return
	}
	defer conn.Close()

	var log strings.Builder
	for _, t := range targets {
		script, err := composeUpdateScript(t.WorkingDir, t.ConfigFiles, "")
		if err != nil {
			log.WriteString("übersprungen: " + t.Project + " (" + err.Error() + ")\n")
			continue
		}
		script += " && docker image prune -f >/dev/null"
		out, code, runErr := conn.Run(privRun(server, script))
		if runErr == nil && code != 0 {
			runErr = fmt.Errorf("compose-update %s endete mit exit-code %d: %s", t.Project, code, summarize(out))
		}
		if runErr != nil {
			e.finishWithHealth(job, server, log.String(), runErr)
			return
		}
		log.WriteString("aktualisiert: compose-projekt " + t.Project + "\n")
	}
	rescanDockerInto(e.servers, conn, server)
	e.finishWithHealth(job, server, log.String(), nil)
}

// containerUpdateTargets wählt die laufenden Standalone-Container auf einem
// veralteten Image. Der Name muss gültig sein - er landet in der Shell.
func (e *Executor) containerUpdateTargets(serverID uint, scope string) ([]domain.DockerContainer, error) {
	updatable, err := e.staleImageIDs(serverID)
	if err != nil {
		return nil, err
	}
	containers, err := e.servers.FindDockerContainers(serverID)
	if err != nil {
		return nil, err
	}
	var out []domain.DockerContainer
	for i := range containers {
		c := containers[i]
		if c.ComposeProject != "" || c.State != "running" || !updatable[c.ImageID] {
			continue
		}
		if scope != "" && c.Name != scope {
			continue
		}
		if !reComposeName.MatchString(c.Name) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// runDockerContainerUpdateRule aktualisiert laufende Standalone-Container. Ein
// fehlgeschlagener Container (er ist dann zurückgebaut, siehe recreateScript)
// hält die übrigen nicht auf; der Lauf meldet sie am Ende gesammelt als Fehler.
func (e *Executor) runDockerContainerUpdateRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
	if e.dockerUpdateSkip(job, server) {
		return
	}
	targets, err := e.containerUpdateTargets(server.ID, rule.Command)
	if err != nil {
		e.jobs.Complete(job, "", nil, fmt.Errorf("docker-inventar laden: %w", err))
		return
	}
	if len(targets) == 0 {
		e.jobs.CompleteSkipped(job, "kein laufender container mit verfügbarem update")
		return
	}
	conn, ok := e.dockerUpdateConn(job, server, rule, triggeredBy)
	if !ok {
		return
	}
	defer conn.Close()

	var log strings.Builder
	var failures []string
	for i := range targets {
		c := &targets[i]
		note, err := e.recreateContainer(conn, server, c)
		switch {
		case err != nil:
			failures = append(failures, c.Name)
			log.WriteString("FEHLER: " + c.Name + ": " + err.Error() + "\n")
		case note != "":
			log.WriteString("übersprungen: " + c.Name + " (" + note + ")\n")
		default:
			log.WriteString("aktualisiert: " + c.Name + "\n")
		}
	}
	rescanDockerInto(e.servers, conn, server)
	var runErr error
	if len(failures) > 0 {
		runErr = fmt.Errorf("%d von %d container nicht aktualisiert: %s", len(failures), len(targets), strings.Join(failures, ", "))
	}
	e.finishWithHealth(job, server, log.String(), runErr)
}

// recreateContainer liest die Konfiguration des Containers, zieht das Image
// unter dem Namen, mit dem er erstellt wurde, und führt den Umbau aus. Der Name
// kommt aus docker inspect: In der Containerliste steht nach einem Pull nur
// noch die Image-ID. Ein Container ohne Image-Namen (per ID gestartet) lässt
// sich nicht aktualisieren; dann kommt eine Notiz statt eines Fehlers zurück.
func (e *Executor) recreateContainer(conn sshx.Conn, server *domain.Server, c *domain.DockerContainer) (string, error) {
	out, code, err := conn.Run(privRun(server, "docker inspect "+shellQuote(c.ContainerID)))
	if err == nil && code != 0 {
		err = fmt.Errorf("docker inspect endete mit exit-code %d", code)
	}
	if err != nil {
		return "", err
	}
	ref, err := inspectImageRef(out)
	if err != nil {
		return "", err
	}
	if ref == "" || strings.HasPrefix(ref, "sha256:") || !reDockerImageRef.MatchString(ref) {
		return "ohne Image-Namen erstellt", nil
	}
	body, err := recreateBody(out, ref)
	if err != nil {
		return "", err
	}
	out, code, err = conn.Run(privRun(server, recreateScript(c.Name, ref, c.ImageID, body)))
	if err == nil && code != 0 {
		err = fmt.Errorf("neuerstellung endete mit exit-code %d: %s", code, summarize(out))
	}
	return "", err
}
