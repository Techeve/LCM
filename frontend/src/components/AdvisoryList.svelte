<script>
  // Frühwarnung: Befunde der Online-Quellen (OSV) zum installierten
  // Paketbestand. Die schnelle Spur neben dem täglichen Trivy-Scan - hier
  // steht, was Minuten alt ist, samt Schadpaket-Meldungen (MAL-), die es in
  // der Trivy-Datenbank gar nicht gibt.
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  import { toasts } from '../stores/toast.svelte.js';
  import { severityBadge, severityLabel, fmtNum } from '../lib/format.js';
  import { waitForJob } from '../lib/jobs.js';
  import Pagination from './Pagination.svelte';

  const t = (k, p) => i18n.t(k, p);

  const PAGE_SIZE = 100;
  let rows = $state([]);
  let total = $state(0);
  let page = $state(1);
  let summary = $state({});
  let status = $state(null);
  let loaded = $state(false);
  let error = $state('');
  let showResolved = $state(false);
  let minSeverity = $state('');
  let busy = $state('');
  let polling = $state(false);

  let pageCount = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));

  async function load(p = page) {
    error = '';
    try {
      const res = await api.system.advisories({
        page: p,
        pageSize: PAGE_SIZE,
        withResolved: showResolved,
        minSeverity,
      });
      rows = res?.items ?? [];
      total = res?.total ?? 0;
      page = res?.page ?? p;
      summary = res?.summary ?? {};
      status = res?.status ?? null;
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

  // Filter setzen immer auf Seite 1 zurück - sonst landet man auf einer
  // Seite, die es nach dem Filtern nicht mehr gibt.
  function toggleResolved() {
    showResolved = !showResolved;
    load(1);
  }
  function changeSeverity(event) {
    minSeverity = event.currentTarget.value;
    load(1);
  }

  // Ein Durchgang auf Knopfdruck. Ohne ihn passiert nach dem Einschalten der
  // Frühwarnung bis zu 15 Minuten sichtbar nichts - nicht zu unterscheiden
  // von „es funktioniert nicht".
  //
  // Gewartet wird bis zum Ergebnis: „gestartet" allein sagt nichts darüber,
  // ob etwas gefunden wurde, nichts zu tun war oder der Lauf scheiterte.
  async function pollNow() {
    polling = true;
    try {
      const res = await api.system.advisoryPoll();
      const job = await waitForJob(null, res.job_id, 60);
      if (!job) {
        toasts.info(t('advisories.pollStillRunning'));
      } else if (job.status === 'failed') {
        toasts.error(t('advisories.pollFailed', { reason: job.output || '' }));
      } else {
        toasts.success(job.output || t('advisories.pollDone'));
      }
      await load(1);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      polling = false;
    }
  }

  // Alter in Worten. Bei einer Frühwarnung ist „seit wann wissen wir davon"
  // die eigentliche Information - ein Zeitstempel allein zwingt den Leser,
  // selbst zu rechnen.
  function age(iso) {
    const then = new Date(iso);
    if (isNaN(then)) return '';
    const minutes = Math.max(0, Math.floor((Date.now() - then.getTime()) / 60000));
    if (minutes < 60) return t('advisories.ageMinutes', { count: minutes });
    const hours = Math.floor(minutes / 60);
    if (hours < 48) return t('advisories.ageHours', { count: hours });
    return t('advisories.ageDays', { count: Math.floor(hours / 24) });
  }

  const fmtWhen = (s) => {
    if (!s) return '';
    const d = new Date(s);
    return isNaN(d) ? '' : d.toLocaleString();
  };

  async function acknowledge(row) {
    busy = row.id;
    try {
      await api.system.acknowledgeAdvisory(row.id);
      toasts.success(t('advisories.ackDone'));
      await load();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = '';
    }
  }

  // Update genau dieses Pakets auf die behebende Version. Der Weg von der
  // Meldung zur Reaktion soll zwei Klicks lang sein, nicht eine SSH-Sitzung.
  async function updatePackage(row) {
    busy = row.id;
    try {
      await api.servers.updatePackages(row.server_id, { names: [row.package_name] });
      toasts.info(t('advisories.updateStarted', { pkg: row.package_name }));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = '';
    }
  }

  // Kein Fix verfügbar: Dann bleibt, die installierte Version einzufrieren,
  // damit ein Update nicht versehentlich auf eine belastete Version springt.
  async function freezeVersion(row) {
    busy = row.id;
    try {
      await api.servers.createPackagePin(row.server_id, {
        name: row.package_name,
        hold: true,
        note: t('advisories.pinNote', { id: row.advisory_id }),
      });
      toasts.success(t('advisories.freezeDone', { pkg: row.package_name }));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = '';
    }
  }

  load();
</script>

<div class="d-flex flex-wrap align-items-center gap-2 mb-2">
  <p class="text-body-secondary mb-0 me-auto">{t('advisories.intro')}</p>
  {#each ['critical', 'high', 'medium', 'low'] as sev}
    {#if summary[sev]}
      <span class="badge {severityBadge(sev)}">{severityLabel(sev)}: {summary[sev]}</span>
    {/if}
  {/each}
</div>

{#if loaded && status && !status.enabled}
  <!-- Ohne diesen Hinweis wäre eine leere Liste nicht von „alles sauber" zu
       unterscheiden - dabei wurde schlicht nie nachgesehen. -->
  <div class="alert alert-secondary py-2" data-testid="advisories-disabled">
    {t('advisories.disabled')}
    {#if auth.can('settings:manage')}
      <a href="/settings/security" use:link class="ms-1">{t('advisories.disabledLink')}</a>
    {/if}
  </div>
{/if}

{#if loaded && status?.local_copy}
  <!-- Im lokalen Betrieb bestimmt der Stand des Spiegels, wie alt die
       Aussage unten ist. Ohne diese Angabe sähe eine tagealte Kopie genauso
       aus wie eine frische. -->
  <div class="alert alert-secondary py-2 small" data-testid="advisories-local">
    {status.mirrored_at
      ? t('advisories.localState', { when: fmtWhen(status.mirrored_at) })
      : t('advisories.localNever')}
  </div>
{/if}

{#if error}<div class="alert alert-danger">{error}</div>{/if}

<div class="d-flex flex-wrap align-items-center gap-3 mb-3">
  {#if auth.can('servers:write')}
    <button type="button" class="btn btn-sm btn-primary" data-testid="advisory-poll-now"
      disabled={polling || !status?.enabled} onclick={pollNow}
      title={t('advisories.pollHint')}>
      {polling ? t('advisories.polling') : t('advisories.pollNow')}
    </button>
  {/if}

  <div class="d-flex align-items-center gap-2">
    <label class="form-label small mb-0" for="adv-sev">{t('advisories.filterSeverity')}</label>
    <select id="adv-sev" class="form-select form-select-sm" style="width: auto"
      data-testid="advisory-severity-filter" value={minSeverity} onchange={changeSeverity}>
      <option value="">{t('advisories.filterAll')}</option>
      <option value="low">{t('severity.low')}</option>
      <option value="medium">{t('severity.medium')}</option>
      <option value="high">{t('severity.high')}</option>
      <option value="critical">{t('severity.critical')}</option>
    </select>
  </div>

  <div class="form-check form-switch mb-0">
    <input class="form-check-input" type="checkbox" role="switch" id="adv-resolved"
      checked={showResolved} onchange={toggleResolved} />
    <label class="form-check-label small" for="adv-resolved">{t('advisories.showResolved')}</label>
  </div>

  <span class="small text-body-secondary ms-auto" data-testid="advisories-last-poll">
    {status?.last_poll_at
      ? t('advisories.lastPoll', { when: fmtWhen(status.last_poll_at) })
      : t('advisories.neverPolled')}
  </span>
</div>

{#if loaded && total > 0}
  <p class="small text-body-secondary mb-2">{t('advisories.entriesTotal', { total: fmtNum(total) })}</p>
{/if}

<div class="table-responsive">
  <table class="table table-sm align-middle" data-testid="advisories-table">
    <thead>
      <tr>
        <th>{t('advisories.colSeverity')}</th>
        <th>{t('advisories.colAge')}</th>
        <th>{t('advisories.colServer')}</th>
        <th>{t('advisories.colAdvisory')}</th>
        <th>{t('advisories.colPackage')}</th>
        <th>{t('advisories.colFixedIn')}</th>
        <th>{t('advisories.colActions')}</th>
      </tr>
    </thead>
    <tbody>
      {#each rows as r (r.id)}
        <tr class={r.resolved_at ? 'text-body-secondary' : r.kind === 'malware' ? 'table-danger' : ''}>
          <td>
            {#if r.kind === 'malware'}
              <span class="badge text-bg-danger" data-testid="advisory-malware">{t('advisories.malware')}</span>
            {:else}
              <span class="badge {severityBadge(r.severity)}">{severityLabel(r.severity)}</span>
            {/if}
            {#if r.exploited}
              <span class="badge text-bg-warning ms-1" title={t('advisories.exploitedHint')}>{t('advisories.exploited')}</span>
            {/if}
          </td>
          <td class="small">
            {#if r.resolved_at}
              <span class="badge border text-body-secondary">{t('advisories.resolved')}</span>
            {:else}
              {age(r.first_seen_at)}
            {/if}
          </td>
          <td class="small"><a href={`/servers/${r.server_id}`} use:link>{r.server_name}</a></td>
          <td class="small">
            {#if r.url}
              <a href={r.url} target="_blank" rel="noopener noreferrer">{r.advisory_id}</a>
            {:else}{r.advisory_id}{/if}
            {#if r.title}<div class="text-body-secondary">{r.title}</div>{/if}
          </td>
          <td class="small">
            {r.package_name}
            <div class="text-body-secondary">{r.installed_version || '-'}</div>
          </td>
          <td class="small">{r.fixed_version || t('advisories.noFix')}</td>
          <td class="small">
            {#if !r.resolved_at && auth.can('servers:write')}
              <!-- flex-nowrap + text-nowrap: Die Knöpfe brachen sonst
                   untereinander um und machten die Zeile dreimal so hoch. -->
              <div class="d-flex flex-nowrap gap-1">
                {#if r.fixed_version}
                  <button class="btn btn-sm btn-outline-primary text-nowrap" disabled={busy === r.id}
                    data-testid="advisory-update" onclick={() => updatePackage(r)}>
                    {t('advisories.actionUpdate')}
                  </button>
                {:else}
                  <button class="btn btn-sm btn-outline-warning text-nowrap" disabled={busy === r.id}
                    data-testid="advisory-freeze" onclick={() => freezeVersion(r)}>
                    {t('advisories.actionFreeze')}
                  </button>
                {/if}
                {#if !r.acknowledged_by}
                  <button class="btn btn-sm btn-outline-secondary text-nowrap" disabled={busy === r.id}
                    data-testid="advisory-ack" onclick={() => acknowledge(r)}>
                    {t('advisories.actionAck')}
                  </button>
                {:else}
                  <span class="badge border text-body-secondary align-self-center text-nowrap">
                    {t('advisories.acked', { who: r.acknowledged_by })}
                  </span>
                {/if}
              </div>
            {/if}
          </td>
        </tr>
      {:else}
        <tr><td colspan="7" class="text-body-secondary small">
          {loaded ? t('advisories.none') : t('common.loading')}
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
  label={t('advisories.pageNav')}
/>
