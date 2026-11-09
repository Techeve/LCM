package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// Ergebnis je Quelle der Prüfung vor der https-Umstellung.
const (
	RepoHTTPSOK      = "ok"       // der Spiegel liefert seine Release-Datei über HTTPS
	RepoHTTPSNone    = "no_https" // HTTPS scheitert: Port zu, falsches Zertifikat, Fehlerseite
	RepoHTTPSUnknown = "unknown"  // nicht prüfbar (kein apt-helper, keine Suite, keine Antwort)
)

// RepoHTTPSResult ist das Ergebnis einer Quelle. Alternative ist ein Spiegel
// derselben Distribution mit HTTPS - nur für Quellen ohne HTTPS und nur, wenn
// LCM einen kennt.
type RepoHTTPSResult struct {
	URI         string `json:"uri"`
	State       string `json:"state"`
	Alternative string `json:"alternative,omitempty"`
}

// RepoHTTPSCheck ist das Ergebnis der Prüfung, je http-Quelle ein Eintrag.
type RepoHTTPSCheck struct {
	Results []RepoHTTPSResult `json:"results"`
	// MissingCertificates: Dem Server fehlen die Stammzertifikate
	// (ca-certificates). Dann scheitert jede HTTPS-Verbindung, und die
	// Prüfung sagt nichts über die Spiegel. Die Umstellung installiert sie
	// selbst, die Quellen gelten deshalb als nicht prüfbar statt ohne HTTPS.
	MissingCertificates bool `json:"missing_certificates,omitempty"`
}

// httpsCheckTimeout begrenzt die ganze Prüfung. Die Proben laufen auf dem
// Server parallel, je Quelle höchstens zwei Abrufe mit 15 Sekunden Frist;
// mehr als das plus Verbindung braucht es nicht, und die Oberfläche wartet
// im offenen Dialog darauf.
var httpsCheckTimeout = 45 * time.Second

// ErrHTTPSCheckTimeout: Der Server hat die Prüfung nicht rechtzeitig beendet.
var ErrHTTPSCheckTimeout = errors.New("der server hat die https-prüfung nicht rechtzeitig beendet")

// CheckRepositoriesHTTPS prüft vom Server aus, welche seiner http-Quellen
// HTTPS sprechen. Gefragt wird der Server selbst, nicht LCM: Nur so zählen
// sein Netz, seine Proxys und seine Zertifikate. Die Quellen ohne HTTPS merkt
// sich LCM am Server, damit die Quellenliste sie markieren kann.
//
// Rein lesend und ohne root: apt-helper lädt die Release-Datei in eine
// temporäre Datei, die danach wieder verschwindet.
func (s *ServerService) CheckRepositoriesHTTPS(scope repositories.AccessScope, id uint, actor string) (*RepoHTTPSCheck, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := secureReposSupported(server); err != nil {
		return nil, err
	}
	repos, err := s.servers.FindRepositories(id)
	if err != nil {
		return nil, err
	}
	probes := domain.HTTPSProbes(repos)
	if len(probes) == 0 {
		s.saveHTTPSUnavailable(server, nil)
		return &RepoHTTPSCheck{Results: []RepoHTTPSResult{}}, nil
	}

	conn, err := s.connectRecRead(server, "repos-https-check", actor)
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	output, err := runBounded(conn, httpsCheckScript(probes), httpsCheckTimeout, ErrHTTPSCheckTimeout)
	if err != nil {
		return nil, err
	}

	check := &RepoHTTPSCheck{Results: parseHTTPSCheck(output, probes), MissingCertificates: strings.Contains(output, httpsNoCerts)}
	s.saveHTTPSUnavailable(server, check.Results)
	return check, nil
}

// httpsCheckScript ruft je Quelle die Release-Datei über https ab, alle
// gleichzeitig. Jede Probe meldet eine Zeile „LCM-HTTPS <ok|fail|unknown|nocerts>
// <http-url>". Die URLs haben domain.SafeRepoURI bestanden und stehen deshalb
// gefahrlos in einfachen Anführungszeichen.
func httpsCheckScript(probes []domain.HTTPSProbe) string {
	lines := []string{
		"H=/usr/lib/apt/apt-helper",
		`D=$(mktemp -d) || exit 1`,
		// Auch wenn die Verbindung abreißt, bleibt nichts in /tmp liegen.
		`trap 'rm -rf "$D"' EXIT`,
		// timeout gehört zu coreutils, fehlt aber auf manchen Minimal-Systemen.
		// Dann begrenzt nur noch apt selbst die Wartezeit.
		`T=; command -v timeout >/dev/null 2>&1 && T="timeout 15"`,
		`fetch() { $T "$H" -o Acquire::https::Timeout=10 download-file "$1" "$2" >/dev/null 2>&1; }`,
		"probe() {",
		`  if [ ! -s ` + systemCertBundle + ` ]; then echo "LCM-HTTPS nocerts $1"; return; fi`,
		`  if [ -z "$2" ] || [ ! -x "$H" ]; then echo "LCM-HTTPS unknown $1"; return; fi`,
		// Manche Spiegel führen nur noch InRelease; erst wenn beides scheitert,
		// gilt die Quelle als ohne HTTPS.
		`  if fetch "$2" "$D/$3" || fetch "${2%Release}InRelease" "$D/$3i"; then`,
		`    echo "LCM-HTTPS ok $1"`,
		"  else",
		`    echo "LCM-HTTPS fail $1"`,
		"  fi",
		"}",
	}
	for i, probe := range probes {
		lines = append(lines, fmt.Sprintf("probe '%s' '%s' r%d &", probe.URI, probe.URL, i))
	}
	return strings.Join(append(lines, "wait"), "\n")
}

// systemCertBundle ist der Zertifikatsspeicher, den apt unter Debian und Ubuntu
// für HTTPS nutzt. Ohne ihn scheitert jede Verbindung.
const systemCertBundle = "/etc/ssl/certs/ca-certificates.crt"

// httpsNoCerts ist die Meldung der Probe, wenn die Stammzertifikate fehlen.
const httpsNoCerts = "LCM-HTTPS nocerts "

// parseHTTPSCheck ordnet die Zeilen der Probe den Quellen zu. Eine Quelle ohne
// Zeile gilt als nicht prüfbar - LCM rät nicht.
func parseHTTPSCheck(output string, probes []domain.HTTPSProbe) []RepoHTTPSResult {
	states := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "LCM-HTTPS" {
			continue
		}
		switch fields[1] {
		case "ok":
			states[fields[2]] = RepoHTTPSOK
		case "fail":
			states[fields[2]] = RepoHTTPSNone
		default:
			states[fields[2]] = RepoHTTPSUnknown
		}
	}
	results := make([]RepoHTTPSResult, 0, len(probes))
	for _, probe := range probes {
		result := RepoHTTPSResult{URI: probe.URI, State: states[probe.URI]}
		if result.State == "" {
			result.State = RepoHTTPSUnknown
		}
		if result.State == RepoHTTPSNone {
			result.Alternative = httpsAlternative(probe.URI)
		}
		results = append(results, result)
	}
	return results
}

// httpsAlternative ist der Ersatz-Spiegel - außer er ist die Quelle selbst
// über https, denn genau die ist eben gescheitert.
func httpsAlternative(uri string) string {
	alternative := domain.HTTPSMirrorAlternative(uri)
	if alternative == strings.TrimRight("https://"+strings.TrimPrefix(uri, "http://"), "/") {
		return ""
	}
	return alternative
}

// saveHTTPSUnavailable merkt sich die Quellen ohne HTTPS aus der Prüfung. Die
// Liste ersetzt die vorige ganz: Was diesmal nicht prüfbar war, verliert sein
// altes „kein HTTPS". Getrennt wird mit Komma wie bei https_revert_urls; eine
// URL mit Komma darin verlöre ihre Markierung, in apt-Quellen kommt das nicht vor.
func (s *ServerService) saveHTTPSUnavailable(server *domain.Server, results []RepoHTTPSResult) {
	var none []string
	for _, result := range results {
		if result.State == RepoHTTPSNone {
			none = append(none, result.URI)
		}
	}
	s.setHTTPSUnavailable(server, strings.Join(none, ","))
}

// pruneHTTPSUnavailableURLs lässt nach einem Scan nur die Markierungen stehen,
// deren Quelle noch als http-Quelle im Bestand steht. Eine umgestellte,
// getauschte oder entfernte Quelle verliert ihr „kein HTTPS" damit von selbst.
func (s *ServerService) pruneHTTPSUnavailableURLs(server *domain.Server, repos []domain.AptRepository) {
	if server.HTTPSUnavailableURLs == "" {
		return
	}
	current := map[string]bool{}
	for _, uri := range domain.InsecureRepoURIs(repos) {
		current[uri] = true
	}
	var keep []string
	for _, uri := range strings.Split(server.HTTPSUnavailableURLs, ",") {
		if uri = strings.TrimSpace(uri); current[uri] {
			keep = append(keep, uri)
		}
	}
	s.setHTTPSUnavailable(server, strings.Join(keep, ","))
}

func (s *ServerService) setHTTPSUnavailable(server *domain.Server, urls string) {
	if urls == server.HTTPSUnavailableURLs {
		return
	}
	if err := s.servers.UpdateFields(server.ID, map[string]any{"https_unavailable_urls": urls}); err == nil {
		server.HTTPSUnavailableURLs = urls
	}
}

// runBounded wartet höchstens limit auf das Kommando und meldet sonst
// errTimeout. Das Kommando selbst läuft auf dem Server dann womöglich noch
// weiter, bis seine eigenen Fristen greifen; der Aufrufer wartet nur nicht
// mehr darauf.
func runBounded(conn sshx.Conn, cmd string, limit time.Duration, errTimeout error) (string, error) {
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	safego.Go("ssh-bounded", func() {
		out, _, err := conn.Run(cmd)
		done <- result{out, err}
	})
	select {
	case r := <-done:
		return r.out, r.err
	case <-time.After(limit):
		return "", errTimeout
	}
}
