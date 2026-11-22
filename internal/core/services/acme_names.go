package services

import (
	"log/slog"
	"slices"
	"strings"

	"LCM/internal/core/domain"
)

// Die Namen im ACME-Zertifikat kommen aus den öffentlichen Adressen, nicht aus
// einem eigenen Feld. Let's Encrypt stellt immer EIN Zertifikat für alle
// Namen aus (Modus single); die Spalten acme_names und acme_mode der älteren
// Versionen lesen wir nur noch für den Umstieg (namesNotice, MigrateNames).

// ACMENames sind die DNS-Namen, die das Zertifikat tragen soll: die
// verschiedenen Namen der wirksamen Adressen von Oberfläche, Agent und MCP, in
// dieser Reihenfolge. Agent und MCP zählen nur, solange sie eingeschaltet sind:
// Ein abgeschalteter Dienst mit eigenem Namen soll die Ausstellung nicht an
// einem Namen scheitern lassen, den niemand mehr pflegt.
func (s *SettingsService) ACMENames(st *domain.GlobalSettings) []string {
	a := s.AddressesOf(st)
	return acmeNamesOf(a.URLs, a.AgentEnabled, a.MCPEnabled)
}

func acmeNamesOf(urls EffectiveURLs, agentEnabled, mcpEnabled bool) []string {
	hosts := []string{sniHost(urls.UI)}
	if agentEnabled {
		hosts = append(hosts, sniHost(urls.Agent))
	}
	if mcpEnabled {
		hosts = append(hosts, sniHost(urls.MCP))
	}
	names := []string{}
	for _, host := range hosts {
		host = strings.ToLower(host)
		// Eine IP-Adresse oder ein Name ohne Punkt (localhost) bekommt von
		// keiner öffentlichen CA ein Zertifikat.
		if checkACMEName(host, ACMEChallengeDNS01) != nil || slices.Contains(names, host) {
			continue
		}
		names = append(names, host)
	}
	return names
}

// ACMENamesNotice sagt, dass sich die Namen im Zertifikat beim Umstieg
// ändern: Old sind die Namen, für die ältere Versionen ausstellten, New die
// aus den Adressen. WasPerName: Es gab je Name ein Zertifikat; jetzt gibt es
// eines für alle.
type ACMENamesNotice struct {
	Old        []string `json:"old"`
	New        []string `json:"new"`
	WasPerName bool     `json:"was_per_name"`
}

// namesNotice liefert den Hinweis zum Umstieg, solange der Admin ihn nicht
// bestätigt hat (AckNamesNotice) und das Zertifikat sich wirklich ändert; nil
// sonst. Ein gespeicherter acme_names ist der Rest der älteren Version: Nach
// Bestätigung, Speichern der Einstellungen oder MigrateNames ist er leer.
//
// Der Hinweis kommt, sobald die alten Namen nicht alle in den Adressen stehen
// (auch wenn aus den Adressen noch gar kein Name folgt) oder es je Name ein
// Zertifikat gab. Solange er steht, stellt LCM auch für die alten Namen aus
// (siehe runNames).
func (s *ACMEService) namesNotice(st *domain.GlobalSettings) *ACMENamesNotice {
	old := splitNames(st.ACMENames)
	if !st.ACMEEnabled || len(old) == 0 {
		return nil
	}
	derived := s.namesOf(st)
	// Je Name ein Zertifikat mit nur einem Namen ist dasselbe wie eines für alle.
	wasPerName := st.ACMEMode == ACMEModePerName && len(old) > 1
	if !wasPerName && coversAll(derived, old) {
		return nil
	}
	return &ACMENamesNotice{Old: old, New: derived, WasPerName: wasPerName}
}

// coversAll: Jeder Name aus names steht in have.
func coversAll(have, names []string) bool {
	for _, name := range names {
		if !slices.Contains(have, name) {
			return false
		}
	}
	return true
}

// namesOf sind die Namen aus den Adressen. Ohne Quelle (nicht verdrahtet) gibt
// es keine.
func (s *ACMEService) namesOf(st *domain.GlobalSettings) []string {
	if s.names == nil {
		return []string{}
	}
	return s.names(st)
}

// runNames sind die Namen für einen Lauf: die aus den Adressen, dazu die alten
// gespeicherten, solange der Umstiegshinweis steht. So verliert eine
// Erneuerung nie still einen Namen, den eine ältere Version eingetragen hat
// (etwa bei einer Installation ohne public_base_url).
func (s *ACMEService) runNames(st *domain.GlobalSettings) []string {
	names := slices.Clone(s.namesOf(st))
	if s.namesNotice(st) == nil {
		return names
	}
	for _, name := range splitNames(st.ACMENames) {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// legacyNamesCleared sind die Werte, die den Umstieg abschließen: kein
// gespeicherter Name mehr, immer ein Zertifikat für alle.
func legacyNamesCleared() map[string]any {
	return map[string]any{"acme_names": "", "acme_mode": ACMEModeSingle}
}

// AckNamesNotice bestätigt den Hinweis zum Umstieg: Die Namen aus den
// Adressen gelten, der alte Stand ist vergessen.
func (s *ACMEService) AckNamesNotice(actor string) (ACMEView, error) {
	s.mu.Lock()
	err := s.settings.UpdateFields(legacyNamesCleared())
	s.mu.Unlock()
	if err != nil {
		return ACMEView{}, err
	}
	s.audit.Log(actor, "settings.acme.notice_ack", "settings", 1, "namen aus den adressen")
	return s.View()
}

// MigrateNames schließt den Umstieg beim Start ab, wo es nichts zu melden gibt:
// ist ACME aus oder stehen alle alten Namen in den Adressen, vergisst es die
// alten Werte. Sonst bleiben sie liegen: für den Hinweis und für die Läufe.
func (s *ACMEService) MigrateNames() {
	st, err := s.settings.Get()
	if err != nil {
		slog.Warn("acme settings not readable", "error", err)
		return
	}
	if st.ACMENames == "" || s.namesNotice(st) != nil {
		return
	}
	if err := s.settings.UpdateFields(legacyNamesCleared()); err != nil {
		slog.Warn("acme legacy names not cleared", "error", err)
	}
}
