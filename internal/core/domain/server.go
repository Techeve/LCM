package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ServerBlindIndex berechnet den deterministischen Blindindex des Server-
// Namens. Standard: normalisierter Klartext (für Tests/ohne Cipher). Die
// storage-/repositories-Ebene ersetzt die Funktion beim Start durch die
// HMAC-Variante (aus dem Master-Key). So bleibt das domain-Paket frei von
// Krypto-Abhängigkeiten.
var ServerBlindIndex = func(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// ServerRef berechnet das deterministische Token eines Servers (HMAC über
// seine id) - der Fremdschlüssel-Ersatz in Kind-Tabellen (vulnerabilities/
// packages), damit die DB die Server-Zuordnung nicht als Klartext-server_id
// preisgibt. Standard: "server:<id>" (Tests/ohne Cipher); die repositories-
// Ebene ersetzt die Funktion beim Start durch die HMAC-Variante.
var ServerRef = func(id uint) string {
	return "server:" + strconv.FormatUint(uint64(id), 10)
}

// CurrentHelperVersion ist der Stand des lcm-helper, den DIESE LCM-Fassung
// ausliefert. Die services-Ebene setzt ihn beim Start (sie kennt das Skript);
// die Domäne braucht ihn nur für den Vergleich mit dem Stand auf dem Server.
// Leer = kein Abgleich möglich, dann wird auch nichts behauptet.
var CurrentHelperVersion string

// HelperOutdated meldet, ob der Helper auf diesem Server hinter der
// ausgelieferten Fassung zurückliegt. Nur im eingeschränkten Modus relevant -
// sonst gibt es gar keinen Helper.
func (s *Server) HelperOutdated() bool {
	if !s.RestrictedSudo || CurrentHelperVersion == "" {
		return false
	}
	return s.HelperVersion != CurrentHelperVersion
}

// Server-Status (Ampelsystem, um „Sehr gut" erweitert).
const (
	// ServerStatusExcellent: makelloser Zustand - grün UND keine einzige
	// bekannte CVE (jeder Schwere), SSH gehärtet und Firewall aktiv.
	ServerStatusExcellent = "excellent"
	ServerStatusGreen     = "green"  // alles ok
	ServerStatusYellow    = "yellow" // Verbindung ok, aber Handlungsbedarf
	ServerStatusRed       = "red"    // keine Verbindung / Auth-Fehler
)

// DNS-Verfügbarkeitstest: dreistufiges Ergebnis, ob die in den globalen
// Einstellungen gepflegten Test-Domains auf dem Server aufgelöst werden können.
const (
	DNSTestFull    = "full"    // alle Test-Domains aufgelöst
	DNSTestPartial = "partial" // einige aufgelöst, andere nicht
	DNSTestNone    = "none"    // keine Test-Domain auflösbar
)

// MaxDNSServers ist die Obergrenze der pro Server setzbaren Nameserver.
const MaxDNSServers = 3

// MaxNTPServers ist die Obergrenze der pro Server setzbaren Zeitserver. Vier
// ist die Empfehlung der NTP-Pool-Projekte: genug für einen belastbaren
// Mehrheitsentscheid, ohne unnötige Last.
const MaxNTPServers = 4

// ClockOffsetWarnSeconds ist die Schwelle, ab der ein Uhrenversatz gemeldet
// wird. Bewusst grob: die Messung enthält die SSH-Laufzeit, und ein paar
// Sekunden Abweichung sind für den Betrieb belanglos. Ab einer halben Minute
// wird es das aber nicht mehr - Kerberos-Tickets brechen bei 5 Minuten,
// TOTP-Codes schon deutlich früher, und Protokolle mehrerer Server lassen
// sich nicht mehr sinnvoll nebeneinanderlegen.
const ClockOffsetWarnSeconds = 30

// splitCSVList zerlegt eine komma-/whitespace-getrennte Liste in bereinigte,
// nicht-leere Einträge.
func splitCSVList(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	}) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// DefaultServiceUser ist der Management-Benutzer, den LCM beim Join
// auf dem Zielsystem anlegt.
const DefaultServiceUser = "lcm-svc"

// Server ist ein per SSH gemanagter Linux-Server.
//
// Zero Trust: Jeder Server hat sein EIGENES SSH-Schlüsselpaar. Der
// Private Key liegt AES-256-GCM-verschlüsselt in PrivateKeyEnc - eine
// Kompromittierung des Schlüssels von Server A gefährdet nie Server B.
// Der beim Join bestätigte Host-Key-Fingerprint erzwingt striktes
// Host-Key-Checking bei jeder späteren Verbindung (MitM-Schutz).
type Server struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Name ist at rest AES-256-GCM-verschlüsselt (wie Host/IP). Da GCM pro
	// Schreibvorgang zufällig ist, kann darauf kein sinnvoller Unique-Index
	// liegen - die Eindeutigkeit sichert der deterministische NameBIdx.
	Name string `gorm:"not null;serializer:aesgcm" json:"name"`
	// NameBIdx ist der HMAC-Blindindex des (klein geschriebenen) Namens: ein
	// deterministischer Wert für die Eindeutigkeits-/Namenssuche, ohne den
	// Klartext zu speichern. Der (Unique-)Index wird in Migrate angelegt -
	// bewusst NICHT im Tag, damit beim Upgrade erst befüllt und dann eindeutig
	// indexiert wird (leere Werte kollidierten sonst).
	NameBIdx string `gorm:"column:name_bidx" json:"-"`
	// Ref ist das deterministische Server-Token (HMAC der id), auf das
	// vulnerabilities/packages verweisen - statt der Klartext-server_id. Der
	// Unique-Index entsteht in storage.Migrate (nach dem Backfill), gesetzt wird
	// Ref im AfterCreate-Hook (die id steht erst nach dem Insert fest).
	Ref string `gorm:"column:ref" json:"-"`
	// Host/IPAddresses sind die sensiblen Netz-Identifikatoren der verwalteten
	// Server - at rest AES-256-GCM-verschlüsselt (transparent ent-/verschlüsselt
	// über den `aesgcm`-GORM-Serializer). Zur Laufzeit liegen sie im Klartext im
	// Struct vor, der Verbindungsaufbau nutzt sie unverändert.
	Host        string `gorm:"not null;serializer:aesgcm" json:"host"` // Hostname oder IP
	SSHPort     int    `gorm:"default:22" json:"ssh_port"`
	ServiceUser string `gorm:"not null" json:"service_user"`

	// SSH-Vertrauensanker - niemals serialisieren bzw. nur der Public-Teil.
	HostKeyFingerprint string `gorm:"not null" json:"host_key_fingerprint"` // SHA256:...
	PrivateKeyEnc      string `gorm:"not null" json:"-"`                    // AES-GCM-verschlüsselter PEM
	PublicKey          string `json:"public_key"`                           // authorized_keys-Zeile

	// RestrictedSudo: Der Management-Benutzer (ServiceUser) wurde mit
	// EINGESCHRÄNKTEN sudo-Rechten angelegt - er darf per sudoers-Whitelist
	// nur eine feste Auswahl an Binaries (Paketverwaltung, Docker, ufw) plus
	// den validierenden LCM-Helper (Repositories, apt-Cache, SSH-Konfiguration
	// und -Port, Benutzer-Sync) ausführen, aber KEIN beliebiges Kommando und
	// keinen Root-Shell-/Dateisystemzugriff. false = Voll-Root (NOPASSWD:ALL,
	// Default und bisheriges Verhalten). Gesperrt bleiben im eingeschränkten
	// Modus nur freie Skripte/Custom-Aktionen und der Neustart.
	RestrictedSudo bool `gorm:"default:false" json:"restricted_sudo"`

	// HelperVersion ist der Stand des lcm-helper AUF dem Server (nur im
	// eingeschränkten Modus gefüllt). LCM schreibt den Helper beim Onboarding
	// und beim Einschränken - danach nie wieder, weil dafür Root nötig wäre,
	// das der eingeschränkte Service-User gerade nicht mehr hat. Ohne diesen
	// Abgleich liefen Korrekturen an den privilegierten Aktionen still ins
	// Leere. Weicht der Wert vom eingebauten Stand ab, weist die Ampel das
	// aus; zurück auf Stand bringt ihn der Reconnect (Admin-Login).
	HelperVersion string `json:"helper_version"`

	// UserSyncDisabled: Der Linux-Benutzer-Sync ist für diesen Server
	// abgeschaltet - LCM legt hier keine Benutzer-Accounts an und verteilt
	// keine authorized_keys. Zuweisungen (direkt oder über Gruppen) bleiben
	// gespeichert und greifen wieder, sobald der Schalter entfernt wird.
	UserSyncDisabled bool `gorm:"default:false" json:"user_sync_disabled"`

	// LCM Remote: Transport bestimmt, wie LCM den Server erreicht.
	//   - TransportSSH (Default): LCM verbindet sich eingehend per SSH.
	//   - TransportAgent: der lcm-agent auf dem Server verbindet sich
	//     AUSGEHEND per MQTT-über-WebSocket mit dem eingebetteten Broker -
	//     für Server hinter NAT / ohne feste IP / unterwegs.
	// Bei Agent-Servern sind Host/SSHPort/HostKeyFingerprint/PrivateKeyEnc
	// leer und ServiceUser ist "root" (der Agent läuft als Root-Dienst;
	// wrapSudo lässt Kommandos für root unverändert).
	Transport string `gorm:"default:ssh" json:"transport"`
	// AgentID ist die zufällige UUID, mit der sich der Agent am Broker
	// ausweist (MQTT-Username = Client-ID). Bewusst NICHT die numerische
	// Server-ID, damit Agent-Identitäten nicht enumerierbar sind.
	AgentID string `gorm:"index" json:"-"`
	// AgentTokenHash ist der SHA-256-Hash des Token-Secrets (Muster API-Key:
	// nur Hash at rest, Klartext wird einmalig beim Erzeugen angezeigt).
	// AgentTokenPrefix (erste Zeichen des Secrets) dient der Wiedererkennung
	// im UI, nie der Authentifizierung.
	AgentTokenHash   string `json:"-"`
	AgentTokenPrefix string `json:"agent_token_prefix,omitempty"`
	// AgentLastSeenAt/AgentVersion pflegt der Broker bei Connect/Disconnect.
	AgentLastSeenAt *time.Time `json:"agent_last_seen_at,omitempty"`
	AgentVersion    string     `json:"agent_version,omitempty"`
	// AgentConnected ist der Laufzeitzustand aus dem AgentHub (nicht
	// persistiert) - vom Controller vor der Auslieferung befüllt.
	AgentConnected bool `gorm:"-" json:"agent_connected"`

	// Ergebnisse des System-Scans (initial + System-Sync).
	// OS-/Kernel-/CPU-Profil at rest verschlüsselt (`aesgcm`): so verrät die per
	// server_ref verknüpfte Server-Zeile einem DB-Leser nichts über das System.
	// Filterung (Dashboard) läuft clientseitig über die entschlüsselten Werte.
	OSName    string `gorm:"serializer:aesgcm" json:"os_name"`    // z.B. "Debian GNU/Linux"
	OSVersion string `gorm:"serializer:aesgcm" json:"os_version"` // z.B. "12 (bookworm)"
	// RebootRequired: Das System fordert einen Neustart an, um Updates
	// vollständig wirksam zu machen (z. B. neuer Kernel/libc). Debian/Ubuntu
	// setzen /var/run/reboot-required, die RHEL-Familie meldet es über
	// needs-restarting, SUSE über zypper needs-rebooting. Wird beim
	// System-Scan/Refresh erhoben.
	RebootRequired bool `gorm:"default:false" json:"reboot_required"`
	// FailedChecks zählt die AUFEINANDERFOLGENDEN fehlgeschlagenen
	// Erreichbarkeits-Kontakte (Health-Ping, Refresh, Rule-Lauf). Jeder
	// erfolgreiche Kontakt setzt den Zähler zurück.
	//
	// Warum nicht einfach Reachable=false? Ein einzelner Fehlschlag ist im
	// Betrieb Alltag - ein Paketverlust, ein laufender Neustart, ein kurzer
	// Netz-Aussetzer. Erst die Wiederholung ist eine Aussage. Der Zähler
	// trennt „gerade nicht erreicht" von „ist offline".
	FailedChecks int `gorm:"default:0" json:"failed_checks"`
	// ListeningPackages: beim System-Scan automatisch erkannte Pakete, deren
	// Prozesse auf von außen erreichbaren Ports lauschen (ss → PID →
	// Paket-Zuordnung, best effort). Kommagetrennt. CVEs dieser Pakete
	// werden automatisch eine Stufe höher gewichtet.
	ListeningPackages string `gorm:"serializer:aesgcm" json:"listening_packages"`
	// OSID/OSVersionID stammen aus /etc/os-release (ID, VERSION_ID) und
	// dienen der Support-/EOL-Bewertung (z.B. "ubuntu"/"22.04", "debian"/"12").
	OSID        string `gorm:"serializer:aesgcm" json:"os_id"`
	OSVersionID string `gorm:"serializer:aesgcm" json:"os_version_id"`
	// Proxmox-Erkennung: Proxmox-Systeme sind Debian-basiert (os-release
	// meldet Debian), tragen aber ihre eigenen Produkt-Pakete. Erkannt wird
	// über den Paketbestand (pve-manager / proxmox-backup-server / pmg-api).
	// ProxmoxType: ProxmoxPVE/PBS/PMG, leer = kein Proxmox. Auf Proxmox-
	// Systemen sperrt LCM einige Aktionen (Repos hinzufügen, ufw-Firewall,
	// Benutzer-Sync), weil Proxmox diese Bereiche selbst verwaltet.
	ProxmoxType    string `json:"proxmox_type"`
	ProxmoxVersion string `json:"proxmox_version"` // z.B. "8.2.4"

	// MikroTik RouterOS: ein grundlegend anderer Gerätetyp - kein Linux mit
	// POSIX-Shell/Paketverwaltung, sondern die RouterOS-CLI über SSH. Erkannt
	// wird er beim dedizierten Onboarding (nicht per os-release). OSID trägt
	// dann OSIDRouterOS; IsRouterOS() steuert das Feature-Gating. LCM überwacht
	// hier nur die Aktualität der RouterOS-Version - Firewall, CVE-Scan, Repos
	// und Benutzer-Sync sind mangels Paketverwaltung nicht möglich.
	//
	// LoginPasswordEnc hält das (AES-GCM-verschlüsselte) Login-Passwort für die
	// Passwort-Authentifizierung; bei Key-Auth bleibt es leer und LCM nutzt den
	// gespeicherten PrivateKeyEnc/PublicKey wie bei SSH-Servern.
	LoginPasswordEnc string `json:"-"`
	// RouterBoardModel ist das Board-/Gerätemodell (z.B. "RB5009UG+S+" oder
	// "CHR" für Cloud Hosted Router). RouterOSChannel ist der Update-Kanal
	// (stable / long-term / testing / development).
	RouterBoardModel string `json:"routerboard_model,omitempty"`
	RouterOSChannel  string `json:"routeros_channel,omitempty"`
	// RouterOSLatestVersion ist die vom Router selbst gemeldete neueste
	// verfügbare Version seines Kanals (/system package update
	// check-for-updates). RouterOSUpdateAvailable = eine neuere Version steht
	// bereit → fließt als Aktualitäts-Warnung in die Statusampel ein.
	RouterOSLatestVersion   string `json:"routeros_latest_version,omitempty"`
	RouterOSUpdateAvailable bool   `gorm:"default:false" json:"routeros_update_available"`

	// Synology DSM (siehe IsSynologyDSM): LCM spricht die Web-API, nicht die
	// Shell. DSMPort ist der HTTPS-Port der DSM-Oberfläche (Standard 5001),
	// DSMCertFingerprint der beim Onboarding bestätigte SHA-256-Fingerprint
	// des TLS-Zertifikats - DSM liefert ab Werk ein selbstsigniertes, das
	// keine Kette prüfen lässt; das Pinning ist hier der MitM-Schutz (analog
	// zum SSH-Host-Key und zum Agent-Token-Pin).
	DSMPort            int    `gorm:"default:0" json:"dsm_port,omitempty"`
	DSMCertFingerprint string `json:"dsm_cert_fingerprint,omitempty"`
	// DSMModel ist das Gerätemodell (z.B. "DS923+" oder "VirtualDSM"),
	// DSMLatestVersion die von Synology gemeldete neuere Fassung.
	DSMModel           string `json:"dsm_model,omitempty"`
	DSMLatestVersion   string `json:"dsm_latest_version,omitempty"`
	DSMUpdateAvailable bool   `gorm:"default:false" json:"dsm_update_available"`
	// DSMSecurityRisks ist die Zahl der Befunde des DSM-eigenen Security
	// Advisors (Schweregrade risk/danger); DSMSecuritySummary nennt die
	// betroffenen Kategorien im Klartext. LCM übernimmt diese Bewertung,
	// statt sie ohne Shell-Zugriff nachzubauen.
	DSMSecurityRisks   int    `gorm:"default:0" json:"dsm_security_risks"`
	DSMSecuritySummary string `json:"dsm_security_summary,omitempty"`
	// Virtualization ist die rohe systemd-detect-virt-Ausgabe: "none" (blankes
	// Blech), ein Container-Typ ("lxc", "docker", …) oder ein VM-Typ ("kvm",
	// "qemu", "vmware", …). Leer = unbekannt.
	Virtualization string `json:"virtualization"`
	// PackageManager ist die auf dem Zielsystem erkannte Paketverwaltung
	// ("apt", "dnf", "yum" oder "zypper"). Sie steuert die Scan- und
	// Update-Kommandos - LCM unterstützt so Debian/Ubuntu ebenso wie die
	// RHEL-Familie und SUSE. Leer = unbekannt/nicht ermittelt.
	PackageManager string `json:"package_manager"`
	// HasACL meldet, ob setfacl/getfacl vorhanden sind, ACLUsable zusätzlich,
	// ob das Dateisystem sie wirklich trägt. Verzeichnisrechte aus den
	// Berechtigungsprofilen brauchen beides - und beides ist auf vielen
	// Systemen NICHT gegeben: Auf Debian/Ubuntu sind Bibliothek und Werkzeuge
	// getrennte Pakete, und ZFS führt POSIX-ACLs nur mit acltype=posixacl.
	// Geraten wird deshalb nichts, es wird geprüft.
	HasACL    bool `gorm:"default:false" json:"has_acl"`
	ACLUsable bool `gorm:"default:false" json:"acl_usable"`
	// ACLRetryAfter drosselt die Grundsatz-Regel „ACL einrichten": Scheitert
	// die Installation (kein Repo-Zugang), darf sie nicht bei jedem
	// Health-Ping erneut versucht und jedes Mal als Eingriff protokolliert
	// werden.
	ACLRetryAfter *time.Time `json:"acl_retry_after,omitempty"`

	// HasSnap: snapd ist auf dem Zielsystem vorhanden (zweite
	// Paketverwaltung, v.a. Ubuntu) - unabhängig davon, ob aktuell
	// Snaps installiert sind.
	HasSnap bool `gorm:"default:false" json:"has_snap"`
	// HasDocker: Docker-CLI vorhanden - Container/Images werden beim Scan
	// mit erfasst. HasCompose: das Compose-v2-Plugin (`docker compose`)
	// ist verfügbar (Voraussetzung für Projekt-Updates aus LCM).
	HasDocker  bool `gorm:"default:false" json:"has_docker"`
	HasCompose bool `gorm:"default:false" json:"has_compose"`
	// CVERelevantContainers: kommagetrennte Container-Namen, deren Image-CVEs
	// in die Status-Bewertung einfließen. Docker-CVEs werden standardmäßig
	// NICHT gewertet (Container-Isolation, Verantwortung beim Image-Anbieter) -
	// nur für hier explizit benannte Container zählen sie mit voller Schwere.
	// Am Container-Namen statt am Image festgemacht, damit die Auswahl
	// Image-Updates und Inventar-Rescans übersteht.
	CVERelevantContainers string `json:"cve_relevant_containers"`
	// DockerUpdatesDisabled: Auf diesem Server spielt LCM keine neuen
	// Image-Versionen ein - weder von Hand noch über eine Regel. Gedacht für
	// Server, deren Container an anderer Stelle gepflegt werden (eigene
	// CI/CD, Anbieter-Wartung): LCM soll dort zusehen, nicht eingreifen.
	// Das Inventar wird weiter erfasst und verfügbare Updates werden
	// weiterhin angezeigt - abgeschaltet ist das Einspielen, nicht das
	// Hinsehen.
	DockerUpdatesDisabled bool `gorm:"default:false" json:"docker_updates_disabled"`
	// DockerCVEsIgnored: Die CVE-Funde aus Container-Images dieses Servers
	// bleiben vollständig außen vor - sie zählen weder für Ampel und Alarme
	// noch erscheinen sie in der Sicherheitsübersicht.
	//
	// Ohne den Schalter zählen Docker-Funde ohnehin nur für ausdrücklich als
	// relevant markierte Container (CVERelevantContainers), sind aber
	// sichtbar. Ist er gesetzt, sticht er auch diese Markierung: Wer die
	// Funde eines Servers gar nicht sehen will, meint alle.
	// Spaltenname explizit: GORM würde aus „DockerCVEsIgnored" sonst
	// docker_cv_es_ignored machen (es trennt an der Grenze CVE→s).
	DockerCVEsIgnored bool `gorm:"column:docker_cves_ignored;default:false" json:"docker_cves_ignored"`
	// KernelVersion ist der LAUFENDE Kernel (`uname -r`) - nicht der neueste
	// installierte. Die beiden weichen nach einem Kernel-Update bis zum
	// Neustart voneinander ab; genau das macht die Angabe wertvoll.
	KernelVersion string `gorm:"serializer:aesgcm" json:"kernel_version"`
	// InstalledKernels ist das erfasste Kernel-Paket-Inventar (JSON-Array von
	// KernelPackage). In Containern bewusst leer: dort laeuft der Kernel des
	// Hosts, installierte Kernel-Pakete waeren wirkungslos.
	InstalledKernels string `gorm:"serializer:aesgcm" json:"installed_kernels"`
	CPUModel         string `gorm:"serializer:aesgcm" json:"cpu_model"`
	CPUCores         int    `json:"cpu_cores"`
	MemTotalMB       int64  `json:"mem_total_mb"`
	MemUsedMB        int64  `json:"mem_used_mb"`
	DiskTotalMB      int64  `json:"disk_total_mb"`
	DiskUsedMB       int64  `json:"disk_used_mb"`
	IPAddresses      string `gorm:"serializer:aesgcm" json:"ip_addresses"` // kommagetrennt, AES-GCM at rest

	// Verfügbarkeit (Health-Check) & Security-Zustand.
	LastSeenAt *time.Time `json:"last_seen_at"` // letzter erfolgreicher SSH-Kontakt
	LastError  string     `json:"last_error"`   // letzter Verbindungs-/Jobfehler
	Reachable  bool       `gorm:"default:false" json:"reachable"`
	// UnreachableUncritical: Ist der Server nicht erreichbar, wird das NICHT
	// sofort als kritisch (rot) gewertet. Bis zum Ablauf der Kulanzfrist
	// (UnreachableGraceDays) behält er seinen zuletzt bekannten Status und wird
	// in der UI nur ausgegraut; erst danach springt er wegen Nichterreichbarkeit
	// auf Rot. Pro Server auf der Einstellungsseite konfigurierbar.
	UnreachableUncritical bool `gorm:"default:false" json:"unreachable_uncritical"`
	// UnreachableGraceDays: Kulanzfrist in Tagen für UnreachableUncritical.
	UnreachableGraceDays int  `gorm:"default:28" json:"unreachable_grace_days"`
	SSHHardened          bool `gorm:"default:false" json:"ssh_hardened"`
	// SSHRootLoginDisabled: LCM erzwingt PermitRootLogin no über ein eigenes
	// sshd-Drop-in (10-lcm-ssh.conf) - der root-Benutzer darf sich dann nicht
	// mehr direkt per SSH anmelden. Unabhängig von der SSH-Härtung schaltbar.
	SSHRootLoginDisabled bool `gorm:"default:false" json:"ssh_root_login_disabled"`
	FirewallActive       bool `gorm:"default:false" json:"firewall_active"`
	// FirewallAllowedPorts sind die zusätzlich zur SSH freigegebenen TCP-Ports
	// (kommagetrennt, z.B. "80,443"). SSH ist immer erlaubt. Legacy-Feld:
	// maßgeblich ist FirewallRules; die CSV-Zusammenfassung wird beim Anwenden
	// mitgepflegt (Anzeige + Abwärtskompatibilität).
	FirewallAllowedPorts string `gorm:"serializer:aesgcm" json:"firewall_allowed_ports"`
	// FirewallRules ist die maßgebliche Firewall-Konfiguration: JSON-Array von
	// FirewallRule (Port, TCP/UDP, Bind-Adresse, IP-Version). Leer = nur die
	// Legacy-Portliste (FirewallAllowedPorts) bekannt.
	FirewallRules string `gorm:"serializer:aesgcm" json:"firewall_rules"`
	// FirewallSSHSources schränkt die SSH-Freigabe auf bestimmte QUELLEN ein
	// (JSON von FirewallSSHSources: benannte Allowlists und/oder eigene
	// IPs/CIDRs). Leer = von überall erreichbar. Die Bind-Adresse oben ist
	// die Gegenrichtung (auf WELCHER lokalen Adresse gelauscht wird).
	FirewallSSHSources string `gorm:"serializer:aesgcm" json:"firewall_ssh_sources"`
	// FirewallTool ist das beim Scan erkannte installierte Firewall-Werkzeug
	// (FirewallToolUfw/Firewalld/Nftables, "" = keines erkannt). Ein erkanntes
	// Werkzeug gewinnt immer vor dem für die Distribution vorgesehenen - LCM
	// installiert nie eine zweite Firewall neben einer vorhandenen.
	FirewallTool string `gorm:"serializer:aesgcm" json:"firewall_tool"`
	// ListeningPorts ist das beim Scan erfasste Inventar lauschender Sockets
	// (JSON-Array von ListeningPort) - die Vorschläge im Firewall-Dialog.
	ListeningPorts string `gorm:"serializer:aesgcm" json:"listening_ports"`
	// AptProxyActive: der Server leitet seine APT-Anfragen über den zentralen
	// APT-Cache (Drop-in /etc/apt/apt.conf.d/02lcm-apt-cache).
	AptProxyActive bool `gorm:"default:false" json:"apt_proxy_active"`
	// RHSMStatus ist der Registrierungsstand bei Red Hats Subscription
	// Management - leer auf allem, was kein subscription-manager kennt
	// (Rocky, AlmaLinux, CentOS und die ganze Debian-Welt).
	//
	// Das Feld gibt es, weil ein nicht registriertes RHEL keine Paketquellen
	// bekommt: dnf findet dann nichts, und LCM meldete bisher zufrieden
	// null Updates. Ein ungepflegter Server sähe damit aus wie ein gepflegter.
	// column: explizit setzen - gorm zerlegte RHSMStatus sonst in Einzelbuchstaben.
	RHSMStatus string `gorm:"column:rhsm_status;serializer:aesgcm" json:"rhsm_status"`

	// HTTPSRevertURLs sind die Paketquellen, die sich auf http zurückstellen
	// lassen (kommagetrennte https-URLs, beim Scan ermittelt). Gefüllt aus dem
	// Protokoll der LCM-Umstellung, ersatzweise aus den Distributions-Spiegeln
	// - siehe HTTPSRevertCandidates. Leer heißt: nichts zurückzustellen.
	// column: explizit setzen - gorm trennte HTTPS sonst zu http_s_revert_urls.
	HTTPSRevertURLs string `gorm:"column:https_revert_urls;serializer:aesgcm" json:"https_revert_urls"`

	// DNS: von LCM gesetzte Nameserver (bis zu drei, kommagetrennt). Werden per
	// Aktion „DNS anwenden" auf den Server geschrieben (systemd-resolved-Drop-in
	// oder /etc/resolv.conf, siehe services/dns.go). Leer = LCM verwaltet DNS nicht.
	DNSServers string `json:"dns_servers"`
	// DNSCurrent sind die TATSÄCHLICH aktiven Resolver des Servers (read-back,
	// kommagetrennt) - reine Anzeige neben der Firewall. Wird beim Anwenden und
	// beim DNS-Test aktualisiert.
	DNSCurrent string `json:"dns_current"`
	// DNSTestStatus ist das Ergebnis des letzten DNS-Verfügbarkeitstests:
	// "" (nie getestet), DNSTestFull, DNSTestPartial oder DNSTestNone.
	DNSTestStatus string     `json:"dns_test_status"`
	DNSTestAt     *time.Time `json:"dns_test_at"`
	// DNSTestDetail hält die pro-Domain-Ergebnisse des letzten Tests (für Tooltip
	// und Job-Output), z.B. "OK github.com; FAIL deb.debian.org".
	DNSTestDetail string `json:"dns_test_detail"`

	// Zeit: Zeitzone, Zeitdienst und Uhrenvergleich (siehe services/timesync.go).
	// Eine falsch gehende Uhr fällt im Betrieb kaum auf, verdirbt aber
	// TLS-Prüfungen, die Reihenfolge in Protokollen über mehrere Server hinweg,
	// zeitbasierte Einmalpasswörter und signierte Paket-Metadaten.
	Timezone string `json:"timezone"` // IANA-Zone, z.B. "Europe/Berlin"
	// NTPService ist der erkannte Zeitdienst ("chrony", "systemd-timesyncd",
	// "ntpd", "busybox-ntpd"); leer = keiner gefunden.
	NTPService string `json:"ntp_service"`
	// NTPSynchronized: der Dienst meldet die Uhr als synchronisiert.
	NTPSynchronized bool `json:"ntp_synchronized"`
	// NTPServers sind die auf dem Server konfigurierten Zeitserver (kommagetrennt).
	NTPServers string `json:"ntp_servers"`
	// ClockOffsetSeconds ist der Versatz der Server-Uhr gegenüber LCM
	// (positiv = der Server geht vor). Die Messung enthält die SSH-Laufzeit,
	// ist also auf ein bis zwei Sekunden genau - für die Frage „geht die Uhr
	// überhaupt richtig?" reicht das.
	ClockOffsetSeconds int        `json:"clock_offset_seconds"`
	TimeCheckedAt      *time.Time `json:"time_checked_at"`

	// Deep Scan (tiefergehende Sicherheitsprüfung auf dem Ziel: Kernel-Reboot-
	// Lücke via needrestart, Kernel-CVEs aus Trivy, Härtungs-/Fehlkonfigurations-
	// Audit via Lynis oder LCM-Eigenprüfungen). Die Einzelbefunde liegen in
	// deep_scan_findings; hier stehen die verdichteten Kennzahlen für Anzeige,
	// Ampel und Alarm.
	HardeningIndex *int       `json:"hardening_index"` // Lynis-Index 0-100; nil = unbekannt/nicht geprüft
	DeepScanAt     *time.Time `json:"deep_scan_at"`
	DeepScanError  string     `json:"deep_scan_error"`
	// KernelRebootPending: der laufende Kernel ist älter als der installierte
	// (needrestart KSTA 2/3) - Sicherheits-Fixes wirken erst nach dem Reboot.
	KernelRebootPending bool `gorm:"default:false" json:"kernel_reboot_pending"`
	// DeepScanWarnings: Anzahl der Befunde mit Schwere warning/critical
	// (Härtungs-Warnungen + Dienste mit alten Bibliotheken) - speist die Ampel,
	// ohne die Einzelbefunde laden zu müssen.
	DeepScanWarnings int `gorm:"default:0" json:"deep_scan_warnings"`

	// Sicherheits-Tools (Intrusion Prevention): fail2ban bzw. CrowdSec - per
	// Aktion „Sicherheit-Tools" installierbar und beim Hardware-Scan erkannt.
	// installed = Binary vorhanden, active = Dienst läuft (systemctl is-active).
	Fail2banInstalled bool `gorm:"column:fail2ban_installed;default:false" json:"fail2ban_installed"`
	Fail2banActive    bool `gorm:"column:fail2ban_active;default:false" json:"fail2ban_active"`
	// column: explizit setzen - sonst würde gorm CrowdSec → crowd_sec trennen und
	// die per UpdateFields-Map (Schlüssel crowdsec_*) geschriebenen Spalten fehlten.
	CrowdSecInstalled bool `gorm:"column:crowdsec_installed;default:false" json:"crowdsec_installed"`
	CrowdSecActive    bool `gorm:"column:crowdsec_active;default:false" json:"crowdsec_active"`
	// CrowdSecLapiMode: wie die CrowdSec-Instanz dieses Servers angebunden
	// wurde ("local"/"remote"/"console"; bei Installation über LCM gesetzt).
	// CrowdSecLapiURL: an welche LAPI der Agent laut seiner Credentials-Datei
	// (/etc/crowdsec/local_api_credentials.yaml) tatsächlich meldet - wird beim
	// Scan live gelesen und speist die „Angebundene Server"-Liste auf der
	// CrowdSec-Einstellungsseite.
	CrowdSecLapiMode string `gorm:"column:crowdsec_lapi_mode" json:"crowdsec_lapi_mode"`
	CrowdSecLapiURL  string `gorm:"column:crowdsec_lapi_url" json:"crowdsec_lapi_url"`
	// SSH2FAEnabled: SSH-Logins verlangen Key + TOTP (Drop-in 55-lcm-2fa.conf,
	// siehe ssh_2fa.go). Spaltenname explizit - GORM zerlegte das Akronym sonst.
	SSH2FAEnabled bool `gorm:"column:ssh_2fa_enabled;default:false" json:"ssh_2fa_enabled"`
	// LCMSourceIP ist die Quell-IP, mit der LCM diesen Server erreicht (aus
	// $SSH_CONNECTION auf dem Ziel gelesen - NAT-korrekt). Sie wird bei der
	// Installation automatisch in die Allowlist von fail2ban/CrowdSec gesetzt,
	// damit LCM sich nicht selbst aussperrt.
	LCMSourceIP string `json:"lcm_source_ip"`

	// CVE-Scan (Trivy) gegen den erfassten Paketbestand.
	LastCVEScanAt *time.Time `json:"last_cve_scan_at"` // letzter erfolgreicher CVE-Scan
	CVEScanError  string     `json:"cve_scan_error"`   // z.B. "Trivy nicht verfügbar"

	// Demo-Server werden nie per SSH kontaktiert (--demo Testdaten).
	IsDemo bool `gorm:"default:false" json:"is_demo"`

	Groups []ServerGroup `gorm:"many2many:server_group_servers" json:"groups,omitempty"`
	// LinuxUsers, deren Accounts + SSH-Keys auf diesen Server provisioniert
	// werden (direkt zugeordnet; zusätzlich kommen die über Gruppen dazu).
	LinuxUsers []LinuxUser `gorm:"many2many:server_linux_users" json:"-"`
}

// Transport-Arten (Server.Transport).
const (
	TransportSSH   = "ssh"   // LCM verbindet sich eingehend per SSH (Default)
	TransportAgent = "agent" // lcm-agent verbindet sich ausgehend per MQTT
)

// IsAgent meldet, ob der Server über den lcm-agent (MQTT) verwaltet wird.
func (s *Server) IsAgent() bool { return s.Transport == TransportAgent }

// Proxmox-Produkttypen (Server.ProxmoxType).
const (
	ProxmoxPVE = "pve" // Proxmox Virtual Environment
	ProxmoxPBS = "pbs" // Proxmox Backup Server
	ProxmoxPMG = "pmg" // Proxmox Mail Gateway
	// ProxmoxPDM: Proxmox Datacenter Manager - das jüngste Produkt der Reihe.
	// Es fehlte in der Erkennung, weshalb die Schutzsperre dort nicht griff und
	// LCM z.B. die Firewall-Konfiguration eines Proxmox-Systems hätte
	// überschreiben können (BUG-025).
	ProxmoxPDM = "pdm"
)

// IsProxmox meldet, ob auf dem Server ein Proxmox-Produkt erkannt wurde.
// (Die Produktnamen zur Anzeige mappt das Frontend - hier bewusst keine
// ungenutzte Anzeige-Logik.)
func (s *Server) IsProxmox() bool { return s.ProxmoxType != "" }

// Registrierungsstand bei Red Hats Subscription Management (RHSMStatus).
const (
	// RHSMRegistered: registriert und mit gültigem Zugriff auf die Quellen.
	// Dazu zählt auch der Status "Disabled" - bei Simple Content Access
	// prüft Red Hat keine Berechtigungen mehr, und das ist der Normalfall.
	RHSMRegistered = "registered"
	// RHSMInvalid: registriert, aber ohne ausreichende Berechtigung
	// (Insufficient/Invalid/Unknown) - die Quellen können trotzdem leer sein.
	RHSMInvalid = "invalid"
	// RHSMUnregistered: gar nicht registriert.
	RHSMUnregistered = "unregistered"
)

// OSIDRouterOS ist der OSID-Marker für MikroTik-RouterOS-Geräte. Anders als
// Linux-OSIDs stammt er nicht aus /etc/os-release, sondern wird beim
// dedizierten RouterOS-Onboarding gesetzt.
const OSIDRouterOS = "routeros"

// IsRouterOS meldet, ob dieser Server ein MikroTik-RouterOS-Gerät ist. Für
// solche Geräte sperrt LCM alle Aktionen, die eine Paketverwaltung oder eine
// POSIX-Shell voraussetzen (Firewall-Verwaltung, CVE-Scan, Repos,
// Benutzer-Sync, SSH-Härtung) und überwacht nur die Versions-Aktualität.
func (s *Server) IsRouterOS() bool { return s.OSID == OSIDRouterOS }

// OSIDSynologyDSM ist der OSID-Marker für Synology-DSM-Geräte. Wie bei
// RouterOS stammt er nicht aus /etc/os-release, sondern wird beim dedizierten
// DSM-Onboarding gesetzt.
const OSIDSynologyDSM = "dsm"

// IsSynologyDSM meldet, ob dieser Server ein Synology-DSM-Gerät ist.
//
// DSM ist zwar Linux-basiert, aber KEIN verwaltbarer Linux-Server: es gibt
// kein /etc/os-release, der Kernel ist ein alter Synology-Fork (Falschalarme
// im CVE-Scan), Pakete verwaltet synopkg statt apt, und Benutzer/Dienste
// verwaltet DSM selbst - ein LCM-Service-User mit sudo würde mit DSMs eigener
// Konfigurationsverwaltung kollidieren. LCM spricht deshalb die dokumentierte
// Web-API (SYNO.*) und überwacht: DSM-Version und verfügbare Updates,
// installierte Pakete, Volumes/Belegung, Zeit/NTP und den Security Advisor.
func (s *Server) IsSynologyDSM() bool { return s.OSID == OSIDSynologyDSM }

// IsAPIDevice meldet, ob dieser Server über eine Geräte-API statt über eine
// POSIX-Shell verwaltet wird (RouterOS, Synology DSM). Für sie sind alle
// shell-/paketverwaltungsgestützten Aktionen gesperrt.
func (s *Server) IsAPIDevice() bool { return s.IsRouterOS() || s.IsSynologyDSM() }

// IsLcmHost meldet, ob dieser Server der LCM-Host selbst ist (Host localhost /
// Loopback). Für ihn zeigt die UI das LCM-Logo und bietet host-spezifische
// Aktionen (Trivy- und apt-cacher-ng-Einrichtung) an.
func (s *Server) IsLcmHost() bool {
	// Nur Loopback UND Standard-SSH-Port zählen als LCM-Host: über
	// 127.0.0.1:<hoher Port> gejointe Systeme sind typischerweise
	// Port-Forwards auf ANDERE Maschinen (NAT, Container) - die dürfen die
	// LCM-Host-Sonderbehandlung (Trivy/apt-cacher/LAPI-Karte) nicht bekommen.
	if s.SSHPort != 0 && s.SSHPort != 22 {
		return false
	}
	return IsLoopbackHost(s.Host)
}

// IsLoopbackHost meldet, ob eine Adresse auf den eigenen Rechner zeigt.
// Eigene Funktion, weil dieselbe Frage an zwei Stellen zählt: bei der
// LCM-Host-Sonderrolle und beim Aufnehmen eines Servers im Container - dort
// meint localhost den Container und nicht die Maschine darunter.
func IsLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// DiskUsagePercent liefert die Festplattenbelegung in Prozent (0 wenn unbekannt).
func (s *Server) DiskUsagePercent() int {
	if s.DiskTotalMB <= 0 {
		return 0
	}
	return int(s.DiskUsedMB * 100 / s.DiskTotalMB)
}

// FormatMiB formatiert eine Größe (Eingabe in Mebibyte) menschenlesbar in
// binären Einheiten (MiB/GiB/TiB), automatisch nach Größenordnung skaliert -
// dieselbe Logik wie die Frontend-Anzeige, für Alarm-/Log-Texte.
func FormatMiB(mb int64) string {
	if mb < 1024 {
		return fmt.Sprintf("%d MiB", mb)
	}
	gib := float64(mb) / 1024
	if gib < 1024 {
		return fmt.Sprintf("%.1f GiB", gib)
	}
	return fmt.Sprintf("%.2f TiB", gib/1024)
}

// BeforeSave hält den Namens-Blindindex synchron zum (verschlüsselten) Namen -
// auf ALLEN GORM-Schreibpfaden (Create/Save), auch direkten db.Create-Aufrufen
// (Seed/Tests). Bei feldweisen Map-Updates ohne Name (s.Name == "") bleibt der
// Blindindex unangetastet, damit z.B. Health-Check-Updates ihn nicht leeren.
func (s *Server) BeforeSave(*gorm.DB) error {
	if s.Name != "" {
		s.NameBIdx = ServerBlindIndex(s.Name)
	}
	return nil
}

// AfterCreate setzt das Server-Token (HMAC der frisch vergebenen id). Es kann
// erst NACH dem Insert bestimmt werden - vulnerabilities/packages verweisen
// darüber auf den Server, ohne die Klartext-id zu speichern.
func (s *Server) AfterCreate(tx *gorm.DB) error {
	ref := ServerRef(s.ID)
	if s.Ref == ref {
		return nil
	}
	s.Ref = ref
	return tx.Model(s).UpdateColumn("ref", ref).Error
}

// StatusInsight ist ein einzelner Befund der Ampel-Bewertung -
// im UI hinter dem Info-Icon (i) aufgeschlüsselt. Severity "info" markiert
// keine Probleme, sondern die Gründe, warum ein „OK"-Server (noch) nicht
// „Sehr gut" ist.
type StatusInsight struct {
	Severity string `json:"severity"` // "info", "warning" oder "critical"
	Message  string `json:"message"`
	// Key/Params tragen dieselbe Aussage in übersetzbarer Form: Die Oberfläche
	// schlägt Key im Sprachkatalog nach und setzt Params ein. Message bleibt
	// der deutsche Klartext für alles ohne Katalog - Benachrichtigungen,
	// Protokolle, Fremdnutzer der API. Ohne Key zeigt die Oberfläche Message.
	Key    string            `json:"key,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// insight bündelt beide Fassungen eines Befunds an einer Stelle.
func insight(severity, key, message string, params map[string]string) StatusInsight {
	return StatusInsight{Severity: severity, Key: key, Message: message, Params: params}
}

// countParams ist der Parametersatz der Mengen-Befunde; die Oberfläche wählt
// darüber zugleich Ein- oder Mehrzahl.
func countParams(n int) map[string]string {
	return map[string]string{"count": itoa(n)}
}

// TrafficLightInput bündelt die Fakten für die Ampel-Bewertung, die
// nicht am Server-Struct selbst hängen.
type TrafficLightInput struct {
	OutdatedPackages int  // Anzahl überfälliger Paket-Updates
	LastJobFailed    bool // letzter abgeschlossener Job fehlgeschlagen?
	LastJobName      string
	// Now aktiviert die OS-Support-/EOL-Bewertung (Zero-Wert = deaktiviert,
	// damit schlanke Tests ohne Zeitbezug unverändert bleiben).
	Now time.Time
	// Bekannte Sicherheitslücken (CVE) aus dem letzten Trivy-Scan.
	// Kritische Funde eskalieren die Ampel auf Rot, hohe auf Gelb.
	CriticalVulns int
	HighVulns     int
	// RaisedVulnPackages: Pakete, deren Funde durch die Gewichtung eine Stufe
	// höher zählen als ihre Roh-Schwere (exponierte oder hoch gewichtete
	// Dienste). Rein erklärend - die Zahlen oben enthalten sie bereits.
	RaisedVulnPackages []string
	// Anzahl genutzter Docker-Images, für die die Registry einen neueren
	// Digest meldet (analog zu überfälligen Paket-Updates → Gelb).
	OutdatedContainerImages int
	// UnfixableVulns: bekannte kritische/hohe Lücken OHNE verfügbaren Fix
	// (effektive Schwere). Reiner Info-Hinweis - sie färben die Ampel
	// nicht, weil es keine mögliche Handlung gibt (R2-056). Verschwiegen
	// werden sie trotzdem nicht.
	UnfixableVulns int
	// TotalVulns: alle BEHEBBAREN bekannten CVEs des Servers (jede Schwere,
	// OS und Docker, ungewichtet). 0 ist Voraussetzung für „Sehr gut" -
	// Unbehebbares zählt nicht dagegen (R2-056): die Bestnote heißt „nichts
	// mehr zu tun", nicht „der Hersteller hat keine offenen Baustellen".
	TotalVulns int
	// InventoryMissing meldet, dass für diesen Server kein einziges Paket
	// erfasst ist - die Bestandsaufnahme ist also nie gelungen. Das macht ihn
	// unbewertbar, nicht makellos: ohne Pakete gibt es definitionsgemäß keine
	// überfälligen Updates und keine CVEs, weshalb genau die Server, die LCM
	// nicht verwalten kann, als die saubersten erschienen (BUG-020).
	//
	// Bewusst als Negativ-Flag: der Nullwert (false) bedeutet "unauffällig",
	// sodass ein Aufrufer, der das Feld nicht kennt, keinen Fehlalarm auslöst.
	InventoryMissing bool
	// DeepScanWarnings: Anzahl der Deep-Scan-Befunde mit Schwere warning/critical.
	// >0 eskaliert die Ampel auf Gelb (Lynis-Empfehlungen zählen bewusst NICHT,
	// damit nicht „alles gelb" wird). Der Kernel-Reboot fließt über RebootRequired.
	DeepScanWarnings int
	// CVEScanError ist der gespeicherte Fehler des letzten CVE-Scans. Er lag
	// bisher in der Datenbank, wirkte sich aber weder auf Ampel noch auf
	// Job-Status aus - ein Server, dessen Sicherheitsbewertung noch nie
	// gelaufen ist, war von einem tatsächlich sauberen nicht zu
	// unterscheiden (BUG-021).
	CVEScanError string
	// CVEDB ist der Stand der zentralen Schwachstellen-Datenbank. Sie gilt für
	// ALLE Server gleichermaßen (der Scan läuft zentral auf dem LCM-Host), ist
	// also kein Merkmal dieses Servers - wohl aber die Grundlage seiner
	// CVE-Bewertung. Eine überalterte Datenbank erzeugt deshalb einen reinen
	// HINWEIS und färbt die Ampel bewusst NICHT: Es ist ein Problem des
	// LCM-Hosts, und alle Server deswegen einzufärben würde eine einzige
	// Ursache vielfach als Server-Problem ausgeben.
	CVEDB CVEDBStatus
}

// OfflineAfterFailedChecks: ab so vielen aufeinanderfolgenden fehlgeschlagenen
// Kontakten gilt ein Server als offline. Zwei, weil ein einzelner Fehlschlag
// im Betrieb Alltag ist und noch nichts beweist - beim zweiten in Folge ist es
// kein Ausrutscher mehr.
const OfflineAfterFailedChecks = 2

// IsOffline meldet, ob der Server als offline zu kennzeichnen ist.
//
// Bewusst unabhängig von der Ampel: Bisher war das Offline-Kennzeichen an die
// Einstellung „Nichterreichbarkeit unkritisch" gekoppelt und erschien NUR bei
// Servern, die deswegen nicht rot wurden. Ein ganz normal ausgefallener Server
// bekam es nie - dabei ist er der Fall, für den die Kennzeichnung gedacht ist.
// Ob die Nichterreichbarkeit toleriert wird, ist eine getrennte Frage: Sie
// steuert die FARBE, nicht die Tatsache.
func (s *Server) IsOffline() bool {
	return !s.Reachable && s.FailedChecks >= OfflineAfterFailedChecks
}

// DiskWarningPercent: ab dieser Belegung gilt der Speicher als knapp.
const DiskWarningPercent = 85

// Kulanzfrist (Tage) für „Nichterreichbarkeit unkritisch": Standard sowie die
// erlaubten Grenzen der pro-Server-Einstellung.
const (
	DefaultUnreachableGraceDays = 28
	UnreachableGraceDaysMin     = 1
	UnreachableGraceDaysMax     = 365
)

// ClampUnreachableGraceDays begrenzt die Kulanzfrist auf den erlaubten Bereich;
// 0/negativ (unbelegt) wird auf den Standard gehoben.
func ClampUnreachableGraceDays(days int) int {
	if days <= 0 {
		return DefaultUnreachableGraceDays
	}
	if days < UnreachableGraceDaysMin {
		return UnreachableGraceDaysMin
	}
	if days > UnreachableGraceDaysMax {
		return UnreachableGraceDaysMax
	}
	return days
}

// unreachableGraceExpired meldet, ob der Server so lange ununterbrochen nicht
// erreichbar ist, dass die Kulanzfrist abgelaufen ist. Bezug ist der letzte
// erfolgreiche Kontakt (LastSeenAt), ersatzweise das Anlagedatum. now.IsZero()
// ⇒ noch nicht abgelaufen (keine Zeitbasis).
func (s *Server) unreachableGraceExpired(now time.Time) bool {
	if now.IsZero() {
		return false
	}
	days := s.UnreachableGraceDays
	if days <= 0 {
		days = DefaultUnreachableGraceDays
	}
	ref := s.LastSeenAt
	if ref == nil || ref.IsZero() {
		ref = &s.CreatedAt
	}
	if ref.IsZero() {
		return false
	}
	return now.Sub(*ref) >= time.Duration(days)*24*time.Hour
}

// TrafficLight bewertet den Server nach dem strikten Ampelsystem:
//
//	🔴 Rot   - keine Verbindung möglich (offline, Auth-/Host-Key-Fehler)
//	🟡 Gelb  - erreichbar, aber Handlungsbedarf (Updates, Disk, Job-Fehler)
//	🟢 Grün  - erreichbar, alles aktuell, letzte Jobs erfolgreich
func (s *Server) TrafficLight(in TrafficLightInput) (string, []StatusInsight) {
	// Nicht erreichbar → normalerweise sofort rot. Ist „Nichterreichbarkeit
	// unkritisch" für diesen Server gesetzt und die Kulanzfrist noch nicht
	// abgelaufen, fällt der Server hier durch und behält den aus den zuletzt
	// erfassten Daten berechneten Status (das Frontend graut ihn aus).
	if !s.Reachable && (!s.UnreachableUncritical || s.unreachableGraceExpired(in.Now)) {
		const base = "Keine Verbindung zum Server möglich"
		if s.LastError != "" {
			return ServerStatusRed, []StatusInsight{insight("critical", "unreachableDetail",
				base+": "+s.LastError, map[string]string{"error": s.LastError})}
		}
		return ServerStatusRed, []StatusInsight{insight("critical", "unreachable", base, nil)}
	}

	// Ohne Paketbestand ist der Server nicht bewertbar - und darf deshalb
	// niemals grün sein. Alle folgenden Kriterien (überfällige Updates, CVEs,
	// EOL) leiten sich aus den erfassten Paketen ab: ohne sie melden sie
	// zwangsläufig "nichts gefunden", was wie ein makelloses System aussieht.
	// Genau dadurch standen die Systeme, die LCM gar nicht verwalten kann, in
	// der Risiko-Übersicht über allen real gepflegten Servern (BUG-020).
	if in.InventoryMissing {
		return ServerStatusRed, []StatusInsight{insight("critical", "inventoryMissing",
			"Bestandsaufnahme fehlgeschlagen - keine Pakete erfasst. "+
				"Updates, Sicherheitslücken und Support-Ende lassen sich für diesen Server nicht bewerten.", nil)}
	}

	// Zwei getrennte Sammlungen, weil sie unterschiedlich wirken:
	//
	//   insights - Befunde mit Handlungsbedarf (warning/critical). Schon einer
	//              davon macht den Server gelb.
	//   infos    - reine Hinweise. Sie erklären etwas, ändern die Bewertung
	//              aber NICHT und werden jeder Stufe beigelegt.
	//
	// Vorher gab es nur die erste Sammlung, und die Färbung hing an ihrer
	// bloßen Länge - ein Hinweis ohne Handlungsbedarf war damit gar nicht
	// ausdrückbar, obwohl StatusInsight die Stufe "info" kennt.
	var insights []StatusInsight
	var infos []StatusInsight

	// Stand der Schwachstellen-Datenbank: Trivy lädt sie beim Scan selbst
	// nach, aber nur mit Netzzugang. Scheitert das, scannt Trivy mit der
	// alten Datenbank weiter - das Ergebnis ist dann kein Fehler, sondern
	// „nichts gefunden". Der Hinweis macht sichtbar, worauf die CVE-Zahlen
	// dieses Servers tatsächlich beruhen.
	if in.CVEDB.IsStale() {
		ageKey, ageParams := in.CVEDB.AgeKey()
		infos = append(infos, insight("info", ageKey,
			"Die Schwachstellen-Datenbank des CVE-Scanners ist vom Stand "+
				in.CVEDB.AgeDescription()+" - die CVE-Bewertung beruht auf diesem Stand.", ageParams))
	}

	// Konnte die Sicherheitsbewertung nicht laufen, ist "keine Lücken
	// gefunden" keine Aussage - das muss sichtbar sein statt als Entwarnung
	// durchzugehen (BUG-021).
	if in.CVEScanError != "" {
		insights = append(insights, insight("warning", "cveScanError",
			"CVE-Bewertung nicht möglich: "+in.CVEScanError,
			map[string]string{"error": in.CVEScanError}))
	}
	// Kritische Sicherheitslücken zuerst - sie eskalieren die Ampel auf Rot,
	// auch wenn der Server erreichbar ist.
	if in.CriticalVulns > 0 {
		insights = append(insights, insight("critical", "criticalVulns",
			formatCount(in.CriticalVulns, "kritische Sicherheitslücke (CVE)", "kritische Sicherheitslücken (CVE)"),
			countParams(in.CriticalVulns)))
	}
	if in.HighVulns > 0 {
		insights = append(insights, insight("warning", "highVulns",
			formatCount(in.HighVulns, "hohe Sicherheitslücke (CVE)", "hohe Sicherheitslücken (CVE)"),
			countParams(in.HighVulns)))
	}
	// Warum die Zahlen oben von der Sicherheitsübersicht abweichen können:
	// Funde exponierter oder hoch gewichteter Dienste zählen hier eine Stufe
	// höher, während die Liste bewusst die unveränderte Roh-Schwere zeigt.
	// Ohne diesen Satz sucht man dort vergeblich nach „hohen" Funden und hält
	// die Bewertung für hängengeblieben.
	if len(in.RaisedVulnPackages) > 0 && (in.CriticalVulns > 0 || in.HighVulns > 0) {
		packages := strings.Join(in.RaisedVulnPackages, ", ")
		infos = append(infos, insight("info", "raisedVulns",
			"Höher gewichtet, weil exponiert oder hoch eingestuft: "+packages+
				". Unter Sicherheit stehen diese Funde mit ihrer ursprünglichen, niedrigeren Schwere.",
			map[string]string{"packages": packages}))
	}
	// Bekannte ernste Lücken OHNE verfügbaren Fix: reiner Hinweis (R2-056).
	// Sie eskalieren nicht - es gibt nichts zu tun, bis der Hersteller
	// liefert - aber sie bleiben sichtbar, statt still wegzufallen.
	if in.UnfixableVulns > 0 {
		infos = append(infos, insight("info", "unfixableVulns",
			formatCount(in.UnfixableVulns,
				"bekannte kritische/hohe Sicherheitslücke ohne verfügbaren Fix - zählt nicht in die Bewertung",
				"bekannte kritische/hohe Sicherheitslücken ohne verfügbaren Fix - zählen nicht in die Bewertung"),
			countParams(in.UnfixableVulns)))
	}
	if in.OutdatedPackages > 0 {
		insights = append(insights, insight("warning", "outdatedPackages",
			formatCount(in.OutdatedPackages, "überfälliges Paket-Update", "überfällige Paket-Updates"),
			countParams(in.OutdatedPackages)))
	}
	if in.OutdatedContainerImages > 0 {
		insights = append(insights, insight("warning", "outdatedImages",
			formatCount(in.OutdatedContainerImages, "Container-Image mit verfügbarem Update", "Container-Images mit verfügbaren Updates"),
			countParams(in.OutdatedContainerImages)))
	}
	if p := s.DiskUsagePercent(); p >= DiskWarningPercent {
		insights = append(insights, insight("warning", "diskLow",
			"Festplattenspeicher wird knapp ("+itoa(p)+"% belegt)",
			map[string]string{"percent": itoa(p)}))
	}
	// Uhrenversatz: eine falsch gehende Uhr verdirbt TLS-Prüfungen, die
	// Reihenfolge in Protokollen über mehrere Server hinweg, zeitbasierte
	// Einmalpasswörter und signierte Paket-Metadaten - ohne dass im Betrieb
	// etwas darauf hindeutet. Deshalb Warnung, nicht bloß Hinweis.
	inContainer := IsContainerVirt(s.Virtualization)
	if off := s.ClockOffsetSeconds; off >= ClockOffsetWarnSeconds || off <= -ClockOffsetWarnSeconds {
		dir, key, secs := "vor", "clockAhead", off
		if off < 0 {
			dir, key, secs = "nach", "clockBehind", -off
		}
		msg := "Uhr geht " + itoa(secs) + " Sekunden " + dir + " - Zeitabgleich prüfen"
		if inContainer {
			// Der Versatz bleibt eine Warnung: ein falsch gehender Host reißt
			// ALLE seine Container mit, und im Container fällt es genauso auf
			// die Füße. Nur der Ort der Behebung ist ein anderer - das gehört
			// in die Meldung, sonst sucht man an der falschen Stelle.
			key += "Container"
			msg = "Uhr geht " + itoa(secs) + " Sekunden " + dir + " - sie kommt vom Virtualisierungs-Host, " +
				"der Zeitabgleich ist dort zu richten"
		}
		insights = append(insights, insight("warning", key, msg, map[string]string{"seconds": itoa(secs)}))
	} else if inContainer {
		// Kein Zeitdienst-Hinweis für Container: dort ist keiner vorgesehen,
		// und ihn anzumahnen wäre eine Aufforderung zu etwas Unmöglichem.
	} else if s.TimeCheckedAt != nil && s.NTPService == "" {
		// Kein Zeitdienst: die Uhr stimmt gerade, hat aber nichts, was sie
		// hält. Reiner Hinweis - noch ist nichts kaputt.
		infos = append(infos, insight("info", "noNtp",
			"kein Zeitdienst aktiv (NTP) - die Uhr läuft ungeregelt", nil))
	} else if s.TimeCheckedAt != nil && !s.NTPSynchronized {
		infos = append(infos, insight("info", "ntpNotSynced",
			"Zeitdienst "+s.NTPService+" läuft, meldet die Uhr aber nicht als synchronisiert",
			map[string]string{"service": s.NTPService}))
	}
	// Veralteter LCM-Helper: im eingeschränkten Modus laufen ALLE
	// privilegierten Aktionen über ihn. Er wird beim Einschränken geschrieben
	// und danach nicht mehr erneuert - ein LCM-Update bringt also Korrekturen
	// mit, die auf diesem Server nicht ankommen. Das gehört sichtbar gemacht,
	// sonst hält man den Server für gepflegt.
	if s.HelperOutdated() {
		insights = append(insights, insight("warning", "helperOutdated",
			"Der LCM-Helper auf diesem Server ist veraltet - im eingeschränkten Modus laufen "+
				"alle privilegierten Aktionen über ihn, Korrekturen aus neueren LCM-Fassungen wirken "+
				"hier nicht. Über „Neu verbinden“ (Admin-Login) wird er erneuert.", nil))
	}
	// Das System fordert selbst einen Neustart an (z. B. nach Kernel-Update).
	if s.RebootRequired {
		insights = append(insights, insight("warning", "rebootRequired",
			"Neustart erforderlich, um installierte Updates vollständig zu aktivieren", nil))
	}
	// Deep-Scan-Warnungen (Härtungs-/Fehlkonfigurations-Warnungen und Dienste,
	// die noch alte Bibliotheken nutzen). Nur echte Warnungen/kritische Befunde
	// zählen - Lynis-Empfehlungen bleiben rein informativ.
	if in.DeepScanWarnings > 0 {
		insights = append(insights, insight("warning", "deepScanWarnings",
			formatCount(in.DeepScanWarnings, "Deep-Scan-Warnung (Härtung/Konfiguration)", "Deep-Scan-Warnungen (Härtung/Konfiguration)"),
			countParams(in.DeepScanWarnings)))
	}
	if in.LastJobFailed {
		if in.LastJobName != "" {
			insights = append(insights, insight("warning", "lastJobFailedNamed",
				"Letzter Job fehlgeschlagen: "+in.LastJobName, map[string]string{"job": in.LastJobName}))
		} else {
			insights = append(insights, insight("warning", "lastJobFailed", "Letzter Job fehlgeschlagen", nil))
		}
	}
	// Betriebssystem außerhalb des Herstellersupports (EOL) oder in weniger als
	// einem Monat davor - Sicherheitsrisiko. Beides eskaliert die Ampel auf Rot.
	osCritical := false
	if os := OSSupportStatus(s.OSID, s.OSVersionID, s.OSName, in.Now); os.Known && os.Severity != "" {
		insights = append(insights, insight(os.Severity, os.SummaryKey, os.Summary, os.SummaryParams))
		if os.Severity == "critical" {
			osCritical = true
		}
	}

	// Red Hat ohne gültige Registrierung: Ohne sie liefert das System keine
	// regulären Paketquellen - und damit keine Updates. Der Befund ist hier,
	// weil die Zahl darüber ("0 überfällige Updates") sonst als Entwarnung
	// gelesen wird, obwohl sie nur bedeutet, dass niemand nachschauen konnte.
	switch s.RHSMStatus {
	case RHSMUnregistered:
		insights = append(insights, insight("warning", "rhsmUnregistered",
			"Nicht bei Red Hat registriert - ohne Subscription bekommt dieses System "+
				"keine regulären Paketquellen. Eine leere Update-Liste ist hier keine Entwarnung.", nil))
	case RHSMInvalid:
		insights = append(insights, insight("warning", "rhsmInvalid",
			"Red-Hat-Registrierung ohne ausreichende Berechtigung - die Paketquellen "+
				"können trotz Registrierung leer bleiben.", nil))
	}

	// MikroTik RouterOS: der Router meldet selbst, ob eine neuere Version
	// seines Kanals verfügbar ist. Da LCM hier weder Pakete noch CVEs bewertet,
	// ist die Versions-Aktualität das zentrale Kriterium - ein verfügbares
	// Update eskaliert auf Gelb.
	if s.IsRouterOS() && s.RouterOSUpdateAvailable {
		insights = append(insights, versionUpdateInsight(
			"routerOsUpdate", "Neuere RouterOS-Version verfügbar", s.RouterOSLatestVersion))
	}

	// Synology DSM: dieselbe Logik wie bei RouterOS - ohne Paket-/CVE-Sicht
	// ist die Aktualität von DSM das zentrale Kriterium. Dazu kommen die
	// Befunde des DSM-eigenen Security Advisors, die LCM übernimmt statt sie
	// nachzubauen.
	if s.IsSynologyDSM() {
		if s.DSMUpdateAvailable {
			insights = append(insights, versionUpdateInsight(
				"dsmUpdate", "Neuere DSM-Version verfügbar", s.DSMLatestVersion))
		}
		if s.DSMSecurityRisks > 0 {
			insights = append(insights, insight("warning", "dsmSecurityRisks",
				formatCount(s.DSMSecurityRisks,
					"Befund des DSM-Security-Advisors", "Befunde des DSM-Security-Advisors"),
				countParams(s.DSMSecurityRisks)))
		}
	}

	// Kritische CVEs oder ein (bald) nicht mehr unterstütztes Betriebssystem
	// machen den Server rot (stärkstes Signal, gleichrangig mit „nicht
	// erreichbar").
	if in.CriticalVulns > 0 || osCritical {
		return ServerStatusRed, withInfos(insights, infos)
	}
	// Nur Befunde mit Handlungsbedarf färben gelb - reine Hinweise nicht.
	if len(insights) > 0 {
		return ServerStatusYellow, withInfos(insights, infos)
	}
	// „Sehr gut": makellos - keine einzige bekannte CVE (auch keine
	// niedrigen), SSH gehärtet und Firewall aktiv. Proxmox bringt seine
	// eigene Firewall mit (LCM sperrt ufw dort) und zählt als abgedeckt.
	//
	// Reine API-Geräte (RouterOS, Synology DSM) verwaltet LCM nur überwachend:
	// Härtung und Firewall laufen dort gar nicht über LCM. Sie als „fehlende
	// Kriterien" aufzuführen wäre eine Aufforderung zu etwas, das es hier
	// nicht gibt - wird diese Stelle erreicht, gab es keine Insights (also
	// auch kein offenes Update), und ein aktuelles, erreichbares Gerät ist
	// die Bestnote.
	if s.IsAPIDevice() {
		return ServerStatusExcellent, infos
	}
	if in.TotalVulns == 0 && s.SSHHardened && (s.FirewallActive || s.IsProxmox()) {
		return ServerStatusExcellent, infos
	}
	// „OK", aber nicht „Sehr gut": die fehlenden Kriterien als Info-Befunde
	// mitgeben - das UI zeigt sie im Status-Popover, damit klar ist, was für
	// die Bestnote noch fehlt.
	var missing []StatusInsight
	if in.TotalVulns > 0 {
		missing = append(missing, insight("info", "fixableVulns",
			formatCount(in.TotalVulns, "behebbare Sicherheitslücke (CVE) - „Sehr gut“ erfordert null", "behebbare Sicherheitslücken (CVE) - „Sehr gut“ erfordert null"),
			countParams(in.TotalVulns)))
	}
	if !s.SSHHardened {
		missing = append(missing, insight("info", "sshNotHardened",
			"SSH ist nicht gehärtet (Härtung unter SSH-Sicherheit aktivierbar)", nil))
	}
	if !s.FirewallActive && !s.IsProxmox() {
		if s.FirewallTool != "" {
			missing = append(missing, insight("info", "firewallInactiveTool",
				"Firewall ("+s.FirewallTool+") ist nicht aktiv", map[string]string{"tool": s.FirewallTool}))
		} else {
			missing = append(missing, insight("info", "firewallInactive", "Firewall ist nicht aktiv", nil))
		}
	}
	return ServerStatusGreen, withInfos(infos, missing)
}

// versionUpdateInsight baut den „neuere Version verfügbar"-Befund für die
// reinen API-Geräte (RouterOS, DSM) - mit Versionsnummer, wenn das Gerät sie
// mitgeliefert hat.
func versionUpdateInsight(key, base, latest string) StatusInsight {
	const tail = " - Update empfohlen"
	if latest == "" {
		return insight("warning", key, base+tail, nil)
	}
	return insight("warning", key+"Version", base+" ("+latest+")"+tail, map[string]string{"version": latest})
}

// withInfos hängt reine Hinweise an die Befundliste an, ohne die
// Eingabe-Slices zu teilen (append darf hier nichts überschreiben, weil beide
// Listen im Aufrufer noch verwendet werden können).
func withInfos(insights, infos []StatusInsight) []StatusInsight {
	if len(infos) == 0 {
		return insights
	}
	out := make([]StatusInsight, 0, len(insights)+len(infos))
	out = append(out, insights...)
	return append(out, infos...)
}

// formatCount: "1 überfälliges Paket-Update" / "3 überfällige Paket-Updates".
func formatCount(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return itoa(n) + " " + plural
}

// itoa ohne strconv-Import in der Domain-Schicht klein zu halten wäre
// unnötige Sparsamkeit - aber als lokale Hilfe lesbarer an einem Ort.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
