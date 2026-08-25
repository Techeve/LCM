<script>
  // Globale Docker-Übersicht in drei Sichten: die Images (mit Update-Status
  // und CVE-Zählern), die tatsächlich laufenden Container und die
  // Compose-Projekte. Es sind die drei Fragen, die man an eine
  // Docker-Landschaft hat - „was liegt herum", „was läuft", „was gehört
  // zusammen" - und sie haben zu verschiedene Zuschnitte für eine Tabelle.
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { i18n } from '../stores/i18n.svelte.js';
  import ServerLinks from '../components/ServerLinks.svelte';
  import Pagination from '../components/Pagination.svelte';
  import { PAGE_SIZE, pageCount, pageSlice } from '../lib/paging.js';

  const t = (k, p) => i18n.t(k, p);

  let imagePage = $state(1);
  let containerPage = $state(1);
  let tab = $state('images');
  let rows = $state([]);
  let containers = $state([]);
  let projects = $state([]);
  let error = $state('');
  let loaded = $state(false);
  let loadedTabs = $state({ images: false, containers: false, compose: false });
  let onlyUpdates = $state(false);
  let onlyRunning = $state(true);

  let visible = $derived(onlyUpdates ? rows.filter((r) => r.update_available) : rows);
  let updateCount = $derived(rows.filter((r) => r.update_available).length);
  let cveCount = $derived(rows.filter((r) => r.critical_vulns > 0 || r.high_vulns > 0).length);
  let visibleContainers = $derived(
    onlyRunning ? containers.filter((c) => c.state === 'running') : containers,
  );

  async function load() {
    error = '';
    try {
      rows = (await api.system.dockerOverview()) ?? [];
      loadedTabs.images = true;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      loaded = true;
    }
  }

  // Container und Compose erst beim Öffnen des Reiters holen - beide sind
  // eigene Abfragen über alle Server, und die meisten Besuche gelten den
  // Images.
  async function openTab(name) {
    tab = name;
    if (name === 'containers' && !loadedTabs.containers) {
      try {
        containers = (await api.system.dockerContainers()) ?? [];
        loadedTabs.containers = true;
      } catch (e) {
        error = e instanceof ApiError ? e.message : String(e);
      }
    }
    if (name === 'compose' && !loadedTabs.compose) {
      try {
        projects = (await api.system.dockerCompose()) ?? [];
        loadedTabs.compose = true;
      } catch (e) {
        error = e instanceof ApiError ? e.message : String(e);
      }
    }
  }

  // Zustandsfarbe eines Containers. „running" ist der Normalfall, alles
  // andere verdient Aufmerksamkeit - „exited" aber nicht dieselbe wie
  // „restarting" (Letzteres ist ein Symptom, Ersteres oft Absicht).
  function stateBadge(state) {
    switch (state) {
      case 'running':
        return 'text-bg-success';
      case 'restarting':
      case 'dead':
        return 'text-bg-danger';
      case 'paused':
        return 'text-bg-warning';
      default:
        return 'border text-body-secondary';
    }
  }

  load();
</script>

<div class="container">
  <div class="d-flex flex-wrap align-items-center gap-2 mb-3">
    <h1 class="h3 mb-0 me-2">{t('docker.title')}</h1>
    {#if updateCount > 0}<span class="badge bg-warning text-dark">{t('docker.withUpdate', { count: updateCount })}</span>{/if}
    {#if cveCount > 0}<span class="badge bg-danger">{t('docker.seriousCves', { count: cveCount })}</span>{/if}
  </div>

  <ul class="nav nav-tabs mb-3">
    <li class="nav-item">
      <button class="nav-link {tab === 'images' ? 'active' : ''}" data-testid="tab-images"
        onclick={() => openTab('images')}>{t('docker.tabImages')}</button>
    </li>
    <li class="nav-item">
      <button class="nav-link {tab === 'containers' ? 'active' : ''}" data-testid="tab-containers"
        onclick={() => openTab('containers')}>{t('docker.tabContainers')}</button>
    </li>
    <li class="nav-item">
      <button class="nav-link {tab === 'compose' ? 'active' : ''}" data-testid="tab-compose"
        onclick={() => openTab('compose')}>{t('docker.tabCompose')}</button>
    </li>
  </ul>

  {#if error}<div class="alert alert-danger">{error}</div>{/if}

  {#if tab === 'images'}
    <p class="text-body-secondary">
      {t('docker.introA')}<strong>{t('docker.checkName')}</strong>{t('docker.introB')}<strong>Trivy</strong>{t('docker.introC')}
    </p>
    <div class="form-check form-switch mb-3">
      <input class="form-check-input" type="checkbox" id="only-updates" bind:checked={onlyUpdates} />
      <label class="form-check-label small" for="only-updates">{t('docker.onlyUpdates')}</label>
    </div>

    <div class="table-responsive">
      <table class="table table-sm align-middle" data-testid="docker-images-table">
        <thead><tr>
          <th>{t('docker.colImage')}</th>
          <th>{t('docker.colServers')}</th>
          <th>{t('docker.colUsed')}</th>
          <th>{t('docker.colStatus')}</th>
          <th>{t('docker.colCves')}</th>
        </tr></thead>
        <tbody>
          {#each pageSlice(visible, imagePage) as r (r.repository + ':' + r.tag)}
            <tr class={r.update_available ? 'table-warning' : r.critical_vulns > 0 ? 'table-danger' : ''}>
              <td><code class="small">{r.tag ? `${r.repository}:${r.tag}` : r.repository}</code></td>
              <td class="small">
                <ServerLinks servers={r.servers ?? []} />
              </td>
              <td class="small">{r.in_use_count > 0 ? `${r.in_use_count}×` : '-'}</td>
              <td>
                {#if r.update_available}
                  <span class="badge bg-warning text-dark">{t('docker.updateAvailable')}</span>
                {:else if r.unverifiable}
                  <span class="badge text-bg-secondary">{t('docker.unverifiable')}</span>
                {:else if r.local_only}
                  <span class="badge border text-body-secondary">{t('docker.localOnly')}</span>
                {:else}
                  <span class="badge text-bg-success">{t('docker.current')}</span>
                {/if}
              </td>
              <td class="small">
                {#if r.critical_vulns > 0}<span class="badge bg-danger">{t('docker.criticalN', { count: r.critical_vulns })}</span>{/if}
                {#if r.high_vulns > 0}<span class="badge bg-warning text-dark ms-1">{t('docker.highN', { count: r.high_vulns })}</span>{/if}
                {#if !r.critical_vulns && !r.high_vulns}-{/if}
              </td>
            </tr>
          {:else}
            <tr><td colspan="5" class="text-body-secondary small">
              {loaded ? (onlyUpdates ? t('docker.noUpdates') : t('docker.noImages')) : t('common.loading')}
            </td></tr>
          {/each}
        </tbody>
      </table>
      <Pagination
        page={imagePage}
        pageCount={pageCount(visible.length)}
        total={visible.length}
        pageSize={PAGE_SIZE}
        testid="docker-image-pagination"
        onchange={(p) => (imagePage = p)}
      />
    </div>

  {:else if tab === 'containers'}
    <p class="text-body-secondary">{t('docker.containersIntro')}</p>
    <div class="form-check form-switch mb-3">
      <input class="form-check-input" type="checkbox" id="only-running" bind:checked={onlyRunning} />
      <label class="form-check-label small" for="only-running">{t('docker.onlyRunning')}</label>
    </div>

    <div class="table-responsive">
      <table class="table table-sm align-middle" data-testid="docker-containers-table">
        <thead><tr>
          <th>{t('docker.colContainer')}</th>
          <th>{t('docker.colServer')}</th>
          <th>{t('docker.colImage')}</th>
          <th>{t('docker.colState')}</th>
          <th>{t('docker.colPorts')}</th>
          <th>{t('docker.colProject')}</th>
        </tr></thead>
        <tbody>
          {#each pageSlice(visibleContainers, containerPage) as c (c.server_id + '/' + c.name)}
            <tr>
              <td class="small">{c.name}</td>
              <td class="small"><a href={`/servers/${c.server_id}`} use:link>{c.server_name}</a></td>
              <td class="small"><code class="small">{c.image || '-'}</code></td>
              <td class="small">
                <span class="badge {stateBadge(c.state)}">{c.state || '-'}</span>
                {#if c.status}<div class="text-body-secondary">{c.status}</div>{/if}
              </td>
              <td class="small text-body-secondary">{c.ports || '-'}</td>
              <td class="small">
                {#if c.compose_project}
                  {c.compose_project}
                  {#if c.compose_service}<div class="text-body-secondary">{c.compose_service}</div>{/if}
                {:else}-{/if}
              </td>
            </tr>
          {:else}
            <tr><td colspan="6" class="text-body-secondary small">
              {loadedTabs.containers
                ? (onlyRunning ? t('docker.noRunning') : t('docker.noContainers'))
                : t('common.loading')}
            </td></tr>
          {/each}
        </tbody>
      </table>
      <Pagination
        page={containerPage}
        pageCount={pageCount(visibleContainers.length)}
        total={visibleContainers.length}
        pageSize={PAGE_SIZE}
        testid="docker-container-pagination"
        onchange={(p) => (containerPage = p)}
      />
    </div>

  {:else}
    <p class="text-body-secondary">{t('docker.composeIntro')}</p>

    <div class="table-responsive">
      <table class="table table-sm align-middle" data-testid="docker-compose-table">
        <thead><tr>
          <th>{t('docker.colProject')}</th>
          <th>{t('docker.colServers')}</th>
          <th>{t('docker.colServices')}</th>
          <th>{t('docker.colRunning')}</th>
          <th>{t('docker.colWorkingDir')}</th>
        </tr></thead>
        <tbody>
          {#each projects as p (p.project)}
            <!-- Nicht alle Container laufen: Das ist die Zeile, die man an
                 einem Compose-Projekt zuerst sehen will. -->
            <tr class={p.running < p.containers ? 'table-warning' : ''}>
              <td class="small">{p.project}</td>
              <td class="small"><ServerLinks servers={p.servers ?? []} /></td>
              <td class="small">
                {#if p.services?.length}
                  <span title={p.services.join(', ')}>{t('docker.servicesN', { count: p.services.length })}</span>
                {:else}-{/if}
              </td>
              <td class="small">{p.running} / {p.containers}</td>
              <td class="small text-body-secondary"><code class="small">{p.working_dir || '-'}</code></td>
            </tr>
          {:else}
            <tr><td colspan="5" class="text-body-secondary small">
              {loadedTabs.compose ? t('docker.noCompose') : t('common.loading')}
            </td></tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
