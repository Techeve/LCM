package services

import "LCM/internal/storage/repositories"

// PackageService liefert die infrastrukturweiten Paket-Sichten
// (welche Pakete existieren, welche kritischen Updates offen sind).
type PackageService struct {
	servers *repositories.ServerRepository
}

func NewPackageService(servers *repositories.ServerRepository) *PackageService {
	return &PackageService{servers: servers}
}

// Overview zeigt alle Pakete über die (sichtbaren) Server hinweg.
func (s *PackageService) Overview(scope repositories.AccessScope) ([]repositories.PackageOverviewRow, error) {
	return s.servers.GlobalPackageOverview(scope)
}

// Vulnerable listet kritische Security-Updates und betroffene Server.
func (s *PackageService) Vulnerable(scope repositories.AccessScope) ([]repositories.VulnerablePackageRow, error) {
	return s.servers.GlobalVulnerablePackages(scope)
}

// Vulnerabilities listet die per Trivy gefundenen CVEs über alle sichtbaren
// Server hinweg (kritischste zuerst).
func (s *PackageService) Vulnerabilities(scope repositories.AccessScope) ([]repositories.CVERow, error) {
	return s.servers.GlobalVulnerabilities(scope)
}

// VulnerabilitiesPage liefert die CVEs seitenweise (kritischste zuerst) plus
// Gesamtzahl und Schwere-Summary - für die paginierte Security-Seite. source
// filtert die Quelle ("os" = nur nativ, "docker" = nur Container, "" = alle).
func (s *PackageService) VulnerabilitiesPage(scope repositories.AccessScope, page, pageSize int, source string) (*repositories.VulnPage, error) {
	return s.servers.GlobalVulnerabilitiesPage(scope, page, pageSize, source)
}

// DockerOverview listet die einzigartigen Docker-Images über alle
// sichtbaren Server hinweg (mit Update-/CVE-Status).
func (s *PackageService) DockerOverview(scope repositories.AccessScope) ([]repositories.DockerImageOverviewRow, error) {
	return s.servers.GlobalDockerOverview(scope)
}

// DockerContainers listet die Container aller sichtbaren Server.
func (s *PackageService) DockerContainers(scope repositories.AccessScope) ([]repositories.DockerContainerRow, error) {
	return s.servers.GlobalDockerContainers(scope)
}

// DockerCompose gruppiert die Container zu Compose-Projekten.
func (s *PackageService) DockerCompose(scope repositories.AccessScope) ([]repositories.DockerComposeRow, error) {
	return s.servers.GlobalDockerCompose(scope)
}
