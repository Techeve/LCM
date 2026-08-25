<script>
  // Alarm-Regeln (Monitoring-/Trigger-Kriterien): binden ein
  // Überwachungskriterium an eine (optionale) Servergruppe und einen
  // Benachrichtigungskanal. Die typ-abhängigen Schwellenwerte werden je nach
  // gewähltem Alarm-Typ ein-/ausgeblendet. Zusätzlich: sofortige Auswertung
  // und die Alarm-Historie.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import Modal from '../../components/Modal.svelte';
  import Pagination from '../../components/Pagination.svelte';
  import ServerGroupPicker from '../../components/ServerGroupPicker.svelte';

  const t = (k, p) => i18n.t(k, p);

  const TYPES = $derived([
    { value: 'disk_capacity', label: t('settings.alerts.typeDiskCapacity') },
    { value: 'storage_forecast', label: t('settings.alerts.typeStorageForecast') },
    { value: 'security_cve', label: t('settings.alerts.typeSecurityCve') },
    { value: 'failed_updates', label: t('settings.alerts.typeFailedUpdates') },
    { value: 'heartbeat', label: t('settings.alerts.typeHeartbeat') },
    { value: 'reboot_required', label: t('settings.alerts.typeRebootRequired') },
    { value: 'apt_cacher_down', label: t('settings.alerts.typeAptCacherDown') },
    { value: 'crowdsec_lapi_down', label: t('settings.alerts.typeCrowdSecLapiDown') },
    { value: 'deep_scan', label: t('settings.alerts.typeDeepScan') },
    { value: 'cve_db_stale', label: t('settings.alerts.typeCveDbStale') },
    { value: 'backup_stale', label: t('settings.alerts.typeBackupStale') },
    { value: 'advisory', label: t('settings.alerts.typeAdvisory') },
  ]);
  const SEVERITIES = $derived([
    { value: 'info', label: t('settings.alerts.sevInfo') },
    { value: 'warning', label: t('settings.alerts.sevWarning') },
    { value: 'critical', label: t('settings.alerts.sevCritical') },
  ]);

  let rules = $state([]);
  let events = $state([]);
  let eventsTotal = $state(0); // Gesamtzahl in der DB (R2-023: Paging)
  let eventsPage = $state(1);
  const EVENTS_PAGE_SIZE = 100;
  let eventsPageCount = $derived(Math.max(1, Math.ceil(eventsTotal / EVENTS_PAGE_SIZE)));
  let groups = $state([]);
  let channels = $state([]);
  let error = $state('');
  let notice = $state('');

  let open = $state(false);
  let form = $state(emptyForm());

  function emptyForm() {
    return {
      id: null,
      name: '',
      type: 'disk_capacity',
      enabled: true,
      group_ids: [],
      channel_id: '',
      severity: 'warning',
      threshold_percent: 90,
      forecast_days: 10,
      max_outdated: 0,
      min_severity: 'high',
      heartbeat_hours: 24,
      cooldown_minutes: 360,
    };
  }

  const typeLabel = (ty) => TYPES.find((x) => x.value === ty)?.label ?? ty;
  const severityLabel = (s) => SEVERITIES.find((x) => x.value === s)?.label ?? s;

  // Selbstbeobachtung: Diese Typen bewerten LCM selbst (Backup-Stand,
  // CVE-Datenbank, Paket-Cache, LAPI). Sie kennen keine Server und damit auch
  // keine Gruppen - das Geprüfte existiert genau einmal.
  const SELF_TYPES = ['backup_stale', 'cve_db_stale', 'apt_cacher_down', 'crowdsec_lapi_down'];
  const isSelfType = (ty) => SELF_TYPES.includes(ty);

  // Ohne Gruppe gilt die Regel für alle Server - das gehört in die Spalte,
  // sonst liest sich die leere Zelle wie „für niemanden".
  function groupNames(rule) {
    if (isSelfType(rule.type)) return t('settings.alerts.selfScope');
    const names = (rule.groups ?? []).map((g) => g.name);
    return names.length ? names.join(', ') : t('settings.alerts.allServers');
  }

  function channelName(id) {
    if (!id) return t('settings.alerts.logOnly');
    return channels.find((c) => c.id === id)?.name ?? `#${id}`;
  }

  // Aktive Regeln, die niemanden erreichen (kein Kanal = nur protokollieren).
  // Im Langzeittest feuerten 74 Alarme, 0 wurden versendet - weil alle Regeln
  // auf dem Default „nur protokollieren" standen. Das läuft still ins Leere:
  // der Betreiber wähnt sich benachrichtigt, ist es aber nicht. Wir sagen es
  // ihm, ohne zu bevormunden (nur protokollieren bleibt ein legitimer Modus).
  let silentRuleCount = $derived(rules.filter((r) => r.enabled && !r.channel_id).length);
  let noChannelsAtAll = $derived(channels.length === 0);

  async function load() {
    error = '';
    try {
      let evPage;
      [rules, groups, channels, evPage] = await Promise.all([
        api.alerts.listRules(),
        api.groups.list(),
        api.notifications.list(),
        api.alerts.events({ page: 1, pageSize: EVENTS_PAGE_SIZE }),
      ]);
      events = evPage.items;
      eventsTotal = evPage.total;
      eventsPage = 1;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  // R2-023: ältere Ereignisse erreichbar machen - geblättert wie in allen
  // anderen Tabellen, statt die Liste per "mehr laden" anwachsen zu lassen.
  async function gotoEventsPage(p) {
    try {
      const next = await api.alerts.events({ page: p, pageSize: EVENTS_PAGE_SIZE });
      events = next.items;
      eventsTotal = next.total;
      eventsPage = p;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function openNew() {
    form = emptyForm();
    open = true;
  }

  function openEdit(r) {
    form = {
      id: r.id,
      name: r.name,
      type: r.type,
      enabled: !!r.enabled,
      group_ids: (r.groups ?? []).map((g) => g.id),
      channel_id: r.channel_id ?? '',
      severity: r.severity || 'warning',
      threshold_percent: r.threshold_percent || 90,
      forecast_days: r.forecast_days || 10,
      max_outdated: r.max_outdated || 0,
      min_severity: r.min_severity || 'high',
      heartbeat_hours: r.heartbeat_hours || 24,
      cooldown_minutes: r.cooldown_minutes || 360,
    };
    open = true;
  }

  async function run(fn, msg) {
    error = '';
    notice = '';
    try {
      await fn();
      notice = msg;
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function buildPayload() {
    const f = form;
    return {
      name: f.name.trim(),
      type: f.type,
      enabled: f.enabled,
      group_ids: f.group_ids,
      channel_id: f.channel_id === '' ? null : Number(f.channel_id),
      severity: f.severity,
      threshold_percent: Number(f.threshold_percent) || 0,
      forecast_days: Number(f.forecast_days) || 0,
      max_outdated: Number(f.max_outdated) || 0,
      min_severity: f.min_severity,
      heartbeat_hours: Number(f.heartbeat_hours) || 0,
      cooldown_minutes: Number(f.cooldown_minutes) || 0,
    };
  }

  async function save() {
    const f = form;
    const data = buildPayload();
    if (f.id) await run(() => api.alerts.updateRule(f.id, data), t('settings.alerts.saved'));
    else await run(() => api.alerts.createRule(data), t('settings.alerts.created'));
    if (!error) open = false;
  }

  async function remove(r) {
    if (!confirm(t('settings.alerts.confirmDelete', { name: r.name }))) return;
    await run(() => api.alerts.removeRule(r.id), t('settings.alerts.deleted'));
  }

  async function evaluate() {
    await run(() => api.alerts.evaluate(), t('settings.alerts.evaluated'));
  }

  // Kurzbeschreibung des relevanten Schwellenwerts je Regel-Typ (für die Liste).
  function thresholdSummary(r) {
    switch (r.type) {
      case 'disk_capacity':
        return t('settings.alerts.thrDisk', { pct: r.threshold_percent || 90 });
      case 'storage_forecast':
        return t('settings.alerts.thrForecast', { days: r.forecast_days || 10 });
      case 'security_cve':
        return t('settings.alerts.thrCve', { sev: r.min_severity || 'high' });
      case 'failed_updates':
        return t('settings.alerts.thrUpdates', { n: r.max_outdated || 0 });
      case 'heartbeat':
        return t('settings.alerts.thrHeartbeat', { h: r.heartbeat_hours || 24 });
      case 'reboot_required':
        return t('settings.alerts.thrReboot');
      case 'apt_cacher_down':
        return t('settings.alerts.thrAptCacherDown');
      case 'crowdsec_lapi_down':
        return t('settings.alerts.thrCrowdSecLapiDown');
      case 'deep_scan':
        return t('settings.alerts.thrDeepScan');
      case 'cve_db_stale':
        return t('settings.alerts.thrCveDbStale');
      case 'backup_stale':
        return t('settings.alerts.thrBackupStale');
      case 'advisory':
        return t('settings.alerts.thrCve', { sev: r.min_severity || 'high' });
      default:
        return '-';
    }
  }

  function sevBadge(s) {
    return s === 'critical' ? 'text-bg-danger' : s === 'warning' ? 'text-bg-warning' : 'text-bg-info';
  }

  // Initiales Laden beim Mount - sonst bleiben die Tabellen und das
  // Kanal-Dropdown leer, bis eine Mutation run()→load() auslöst (Muster
  // wie General/Notifications/Backups.svelte).
  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.alerts.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  {#if silentRuleCount > 0}
    <div class="alert alert-info d-flex align-items-start gap-2 py-2 small" data-testid="alerts-no-channel">
      <span>ⓘ</span>
      <div>
        {t('settings.alerts.silentWarn', { count: silentRuleCount })}
        {#if noChannelsAtAll}
          <a href="#/settings/notifications">{t('settings.alerts.silentWarnLink')}</a>
        {/if}
      </div>
    </div>
  {/if}

  <div class="d-flex justify-content-between align-items-center mb-3">
    <p class="small text-body-secondary mb-0">
      {t('settings.alerts.intro')}
    </p>
    <div class="text-nowrap ms-3">
      <button class="btn btn-sm btn-outline-secondary" onclick={evaluate}>{t('settings.alerts.evaluateBtn')}</button>
      <button class="btn btn-sm btn-primary" onclick={openNew}>{t('settings.alerts.addRule')}</button>
    </div>
  </div>

  <div class="table-responsive">
    <table class="table table-sm align-middle">
      <thead>
        <tr><th>{t('common.name')}</th><th>{t('settings.alerts.colType')}</th><th>{t('settings.alerts.colThreshold')}</th><th>{t('settings.alerts.colGroup')}</th><th>{t('settings.alerts.colChannel')}</th><th>{t('settings.alerts.colSeverity')}</th><th>{t('common.status')}</th><th class="text-end">{t('settings.alerts.colManage')}</th></tr>
      </thead>
      <tbody>
        {#each rules as r (r.id)}
          <tr>
            <td>{r.name}</td>
            <td class="small">{typeLabel(r.type)}</td>
            <td class="small text-body-secondary">{thresholdSummary(r)}</td>
            <td class="small">{groupNames(r)}</td>
            <td class="small">{channelName(r.channel_id)}</td>
            <td><span class="badge {sevBadge(r.severity)}">{severityLabel(r.severity)}</span></td>
            <td>
              {#if r.enabled}
                <span class="badge text-bg-success">{t('common.active')}</span>
              {:else}
                <span class="badge text-bg-secondary">{t('settings.alerts.inactive')}</span>
              {/if}
            </td>
            <td class="text-end text-nowrap">
              <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEdit(r)}>{t('common.edit')}</button>
              <button class="btn btn-sm btn-outline-danger py-0" onclick={() => remove(r)}>{t('common.delete')}</button>
            </td>
          </tr>
        {:else}
          <tr><td colspan="8" class="text-body-secondary small">{t('settings.alerts.noRules')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>

  <h6 class="mt-4">{t('settings.alerts.historyTitle')}</h6>
  <div class="table-responsive">
    <table class="table table-sm align-middle">
      <thead>
        <tr><th>{t('settings.alerts.histTime')}</th><th>{t('settings.alerts.histRule')}</th><th>{t('settings.alerts.histServer')}</th><th>{t('settings.alerts.colSeverity')}</th><th>{t('common.description')}</th><th>{t('settings.alerts.histNotified')}</th></tr>
      </thead>
      <tbody>
        {#each events as e (e.id)}
          <tr>
            <td class="small text-body-secondary text-nowrap">{new Date(e.created_at).toLocaleString()}</td>
            <td class="small">{e.rule_name}</td>
            <td class="small">{e.server_name || '-'}</td>
            <td><span class="badge {sevBadge(e.severity)}">{severityLabel(e.severity)}</span></td>
            <td class="small">{e.description}</td>
            <td class="small">
              {#if e.notified}
                <span class="text-success">✓</span>
              {:else if e.notify_error}
                <span class="text-danger" title={e.notify_error}>{t('settings.alerts.notifyError')}</span>
              {:else}
                <span class="text-body-secondary">-</span>
              {/if}
            </td>
          </tr>
        {:else}
          <tr><td colspan="6" class="text-body-secondary small">{t('settings.alerts.noEvents')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
  <Pagination
    page={eventsPage}
    pageCount={eventsPageCount}
    total={eventsTotal}
    pageSize={EVENTS_PAGE_SIZE}
    onchange={gotoEventsPage}
    testid="alert-events-pagination"
  />
</SettingsLayout>

<Modal title={form.id ? t('settings.alerts.editTitle') : t('settings.alerts.newTitle')} bind:open size="modal-lg">
  <div class="row g-2">
    <div class="col-md-7">
      <label class="form-label small mb-1" for="al-name">{t('common.name')}</label>
      <input id="al-name" class="form-control" placeholder={t('settings.alerts.namePlaceholder')} bind:value={form.name} />
    </div>
    <div class="col-md-5">
      <label class="form-label small mb-1" for="al-type">{t('settings.alerts.typeField')}</label>
      <select id="al-type" class="form-select" bind:value={form.type}>
        {#each TYPES as ty (ty.value)}<option value={ty.value}>{ty.label}</option>{/each}
      </select>
    </div>
  </div>

  <div class="row g-2 mt-0">
    <div class="col-md-6">
      <span class="form-label small mb-1 d-block">{t('settings.alerts.groupField')}</span>
      {#if isSelfType(form.type)}
        <!-- Keine Auswahl anbieten, die nichts bewirkt: Diese Regel bewertet
             LCM selbst, da gibt es nichts einzugrenzen. -->
        <p class="form-control-plaintext small text-body-secondary mb-0" data-testid="alert-self-scope">
          {t('settings.alerts.selfScopeHint')}
        </p>
      {:else}
        <ServerGroupPicker {groups} bind:selected={form.group_ids} />
      {/if}
    </div>
    <div class="col-md-6">
      <label class="form-label small mb-1" for="al-channel">{t('settings.alerts.channelField')}</label>
      <select id="al-channel" class="form-select" bind:value={form.channel_id}>
        <option value="">{t('settings.alerts.channelLogOnly')}</option>
        {#each channels as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
      </select>
    </div>
  </div>

  <hr class="my-3" />

  <!-- Typ-abhängige Schwellenwerte -->
  {#if form.type === 'disk_capacity'}
    <label class="form-label small mb-1" for="al-pct">{t('settings.alerts.diskLabel')}</label>
    <input id="al-pct" type="number" min="1" max="100" class="form-control" bind:value={form.threshold_percent} />
  {:else if form.type === 'storage_forecast'}
    <label class="form-label small mb-1" for="al-fdays">{t('settings.alerts.forecastLabel')}</label>
    <input id="al-fdays" type="number" min="1" class="form-control" bind:value={form.forecast_days} />
  {:else if form.type === 'security_cve' || form.type === 'advisory'}
    <label class="form-label small mb-1" for="al-minsev">{t('settings.alerts.minSevLabel')}</label>
    <select id="al-minsev" class="form-select" bind:value={form.min_severity}>
      <option value="low">{t('severity.low')}</option>
      <option value="medium">{t('severity.medium')}</option>
      <option value="high">{t('severity.high')}</option>
      <option value="critical">{t('severity.critical')}</option>
    </select>
  {:else if form.type === 'failed_updates'}
    <label class="form-label small mb-1" for="al-outdated">{t('settings.alerts.maxOutdatedLabel')}</label>
    <input id="al-outdated" type="number" min="0" class="form-control" bind:value={form.max_outdated} />
  {:else if form.type === 'heartbeat'}
    <label class="form-label small mb-1" for="al-hb">{t('settings.alerts.heartbeatLabel')}</label>
    <input id="al-hb" type="number" min="1" class="form-control" bind:value={form.heartbeat_hours} />
  {:else if form.type === 'reboot_required'}
    <p class="small text-body-secondary mb-0">{t('settings.alerts.rebootHint')}</p>
  {:else if form.type === 'apt_cacher_down'}
    <p class="small text-body-secondary mb-0">{t('settings.alerts.aptCacherDownHint')}</p>
  {:else if form.type === 'crowdsec_lapi_down'}
    <p class="small text-body-secondary mb-0">{t('settings.alerts.crowdsecLapiDownHint')}</p>
  {:else if form.type === 'cve_db_stale'}
    <p class="small text-body-secondary mb-0">{t('settings.alerts.cveDbStaleHint')}</p>
  {:else if form.type === 'backup_stale'}
    <p class="small text-body-secondary mb-0">{t('settings.alerts.backupStaleHint')}</p>
  {/if}
  {#if form.type === 'advisory'}
    <p class="small text-body-secondary mb-0">{t('settings.alerts.advisoryHint')}</p>
  {/if}

  <div class="row g-2 mt-0">
    <div class="col-md-4">
      <label class="form-label small mb-1" for="al-sev">{t('settings.alerts.urgencyLabel')}</label>
      <select id="al-sev" class="form-select" bind:value={form.severity}>
        {#each SEVERITIES as s (s.value)}<option value={s.value}>{s.label}</option>{/each}
      </select>
    </div>
    <div class="col-md-4">
      <label class="form-label small mb-1" for="al-cd">{t('settings.alerts.cooldownLabel')}</label>
      <input id="al-cd" type="number" min="0" class="form-control" bind:value={form.cooldown_minutes} />
      <div class="form-text">{t('settings.alerts.cooldownHint')}</div>
    </div>
    <div class="col-md-4 d-flex align-items-end">
      <div class="form-check">
        <input id="al-enabled" class="form-check-input" type="checkbox" bind:checked={form.enabled} />
        <label class="form-check-label small" for="al-enabled">{t('settings.alerts.ruleEnabled')}</label>
      </div>
    </div>
  </div>

  <div class="text-end mt-3">
    <button class="btn btn-secondary" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" onclick={save} disabled={!form.name.trim()}>
      {form.id ? t('common.save') : t('common.create')}
    </button>
  </div>
</Modal>
