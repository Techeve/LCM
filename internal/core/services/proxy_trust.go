package services

import (
	"fmt"
	"log/slog"

	"LCM/internal/core/domain"
	"LCM/internal/netfilter"
)

// WithProxyTrust verdrahtet die Vertrauensregel für X-Forwarded-For, die die
// Middleware bei jeder Anfrage liest, und setzt sie aus den gespeicherten
// Einstellungen. Ein späteres Speichern von trust_proxy_header oder
// trusted_proxies tauscht sie aus, ohne Neustart.
func (s *SettingsService) WithProxyTrust(holder *netfilter.ProxyTrustHolder) (*SettingsService, error) {
	s.proxyTrust = holder
	st, err := s.settings.Get()
	if err != nil {
		return s, err
	}
	trust, err := proxyTrustOf(st)
	if err != nil {
		return s, err
	}
	holder.Store(trust)
	return s, nil
}

// WithConfigSync verdrahtet das Schreiben der gespiegelten Einstellungen in die
// config.json nach jedem Speichern.
func (s *SettingsService) WithConfigSync(fn func() error) *SettingsService {
	s.configSync = fn
	return s
}

// syncConfig schreibt die config.json nach einem Speichern. Ein Fehler ist
// nicht fatal: Die Datenbank gilt, LCM warnt, und der Text geht an den Aufrufer
// (config_sync_error in der Antwort). Leer, wenn alles geklappt hat.
func (s *SettingsService) syncConfig() string {
	if s.configSync == nil {
		return ""
	}
	if err := s.configSync(); err != nil {
		slog.Warn("config.json konnte nicht mit den Einstellungen abgeglichen werden", "error", err)
		return err.Error()
	}
	return ""
}

// SyncConfig gleicht die config.json mit den gespeicherten Einstellungen ab.
func (s *SettingsService) SyncConfig() error {
	if s.configSync == nil {
		return nil
	}
	return s.configSync()
}

// publishProxyTrust gibt die gespeicherte Vertrauensregel an die Middleware.
func (s *SettingsService) publishProxyTrust(st *domain.GlobalSettings) {
	if s.proxyTrust == nil {
		return
	}
	trust, err := proxyTrustOf(st)
	if err != nil {
		// Unerreichbar: applyProxyTrustInput hat die Liste geprüft.
		slog.Error("trusted_proxies nicht lesbar - die bisherige Regel bleibt", "error", err)
		return
	}
	s.proxyTrust.Store(trust)
}

func proxyTrustOf(st *domain.GlobalSettings) (netfilter.ProxyTrust, error) {
	proxies, err := netfilter.Parse(st.TrustedProxies)
	if err != nil {
		return netfilter.ProxyTrust{}, fmt.Errorf("trusted_proxies: %w", err)
	}
	return netfilter.ProxyTrust{Enabled: st.TrustProxyHeader, Proxies: proxies}, nil
}

// applyProxyTrustInput übernimmt trust_proxy_header und trusted_proxies aus dem
// PATCH. Jeder Eintrag muss eine IP-Adresse, ein Netz in CIDR-Schreibweise oder
// eines der Wörter localhost und private sein; ein Bereich, der alle Adressen
// umfasst, ist verboten. Eingeschaltet braucht der Schalter mindestens einen
// Eintrag - ohne Liste vertraute LCM niemandem, und der Schalter täuschte eine
// Einstellung vor, die nichts bewirkt. Geprüft wird nur, was dieser PATCH
// anfasst: Ein älterer Stand blockiert das Speichern anderer Felder nicht.
func applyProxyTrustInput(in GlobalSettingsInput, st *domain.GlobalSettings, touch func(string)) error {
	if in.TrustedProxies != nil {
		list, err := netfilter.NormalizeProxies(*in.TrustedProxies)
		if err != nil {
			return fmt.Errorf("%w: trusted_proxies: %v", ErrSettingInvalid, err)
		}
		st.TrustedProxies = list
		touch("trusted_proxies")
	}
	if in.TrustProxyHeader != nil {
		st.TrustProxyHeader = *in.TrustProxyHeader
		touch("trust_proxy_header")
	}
	touched := in.TrustedProxies != nil || in.TrustProxyHeader != nil
	if touched && st.TrustProxyHeader && len(st.TrustedProxies) == 0 {
		return fmt.Errorf("%w: trust_proxy_header braucht mindestens einen Eintrag in trusted_proxies - "+
			"sonst glaubt LCM keinem Proxy und der Schalter bewirkt nichts", ErrSettingInvalid)
	}
	return nil
}
