package services

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/remote/wire"
	"LCM/internal/storage/repositories"
)

// LCM Remote: Enrollment und Token-Verwaltung für Agent-Server.
//
// Ein Agent-Server wird NICHT per SSH provisioniert - stattdessen wird der
// Datensatz sofort (offline) angelegt und ein Einmal-Token erzeugt, mit dem
// sich der lcm-agent auf dem Zielsystem ausgehend am eingebetteten MQTT-
// Broker anmeldet. Das Token ist zugleich das dauerhafte Credential des
// Agents (wie ein API-Key): at rest liegt nur der SHA-256-Hash des Secrets,
// der Klartext wird einmalig bei der Erzeugung angezeigt.

var (
	// ErrAgentTransport: die Aktion ist SSH-spezifisch und für Agent-Server
	// nicht verfügbar (z.B. SSH-Härtung, Key-Rotation, Reconnect).
	ErrAgentTransport = errors.New("für Agent-Server nicht verfügbar - dieser Server wird über den lcm-agent (MQTT) verwaltet, nicht per SSH")
	// ErrNotAgentServer: die Aktion gilt nur für Agent-Server (z.B. Token
	// neu erzeugen).
	ErrNotAgentServer = errors.New("kein Agent-Server - dieser Server wird per SSH verwaltet")
	// ErrAgentOffline: der Agent des Servers ist aktuell nicht mit dem
	// Broker verbunden.
	ErrAgentOffline = errors.New("Agent nicht verbunden")
)

// ensureSSHTransport weist SSH-spezifische Aktionen für Agent-Server ab
// (Gegenstück zu ensureFullSudo für den Restricted-Modus).
func ensureSSHTransport(server *domain.Server) error {
	if server.IsAgent() {
		return ErrAgentTransport
	}
	return nil
}

// AgentConnector ist die Sicht des ServerService auf den MQTT-AgentHub
// (internal/remote) - als Interface, damit services nicht von remote
// abhängt und schlanke Tests einen Fake einsetzen können.
type AgentConnector interface {
	// Conn liefert eine sshx.Conn, die Kommandos über MQTT an den
	// verbundenen Agent leitet - oder einen Fehler, wenn er offline ist.
	Conn(server *domain.Server) (sshx.Conn, error)
	// Online meldet, ob der Agent mit der ID aktuell verbunden ist.
	Online(agentID string) bool
	// Disconnect trennt eine aktive Agent-Session (Token-Revocation).
	Disconnect(agentID string)
}

// WithAgentHub verdrahtet den MQTT-AgentHub für den Agent-Transport.
// Optional - ohne ihn schlagen Verbindungen zu Agent-Servern fehl.
func (s *ServerService) WithAgentHub(h AgentConnector) *ServerService {
	s.agents = h
	return s
}

// WithCertFingerprint verdrahtet die Abfrage des SHA-256-Fingerprints des
// aktiven HTTPS-Zertifikats - er wird ins Enrollment-Token eingebettet,
// damit der Agent beim Erstkontakt das (selbstsignierte) Broker-Zertifikat
// pinnen kann (MitM-Schutz, analog zur SSH-Fingerprint-Bestätigung).
// Optional; leer/nil im --dev-Modus (dort kein TLS).
func (s *ServerService) WithCertFingerprint(fn func() (string, error)) *ServerService {
	s.agentCertFP = fn
	return s
}

// ---- Token ------------------------------------------------------------------
//
// Tokenformat, Encode/Parse und die Hash-Definition liegen im geteilten
// Paket internal/remote/wire - Server (Enrollment + Broker-Auth) und
// lcm-agent nutzen dieselbe Implementierung.

// EncodeAgentToken baut das Klartext-Token für die einmalige Anzeige.
func EncodeAgentToken(agentID, secret, certFP string) string {
	return wire.EncodeToken(agentID, secret, certFP)
}

// ParseAgentToken zerlegt ein Klartext-Token (Gegenstück zu EncodeAgentToken).
func ParseAgentToken(token string) (agentID, secret, certFP string, err error) {
	return wire.ParseToken(token)
}

// HashAgentSecret bildet den At-Rest-Hash des Token-Secrets - dieselbe
// Definition (wire.HashSecret), die der Broker beim Connect constant-time
// vergleicht.
func HashAgentSecret(secret string) string {
	return wire.HashSecret(secret)
}

// ---- Enrollment ---------------------------------------------------------------

// certFingerprint liefert den Zertifikats-Fingerprint fürs Token
// (leer, wenn nicht verdrahtet - z.B. --dev ohne TLS).
func (s *ServerService) certFingerprint() string {
	if s.agentCertFP == nil {
		return ""
	}
	fp, err := s.agentCertFP()
	if err != nil {
		// Best effort: ohne Pin fällt der Agent auf Systemroot-Prüfung
		// zurück - besser als das Enrollment ganz zu blockieren.
		return ""
	}
	return fp
}

// CreateAgentServer legt einen Agent-Server an (Transport=agent, offline)
// und erzeugt sein Enrollment-Token. Rückgabe: Server + Klartext-Token
// (einmalig!). Der Server geht online, sobald sich der Agent verbindet;
// der Erst-Scan läuft dann automatisch über den Broker-Callback.
func (s *ServerService) CreateAgentServer(name, actor string) (*domain.Server, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", errors.New("name darf nicht leer sein")
	}
	if _, err := s.servers.FindByName(name); err == nil {
		return nil, "", ErrServerNameTaken
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, "", err
	}

	secret := config.RandomSecret(32)
	server := &domain.Server{
		Name:      name,
		Transport: domain.TransportAgent,
		AgentID:   uuid.NewString(),
		// Der Agent läuft als Root-Dienst auf dem Zielsystem - wrapSudo
		// lässt Kommandos für root unverändert (kein sudo nötig).
		ServiceUser:      "root",
		AgentTokenHash:   HashAgentSecret(secret),
		AgentTokenPrefix: secret[:12],
		Reachable:        false,
		LastError:        "Agent noch nicht verbunden",
	}
	if err := s.servers.Create(server); err != nil {
		return nil, "", err
	}
	// Wie beim SSH-Join: automatisch in die System-Gruppe, damit die
	// Basis-Schedules (Health-Check, System-Sync) sofort gelten.
	s.assignToSystemGroup(server)

	now := time.Now()
	job := &domain.Job{
		ServerID: &server.ID, Type: "join", Name: "Agent-Onboarding: " + name,
		Status: domain.JobStatusSuccess, TriggeredBy: actor,
		StartedAt: &now, FinishedAt: &now,
		Output: "Agent-Server angelegt (Transport: MQTT über WebSocket).\n" +
			"Enrollment-Token erzeugt (Prefix " + server.AgentTokenPrefix + "…).\n" +
			"Warte auf erste Verbindung des lcm-agent.",
	}
	_ = s.jobs.jobs.Create(job)
	s.audit.Log(actor, "server.agent_create", "server", server.ID, "Agent-ID "+server.AgentID)

	return server, EncodeAgentToken(server.AgentID, secret, s.certFingerprint()), nil
}

// RegenerateAgentToken ersetzt das Token eines Agent-Servers (z.B. bei
// Verlust oder Kompromittierung). Das alte Secret ist sofort wertlos; eine
// aktive Agent-Session wird getrennt, der Agent muss neu enrollt werden.
func (s *ServerService) RegenerateAgentToken(scope repositories.AccessScope, id uint, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	if !server.IsAgent() {
		return "", ErrNotAgentServer
	}
	secret := config.RandomSecret(32)
	if err := s.servers.UpdateFields(server.ID, map[string]any{
		"agent_token_hash":   HashAgentSecret(secret),
		"agent_token_prefix": secret[:12],
	}); err != nil {
		return "", err
	}
	// Aktive Session trennen - der alte Token darf nicht weiterlaufen.
	if s.agents != nil {
		s.agents.Disconnect(server.AgentID)
	}
	s.audit.Log(actor, "server.agent_token_regenerate", "server", server.ID, "Agent-ID "+server.AgentID)
	return EncodeAgentToken(server.AgentID, secret, s.certFingerprint()), nil
}
