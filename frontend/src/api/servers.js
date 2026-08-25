/**
 * ServersApi - Server-Onboarding, Monitoring, Härtung.
 */
export class ServersApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  list() {
    return this.#client.get('/servers');
  }

  get(id) {
    return this.#client.get(`/servers/${id}`);
  }

  status(id) {
    return this.#client.get(`/servers/${id}/status`);
  }

  /** Server-Einstellungen ändern (z. B. { user_sync_disabled: true }). */
  updateSettings(id, data) {
    return this.#client.patch(`/servers/${id}/settings`, data);
  }

  /** Aktuell laufender Job des Servers ({job: …|null}) - für die Job-Sperre in der UI. */
  activeJob(id) {
    return this.#client.get(`/servers/${id}/active-job`);
  }

  packages(id) {
    return this.#client.get(`/servers/${id}/packages`);
  }

  /** Alle Snaps aktualisieren - startet einen Job. */
  refreshAllSnaps(id) {
    return this.#client.post(`/servers/${id}/snaps/refresh-all`);
  }

  /** Einzelne Snaps aktualisieren - startet einen Job. */
  refreshSnaps(id, names) {
    return this.#client.post(`/servers/${id}/snaps/refresh`, { names });
  }

  /** Snaps entfernen - snapd und die core-Basen werden abgelehnt. */
  removeSnaps(id, names) {
    return this.#client.post(`/servers/${id}/snaps/remove`, { names });
  }

  snaps(id) {
    return this.#client.get(`/servers/${id}/snaps`);
  }

  outdatedPackages(id) {
    return this.#client.get(`/servers/${id}/outdated-packages`);
  }

  /** Verlauf der Festplattenbelegung (Tagesdurchschnitte) + aktueller Stand. */
  storageHistory(id) {
    return this.#client.get(`/servers/${id}/storage-history`);
  }

  /** CVE-Scan-Bericht (Trivy): Funde, Zusammenfassung, Scan-Metadaten. */
  vulnerabilities(id) {
    return this.#client.get(`/servers/${id}/vulnerabilities`);
  }

  /** CVE-Scan für diesen Server anstoßen - startet einen Job. */
  scanVulnerabilities(id) {
    return this.#client.post(`/servers/${id}/vulnerabilities/scan`);
  }

  /** Docker-Inventar: Container (mit Compose-Zuordnung), Images, CVE-Zähler. */
  docker(id) {
    return this.#client.get(`/servers/${id}/docker`);
  }

  /** Docker-Inventar neu einlesen - startet einen Job. */
  refreshDocker(id) {
    return this.#client.post(`/servers/${id}/docker/refresh`);
  }

  /** Compose-Projekt aktualisieren (pull && up -d) - startet einen Job. */
  composeUpdate(id, project, service = '') {
    return this.#client.post(`/servers/${id}/docker/compose-update`, { project, service });
  }

  /** Neueste Version eines Image-Tags ziehen (ohne Container-Neustart). */
  dockerPull(id, image) {
    return this.#client.post(`/servers/${id}/docker/pull`, { image });
  }

  /** Alle genutzten, getaggten Registry-Images aktualisieren (ein Job). */
  dockerPullAll(id) {
    return this.#client.post(`/servers/${id}/docker/pull-all`);
  }

  /** Container als CVE-relevant markieren bzw. Markierung aufheben. */
  dockerCveRelevance(id, name, relevant) {
    return this.#client.post(`/servers/${id}/docker/cve-relevance`, { name, relevant });
  }

  /** Ungenutztes Image löschen (docker rmi) - startet einen Job. */
  dockerRemoveImage(id, image) {
    return this.#client.post(`/servers/${id}/docker/remove-image`, { image });
  }

  /** Alle ungenutzten Images aufräumen (docker image prune -af) - startet einen Job. */
  dockerPrune(id) {
    return this.#client.post(`/servers/${id}/docker/prune`);
  }

  /** Installierbare Versionen eines Pakets (neueste zuerst). */
  packageVersions(id, name) {
    return this.#client.get(`/servers/${id}/packages/${encodeURIComponent(name)}/versions`);
  }

  /** Paketliste aktualisieren (apt-get update + Bestandsaufnahme) - startet einen Job. */
  refreshPackages(id) {
    return this.#client.post(`/servers/${id}/packages/refresh`);
  }

  /** Hardware-/OS-Daten neu auslesen (kein Upgrade) - startet einen Job. */
  refreshHardware(id) {
    return this.#client.post(`/servers/${id}/refresh-hardware`);
  }

  /** Alles neu auslesen: Hardware, Pakete, Docker, Speicher, Firewall/SSH - startet einen Job. */
  refreshAll(id) {
    return this.#client.post(`/servers/${id}/refresh-all`);
  }

  /** Server neu starten - startet einen Job (braucht vollen Root-Zugriff). */
  reboot(id) {
    return this.#client.post(`/servers/${id}/reboot`);
  }

  /** LCM-Host: Einrichtungsstatus (Trivy, apt-cacher-ng). */
  lcmHostStatus(id) {
    return this.#client.get(`/servers/${id}/lcm-host/status`);
  }

  /** LCM-Host: Trivy installieren & einrichten - startet einen Job. */
  installTrivy(id) {
    return this.#client.post(`/servers/${id}/lcm-host/install-trivy`);
  }

  /** LCM-Host: apt-cacher-ng installieren & einrichten - startet einen Job. */
  installAptCacher(id) {
    return this.#client.post(`/servers/${id}/lcm-host/install-apt-cacher`);
  }


  /** apt-cacher-ng: Dienst neu starten - startet einen Job. */
  restartAptCacher(id) {
    return this.#client.post(`/servers/${id}/apt-cache/restart`);
  }

  /** apt-cacher-ng: permanentes Caching (automatischen Ablauf-Job) an-/ausschalten. */
  setAptCacherPermanentCache(id, enabled) {
    return this.#client.post(`/servers/${id}/apt-cache/permanent-cache`, { enabled });
  }

  /** Bis zu drei Nameserver setzen (leere Liste = LCM-DNS-Verwaltung entfernen). */
  configureDNS(id, servers) {
    return this.#client.post(`/servers/${id}/dns`, { servers });
  }

  /** DNS-Verfügbarkeitstest: prüft die gepflegten Test-Domains auf dem Server. */
  dnsTest(id) {
    return this.#client.post(`/servers/${id}/dns-test`);
  }

  /** Deep Scan (Kernel-Reboot-Lücke, Kernel-CVEs, Härtungs-Audit) starten - Job. */
  deepScan(id) {
    return this.#client.post(`/servers/${id}/deep-scan`);
  }

  /** Lesesicht des letzten Deep Scans (Befunde, Kernel-CVEs, Härtungs-Index). */
  deepScanReport(id) {
    return this.#client.get(`/servers/${id}/deep-scan`);
  }

  /** Zeitzone, Zeitdienst und Uhrenversatz frisch auslesen (rein lesend auf dem Ziel). */
  timeCheck(id) {
    return this.#client.post(`/servers/${id}/time-check`);
  }

  /** Zeitzone des Servers setzen (wird zur Bestätigung zurückgelesen). */
  setTimezone(id, timezone) {
    return this.#client.post(`/servers/${id}/timezone`, { timezone });
  }

  /** Zeitserver eintragen und die Synchronisierung belegen lassen. */
  configureNTP(id, servers) {
    return this.#client.post(`/servers/${id}/ntp`, { servers });
  }

  /** Ein einzelner, datierter Deep-Scan-Lauf samt seiner Befunde. */
  deepScanReportDetail(id, reportId) {
    return this.#client.get(`/servers/${id}/deep-scan/reports/${reportId}`);
  }

  /** needrestart + lynis auf dem Server installieren (für den Voll-Scan) - Job. */
  // Installiert das Paket „acl" - ohne das bleiben die Verzeichnisrechte der
  // Berechtigungsprofile auf diesem Server wirkungslos.
  hardenedPaths(id) {
    return this.#client.get(`/servers/${id}/hardened-paths`);
  }

  hardenSuggestions(id) {
    return this.#client.get(`/servers/${id}/harden-suggestions`);
  }

  // Mehrere Verzeichnisse in EINER Verbindung abschotten.
  hardenPathsBulk(id, targets) {
    return this.#client.post(`/servers/${id}/hardened-paths/bulk`, { targets });
  }

  hardenPath(id, path, group, unit) {
    return this.#client.post(`/servers/${id}/hardened-paths`, { path, group, unit });
  }

  restorePath(id, pathId) {
    return this.#client.delete(`/servers/${id}/hardened-paths/${pathId}`);
  }

  installACLSupport(id) {
    return this.#client.post(`/servers/${id}/acl/install`);
  }

  installDeepScanTools(id) {
    return this.#client.post(`/servers/${id}/deep-scan/install-tools`);
  }

  /**
   * Sicherheits-Tool (fail2ban/CrowdSec) installieren & einrichten - Job.
   * opts: {tool, allowlist_ips, bouncer, collections, lapi_mode}.
   */
  configureSecurityTool(id, opts) {
    return this.#client.post(`/servers/${id}/security-tool`, opts);
  }

  /**
   * Ein bereits installiertes Sicherheits-Tool bedienen - Job.
   * opts: {tool, action, unban_ip?, allowlist_ips?, allowlist_ids?}
   * action: start|stop|restart|enable|disable|uninstall|allowlist|unban
   */
  manageSecurityTool(id, opts) {
    return this.#client.post(`/servers/${id}/security-tool/manage`, opts);
  }

  /**
   * Aktuelle Sperren eines Sicherheits-Tools - synchron, kein Job:
   * Wer sich selbst ausgesperrt hat, soll die Liste sofort sehen.
   */
  securityToolBans(id, tool) {
    return this.#client.get(`/servers/${id}/security-tool/bans?tool=${encodeURIComponent(tool)}`);
  }

  /** Einen LCM-Linux-Benutzer diesem Server zuordnen (verteilt sofort). */
  assignLinuxUser(id, linuxUserId) {
    return this.#client.post(`/servers/${id}/assign-user`, { linux_user_id: linuxUserId });
  }

  /** SSH-2FA (TOTP neben dem SSH-Key) aktivieren/entfernen - Job. */
  configureSSH2FA(id, enable) {
    return this.#client.post(`/servers/${id}/ssh-2fa`, { enable });
  }

  /** Die beim Scan erfassten anmeldefähigen Linux-Konten des Servers. */
  serverUsers(id) {
    return this.#client.get(`/servers/${id}/users`);
  }

  /** Sandbox des CVE-Scanners nachrüsten (bubblewrap) - Job. */
  installSandbox(id) {
    return this.#client.post(`/servers/${id}/lcm-host/install-sandbox`);
  }

  /** Offene Benutzer-Abgleiche (Rückstand nicht erreichbarer Server). */
  pendingUserSyncs(id) {
    return this.#client.get(`/servers/${id}/users/pending`);
  }

  /** Linux-Konten jetzt frisch über SSH erheben. */
  refreshServerUsers(id) {
    return this.#client.post(`/servers/${id}/users/refresh`);
  }

  /** Konto auf dem Zielsystem deaktivieren (Passwort gesperrt + abgelaufen). */
  disableServerUser(id, username) {
    return this.#client.post(`/servers/${id}/users/disable`, { username });
  }

  /** Deaktiviertes Konto wieder freigeben. */
  enableServerUser(id, username) {
    return this.#client.post(`/servers/${id}/users/enable`, { username });
  }

  /** Anmelde-Historie eines Kontos (aus wtmp). */
  serverUserLogins(id, username) {
    return this.#client.get(`/servers/${id}/users/${encodeURIComponent(username)}/logins`);
  }

  /** Konto ENDGÜLTIG vom Zielsystem entfernen (samt Home-Verzeichnis). */
  removeServerUser(id, username) {
    return this.#client.post(`/servers/${id}/users/remove`, { username });
  }

  /** Alle Pakete aktualisieren (apt upgrade) - startet einen Job. */
  upgradeAllPackages(id) {
    return this.#client.post(`/servers/${id}/packages/upgrade-all`);
  }

  /** Nur Security-/Bugfix-Updates einspielen - startet einen Job. */
  upgradeSecurityPackages(id) {
    return this.#client.post(`/servers/${id}/packages/upgrade-security`);
  }

  /** Gezielt Pakete aktualisieren. data: {names: [...], version?: ""}. */
  updatePackages(id, data) {
    return this.#client.post(`/servers/${id}/packages/update`, data);
  }

  /**
   * Alte Kernel samt Begleitpaketen entfernen - startet einen Job. Was stehen
   * bleibt (laufender Kernel, neuere, eine Rückfallebene), entscheidet der
   * Server; der Aufruf braucht keine Liste.
   */
  removeOldKernels(id) {
    return this.#client.post(`/servers/${id}/kernels/cleanup`);
  }

  /** Nicht mehr benötigte Pakete entfernen (apt autoremove) - startet einen Job. */
  autoremovePackages(id) {
    return this.#client.post(`/servers/${id}/packages/autoremove`);
  }

  /** Gezielt Pakete entfernen. data: {names: [...]}. Startet einen Job. */
  removePackages(id, names) {
    return this.#client.post(`/servers/${id}/packages/remove`, { names });
  }

  /**
   * Paket-Pins: schützen Pakete vor dem Aufräumen (Autoremove) und frieren
   * optional ihre Version ein. Liefert globale und serverspezifische Pins,
   * die Verfügbarkeit (auf Proxmox ausgenommen) und den Kernel-Vorschlag.
   */
  packagePins(id) {
    return this.#client.get(`/servers/${id}/packages/pins`);
  }

  /** Pin anlegen. data: {name, no_remove, hold, note, global}. */
  createPackagePin(id, data) {
    return this.#client.post(`/servers/${id}/packages/pins`, data);
  }

  /** Pin entfernen. */
  deletePackagePin(id, pinId) {
    return this.#client.delete(`/servers/${id}/packages/pins/${pinId}`);
  }

  /** Ein-Klick-Kernelschutz (mehrere Kernel behalten). {global: bool}. */
  pinKernelPreset(id, global = false) {
    return this.#client.post(`/servers/${id}/packages/pins/kernel`, { global });
  }

  /** Pins auf dem Server festschreiben - startet einen Job. */
  applyPackagePins(id) {
    return this.#client.post(`/servers/${id}/packages/pins/apply`);
  }

  repositories(id) {
    return this.#client.get(`/servers/${id}/repositories`);
  }

  /** SSH-Protokoll-Sessions eines Servers (ohne Kommando-Ausgaben). */
  sshSessions(id, limit) {
    return this.#client.get(`/servers/${id}/ssh-sessions${limit ? `?limit=${limit}` : ''}`);
  }

  /** Eine einzelne SSH-Session mit allen Kommandos. */
  sshSession(sessionId) {
    return this.#client.get(`/ssh-sessions/${sessionId}`);
  }

  /** Katalog der bekannten Paketquellen (Docker, PostgreSQL, …). */
  knownRepos() {
    return this.#client.get('/servers/known-repos');
  }

  /** Benannte IP-Allowlists (Auswahl in Firewall-Regeln/Security-Tools). */
  ipAllowlists() {
    return this.#client.get('/ip-allowlists');
  }

  /** CrowdSec-LAPI-Server auf dem LCM-Host einrichten (optional mit Bouncer). */
  installCrowdSecLapi(id, { bouncer } = {}) {
    return this.#client.post(`/servers/${id}/lcm-host/install-crowdsec-lapi`, { bouncer: !!bouncer });
  }

  /** Alle http-Quellen auf https umstellen (mit serverseitigem Rollback). */
  secureRepositories(id) {
    return this.#client.post(`/servers/${id}/repositories/secure`);
  }

  /**
   * Paketquellen wieder auf http zurückstellen - nur die, die vor der
   * LCM-Umstellung http waren. Ohne uris alle Kandidaten des Servers.
   */
  revertRepositoriesHTTPS(id, uris) {
    return this.#client.post(`/servers/${id}/repositories/revert-https`, { uris: uris ?? [] });
  }

  /**
   * Anwendungen dieses Servers, die nicht aus der Paketverwaltung stammen -
   * erkannte Katalog-Einträge und Dienste ohne Paketzugehörigkeit.
   */
  apps(id) {
    return this.#client.get(`/servers/${id}/apps`);
  }

  /**
   * Sicherung oder Update einer erkannten Anwendung anstoßen. Beim Update
   * läuft die hinterlegte Sicherung vorweg, sofern nicht abgewählt.
   */
  runAppAction(id, slug, action, withBackup = true) {
    return this.#client.post(`/servers/${id}/apps/${slug}/${action}`, { with_backup: withBackup });
  }

  /** Bekannte Paketquelle aus dem Katalog einrichten (GPG-Key + Quelle). */
  addRepository(id, key) {
    return this.#client.post(`/servers/${id}/repositories/add`, { key });
  }

  /** Schritt 1 des Onboardings: Host-Key-Fingerprint auslesen. */
  probe(host, port) {
    return this.#client.post('/servers/probe', { host, port });
  }

  /** Schritt 2: Server joinen (nach Fingerprint-Bestätigung). */
  join(data) {
    return this.#client.post('/servers/join', data);
  }

  /** Bestehenden Server neu verbinden - überschreibt die Credentials. */
  reconnect(id, data) {
    return this.#client.post(`/servers/${id}/reconnect`, data);
  }

  /**
   * LCM Remote: Agent-Server anlegen - liefert {server, token}. Das
   * Enrollment-Token wird EINMALIG angezeigt (at rest nur als Hash).
   */
  createAgent(data) {
    return this.#client.post('/servers/agent', data);
  }

  /**
   * MikroTik RouterOS: Gerät zur reinen Überwachung anlegen. Passwort-Modus
   * verbindet sofort und scannt; Key-Modus liefert {server, public_key} zum
   * Import auf dem Router.
   */
  createRouterOS(data) {
    return this.#client.post('/servers/routeros', data);
  }

  /**
   * Synology DSM: Zertifikats-Fingerprint lesen (Trust-on-First-Use), BEVOR
   * Zugangsdaten übertragen werden - liefert {cert_fingerprint}.
   */
  probeDSM(host, port) {
    return this.#client.post('/servers/dsm/probe', { host, port });
  }

  /**
   * Synology DSM: Gerät zur Überwachung über die DSM-Web-API anlegen.
   * Verbindet sofort, erhebt den Zustand und legt es online an.
   */
  createDSM(data) {
    return this.#client.post('/servers/dsm', data);
  }

  /** LCM Remote: Agent-Token ersetzen (Verlust/Kompromittierung) - {token}. */
  regenerateAgentToken(id) {
    return this.#client.post(`/servers/${id}/agent-token`);
  }

  syncUsers(id) {
    return this.#client.post(`/servers/${id}/sync-users`);
  }

  hardenSsh(id) {
    return this.#client.post(`/servers/${id}/harden-ssh`);
  }

  /** SSH-Härtung aufheben - stellt die ursprüngliche sshd-Konfiguration wieder her. */
  unhardenSsh(id) {
    return this.#client.post(`/servers/${id}/unharden-ssh`);
  }

  /** Direktes root-SSH-Login sperren/erlauben (PermitRootLogin no). */
  setSshRootLogin(id, disabled) {
    return this.#client.post(`/servers/${id}/ssh-root-login`, { disabled });
  }

  /** LCM-Benutzer nachträglich auf die sudo-Whitelist einschränken (Einweg!). */
  restrictSudo(id) {
    return this.#client.post(`/servers/${id}/restrict-sudo`);
  }

  /** SSH-Port umstellen (erst verifizieren, dann übernehmen - kein Aussperren). */
  changeSshPort(id, port) {
    return this.#client.post(`/servers/${id}/ssh-port`, { port });
  }

  /**
   * Firewall konfigurieren (asynchroner Job): enable an/aus, rules = Regel-
   * Objekte {port, proto, ip_version, allowlist_ids, source_ips, comment},
   * sshSources = Quell-Einschränkung der stets erzwungenen SSH-Freigabe.
   * Antwort: {job_id, job_name}.
   */
  configureFirewall(id, enable, rules, sshSources = null) {
    return this.#client.post(`/servers/${id}/firewall`, {
      enable,
      rules,
      ...(sshSources ? { ssh_sources: sshSources } : {}),
    });
  }

  /**
   * Lauschende Dienste sofort vom Server einlesen (ss/netstat) - die Grundlage
   * der Port-Vorschläge im Firewall-Dialog. Antwort: {listening_ports}.
   */
  scanListeningPorts(id) {
    return this.#client.post(`/servers/${id}/listening-ports/scan`, {});
  }

  /** APT-Anfragen des Servers über den zentralen APT-Cache leiten (oder trennen). */
  configureAptProxy(id, enable) {
    return this.#client.post(`/servers/${id}/apt-proxy`, { enable });
  }

  rotateKey(id) {
    return this.#client.post(`/servers/${id}/rotate-key`);
  }

  /** Server entfernen. purge=true bereinigt zuvor den Zielserver. */
  decommission(id, purge = false) {
    return this.#client.post(`/servers/${id}/decommission`, { purge });
  }
}
