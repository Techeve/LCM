// Package notify implementiert das generische Provider-Modell des
// Notification-Service. Ein Provider übersetzt ein standardisiertes
// domain.NotificationEvent in sein Zielformat und versendet es. Der MVP
// enthält den E-Mail-Provider (SMTP); weitere Kanäle (Webhook, SMS, …)
// lassen sich durch Implementierung des Provider-Interfaces ergänzen.
package notify

import (
	"errors"
	"fmt"

	"LCM/internal/core/domain"
)

// ErrUnknownChannelType wird gemeldet, wenn für einen Kanal-Typ kein
// Provider registriert ist.
var ErrUnknownChannelType = errors.New("unbekannter benachrichtigungs-kanaltyp")

// Provider ist die generische Kanal-Schnittstelle des Notification-Service.
// Jede konkrete Implementierung (E-Mail, …) erfüllt dieses Interface.
type Provider interface {
	// Type liefert den Kanal-Typ (domain.ChannelType*).
	Type() string
	// Validate prüft die Konfiguration des Providers auf Vollständigkeit
	// und Plausibilität, ohne etwas zu versenden.
	Validate() error
	// Send übersetzt das standardisierte Event ins Zielformat und versendet es.
	Send(event domain.NotificationEvent) error
}

// New baut aus einem Kanal (nicht-sensible Config als JSON) und dem bereits
// entschlüsselten Secret den passenden Provider. Neue Kanal-Typen werden
// hier registriert.
func New(channel domain.NotificationChannel, secret string) (Provider, error) {
	switch channel.Type {
	case domain.ChannelTypeEmail:
		cfg, err := parseEmailConfig(channel.Config)
		if err != nil {
			return nil, err
		}
		return NewEmailProvider(cfg, secret), nil
	case domain.ChannelTypeWebhook:
		cfg, err := parseWebhookConfig(channel.Config)
		if err != nil {
			return nil, err
		}
		// Das Secret ist hier die Ziel-URL (die URL ist das Credential).
		return NewWebhookProvider(cfg, secret), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownChannelType, channel.Type)
	}
}
