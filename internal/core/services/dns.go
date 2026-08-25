package services

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// DNS-Konfiguration & -Verfügbarkeitstest. Aufbau bewusst analog zum APT-Cache
// (aptproxy.go): ein Inline-Skript für den Voll-Sudo-Modus, ein validierendes
// Helper-Unterkommando (lcm_helper.go, dns()) für den eingeschränkten Modus,
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

// dnsResolvConf/dnsResolvedDropin sind die von LCM verwalteten Pfade.
const (
	dnsResolvConf     = "/etc/resolv.conf"
	dnsResolvedDropin = "/etc/systemd/resolved.conf.d/lcm-dns.conf"
)

// validDNSServers validiert die gewünschten Nameserver: höchstens MaxDNSServers,
// jeder eine gültige IPv4/IPv6-Adresse. Liefert die bereinigte Liste zurück.
func validDNSServers(servers []string) ([]string, error) {
	var out []string
	for _, raw := range servers {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		if net.ParseIP(ip) == nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidDNSServer, ip)
		}
		out = append(out, ip)
	}
	if len(out) > domain.MaxDNSServers {
		return nil, ErrTooManyDNSServers
	}
	return out, nil
}

// sanitizeDNSDomains filtert die Test-Domains auf unbedenkliche Zeichen.
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

// dnsApplyScript schreibt die Nameserver auf den Server (systemd-resolved-Drop-in
// wenn aktiv, sonst /etc/resolv.conf) und verifiziert die Auflösung mit
// testDomain; scheitert der Test, wird die Änderung zurückgerollt (exit 1).
// Leere Server-Liste = Rückbau der LCM-Verwaltung (dnsUnmanageScript).
func dnsApplyScript(servers []string, testDomain string) string {
	if len(servers) == 0 {
		return dnsUnmanageScript()
	}
	if testDomain == "" {
		testDomain = "deb.debian.org"
	}
	var nsLines strings.Builder
	for _, ip := range servers {
		nsLines.WriteString("nameserver " + ip + "\n")
	}
	return fmt.Sprintf(`resolve_ok() { getent hosts "$1" >/dev/null 2>&1 || nslookup "$1" >/dev/null 2>&1; }
if command -v resolvectl >/dev/null 2>&1 && systemctl is-active --quiet systemd-resolved 2>/dev/null; then
  install -d -m 755 /etc/systemd/resolved.conf.d
  [ -f %[1]s ] && cp -f %[1]s %[1]s.lcm-bak
  printf '[Resolve]\nDNS=%[2]s\n' > %[1]s
  systemctl restart systemd-resolved 2>/dev/null || true
  if resolve_ok '%[4]s'; then
    echo 'LCM: DNS gesetzt (systemd-resolved): %[2]s'
  else
    rm -f %[1]s; [ -f %[1]s.lcm-bak ] && mv %[1]s.lcm-bak %[1]s
    systemctl restart systemd-resolved 2>/dev/null || true
    echo 'LCM: DNS-Test nach dem Setzen fehlgeschlagen - zurueckgerollt'; exit 1
  fi
else
  [ -e %[3]s.lcm-bak ] || cp -f %[3]s %[3]s.lcm-bak 2>/dev/null || true
  [ -L %[3]s ] && rm -f %[3]s
  printf '%[5]s' > %[3]s
  if resolve_ok '%[4]s'; then
    echo 'LCM: DNS gesetzt (/etc/resolv.conf): %[2]s'
  else
    [ -e %[3]s.lcm-bak ] && cp -f %[3]s.lcm-bak %[3]s
    echo 'LCM: DNS-Test nach dem Setzen fehlgeschlagen - zurueckgerollt'; exit 1
  fi
fi`,
		dnsResolvedDropin,          // %[1]s
		strings.Join(servers, " "), // %[2]s (resolved DNS= und Meldung)
		dnsResolvConf,              // %[3]s
		testDomain,                 // %[4]s
		nsLines.String(),           // %[5]s (nameserver-Zeilen, printf-Format)
	)
}

// dnsUnmanageScript entfernt die LCM-DNS-Verwaltung wieder: Drop-in weg +
// systemd-resolved neu starten, bzw. das resolv.conf-Backup zurückspielen.
func dnsUnmanageScript() string {
	return fmt.Sprintf(`if command -v resolvectl >/dev/null 2>&1 && systemctl is-active --quiet systemd-resolved 2>/dev/null; then
  rm -f %[1]s %[1]s.lcm-bak
  systemctl restart systemd-resolved 2>/dev/null || true
  echo 'LCM: DNS-Verwaltung entfernt (systemd-resolved)'
else
  if [ -e %[2]s.lcm-bak ]; then mv -f %[2]s.lcm-bak %[2]s; fi
  echo 'LCM: DNS-Verwaltung entfernt (/etc/resolv.conf)'
fi`, dnsResolvedDropin, dnsResolvConf)
}

// dnsCurrentScript liest die tatsächlich aktiven Resolver: bevorzugt die echten
// Upstreams von systemd-resolved (resolvectl), sonst die nameserver aus
// /etc/resolv.conf (ohne den 127.0.0.53-Stub). Ausgabe: IPs, leerzeichengetrennt.
func dnsCurrentScript() string {
	return `out=""
if command -v resolvectl >/dev/null 2>&1; then
  out=$(resolvectl dns 2>/dev/null | sed 's/^[^:]*: *//' | tr ' ' '\n' | grep -E '^[0-9a-fA-F.:]+$' | sort -u | tr '\n' ' ')
fi
if [ -z "$(printf %s "$out" | tr -d ' ')" ]; then
  out=$(awk '/^nameserver/ && $2 != "127.0.0.53" {print $2}' /etc/resolv.conf 2>/dev/null | sort -u | tr '\n' ' ')
fi
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

// ConfigureDNS setzt bis zu drei Nameserver auf dem Server (leere Liste = LCM
// gibt die DNS-Verwaltung wieder frei). Synchrone Aktion mit Rollback bei
// gebrochener Auflösung, Muster ConfigureAptProxy.
func (s *ServerService) ConfigureDNS(scope repositories.AccessScope, id uint, servers []string, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	// RouterOS hat kein resolv.conf/systemd-resolved - sauber ablehnen statt
	// ein Linux-Shell-Skript in die RouterOS-CLI zu schicken.
	if err := ensureNotRouterOS(server); err != nil {
		return "", err
	}
	clean, err := validDNSServers(servers)
	if err != nil {
		return "", err
	}
	testDomain := "deb.debian.org"
	if doms := sanitizeDNSDomains(s.dnsTestDomainList()); len(doms) > 0 {
		testDomain = doms[0]
	}

	script := dnsApplyScript(clean, testDomain)
	if server.RestrictedSudo {
		// Eingeschränkter Modus: der Helper schreibt DNS validiert (gleiche
		// Logik samt Auflösungstest + Rollback).
		arg := "off"
		if len(clean) > 0 {
			arg = strings.Join(clean, ",")
		}
		script = helperCmd("dns", arg, testDomain)
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
