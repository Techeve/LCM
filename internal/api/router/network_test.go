package router_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/api/router"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/netfilter"
	"LCM/internal/storage/repositories"
)

// hubStub spielt den Hub des Agent-Endpunkts.
type hubStub struct {
	peers        []domain.AgentPeer
	disconnected []string
}

func (h *hubStub) ConnectedPeers() []domain.AgentPeer { return h.peers }
func (h *hubStub) Disconnect(agentID string)          { h.disconnected = append(h.disconnected, agentID) }

// networkApp baut die App mit den Netzbereichen. Der Test-Harness meldet
// 0.0.0.0 als Gegenstelle; sie gilt als Proxy, sodass X-Forwarded-For die
// Client-Adresse vorgibt.
func networkApp(t *testing.T, hub *hubStub) (*fiber.App, *gorm.DB, *services.NetworkAccessService) {
	t.Helper()
	var network *services.NetworkAccessService
	app, db := buildTestAppDB(t, 0, nil, func(d *router.Deps, db *gorm.DB) {
		audit := services.NewAuditService(repositories.NewAuditRepository(db))
		var err error
		network, err = services.NewNetworkAccessService(repositories.NewSettingsRepository(db), audit)
		if err != nil {
			t.Fatal(err)
		}
		network.WithAgentPeers(hub)
		proxies, _ := netfilter.Parse([]string{"0.0.0.0/32"})
		d.ProxyTrust = netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: true, Proxies: proxies})
		d.UIAccess = network.Holder(netfilter.EndpointUI)
		d.Network = network
	})
	return app, db, network
}

// fromAddr ist die Client-Adresse des Aufrufs.
func fromAddr(addr string) map[string]string { return map[string]string{"X-Forwarded-For": addr} }

func putNetwork(t *testing.T, app *fiber.App, token, endpoint, body, client string) *http.Response {
	t.Helper()
	return doRequestWithHeaders(t, app, "PUT", "/api/v1/system/network/"+endpoint, token, body, fromAddr(client))
}

func TestNetworkListShowsEndpointsAndOwnAddress(t *testing.T) {
	app, _, network := networkApp(t, &hubStub{})
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	if _, err := network.Update(netfilter.EndpointMCP, []string{"127.0.0.1"}, services.NetworkUpdate{}, "test"); err != nil {
		t.Fatal(err)
	}

	resp := doRequestWithHeaders(t, app, "GET", "/api/v1/system/network", admin, "", fromAddr("10.1.2.3"))
	if resp.StatusCode != 200 {
		t.Fatalf("Status %d", resp.StatusCode)
	}
	var out struct {
		ClientAddress string `json:"client_address"`
		Endpoints     []struct {
			Endpoint string   `json:"endpoint"`
			Allowed  []string `json:"allowed"`
		} `json:"endpoints"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ClientAddress != "10.1.2.3" || len(out.Endpoints) != 3 {
		t.Fatalf("Antwort: %+v", out)
	}
	mcp := out.Endpoints[2]
	if mcp.Endpoint != "mcp" || len(mcp.Allowed) != 1 {
		t.Errorf("MCP: %+v", mcp)
	}
	if ui := out.Endpoints[0]; ui.Endpoint != "ui" || ui.Allowed == nil || len(ui.Allowed) != 0 {
		t.Errorf("Weboberfläche: %+v", ui)
	}
}

// system:endpoints gehört allein admin: Ein Manager mit settings:manage kommt
// nicht durch, weder lesend noch schreibend.
func TestNetworkNeedsSystemEndpointsPermission(t *testing.T) {
	app, db, _ := networkApp(t, &hubStub{})
	roles := repositories.NewRoleRepository(db)
	role := &domain.Role{Name: "betreiber", Description: "Einstellungen ohne Endpunkte"}
	if err := roles.Create(role); err != nil {
		t.Fatal(err)
	}
	perm, err := roles.EnsurePermission(domain.PermSettingsManage, "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.SetPermissions(role, []domain.Permission{*perm}); err != nil {
		t.Fatal(err)
	}
	users := services.NewUserService(repositories.NewUserRepository(db), roles)
	if _, err := users.CreateUser("betreiber", "", "Regen9-Amsel!Turmfalk", "", "", []string{"betreiber"}, "test"); err != nil {
		t.Fatal(err)
	}
	operator := loginToken(t, app, "betreiber", "Regen9-Amsel!Turmfalk")
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	if r := doRequest(t, app, "GET", "/api/v1/system/network", operator, ""); r.StatusCode != 403 {
		t.Errorf("GET als Betreiber: %d, erwartet 403", r.StatusCode)
	}
	if r := putNetwork(t, app, operator, "mcp", `{"allowed":["10.0.0.0/8"]}`, "10.1.2.3"); r.StatusCode != 403 {
		t.Errorf("PUT als Betreiber: %d, erwartet 403", r.StatusCode)
	}
	if r := doRequest(t, app, "GET", "/api/v1/system/network", admin, ""); r.StatusCode != 200 {
		t.Errorf("GET als Admin: %d, erwartet 200", r.StatusCode)
	}
	for _, code := range domain.ManagerPermissions() {
		if code == domain.PermSystemEndpoints {
			t.Fatal("system:endpoints steckt in den Verwalter-Rechten - es gehört allein zu admin")
		}
	}
}

// Die Liste der Weboberfläche wirkt ab der nächsten Anfrage, und wer sich mit
// ihr aussperren würde, kommt nicht zum Speichern.
func TestNetworkUIListTakesEffectAndProtectsAdmin(t *testing.T) {
	app, _, _ := networkApp(t, &hubStub{})
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	r := putNetwork(t, app, admin, "ui", `{"allowed":["192.168.0.0/16"]}`, "10.1.2.3")
	if r.StatusCode != 409 {
		t.Fatalf("Aussperren: %d, erwartet 409", r.StatusCode)
	}
	assertErrorCode(t, r, "endpoint.would_lock_out")
	if r := doRequestWithHeaders(t, app, "GET", "/api/v1/health", "", "", fromAddr("8.8.8.8")); r.StatusCode != 200 {
		t.Fatalf("die abgelehnte Liste wirkt trotzdem: %d", r.StatusCode)
	}

	if r := putNetwork(t, app, admin, "ui", `{"allowed":["10.0.0.0/8"]}`, "10.1.2.3"); r.StatusCode != 200 {
		t.Fatalf("Speichern mit eigener Adresse: %d", r.StatusCode)
	}
	if r := doRequestWithHeaders(t, app, "GET", "/api/v1/system/network", admin, "", fromAddr("8.8.8.8")); r.StatusCode != 403 {
		t.Errorf("fremde Adresse nach dem Speichern: %d, erwartet 403", r.StatusCode)
	}
	if r := doRequestWithHeaders(t, app, "GET", "/api/v1/system/network", admin, "", fromAddr("10.9.9.9")); r.StatusCode != 200 {
		t.Errorf("Adresse im Netz: %d, erwartet 200", r.StatusCode)
	}
}

func TestNetworkRejectsBadRequests(t *testing.T) {
	app, _, _ := networkApp(t, &hubStub{})
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	cases := []struct {
		endpoint, body, code string
		status               int
	}{
		{"ui", `{"allowed":["kein-netz"]}`, "endpoint.invalid_networks", 422},
		{"ui", `{}`, "request.missing_fields", 400},
		{"ui", `{"allowed":null}`, "request.missing_fields", 400},
		{"web", `{"allowed":[]}`, "endpoint.unknown", 404},
	}
	for _, tc := range cases {
		r := putNetwork(t, app, admin, tc.endpoint, tc.body, "10.1.2.3")
		if r.StatusCode != tc.status {
			t.Errorf("%s %s: %d, erwartet %d", tc.endpoint, tc.body, r.StatusCode, tc.status)
			continue
		}
		assertErrorCode(t, r, tc.code)
	}
}

// Eine engere Agent-Liste nennt die betroffenen Agenten (409, nichts
// gespeichert) und trennt sie erst mit confirm.
func TestNetworkAgentNarrowingNeedsConfirm(t *testing.T) {
	hub := &hubStub{peers: []domain.AgentPeer{
		{AgentID: "a1", Name: "intern", Addr: netip.MustParseAddr("10.0.0.5")},
		{AgentID: "a2", Name: "extern", Addr: netip.MustParseAddr("203.0.113.9")},
	}}
	app, _, network := networkApp(t, hub)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	r := putNetwork(t, app, admin, "agent", `{"allowed":["10.0.0.0/8"]}`, "10.1.2.3")
	if r.StatusCode != 409 {
		t.Fatalf("ohne confirm: %d, erwartet 409", r.StatusCode)
	}
	var body struct {
		Code   string `json:"code"`
		Params struct {
			Count  int `json:"count"`
			Agents []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Address string `json:"address"`
			} `json:"agents"`
		} `json:"params"`
	}
	raw, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "endpoint.agents_outside" || body.Params.Count != 1 ||
		body.Params.Agents[0].ID != "a2" || body.Params.Agents[0].Address != "203.0.113.9" {
		t.Fatalf("Antwort: %s", raw)
	}
	if !network.Allows(netfilter.EndpointAgent, netip.MustParseAddr("203.0.113.9")) || len(hub.disconnected) != 0 {
		t.Fatal("ohne confirm wurde etwas geändert")
	}

	if r := putNetwork(t, app, admin, "agent", `{"allowed":["10.0.0.0/8"],"confirm":true}`, "10.1.2.3"); r.StatusCode != 200 {
		t.Fatalf("mit confirm: %d", r.StatusCode)
	}
	if network.Allows(netfilter.EndpointAgent, netip.MustParseAddr("203.0.113.9")) || len(hub.disconnected) != 1 || hub.disconnected[0] != "a2" {
		t.Errorf("Liste gilt nicht oder falsch getrennt: %v", hub.disconnected)
	}
}

// Der Agent-Endpunkt filtert nach der Liste und liest hinter einem vertrauten
// Proxy die weitergeleitete Adresse; ohne Liste kommt jeder durch.
func TestAgentGatewayHonorsNetworkList(t *testing.T) {
	holder := &netfilter.Holder{}
	proxies, _ := netfilter.Parse([]string{"0.0.0.0/32"})
	gw := router.NewAgentGateway(func(c fiber.Ctx) error { return c.SendString("ok") },
		netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: true, Proxies: proxies}), holder, nil)
	get := func(xff string) int {
		req := httptest.NewRequest(http.MethodGet, "/mqtt", nil)
		req.Header.Set("X-Forwarded-For", xff)
		resp, err := gw.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}

	if got := get("8.8.8.8"); got != 200 {
		t.Fatalf("ohne Liste: %d, erwartet 200", got)
	}
	list, _ := netfilter.Parse([]string{"10.0.0.0/8"})
	holder.Store(list)
	if got := get("8.8.8.8"); got != 403 {
		t.Errorf("außerhalb: %d, erwartet 403", got)
	}
	if got := get("10.1.1.1"); got != 200 {
		t.Errorf("innerhalb: %d, erwartet 200", got)
	}
}
