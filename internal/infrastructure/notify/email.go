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
	"net/smtp"
	"os"
	"strings"
	"text/template"
	"time"

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
	// UseTLS aktiviert STARTTLS beim Versand (Standard bei Port 587).
	UseTLS bool `json:"use_tls"`
}

// parseEmailConfig liest die JSON-Konfiguration eines E-Mail-Kanals.
func parseEmailConfig(raw string) (EmailConfig, error) {
	var cfg EmailConfig
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, fmt.Errorf("e-mail-konfiguration ungültig: %w", err)
	}
	return cfg, nil
}

// sendMailFunc ist die Versand-Funktion (in Tests austauschbar).
type sendMailFunc func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error

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

// sendMail ist der eigene Versandkern anstelle von smtp.SendMail: dessen
// EHLO meldete sich wortwörtlich als „localhost" - Spam-Filter werten das
// als Verdachtsmoment, und im Server-Log der Gegenstelle ist der Absender
// nicht zuzuordnen (R2-031). Hier: EHLO mit dem tatsächlichen Hostnamen,
// STARTTLS wenn angeboten, Deadline über die gesamte Sitzung.
func sendMail(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", addr, smtpTimeout)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(smtpTimeout))
	host, _, _ := net.SplitHostPort(addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Hello(heloName()); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
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

// Type liefert den Kanal-Typ.
func (p *EmailProvider) Type() string { return domain.ChannelTypeEmail }

// Validate prüft die SMTP-Konfiguration auf Vollständigkeit.
func (p *EmailProvider) Validate() error {
	if strings.TrimSpace(p.cfg.Host) == "" {
		return errors.New("smtp-host fehlt")
	}
	if p.cfg.Port <= 0 || p.cfg.Port > 65535 {
		return errors.New("smtp-port ungültig")
	}
	if strings.TrimSpace(p.cfg.From) == "" {
		return errors.New("absenderadresse (from) fehlt")
	}
	if len(p.recipients()) == 0 {
		return errors.New("mindestens ein empfänger ist erforderlich")
	}
	return nil
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
	recipients := p.recipients()
	msg := buildMessage(p.cfg.From, recipients, subject, body)

	var auth smtp.Auth
	if p.cfg.Username != "" {
		auth = smtp.PlainAuth("", p.cfg.Username, p.password, p.cfg.Host)
	}
	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)
	if err := p.send(addr, auth, p.cfg.From, recipients, msg); err != nil {
		return fmt.Errorf("smtp-versand: %w", err)
	}
	return nil
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
	msg := buildMessage(p.cfg.From, recipients, subject, body)
	var auth smtp.Auth
	if p.cfg.Username != "" {
		auth = smtp.PlainAuth("", p.cfg.Username, p.password, p.cfg.Host)
	}
	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)
	if err := p.send(addr, auth, p.cfg.From, recipients, msg); err != nil {
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
func buildMessage(from string, to []string, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", messageToken(), messageDomain(from))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return []byte(b.String())
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
func messageDomain(from string) string {
	if i := strings.LastIndex(from, "@"); i >= 0 && i < len(from)-1 {
		return strings.TrimRight(strings.TrimSpace(from[i+1:]), ">")
	}
	return heloName()
}
