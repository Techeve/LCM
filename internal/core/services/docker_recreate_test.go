package services

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// Ein laufender Standalone-Container mit Port, Volume, zweitem Netz und einem
// anonymen Volume aus dem Image - so, wie `docker inspect` ihn ausgibt.
const inspectStandalone = `[{"Id":"bbb222ccc333ddd444eee555fff666aaa111bbb222ccc333ddd444eee555fff6","Name":"/cache",
"Config":{"Image":"redis:7","Env":["REDIS_PASSWORD=x"],"Cmd":["redis-server"],"Volumes":{"/data":{}}},
"HostConfig":{"Binds":["/srv/cache:/etc/redis:ro"],"RestartPolicy":{"Name":"unless-stopped"},
  "PortBindings":{"6379/tcp":[{"HostIp":"127.0.0.1","HostPort":"6379"}]},"NetworkMode":"proxy"},
"Mounts":[{"Type":"bind","Source":"/srv/cache","Destination":"/etc/redis","RW":false},
  {"Type":"volume","Name":"cache-anon-vol","Destination":"/data","RW":true}],
"NetworkSettings":{"Networks":{"proxy":{"Aliases":["cache","bbb222ccc333"]},"bridge":{"Aliases":null}}}}]`

// recreateBody übernimmt die Konfiguration, setzt das neue Image und
// ergänzt das anonyme Volume, das nicht schon über Binds steht.
func TestRecreateBodyKeepsConfigOnNewImage(t *testing.T) {
	raw, err := recreateBody(inspectStandalone, "redis:7")
	if err != nil {
		t.Fatalf("recreateBody: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	var image string
	_ = json.Unmarshal(body["Image"], &image)
	if image != "redis:7" {
		t.Errorf("Image = %q, erwartet redis:7", image)
	}
	// Die Engine-API erwartet die Config-Felder auf oberster Ebene.
	var cfg struct {
		Env []string `json:"Env"`
		Cmd []string `json:"Cmd"`
	}
	_ = json.Unmarshal(raw, &cfg)
	if len(cfg.Env) != 1 || cfg.Env[0] != "REDIS_PASSWORD=x" || len(cfg.Cmd) != 1 {
		t.Errorf("Config nicht übernommen: %+v", cfg)
	}
	var hc struct {
		Binds         []string `json:"Binds"`
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
		PortBindings map[string][]struct {
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
		Mounts []struct {
			Type, Source, Target string
			ReadOnly             bool
		} `json:"Mounts"`
	}
	_ = json.Unmarshal(body["HostConfig"], &hc)
	if hc.RestartPolicy.Name != "unless-stopped" || hc.PortBindings["6379/tcp"][0].HostPort != "6379" {
		t.Errorf("HostConfig (Restart/Ports) nicht übernommen: %+v", hc)
	}
	if len(hc.Binds) != 1 {
		t.Errorf("Binds müssen erhalten bleiben: %v", hc.Binds)
	}
	// Das Bind-Volume /etc/redis steht schon in Binds und darf nicht doppelt
	// kommen; das anonyme /data mit seinem vorhandenen Namen schon.
	if len(hc.Mounts) != 1 || hc.Mounts[0].Source != "cache-anon-vol" || hc.Mounts[0].Target != "/data" {
		t.Errorf("anonymes volume fehlt oder doppelt: %+v", hc.Mounts)
	}
	// Netz: Aliase ohne die alte Kurz-ID; das Standardnetz bleibt ohne Alias.
	var nc struct {
		EndpointsConfig map[string]struct {
			Aliases []string `json:"Aliases"`
		} `json:"EndpointsConfig"`
	}
	_ = json.Unmarshal(body["NetworkingConfig"], &nc)
	if got := nc.EndpointsConfig["proxy"].Aliases; len(got) != 1 || got[0] != "cache" {
		t.Errorf("aliase des netzes falsch: %v", got)
	}
	if _, ok := nc.EndpointsConfig["bridge"]; !ok {
		t.Errorf("zweites netz fehlt: %+v", nc.EndpointsConfig)
	}
}

func TestRecreateBodyRejectsOtherThanOneContainer(t *testing.T) {
	if _, err := recreateBody(`[]`, "redis:7"); err == nil {
		t.Error("leere inspect-ausgabe muss abgewiesen werden")
	}
	if _, err := recreateBody(`kein json`, "redis:7"); err == nil {
		t.Error("kaputte inspect-ausgabe muss abgewiesen werden")
	}
}

// Das Skript holt das neue Image zuerst, benennt den alten Container nur um,
// erstellt den neuen aus dem Body und stellt bei jedem Fehler den alten wieder
// her. Das überholte Image geht ohne -f weg.
func TestRecreateScriptOrderAndRollback(t *testing.T) {
	body := []byte(`{"Image":"redis:7"}`)
	script := recreateScript("cache", "redis:7", "sha256:old", body)

	pull := strings.Index(script, "docker pull 'redis:7'")
	stop := strings.Index(script, "docker stop 'cache'")
	create := strings.Index(script, "--unix-socket /var/run/docker.sock")
	if pull < 0 || stop < 0 || create < 0 || !(pull < stop && stop < create) {
		t.Fatalf("reihenfolge falsch (pull %d, stop %d, create %d):\n%s", pull, stop, create, script)
	}
	if !strings.Contains(script, "docker rename 'cache' 'cache.lcm-old'") {
		t.Error("alter container wird nicht zuerst umbenannt")
	}
	if !strings.Contains(script, "docker rename 'cache.lcm-old' 'cache'") || !strings.Contains(script, "docker start 'cache'") {
		t.Error("rollback fehlt")
	}
	if !strings.Contains(script, "docker image rm 'sha256:old' >/dev/null 2>&1") || strings.Contains(script, "image rm -f") {
		t.Errorf("überholtes image muss ohne -f entfernt werden:\n%s", script)
	}
	// Der Body steckt base64-kodiert im Skript - und kommt unverändert an.
	enc := base64.StdEncoding.EncodeToString(body)
	if !strings.Contains(script, enc) {
		t.Errorf("create-body fehlt im skript")
	}
}

func TestRecreateScriptWithoutOldImageID(t *testing.T) {
	script := recreateScript("cache", "redis:7", "", []byte(`{}`))
	if strings.Contains(script, "docker image rm") {
		t.Errorf("ohne alte image-id darf nichts gelöscht werden:\n%s", script)
	}
}

func TestValidDockerScope(t *testing.T) {
	for _, ok := range []string{"", "  ", "webshop", "web-1", "cache.v2"} {
		if !validDockerScope(ok) {
			t.Errorf("%q sollte gültig sein", ok)
		}
	}
	for _, bad := range []string{"a b", "x;rm -rf /", "$(id)", "-x"} {
		if validDockerScope(bad) {
			t.Errorf("%q sollte abgewiesen werden", bad)
		}
	}
}

func TestInspectImageRef(t *testing.T) {
	ref, err := inspectImageRef(inspectStandalone)
	if err != nil || ref != "redis:7" {
		t.Errorf("ref = %q, err = %v", ref, err)
	}
	ref, _ = inspectImageRef(`[{"Config":{"Image":"sha256:abc"}}]`)
	if ref != "sha256:abc" {
		t.Errorf("auch eine ID wird unverändert geliefert, damit der Aufrufer sie erkennt: %q", ref)
	}
	if _, err := inspectImageRef(`nix`); err == nil {
		t.Error("kaputte Ausgabe muss abgewiesen werden")
	}
}
