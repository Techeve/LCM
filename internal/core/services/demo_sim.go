package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Demo-Simulation: Demo-Server (IsDemo) werden nie per SSH kontaktiert.
// Damit sich Aktionen in der Demo trotzdem „echt" anfühlen, erzeugen die
// Funktionen hier plausiblen Konsolen-Output UND ziehen den Datenbestand
// nach - ein simuliertes Update hebt die Paketstände wirklich an, ein
// simulierter Neustart löscht reboot_required. Die UI zeigt danach exakt
// das, was ein echter Lauf gezeigt hätte.

// demoSimulatePackageUpdate spielt alle ausstehenden Updates eines
// Demo-Servers „ein": Output im Stil des Paketmanagers, Paketstand auf die
// Kandidaten-Versionen gehoben, dadurch geschlossene CVEs entfernt und bei
// Kernel-Paketen reboot_required gesetzt.
func demoSimulatePackageUpdate(servers *repositories.ServerRepository, server *domain.Server) string {
	pkgs, err := servers.FindPackages(server.ID)
	if err != nil {
		return "demo-server: paket-update simuliert (kein ssh-kontakt)"
	}
	var updated []domain.Package
	for i := range pkgs {
		if pkgs[i].CandidateVersion != "" {
			updated = append(updated, pkgs[i])
			pkgs[i].Version = pkgs[i].CandidateVersion
			pkgs[i].CandidateVersion = ""
			pkgs[i].Security = false
		}
	}
	if len(updated) == 0 {
		return demoPkgOutputNothing(server.PackageManager)
	}
	if err := servers.ReplacePackages(server.ID, pkgs); err != nil {
		return "demo-server: paket-update simuliert (kein ssh-kontakt)"
	}
	demoDropFixedCVEs(servers, server.ID, updated)
	if demoContainsKernel(updated) {
		_ = servers.UpdateFields(server.ID, map[string]any{"reboot_required": true})
	}
	return demoPkgOutput(server.PackageManager, updated)
}

// demoDropFixedCVEs entfernt OS-CVEs, deren Paket soeben auf eine Version mit
// verfügbarem Fix gehoben wurde. Docker-Funde bleiben unberührt.
func demoDropFixedCVEs(servers *repositories.ServerRepository, serverID uint, updated []domain.Package) {
	vulns, err := servers.FindVulnerabilities(serverID)
	if err != nil {
		return
	}
	updatedNames := map[string]bool{}
	for _, p := range updated {
		updatedNames[p.Name] = true
	}
	var remaining []domain.Vulnerability
	for _, v := range vulns {
		if v.Source == domain.VulnSourceDocker {
			continue // eigene Quelle, wird hier nicht ersetzt
		}
		if v.FixedVersion != "" && updatedNames[v.PackageName] {
			continue // durch das Update geschlossen
		}
		remaining = append(remaining, v)
	}
	_ = servers.ReplaceVulnerabilities(serverID, "os", remaining)
}

func demoContainsKernel(pkgs []domain.Package) bool {
	for _, p := range pkgs {
		if strings.Contains(p.Name, "linux-image") || strings.Contains(p.Name, "kernel") {
			return true
		}
	}
	return false
}

func demoPkgOutputNothing(manager string) string {
	switch manager {
	case "dnf", "yum":
		return "Dependencies resolved.\nNothing to do.\nComplete!"
	case "zypper":
		return "Loading repository data...\nReading installed packages...\nNothing to do."
	default:
		return "Reading package lists...\nBuilding dependency tree...\nReading state information...\n" +
			"0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded."
	}
}

func demoPkgOutput(manager string, updated []domain.Package) string {
	var b strings.Builder
	switch manager {
	case "dnf", "yum":
		b.WriteString("Dependencies resolved.\nUpgrading:\n")
		for _, p := range updated {
			fmt.Fprintf(&b, " %-24s x86_64  %s\n", p.Name, p.CandidateVersion)
		}
		fmt.Fprintf(&b, "Transaction Summary: %d Package(s) upgraded\nComplete!", len(updated))
	case "zypper":
		b.WriteString("Loading repository data...\nReading installed packages...\n")
		fmt.Fprintf(&b, "The following %d packages are going to be upgraded:\n  ", len(updated))
		for i, p := range updated {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(p.Name)
		}
		b.WriteString("\n")
		for _, p := range updated {
			fmt.Fprintf(&b, "Installing: %s-%s ... done\n", p.Name, p.CandidateVersion)
		}
	default: // apt
		b.WriteString("Reading package lists...\nBuilding dependency tree...\nReading state information...\n")
		b.WriteString("The following packages will be upgraded:\n  ")
		for i, p := range updated {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(p.Name)
		}
		fmt.Fprintf(&b, "\n%d upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n", len(updated))
		for _, p := range updated {
			fmt.Fprintf(&b, "Unpacking %s (%s) over (%s) ...\n", p.Name, p.CandidateVersion, p.Version)
		}
		for _, p := range updated {
			fmt.Fprintf(&b, "Setting up %s (%s) ...\n", p.Name, p.CandidateVersion)
		}
	}
	return b.String()
}

// demoSimulateReboot „startet" einen Demo-Server neu: reboot_required wird
// gelöscht, und läuft ein älterer als der neueste installierte Kernel, bootet
// der Server in den neuesten (der pve01-Fall der Demodaten).
func demoSimulateReboot(servers *repositories.ServerRepository, server *domain.Server) string {
	fields := map[string]any{"reboot_required": false}
	booted := server.KernelVersion
	if server.InstalledKernels != "" {
		var kernels []domain.KernelPackage
		if json.Unmarshal([]byte(server.InstalledKernels), &kernels) == nil &&
			len(kernels) > 0 && kernels[0].Release != "" {
			booted = kernels[0].Release
			fields["kernel_version"] = booted
		}
	}
	_ = servers.UpdateFields(server.ID, fields)
	return "$ systemctl reboot\nVerbindung getrennt - Server startet neu ...\n" +
		fmt.Sprintf("Server wieder erreichbar (Kernel %s).", booted)
}

// demoSimulateDockerUpdate zieht auf einem Demo-Server alle Images mit
// verfügbarem Update auf den neuen Digest und meldet Compose-Container als
// frisch gestartet.
func demoSimulateDockerUpdate(servers *repositories.ServerRepository, server *domain.Server) string {
	images, err := servers.FindDockerImages(server.ID)
	if err != nil {
		return "demo-server: docker-aktion simuliert (kein ssh-kontakt)"
	}
	var b strings.Builder
	b.WriteString("$ docker compose pull && docker compose up -d\n")
	pulled := 0
	for _, img := range images {
		if !img.UpdateAvailable {
			continue
		}
		pulled++
		fmt.Fprintf(&b, "Pulling %s:%s ... done\n", img.Repository, img.Tag)
		_ = servers.UpdateDockerImageCheck(img.ID, map[string]any{
			"repo_digest": img.CandidateDigest, "update_available": false,
		})
	}
	if pulled == 0 {
		b.WriteString("Images sind aktuell - nichts zu tun.\n")
	}
	demoRestartComposeContainers(servers, server.ID, &b)
	return strings.TrimRight(b.String(), "\n")
}

func demoRestartComposeContainers(servers *repositories.ServerRepository, serverID uint, b *strings.Builder) {
	containers, err := servers.FindDockerContainers(serverID)
	if err != nil {
		return
	}
	changed := false
	for i := range containers {
		if containers[i].ComposeProject == "" || containers[i].State != "running" {
			continue
		}
		containers[i].Status = "Up 1 second"
		fmt.Fprintf(b, "Recreating %s ... done\n", containers[i].Name)
		changed = true
	}
	if changed {
		_ = servers.ReplaceDockerContainers(serverID, containers)
	}
}

// demoRuleOutput liefert für eine Rule auf einem Demo-Server den simulierten
// Job-Output - inklusive der Datenbestand-Effekte der jeweiligen Aktion.
func demoRuleOutput(servers *repositories.ServerRepository, server *domain.Server, rule *domain.Rule) string {
	switch rule.Type {
	case domain.RuleTypeUpdate, domain.RuleTypeSecurity, domain.RuleTypePackages:
		return demoSimulatePackageUpdate(servers, server)
	case domain.RuleTypeReboot:
		return demoSimulateReboot(servers, server)
	case domain.RuleTypeRebootIfNeeded:
		if !server.RebootRequired {
			return "Kein Neustart erforderlich - nichts zu tun."
		}
		return demoSimulateReboot(servers, server)
	case domain.RuleTypeDockerUpdate:
		return demoSimulateDockerUpdate(servers, server)
	case domain.RuleTypeHealth:
		return "lcm-health-ok"
	case domain.RuleTypeScript:
		return "$ " + rule.Command + "\n(Demo-Server - Ausführung simuliert, Exit 0)"
	default:
		return "demo-server: ausführung simuliert (kein ssh-kontakt)"
	}
}
