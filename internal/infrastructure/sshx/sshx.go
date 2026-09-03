// Package sshx ist die SSH-Infrastruktur des LCM auf Basis von
// golang.org/x/crypto/ssh.
//
// Sicherheitsmodell:
//   - Probe() liest den Host-Key-Fingerprint eines neuen Servers aus,
//     OHNE Credentials zu übertragen - der Admin bestätigt den
//     Fingerprint in der UI (Schutz vor MitM beim Trust-On-First-Use).
//   - Alle authentifizierten Verbindungen erzwingen strikten Host-Key-
//     Vergleich gegen den gespeicherten Fingerprint. Ändert sich der
//     Host-Key (möglicher MitM-Angriff), bricht die Verbindung sofort
//     mit einem kritischen Fehler ab.
package sshx

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// syncBuffer ist ein nebenläufigkeitssicherer Puffer. x/crypto/ssh kopiert
// Stdout und Stderr in ZWEI parallelen Goroutinen; würden beide in denselben
// bytes.Buffer schreiben (der nicht thread-safe ist), gingen bei längeren
// Ausgaben durch den Data Race Daten verloren. Dieser Mutex verhindert das.
//
// onWrite ist das Lebenszeichen des laufenden Kommandos: Der Puffer füllt
// sich, WÄHREND das Kommando läuft - jeder eintreffende Block belegt, dass
// die Gegenseite noch arbeitet. Darauf stützt sich der Job-Watchdog, statt
// eine feste Maximaldauer zu unterstellen. Das Feld wird vor dem Start
// gesetzt und danach nur noch gelesen.
type syncBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	onWrite func()
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	n, err := b.buf.Write(p)
	b.mu.Unlock()
	if b.onWrite != nil {
		b.onWrite()
	}
	return n, err
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ErrHostKeyMismatch signalisiert einen abweichenden Host-Key -
// möglicher Man-in-the-Middle-Angriff, Verbindung wird verweigert.
var ErrHostKeyMismatch = errors.New("SSH-Host-Key stimmt nicht mit dem gespeicherten Fingerprint überein - mögliche MitM-Attacke, Verbindung abgebrochen")

// ErrPasswordAuthUnavailable: der Server bietet keine Passwort-Anmeldung an
// (typisch nach SSH-Härtung - nur Zertifikats-Login). Ein Passwort-Login ist
// dann strukturell unmöglich; stattdessen den System-SSH-Key nutzen.
var ErrPasswordAuthUnavailable = errors.New("der Server bietet keine Passwort-Anmeldung an (vermutlich SSH-gehärtet) - nutze die Anmeldung per System-SSH-Key und hinterlege dessen Public Key auf dem Server")

// ErrPasswordRejected: Der Server bietet die Passwort-Anmeldung an, hat die
// Zugangsdaten aber zurückgewiesen. Die Unterscheidung zu
// ErrPasswordAuthUnavailable ist der Unterschied zwischen „geht hier gar
// nicht" und „stimmt nicht" - und damit zwischen dem Umbau der
// sshd-Konfiguration und einem zweiten Blick auf das Passwort.
var ErrPasswordRejected = errors.New("Anmeldung abgelehnt - Benutzername oder Passwort stimmt nicht (der Server bietet die Passwort-Anmeldung an)")

// DialTimeout begrenzt Verbindungsaufbau und Handshake.
const DialTimeout = 15 * time.Second

// Conn ist eine authentifizierte SSH-Verbindung zu einem Server.
type Conn interface {
	// Run führt ein Kommando aus und liefert den kombinierten
	// Stdout/Stderr-Output sowie den Exit-Code.
	Run(cmd string) (output string, exitCode int, err error)
	// RunStdin wie Run, speist dem Kommando aber stdin ein - z.B. um `sudo -S`
	// das Passwort zu übergeben, ohne es in die (protokollierte) Kommandozeile
	// zu schreiben. Bei leerem stdin verhält es sich wie Run.
	RunStdin(cmd, stdin string) (output string, exitCode int, err error)
	// OnActivity hinterlegt einen Rückruf für die Lebenszeichen laufender
	// Kommandos: Er feuert bei jedem eintreffenden Ausgabe-Block sowie zu
	// Beginn und Ende jedes Kommandos. Der Job-Watchdog erkennt daran, ob ein
	// Lauf noch arbeitet oder hängt (siehe JobService.MarkActivity). fn muss
	// schnell zurückkehren - der Aufruf hält den Ausgabestrom auf.
	OnActivity(fn func())
	Close() error
}

// Dialer baut SSH-Verbindungen auf. Als Interface definiert, damit
// Services im Test (und im Demo-Modus) eine Fake-Implementierung nutzen.
type Dialer interface {
	// Probe liest den Host-Key eines Servers aus (ohne Anmeldung).
	Probe(host string, port int) (fingerprint, keyType string, err error)
	// DialPassword verbindet mit User/Passwort; expectedFingerprint
	// muss dem zuvor per Probe bestätigten Wert entsprechen.
	DialPassword(host string, port int, user, password, expectedFingerprint string) (Conn, error)
	// DialKey verbindet mit einem PEM-kodierten Private Key.
	DialKey(host string, port int, user, privateKeyPEM, expectedFingerprint string) (Conn, error)
}

// Client ist die produktive Dialer-Implementierung.
type Client struct{}

func NewClient() *Client { return &Client{} }

// Probe verbindet sich ohne Credentials, extrahiert den Host-Key aus dem
// Handshake und bricht dann ab. Es werden keinerlei Anmeldedaten gesendet.
func (c *Client) Probe(host string, port int) (string, string, error) {
	var fingerprint, keyType string
	config := &ssh.ClientConfig{
		User: "lcm-probe",
		// Kein Auth-Verfahren: Der Handshake scheitert NACH dem
		// Host-Key-Austausch - genau das reicht für den Fingerprint.
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			fingerprint = ssh.FingerprintSHA256(key)
			keyType = key.Type()
			return nil
		},
		Timeout: DialTimeout,
	}
	conn, err := ssh.Dial("tcp", addr(host, port), config)
	if err == nil {
		conn.Close()
	}
	if fingerprint == "" {
		return "", "", fmt.Errorf("host-key von %s nicht lesbar: %w", host, err)
	}
	return fingerprint, keyType, nil
}

// DialPassword verbindet per Passwort-Authentifizierung (nur Onboarding).
// Bietet der Server die Passwort-Methode gar nicht an (gehärtet, nur
// publickey), kommt der gezielte ErrPasswordAuthUnavailable zurück.
//
// Neben der klassischen "password"-Methode wird zusätzlich
// "keyboard-interactive" angeboten (beantwortet jede Frage mit demselben
// Passwort): viele Systeme wickeln den Passwort-Login über eine
// PAM-Conversation statt über die klassische Methode ab - u.a. root-Login
// ohne gesetztes Passwort (leeres `password`, PAM mit `nullok`). Der Server
// wählt die Methode, die er tatsächlich anbietet; nicht unterstützte
// Methoden werden vom Handshake ignoriert.
func (c *Client) DialPassword(host string, port int, user, password, expectedFingerprint string) (Conn, error) {
	auth := []ssh.AuthMethod{
		ssh.Password(password),
		ssh.KeyboardInteractive(passwordKeyboardInteractive(password)),
	}
	conn, err := c.dial(host, port, user, auth, expectedFingerprint)
	if err != nil {
		// Reihenfolge entscheidet: „nur none versucht" ist der speziellere
		// Fall und muss vor der allgemeinen Ablehnung geprüft werden.
		if isNoPasswordAuthError(err) {
			return nil, ErrPasswordAuthUnavailable
		}
		if isAuthRejectedError(err) {
			return nil, ErrPasswordRejected
		}
	}
	return conn, err
}

// passwordKeyboardInteractive beantwortet jede Frage einer
// "keyboard-interactive"-Authentifizierung (PAM-Conversation) mit dem
// übergebenen Passwort - inklusive leerem String für passwortlose Logins
// (z.B. root ohne gesetztes Passwort, PAM erlaubt das per `nullok`).
func passwordKeyboardInteractive(password string) ssh.KeyboardInteractiveChallenge {
	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i := range answers {
			answers[i] = password
		}
		return answers, nil
	}
}

// isNoPasswordAuthError erkennt den x/crypto-Fehler, wenn der Server die
// Passwort-Methode gar nicht anbietet (gehärteter Server, nur publickey).
//
// Erkennungsmerkmal ist AUSSCHLIESSLICH die Liste der versuchten Methoden:
// Bleibt es bei „[none]", hat der Server nichts weiter zugelassen. Wurde
// dagegen „[none password …]" versucht, bot er die Passwort-Methode an und hat
// die Zugangsdaten abgelehnt - ein ganz anderer Fall.
//
// Die schließende Klammer ist dabei wesentlich: „attempted methods [none]"
// steckt NICHT in „attempted methods [none password]".
//
// Vorher wurde zusätzlich auf „no supported methods remain" geprüft. Dieser
// Satz steht in BEIDEN Fällen am Ende der x/crypto-Meldung, weshalb ein
// schlichter Tippfehler im Passwort als „Server ist gehärtet" gemeldet wurde -
// samt Empfehlung, auf Schlüssel-Anmeldung umzustellen. Wer dem folgte, baute
// die sshd-Konfiguration um, um ein Problem zu lösen, das es nicht gab.
func isNoPasswordAuthError(err error) bool {
	return strings.Contains(err.Error(), "attempted methods [none]")
}

// isAuthRejectedError erkennt die abgelehnte Anmeldung: Die Methode stand zur
// Verfügung, die Zugangsdaten passten nicht.
func isAuthRejectedError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "no supported methods remain")
}

// DialKey verbindet per Private-Key-Authentifizierung (PEM) - der
// Regelfall nach dem Onboarding (pro-Server-Keypair bzw. Onboarding-Key).
func (c *Client) DialKey(host string, port int, user, privateKeyPEM, expectedFingerprint string) (Conn, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("private key parsen: %w", err)
	}
	return c.dial(host, port, user, []ssh.AuthMethod{ssh.PublicKeys(signer)}, expectedFingerprint)
}

func (c *Client) dial(host string, port int, user string, auth []ssh.AuthMethod, expectedFingerprint string) (Conn, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: auth,
		// Strict Host Key Checking gegen den gespeicherten Fingerprint.
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != expectedFingerprint {
				return ErrHostKeyMismatch
			}
			return nil
		},
		Timeout: DialTimeout,
	}
	client, err := ssh.Dial("tcp", addr(host, port), config)
	if err != nil {
		if strings.Contains(err.Error(), ErrHostKeyMismatch.Error()) {
			return nil, ErrHostKeyMismatch
		}
		return nil, fmt.Errorf("ssh-verbindung zu %s: %w", host, err)
	}
	return &clientConn{client: client}, nil
}

type clientConn struct {
	client *ssh.Client

	mu         sync.Mutex
	onActivity func() // Lebenszeichen-Rückruf, siehe Conn.OnActivity
}

// OnActivity hinterlegt den Lebenszeichen-Rückruf dieser Verbindung.
func (c *clientConn) OnActivity(fn func()) {
	c.mu.Lock()
	c.onActivity = fn
	c.mu.Unlock()
}

// activity meldet ein Lebenszeichen, sofern ein Rückruf hinterlegt ist.
func (c *clientConn) activity() {
	c.mu.Lock()
	fn := c.onActivity
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// Run führt ein Kommando aus. Stdout und Stderr werden gemeinsam
// aufgezeichnet (chronologisch gemischt), damit der exakte Konsolen-
// Output zur Fehleranalyse zur Verfügung steht.
func (c *clientConn) Run(cmd string) (string, int, error) {
	return c.RunStdin(cmd, "")
}

// RunStdin wie Run, leitet aber stdin ins Kommando (für `sudo -S`).
func (c *clientConn) RunStdin(cmd, stdin string) (string, int, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", -1, fmt.Errorf("ssh-session: %w", err)
	}
	defer session.Close()

	// Start und Ende zählen als Lebenszeichen: Ein Ablauf aus vielen kurzen,
	// stillen Kommandos (Scan) arbeitet sichtbar, ohne Ausgabe zu erzeugen.
	c.activity()
	defer c.activity()

	buf := syncBuffer{onWrite: c.activity}
	session.Stdout = &buf
	session.Stderr = &buf
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}

	err = session.Run(cmd)
	output := buf.String()
	if err == nil {
		return output, 0, nil
	}
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return output, exitErr.ExitStatus(), nil // Non-Zero-Exit ist kein Transportfehler
	}
	return output, -1, fmt.Errorf("ssh-kommando: %w", err)
}

func (c *clientConn) Close() error { return c.client.Close() }

func addr(host string, port int) string {
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

// GenerateKeyPair erzeugt ein neues ed25519-SSH-Schlüsselpaar.
// Rückgabe: PEM-kodierter Private Key (OpenSSH-Format) und die
// authorized_keys-Zeile des Public Keys.
func GenerateKeyPair(comment string) (privatePEM, publicLine string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("keypair erzeugen: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return "", "", fmt.Errorf("private key kodieren: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}
	return string(pem.EncodeToMemory(block)), line, nil
}
