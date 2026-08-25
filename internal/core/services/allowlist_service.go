package services

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// ErrIPAllowlistInvalid signalisiert eine fehlgeschlagene Validierung einer
// Allowlist (Name oder Einträge); die konkrete Ursache steckt in der Meldung.
var ErrIPAllowlistInvalid = errors.New("ungültige allowlist")

// validAllowlistName prüft den Anzeigenamen: nicht leer, höchstens 64 Zeichen,
// keine Steuerzeichen und keine Quotes/Backslashes/Backticks. Der Name fließt
// NICHT in Shell-Skripte ein (nur die kanonisierten Einträge tun das), Umlaute
// und andere Unicode-Buchstaben sind daher erlaubt.
func validAllowlistName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return false
	}
	return !strings.ContainsAny(name, "\"'`\\\n\r\t")
}

// WithIPAllowlists verdrahtet die benannte IP-Allowlist-Verwaltung.
func (s *SettingsService) WithIPAllowlists(repo *repositories.IPAllowlistRepository) *SettingsService {
	s.ipAllowlists = repo
	return s
}

// ListIPAllowlists liefert alle benannten Allowlists.
func (s *SettingsService) ListIPAllowlists() ([]domain.IPAllowlist, error) {
	return s.ipAllowlists.List()
}

// SaveIPAllowlist legt eine Allowlist an (ID 0) oder aktualisiert sie. Jeder
// Eintrag wird über canonicalAddr validiert (IP ODER CIDR, v4/v6) und in
// kanonischer Form dedupliziert gespeichert - so sind die Werte, die später in
// Firewall-/fail2ban-/CrowdSec-Konfigurationen einfließen, garantiert sauber.
// allowlistAuditDetail fasst eine Allowlist fürs Audit zusammen: Name +
// Anzahl der Einträge + eine gekürzte Liste. Vorher enthielt details nur den
// Namen - WAS zugelassen wurde, war nicht nachvollziehbar (R2-073).
func allowlistAuditDetail(name, entries string) string {
	list := strings.FieldsFunc(entries, func(r rune) bool { return r == '\n' || r == ',' })
	joined := strings.Join(list, ", ")
	const max = 200
	if len(joined) > max {
		joined = joined[:max] + " …"
	}
	return fmt.Sprintf("%s - %d Eintrag/Einträge: [%s]", name, len(list), joined)
}

func (s *SettingsService) SaveIPAllowlist(in domain.IPAllowlist, actor string) (*domain.IPAllowlist, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if !validAllowlistName(in.Name) {
		// Die Meldung benennt, was tatsächlich geprüft wird - „unerlaubte
		// Zeichen" behauptete eine strenge Zeichenklasse, die es hier bewusst
		// nicht gibt (der Name fließt nie in Shell-Skripte; R2-074).
		return nil, fmt.Errorf("%w: name fehlt, ist länger als 64 Zeichen oder enthält Anführungszeichen/Steuerzeichen", ErrIPAllowlistInvalid)
	}

	entries, err := normalizeAllowlistEntries(in.Entries)
	if err != nil {
		return nil, err
	}
	in.Entries = strings.Join(entries, "\n")

	// Namens-Eindeutigkeit vorab prüfen (klare Meldung statt DB-Constraint).
	if existing, err := s.ipAllowlists.FindByName(in.Name); err == nil && existing.ID != in.ID {
		return nil, fmt.Errorf("%w: name %q ist bereits vergeben", ErrIPAllowlistInvalid, in.Name)
	}

	if in.ID == 0 {
		if err := s.ipAllowlists.Create(&in); err != nil {
			return nil, err
		}
		s.audit.Log(actor, "settings.ip-allowlist.create", "ip_allowlist", in.ID, allowlistAuditDetail(in.Name, in.Entries))
		return &in, nil
	}
	existing, err := s.ipAllowlists.FindByID(in.ID)
	if err != nil {
		return nil, err
	}
	existing.Name = in.Name
	existing.Description = in.Description
	existing.Entries = in.Entries
	if err := s.ipAllowlists.Update(existing); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "settings.ip-allowlist.update", "ip_allowlist", existing.ID, allowlistAuditDetail(existing.Name, existing.Entries))
	return existing, nil
}

// ErrAllowlistInUse: die Allowlist wird von Firewall-Regeln referenziert -
// Löschen abgelehnt (409). Ohne diese Sperre riss das Löschen die
// verweisenden Regeln erst beim NÄCHSTEN turnusmäßigen Anwenden um: der
// Port fiel still weg, zeitlich entkoppelt von der Ursache (R2-072/R2-071).
var ErrAllowlistInUse = errors.New("die IP-Allowlist wird noch verwendet")

// DeleteIPAllowlist entfernt eine Allowlist - aber nur, wenn keine
// Firewall-Regel (Server oder Gruppen-Regel) mehr auf sie verweist. Die
// Verweise werden mit Namen genannt, damit der Betreiber weiß, wo er sie
// zuerst lösen muss (gleiche Löschsperre wie bei Custom Actions).
func (s *SettingsService) DeleteIPAllowlist(id uint, actor string) error {
	list, err := s.ipAllowlists.FindByID(id)
	if err != nil {
		return err
	}
	if s.ipAllowlistUsage != nil {
		if refs := s.ipAllowlistUsage(id); len(refs) > 0 {
			return fmt.Errorf("%w: %s - dort zuerst die Regel anpassen", ErrAllowlistInUse, strings.Join(refs, ", "))
		}
	}
	if err := s.ipAllowlists.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "settings.ip-allowlist.delete", "ip_allowlist", id, allowlistAuditDetail(list.Name, list.Entries))
	return nil
}

// IPAllowlistUsage sammelt alle Stellen, die eine Allowlist-ID referenzieren:
// Firewall-Regeln der Server (FirewallRules-JSON) und Firewall-Gruppen-Regeln
// (Command-JSON). Rückgabe: menschenlesbare Verweise für die Fehlermeldung.
func IPAllowlistUsage(servers *repositories.ServerRepository, groups *repositories.GroupRepository, id uint) []string {
	var refs []string
	if all, err := servers.FindAll(repositories.ScopeAll()); err == nil {
		for i := range all {
			for _, r := range firewallRulesFromServer(&all[i]) {
				if containsUint(r.AllowlistIDs, id) {
					refs = append(refs, fmt.Sprintf("Server %q (Firewall-Regel Port %d)", all[i].Name, r.Port))
					break
				}
			}
		}
	}
	if schedules, err := groups.FindAllSchedules(); err == nil {
		for _, sched := range schedules {
			for _, rule := range sched.Rules {
				if rule.Type != domain.RuleTypeFirewall {
					continue
				}
				rules, err := parseFirewallRuleSpec(rule.Command)
				if err != nil {
					continue
				}
				for _, r := range rules {
					if containsUint(r.AllowlistIDs, id) {
						refs = append(refs, fmt.Sprintf("Gruppen-Regel %q", rule.Name))
						break
					}
				}
			}
		}
	}
	return refs
}

func containsUint(list []uint, v uint) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ExpandIPAllowlists löst eine Menge von Allowlist-IDs in die Vereinigung
// ihrer (kanonischen) Einträge auf, dedupliziert und sortiert. Eine
// UNBEKANNTE ID ist ein FEHLER - früher wurde sie ausgelassen, wodurch
// eine gelöschte Liste je nach Pfad den Port still schloss (R2-071) oder
// den Aussperrschutz still aushebelte (R2-075); der Aufrufer erfuhr davon
// nichts. Eine existierende, aber leere Liste ist dagegen KEIN Fehler hier
// - die Aufrufer entscheiden über Warnung/fail-closed. Diese Funktion wird
// als Closure in die Firewall- und Security-Tool-Anwendung injiziert.
func (s *SettingsService) ExpandIPAllowlists(ids []uint) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	lists, err := s.ipAllowlists.FindByIDs(ids)
	if err != nil {
		return nil, err
	}
	found := map[uint]bool{}
	seen := map[string]bool{}
	var out []string
	for _, l := range lists {
		found[l.ID] = true
		for _, e := range l.EntryList() {
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	var missing []string
	for _, id := range ids {
		if !found[id] {
			missing = append(missing, fmt.Sprint(id))
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("IP-Allowlist nicht gefunden (gelöscht?): ID %s", strings.Join(missing, ", "))
	}
	sort.Strings(out)
	return out, nil
}

// normalizeAllowlistEntries validiert und kanonisiert die Einträge einer
// Allowlist (eine IP oder ein CIDR je Zeile), dedupliziert und sortiert.
func normalizeAllowlistEntries(raw string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t'
	}) {
		canon, _, err := canonicalAddr(line)
		if err != nil {
			// Eigene Meldung statt der von canonicalAddr: dessen
			// „Bind-Adresse" meint im selben Produkt das GEGENTEIL (die
			// lokale Lausch-Adresse einer Firewall-Regel) - ein
			// Allowlist-Eintrag ist eine QUELL-IP (R2-074).
			return nil, fmt.Errorf("%w: ungültige Quell-IP %q (eine IP-Adresse oder ein CIDR-Bereich je Zeile)", ErrIPAllowlistInvalid, line)
		}
		if !seen[canon] {
			seen[canon] = true
			out = append(out, canon)
		}
	}
	sort.Strings(out)
	return out, nil
}
