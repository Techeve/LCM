<script>
  // Zentrale APT-Cache-Seite (apt-cacher-ng): Verbindung/URL, Transfer-Statistiken,
  // erweiterte Einstellungen (Neustart, permanentes Caching) und die Überwachung
  // an einem Ort. Verwaltungs-Aktionen sind nur möglich, wenn apt-cacher-ng auf
  // dem LCM-Host selbst läuft (managed) - Statistik und Erreichbarkeit werden für
  // jede konfigurierte URL angezeigt.
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import { auth } from '../../stores/auth.svelte.js';
  import { icons } from '../../lib/icons.js';
  import { waitForJob } from '../../lib/jobs.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import Modal from '../../components/Modal.svelte';

  const t = (k, p) => i18n.t(k, p);

  let overview = $state(null); // { url, status, stats, managed, server_id, installed, permanent_cache, disk_usage }
  let error = $state('');
  let notice = $state('');
  let busy = $state(false); // laufende Verwaltungs-Aktion (Neustart / permanentes Caching)
  let checking = $state(false);

  // Die APT-Caches (je Name + URL), ihr letzter Check und der Cache, dessen
  // Statistik gerade gezeigt wird.
  let caches = $state([]);
  let cacheStatus = $state({});
  let checkingId = $state(null);
  let selectedId = $state(0);
  let cacheOpen = $state(false);
  let cacheForm = $state({ id: 0, name: '', url: '' });
  let cacheByUrl = $derived(new Map(caches.map((c) => [c.url, c])));

  // Angebundene Server: zentrale Sicht, welche Server ihre apt-Anfragen über
  // den Cache leiten - mit Trennen-Button direkt hier (sonst müsste man jeden
  // Server einzeln im Repos-Tab aufsuchen).
  let servers = $state([]);
  let disconnectOutput = $state(''); // Konsolen-Output der letzten Trennung (inkl. Enforce-Hinweis)
  let connected = $derived(servers.filter((s) => s.apt_proxy_active));
  const connectedCount = (cache) => connected.filter((s) => s.apt_proxy_url === cache.url).length;

  // Überwachung: bestehende apt_cacher_down-Alarmregel + letztes Ereignis.
  let alertRules = $state([]);
  let alertEvents = $state([]);
  const canAlerts = auth.can('alerts:manage');

  let aptDownRule = $derived(alertRules.find((r) => r.type === 'apt_cacher_down') ?? null);
  let lastAptEvent = $derived(alertEvents.find((e) => e.type === 'apt_cacher_down') ?? null);

  async function load() {
    error = '';
    try {
      caches = await api.system.aptCaches();
      selectedId = caches.some((c) => c.id === selectedId) ? selectedId : (caches[0]?.id ?? 0);
      overview = await api.system.aptCacheOverview(selectedId);
      if (auth.can('servers:read')) {
        servers = await api.servers.list();
      }
      if (canAlerts) {
        [alertRules, alertEvents] = await Promise.all([api.alerts.listRules(), api.alerts.events()]);
      alertEvents = alertEvents.items ?? alertEvents;
      }
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function recheck() {
    checking = true;
    error = '';
    try {
      overview = await api.system.aptCacheOverview(selectedId);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      checking = false;
    }
  }

  async function checkCache(cache) {
    checkingId = cache.id;
    error = '';
    try {
      cacheStatus = { ...cacheStatus, [cache.id]: await api.system.aptCacheStatus(cache.id) };
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      checkingId = null;
    }
  }

  function openCache(cache) {
    cacheForm = cache ? { id: cache.id, name: cache.name, url: cache.url } : { id: 0, name: '', url: '' };
    cacheOpen = true;
  }

  async function saveCache() {
    error = '';
    try {
      await api.system.saveAptCache(cacheForm);
      cacheOpen = false;
      notice = t('settings.aptCache.cacheSaved', { name: cacheForm.name });
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function deleteCache(cache) {
    if (!confirm(t('settings.aptCache.cacheConfirmDelete', { name: cache.name }))) return;
    error = '';
    try {
      await api.system.deleteAptCache(cache.id);
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function restart() {
    if (!overview?.server_id) return;
    busy = true;
    error = '';
    notice = '';
    try {
      const res = await api.servers.restartAptCacher(overview.server_id);
      notice = t('settings.aptCache.jobStarted', { label: t('settings.aptCache.restart'), jobId: res.job_id });
      await waitForJob(overview.server_id, res.job_id);
      overview = await api.system.aptCacheOverview(selectedId);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function togglePermanent() {
    if (!overview?.server_id) return;
    busy = true;
    error = '';
    notice = '';
    try {
      const res = await api.servers.setAptCacherPermanentCache(overview.server_id, !overview.permanent_cache);
      notice = t('settings.aptCache.jobStarted', { label: t('settings.aptCache.permanentCache'), jobId: res.job_id });
      await waitForJob(overview.server_id, res.job_id);
      overview = await api.system.aptCacheOverview(selectedId);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  // Einen Server vom Cache trennen - gleiche Aktion wie im Repos-Tab der
  // Server-Detailseite. Der Output (inkl. eines Hinweises, falls eine
  // Grundsatz-Regel die Anbindung wieder erzwingen würde) wird angezeigt.
  async function disconnect(s) {
    if (!confirm(t('settings.aptCache.disconnectConfirm', { name: s.name }))) return;
    busy = true;
    error = '';
    notice = '';
    disconnectOutput = '';
    try {
      const res = await api.servers.configureAptProxy(s.id, false);
      disconnectOutput = res?.output ?? '';
      notice = t('settings.aptCache.disconnected', { name: s.name });
      servers = await api.servers.list();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  load();
</script>

<SettingsLayout title={t('settings.aptCache.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  {#if !overview}
    <div class="small text-body-secondary">{t('common.loading')}</div>
  {:else}
    <p class="small text-body-secondary">{t('settings.aptCache.intro')}</p>

    <!-- 1. Die APT-Caches -->
    <div class="card mb-3"><div class="card-body">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-2">
        <h3 class="h6 mb-0">{t('settings.aptCache.cachesTitle')}</h3>
        <button class="btn btn-sm btn-primary" onclick={() => openCache(null)} data-testid="apt-cache-add">{t('settings.aptCache.cacheAdd')}</button>
      </div>
      <p class="small text-body-secondary">{t('settings.aptCache.cachesIntro')}</p>
      {#if caches.length === 0}
        <p class="small text-body-secondary mb-0" data-testid="apt-cache-none">{t('settings.aptCache.cacheNone')}</p>
      {:else}
        <div class="table-responsive">
          <table class="table table-sm align-middle mb-0" data-testid="apt-cache-table">
            <thead><tr>
              <th>{t('settings.aptCache.cacheName')}</th>
              <th>{t('settings.aptCache.urlLabel')}</th>
              <th>{t('settings.aptCache.colServers')}</th>
              <th>{t('settings.aptCache.colStatus')}</th>
              <th></th>
            </tr></thead>
            <tbody>
              {#each caches as cache (cache.id)}
                {@const st = cacheStatus[cache.id]}
                <tr data-testid="apt-cache-row">
                  <td class="fw-semibold">{cache.name}</td>
                  <td class="font-monospace small">{cache.url}</td>
                  <td class="small">{connectedCount(cache)}</td>
                  <td>
                    {#if st}
                      {#if st.running}
                        <span class="badge text-bg-success" data-testid="apt-cache-badge" title={st.message}>{t('settings.aptCache.running')}</span>
                      {:else if st.reachable}
                        <span class="badge text-bg-warning" data-testid="apt-cache-badge" title={st.message}>{t('settings.aptCache.reachableNoCacher')}</span>
                      {:else}
                        <span class="badge text-bg-danger" data-testid="apt-cache-badge" title={st.message}>{t('settings.aptCache.unreachable')}</span>
                      {/if}
                    {:else}
                      <span class="text-body-secondary small">-</span>
                    {/if}
                  </td>
                  <td>
                    <div class="d-flex flex-wrap justify-content-end gap-1">
                      <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => checkCache(cache)} disabled={checkingId === cache.id} data-testid="apt-cache-check">
                        {checkingId === cache.id ? t('settings.aptCache.checking') : t('settings.aptCache.checkConn')}
                      </button>
                      <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openCache(cache)} data-testid="apt-cache-edit">{t('common.edit')}</button>
                      <button class="btn btn-sm btn-outline-danger py-0" onclick={() => deleteCache(cache)} aria-label={t('settings.aptCache.cacheDelete')} data-testid="apt-cache-delete">×</button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div></div>

    <!-- 2. Transfer-Statistiken des gewählten Caches -->
    <div class="card mb-3"><div class="card-body">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-2">
        <h3 class="h6 mb-0">{t('settings.aptCache.statsTitle')}</h3>
        {#if caches.length > 1}
          <select class="form-select form-select-sm w-auto" bind:value={selectedId} onchange={recheck} data-testid="apt-cache-stats-select">
            {#each caches as cache (cache.id)}<option value={cache.id}>{cache.name}</option>{/each}
          </select>
        {/if}
      </div>
      {#if overview.stats}
        <div class="row g-3">
          <div class="col-sm-6">
            <div class="border rounded p-3 h-100">
              <div class="small text-body-secondary">{t('settings.aptCache.dataFetched')}</div>
              <div class="fs-4 fw-semibold">{overview.stats.data_fetched || '-'}</div>
              <div class="small text-body-secondary">{t('settings.aptCache.recentHistory')}: {overview.stats.data_fetched_recent || '-'}</div>
            </div>
          </div>
          <div class="col-sm-6">
            <div class="border rounded p-3 h-100">
              <div class="small text-body-secondary">{t('settings.aptCache.dataServed')}</div>
              <div class="fs-4 fw-semibold">{overview.stats.data_served || '-'}</div>
              <div class="small text-body-secondary">{t('settings.aptCache.recentHistory')}: {overview.stats.data_served_recent || '-'}</div>
            </div>
          </div>
        </div>
        <p class="form-text mb-0">{t('settings.aptCache.statsHint')}</p>
      {:else}
        <p class="small text-body-secondary mb-0">{t('settings.aptCache.statsUnavailable')}</p>
      {/if}
    </div></div>

    <!-- 3. Angebundene Server: zentrale Übersicht + Trennen ohne Umweg über
         die einzelnen Server-Detailseiten. -->
    {#if auth.can('servers:read')}
      <div class="card mb-3"><div class="card-body">
        <h3 class="h6">{t('settings.aptCache.connectedTitle')}</h3>
        <p class="small text-body-secondary">{t('settings.aptCache.connectedIntro')}</p>
        {#if connected.length > 0}
          <ul class="list-group mb-2" data-testid="apt-cache-connected">
            {#each connected as s (s.id)}
              <li class="list-group-item d-flex justify-content-between align-items-center gap-2">
                <span>
                  <a href={`/servers/${s.id}`} use:link class="fw-semibold text-decoration-none">{s.name}</a>
                  <span class="small text-body-secondary ms-2">{s.host}</span>
                  <span class="badge text-bg-secondary ms-2">{cacheByUrl.get(s.apt_proxy_url)?.name ?? s.apt_proxy_url ?? '?'}</span>
                </span>
                {#if auth.can('servers:write')}
                  <button class="btn btn-sm btn-outline-danger" disabled={busy}
                    title={t('settings.aptCache.disconnectTitle')}
                    onclick={() => disconnect(s)}>{t('settings.aptCache.disconnect')}</button>
                {/if}
              </li>
            {/each}
          </ul>
        {:else}
          <p class="small text-body-secondary mb-0">{t('settings.aptCache.noneConnected')}</p>
        {/if}
        {#if disconnectOutput}
          <h4 class="h6 small mt-2 mb-1">{t('settings.aptCache.outputTitle')}</h4>
          <pre class="small mb-0" style="max-height: 220px; overflow: auto">{disconnectOutput}</pre>
        {/if}
      </div></div>
    {/if}

    <!-- 4. Erweiterte Einstellungen (nur wenn auf dem LCM-Host verwaltbar) -->
    <div class="card mb-3"><div class="card-body">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-2">
        <h3 class="h6 mb-0">{t('settings.aptCache.settingsTitle')}</h3>
        {#if overview.managed && auth.can('servers:write')}
          <button class="btn btn-sm btn-outline-secondary" disabled={busy} onclick={restart} data-testid="apt-cache-restart">
            {@html icons.refresh} {t('settings.aptCache.restart')}
          </button>
        {/if}
      </div>
      {#if overview.managed}
        <div class="form-check form-switch">
          <input class="form-check-input" type="checkbox" role="switch" id="apt-cache-permanent"
            checked={overview.permanent_cache} disabled={busy || !auth.can('servers:write')}
            data-testid="apt-cache-permanent-toggle" onchange={togglePermanent} />
          <label class="form-check-label" for="apt-cache-permanent">{t('settings.aptCache.permanentCache')}</label>
        </div>
        <div class="form-text">{t('settings.aptCache.permanentCacheHint')}</div>
        {#if overview.disk_usage}
          <div class="small text-body-secondary mt-2">{t('settings.aptCache.diskUsage')}: <span class="fw-semibold">{overview.disk_usage}</span></div>
        {/if}
      {:else}
        <p class="small text-body-secondary mb-0">{t('settings.aptCache.notManaged')}</p>
      {/if}
    </div></div>

    <!-- 5. Überwachung -->
    <div class="card mb-3"><div class="card-body">
      <h3 class="h6">{t('settings.aptCache.monitorTitle')}</h3>
      <p class="small text-body-secondary">{t('settings.aptCache.monitorIntro')}</p>
      {#if canAlerts}
        {#if aptDownRule}
          {#if aptDownRule.enabled && aptDownRule.channel_id}
            <p class="small mb-1"><span class="badge text-bg-success me-1">{t('settings.aptCache.monitorActive')}</span>{t('settings.aptCache.monitorActiveHint')}</p>
          {:else if aptDownRule.enabled}
            <p class="small mb-1"><span class="badge text-bg-warning me-1">{t('settings.aptCache.monitorLogOnly')}</span>{t('settings.aptCache.monitorLogOnlyHint')}</p>
          {:else}
            <p class="small mb-1"><span class="badge text-bg-secondary me-1">{t('settings.aptCache.monitorDisabled')}</span>{t('settings.aptCache.monitorDisabledHint')}</p>
          {/if}
        {:else}
          <p class="small mb-1"><span class="badge text-bg-secondary me-1">{t('settings.aptCache.monitorNone')}</span>{t('settings.aptCache.monitorNoneHint')}</p>
        {/if}
        {#if lastAptEvent}
          <p class="small text-body-secondary mb-2">
            {t('settings.aptCache.monitorLastEvent')}: {new Date(lastAptEvent.created_at).toLocaleString()} - {lastAptEvent.description}
          </p>
        {/if}
        <a class="btn btn-sm btn-outline-primary" href="/settings/alerts" use:link data-testid="apt-cache-alerts-link">
          {t('settings.aptCache.monitorManage')}
        </a>
      {:else}
        <p class="small text-body-secondary mb-0">{t('settings.aptCache.monitorNoPermission')}</p>
      {/if}
    </div></div>
  {/if}
</SettingsLayout>

<Modal title={cacheForm.id ? t('settings.aptCache.cacheEditTitle') : t('settings.aptCache.cacheAddTitle')} bind:open={cacheOpen}>
  <div class="mb-2">
    <label class="form-label small mb-1" for="ac-name">{t('settings.aptCache.cacheName')}</label>
    <input id="ac-name" class="form-control" placeholder={t('settings.aptCache.cacheNamePlaceholder')} bind:value={cacheForm.name} data-testid="apt-cache-name" />
  </div>
  <div class="mb-1">
    <label class="form-label small mb-1" for="ac-url">{t('settings.aptCache.urlLabel')}</label>
    <input id="ac-url" class="form-control font-monospace" placeholder="http://192.168.1.10:3142" bind:value={cacheForm.url} data-testid="apt-cache-url" />
  </div>
  <div class="form-text mb-3">{t('settings.aptCache.urlHint')}</div>
  <div class="text-end">
    <button class="btn btn-secondary" onclick={() => (cacheOpen = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" onclick={saveCache} disabled={!cacheForm.name || !cacheForm.url} data-testid="apt-cache-save">{t('common.save')}</button>
  </div>
</Modal>
