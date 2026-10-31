//go:build smtpfake

// Opt-in-Test für den SMTP-Versand ohne Verschlüsselung. Läuft nicht in
// `go test ./...` (also auch nicht in der CI), sondern nur bei:
//
//	go test -tags smtpfake ./internal/infrastructure/notify/

package notify

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// fakeSMTP lauscht auf 127.0.0.1 und bietet AUTH PLAIN an, aber kein STARTTLS
// - wie ein Relay auf Port 25 ohne Verschlüsselung. Die Zugangsdaten landen
// im Kanal creds (Format: \x00user\x00passwort).
func fakeSMTP(t *testing.T) (port int, creds <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan string, 1)
	go serveFakeSMTP(ln, got)
	return ln.Addr().(*net.TCPAddr).Port, got
}

func serveFakeSMTP(ln net.Listener, creds chan<- string) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
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
			fmt.Fprint(conn, "250-fake\r\n250 AUTH PLAIN\r\n")
		case strings.HasPrefix(cmd, "AUTH PLAIN "):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(cmd, "AUTH PLAIN "))
			creds <- string(raw)
			reply("235 angemeldet")
		case cmd == "DATA":
			reply("354 weiter")
			skipData(r)
			reply("250 angenommen")
		case cmd == "QUIT":
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

// Ohne STARTTLS-Haken geht das Passwort im Klartext an den Server - der
// Nutzer hat die Verschlüsselung ausdrücklich abgewählt.
func TestEmailKlartextAnmeldungOhneTLS(t *testing.T) {
	port, creds := fakeSMTP(t)
	p := NewEmailProvider(EmailConfig{
		Host: "127.0.0.1", Port: port, Username: "lcm", From: "lcm@example.com", UseTLS: false,
	}, "geheim")

	if err := p.SendRaw("Betreff", "Rumpf", []string{"ops@example.com"}); err != nil {
		t.Fatalf("SendRaw ohne TLS: %v", err)
	}
	if got := <-creds; got != "\x00lcm\x00geheim" {
		t.Errorf("Zugangsdaten beim Server = %q", got)
	}
}

// Mit aktivem STARTTLS-Haken und einem Server ohne STARTTLS wird nichts im
// Klartext angemeldet, sondern mit einer verständlichen Meldung abgebrochen.
func TestEmailMitTLSOhneSTARTTLSBrichtAb(t *testing.T) {
	port, creds := fakeSMTP(t)
	p := NewEmailProvider(EmailConfig{
		Host: "127.0.0.1", Port: port, Username: "lcm", From: "lcm@example.com", UseTLS: true,
	}, "geheim")

	err := p.SendRaw("Betreff", "Rumpf", []string{"ops@example.com"})
	if !errors.Is(err, errKeinSTARTTLS) {
		t.Fatalf("erwartetes errKeinSTARTTLS, bekam %v", err)
	}
	select {
	case got := <-creds:
		t.Errorf("Passwort wurde trotz fehlendem STARTTLS gesendet: %q", got)
	default:
	}
}
