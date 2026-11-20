package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/api/router"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/portscan"
	"LCM/internal/storage/repositories"
)

// portScanDials hält fest, welche Adressen der Scanner wählt. Jeder Port
// lehnt ab - es geht um das Ziel, nicht um das Ergebnis.
type portScanDials struct {
	mu    sync.Mutex
	hosts map[string]bool
}

func (d *portScanDials) dial(_ context.Context, _, address string) (net.Conn, error) {
	host, _, _ := net.SplitHostPort(address)
	d.mu.Lock()
	d.hosts[host] = true
	d.mu.Unlock()
	return nil, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
}

func (d *portScanDials) seen() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for h := range d.hosts {
		out = append(out, h)
	}
	return out
}

// portScanApp verdrahtet den Portscan mit einem Schein-Netz: Jeder Name
// löst auf 192.0.2.50 auf.
func portScanApp(t *testing.T, demoPublic bool) (*fiber.App, *gorm.DB, *portScanDials) {
	t.Helper()
	dials := &portScanDials{hosts: map[string]bool{}}
	app, db := buildTestAppDB(t, 0, nil, func(d *router.Deps, db *gorm.DB) {
		d.DemoPublic = demoPublic
		d.PortScans = services.NewPortScanService(repositories.NewServerRepository(db),
			repositories.NewPortScanRepository(db), d.Jobs, d.Audit).
			WithScanner(func() *portscan.Scanner {
				s := portscan.New()
				s.Dial, s.Gap, s.Timeout = dials.dial, 0, time.Second
				return s
			}, func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("192.0.2.50")}, nil
			})
	})
	return app, db, dials
}

func waitJobDone(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var job domain.Job
		if db.Where("id = ?", id).First(&job).Error == nil && job.Status == domain.JobStatusSuccess {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Job %s nicht rechtzeitig fertig", id)
}

// Der Rumpf wird ignoriert: Wer eine fremde Adresse mitschickt, scannt
// trotzdem nur den gespeicherten Server.
func TestPortScanZielIstImmerDerGespeicherteServer(t *testing.T) {
	app, db, dials := portScanApp(t, false)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")

	path := fmt.Sprintf("/api/v1/servers/%d/port-scan", id)
	resp := doRequest(t, app, "POST", path, admin, `{"host":"10.9.9.9","address":"10.9.9.9","targets":["10.9.9.9"]}`)
	if resp.StatusCode != 202 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Start: status %d: %s", resp.StatusCode, body)
	}
	var started struct {
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil || started.JobID == "" {
		t.Fatalf("keine Job-ID: %v", err)
	}
	waitJobDone(t, db, started.JobID)
	if got := dials.seen(); len(got) != 1 || got[0] != "192.0.2.50" {
		t.Fatalf("gewählte Adressen %v - erlaubt ist nur die des Servers", got)
	}

	get := doRequest(t, app, "GET", path, admin, "")
	var view struct {
		ScannedAt *time.Time `json:"scanned_at"`
		Targets   []struct {
			Address string `json:"address"`
			Closed  int    `json:"closed"`
		} `json:"targets"`
	}
	if err := json.NewDecoder(get.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.ScannedAt == nil || len(view.Targets) != 1 || view.Targets[0].Address != "192.0.2.50" || view.Targets[0].Closed == 0 {
		t.Fatalf("Ergebnis %+v", view)
	}
}

// Lesen braucht servers:read, Starten servers:write; ein unbekannter Server
// ist 404 und löst nichts aus.
func TestPortScanRechte(t *testing.T) {
	app, db, dials := portScanApp(t, false)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")
	reader := readOnlyUserToken(t, app, db, admin)
	var leser domain.User
	if err := db.Where("username = ?", "leser").First(&leser).Error; err != nil {
		t.Fatal(err)
	}
	addServerToGroup(t, db, "Leser-Gruppe", id, &leser)

	path := fmt.Sprintf("/api/v1/servers/%d/port-scan", id)
	if resp := doRequest(t, app, "POST", path, reader, ""); resp.StatusCode != 403 {
		t.Errorf("Start ohne servers:write: erwartet 403, bekam %d", resp.StatusCode)
	}
	if resp := doRequest(t, app, "GET", path, reader, ""); resp.StatusCode != 200 {
		t.Errorf("Lesen mit servers:read: erwartet 200, bekam %d", resp.StatusCode)
	}
	if resp := doRequest(t, app, "GET", path, "", ""); resp.StatusCode != 401 {
		t.Errorf("Lesen ohne Anmeldung: erwartet 401, bekam %d", resp.StatusCode)
	}
	if resp := doRequest(t, app, "POST", "/api/v1/servers/9999/port-scan", admin, ""); resp.StatusCode != 404 {
		t.Errorf("unbekannter Server: erwartet 404, bekam %d", resp.StatusCode)
	}
	if len(dials.seen()) != 0 {
		t.Fatalf("es wurde gewählt: %v", dials.seen())
	}
}

// Der LCM-Host über Loopback hat keine Adresse, die ein Scan sinnvoll prüft.
func TestPortScanOhneZielIst409(t *testing.T) {
	app, db, _ := portScanApp(t, false)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "lcm", "apt")
	if err := db.Model(&domain.Server{}).Where("id = ?", id).Update("host", "127.0.0.1").Error; err != nil {
		t.Fatal(err)
	}
	resp := doRequest(t, app, "POST", fmt.Sprintf("/api/v1/servers/%d/port-scan", id), admin, "")
	if resp.StatusCode != 409 {
		t.Fatalf("erwartet 409, bekam %d", resp.StatusCode)
	}
	assertErrorCode(t, resp, "server.portscan_no_target")
}

// Die öffentliche Demo startet keinen Scan; das Lesen bleibt.
func TestPortScanInDerOeffentlichenDemoGesperrt(t *testing.T) {
	app, db, dials := portScanApp(t, true)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	id := addCapabilityServer(t, db, "web01", "apt")
	path := fmt.Sprintf("/api/v1/servers/%d/port-scan", id)
	if resp := doRequest(t, app, "POST", path, admin, ""); resp.StatusCode != 403 {
		t.Errorf("Demo: erwartet 403, bekam %d", resp.StatusCode)
	}
	if resp := doRequest(t, app, "GET", path, admin, ""); resp.StatusCode != 200 {
		t.Errorf("Demo lesen: erwartet 200, bekam %d", resp.StatusCode)
	}
	if len(dials.seen()) != 0 {
		t.Fatalf("Demo hat gewählt: %v", dials.seen())
	}
}
