<script>
  // Job-Historie: serverseitig gefiltert und seitenweise (100/Seite). Der
  // Konsolen-Output öffnet sich inline unter der angeklickten Zeile.
  import { onDestroy } from 'svelte';
  import { api, ApiError } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  import SshSessions from '../components/SshSessions.svelte';
  import Pagination from '../components/Pagination.svelte';

  const t = (k, p) => i18n.t(k, p);

  const PAGE_SIZE = 100;
  const STATUSES = ['success', 'failed', 'aborted', 'running', 'blocked', 'pending'];

  let jobs = $state([]);
  let total = $state(0);
  let page = $state(1);
  let typeOptions = $state([]);
  let triggeredByOptions = $state([]);

  let filters = $state({ status: '', type: '', q: '', triggered_by: '', from: '', to: '' });
  let hideHealth = $state(true); // Health-Check-Jobs standardmäßig ausblenden

  let expandedId = $state(null); // Job, dessen Output inline offen ist
  let output = $state(''); // Konsolen-Output des offenen Jobs
  let sessions = $state([]); // verknüpfte SSH-Protokolle des offenen Jobs
  let error = $state('');
  let loading = $state(true);

  let totalPages = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));

  // silent: Hintergrund-Aktualisierung (Polling) - ohne Lade-Anzeige, damit
  // die Tabelle nicht alle paar Sekunden gegen „Lädt …" getauscht wird.
  async function load(silent = false) {
    if (!silent) loading = true;
    error = '';
    try {
      const res = await api.jobs.history(null, {
        ...filters,
        hide_health: hideHealth,
        page,
        page_size: PAGE_SIZE,
      });
      jobs = res.items ?? [];
      total = res.total ?? 0;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      if (!silent) loading = false;
    }
  }

  async function loadOptions() {
    try {
      const o = await api.jobs.filterOptions();
      typeOptions = o.types ?? [];
      triggeredByOptions = (o.triggered_by ?? []).filter(Boolean);
    } catch {
      /* Optionen sind optional - Filter funktionieren auch ohne. */
    }
  }

  // Filteränderung: zurück auf Seite 1 und neu laden.
  function applyFilters() {
    page = 1;
    expandedId = null;
    load();
  }

  // Namenssuche mit kleiner Verzögerung (nicht bei jedem Tastendruck laden).
  let searchTimer;
  function onSearchInput() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(applyFilters, 350);
  }
  onDestroy(() => clearTimeout(searchTimer));

  function resetFilters() {
    filters = { status: '', type: '', q: '', triggered_by: '', from: '', to: '' };
    hideHealth = true;
    applyFilters();
  }

  function goto(p) {
    if (p < 1 || p > totalPages || p === page) return;
    page = p;
    expandedId = null;
    load();
  }

  async function toggleOutput(j) {
    if (expandedId === j.id) {
      expandedId = null;
      return;
    }
    expandedId = j.id;
    output = t('common.loading');
    sessions = [];
    try {
      const res = await api.jobs.consoleOutput(j.id);
      output = res.output ?? '';
      sessions = await api.jobs.sshSessions(j.id);
    } catch (e) {
      output = e instanceof ApiError ? e.message : String(e);
    }
  }

  function badge(status) {
    return status === 'success' ? 'bg-success'
      : status === 'failed' ? 'bg-danger'
      : status === 'aborted' ? 'bg-secondary text-light'
      : status === 'running' ? 'bg-primary'
      : status === 'blocked' ? 'bg-warning text-dark'
      : 'bg-secondary';
  }

  // Laufenden Job manuell abbrechen (hebt die Server-Sperre auf).
  async function abortJob(j) {
    if (!confirm(t('jobs.abortConfirm', { name: j.name }))) return;
    error = '';
    try {
      await api.jobs.abort(j.id);
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  // Manuelles Neuladen (ohne Filter zurückzusetzen).
  let refreshing = $state(false);
  async function refresh() {
    refreshing = true;
    try {
      await load();
    } finally {
      refreshing = false;
    }
  }

  // Automatisches Nachladen, solange ein Job läuft.
  //
  // Ohne das blieb ein laufender Job auf dieser Seite für immer „running":
  // Die Liste wurde nur beim Betreten geladen, und einen Aktualisieren-Knopf
  // gab es nicht. Gepollt wird bewusst NUR, wenn tatsächlich etwas läuft -
  // eine ruhige Historie erzeugt keinen Dauerverkehr.
  const POLL_MS = 5000;
  let hasRunning = $derived(jobs.some((j) => j.status === 'running' || j.status === 'pending'));
  let pollTimer = null;
  $effect(() => {
    if (!hasRunning) return;
    pollTimer = setInterval(() => {
      // Ein offener Output-Block würde beim Neuladen nicht verloren gehen,
      // aber ein Ladevorgang während des Tippens im Suchfeld stört - deshalb
      // still im Hintergrund und ohne Lade-Anzeige.
      load(true);
    }, POLL_MS);
    return () => {
      clearInterval(pollTimer);
      pollTimer = null;
    };
  });
  onDestroy(() => clearInterval(pollTimer));

  $effect(() => {
    if (auth.isLoggedIn) {
      load();
      loadOptions();
    }
  });
</script>

<div class="container">
  <h1 class="h3 mb-3">{t('jobs.title')}</h1>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}

  <!-- Filterleiste -->
  <div class="card mb-3"><div class="card-body py-2">
    <div class="row g-2 align-items-end">
      <div class="col-6 col-md-2">
        <label class="form-label small mb-1" for="f-status">{t('jobs.filterStatus')}</label>
        <select id="f-status" class="form-select form-select-sm" bind:value={filters.status} onchange={applyFilters}>
          <option value="">{t('common.all')}</option>
          {#each STATUSES as s (s)}<option value={s}>{s}</option>{/each}
        </select>
      </div>
      <div class="col-6 col-md-2">
        <label class="form-label small mb-1" for="f-type">{t('jobs.filterType')}</label>
        <select id="f-type" class="form-select form-select-sm" bind:value={filters.type} onchange={applyFilters}>
          <option value="">{t('common.all')}</option>
          {#each typeOptions as ty (ty)}<option value={ty}>{ty}</option>{/each}
        </select>
      </div>
      <div class="col-12 col-md-3">
        <label class="form-label small mb-1" for="f-q">{t('jobs.filterName')}</label>
        <input id="f-q" class="form-control form-control-sm" placeholder={t('jobs.searchName')} bind:value={filters.q} oninput={onSearchInput} />
      </div>
      <div class="col-6 col-md-2">
        <label class="form-label small mb-1" for="f-by">{t('jobs.filterTriggeredBy')}</label>
        <select id="f-by" class="form-select form-select-sm" bind:value={filters.triggered_by} onchange={applyFilters}>
          <option value="">{t('common.all')}</option>
          {#each triggeredByOptions as b (b)}<option value={b}>{b}</option>{/each}
        </select>
      </div>
      <div class="col-6 col-md-3 d-flex align-items-end">
        <div class="form-check form-switch mb-1">
          <input class="form-check-input" type="checkbox" id="hide-health-jobs" bind:checked={hideHealth} onchange={applyFilters} />
          <label class="form-check-label small" for="hide-health-jobs">{t('jobs.hideHealth')}</label>
        </div>
      </div>
      <div class="col-6 col-md-2">
        <label class="form-label small mb-1" for="f-from">{t('jobs.from')}</label>
        <input id="f-from" type="date" class="form-control form-control-sm" bind:value={filters.from} onchange={applyFilters} />
      </div>
      <div class="col-6 col-md-2">
        <label class="form-label small mb-1" for="f-to">{t('jobs.to')}</label>
        <input id="f-to" type="date" class="form-control form-control-sm" bind:value={filters.to} onchange={applyFilters} />
      </div>
      <div class="col-12 col-md-auto ms-md-auto d-flex gap-2">
        <button class="btn btn-sm btn-outline-secondary" onclick={resetFilters}>{t('jobs.resetFilters')}</button>
        <button class="btn btn-sm btn-outline-primary" data-testid="jobs-refresh"
          disabled={refreshing} onclick={refresh}>
          {refreshing ? t('common.loading') : t('jobs.refresh')}
        </button>
      </div>
    </div>
  </div></div>

  {#if loading}
    <div class="text-body-secondary">{t('jobs.loading')}</div>
  {:else}
    <div class="mb-2 small text-body-secondary">{total} {t('jobs.countLabel')}</div>
    <div class="table-responsive">
      <table class="table table-hover table-sm align-middle">
        <thead><tr><th>{t('jobs.colJob')}</th><th>{t('jobs.colType')}</th><th>{t('jobs.colStatus')}</th><th>{t('jobs.colTriggeredBy')}</th><th>{t('jobs.colTime')}</th><th></th></tr></thead>
        <tbody>
          {#each jobs as j (j.id)}
            <tr class={expandedId === j.id ? 'table-active' : ''}>
              <td>{j.name}</td>
              <td class="small text-body-secondary">{j.type}</td>
              <td><span class="badge {badge(j.status)}">{j.status}</span></td>
              <td class="small">{j.triggered_by}</td>
              <td class="small text-body-secondary">{j.started_at ? new Date(j.started_at).toLocaleString() : '-'}</td>
              <td class="text-end text-nowrap">
                {#if j.status === 'running' && auth.can('servers:write')}
                  <button class="btn btn-sm btn-outline-danger" onclick={() => abortJob(j)}>{t('jobs.abort')}</button>
                {/if}
                <button class="btn btn-sm btn-outline-secondary" onclick={() => toggleOutput(j)}>
                  {expandedId === j.id ? t('jobs.close') : t('jobs.output')}
                </button>
              </td>
            </tr>
            {#if expandedId === j.id}
              <tr class="table-active">
                <td colspan="6">
                  <div class="mb-2">
                    <strong>{j.name}</strong>
                    {#if j.exit_code !== null && j.exit_code !== undefined}<span class="small text-body-secondary ms-2">{t('jobs.exitCode', { code: j.exit_code })}</span>{/if}
                  </div>
                  <pre class="bg-body-tertiary border rounded p-2 small mb-2" style="max-height: 360px; overflow: auto; white-space: pre-wrap;">{output || t('jobs.noOutput')}</pre>
                  {#if sessions.length}
                    <div class="small text-body-secondary mb-1">{t('jobs.sshLogTitle')}</div>
                    <SshSessions {sessions} />
                  {/if}
                </td>
              </tr>
            {/if}
          {:else}
            <tr><td colspan="6" class="text-body-secondary small">{t('jobs.none')}</td></tr>
          {/each}
        </tbody>
      </table>
    </div>

    <Pagination page={page} pageCount={totalPages} {total} pageSize={PAGE_SIZE} onchange={goto} />
  {/if}
</div>
