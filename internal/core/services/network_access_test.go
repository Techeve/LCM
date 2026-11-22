package services_test

import (
	"errors"
	"fmt"
	"net/netip"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/netfilter"
	"LCM/internal/storage/repositories"
)

// fakePeers spielt den Hub: feste verbundene Agenten, getrennte werden notiert.
type fakePeers struct {
	peers        []domain.AgentPeer
	disconnected []string
}

func (f *fakePeers) ConnectedPeers() []domain.AgentPeer { return f.peers }
func (f *fakePeers) Disconnect(agentID string)          { f.disconnected = append(f.disconnected, agentID) }

func peerAt(id, addr string) domain.AgentPeer {
	return domain.AgentPeer{AgentID: id, Name: "srv-" + id, Addr: netip.MustParseAddr(addr)}
}

func newNetworkService(t *testing.T, env *testEnv) *services.NetworkAccessService {
	t.Helper()
	audit := services.NewAuditService(repositories.NewAuditRepository(env.DB()))
	svc, err := services.NewNetworkAccessService(repositories.NewSettingsRepository(env.DB()), audit)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func viewOf(t *testing.T, svc *services.NetworkAccessService, ep netfilter.Endpoint) services.NetworkView {
	t.Helper()
	views, err := svc.View()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.Endpoint == string(ep) {
			return v
		}
	}
	t.Fatalf("kein Eintrag für %s", ep)
	return services.NetworkView{}
}

func auditCount(t *testing.T, env *testEnv, action string) int64 {
	t.Helper()
	var n int64
	if err := env.DB().Model(&domain.AuditLog{}).Where("action = ?", action).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

var adminAddr = netip.MustParseAddr("10.1.2.3")

func TestNetworkAccessDefaultAllowsEverything(t *testing.T) {
	svc := newNetworkService(t, newTestEnv(t))
	for _, ep := range netfilter.Endpoints {
		v := viewOf(t, svc, ep)
		if len(v.Allowed) != 0 {
			t.Errorf("%s: %+v, erwartet leer", ep, v)
		}
		if !svc.Allows(ep, netip.MustParseAddr("8.8.8.8")) {
			t.Errorf("%s: leere Liste muss alles zulassen", ep)
		}
	}
}

// Jeder Endpunkt hat seine eigene gespeicherte Liste.
func TestNetworkAccessListsAreIndependent(t *testing.T) {
	env := newTestEnv(t)
	repo := repositories.NewSettingsRepository(env.DB())
	if err := repo.UpdateFields(map[string]any{"allowed_nets_ui": "192.168.0.0/16", "allowed_nets_agent": "172.16.0.0/12"}); err != nil {
		t.Fatal(err)
	}
	svc := newNetworkService(t, env)

	if v := viewOf(t, svc, netfilter.EndpointUI); len(v.Allowed) != 1 || v.Allowed[0] != "192.168.0.0/16" {
		t.Errorf("Weboberfläche: %+v", v)
	}
	if v := viewOf(t, svc, netfilter.EndpointAgent); len(v.Allowed) != 1 || v.Allowed[0] != "172.16.0.0/12" {
		t.Errorf("Agent: %+v", v)
	}
	if v := viewOf(t, svc, netfilter.EndpointMCP); len(v.Allowed) != 0 {
		t.Errorf("MCP: %+v", v)
	}
	if svc.Allows(netfilter.EndpointUI, adminAddr) || !svc.Allows(netfilter.EndpointMCP, adminAddr) {
		t.Error("die Listen gelten nicht je Endpunkt")
	}
}

// Nach jeder Änderung läuft der Abgleich der config.json; ein Fehler dabei ist
// nicht fatal und steht in der Antwort.
func TestNetworkAccessSyncsConfigJson(t *testing.T) {
	svc := newNetworkService(t, newTestEnv(t))
	calls := 0
	svc.WithConfigSync(func() error { calls++; return nil })

	if res, err := svc.Update(netfilter.EndpointMCP, []string{"127.0.0.1"}, services.NetworkUpdate{}, "admin"); err != nil || res.ConfigSyncError != "" {
		t.Fatalf("Update: %+v, %v", res, err)
	}
	if err := svc.Reset(netfilter.EndpointMCP, "cli"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("Abgleich %d-mal, erwartet 2 (Update und Reset)", calls)
	}

	svc.WithConfigSync(func() error { return errTest })
	res, err := svc.Update(netfilter.EndpointMCP, []string{"127.0.0.1"}, services.NetworkUpdate{}, "admin")
	if err != nil || res.ConfigSyncError != errTest.Error() {
		t.Errorf("Fehler beim Abgleich: %+v, %v", res, err)
	}
	if svc.Allows(netfilter.EndpointMCP, netip.MustParseAddr("8.8.8.8")) {
		t.Error("die Liste gilt wegen des Abgleichfehlers nicht")
	}
}

// Speichern wirkt sofort und übersteht einen Neustart; Einträge werden
// normalisiert gespeichert, das Audit-Log hat den Eintrag.
func TestNetworkAccessUpdateAppliesAndPersists(t *testing.T) {
	env := newTestEnv(t)
	svc := newNetworkService(t, env)

	res, err := svc.Update(netfilter.EndpointUI, []string{"10.0.0.0/8", " 10.0.0.0/8 ", "192.168.1.7"}, services.NetworkUpdate{ClientAddr: adminAddr}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(res.Allowed); got != "[10.0.0.0/8 192.168.1.7/32]" {
		t.Errorf("Liste = %s", got)
	}
	if svc.Allows(netfilter.EndpointUI, netip.MustParseAddr("8.8.8.8")) || !svc.Allows(netfilter.EndpointUI, adminAddr) {
		t.Error("die neue Liste gilt nicht")
	}

	restarted := newNetworkService(t, env)
	if restarted.Allows(netfilter.EndpointUI, netip.MustParseAddr("8.8.8.8")) || !restarted.Allows(netfilter.EndpointUI, adminAddr) {
		t.Error("die Liste kam nach dem Neustart nicht aus der Datenbank")
	}
	if n := auditCount(t, env, "settings.network.update"); n != 1 {
		t.Errorf("%d Audit-Einträge, erwartet 1", n)
	}
}

func TestNetworkAccessRejectsInvalidLists(t *testing.T) {
	svc := newNetworkService(t, newTestEnv(t))
	tooMany := make([]string, netfilter.MaxNetworkEntries+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("10.%d.0.0/16", i)
	}
	for name, entries := range map[string][]string{"Müll": {"kein-netz"}, "zu lang": tooMany} {
		var invalid *services.NetworkInvalidError
		_, err := svc.Update(netfilter.EndpointMCP, entries, services.NetworkUpdate{}, "admin")
		if !errors.As(err, &invalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if viewOf(t, svc, netfilter.EndpointMCP).Allowed == nil || len(viewOf(t, svc, netfilter.EndpointMCP).Allowed) != 0 {
		t.Error("eine abgelehnte Liste darf nichts ändern")
	}
}

// Die Liste der Weboberfläche muss die Adresse des Admins enthalten; leer
// (alle Netze) sperrt niemanden aus.
func TestNetworkAccessRefusesSelfLockout(t *testing.T) {
	env := newTestEnv(t)
	svc := newNetworkService(t, env)
	req := services.NetworkUpdate{ClientAddr: adminAddr}

	if _, err := svc.Update(netfilter.EndpointUI, []string{"192.168.0.0/16"}, req, "admin"); !errors.Is(err, services.ErrNetworkLockout) {
		t.Fatalf("eigene Adresse fehlt: %v", err)
	}
	if !svc.Allows(netfilter.EndpointUI, netip.MustParseAddr("8.8.8.8")) {
		t.Error("abgelehnte Liste wurde trotzdem wirksam")
	}
	if _, err := svc.Update(netfilter.EndpointUI, []string{"10.0.0.0/8"}, req, "admin"); err != nil {
		t.Errorf("eigene Adresse enthalten: %v", err)
	}
	if _, err := svc.Update(netfilter.EndpointUI, nil, req, "admin"); err != nil {
		t.Errorf("leere Liste: %v", err)
	}
	// Die Prüfung gilt nur der Weboberfläche: Agent und MCP schließen den Admin nicht aus.
	if _, err := svc.Update(netfilter.EndpointMCP, []string{"192.168.0.0/16"}, req, "admin"); err != nil {
		t.Errorf("MCP-Liste ohne eigene Adresse: %v", err)
	}
}

// Eine engere Agent-Liste nennt die Betroffenen und speichert erst mit
// Bestätigung - dann trennt sie genau diese.
func TestNetworkAccessAgentNarrowingNeedsConfirmation(t *testing.T) {
	env := newTestEnv(t)
	svc := newNetworkService(t, env)
	hub := &fakePeers{peers: []domain.AgentPeer{peerAt("a1", "10.0.0.5"), peerAt("a2", "203.0.113.9")}}
	svc.WithAgentPeers(hub)

	var outside *services.AgentsOutsideError
	_, err := svc.Update(netfilter.EndpointAgent, []string{"10.0.0.0/8"}, services.NetworkUpdate{}, "admin")
	if !errors.As(err, &outside) || len(outside.Agents) != 1 || outside.Agents[0].AgentID != "a2" {
		t.Fatalf("ohne Bestätigung: %v", err)
	}
	if v := viewOf(t, svc, netfilter.EndpointAgent); len(v.Allowed) != 0 || len(hub.disconnected) != 0 {
		t.Fatalf("ohne Bestätigung wurde etwas geändert: %+v, getrennt %v", v, hub.disconnected)
	}

	res, err := svc.Update(netfilter.EndpointAgent, []string{"10.0.0.0/8"}, services.NetworkUpdate{Confirm: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Disconnected) != 1 || len(hub.disconnected) != 1 || hub.disconnected[0] != "a2" {
		t.Errorf("getrennt: %v / %v, erwartet nur a2", res.Disconnected, hub.disconnected)
	}
	if svc.Allows(netfilter.EndpointAgent, netip.MustParseAddr("203.0.113.9")) {
		t.Error("die neue Agent-Liste gilt nicht")
	}

	// Liegen alle Agenten im neuen Bereich, braucht es keine Bestätigung.
	hub.peers, hub.disconnected = []domain.AgentPeer{peerAt("a1", "10.0.0.5")}, nil
	if _, err := svc.Update(netfilter.EndpointAgent, []string{"10.0.0.0/8"}, services.NetworkUpdate{}, "admin"); err != nil || len(hub.disconnected) != 0 {
		t.Errorf("alle Agenten innerhalb: %v, getrennt %v", err, hub.disconnected)
	}
}

func TestNetworkAccessResetOpensEverything(t *testing.T) {
	env := newTestEnv(t)
	svc := newNetworkService(t, env)
	if _, err := svc.Update(netfilter.EndpointMCP, []string{"10.0.0.0/8"}, services.NetworkUpdate{}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reset(netfilter.EndpointMCP, "cli"); err != nil {
		t.Fatal(err)
	}
	if !svc.Allows(netfilter.EndpointMCP, netip.MustParseAddr("8.8.8.8")) || len(viewOf(t, svc, netfilter.EndpointMCP).Allowed) != 0 {
		t.Error("Reset hat die Liste nicht geleert")
	}
	var actor string
	if err := env.DB().Model(&domain.AuditLog{}).Where("action = ?", "settings.network.reset").Select("actor").Scan(&actor).Error; err != nil || actor != "cli" {
		t.Errorf("Audit-Actor = %q (%v), erwartet cli", actor, err)
	}
}
