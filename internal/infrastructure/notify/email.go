package notify

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"text/template"
	"time"
	"unicode"

	"LCM/internal/core/domain"
)

// EmailConfig ist die nicht-sensible SMTP-Konfiguration eines E-Mail-Kanals
// (das Passwort wird getrennt, verschlüsselt gehalten).
type EmailConfig struct {
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	Username   string   `json:"username"`
	From       string   `json:"from"`
	Recipients []string `json:"recipients"`
	// Security ist die Verschlüsselung: domain.MailSecurityTLS (implizit,
	// Port 465), MailSecurityStartTLS (Pflicht, Port 587) oder MailSecurityNone
	// (Port 25). Leer bei Kanälen vor der Auswahl - dann gilt UseTLS.
	Security string `json:"security"`
	// UseTLS ist das alte Ja/Nein (true = starttls, false = none); nur
	// maßgeblich, solange Security leer ist.
	UseTLS bool `json:"use_tls"`
}

// security liefert den wirksamen Modus.
func (c EmailConfig) security() string {
	if c.Security != "" {
		return c.Security
	}
	return domain.MailSecurityFromLegacy(c.UseTLS)
}

// parseEmailConfig liest die JSON-Konfiguration eines E-Mail-Kanals.
func parseEmailConfig(raw string) (EmailConfig, error) {
	// Fehlen security und use_tls, bleibt die Verschlüsselung an (nie stiller
	// Klartext).
	cfg := EmailConfig{UseTLS: true}
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, fmt.Errorf("e-mail-konfiguration ungültig: %w", err)
	}
	return cfg, nil
}

// sendMailFunc ist die Versand-Funktion (in Tests austauschbar).
type sendMailFunc func(addr, security string, auth smtp.Auth, from string, to []string, msg []byte) error

// EmailProvider ist die konkrete E-Mail-Implementierung der Kanal-
// Schnittstelle: SMTP-Konfiguration, Authentifizierung und Templating der
// E-Mail-Inhalte (Betreff, Dringlichkeitsstufe, formatierte Fehlermeldung).
type EmailProvider struct {
	cfg      EmailConfig
	password string
	send     sendMailFunc
}

// NewEmailProvider erstellt einen E-Mail-Provider aus Konfiguration und
// (bereits entschlüsseltem) SMTP-Passwort.
func NewEmailProvider(cfg EmailConfig, password string) *EmailProvider {
	return &EmailProvider{cfg: cfg, password: password, send: sendMail}
}

// smtpTimeout begrenzt Verbindungsaufbau UND Gesamtsitzung - ein
// unerreichbarer SMTP-Host darf den Aufrufer nicht minutenlang halten
// (R2-033; der Versand selbst läuft zusätzlich asynchron).
const smtpTimeout = 15 * time.Second

// errKeinSTARTTLS: Der Modus STARTTLS verlangt Verschlüsselung, der Server
// bietet sie aber nicht an.
var errKeinSTARTTLS = errors.New("server bietet kein STARTTLS an - STARTTLS am Server aktivieren, die Verschlüsselung „TLS“ wählen (üblich Port 465) oder „Keine“ wählen, wenn der Server unverschlüsselt arbeiten soll")

// tlsConfigFor liefert die TLS-Einstellungen für die Verbindung zu host (in
// Tests austauschbar, etwa um ein selbst signiertes Zertifikat zu vertrauen).
var tlsConfigFor = func(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}

// dialSMTP baut die Verbindung auf: bei „tls“ mit implizitem TLS ab dem
// ersten Byte, sonst als einfache TCP-Verbindung.
func dialSMTP(addr, host, security string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: smtpTimeout}
	if security == domain.MailSecurityTLS {
		return tls.DialWithDialer(dialer, "tcp", addr, tlsConfigFor(host))
	}
	return dialer.Dial("tcp", addr)
}

// sendMail ist der eigene Versandkern anstelle von smtp.SendMail: dessen
// EHLO meldete sich wortwörtlich als „localhost" - Spam-Filter werten das
// als Verdachtsmoment, und im Server-Log der Gegenstelle ist der Absender
// nicht zuzuordnen (R2-031). Hier: EHLO mit dem tatsächlichen Hostnamen,
// Verschlüsselung nach security (tls: implizit; starttls: Pflicht, sonst
// Abbruch; none: keine), Deadline über die gesamte Sitzung. Nie stiller
// Klartext, wenn Verschlüsselung gewählt ist.
func sendMail(addr, security string, auth smtp.Auth, from string, to []string, msg []byte) error {
	host, _, _ := net.SplitHostPort(addr)
	conn, err := dialSMTP(addr, host, security)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(smtpTimeout))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Hello(heloName()); err != nil {
		return err
	}
	if security == domain.MailSecurityStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errKeinSTARTTLS
		}
		if err := c.StartTLS(tlsConfigFor(host)); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// heloName liefert den EHLO-Namen: der eigene Hostname, Rückfall "lcm".
func heloName() string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return h
	}
	return "lcm"
}

// withSender ersetzt die Versand-Funktion (nur für Tests).
func (p *EmailProvider) withSender(fn sendMailFunc) *EmailProvider {
	p.send = fn
	return p
}

// auth liefert die Anmeldung am Server, nil ohne Benutzer. Mit tls und
// starttls schützt die Verschlüsselung das Passwort. Mit none hat der Nutzer
// die Verschlüsselung ausdrücklich abgewählt, dann geht das Passwort im
// Klartext.
func (p *EmailProvider) auth() smtp.Auth {
	if p.cfg.Username == "" {
		return nil
	}
	if p.cfg.security() != domain.MailSecurityNone {
		return smtp.PlainAuth("", p.cfg.Username, p.password, p.cfg.Host)
	}
	return plainAuthKlartext{username: p.cfg.Username, password: p.password}
}

// plainAuthKlartext ist PLAIN ohne die TLS-Sperre von smtp.PlainAuth. Nur
// für Server ohne Verschlüsselung, die der Nutzer selbst gewählt hat.
type plainAuthKlartext struct {
	username, password string
}

func (a plainAuthKlartext) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

func (a plainAuthKlartext) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unerwartete Anfrage des Servers bei der Anmeldung")
	}
	return nil, nil
}

// Type liefert den Kanal-Typ.
func (p *EmailProvider) Type() string { return domain.ChannelTypeEmail }

// Validate prüft die SMTP-Konfiguration: vollständig, Verschlüsselung einer
// der drei Modi, Host nur aus den Zeichen eines Hostnamens, Absender eine
// Adresse mit oder ohne Anzeigenamen, Empfänger je eine einzelne Adresse. Die
// Adressen landen in den Kopfzeilen der Nachricht - ein Zeilenumbruch darin
// ließe zusätzliche Kopfzeilen einschmuggeln.
func (p *EmailProvider) Validate() error {
	host := strings.TrimSpace(p.cfg.Host)
	if host == "" {
		return errors.New("smtp-host fehlt")
	}
	if !validSMTPHost(host) {
		return errors.New("smtp-host enthält unzulässige Zeichen")
	}
	if p.cfg.Port <= 0 || p.cfg.Port > 65535 {
		return errors.New("smtp-port ungültig")
	}
	if !domain.ValidMailSecurity(p.cfg.security()) {
		return fmt.Errorf("verschlüsselung (security) ungültig: %q, erlaubt sind tls, starttls und none", p.cfg.Security)
	}
	if strings.TrimSpace(p.cfg.From) == "" {
		return errors.New("absenderadresse (from) fehlt")
	}
	if _, err := parseSender(p.cfg.From); err != nil {
		return fmt.Errorf("absenderadresse (from) ungültig: %q", p.cfg.From)
	}
	recipients := p.recipients()
	if len(recipients) == 0 {
		return errors.New("mindestens ein empfänger ist erforderlich")
	}
	if len(recipients) > maxRecipients {
		return fmt.Errorf("zu viele empfänger (höchstens %d)", maxRecipients)
	}
	for _, r := range recipients {
		if !singleAddress(r) {
			return fmt.Errorf("empfängeradresse ungültig: %q", r)
		}
	}
	return nil
}

// maxRecipients begrenzt die Empfänger eines Kanals.
const maxRecipients = 50

// validSMTPHost: Hostname oder IP-Adresse, nichts mit Bedeutung in Shell oder URL.
func validSMTPHost(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, r := range host {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '-' || r == ':' || r == '[' || r == ']' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// parseSender liest den Absender: eine Adresse, mit oder ohne Anzeigenamen
// (`lcm@example.com`, `LCM <lcm@example.com>`), ohne Steuerzeichen. Für den
// SMTP-Umschlag gilt nur Address, für die From-Kopfzeile String().
func parseSender(v string) (*mail.Address, error) {
	v = strings.TrimSpace(v)
	for _, r := range v {
		if unicode.IsControl(r) {
			return nil, errors.New("steuerzeichen im absender")
		}
	}
	return mail.ParseAddress(v)
}

// singleAddress ist wahr für genau eine Adresse ohne Anzeigenamen.
func singleAddress(v string) bool {
	v = strings.TrimSpace(v)
	addr, err := mail.ParseAddress(v)
	return err == nil && addr.Address == v && !strings.ContainsAny(v, "\r\n")
}

// recipients liefert die getrimmten, nicht-leeren Empfängeradressen.
func (p *EmailProvider) recipients() []string {
	out := make([]string, 0, len(p.cfg.Recipients))
	for _, r := range p.cfg.Recipients {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// Send baut Betreff und Rumpf aus dem Event und versendet die E-Mail per SMTP.
func (p *EmailProvider) Send(event domain.NotificationEvent) error {
	if err := p.Validate(); err != nil {
		return err
	}
	subject := RenderSubject(event)
	body, err := RenderBody(event)
	if err != nil {
		return err
	}
	return p.deliver(subject, body, p.recipients())
}

// SendRaw versendet eine transaktionale Mail (Betreff + Klartext-Rumpf) an
// explizite Empfänger - der Versandkern für den System-Mailer (Passwort-
// Reset, Einladungslinks, Admin-Hinweise). Die in der Konfiguration
// hinterlegten Empfänger werden dabei bewusst ignoriert.
func (p *EmailProvider) SendRaw(subject, body string, to []string) error {
	recipients := make([]string, 0, len(to))
	for _, r := range to {
		if r = strings.TrimSpace(r); r != "" {
			recipients = append(recipients, r)
		}
	}
	if len(recipients) == 0 {
		return errors.New("mindestens ein empfänger ist erforderlich")
	}
	return p.deliver(subject, body, recipients)
}

// deliver baut die Nachricht und übergibt sie dem Versandkern. Im Umschlag
// (MAIL FROM) steht nur die Adresse, in der From-Kopfzeile der Absender mit
// Anzeigenamen.
func (p *EmailProvider) deliver(subject, body string, recipients []string) error {
	sender, err := parseSender(p.cfg.From)
	if err != nil {
		return fmt.Errorf("absenderadresse (from) ungültig: %q", p.cfg.From)
	}
	msg := buildMessage(sender, recipients, subject, body)
	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)
	if err := p.send(addr, p.cfg.security(), p.auth(), sender.Address, recipients, msg); err != nil {
		return fmt.Errorf("smtp-versand: %w", err)
	}
	return nil
}

// severityLabel liefert das Betreff-Präfix der Dringlichkeitsstufe.
func severityLabel(severity string) string {
	switch severity {
	case domain.AlertSeverityCritical:
		return "KRITISCH"
	case domain.AlertSeverityWarning:
		return "WARNUNG"
	default:
		return "INFO"
	}
}

// RenderSubject baut den Betreff aus Dringlichkeitsstufe und Beschreibung.
func RenderSubject(event domain.NotificationEvent) string {
	label := severityLabel(event.Severity)
	scope := event.ServerName
	if scope == "" {
		scope = event.ServerGroup
	}
	if scope != "" {
		return fmt.Sprintf("[LCM %s] %s - %s", label, scope, event.Description)
	}
	return fmt.Sprintf("[LCM %s] %s", label, event.Description)
}

// bodyTemplate ist die Vorlage für den E-Mail-Rumpf (formatierte Meldung).
var bodyTemplate = template.Must(template.New("email").Parse(
	`LCM - Linux Centralized Management

Dringlichkeit: {{ .Label }}
Zeitpunkt:     {{ .Timestamp }}
{{- if .ServerGroup }}
Servergruppe:  {{ .ServerGroup }}
{{- end }}
{{- if .ServerName }}
Server:        {{ .ServerName }}
{{- end }}
{{- if .Code }}
Fehlercode:    {{ .Code }}
{{- end }}

{{ .Description }}
{{- if .Details }}

{{ .Details }}
{{- end }}
`))

// RenderBody rendert den E-Mail-Rumpf aus dem standardisierten Event.
func RenderBody(event domain.NotificationEvent) (string, error) {
	ts := event.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	data := struct {
		Label       string
		Timestamp   string
		ServerGroup string
		ServerName  string
		Code        string
		Description string
		Details     string
	}{
		Label:       severityLabel(event.Severity),
		Timestamp:   ts.Format("2006-01-02 15:04:05 MST"),
		ServerGroup: event.ServerGroup,
		ServerName:  event.ServerName,
		Code:        event.Code,
		Description: event.Description,
		Details:     event.Details,
	}
	var buf bytes.Buffer
	if err := bodyTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// buildMessage setzt eine RFC-5322-konforme Nachricht aus Headern und Rumpf
// zusammen (UTF-8, CRLF-Zeilenenden). Date und Message-ID sind Pflicht der
// RFC - ohne sie stempeln Gegenstellen die Mail nach Empfangszeit und
// Spam-Filter werten das Fehlen als Verdachtsmoment (R2-031).
func buildMessage(from *mail.Address, to []string, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", headerValue(from.String()))
	fmt.Fprintf(&b, "To: %s\r\n", headerValue(strings.Join(to, ", ")))
	fmt.Fprintf(&b, "Subject: %s\r\n", headerValue(subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", messageToken(), messageDomain(from.Address))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return []byte(b.String())
}

// headerValue macht aus einem Wert eine einzelne Kopfzeile: Zeilenumbrüche
// werden zu Leerzeichen, damit kein Wert eine weitere Kopfzeile eröffnen kann.
func headerValue(v string) string {
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(v)
}

// messageToken erzeugt den eindeutigen linken Teil der Message-ID.
func messageToken() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d.%s", time.Now().Unix(), hex.EncodeToString(buf))
}

// messageDomain leitet die Message-ID-Domain aus der Absenderadresse ab;
// Rückfall ist der eigene Hostname.
func messageDomain(address string) string {
	if i := strings.LastIndex(address, "@"); i >= 0 && i < len(address)-1 {
		return address[i+1:]
	}
	return heloName()
}
