package storage

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// seedDemo legt umfassende Testdaten an (--demo): Demo-Server mit
// simulierter Hardware und Paketen, eine Servergruppe mit Rule,
// Job-Historien und Fake-User. Demo-Server tragen IsDemo=true und werden
// nie per SSH kontaktiert. So lässt sich die komplette UI/UX ausprobieren,
// ohne echte Linux-Server anzubinden.
func seedDemo(db *gorm.DB, roleRepo *repositories.RoleRepository) error {
	// Referenzzeitpunkt (relativ, nicht Date.now - deterministischer Seed).
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// 1a. LCM-Verwaltungs-User (Manager) - Login-Benutzer.
	managerRole, err := roleRepo.FindByName(domain.RoleManager)
	if err != nil {
		return err
	}
	mgrHash, _ := services.HashPassword("demo-passwort-1234")
	if err := db.Create(&domain.User{
		Username: "ops.manager", Email: "ops.manager@demo.local",
		PasswordHash: mgrHash, FirstName: "Olivia", LastName: "Ops",
		Active: true, Roles: []domain.Role{*managerRole},
	}).Error; err != nil {
		return fmt.Errorf("demo-manager: %w", err)
	}

	// 1b. Linux-Benutzer (Betriebssystem-Accounts für die Verteilung).
	linuxUsers := []domain.LinuxUser{
		{Username: "deploy", FullName: "Deployment Bot", Shell: "/bin/bash", Sudo: true, Active: true,
			SSHKeys: []domain.LinuxUserSSHKey{{Name: "CI-Key", PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDEMOdeploy deploy@ci"}}},
		{Username: "mmuster", FullName: "Max Mustermann", Shell: "/bin/bash", Active: true,
			SSHKeys: []domain.LinuxUserSSHKey{{Name: "Laptop", PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDEMOmmuster mmuster@laptop"}}},
		// anna ist zugleich als Konto auf web01 erfasst - so zeigt die Demo
		// ein VERWALTETES Konto: mit LCM-Kennzeichen, sperrbar, aber nicht
		// endgültig entfernbar (das legte der nächste Sync wieder an).
		{Username: "anna", FullName: "Anna Admin", Shell: "/bin/bash", Active: true,
			SSHKeys: []domain.LinuxUserSSHKey{{Name: "Laptop", PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDEMOanna anna@laptop"}}},
	}
	for i := range linuxUsers {
		if err := db.Create(&linuxUsers[i]).Error; err != nil {
			return fmt.Errorf("demo-linux-user: %w", err)
		}
	}

	// 2. Demo-Server mit simulierter Hardware.
	// edgeSeen: letzter Kontakt des (offline, aber tolerierten) edge01.
	edgeSeen := base
	demoServers := []domain.Server{
		{
			Name: "web01", Host: "10.10.0.11", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
			HostKeyFingerprint: "SHA256:DEMOweb01aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PrivateKeyEnc: "demo", PublicKey: "ssh-ed25519 DEMO web01",
			OSName: "Debian GNU/Linux", OSVersion: "12 (bookworm)", OSID: "debian", OSVersionID: "12",
			Virtualization: "kvm", PackageManager: "apt", HasDocker: true, HasCompose: true, KernelVersion: "6.1.0-13-amd64",
			// Debian-Fall: der laufende Kernel ist der neueste, ein aelterer
			// bleibt als Rueckfallebene liegen.
			InstalledKernels: `[{"name":"linux-image-6.1.0-13-amd64","release":"6.1.0-13-amd64","version":"6.1.55-1"},` +
				`{"name":"linux-image-6.1.0-10-amd64","release":"6.1.0-10-amd64","version":"6.1.38-4"}]`,
			CPUModel: "Intel Xeon E5-2670", CPUCores: 4, MemTotalMB: 8192, MemUsedMB: 3120,
			DiskTotalMB: 40960, DiskUsedMB: 12800, IPAddresses: "10.10.0.11",
			// Lauschende Dienste (automatische CVE-Hochgewichtung in der Demo).
			ListeningPackages: "nginx, openssh-server",
			// Firewall: Debian → nftables-Backend; Regel-Konfiguration + Port-
			// Inventar (Vorschlags-Chips im Firewall-Dialog) für die Demo.
			FirewallTool:         "nftables",
			FirewallAllowedPorts: "80,443",
			FirewallRules:        `[{"port":80,"proto":"tcp","ip_version":"any"},{"port":443,"proto":"tcp","ip_version":"any"}]`,
			ListeningPorts:       `[{"port":22,"proto":"tcp","bind":"0.0.0.0","ip_version":"v4","process":"sshd"},{"port":80,"proto":"tcp","bind":"0.0.0.0","ip_version":"v4","process":"nginx"},{"port":443,"proto":"tcp","bind":"0.0.0.0","ip_version":"v4","process":"nginx"},{"port":9100,"proto":"tcp","bind":"10.10.0.11","ip_version":"v4","process":"node_exporter"},{"port":53,"proto":"udp","bind":"::","ip_version":"v6","process":"systemd-resolve"}]`,
			// Quell-IP, mit der LCM diesen Server erreicht - sie ist im
			// Firewall-Dialog die Vorlage für die SSH-Quellen (und die
			// Grundlage der Aussperr-Warnung).
			LCMSourceIP: "10.10.0.2",
			Reachable:   true, SSHHardened: true, FirewallActive: true, IsDemo: true,
			// Brute-Force-Schutz: web01 zeigt die fail2ban-Verwaltung im
			// Sicherheit-Tab (db01 zeigt dieselbe Karte für CrowdSec).
			Fail2banInstalled: true, Fail2banActive: true,
		},
		{
			Name: "db01", Host: "10.10.0.12", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
			HostKeyFingerprint: "SHA256:DEMOdb01bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PrivateKeyEnc: "demo", PublicKey: "ssh-ed25519 DEMO db01",
			OSName: "Ubuntu", OSVersion: "22.04 LTS", OSID: "ubuntu", OSVersionID: "22.04",
			Virtualization: "lxc", PackageManager: "apt", HasSnap: true, HasDocker: true, KernelVersion: "5.15.0-91-generic",
			CPUModel: "AMD EPYC 7401", CPUCores: 8, MemTotalMB: 32768, MemUsedMB: 21500,
			DiskTotalMB: 102400, DiskUsedMB: 91000, IPAddresses: "10.10.0.12", // fast volle Platte => gelb
			// Ubuntu-typisch: /var/run/reboot-required nach Kernel-Update gesetzt.
			RebootRequired: true,
			// ufw installiert, aber inaktiv - der Dialog zeigt Backend-Badge
			// und Vorschläge aus dem Port-Inventar (mysqld mit Bind-Adresse).
			FirewallTool:   "ufw",
			ListeningPorts: `[{"port":22,"proto":"tcp","bind":"0.0.0.0","ip_version":"v4","process":"sshd"},{"port":3306,"proto":"tcp","bind":"10.10.0.12","ip_version":"v4","process":"mysqld"}]`,
			Reachable:      true, SSHHardened: true, FirewallActive: false, IsDemo: true,
			// CrowdSec installiert, Dienst aber gestoppt - zeigt in der
			// Verwaltungs-Karte den inaktiven Zustand samt „Starten".
			CrowdSecInstalled: true, CrowdSecActive: false,
		},
		{
			Name: "cache01", Host: "10.10.0.13", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
			HostKeyFingerprint: "SHA256:DEMOcache01cccccccccccccccccccccccccccc", PrivateKeyEnc: "demo", PublicKey: "ssh-ed25519 DEMO cache01",
			OSName: "Debian GNU/Linux", OSVersion: "11 (bullseye)", OSID: "debian", OSVersionID: "11",
			Virtualization: "none", PackageManager: "apt", KernelVersion: "5.10.0-27-amd64",
			CPUModel: "Intel Xeon E5-2670", CPUCores: 2, MemTotalMB: 4096, MemUsedMB: 1800,
			DiskTotalMB: 20480, DiskUsedMB: 4000, IPAddresses: "10.10.0.13",
			// Mehrere fehlgeschlagene Kontakte in Folge => Offline-Kennzeichen,
			// obwohl der Server ganz normal rot ist (nicht „unkritisch").
			Reachable: false, FailedChecks: 7,
			LastError: "Verbindung abgelehnt (Host offline)", IsDemo: true, // rot
		},
		{
			Name: "rocky01", Host: "10.10.0.21", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
			HostKeyFingerprint: "SHA256:DEMOrocky01dddddddddddddddddddddddddddd", PrivateKeyEnc: "demo", PublicKey: "ssh-ed25519 DEMO rocky01",
			OSName: "Rocky Linux", OSVersion: "9.3 (Blue Onyx)", OSID: "rocky", OSVersionID: "9.3",
			Virtualization: "kvm", PackageManager: "dnf", KernelVersion: "5.14.0-362.el9",
			CPUModel: "AMD EPYC 7302", CPUCores: 4, MemTotalMB: 16384, MemUsedMB: 5200,
			DiskTotalMB: 51200, DiskUsedMB: 15000, IPAddresses: "10.10.0.21",
			Reachable: true, SSHHardened: true, FirewallActive: true, IsDemo: true,
		},
		{
			Name: "suse01", Host: "10.10.0.22", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
			HostKeyFingerprint: "SHA256:DEMOsuse01eeeeeeeeeeeeeeeeeeeeeeeeeeeeee", PrivateKeyEnc: "demo", PublicKey: "ssh-ed25519 DEMO suse01",
			OSName: "openSUSE Leap", OSVersion: "15.5", OSID: "opensuse-leap", OSVersionID: "15.5",
			Virtualization: "lxc", PackageManager: "zypper", KernelVersion: "5.14.21-150500",
			CPUModel: "Intel Xeon Gold 6248", CPUCores: 2, MemTotalMB: 8192, MemUsedMB: 2600,
			DiskTotalMB: 40960, DiskUsedMB: 9000, IPAddresses: "10.10.0.22",
			Reachable: true, SSHHardened: false, FirewallActive: false, IsDemo: true,
		},
		{
			// Proxmox-Host: Debian-basiert, aber als Proxmox VE erkannt -
			// zeigt Logo/Typ und die gesperrten Aktionen (Repos/Firewall/
			// Benutzer-Sync) in der Demo.
			Name: "pve01", Host: "10.10.0.30", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
			HostKeyFingerprint: "SHA256:DEMOpve01ffffffffffffffffffffffffffffff", PrivateKeyEnc: "demo", PublicKey: "ssh-ed25519 DEMO pve01",
			OSName: "Debian GNU/Linux", OSVersion: "12 (bookworm)", OSID: "debian", OSVersionID: "12",
			ProxmoxType: domain.ProxmoxPVE, ProxmoxVersion: "8.2.4",
			Virtualization: "none", PackageManager: "apt", KernelVersion: "6.8.12-1-pve",
			// Proxmox behaelt mehrere Kernel als Rueckfallebene. Der laufende
			// ist hier bewusst NICHT der neueste - genau die Lage, die man
			// erkennen koennen muss (installiert != gebootet). Der aelteste
			// ist ebenso bewusst einer zu viel: Er zeigt, was die Aktion
			// „Alte Kernel entfernen" aufraeumt (neuer, laufender und die
			// naechstaeltere Rueckfallebene bleiben).
			InstalledKernels: `[{"name":"proxmox-kernel-6.8.12-4-pve","release":"6.8.12-4-pve","version":"6.8.12-4"},` +
				`{"name":"proxmox-kernel-6.8.12-1-pve","release":"6.8.12-1-pve","version":"6.8.12-1"},` +
				`{"name":"pve-kernel-6.5.13-5-pve","release":"6.5.13-5-pve","version":"6.5.13-5"},` +
				`{"name":"pve-kernel-6.5.11-4-pve","release":"6.5.11-4-pve","version":"6.5.11-4"}]`,
			CPUModel: "AMD EPYC 7443P", CPUCores: 24, MemTotalMB: 131072, MemUsedMB: 74000,
			DiskTotalMB: 512000, DiskUsedMB: 180000, IPAddresses: "10.10.0.30",
			Reachable: true, SSHHardened: true, FirewallActive: false, IsDemo: true,
		},
		{
			// edge01: ein Roaming-/Außenstellen-Server, der oft offline ist. Er
			// ist als „Nichterreichbarkeit unkritisch" markiert und behält daher
			// im Dashboard seinen (grünen) Status - nur ausgegraut statt rot,
			// bis die (großzügige) Kulanzfrist abläuft.
			Name: "edge01", Host: "10.20.0.9", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
			HostKeyFingerprint: "SHA256:DEMOedge01eeeeeeeeeeeeeeeeeeeeeeeeeeeeee", PrivateKeyEnc: "demo", PublicKey: "ssh-ed25519 DEMO edge01",
			OSName: "Ubuntu", OSVersion: "24.04 LTS", OSID: "ubuntu", OSVersionID: "24.04",
			Virtualization: "kvm", PackageManager: "apt", KernelVersion: "6.8.0-31-generic",
			CPUModel: "Intel N100", CPUCores: 4, MemTotalMB: 8192, MemUsedMB: 2600,
			DiskTotalMB: 61440, DiskUsedMB: 15000, IPAddresses: "10.20.0.9",
			Reachable: false, FailedChecks: 12, LastSeenAt: &edgeSeen, LastError: "Zeitüberschreitung (Host offline)",
			SSHHardened: true, FirewallActive: true,
			UnreachableUncritical: true, UnreachableGraceDays: 365, IsDemo: true,
		},
		{
			// lcm-self: der LCM-Host selbst (Host localhost). Zeigt das LCM-Logo
			// statt eines OS-Logos und bietet in der Übersicht die Einrichtung
			// von Trivy und apt-cacher-ng an.
			Name: "lcm-self", Host: "localhost", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
			HostKeyFingerprint: "SHA256:DEMOlcmselflllllllllllllllllllllllllll", PrivateKeyEnc: "demo", PublicKey: "ssh-ed25519 DEMO lcm-self",
			OSName: "Debian GNU/Linux", OSVersion: "12 (bookworm)", OSID: "debian", OSVersionID: "12",
			Virtualization: "none", PackageManager: "apt", KernelVersion: "6.1.0-13-amd64",
			CPUModel: "Intel Core i5", CPUCores: 4, MemTotalMB: 16384, MemUsedMB: 5200,
			DiskTotalMB: 81920, DiskUsedMB: 22000, IPAddresses: "127.0.0.1",
			Reachable: true, SSHHardened: true, FirewallActive: true, IsDemo: true,
		},
	}
	for i := range demoServers {
		if err := db.Create(&demoServers[i]).Error; err != nil {
			return fmt.Errorf("demo-server: %w", err)
		}
	}

	// 3. Pakete (web01 mit einem Security-Update => gelb).
	db.Create(&[]domain.Package{
		{ServerRef: domain.ServerRef(demoServers[0].ID), Name: "nginx", Version: "1.22.1-9"},
		{ServerRef: domain.ServerRef(demoServers[0].ID), Name: "openssl", Version: "3.0.11-1", CandidateVersion: "3.0.14-1", Security: true},
		{ServerRef: domain.ServerRef(demoServers[0].ID), Name: "curl", Version: "7.88.1-10"},
		{ServerRef: domain.ServerRef(demoServers[1].ID), Name: "postgresql-14", Version: "14.10-0ubuntu0.22.04.1"},
		{ServerRef: domain.ServerRef(demoServers[1].ID), Name: "openssh-server", Version: "8.9p1-3"},
		// rocky01 (dnf/RPM): ein Paket mit verfügbarem Update.
		{ServerRef: domain.ServerRef(demoServers[3].ID), Name: "nginx", Version: "1.20.1-14.el9"},
		{ServerRef: domain.ServerRef(demoServers[3].ID), Name: "openssl", Version: "3.0.7-24.el9", CandidateVersion: "3.0.7-27.el9", Security: true},
		{ServerRef: domain.ServerRef(demoServers[3].ID), Name: "curl", Version: "7.76.1-23.el9"},
		// suse01 (zypper/RPM).
		{ServerRef: domain.ServerRef(demoServers[4].ID), Name: "nginx", Version: "1.21.5-150400"},
		{ServerRef: domain.ServerRef(demoServers[4].ID), Name: "openSSH", Version: "8.4p1-150300"},
		// Auch die übrigen Demo-Server brauchen einen Bestand: ein Server ohne
		// erfasste Pakete gilt seit BUG-020 zu Recht als unbewertbar (rot),
		// weil sich Updates, CVEs und Support-Ende daraus ableiten. Ohne diese
		// Einträge wären cache01, pve01, edge01 und lcm-self dauerhaft rot und
		// die Demo würde einen Fehlerzustand statt eines Normalbetriebs zeigen.
		{ServerRef: domain.ServerRef(demoServers[2].ID), Name: "redis-server", Version: "7.0.15-1"},
		{ServerRef: domain.ServerRef(demoServers[2].ID), Name: "openssh-server", Version: "9.2p1-2"},
		{ServerRef: domain.ServerRef(demoServers[5].ID), Name: "pve-manager", Version: "8.2.4"},
		{ServerRef: domain.ServerRef(demoServers[5].ID), Name: "openssh-server", Version: "9.2p1-2"},
		{ServerRef: domain.ServerRef(demoServers[6].ID), Name: "nginx", Version: "1.24.0-2"},
		{ServerRef: domain.ServerRef(demoServers[6].ID), Name: "openssh-server", Version: "9.6p1-3"},
		{ServerRef: domain.ServerRef(demoServers[7].ID), Name: "lcm", Version: "1.6.0"},
		{ServerRef: domain.ServerRef(demoServers[7].ID), Name: "openssh-server", Version: "9.2p1-2"},
	})
	db.Create(&[]domain.AptRepository{
		{ServerID: demoServers[0].ID, Line: "deb https://deb.debian.org/debian bookworm main"},
		{ServerID: demoServers[0].ID, Line: "deb http://security.debian.org bookworm-security main", Insecure: true},
		// rocky01: RPM-Repos (baseurl aus /etc/yum.repos.d).
		{ServerID: demoServers[3].ID, Line: "https://download.rockylinux.org/pub/rocky/9/BaseOS/x86_64/os/"},
		{ServerID: demoServers[3].ID, Line: "https://download.rockylinux.org/pub/rocky/9/AppStream/x86_64/os/"},
		// suse01: zypper-Repos (baseurl aus /etc/zypp/repos.d).
		{ServerID: demoServers[4].ID, Line: "https://download.opensuse.org/distribution/leap/15.5/repo/oss/"},
	})

	// Snaps: zweite Paketverwaltung, v.a. Ubuntu (db01). Eins mit Update.
	db.Create(&[]domain.SnapPackage{
		{ServerID: demoServers[1].ID, Name: "core22", Version: "20240111", Revision: "1122", Channel: "latest/stable", Publisher: "canonical"},
		{ServerID: demoServers[1].ID, Name: "lxd", Version: "5.0.2", Revision: "27948", Channel: "5.0/stable", Publisher: "canonical", CandidateVersion: "5.0.3"},
		{ServerID: demoServers[1].ID, Name: "snapd", Version: "2.61.1", Revision: "20671", Channel: "latest/stable", Publisher: "canonical"},
	})

	// Docker-Inventar: web01 betreibt ein Compose-Projekt „webshop"
	// (nginx mit verfügbarem Update, redis aktuell) plus einen
	// Standalone-Container; db01 einen Standalone-Postgres aus einer
	// privaten Registry (Update-Check „nicht prüfbar").
	db.Create(&[]domain.DockerContainer{
		{ServerID: demoServers[0].ID, ContainerID: "demo0000000000000000000000000000000000000000000000000000000web1",
			Name: "webshop-web-1", Image: "nginx:1.25", ImageID: "sha256:demo-nginx", State: "running", Status: "Up 3 days",
			Ports: "0.0.0.0:80->80/tcp", ComposeProject: "webshop", ComposeService: "web",
			ComposeWorkingDir: "/opt/webshop", ComposeConfigFiles: "/opt/webshop/compose.yaml"},
		{ServerID: demoServers[0].ID, ContainerID: "demo0000000000000000000000000000000000000000000000000000000red1",
			Name: "webshop-cache-1", Image: "redis:7", ImageID: "sha256:demo-redis", State: "running", Status: "Up 3 days",
			ComposeProject: "webshop", ComposeService: "cache",
			ComposeWorkingDir: "/opt/webshop", ComposeConfigFiles: "/opt/webshop/compose.yaml"},
		{ServerID: demoServers[0].ID, ContainerID: "demo0000000000000000000000000000000000000000000000000000000wtc1",
			Name: "uptime-kuma", Image: "louislam/uptime-kuma:1", ImageID: "sha256:demo-kuma", State: "exited", Status: "Exited (0) 2 weeks ago"},
		{ServerID: demoServers[1].ID, ContainerID: "demo0000000000000000000000000000000000000000000000000000000pg01",
			Name: "postgres", Image: "registry.firma.intern/db/postgres:16", ImageID: "sha256:demo-pg", State: "running", Status: "Up 5 days",
			Ports: "5432/tcp"},
	})
	db.Create(&[]domain.DockerImage{
		{ServerID: demoServers[0].ID, Repository: "nginx", Tag: "1.25", ImageID: "sha256:demo-nginx",
			RepoDigest: "sha256:demo-nginx-alt", SizeText: "188MB", InUse: true,
			CandidateDigest: "sha256:demo-nginx-neu", UpdateAvailable: true, CheckStatus: domain.DockerCheckOK, LastCheckedAt: &base},
		{ServerID: demoServers[0].ID, Repository: "redis", Tag: "7", ImageID: "sha256:demo-redis",
			RepoDigest: "sha256:demo-redis-akt", SizeText: "117MB", InUse: true,
			CandidateDigest: "sha256:demo-redis-akt", CheckStatus: domain.DockerCheckOK, LastCheckedAt: &base},
		{ServerID: demoServers[0].ID, Repository: "louislam/uptime-kuma", Tag: "1", ImageID: "sha256:demo-kuma",
			RepoDigest: "sha256:demo-kuma-akt", SizeText: "406MB", InUse: true,
			CandidateDigest: "sha256:demo-kuma-akt", CheckStatus: domain.DockerCheckOK, LastCheckedAt: &base},
		{ServerID: demoServers[0].ID, Repository: "meinapp", Tag: "dev", ImageID: "sha256:demo-local",
			SizeText: "92MB", InUse: false, CheckStatus: domain.DockerCheckLocal},
		{ServerID: demoServers[1].ID, Repository: "registry.firma.intern/db/postgres", Tag: "16", ImageID: "sha256:demo-pg",
			RepoDigest: "sha256:demo-pg-priv", SizeText: "431MB", InUse: true,
			CheckStatus: domain.DockerCheckUnauthorized, CheckError: "privates image - anonym nicht prüfbar", LastCheckedAt: &base},
	})

	// CVE-Funde (Trivy-Demo). web01 hat eine KRITISCHE openssl-Lücke → rot.
	db.Create(&[]domain.Vulnerability{
		{ServerRef: domain.ServerRef(demoServers[0].ID), CVEID: "CVE-2023-0286", PackageName: "openssl", InstalledVersion: "3.0.11-1",
			FixedVersion: "3.0.14-1", Severity: domain.SeverityCritical, PkgManager: "apt",
			Title: "X.400 address type confusion in X.509 GeneralName", PrimaryURL: "https://avd.aquasec.com/nvd/cve-2023-0286"},
		{ServerRef: domain.ServerRef(demoServers[0].ID), CVEID: "CVE-2023-44487", PackageName: "nginx", InstalledVersion: "1.22.1-9",
			FixedVersion: "1.24.0-1", Severity: domain.SeverityHigh, PkgManager: "apt",
			Title: "HTTP/2 Rapid Reset Attack", PrimaryURL: "https://avd.aquasec.com/nvd/cve-2023-44487"},
		{ServerRef: domain.ServerRef(demoServers[0].ID), CVEID: "CVE-2023-38545", PackageName: "curl", InstalledVersion: "7.88.1-10",
			FixedVersion: "", Severity: domain.SeverityMedium, PkgManager: "apt",
			Title: "SOCKS5 heap buffer overflow", PrimaryURL: "https://avd.aquasec.com/nvd/cve-2023-38545"},
		// rocky01 (dnf/RPM): eine hohe Lücke.
		{ServerRef: domain.ServerRef(demoServers[3].ID), CVEID: "CVE-2023-3446", PackageName: "openssl", InstalledVersion: "3.0.7-24.el9",
			FixedVersion: "3.0.7-27.el9", Severity: domain.SeverityHigh, PkgManager: "dnf",
			Title: "Excessive time spent checking DH keys and parameters", PrimaryURL: "https://avd.aquasec.com/nvd/cve-2023-3446"},
		// Docker-Image-Scan (Quelle "docker"): eine hohe Lücke im nginx-Image.
		{ServerRef: domain.ServerRef(demoServers[0].ID), CVEID: "CVE-2024-7347", PackageName: "libssl3", InstalledVersion: "3.0.11-1~deb12u2",
			FixedVersion: "3.0.14-1~deb12u2", Severity: domain.SeverityHigh, PkgManager: "docker",
			Source: domain.VulnSourceDocker, ImageRef: "nginx:1.25",
			Title: "nginx: ngx_http_mp4_module buffer overread", PrimaryURL: "https://avd.aquasec.com/nvd/cve-2024-7347"},
	})
	// Reachable Demo-Server gelten als „zuletzt gescannt" (für die UI-Anzeige).
	db.Model(&domain.Server{}).Where("reachable = ?", true).Update("last_cve_scan_at", base)

	// Deep-Scan-Berichte: ZWEI Läufe auf web01, damit die Demo den Punkt der
	// Berichtsansicht zeigt - den Fortschritt zwischen zwei Ständen. Ein
	// einzelner Lauf sähe wieder aus wie die alte flache Befundliste.
	seedDemoDeepScan(db, demoServers[0].ID, base)

	// Speicher-Verlauf: 30 Tage Tagesdurchschnitte, damit der Speicher-Tab
	// eine Entwicklung zeigt. web01 wächst moderat, db01 läuft auf eine fast
	// volle Platte zu (verdeutlicht den steigenden Trend).
	var storageHist []domain.StorageHistory
	for d := 0; d < 30; d++ {
		day := base.AddDate(0, 0, -29+d)
		storageHist = append(storageHist,
			domain.StorageHistory{
				ServerID: demoServers[0].ID, Day: day.Format("2006-01-02"),
				DiskTotalMB: 40960, DiskUsedMB: int64(11200 + d*55),
				Samples: 24, LastSampleAt: day,
			},
			domain.StorageHistory{
				ServerID: demoServers[1].ID, Day: day.Format("2006-01-02"),
				DiskTotalMB: 102400, DiskUsedMB: int64(82000 + d*310),
				Samples: 24, LastSampleAt: day,
			},
		)
	}
	db.Create(&storageHist)

	// Eingehängte Volumes (Dateisysteme). Das Root-Volume „/" spiegelt die
	// maßgeblichen Server-Werte; zusätzliche Volumes zeigen, dass LCM mehrere
	// durchgereichte Dateisysteme überwacht.
	db.Create(&[]domain.DiskVolume{
		{ServerID: demoServers[0].ID, Mountpoint: "/", Device: "/dev/sda1", Fstype: "ext4", TotalMB: 40960, UsedMB: 12800},
		{ServerID: demoServers[0].ID, Mountpoint: "/boot", Device: "/dev/sda2", Fstype: "ext4", TotalMB: 976, UsedMB: 180},
		{ServerID: demoServers[0].ID, Mountpoint: "/data", Device: "/dev/sdb1", Fstype: "xfs", TotalMB: 512000, UsedMB: 384000},
		{ServerID: demoServers[1].ID, Mountpoint: "/", Device: "/dev/vda1", Fstype: "ext4", TotalMB: 102400, UsedMB: 91000},
		{ServerID: demoServers[1].ID, Mountpoint: "/var/lib/mysql", Device: "/dev/mapper/vg-db", Fstype: "ext4", TotalMB: 204800, UsedMB: 153600},
	})

	// Anmeldefähige Linux-Konten (Benutzer-Übersicht): zeigt die typischen
	// Zustände - root mit Passwort, Service-User und ein Nur-Key-Benutzer mit
	// 2FA, ein Altkonto mit Passwort ohne Keys und ein deaktiviertes Konto.
	vorTagen := func(d int) *time.Time { t := time.Now().AddDate(0, 0, -d); return &t }
	db.Create(&[]domain.ServerUser{
		{ServerID: demoServers[0].ID, Username: "root", UID: 0, Shell: "/bin/bash", PasswordStatus: domain.PasswordStatusSet, LastLoginAt: vorTagen(45)},
		{ServerID: demoServers[0].ID, Username: "lcm-svc", UID: 1000, Shell: "/bin/bash", PasswordStatus: domain.PasswordStatusNone, SSHKeyCount: 1},
		{ServerID: demoServers[0].ID, Username: "anna", UID: 1001, Shell: "/bin/bash", PasswordStatus: domain.PasswordStatusNone, SSHKeyCount: 2, TwoFactorEnrolled: true, LastLoginAt: vorTagen(1)},
		{ServerID: demoServers[0].ID, Username: "legacy-app", UID: 1002, Shell: "/bin/sh", PasswordStatus: domain.PasswordStatusSet, LastLoginAt: vorTagen(200)},
		{ServerID: demoServers[1].ID, Username: "root", UID: 0, Shell: "/bin/bash", PasswordStatus: domain.PasswordStatusLocked, SSHKeyCount: 1},
		{ServerID: demoServers[1].ID, Username: "praktikant", UID: 1003, Shell: "/bin/bash", PasswordStatus: domain.PasswordStatusLocked, Disabled: true, LastLoginAt: vorTagen(90)},
	})

	// Anmelde-Historie: mehrere Sitzungen je Konto - genau das, was die
	// Übersicht zeigen soll (nicht nur „zuletzt gesehen").
	vorStunden := func(h int) time.Time { return time.Now().Add(-time.Duration(h) * time.Hour) }
	endeNach := func(t time.Time, min int) *time.Time { e := t.Add(time.Duration(min) * time.Minute); return &e }
	annaGestern := vorStunden(26)
	annaFrueher := vorStunden(30)
	annaJetzt := vorStunden(2)
	rootAlt := vorStunden(24 * 45)
	db.Create(&[]domain.ServerUserLogin{
		{ServerID: demoServers[0].ID, Username: "anna", TTY: "pts/0", FromHost: "192.168.10.27",
			StartedAt: annaJetzt, StillActive: true},
		{ServerID: demoServers[0].ID, Username: "anna", TTY: "pts/1", FromHost: "192.168.10.27",
			StartedAt: annaGestern, EndedAt: endeNach(annaGestern, 95)},
		{ServerID: demoServers[0].ID, Username: "anna", TTY: "pts/0", FromHost: "10.8.0.14",
			StartedAt: annaFrueher, EndedAt: endeNach(annaFrueher, 12)},
		{ServerID: demoServers[0].ID, Username: "root", TTY: "tty1", FromHost: "",
			StartedAt: rootAlt, EndedAt: endeNach(rootAlt, 40)},
	})

	// 4. Servergruppe "Produktion" mit Grundsatz-Regel und den Demo-Servern.
	// Bewusst OHNE eigenen Wartungs-Schedule: Zeitpläne legt der Betreiber
	// selbst an; die Basis-Schedules bringt die System-Gruppe mit.
	group := domain.ServerGroup{Name: "Produktion", Description: "Produktive Server (Demo)"}
	if err := db.Create(&group).Error; err != nil {
		return err
	}
	db.Model(&group).Association("Servers").Append(&demoServers[0], &demoServers[1])
	// Linux-Benutzer der Gruppe zuordnen (werden auf ihre Server verteilt).
	db.Model(&group).Association("LinuxUsers").Append(&linuxUsers[0], &linuxUsers[1])
	// anna direkt an web01 - nicht über die Gruppe, damit die Demo beide
	// Zuordnungswege zeigt (und die Sperre unabhängig vom Weg greift).
	db.Model(&demoServers[0]).Association("LinuxUsers").Append(&linuxUsers[2])
	// Grundsatz-Regel: wird bei jeder Verbindung (Health-Ping) geprüft und
	// bei Abweichung durchgesetzt - hängt an keinem Zeitplan.
	db.Create(&domain.Rule{
		GroupID: group.ID, Enforce: true,
		Name: "Firewall Webserver", Type: domain.RuleTypeFirewall, Command: "80,443", Enabled: true,
	})

	// 5. Job-Historien (Mischung aus Erfolg und Fehler).
	web01 := demoServers[0].ID
	cache01 := demoServers[2].ID
	jobs := []domain.Job{
		{ServerID: &web01, Type: "update", Name: "Tägliche Updates @ web01", Status: domain.JobStatusSuccess,
			TriggeredBy: "scheduler", Output: "apt-get upgrade: 3 Pakete aktualisiert.\n", ExitCode: ptrInt(0),
			StartedAt: ptrTime(base), FinishedAt: ptrTime(base.Add(30 * time.Second))},
		{ServerID: &cache01, Type: "health", Name: "Health-Check (Ping) @ cache01", Status: domain.JobStatusFailed,
			TriggeredBy: "scheduler", Output: "FEHLER: Verbindung abgelehnt (Host offline)",
			StartedAt: ptrTime(base.Add(time.Hour)), FinishedAt: ptrTime(base.Add(time.Hour + 5*time.Second))},
		{ServerID: &web01, Type: "harden-ssh", Name: "SSH-Härtung @ web01", Status: domain.JobStatusSuccess,
			TriggeredBy: "admin", Output: "sshd_config.d/60-lcm-hardening.conf geschrieben, sshd neu geladen.\n", ExitCode: ptrInt(0),
			StartedAt: ptrTime(base.Add(2 * time.Hour)), FinishedAt: ptrTime(base.Add(2*time.Hour + 3*time.Second))},
	}
	for i := range jobs {
		if err := db.Create(&jobs[i]).Error; err != nil {
			return fmt.Errorf("demo-job: %w", err)
		}
	}

	// 6. Benachrichtigungskanal + Alarmregel (damit die Alarme-Seite im Demo
	// bereits gefüllt ist und der Kanal beim Anlegen einer Regel wählbar ist).
	channel := domain.NotificationChannel{
		Name: "Ops-Team (E-Mail)", Type: domain.ChannelTypeEmail, Enabled: true,
		Config: `{"host":"smtp.example.com","port":587,"from":"lcm@example.com","to":"ops@example.com"}`,
	}
	if err := db.Create(&channel).Error; err != nil {
		return fmt.Errorf("demo-channel: %w", err)
	}
	db.Create(&[]domain.AlertRule{
		{Name: "Kritische CVEs melden", Type: domain.AlertTypeSecurityCVE, Enabled: true,
			ChannelID: &channel.ID, Severity: "critical", MinSeverity: domain.SeverityCritical},
		{Name: "Festplatte fast voll", Type: domain.AlertTypeDiskCapacity, Enabled: true,
			ChannelID: &channel.ID, Severity: "warning", ThresholdPercent: 90},
		{Name: "Neustart erforderlich", Type: domain.AlertTypeRebootRequired, Enabled: true,
			ChannelID: &channel.ID, Severity: "warning"},
	})

	// Beispiel-Allowlists (gemeinsamer Pool) - auswählbar in Firewall-Regeln
	// sowie bei fail2ban/CrowdSec.
	db.Create(&[]domain.IPAllowlist{
		{Name: "Büro-Netze", Description: "Standort-Netze (Beispiel)", Entries: "203.0.113.0/24\n2001:db8:1::/48"},
		{Name: "Monitoring", Description: "Prometheus/Grafana (Beispiel)", Entries: "10.10.0.20"},
	})
	return nil
}

// seedDemoDeepScan legt zwei Deep-Scan-Läufe an: einen älteren und einen
// aktuellen. Zwischen beiden wurde eine Härtungslücke geschlossen und eine
// neue ist dazugekommen - genau der Fall, den die Berichtsansicht sichtbar
// machen soll (+neu / −behoben, Härtungs-Index von 61 auf 72).
func seedDemoDeepScan(db *gorm.DB, serverID uint, base time.Time) {
	first := &domain.DeepScanReport{
		ServerID: serverID, CreatedAt: base.AddDate(0, 0, -7),
		Warnings: 2, Infos: 1, HardeningIndex: ptrInt(61), Tools: "needrestart,lynis",
	}
	firstFindings := []domain.DeepScanFinding{
		{ServerID: serverID, Category: domain.DeepScanMisconfig, Severity: domain.DeepScanWarning, Tool: "lynis",
			Title: "SSH: Passwort-Anmeldung ist erlaubt", Detail: "PasswordAuthentication auf no setzen."},
		{ServerID: serverID, Category: domain.DeepScanMisconfig, Severity: domain.DeepScanWarning, Tool: "lynis",
			Title: "Keine automatischen Sicherheitsupdates (unattended-upgrades nicht installiert)"},
		{ServerID: serverID, Category: domain.DeepScanMisconfig, Severity: domain.DeepScanInfo, Tool: "lynis",
			Title: "Legal-Banner in /etc/issue ergänzen"},
	}
	second := &domain.DeepScanReport{
		ServerID: serverID, CreatedAt: base,
		Warnings: 1, Infos: 1, HardeningIndex: ptrInt(72), Tools: "needrestart,lynis",
	}
	secondFindings := []domain.DeepScanFinding{
		// bleibt offen
		{ServerID: serverID, Category: domain.DeepScanMisconfig, Severity: domain.DeepScanInfo, Tool: "lynis",
			Title: "Legal-Banner in /etc/issue ergänzen"},
		// neu dazugekommen
		{ServerID: serverID, Category: domain.DeepScanRestart, Severity: domain.DeepScanWarning, Tool: "needrestart",
			Title:  "Dienst nutzt veraltete Bibliotheken: nginx.service",
			Detail: "Neu starten, damit aktualisierte Bibliotheken geladen werden."},
	}
	repo := repositories.NewServerRepository(db)
	if err := repo.SaveDeepScanReport(first, firstFindings); err != nil {
		return
	}
	// CreatedAt wird von GORM beim Anlegen gesetzt - für die Demo brauchen die
	// beiden Läufe aber einen sichtbaren zeitlichen Abstand.
	db.Model(&domain.DeepScanReport{}).Where("id = ?", first.ID).
		Update("created_at", base.AddDate(0, 0, -7))
	if err := repo.SaveDeepScanReport(second, secondFindings); err != nil {
		return
	}
	db.Model(&domain.Server{}).Where("id = ?", serverID).Updates(map[string]any{
		"deep_scan_at": base, "deep_scan_warnings": 1, "hardening_index": 72,
	})
}

func ptrTime(t time.Time) *time.Time { return &t }
func ptrInt(i int) *int              { return &i }
