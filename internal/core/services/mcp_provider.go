package services

import (
	"errors"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/mcp"
	"LCM/internal/storage/repositories"
)

// MCPProvider implementiert mcp.Provider: er baut die read-only ServerViews
// aus dem ServerService. Die Whitelist (welche Felder ausgegeben werden)
// steckt vollständig in buildServerView - Geheimnisse werden hier bewusst
// nicht kopiert.
type MCPProvider struct {
	svc *ServerService
}

// MCPProvider liefert die Provider-Sicht für die MCP-Schnittstelle. Die
// MCP-Tools sehen die Server im Rechte-Kontext ALLER Server (der MCP-Key wird
// von einem settings:manage-Admin erzeugt); Zugangsdaten werden nie exponiert.
func (s *ServerService) MCPProvider() mcp.Provider {
	return &MCPProvider{svc: s}
}

// Servers liefert die Sichten aller Server.
func (p *MCPProvider) Servers() ([]mcp.ServerView, error) {
	servers, err := p.svc.List(repositories.ScopeAll())
	if err != nil {
		return nil, err
	}
	views := make([]mcp.ServerView, 0, len(servers))
	for i := range servers {
		views = append(views, p.buildServerView(&servers[i]))
	}
	return views, nil
}

// Server liefert eine Server-Sicht per numerischer ID oder exaktem Namen.
func (p *MCPProvider) Server(idOrName string) (*mcp.ServerView, bool, error) {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return nil, false, nil
	}
	// Erst als numerische ID versuchen, sonst über den Namen suchen.
	if id, err := strconv.ParseUint(idOrName, 10, 64); err == nil {
		server, err := p.svc.servers.FindByID(repositories.ScopeAll(), uint(id))
		if err != nil {
			if isNotFound(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		v := p.buildServerView(server)
		return &v, true, nil
	}
	server, err := p.svc.servers.FindByName(idOrName)
	if err != nil {
		if isNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	v := p.buildServerView(server)
	return &v, true, nil
}

// isNotFound kapselt die Repository-NotFound-Prüfung (errors.Is, damit auch
// gewrappte Fehler erkannt werden - wie überall sonst im Projekt).
func isNotFound(err error) bool {
	return errors.Is(err, repositories.ErrNotFound)
}

// buildServerView ist die EINZIGE Stelle, die domain.Server → mcp.ServerView
// abbildet. Sie kopiert ausschließlich unbedenkliche Felder - niemals
// ServiceUser, HostKeyFingerprint, PublicKey, PrivateKeyEnc, LoginPasswordEnc,
// AgentTokenHash/-Prefix, AgentID o. Ä.
func (p *MCPProvider) buildServerView(s *domain.Server) mcp.ServerView {
	v := mcp.ServerView{
		ID:          s.ID,
		Name:        s.Name,
		Host:        s.Host,
		IPAddresses: s.IPAddresses,
		OSName:      s.OSName,
		OSVersion:   s.OSVersion,
		OSID:        s.OSID,
		Transport:   s.Transport,
		Reachable:   s.Reachable,
		LastSeenAt:  s.LastSeenAt,

		CPUModel:         s.CPUModel,
		CPUCores:         s.CPUCores,
		MemTotalMB:       s.MemTotalMB,
		MemUsedMB:        s.MemUsedMB,
		DiskTotalMB:      s.DiskTotalMB,
		DiskUsedMB:       s.DiskUsedMB,
		DiskUsagePercent: s.DiskUsagePercent(),
		KernelVersion:    s.KernelVersion,
		Virtualization:   s.Virtualization,

		RebootRequired:          s.RebootRequired,
		RouterOSChannel:         s.RouterOSChannel,
		RouterOSLatestVersion:   s.RouterOSLatestVersion,
		RouterOSUpdateAvailable: s.RouterOSUpdateAvailable,

		SSHHardened:    s.SSHHardened,
		FirewallActive: s.FirewallActive,
		FirewallTool:   s.FirewallTool,
		HardeningIndex: s.HardeningIndex,
		ProxmoxType:    s.ProxmoxType,
	}
	if s.Transport == "" {
		v.Transport = domain.TransportSSH
	}

	// Ampel-Status + Insights (identische Berechnung wie die REST-Statussicht).
	if status, insights, _, err := p.svc.Status(repositories.ScopeAll(), s.ID); err == nil {
		v.Status = status
		for _, in := range insights {
			v.Insights = append(v.Insights, in.Message)
		}
	}
	// Überfällige Paket-Updates.
	if n, err := p.svc.servers.CountOutdatedPackages(s.ID); err == nil {
		v.OutdatedPackages = int(n)
	}
	// Gewichtete CVE-Zählung (Docker-CVEs nur für relevante Container) - exakt
	// wie in ServerService.Status.
	if facts, err := p.svc.servers.VulnerabilityFacts(s.ID); err == nil {
		relevantRefs := dockerRelevantRefs(p.svc.servers, s)
		weighted := weightedVulnSummary(facts, p.svc.cveWeightList(), splitCSVList(s.ListeningPackages), relevantRefs)
		v.CVECritical = weighted[domain.SeverityCritical]
		v.CVEHigh = weighted[domain.SeverityHigh]
	}
	return v
}
