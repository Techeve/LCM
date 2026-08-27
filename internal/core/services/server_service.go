package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

var (
	ErrServerNameTaken     = errors.New("ein server mit diesem namen existiert bereits")
	ErrServerHostTaken     = errors.New("dieser server ist bereits angelegt (Host/IP schon vorhanden)")
	ErrFingerprintMismatch = errors.New("der host-key-fingerprint weicht vom bestätigten wert ab")
	// ErrLoopbackInContainer: LCM läuft im Container; localhost zeigt dort auf
	// den Container selbst. Der Docker-Host wird über seine im Netz
	// erreichbare Adresse aufgenommen, wie jeder andere Server auch.
	ErrLoopbackInContainer = errors.New("LCM läuft in einem Container - localhost wäre der Container selbst; den Docker-Host bitte über seine Netzwerk-Adresse aufnehmen")
	// ErrProxmoxRestricted: auf erkannten Proxmox-Systemen sperrt LCM
	// Aktionen, die mit der Proxmox-eigenen Verwaltung kollidieren würden
	// (Repositories hinzufügen, ufw-Firewall, Linux-Benutzer-Sync).
	ErrProxmoxRestricted = errors.New("auf proxmox-systemen gesperrt - proxmox verwaltet repositories, firewall und benutzer selbst")

	// ErrFirewallToolMissing: das für die Distribution vorgesehene
	// Firewall-Werkzeug (ufw/firewalld/nftables) fehlt auf dem Zielsystem und
	// konnte auch nicht installiert werden. Ohne Werkzeug findet keine
	// Absicherung statt - das wird als Fehler gemeldet, damit Gruppenregeln
	// den Fehlschlag sichtbar machen (BUG-024).
	ErrFirewallToolMissing = errors.New("das vorgesehene Firewall-Werkzeug (ufw, firewalld oder nftables - je nach Distribution) ist auf diesem System nicht installiert")
	// ErrRestrictedSudo: Der Server wurde mit einem eingeschränkten
	// Management-Benutzer angelegt (sudoers-Whitelist + LCM-Helper). Die
	// Kernfunktionen (Updates, Speicher, CVE-Scan, Repositories, apt-Cache,
	// Firewall, SSH-Konfiguration/-Port, Benutzer-Sync) laufen weiter;
	// gesperrt bleiben nur Aktionen mit freier Root-Shell (eigene Skripte,
	// Custom-Aktionen) und der Neustart.
	ErrRestrictedSudo = errors.New("im eingeschränkten Modus nicht verfügbar - diese Aktion braucht eine Root-Shell (frei definierte Kommandos/Neustart), die der Management-Benutzer dieses Servers nicht mehr hat")
	// ErrUserSyncDisabled: Für diesen Server ist der Linux-Benutzer-Sync in
	// den Server-Einstellungen deaktiviert.
	ErrUserSyncDisabled = errors.New("der linux-benutzer-sync ist für diesen server deaktiviert")

	// ErrRouterOSUnsupported: MikroTik-RouterOS-Geräte haben keine
	// Paketverwaltung und keine POSIX-Shell - LCM überwacht dort nur die
	// Versions-Aktualität. Firewall-Verwaltung, CVE-Scan, Repositories,
	// Benutzer-Sync und SSH-Härtung sind nicht möglich.
	ErrRouterOSUnsupported = errors.New("für MikroTik RouterOS nicht verfügbar - LCM überwacht dieses Gerät nur (keine Paketverwaltung/Shell für Firewall, CVE-Scan, Repos oder Benutzer)")

	// ErrScannerUnavailable: Der CVE-Scanner (Trivy) ist auf dem LCM-Host
	// nicht installiert - ohne ihn gibt es weder Scan noch Datenbank.
	ErrScannerUnavailable = errors.New("der CVE-Scanner (Trivy) ist auf dem LCM-Host nicht verfügbar - er lässt sich in der Server-Detailansicht des LCM-Hosts einrichten")
)

// ensureNotRouterOS weist Aktionen ab, die auf RouterOS-Geräten mangels
// Paketverwaltung/POSIX-Shell nicht anwendbar sind.
// ensureNotRouterOS weist Aktionen ab, die eine POSIX-Shell oder eine
// Paketverwaltung voraussetzen. Beides fehlt bei den reinen API-Gerätetypen -
// RouterOS und Synology DSM -, deshalb deckt derselbe Riegel beide ab; die
// Meldung nennt jeweils das konkrete Gerät.
func ensureNotRouterOS(server *domain.Server) error {
	if server.IsRouterOS() {
		return ErrRouterOSUnsupported
	}
	if server.IsSynologyDSM() {
		return ErrDSMUnsupported
	}
	return nil
}

// ensureFullSudo weist Aktionen ab, die über die eingeschränkte sudo-Whitelist
// hinausgehen (Root-Shell-/Dateisystemzugriff). Im Voll-Modus ein No-op.
func ensureFullSudo(server *domain.Server) error {
	if server.RestrictedSudo {
		return ErrRestrictedSudo
	}
	return nil
}

// ServerService verantwortet Onboarding, Scan, Härtung, Key-Rotation und
// das Decommissioning von Servern. Es kapselt die gesamte SSH-Logik und
// die At-Rest-Verschlüsselung der Server-Private-Keys.
type ServerService struct {
	servers  *repositories.ServerRepository
	jobs     *JobService
	audit    *AuditService
	cipher   *crypto.Cipher
	dialer   sshx.Dialer
	recorder *SSHRecorder
	linux    *repositories.LinuxUserRepository // read-only, für die Bereinigung beim Entfernen
	groups   *repositories.GroupRepository     // für die Auto-Zuordnung neuer Server zur System-Gruppe
	scanner  VulnScanner                       // CVE-Scanner (Trivy), optional
	// onboardingKey liefert den entschlüsselten Onboarding-Private-Key (PEM)
	// für den Key-Login-Zweig beim Join/Reconnect. Optional.
	onboardingKey func() (string, error)
	// knownRepos ist der pflegbare Katalog bekannter Paketquellen. Optional -
	// ohne DB-Sicht (schlanke Tests) greift der mitgelieferte Default-Katalog.
	knownRepos *repositories.KnownRepoRepository
	// apps ist der Anwendungskatalog samt der Funde je Server. Optional -
	// ohne ihn entfällt die Erkennung (schlanke Tests).
	apps *repositories.AppRepository
	// aptCacheURL liefert die konfigurierte APT-Cache-URL aus den globalen
	// Einstellungen (leer = Feature aus). Optional.
	aptCacheURL func() (string, error)
	// cveScanEnabled meldet, ob der CVE-Scan global aktiviert ist - steuert die
	// automatische CVE-Neubewertung nach Paket-Updates. Optional; nil = an.
	cveScanEnabled func() bool
	// selfRegisterOff schaltet die automatische Selbstaufnahme des LCM-Hosts
	// dauerhaft ab. Wird aufgerufen, wenn genau dieser Server gelöscht wird -
	// sonst legte ihn das Installationsskript beim nächsten Paket-Update
	// erneut an. Optional; nil = kein Vermerk (z.B. in Tests).
	selfRegisterOff func() error
	// connLimit deckelt die gleichzeitigen SSH-Verbindungen pro Server auf
	// 1 ausführende + 1 lesende - alle Verbindungen laufen durch connect/
	// connectRead dieses Services (auch Executor und Provisioning).
	connLimit *ConnLimiter
	// weightList liefert die CVE-Hochgewichtungs-Liste aus den globalen
	// Einstellungen. Optional; nil = eingebaute Standardliste.
	weightList func() []string
	// dnsTestDomains liefert die zu prüfenden DNS-Test-Domains aus den globalen
	// Einstellungen. Optional; nil = eingebaute Standardliste.
	dnsTestDomains func() []string
	// crowdsecConfig liefert den entschlüsselten CrowdSec-Zugang (LAPI/Console)
	// aus den globalen Einstellungen - für die Aktion „Sicherheit-Tools". Optional.
	crowdsecConfig func() (CrowdSecConfig, error)
	// pins ist der Speicher der Paket-Pins (Schutz vor Autoremove, optionale
	// Versions-Fixierung). Optional - ohne ihn sind die Pin-Aktionen nicht
	// verfügbar und paketbezogene Jobs laufen ohne Pin-Schutz.
	pins *repositories.PackagePinRepository
	// ipAllowlistExpand löst benannte IP-Allowlists (IDs) in ihre konkreten
	// Quell-CIDRs auf - für die Quell-Einschränkung in Firewall-Regeln und die
	// ignoreip/Whitelist der Sicherheits-Tools. Optional; nil = keine Auflösung.
	ipAllowlistExpand func([]uint) ([]string, error)
	// dockerCheckTrigger stößt den zentralen Docker-Check (Registry-Abgleich
	// + Image-CVE-Scan) an - nach Container-/Image-Updates, damit die
	// CVE-Bewertung nicht bis zum nächsten Tageslauf veraltet bleibt.
	dockerCheckTrigger func(actor string)

	// bulk hält den Fortschritt eines laufenden „Alle VMs aktualisieren"-Laufs
	// (Security-Seite). Zero-Wert ist einsatzbereit; es läuft höchstens einer.
	bulk bulkUpdateRunner

	// enableCVEScan/setAptCacheURL richten Trivy bzw. apt-cacher-ng nach der
	// Installation auf dem LCM-Host in den globalen Einstellungen ein. Optional.
	enableCVEScan  func() error
	setAptCacheURL func(url string) error
	// setCrowdSecLapi trägt die auf dem LCM-Host erzeugten CrowdSec-LAPI-
	// Zugangsdaten (URL/Login/Passwort) in die Einstellungen ein, damit
	// verwaltete Server sofort im Remote-Modus enrollen können. Optional.
	setCrowdSecLapi func(url, login, password string) error

	// agents ist der MQTT-AgentHub für Server mit Transport=agent (LCM
	// Remote). Optional; ohne ihn sind Agent-Server nicht erreichbar.
	agents AgentConnector
	// agentCertFP liefert den SHA-256-Fingerprint des aktiven HTTPS-
	// Zertifikats (fürs Cert-Pinning im Enrollment-Token). Optional.
	agentCertFP func() (string, error)
	// containerCheck überschreibt die Betriebsart-Erkennung in Tests;
	// nil = die echte aus runtimeenv (siehe inContainer).
	containerCheck func() bool
}

// WithLcmHostConfig verdrahtet die Selbst-Einrichtung des LCM-Hosts: nach der
// Installation von Trivy bzw. apt-cacher-ng aktiviert LCM den CVE-Scan bzw.
// trägt die lokale APT-Cache-URL ein.
func (s *ServerService) WithLcmHostConfig(enableCVEScan func() error, setAptCacheURL func(url string) error) *ServerService {
	s.enableCVEScan = enableCVEScan
	s.setAptCacheURL = setAptCacheURL
	return s
}

// WithCVERescanEnabled verdrahtet die Abfrage, ob der CVE-Scan global aktiviert
// ist (globale Einstellung). Steuert, ob nach einem Paket-Update die
// CVE-Bewertung automatisch aufgefrischt wird. Optional; ohne ihn gilt „an".
func (s *ServerService) WithCVERescanEnabled(fn func() bool) *ServerService {
	s.cveScanEnabled = fn
	return s
}

// WithSelfRegisterOff verdrahtet den Vermerk, dass sich der LCM-Host nicht
// mehr selbst aufnehmen soll. Wird beim Löschen genau dieses Servers gezogen.
func (s *ServerService) WithSelfRegisterOff(fn func() error) *ServerService {
	s.selfRegisterOff = fn
	return s
}

// disableSelfRegistration setzt den Vermerk, sofern verdrahtet.
func (s *ServerService) disableSelfRegistration() error {
	if s.selfRegisterOff == nil {
		return nil
	}
	return s.selfRegisterOff()
}

// WithAptCacheURL verdrahtet den Zugriff auf die APT-Cache-URL der globalen
// Einstellungen (für die Aktion „APT-Cache verwenden").
func (s *ServerService) WithAptCacheURL(fn func() (string, error)) *ServerService {
	s.aptCacheURL = fn
	return s
}

// WithKnownRepos verdrahtet den pflegbaren Katalog bekannter Paketquellen
// (Einstellungen → Repositories). Optional; ohne ihn gilt der Default-Katalog.
// WithApps verdrahtet den Anwendungskatalog. Ohne ihn läuft die Erkennung
// der nicht paketverwalteten Anwendungen nicht mit.
func (s *ServerService) WithApps(repo *repositories.AppRepository) *ServerService {
	s.apps = repo
	return s
}

func (s *ServerService) WithKnownRepos(repo *repositories.KnownRepoRepository) *ServerService {
	s.knownRepos = repo
	return s
}

// WithOnboardingKey verdrahtet den Zugriff auf den System-Onboarding-Key
// (für den Key-Login beim Join/Reconnect gehärteter Server).
func (s *ServerService) WithOnboardingKey(fn func() (string, error)) *ServerService {
	s.onboardingKey = fn
	return s
}

// WithGroups verdrahtet die Servergruppen-Sicht - damit ein neu gejointer
// Server automatisch der geschützten System-Gruppe zugeordnet wird (deren
// Basis-Schedules dann für ihn gelten). Optional, damit schlanke Tests ohne
// sie auskommen.
func (s *ServerService) WithGroups(groups *repositories.GroupRepository) *ServerService {
	s.groups = groups
	return s
}

// WithLinux verdrahtet die Linux-Benutzer-Sicht (für die Ziel-Bereinigung
// beim Entfernen). Optional, damit schlanke Tests ohne sie auskommen.
func (s *ServerService) WithLinux(linux *repositories.LinuxUserRepository) *ServerService {
	s.linux = linux
	return s
}

func NewServerService(servers *repositories.ServerRepository, jobs *JobService, audit *AuditService, cipher *crypto.Cipher, dialer sshx.Dialer) *ServerService {
	return &ServerService{servers: servers, jobs: jobs, audit: audit, cipher: cipher, dialer: dialer,
		connLimit: NewConnLimiter()}
}

// WithRecorder verdrahtet die SSH-Protokollierung (optional, damit schlanke
// Tests den Service ohne Recorder erzeugen können).
func (s *ServerService) WithRecorder(rec *SSHRecorder) *ServerService {
	s.recorder = rec
	return s
}

// WithScanner verdrahtet den CVE-Scanner (Trivy), damit die Detailansicht die
// Verfügbarkeit melden kann. Optional (graceful degrade).
func (s *ServerService) WithScanner(scanner VulnScanner) *ServerService {
	s.scanner = scanner
	return s
}

// ScannerAvailable meldet, ob der CVE-Scan einsatzbereit ist (Trivy vorhanden).
func (s *ServerService) ScannerAvailable() bool {
	return s.scanner != nil && s.scanner.Available()
}

// KernelInfo liefert die Kernel-Sicht eines Servers: den LAUFENDEN Kernel
// (`uname -r`) und die installierten Kernel-Pakete, mit Markierung, welches
// Paket den laufenden stellt.
//
// Warum zusammengesetzt statt roh: Erst der Abgleich beider Angaben
// beantwortet die betrieblich wichtigen Fragen - laeuft der neueste Kernel,
// gibt es noch eine aeltere Fassung als Rueckfallebene, und fehlt nur noch
// ein Neustart? Einzeln sagt keine der beiden Angaben das.
func (s *ServerService) KernelInfo(scope repositories.AccessScope, id uint) (domain.KernelInfo, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return domain.KernelInfo{}, err
	}
	return domain.BuildKernelInfoFor(
		server.KernelVersion, server.Virtualization,
		domain.ParseKernelPackages(server.InstalledKernels),
		!server.IsAPIDevice(),
	), nil
}

// CVEDBStatus liefert Version und Datenbank-Stand des CVE-Scanners, bereits
// bewertet (frisch/ueberaltert/kritisch).
//
// Warum das eine eigene Angabe braucht: Trivy laedt seine Datenbank beim Scan
// selbst nach - aber nur mit Netzzugang. Scheitert das, scannt Trivy mit der
// alten Datenbank weiter und meldet keinen Fehler. „Keine Sicherheitsluecken
// gefunden" ist dann nicht von „seit Wochen nicht mehr nachgesehen" zu
// unterscheiden. Genau diese Verwechslung schliesst der Stand hier aus.
func (s *ServerService) CVEDBStatus() domain.CVEDBStatus {
	if s.scanner == nil {
		return domain.CVEDBStatus{Available: false, Freshness: domain.CVEDBUnknown}
	}
	st := s.scanner.Info(context.Background())
	st.EvaluateCVEDB(time.Now())
	return st
}

// UpdateCVEDB laedt die Schwachstellen-Datenbank herunter - als Job, damit ein
// Fehlschlag (Proxy, Rate-Limit, kein Netz) samt Ausgabe im Protokoll landet
// statt still zu verpuffen. Der Job haengt an keinem Server: Scanner und
// Datenbank liegen zentral auf dem LCM-Host.
func (s *ServerService) UpdateCVEDB(actor string) (*domain.Job, error) {
	if s.scanner == nil || !s.scanner.Available() {
		return nil, ErrScannerUnavailable
	}
	job, err := s.jobs.Start(nil, nil, domain.RuleTypeScript, "CVE-Datenbank aktualisieren", actor)
	if err != nil {
		return nil, err
	}
	s.audit.Log(actor, "system.cve-db-update", "system", 0, "CVE-Datenbank aktualisieren")
	safego.GoCleanup("job:cve-db-update", jobPanicCleanup(s.jobs, job), func() {
		out, err := s.scanner.UpdateDB(context.Background())
		if err != nil {
			s.jobs.Complete(job, out, nil, err)
			return
		}
		// Der neue Stand gehoert in die Erfolgsmeldung - sonst muesste man
		// raten, ob der Download ueberhaupt etwas Neues gebracht hat.
		st := s.CVEDBStatus()
		if st.UpdatedAt != nil {
			out += "\n\nDatenbank-Stand: " + st.UpdatedAt.Format(time.RFC3339) + " (" + st.AgeDescription() + ")"
		}
		s.jobs.Complete(job, out, ptrInt(0), nil)
	})
	return job, nil
}

// cveRescanEnabled meldet, ob die automatische CVE-Neubewertung nach einem
// Paket-Update laufen soll (globale Einstellung; ohne Verdrahtung: an).
func (s *ServerService) cveRescanEnabled() bool {
	return s.cveScanEnabled == nil || s.cveScanEnabled()
}

// WithCVEWeightList verdrahtet die CVE-Hochgewichtungs-Liste (globale
// Einstellung). Optional; ohne sie gilt die eingebaute Standardliste.
func (s *ServerService) WithCVEWeightList(fn func() []string) *ServerService {
	s.weightList = fn
	return s
}

// cveWeightList liefert die effektive Hochgewichtungs-Liste.
func (s *ServerService) cveWeightList() []string {
	if s.weightList != nil {
		return s.weightList()
	}
	return (&domain.GlobalSettings{}).CVEHighWeightList()
}

// WithDNSTestDomains verdrahtet die gepflegten DNS-Test-Domains (für die
// DNS-Test-Aktion und -Regel).
func (s *ServerService) WithDNSTestDomains(fn func() []string) *ServerService {
	s.dnsTestDomains = fn
	return s
}

// dnsTestDomainList liefert die effektiven DNS-Test-Domains (nil-Closure =
// eingebaute Standardliste).
func (s *ServerService) dnsTestDomainList() []string {
	if s.dnsTestDomains != nil {
		return s.dnsTestDomains()
	}
	return (&domain.GlobalSettings{}).DNSTestDomainList()
}

// WithCrowdSecConfig verdrahtet den entschlüsselten CrowdSec-Zugang (LAPI/Console).
func (s *ServerService) WithCrowdSecConfig(fn func() (CrowdSecConfig, error)) *ServerService {
	s.crowdsecConfig = fn
	return s
}

// WithIPAllowlists verdrahtet die Auflösung benannter IP-Allowlists (IDs →
// konkrete Quell-CIDRs) für Firewall-Regeln und Sicherheits-Tools.
func (s *ServerService) WithIPAllowlists(expand func([]uint) ([]string, error)) *ServerService {
	s.ipAllowlistExpand = expand
	return s
}

// WithDockerCheckTrigger verdrahtet den Anstoß des zentralen Docker-Checks
// (Registry-Abgleich + Image-CVE-Scan) nach Container-/Image-Updates.
func (s *ServerService) WithDockerCheckTrigger(fn func(actor string)) *ServerService {
	s.dockerCheckTrigger = fn
	return s
}

// connectRec verbindet zu einem Server und legt den Protokoll-Decorator um
// die Verbindung - jedes Kommando dieser Aktion landet damit im Protokoll.
func (s *ServerService) connectRec(server *domain.Server, purpose, actor string) (sshx.Conn, error) {
	conn, err := s.connect(server)
	if err != nil {
		return nil, err
	}
	return s.recorder.Record(conn, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: purpose,
		Host: server.Host, User: server.ServiceUser,
	}), nil
}

// connectRecRead wie connectRec, aber über den LESE-Slot - für Abfragen, die
// parallel zu einem laufenden Job erlaubt sind (z.B. Paketversionen).
func (s *ServerService) connectRecRead(server *domain.Server, purpose, actor string) (sshx.Conn, error) {
	conn, err := s.connectRead(server)
	if err != nil {
		return nil, err
	}
	return s.recorder.Record(conn, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: purpose,
		Host: server.Host, User: server.ServiceUser,
	}), nil
}

// ProbeResult ist das Ergebnis des ersten Join-Schritts.
type ProbeResult struct {
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"key_type"`
}

// Probe liest den SSH-Host-Key-Fingerprint eines neuen Servers aus, ohne
// Credentials zu senden. Der Admin bestätigt diesen Fingerprint in der UI,
// bevor im zweiten Schritt (Join) Zugangsdaten übertragen werden - so wird
// ein Man-in-the-Middle beim Trust-On-First-Use verhindert.
func (s *ServerService) Probe(host string, port int) (*ProbeResult, error) {
	fp, keyType, err := s.dialer.Probe(host, port)
	if err != nil {
		return nil, err
	}
	return &ProbeResult{Fingerprint: fp, KeyType: keyType}, nil
}

// Anmeldeverfahren für den initialen Login beim Join/Reconnect.
const (
	AuthMethodPassword = "password" // Passwort-Login (Default)
	AuthMethodKey      = "key"      // System-Onboarding-SSH-Key
)

// ErrNoOnboardingKey: Key-Login angefordert, aber kein System-Onboarding-Key
// verdrahtet/vorhanden.
var ErrNoOnboardingKey = errors.New("kein system-onboarding-ssh-key verfügbar")

// JoinRequest bündelt die Eingaben des Onboarding-Formulars.
type JoinRequest struct {
	Name                 string
	Host                 string
	Port                 int
	LoginUser            string
	LoginPassword        string
	AuthMethod           string // "password" (Default) oder "key"
	ConfirmedFingerprint string // vom Admin in der UI bestätigt
	Actor                string // Username für Audit/Job
	// RestrictedAccess legt den Management-Benutzer mit eingeschränkten
	// sudo-Rechten an (nur Paketverwaltung, Docker, ufw - kein Root-Shell-/
	// Dateisystemzugriff). false = Voll-Root (Default).
	RestrictedAccess bool
}

// dialLogin öffnet die initiale Login-Verbindung - per Passwort oder per
// System-Onboarding-Key, je nach authMethod.
func (s *ServerService) dialLogin(authMethod, host string, port int, user, password, fp string) (sshx.Conn, error) {
	if authMethod == AuthMethodKey {
		if s.onboardingKey == nil {
			return nil, ErrNoOnboardingKey
		}
		priv, err := s.onboardingKey()
		if err != nil {
			return nil, err
		}
		return s.dialer.DialKey(host, port, user, priv, fp)
	}
	return s.dialer.DialPassword(host, port, user, password, fp)
}

// Join führt das vollständige Onboarding durch:
//  1. Fingerprint erneut auslesen und mit dem bestätigten Wert abgleichen
//  2. Mit Login-Credentials verbinden
//  3. Dedizierten Service-User anlegen (sudo-fähig)
//  4. Server-spezifisches, einzigartiges SSH-Keypair erzeugen (Zero Trust)
//  5. Public Key beim Service-User hinterlegen, Key-Login testen
//  6. Private Key AES-GCM-verschlüsselt speichern
//  7. Initialen System-Scan durchführen und persistieren
func (s *ServerService) Join(req JoinRequest) (*domain.Server, error) {
	if req.Port <= 0 {
		req.Port = 22
	}
	// Im Container zeigt localhost auf den Container selbst, nicht auf die
	// Maschine darunter. Ein solcher Eintrag verwaltete also LCMs eigenes
	// Wegwerf-Dateisystem - und bekäme obendrein die LCM-Host-Sonderrolle
	// (IsLcmHost) mit Karten und Aktionen, die dort nichts ausrichten können.
	// Deshalb gar nicht erst anlegen: Der Docker-Host wird wie jeder andere
	// Server über seine erreichbare Adresse aufgenommen.
	if s.inContainer() && domain.IsLoopbackHost(req.Host) {
		return nil, ErrLoopbackInContainer
	}
	if _, err := s.servers.FindByName(req.Name); err == nil {
		return nil, ErrServerNameTaken
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, err
	}
	// Gleiche Host/IP-Port-Kombination wie ein bereits angelegter Server →
	// sofort ablehnen, damit dieselbe Maschine nicht doppelt verwaltet wird
	// (unterschiedliche Ports hinter derselben IP sind erlaubt - NAT/Forwarding).
	if taken, err := s.servers.HostExists(req.Host, req.Port, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrServerHostTaken
	}

	// 1. Fingerprint gegen den bestätigten Wert absichern (MitM-Schutz).
	fp, _, err := s.dialer.Probe(req.Host, req.Port)
	if err != nil {
		return nil, fmt.Errorf("host-key auslesen: %w", err)
	}
	if req.ConfirmedFingerprint != "" && fp != req.ConfirmedFingerprint {
		return nil, ErrFingerprintMismatch
	}

	// 2. Mit Login-Credentials verbinden (Passwort oder System-Key).
	rawConn, err := s.dialLogin(req.AuthMethod, req.Host, req.Port, req.LoginUser, req.LoginPassword, fp)
	if err != nil {
		return nil, fmt.Errorf("login fehlgeschlagen: %w", err)
	}
	// Protokollieren (Server-ID/Job folgen unten per Link, sobald bekannt).
	conn := s.recorder.Record(rawConn, SessionContext{
		Actor: req.Actor, Purpose: "join:" + req.Name, Host: req.Host, User: req.LoginUser,
	})
	defer conn.Close()

	// 3.-7. Service-User provisionieren, einzigartiges Keypair erzeugen,
	// Key-Login verifizieren, Private Key verschlüsseln und System scannen.
	acc, err := s.establishAccess(conn, req.Host, req.Port, fp, req.Name, req.LoginUser, req.LoginPassword, req.Actor, "join:"+req.Name, req.RestrictedAccess)
	if err != nil {
		return nil, err
	}
	defer acc.KeyConn.Close()

	now := time.Now()
	server := &domain.Server{
		Name:               req.Name,
		Host:               req.Host,
		SSHPort:            req.Port,
		ServiceUser:        acc.ServiceUser,
		HostKeyFingerprint: fp,
		PrivateKeyEnc:      acc.PrivateKeyEnc,
		PublicKey:          acc.PublicKey,
		RestrictedSudo:     acc.RestrictedSudo,
		Reachable:          true,
		LastSeenAt:         &now,
	}
	applyScan(server, acc.Scan)
	if err := s.servers.Create(server); err != nil {
		return nil, err
	}
	if err := s.servers.ReplacePackages(server.ID, acc.Scan.Packages); err != nil {
		return nil, err
	}
	if err := s.servers.ReplaceSnapPackages(server.ID, acc.Scan.Snaps); err != nil {
		return nil, err
	}
	if err := s.servers.ReplaceRepositories(server.ID, acc.Scan.Repositories); err != nil {
		return nil, err
	}
	_ = s.servers.ReplaceDiskVolumes(server.ID, acc.Scan.DiskVolumes)
	_ = s.servers.ReplaceServerUsers(server.ID, acc.Scan.Users)
	_ = s.servers.ReplaceServerUserLogins(server.ID, acc.Scan.UserLogins)
	_ = s.servers.ReplaceDockerContainers(server.ID, acc.Scan.DockerContainers)
	_ = s.servers.ReplaceDockerImages(server.ID, acc.Scan.DockerImages)

	// Neuen Server automatisch der geschützten System-Gruppe zuordnen, damit
	// deren Basis-Schedules (Health-Check, System-Sync) sofort greifen.
	s.assignToSystemGroup(server)

	// Onboarding als Job protokollieren (mit vollem Konsolen-Output).
	job := &domain.Job{
		ServerID: &server.ID, Type: "join", Name: "Server-Onboarding: " + req.Name,
		Status: domain.JobStatusSuccess, TriggeredBy: req.Actor,
		StartedAt: &now, FinishedAt: &now,
		Output: acc.Log + "\n### System-Scan\n" + acc.Scan.Output,
	}
	_ = s.jobs.jobs.Create(job)
	// Die Onboarding-Sessions nachträglich Server und Job zuordnen, damit
	// sie unter dem Server und beim Onboarding-Job auffindbar sind.
	s.recorder.Link(conn, server.ID, &job.ID)
	s.recorder.Link(acc.KeyConn, server.ID, &job.ID)
	s.audit.Log(req.Actor, "server.join", "server", server.ID, "Host "+req.Host+", Fingerprint "+fp)
	return server, nil
}

// assignToSystemGroup ordnet einen frisch gejointen Server der geschützten
// System-Gruppe zu (best effort - schlägt es fehl, bleibt der Server dennoch
// voll nutzbar; er ist dann nur nicht in der System-Gruppe).
func (s *ServerService) assignToSystemGroup(server *domain.Server) {
	if s.groups == nil {
		return
	}
	group, err := s.groups.FindByName(domain.SystemGroupName)
	if err != nil {
		slog.Warn("auto-assignment: system group not found", "server", server.Name, "error", err)
		return
	}
	if err := s.groups.AddServer(group, server); err != nil {
		slog.Warn("auto-assignment: server not assigned to system group", "server", server.Name, "error", err)
	}
}

// ReconnectRequest bündelt die Eingaben des Reconnect-Prozesses. Host, Port
// und LoginUser sind optional - leer bedeutet: den bisherigen Wert des
// Servers weiterverwenden (z.B. wenn nur die Credentials geändert wurden).
// ReconnectRequest: leere Werte für Host/Port übernehmen den Bestand des
// Servers; ein leerer LoginUser fällt auf "root" zurück (der bisherige
// Login-Benutzer wird nicht gespeichert).
type ReconnectRequest struct {
	ID                   uint
	Host                 string
	Port                 int
	LoginUser            string
	LoginPassword        string
	AuthMethod           string // "password" (Default) oder "key"
	ConfirmedFingerprint string
	Actor                string
	// RestrictedAccess richtet den Management-Benutzer mit eingeschränkten
	// sudo-Rechten ein (siehe JoinRequest) - beim Reconnect kann so auch der
	// Rechte-Modus eines bestehenden Servers gewechselt werden.
	RestrictedAccess bool
}

// Reconnect stellt die Verbindung zu einem BESTEHENDEN Server neu her -
// für den Fall, dass die Credentials händisch geändert wurden oder der
// Server komplett ausgetauscht wurde, aber dieselbe Position im LCM
// einnimmt. Der Ablauf entspricht dem Onboarding (Fingerprint bestätigen,
// Passwort-Login, Service-User + NEUES Zertifikat, Verifikation, Scan),
// aber statt eines neuen Datensatzes werden die Credentials des bestehenden
// Servers ÜBERSCHRIEBEN. Gruppen-, Benutzer-Zuordnungen, Protokolle und
// Job-Historie bleiben erhalten.
func (s *ServerService) Reconnect(scope repositories.AccessScope, req ReconnectRequest) (*domain.Server, error) {
	server, err := s.servers.FindByID(scope, req.ID)
	if err != nil {
		return nil, err
	}
	if server.IsDemo {
		return nil, errors.New("demo-server können nicht neu verbunden werden")
	}
	if err := ensureSSHTransport(server); err != nil {
		return nil, err
	}
	// Reconnect ist das NEU-Onboarding eines ersetzten/neu aufgesetzten
	// Servers (Admin-Login, neue Credentials) - keine „Wiederverbindung"
	// über den bestehenden Service-User-Key. Auf einem Server, den LCM
	// gerade normal erreicht, richtet er nichts, was nicht schon da ist -
	// die Aktion früh benennen erspart die Verwirrung, warum ein voll
	// verwalteter Server nach einem Admin-Passwort fragt (R2-010).
	// AUSNAHME eingeschränkter Modus: dessen dokumentierter Rückweg in den
	// Voll-Modus ist genau dieser Reconnect - dort bleibt er erlaubt.
	if server.Reachable && !server.RestrictedSudo {
		return nil, errors.New("dieser Server ist verbunden und wird normal verwaltet - Reconnect ist für den Fall gedacht, dass ein Server neu aufgesetzt oder ausgetauscht wurde und LCM ihn mit frischen Zugangsdaten (Admin-Login) neu übernehmen soll; für einen laufenden Server ist keine Aktion nötig")
	}
	// RouterOS hat keinen Service-User/sudoers-Provisionierungspfad - der
	// Reconnect liefe sonst in establishAccess und scheiterte unverständlich.
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}

	// Verbindungsdaten: neue Werte überschreiben, leere behalten den Bestand.
	host, port, loginUser := server.Host, server.SSHPort, req.LoginUser
	if req.Host != "" {
		host = req.Host
	}
	if req.Port > 0 {
		port = req.Port
	}
	if loginUser == "" {
		loginUser = "root"
	}

	// 1. Fingerprint erneut auslesen und (falls bestätigt) abgleichen. Beim
	// Austausch des Servers ändert sich der Host-Key - der Admin bestätigt
	// den neuen Fingerprint im Wizard.
	fp, _, err := s.dialer.Probe(host, port)
	if err != nil {
		return nil, fmt.Errorf("host-key auslesen: %w", err)
	}
	if req.ConfirmedFingerprint != "" && fp != req.ConfirmedFingerprint {
		return nil, ErrFingerprintMismatch
	}

	// 2. Mit den (neuen) Login-Credentials verbinden (Passwort oder System-Key).
	rawConn, err := s.dialLogin(req.AuthMethod, host, port, loginUser, req.LoginPassword, fp)
	if err != nil {
		return nil, fmt.Errorf("login fehlgeschlagen: %w", err)
	}
	conn := s.recorder.Record(rawConn, SessionContext{
		ServerID: server.ID, Actor: req.Actor, Purpose: "reconnect:" + server.Name, Host: host, User: loginUser,
	})
	defer conn.Close()

	// 3.-7. Wie beim Join: Service-User + neues Zertifikat + Scan.
	acc, err := s.establishAccess(conn, host, port, fp, server.Name, loginUser, req.LoginPassword, req.Actor, "reconnect:"+server.Name, req.RestrictedAccess)
	if err != nil {
		return nil, err
	}
	defer acc.KeyConn.Close()

	// Credentials des bestehenden Datensatzes überschreiben.
	now := time.Now()
	server.Host = host
	server.SSHPort = port
	server.ServiceUser = acc.ServiceUser
	server.HostKeyFingerprint = fp
	server.PrivateKeyEnc = acc.PrivateKeyEnc
	server.PublicKey = acc.PublicKey
	server.RestrictedSudo = acc.RestrictedSudo
	server.Reachable = true
	server.LastSeenAt = &now
	server.LastError = ""
	applyScan(server, acc.Scan)
	if err := s.servers.Update(server); err != nil {
		return nil, err
	}
	_ = s.servers.ReplacePackages(server.ID, acc.Scan.Packages)
	_ = s.servers.ReplaceSnapPackages(server.ID, acc.Scan.Snaps)
	_ = s.servers.ReplaceRepositories(server.ID, acc.Scan.Repositories)
	_ = s.servers.ReplaceDiskVolumes(server.ID, acc.Scan.DiskVolumes)
	_ = s.servers.ReplaceServerUsers(server.ID, acc.Scan.Users)
	_ = s.servers.ReplaceServerUserLogins(server.ID, acc.Scan.UserLogins)
	_ = s.servers.ReplaceDockerContainers(server.ID, acc.Scan.DockerContainers)
	_ = s.servers.ReplaceDockerImages(server.ID, acc.Scan.DockerImages)

	job := &domain.Job{
		ServerID: &server.ID, Type: "reconnect", Name: "Reconnect: " + server.Name,
		Status: domain.JobStatusSuccess, TriggeredBy: req.Actor,
		StartedAt: &now, FinishedAt: &now,
		Output: acc.Log + "\n### System-Scan\n" + acc.Scan.Output,
	}
	_ = s.jobs.jobs.Create(job)
	s.recorder.Link(conn, server.ID, &job.ID)
	s.recorder.Link(acc.KeyConn, server.ID, &job.ID)
	s.audit.Log(req.Actor, "server.reconnect", "server", server.ID, "Host "+host+", Fingerprint "+fp)
	return server, nil
}

// accessResult bündelt das Ergebnis von establishAccess.
type accessResult struct {
	ServiceUser    string
	PrivateKeyEnc  string
	PublicKey      string
	RestrictedSudo bool
	Scan           *scanResult
	Log            string
	KeyConn        sshx.Conn // protokollierte Key-Verbindung; der Aufrufer schließt & verknüpft sie
}

// establishAccess (weiter unten) ist der gemeinsame Kern von Join und
// Reconnect: über die bereits passwort-authentifizierte Verbindung wird der
// Management-Benutzer (idempotent) provisioniert, ein NEUES einzigartiges
// Keypair erzeugt und hinterlegt, der Key-Login verifiziert, der Private Key
// AES-GCM-verschlüsselt und ein System-Scan durchgeführt. Die zurückgegebene
// KeyConn ist bereits protokolliert - der Aufrufer verknüpft sie mit
// Server/Job und schließt sie.

// createUserScript legt den Service-User an - idempotent und ohne ein
// bestimmtes Werkzeug vorauszusetzen. shadow-utils (`useradd`) fehlt auf
// BusyBox-Systemen wie Alpine, dort gibt es nur `adduser` mit eigener Syntax;
// fest verdrahtetes useradd ließ den Join dort mit einem nackten "exit-code
// 127" scheitern (BUG-008). Fehlt beides, benennt das Skript das ausdrücklich,
// statt eine Shell-Fehlernummer durchzureichen.
func createUserScript(svcUser string) string {
	return createUserWithShellScript(svcUser, "/bin/bash")
}

// createUserWithShellScript ist die Variante mit frei wählbarer Login-Shell -
// für die provisionierten Linux-Benutzer, deren Shell aus LCM kommt. Fehlt
// die gewünschte Shell auf dem Zielsystem, greift /bin/sh: Alpine/BusyBox hat
// kein /bin/bash, und OpenSSH lehnt den Login zu einem Konto mit nicht
// existierender Shell bereits im Preauth ab - der Key-Login des Service-Users
// scheiterte damit auf jedem Alpine ohne nachinstallierte Bash (R2-003; der
// hier früher nur im Kommentar versprochene Fallback fehlte im Skript).
func createUserWithShellScript(username, shell string) string {
	return fmt.Sprintf(
		"if ! id -u %s >/dev/null 2>&1; then "+
			"USHELL='%s'; [ -x \"$USHELL\" ] || USHELL=/bin/sh; "+
			"if command -v useradd >/dev/null 2>&1; then useradd -m -s \"$USHELL\" %s; "+
			"elif command -v adduser >/dev/null 2>&1; then adduser -D -h /home/%s -s \"$USHELL\" %s; "+
			"else echo 'weder useradd noch adduser vorhanden' >&2; exit 1; fi; fi; "+
			// Home-Rechte normalisieren: BusyBox-adduser legt /home/<user>
			// mit 2755 (drwxr-sr-x) an - jeder lokale Benutzer konnte das
			// Verzeichnis lesen, während useradd-Systeme 700 vergeben. Ein
			// Verwaltungswerkzeug stellt denselben Ausgangszustand her,
			// statt den Standard des jeweiligen Werkzeugs zu erben (R2-046).
			"[ -d /home/%s ] && chmod 700 /home/%s || true",
		username, shell, username, username, username, username, username)
}

// unlockAccountScript hebt die Passwortsperre des Kontos auf.
//
// Ein frisch per useradd/adduser angelegtes Konto hat ein GESPERRTES
// Passwortfeld ("!"). Läuft der sshd des Zielsystems mit `UsePAM no` - so
// ausgeliefert z.B. von openSUSE -, lehnt OpenSSH die Anmeldung an solchen
// Konten ab, AUCH mit gültigem Schlüssel. Der Join scheiterte dort am
// Key-Login, obwohl Schlüssel und Dateirechte korrekt waren (BUG-007, und
// für provisionierte Linux-Benutzer BUG-028).
//
// "*" ersetzt die Sperre durch ein unbrauchbares Passwort-Hash: eine
// Passwort-Anmeldung bleibt unmöglich, die Sperre ist aber weg. Auf
// BusyBox-Systemen ohne usermod setzt `chpasswd -e` denselben Wert -
// BusyBox-adduser hinterließ dort ein LEERES Passwortfeld statt "*"/"!"
// (R2-046): praktisch nicht ausnutzbar (BusyBox-su und sshd lehnen ab),
// aber ein anderer Ausgangszustand als auf jeder anderen Distribution.
// Als letzter Rückfall bleibt `passwd -u`. Schlägt alles fehl, ist das
// kein Abbruchgrund - der anschließende Key-Login-Test entscheidet.
func unlockAccountScript(svcUser string) string {
	return fmt.Sprintf(
		"if command -v usermod >/dev/null 2>&1; then usermod -p '*' %s >/dev/null 2>&1 || true; "+
			"else printf '%%s:*\\n' %s | chpasswd -e >/dev/null 2>&1 || passwd -u %s >/dev/null 2>&1 || true; fi",
		svcUser, svcUser, svcUser)
}

// cleanupProvisioning macht eine angefangene Provisionierung rückgängig.
//
// Scheitert der Join nach diesem Schritt, blieb bisher ein Konto mit
// passwortlosem sudo auf dem Zielsystem zurück - von LCM nicht erfasst, vom
// Administrator nicht erwartet, und bei einem erneuten Versuch stillschweigend
// weiterbenutzt (BUG-009). Best effort: die Schritte laufen mit ";" verkettet,
// damit ein fehlschlagender den nächsten nicht verhindert; das Ergebnis landet
// im Provisionierungs-Mitschnitt.
func cleanupProvisioning(conn sshx.Conn, esc *rootEscalation, svcUser string, log *strings.Builder) {
	if esc == nil {
		return
	}
	script := strings.Join([]string{
		fmt.Sprintf("rm -f /etc/sudoers.d/%s", svcUser),
		fmt.Sprintf("rm -f %s", lcmHelperPath),
		fmt.Sprintf("if command -v userdel >/dev/null 2>&1; then userdel -r %s >/dev/null 2>&1; "+
			"elif command -v deluser >/dev/null 2>&1; then deluser --remove-home %s >/dev/null 2>&1; fi",
			svcUser, svcUser),
	}, "; ")
	cmd, stdin := esc.wrap(script)
	out, _, err := conn.RunStdin(cmd, stdin)
	log.WriteString("### Rücknahme der Provisionierung\n$ " + cmd + "\n" + out + "\n")
	if err != nil {
		log.WriteString("Rücknahme unvollständig: " + err.Error() + "\n")
		slog.Warn("provisioning rollback failed - check service user",
			"service_user", svcUser, "error", err)
	}
}

func (s *ServerService) establishAccess(conn sshx.Conn, host string, port int, fp, name, loginUser, loginPassword, actor, purpose string, restricted bool) (*accessResult, error) {
	var log strings.Builder
	// must führt einen Provisionierungs-Schritt aus. stdin speist bei
	// passwort-pflichtigem sudo das Passwort ein (steht NICHT in der
	// protokollierten Kommandozeile).
	must := func(step, cmd, stdin string) error {
		out, code, err := conn.RunStdin(cmd, stdin)
		log.WriteString("### " + step + "\n$ " + cmd + "\n" + out + "\n")
		if err != nil {
			return fmt.Errorf("%s: %w", step, err)
		}
		if code != 0 {
			return fmt.Errorf("%s: exit-code %d", step, code)
		}
		return nil
	}

	svcUser := domain.DefaultServiceUser
	privPEM, pubLine, err := sshx.GenerateKeyPair("lcm@" + name)
	if err != nil {
		return nil, err
	}
	steps := []string{
		createUserScript(svcUser),
		unlockAccountScript(svcUser),
		fmt.Sprintf("install -d -m 700 -o %s -g %s /home/%s/.ssh", svcUser, svcUser, svcUser),
		fmt.Sprintf("printf '%%s\\n' %s > /home/%s/.ssh/authorized_keys", shellQuote(pubLine), svcUser),
		fmt.Sprintf("chown %s:%s /home/%s/.ssh/authorized_keys", svcUser, svcUser, svcUser),
		fmt.Sprintf("chmod 600 /home/%s/.ssh/authorized_keys", svcUser),
	}
	if restricted {
		// Eingeschränkter Modus: sudoers-Whitelist + Shim-Wrapper (nur
		// Paketverwaltung, Docker, ufw - kein Root-Shell-/Dateisystemzugriff).
		steps = append(steps, restrictedProvisionScript(svcUser)...)
		// Belegen, dass der eingeschränkte Benutzer Helper und Paketverwaltung
		// wirklich erreicht. Hier ohne Rollback: schlägt die Probe fehl, soll
		// das Onboarding sichtbar scheitern, statt einen Server aufzunehmen,
		// auf dem die Kernfunktionen tot sind (R2-019/R2-020).
		steps = append(steps, restrictedSelfTestScript(svcUser))
	} else {
		// Voll-Modus (Default): passwortloses sudo für alles.
		// mkdir -p: /etc/sudoers.d fehlt auf Systemen ohne installiertes sudo -
		// ohne das Verzeichnis scheiterte der Schritt mit einem nackten
		// "exit-code 1", der die Ursache verschwieg (BUG-010).
		steps = append(steps,
			"mkdir -p /etc/sudoers.d",
			fmt.Sprintf("printf '%%s ALL=(ALL) NOPASSWD:ALL\\n' %s > /etc/sudoers.d/%s", svcUser, svcUser),
			fmt.Sprintf("chmod 440 /etc/sudoers.d/%s", svcUser),
		)
	}
	provision := strings.Join(steps, " && ")
	// Automatische Rechte-Erkennung: WIE wird der Login-Benutzer root?
	// Direkt (root), per sudo (mit/ohne Passwort) oder per su (ohne
	// Passwort bzw. mit dem Login-Passwort - z. B. Debian ohne sudo,
	// wenn der Benutzer das root-Passwort kennt).
	esc := detectRootEscalation(conn, loginUser, loginPassword, &log)
	if esc == nil {
		return nil, fmt.Errorf("keine Root-Rechte für die Provisionierung: der Login-Benutzer %q ist nicht root, hat keine sudo-Rechte, kann auch nicht per su zu root wechseln (weder ohne Passwort noch mit dem Login-Passwort) und auch login root ohne Passwort schlug fehl. Trage den Benutzer in sudoers ein, verwende root, einen Benutzer, dessen Passwort auch für root gilt, oder stelle sicher, dass root ohne gesetztes Passwort per login erreichbar ist", loginUser)
	}
	if esc.method != "root" {
		log.WriteString("### Rechte-Erkennung\nProvisionierung läuft per " + esc.method + "\n")
	}
	sudoCmd, sudoStdin := esc.wrap(provision)
	if err := must("service-user provisionieren", sudoCmd, sudoStdin); err != nil {
		cleanupProvisioning(conn, esc, svcUser, &log)
		return nil, provisionSudoError(loginUser, withProvisionLog(err, log.String()))
	}

	rawKeyConn, err := s.dialer.DialKey(host, port, svcUser, privPEM, fp)
	if err != nil {
		cleanupProvisioning(conn, esc, svcUser, &log)
		return nil, fmt.Errorf("zertifikats-login des service-users fehlgeschlagen: %w", err)
	}
	keyConn := s.recorder.Record(rawKeyConn, SessionContext{
		Actor: actor, Purpose: purpose + " (scan)", Host: host, User: svcUser,
	})

	// Ab hier ist der Join nur noch dann sinnvoll, wenn LCM den Server auch
	// wirklich bedienen kann. Beide Prüfungen laufen, BEVOR ein Datensatz
	// entsteht - ein Server, den LCM nicht verwalten kann, darf gar nicht erst
	// als scheinbar gesund in der Übersicht landen.
	failVerification := func(err error) (*accessResult, error) {
		keyConn.Close()
		cleanupProvisioning(conn, esc, svcUser, &log)
		return nil, withProvisionLog(err, log.String())
	}

	// 1. Rechte-Funktionstest als Service-User. Bisher galt der Join als
	// gelungen, sobald der sudoers-Eintrag geschrieben war - auf Proxmox
	// VE/PBS/PDM liegt /etc/sudoers.d/ aber vor, OHNE dass sudo installiert
	// ist. Der Eintrag berechtigte dort zu einem Programm, das es nicht gibt:
	// Join grün, danach scheiterte jede Aktion mit "sudo: command not found"
	// (BUG-019). Im eingeschränkten Modus prüft der Shim-Pfad selbst.
	if !restricted {
		// "id -u" statt "true": das belegt, dass tatsächlich root erreicht wird,
		// nicht bloß, dass sudo startet.
		out, code, err := keyConn.Run("sudo -n id -u")
		log.WriteString("### Rechte-Funktionstest (sudo -n id -u)\n" + out + "\n")
		if err != nil || code != 0 || strings.TrimSpace(firstLine(out)) != "0" {
			detail := strings.TrimSpace(firstLine(out))
			if detail == "" {
				detail = fmt.Sprintf("exit-code %d", code)
			}
			return failVerification(fmt.Errorf(
				"der Service-User %q erreicht auf %s keine Root-Rechte (%s). "+
					"Häufigste Ursache: das Paket sudo ist nicht installiert - Proxmox VE/PBS/PDM "+
					"liefern zwar /etc/sudoers.d/ mit, aber nicht das Programm selbst, sodass der "+
					"geschriebene sudoers-Eintrag ins Leere zeigt. "+
					"Auf dem Zielsystem sudo installieren (z.B. apt-get install sudo) und erneut verbinden",
				svcUser, host, detail))
		}
	}

	scan := scanServerMode(keyConn, svcUser, restricted)

	// 2. Paketverwaltung. Ohne unterstützte Paketverwaltung liefe jede
	// Paketaktion ins Leere: LCM fiel bei Unbekanntem still auf apt-get
	// zurück, der Join meldete Erfolg und der Server blieb dauerhaft ohne
	// Paketbestand - und wirkte dadurch sogar besonders gesund (BUG-012).
	if !PackageManagerSupported(scan.PackageManager) {
		return failVerification(fmt.Errorf(
			"die Paketverwaltung dieses Systems wird nicht unterstützt (%s). "+
				"LCM kann apt, dnf/yum, zypper, pacman und apk bedienen; auf Systemen mit "+
				"anderer oder nicht erkennbarer Paketverwaltung fehlen die Paketkommandos, "+
				"sodass Bestandsaufnahme, Updates und CVE-Bewertung ohne Ergebnis blieben. "+
				"Der Server wurde deshalb nicht aufgenommen",
			PackageManagerLabel(scan.PackageManager)))
	}

	privEnc, err := s.cipher.EncryptString(privPEM)
	if err != nil {
		keyConn.Close()
		return nil, err
	}

	return &accessResult{
		ServiceUser: svcUser, PrivateKeyEnc: privEnc, PublicKey: pubLine,
		RestrictedSudo: restricted,
		Scan:           scan, Log: log.String(), KeyConn: keyConn,
	}, nil
}

// withProvisionLog hängt den Provisionierungs-Mitschnitt an einen Fehler an.
//
// Der Mitschnitt (jedes Kommando mit Ausgabe) wurde bisher nur im Erfolgsfall
// in den Job übernommen und im Fehlerfall verworfen - der Anwender sah dann
// nur "exit-code 1", obwohl LCM die Ursache im Klartext vorliegen hatte
// (BUG-010/011). Genau dann wird er am dringendsten gebraucht.
func withProvisionLog(err error, provisionLog string) error {
	if strings.TrimSpace(provisionLog) == "" {
		return err
	}
	return fmt.Errorf("%w\n\n--- Provisionierungs-Protokoll ---\n%s", err, strings.TrimSpace(provisionLog))
}

// applyScan überträgt das Scan-Ergebnis auf das Server-Struct.
func applyScan(server *domain.Server, scan *scanResult) {
	server.OSName = scan.OSName
	server.OSVersion = scan.OSVersion
	server.OSID = scan.OSID
	server.OSVersionID = scan.OSVersionID
	server.ProxmoxType = scan.ProxmoxType
	server.ProxmoxVersion = scan.ProxmoxVersion
	server.Virtualization = scan.Virtualization
	server.PackageManager = scan.PackageManager
	server.HasSnap = scan.HasSnap
	server.HasACL, server.ACLUsable = scan.HasACL, scan.ACLUsable
	server.RebootRequired = scan.RebootRequired
	server.ListeningPackages = scan.ListeningPackages
	server.HasDocker = scan.HasDocker
	server.HasCompose = scan.HasCompose
	server.Fail2banInstalled = scan.Fail2banInstalled
	server.CrowdSecInstalled = scan.CrowdSecInstalled
	server.SSH2FAEnabled = scan.SSH2FAEnabled
	if scan.LCMSourceIP != "" {
		server.LCMSourceIP = scan.LCMSourceIP
	}
	server.FirewallTool = scan.FirewallTool
	server.ListeningPorts = scan.ListeningPorts
	// Zeit-Zustand gehört ab dem ersten Kontakt dazu - sonst stünde ein frisch
	// aufgenommener Server ohne Zeitzone und ohne Uhrenvergleich da, bis
	// zufällig der erste Voll-Refresh läuft.
	server.Timezone = scan.Timezone
	server.NTPService = scan.NTPService
	server.NTPSynchronized = scan.NTPSynchronized
	server.NTPServers = scan.NTPServers
	server.ClockOffsetSeconds = scan.ClockOffsetSeconds
	now := time.Now()
	server.TimeCheckedAt = &now
	server.KernelVersion = scan.KernelVersion
	server.InstalledKernels = domain.MarshalKernelPackages(scan.Kernels)
	server.CPUModel = scan.CPUModel
	server.CPUCores = scan.CPUCores
	server.MemTotalMB = scan.MemTotalMB
	server.MemUsedMB = scan.MemUsedMB
	server.DiskTotalMB = scan.DiskTotalMB
	server.DiskUsedMB = scan.DiskUsedMB
	server.IPAddresses = scan.IPAddresses
	// RouterOS-spezifische Felder (beim Linux-Scan leer → unverändert).
	server.RouterBoardModel = scan.RouterBoardModel
	server.RouterOSChannel = scan.RouterOSChannel
	server.RouterOSLatestVersion = scan.RouterOSLatestVersion
	server.RouterOSUpdateAvailable = scan.RouterOSUpdateAvailable
}

// wrapSudo baut den privilegierten Aufruf von cmd für den Service-User:
//   - restricted: cmd läuft als der (nicht-root) Service-User mit dem Shim-PATH
//     davor - nur die Whitelist-Binaries gehen über sudo (siehe privilege.go).
//   - full + non-root: `sudo sh -c '<cmd>'` (ganzes Skript als root).
//   - full + root: cmd unverändert.
//
// Im laufenden Betrieb ist loginUser stets der Service-User; privRun(server, …)
// ist die bequeme, server-bewusste Hülle.
func wrapSudo(loginUser string, restricted bool, cmd string) string {
	inner := detachBackgroundFDs(cmd)
	if restricted {
		return restrictedPathPrelude(loginUser) + " sh -c " + shellQuote(inner)
	}
	if loginUser == "root" {
		return "sh -c " + shellQuote(inner)
	}
	return "sudo sh -c " + shellQuote(inner)
}

// detachBackgroundFDs entkoppelt die Ausgabe von cmd über eine temporäre
// Datei: cmd läuft in einer Subshell, deren stdout/stderr in die Datei geht und
// deren stdin geschlossen ist; danach wird die Datei gesammelt ausgegeben.
//
// Grund: Manche Kommandos (v.a. `apt-get update` mit Ubuntu-Hooks wie apt-news/
// ESM) forken Hintergrundprozesse, die die stdout/stderr-Deskriptoren erben. Die
// Go-SSH-Session (session.Run) wartet auf das Schließen dieser Kanal-fds - ein
// solcher Hintergrundprozess hält sie offen und der Aufruf hängt, obwohl das
// eigentliche Kommando längst fertig ist. Über die Datei erben die Kinder den
// Datei-fd (nicht den SSH-Kanal), sodass der Kanal sauber schließt.
// Die Subshell `( … )` fängt ein `exit` innerhalb von cmd ab (sonst würde die
// äußere Shell vor der Ausgabe enden).
func detachBackgroundFDs(cmd string) string {
	return `__lf=$(mktemp); ( ` + cmd + ` ) >"$__lf" 2>&1 </dev/null; __rc=$?; cat "$__lf"; rm -f "$__lf"; exit $__rc`
}

// rootEscalation beschreibt, wie der Login-Benutzer beim Onboarding root
// wird. wrap baut aus einem Provisionierungs-Skript das auszuführende
// Kommando plus den einzuspeisenden stdin (Passwörter laufen IMMER über
// stdin, nie über die protokollierte Kommandozeile).
type rootEscalation struct {
	method string // "root" | "sudo" | "su" | "login"
	wrap   func(cmd string) (command, stdin string)
}

// detectRootEscalation probiert beim Onboarding automatisch die Wege zu
// Root-Rechten, in dieser Reihenfolge:
//
//  1. Login-Benutzer IST root - direkt, keine Probe nötig.
//  2. sudo mit dem Login-Passwort (`sudo -S -p ”`, Passwort via stdin).
//  3. sudo ohne Passwort (`sudo -n`, NOPASSWD-Konfiguration/Key-Login).
//  4. su ohne Passwort (z. B. pam_wheel trust oder leeres root-Passwort).
//  5. su mit dem LOGIN-Passwort - der verbreitete Debian-Fall: kein sudo
//     installiert/konfiguriert, aber root nutzt dasselbe Passwort.
//  6. login ohne Passwort - weder sudo noch su erlaubt (z. B. per
//     pam_wheel/Gruppen-Mitgliedschaft gesperrt), aber das login-Programm
//     (eigene PAM-Policy /etc/pam.d/login, meist ohne Gruppen-Restriktion)
//     lässt root ohne Passwort herein, weil root keines gesetzt hat.
//
// Jede Probe führt ein harmloses `true` aus; entscheidend ist der Exit-Code.
// nil = kein Weg funktioniert (der Aufrufer formuliert die Fehlermeldung).
func detectRootEscalation(conn sshx.Conn, loginUser, password string, log *strings.Builder) *rootEscalation {
	if loginUser == "root" {
		return &rootEscalation{method: "root", wrap: func(cmd string) (string, string) { return cmd, "" }}
	}
	probe := func(step, cmd, stdin string) bool {
		out, code, err := conn.RunStdin(cmd, stdin)
		ok := err == nil && code == 0
		log.WriteString("### Rechte-Erkennung: " + step + "\n$ " + cmd + "\n" + out + "\n")
		return ok
	}
	if password != "" && probe("sudo mit Passwort", "sudo -S -p '' sh -c true", password+"\n") {
		return &rootEscalation{method: "sudo", wrap: func(cmd string) (string, string) {
			return "sudo -S -p '' sh -c " + shellQuote(cmd), password + "\n"
		}}
	}
	if probe("sudo ohne Passwort", "sudo -n sh -c true", "") {
		return &rootEscalation{method: "sudo", wrap: func(cmd string) (string, string) {
			return "sudo -n sh -c " + shellQuote(cmd), ""
		}}
	}
	// su liest das Passwort ohne TTY von stdin (util-linux); ein "\n" deckt
	// den prompt-losen Fall UND ein leeres root-Passwort ab.
	if probe("su ohne Passwort", "su root -c true", "\n") {
		return &rootEscalation{method: "su", wrap: func(cmd string) (string, string) {
			return "su root -c " + shellQuote(cmd), "\n"
		}}
	}
	if password != "" && probe("su mit Login-Passwort", "su root -c true", password+"\n") {
		return &rootEscalation{method: "su", wrap: func(cmd string) (string, string) {
			return "su root -c " + shellQuote(cmd), password + "\n"
		}}
	}
	// login kennt kein "-c <kommando>" wie su - es startet nach erfolgreicher
	// Anmeldung eine Login-Shell für root, die (ohne TTY) das restliche stdin
	// als Skript abarbeitet. Der Aufbau ist deshalb immer gleich: eine leere
	// Zeile für den Passwort-Prompt, danach das eigentliche Kommando, danach
	// ein explizites exit (dessen Code der Root-Shell nach außen durchreicht).
	loginWrap := func(cmd string) (string, string) {
		return "login root", "\n" + cmd + "\nexit\n"
	}
	if probeCmd, probeStdin := loginWrap("true"); probe("login ohne Passwort", probeCmd, probeStdin) {
		return &rootEscalation{method: "login", wrap: loginWrap}
	}
	return nil
}

// provisionSudoError reichert einen Provisionierungsfehler beim Nicht-root-Login
// mit einem Klartext-Hinweis an - häufigste Ursachen: fehlende/ falsche
// sudo-Rechte, falsches Passwort oder `requiretty` in der sudoers-Konfiguration.
func provisionSudoError(loginUser string, err error) error {
	if loginUser == "root" {
		return err
	}
	return fmt.Errorf("%w - Hinweis: der Login-Benutzer %q benötigt Root-Rechte über sudo (mit korrektem Passwort oder passwortlos) oder su (ohne Passwort bzw. mit dem Login-Passwort); prüfe auch, dass in der sudoers-Konfiguration kein 'requiretty' gesetzt ist", err, loginUser)
}

// shellQuote setzt einen String in einfache Anführungszeichen (sicher
// gegen Shell-Interpretation) - eigene Implementierung (Zero-Bloat).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Connect stellt eine authentifizierte Key-Verbindung zu einem bereits
// gejointen Server her (striktes Host-Key-Checking) - von Executor und
// ProvisioningService als connect-Closure genutzt.
func (s *ServerService) Connect(server *domain.Server) (sshx.Conn, error) {
	return s.connect(server)
}

// connect stellt eine AUSFÜHRENDE Verbindung her: belegt den Exec-Slot des
// Servers (max. 1 gleichzeitig; Freigabe über conn.Close()).
func (s *ServerService) connect(server *domain.Server) (sshx.Conn, error) {
	release, err := s.connLimit.AcquireExec(server.ID)
	if err != nil {
		return nil, err
	}
	conn, err := s.dial(server)
	if err != nil {
		release()
		return nil, err
	}
	return &limitedConn{Conn: conn, release: release}, nil
}

// connectRead stellt eine rein LESENDE Verbindung her (eigener Slot, damit
// z.B. eine Versions-Abfrage neben einem laufenden Job möglich bleibt -
// aber nie mehr als eine zusätzliche Verbindung entsteht).
func (s *ServerService) connectRead(server *domain.Server) (sshx.Conn, error) {
	release, err := s.connLimit.AcquireRead(server.ID)
	if err != nil {
		return nil, err
	}
	conn, err := s.dial(server)
	if err != nil {
		release()
		return nil, err
	}
	return &limitedConn{Conn: conn, release: release}, nil
}

// dial stellt eine authentifizierte Key-Verbindung zu einem bereits
// gejointen Server her (striktes Host-Key-Checking) - ohne Slot-Verwaltung.
// Für Agent-Server (LCM Remote) liefert es stattdessen die MQTT-Verbindung
// des Hubs; ConnLimiter und SSHRecorder liegen in beiden Fällen darüber.
func (s *ServerService) dial(server *domain.Server) (sshx.Conn, error) {
	if server.IsDemo {
		return nil, errors.New("demo-server werden nicht per ssh kontaktiert")
	}
	if server.IsAgent() {
		if s.agents == nil {
			return nil, errors.New("agent-transport nicht verfügbar (hub nicht verdrahtet)")
		}
		return s.agents.Conn(server)
	}
	// RouterOS mit Passwort-Authentifizierung: das (verschlüsselte) Login-
	// Passwort wird für jede Verbindung entschlüsselt. RouterOS mit Key-Auth
	// (LoginPasswordEnc leer) fällt auf den gemeinsamen Key-Pfad unten durch.
	if server.IsRouterOS() && server.LoginPasswordEnc != "" {
		pw, err := s.cipher.DecryptString(server.LoginPasswordEnc)
		if err != nil {
			return nil, fmt.Errorf("routeros-passwort entschlüsseln (lcm.key passt nicht zu den gespeicherten credentials - server neu verbinden): %w", err)
		}
		return s.dialer.DialPassword(server.Host, server.SSHPort, server.ServiceUser, pw, server.HostKeyFingerprint)
	}
	privPEM, err := s.cipher.DecryptString(server.PrivateKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("private key entschlüsseln (der master-key/lcm.key passt nicht zu den gespeicherten credentials - "+
			"lcm.key ggf. verloren; server neu verbinden): %w", err)
	}
	return s.dialer.DialKey(server.Host, server.SSHPort, server.ServiceUser, privPEM, server.HostKeyFingerprint)
}

// DecommissionOptions steuert, wie tief ein Server entfernt wird.
type DecommissionOptions struct {
	// PurgeTarget: vor dem lokalen Löschen den Zielserver bereinigen - die
	// von LCM angelegten Linux-Benutzer entfernen und alle von LCM abgelegten
	// Zugänge (Service-User, dessen authorized_keys, sudoers) löschen. Erfordert
	// eine funktionierende Verbindung. Ohne PurgeTarget wird der Server nur aus
	// dem LCM entfernt (für bereits getrennte/ausgemusterte Server).
	PurgeTarget bool
}

// Decommission entfernt einen Server aus dem LCM und räumt lokal ALLE damit
// verbundenen Daten weg (Pakete, Repos, Gruppen-/Benutzer-Zuordnungen, Jobs
// und SSH-Protokolle). Bei PurgeTarget wird zuvor der Zielserver bereinigt:
// die provisionierten Linux-Benutzer und die von LCM hinterlegten Zugänge
// werden dort entfernt. Liefert den Konsolen-Output der Ziel-Bereinigung.
func (s *ServerService) Decommission(scope repositories.AccessScope, id uint, actor string, opts DecommissionOptions) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}

	var output string
	if opts.PurgeTarget && server.IsAgent() {
		// Auf Agent-Servern gibt es keine LCM-Spuren zum Bereinigen (kein
		// Service-User, keine authorized_keys) - der Agent selbst wird auf
		// dem Host per `lcm-agent uninstall` entfernt.
		return "", fmt.Errorf("%w - Ziel-Bereinigung entfällt; den Agent auf dem Server mit `lcm-agent uninstall` entfernen und den Server ohne Bereinigung löschen", ErrAgentTransport)
	}
	if opts.PurgeTarget && !server.IsDemo {
		out, err := s.purgeTarget(server, actor)
		if err != nil {
			// Bereinigung war ausdrücklich gewünscht, aber der Server ist nicht
			// erreichbar - klar melden, statt still nur lokal zu löschen.
			return "", fmt.Errorf("bereinigung fehlgeschlagen (server nicht erreichbar?): %w", err)
		}
		output = out
	}

	if err := s.servers.Delete(id); err != nil {
		return "", err
	}
	// Serverspezifische Paket-Pins mitnehmen - sie hätten ohne ihren Server
	// keine Bedeutung mehr und würden bei einer späteren Neuvergabe derselben
	// ID stillschweigend wieder greifen. Globale Pins bleiben unberührt.
	if s.pins != nil {
		if err := s.pins.DeleteForServer(id); err != nil {
			slog.Warn("paket-pins des entfernten servers nicht aufgeräumt", "server_id", id, "err", err)
		}
	}
	// Wird der LCM-Host selbst entfernt, ist das eine bewusste Entscheidung -
	// sie muss die automatische Selbstaufnahme dauerhaft abschalten. Sonst
	// legt das Installationsskript beim nächsten Paket-Update erneut die
	// Übergabedatei an und der Eintrag wäre wieder da.
	if server.IsLcmHost() {
		if err := s.disableSelfRegistration(); err != nil {
			slog.Warn("self-registration could not be disabled - this host may reappear after a package update",
				"error", err)
		}
	}
	// Eine noch aktive Agent-Session sofort trennen - der Datensatz ist weg,
	// die laufende Verbindung darf ihn nicht überdauern.
	if server.IsAgent() && s.agents != nil {
		s.agents.Disconnect(server.AgentID)
	}
	detail := server.Name
	if opts.PurgeTarget {
		detail += " (mit Ziel-Bereinigung)"
	}
	s.audit.Log(actor, "server.decommission", "server", id, detail)
	return output, nil
}

// purgeTarget entfernt auf dem Zielserver alle von LCM angelegten Spuren:
// die provisionierten Linux-Benutzer (Account + sudoers) sowie die Zugänge
// des Service-Users (authorized_keys zuerst - damit die Credentials selbst
// dann garantiert weg sind, falls userdel den eingeloggten User nicht
// entfernen kann).
func (s *ServerService) purgeTarget(server *domain.Server, actor string) (string, error) {
	conn, err := s.connectRec(server, "decommission (bereinigung)", actor)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	var steps []string
	// 1. Von LCM provisionierte Linux-Benutzer entfernen. Distributions-
	// bewusst (BusyBox hat kein userdel - R2-040); der Abschied bleibt
	// best effort, aber ein zurückbleibendes Konto wird im Protokoll
	// benannt statt still verschwiegen.
	if s.linux != nil {
		if users, err := s.linux.AssignedForServer(server.ID); err == nil {
			for _, u := range users {
				steps = append(steps,
					fmt.Sprintf("{ userdel -r %s || deluser --remove-home %s; } >/dev/null 2>&1 || true", u.Username, u.Username),
					fmt.Sprintf("if id -u %s >/dev/null 2>&1; then echo 'WARNUNG: konto %s konnte nicht entfernt werden'; fi", u.Username, u.Username),
					fmt.Sprintf("rm -f /etc/sudoers.d/lcm-%s", u.Username))
			}
		}
	}
	// 2. Zugänge des LCM-Service-Users entfernen. authorized_keys zuerst
	// (garantiert Credential-Widerruf), dann sudoers, dann der Account.
	steps = append(steps,
		fmt.Sprintf("rm -f /home/%s/.ssh/authorized_keys", server.ServiceUser),
		fmt.Sprintf("rm -f /etc/sudoers.d/%s", server.ServiceUser),
		fmt.Sprintf("userdel -rf %s 2>/dev/null || true", server.ServiceUser),
	)

	out, _, runErr := conn.Run(privRun(server, strings.Join(steps, "; ")))
	return out, runErr
}

// RotateKey erzeugt ein neues SSH-Keypair, hinterlegt es auf dem Server
// (verbunden mit dem ALTEN Key), testet es und entzieht danach den alten
// Schlüssel (Security-Konzept 9.8).
func (s *ServerService) RotateKey(scope repositories.AccessScope, id uint, actor string) error {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return err
	}
	if err := ensureSSHTransport(server); err != nil {
		return err
	}
	conn, err := s.connectRec(server, "rotate-key", actor)
	if err != nil {
		return fmt.Errorf("verbindung mit altem key: %w", err)
	}
	defer conn.Close()

	newPriv, newPub, err := sshx.GenerateKeyPair("lcm@" + server.Name)
	if err != nil {
		return err
	}
	authFile := fmt.Sprintf("/home/%s/.ssh/authorized_keys", server.ServiceUser)
	// rotateFail auditiert den gescheiterten Versuch (R2-026 -
	// sicherheitsrelevante Fehlschläge gehören ins Log) und liefert den
	// Fehler zurück.
	rotateFail := func(err error) error {
		s.audit.Log(actor, "server.rotate-key.failed", "server", id, server.Name+": "+err.Error())
		return err
	}
	// Neuen Key zusätzlich hinterlegen (alter bleibt vorerst gültig).
	if out, code, err := conn.Run(fmt.Sprintf("printf '%%s\\n' %s >> %s", shellQuote(newPub), authFile)); err != nil || code != 0 {
		// Ursache in die Meldung ziehen: bei code!=0 ist err oft nil und der
		// Grund steht nur in der Ausgabe (z.B. „Permission denied") - der
		// nackte %w ergab sonst „%!w(<nil>)" (R2-024).
		return rotateFail(fmt.Errorf("neuen key hinterlegen fehlgeschlagen: %s", rotateErrDetail(code, out, err)))
	}
	// Neuen Key testen.
	rawTest, err := s.dialer.DialKey(server.Host, server.SSHPort, server.ServiceUser, newPriv, server.HostKeyFingerprint)
	if err != nil {
		// Schritt 1 rückgängig machen: den frisch angehängten (aber nie
		// übernommenen) Key wieder aus authorized_keys entfernen - sonst
		// sammeln sich bei jedem Fehlversuch verwaiste Schlüssel an (R2-025).
		// Über die noch offene ALTE Verbindung; best effort.
		_, _, _ = conn.Run(fmt.Sprintf("grep -vF %s %s > %s.lcmtmp 2>/dev/null && mv %s.lcmtmp %s",
			shellQuote(newPub), authFile, authFile, authFile, authFile))
		return rotateFail(fmt.Errorf("neuer key funktioniert nicht - rotation abgebrochen: %w", err))
	}
	testConn := s.recorder.Record(rawTest, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: "rotate-key (verify)",
		Host: server.Host, User: server.ServiceUser,
	})
	// Alten Key entziehen: authorized_keys auf genau den neuen Key setzen.
	if out, code, err := testConn.Run(fmt.Sprintf("printf '%%s\\n' %s > %s", shellQuote(newPub), authFile)); err != nil || code != 0 {
		testConn.Close()
		return rotateFail(fmt.Errorf("alten key entziehen fehlgeschlagen: %s", rotateErrDetail(code, out, err)))
	}
	testConn.Close()

	enc, err := s.cipher.EncryptString(newPriv)
	if err != nil {
		return err
	}
	if err := s.servers.UpdateFields(id, map[string]any{"private_key_enc": enc, "public_key": newPub}); err != nil {
		return err
	}
	s.audit.Log(actor, "server.rotate-key", "server", id, server.Name)
	slog.Info("service key rotated", "server", server.Name, "actor", actor)
	return nil
}

// rotateErrDetail baut eine aussagekräftige Fehlerursache aus Exit-Code,
// Kommando-Ausgabe und (optionalem) Transportfehler - nie ein nacktes
// „%!w(<nil>)" (R2-024).
func rotateErrDetail(code int, out string, err error) string {
	if err != nil {
		return err.Error()
	}
	if o := strings.TrimSpace(out); o != "" {
		return fmt.Sprintf("exit-code %d: %s", code, summarize(o))
	}
	return fmt.Sprintf("exit-code %d", code)
}

// HardenSSH konfiguriert den SSH-Dienst des Servers so, dass Logins
// ausschließlich über Zertifikate möglich sind (kein Passwort). Ein
// Drop-in unter /etc/ssh/sshd_config.d/ überschreibt PasswordAuthentication
// und aktiviert PubkeyAuthentication; danach wird sshd neu geladen.
func (s *ServerService) HardenSSH(scope repositories.AccessScope, id uint, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	if err := ensureSSHTransport(server); err != nil {
		return "", err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return "", err
	}
	conn, err := s.connectRec(server, "harden-ssh", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	dropin := "PasswordAuthentication no\\nChallengeResponseAuthentication no\\n" +
		"PubkeyAuthentication yes\\nPermitRootLogin prohibit-password\\n"
	script := strings.Join([]string{
		sshdEnsureIncludeScript,
		"install -d -m 755 /etc/ssh/sshd_config.d",
		fmt.Sprintf("printf '%s' > /etc/ssh/sshd_config.d/60-lcm-hardening.conf", dropin),
		// Konfiguration prüfen, bevor der Dienst neu geladen wird.
		"sshd -t",
		sshdReloadScript,
	}, " && ")
	// Eingeschränkter Modus: das Drop-in schreibt der LCM-Helper (fester
	// Inhalt, mit Rollback bei sshd-Fehler).
	if server.RestrictedSudo {
		script = helperCmd("ssh-harden", "on")
	}

	output, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return output, runErr
	}
	if code != 0 {
		return output, fmt.Errorf("ssh-härtung fehlgeschlagen (exit %d)", code)
	}
	// Nachweis statt Annahme: erst die EFFEKTIVE Konfiguration entscheidet, ob
	// die Härtung wirkt. Auf openSUSE band sshd_config das Drop-in-Verzeichnis
	// nicht ein - die Datei wurde geschrieben und nie gelesen, der Reload
	// gelang, und LCM meldete "gehärtet", während Passwort-Anmeldung offen
	// blieb (BUG-026). Bei einer Sicherheitsfunktion ist eine unbelegte
	// Erfolgsmeldung schlimmer als ein ehrlicher Fehlschlag.
	// Im eingeschränkten Modus hat der LCM-Helper die Wirkung bereits selbst
	// belegt (sshd_password_auth_off, fail-closed) - er hätte sonst
	// abgebrochen und zurückgerollt. Eine zweite Kontrolle von hier aus wäre
	// nicht nur doppelt, sie kann dort gar nicht laufen: `sshd -T` steht aus
	// gutem Grund nicht auf der sudo-Whitelist, liefe also als unprivilegierter
	// Benutzer und ergebnislos - und würde die geglückte Härtung fälschlich
	// als „nicht überprüfbar" abstempeln.
	if !server.RestrictedSudo {
		if err := s.verifyPasswordAuthDisabled(conn, server); err != nil {
			return output, err
		}
	}
	_ = s.servers.UpdateFields(id, map[string]any{"ssh_hardened": true})
	s.audit.Log(actor, "server.harden-ssh", "server", id, server.Name)
	return output, nil
}

// sshdEnsureIncludeScript stellt sicher, dass sshd_config das Drop-in-
// Verzeichnis überhaupt einliest. Debian/Ubuntu liefern die Include-Zeile mit,
// openSUSE Leap 16 nicht - dort blieb jedes Drop-in wirkungslos.
//
// Die Zeile muss GANZ OBEN stehen: OpenSSH nimmt für jede Option den ZUERST
// gefundenen Wert. Stünde sie am Ende, gewänne ein früheres
// "PasswordAuthentication yes" aus der Hauptdatei. Eine Sicherung der
// Originaldatei bleibt daneben liegen.
// Die Hauptdatei wird dabei NICHT als /etc/ssh/sshd_config vorausgesetzt:
// openSUSE Leap 16 hat ein stateless /etc und liefert sie unter
// /usr/etc/ssh/sshd_config aus. Dort brach die Kette bisher schon am grep/cp
// ab („No such file or directory"), das Drop-in wurde nie geschrieben, und
// LCM meldete trotzdem einen Konfigurationskonflikt statt der wahren Ursache
// (R2-015). Fehlt die Include-Zeile in einer /usr/etc-Vorgabe, entsteht die
// Ergänzung als neue Datei unter /etc - das ist bei stateless /etc der
// vorgesehene Weg, die Vorgabe zu überschreiben.
const sshdEnsureIncludeScript = `SSHDCONF=/etc/ssh/sshd_config; ` +
	`[ -f "$SSHDCONF" ] || SSHDCONF=/usr/etc/ssh/sshd_config; ` +
	`[ -f "$SSHDCONF" ] || { echo "sshd_config weder unter /etc/ssh noch unter /usr/etc/ssh gefunden" >&2; exit 1; }; ` +
	`if ! grep -qiE '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/' "$SSHDCONF"; then ` +
	`cp -f "$SSHDCONF" /etc/ssh/sshd_config.lcm-backup && ` +
	`{ echo '# von LCM ergänzt: Drop-ins einlesen (muss oben stehen)'; ` +
	`echo 'Include /etc/ssh/sshd_config.d/*.conf'; cat /etc/ssh/sshd_config.lcm-backup; } > /etc/ssh/sshd_config; fi`

// sshdReloadScript lädt den sshd neu, ohne einen bestimmten Init-Dienst
// vorauszusetzen: systemd deckt die Mehrheit ab, OpenRC (Alpine) und das
// klassische service-Skript die übrigen. Zuvor war nur systemctl/service
// vorgesehen, weshalb auf Alpine jede Aktion mit Dienst-Neustart scheiterte
// (BUG-027).
//
// Die Kaskade steht in einer Gruppe { …; }. Ohne sie verbindet die Shell die
// vorangehenden `&&`-Schritte und die `||`-Alternativen gleichrangig von
// links: schlug ein früherer Schritt fehl (z.B. `sshd -t`), sprang die
// Ausführung in den ersten `||`-Zweig, und ein dort gelingender Reload setzte
// den Exit-Code wieder auf 0 - der Fehlschlag der Schreibkette blieb
// unsichtbar (R2-014, Nebenbeobachtung; belegt auf openSUSE über R2-015).
//
// Socket-Aktivierung zuerst abfangen: Debian 13 (und neuere Ubuntu) halten
// Port 22 über `ssh.socket` und starten sshd pro Verbindung. Ein
// `systemctl reload ssh.service` startet dort einen ZWEITEN sshd, der beim
// SIGHUP am eigenen Bind scheitert („fatal: Cannot bind any address"). Der
// Dienst bleibt `failed` liegen - und mit ihm verschwindet sein
// RuntimeDirectory /run/sshd, ohne das `sshd -T` nicht mehr läuft. Damit ist
// ausgerechnet die Nachweis-Grundlage der Härtung zerstört.
//
// Socket-aktiviert heißt aber NICHT immer „sshd läuft pro Verbindung": Debian
// 13 betreibt ssh.socket mit Accept=no - systemd reicht den lauschenden
// Socket an EINEN dauerhaften ssh.service weiter, der alle Verbindungen
// bedient. Ein bloßes Überspringen ließ dort jede Konfigurationsänderung
// (Härtung, Root-Login, 2FA) wirkungslos im alten Daemon liegen, während LCM
// Erfolg meldete - aufgefallen beim 2FA-Test: sshd -T zeigte die neue
// Konfiguration, der laufende sshd verlangte weiter nur den Key. Deshalb:
// läuft neben dem Socket ein aktiver Dienst, wird DER neu gestartet (restart
// statt reload - beim Reload scheitert der re-exec am systemd-eigenen Socket;
// bestehende Sitzungen überleben dank KillMode=process). Nur ohne aktiven
// Dienst (echter Pro-Verbindung-Modus, Accept=yes) bleibt der Skip richtig,
// weil jede neue Verbindung einen frischen sshd mit aktueller Konfiguration
// bekommt.
const sshdSocketActiveScript = "systemctl is-active --quiet ssh.socket 2>/dev/null || " +
	"systemctl is-active --quiet sshd.socket 2>/dev/null"

const sshdReloadScript = "{ if " + sshdSocketActiveScript + "; then " +
	"if systemctl is-active --quiet ssh.service 2>/dev/null; then systemctl restart ssh; " +
	"elif systemctl is-active --quiet sshd.service 2>/dev/null; then systemctl restart sshd; fi; " +
	"else " +
	"systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || " +
	"rc-service sshd reload 2>/dev/null || rc-service sshd restart 2>/dev/null || " +
	"service ssh reload 2>/dev/null || service sshd reload; fi; }"

// verifyPasswordAuthDisabled liest die effektive sshd-Konfiguration (sshd -T
// wertet Includes und Match-Blöcke aus) und besteht darauf, dass die
// Passwort-Anmeldung tatsächlich abgeschaltet ist.
func (s *ServerService) verifyPasswordAuthDisabled(conn sshx.Conn, server *domain.Server) error {
	// `mkdir -p /run/sshd` vorweg: auf socket-aktiviertem sshd (Debian 13)
	// existiert das Verzeichnis nur für die Lebensdauer einer Verbindung,
	// weshalb `sshd -T` in unserer eigenen, späteren Sitzung mit „Missing
	// privilege separation directory" abbrach - die Prüfung lief also
	// ergebnislos ins Leere (R2-014). Die Fehlerausgabe wird nicht mehr
	// verworfen, sondern in die Meldung übernommen.
	const probe = "mkdir -p /run/sshd 2>/dev/null; sshd -T 2>&1 | grep -i '^passwordauthentication'"
	out, _, err := conn.Run(privRun(server, probe))
	if err != nil {
		return fmt.Errorf("wirkung der ssh-härtung nicht prüfbar: %w", err)
	}
	value := strings.ToLower(strings.TrimSpace(firstLine(out)))
	switch {
	case value == "":
		// Kein Ergebnis heißt: unbelegt - und unbelegt darf bei einer
		// Sicherheitsfunktion nicht als Erfolg durchgehen. Bisher meldete LCM
		// hier „gehärtet" und setzte ssh_hardened=true, während die
		// Passwort-Anmeldung nachweislich offen blieb; der Zweifel stand nur
		// im Log (R2-014). Das Drop-in bleibt bewusst liegen - es zu
		// entfernen hieße, einen womöglich wirksamen Schutz wegen einer
		// fehlgeschlagenen Messung abzuräumen.
		slog.Warn("ssh hardening effect not verifiable (sshd -T returned no result)",
			"server", server.Name)
		return fmt.Errorf("die SSH-Härtung wurde geschrieben, ihre Wirkung ist aber nicht überprüfbar: " +
			"`sshd -T` lieferte kein Ergebnis für PasswordAuthentication. Der Server gilt deshalb " +
			"NICHT als gehärtet - prüfe auf dem System selbst mit `sshd -T | grep -i passwordauth` " +
			"und härte danach erneut")
	case strings.HasSuffix(value, " no"):
		return nil
	default:
		return fmt.Errorf("die SSH-Härtung wurde geschrieben, wirkt aber nicht: der Dienst meldet weiterhin %q. "+
			"Prüfe, ob /etc/ssh/sshd_config eigene Einträge für PasswordAuthentication enthält, "+
			"die vor dem Drop-in stehen - OpenSSH nimmt jeweils den zuerst gefundenen Wert", value)
	}
}

// UnhardenSSH hebt die LCM-SSH-Härtung wieder auf: das von HardenSSH
// geschriebene Drop-in wird entfernt und sshd neu geladen - der Server
// fällt damit auf seine ursprüngliche sshd-Konfiguration zurück (z.B.
// wieder erlaubte Passwort-Anmeldung).
func (s *ServerService) UnhardenSSH(scope repositories.AccessScope, id uint, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	if err := ensureSSHTransport(server); err != nil {
		return "", err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return "", err
	}
	conn, err := s.connectRec(server, "unharden-ssh", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	script := strings.Join([]string{
		"rm -f /etc/ssh/sshd_config.d/60-lcm-hardening.conf",
		// Konfiguration prüfen, bevor der Dienst neu geladen wird.
		"sshd -t",
		sshdReloadScript,
	}, " && ")
	if server.RestrictedSudo {
		script = helperCmd("ssh-harden", "off")
	}

	output, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return output, runErr
	}
	if code != 0 {
		return output, fmt.Errorf("ssh-härtung aufheben fehlgeschlagen (exit %d)", code)
	}
	_ = s.servers.UpdateFields(id, map[string]any{"ssh_hardened": false})
	s.audit.Log(actor, "server.unharden-ssh", "server", id, server.Name)
	return output, nil
}

// ConfigureFirewall (asynchroner Job, Multi-Backend) liegt in
// firewall_action.go - hier stand früher die synchrone ufw-only-Variante.

// ---- Paket-Updates (ad-hoc, als asynchrone Jobs) ----------------------------

// UpgradeAllPackages spielt alle verfügbaren Paket-Updates ein (apt upgrade).
func (s *ServerService) UpgradeAllPackages(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	return s.startPackageJob(scope, id, domain.RuleTypeUpdate,
		"Alle Pakete aktualisieren", pkgUpgradeAllScript, actor)
}

// RefreshPackages aktualisiert die Paketliste eines Servers: apt-get update
// (Metadaten auffrischen) und anschließend Neu-Erfassung des installierten
// Bestands samt verfügbarer Updates. Es wird NICHTS installiert.
func (s *ServerService) RefreshPackages(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	return s.startPackageJob(scope, id, domain.RuleTypePackageScan,
		"Paketliste aktualisieren", pkgRefreshScript, actor)
}

// UpgradeSecurityPackages spielt nur Security-/Bugfix-Updates ein.
func (s *ServerService) UpgradeSecurityPackages(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	return s.startPackageJob(scope, id, domain.RuleTypeSecurity,
		"Security-Updates einspielen", pkgSecurityUpgradeScript, actor)
}

// AutoremovePackages entfernt nicht mehr benötigte Pakete (verwaiste
// Abhängigkeiten) - `apt autoremove` und die Pendants der übrigen
// Paketverwaltungen.
//
// Die gesetzten Paket-Pins werden dabei durchgesetzt: Bei apt und dnf schreibt
// der Lauf zuerst die Schutzdatei (die Paketverwaltung respektiert sie dann
// selbst), bei zypper und pacman - die keine solche Datei kennen - wird die
// Kandidatenliste um die geschützten Pakete gekürzt. Ohne das räumte der Lauf
// weiterhin ältere Kernel weg.
func (s *ServerService) AutoremovePackages(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	pins := s.effectivePins(server)
	build := func(mgr string) string {
		if len(pins) == 0 {
			return pkgAutoremoveScript(mgr)
		}
		// Erst schützen, dann aufräumen - in dieser Reihenfolge, sonst ist die
		// Schutzdatei beim Lauf noch nicht da.
		return pkgPinScript(mgr, pins) + "\n" + pkgAutoremoveScriptWithPins(mgr, pins)
	}
	return s.startPackageJob(scope, id, domain.RuleTypeAutoremove,
		"Nicht mehr benötigte Pakete entfernen", build, actor)
}

// RemovePackages deinstalliert gezielt die genannten Pakete. Kritische
// Systempakete (SSH-Server, sudo, die Paketverwaltung selbst, Kernel, libc …)
// werden abgelehnt - ihr Entfernen würde den Server oder den LCM-Zugang
// unbrauchbar machen (Aussperr-/Selbstschuss-Schutz).
func (s *ServerService) RemovePackages(scope repositories.AccessScope, id uint, names []string, actor string) (*domain.Job, error) {
	clean, err := parsePackageNames(strings.Join(names, " "))
	if err != nil {
		return nil, err
	}
	for _, n := range clean {
		if isProtectedPackage(n) {
			return nil, fmt.Errorf("%w: %s", ErrProtectedPackage, n)
		}
	}
	// Ein Pin mit „nicht entfernen" gilt auch für das gezielte Entfernen -
	// sonst schützte er nur gegen den Aufräum-Lauf und der Klick daneben
	// entfernte das Paket doch.
	if server, ferr := s.servers.FindByID(scope, id); ferr == nil {
		for _, pin := range s.effectivePins(server) {
			if !pin.NoRemove {
				continue
			}
			for _, n := range clean {
				if pin.Matches(n) {
					return nil, fmt.Errorf("%w: %s (Pin: %s)", ErrPinnedPackage, n, pin.Name)
				}
			}
		}
	}
	build := func(mgr string) string { return pkgRemovePackagesScript(mgr, clean) }
	name := "Pakete entfernen: " + strings.Join(clean, ", ")
	return s.startPackageJob(scope, id, domain.RuleTypeAutoremove, name, build, actor)
}

// UpdatePackages aktualisiert gezielt Pakete. Ist version gesetzt, wird genau
// EIN Paket auf diese exakte Version gebracht (Downgrades erlaubt); sonst
// werden alle genannten Pakete auf die neueste Version aktualisiert.
func (s *ServerService) UpdatePackages(scope repositories.AccessScope, id uint, names []string, version, actor string) (*domain.Job, error) {
	var build func(mgr string) string
	var name string
	if version != "" {
		if len(names) != 1 {
			return nil, ErrVersionOnePkg
		}
		if !rePackageName.MatchString(names[0]) {
			return nil, ErrInvalidPackage
		}
		if !rePackageVersion.MatchString(version) {
			return nil, ErrInvalidVersion
		}
		build = func(mgr string) string { return pkgInstallVersionScript(mgr, names[0], version) }
		name = fmt.Sprintf("Paket %s → %s", names[0], version)
	} else {
		clean, err := parsePackageNames(strings.Join(names, " "))
		if err != nil {
			return nil, err
		}
		build = func(mgr string) string { return pkgUpgradePackagesScript(mgr, clean) }
		name = "Pakete aktualisieren: " + strings.Join(clean, ", ")
	}
	return s.startPackageJob(scope, id, domain.RuleTypePackages, name, build, actor)
}

// PackageVersions liefert die installierbaren Versionen eines Pakets
// (neueste zuerst) für die versionsgenaue Auswahl in der UI.
func (s *ServerService) PackageVersions(scope repositories.AccessScope, id uint, name string) ([]string, error) {
	if !rePackageName.MatchString(name) {
		return nil, ErrInvalidPackage
	}
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if server.IsDemo {
		return nil, nil
	}
	conn, err := s.connectRecRead(server, "package-versions", "system")
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	out, _, err := conn.Run(pkgVersionsCommand(server.PackageManager, name))
	if err != nil {
		return nil, err
	}
	return parsePkgVersions(server.PackageManager, name, out), nil
}

// startPackageJob prüft den Zugriff, baut das Skript für die erkannte
// Paketverwaltung des Servers, legt einen Job an (Concurrency-Lock pro
// Server) und führt die Paket-Aktion asynchron aus.
func (s *ServerService) startPackageJob(scope repositories.AccessScope, id uint, jobType, name string, build func(mgr string) string, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	// RouterOS hat keine (Linux-)Paketverwaltung - alle paketbasierten Aktionen
	// (Updates, Tool-Installationen) sind dort nicht anwendbar. Zentraler
	// Sperrpunkt, bevor ein sinnloser Job startet.
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	script := build(server.PackageManager)
	job, err := s.jobs.Start(&server.ID, nil, jobType, name+" @ "+server.Name, actor)
	if err != nil {
		return nil, err // u.a. ErrServerBusy → der Controller mappt auf 409
	}
	s.audit.Log(actor, "server.package-update", "server", id, name)
	safego.GoCleanup("job:package", jobPanicCleanup(s.jobs, job), func() {
		s.runPackageJob(job, server, script, actor)
	})
	return job, nil
}

// runPackageJob führt das Paket-Skript auf dem Server aus (protokolliert,
// mit dem Job verknüpft) und liest danach den Paketbestand neu ein.
func (s *ServerService) runPackageJob(job *domain.Job, server *domain.Server, script, actor string) {
	if server.IsDemo {
		s.jobs.Complete(job, demoSimulatePackageUpdate(s.servers, server), ptrInt(0), nil)
		return
	}
	conn, err := s.connect(server)
	if err != nil {
		_ = s.servers.UpdateFields(server.ID, unreachableFields(server, err))
		s.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = s.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: actor,
		Purpose: "package-update", Host: server.Host, User: server.ServiceUser,
	})
	defer conn.Close()

	output, code, runErr := conn.Run(privRun(server, script))
	if runErr == nil && code != 0 {
		runErr = fmt.Errorf("paket-update endete mit exit-code %d", code)
	}
	if runErr == nil {
		rescanPackagesInto(s.servers, conn, server)
		_ = s.servers.UpdateFields(server.ID, map[string]any{
			"reachable": true, "last_seen_at": time.Now(), "last_error": "", "failed_checks": 0,
		})
		// Nach dem Update die CVE-Bewertung auffrischen, damit keine veralteten
		// Sicherheits-Labels an bereits aktualisierten Paketen hängen bleiben.
		output += rescanCVEsAfterPackageUpdate(s.scanner, s.servers, s.cveRescanEnabled(), server)
	}
	s.jobs.Complete(job, output, ptrInt(code), runErr)
}

// ---- Queries (scope-gefiltert) ----------------------------------------------

func (s *ServerService) List(scope repositories.AccessScope) ([]domain.Server, error) {
	servers, err := s.servers.FindAll(scope)
	if err != nil {
		return nil, err
	}
	for i := range servers {
		s.fillAgentState(&servers[i])
	}
	return servers, nil
}

func (s *ServerService) Get(scope repositories.AccessScope, id uint) (*domain.Server, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	s.fillAgentState(server)
	return server, nil
}

// fillAgentState befüllt den Laufzeitzustand AgentConnected aus dem Hub
// (nicht persistiert; false ohne verdrahteten Hub oder für SSH-Server).
func (s *ServerService) fillAgentState(server *domain.Server) {
	if server.IsAgent() && s.agents != nil {
		server.AgentConnected = s.agents.Online(server.AgentID)
	}
}

// ActiveJob liefert den aktuell laufenden Job eines Servers (nil, wenn keiner
// läuft) - für die Laufender-Job-Anzeige und die Aktions-Sperre in der UI.
func (s *ServerService) ActiveJob(scope repositories.AccessScope, id uint) (*domain.Job, error) {
	if _, err := s.servers.FindByID(scope, id); err != nil {
		return nil, err
	}
	return s.jobs.RunningForServer(id)
}

// Status liefert den Ampel-Status inkl. Insights und der OS-Support-Bewertung
// (aktuelle LTS/EOL) zum aktuellen Zeitpunkt.
func (s *ServerService) Status(scope repositories.AccessScope, id uint) (string, []domain.StatusInsight, domain.OSSupportInfo, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	outdated, err := s.servers.CountOutdatedPackages(id)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	last, err := s.jobs.jobs.LastFinishedForServer(id)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	// CVE-Zählung GEWICHTET: Docker-CVEs zählen nur für ausdrücklich als
	// relevant markierte Container; Hochgewichtungs-Liste und lauschende
	// Dienste heben eine Stufe an.
	facts, err := s.servers.VulnerabilityFacts(id)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	relevantRefs := dockerRelevantRefs(s.servers, server)
	weighted := weightedVulnSummary(facts, s.cveWeightList(), splitCSVList(server.ListeningPackages), relevantRefs)
	outdatedImages, err := s.servers.CountOutdatedDockerImages(id)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	// Größe des Paketbestands: 0 heißt "nie erfasst" und macht den Server
	// unbewertbar statt makellos (BUG-020).
	inventoried, err := s.servers.CountPackages(id)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	in := domain.TrafficLightInput{
		OutdatedPackages: int(outdated), Now: time.Now(),
		CriticalVulns: weighted[domain.SeverityCritical], HighVulns: weighted[domain.SeverityHigh],
		RaisedVulnPackages:      raisedVulnPackages(facts, s.cveWeightList(), splitCSVList(server.ListeningPackages), relevantRefs),
		OutdatedContainerImages: int(outdatedImages),
		TotalVulns:              countedVulns(facts, relevantRefs),
		// Ernste, aber unbehebbare Lücken: reiner Info-Hinweis (R2-056).
		UnfixableVulns: unfixableCritHigh(facts, s.cveWeightList(), splitCSVList(server.ListeningPackages), relevantRefs),
		// RouterOS hat konstruktionsbedingt keinen Paketbestand - das darf hier
		// nicht als „nicht bewertbar" (Rot) durchschlagen; die Bewertung läuft
		// dort über die Versions-Aktualität (siehe TrafficLight).
		InventoryMissing: inventoried == 0 && !server.IsRouterOS(),
		CVEScanError:     server.CVEScanError,
		DeepScanWarnings: server.DeepScanWarnings,
		// Stand der zentralen Schwachstellen-Datenbank - reiner Hinweis, er
		// faerbt die Ampel nicht (siehe TrafficLightInput.CVEDB).
		CVEDB: s.CVEDBStatus(),
	}
	if last != nil {
		in.LastJobFailed = last.Status == domain.JobStatusFailed
		in.LastJobName = last.Name
	}
	status, insights := server.TrafficLight(in)
	// Die Docker-Qualifizierung hängt BEWUSST hinter der Farbentscheidung:
	// sie beschreibt die Erreichbarkeit genauer, ist aber kein Mangel, der
	// den Server gelb färben dürfte (siehe dockerFirewallInsight).
	if qual := dockerFirewallInsight(dockerPortExposures(s.servers, server), server.FirewallActive); qual != nil {
		insights = append(insights, *qual)
	}
	osSupport := domain.OSSupportStatus(server.OSID, server.OSVersionID, server.OSName, in.Now)
	return status, insights, osSupport, nil
}

// DockerPortExposures liefert die von außen erreichbaren Docker-Ports eines
// Servers - für die Firewall-Anzeige in der Server-Detailansicht.
func (s *ServerService) DockerPortExposures(scope repositories.AccessScope, id uint) ([]DockerPortExposure, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	return dockerPortExposures(s.servers, server), nil
}

// Packages liefert den Paketbestand eines Servers. Das vorgeschaltete
// FindByID ist KEIN überflüssiger Lookup - es erzwingt die Mandantentrennung
// (Scope-Prüfung), bevor die ungescopte Detail-Query läuft. Gleiches Muster
// in OutdatedPackages/Repositories.
func (s *ServerService) Packages(scope repositories.AccessScope, id uint) ([]domain.Package, error) {
	if _, err := s.servers.FindByID(scope, id); err != nil {
		return nil, err
	}
	return s.servers.FindPackages(id)
}

// Snaps liefert die installierten Snap-Pakete eines Servers (zweite
// Paketverwaltung; leer, wenn der Server kein snap nutzt).
func (s *ServerService) Snaps(scope repositories.AccessScope, id uint) ([]domain.SnapPackage, error) {
	if _, err := s.servers.FindByID(scope, id); err != nil {
		return nil, err
	}
	return s.servers.FindSnapPackages(id)
}

// RefreshAllSnaps aktualisiert alle Snaps eines Servers.
func (s *ServerService) RefreshAllSnaps(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	if err := s.requireSnapd(scope, id); err != nil {
		return nil, err
	}
	return s.startPackageJob(scope, id, domain.RuleTypeUpdate, "Alle Snaps aktualisieren",
		func(string) string { return snapRefreshAllScript() }, actor)
}

// RefreshSnaps aktualisiert die genannten Snaps.
func (s *ServerService) RefreshSnaps(scope repositories.AccessScope, id uint, names []string, actor string) (*domain.Job, error) {
	clean, err := parseSnapNames(strings.Join(names, " "))
	if err != nil {
		return nil, err
	}
	if err := s.requireSnapd(scope, id); err != nil {
		return nil, err
	}
	return s.startPackageJob(scope, id, domain.RuleTypePackages,
		"Snap aktualisieren: "+strings.Join(clean, ", "),
		func(string) string { return snapRefreshScript(clean) }, actor)
}

// RemoveSnaps entfernt die genannten Snaps. Die Grundlagen der
// Snap-Verwaltung (snapd, die core-Basen) sind ausgenommen - sie zu entfernen
// nähme allen übrigen Snaps die Laufzeit.
func (s *ServerService) RemoveSnaps(scope repositories.AccessScope, id uint, names []string, actor string) (*domain.Job, error) {
	clean, err := parseSnapNames(strings.Join(names, " "))
	if err != nil {
		return nil, err
	}
	for _, n := range clean {
		if isProtectedSnap(n) {
			return nil, fmt.Errorf("%w: %s", ErrProtectedSnap, n)
		}
	}
	if err := s.requireSnapd(scope, id); err != nil {
		return nil, err
	}
	return s.startPackageJob(scope, id, domain.RuleTypePackages,
		"Snap entfernen: "+strings.Join(clean, ", "),
		func(string) string { return snapRemoveScript(clean) }, actor)
}

// ErrNoSnapd: Auf dem Server gibt es keine Snap-Verwaltung.
var ErrNoSnapd = errors.New("auf diesem server ist snapd nicht vorhanden")

// requireSnapd weist Snap-Aktionen auf Servern ohne snapd ab, statt ein
// Kommando abzusetzen, das mit „command not found" endet.
func (s *ServerService) requireSnapd(scope repositories.AccessScope, id uint) error {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return err
	}
	if !server.HasSnap {
		return ErrNoSnapd
	}
	return nil
}

// VulnerabilityView ist ein CVE-Fund samt der Frage, ob er überhaupt zählt.
type VulnerabilityView struct {
	domain.Vulnerability
	// ImageUnused: Der Fund stammt aus einem Docker-Image, das kein
	// Container verwendet. Solche Funde werden angezeigt - sie sind ja da -,
	// fließen aber nicht in die Zusammenfassung ein: Ein Image, das nirgends
	// läuft, hat keine Angriffsfläche, und mitgezählt verschöbe es die Zahlen
	// nach oben, ohne dass jemand etwas tun könnte oder müsste.
	ImageUnused bool `json:"image_unused"`
}

// VulnerabilityReport bündelt die CVE-Sicht eines Servers für die UI.
type VulnerabilityReport struct {
	Vulnerabilities []VulnerabilityView `json:"vulnerabilities"`
	// Summary zählt die BEWERTETEN Funde (severity → Anzahl) - ohne die aus
	// ungenutzten Images.
	Summary map[string]int `json:"summary"`
	// UnusedSummary zählt die ausgenommenen Funde getrennt. Sie zu
	// verschweigen wäre die falsche Art von Ruhe: Wer ein altes Image
	// aufräumt oder wieder in Betrieb nimmt, soll wissen, was daran hängt.
	UnusedSummary    map[string]int `json:"unused_summary"`
	LastScanAt       *time.Time     `json:"last_scan_at"`
	ScannerAvailable bool           `json:"scanner_available"`
}

// Vulnerabilities liefert die gefundenen CVEs eines Servers samt Zusammenfassung
// und Scan-Metadaten (kritischste zuerst).
func (s *ServerService) Vulnerabilities(scope repositories.AccessScope, id uint) (*VulnerabilityReport, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	list, err := s.servers.FindVulnerabilities(id)
	if err != nil {
		return nil, err
	}

	// Welche Image-Referenzen nutzt überhaupt ein Container? Die Antwort
	// steht im Image-Inventar (in_use), das der Docker-Check pflegt.
	unusedRefs := map[string]bool{}
	if images, err := s.servers.FindDockerImages(id); err == nil {
		for i := range images {
			if !images[i].InUse {
				unusedRefs[images[i].Ref()] = true
			}
		}
	}

	views := make([]VulnerabilityView, 0, len(list))
	summary := map[string]int{}
	unusedSummary := map[string]int{}
	for i := range list {
		v := VulnerabilityView{Vulnerability: list[i]}
		v.ImageUnused = list[i].Source == domain.VulnSourceDocker && unusedRefs[list[i].ImageRef]
		if v.ImageUnused {
			unusedSummary[list[i].Severity]++
		} else {
			summary[list[i].Severity]++
		}
		views = append(views, v)
	}

	return &VulnerabilityReport{
		Vulnerabilities:  views,
		Summary:          summary,
		UnusedSummary:    unusedSummary,
		LastScanAt:       server.LastCVEScanAt,
		ScannerAvailable: s.ScannerAvailable(),
	}, nil
}

// StorageHistoryReport bündelt den Speicher-Verlauf eines Servers samt aktuellem
// Live-Wert und linearer Prognose für die grafische Darstellung.
type StorageHistoryReport struct {
	History        []domain.StorageHistory `json:"history"` // chronologisch aufsteigend
	CurrentTotalMB int64                   `json:"current_total_mb"`
	CurrentUsedMB  int64                   `json:"current_used_mb"`
	CurrentPercent int                     `json:"current_percent"`
	Forecast       domain.StorageForecast  `json:"forecast"`
	// Volumes: alle eingehängten Dateisysteme (Root „/" zuerst). Verlauf und
	// Prognose beziehen sich weiterhin auf das maßgebliche Root-Volume.
	Volumes []domain.DiskVolume `json:"volumes"`
}

// StorageHistory liefert die tägliche Festplattenbelegung eines Servers über
// die Zeit (aufsteigend) plus den aktuellen Live-Stand.
func (s *ServerService) StorageHistory(scope repositories.AccessScope, id uint) (*StorageHistoryReport, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	hist, err := s.servers.FindStorageHistory(id)
	if err != nil {
		return nil, err
	}
	volumes, err := s.servers.FindDiskVolumes(id)
	if err != nil {
		return nil, err
	}
	return &StorageHistoryReport{
		History:        hist,
		CurrentTotalMB: server.DiskTotalMB,
		CurrentUsedMB:  server.DiskUsedMB,
		CurrentPercent: server.DiskUsagePercent(),
		Forecast:       domain.ComputeForecast(hist, server.DiskTotalMB),
		Volumes:        volumes,
	}, nil
}

func (s *ServerService) OutdatedPackages(scope repositories.AccessScope, id uint) ([]domain.Package, error) {
	if _, err := s.servers.FindByID(scope, id); err != nil {
		return nil, err
	}
	return s.servers.FindOutdatedPackages(id)
}

func (s *ServerService) Repositories(scope repositories.AccessScope, id uint) ([]domain.AptRepository, error) {
	if _, err := s.servers.FindByID(scope, id); err != nil {
		return nil, err
	}
	return s.servers.FindRepositories(id)
}

// ServerSettingsInput bündelt die änderbaren Server-Metadaten/Schalter der
// Einstellungsseite. Pointer-/Leerwerte bedeuten „unverändert lassen".
type ServerSettingsInput struct {
	Name                  string
	Host                  string
	Port                  int
	UserSyncDisabled      *bool
	UnreachableUncritical *bool
	UnreachableGraceDays  *int
	DockerUpdatesDisabled *bool
	DockerCVEsIgnored     *bool
}

// UpdateSettings ändert Metadaten eines Servers (Name, Host, Port) sowie die
// Server-Schalter (Benutzer-Sync, Nichterreichbarkeit unkritisch + Kulanzfrist).
// nil-/Leerwerte lassen das jeweilige Feld unverändert.
func (s *ServerService) UpdateSettings(scope repositories.AccessScope, id uint, in ServerSettingsInput, actor string) (*domain.Server, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if in.Name != "" {
		server.Name = in.Name
	}
	// Host/Port-Eindeutigkeit auch hier erzwingen - beim Join/Anlegen wird
	// sie geprüft, eine Änderung über die Einstellungen darf sie nicht
	// umgehen. Geprüft wird die NEUE Kombination aus Host und Port.
	newHost, newPort := server.Host, server.SSHPort
	if in.Host != "" {
		newHost = in.Host
	}
	if in.Port > 0 {
		newPort = in.Port
	}
	if newHost != server.Host || newPort != server.SSHPort {
		if taken, err := s.servers.HostExists(newHost, newPort, id); err != nil {
			return nil, err
		} else if taken {
			return nil, ErrServerHostTaken
		}
	}
	server.Host = newHost
	server.SSHPort = newPort
	if in.UserSyncDisabled != nil {
		server.UserSyncDisabled = *in.UserSyncDisabled
	}
	if in.UnreachableUncritical != nil {
		server.UnreachableUncritical = *in.UnreachableUncritical
	}
	if in.UnreachableGraceDays != nil {
		server.UnreachableGraceDays = domain.ClampUnreachableGraceDays(*in.UnreachableGraceDays)
	}
	if in.DockerUpdatesDisabled != nil {
		server.DockerUpdatesDisabled = *in.DockerUpdatesDisabled
	}
	if in.DockerCVEsIgnored != nil {
		server.DockerCVEsIgnored = *in.DockerCVEsIgnored
	}
	if err := s.servers.Update(server); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "server.update", "server", id, server.Name)
	return server, nil
}
