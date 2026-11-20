package synology

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testCert erzeugt ein selbstsigniertes Zertifikat samt Schlüssel.
func testCert(t *testing.T, name string) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

// dsmServer startet eine nachgebaute DSM-API, die die übergebene Kette
// vorlegt, und liefert einen Client mit dem Pin dorthin. Der Schlüssel
// gehört zum ersten Zertifikat - wie bei einem Angreifer, der hinter sein
// eigenes das echte DSM-Zertifikat hängt.
func dsmServer(t *testing.T, chain [][]byte, key *ecdsa.PrivateKey, pin []byte) *Client {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"sid":"SID123"}}`))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: chain, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().(*net.TCPAddr)
	return New(Config{Host: addr.IP.String(), Port: addr.Port, CertFingerprint: fingerprintOf(pin)})
}

// TestPinGiltNurFuerDasServerzertifikat: Ein Angreifer im Netz legt sein
// eigenes Zertifikat vor und hängt das echte DSM-Zertifikat hinten an. Das
// darf nicht reichen - der Pin zählt nur am ersten Zertifikat der Kette.
func TestPinGiltNurFuerDasServerzertifikat(t *testing.T) {
	echt, _ := testCert(t, "dsm")
	fremd, fremdKey := testCert(t, "angreifer")
	c := dsmServer(t, [][]byte{fremd, echt}, fremdKey, echt)

	err := c.Login("tony", "geheim")
	if !errors.Is(err, ErrCertMismatch) {
		t.Fatalf("angehängtes echtes Zertifikat wurde akzeptiert: err=%v", err)
	}
}

// TestPinAmServerzertifikatWirdAngenommen: der Normalfall - das gepinnte
// Zertifikat steht vorn.
func TestPinAmServerzertifikatWirdAngenommen(t *testing.T) {
	echt, key := testCert(t, "dsm")
	c := dsmServer(t, [][]byte{echt}, key, echt)

	if err := c.Login("tony", "geheim"); err != nil {
		t.Fatalf("Login mit passendem Pin: %v", err)
	}
}

// TestZugangsdatenNichtInDerURL: Passwort und Session-ID stehen im Rumpf
// eines POST, nicht in der Adresse - die landet in Proxy- und
// Fehlerprotokollen.
func TestZugangsdatenNichtInDerURL(t *testing.T) {
	var methods, queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		queries = append(queries, r.URL.RawQuery)
		if r.PostFormValue("passwd") == "geheim" || r.PostFormValue("_sid") == "SID123" {
			_, _ = w.Write([]byte(`{"success":true,"data":{"sid":"SID123"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":400}}`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL + "/webapi/", http: srv.Client()}
	if err := c.Login("tony", "geheim"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := c.Collect(); err != nil {
		t.Fatalf("Collect mit Session-ID im Rumpf: %v", err)
	}
	for i, m := range methods {
		if m != http.MethodPost {
			t.Errorf("Aufruf %d mit %s statt POST", i, m)
		}
		if strings.Contains(queries[i], "geheim") || strings.Contains(queries[i], "SID123") {
			t.Errorf("Aufruf %d trägt Zugangsdaten in der URL: %q", i, queries[i])
		}
	}
}

// TestNetzfehlerVerraetKeineZugangsdaten: Scheitert die Verbindung, darf
// der Fehlertext weder Passwort noch Session-ID noch die Adresse enthalten -
// er landet im Job-Protokoll und im Log.
func TestNetzfehlerVerraetKeineZugangsdaten(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // ab hier antwortet niemand mehr

	c := New(Config{Host: "127.0.0.1", Port: port})
	err = c.Login("tony", "Kennwort-4711")
	if err == nil {
		t.Fatal("Login ohne Gegenstelle muss scheitern")
	}
	if strings.Contains(err.Error(), "Kennwort-4711") || strings.Contains(err.Error(), "entry.cgi") {
		t.Errorf("Fehlertext verrät Passwort oder Adresse: %v", err)
	}

	c.sid = "SID-geheim"
	_, err = c.Collect()
	if err == nil || strings.Contains(err.Error(), "SID-geheim") || strings.Contains(err.Error(), "entry.cgi") {
		t.Errorf("Fehlertext verrät Session-ID oder Adresse: %v", err)
	}
}
