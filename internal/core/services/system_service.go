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
	startedAt time.Time
	agentPort int
}

func NewSystemService() *SystemService {
	return &SystemService{startedAt: time.Now()}
}

// WithAgentPort hinterlegt den dedizierten Agent-Port (LCM Remote), damit die
// UI die Enroll-URL für neue Agent-Server auf diesen Port zeigen lassen kann.
// 0 = kein dedizierter Agent-Listener aktiv.
func (s *SystemService) WithAgentPort(port int) *SystemService {
	s.agentPort = port
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
		AgentPort:     s.agentPort,
	}
}
