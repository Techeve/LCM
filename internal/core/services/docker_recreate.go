package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Standalone-Container neu erstellen (Regel docker-container-update). Docker
// kennt dafür kein eigenes Kommando. Die Konfiguration kommt deshalb aus
// `docker inspect` und geht als Create-Anfrage an die Engine-API - so bleiben
// Ports, Volumes, Umgebung, Netz, Neustart-Regel und Rechte erhalten, ohne dass
// eine Auswahl von docker-run-Flags nachgebaut werden müsste.

// inspectedContainer ist der Teil von `docker inspect` (ein Eintrag des
// JSON-Arrays), den die Neuerstellung braucht. Config und HostConfig bleiben
// roh, damit alle Felder unverändert übernommen werden.
type inspectedContainer struct {
	ID         string                     `json:"Id"`
	Config     map[string]json.RawMessage `json:"Config"`
	HostConfig map[string]json.RawMessage `json:"HostConfig"`
	Mounts     []inspectedMount           `json:"Mounts"`
	Networks   struct {
		Networks map[string]struct {
			Aliases []string `json:"Aliases"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

type inspectedMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

// inspectImageRef liefert den Image-Namen, mit dem der Container erstellt
// wurde (Config.Image) - leer, wenn er nur eine Image-ID trägt.
func inspectImageRef(inspectOut string) (string, error) {
	var list []struct {
		Config struct {
			Image string `json:"Image"`
		} `json:"Config"`
	}
	if err := json.Unmarshal([]byte(inspectOut), &list); err != nil {
		return "", fmt.Errorf("docker inspect nicht lesbar: %w", err)
	}
	if len(list) != 1 {
		return "", fmt.Errorf("docker inspect lieferte %d container statt 1", len(list))
	}
	return list[0].Config.Image, nil
}

// recreateBody baut den Create-Request (POST /containers/create) aus dem
// inspect-Stand eines Containers. ref ist das neue Image, auf dem der Container
// laufen soll. Anonyme Volumes (aus dem Image) werden mit ihrem vorhandenen
// Namen übernommen, damit ihre Daten am Container hängen bleiben.
func recreateBody(inspectOut, ref string) ([]byte, error) {
	var list []inspectedContainer
	if err := json.Unmarshal([]byte(inspectOut), &list); err != nil {
		return nil, fmt.Errorf("docker inspect nicht lesbar: %w", err)
	}
	if len(list) != 1 {
		return nil, fmt.Errorf("docker inspect lieferte %d container statt 1", len(list))
	}
	c := list[0]

	hostConfig := c.HostConfig
	if mounts := volumeMountsMissingFromBinds(c.HostConfig, c.Mounts); len(mounts) > 0 {
		raw, err := rawJSON(mounts)
		if err != nil {
			return nil, err
		}
		hostConfig["Mounts"] = raw
	}

	// Die Container-ID wird bei der Neuerstellung neu vergeben; ihr Kurzname
	// als Netz-Alias würde nicht mehr passen und wird deshalb weggelassen.
	shortID := c.ID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	endpoints := map[string]map[string]any{}
	for net, ep := range c.Networks.Networks {
		aliases := []string{}
		for _, a := range ep.Aliases {
			if a != shortID {
				aliases = append(aliases, a)
			}
		}
		endpoints[net] = map[string]any{"Aliases": aliases}
	}

	body := make(map[string]json.RawMessage, len(c.Config)+3)
	for k, v := range c.Config {
		body[k] = v
	}
	fields := map[string]any{
		"Image":            ref,
		"HostConfig":       hostConfig,
		"NetworkingConfig": map[string]any{"EndpointsConfig": endpoints},
	}
	for k, v := range fields {
		raw, err := rawJSON(v)
		if err != nil {
			return nil, err
		}
		body[k] = raw
	}
	return json.Marshal(body)
}

// volumeMountsMissingFromBinds liefert die Volume-Mounts, deren Ziel nicht
// schon über HostConfig.Binds (-v) abgedeckt ist. Ein Volume aus --mount oder
// ein anonymes Volume steht nur in Mounts; ohne diese Übernahme ginge es
// verloren.
func volumeMountsMissingFromBinds(hostConfig map[string]json.RawMessage, mounts []inspectedMount) []map[string]any {
	covered := map[string]bool{}
	var binds []string
	if raw, ok := hostConfig["Binds"]; ok {
		_ = json.Unmarshal(raw, &binds)
	}
	for _, b := range binds {
		parts := strings.Split(b, ":")
		if len(parts) >= 2 {
			covered[parts[1]] = true
		}
	}
	var out []map[string]any
	for _, m := range mounts {
		if m.Type != "volume" || m.Name == "" || covered[m.Destination] {
			continue
		}
		out = append(out, map[string]any{
			"Type": "volume", "Source": m.Name, "Target": m.Destination, "ReadOnly": !m.RW,
		})
	}
	return out
}

// recreateScript baut den Umbau eines Containers als eine Shell-Zeile. Der
// alte Container wird nur umbenannt (NAME.lcm-old); erst wenn der neue läuft,
// wird er entfernt. Scheitert ein Schritt, kommt der alte zurück und läuft
// wieder. Das überholte Image löscht `docker image rm` ohne -f: Ist es noch
// von einem anderen Container genutzt oder noch getaggt, bleibt es stehen.
func recreateScript(name, ref, oldImageID string, body []byte) string {
	n := shellQuote(name)
	old := shellQuote(name + ".lcm-old")
	payload := base64.StdEncoding.EncodeToString(body)
	create := "printf %s " + shellQuote(payload) + " | base64 -d | " +
		"curl -sS --fail --unix-socket /var/run/docker.sock -H 'Content-Type: application/json' " +
		"-X POST --data-binary @- " + shellQuote("http://localhost/containers/create?name="+name) + " >/dev/null"
	cleanup := "docker rm " + old + " >/dev/null"
	if oldImageID != "" {
		cleanup += "; docker image rm " + shellQuote(oldImageID) + " >/dev/null 2>&1 || true"
	}
	return strings.Join([]string{
		"docker pull " + shellQuote(ref) + " >/dev/null || exit 1",
		"docker stop " + n + " >/dev/null && docker rename " + n + " " + old + " || exit 1",
		"if " + create + " && docker start " + n + " >/dev/null; then " + cleanup + ";" +
			" else docker rm -f " + n + " >/dev/null 2>&1; docker rename " + old + " " + n + " >/dev/null;" +
			" docker start " + n + " >/dev/null 2>&1; exit 1; fi",
	}, "; ")
}

// rawJSON kodiert einen Wert als rohes JSON-Feld.
func rawJSON(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("create-anfrage kodieren: %w", err)
	}
	return b, nil
}
