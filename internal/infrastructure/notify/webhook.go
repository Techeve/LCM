package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"LCM/internal/core/domain"
)

// Webhook-Formate: generisches JSON (das rohe Event plus Betreffzeile) oder
// Microsoft Teams (Adaptive Card für eine Teams-Workflows-URL).
const (
	WebhookFormatGeneric = "generic"
	WebhookFormatTeams   = "teams"
)

// WebhookConfig ist die nicht-sensible Konfiguration eines Webhook-Kanals.
// Die Ziel-URL ist das Credential (Teams-Webhook-URLs sind Geheimnisse) und
// wird deshalb getrennt, verschlüsselt gehalten - nicht hier.
type WebhookConfig struct {
	Format string `json:"format"` // WebhookFormat*
}

// parseWebhookConfig liest die JSON-Konfiguration eines Webhook-Kanals.
func parseWebhookConfig(raw string) (WebhookConfig, error) {
	var cfg WebhookConfig
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, fmt.Errorf("webhook-konfiguration ungültig: %w", err)
	}
	return cfg, nil
}

// WebhookProvider versendet Events per HTTP-POST an eine konfigurierte URL.
// Härtung: HTTPS-Pflicht (Ausnahme: Loopback für lokale Tests), kurzer
// Timeout, kein Redirect-Following.
type WebhookProvider struct {
	cfg    WebhookConfig
	url    string
	client *http.Client
}

// NewWebhookProvider erstellt einen Webhook-Provider aus Konfiguration und
// (bereits entschlüsselter) Ziel-URL.
func NewWebhookProvider(cfg WebhookConfig, targetURL string) *WebhookProvider {
	return &WebhookProvider{
		cfg: cfg,
		url: strings.TrimSpace(targetURL),
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Redirects wären ein Weg, den Request auf ein anderes Ziel
			// umzulenken - bewusst verboten.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("webhook: redirects sind nicht erlaubt")
			},
		},
	}
}

// Type liefert den Kanal-Typ.
func (p *WebhookProvider) Type() string { return domain.ChannelTypeWebhook }

// Validate prüft Format und Ziel-URL. HTTP (unverschlüsselt) ist nur für
// Loopback-Adressen erlaubt - Webhook-Payloads können sensible Serverdaten
// enthalten und die URL selbst ist ein Credential.
func (p *WebhookProvider) Validate() error {
	switch p.cfg.Format {
	case WebhookFormatGeneric, WebhookFormatTeams:
	case "":
		return errors.New("webhook-format fehlt (generic oder teams)")
	default:
		return fmt.Errorf("unbekanntes webhook-format %q", p.cfg.Format)
	}
	if p.url == "" {
		return errors.New("webhook-url fehlt")
	}
	u, err := url.Parse(p.url)
	if err != nil {
		return fmt.Errorf("webhook-url ungültig: %w", err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return errors.New("webhook-url muss https verwenden (http nur für localhost)")
		}
	default:
		return errors.New("webhook-url muss mit https:// beginnen")
	}
	if u.Host == "" {
		return errors.New("webhook-url ungültig: host fehlt")
	}
	return nil
}

// Send baut die Payload im konfigurierten Format und POSTet sie an die URL.
func (p *WebhookProvider) Send(event domain.NotificationEvent) error {
	if err := p.Validate(); err != nil {
		return err
	}
	var payload any
	if p.cfg.Format == WebhookFormatTeams {
		payload = teamsPayload(event)
	} else {
		payload = genericPayload(event)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := p.client.Post(p.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook-versand: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook-versand: ziel antwortete mit status %d", resp.StatusCode)
	}
	return nil
}

// genericPayload ist das rohe Event plus vorformatierter Betreffzeile -
// für eigene Empfänger (Automationen, selbstgebaute Receiver).
func genericPayload(event domain.NotificationEvent) any {
	return struct {
		domain.NotificationEvent
		Subject string `json:"subject"`
	}{NotificationEvent: event, Subject: RenderSubject(event)}
}

// teamsPayload baut eine Adaptive Card im Umschlag, den Microsoft-Teams-
// Workflows-URLs (Power Automate, „When a Teams webhook request is
// received") erwarten. Die klassischen O365-Connector-Webhooks sind
// abgekündigt; dieses Format deckt den aktuellen Weg ab.
func teamsPayload(event domain.NotificationEvent) any {
	color := "Good"
	switch event.Severity {
	case domain.AlertSeverityCritical:
		color = "Attention"
	case domain.AlertSeverityWarning:
		color = "Warning"
	}

	facts := []map[string]string{
		{"title": "Dringlichkeit", "value": severityLabel(event.Severity)},
	}
	if event.ServerGroup != "" {
		facts = append(facts, map[string]string{"title": "Servergruppe", "value": event.ServerGroup})
	}
	if event.ServerName != "" {
		facts = append(facts, map[string]string{"title": "Server", "value": event.ServerName})
	}
	if event.Code != "" {
		facts = append(facts, map[string]string{"title": "Code", "value": event.Code})
	}
	ts := event.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	facts = append(facts, map[string]string{"title": "Zeitpunkt", "value": ts.Format("2006-01-02 15:04:05 MST")})

	body := []map[string]any{
		{"type": "TextBlock", "size": "Medium", "weight": "Bolder", "color": color,
			"text": RenderSubject(event), "wrap": true},
		{"type": "FactSet", "facts": facts},
		{"type": "TextBlock", "text": event.Description, "wrap": true},
	}
	if event.Details != "" {
		body = append(body, map[string]any{"type": "TextBlock", "text": event.Details,
			"wrap": true, "isSubtle": true, "spacing": "Small"})
	}

	return map[string]any{
		"type": "message",
		"attachments": []map[string]any{{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content": map[string]any{
				"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
				"type":    "AdaptiveCard",
				"version": "1.4",
				"msteams": map[string]string{"width": "Full"},
				"body":    body,
			},
		}},
	}
}
