<script>
  // Globale Sicherheits-Übersicht: alle per Trivy gefundenen CVEs über die
  // sichtbaren Server hinweg, kritischste zuerst.
  import { onDestroy } from 'svelte';
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  import { toasts } from '../stores/toast.svelte.js';
  import { waitForJob } from '../lib/jobs.js';
  import Pagination from '../components/Pagination.svelte';
  import AdvisoryList from '../components/AdvisoryList.svelte';
  import CacheStats from '../components/CacheStats.svelte';
  import SandboxBadge from '../components/SandboxBadge.svelte';
  import { severityBadge, severityLabel, fmtNum } from '../lib/format.js';

  const t = (k, p) => i18n.t(k, p);

  // Zwei Sichten auf dieselbe Frage „was ist unsicher": der taegliche
  // Trivy-Scan (gruendlich, distro-genau) und die Fruehwarnung (Minuten
  // alt, kennt Schadpakete). Getrennt, weil sie unterschiedlich schnell
  // und unterschiedlich vollstaendig sind - vermischt waere keiner der
  // beiden Werte mehr interpretierbar.
  let tab = $state('cve');

  const PAGE_SIZE = 50;
  let rows = $state([]);
  let error = $state('');
  let loaded = $state(false);
  let page = $state(1);
  let total = $state(0);
  // Docker-CVEs ausblenden (nur nativ installierte anzeigen).
  let hideDocker = $state(false);
  // Fortschritt des Sammel-Updates (null = kein Lauf bekannt).
  let bulk = $state(null);
  let starting = $state(false);
  let pollTimer = null;
  // Schwere-Summary kommt vom Server (zählt über ALLE Treffer, nicht nur die Seite).
  let summary = $state({ critical: 0, high: 0, medium: 0, low: 0, unknown: 0 });

  let pageCount = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));

  async function load(p = 1) {
    error = '';
    try {
      const res = await api.system.vulnerabilities(p, PAGE_SIZE, hideDocker ? 'os' : '');
      rows = res?.items ?? [];
      total = res?.total ?? 0;
      page = res?.page ?? p;
      summary = { critical: 0, high: 0, medium: 0, low: 0, unknown: 0, ...(res?.summary ?? {}) };
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      loaded = true;
    }
  }

  function goto(p) {
    if (p < 1 || p > pageCount || p === page) return;
    load(p);
  }

  function toggleDocker() {
    hideDocker = !hideDocker;
    page = 1;
    load(1);
  }

  // --- Sammel-Update aller VMs ------------------------------------------------
  function startPolling() {
    if (pollTimer) return;
    pollTimer = setInterval(async () => {
      try {
        bulk = await api.system.bulkUpdateStatus();
        if (!bulk?.running) {
          stopPolling();
          load(page); // behobene CVEs sichtbar machen
        }
      } catch {
        /* transienter Fehler - nächster Tick versucht es erneut */
      }
    }, 2500);
  }
  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  async function refreshBulk() {
    try {
      bulk = await api.system.bulkUpdateStatus();
    } catch {
      bulk = null;
    }
    if (bulk?.running) startPolling();
  }

  async function updateAll() {
    starting = true;
    error = '';
    try {
      bulk = await api.system.startBulkUpdate();
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        // 409 = läuft bereits; den aktuellen Stand trotzdem holen.
        await refreshBulk();
      } else {
        error = e instanceof ApiError ? e.message : String(e);
      }
    } finally {
      starting = false;
      startPolling();
    }
  }

  // --- Stand des CVE-Scanners -------------------------------------------------
  // Trivy lädt seine Schwachstellen-Datenbank beim Scan selbst nach - aber nur
  // mit Netzzugang. Scheitert das, scannt Trivy mit der ALTEN Datenbank weiter
  // und meldet keinen Fehler. Ohne diese Anzeige wäre „keine Sicherheitslücken
  // gefunden" nicht von „seit Wochen nicht mehr nachgesehen" zu unterscheiden.
  let scanner = $state(null);
  let dbUpdating = $state(false);

  async function loadScanner() {
    try {
      scanner = await api.system.scannerStatus();
    } catch {
      scanner = null; // kein Recht oder Endpunkt weg - dann eben keine Anzeige
    }
  }

  // Datum + Uhrzeit des DB-Stands in der Sprache des Nutzers.
  const fmtWhen = (s) => {
    if (!s) return '';
    const d = new Date(s);
    return isNaN(d) ? '' : d.toLocaleString();
  };

  // Alter in Worten. Ab zwei Tagen in Tagen - „seit 240 Stunden" muss der
  // Leser erst umrechnen, „seit 10 Tagen" nicht.
  let dbAge = $derived.by(() => {
    const h = scanner?.age_hours ?? 0;
    return h >= 48
      ? t('security.db.ageDays', { days: Math.floor(h / 24) })
      : t('security.db.ageHours', { hours: h });
  });

  // Anzeige-Klasse der Frische: „stale" ist eine Warnung, „critical" ein
  // echter Missstand, „unknown" bleibt neutral (wir wissen es schlicht nicht).
  let dbClass = $derived(
    scanner?.freshness === 'critical' ? 'alert-danger'
      : scanner?.freshness === 'stale' ? 'alert-warning'
      : 'alert-secondary',
  );

  async function updateDB() {
    dbUpdating = true;
    toasts.clear();
    try {
      const res = await api.system.updateScannerDB();
      const startedToast = toasts.info(t('security.db.updateStarted'), { timeout: 900000 });
      // Der Download kann Minuten dauern - großzügig warten, statt vorschnell
      // „läuft noch" zu melden.
      const job = await waitForJob(null, res.job_id, 120);
      toasts.dismiss(startedToast);
      await loadScanner();
      if (job?.status === 'failed') {
        toasts.error(t('security.db.updateFailed'));
      } else if (job) {
        toasts.success(t('security.db.updateDone'));
      } else {
        toasts.info(t('security.db.updateStillRunning'));
      }
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      dbUpdating = false;
    }
  }

  onDestroy(stopPolling);
  load();
  refreshBulk();
  loadScanner();
</script>

<div class="container">
  <div class="d-flex flex-wrap align-items-center gap-2 mb-3">
    <h1 class="h3 mb-0 me-2">{t('security.title')}</h1>
    {#each ['critical', 'high', 'medium', 'low'] as sev}
      {#if summary[sev]}
        <span class="badge {severityBadge(sev)}">{severityLabel(sev)}: {summary[sev]}</span>
      {/if}
    {/each}
  </div>
  <ul class="nav nav-tabs mb-3">
    <li class="nav-item">
      <button class="nav-link {tab === 'cve' ? 'active' : ''}" data-testid="tab-cve"
        onclick={() => (tab = 'cve')}>{t('security.tabCve')}</button>
    </li>
    <li class="nav-item">
      <button class="nav-link {tab === 'advisories' ? 'active' : ''}" data-testid="tab-advisories"
        onclick={() => (tab = 'advisories')}>{t('security.tabAdvisories')}</button>
    </li>
    <li class="nav-item">
      <button class="nav-link {tab === 'caches' ? 'active' : ''}" data-testid="tab-caches"
        onclick={() => (tab = 'caches')}>{t('security.tabCaches')}</button>
    </li>
  </ul>

  {#if tab === 'advisories'}
    <AdvisoryList />
  {:else if tab === 'caches'}
    <CacheStats />
  {:else}
  <p class="text-body-secondary">
    {t('security.introBefore')}<strong>Trivy</strong>{t('security.introAfter')}
  </p>
  <p class="small text-body-secondary">
    {t('security.weightingHint')}
  </p>

  <div class="d-flex flex-wrap align-items-center gap-3 mb-3">
    <button
      type="button"
      class="btn btn-sm btn-primary"
      data-testid="bulk-update-all"
      onclick={updateAll}
      disabled={starting || bulk?.running}
      title={t('security.updateAllHint')}
    >
      {bulk?.running ? t('security.bulkRunning', { done: (bulk.completed ?? 0) + (bulk.failed ?? 0), total: bulk.total ?? 0 }) : t('security.updateAll')}
    </button>
    <div class="form-check form-switch mb-0">
      <input class="form-check-input" type="checkbox" role="switch" id="hide-docker"
        checked={hideDocker} onchange={toggleDocker} />
      <label class="form-check-label small" for="hide-docker">{t('security.hideDocker')}</label>
    </div>
  </div>

  <!-- Stand der Schwachstellen-Datenbank. Steht bewusst ÜBER der Fundliste:
       Wie belastbar die Liste darunter ist, hängt genau daran. -->
  {#if scanner}
    <div class="alert {dbClass} py-2 d-flex flex-wrap align-items-center gap-2" data-testid="cve-db-status">
      {#if !scanner.available}
        <span>{t('security.db.noScanner')}</span>
      {:else if scanner.updated_at}
        <span>
          {t('security.db.state', { when: fmtWhen(scanner.updated_at) })}
          {#if scanner.freshness !== 'fresh'}
            <strong data-testid="cve-db-stale">{t('security.db.ageWarn', { age: dbAge })}</strong>
          {/if}
        </span>
        <span class="text-body-secondary small">Trivy {scanner.version}</span>
      {:else}
        <span data-testid="cve-db-missing">{t('security.db.never')}</span>
      {/if}
      {#if scanner.available && auth.can('settings:manage')}
        <button class="btn btn-sm btn-outline-secondary ms-auto" data-testid="cve-db-update"
          disabled={dbUpdating} onclick={updateDB}>
          {dbUpdating ? t('security.db.updating') : t('security.db.update')}
        </button>
      {/if}
      <!-- Abschottung: Sie stand bisher NUR auf der LCM-Host-Karte - und die
           gibt es nur, wenn der eigene Rechner als Server aufgenommen ist. Im
           Container ist er das bewusst nicht; dort war also unsichtbar, ob der
           Scanner eingesperrt läuft. Ausgerechnet beim Schutz des Master-Keys
           ist „nicht sichtbar" die schlechteste Auskunft. -->
      <span class="w-100"><SandboxBadge info={scanner} /></span>
    </div>
  {/if}

  {#if bulk?.running}
    <div class="alert alert-info py-2 d-flex align-items-center gap-2" data-testid="bulk-progress">
      <span class="spinner-border spinner-border-sm" role="status" aria-hidden="true"></span>
      <span>
        {t('security.bulkRunning', { done: (bulk.completed ?? 0) + (bulk.failed ?? 0), total: bulk.total ?? 0 })}
        {#if bulk.current}<span class="text-body-secondary"> - {t('security.bulkCurrent', { name: bulk.current })}</span>{/if}
      </span>
    </div>
  {:else if bulk?.finished_at}
    <div class="alert {bulk.failed > 0 ? 'alert-warning' : 'alert-success'} py-2" data-testid="bulk-done">
      {t('security.bulkDone', { done: bulk.completed ?? 0, failed: bulk.failed ?? 0 })}
      {#if bulk.last_error}<div class="small text-body-secondary">{t('security.bulkError', { err: bulk.last_error })}</div>{/if}
    </div>
  {/if}

  {#if loaded && total > 0}
    <p class="small text-body-secondary mb-2">
      {t('security.entriesTotal', { total: fmtNum(total) })}
    </p>
  {/if}

  {#if error}<div class="alert alert-danger">{error}</div>{/if}

  <div class="table-responsive">
    <table class="table table-sm align-middle">
      <thead><tr><th>{t('security.colSeverity')}</th><th>{t('security.colSource')}</th><th>{t('security.colServer')}</th><th>{t('security.colCve')}</th><th>{t('security.colPackage')}</th><th>{t('security.colInstalled')}</th><th>{t('security.colFixedIn')}</th><th>{t('security.colTitle')}</th></tr></thead>
      <tbody>
        {#each rows as v (v.server_id + v.cve_id + v.package_name + (v.image_ref ?? ''))}
          <tr class={v.severity === 'critical' ? 'table-danger' : v.severity === 'high' ? 'table-warning' : ''}>
            <td><span class="badge {severityBadge(v.severity)}">{severityLabel(v.severity)}</span></td>
            <td class="small">
              {#if v.source === 'docker'}
                <span class="badge text-bg-info" title={v.image_ref}>Docker</span>
                {#if v.image_ref}<div class="text-body-secondary"><code class="small">{v.image_ref}</code></div>{/if}
              {:else}
                <span class="badge border text-body-secondary">{t('security.sourceOs')}</span>
              {/if}
            </td>
            <td class="small"><a href={`/servers/${v.server_id}`} use:link>{v.server_name}</a></td>
            <td class="small">
              {#if v.primary_url}
                <a href={v.primary_url} target="_blank" rel="noopener noreferrer">{v.cve_id}</a>
              {:else}{v.cve_id}{/if}
            </td>
            <td class="small">{v.package_name}</td>
            <td class="small">{v.installed_version || '-'}</td>
            <td class="small">{v.fixed_version || '-'}</td>
            <td class="small text-body-secondary">{v.title || '-'}</td>
          </tr>
        {:else}
          <tr><td colspan="8" class="text-body-secondary small">
            {loaded ? t('security.none') : t('common.loading')}
          </td></tr>
        {/each}
      </tbody>
    </table>
  </div>

  <Pagination
    page={page}
    {pageCount}
    {total}
    pageSize={PAGE_SIZE}
    onchange={goto}
    label={t('security.pageNav')}
  />
  {/if}
</div>
