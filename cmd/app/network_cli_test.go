package main

import (
	"bytes"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"LCM/internal/control"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/netfilter"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// newNetworkService baut einen Dienst und öffnet seine Steuerbuchse; geliefert
// wird das Datenverzeichnis.
func newNetworkService(t *testing.T) (*services.NetworkAccessService, string) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.GlobalSettings{ID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	audit := services.NewAuditService(repositories.NewAuditRepository(db))
	network, err := services.NewNetworkAccessService(repositories.NewSettingsRepository(db), audit)
	if err != nil {
		t.Fatal(err)
	}
	dataDir := shortTempDir(t)
	server, err := control.Listen(control.SocketPath(dataDir), networkCommands(network))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return network, dataDir
}

func runNetwork(dataDir string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runNetworkCommand(dataDir, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestNetworkStatusAusgabe(t *testing.T) {
	network, dataDir := newNetworkService(t)
	if _, err := network.Update(netfilter.EndpointAgent, []string{"10.0.0.0/8", "192.168.1.5"}, services.NetworkUpdate{}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := network.Update(netfilter.EndpointMCP, []string{"127.0.0.1"}, services.NetworkUpdate{}, "admin"); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runNetwork(dataDir, "status")
	if code != 0 {
		t.Fatalf("Exit %d: %s", code, errOut)
	}
	for _, want := range []string{
		"Weboberfläche  alle Netze erlaubt\n",
		"Agent          10.0.0.0/8, 192.168.1.5/32\n",
		"MCP            127.0.0.1/32\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Ausgabe enthält nicht %q:\n%s", want, out)
		}
	}
}

func TestNetworkResetLeertDieListe(t *testing.T) {
	network, dataDir := newNetworkService(t)
	if _, err := network.Update(netfilter.EndpointUI, []string{"10.0.0.0/8"}, services.NetworkUpdate{ClientAddr: netip.MustParseAddr("10.1.1.1")}, "admin"); err != nil {
		t.Fatal(err)
	}
	if network.Allows(netfilter.EndpointUI, netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("die Liste gilt nicht")
	}

	code, out, errOut := runNetwork(dataDir, "reset", "ui")
	if code != 0 || !strings.Contains(out, "Netzbereiche für Weboberfläche zurückgesetzt") {
		t.Errorf("Exit %d, Ausgabe %q, Fehler %q", code, out, errOut)
	}
	if !network.Allows(netfilter.EndpointUI, netip.MustParseAddr("8.8.8.8")) {
		t.Error("reset wirkt nicht sofort")
	}
}

func TestNetworkAufrufform(t *testing.T) {
	_, dataDir := newNetworkService(t)
	for _, args := range [][]string{nil, {"reset"}, {"reset", "web"}, {"reset", "ui", "agent"}, {"status", "ui"}, {"unsinn"}} {
		if code, _, errOut := runNetwork(dataDir, args...); code != 2 || !strings.Contains(errOut, "verwendung") {
			t.Errorf("%v: Exit %d, %q", args, code, errOut)
		}
	}
}

// Ein Aufruf der Buchse mit unbekanntem Endpunkt (ohne CLI davor) wird vom
// Dienst abgelehnt.
func TestNetworkResetUnbekannterEndpunktAmDienst(t *testing.T) {
	_, dataDir := newNetworkService(t)
	err := control.CallWithArg(control.SocketPath(dataDir), cmdNetworkReset, "web", nil)
	var remote control.RemoteError
	if !errors.As(err, &remote) || !strings.Contains(string(remote), "unbekannter endpunkt") {
		t.Errorf("Fehler = %v", err)
	}
}

func TestNetworkCLIOhneDienst(t *testing.T) {
	code, _, errOut := runNetwork(shortTempDir(t), "status")
	if code != 1 || !strings.Contains(errOut, "mit sudo aufrufen") {
		t.Errorf("Exit %d, Fehler %q", code, errOut)
	}
}
