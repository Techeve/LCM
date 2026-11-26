package agent

import (
	"crypto/x509"
	"strings"
	"testing"
	"time"
)

// Tests zur Sicherheitsdurchsicht G-1 (doc/sicherheit-agent-2026-11.md): Sie
// halten fest, was die Prüfung des Server-Zertifikats heute tut, damit eine
// spätere Änderung bewusst geschieht. Sie ändern nichts am Verhalten.

// Ohne jeden Pin gilt nur die reguläre Prüfung gegen die Systemroots: Ein
// selbstsigniertes Zertifikat kommt nicht durch, auch nicht mit passendem Namen.
func TestLerePinsNehmenKeinSelbstsigniertesZertifikat(t *testing.T) {
	cert := selfSigned(t, newKey(t))
	if learned, err := checkPeer([][]byte{cert}, pins{}, "lcm.example"); err == nil || learned.any() {
		t.Errorf("ohne Pins angenommen: gelernt %+v, err %v", learned, err)
	}
	if learned, err := checkPeer([][]byte{cert}, pins{}, ""); err == nil || learned.any() {
		t.Errorf("ohne Pins und Namen angenommen: gelernt %+v, err %v", learned, err)
	}
}

// S-04: Ohne Namen in der Server-Adresse gibt es keinen Rückfall auf die
// Systemroots (x509 überspränge die Namensprüfung). Er scheitert schon vor der
// x509-Prüfung, mit und ohne Pins; ein passender Pin gilt weiter.
func TestLeererHostnameVerweigertSystemrootRueckfall(t *testing.T) {
	cert := selfSigned(t, newKey(t))
	for name, p := range map[string]pins{"ohne Pins": {}, "mit fremdem Pin": {cert: "00"}} {
		_, err := checkPeer([][]byte{cert}, p, "")
		if err == nil || !strings.Contains(err.Error(), "ohne namen") {
			t.Errorf("%s: Fehler %v, erwartet Verweigerung wegen fehlendem Namen", name, err)
		}
	}
	if _, err := checkPeer([][]byte{cert}, pins{cert: fingerprintOf(cert)}, ""); err != nil {
		t.Errorf("passender Pin ohne Hostnamen abgelehnt: %v", err)
	}
}

// Befund G-1.2: Ein Zertifikats-Pin prüft nur den Fingerprint, nicht die
// Laufzeit. Ein abgelaufenes Zertifikat, dessen Pin passt, gilt weiter. Das ist
// gewollt (Agenten ohne Uhr, ein vergessener Termin legt nicht alle Server
// still), und der Angreifer braucht ohnehin den privaten Schlüssel; der Test
// hält es fest.
func TestAbgelaufenesZertifikatMitPassendemPinWirdAngenommen(t *testing.T) {
	ca := newTestCA(t)
	key := newKey(t)
	expired := ca.leaf(t, key, true, time.Now().AddDate(0, -1, 0))

	if _, err := checkPeer([][]byte{expired}, pins{cert: fingerprintOf(expired)}, "lcm.example"); err != nil {
		t.Errorf("Zertifikats-Pin: abgelaufenes Zertifikat abgelehnt: %v", err)
	}
	if _, err := checkPeer([][]byte{expired}, pins{key: keyPin(t, expired)}, "lcm.example"); err != nil {
		t.Errorf("Schlüssel-Pin: abgelaufenes Zertifikat abgelehnt: %v", err)
	}
	// Über die CA dagegen prüft x509 die Laufzeit des Zertifikats.
	if _, err := checkPeer([][]byte{expired, ca.cert.Raw}, pins{ca: ca.pin()}, "lcm.example"); err == nil {
		t.Error("CA-Pin: abgelaufenes Zertifikat der CA angenommen")
	}
}

// Befund G-1.3: Der Schlüssel-Pin nimmt jedes Zertifikat mit diesem Schlüssel, auch
// eines ohne die Kennung von LCM und mit fremdem Namen.
func TestSchluesselPinNimmtJedesZertifikatMitDemSchluessel(t *testing.T) {
	ca := newTestCA(t)
	key := newKey(t)
	withMarker := ca.leaf(t, key, true, time.Now().AddDate(0, 3, 0))
	other := selfSigned(t, key) // anderer Aussteller, keine Kennung
	if _, err := checkPeer([][]byte{other}, pins{key: keyPin(t, withMarker)}, "ganz-anderer-name.example"); err != nil {
		t.Errorf("Schlüssel-Pin lehnt ein Zertifikat mit dem Schlüssel ab: %v", err)
	}
}

// Das Lernen aus einer Kette ändert den Pin nur nach bestätigtem Zertifikat und
// nie auf das Zertifikat einer fremden CA (siehe pin_test.go); hier die Grenze:
// Ein bestätigtes Zertifikat, dessen Kette kaputt ist, lehrt nichts über die CA.
func TestKaputteKetteLehrtKeineCA(t *testing.T) {
	cert := selfSigned(t, newKey(t))
	learned, err := checkPeer([][]byte{cert, []byte("müll")}, pins{cert: fingerprintOf(cert)}, "lcm.example")
	if err != nil {
		t.Fatal(err)
	}
	if learned.ca != "" || len(learned.customCAs) != 0 {
		t.Errorf("gelernt %+v", learned)
	}
	if parsed, _ := x509.ParseCertificate(cert); learned.key != keyPinOf(parsed) {
		t.Errorf("Schlüssel-Pin %q", learned.key)
	}
}
