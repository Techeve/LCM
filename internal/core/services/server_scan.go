package services

import (
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// diskUsageCmd liest Gesamt- und belegten Speicher des Root-Dateisystems in MB
// ("total used"). Wird sowohl vom vollen System-Scan als auch vom stündlichen
// Speicher-Snapshot des Health-Checks genutzt (parsebar via parseTwoInts).
const diskUsageCmd = `df -BM / | awk 'NR==2{gsub(/M/,""); print $2" "$3}'`

// diskVolumesCmd listet ALLE eingehängten „echten" Dateisysteme (Volumes, die
// dem System durchgereicht sind) mit Typ und Größe in MiB. Pseudo-Dateisysteme
// (tmpfs/overlay/squashfs …) werden ausgeschlossen - es geht um Speicher-Volumes,
// nicht um jeden Kernel-Mount. `-P` erzwingt eine Zeile pro Eintrag (kein
// Umbruch bei langen Gerätenamen). Ausgabe je Volume als TSV:
// mountpoint \t device \t fstype \t totalMB \t usedMB.
const diskVolumesCmd = `df -PTBM -x tmpfs -x devtmpfs -x squashfs -x overlay -x efivarfs -x ramfs -x fuse.gvfsd-fuse 2>/dev/null | ` +
	`awk 'NR>1 && $7 ~ /^\// {gsub(/M$/,"",$3); gsub(/M$/,"",$4); print $7"\t"$1"\t"$2"\t"$3"\t"$4}' | head -50`

// scanResult bündelt das Ergebnis eines System-Scans.
type scanResult struct {
	OSName         string
	OSVersion      string
	OSID           string // os-release ID, z.B. "ubuntu"/"debian"
	OSVersionID    string // os-release VERSION_ID, z.B. "22.04"/"12"
	ProxmoxType    string // domain.ProxmoxPVE/PBS/PMG, leer = kein Proxmox
	ProxmoxVersion string // Produktversion, z.B. "8.2.4"
	Virtualization string // systemd-detect-virt: "none"/"lxc"/"kvm"/…
	PackageManager string // erkannte Paketverwaltung: "apt"/"dnf"/"yum"/"zypper"
	HasSnap        bool   // snapd vorhanden (auch ohne installierte Snaps)
	HasACL         bool   // setfacl/getfacl vorhanden
	ACLUsable      bool   // ACLs auf dem Dateisystem tatsächlich nutzbar
	RebootRequired bool   // System fordert Neustart an (z. B. nach Kernel-Update)
	// ListeningPackages: Pakete, deren Prozesse auf von außen erreichbaren
	// Ports lauschen (kommagetrennt; automatische CVE-Hochgewichtung).
	ListeningPackages string
	KernelVersion     string
	// Kernels sind die installierten Kernel-Pakete (leer in Containern -
	// dort kommt der Kernel vom Host).
	Kernels      []domain.KernelPackage
	CPUModel     string
	CPUCores     int
	MemTotalMB   int64
	MemUsedMB    int64
	DiskTotalMB  int64
	DiskUsedMB   int64
	DiskVolumes  []domain.DiskVolume // alle eingehängten Volumes (inkl. „/")
	IPAddresses  string
	Packages     []domain.Package
	Snaps        []domain.SnapPackage
	Repositories []domain.AptRepository
	// Docker-Inventar (nur wenn HasDocker; siehe docker_scan.go).
	HasDocker        bool
	HasCompose       bool
	DockerContainers []domain.DockerContainer
	DockerImages     []domain.DockerImage
	// Users: anmeldefähige Linux-Konten (siehe users_scan.go). Leer, wenn der
	// Scan sie nicht erheben konnte - dann bleibt der alte Bestand stehen.
	Users []domain.ServerUser
	// UserLogins: Anmelde-Historie aus wtmp, im selben Durchgang erhoben.
	UserLogins []domain.ServerUserLogin
	// Sicherheits-Tools (Intrusion Prevention): vorhanden? (Aktiv-Status wird
	// beim Voll-Refresh in readServerLiveStatus gelesen.)
	Fail2banInstalled bool
	CrowdSecInstalled bool
	// SSH2FAEnabled: das LCM-2FA-Drop-in ist vorhanden (siehe ssh_2fa.go).
	SSH2FAEnabled bool
	// LCMSourceIP: Quell-IP, mit der LCM diesen Server erreicht ($SSH_CONNECTION).
	LCMSourceIP string
	// FirewallTool: erkanntes Firewall-Werkzeug (ufw/firewalld/nftables, ""=keins).
	FirewallTool string
	// Zeit-Zustand: Zeitzone, Zeitdienst und Uhrenversatz (siehe timesync.go).
	Timezone           string
	NTPService         string
	NTPSynchronized    bool
	NTPServers         string
	ClockOffsetSeconds int
	// ListeningPorts: lauschende Sockets als JSON (Firewall-Vorschläge).
	ListeningPorts string
	// RouterOS-spezifisch (nur beim RouterOS-Scan gesetzt, siehe routeros.go).
	RouterBoardModel        string
	RouterOSChannel         string
	RouterOSLatestVersion   string
	RouterOSUpdateAvailable bool
	Output                  string // gesammelter Konsolen-Output für das Job-Protokoll
}

// scanServerMode führt den System-Scan über eine bestehende SSH-Verbindung
// aus: Distribution, Kernel, Hardware (CPU/RAM/Disk), Netzwerk, Pakete
// (inkl. Update-Kandidaten), apt-Repositories (mit Sicherheitsprüfung)
// und Docker-Inventar. loginUser ist der verbundene Benutzer - Docker-
// Kommandos brauchen root; im eingeschränkten Modus (restricted) laufen die
// privilegierten Scan-Kommandos über die sudo-Whitelist statt über einen
// root-Shell-Wrap. Einzelne fehlschlagende Kommandos brechen den Scan nicht
// ab - was lesbar ist, wird übernommen.
func scanServerMode(conn sshx.Conn, loginUser string, restricted bool) *scanResult {
	res := &scanResult{}
	var log strings.Builder

	run := func(label, cmd string) string {
		out, code, err := conn.Run(cmd)
		log.WriteString("$ " + cmd + "\n")
		if err != nil {
			log.WriteString("(transportfehler: " + err.Error() + ")\n")
			return ""
		}
		if code != 0 {
			log.WriteString(out + "(exit " + strconv.Itoa(code) + ")\n")
			return ""
		}
		if label != "packages" { // Paketliste ist zu lang fürs Protokoll
			log.WriteString(out)
		} else {
			log.WriteString("(" + strconv.Itoa(strings.Count(out, "\n")) + " pakete)\n")
		}
		return out
	}

	res.OSName, res.OSVersion, res.OSID, res.OSVersionID = parseOSRelease(run("os", "cat /etc/os-release"))
	// Proxmox meldet sich in os-release schlicht als Debian - das Produkt
	// verrät sich über seine Pakete (pve-manager & Co., siehe detectProxmox).
	res.ProxmoxType, res.ProxmoxVersion = detectProxmox(run)
	// systemd-detect-virt gibt den Virtualisierungstyp aus ("none" = Bare
	// Metal) und beendet sich bei Bare Metal mit Exit 1 - mit "; true"
	// bleibt die Ausgabe erhalten; fehlt das Tool, bleibt der Wert leer.
	res.Virtualization = firstLine(run("virt", "systemd-detect-virt 2>/dev/null; true"))
	// Der laufende Kernel. `uname -r` ist die einzige Quelle, die nicht luegen
	// kann: Sie meldet den tatsaechlich gebooteten Kernel - nicht das, was die
	// Paketverwaltung vorhaelt. Nach einem Kernel-Update weichen beide bis zum
	// Neustart voneinander ab.
	res.KernelVersion = strings.TrimSpace(run("kernel", "uname -r"))
	res.CPUModel = strings.TrimSpace(run("cpu", "sed -n 's/^model name[^:]*: //p' /proc/cpuinfo | head -1"))
	res.CPUCores, _ = strconv.Atoi(strings.TrimSpace(run("cores", "nproc")))
	res.MemTotalMB, res.MemUsedMB = parseTwoInts(run("mem", "free -m | awk '/^Mem:/{print $2\" \"$3}'"))
	res.DiskTotalMB, res.DiskUsedMB = parseTwoInts(run("disk", diskUsageCmd))
	res.DiskVolumes = parseDiskVolumes(run("volumes", diskVolumesCmd))
	res.IPAddresses = strings.Join(strings.Fields(run("ips", "hostname -I")), ", ")

	// Paketverwaltung erkennen und bestandsabhängig scannen (apt/dnf/zypper).
	res.PackageManager = detectPackageManager(run)
	res.Packages, res.Repositories = scanPackagesAndRepos(res.PackageManager, run)
	// Installierte Kernel-Pakete (Rueckfallebene + Erkennung „neuer Kernel
	// installiert, laeuft aber noch nicht"). Braucht die erkannte
	// Paketverwaltung und den Proxmox-Befund, steht deshalb hier.
	res.Kernels = scanKernels(res.PackageManager, res.Virtualization, res.ProxmoxType != "", run)
	res.RebootRequired = scanRebootRequired(res.PackageManager, run)
	res.ListeningPackages = scanListeningPackages(res.PackageManager, loginUser, restricted, run)

	// Zweite Paketverwaltung: Snaps (v.a. Ubuntu). HasSnap hält fest, ob
	// snapd überhaupt vorhanden ist - auch bei NULL installierten Snaps
	// (deren Hinweis "No snaps are installed yet" landet auf stderr, die
	// Liste ist dann schlicht leer). Ohne PTY gibt snap unformatiert aus;
	// auf Farb-Flags wird verzichtet (nicht jede snapd-Version kennt sie).
	res.HasSnap = firstLine(run("has-snap", "command -v snap 2>/dev/null || true")) != ""

	// ACL-Fähigkeit: Das Werkzeug allein genügt nicht - entscheidend ist, ob
	// das Dateisystem ACLs trägt. Deshalb eine Probe, die einen Eintrag setzt
	// und sofort wieder entfernt; sie fängt ZFS ohne acltype=posixacl,
	// NFS-Mounts und Container-Overlays ab, bei denen setfacl vorhanden ist,
	// aber nichts bewirkt.
	res.HasACL = firstLine(run("has-acl", "command -v setfacl 2>/dev/null || true")) != ""
	if res.HasACL {
		res.ACLUsable = strings.Contains(run("acl-probe", aclProbeScript), "acl-ok")
	}
	res.Snaps = parseSnapList(run("snaps", "snap list 2>/dev/null || true"))
	applySnapRefresh(res.Snaps, run("snap-refresh", "snap refresh --list 2>/dev/null || true"))

	// Docker-Inventar: Container + Images (siehe docker_scan.go).
	scanDocker(res, loginUser, restricted, run)

	// Anmeldefähige Linux-Konten (braucht root für /etc/shadow; im
	// eingeschränkten Modus über das Helper-Unterkommando users-scan -
	// ein veralteter Helper kennt es noch nicht, dann bleibt die Liste leer).
	usersCmd := usersScanScript()
	if restricted {
		usersCmd = helperCmd("users-scan")
	}
	usersOut := run("users", wrapSudo(loginUser, restricted, usersCmd))
	res.Users = parseServerUsers(usersOut)
	res.UserLogins = parseLastLogins(usersOut)
	applyLoginHistory(res.Users, res.UserLogins)

	// Sicherheits-Tools (fail2ban/CrowdSec) erkennen - Vorhandensein reicht hier,
	// der Aktiv-Status kommt beim Voll-Refresh (readServerLiveStatus).
	res.Fail2banInstalled = firstLine(run("has-f2b", "command -v fail2ban-client 2>/dev/null || true")) != ""
	res.CrowdSecInstalled = firstLine(run("has-cs", "command -v cscli 2>/dev/null || command -v crowdsec 2>/dev/null || true")) != ""
	// SSH-2FA: das Drop-in ist weltlesbar - kein sudo nötig.
	res.SSH2FAEnabled = firstLine(run("has-2fa", "test -f "+ssh2faDropinPath+" && echo yes || echo no")) == "yes"
	// LCM-Quell-IP OHNE sudo lesen ($SSH_CONNECTION ist nur in der Session-Umgebung,
	// nicht nach einem sudo-Wrap) - für die Auto-Allowlist von fail2ban/CrowdSec.
	res.LCMSourceIP = firstLine(run("lcm-ip", "echo \"$SSH_CONNECTION\" | awk '{print $1}'"))

	// Firewall-Werkzeug erkennen (ufw/firewalld/nftables). Im eingeschränkten
	// Modus läuft die Prüfung unprivilegiert - command -v und systemctl
	// is-enabled reichen dort; nur die nft-Tabellen-Prüfung braucht root.
	res.FirewallTool = parseFirewallDetect(run("firewall-tool", wrapSudo(loginUser, restricted, firewallDetectCmd)))
	// Lauschende Sockets als Firewall-Vorschläge inventarisieren.
	res.ListeningPorts = scanListeningPorts(loginUser, restricted, run)
	// Zeitzone, Zeitdienst und Uhrenversatz - rein lesend, daher auch im
	// eingeschränkten Modus vollständig.
	scanTimeState(res, run)

	res.Output = log.String()
	return res
}

// detectPackageManager ermittelt die auf dem Zielsystem vorhandene
// Paketverwaltung (erstes gefundenes Binary gewinnt).
//
// pacman und apk werden mit erkannt, obwohl LCM sie nicht bedienen kann: nur
// so lässt sich dem Anwender sagen, WORAN es liegt ("Paketverwaltung pacman
// wird nicht unterstützt") statt den Server stillschweigend mit leerem
// Bestand aufzunehmen. Die Unterstützungsfrage klärt PackageManagerSupported.
func detectPackageManager(run func(label, cmd string) string) string {
	switch firstLine(run("pkgmgr", "for m in apt-get dnf zypper yum pacman apk; do command -v $m >/dev/null 2>&1 && { echo $m; break; }; done")) {
	case "apt-get":
		return pkgApt
	case "dnf":
		return pkgDnf
	case "yum":
		return pkgYum
	case "zypper":
		return pkgZypper
	case "pacman":
		return pkgPacman
	case "apk":
		return pkgApk
	default:
		return ""
	}
}

// scanPackagesAndRepos liest installierte Pakete (mit Update-Kandidaten) und
// Paketquellen passend zur erkannten Paketverwaltung.
func scanPackagesAndRepos(mgr string, run func(label, cmd string) string) ([]domain.Package, []domain.AptRepository) {
	var pkgs []domain.Package
	switch pkgFamily(mgr) {
	case pkgDnf:
		pkgs = parseRPMList(run("packages", "rpm -qa --qf '%{NAME} %{VERSION}-%{RELEASE}\\n'"))
		applyDnfUpgrades(pkgs, run("upgradable", dnfBin(mgr)+" -q list --upgrades 2>/dev/null"))
	case pkgZypper:
		pkgs = parseRPMList(run("packages", "rpm -qa --qf '%{NAME} %{VERSION}-%{RELEASE}\\n'"))
		applyZypperUpdates(pkgs, run("upgradable", "zypper --non-interactive list-updates 2>/dev/null"))
	case pkgPacman:
		// pacman -Q listet "name version" (wie dpkg); -Qu die aktualisierbaren.
		pkgs = parseDpkgList(run("packages", "pacman -Q 2>/dev/null"))
		applyPacmanUpgrades(pkgs, run("upgradable", "pacman -Qu 2>/dev/null"))
	case pkgApk:
		pkgs = parseApkList(run("packages", "apk list -I 2>/dev/null"))
		applyApkUpgrades(pkgs, run("upgradable", "apk version -l '<' 2>/dev/null"))
	default:
		pkgs = parseDpkgList(run("packages", "dpkg-query -W -f='${Package} ${Version}\\n'"))
		applyUpgradable(pkgs, run("upgradable", "apt list --upgradable 2>/dev/null"))
	}
	return pkgs, scanReposFor(mgr, run)
}

// scanReposFor liest die konfigurierten Paketquellen passend zur
// Paketverwaltung ein - geteilt zwischen dem vollen System-Scan und dem
// Rescan nach einer Repo-Aktion (add-repo/https-Umstellung).
func scanReposFor(mgr string, run func(label, cmd string) string) []domain.AptRepository {
	switch pkgFamily(mgr) {
	case pkgDnf:
		return parseRepoURIs(run("repos", repoIniScan("/etc/yum.repos.d")))
	case pkgZypper:
		return parseRepoURIs(run("repos", repoIniScan("/etc/zypp/repos.d")))
	case pkgPacman:
		// Aktive Spiegel/Server aus pacman.conf und den eingebundenen
		// Mirrorlisten (nur unkommentierte Server=-Zeilen).
		return parseRepoURIs(run("repos", `grep -rhsE '^[[:space:]]*Server[[:space:]]*=' /etc/pacman.conf /etc/pacman.d/ 2>/dev/null | head -50 || true`))
	case pkgApk:
		return parseApkRepos(run("repos", "cat /etc/apk/repositories 2>/dev/null || true"))
	default:
		return parseAptRepos(run("repos", aptRepoScanCmd))
	}
}

// scanRebootRequired prüft, ob das System selbst einen Neustart anfordert -
// z. B. nach Kernel-/libc-Updates (der Hinweis, den Ubuntu beim Login zeigt).
// Debian/Ubuntu setzen dafür /var/run/reboot-required, die RHEL-Familie
// meldet es über needs-restarting -r (Exit 1), SUSE über zypper
// needs-rebooting (Exit 102). Fehlt das jeweilige Werkzeug, gilt „kein
// Neustart nötig" (best effort, rein lesend, ohne Root-Rechte).
func scanRebootRequired(mgr string, run func(label, cmd string) string) bool {
	var cmd string
	switch pkgFamily(mgr) {
	case pkgDnf:
		cmd = "command -v needs-restarting >/dev/null 2>&1 && { needs-restarting -r >/dev/null 2>&1; [ $? -eq 1 ] && echo yes || echo no; } || echo no"
	case pkgZypper:
		cmd = "zypper needs-rebooting >/dev/null 2>&1; [ $? -eq 102 ] && echo yes || echo no"
	default:
		cmd = "test -f /var/run/reboot-required && echo yes || echo no"
	}
	return firstLine(run("reboot-required", cmd)) == "yes"
}

// scanListeningPackages ermittelt, welche PAKETE auf von außen erreichbaren
// Ports lauschen: ss listet lauschende TCP/UDP-Sockets samt PID (reine
// localhost-Listener werden ausgefiltert), /proc/<pid>/exe führt zum Binary
// und die Paketverwaltung (dpkg -S bzw. rpm -qf) zum Paketnamen. Diese Pakete
// werden bei der CVE-Bewertung automatisch eine Stufe höher gewichtet.
// Braucht Root (Prozess-Infos fremder Benutzer) - läuft daher über sudo; im
// eingeschränkten Modus (ss nicht auf der Whitelist) entfällt die Erkennung.
// Best effort: Fehler liefern schlicht eine leere Liste.
func scanListeningPackages(mgr, loginUser string, restricted bool, run func(label, cmd string) string) string {
	if restricted {
		return ""
	}
	resolve := `dpkg -S "$exe" 2>/dev/null | head -1 | cut -d: -f1`
	switch pkgFamily(mgr) {
	case pkgDnf, pkgZypper:
		resolve = `rpm -qf --qf "%{NAME}\n" "$exe" 2>/dev/null | grep -v " "`
	case pkgPacman:
		resolve = `pacman -Qoq "$exe" 2>/dev/null`
	case pkgApk:
		// apk info -W: "<exe> is owned by <name>-<ver>-r<rel>" → Version-Suffix
		// (-<ver>-r<rel>) abschneiden, damit der reine Paketname übrig bleibt.
		resolve = `apk info -W "$exe" 2>/dev/null | sed -n 's/.* is owned by //p' | sed 's/-[0-9][^-]*-r[0-9]*$//'`
	}
	// Die Loopback-Auslese passiert in GO, nicht per grep auf dem Ziel: das
	// zeilenweise grep -vE "127\.0\.0\.1|\[::1\]" übersah je nach
	// ss-Ausgabeformat (v4-gemappte v6-Adressen, Scopes) Loopback-Listener -
	// derselbe Dienst stand damit auf einem Server in der Liste und fehlte
	// auf dem nächsten (R2-084). Jetzt gilt überall derselbe Umfang: Pakete,
	// die auf NICHT-Loopback-Adressen lauschen.
	ssOut := run("listening-ss", wrapSudo(loginUser, restricted, "ss -tulnpH 2>/dev/null || true"))
	pids := nonLoopbackListenerPIDs(ssOut)
	if len(pids) == 0 {
		return ""
	}
	script := `for pid in ` + strings.Join(pids, " ") + `; do ` +
		`exe=$(readlink -f /proc/$pid/exe 2>/dev/null) || continue; ` + resolve + `; done | sort -u | head -30`
	out := run("listening", wrapSudo(loginUser, restricted, script))
	var pkgs []string
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			pkgs = append(pkgs, p)
		}
	}
	return strings.Join(pkgs, ", ")
}

// scanPackages liest den Paketbestand samt Update-Kandidaten über eine
// bestehende Verbindung neu ein (ohne den vollen System-Scan). Fehler
// einzelner Kommandos werden ignoriert - was lesbar ist, wird übernommen.
func scanPackages(conn sshx.Conn, mgr string) []domain.Package {
	run := func(_, cmd string) string { out, _, _ := conn.Run(cmd); return out }
	pkgs, _ := scanPackagesAndRepos(mgr, run)
	return pkgs
}

// rescanPackagesInto aktualisiert den Paketbestand eines Servers in der DB
// nach einer Paket-Aktion (best effort - der Outdated-Zähler wird live aus
// der Paket-Tabelle berechnet, daher genügt das Ersetzen des Bestands).
func rescanPackagesInto(servers *repositories.ServerRepository, conn sshx.Conn, server *domain.Server) {
	mgr := server.PackageManager
	pkgs := scanPackages(conn, mgr)
	if len(pkgs) == 0 {
		return
	}
	_ = servers.ReplacePackages(server.ID, pkgs)
	run := func(label, cmd string) string {
		out, code, err := conn.Run(cmd)
		if err != nil || code != 0 {
			return ""
		}
		return out
	}
	// Direkt nach Paket-Updates neu bewerten, ob das System jetzt einen
	// Neustart anfordert (z. B. neuer Kernel) - nicht erst beim nächsten
	// vollen System-Scan.
	//
	// Dasselbe gilt fürs Kernel-Inventar, und dort fiel es lange nicht auf:
	// Jedes Update kann einen Kernel hinzufügen oder wegräumen. Wurde nur der
	// Paketbestand nachgezogen, zeigte die Kernel-Liste weiter den Stand des
	// letzten VOLLEN Scans - auf regelmäßig aktualisierten Servern also
	// dauerhaft zu wenige Kernel.
	fields := map[string]any{"reboot_required": scanRebootRequired(mgr, run)}
	if kernels := scanKernels(mgr, server.Virtualization, server.ProxmoxType != "", run); len(kernels) > 0 {
		fields["installed_kernels"] = domain.MarshalKernelPackages(kernels)
	}
	_ = servers.UpdateFields(server.ID, fields)
}

// parseOSRelease extrahiert NAME, VERSION, ID und VERSION_ID aus
// /etc/os-release. ID/VERSION_ID (z.B. "ubuntu"/"22.04") speisen die
// Support-/EOL-Bewertung.
func parseOSRelease(out string) (name, version, id, versionID string) {
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "NAME":
			name = value
		case "VERSION":
			version = value
		case "ID":
			id = value
		case "VERSION_ID":
			versionID = value
		}
	}
	return name, version, id, versionID
}

// detectProxmox erkennt Proxmox-Produkte über ihre charakteristischen
// Pakete: pve-manager (Virtual Environment), proxmox-backup-server
// (Backup Server), pmg-api (Mail Gateway), proxmox-datacenter-manager
// (Datacenter Manager). dpkg-query endet mit Exit != 0,
// sobald EIN abgefragtes Paket fehlt (der Normalfall) - "; true" erhält
// die Ausgabe der gefundenen. Auf Nicht-Debian-Systemen fehlt dpkg-query
// komplett → leeres Ergebnis, kein Proxmox.
func detectProxmox(run func(label, cmd string) string) (string, string) {
	out := run("proxmox",
		`dpkg-query -W -f='${Package} ${Version}\n' pve-manager proxmox-backup-server pmg-api proxmox-datacenter-manager 2>/dev/null; true`)
	found := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		pkg, version, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		found[pkg] = proxmoxDisplayVersion(version)
	}
	// Priorität bei Mehrfach-Treffern (unüblich): das Hauptprodukt gewinnt.
	switch {
	case found["pve-manager"] != "":
		return domain.ProxmoxPVE, found["pve-manager"]
	case found["proxmox-backup-server"] != "":
		return domain.ProxmoxPBS, found["proxmox-backup-server"]
	case found["pmg-api"] != "":
		return domain.ProxmoxPMG, found["pmg-api"]
	case found["proxmox-datacenter-manager"] != "":
		return domain.ProxmoxPDM, found["proxmox-datacenter-manager"]
	}
	return "", ""
}

// proxmoxDisplayVersion reduziert eine Debian-Paketversion auf die
// Produktversion: Epoch und Debian-Revision fallen weg ("8.2.4-1" → "8.2.4").
func proxmoxDisplayVersion(v string) string {
	v = stripEpoch(strings.TrimSpace(v))
	if idx := strings.Index(v, "-"); idx > 0 {
		v = v[:idx]
	}
	return v
}

// firstLine liefert die erste nicht-leere, getrimmte Zeile (z.B. für die
// einzeilige systemd-detect-virt-Ausgabe).
func firstLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// parseTwoInts parst "total used" (z.B. aus free/df).
func parseTwoInts(out string) (int64, int64) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 2 {
		return 0, 0
	}
	total, _ := strconv.ParseInt(fields[0], 10, 64)
	used, _ := strconv.ParseInt(fields[1], 10, 64)
	return total, used
}

// parseDiskVolumes parst die TSV-Ausgabe von diskVolumesCmd:
// mountpoint \t device \t fstype \t totalMB \t usedMB (je Zeile ein Volume).
// Zeilen ohne verwertbare Kapazität werden übersprungen.
func parseDiskVolumes(out string) []domain.DiskVolume {
	var vols []domain.DiskVolume
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) != 5 {
			continue
		}
		total, _ := strconv.ParseInt(strings.TrimSpace(f[3]), 10, 64)
		used, _ := strconv.ParseInt(strings.TrimSpace(f[4]), 10, 64)
		if total <= 0 {
			continue
		}
		vols = append(vols, domain.DiskVolume{
			Mountpoint: strings.TrimSpace(f[0]),
			Device:     strings.TrimSpace(f[1]),
			Fstype:     strings.TrimSpace(f[2]),
			TotalMB:    total,
			UsedMB:     used,
		})
	}
	return vols
}

// parseDpkgList parst "paket version"-Zeilen von dpkg-query.
func parseDpkgList(out string) []domain.Package {
	var pkgs []domain.Package
	for _, line := range strings.Split(out, "\n") {
		name, version, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || name == "" {
			continue
		}
		pkgs = append(pkgs, domain.Package{Name: name, Version: version})
	}
	return dedupePackages(pkgs)
}

// dedupePackages behält je Paketnamen den ersten Eintrag. Sicherheitsnetz für
// alle Paketverwaltungen: der Paketbestand hat einen Unique-Index über
// (server_id, name) - ein Duplikat darf niemals die komplette Bestandsaufnahme
// eines Servers zum Scheitern bringen und ihn als leere Hülle zurücklassen.
func dedupePackages(pkgs []domain.Package) []domain.Package {
	seen := make(map[string]bool, len(pkgs))
	out := pkgs[:0]
	for _, p := range pkgs {
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	return out
}

// parseRPMList parst "name version-release"-Zeilen von `rpm -qa` (gleiches
// Format wie dpkg: Name, Leerzeichen, Version) - für dnf/yum und zypper.
//
// Sonderfall gpg-pubkey: RPM führt JEDEN importierten Repository-Signatur-
// schlüssel als Pseudo-Paket unter genau diesem Namen. Ein System mit mehreren
// Fremdquellen (EPEL, Docker, …) liefert den Namen also mehrfach - openSUSE
// bringt im Auslieferungszustand sogar sechs mit. Das sind keine Pakete und
// gehören nicht in den Bestand; ungefiltert kollidieren sie am Unique-Index
// (server_id, name) und ließen die gesamte Bestandsaufnahme scheitern.
func parseRPMList(out string) []domain.Package {
	return filterOutRPMPseudoPackages(parseDpkgList(out))
}

// rpmPseudoPackages sind Einträge, die `rpm -qa` listet, die aber keine
// installierten Pakete sind.
var rpmPseudoPackages = map[string]bool{"gpg-pubkey": true}

func filterOutRPMPseudoPackages(pkgs []domain.Package) []domain.Package {
	out := pkgs[:0]
	for _, p := range pkgs {
		if !rpmPseudoPackages[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

// rpmArchSuffixes sind die Architektur-Endungen, die dnf an "name.arch"
// anhängt und die zum Abgleich mit rpm -qa (nur Name) entfernt werden.
var rpmArchSuffixes = []string{".x86_64", ".noarch", ".i686", ".aarch64", ".armv7hl", ".ppc64le", ".s390x", ".src"}

// stripRPMArch entfernt eine angehängte Architektur ("nginx.x86_64" → "nginx").
func stripRPMArch(nameArch string) string {
	for _, suf := range rpmArchSuffixes {
		if strings.HasSuffix(nameArch, suf) {
			return strings.TrimSuffix(nameArch, suf)
		}
	}
	return nameArch
}

// stripEpoch entfernt ein führendes "N:" (RPM-Epoch), damit die Kandidaten-
// version mit der (epochlosen) installierten Version vergleichbar bleibt.
func stripEpoch(v string) string {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		return v[i+1:]
	}
	return v
}

// applyDnfUpgrades ergänzt Update-Kandidaten aus `dnf list --upgrades`.
// Zeilenformat: "name.arch  [epoch:]version-release  repo".
func applyDnfUpgrades(pkgs []domain.Package, out string) {
	byName := make(map[string]*domain.Package, len(pkgs))
	for i := range pkgs {
		byName[pkgs[i].Name] = &pkgs[i]
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(fields[0], ".") {
			continue
		}
		name := stripRPMArch(fields[0])
		if p, ok := byName[name]; ok {
			p.CandidateVersion = stripEpoch(fields[1])
		}
	}
}

// applyZypperUpdates ergänzt Update-Kandidaten aus `zypper list-updates`.
// Pipe-Tabelle: "S | Repository | Name | Current Version | Available Version | Arch".
func applyZypperUpdates(pkgs []domain.Package, out string) {
	byName := make(map[string]*domain.Package, len(pkgs))
	for i := range pkgs {
		byName[pkgs[i].Name] = &pkgs[i]
	}
	for _, line := range strings.Split(out, "\n") {
		cols := strings.Split(line, "|")
		if len(cols) < 5 {
			continue
		}
		name := strings.TrimSpace(cols[2])
		avail := strings.TrimSpace(cols[4])
		if name == "" || name == "Name" || avail == "" {
			continue
		}
		if p, ok := byName[name]; ok {
			p.CandidateVersion = avail
		}
	}
}

// repoIniScan liest die Paketquellen der rpm-Welt aus ihren ini-Dateien.
//
// Zwei Dinge, die ein einfaches grep nicht leistet und die auf echten
// Systemen den Unterschied machen:
//
//   - Red Hat schreibt "baseurl = https://…" MIT Leerzeichen um das
//     Gleichheitszeichen. Ein Muster auf "^baseurl=" findet auf einem
//     RHEL-Server deshalb keine einzige Quelle - auf Rocky und AlmaLinux,
//     die ohne Leerzeichen schreiben, fiel das nie auf.
//   - Die Dateien führen neben den aktiven auch abgeschaltete Sektionen
//     (Debug- und Quellcode-Spiegel, enabled = 0). Sie mitzuzählen erweckte
//     den Eindruck, der Server zöge aus dreimal so vielen Quellen wie in
//     Wirklichkeit.
//
// Deshalb ein awk, das je Sektion die URL merkt und erst am Sektionsende
// entscheidet - enabled kann vor oder nach der URL stehen. Ausgegeben wird
// weiterhin "baseurl=<url>", damit parseRepoURIs unverändert bleibt.
func repoIniScan(dir string) string {
	return `awk '
	function fertig() { if (url != "" && an == 1) printf "baseurl=%s\n", url; url=""; an=1 }
	/^[[:space:]]*\[/ { fertig(); next }
	/^[[:space:]]*(baseurl|mirrorlist|metalink)[[:space:]]*=/ {
		if (url == "") { sub(/^[^=]*=[[:space:]]*/, ""); url=$0 }
		next
	}
	/^[[:space:]]*enabled[[:space:]]*=/ { sub(/^[^=]*=[[:space:]]*/, ""); an=($0+0) }
	END { fertig() }
	' ` + dir + `/*.repo 2>/dev/null || true`
}

// parseRepoURIs baut aus "baseurl=…/mirrorlist=…/metalink=…"-Zeilen der
// RPM-/zypper-Repo-Dateien AptRepository-Einträge (URI + Unsicher-Flag).
func parseRepoURIs(out string) []domain.AptRepository {
	seen := map[string]bool{}
	var repos []domain.AptRepository
	for _, line := range strings.Split(out, "\n") {
		_, uri, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		uri = strings.TrimSpace(uri)
		if uri == "" || seen[uri] {
			continue
		}
		seen[uri] = true
		repos = append(repos, domain.AptRepository{
			Line:     uri,
			Insecure: strings.Contains(uri, "http://"),
		})
	}
	return repos
}

// applyPacmanUpgrades ergänzt Update-Kandidaten aus `pacman -Qu`.
// Zeilenformat: "name altversion -> neueversion".
func applyPacmanUpgrades(pkgs []domain.Package, out string) {
	byName := make(map[string]*domain.Package, len(pkgs))
	for i := range pkgs {
		byName[pkgs[i].Name] = &pkgs[i]
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "->" {
			continue
		}
		if p, ok := byName[fields[0]]; ok {
			p.CandidateVersion = fields[3]
		}
	}
}

// parseApkList parst `apk list -I`. Jede Zeile beginnt mit dem Token
// "name-pkgver-rN" (Feld 0); der Rest (Arch, Origin, Lizenz, Flags) ist für
// den Bestand unerheblich.
func parseApkList(out string) []domain.Package {
	var pkgs []domain.Package
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name, version := splitApkNameVersion(fields[0])
		if name == "" {
			continue
		}
		pkgs = append(pkgs, domain.Package{Name: name, Version: version})
	}
	return dedupePackages(pkgs)
}

// applyApkUpgrades ergänzt Update-Kandidaten aus `apk version -l '<'`.
// Zeilenformat: "name-pkgver-rN  <  neueversion".
func applyApkUpgrades(pkgs []domain.Package, out string) {
	byName := make(map[string]*domain.Package, len(pkgs))
	for i := range pkgs {
		byName[pkgs[i].Name] = &pkgs[i]
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != "<" {
			continue
		}
		name, _ := splitApkNameVersion(fields[0])
		if p, ok := byName[name]; ok {
			p.CandidateVersion = fields[len(fields)-1]
		}
	}
}

// parseApkRepos liest /etc/apk/repositories: eine Repository-URL je Zeile
// (Kommentare mit '#' und Leerzeilen entfallen).
func parseApkRepos(out string) []domain.AptRepository {
	seen := map[string]bool{}
	var repos []domain.AptRepository
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || seen[line] {
			continue
		}
		seen[line] = true
		repos = append(repos, domain.AptRepository{
			Line:     line,
			Insecure: strings.Contains(line, "http://"),
		})
	}
	return repos
}

// splitApkNameVersion zerlegt ein apk-Token "name-pkgver-rN" in Paketname und
// Version. apk-pkgver darf kein '-' enthalten, die Revision ist stets "-rN" -
// daher sind die letzten beiden Bindestrich-Segmente Version+Revision und
// alles davor der (auch bindestrichhaltige) Paketname. Passt das Muster nicht,
// gilt das ganze Token als Name (Version leer) - lieber ohne Version als mit
// falschem Split.
func splitApkNameVersion(token string) (name, version string) {
	i2 := strings.LastIndexByte(token, '-')
	if i2 <= 0 {
		return token, ""
	}
	rel := token[i2+1:]
	if len(rel) < 2 || rel[0] != 'r' || !isAllDigits(rel[1:]) {
		return token, ""
	}
	i1 := strings.LastIndexByte(token[:i2], '-')
	if i1 <= 0 {
		return token, ""
	}
	pkgver := token[i1+1 : i2]
	if pkgver == "" || pkgver[0] < '0' || pkgver[0] > '9' {
		return token, ""
	}
	return token[:i1], token[i1+1:]
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// applyUpgradable ergänzt Update-Kandidaten aus `apt list --upgradable`.
// Zeilenformat: "paket/quelle kandidat arch [upgradable from: alt]".
// Quellen mit "security" markieren ein Security-Update.
func applyUpgradable(pkgs []domain.Package, out string) {
	byName := make(map[string]*domain.Package, len(pkgs))
	for i := range pkgs {
		byName[pkgs[i].Name] = &pkgs[i]
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Listing") || strings.HasPrefix(line, "WARNING") {
			continue
		}
		nameSource, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		name, source, _ := strings.Cut(nameSource, "/")
		fields := strings.Fields(rest)
		if len(fields) < 1 {
			continue
		}
		if p, exists := byName[name]; exists {
			p.CandidateVersion = fields[0]
			p.Security = strings.Contains(source, "security")
		}
	}
}

// parseSnapList parst die Ausgabe von `snap list`. Spalten (feste
// Reihenfolge): Name Version Rev Tracking Publisher Notes. Die Kopfzeile
// und Meldungen ("No snaps are installed …") werden übersprungen.
func parseSnapList(out string) []domain.SnapPackage {
	var snaps []domain.SnapPackage
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if fields[0] == "Name" && fields[1] == "Version" {
			continue // Kopfzeile
		}
		snaps = append(snaps, domain.SnapPackage{
			Name:      fields[0],
			Version:   fields[1],
			Revision:  fields[2],
			Channel:   fields[3],
			Publisher: strings.TrimRight(fields[4], "*✓"), // Verifiziert-Häkchen entfernen
		})
	}
	return snaps
}

// applySnapRefresh ergänzt Update-Kandidaten aus `snap refresh --list`.
// Gleiches Spaltenlayout wie `snap list`; die Version ist die verfügbare
// (neue) Version. "All snaps up to date." wird ignoriert.
func applySnapRefresh(snaps []domain.SnapPackage, out string) {
	byName := make(map[string]*domain.SnapPackage, len(snaps))
	for i := range snaps {
		byName[snaps[i].Name] = &snaps[i]
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "Name" {
			continue
		}
		if s, ok := byName[fields[0]]; ok {
			s.CandidateVersion = fields[1]
		}
	}
}

// aptRepoScanCmdIn baut den Lesebefehl für die apt-Paketquellen unterhalb von
// etc (produktiv /etc/apt) - klassische sources.list-Zeilen UND deb822-
// .sources-Dateien (ab Ubuntu 24.04 Standard). Der Marker @@@DEB822@@@ trennt
// die beiden Formate für parseAptRepos.
//
// Gelesen wird genau das, was apt selbst liest: sources.list sowie *.list und
// *.sources in sources.list.d. Ein rekursives grep über das ganze Verzeichnis
// wäre falsch - es zählte stillgelegte Quellen (…​.list.disabled), Sicherungs-
// kopien und Fremddateien als aktive Paketquelle, und die Übersicht zeigte
// Kanäle an, aus denen gar nichts mehr kommt.
func aptRepoScanCmdIn(etc string) string {
	return "grep -hsE '^deb ' " + etc + "/sources.list " + etc + "/sources.list.d/*.list 2>/dev/null; " +
		"echo '@@@DEB822@@@'; awk 'FNR==1{print \"\"}{print}' " + etc + "/sources.list.d/*.sources 2>/dev/null || true"
}

var aptRepoScanCmd = aptRepoScanCmdIn("/etc/apt")

// parseAptRepos parst apt-Paketquellen und bewertet die Sicherheit (Quellen
// über unverschlüsseltes http gelten als unsicher). Es versteht beide Formate:
// das klassische Einzeilen-Format (sources.list) und das deb822-Format
// (.sources, ab Ubuntu 24.04 Standard); der Marker @@@DEB822@@@ trennt sie.
func parseAptRepos(out string) []domain.AptRepository {
	classic, deb822, _ := strings.Cut(out, "@@@DEB822@@@")
	seen := map[string]bool{}
	var repos []domain.AptRepository
	add := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			return
		}
		seen[line] = true
		repos = append(repos, domain.AptRepository{
			Line: line,
			// "http://" (ohne s) => unverschlüsselte Paketquelle.
			Insecure: strings.Contains(line, "http://"),
		})
	}
	for _, line := range strings.Split(classic, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "deb ") || strings.HasPrefix(line, "deb-src ") {
			add(line)
		}
	}
	for _, line := range parseDeb822Repos(deb822) {
		add(line)
	}
	return repos
}

// parseDeb822Repos übersetzt deb822-Stanzas (.sources) in klassische
// "deb <uri> <suite> <components>"-Zeilen - je Kombination aus Typ, URI und
// Suite eine Zeile (wie apt sie intern expandiert). Deaktivierte Stanzas
// (Enabled: no) werden übersprungen. Gefaltete Werte (z.B. eingebettete
// Signed-By-Schlüssel) sind unkritisch: Fortsetzungszeilen ohne ":" entfallen.
func parseDeb822Repos(s string) []string {
	var out []string
	for _, stanza := range strings.Split(s, "\n\n") {
		fields := map[string]string{}
		for _, line := range strings.Split(stanza, "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			fields[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
		if strings.EqualFold(fields["enabled"], "no") || strings.EqualFold(fields["enabled"], "false") {
			continue
		}
		uris := strings.Fields(fields["uris"])
		suites := strings.Fields(fields["suites"])
		if len(uris) == 0 || len(suites) == 0 {
			continue
		}
		types := strings.Fields(fields["types"])
		if len(types) == 0 {
			types = []string{"deb"}
		}
		comps := fields["components"]
		for _, typ := range types {
			for _, uri := range uris {
				for _, suite := range suites {
					line := typ + " " + uri + " " + suite
					if comps != "" {
						line += " " + comps
					}
					out = append(out, line)
				}
			}
		}
	}
	return out
}
