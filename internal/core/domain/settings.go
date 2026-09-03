package domain

import (
	"strings"
	"time"
)

// GlobalSettings sind die über die UI konfigurierbaren Systemeinstellungen
// (genau eine Zeile, ID 1). Sensible Werte sind AES-GCM-verschlüsselt.
type GlobalSettings struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	UpdatedAt time.Time `json:"updated_at"`

	// System-Standard-Credentials für das Server-Onboarding (optional):
	// werden im Join-Formular als Default angeboten.
	DefaultSSHUser        string `json:"default_ssh_user"`
	DefaultSSHPasswordEnc string `json:"-"` // AES-GCM
	DefaultSSHPort        int    `gorm:"default:22" json:"default_ssh_port"`

	// Log Retention: Job-Historien und Konsolen-Outputs werden nach
	// dieser Frist automatisch gelöscht (0 = nie).
	LogRetentionDays int `gorm:"default:90" json:"log_retention_days"`

	// Speicher-Verlauf: Aufbewahrung der täglichen Festplatten-Snapshots
	// (Tagesdurchschnitte). Über die UI einstellbar, wird auf 90-365 Tage
	// begrenzt.
	StorageHistoryRetentionDays int `gorm:"default:90" json:"storage_history_retention_days"`

	// Automatische System-Backups (DB + Konfiguration).
	BackupEnabled       bool   `gorm:"default:true" json:"backup_enabled"`
	BackupIntervalHours int    `gorm:"default:24" json:"backup_interval_hours"`
	BackupRetention     int    `gorm:"default:14" json:"backup_retention"` // Anzahl aufbewahrter Backups
	BackupDir           string `json:"backup_dir"`                         // leer = <data>/backups
	// BackupTime verankert den Zeitplan an einer festen Uhrzeit (HH:MM,
	// Serverzeit). Vorher zählte ein @every-Intervall ab dem letzten
	// Scheduler-Reload - jedes Speichern der Einstellungen und jeder Neustart
	// verschob damit den Lauf, und wer öfter speicherte, als das Intervall
	// lang war, bekam nie ein Backup (R2-034). Bei Intervallen, die den Tag
	// teilen (1,2,3,4,6,8,12,24 h), läuft das Backup jetzt zu festen, aus
	// dieser Uhrzeit abgeleiteten Zeiten; andere Intervalle behalten @every
	// samt Nachhol-Watchdog. Leer = DefaultBackupTime.
	BackupTime string `json:"backup_time"`
	// BackupPassphraseEnc: Passphrase für UNBEAUFSICHTIGTE (geplante)
	// Backups, AES-GCM-verschlüsselt. Ohne sie (und ohne
	// LCM_BACKUP_PASSPHRASE) konnte das ab Werk aktive geplante Backup
	// PRINZIPBEDINGT nie laufen - 13 stille Fehlversuche im Langzeittest
	// (R2-027). Zirkularität ist unbedenklich: Die Passphrase liegt in der
	// DB, die nur im passphrase-verschlüsselten Archiv steckt - wer das
	// Archiv öffnen kann, kannte die Passphrase schon; wer den Host samt
	// Master-Key kompromittiert, hat ohnehin alles.
	BackupPassphraseEnc string `json:"-"` // AES-GCM

	// RestoreAutoRestart: Wird ein Backup zur Wiederherstellung vorbereitet
	// (Staging), startet LCM sich bei true selbst neu, um es anzuwenden - sinnvoll
	// nur unter einem Prozess-Supervisor (systemd/Docker mit Restart-Policy).
	// Bei false bleibt der Restore vorbereitet und der Betreiber startet manuell.
	// Die Umgebungsvariable LCM_RESTORE_AUTO_RESTART hat Vorrang.
	RestoreAutoRestart bool `gorm:"default:false" json:"restore_auto_restart"`

	// MCP-Schnittstelle (Model Context Protocol): ein separater, per Bearer-
	// API-Key authentifizierter HTTP-Listener, über den KI-Agenten read-only
	// Server-Eigenschaften abrufen (nie Passwörter/Keys). Vollständig über
	// diese Einstellungen an-/abschaltbar (MCPEnabled) und in Bind-Adresse
	// (MCPBindHost) und Port (MCPPort) konfigurierbar. Der Listener wird beim
	// Umschalten zur Laufzeit gestartet/gestoppt.
	MCPEnabled  bool   `gorm:"default:false" json:"mcp_enabled"`
	MCPBindHost string `gorm:"default:'127.0.0.1'" json:"mcp_bind_host"`
	MCPPort     int    `gorm:"default:9330" json:"mcp_port"`

	// CVE-Scan (Trivy): täglicher Abgleich des Paketbestands gegen die
	// Schwachstellen-Datenbank. Der Zeitplan ist ein Cron-Ausdruck.
	CVEScanEnabled bool `gorm:"default:true" json:"cve_scan_enabled"`
	// CVEScanCron: bewusst NICHT 04:00. Dort liegt der mitgelieferte
	// System-Sync, und der Trivy-Lauf ist der schwerste Posten des Tages -
	// beides zusammen auf einer kleinen Maschine hat den Dienst schon unter
	// den systemd-Watchdog gebracht. Bestandsinstallationen behalten ihren
	// eingestellten Wert; wer noch auf 04:00 steht, verschiebt ihn in den
	// Einstellungen.
	CVEScanCron string `gorm:"default:'30 2 * * *'" json:"cve_scan_cron"`
	// Frühwarnung (Etappe B): Zusätzlich zum täglichen Trivy-Scan fragt LCM
	// alle 15 Minuten die Online-Quelle OSV nach Befunden zum installierten
	// Paketbestand. Das ist die schnelle Spur - dort stehen neue Advisories
	// und Schadpaket-Meldungen binnen Minuten, während die Trivy-Datenbank
	// erst gebaut und heruntergeladen werden muss.
	//
	// Standard AUS und bewusst opt-in: Die Abfrage schickt den
	// (deduplizierten, serverlosen) Paketbestand an einen fremden Dienst.
	// Das ist eine Aussage über die eigene Infrastruktur, die der Betreiber
	// treffen muss - nicht die Voreinstellung.
	AdvisoryPollingEnabled bool `gorm:"default:false" json:"advisory_polling_enabled"`
	// AdvisoryLocalCopy stellt die Frühwarnung auf eine lokale Kopie der
	// OSV-Datenbank um: Der Paketbestand verlässt das Haus dann nicht mehr.
	// Der Preis ist ausdrücklich benannt - die Frühwarn-Latenz steigt vom
	// Minutentakt auf den Rhythmus des Spiegels, also etwa einen Tag.
	AdvisoryLocalCopy bool `gorm:"default:false" json:"advisory_local_copy"`
	// AdvisoryLastPollAt hält fest, wann die Frühwarnung zuletzt erfolgreich
	// durchgelaufen ist. Kein Konfigurationswert, sondern Betriebszustand -
	// er steht hier, weil die Oberfläche ihn zusammen mit den Einstellungen
	// liest und weil es dafür bereits Vorbilder gibt (Subscription-Prüfung).
	AdvisoryLastPollAt *time.Time `json:"advisory_last_poll_at"`
	// AdvisoryCacheTTLMinutes ist die Gültigkeit eines Paket-Befunds im
	// lokalen Zwischenspeicher. 0 = Zwischenspeicher aus (jeder Durchgang
	// fragt alles neu).
	//
	// Die Obergrenze (AdvisoryCacheTTLMax) ist bewusst niedrig: Ein Treffer
	// im Zwischenspeicher heißt, dass NICHT nachgesehen wurde - und zwar
	// genau in dem Zeitfenster, für das die Frühwarnung überhaupt existiert.
	// Die Untergrenze ist der Poll-Takt - darunter läuft jeder Eintrag vor
	// seiner ersten Verwendung ab (siehe AdvisoryCacheTTLMin).
	AdvisoryCacheTTLMinutes int `gorm:"default:20" json:"advisory_cache_ttl_minutes"`

	// CVEHighWeightPackages: kommagetrennte Liste von Paketen, deren CVEs
	// eine Schwere-Stufe HÖHER gewichtet werden (exponierte Dienste:
	// Webserver, Protokoll-Server, alles mit offenen Ports oder hohem
	// Systemzugriff). Leer = eingebaute Standardliste; ein einzelnes "-"
	// deaktiviert die Liste. Präfix-Match: "postgresql" trifft auch
	// "postgresql-14".
	CVEHighWeightPackages string `json:"cve_high_weight_packages"`

	// DNSServerPresets: gepflegte Vorgabe-Nameserver für die Server-Einstellung
	// „DNS setzen". Je Zeile ein Eintrag, optional als „Label = IP"
	// (z.B. "Cloudflare = 1.1.1.1"); ohne Label genügt die IP. Leer =
	// eingebaute Standardliste (DefaultDNSServerPresets).
	DNSServerPresets string `json:"dns_server_presets"`

	// NTPServerPresets: gepflegte Vorgabe-Zeitserver für die Server-Aktion
	// „NTP einrichten" (je Zeile „Label = Host"). Leer = eingebaute
	// Standardliste (DefaultNTPServerPresets).
	NTPServerPresets string `json:"ntp_server_presets"`
	// DefaultTimezone ist die Zeitzone, mit der die Aktion „Zeitzone setzen"
	// vorbelegt wird - in der Regel die des Betreibers. Leer = keine Vorgabe.
	DefaultTimezone string `json:"default_timezone"`
	// DNSTestDomains: Domains, deren Auflösbarkeit der DNS-Test auf dem Server
	// prüft (je Zeile eine). Leer = eingebaute Standardliste (DefaultDNSTestDomains).
	DNSTestDomains string `json:"dns_test_domains"`

	// SessionTTLMinutes ist die Lebensdauer einer Anmeldesession (JWT) in
	// Minuten. 0 = Vorgabe aus der config.json (access_token_ttl_minutes).
	SessionTTLMinutes int `gorm:"default:0" json:"session_ttl_minutes"`

	// JobIdleTimeoutMinutes ist die erlaubte STILLE eines laufenden Jobs:
	// Solange von der Gegenseite Ausgabe kommt, arbeitet der Lauf und darf
	// beliebig lange dauern; kommt so lange gar nichts mehr, bricht der
	// Job-Watchdog ab und gibt die Server-Sperre wieder frei (z.B. apt, das
	// auf einen dpkg-Lock wartet). 0 = Watchdog aus.
	//
	// Eine Maximaldauer gibt es bewusst nicht mehr: Sie kann einen langsamen
	// Rechner nicht von einem hängenden Prozess unterscheiden und schnitt
	// große Upgrades auf schwacher Hardware mitten im Lauf ab.
	JobIdleTimeoutMinutes int `gorm:"default:5" json:"job_idle_timeout_minutes"`
	// JobIdleTimeoutSlowMinutes ist dieselbe Frist für schwache Hardware
	// (Raspberry Pi & Co., siehe Server.IsSlowHardware). Dort sind lange
	// stille Phasen normal - ein einzelner dpkg-Trigger wie update-initramfs
	// läuft auf einer SD-Karte minutenlang ohne eine Zeile Ausgabe.
	JobIdleTimeoutSlowMinutes int `gorm:"default:30" json:"job_idle_timeout_slow_minutes"`

	// TerminalEnabled gibt die Web-Konsole für diese Installation frei.
	//
	// Sie ist die eingriffsstärkste Funktion des Systems: Wer sie benutzen
	// darf, bekommt über LCMs Schlüssel eine Root-Shell auf jedem verwalteten
	// Server. Die Berechtigung (PermServersConsole) regelt, WER sie benutzen
	// darf; dieser Schalter entscheidet, ob es sie hier überhaupt gibt.
	//
	// Vorgabe an, aber ohne Wirkung für die meisten: Die Berechtigung liegt
	// im Auslieferungszustand allein bei admin. Ein Betreiber, der die
	// Fähigkeit gar nicht im Haus haben will, legt hier den Schalter um -
	// dann führt auch ein versehentlich vergebenes Recht zu nichts.
	TerminalEnabled bool `gorm:"default:true" json:"terminal_enabled"`

	// APT-Cache (apt-cacher-ng): Basis-URL des zentralen Paket-Caches,
	// z.B. http://192.168.1.10:3142. Leer = Feature aus. Server leiten ihre
	// APT-Anfragen über diesen Proxy, sobald die Aktion „APT-Cache verwenden"
	// bzw. die Gruppen-Regel apt-proxy greift.
	AptCacheURL string `json:"apt_cache_url"`

	// Onboarding-SSH-Key: ein beim ersten Start erzeugtes Schlüsselpaar, das
	// als ALTERNATIVE zum Passwort für den initialen Login beim Join/Reconnect
	// dient. Der Public Key (OnboardingPubKey) wird in der UI angezeigt und
	// vom Betreiber auf neuen/gehärteten Servern in ~/.ssh/authorized_keys von
	// root hinterlegt; der Private Key liegt AES-GCM-verschlüsselt in
	// OnboardingKeyEnc und wird nie im Klartext ausgegeben.
	OnboardingKeyEnc string `json:"-"`
	OnboardingPubKey string `json:"onboarding_pub_key"`

	// SelfServerDisabled: Der LCM-Host wurde bewusst aus der Verwaltung
	// entfernt und darf sich nicht selbst wieder aufnehmen. Ohne dieses
	// Merkmal käme der Eintrag beim nächsten Paket-Update zurück - das
	// Installationsskript legt die Übergabedatei bei jedem Lauf neu an.
	SelfServerDisabled bool `gorm:"default:false" json:"self_server_disabled"`

	// 2FA-Enforcement: Rollen (kommagetrennt), für die TOTP zwingend ist.
	Require2FARoles string `json:"require_2fa_roles"` // z.B. "admin,manager"

	// PublicBaseURL ist die von außen erreichbare Basis-Adresse dieser
	// Installation (z.B. "https://lcm.example.com"). Sie ist die EINZIGE
	// Quelle für Links in versendeten Mails (Passwort-Reset, Einladung).
	//
	// SICHERHEIT: Früher wurde die Basis aus dem Host-Header des Requests
	// abgeleitet. Damit konnte ein Angreifer einen Passwort-Reset für ein
	// fremdes Konto mit gefälschtem Host-Header anstoßen - das Opfer bekam
	// eine echte LCM-Mail mit einem gültigen Token auf der Domain des
	// Angreifers (Kontoübernahme per Klick). Die Basis darf deshalb NIE aus
	// dem Request stammen. Leer = sicherer Rückfall auf die Adresse aus der
	// Konfiguration (siehe SettingsService.LinkBaseURL).
	PublicBaseURL string `json:"public_base_url"`

	// Standard-E-Mail-Versand (System-Mailer): SMTP-Konfiguration für
	// transaktionale Mails - Passwort-Reset, Einladungs-/Aktivierungslinks
	// und Admin-Hinweise. Getrennt von den Benachrichtigungskanälen; kann
	// über den verwalteten Kanal (Typ system_email) zusätzlich als
	// Benachrichtigungskanal dienen. Das Passwort ist AES-GCM-verschlüsselt.
	MailEnabled     bool   `gorm:"default:false" json:"mail_enabled"`
	MailHost        string `json:"mail_host"`
	MailPort        int    `gorm:"default:587" json:"mail_port"`
	MailUsername    string `json:"mail_username"`
	MailPasswordEnc string `json:"-"` // AES-GCM
	MailFrom        string `json:"mail_from"`
	// MailUseTLS aktiviert STARTTLS beim Versand (Standard bei Port 587).
	MailUseTLS bool `gorm:"default:true" json:"mail_use_tls"`
	// MailAdminRecipients: Empfänger für Admin-Hinweise (kommagetrennt).
	MailAdminRecipients string `json:"mail_admin_recipients"`

	// SSL/TLS: eigenes Zertifikat (PEM). Leer = Self-Signed beim Start.
	TLSCertPEM    string `json:"-"`
	TLSKeyPEMEnc  string `json:"-"` // AES-GCM
	TLSCustomUsed bool   `gorm:"default:false" json:"tls_custom_used"`

	// CrowdSec-Zugang für die Aktion „Sicherheit-Tools": entweder eine
	// self-hosted zentrale LAPI (URL + Maschinen-Login + Passwort) oder ein
	// CrowdSec-Console-Enrollment-Key. Passwort/Key AES-GCM-verschlüsselt; im
	// Formular je Installation wählbar (lokal/remote/console).
	CrowdSecLapiURL         string `json:"crowdsec_lapi_url"`
	CrowdSecLapiLogin       string `json:"crowdsec_lapi_login"`
	CrowdSecLapiPasswordEnc string `json:"-"` // AES-GCM
	CrowdSecConsoleKeyEnc   string `json:"-"` // AES-GCM

	// InstanceID ist die beim ersten Start erzeugte, dauerhafte Kennung
	// DIESER LCM-Installation (UUID). Sie liegt bewusst in der Datenbank
	// und wandert damit im Backup mit: Nach einem Server-Umzug aus dem
	// Backup bleibt es dieselbe Instanz - die Subscription folgt der
	// Installation, nicht dem Blech.
	InstanceID string `json:"instance_id"`

	// Enterprise-Subscription (Support + abgehangener Paketkanal):
	// Der Subscription-Key wird beim Anbieter-Dienst gegen einen
	// instanzgebundenen Repository-Zugangsschlüssel getauscht. Key und
	// Zugangsschlüssel AES-GCM-verschlüsselt; die übrigen Felder sind der
	// zuletzt vom Dienst gemeldete Stand (Anzeige, keine Durchsetzung -
	// Community und Enterprise sind funktional identisch).
	SubscriptionServiceURL   string `json:"subscription_service_url"` // leer = DefaultSubscriptionServiceURL
	SubscriptionKeyEnc       string `json:"-"`                        // AES-GCM
	SubscriptionAccessKeyEnc string `json:"-"`                        // AES-GCM
	SubscriptionRepoURL      string `json:"subscription_repo_url"`
	SubscriptionCustomer     string `json:"subscription_customer"`
	SubscriptionKeyPrefix    string `json:"subscription_key_prefix"`
	// SubscriptionStatus: "" = keine Subscription hinterlegt; sonst
	// active/expired/revoked (vom Dienst) oder unreachable (letzte
	// Prüfung scheiterte am Transport - Aussage unbekannt, nicht schlecht).
	SubscriptionStatus      string     `json:"subscription_status"`
	SubscriptionExpiresAt   string     `json:"subscription_expires_at"` // YYYY-MM-DD, leer = unbefristet
	SubscriptionLastCheckAt *time.Time `json:"subscription_last_check_at"`
	SubscriptionLastError   string     `json:"subscription_last_error"`
	// SubscriptionIncludedServers: das vertraglich enthaltene Server-
	// Kontingent laut Dienst (0 = keins hinterlegt). Reine Anzeige - bei
	// Überschreitung gibt es einen Hinweis, nie eine Sperre.
	SubscriptionIncludedServers int `gorm:"default:0" json:"subscription_included_servers"`
	// SubscriptionAptChannel: der Paketkanal, auf dem der LCM-Host steht -
	// gesetzt nach erfolgreichem Umstell-Job, leer = Community. Nur der
	// Enterprise-Kanal hängt an der Subscription; Community und Beta sind
	// offen (siehe AptChannel*).
	SubscriptionAptChannel string `gorm:"default:'community'" json:"subscription_apt_channel"`
}

// Die Paketkanäle des LCM-Hosts. Community (jedes Release sofort) und Beta
// (Vorabversionen) sind offen; Enterprise (abgehangene Releases) verlangt
// eine aktive Subscription.
const (
	AptChannelCommunity  = "community"
	AptChannelBeta       = "beta"
	AptChannelEnterprise = "enterprise"
)

// ValidAptChannel meldet, ob c einer der drei Kanäle ist.
func ValidAptChannel(c string) bool {
	switch c {
	case AptChannelCommunity, AptChannelBeta, AptChannelEnterprise:
		return true
	}
	return false
}

// AptChannel liefert den gespeicherten Kanal; alles Unbekannte (auch der
// leere Altbestand) gilt als Community - das ist der Auslieferungszustand.
func (g *GlobalSettings) AptChannel() string {
	if ValidAptChannel(g.SubscriptionAptChannel) {
		return g.SubscriptionAptChannel
	}
	return AptChannelCommunity
}

// DefaultSubscriptionServiceURL ist der Standard-Anbieter-Dienst für die
// Enterprise-Subscription (Aktivierung, Lebenszeichen, Paketkanal).
const DefaultSubscriptionServiceURL = "https://repo.techeve.de"

// SubscriptionConfigured meldet, ob eine Subscription hinterlegt ist.
func (g *GlobalSettings) SubscriptionConfigured() bool {
	return g.SubscriptionAccessKeyEnc != ""
}

// SubscriptionServiceBase liefert die effektive Dienst-Basis-URL.
func (g *GlobalSettings) SubscriptionServiceBase() string {
	if u := strings.TrimSpace(g.SubscriptionServiceURL); u != "" {
		return strings.TrimRight(u, "/")
	}
	return DefaultSubscriptionServiceURL
}

// CrowdSecLapiConfigured meldet, ob eine self-hosted LAPI hinterlegt ist
// (URL + Login + Passwort). Für die UI, ohne die Geheimnisse preiszugeben.
func (g *GlobalSettings) CrowdSecLapiConfigured() bool {
	return g.CrowdSecLapiURL != "" && g.CrowdSecLapiLogin != "" && g.CrowdSecLapiPasswordEnc != ""
}

// CrowdSecConsoleConfigured meldet, ob ein Console-Enrollment-Key hinterlegt ist.
func (g *GlobalSettings) CrowdSecConsoleConfigured() bool {
	return g.CrowdSecConsoleKeyEnc != ""
}

// Grenzen für die Aufbewahrung des Speicher-Verlaufs (Tage).
const (
	StorageHistoryRetentionMin = 90
	StorageHistoryRetentionMax = 365
)

// ClampStorageHistoryRetention begrenzt die Aufbewahrung des Speicher-Verlaufs
// auf den erlaubten Bereich (mindestens 90, höchstens 365 Tage). Der Zero-Wert
// (0, z.B. aus einem Alt-Bestand) wird auf den Mindestwert gehoben.
func ClampStorageHistoryRetention(days int) int {
	if days < StorageHistoryRetentionMin {
		return StorageHistoryRetentionMin
	}
	if days > StorageHistoryRetentionMax {
		return StorageHistoryRetentionMax
	}
	return days
}

// Grenzen der erlaubten Stille (Minuten). 0 bedeutet: Watchdog aus.
const (
	JobIdleTimeoutMin = 1
	JobIdleTimeoutMax = 24 * 60
)

// ClampJobIdleTimeout begrenzt die erlaubte Stille auf den zulässigen
// Bereich; 0 (Watchdog aus) bleibt erhalten.
func ClampJobIdleTimeout(minutes int) int {
	if minutes <= 0 {
		return 0
	}
	if minutes < JobIdleTimeoutMin {
		return JobIdleTimeoutMin
	}
	if minutes > JobIdleTimeoutMax {
		return JobIdleTimeoutMax
	}
	return minutes
}

// DefaultCVEHighWeightPackages ist die eingebaute Liste exponierter Pakete,
// deren CVEs höher gewichtet werden, solange der Betreiber keine eigene Liste
// hinterlegt: Webserver, Reverse-Proxies, SSH-/Mail-/DNS-/Datei-Server,
// Datenbanken und andere Dienste, die Ports öffnen oder weitreichenden
// Systemzugriff haben.
const DefaultCVEHighWeightPackages = "nginx, apache2, httpd, lighttpd, caddy, traefik, haproxy, " +
	"openssh-server, dropbear, vsftpd, proftpd, " +
	"postfix, exim4, dovecot, " +
	"bind9, dnsmasq, unbound, " +
	"samba, smbd, nfs-kernel-server, " +
	"mariadb-server, mysql-server, postgresql, redis, memcached, mongodb, " +
	"php-fpm, tomcat9, tomcat10"

// CVEHighWeightList liefert die effektive Hochgewichtungs-Liste als
// normalisierte (kleingeschriebene) Paketnamen. Leeres Feld = Standardliste,
// ein einzelnes "-" = bewusst deaktiviert.
func (g *GlobalSettings) CVEHighWeightList() []string {
	raw := strings.TrimSpace(g.CVEHighWeightPackages)
	if raw == "" {
		raw = DefaultCVEHighWeightPackages
	}
	if raw == "-" {
		return nil
	}
	var list []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	}) {
		if p := strings.ToLower(strings.TrimSpace(part)); p != "" && p != "-" {
			list = append(list, p)
		}
	}
	return list
}

// DefaultDNSServerPresets sind die eingebauten Vorgabe-Nameserver, solange der
// Betreiber keine eigene Liste pflegt.
const DefaultDNSServerPresets = "Cloudflare = 1.1.1.1\n" +
	"Cloudflare (2) = 1.0.0.1\n" +
	"Google = 8.8.8.8\n" +
	"Google (2) = 8.8.4.4\n" +
	"Quad9 = 9.9.9.9"

// DefaultDNSTestDomains sind die eingebauten Test-Domains für den
// DNS-Verfügbarkeitstest, solange der Betreiber keine eigenen pflegt.
const DefaultDNSTestDomains = "deb.debian.org\ngithub.com\ncloudflare.com"

// Hinweis: das Parsen der Vorgabe-Nameserver („Label = IP" je Zeile) macht
// AUSSCHLIESSLICH das Frontend (parseDnsPresets in ServerDetail.svelte) -
// das Backend reicht DNSServerPresets nur als Rohtext durch.

// DefaultNTPServerPresets sind die eingebauten Vorgabe-Zeitserver. Der
// NTP-Pool ist bewusst die Vorgabe: er ist geografisch verteilt, ohne
// Anmeldung nutzbar und in jeder Distribution der Normalfall.
const DefaultNTPServerPresets = "NTP-Pool (1) = 0.pool.ntp.org\n" +
	"NTP-Pool (2) = 1.pool.ntp.org\n" +
	"NTP-Pool (3) = 2.pool.ntp.org\n" +
	"NTP-Pool (4) = 3.pool.ntp.org\n" +
	"Cloudflare = time.cloudflare.com\n" +
	"Google = time.google.com"

// NTPServerPresetsOrDefault liefert die gepflegten Vorgabe-Zeitserver, sonst
// die eingebaute Liste. Das Parsen der „Label = Host"-Zeilen macht wie bei
// den Nameservern das Frontend.
func (g *GlobalSettings) NTPServerPresetsOrDefault() string {
	if raw := strings.TrimSpace(g.NTPServerPresets); raw != "" {
		return raw
	}
	return DefaultNTPServerPresets
}

// DefaultBackupTime ist die Vorgabe-Uhrzeit des automatischen Backups -
// nachts, nach der üblichen Log-Bereinigung und vor dem Arbeitstag.
const DefaultBackupTime = "03:30"

// BackupTimeOrDefault liefert die Anker-Uhrzeit des Backup-Zeitplans (HH:MM);
// leeres Feld = DefaultBackupTime.
func (g *GlobalSettings) BackupTimeOrDefault() string {
	if t := strings.TrimSpace(g.BackupTime); t != "" {
		return t
	}
	return DefaultBackupTime
}

// DNSTestDomainList liefert die zu prüfenden Test-Domains. Leeres Feld =
// Standardliste.
func (g *GlobalSettings) DNSTestDomainList() []string {
	raw := strings.TrimSpace(g.DNSTestDomains)
	if raw == "" {
		raw = DefaultDNSTestDomains
	}
	var list []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	}) {
		if p := strings.TrimSpace(part); p != "" {
			list = append(list, p)
		}
	}
	return list
}

// Requires2FA prüft, ob für eine der übergebenen Rollen 2FA erzwungen wird.
func (g *GlobalSettings) Requires2FA(roleNames []string) bool {
	if g.Require2FARoles == "" {
		return false
	}
	required := map[string]bool{}
	for _, r := range strings.Split(g.Require2FARoles, ",") {
		required[strings.TrimSpace(r)] = true
	}
	for _, r := range roleNames {
		if required[r] {
			return true
		}
	}
	return false
}

// Backup ist ein erstelltes System-Backup (Datenbank + Konfiguration).
type Backup struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	FileName  string `gorm:"not null" json:"file_name"`
	SizeBytes int64  `json:"size_bytes"`
	Trigger   string `json:"trigger"` // "scheduler" oder Username
}

// AdvisoryCacheTTL liefert die wirksame Cache-Gültigkeit der Frühwarnung in
// Minuten. 0 bleibt 0 (Zwischenspeicher aus); alles darüber wird auf den
// erlaubten Bereich begrenzt - auch ein per API gesetzter Fantasiewert kann
// die Frühwarnung damit nicht über AdvisoryCacheTTLMax hinaus blind machen.
func (g *GlobalSettings) AdvisoryCacheTTL() int {
	return ClampAdvisoryCacheTTL(g.AdvisoryCacheTTLMinutes)
}

// ClampAdvisoryCacheTTL begrenzt die Cache-Gültigkeit auf {0} ∪ [Min, Max].
//
// 0 bleibt 0 - „aus" ist eine gültige Entscheidung. Ein Wert dazwischen ist
// dagegen keine: Er schaltet den Zwischenspeicher ein, lässt ihn aber vor dem
// nächsten Durchgang ablaufen. Statt dieser wirkungslosen Einstellung gilt
// der kleinste Wert, der tatsächlich etwas bewirkt.
func ClampAdvisoryCacheTTL(minutes int) int {
	if minutes <= 0 {
		return 0
	}
	if minutes < AdvisoryCacheTTLMin {
		return AdvisoryCacheTTLMin
	}
	if minutes > AdvisoryCacheTTLMax {
		return AdvisoryCacheTTLMax
	}
	return minutes
}
