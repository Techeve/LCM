package notify

import (
	"net/smtp"
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

func sampleEvent() domain.NotificationEvent {
	return domain.NotificationEvent{
		Timestamp:   time.Date(2026, 7, 7, 18, 56, 0, 0, time.UTC),
		Severity:    domain.AlertSeverityCritical,
		ServerGroup: "Produktion",
		ServerName:  "db-01",
		Code:        domain.AlertTypeDiskCapacity,
		Description: "Festplatte zu 95% belegt",
		Details:     "Belegt: 95000 MB von 100000 MB.",
	}
}

func TestEmailValidate(t *testing.T) {
	valid := NewEmailProvider(EmailConfig{
		Host: "smtp.example.com", Port: 587, From: "lcm@example.com",
		Recipients: []string{"ops@example.com"},
	}, "")
	if err := valid.Validate(); err != nil {
		t.Fatalf("erwartete gültige Konfiguration, bekam: %v", err)
	}

	cases := map[string]EmailConfig{
		"host fehlt":     {Port: 587, From: "a@b.c", Recipients: []string{"x@y.z"}},
		"port ungültig":  {Host: "h", Port: 0, From: "a@b.c", Recipients: []string{"x@y.z"}},
		"from fehlt":     {Host: "h", Port: 25, Recipients: []string{"x@y.z"}},
		"kein empfänger": {Host: "h", Port: 25, From: "a@b.c"},
	}
	for name, cfg := range cases {
		if err := NewEmailProvider(cfg, "").Validate(); err == nil {
			t.Errorf("%s: erwartete Validierungsfehler, bekam keinen", name)
		}
	}
}

func TestEmailRenderSubjectAndBody(t *testing.T) {
	event := sampleEvent()
	subject := RenderSubject(event)
	if !strings.Contains(subject, "KRITISCH") || !strings.Contains(subject, "db-01") ||
		!strings.Contains(subject, "Festplatte zu 95% belegt") {
		t.Errorf("unerwarteter Betreff: %q", subject)
	}
	body, err := RenderBody(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"KRITISCH", "Produktion", "db-01", domain.AlertTypeDiskCapacity,
		"Festplatte zu 95% belegt", "95000 MB"} {
		if !strings.Contains(body, want) {
			t.Errorf("Rumpf enthält %q nicht:\n%s", want, body)
		}
	}
}

func TestEmailSendUsesConfig(t *testing.T) {
	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte
	var gotAuth smtp.Auth
	provider := NewEmailProvider(EmailConfig{
		Host: "smtp.example.com", Port: 587, Username: "user", From: "lcm@example.com",
		Recipients: []string{"ops@example.com", " ", "team@example.com"},
	}, "geheim").withSender(func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotAuth, gotFrom, gotTo, gotMsg = addr, auth, from, to, msg
		return nil
	})

	if err := provider.Send(sampleEvent()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAddr != "smtp.example.com:587" {
		t.Errorf("addr = %q", gotAddr)
	}
	if gotFrom != "lcm@example.com" {
		t.Errorf("from = %q", gotFrom)
	}
	// Leere Empfänger werden herausgefiltert.
	if len(gotTo) != 2 {
		t.Errorf("erwartete 2 Empfänger, bekam %d: %v", len(gotTo), gotTo)
	}
	if gotAuth == nil {
		t.Error("erwartete SMTP-Auth (Username gesetzt)")
	}
	if !strings.Contains(string(gotMsg), "Subject:") {
		t.Errorf("Nachricht ohne Subject-Header:\n%s", gotMsg)
	}
}

func TestNewFactoryEmail(t *testing.T) {
	channel := domain.NotificationChannel{
		Type:   domain.ChannelTypeEmail,
		Config: `{"host":"h","port":25,"from":"a@b.c","recipients":["x@y.z"]}`,
	}
	provider, err := New(channel, "")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Type() != domain.ChannelTypeEmail {
		t.Errorf("Type = %q", provider.Type())
	}
	if err := provider.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}

	if _, err := New(domain.NotificationChannel{Type: "sms"}, ""); err == nil {
		t.Error("erwartete Fehler für unbekannten Kanaltyp")
	}
}

// TestNachrichtTraegtDateUndMessageID (R2-031): ohne Date stempeln
// Gegenstellen die Mail nach Empfangszeit, ohne Message-ID werten
// Spam-Filter das Fehlen als Verdachtsmoment.
func TestNachrichtTraegtDateUndMessageID(t *testing.T) {
	msg := string(buildMessage("lcm@example.org", []string{"ops@example.org"}, "Betreff", "Rumpf"))
	if !strings.Contains(msg, "Date: ") {
		t.Error("Date-Header fehlt")
	}
	if !strings.Contains(msg, "Message-ID: <") || !strings.Contains(msg, "@example.org>") {
		t.Errorf("Message-ID fehlt oder ohne Absender-Domain: %s", msg)
	}
	// EHLO-Name: nie "localhost".
	if heloName() == "localhost" {
		t.Error("heloName darf nicht localhost sein")
	}
}
