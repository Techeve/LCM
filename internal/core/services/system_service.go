package services

import (
	"runtime"
	"time"

	"LCM/internal/version"
)

// SystemService ist das Referenzbeispiel für einen Service OHNE
// Datenbank-Abhängigkeit: reine Logik-Prozesse (Versionsinfo, Uptime,
// Laufzeitumgebung). Die Kette ist hier nur Controller -> Service -
// die Repository-Schicht entfällt, weil es nichts zu persistieren gibt.
//
// Nach diesem Muster baut man z.B. auch Report-Generierung, externe
// API-Aufrufe oder Berechnungs-Endpunkte.
type SystemService struct {
	startedAt      time.Time
	agentPort      func() int
	agentURL       func() string
	agentProxyHint func() bool
}

func NewSystemService() *SystemService {
	return &SystemService{startedAt: time.Now()}
}

// WithAgentPort hinterlegt, woher der dedizierte Agent-Port (LCM Remote)
// kommt, damit die UI die Enroll-URL für neue Agent-Server auf diesen Port
// zeigen lassen kann. Der Port ändert sich zur Laufzeit (Einstellungen), darum
// eine Funktion. 0 = kein dedizierter Agent-Listener aktiv.
func (s *SystemService) WithAgentPort(port func() int) *SystemService {
	s.agentPort = port
	return s
}

// WithAgentURL hinterlegt, woher die öffentliche Adresse des Agent-Ports kommt
// (Einstellung public_agent_url). Gesetzt ersetzt sie in der UI Host und Port.
func (s *SystemService) WithAgentURL(fn func() string) *SystemService {
	s.agentURL = fn
	return s
}

// WithAgentProxyHint hinterlegt, ob die Agent-Adresse ohne Port einen Reverse
// Proxy auf 443 voraussetzt (SettingsService.AgentURLNeedsProxy).
func (s *SystemService) WithAgentProxyHint(fn func() bool) *SystemService {
	s.agentProxyHint = fn
	return s
}

// SystemInfo ist die Antwort von GET /api/v1/system/info.
type SystemInfo struct {
	Version string `json:"version"`  // Semantic Version (Datei VERSION)
	Build   string `json:"build"`    // fortlaufende Build-Nummer
	BuiltAt string `json:"built_at"` // UTC-Zeitpunkt des Builds
	// Commit ist der Git-Commit des Builds (gekürzt). Version und Build-Nummer
	// allein sagen NICHT eindeutig, welcher Quellstand läuft - erst der Commit
	// macht eine laufende Instanz einem Repository-Stand zuordenbar.
	Commit string `json:"commit"`
	// Dirty meldet einen Build aus einem Arbeitsbaum mit uncommitteten
	// Änderungen - dann entspricht das Binary KEINEM Commit des Repositorys.
	Dirty         bool   `json:"dirty"`
	GoVersion     string `json:"go_version"`
	Platform      string `json:"platform"` // z.B. linux/arm64
	UptimeSeconds int64  `json:"uptime_seconds"`
	// AgentPort ist der dedizierte Port des lcm-agent-Listeners (LCM Remote).
	// Die UI baut daraus die Enroll-URL (gleicher Host wie im Browser, aber
	// dieser Port). 0 = kein dedizierter Agent-Listener aktiv.
	AgentPort int `json:"agent_port"`
	// AgentURL ist die öffentliche Adresse des Agent-Ports, wenn ein Reverse
	// Proxy ihn unter eigenem Namen veröffentlicht; leer = Host und AgentPort.
	AgentURL string `json:"agent_url"`
	// AgentURLNeedsProxy: AgentURL hat keinen Port, LCM lauscht aber nicht
	// auf 443 - der Enroll-Befehl geht nur mit einem Reverse Proxy davor.
	AgentURLNeedsProxy bool `json:"agent_url_needs_proxy"`
}

// Info sammelt Version und Laufzeitdaten der Anwendung.
func (s *SystemService) Info() SystemInfo {
	return SystemInfo{
		Version:       version.Version,
		Build:         version.Build,
		BuiltAt:       version.BuiltAt,
		Commit:        version.ShortCommit(),
		Dirty:         version.IsDirty(),
		GoVersion:     runtime.Version(),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		AgentPort:     s.currentAgentPort(),
		AgentURL:      s.publicAgentURL(),

		AgentURLNeedsProxy: s.agentProxyHint != nil && s.agentProxyHint(),
	}
}

func (s *SystemService) currentAgentPort() int {
	if s.agentPort == nil {
		return 0
	}
	return s.agentPort()
}

func (s *SystemService) publicAgentURL() string {
	if s.agentURL == nil {
		return ""
	}
	return s.agentURL()
}
