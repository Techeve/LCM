package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
)

func TestWebhookValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     WebhookConfig
		url     string
		wantErr string // "" = gültig
	}{
		{"gültig https+generic", WebhookConfig{Format: WebhookFormatGeneric}, "https://hooks.example.com/x", ""},
		{"gültig https+teams", WebhookConfig{Format: WebhookFormatTeams}, "https://prod.westeurope.logic.azure.com/workflows/abc", ""},
		{"gültig http localhost", WebhookConfig{Format: WebhookFormatGeneric}, "http://localhost:9999/hook", ""},
		{"format fehlt", WebhookConfig{}, "https://hooks.example.com/x", "webhook-format fehlt"},
		{"format unbekannt", WebhookConfig{Format: "slack"}, "https://hooks.example.com/x", "unbekanntes webhook-format"},
		{"url fehlt", WebhookConfig{Format: WebhookFormatGeneric}, "", "webhook-url fehlt"},
		{"http remote verboten", WebhookConfig{Format: WebhookFormatGeneric}, "http://hooks.example.com/x", "muss https verwenden"},
		{"falsches schema", WebhookConfig{Format: WebhookFormatGeneric}, "ftp://hooks.example.com/x", "muss mit https://"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewWebhookProvider(tc.cfg, tc.url).Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("erwartete gültige Konfiguration, bekam: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("erwarteter Fehler %q, bekam: %v", tc.wantErr, err)
			}
		})
	}
}

// TestWebhookSendGeneric prüft die generische JSON-Payload gegen einen
// lokalen Test-Receiver (http auf Loopback ist dafür erlaubt).
func TestWebhookSendGeneric(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("erwarteter JSON-POST, bekam %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// httptest bindet auf 127.0.0.1 - von Validate als Loopback akzeptiert.
	p := NewWebhookProvider(WebhookConfig{Format: WebhookFormatGeneric}, srv.URL)
	if err := p.Send(sampleEvent()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got["severity"] != "critical" || got["server_name"] != "db-01" {
		t.Errorf("payload unvollständig: %v", got)
	}
	subject, _ := got["subject"].(string)
	if !strings.Contains(subject, "KRITISCH") {
		t.Errorf("betreff fehlt/falsch: %q", subject)
	}
}

// TestWebhookSendTeams prüft den Adaptive-Card-Umschlag für Teams-Workflows.
func TestWebhookSendTeams(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewWebhookProvider(WebhookConfig{Format: WebhookFormatTeams}, srv.URL)
	if err := p.Send(sampleEvent()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got["type"] != "message" {
		t.Fatalf("teams-umschlag fehlt: %v", got)
	}
	attachments, _ := got["attachments"].([]any)
	if len(attachments) != 1 {
		t.Fatalf("erwartete genau ein attachment, bekam %d", len(attachments))
	}
	att, _ := attachments[0].(map[string]any)
	if att["contentType"] != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("contentType falsch: %v", att["contentType"])
	}
	card, _ := att["content"].(map[string]any)
	if card["type"] != "AdaptiveCard" {
		t.Errorf("adaptive card fehlt: %v", card)
	}
}

// TestWebhookSendNonOKStatus: Nicht-2xx-Antworten sind ein Versandfehler.
func TestWebhookSendNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	p := NewWebhookProvider(WebhookConfig{Format: WebhookFormatGeneric}, srv.URL)
	err := p.Send(sampleEvent())
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("erwarteter Status-Fehler, bekam: %v", err)
	}
}

// TestWebhookSendRefusesRedirect: Redirects werden nicht verfolgt (die
// Payload darf nicht auf ein anderes Ziel umgelenkt werden).
func TestWebhookSendRefusesRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/undressed", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	p := NewWebhookProvider(WebhookConfig{Format: WebhookFormatGeneric}, srv.URL)
	err := p.Send(sampleEvent())
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("erwarteter Redirect-Fehler, bekam: %v", err)
	}
}

// TestEmailSendRaw: der transaktionale Versandkern nutzt explizite
// Empfänger und ignoriert die konfigurierten (Admin-)Empfänger.
func TestEmailSendRaw(t *testing.T) {
	var gotTo []string
	var gotMsg string
	p := NewEmailProvider(EmailConfig{
		Host: "smtp.example.com", Port: 587, From: "lcm@example.com",
		Recipients: []string{"admin@example.com"},
	}, "geheim").withSender(func(_ string, _ smtp.Auth, _ string, to []string, msg []byte) error {
		gotTo = to
		gotMsg = string(msg)
		return nil
	})

	if err := p.SendRaw("Betreff", "Rumpf", []string{"user@example.com"}); err != nil {
		t.Fatalf("SendRaw: %v", err)
	}
	if len(gotTo) != 1 || gotTo[0] != "user@example.com" {
		t.Errorf("empfänger falsch (konfigurierte dürfen nicht greifen): %v", gotTo)
	}
	if !strings.Contains(gotMsg, "Subject: Betreff") || !strings.Contains(gotMsg, "Rumpf") {
		t.Errorf("nachricht unvollständig: %q", gotMsg)
	}

	if err := p.SendRaw("x", "y", nil); err == nil {
		t.Error("SendRaw ohne Empfänger muss fehlschlagen")
	}
}
