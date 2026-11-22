package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/acmex"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// Werte der ACME-Einstellungen.
const (
	ACMEModeSingle      = "single"   // ein Zertifikat mit allen Namen (der einzige Modus)
	ACMEModePerName     = "per_name" // je Name ein Zertifikat; nur noch als alter Stand lesbar
	ACMEChallengeHTTP01 = "http-01"
	ACMEChallengeDNS01  = "dns-01"

	// DefaultACMEDirectory ist die Produktion von Let's Encrypt.
	DefaultACMEDirectory = "https://acme-v02.api.letsencrypt.org/directory"

	maxACMENames = 3

	// acmeRunTimeout begrenzt einen Lauf: Eine CA, die nicht antwortet, hält
	// ihn nicht ewig fest.
	acmeRunTimeout = 10 * time.Minute
	// Der erste Lauf kommt kurz nach dem Start, wenn der Listener steht;
	// danach täglich, nach einem Fehler schon nach einer Stunde.
	acmeFirstRunDelay   = time.Minute
	acmeRunInterval     = 24 * time.Hour
	acmeRetryAfterError = time.Hour

	acmeSystemActor = "system"
)

var (
	// ErrACMERunning: Ein Lauf ist schon aktiv.
	ErrACMERunning = errors.New("ein acme-lauf ist bereits aktiv")
	// ErrACMENotActive: ACME ist nicht eingeschaltet.
	ErrACMENotActive = errors.New("acme ist nicht eingeschaltet")
	// ErrACMETermsRequired: Das Einschalten verlangt, die Nutzungsbedingungen
	// der CA anzunehmen.
	ErrACMETermsRequired = errors.New("die nutzungsbedingungen der ca müssen angenommen werden (accept_terms)")
)

// ACMEInvalidError: eine Angabe ist ungültig oder fehlt (Missing). Field
// nennt das Feld der API.
type ACMEInvalidError struct {
	Field, Detail string
	Missing       bool
}

func (e *ACMEInvalidError) Error() string {
	if e.Missing {
		return e.Field + ": fehlt"
	}
	return e.Field + ": " + e.Detail
}

// ACMEListener ist der Listener für HTTP-01 (acmehttp.Server). Der Dienst
// öffnet ihn erst, wenn eine Prüfung läuft, und schließt ihn am Ende des Laufs.
type ACMEListener interface {
	Start(addr string) error
	Stop()
	Running() bool
}

// ACMEUpdate sind die Einstellungen, die ein Admin setzt.
type ACMEUpdate struct {
	Enabled      bool
	DirectoryURL string // leer = Let's Encrypt
	Email        string
	Challenge    string // leer = http-01
	// Bei dns-01: der Anbieter, seine Werte und die Prüf-Nameserver. Bei den
	// Werten heißt ein fehlendes Geheimfeld "behalten", ein leeres "löschen".
	DNSProvider    string
	DNSValues      map[string]string
	DNSNameservers []string
	// AcceptTerms nimmt die Nutzungsbedingungen der CA an. Beim Einschalten
	// verlangt LCM es, bis der Admin sie für diese CA einmal angenommen hat.
	AcceptTerms bool
}

// ACMEView ist der Stand: Einstellungen, Zustand und ausgestellte Zertifikate.
type ACMEView struct {
	Enabled       bool     `json:"enabled"`
	DirectoryURL  string   `json:"directory_url"`
	Email         string   `json:"email"`
	Names         []string `json:"names"` // die Namen aus den Adressen, für die das Zertifikat ausgestellt wird
	Mode          string   `json:"mode"`  // immer single
	Challenge     string   `json:"challenge"`
	TermsAccepted bool     `json:"terms_accepted"` // für die gewählte CA
	// NamesChangedNotice: einmaliger Hinweis beim Umstieg von einem eigenen
	// Namensfeld auf die Namen aus den Adressen; null, wenn es nichts zu melden
	// gibt. AckNamesNotice (oder Speichern der Einstellungen) nimmt ihn weg.
	NamesChangedNotice *ACMENamesNotice `json:"names_changed_notice"`
	// DNS-01: der gewählte Anbieter und seine Werte ohne Geheimnisse (je
	// Geheimfeld nur, ob es gesetzt ist), die Prüf-Nameserver und die
	// Anbieter zur Auswahl mit ihren Feldern.
	DNSProvider    string              `json:"dns_provider"`
	DNSValues      map[string]any      `json:"dns_values"`
	DNSSecretsSet  map[string]bool     `json:"dns_secrets_set"`
	DNSNameservers []string            `json:"dns_check_nameservers"`
	DNSProviders   []acmex.DNSProvider `json:"dns_providers"`
	// Der Listener für HTTP-01 (acme_http_listen der config.json). Er ist nur
	// während einer Prüfung offen; ListenerError ist der Grund, wenn er sich
	// beim letzten Mal nicht öffnen ließ.
	HTTPListen      string `json:"http_listen"`
	ListenerRunning bool   `json:"listener_running"`
	ListenerError   string `json:"listener_error,omitempty"`
	// Zustand der Läufe.
	Running      bool             `json:"running"`
	LastRunAt    *time.Time       `json:"last_run_at"`
	LastError    string           `json:"last_error,omitempty"`
	Certificates []acmex.CertInfo `json:"certificates"`
}

// ACMEService verwaltet die ACME-Einstellungen und hält die Zertifikate auf
// dem Stand: Der tägliche Lauf stellt aus und erneuert, und er meldet
// Fehler als Alarm und im Audit-Log.
type ACMEService struct {
	settings   *repositories.SettingsRepository
	audit      *AuditService
	manager    *acmex.Manager
	tokens     *acmex.ChallengeStore
	listener   ACMEListener
	httpListen string
	// reload lädt die Zertifikate in den Pool (certReloader).
	reload func()
	// cipher verschlüsselt die Werte des DNS-Anbieters; dnsEnv sagt, was
	// LCM dafür selbst beisteuert (Datenverzeichnis, Programm für exec).
	cipher *crypto.Cipher
	dnsEnv acmex.DNSEnv
	// names liefert die DNS-Namen aus den Adressen (SettingsService.ACMENames).
	names func(*domain.GlobalSettings) []string

	mu        sync.Mutex // serialisiert Änderungen und den Listener
	listenErr string
	running   atomic.Bool

	// autoIssue: Das Speichern startet bei fehlender Abdeckung einen Lauf
	// (acme_coverage.go). autoFail ist der letzte gescheiterte selbst gestartete
	// Lauf; clock ersetzt die Uhr im Test.
	autoIssue bool
	autoMu    sync.Mutex
	autoFail  autoFailure
	clock     func() time.Time
}

func NewACMEService(settings *repositories.SettingsRepository, audit *AuditService, manager *acmex.Manager,
	tokens *acmex.ChallengeStore, listener ACMEListener, httpListen string, reload func(),
	cipher *crypto.Cipher, dnsEnv acmex.DNSEnv) *ACMEService {
	return &ACMEService{settings: settings, audit: audit, manager: manager, tokens: tokens,
		listener: listener, httpListen: httpListen, reload: reload, cipher: cipher, dnsEnv: dnsEnv}
}

// WithNames verdrahtet die Quelle der Namen: die öffentlichen Adressen.
func (s *ACMEService) WithNames(names func(*domain.GlobalSettings) []string) *ACMEService {
	s.names = names
	return s
}

// View liefert den Stand.
func (s *ACMEService) View() (ACMEView, error) {
	st, err := s.settings.Get()
	if err != nil {
		return ACMEView{}, err
	}
	certs, err := s.manager.Certificates()
	if err != nil {
		return ACMEView{}, err
	}
	s.mu.Lock()
	listenErr := s.listenErr
	s.mu.Unlock()
	dns := s.dnsView(st)
	return ACMEView{
		Enabled:            st.ACMEEnabled,
		DirectoryURL:       st.ACMEDirectoryURL,
		Email:              st.ACMEEmail,
		Names:              s.namesOf(st),
		Mode:               ACMEModeSingle,
		Challenge:          st.ACMEChallenge,
		TermsAccepted:      termsAccepted(st),
		NamesChangedNotice: s.namesNotice(st),
		DNSProvider:        dns.Provider,
		DNSValues:          dns.Values,
		DNSSecretsSet:      dns.SecretsSet,
		DNSNameservers:     dns.Nameservers,
		DNSProviders:       acmex.DNSProviders(s.dnsEnv.ExecPath != ""),
		HTTPListen:         s.httpListen,
		ListenerRunning:    s.listener.Running(),
		ListenerError:      listenErr,
		Running:            s.running.Load(),
		LastRunAt:          st.ACMELastRunAt,
		LastError:          st.ACMELastError,
		Certificates:       certs,
	}, nil
}

// Update speichert die Einstellungen. Ändert sich etwas am Ziel (CA, die
// Namen der Adressen), stellt der nächste Lauf oder „jetzt ausstellen" das
// neue Zertifikat aus; Ausschalten entfernt die ausgestellten Zertifikate.
// Die Namen kommen aus den Adressen und nicht aus der Anfrage.
func (s *ACMEService) Update(in ACMEUpdate, actor string) (ACMEView, error) {
	in, err := normalizeACMEUpdate(in)
	if err != nil {
		return ACMEView{}, err
	}
	release := func() {}
	if !in.Enabled {
		// Ein laufender Lauf legte die Zertifikate nach dem Entfernen wieder an.
		if !s.running.CompareAndSwap(false, true) {
			return ACMEView{}, ErrACMERunning
		}
		release = func() { s.running.Store(false) }
		defer release()
	}
	s.mu.Lock()
	st, err := s.settings.Get()
	if err != nil {
		s.mu.Unlock()
		return ACMEView{}, err
	}
	if in.Enabled && len(s.namesOf(st)) == 0 {
		s.mu.Unlock()
		return ACMEView{}, &ACMEInvalidError{Field: "names", Detail: "aus den öffentlichen Adressen ergibt sich kein DNS-Name " +
			"(eine IP-Adresse oder localhost bekommt kein Zertifikat): erst eine Adresse mit einem Namen eintragen"}
	}
	accepted := termsAcceptedFor(st, in.DirectoryURL)
	if in.Enabled && !accepted && !in.AcceptTerms {
		s.mu.Unlock()
		return ACMEView{}, ErrACMETermsRequired
	}
	fields := map[string]any{
		"acme_enabled":       in.Enabled,
		"acme_directory_url": in.DirectoryURL,
		"acme_email":         in.Email,
		"acme_challenge":     in.Challenge,
	}
	// Wer speichert, hat den Stand gesehen: Der Rest der älteren Version (eigene
	// Namen, je Name) ist damit erledigt.
	maps.Copy(fields, legacyNamesCleared())
	if in.Enabled && in.AcceptTerms {
		fields["acme_terms_directory"] = in.DirectoryURL
	}
	dnsFields, err := s.dnsSettings(st, in)
	if err != nil {
		s.mu.Unlock()
		return ACMEView{}, err
	}
	maps.Copy(fields, dnsFields)
	if err := s.settings.UpdateFields(fields); err != nil {
		s.mu.Unlock()
		return ACMEView{}, err
	}
	s.listenErr = "" // der Grund eines früheren Bindefehlers gilt für die alten Einstellungen
	s.mu.Unlock()

	if !in.Enabled {
		s.dropCertificates()
	}
	release()
	s.audit.Log(actor, "settings.acme.update", "settings", 1, describeACME(in, s.namesOf(st)))
	if s.autoIssue {
		s.forgetAutoFailure()
		s.EnsureCoverage()
	}
	return s.View()
}

// dropCertificates entfernt die ausgestellten Zertifikate. Ein abgeschaltetes
// ACME erneuert nichts mehr; ein Zertifikat, das still abliefe, wäre
// schlimmer als der Rückfall auf das der LCM-CA, den jeder sofort sieht.
func (s *ACMEService) dropCertificates() {
	if err := s.manager.RemoveAll(); err != nil {
		slog.Warn("acme certificates could not be removed", "error", err)
	}
	s.reload()
}

// Stop schließt den Listener (für den Prozess-Shutdown).
func (s *ACMEService) Stop() { s.closeListener() }

// openListener öffnet den Listener für HTTP-01. Er ist nur offen, solange eine
// Prüfung läuft: Der Lauf ruft das erst, wenn die CA die Prüfung verlangt (die
// Antwort wird ausgelegt, siehe challengeWindow). Scheitert das Binden, ist
// der Grund (mit Hinweis auf CAP_NET_BIND_SERVICE und Portweiterleitung) der
// Fehler des Laufs, im Zustand und im Alarm.
func (s *ACMEService) openListener() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.listener.Start(s.httpListen); err != nil {
		s.listenErr = err.Error()
		slog.Warn("ACME HTTP-01 listener could not open", "error", err)
		return err
	}
	s.listenErr = ""
	return nil
}

// closeListener schließt ihn wieder; ein geschlossener bleibt geschlossen.
func (s *ACMEService) closeListener() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listener.Stop()
}

// challengeWindow legt die Antworten der HTTP-01-Prüfung aus und öffnet dafür
// beim ersten Token den Listener; der Lauf schließt ihn am Ende (defer in
// reconcile). So steht Port 80 nur offen, wenn die CA wirklich prüft, nie
// wegen eines Laufs, bei dem nichts fällig ist.
type challengeWindow struct{ svc *ACMEService }

func (w challengeWindow) Present(ctx context.Context, domain, token, keyAuth string) error {
	if err := w.svc.openListener(); err != nil {
		return err
	}
	return w.svc.tokens.Present(ctx, domain, token, keyAuth)
}

func (w challengeWindow) CleanUp(ctx context.Context, domain, token, keyAuth string) error {
	return w.svc.tokens.CleanUp(ctx, domain, token, keyAuth)
}

// IssueNow stößt einen Lauf im Hintergrund an. Läuft schon einer, ist es
// ErrACMERunning.
func (s *ACMEService) IssueNow(actor string) error {
	st, err := s.settings.Get()
	if err != nil {
		return err
	}
	if !st.ACMEEnabled {
		return ErrACMENotActive
	}
	if !s.running.CompareAndSwap(false, true) {
		return ErrACMERunning
	}
	safego.Go("acme:issue", func() {
		defer s.running.Store(false)
		_ = s.runSafe(actor)
	})
	return nil
}

// Run macht einen Lauf und wartet auf ihn. Läuft schon einer, ist es
// ErrACMERunning.
func (s *ACMEService) Run(actor string) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrACMERunning
	}
	defer s.running.Store(false)
	return s.runSafe(actor)
}

// run stellt aus und erneuert. Der Aufrufer hat s.running gesetzt.
func (s *ACMEService) run(actor string) error {
	st, err := s.settings.Get()
	if err != nil {
		return err
	}
	if !st.ACMEEnabled {
		return ErrACMENotActive
	}
	failure := s.reconcile(st, actor)
	s.recordRun(failure)
	if failure != nil {
		slog.Error("acme run failed", "error", failure)
		s.audit.Log(actor, "settings.acme.failed", "settings", 1, failure.Error())
	}
	return failure
}

// runSafe führt einen Lauf so aus, dass ein Panic darin (etwa in lego, wenn
// eine CA-Datei unlesbar ist) als Fehler des Laufs endet: Zeitpunkt und Fehler
// stehen im Zustand, der Alarm greift, und der Dienst läuft weiter. Die
// tägliche Schleife wertet den Lauf als Fehler und versucht es nach einer
// Stunde erneut.
func (s *ACMEService) runSafe(actor string) (err error) {
	defer safego.Recover("acme:run", func(panicErr error) {
		err = fmt.Errorf("acme-lauf abgebrochen: %w", panicErr)
		s.recordRun(err)
		s.audit.Log(actor, "settings.acme.failed", "settings", 1, err.Error())
	})
	return s.run(actor)
}

// reconcile hält die Zertifikate auf dem Stand der Einstellungen und lädt
// sie in den Pool, wenn sich etwas geändert hat.
func (s *ACMEService) reconcile(st *domain.GlobalSettings, actor string) error {
	if st.ACMEChallenge == ACMEChallengeHTTP01 {
		// Gelingt der Lauf, scheitert er oder gerät er in Panik: Port 80 geht zu.
		defer s.closeListener()
	}
	names := s.runNames(st)
	if len(names) == 0 {
		return errors.New("aus den öffentlichen Adressen ergibt sich kein DNS-Name, für den es ein Zertifikat geben kann")
	}
	solver, err := s.solver(st)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), acmeRunTimeout)
	defer cancel()
	outcomes, err := s.manager.Reconcile(ctx, acmex.Config{
		DirectoryURL:  st.ACMEDirectoryURL,
		Email:         st.ACMEEmail,
		Names:         names,
		TermsAccepted: termsAccepted(st),
		Solver:        solver,
	})
	failure := errors.Join(acmex.Failures(outcomes), err)
	if changed := changedOutcomes(outcomes); len(changed) > 0 {
		s.reload()
		s.audit.Log(actor, "settings.acme.issue", "settings", 1, strings.Join(changed, "; "))
	}
	return failure
}

func changedOutcomes(outcomes []acmex.Outcome) []string {
	var changed []string
	for _, o := range outcomes {
		if o.Changed() {
			changed = append(changed, fmt.Sprintf("%s: %s", o.Action, strings.Join(o.Names, ", ")))
		}
	}
	return changed
}

// recordRun hält Zeitpunkt und Ausgang des Laufs fest.
func (s *ACMEService) recordRun(failure error) {
	errText := ""
	if failure != nil {
		errText = failure.Error()
	}
	if err := s.settings.UpdateFields(map[string]any{"acme_last_run_at": time.Now(), "acme_last_error": errText}); err != nil {
		slog.Warn("acme run state not stored", "error", err)
	}
}

// RunDaily läuft einmal am Tag und beim Start nach kurzer Pause. Nach einem
// Fehler versucht es schon nach einer Stunde wieder: Ein Zertifikat, das
// wegen eines kurzen Ausfalls nicht kam, soll keinen Tag warten.
func (s *ACMEService) RunDaily() {
	delay := acmeFirstRunDelay
	for {
		time.Sleep(delay)
		delay = acmeRunInterval
		err := s.Run(acmeSystemActor)
		switch {
		case errors.Is(err, ErrACMERunning):
			delay = acmeFirstRunDelay // ein anderer Lauf ist aktiv (etwa der beim Start): gleich wieder nachsehen
		case err != nil && !errors.Is(err, ErrACMENotActive):
			delay = acmeRetryAfterError
		}
	}
}

// RenewalFailure ist der Hinweis für den Alarm „Zertifikat-Erneuerung
// fehlgeschlagen": der Fehler des letzten Laufs, solange ACME an ist. Leer,
// wenn nichts zu tun ist.
func (s *ACMEService) RenewalFailure() string {
	st, err := s.settings.Get()
	if err != nil || !st.ACMEEnabled {
		return ""
	}
	return st.ACMELastError
}

func termsAccepted(st *domain.GlobalSettings) bool {
	return termsAcceptedFor(st, st.ACMEDirectoryURL)
}

func termsAcceptedFor(st *domain.GlobalSettings, directoryURL string) bool {
	return st.ACMETermsDirectory != "" && st.ACMETermsDirectory == directoryURL
}

func splitNames(stored string) []string {
	return cleanEntries(strings.Split(stored, ","))
}

func describeACME(in ACMEUpdate, names []string) string {
	if !in.Enabled {
		return "aus"
	}
	text := fmt.Sprintf("an: %s, ca %s, challenge %s",
		strings.Join(names, ", "), in.DirectoryURL, in.Challenge)
	if in.Challenge == ACMEChallengeDNS01 {
		text += ", dns-anbieter " + in.DNSProvider
	}
	return text
}
