package mcp_test

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"LCM/internal/api/middlewares"
	"LCM/internal/mcp"
	"LCM/internal/netfilter"
)

type noServers struct{}

func (noServers) Servers() ([]mcp.ServerView, error)           { return nil, nil }
func (noServers) Server(string) (*mcp.ServerView, bool, error) { return nil, false, nil }

// startGuarded startet den MCP-Listener auf einem freien Port mit dem
// Netzbereich-Filter und liefert die Adresse.
func startGuarded(t *testing.T, holder *netfilter.Holder, trust *netfilter.ProxyTrustHolder) string {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	handler := mcp.NewHandler(noServers{}, func(string) bool { return true }, "LCM", "test")
	server := mcp.NewServer(handler, slog.New(slog.DiscardHandler)).WithGuards(
		middlewares.ResolveClientIP(trust),
		middlewares.IPAllowlist(holder, netfilter.EndpointMCP, nil),
	)
	_ = server.Apply(true, "127.0.0.1", port, false)
	t.Cleanup(server.Shutdown)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for range 50 {
		if conn, err := net.Dial("tcp", addr); err == nil {
			_ = conn.Close()
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("MCP-Listener startet nicht")
	return ""
}

func ping(t *testing.T, addr, xff string) int {
	t.Helper()
	req, err := http.NewRequest("POST", "http://"+addr+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer x")
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func listOf(t *testing.T, entries ...string) netfilter.Allowlist {
	t.Helper()
	list, err := netfilter.Parse(entries)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// Der MCP-Endpunkt weist Adressen außerhalb der Liste mit 403 ab, ohne Liste
// lässt er alle durch, und eine Änderung gilt ohne Neustart.
func TestMCPNetworkList(t *testing.T) {
	holder := &netfilter.Holder{}
	addr := startGuarded(t, holder, nil)

	if got := ping(t, addr, ""); got != 200 {
		t.Fatalf("ohne Liste: %d, erwartet 200", got)
	}
	holder.Store(listOf(t, "10.0.0.0/8"))
	if got := ping(t, addr, ""); got != 403 {
		t.Errorf("Peer 127.0.0.1 außerhalb der Liste: %d, erwartet 403", got)
	}
	holder.Store(listOf(t, "127.0.0.0/8"))
	if got := ping(t, addr, ""); got != 200 {
		t.Errorf("Peer innerhalb der Liste: %d, erwartet 200", got)
	}
}

// Hinter einem vertrauten Proxy zählt die weitergeleitete Adresse, hinter
// einem nicht vertrauten wird die Kopfzeile ignoriert.
func TestMCPNetworkListForwardedFor(t *testing.T) {
	holder := &netfilter.Holder{}
	holder.Store(listOf(t, "10.0.0.0/8"))

	trusted := netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: true, Proxies: listOf(t, "127.0.0.1/32")})
	addr := startGuarded(t, holder, trusted)
	if got := ping(t, addr, "10.1.1.1"); got != 200 {
		t.Errorf("vertrauter Proxy, Adresse im Netz: %d, erwartet 200", got)
	}
	if got := ping(t, addr, "8.8.8.8"); got != 403 {
		t.Errorf("vertrauter Proxy, Adresse außerhalb: %d, erwartet 403", got)
	}

	untrusted := netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: true, Proxies: listOf(t, "192.0.2.1/32")})
	addr = startGuarded(t, holder, untrusted)
	if got := ping(t, addr, "10.1.1.1"); got != 403 {
		t.Errorf("nicht vertrauter Proxy, gefälschte Kopfzeile: %d, erwartet 403", got)
	}
}
