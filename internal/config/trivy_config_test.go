package config

import (
	"strings"
	"testing"
)

// TestSidecarOhneTokenFaelltBeimStartAuf haelt den Fall fest, der sonst still
// bliebe: Ist trivy_url gesetzt, aber kein Token, laeuft LCM anstandslos
// weiter - und bekommt vom Sidecar auf JEDE Anfrage eine 401. Der CVE-Scan
// waere damit tot, ohne dass beim Start irgendetwas auffaellt. In der
// Oberflaeche saehe das aus wie „keine Funde".
func TestSidecarOhneTokenFaelltBeimStartAuf(t *testing.T) {
	cfg := &Config{TrivyURL: "http://trivy:9330"}
	err := cfg.ValidateTrivy()
	if err == nil {
		t.Fatal("eine Sidecar-Adresse ohne Token muss den Start abbrechen")
	}
	// Die Meldung muss sagen, WAS zu tun ist - nicht nur, dass etwas fehlt.
	if !strings.Contains(err.Error(), "LCM_TRIVY_TOKEN") {
		t.Errorf("die Meldung soll den Weg nennen: %v", err)
	}
}

func TestSidecarAdresseWirdGeprueft(t *testing.T) {
	for _, url := range []string{"trivy:9330", "//trivy", "ftp://trivy"} {
		cfg := &Config{TrivyURL: url, TrivyToken: "t"}
		if err := cfg.ValidateTrivy(); err == nil {
			t.Errorf("%q sollte abgelehnt werden", url)
		}
	}
	for _, url := range []string{"http://trivy:9330", "https://trivy.intern"} {
		cfg := &Config{TrivyURL: url, TrivyToken: "t"}
		if err := cfg.ValidateTrivy(); err != nil {
			t.Errorf("%q sollte erlaubt sein, bekam %v", url, err)
		}
	}
}

// TestOhneSidecarBleibtAllesWieBisher: Der Regelfall ist die Installation auf
// einem Host. Dort ist trivy_url leer, und die Pruefung darf nichts fordern.
func TestOhneSidecarBleibtAllesWieBisher(t *testing.T) {
	cfg := &Config{TrivyPath: "trivy"}
	if err := cfg.ValidateTrivy(); err != nil {
		t.Errorf("ohne Sidecar darf nichts gefordert werden, bekam %v", err)
	}
}
