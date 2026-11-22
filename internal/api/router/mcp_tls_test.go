package router_test

import "testing"

// MCP spricht immer TLS und bindet an den Host der Oberfläche: PUT /settings/mcp
// nimmt nur noch enabled; bind_host, port und tls im Body älterer Clients werden
// überlesen, nicht abgelehnt.
func TestMCPSchalterIgnoriertHostPortUndTLS(t *testing.T) {
	do, status := sharedPortApp(t)
	do("PATCH", "/api/v1/settings/global",
		`{"public_base_url":"https://lcm.example.org","public_mcp_url":"https://mcp.example.org"}`)
	if status() != 200 {
		t.Fatalf("Adressen: %d", status())
	}
	before := do("GET", "/api/v1/settings/global", "")

	saved := do("PUT", "/api/v1/settings/mcp", `{"enabled":true,"bind_host":"0.0.0.0","port":9999,"tls":false}`)
	if status() != 200 || saved["mcp_enabled"] != true || saved["mcp_tls"] != true {
		t.Fatalf("PUT: %d %v, erwartet 200 mit mcp_enabled und mcp_tls true", status(), saved)
	}
	got := do("GET", "/api/v1/settings/global", "")
	if got["mcp_enabled"] != true || got["mcp_tls"] != true {
		t.Errorf("GET: mcp_enabled=%v mcp_tls=%v", got["mcp_enabled"], got["mcp_tls"])
	}
	if got["mcp_port"] != before["mcp_port"] || got["mcp_bind_host"] != before["mcp_bind_host"] {
		t.Errorf("Port oder Bind-Host wurden aus dem Body übernommen: vorher %v/%v, nachher %v/%v",
			before["mcp_port"], before["mcp_bind_host"], got["mcp_port"], got["mcp_bind_host"])
	}

	off := do("PUT", "/api/v1/settings/mcp", `{"enabled":false}`)
	if status() != 200 || off["mcp_enabled"] != false || off["mcp_tls"] != true {
		t.Errorf("Ausschalten: %d %v", status(), off)
	}
}

// Einschalten ohne Namen im Modus C lehnt PUT /settings/mcp mit settings.invalid_input ab.
func TestMCPEinschaltenOhneNamenImModusC(t *testing.T) {
	do, status := sharedPortApp(t)
	body := do("PUT", "/api/v1/settings/mcp", `{"enabled":true}`)
	if status() != 400 || body["code"] != "settings.invalid_input" {
		t.Errorf("%d %v, erwartet 400 settings.invalid_input", status(), body)
	}
}

// Agent- und MCP-Schalter lassen sich mit den Adressen in einem PATCH speichern;
// ohne Namen im Modus C lehnt es das Einschalten ab, ein leerer Name schaltet
// den Dienst aus. Die Antwort trägt agent_servers und die Sperre address_locks.agent_enabled.
func TestSchalterImGlobalenPatch(t *testing.T) {
	do, status := sharedPortApp(t)

	body := do("PATCH", "/api/v1/settings/global", `{"agent_enabled":true}`)
	if status() != 400 || body["code"] != "settings.invalid_input" {
		t.Fatalf("Agent ohne Namen: %d %v, erwartet 400 settings.invalid_input", status(), body)
	}

	body = do("PATCH", "/api/v1/settings/global",
		`{"public_base_url":"https://lcm.example.org","public_agent_url":"https://agent.example.org","agent_enabled":true,`+
			`"public_mcp_url":"https://mcp.example.org","mcp_enabled":true}`)
	if status() != 200 || body["agent_enabled"] != true || body["mcp_enabled"] != true {
		t.Fatalf("mit Namen: %d %v", status(), body)
	}

	body = do("PATCH", "/api/v1/settings/global", `{"public_agent_url":""}`)
	if status() != 200 || body["agent_enabled"] != false || body["mcp_enabled"] != true {
		t.Errorf("leerer Agent-Name: %d agent=%v mcp=%v, erwartet aus, an", status(), body["agent_enabled"], body["mcp_enabled"])
	}

	got := do("GET", "/api/v1/settings/global", "")
	locks, _ := got["address_locks"].(map[string]any)
	if _, ok := got["agent_servers"].(float64); !ok {
		t.Errorf("agent_servers fehlt: %v", got["agent_servers"])
	}
	if locks["agent_enabled"] != false {
		t.Errorf("address_locks.agent_enabled = %v, erwartet false", locks["agent_enabled"])
	}
}
