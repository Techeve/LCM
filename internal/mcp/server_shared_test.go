package mcp_test

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
	"LCM/internal/mcp"
)

// newPool legt einen Zertifikatspool an, der nur das LCM-Zertifikat liefert.
func newPool(t *testing.T) *tlsx.Pool {
	t.Helper()
	dir := t.TempDir()
	tlsxtest.ServerCert(t, dir, time.Now().Add(-time.Hour))
	store, err := tlsx.NewCertStore(filepath.Join(dir, tlsx.CertFileName), filepath.Join(dir, tlsx.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	return tlsx.NewPool(store)
}

func newServer() *mcp.Server {
	handler := mcp.NewHandler(noServers{}, func(string) bool { return true }, "LCM", "test")
	return mcp.NewServer(handler, slog.New(slog.DiscardHandler))
}

func freePort(t *testing.T) int {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = probe.Close() }()
	return probe.Addr().(*net.TCPAddr).Port
}

// pingTLS ruft MCP über HTTPS auf und liefert Status und TLS-Zustand.
func pingTLS(addr string) (int, *tls.ConnectionState, error) {
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	req, err := http.NewRequest("POST", "https://"+addr+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer x")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, resp.TLS, nil
}

func waitUntilServing(t *testing.T, addr string) {
	t.Helper()
	for range 50 {
		if conn, err := net.Dial("tcp", addr); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s nimmt keine Verbindungen an", addr)
}

// Am gemeinsamen Port bedient derselbe Server die Verbindungen des Routers;
// beim Stoppen wird der Listener geschlossen.
func TestMCPBedientDenGemeinsamenPort(t *testing.T) {
	pool := newPool(t)
	sharedRaw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	shared := tls.NewListener(sharedRaw, pool.TLSConfig())

	server := newServer().WithShared(func() net.Listener { return shared })
	_ = server.Apply(true, "127.0.0.1", freePort(t), false)
	t.Cleanup(server.Shutdown)
	waitUntilServing(t, sharedRaw.Addr().String())

	if status, _, err := pingTLS(sharedRaw.Addr().String()); err != nil || status != 200 {
		t.Fatalf("am gemeinsamen Port: Status %d, Fehler %v", status, err)
	}

	_ = server.Apply(false, "", 0, false)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", sharedRaw.Addr().String())
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("der Listener am gemeinsamen Port blieb nach dem Stoppen offen")
}
