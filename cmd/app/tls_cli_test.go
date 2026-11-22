package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/control"
	"LCM/internal/infrastructure/tlsx"
)

var (
	testNotAfter = time.Date(2027, 2, 12, 10, 0, 0, 0, time.UTC)
	testFinger   = "3f9a1c7e5b02d4a8c6e1f0b9a7d5c3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6"
)

func testStatus() statusReport {
	own := tlsx.CertStatus{ID: "config-0", Source: tlsx.SourceConfig, Names: []string{"lcm.example.org", "192.0.2.7"},
		Issuer: "R11", NotAfter: testNotAfter, Fingerprint: testFinger,
		Warnings: []string{"kette unvollständig"}}
	old := tlsx.CertStatus{ID: "upload-0a1b2c3d", Source: tlsx.SourceUpload, Names: []string{"alt.example.org"},
		Issuer: "R10", NotAfter: testNotAfter.AddDate(-1, 0, 0), Expired: true, Fingerprint: testFinger}
	fallback := tlsx.CertStatus{ID: "lcm-ca", Source: tlsx.SourceLCMCA, Names: []string{"localhost"},
		Issuer: "LCM Root CA", NotAfter: testNotAfter, Fingerprint: testFinger}
	return statusReport{
		Certificates: []tlsx.CertStatus{own, old, fallback},
		Endpoints: []endpointStatus{
			{Name: "ui", URL: "https://lcm.example.org:9310", Host: "lcm.example.org", Cert: &own},
			{Name: "agent"},
			{Name: "mcp", URL: "https://mcp.example.org", Host: "mcp.example.org", Cert: &fallback},
		},
	}
}

func TestAusgabeStatus(t *testing.T) {
	var out bytes.Buffer
	printStatusReport(&out, testStatus())
	text := out.String()

	for _, want := range []string{
		"config-0 - config.json",
		"Namen:        lcm.example.org, 192.0.2.7",
		"Aussteller:   R11",
		"Gültig bis:   2027-02-12\n",
		"Fingerprint:  3f9a1c7e5b02d4a8…",
		"Warnung:      kette unvollständig",
		"upload-0a1b2c3d - Upload",
		"Gültig bis:   2026-02-12 (abgelaufen)",
		"lcm-ca - LCM-Zertifikat (Rückfall)",
		"Weboberfläche  https://lcm.example.org:9310",
		"für lcm.example.org wird config-0 (config.json) ausgeliefert",
		"Agent          keine öffentliche Adresse eingetragen - ausgeliefert wird das LCM-Zertifikat",
		"für mcp.example.org passt kein eigenes Zertifikat",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Ausgabe enthält nicht %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, testFinger) {
		t.Error("der Fingerprint ist nicht gekürzt")
	}
}

func TestAusgabeNeuladen(t *testing.T) {
	own := testStatus().Certificates[0]
	report := reloadReport{
		Certificates: []certResult{
			{ID: "config-0", Cert: &own},
			{ID: "config-1", Error: "zertifikat abgelaufen (seit 2026-01-01)", Kept: true},
			{ID: "config-2", Error: "zertifikat lesen: datei fehlt"},
		},
		Problems: []string{"config.json parsen: kaputt - die bisherigen Einträge aus der config.json bleiben"},
	}
	var out bytes.Buffer
	printReloadReport(&out, report)
	text := out.String()

	for _, want := range []string{
		"1 geladen, 2 abgelehnt",
		"geladen    config-0  lcm.example.org, 192.0.2.7  gültig bis 2027-02-12",
		"Warnung: kette unvollständig",
		"abgelehnt  config-1  zertifikat abgelaufen (seit 2026-01-01) - bisheriges bleibt aktiv",
		"abgelehnt  config-2  zertifikat lesen: datei fehlt - es gibt kein bisheriges Zertifikat",
		"Problem: config.json parsen",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Ausgabe enthält nicht %q:\n%s", want, text)
		}
	}

	out.Reset()
	printReloadReport(&out, reloadReport{})
	if !strings.Contains(out.String(), "Keine eigenen Zertifikate") {
		t.Errorf("leerer Bericht: %s", out.String())
	}
}

func TestCLIAufrufe(t *testing.T) {
	a := newTestCert(t, time.Now().Add(30*24*time.Hour))
	f := newReloadFixture(t, a)
	server, err := control.Listen(control.SocketPath(f.dataDir), controlCommands(f.reloader))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := runTLSCommand(f.dataDir, args, &out, &errOut)
		return code, out.String(), errOut.String()
	}

	if code, out, _ := run("reload"); code != 0 || !strings.Contains(out, "1 geladen, 0 abgelehnt") {
		t.Errorf("reload: Exit %d, Ausgabe %q", code, out)
	}
	if code, out, _ := run("status"); code != 0 || !strings.Contains(out, "config-0 - config.json") {
		t.Errorf("status: Exit %d, Ausgabe %q", code, out)
	}
	if code, _, errOut := run("unsinn"); code != 2 || !strings.Contains(errOut, "verwendung") {
		t.Errorf("unbekannter Unterbefehl: Exit %d, %q", code, errOut)
	}

	// Ein abgelehntes Zertifikat macht den Exit-Code von reload zu 1.
	if err := os.WriteFile(a.cert, []byte("kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run("reload"); code != 1 || !strings.Contains(out, "bisheriges bleibt aktiv") {
		t.Errorf("reload mit Ablehnung: Exit %d, Ausgabe %q", code, out)
	}
}

func TestCLIOhneDienst(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runTLSCommand(shortTempDir(t), []string{"status"}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "mit sudo aufrufen") {
		t.Errorf("Exit %d, Fehler %q", code, errOut.String())
	}
}

func TestSteuerbuchsePfad(t *testing.T) {
	packaged := t.TempDir()
	t.Setenv("LCM_DATA", "")

	// -data gewinnt vor LCM_DATA.
	t.Setenv("LCM_DATA", "/srv/env")
	if got, _ := controlSocketPath("/srv/flag", packaged); got != "/srv/flag/"+control.SocketName {
		t.Errorf("-data: %q", got)
	}
	if got, _ := controlSocketPath("", packaged); got != "/srv/env/"+control.SocketName {
		t.Errorf("LCM_DATA: %q", got)
	}

	// Ohne beides: die Buchse der Paket-Installation, wenn es sie gibt.
	t.Setenv("LCM_DATA", "")
	exeSocket, _ := controlSocketPath("", filepath.Join(packaged, "fehlt"))
	if filepath.Base(exeSocket) != control.SocketName || filepath.Dir(exeSocket) == packaged {
		t.Errorf("ohne Paket-Buchse: %q", exeSocket)
	}
	if err := os.WriteFile(filepath.Join(packaged, control.SocketName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := controlSocketPath("", packaged); got != filepath.Join(packaged, control.SocketName) {
		t.Errorf("Paket-Buchse: %q", got)
	}
}

func TestAusgabeStatusGemeinsamerPort(t *testing.T) {
	status := testStatus()
	status.SharedPort = &sharedStatus{Address: ":443", Listening: true, Routes: []sharedRoute{
		{Service: "ui", Host: "lcm.example.org"},
		{Service: "agent", Host: "lcm.example.org", ShadowedBy: "ui"},
		{Service: "mcp", Host: "mcp.example.org", Inactive: true},
	}}
	var out bytes.Buffer
	printStatusReport(&out, status)
	text := out.String()

	for _, want := range []string{
		"Gemeinsamer Port :443 (aktiv)",
		"Weboberfläche  lcm.example.org\n",
		"Agent          lcm.example.org - derselbe Name wie Weboberfläche",
		"MCP            mcp.example.org - ausgeschaltet",
		"Weboberfläche  auch ohne Namen und für jeden anderen Namen",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Ausgabe enthält nicht %q:\n%s", want, text)
		}
	}

	status.SharedPort.Listening = false
	out.Reset()
	printStatusReport(&out, status)
	if !strings.Contains(out.String(), "nicht geöffnet") {
		t.Errorf("ein nicht geöffneter Port steht als aktiv da:\n%s", out.String())
	}
}

func TestAusgabeStatusOhneGemeinsamenPort(t *testing.T) {
	var out bytes.Buffer
	printStatusReport(&out, testStatus())
	if !strings.Contains(out.String(), "Gemeinsamer Port: aus") {
		t.Errorf("Ausgabe nennt den ausgeschalteten gemeinsamen Port nicht:\n%s", out.String())
	}
}
