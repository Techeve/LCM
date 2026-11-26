package mcp_test

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"LCM/internal/mcp"
)

// Mit TLS liefert der MCP-Listener das Zertifikat des Pools.
func TestMCPMitTLSLiefertDasPoolZertifikat(t *testing.T) {
	pool := newPool(t)
	server := newServer().WithTLS(pool.TLSConfig)
	port := freePort(t)
	_ = server.Apply(true, "127.0.0.1", port, true)
	t.Cleanup(server.Shutdown)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	waitUntilServing(t, addr)

	status, state, err := pingTLS(addr)
	if err != nil || status != 200 {
		t.Fatalf("MCP über TLS: Status %d, Fehler %v", status, err)
	}
	want := pool.CertificateFor("")
	if got := state.PeerCertificates[0].Raw; string(got) != string(want.Certificate[0]) {
		t.Error("MCP lieferte nicht das Zertifikat des Pools")
	}

	// Ein Klartext-Client kommt nicht durch.
	if resp, err := http.Post("http://"+addr+"/mcp", "application/json", strings.NewReader("{}")); err == nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == 200 {
			t.Error("MCP mit TLS beantwortete eine Klartext-Anfrage mit 200")
		}
	}
}

// Wechselt nur der TLS-Schalter, startet der Listener neu.
func TestMCPUmschaltenAufTLSStartetNeu(t *testing.T) {
	server := newServer().WithTLS(newPool(t).TLSConfig)
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	_ = server.Apply(true, "127.0.0.1", port, false)
	t.Cleanup(server.Shutdown)
	waitUntilServing(t, addr)
	if got := ping(t, addr, ""); got != 200 {
		t.Fatalf("ohne TLS: %d", got)
	}

	_ = server.Apply(true, "127.0.0.1", port, true)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status, _, err := pingTLS(addr); err == nil && status == 200 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("nach dem Umschalten antwortet der Listener nicht über TLS")
}

// Ohne Zertifikatspool (--dev) spricht MCP auf Loopback HTTP, statt eine Adressänderung zu sperren.
func TestMCPMitTLSOhnePoolLaeuftAufLoopbackOhneTLS(t *testing.T) {
	server := newServer()
	port := freePort(t)
	if err := server.Apply(true, "127.0.0.1", port, true); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	t.Cleanup(server.Shutdown)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	waitUntilServing(t, addr)
	if got := ping(t, addr, ""); got != 200 {
		t.Errorf("ohne Pool, Loopback: %d", got)
	}
}

// Ohne Zertifikatspool startet MCP außerhalb von Loopback nicht, statt im Klartext zu laufen; es ist kein Fehler.
func TestMCPMitTLSOhnePoolStartetNichtImNetz(t *testing.T) {
	server := newServer()
	port := freePort(t)
	if err := server.Apply(true, "0.0.0.0", port, true); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	t.Cleanup(server.Shutdown)

	time.Sleep(200 * time.Millisecond)
	if conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		_ = conn.Close()
		t.Fatal("MCP lauscht, obwohl TLS verlangt und nicht möglich war")
	}
}

// Wechselt nur TLS und lässt sich die Adresse nicht wieder binden, kommt der
// Fehler beim Aufrufer an (nicht nur im Log).
func TestMCPUmschaltenAufTLSMeldetBindFehler(t *testing.T) {
	defer mcp.SetRebindForTest(50 * time.Millisecond)()
	server := newServer().WithTLS(newPool(t).TLSConfig)
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if err := server.Apply(true, "127.0.0.1", port, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	waitUntilServing(t, addr)

	// Ein anderer Prozess nimmt den Port, sobald der Listener ihn freigibt.
	var squatter net.Listener
	defer mcp.SetAfterReleaseForTest(func(addr string) {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			t.Errorf("Port nach der Freigabe nicht zu belegen: %v", err)
			return
		}
		squatter = ln
	})()
	err := server.Apply(true, "127.0.0.1", port, true)
	if squatter != nil {
		_ = squatter.Close()
	}
	if err == nil {
		t.Fatal("kein Fehler, obwohl die Adresse nicht mehr zu binden war")
	}
}

// Gelingt das Binden, meldet der Wechsel von TLS keinen Fehler.
func TestMCPUmschaltenAufTLSOhneFehler(t *testing.T) {
	server := newServer().WithTLS(newPool(t).TLSConfig)
	port := freePort(t)
	if err := server.Apply(true, "127.0.0.1", port, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	waitUntilServing(t, fmt.Sprintf("127.0.0.1:%d", port))
	if err := server.Apply(true, "127.0.0.1", port, true); err != nil {
		t.Errorf("Wechsel auf TLS: %v", err)
	}
}

// Hält der Server gleich nach dem Start an, gibt er den Port trotzdem frei,
// auch wenn die Listen-Goroutine den Listener noch nicht übernommen hat.
func TestMCPGibtPortFreiWennGleichNachDemStartGestoppt(t *testing.T) {
	for range 20 {
		server := newServer()
		port := freePort(t)
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		if err := server.Apply(true, "127.0.0.1", port, false); err != nil {
			t.Fatal(err)
		}
		server.Shutdown()
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatalf("Port nach dem Stoppen noch belegt: %v", err)
		}
		_ = ln.Close()
	}
}
