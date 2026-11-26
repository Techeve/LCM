package services

import (
	"errors"
	"regexp"
	"strings"
)

// Validierung der Docker-Aktions-Eingaben. Die Werte werden zusätzlich
// gegen das gespeicherte Inventar geprüft (nur bekannte Projekte/Images
// sind ausführbar) - Regex + shellQuote sind die zweite Verteidigungslinie
// gegen Shell-Injection.
var (
	// Compose-Projekt-/Service-Namen (Docker normalisiert auf [a-z0-9_-],
	// wir erlauben defensiv auch Punkt und Großbuchstaben).
	reComposeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	// Image-Referenzen: repo[:tag], optional Registry-Host mit Port,
	// optional @sha256-Digest.
	reDockerImageRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_./:@-]*$`)
)

var (
	ErrInvalidComposeName     = errors.New("ungültiger compose-projekt-/service-name")
	ErrInvalidDockerRef       = errors.New("ungültige image-referenz")
	ErrComposeUnavailable     = errors.New("docker compose (v2) ist auf dem server nicht verfügbar")
	ErrDockerUnavailable      = errors.New("docker ist auf dem server nicht verfügbar")
	ErrComposeProjectUnknown  = errors.New("compose-projekt auf diesem server nicht bekannt")
	ErrDockerImageUnknown     = errors.New("image auf diesem server nicht bekannt")
	ErrDockerImageInUse       = errors.New("image wird von einem container genutzt und kann nicht gelöscht werden")
	ErrComposePathMissing     = errors.New("weder working_dir noch config_files des compose-projekts bekannt - bitte inventar aktualisieren")
	ErrDockerContainerUnknown = errors.New("container auf diesem server nicht bekannt")
	ErrNoImagesToUpdate       = errors.New("keine genutzten, getaggten registry-images auf diesem server - nichts zu aktualisieren")
	// ErrDockerUpdatesDisabled: In den Server-Einstellungen ist das Einspielen
	// neuer Image-Versionen abgeschaltet. Betrifft nur verändernde Aktionen -
	// Inventar und Update-Anzeige laufen weiter.
	ErrDockerUpdatesDisabled = errors.New("docker-updates sind für diesen server abgeschaltet - das inventar wird weiter erfasst, eingespielt wird nichts")
)

// composeUpdateScript baut das Update-Skript eines Compose-Projekts:
// neue Images ziehen und die Container neu erstellen (`pull && up -d`).
// Bevorzugt wird ins working_dir des Projekts gewechselt (dort liegt die
// compose.yaml); als Fallback werden die config_files per -f übergeben.
// service schränkt optional auf einen einzelnen Dienst ein.
//
// restricted: Das Skript läuft dann als nicht-privilegierter Service-User, nur
// `docker` geht über sudo. Ein `cd` ins Projektverzeichnis scheitert dort, sobald
// es dem Service-User nicht zugänglich ist (etwa unter /home/<benutzer> mit
// Modus 750). Deshalb bekommt Docker das Verzeichnis und die Dateien als
// Argumente und liest sie als root - das Skript wechselt nirgends hin.
func composeUpdateScript(workingDir, configFiles, service string, restricted bool) (string, error) {
	svc := ""
	if service != "" {
		if !reComposeName.MatchString(service) {
			return "", ErrInvalidComposeName
		}
		svc = " " + shellQuote(service)
	}
	if restricted {
		return composeUpdateByFlags(composeProjectFlags(workingDir, configFiles), svc)
	}
	if workingDir != "" {
		return "cd " + shellQuote(workingDir) +
			" && docker compose pull" + svc +
			" && docker compose up -d" + svc, nil
	}
	return composeUpdateByFlags(composeFileFlags(configFiles), svc)
}

// composeUpdateByFlags baut `pull && up -d` mit den Flags vor dem Unterbefehl.
func composeUpdateByFlags(flags []string, svc string) (string, error) {
	if len(flags) == 0 {
		return "", ErrComposePathMissing
	}
	base := "docker compose " + strings.Join(flags, " ")
	return base + " pull" + svc + " && " + base + " up -d" + svc, nil
}

// composeFileFlags übersetzt die kommagetrennten config_files in -f-Flags.
func composeFileFlags(configFiles string) []string {
	var flags []string
	for _, f := range strings.Split(configFiles, ",") {
		if f = strings.TrimSpace(f); f != "" {
			flags = append(flags, "-f "+shellQuote(f))
		}
	}
	return flags
}

// composeProjectFlags sind die Flags ohne `cd`: das Projektverzeichnis (für
// .env und relative Pfade) und die Compose-Dateien des Projekts. Fehlen die
// Dateien, sucht Compose im Projektverzeichnis nach seinen Standardnamen.
func composeProjectFlags(workingDir, configFiles string) []string {
	var flags []string
	if workingDir != "" {
		flags = append(flags, "--project-directory "+shellQuote(workingDir))
	}
	return append(flags, composeFileFlags(configFiles)...)
}

// dockerPullScript zieht ein Image (der Container wird bewusst NICHT neu
// erstellt - bei Standalone-Containern übernimmt das der Betreiber).
func dockerPullScript(ref string) string {
	return "docker pull " + shellQuote(ref)
}

// dockerPullAllScript zieht mehrere Images nacheinander. Ein einzelner
// Fehlschlag bricht den Lauf nicht ab (die übrigen Images werden trotzdem
// aktualisiert), führt am Ende aber zu Exit 1 - der Job zeigt den Fehler.
func dockerPullAllScript(refs []string) string {
	steps := []string{"fail=0"}
	for _, r := range refs {
		steps = append(steps, "docker pull "+shellQuote(r)+" || fail=1")
	}
	steps = append(steps, "exit $fail")
	return strings.Join(steps, "; ")
}

// dockerRemoveImageScript löscht ein einzelnes Image (docker rmi). Der
// Aufrufer stellt sicher, dass kein Container es nutzt.
func dockerRemoveImageScript(ref string) string {
	return "docker rmi " + shellQuote(ref)
}

// dockerPruneScript entfernt ALLE ungenutzten Images (von keinem Container
// referenziert) - für die Gruppen-Regel „ungenutzte Images aufräumen".
func dockerPruneScript() string {
	return "docker image prune -af"
}
