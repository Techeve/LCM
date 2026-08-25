package services

import (
	"strings"
	"testing"
)

// TestCrowdSecLapiInstallScript prüft die Schlüsselschritte der LAPI-
// Einrichtung: CrowdSec-Repo/-Paket, LAPI auf 0.0.0.0, Maschinen-Konto,
// Passwort-Marker; Bouncer nur wenn angefordert.
func TestCrowdSecLapiInstallScript(t *testing.T) {
	script := crowdsecLapiInstallScript(false)
	for _, want := range []string{
		"packagecloud.io/install/repositories/crowdsec/crowdsec",
		"apt-get install -y crowdsec",
		"listen_uri: 0.0.0.0:8080",
		"cscli machines add lcm-managed --password",
		`echo "LCM-LAPI-PW: $PW"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("LAPI-skript enthält %q nicht:\n%s", want, script)
		}
	}
	// Ohne Bouncer-Option kein Bouncer-Paket.
	if strings.Contains(script, "crowdsec-firewall-bouncer") {
		t.Errorf("bouncer darf ohne option nicht installiert werden:\n%s", script)
	}
	// Mit Bouncer-Option.
	if b := crowdsecLapiInstallScript(true); !strings.Contains(b, "crowdsec-firewall-bouncer") {
		t.Errorf("bouncer-option nicht umgesetzt:\n%s", b)
	}
}

// TestLapiPwMarker: der Passwort-Marker wird korrekt geparst.
func TestLapiPwMarker(t *testing.T) {
	out := "cscli lapi status\nLCM-LAPI-PW: deadbeef1234\n"
	m := reLapiPwMarker.FindStringSubmatch(out)
	if m == nil || m[1] != "deadbeef1234" {
		t.Fatalf("marker nicht geparst: %v", m)
	}
	// Redaction ersetzt das Passwort.
	red := reLapiPwMarker.ReplaceAllString(out, "LCM-LAPI-PW: ********")
	if strings.Contains(red, "deadbeef1234") {
		t.Errorf("passwort nicht redigiert: %q", red)
	}
}
