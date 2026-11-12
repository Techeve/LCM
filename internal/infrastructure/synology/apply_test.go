package synology

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

// dsm73Security ist das Formular, das aus der gelesenen Antwort von
// SYNO.Core.Security.DSM zurückgeht, wenn nichts zu ändern ist.
var dsm73Security = map[string]string{
	"allow_2fa_device_lost_option": "true", "allow_stay_signed_in_option": "true",
	"allow_trust_device_2fa_option": "true", "csp_header_option": "true",
	"enable_csrf_protection": "true", "restart_clean_session": "true",
	"skip_ip_checking": "false", "timeout": "15",
}

// TestDSM73SollwerteSetzen: An Tonys VirtualDSM stimmt alles bis auf den
// DoS-Schutz. Der set-Aufruf geht trotzdem für jede Einstellung raus, mit
// allen gelesenen Feldern, jeder Wert JSON-kodiert, in der Version des
// Lesens.
func TestDSM73SollwerteSetzen(t *testing.T) {
	tests := []struct {
		key, api, version string
		params            map[string]string
	}{
		{"skipIpCheck", "SYNO.Core.Security.DSM", "6", dsm73Security},
		{"csrf", "SYNO.Core.Security.DSM", "6", dsm73Security},
		{"csp", "SYNO.Core.Security.DSM", "6", dsm73Security},
		{"sessionClear", "SYNO.Core.Security.DSM", "6", dsm73Security},
		{"iframe", "SYNO.Core.Security.DSM.Embed", "1", map[string]string{
			"enable_block": "true", "whitelist": `["gofile.me/","find.synology.com/"]`,
		}},
		{"autoBlock", "SYNO.Core.Security.AutoBlock", "1", map[string]string{
			"enable": "true", "attempts": "10", "within_mins": "5", "expire_day": "0", "enable_expire": "false",
		}},
		{"dos", "SYNO.Core.Security.DoS", "2", map[string]string{
			"configs": `[{"adapter":"eth0","dos_protect_enable":true}]`,
		}},
		{"tlsProfile", "SYNO.Core.Web.Security.TLSProfile", "1", map[string]string{
			"default-level": "1",
			"services": `{"ActiveProtectVault":{"current-level":0,"display-name":"ActiveProtect Vault","display-name-i18n":"SYNO.AEV.App.Instance:app:package_name"},` +
				`"dsm":{"current-level":0,"display-name":"DSM Desktop Service","display-name-i18n":"common:web_desktop"},` +
				`"smbftpd":{"current-level":0,"display-name":"FTPS","display-name-i18n":"ftp:ftps"}}`,
		}},
		{"spectre", "SYNO.Core.Hardware.SpectreMeltdown", "1", map[string]string{
			"enable_spectre_meltdown_mitigation": "true",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			c, dev := startFixture(t, "dsm73")
			note, err := c.ApplyTarget(tt.key)
			if err != nil {
				t.Fatalf("ApplyTarget: %v", err)
			}
			if len(dev.sets) != 1 {
				t.Fatalf("erwartet genau einen set-Aufruf, bekam %+v", dev.sets)
			}
			got := dev.sets[0]
			if got.api != tt.api || got.version != tt.version || !maps.Equal(got.params, tt.params) {
				t.Errorf("gesendet %s v%s %v\nerwartet %s v%s %v", got.api, got.version, got.params, tt.api, tt.version, tt.params)
			}
			if !strings.HasPrefix(note, tt.api+" set:") {
				t.Errorf("Notiz nennt den Aufruf nicht: %s", note)
			}
		})
	}
}

// TestSollwertAendertNurZielfelder: Nur das Zielfeld bekommt einen neuen
// Wert, eine zweite Abweichung und alle übrigen Felder gehen zurück, wie sie
// gelesen wurden.
func TestSollwertAendertNurZielfelder(t *testing.T) {
	c, dev := startFixture(t, "dsm73")
	dev.answers = map[string]string{"SYNO.Core.Security.DSM.get": `{"success":true,"data":` +
		`{"skip_ip_checking":true,"enable_csrf_protection":false,"csp_header_option":true,"timeout":30}}`}
	note, err := c.ApplyTarget("skipIpCheck")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"skip_ip_checking": "false", "enable_csrf_protection": "false", "csp_header_option": "true", "timeout": "30"}
	if len(dev.sets) != 1 || !maps.Equal(dev.sets[0].params, want) {
		t.Fatalf("gesendet %+v, erwartet %v", dev.sets, want)
	}
	if !strings.Contains(note, "skip_ip_checking true → false;") {
		t.Errorf("Notiz ohne vorher/nachher: %s", note)
	}
}

// TestAutoBlockGrenzwerte: höchstens 10 Versuche (0 zählt als zu viele),
// mindestens 5 Minuten; strengere Werte bleiben stehen.
func TestAutoBlockGrenzwerte(t *testing.T) {
	tests := []struct {
		read                string
		wantTries, wantMins string
	}{
		{`"enable":false,"attempts":20,"within_mins":2`, "10", "5"},
		{`"enable":true,"attempts":0,"within_mins":5`, "10", "5"},
		{`"enable":true,"attempts":3,"within_mins":30`, "3", "30"},
	}
	for _, tt := range tests {
		c, dev := startFixture(t, "dsm73")
		dev.answers = map[string]string{"SYNO.Core.Security.AutoBlock.get": `{"success":true,"data":{` + tt.read + `,"expire_day":7}}`}
		if _, err := c.ApplyTarget("autoBlock"); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"enable": "true", "attempts": tt.wantTries, "within_mins": tt.wantMins, "expire_day": "7"}
		if !maps.Equal(dev.sets[0].params, want) {
			t.Errorf("%s: gesendet %v, erwartet %v", tt.read, dev.sets[0].params, want)
		}
	}
}

// TestDoSJeSchnittstelle: Jede gelesene Schnittstelle geht eingeschaltet
// zurück, mit allen ihren Feldern.
func TestDoSJeSchnittstelle(t *testing.T) {
	c, dev := startFixture(t, "dsm73")
	dev.answers = map[string]string{"SYNO.Core.Security.DoS.get": `{"success":true,"data":[` +
		`{"adapter":"eth0","dos_protect_enable":false},{"adapter":"ovs_eth1","dos_protect_enable":true,"level":2}]}`}
	if _, err := c.ApplyTarget("dos"); err != nil {
		t.Fatal(err)
	}
	want := `[{"adapter":"eth0","dos_protect_enable":true},{"adapter":"ovs_eth1","dos_protect_enable":true,"level":2}]`
	if got := dev.sets[0].params["configs"]; got != want {
		t.Errorf("configs = %s, erwartet %s", got, want)
	}
}

// TestSpectreNeustart: Die Notiz sagt immer, dass der Schutz erst nach einem
// Neustart wirkt, und gibt weiter, wenn DSM selbst einen verlangt.
func TestSpectreNeustart(t *testing.T) {
	c, dev := startFixture(t, "dsm73")
	dev.answers = map[string]string{"SYNO.Core.Hardware.SpectreMeltdown.set": `{"success":true,"data":{"need_reboot":true}}`}
	note, err := c.ApplyTarget("spectre")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "nach einem Neustart") || !strings.Contains(note, "DSM meldet: Neustart nötig") {
		t.Errorf("Neustart nicht vermerkt: %s", note)
	}
}

// TestNichtsGeratenesGesendet: Firewall und unbekannte Schlüssel lassen
// sich nicht setzen, ein fehlendes Zielfeld wird nicht erfunden. In allen
// Fällen geht kein set-Aufruf raus.
func TestNichtsGeratenesGesendet(t *testing.T) {
	c, dev := startFixture(t, "dsm73")
	for _, key := range []string{"firewall", "unbekannt"} {
		if HasTarget(key) {
			t.Errorf("%s gilt als setzbar", key)
		}
		if _, err := c.ApplyTarget(key); err == nil {
			t.Errorf("%s: Fehler erwartet", key)
		}
	}
	dev.answers = map[string]string{"SYNO.Core.Security.DSM.get": `{"success":true,"data":{"timeout":15}}`}
	if _, err := c.ApplyTarget("csp"); !errors.Is(err, errFieldMissing) {
		t.Errorf("fehlendes Feld: erwartet errFieldMissing, bekam %v", err)
	}
	dev.answers["SYNO.Core.Security.DSM.get"] = `{"success":true,"data":{"csp_header_option":false,"version":2}}`
	if _, err := c.ApplyTarget("csp"); err == nil || !strings.Contains(err.Error(), "kollidiert") {
		t.Errorf("Feld version darf den Aufruf nicht überschreiben: %v", err)
	}
	if len(dev.sets) != 0 {
		t.Errorf("es wurde gesendet: %+v", dev.sets)
	}
}
