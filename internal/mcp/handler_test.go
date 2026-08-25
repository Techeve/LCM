package mcp

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// fakeProvider liefert feste Testdaten.
type fakeProvider struct{ views []ServerView }

func (f *fakeProvider) Servers() ([]ServerView, error) { return f.views, nil }
func (f *fakeProvider) Server(idOrName string) (*ServerView, bool, error) {
	for i := range f.views {
		if f.views[i].Name == idOrName {
			return &f.views[i], true, nil
		}
	}
	return nil, false, nil
}

func testApp(auth AuthFunc) *fiber.App {
	h := NewHandler(&fakeProvider{views: []ServerView{
		{ID: 1, Name: "web01", OSName: "Ubuntu", Status: "green", Reachable: true, CVECritical: 0},
	}}, auth, "LCM", "1.2.3")
	app := fiber.New()
	app.Post("/mcp", h.Fiber())
	return app
}

func post(t *testing.T, app *fiber.App, body, bearer string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestMCPAuthRequired: ohne/mit falschem Bearer-Token → 401.
func TestMCPAuthRequired(t *testing.T) {
	app := testApp(func(tok string) bool { return tok == "good" })
	if code, _ := post(t, app, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, ""); code != 401 {
		t.Errorf("ohne Token: erwartet 401, bekam %d", code)
	}
	if code, _ := post(t, app, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "bad"); code != 401 {
		t.Errorf("falscher Token: erwartet 401, bekam %d", code)
	}
	if code, _ := post(t, app, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "good"); code != 200 {
		t.Errorf("gültiger Token: erwartet 200, bekam %d", code)
	}
}

// TestMCPInitialize: Handshake liefert protocolVersion + serverInfo.
func TestMCPInitialize(t *testing.T) {
	app := testApp(func(string) bool { return true })
	_, body := post(t, app, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, "x")
	var r struct {
		Result struct {
			ProtocolVersion string         `json:"protocolVersion"`
			ServerInfo      map[string]any `json:"serverInfo"`
			Capabilities    map[string]any `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("parse: %v (%s)", err, body)
	}
	if r.Result.ProtocolVersion != "2025-06-18" {
		t.Errorf("protocolVersion = %q", r.Result.ProtocolVersion)
	}
	if r.Result.ServerInfo["name"] != "LCM" {
		t.Errorf("serverInfo.name = %v", r.Result.ServerInfo["name"])
	}
	if _, ok := r.Result.Capabilities["tools"]; !ok {
		t.Error("capabilities.tools fehlt")
	}
}

// TestMCPToolsList: tools/list liefert die read-only Tools.
func TestMCPToolsList(t *testing.T) {
	app := testApp(func(string) bool { return true })
	_, body := post(t, app, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "x")
	var r struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range r.Result.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"list_servers", "get_server", "fleet_summary"} {
		if !names[want] {
			t.Errorf("tool %q fehlt in tools/list", want)
		}
	}
}

// TestMCPToolCall: list_servers liefert die Serverdaten.
func TestMCPToolCall(t *testing.T) {
	app := testApp(func(string) bool { return true })
	_, body := post(t, app, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_servers","arguments":{}}}`, "x")
	if !strings.Contains(body, "web01") {
		t.Errorf("list_servers sollte web01 enthalten: %s", body)
	}
}

// TestServerViewHasNoSecretFields ist die zentrale Sicherheitszusage: das
// read-only DTO darf NIE ein Feld tragen, das ein Geheimnis transportiert.
// Prüft rekursiv Feldnamen UND JSON-Tags gegen verbotene Muster.
func TestServerViewHasNoSecretFields(t *testing.T) {
	forbidden := []string{
		"password", "passwort", "secret", "privatekey", "private_key",
		"serviceuser", "service_user", "loginuser", "login_user",
		"fingerprint", "publickey", "public_key", "token", "keyhash",
		"key_hash", "enc", "hash", "credential", "ssh_key",
	}
	var check func(rt reflect.Type)
	check = func(rt reflect.Type) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name := strings.ToLower(f.Name)
			tag := strings.ToLower(f.Tag.Get("json"))
			for _, bad := range forbidden {
				if strings.Contains(name, bad) || strings.Contains(tag, bad) {
					t.Errorf("ServerView-Feld %q (tag %q) matcht verbotenes Muster %q - Geheimnis-Leck möglich!", f.Name, tag, bad)
				}
			}
		}
	}
	check(reflect.TypeOf(ServerView{}))
}
