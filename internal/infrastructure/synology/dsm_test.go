package synology

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseUptime: DSM liefert die Laufzeit als "Stunden:Minuten:Sekunden".
func TestParseUptime(t *testing.T) {
	if got := parseUptime("48:52:9"); got != 48*3600+52*60+9 {
		t.Errorf("parseUptime = %d", got)
	}
	// Unerwartete Formen dürfen keine erfundene Zahl ergeben.
	for _, bad := range []string{"", "kaputt", "1:2", "a:b:c"} {
		if got := parseUptime(bad); got != 0 {
			t.Errorf("parseUptime(%q) = %d, erwartet 0", bad, got)
		}
	}
}

// TestNormalizeFingerprint: der Pin muss unabhängig von Schreibweise und
// Doppelpunkten vergleichbar sein - sonst schlüge das Pinning fehl, obwohl
// das Zertifikat stimmt.
func TestNormalizeFingerprint(t *testing.T) {
	a := normalizeFingerprint("FF:0D:8D:22")
	b := normalizeFingerprint("ff0d8d22")
	if a != b || a != "ff0d8d22" {
		t.Errorf("normalizeFingerprint uneinheitlich: %q vs %q", a, b)
	}
}

// TestCollectLiestDenZustand prüft den Ablauf gegen eine nachgebaute
// DSM-API - mit den ECHTEN Antwortformen des Testgeräts (Größen als
// Zeichenketten, CPU-Kerne als Zeichenkette, Security-Advisor je Kategorie).
func TestCollectLiestDenZustand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api := r.URL.Query().Get("api")
		w.Header().Set("Content-Type", "application/json")
		switch api {
		case "SYNO.API.Auth":
			_, _ = w.Write([]byte(`{"success":true,"data":{"sid":"SID123"}}`))
		case "SYNO.Core.System":
			_, _ = w.Write([]byte(`{"success":true,"data":{"model":"VirtualDSM","firmware_ver":"DSM 7.3.2-86009",
				"serial":"V44G7CKO1S97X","cpu_cores":"2","ram_size":2048,"up_time":"48:52:9",
				"time_zone":"Amsterdam","enabled_ntp":true,"ntp_server":"time.google.com","time":"2026-08-06 17:40:02"}}`))
		case "SYNO.Core.Upgrade.Server":
			_, _ = w.Write([]byte(`{"success":true,"data":{"available":true,"version":"DSM 7.3.3"}}`))
		case "SYNO.Core.Package":
			_, _ = w.Write([]byte(`{"success":true,"data":{"packages":[{"id":"FileStation","name":"File Station","version":"1.4.3-1609"}]}}`))
		case "SYNO.Storage.CGI.Storage":
			_, _ = w.Write([]byte(`{"success":true,"data":{"volumes":[{"id":"volume_1","status":"normal",
				"size":{"total":"129876824064","used":"76095488"}}]}}`))
		case "SYNO.Core.SecurityScan.Status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":{
				"network":{"category":"network","fail":{"danger":1,"risk":2,"warning":5,"outOfDate":0},"failSeverity":"danger"},
				"malware":{"category":"malware","fail":{"danger":0,"risk":0,"warning":0,"outOfDate":0},"failSeverity":"safe"}}}}`))
		default:
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":103}}`))
		}
	}))
	defer srv.Close()

	c := &Client{base: srv.URL + "/webapi/", http: srv.Client()}
	if err := c.Login("tony", "geheim"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if c.sid != "SID123" {
		t.Fatalf("Sitzungs-ID nicht übernommen: %q", c.sid)
	}
	info, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if info.Model != "VirtualDSM" || info.Version != "DSM 7.3.2-86009" || info.CPUCores != 2 {
		t.Errorf("Systemdaten falsch gelesen: %+v", info)
	}
	if !info.UpdateAvailable || info.LatestVersion != "DSM 7.3.3" {
		t.Errorf("Update-Stand falsch: %+v", info)
	}
	if len(info.Packages) != 1 || info.Packages[0].Name != "File Station" {
		t.Errorf("Paketliste falsch: %+v", info.Packages)
	}
	// Größen kommen als Zeichenketten in Bytes - MB-Umrechnung muss stimmen.
	if info.VolumeTotalMB != 123860 || info.VolumeUsedMB != 72 {
		t.Errorf("Speichergrößen falsch: total=%d used=%d", info.VolumeTotalMB, info.VolumeUsedMB)
	}
	// Nur risk+danger zählen - Warnungen/Hinweise des Advisors sind kein Befund.
	if info.SecurityRisks != 3 || !strings.Contains(info.SecuritySummary, "network") {
		t.Errorf("Security-Advisor falsch gewertet: %d (%q)", info.SecurityRisks, info.SecuritySummary)
	}
	if !info.NTPEnabled || info.NTPServer != "time.google.com" || info.Timezone != "Amsterdam" {
		t.Errorf("Zeit-Zustand falsch: %+v", info)
	}
}

// TestFehlercodeWirdUebersetzt: die 2FA-Ablehnung ist der häufigste Stolper-
// stein beim Einrichten - sie muss als Satz ankommen, nicht als Zahl.
func TestFehlercodeWirdUebersetzt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false, "error": map[string]int{"code": 403},
		})
	}))
	defer srv.Close()
	c := &Client{base: srv.URL + "/webapi/", http: srv.Client()}
	err := c.Login("tony", "falsch")
	if err == nil {
		t.Fatal("fehlerhafte Anmeldung muss scheitern")
	}
	if !strings.Contains(err.Error(), "Zwei-Faktor") {
		t.Errorf("Fehlercode nicht übersetzt: %v", err)
	}
}
