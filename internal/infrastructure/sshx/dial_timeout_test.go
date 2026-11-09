package sshx

import (
	"net"
	"strconv"
	"testing"
	"time"
)

// Ein Server, der nach dem Banner schweigt, darf die Verbindung nicht ewig
// aufhalten: Genau so sah es aus, als der Kernel eines Hosts hing.
func TestHaengenderHandshakeLaeuftInDieFrist(t *testing.T) {
	old := HandshakeTimeout
	HandshakeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { HandshakeTimeout = old })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			_, _ = conn.Write([]byte("SSH-2.0-OpenSSH_9.6\r\n"))
		}
	}()

	host, portText, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portText)
	done := make(chan error, 1)
	go func() {
		_, err := NewClient().DialPassword(host, port, "root", "x", "SHA256:x")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Verbindung zu einem stummen Server gelang")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("der Handshake hing über die Frist hinaus")
	}
}
