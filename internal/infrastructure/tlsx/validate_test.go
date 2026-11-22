package tlsx

import (
	"crypto/x509"
	"net"
	"strings"
	"testing"
	"time"
)

func TestValidatePairAkzeptiertGueltigesZertifikat(t *testing.T) {
	c := newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}})
	v, err := ValidatePair(c.certPEM, c.keyPEM, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Warnings) != 0 {
		t.Errorf("selbst ausgestelltes Zertifikat mit Warnungen: %v", v.Warnings)
	}
	// Nur ein IP-Name und eine leere EKU-Liste reichen ebenfalls.
	ip := newTestCert(t, leafOpts{ips: []net.IP{net.ParseIP("192.0.2.7")}, eku: []x509.ExtKeyUsage{}})
	if _, err := ValidatePair(ip.certPEM, ip.keyPEM, time.Now()); err != nil {
		t.Errorf("IP-Zertifikat ohne EKU: %v", err)
	}
}

func TestValidatePairLehntAb(t *testing.T) {
	now := time.Now()
	dns := []string{"lcm.example.org"}
	other := newTestCert(t, leafOpts{dns: dns})
	cases := []struct {
		name     string
		cert     testCert
		key      []byte // überschreibt den Schlüssel des Zertifikats
		contains string
	}{
		{"abgelaufen", newTestCert(t, leafOpts{dns: dns, notBefore: now.Add(-48 * time.Hour), notAfter: now.Add(-time.Hour)}), nil, "abgelaufen"},
		{"noch nicht gültig", newTestCert(t, leafOpts{dns: dns, notBefore: now.Add(time.Hour), notAfter: now.Add(2 * time.Hour)}), nil, "noch nicht gültig"},
		{"falscher Schlüssel", newTestCert(t, leafOpts{dns: dns}), other.keyPEM, "zertifikat laden"},
		{"ohne SAN", newTestCert(t, leafOpts{}), nil, "dns- noch einen ip-namen"},
		{"falsche EKU", newTestCert(t, leafOpts{dns: dns, eku: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}), nil, "serverAuth"},
		{"kein PEM", testCert{certPEM: []byte("kaputt"), keyPEM: other.keyPEM}, nil, "zertifikat laden"},
	}
	for _, c := range cases {
		key := c.cert.keyPEM
		if c.key != nil {
			key = c.key
		}
		_, err := ValidatePair(c.cert.certPEM, key, now)
		if err == nil || !strings.Contains(err.Error(), c.contains) {
			t.Errorf("%s: Fehler = %v, erwartet Teil %q", c.name, err, c.contains)
		}
	}
}

func TestValidatePairWarntBeiUnvollstaendigerKette(t *testing.T) {
	ca := newTestCA(t, "Zwischen-CA")
	without := newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}, issuer: ca.cert, issuerKey: ca.key})
	with := newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}, issuer: ca.cert, issuerKey: ca.key, chainInFile: true})

	v, err := ValidatePair(without.certPEM, without.keyPEM, time.Now())
	if err != nil {
		t.Fatalf("eine unvollständige Kette ist kein Fehler: %v", err)
	}
	if len(v.Warnings) != 1 || !strings.Contains(v.Warnings[0], "kette unvollständig") {
		t.Errorf("Warnungen = %v", v.Warnings)
	}
	v, err = ValidatePair(with.certPEM, with.keyPEM, time.Now())
	if err != nil || len(v.Warnings) != 0 {
		t.Errorf("mit Aussteller in der Datei: %v, %v", v.Warnings, err)
	}
}

func TestValidatePairErklaertDieHaeufigstenFehler(t *testing.T) {
	cert := newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}})
	other := newTestCert(t, leafOpts{dns: []string{"lcm.example.org"}})
	cases := []struct {
		name     string
		certPEM  []byte
		keyPEM   []byte
		contains string
	}{
		{"falscher Schlüssel", cert.certPEM, other.keyPEM, "gehört nicht zu diesem Zertifikat"},
		{"vertauscht", cert.keyPEM, cert.certPEM, "vertauscht"},
		{"Zertifikat ohne PEM", []byte("kaputt"), cert.keyPEM, "Zertifikatsdatei"},
		{"Schlüssel ohne PEM", cert.certPEM, []byte("kaputt"), "Schlüsseldatei"},
	}
	for _, c := range cases {
		_, err := ValidatePair(c.certPEM, c.keyPEM, time.Now())
		if err == nil || !strings.Contains(err.Error(), c.contains) {
			t.Errorf("%s: Fehler = %v, erwartet Teil %q", c.name, err, c.contains)
		}
	}
}
