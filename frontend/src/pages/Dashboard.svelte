<script>
  // Dashboard: Ampel-Übersicht aller Server + Verknüpfung zum Onboarding.
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  import StatusBadge from '../components/StatusBadge.svelte';
  import OsLogo from '../components/OsLogo.svelte';
  import Pagination from '../components/Pagination.svelte';
  import { lastSeen } from '../lib/format.js';
  import { icons } from '../lib/icons.js';

  const t = (k, p) => i18n.t(k, p);

  const PAGE_SIZE = 100;
  // Proxmox-Systeme zeigen den Produktnamen statt des (generischen) Debian.
  const PROXMOX_NAMES = {
    pve: 'Proxmox VE',
    pbs: 'Proxmox Backup Server',
    pmg: 'Proxmox Mail Gateway',
    pdm: 'Proxmox Datacenter Manager',
  };
  const osLabel = (s) =>
    s.proxmox_type
      ? `${PROXMOX_NAMES[s.proxmox_type] ?? 'Proxmox'} ${s.proxmox_version ?? ''}`.trim()
      : `${s.os_name} ${s.os_version}`;

  let servers = $state([]);
  let statuses = $state({}); // serverId -> { status, insights }
  let error = $state('');
  let loading = $state(true);

  // Filter (clientseitig - Status wird ohnehin pro Server geladen) + Seite.
  let filters = $state({ name: '', host: '', os: '', kernel: '', status: '' });
  let page = $state(1);

  // Kernel-Filter: komponentenweiser Präfix-Vergleich über die Punkte. „6"
  // trifft 6.5.34 und 6.2.44, aber nicht 61.1.2 oder 7.1.42; „6.5" trifft nur
  // 6.5.x. Je Kernel-Komponente zählt der führende Zahlenanteil (Debian hängt
  // an die Patch-Ziffer noch „-13-amd64" o.ä.), verglichen wird exakt je Stelle.
  function kernelMatches(kernel, query) {
    const q = (query ?? '').trim();
    if (!q) return true;
    const qParts = q.split('.').filter((p) => p !== '');
    const kParts = String(kernel ?? '')
      .split('.')
      .map((p) => (p.match(/^\d+/) || [p])[0]);
    if (qParts.length > kParts.length) return false;
    return qParts.every((qp, i) => qp === kParts[i]);
  }

  // Auswahl fürs OS-Dropdown aus den tatsächlich vorkommenden Werten.
  let osOptions = $derived(
    [...new Set(servers.map((s) => s.os_name).filter(Boolean))].sort((a, b) => a.localeCompare(b)),
  );

  let filtered = $derived(
    servers.filter((s) => {
      const f = filters;
      if (f.name && !(s.name ?? '').toLowerCase().includes(f.name.toLowerCase())) return false;
      if (f.host && !(s.host ?? '').toLowerCase().includes(f.host.toLowerCase())) return false;
      if (f.os && s.os_name !== f.os) return false;
      if (f.kernel && !kernelMatches(s.kernel_version, f.kernel)) return false;
      if (f.status && (statuses[s.id]?.status ?? 'red') !== f.status) return false;
      return true;
    }),
  );

  let totalPages = $derived(Math.max(1, Math.ceil(filtered.length / PAGE_SIZE)));
  // Seite in Schranken halten, falls die Filtermenge schrumpft.
  let pageSafe = $derived(Math.min(page, totalPages));
  let pageItems = $derived(filtered.slice((pageSafe - 1) * PAGE_SIZE, pageSafe * PAGE_SIZE));

  // Jede Filteränderung springt zurück auf Seite 1.
  function applyFilters() {
    page = 1;
  }
  function resetFilters() {
    // Einzelne Felder leeren (nicht das Objekt neu zuweisen) - so ziehen
    // die an filters.* gebundenen Eingaben/Selects zuverlässig nach.
    filters.name = '';
    filters.host = '';
    filters.os = '';
    filters.kernel = '';
    filters.status = '';
    page = 1;
  }
  function goto(p) {
    if (p < 1 || p > totalPages || p === pageSafe) return;
    page = p;
  }

  async function load() {
    loading = true;
    error = '';
    try {
      servers = await api.servers.list();
      // Status je Server parallel laden.
      await Promise.all(
        servers.map(async (s) => {
          try {
            statuses[s.id] = await api.servers.status(s.id);
          } catch {
            statuses[s.id] = { status: 'red', insights: [] };
          }
        }),
      );
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  function counts() {
    const c = { excellent: 0, green: 0, yellow: 0, red: 0 };
    for (const s of servers) {
      const st = statuses[s.id]?.status ?? 'red';
      c[st] = (c[st] ?? 0) + 1;
    }
    return c;
  }

  // Offline: nicht erreichbar UND mindestens zwei fehlgeschlagene Kontakte in
  // Folge. Ein einzelner Fehlschlag ist im Betrieb Alltag und noch keine
  // Aussage - deshalb erst der zweite. Gilt für JEDEN Server; ob die
  // Nichterreichbarkeit toleriert wird, ist eine getrennte Frage (sie steuert
  // die Ampelfarbe, nicht die Tatsache).
  const OFFLINE_AFTER_FAILED_CHECKS = 2;
  function isOffline(s) {
    return !s.reachable && (s.failed_checks ?? 0) >= OFFLINE_AFTER_FAILED_CHECKS;
  }

  // Ausgegraut wird NUR der tolerierte Fall (Status nicht rot) - ein echter
  // Ausfall soll seine rote Zeile behalten und nicht optisch zurücktreten.
  function offlineDimmed(s) {
    return !s.reachable && (statuses[s.id]?.status ?? 'red') !== 'red';
  }

  $effect(() => {
    if (auth.isLoggedIn) load();
  });
</script>

<div class="container">
  <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-4">
    <h1 class="h3 mb-0">{t('dashboard.title')}</h1>
    {#if auth.can('servers:write')}
      <a class="btn btn-primary" href="/servers/join" use:link>{t('dashboard.addServer')}</a>
    {/if}
  </div>

  {#if error}
    <div class="alert alert-danger">{error}</div>
  {/if}

  {#if loading}
    <div class="text-body-secondary">{t('dashboard.loading')}</div>
  {:else}
    {@const c = counts()}
    <div class="row g-3 mb-4">
      <div class="col-6 col-md-3">
        <div class="card text-center border-success">
          <div class="card-body px-1"><div class="display-6 text-success">{c.excellent}</div><small class="text-nowrap">{t('dashboard.excellentShort')}</small></div>
        </div>
      </div>
      <div class="col-6 col-md-3">
        <div class="card text-center border-success-subtle">
          <div class="card-body px-1"><div class="display-6 text-success-emphasis" style="opacity:.75">{c.green}</div><small class="text-nowrap">{t('dashboard.okShort')}</small></div>
        </div>
      </div>
      <div class="col-6 col-md-3">
        <div class="card text-center border-warning">
          <div class="card-body px-1"><div class="display-6 text-warning">{c.yellow}</div><small class="text-nowrap">{t('dashboard.warning')}</small></div>
        </div>
      </div>
      <div class="col-6 col-md-3">
        <div class="card text-center border-danger">
          <div class="card-body px-1"><div class="display-6 text-danger">{c.red}</div><small class="text-nowrap">{t('dashboard.critical')}</small></div>
        </div>
      </div>
    </div>

    {#if servers.length === 0}
      <div class="alert alert-info">
        {t('dashboard.noServers')} {#if auth.can('servers:write')}{t('dashboard.noServersHint')}{/if}
      </div>
    {:else}
      <!-- Filterleiste -->
      <div class="card mb-3"><div class="card-body py-2">
        <div class="row g-2 align-items-end">
          <div class="col-12 col-md-3">
            <label class="form-label small mb-1" for="f-name">{t('dashboard.filterName')}</label>
            <input id="f-name" class="form-control form-control-sm" placeholder={t('dashboard.searchName')} bind:value={filters.name} oninput={applyFilters} />
          </div>
          <div class="col-12 col-md-2">
            <label class="form-label small mb-1" for="f-host">{t('dashboard.filterHost')}</label>
            <input id="f-host" class="form-control form-control-sm" placeholder={t('dashboard.searchHost')} bind:value={filters.host} oninput={applyFilters} />
          </div>
          <div class="col-6 col-md-2">
            <label class="form-label small mb-1" for="f-os">{t('dashboard.filterOs')}</label>
            <select id="f-os" class="form-select form-select-sm" bind:value={filters.os} onchange={applyFilters}>
              <option value="">{t('common.all')}</option>
              {#each osOptions as o (o)}<option value={o}>{o}</option>{/each}
            </select>
          </div>
          <div class="col-6 col-md-2">
            <label class="form-label small mb-1" for="f-kernel">{t('dashboard.filterKernel')}</label>
            <input id="f-kernel" class="form-control form-control-sm" placeholder={t('dashboard.searchKernel')} bind:value={filters.kernel} oninput={applyFilters} data-testid="filter-kernel" />
          </div>
          <div class="col-6 col-md-2">
            <label class="form-label small mb-1" for="f-status">{t('dashboard.filterStatus')}</label>
            <select id="f-status" class="form-select form-select-sm" bind:value={filters.status} onchange={applyFilters}>
              <option value="">{t('common.all')}</option>
              <option value="excellent">{t('dashboard.excellentShort')}</option>
              <option value="green">{t('dashboard.okShort')}</option>
              <option value="yellow">{t('dashboard.warning')}</option>
              <option value="red">{t('dashboard.critical')}</option>
            </select>
          </div>
          <div class="col-6 col-md-1 d-grid">
            <button class="btn btn-sm btn-outline-secondary" onclick={resetFilters}>{t('common.reset')}</button>
          </div>
        </div>
      </div></div>

      <div class="mb-2 small text-body-secondary">{filtered.length} {t('dashboard.serversLabel')}</div>

      <div class="table-responsive">
        <table class="table table-hover align-middle">
          <thead>
            <tr>
              <th>{t('dashboard.colName')}</th><th>{t('dashboard.colHost')}</th><th>{t('dashboard.colOs')}</th><th>{t('dashboard.colStatus')}</th>
              <th>{t('dashboard.colLastSeen')}</th><th class="text-end">{t('dashboard.colDisk')}</th>
            </tr>
          </thead>
          <tbody>
            {#each pageItems as s (s.id)}
              <tr style:opacity={offlineDimmed(s) ? 0.5 : null} class:table-active={offlineDimmed(s)}>
                <td><a href={`/servers/${s.id}`} use:link class="fw-semibold text-decoration-none">{s.name}</a></td>
                <td class="text-body-secondary">
                  {#if s.transport === 'agent'}
                    <span class="badge text-bg-info" title={t('dashboard.agentTitle')}>{@html icons.link} {t('dashboard.agentBadge')}</span>
                  {:else}
                    {s.host}:{s.ssh_port}
                  {/if}
                </td>
                <td class="small">
                  <span class="d-inline-flex align-items-center gap-2">
                    <OsLogo os={s.os_name} proxmox={s.proxmox_type} host={s.host} port={s.ssh_port} size={20} />
                    <span>{osLabel(s)}</span>
                  </span>
                </td>
                <td>
                  <StatusBadge status={statuses[s.id]?.status} insights={statuses[s.id]?.insights} />
                  {#if isOffline(s)}
                    <span class="badge text-bg-secondary ms-1" data-testid="offline-badge"
                      title={offlineDimmed(s) ? t('dashboard.offlineTolerated') : t('dashboard.offlineTitle', { count: s.failed_checks })}>
                      {t('dashboard.offline')}
                    </span>
                  {/if}
                </td>
                <td class="small text-body-secondary">{lastSeen(s.last_seen_at)}</td>
                <td class="text-end">
                  {#if s.disk_total_mb > 0}
                    {Math.round((s.disk_used_mb / s.disk_total_mb) * 100)}%
                  {:else}-{/if}
                </td>
              </tr>
            {:else}
              <tr><td colspan="6" class="text-body-secondary small">{t('dashboard.noneForFilters')}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>

      <Pagination
        page={pageSafe}
        pageCount={totalPages}
        total={filtered.length}
        pageSize={PAGE_SIZE}
        onchange={goto}
      />
    {/if}
  {/if}
</div>
