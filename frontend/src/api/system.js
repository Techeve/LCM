/**
 * SystemApi - Systeminfos, globale Scheduler-Übersicht, Backups,
 * globale Einstellungen und Paket-Übersichten.
 */
export class SystemApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  /** Version, Build, Uptime (öffentlich). */
  info() {
    return this.#client.get('/system/info');
  }

  /** Update-Status: installierte vs. neueste bekannte Version (fürs Banner). */
  /** Update-Prüfung sofort auslösen und den frischen Stand zurückgeben. */
  checkUpdateNow() {
    return this.#client.post('/system/update-check', {});
  }

  updateInfo() {
    return this.#client.get('/system/update-info');
  }

  /** Kann sich LCM hier selbst aktualisieren, und wie weit ist es? */
  selfUpdateStatus() {
    return this.#client.get('/system/self-update');
  }

  /**
   * Selbst-Update anfordern. Kehrt sofort zurück: LCM wartet danach, bis kein
   * Job mehr läuft, sichert, spielt das Paket ein und startet sich neu.
   *
   * withBackup=false überspringt die Sicherung - die Vorgabe ist bewusst
   * anders: Wer sein eigenes Verwaltungssystem aktualisiert, hat im Fehlerfall
   * kein zweites, das ihm hilft.
   */
  startSelfUpdate(withBackup = true) {
    return this.#client.post('/system/self-update', { with_backup: withBackup });
  }

  // ---- Scheduler ----
  schedulesOverview() {
    return this.#client.get('/system/schedules/overview');
  }

  /** System-Schedule (backup|cleanup) manuell auslösen. */
  triggerSystemSchedule(kind) {
    return this.#client.post(`/system/schedules/kind/${kind}/trigger-now`);
  }

  // ---- Backups ----
  listBackups() {
    return this.#client.get('/system/backups');
  }

  /** Backup jetzt erstellen. Ohne Passphrase greift LCM_BACKUP_PASSPHRASE. */
  triggerBackup(passphrase) {
    return this.#client.post('/system/backups/trigger-now', passphrase ? { passphrase } : undefined);
  }

  configureBackups(data) {
    return this.#client.patch('/system/backups/settings', data);
  }

  /** Ein Backup dauerhaft löschen (Datei + Metadaten). */
  deleteBackup(name) {
    return this.#client.delete(`/system/backups/${encodeURIComponent(name)}`);
  }

  /** Ein verschlüsseltes Backup-Archiv herunterladen. */
  downloadBackup(name) {
    return this.#client.download(`/system/backups/${encodeURIComponent(name)}/download`, name);
  }

  /** Wiederherstellung aus einem Backup der Historie vorbereiten (Rollback). */
  restoreBackup(name, passphrase) {
    return this.#client.post(`/system/backups/${encodeURIComponent(name)}/restore`, { passphrase });
  }

  /** Wiederherstellung aus einer hochgeladenen .lcmbak-Datei vorbereiten. */
  restoreUpload(file, passphrase) {
    const fd = new FormData();
    fd.append('archive', file);
    fd.append('passphrase', passphrase ?? '');
    return this.#client.upload('/system/backups/restore-upload', fd);
  }

  // ---- Globale Einstellungen ----
  getSettings() {
    return this.#client.get('/settings/global');
  }

  updateSettings(data) {
    return this.#client.patch('/settings/global', data);
  }

  /** MCP-Schnittstelle an-/abschalten + Bind-Adresse/Port setzen. */
  setMCP(data) {
    return this.#client.put('/settings/mcp', data);
  }


  /**
   * Übersicht für die zentrale APT-Cache-Seite: URL, Erreichbarkeit,
   * Transfer-Statistiken und - falls apt-cacher-ng auf dem LCM-Host läuft -
   * dessen Verwaltbarkeit (managed, server_id, permanent_cache, disk_usage).
   */
  aptCacheOverview() {
    return this.#client.get('/settings/apt-cache/overview');
  }

  /**
   * Erreichbarkeits-Check der konfigurierten CrowdSec-LAPI (Login-Probe vom
   * LCM-Host aus): {configured, reachable, running, http_status, message}.
   */
  crowdsecStatus() {
    return this.#client.get('/settings/crowdsec/status');
  }

  // ---- Enterprise-Subscription ----
  /** Subscription-Status: {configured, customer, status, days_left, apt_channel, …}. */
  subscriptionStatus() {
    return this.#client.get('/subscription');
  }

  /** Key beim Anbieter-Dienst aktivieren (optional mit eigener Dienst-URL). */
  subscriptionActivate(subscriptionKey, serviceUrl) {
    return this.#client.post('/subscription/activate', {
      subscription_key: subscriptionKey,
      service_url: serviceUrl ?? '',
    });
  }

  /** Manuelles Lebenszeichen - holt den aktuellen Vertragsstand. */
  subscriptionVerify() {
    return this.#client.post('/subscription/verify');
  }

  /** Subscription lokal entfernen (Bindung beim Anbieter bleibt). */
  subscriptionRemove() {
    return this.#client.delete('/subscription');
  }

  /** LCM-Host per Job auf einen Paketkanal umstellen: community | beta | enterprise. */
  subscriptionAptChannel(channel) {
    return this.#client.post('/subscription/apt', { channel });
  }

  // ---- Standard-E-Mail-Versand ----
  /** Testnachricht an die Admin-Empfänger des System-Mailers. */
  testMail() {
    return this.#client.post('/settings/global/test-mail');
  }

  /** Zustand der Checkbox „als Benachrichtigungskanal anbieten". */
  getMailChannel() {
    return this.#client.get('/settings/mail-channel');
  }

  /** Verwalteten System-E-Mail-Kanal an-/abschalten. */
  setMailChannel(enabled) {
    return this.#client.put('/settings/mail-channel', { enabled });
  }

  // ---- Katalog bekannter Paketquellen ----
  knownRepos() {
    return this.#client.get('/settings/known-repos');
  }

  /** Katalog-Eintrag anlegen (ohne id) oder aktualisieren (mit id). */
  saveKnownRepo(data) {
    return this.#client.post('/settings/known-repos', data);
  }

  deleteKnownRepo(id) {
    return this.#client.delete(`/settings/known-repos/${id}`);
  }

  // ---- IP-Allowlists (gemeinsamer Pool) ----
  ipAllowlists() {
    return this.#client.get('/settings/ip-allowlists');
  }

  /** Allowlist anlegen (ohne id) oder aktualisieren (mit id). */
  saveIPAllowlist(data) {
    return this.#client.post('/settings/ip-allowlists', data);
  }

  deleteIPAllowlist(id) {
    return this.#client.delete(`/settings/ip-allowlists/${id}`);
  }

  /**
   * Globale CVE-Übersicht (Trivy), seitenweise. Antwort:
   * {items, total, page, page_size, summary}. source filtert die Quelle:
   * 'os' (nur nativ installiert, Docker-CVEs ausgeblendet), 'docker' oder ''.
   */
  vulnerabilities(page = 1, pageSize = 50, source = '') {
    const src = source ? `&source=${encodeURIComponent(source)}` : '';
    return this.#client.get(`/security/vulnerabilities?page=${page}&page_size=${pageSize}${src}`);
  }

  /** Fortschritt des laufenden „Alle VMs aktualisieren"-Laufs. */
  bulkUpdateStatus() {
    return this.#client.get('/security/update-all');
  }

  /** Startet Security-Updates auf allen erreichbaren Servern (Sammel-Lauf). */
  startBulkUpdate() {
    return this.#client.post('/security/update-all', {});
  }

  /**
   * Stand des CVE-Scanners: Version und Schwachstellen-Datenbank
   * ({available, version, updated_at, next_update, freshness, age_hours, error}).
   * freshness: fresh | stale | critical | unknown.
   */
  scannerStatus() {
    return this.#client.get('/security/scanner');
  }

  /**
   * Frühwarn-Befunde der Online-Quellen (OSV), jüngste zuerst. Antwort:
   * {items, enabled}. enabled=false heißt: Die Frühwarnung ist gar nicht
   * eingeschaltet - dann sagt eine leere Liste nichts über den Zustand aus.
   * withResolved=true nimmt behobene Befunde mit (Verlauf).
   */
  advisories({ page = 1, pageSize = 100, withResolved = false, minSeverity = '' } = {}) {
    const q = new URLSearchParams({ page, page_size: pageSize });
    if (withResolved) q.set('resolved', '1');
    if (minSeverity) q.set('min_severity', minSeverity);
    return this.#client.get(`/security/advisories?${q}`);
  }

  /** Betriebszustand der Frühwarnung ohne die Fundliste. */
  advisoryStatus() {
    return this.#client.get('/security/advisories/status');
  }

  /** Durchgang sofort anstoßen, statt auf den Viertelstundentakt zu warten. */
  advisoryPoll() {
    return this.#client.post('/security/advisories/poll', {});
  }

  /** Trefferquote und Belegung beider Zwischenspeicher. */
  cacheStats() {
    return this.#client.get('/security/caches');
  }

  /** Lokale OSV-Kopie sofort spiegeln. */
  advisoryMirror() {
    return this.#client.post('/security/advisories/mirror', {});
  }

  /** Befund zur Kenntnis nehmen - er löst danach keinen Alarm mehr aus. */
  acknowledgeAdvisory(id) {
    return this.#client.post(`/security/advisories/${encodeURIComponent(id)}/acknowledge`, {});
  }

  /** Schwachstellen-Datenbank nachladen - startet einen Job. */
  updateScannerDB() {
    return this.#client.post('/security/scanner/update-db', {});
  }

  /** Alle Container der sichtbaren Server, laufende zuerst. */
  dockerContainers() {
    return this.#client.get('/docker/containers');
  }

  /** Compose-Projekte über alle sichtbaren Server hinweg. */
  dockerCompose() {
    return this.#client.get('/docker/compose');
  }

  /** Globale Docker-Übersicht: unique Images mit Update-/CVE-Status. */
  dockerOverview() {
    return this.#client.get('/docker/overview');
  }
}
