package notify

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// fakeSMTP ist ein minimaler SMTP-Server auf 127.0.0.1 für genau eine Sitzung.
// Er hält fest, ob die Sitzung verschlüsselt war, welche Zugangsdaten
// ankamen (\x00user\x00passwort) und was im MAIL FROM stand.
type fakeSMTP struct {
	port     int
	offerTLS bool // bietet STARTTLS an
	implicit bool // spricht TLS ab dem ersten Byte
	creds    chan string
	from     chan string
	encrypt  chan bool
}

// testCert erzeugt ein selbst signiertes Zertifikat für 127.0.0.1 und stellt
// den Versandkern darauf ein, es zu akzeptieren.
func testCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake-smtp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	orig := tlsConfigFor
	tlsConfigFor = func(host string) *tls.Config {
		return &tls.Config{ServerName: host, RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	t.Cleanup(func() { tlsConfigFor = orig })
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func startFakeSMTP(t *testing.T, offerTLS, implicit bool) *fakeSMTP {
	t.Helper()
	f := &fakeSMTP{
		offerTLS: offerTLS, implicit: implicit,
		creds: make(chan string, 1), from: make(chan string, 1), encrypt: make(chan bool, 1),
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f.port = ln.Addr().(*net.TCPAddr).Port
	cert := testCert(t)
	go f.serve(ln, &tls.Config{Certificates: []tls.Certificate{cert}})
	return f
}

func (f *fakeSMTP) serve(ln net.Listener, cfg *tls.Config) {
	raw, err := ln.Accept()
	if err != nil {
		return
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	var conn net.Conn = raw
	encrypted := false
	if f.implicit {
		conn, encrypted = tls.Server(raw, cfg), true
	}
	r := bufio.NewReader(conn)
	reply := func(s string) { fmt.Fprint(conn, s+"\r\n") }
	reply("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			caps := "250-fake\r\n"
			if f.offerTLS && !encrypted {
				caps += "250-STARTTLS\r\n"
			}
			fmt.Fprint(conn, caps+"250 AUTH PLAIN\r\n")
		case cmd == "STARTTLS":
			reply("220 los")
			conn, encrypted = tls.Server(raw, cfg), true
			r = bufio.NewReader(conn)
		case strings.HasPrefix(cmd, "AUTH PLAIN "):
			decoded, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(cmd, "AUTH PLAIN "))
			f.creds <- string(decoded)
			reply("235 angemeldet")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			f.from <- strings.TrimPrefix(cmd, "MAIL FROM:")
			reply("250 ok")
		case cmd == "DATA":
			reply("354 weiter")
			skipData(r)
			reply("250 angenommen")
		case cmd == "QUIT":
			f.encrypt <- encrypted
			reply("221 tschüss")
			return
		default:
			reply("250 ok")
		}
	}
}

// skipData liest den Nachrichtenkörper bis zum Punkt auf einer eigenen Zeile.
func skipData(r *bufio.Reader) {
	for {
		line, err := r.ReadString('\n')
		if err != nil || line == ".\r\n" {
			return
		}
	}
}

func fakeProvider(port int, security string) *EmailProvider {
	return NewEmailProvider(EmailConfig{
		Host: "127.0.0.1", Port: port, Username: "lcm", From: "LCM <lcm@example.com>", Security: security,
	}, "geheim")
}

func sendTestMail(p *EmailProvider) error {
	return p.SendRaw("Betreff", "Rumpf", []string{"ops@example.com"})
}

// tls: Die Sitzung ist ab dem ersten Byte verschlüsselt, Anmeldung und
// Absender kommen an.
func TestSendMailImpliziteTLS(t *testing.T) {
	f := startFakeSMTP(t, false, true)
	if err := sendTestMail(fakeProvider(f.port, domain.MailSecurityTLS)); err != nil {
		t.Fatalf("Versand mit implizitem TLS: %v", err)
	}
	if !<-f.encrypt {
		t.Error("Sitzung war nicht verschlüsselt")
	}
	if got := <-f.creds; got != "\x00lcm\x00geheim" {
		t.Errorf("Zugangsdaten = %q", got)
	}
	if got := <-f.from; !strings.Contains(got, "<lcm@example.com>") || strings.Contains(got, "LCM") {
		t.Errorf("MAIL FROM = %q, erwartet nur die Adresse", got)
	}
}

// tls gegen einen Server ohne TLS scheitert, statt Klartext zu senden.
func TestSendMailTLSGegenKlartextServerScheitert(t *testing.T) {
	f := startFakeSMTP(t, false, false)
	if err := sendTestMail(fakeProvider(f.port, domain.MailSecurityTLS)); err == nil {
		t.Fatal("erwartete Fehler beim TLS-Aufbau gegen einen Klartext-Server")
	}
	select {
	case got := <-f.creds:
		t.Errorf("Passwort ging ohne TLS raus: %q", got)
	default:
	}
}

// starttls: Wird STARTTLS angeboten, läuft die Anmeldung verschlüsselt.
func TestSendMailSTARTTLS(t *testing.T) {
	f := startFakeSMTP(t, true, false)
	if err := sendTestMail(fakeProvider(f.port, domain.MailSecurityStartTLS)); err != nil {
		t.Fatalf("Versand mit STARTTLS: %v", err)
	}
	if !<-f.encrypt {
		t.Error("Sitzung war nach STARTTLS nicht verschlüsselt")
	}
	if got := <-f.creds; got != "\x00lcm\x00geheim" {
		t.Errorf("Zugangsdaten = %q", got)
	}
}

// starttls ohne angebotenes STARTTLS bricht ab - auch ohne Anmeldung, nie
// stiller Klartext.
func TestSendMailSTARTTLSOhneAngebotBrichtAb(t *testing.T) {
	for _, username := range []string{"lcm", ""} {
		f := startFakeSMTP(t, false, false)
		p := fakeProvider(f.port, domain.MailSecurityStartTLS)
		p.cfg.Username = username
		if err := sendTestMail(p); !errors.Is(err, errKeinSTARTTLS) {
			t.Fatalf("Benutzer %q: erwartetes errKeinSTARTTLS, bekam %v", username, err)
		}
		select {
		case got := <-f.creds:
			t.Errorf("Passwort ging ohne STARTTLS raus: %q", got)
		case got := <-f.from:
			t.Errorf("Mail ging ohne STARTTLS raus: %q", got)
		default:
		}
	}
}

// none: unverschlüsselt, die Anmeldung geht im Klartext - der Nutzer hat es
// ausdrücklich so gewählt. Auch ein angebotenes STARTTLS wird nicht benutzt.
func TestSendMailOhneVerschluesselung(t *testing.T) {
	f := startFakeSMTP(t, true, false)
	if err := sendTestMail(fakeProvider(f.port, domain.MailSecurityNone)); err != nil {
		t.Fatalf("Versand ohne Verschlüsselung: %v", err)
	}
	if <-f.encrypt {
		t.Error("Sitzung hätte unverschlüsselt bleiben müssen")
	}
	if got := <-f.creds; got != "\x00lcm\x00geheim" {
		t.Errorf("Zugangsdaten = %q", got)
	}
}
