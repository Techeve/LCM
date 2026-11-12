package services

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/inputcheck"
	"LCM/internal/storage/repositories"
)

// DNS-Konfiguration & -Verfügbarkeitstest. Aufbau bewusst analog zum APT-Cache
// (aptproxy.go): ein Inline-Skript für den Voll-Sudo-Modus, ein validierendes
// Helper-Unterkommando (lcm-helper.sh, dns()) für den eingeschränkten Modus,
// jeweils mit Auflösungstest + Rollback. Der Test selbst ist rein lesend.

var (
	// ErrTooManyDNSServers: mehr als domain.MaxDNSServers Nameserver angefragt.
	ErrTooManyDNSServers = fmt.Errorf("höchstens %d DNS-Server erlaubt", domain.MaxDNSServers)
	// ErrInvalidDNSServer: ein Eintrag ist keine gültige IP-Adresse.
	ErrInvalidDNSServer = errors.New("ungültige DNS-Server-IP")
)

// reDNSDomain lässt nur unbedenkliche Domain-Zeichen zu (die Domains stammen aus
// den Einstellungen, werden aber wörtlich in ein Server-Skript eingesetzt).
var reDNSDomain = regexp.MustCompile(`^[A-Za-z0-9._-]{1,253}$`)

// validDNSServers validiert die gewünschten Nameserver: höchstens MaxDNSServers,
// jeder eine gültige IPv4/IPv6-Adresse. Liefert die bereinigte Liste zurück.
func validDNSServers(servers []string) ([]string, error) {
	var out []string
	for _, raw := range servers {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		if reason := inputcheck.IPProblem(ip); reason != "" {
			return nil, fmt.Errorf("%w: %q: %s", ErrInvalidDNSServer, ip, inputcheck.Describe(reason))
		}
		out = append(out, ip)
	}
	if len(out) > domain.MaxDNSServers {
		return nil, ErrTooManyDNSServers
	}
	return out, nil
}

// sanitizeDNSDomains filtert die Test-Domains auf unbedenkliche Zeichen.
// ErrInvalidDNSTestDomain: In der Domainliste einer DNS-Test-Regel steht ein
// Eintrag, der kein Hostname ist.
var ErrInvalidDNSTestDomain = errors.New("ungültige test-domain")

// normalizeDNSTestDomains prüft die eigenen Domains einer DNS-Test-Regel und
// legt sie zeilenweise ab. Leer bleibt leer - dann gelten die Domains aus
// den Einstellungen. Ein ungültiger Eintrag wird benannt statt still
// verworfen: Sonst prüfte die Regel weniger, als ihr Betreiber glaubt.
func normalizeDNSTestDomains(raw string) (string, error) {
	list := domain.SplitDomainList(raw)
	for _, d := range list {
		if !reDNSDomain.MatchString(d) {
			return "", fmt.Errorf("%w: %q", ErrInvalidDNSTestDomain, d)
		}
	}
	return strings.Join(list, "\n"), nil
}

// ruleTestDomains liefert die Domains, die eine DNS-Test-Regel prüft: ihre
// eigenen, sonst die aus den Einstellungen.
func ruleTestDomains(rule *domain.Rule, settings *domain.GlobalSettings) []string {
	if own := domain.SplitDomainList(rule.Command); len(own) > 0 {
		return sanitizeDNSDomains(own)
	}
	return sanitizeDNSDomains(settings.DNSTestDomainList())
}

func sanitizeDNSDomains(domains []string) []string {
	var out []string
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if reDNSDomain.MatchString(d) {
			out = append(out, d)
		}
	}
	return out
}

// dnsApplyScript setzt die Nameserver wirksam (dnsShellFuncs: Drop-in für
// systemd-resolved oder /etc/resolv.conf, dazu Schutz vor dem Überschreiben
// durch Proxmox und DHCP-Clients) und verifiziert die Auflösung mit
// testDomain; scheitert der Test, wird die Änderung zurückgerollt (exit 1).
// Leere Server-Liste = Rückbau der LCM-Verwaltung (dnsUnmanageScript).
func dnsApplyScript(servers []string, testDomain string) string {
	if len(servers) == 0 {
		return dnsUnmanageScript()
	}
	if testDomain == "" {
		testDomain = "deb.debian.org"
	}
	return dnsShellFuncs + "\nlcm_dns_set " + shellQuote(strings.Join(servers, " ")) + " " +
		shellQuote(testDomain) + " || exit 1"
}

// dnsUnmanageScript entfernt die LCM-DNS-Verwaltung wieder und stellt den
// Zustand vor der ersten Verwaltung her.
func dnsUnmanageScript() string {
	return dnsShellFuncs + "\nlcm_dns_unset"
}

// dnsCurrentScript liest die Resolver, die Anwendungen tatsächlich fragen.
// Maßgeblich ist /etc/resolv.conf: Steht dort der Stub von systemd-resolved,
// zählen dessen Server, bei Domains=~. nur die globalen. Sonst gelten die
// nameserver-Zeilen der Datei, davon liest glibc höchstens drei. Wer nur
// resolvectl fragt, sieht Server, die keine Anwendung erreicht, sobald die
// Datei an systemd-resolved vorbeiführt. Ausgabe: IPs, leerzeichengetrennt.
func dnsCurrentScript() string {
	return `ip_only() { tr ' ' '\n' | sed 's/[#%].*//' | grep -E '^[0-9a-fA-F.:]+$' | awk '!seen[$0]++' | tr '\n' ' '; }
ns=$(awk '/^nameserver/ {print $2}' /etc/resolv.conf 2>/dev/null | tr '\n' ' ')
out=""
case " $ns " in
  *" 127.0.0.53 "*|*" 127.0.0.54 "*)
    if command -v resolvectl >/dev/null 2>&1; then
      if resolvectl domain 2>/dev/null | grep -E '^Global:' | grep -q '~\.'; then
        out=$(resolvectl dns 2>/dev/null | sed -n 's/^Global: *//p' | ip_only)
      fi
      [ -n "$(printf %s "$out" | tr -d ' ')" ] || out=$(resolvectl dns 2>/dev/null | sed 's/^[^:]*: *//' | ip_only)
    fi ;;
  *) out=$(printf %s "$ns" | ip_only | awk '{print $1, $2, $3}') ;;
esac
printf '%s\n' "$out"`
}

// dnsTestScript prüft je Domain die Auflösbarkeit (getent, Fallback nslookup)
// und gibt "OK <domain>" bzw. "FAIL <domain>" aus. Rein lesend, kein sudo.
func dnsTestScript(domains []string) string {
	quoted := make([]string, 0, len(domains))
	for _, d := range domains {
		quoted = append(quoted, shellQuote(d))
	}
	return fmt.Sprintf(`for d in %s; do
  if getent hosts "$d" >/dev/null 2>&1 || nslookup "$d" >/dev/null 2>&1; then
    echo "OK $d"
  else
    echo "FAIL $d"
  fi
done`, strings.Join(quoted, " "))
}

// parseDNSTest wertet die Ausgabe von dnsTestScript aus: dreistufiger Status
// (full/partial/none) plus eine menschenlesbare Detailzeile.
func parseDNSTest(out string) (status, detail string) {
	var ok, fail []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "OK "):
			ok = append(ok, strings.TrimSpace(strings.TrimPrefix(line, "OK ")))
		case strings.HasPrefix(line, "FAIL "):
			fail = append(fail, strings.TrimSpace(strings.TrimPrefix(line, "FAIL ")))
		}
	}
	total := len(ok) + len(fail)
	switch {
	case total == 0:
		status = ""
	case len(fail) == 0:
		status = domain.DNSTestFull
	case len(ok) == 0:
		status = domain.DNSTestNone
	default:
		status = domain.DNSTestPartial
	}
	var parts []string
	if len(ok) > 0 {
		parts = append(parts, "OK: "+strings.Join(ok, ", "))
	}
	if len(fail) > 0 {
		parts = append(parts, "FAIL: "+strings.Join(fail, ", "))
	}
	return status, strings.Join(parts, " | ")
}

// parseDNSList normalisiert die leerzeichengetrennte Resolver-Ausgabe zu einer
// kommagetrennten Liste.
func parseDNSList(out string) string {
	return strings.Join(strings.Fields(out), ", ")
}

// dnsScanFields führt den lesenden DNS-Teil eines Scans aus: aktive Resolver
// auslesen und (sofern Test-Domains gepflegt sind) den Auflösungstest. Liefert
// die zu speichernden Server-Felder. Best effort - schlägt ein Kommando fehl,
// bleibt der gespeicherte Wert unangetastet statt fälschlich geleert zu werden.
// Rein lesend und sudo-frei, daher in jedem Modus (auch eingeschränkt) nutzbar.
func dnsScanFields(conn sshx.Conn, domains []string) map[string]any {
	fields := map[string]any{}
	if cur, _, err := conn.Run(dnsCurrentScript()); err == nil {
		fields["dns_current"] = parseDNSList(cur)
	}
	if len(domains) == 0 {
		return fields
	}
	if out, _, err := conn.Run(dnsTestScript(domains)); err == nil {
		status, detail := parseDNSTest(out)
		fields["dns_test_status"] = status
		fields["dns_test_at"] = time.Now()
		fields["dns_test_detail"] = detail
	}
	return fields
}

// DNSTestResult ist das Ergebnis eines DNS-Verfügbarkeitstests.
type DNSTestResult struct {
	Status  string `json:"status"` // full/partial/none/"" (nichts geprüft)
	Detail  string `json:"detail"`
	Current string `json:"current"` // aktive Resolver (kommagetrennt)
	Output  string `json:"output"`  // roher Konsolen-Output
}

// ConfigureDNS setzt bis zu vier Nameserver auf dem Server (leere Liste = LCM
// gibt die DNS-Verwaltung wieder frei). Synchrone Aktion mit Rollback bei
// gebrochener Auflösung, Muster ConfigureAptProxy.
func (s *ServerService) ConfigureDNS(scope repositories.AccessScope, id uint, servers []string, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	script, clean, _, err := s.planDNS(server, servers)
	if err != nil {
		return "", err
	}

	conn, err := s.connectRec(server, "dns", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	output, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return output, runErr
	}
	if code != 0 {
		return output, fmt.Errorf("DNS setzen fehlgeschlagen (exit %d)", code)
	}

	fields := map[string]any{"dns_servers": strings.Join(clean, ",")}
	// Aktive Resolver frisch nachlesen (unprivilegiert).
	if cur, _, err := conn.Run(dnsCurrentScript()); err == nil {
		fields["dns_current"] = parseDNSList(cur)
	}
	_ = s.servers.UpdateFields(id, fields)
	s.audit.Log(actor, "server.dns", "server", id, server.Name+": "+strings.Join(clean, ","))
	return output, nil
}

// planDNS prüft Server und Nameserver und baut das Skript - für den Lauf und
// die Befehlsvorschau gleich. Zurück kommen Skript, bereinigte Nameserver und
// die Test-Domain, an der die Auflösung belegt wird.
func (s *ServerService) planDNS(server *domain.Server, servers []string) (string, []string, string, error) {
	// RouterOS hat kein resolv.conf/systemd-resolved - sauber ablehnen statt
	// ein Linux-Shell-Skript in die RouterOS-CLI zu schicken.
	if err := ensureNotRouterOS(server); err != nil {
		return "", nil, "", err
	}
	clean, err := validDNSServers(servers)
	if err != nil {
		return "", nil, "", err
	}
	testDomain := "deb.debian.org"
	if doms := sanitizeDNSDomains(s.dnsTestDomainList()); len(doms) > 0 {
		testDomain = doms[0]
	}
	if server.RestrictedSudo {
		// Eingeschränkter Modus: der Helper schreibt DNS validiert (gleiche
		// Logik samt Auflösungstest + Rollback).
		arg := "off"
		if len(clean) > 0 {
			arg = strings.Join(clean, ",")
		}
		return helperCmd("dns", arg, testDomain), clean, testDomain, nil
	}
	return dnsApplyScript(clean, testDomain), clean, testDomain, nil
}

// DNSTest prüft die Auflösbarkeit der gepflegten Test-Domains auf dem Server und
// speichert das dreistufige Ergebnis. Rein lesend - auch im eingeschränkten
// Modus nutzbar. Muster ConfigureFirewall (synchrone Aktion).
func (s *ServerService) DNSTest(scope repositories.AccessScope, id uint, actor string) (*DNSTestResult, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	// Wie ConfigureDNS: die Testkommandos sind Linux-Shell, kein RouterOS.
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	domains := sanitizeDNSDomains(s.dnsTestDomainList())
	if len(domains) == 0 {
		return nil, errors.New("keine Test-Domains gepflegt - bitte unter Einstellungen → DNS hinterlegen")
	}

	conn, err := s.connectRecRead(server, "dns-test", actor)
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	output, _, runErr := conn.Run(dnsTestScript(domains))
	if runErr != nil {
		return nil, runErr
	}
	status, detail := parseDNSTest(output)
	result := &DNSTestResult{Status: status, Detail: detail, Output: output}
	if cur, _, err := conn.Run(dnsCurrentScript()); err == nil {
		result.Current = parseDNSList(cur)
	}

	now := time.Now()
	_ = s.servers.UpdateFields(id, map[string]any{
		"dns_test_status": status,
		"dns_test_at":     now,
		"dns_test_detail": detail,
		"dns_current":     result.Current,
	})
	s.audit.Log(actor, "server.dns-test", "server", id, server.Name+": "+status)
	return result, nil
}
