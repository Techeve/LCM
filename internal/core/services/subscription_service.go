package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"
)

// SubscriptionService verwaltet die Enterprise-Subscription dieser
// LCM-Installation: Aktivierung beim Anbieter-Dienst (Subscription-Key ↔
// instanzgebundener Repository-Zugangsschlüssel), das tägliche
// Lebenszeichen (verify) und die Umstellung des LCM-Hosts auf den
// Enterprise-Paketkanal.
//
// Community und Enterprise sind funktional identisch - dieser Service
// schaltet KEINE Features. Er hält nur den Vertragsstand sichtbar und
// besorgt den Repository-Zugang.
type SubscriptionService struct {
	settings *repositories.SettingsRepository
	servers  *repositories.ServerRepository
	cipher   *crypto.Cipher
	audit    *AuditService
	client   *http.Client
	// runLcmHostScript führt ein Skript als Job auf dem LCM-Host aus
	// (apt-Umstellung); after wird nach Job-Ende mit dem Erfolg gerufen.
	// Optional - ohne Verdrahtung ist die apt-Umstellung nicht verfügbar.
	runLcmHostScript func(name, script, actor string, after func(ok bool)) (*domain.Job, error)
}

func NewSubscriptionService(settings *repositories.SettingsRepository, servers *repositories.ServerRepository, cipher *crypto.Cipher, audit *AuditService) *SubscriptionService {
	return &SubscriptionService{
		settings: settings, servers: servers, cipher: cipher, audit: audit,
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

// WithLcmHostRunner verdrahtet die Job-Ausführung auf dem LCM-Host
// (ServerService) für die apt-Kanal-Umstellung.
func (s *SubscriptionService) WithLcmHostRunner(run func(name, script, actor string, after func(ok bool)) (*domain.Job, error)) *SubscriptionService {
	s.runLcmHostScript = run
	return s
}

// EnsureInstanceID erzeugt beim ersten Start die dauerhafte Instanz-Kennung.
// Idempotent - eine vorhandene ID wird NIE überschrieben (sie ist die
// Identität dieser Installation gegenüber dem Subscription-Dienst).
func (s *SubscriptionService) EnsureInstanceID() error {
	settings, err := s.settings.Get()
	if err != nil {
		return err
	}
	if settings.InstanceID != "" {
		return nil
	}
	return s.settings.UpdateFields(map[string]any{"instance_id": uuid.NewString()})
}

// --- Dienst-Protokoll ---------------------------------------------------------
//
// Gegenstück zum eigenständigen Dienst (eigenes Repository:
// gitlab.techeve.de/techeve/lcm-subscription-service).

type subscriptionActivateReq struct {
	SubscriptionKey string `json:"subscription_key"`
	InstanceID      string `json:"instance_id"`
	InstanceName    string `json:"instance_name"`
	LCMVersion      string `json:"lcm_version"`
	ServerCount     int    `json:"server_count"`
}

type subscriptionActivateResp struct {
	KeyPrefix       string `json:"key_prefix"`
	Customer        string `json:"customer"`
	Status          string `json:"status"`
	ExpiresAt       string `json:"expires_at"`
	IncludedServers int    `json:"included_servers"`
	RepoURL         string `json:"repo_url"`
	RepoLogin       string `json:"repo_login"`
	RepoPassword    string `json:"repo_password"`
}

type subscriptionVerifyReq struct {
	InstanceID  string `json:"instance_id"`
	AccessKey   string `json:"access_key"`
	LCMVersion  string `json:"lcm_version"`
	ServerCount int    `json:"server_count"`
}

type subscriptionVerifyResp struct {
	KeyPrefix       string `json:"key_prefix"`
	Customer        string `json:"customer"`
	Status          string `json:"status"`
	ExpiresAt       string `json:"expires_at"`
	IncludedServers int    `json:"included_servers"`
}

// subscriptionStatusUnreachable: die letzte Prüfung scheiterte am Transport -
// der Vertragsstand ist UNBEKANNT, nicht schlecht. Bewusst eigener Wert
// statt eines stillgelassenen alten Status.
const subscriptionStatusUnreachable = "unreachable"

// postJSON schickt einen JSON-Request an den Dienst und liefert Status +
// Body. Fehlertexte des Dienstes ({"error": "..."}) werden durchgereicht -
// sie sind für Menschen geschrieben (z. B. „bereits an andere Instanz
// gebunden").
func (s *SubscriptionService) postJSON(url string, payload any) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return resp.StatusCode, data, err
}

func serviceError(data []byte, fallback string) error {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return errors.New(e.Error)
	}
	return errors.New(fallback)
}

// serverCount liefert die Zahl der verwalteten Server - sie wird dem Dienst
// GEMELDET (Anzeige beim Anbieter), nie durchgesetzt.
func (s *SubscriptionService) serverCount() int {
	servers, err := s.servers.FindAll(repositories.ScopeAll())
	if err != nil {
		return 0
	}
	return len(servers)
}

// instanceName ist der menschenlesbare Name dieser Installation beim
// Anbieter: die öffentliche Basis-URL, sonst der Hostname des LCM-Hosts.
func instanceName(settings *domain.GlobalSettings) string {
	if u := strings.TrimSpace(settings.PublicBaseURL); u != "" {
		return strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	}
	return ""
}

// --- Aktivierung -------------------------------------------------------------

// Activate tauscht den eingegebenen Subscription-Key beim Anbieter-Dienst
// gegen den instanzgebundenen Zugangsschlüssel und speichert beides
// verschlüsselt. Schlägt die Bindung fehl (Key gehört einer anderen
// Instanz, abgelaufen, widerrufen), kommt der Klartext des Dienstes zurück
// - es wird dann NICHTS am gespeicherten Zustand geändert.
func (s *SubscriptionService) Activate(key, actor string) (*domain.GlobalSettings, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("subscription-key fehlt")
	}
	if err := s.EnsureInstanceID(); err != nil {
		return nil, err
	}
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}

	status, data, err := s.postJSON(settings.SubscriptionServiceBase()+"/subscription/v1/activate", subscriptionActivateReq{
		SubscriptionKey: key,
		InstanceID:      settings.InstanceID,
		InstanceName:    instanceName(settings),
		LCMVersion:      version.Version,
		ServerCount:     s.serverCount(),
	})
	if err != nil {
		return nil, fmt.Errorf("subscription-dienst nicht erreichbar: %w", err)
	}
	if status != http.StatusOK {
		return nil, serviceError(data, fmt.Sprintf("aktivierung fehlgeschlagen (HTTP %d)", status))
	}
	var resp subscriptionActivateResp
	if err := json.Unmarshal(data, &resp); err != nil || resp.RepoPassword == "" {
		return nil, errors.New("unerwartete antwort des subscription-dienstes")
	}

	keyEnc, err := s.cipher.EncryptString(key)
	if err != nil {
		return nil, err
	}
	accessEnc, err := s.cipher.EncryptString(resp.RepoPassword)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.settings.UpdateFields(map[string]any{
		"subscription_key_enc":          keyEnc,
		"subscription_access_key_enc":   accessEnc,
		"subscription_repo_url":         resp.RepoURL,
		"subscription_customer":         resp.Customer,
		"subscription_key_prefix":       resp.KeyPrefix,
		"subscription_status":           resp.Status,
		"subscription_expires_at":       resp.ExpiresAt,
		"subscription_included_servers": resp.IncludedServers,
		"subscription_last_check_at":    now,
		"subscription_last_error":       "",
	}); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "subscription.activate", "settings", 1, resp.KeyPrefix)
	return s.settings.Get()
}

// --- Lebenszeichen -----------------------------------------------------------

// Verify ist das Lebenszeichen: bestätigt den Zugangsschlüssel beim Dienst,
// meldet Version + Serverzahl und übernimmt den aktuellen Vertragsstand
// (Status, Ablaufdatum). Transportfehler setzen den Status auf
// "unreachable" - der Vertragsstand ist dann unbekannt, nicht schlecht.
func (s *SubscriptionService) Verify(actor string) (*domain.GlobalSettings, error) {
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	if !settings.SubscriptionConfigured() {
		return nil, ErrNoSubscription
	}
	accessKey, err := s.cipher.DecryptString(settings.SubscriptionAccessKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("zugangsschlüssel entschlüsseln: %w", err)
	}

	now := time.Now()
	status, data, err := s.postJSON(settings.SubscriptionServiceBase()+"/subscription/v1/verify", subscriptionVerifyReq{
		InstanceID:  settings.InstanceID,
		AccessKey:   accessKey,
		LCMVersion:  version.Version,
		ServerCount: s.serverCount(),
	})
	fields := map[string]any{"subscription_last_check_at": now}
	switch {
	case err != nil:
		fields["subscription_status"] = subscriptionStatusUnreachable
		fields["subscription_last_error"] = "Dienst nicht erreichbar: " + err.Error()
	case status == http.StatusOK:
		var resp subscriptionVerifyResp
		if jsonErr := json.Unmarshal(data, &resp); jsonErr != nil {
			fields["subscription_last_error"] = "unerwartete Antwort des Dienstes"
		} else {
			fields["subscription_status"] = resp.Status
			fields["subscription_expires_at"] = resp.ExpiresAt
			fields["subscription_customer"] = resp.Customer
			fields["subscription_included_servers"] = resp.IncludedServers
			fields["subscription_last_error"] = ""
		}
	case status == http.StatusUnauthorized:
		// Der Zugangsschlüssel gilt nicht mehr: widerrufen oder vom
		// Anbieter freigegeben (Umzugs-Vorbereitung). Ehrlich benennen.
		fields["subscription_status"] = "revoked"
		fields["subscription_last_error"] = "Der Zugangsschlüssel wird vom Dienst nicht mehr akzeptiert (widerrufen oder freigegeben). Mit dem Subscription-Key neu aktivieren."
	default:
		fields["subscription_last_error"] = serviceError(data, fmt.Sprintf("Prüfung fehlgeschlagen (HTTP %d)", status)).Error()
	}
	if err := s.settings.UpdateFields(fields); err != nil {
		return nil, err
	}
	if actor != "" {
		s.audit.Log(actor, "subscription.verify", "settings", 1, "")
	}
	return s.settings.Get()
}

// RunDailyCheck ist der Scheduler-Einstieg: still, ohne Audit-Eintrag, und
// nur wenn überhaupt eine Subscription hinterlegt ist.
func (s *SubscriptionService) RunDailyCheck() {
	settings, err := s.settings.Get()
	if err != nil || !settings.SubscriptionConfigured() {
		return
	}
	if _, err := s.Verify(""); err != nil {
		slog.Warn("subscription check failed", "error", err)
	}
}

// CheckOnStartupIfStale holt ein überfälliges Lebenszeichen nach - der
// tägliche Cron zählt ab Prozessstart, eine häufig neu startende Instanz
// käme sonst nie zu einer Prüfung.
func (s *SubscriptionService) CheckOnStartupIfStale() {
	settings, err := s.settings.Get()
	if err != nil || !settings.SubscriptionConfigured() {
		return
	}
	if settings.SubscriptionLastCheckAt != nil && time.Since(*settings.SubscriptionLastCheckAt) < 24*time.Hour {
		return
	}
	s.RunDailyCheck()
}

// Remove löscht die Subscription LOKAL (Key, Zugangsschlüssel, Stand).
// Die Bindung beim Anbieter bleibt bestehen, bis er sie freigibt - das
// gehört in die Meldung an den Anwender.
func (s *SubscriptionService) Remove(actor string) error {
	if err := s.settings.UpdateFields(map[string]any{
		"subscription_key_enc":          "",
		"subscription_access_key_enc":   "",
		"subscription_repo_url":         "",
		"subscription_customer":         "",
		"subscription_key_prefix":       "",
		"subscription_status":           "",
		"subscription_expires_at":       "",
		"subscription_included_servers": 0,
		"subscription_last_check_at":    nil,
		"subscription_last_error":       "",
	}); err != nil {
		return err
	}
	s.audit.Log(actor, "subscription.remove", "settings", 1, "")
	return nil
}

// ErrNoSubscription: Aktion verlangt eine hinterlegte Subscription.
var ErrNoSubscription = errors.New("keine Subscription hinterlegt")

// --- Status-Sicht ------------------------------------------------------------

// SubscriptionStatusView ist die UI-Sicht auf den Subscription-Zustand.
type SubscriptionStatusView struct {
	Configured  bool   `json:"configured"`
	InstanceID  string `json:"instance_id"`
	ServiceURL  string `json:"service_url"`
	Customer    string `json:"customer,omitempty"`
	KeyPrefix   string `json:"key_prefix,omitempty"`
	Status      string `json:"status,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	DaysLeft    *int   `json:"days_left,omitempty"` // nil = unbefristet/unbekannt
	LastCheckAt string `json:"last_check_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	// Server-Kontingent laut Vertrag (0 = keins hinterlegt) und die
	// aktuell verwaltete Serverzahl dieser Instanz. QuotaExceeded ist
	// ein reiner Hinweis - LCM schränkt bei Überschreitung nichts ein.
	IncludedServers int    `json:"included_servers"`
	ManagedServers  int    `json:"managed_servers"`
	QuotaExceeded   bool   `json:"quota_exceeded"`
	RepoURL         string `json:"repo_url,omitempty"`
	// AptChannel: der Paketkanal des LCM-Hosts (community/beta/enterprise).
	// AptActive bleibt als kurze Frage „steht er auf Enterprise?" erhalten -
	// ältere Clients kennen nur die.
	AptChannel string `json:"apt_channel"`
	AptActive  bool   `json:"apt_active"`
	// AptAvailable: es gibt einen LCM-Host mit apt - nur dann ist die
	// Kanal-Umstellung anbietbar.
	AptAvailable bool `json:"apt_available"`
}

// Status liefert die UI-Sicht; DaysLeft zählt bis zum LETZTEN gültigen Tag
// (einschließlich), negativ wird nicht gemeldet (dann ist der Status
// ohnehin expired).
func (s *SubscriptionService) Status() (*SubscriptionStatusView, error) {
	if err := s.EnsureInstanceID(); err != nil {
		return nil, err
	}
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	v := &SubscriptionStatusView{
		Configured:      settings.SubscriptionConfigured(),
		InstanceID:      settings.InstanceID,
		ServiceURL:      settings.SubscriptionServiceBase(),
		Customer:        settings.SubscriptionCustomer,
		KeyPrefix:       settings.SubscriptionKeyPrefix,
		Status:          settings.SubscriptionStatus,
		ExpiresAt:       settings.SubscriptionExpiresAt,
		LastError:       settings.SubscriptionLastError,
		IncludedServers: settings.SubscriptionIncludedServers,
		ManagedServers:  s.serverCount(),
		RepoURL:         settings.SubscriptionRepoURL,
		AptChannel:      settings.AptChannel(),
		AptActive:       settings.AptChannel() == domain.AptChannelEnterprise,
		AptAvailable:    s.lcmHostAptServer() != nil,
	}
	v.QuotaExceeded = v.IncludedServers > 0 && v.ManagedServers > v.IncludedServers
	if settings.SubscriptionLastCheckAt != nil {
		v.LastCheckAt = settings.SubscriptionLastCheckAt.Format(time.RFC3339)
	}
	if d, ok := subscriptionDaysLeft(settings.SubscriptionExpiresAt, time.Now()); ok {
		v.DaysLeft = &d
	}
	return v, nil
}

// subscriptionDaysLeft rechnet das Ablaufdatum (YYYY-MM-DD, letzter
// gültiger Tag) in verbleibende volle Tage um. ok=false bei leerem oder
// unlesbarem Datum.
func subscriptionDaysLeft(expires string, now time.Time) (int, bool) {
	if expires == "" {
		return 0, false
	}
	t, err := time.ParseInLocation("2006-01-02", expires, time.Local)
	if err != nil {
		return 0, false
	}
	// Ende des letzten gültigen Tags minus jetzt, aufgerundet in Tagen.
	left := t.AddDate(0, 0, 1).Sub(now)
	if left < 0 {
		return 0, true
	}
	return int(left / (24 * time.Hour)), true
}

// SetServiceURL speichert die Dienst-Adresse (leer = Standard). Nur die
// Basis - die Pfade (/subscription/v1/…) sind Protokoll, keine Konfiguration.
func (s *SubscriptionService) SetServiceURL(raw, actor string) error {
	u := strings.TrimSpace(raw)
	if u != "" && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return errors.New("die Dienst-Adresse muss mit https:// beginnen")
	}
	if err := s.settings.UpdateFields(map[string]any{"subscription_service_url": strings.TrimRight(u, "/")}); err != nil {
		return err
	}
	s.audit.Log(actor, "subscription.service-url", "settings", 1, u)
	return nil
}
