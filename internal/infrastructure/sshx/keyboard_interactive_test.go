package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strconv"
	"testing"

	"golang.org/x/crypto/ssh"
)

// startTestSSHServer startet einen minimalen In-Process-SSH-Server für genau
// eine Verbindung (Handshake + Auth, keine Sessions) und liefert Adresse +
// Host-Key-Fingerprint zurück.
func startTestSSHServer(t *testing.T, config *ssh.ServerConfig) (addr, fingerprint string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("host-key erzeugen: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	config.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	// Akzeptiert Verbindungen in einer Schleife (manche Tests dialen mehrfach
	// gegen denselben Server, z.B. für einen "falsches Passwort"-Fehlversuch
	// nach dem erfolgreichen Login) - bis der Listener beim Test-Cleanup
	// geschlossen wird.
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer nc.Close()
				conn, chans, reqs, err := ssh.NewServerConn(nc, config)
				if err != nil {
					return // Auth fehlgeschlagen - der Test prüft das über den Client-Fehler.
				}
				defer conn.Close()
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					_ = ch.Reject(ssh.Prohibited, "test-server: keine sessions")
				}
			}()
		}
	}()

	return ln.Addr().String(), ssh.FingerprintSHA256(signer.PublicKey())
}

// TestDialPasswordKeyboardInteractiveEmptyPassword bildet den Onboarding-
// Sonderfall ab: root ohne gesetztes Passwort. Der sshd/PAM-Server bietet
// dafür NUR "keyboard-interactive" an (PAM-Conversation), keine klassische
// "password"-Methode - und akzeptiert nur eine leere Antwort.
func TestDialPasswordKeyboardInteractiveEmptyPassword(t *testing.T) {
	config := &ssh.ServerConfig{
		KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := client("", "", []string{"Password: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) != 1 || answers[0] != "" {
				return nil, errAuthDenied
			}
			return nil, nil
		},
	}
	addr, fp := startTestSSHServer(t, config)
	host, port := splitHostPort(t, addr)

	client := NewClient()
	conn, err := client.DialPassword(host, port, "root", "", fp)
	if err != nil {
		t.Fatalf("erwartete erfolgreichen login ohne passwort, bekam: %v", err)
	}
	defer conn.Close()
}

// TestDialPasswordClassicStillWorks: die klassische "password"-Methode
// funktioniert unverändert weiter (Regression-Schutz für die um
// keyboard-interactive erweiterte Auth-Liste).
func TestDialPasswordClassicStillWorks(t *testing.T) {
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) != "geheim123" {
				return nil, errAuthDenied
			}
			return nil, nil
		},
	}
	addr, fp := startTestSSHServer(t, config)
	host, port := splitHostPort(t, addr)

	client := NewClient()
	conn, err := client.DialPassword(host, port, "admin", "geheim123", fp)
	if err != nil {
		t.Fatalf("erwartete erfolgreichen klassischen passwort-login, bekam: %v", err)
	}
	defer conn.Close()

	if _, err := client.DialPassword(host, port, "admin", "falsch", fp); err == nil {
		t.Error("falsches passwort sollte abgelehnt werden")
	}
}

// TestDialPasswordKeyboardInteractiveWrongAnswerRejected stellt sicher, dass
// die neue Methode kein Passwort erzwingt - ein Server, der eine NICHT-leere
// Antwort verlangt, weist einen leeren Login weiterhin ab.
func TestDialPasswordKeyboardInteractiveWrongAnswerRejected(t *testing.T) {
	config := &ssh.ServerConfig{
		KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := client("", "", []string{"Password: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) != 1 || answers[0] != "korrektes-passwort" {
				return nil, errAuthDenied
			}
			return nil, nil
		},
	}
	addr, fp := startTestSSHServer(t, config)
	host, port := splitHostPort(t, addr)

	client := NewClient()
	if _, err := client.DialPassword(host, port, "root", "", fp); err == nil {
		t.Error("leeres passwort sollte hier abgelehnt werden (server verlangt echtes passwort)")
	}
}

var errAuthDenied = &sshAuthError{"zugriff verweigert"}

type sshAuthError struct{ msg string }

func (e *sshAuthError) Error() string { return e.msg }

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("addr splitten: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port parsen: %v", err)
	}
	return host, port
}
