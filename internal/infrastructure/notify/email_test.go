package notify

import (
	"net/mail"
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
	}, "geheim").withSender(func(addr, _ string, auth smtp.Auth, from string, to []string, msg []byte) error {
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
	msg := string(buildMessage(&mail.Address{Address: "lcm@example.org"}, []string{"ops@example.org"}, "Betreff", "Rumpf"))
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

// Die Verschlüsselung folgt security; fehlt es, gilt das alte use_tls, und
// fehlt auch das, bleibt STARTTLS an (nie stiller Klartext).
func TestParseEmailConfigVerschluesselung(t *testing.T) {
	cases := map[string]string{
		`{"host":"h","port":25}`:                                   domain.MailSecurityStartTLS,
		`{"host":"h","port":25,"use_tls":true}`:                    domain.MailSecurityStartTLS,
		`{"host":"h","port":25,"use_tls":false}`:                   domain.MailSecurityNone,
		`{"host":"h","port":465,"security":"tls"}`:                 domain.MailSecurityTLS,
		`{"host":"h","port":587,"security":"starttls"}`:            domain.MailSecurityStartTLS,
		`{"host":"h","port":25,"security":"none"}`:                 domain.MailSecurityNone,
		`{"host":"h","port":25,"security":"none","use_tls":true}`:  domain.MailSecurityNone,
		`{"host":"h","port":465,"security":"tls","use_tls":false}`: domain.MailSecurityTLS,
		``: domain.MailSecurityStartTLS,
	}
	for raw, want := range cases {
		cfg, err := parseEmailConfig(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if got := cfg.security(); got != want {
			t.Errorf("%s: security = %q, erwartet %q", raw, got, want)
		}
	}
}

// Absender mit Anzeigenamen: Im Umschlag steht nur die Adresse, in der
// From-Kopfzeile der kodierte Absender, Message-ID und Domain kommen aus der
// Adresse.
func TestEmailAbsenderMitAnzeigenamen(t *testing.T) {
	cases := []struct {
		from, envelope, header string
	}{
		{"lcm@example.com", "lcm@example.com", "From: <lcm@example.com>"},
		{"LCM <lcm@example.com>", "lcm@example.com", `From: "LCM" <lcm@example.com>`},
		{`"LCM, Betrieb" <lcm@example.com>`, "lcm@example.com", `From: "LCM, Betrieb" <lcm@example.com>`},
		{"LCM Überwachung <lcm@example.com>", "lcm@example.com", "From: =?utf-8?q?LCM_=C3=9Cberwachung?= <lcm@example.com>"},
	}
	for _, tc := range cases {
		var gotFrom string
		var gotMsg []byte
		p := NewEmailProvider(EmailConfig{Host: "h", Port: 587, From: tc.from, Security: domain.MailSecurityStartTLS}, "").
			withSender(func(_, _ string, _ smtp.Auth, from string, _ []string, msg []byte) error {
				gotFrom, gotMsg = from, msg
				return nil
			})
		if err := p.SendRaw("Betreff", "Rumpf", []string{"ops@example.com"}); err != nil {
			t.Fatalf("%s: %v", tc.from, err)
		}
		if gotFrom != tc.envelope {
			t.Errorf("%s: MAIL FROM = %q, erwartet %q", tc.from, gotFrom, tc.envelope)
		}
		if !strings.Contains(string(gotMsg), tc.header+"\r\n") {
			t.Errorf("%s: Kopfzeile fehlt, erwartet %q in:\n%s", tc.from, tc.header, gotMsg)
		}
		if !strings.Contains(string(gotMsg), "@example.com>\r\n") || strings.Contains(string(gotMsg), "@example.com>>") {
			t.Errorf("%s: Message-ID ohne saubere Domain:\n%s", tc.from, gotMsg)
		}
	}
}

// Der Modus erreicht den Versandkern unverändert.
func TestEmailSendReichtVerschluesselungDurch(t *testing.T) {
	for _, mode := range []string{domain.MailSecurityTLS, domain.MailSecurityStartTLS, domain.MailSecurityNone} {
		var got string
		p := NewEmailProvider(EmailConfig{Host: "h", Port: 465, From: "a@b.c", Security: mode}, "").
			withSender(func(_, security string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
				got = security
				return nil
			})
		if err := p.SendRaw("B", "R", []string{"x@y.z"}); err != nil {
			t.Fatal(err)
		}
		if got != mode {
			t.Errorf("Versandkern bekam %q, erwartet %q", got, mode)
		}
	}
}

// Absender, Empfänger und Host landen in Kopfzeilen und SMTP-Befehlen: nur
// einzelne, wohlgeformte Adressen und ein Host aus Hostnamen-Zeichen gehen durch.
func TestEmailValidateRejectsMalformedAddressesAndHost(t *testing.T) {
	base := EmailConfig{Host: "smtp.example.com", Port: 587, From: "lcm@example.com", Recipients: []string{"ops@example.com"}}
	cases := map[string]func(*EmailConfig){
		"verschlüsselung unbekannt":   func(c *EmailConfig) { c.Security = "ssl" },
		"absender zwei Adressen":      func(c *EmailConfig) { c.From = "a@example.com, b@example.com" },
		"absender Steuerzeichen":      func(c *EmailConfig) { c.From = "LCM\x00 <lcm@example.com>" },
		"absender mit Zeilenumbruch":  func(c *EmailConfig) { c.From = "lcm@example.com\r\nBcc: x@y.z" },
		"absender keine Adresse":      func(c *EmailConfig) { c.From = "lcm" },
		"empfänger keine Adresse":     func(c *EmailConfig) { c.Recipients = []string{"ops"} },
		"empfänger mit Zeilenumbruch": func(c *EmailConfig) { c.Recipients = []string{"a@b.c\nBcc: x@y.z"} },
		"empfänger zwei Adressen":     func(c *EmailConfig) { c.Recipients = []string{"a@b.c, d@e.f"} },
		"host mit Leerzeichen":        func(c *EmailConfig) { c.Host = "smtp example.com" },
		"host mit Schrägstrich":       func(c *EmailConfig) { c.Host = "smtp.example.com/x" },
		"host mit Zeilenumbruch":      func(c *EmailConfig) { c.Host = "smtp\r\nexample.com" },
		"zu viele Empfänger":          func(c *EmailConfig) { c.Recipients = manyRecipients(maxRecipients + 1) },
	}
	for name, mutate := range cases {
		cfg := base
		mutate(&cfg)
		if err := NewEmailProvider(cfg, "").Validate(); err == nil {
			t.Errorf("%s: Validate hätte ablehnen müssen", name)
		}
	}
	ok := base
	ok.Host = "[2001:db8::1]"
	ok.Recipients = manyRecipients(maxRecipients)
	if err := NewEmailProvider(ok, "").Validate(); err != nil {
		t.Errorf("Host in Klammern und höchstens %d Empfänger sind gültig: %v", maxRecipients, err)
	}
}

func manyRecipients(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "user" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "@example.com"
	}
	return out
}

// Kein Wert darf eine weitere Kopfzeile eröffnen.
func TestNachrichtKopfzeilenLassenKeineEinschleusungZu(t *testing.T) {
	from := &mail.Address{Name: "LCM\r\nBcc: evil@example.com", Address: "lcm@example.com"}
	msg := string(buildMessage(from, []string{"a@b.c\nBcc: evil@example.com"}, "Betreff\r\nBcc: evil@example.com", "Text"))
	head, _, _ := strings.Cut(msg, "\r\n\r\n")
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Errorf("eingeschmuggelte Kopfzeile: %q", line)
		}
	}
}
