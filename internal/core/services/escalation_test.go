package services

import (
	"strings"
	"testing"
)

// scriptedConn ist eine minimale sshx.Conn, deren Antworten der Test über
// einen Handler (Kommando + stdin) steuert - für die Rechte-Erkennung, wo
// derselbe Befehl (su root -c true) je nach stdin unterschiedlich ausgeht.
type scriptedConn struct {
	calls   []string
	handler func(cmd, stdin string) (out string, code int)
}

func (c *scriptedConn) Run(cmd string) (string, int, error) { return c.RunStdin(cmd, "") }
func (c *scriptedConn) RunStdin(cmd, stdin string) (string, int, error) {
	c.calls = append(c.calls, cmd)
	out, code := c.handler(cmd, stdin)
	return out, code, nil
}
func (c *scriptedConn) KeyExchange() string { return "" }

func (c *scriptedConn) OnActivity(func()) {}
func (c *scriptedConn) Close() error      { return nil }

// TestDetectRootEscalationRoot: root braucht keine Proben.
func TestDetectRootEscalationRoot(t *testing.T) {
	conn := &scriptedConn{handler: func(string, string) (string, int) { return "", 1 }}
	var log strings.Builder
	esc := detectRootEscalation(conn, "root", "pw", &log)
	if esc == nil || esc.method != "root" {
		t.Fatalf("root: erwartet methode root, bekam %+v", esc)
	}
	if len(conn.calls) != 0 {
		t.Errorf("root sollte ohne Proben auskommen, lief: %v", conn.calls)
	}
	if cmd, stdin := esc.wrap("id -u"); cmd != "id -u" || stdin != "" {
		t.Errorf("root-wrap sollte unverändert lassen: %q / %q", cmd, stdin)
	}
}

// TestDetectRootEscalationPrefersSudo: funktioniert sudo, wird su nie probiert.
func TestDetectRootEscalationPrefersSudo(t *testing.T) {
	conn := &scriptedConn{handler: func(cmd, stdin string) (string, int) {
		if strings.HasPrefix(cmd, "sudo -S -p") && stdin == "pw\n" {
			return "", 0
		}
		return "", 1
	}}
	var log strings.Builder
	esc := detectRootEscalation(conn, "deploy", "pw", &log)
	if esc == nil || esc.method != "sudo" {
		t.Fatalf("erwartet sudo, bekam %+v", esc)
	}
	for _, c := range conn.calls {
		if strings.HasPrefix(c, "su ") {
			t.Errorf("su sollte bei funktionierendem sudo nicht probiert werden: %v", conn.calls)
		}
	}
}

// TestDetectRootEscalationSuWithLoginPassword: kein sudo, aber su akzeptiert
// das Login-Passwort - der klassische Debian-Fall ohne sudo-Paket.
func TestDetectRootEscalationSuWithLoginPassword(t *testing.T) {
	conn := &scriptedConn{handler: func(cmd, stdin string) (string, int) {
		if strings.HasPrefix(cmd, "sudo") {
			return "sudo: command not found", 127
		}
		if strings.HasPrefix(cmd, "su root -c") && stdin == "geheim\n" {
			return "", 0
		}
		return "su: Authentication failure", 1
	}}
	var log strings.Builder
	esc := detectRootEscalation(conn, "tony", "geheim", &log)
	if esc == nil || esc.method != "su" {
		t.Fatalf("erwartet su, bekam %+v", esc)
	}
	cmd, stdin := esc.wrap("id -u")
	if !strings.HasPrefix(cmd, "su root -c ") {
		t.Errorf("wrap sollte per su laufen, bekam %q", cmd)
	}
	if stdin != "geheim\n" {
		t.Errorf("su sollte das Login-Passwort via stdin bekommen, bekam %q", stdin)
	}
	// Passwort darf nie in der Kommandozeile stehen.
	if strings.Contains(cmd, "geheim") {
		t.Error("passwort steht in der kommandozeile (leak)")
	}
}

// TestDetectRootEscalationSuWithoutPassword: su klappt schon mit leerem stdin
// (pam-Vertrauen bzw. leeres root-Passwort) - wird vor dem Passwort-su erkannt.
func TestDetectRootEscalationSuWithoutPassword(t *testing.T) {
	conn := &scriptedConn{handler: func(cmd, stdin string) (string, int) {
		if strings.HasPrefix(cmd, "su root -c") && stdin == "\n" {
			return "", 0
		}
		return "", 1
	}}
	var log strings.Builder
	esc := detectRootEscalation(conn, "tony", "geheim", &log)
	if esc == nil || esc.method != "su" {
		t.Fatalf("erwartet su, bekam %+v", esc)
	}
	if _, stdin := esc.wrap("true"); stdin != "\n" {
		t.Errorf("passwortloses su sollte nur newline einspeisen, bekam %q", stdin)
	}
}

// TestDetectRootEscalationLogin: scheitern sudo UND su (z. B. Gruppen-
// Restriktion), aber login lässt root ohne Passwort herein - das komplette
// Kommando (inkl. der leeren Zeile für den Passwort-Prompt) läuft über stdin,
// niemals über die Kommandozeile.
func TestDetectRootEscalationLogin(t *testing.T) {
	conn := &scriptedConn{handler: func(cmd, stdin string) (string, int) {
		if cmd == "login root" && stdin == "\ntrue\nexit\n" {
			return "", 0
		}
		return "", 1
	}}
	var log strings.Builder
	esc := detectRootEscalation(conn, "tony", "geheim", &log)
	if esc == nil || esc.method != "login" {
		t.Fatalf("erwartet login, bekam %+v", esc)
	}
	cmd, stdin := esc.wrap("id -u")
	if cmd != "login root" {
		t.Errorf("wrap sollte 'login root' laufen, bekam %q", cmd)
	}
	if stdin != "\nid -u\nexit\n" {
		t.Errorf("erwartete leere zeile + kommando + exit via stdin, bekam %q", stdin)
	}
	if strings.Contains(cmd, "id -u") {
		t.Error("das eigentliche kommando darf nicht in der kommandozeile stehen")
	}
}

// TestDetectRootEscalationNone: kein Weg führt zu root → nil.
func TestDetectRootEscalationNone(t *testing.T) {
	conn := &scriptedConn{handler: func(string, string) (string, int) { return "", 1 }}
	var log strings.Builder
	if esc := detectRootEscalation(conn, "tony", "pw", &log); esc != nil {
		t.Fatalf("erwartet nil (keine Rechte), bekam %+v", esc)
	}
	// Alle sechs Proben wurden versucht.
	if len(conn.calls) != 6 {
		t.Errorf("erwartete 6 Proben (2× sudo, 2× su, 2× login), bekam %d: %v", len(conn.calls), conn.calls)
	}
}

// TestDetectRootEscalationLoginWithoutPasswordWins: login ohne Passwort hat
// Vorrang - das Login-Passwort wird dann gar nicht erst eingespeist.
func TestDetectRootEscalationLoginWithoutPasswordWins(t *testing.T) {
	var stdins []string
	conn := &scriptedConn{handler: func(cmd, stdin string) (string, int) {
		if cmd == "login root" {
			stdins = append(stdins, stdin)
			return "", 0
		}
		return "", 1
	}}
	var log strings.Builder
	esc := detectRootEscalation(conn, "tony", "geheim", &log)
	if esc == nil || esc.method != "login" {
		t.Fatalf("erwartet login, bekam %+v", esc)
	}
	if len(stdins) != 1 || stdins[0] != "\ntrue\nexit\n" {
		t.Errorf("erwartet genau eine login-Probe ohne Passwort, bekam %q", stdins)
	}
	if _, stdin := esc.wrap("id -u"); strings.Contains(stdin, "geheim") {
		t.Errorf("wrap sollte ohne Passwort arbeiten, bekam %q", stdin)
	}
}

// TestDetectRootEscalationLoginWithLoginPassword: sudo und su sind gesperrt,
// login akzeptiert root nur mit dem Login-Passwort. Das Passwort läuft über
// stdin - nie über die Kommandozeile oder das Protokoll - und wird bei jedem
// späteren Kommando wieder eingespeist.
func TestDetectRootEscalationLoginWithLoginPassword(t *testing.T) {
	conn := &scriptedConn{handler: func(cmd, stdin string) (string, int) {
		if cmd == "login root" && stdin == "geheim\ntrue\nexit\n" {
			return "", 0
		}
		return "Login incorrect", 1
	}}
	var log strings.Builder
	esc := detectRootEscalation(conn, "tony", "geheim", &log)
	if esc == nil || esc.method != "login" {
		t.Fatalf("erwartet login, bekam %+v", esc)
	}
	for _, c := range conn.calls {
		if strings.Contains(c, "geheim") {
			t.Errorf("passwort steht in einer kommandozeile (leak): %q", c)
		}
	}
	if strings.Contains(log.String(), "geheim") {
		t.Error("passwort steht im Provisionierungs-Protokoll (leak)")
	}
	for _, kommando := range []string{"id -u", "apt-get install -y sudo"} {
		cmd, stdin := esc.wrap(kommando)
		if cmd != "login root" {
			t.Errorf("wrap sollte 'login root' laufen, bekam %q", cmd)
		}
		if want := "geheim\n" + kommando + "\nexit\n"; stdin != want {
			t.Errorf("wrap sollte das Passwort erneut einspeisen: erwartet %q, bekam %q", want, stdin)
		}
	}
}

// TestDetectRootEscalationNoPasswordNoLoginPasswordProbe: ohne Login-Passwort
// gibt es weder sudo-, su- noch login-Probe mit Passwort.
func TestDetectRootEscalationNoPasswordNoLoginPasswordProbe(t *testing.T) {
	conn := &scriptedConn{handler: func(string, string) (string, int) { return "", 1 }}
	var log strings.Builder
	if esc := detectRootEscalation(conn, "tony", "", &log); esc != nil {
		t.Fatalf("erwartet nil, bekam %+v", esc)
	}
	// sudo -n, su ohne Passwort, login ohne Passwort.
	if len(conn.calls) != 3 {
		t.Errorf("erwartete 3 Proben ohne Passwort, bekam %d: %v", len(conn.calls), conn.calls)
	}
}

// TestSuCommandSetztSbinPfad: su ohne "-" behält den Pfad des SSH-Logins, der
// auf Debian kein sbin enthält. Ohne den Zusatz fand die Provisionierung auf
// Debian 12 weder useradd noch adduser.
func TestSuCommandSetztSbinPfad(t *testing.T) {
	cmd := suCommand("useradd lcm-svc")
	for _, want := range []string{"su root -c ", "/usr/sbin", "/sbin", "useradd lcm-svc"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("su-Kommando enthält %q nicht: %s", want, cmd)
		}
	}
}
