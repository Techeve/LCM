<script>
  // CrowdSec-Zugang für die Aktion „Sicherheit-Tools": eine Liste zentraler,
  // self-hosted LAPIs (je Name, URL, Maschinen-Login, Passwort) und ein
  // Console-Enrollment-Key. Passwort und Key sind write-only (leer =
  // unverändert); die Werte werden nie zurückgegeben. Dazu je LAPI ein
  // Erreichbarkeits-Check, die Überwachungs-Empfehlung (Alarm-Regel
  // crowdsec_lapi_down) und die Liste der angebundenen Server.
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import { auth } from '../../stores/auth.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import Modal from '../../components/Modal.svelte';

  const t = (k, p) => i18n.t(k, p);

  let settings = $state(null);
  let consoleKey = $state(''); // write-only
  let error = $state('');
  let notice = $state('');
  let saving = $state(false);

  // Zentrale LAPIs und ihr letzter Check (je ID).
  let lapis = $state([]);
  let lapiStatus = $state({});
  let checkingId = $state(null);
  let lapiOpen = $state(false);
  let lapiForm = $state({ id: 0, name: '', url: '', login: 'lcm-managed', password: '' });

  // Angebundene Server: CrowdSec installiert UND meldet laut Credentials-Datei
  // an eine der hier hinterlegten LAPIs.
  let servers = $state([]);
  let lapiByUrl = $derived(new Map(lapis.map((l) => [l.url, l])));
  let connected = $derived(servers.filter((s) => s.crowdsec_installed && lapiByUrl.has(s.crowdsec_lapi_url)));
  const connectedCount = (lapi) => connected.filter((s) => s.crowdsec_lapi_url === lapi.url).length;

  // Überwachung: bestehende crowdsec_lapi_down-Alarmregel + letztes Ereignis.
  let alertRules = $state([]);
  let alertEvents = $state([]);
  const canAlerts = auth.can('alerts:manage');
  let lapiDownRule = $derived(alertRules.find((r) => r.type === 'crowdsec_lapi_down') ?? null);
  let lastLapiEvent = $derived(alertEvents.find((e) => e.type === 'crowdsec_lapi_down') ?? null);
  let creatingRule = $state(false);

  async function load() {
    error = '';
    try {
      [settings, lapis] = await Promise.all([api.system.getSettings(), api.system.crowdsecLapis()]);
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

  async function checkLapi(lapi) {
    checkingId = lapi.id;
    error = '';
    try {
      lapiStatus = { ...lapiStatus, [lapi.id]: await api.system.crowdsecLapiStatus(lapi.id) };
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      checkingId = null;
    }
  }

  function openLapi(lapi) {
    lapiForm = lapi
      ? { id: lapi.id, name: lapi.name, url: lapi.url, login: lapi.login, password: '' }
      : { id: 0, name: '', url: '', login: 'lcm-managed', password: '' };
    lapiOpen = true;
  }

  async function saveLapi() {
    error = '';
    try {
      await api.system.saveCrowdsecLapi(lapiForm);
      lapiOpen = false;
      lapis = await api.system.crowdsecLapis();
      notice = t('settings.crowdsec.lapiSaved', { name: lapiForm.name });
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function deleteLapi(lapi) {
    if (!confirm(t('settings.crowdsec.lapiConfirmDelete', { name: lapi.name }))) return;
    error = '';
    try {
      await api.system.deleteCrowdsecLapi(lapi.id);
      lapis = await api.system.crowdsecLapis();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  // Empfohlene Überwachungs-Regel per Klick anlegen (Kanal wählt man danach
  // unter Einstellungen → Alarme - bis dahin wird nur die Historie geführt).
  async function createMonitorRule() {
    creatingRule = true;
    error = '';
    try {
      await api.alerts.createRule({
        name: t('settings.crowdsec.monitorRuleName'),
        type: 'crowdsec_lapi_down',
        enabled: true,
        severity: 'critical',
        cooldown_minutes: 60,
      });
      alertRules = await api.alerts.listRules();
      notice = t('settings.crowdsec.monitorRuleCreated');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      creatingRule = false;
    }
  }

  async function saveConsole() {
    saving = true;
    error = '';
    notice = '';
    try {
      await api.system.updateSettings({ crowdsec_console_key: consoleKey });
      // Frisch laden: nur GET liefert das abgeleitete Flag crowdsec_console_configured.
      settings = await api.system.getSettings();
      consoleKey = '';
      notice = t('settings.crowdsec.saved');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  load();
</script>

<SettingsLayout title={t('settings.crowdsec.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  {#if !settings}
    <div class="small text-body-secondary">{t('common.loading')}</div>
  {:else}
    <p class="small text-body-secondary">{t('settings.crowdsec.intro')}</p>

    <div class="card mb-3"><div class="card-body">
      <div class="d-flex justify-content-between align-items-center mb-2">
        <h3 class="h6 mb-0">{t('settings.crowdsec.lapiTitle')}</h3>
        <button class="btn btn-sm btn-primary" onclick={() => openLapi(null)} data-testid="cs-lapi-add">{t('settings.crowdsec.lapiAdd')}</button>
      </div>
      <p class="small text-body-secondary">{t('settings.crowdsec.lapiIntro')}</p>
      {#if lapis.length === 0}
        <p class="small text-body-secondary mb-0" data-testid="cs-lapi-none">{t('settings.crowdsec.lapiNone')}</p>
      {:else}
        <div class="table-responsive">
          <table class="table table-sm align-middle mb-0" data-testid="cs-lapi-table">
            <thead><tr>
              <th>{t('settings.crowdsec.lapiName')}</th>
              <th>{t('settings.crowdsec.lapiUrl')}</th>
              <th>{t('settings.crowdsec.colServers')}</th>
              <th>{t('settings.crowdsec.colStatus')}</th>
              <th></th>
            </tr></thead>
            <tbody>
              {#each lapis as lapi (lapi.id)}
                {@const st = lapiStatus[lapi.id]}
                <tr data-testid="cs-lapi-row">
                  <td class="fw-semibold">{lapi.name}</td>
                  <td>
                    <div class="font-monospace small">{lapi.url}</div>
                    <div class="small text-body-secondary">{t('settings.crowdsec.lapiLogin')}: <span class="font-monospace">{lapi.login}</span></div>
                  </td>
                  <td class="small">{connectedCount(lapi)}</td>
                  <td>
                    {#if st}
                      {#if st.running}
                        <span class="badge text-bg-success" data-testid="cs-status-badge" title={st.message}>{t('settings.crowdsec.statusOk')}</span>
                      {:else if st.reachable}
                        <span class="badge text-bg-warning" data-testid="cs-status-badge" title={st.message}>{t('settings.crowdsec.statusLoginFailed')}</span>
                      {:else}
                        <span class="badge text-bg-danger" data-testid="cs-status-badge" title={st.message}>{t('settings.crowdsec.statusDown')}</span>
                      {/if}
                    {:else}
                      <span class="text-body-secondary small">-</span>
                    {/if}
                  </td>
                  <td>
                    <div class="d-flex flex-wrap justify-content-end gap-1">
                    <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => checkLapi(lapi)} disabled={checkingId === lapi.id} data-testid="cs-check">
                      {checkingId === lapi.id ? t('common.loading') : t('settings.crowdsec.checkNow')}
                    </button>
                    <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openLapi(lapi)} data-testid="cs-lapi-edit">{t('common.edit')}</button>
                    <button class="btn btn-sm btn-outline-danger py-0" onclick={() => deleteLapi(lapi)} aria-label={t('settings.crowdsec.lapiDelete')} data-testid="cs-lapi-delete">×</button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div></div>

    <!-- Überwachung: Empfehlung für die crowdsec_lapi_down-Alarm-Regel. -->
    {#if lapis.length > 0}
      <div class="card mb-3"><div class="card-body">
        <h3 class="h6">{t('settings.crowdsec.monitorTitle')}</h3>
        <p class="small text-body-secondary">{t('settings.crowdsec.monitorIntro')}</p>
        {#if canAlerts}
          {#if lapiDownRule}
            {#if lapiDownRule.enabled && lapiDownRule.channel_id}
              <p class="small mb-1"><span class="badge text-bg-success me-1">{t('settings.crowdsec.monitorActive')}</span>{t('settings.crowdsec.monitorActiveHint')}</p>
            {:else if lapiDownRule.enabled}
              <p class="small mb-1"><span class="badge text-bg-warning me-1">{t('settings.crowdsec.monitorLogOnly')}</span>{t('settings.crowdsec.monitorLogOnlyHint')}</p>
            {:else}
              <p class="small mb-1"><span class="badge text-bg-secondary me-1">{t('settings.crowdsec.monitorDisabled')}</span>{t('settings.crowdsec.monitorDisabledHint')}</p>
            {/if}
          {:else}
            <p class="small mb-2"><span class="badge text-bg-secondary me-1">{t('settings.crowdsec.monitorNone')}</span>{t('settings.crowdsec.monitorNoneHint')}</p>
            <button class="btn btn-sm btn-outline-primary me-2" onclick={createMonitorRule} disabled={creatingRule} data-testid="cs-create-rule">
              {t('settings.crowdsec.monitorCreateRule')}
            </button>
          {/if}
          {#if lastLapiEvent}
            <p class="small text-body-secondary mb-2">
              {t('settings.crowdsec.monitorLastEvent')}: {new Date(lastLapiEvent.created_at).toLocaleString()} - {lastLapiEvent.description}
            </p>
          {/if}
          <a class="btn btn-sm btn-outline-secondary" href="/settings/alerts" use:link data-testid="cs-alerts-link">
            {t('settings.crowdsec.monitorManage')}
          </a>
        {:else}
          <p class="small text-body-secondary mb-0">{t('settings.crowdsec.monitorNoPermission')}</p>
        {/if}
      </div></div>

      <!-- Angebundene Server: melden laut Credentials-Datei an diese LAPI. -->
      {#if auth.can('servers:read')}
        <div class="card mb-3"><div class="card-body">
          <h3 class="h6">{t('settings.crowdsec.connectedTitle')}</h3>
          <p class="small text-body-secondary">{t('settings.crowdsec.connectedIntro')}</p>
          {#if connected.length === 0}
            <p class="small text-body-secondary mb-0" data-testid="cs-none-connected">{t('settings.crowdsec.noneConnected')}</p>
          {:else}
            <div class="table-responsive">
              <table class="table table-sm align-middle mb-0" data-testid="cs-connected-table">
                <thead><tr>
                  <th>{t('settings.crowdsec.colServer')}</th>
                  <th>{t('settings.crowdsec.colHost')}</th>
                  <th>{t('settings.crowdsec.colLapi')}</th>
                  <th>{t('settings.crowdsec.colMode')}</th>
                  <th>{t('settings.crowdsec.colActive')}</th>
                </tr></thead>
                <tbody>
                  {#each connected as s (s.id)}
                    <tr>
                      <td><a href={`/servers/${s.id}`} use:link class="fw-semibold text-decoration-none">{s.name}</a></td>
                      <td class="font-monospace small">{s.host}</td>
                      <td class="small">{lapiByUrl.get(s.crowdsec_lapi_url)?.name}</td>
                      <td>
                        {#if s.crowdsec_lapi_mode}
                          <span class="badge text-bg-secondary">{s.crowdsec_lapi_mode}</span>
                        {:else}
                          <span class="text-body-secondary small">-</span>
                        {/if}
                      </td>
                      <td>
                        {#if s.crowdsec_active}
                          <span class="badge text-bg-success">{t('settings.crowdsec.agentActive')}</span>
                        {:else}
                          <span class="badge text-bg-warning">{t('settings.crowdsec.agentInactive')}</span>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        </div></div>
      {/if}
    {/if}

    <div class="card mb-3"><div class="card-body">
      <div class="d-flex justify-content-between align-items-center mb-2">
        <h3 class="h6 mb-0">{t('settings.crowdsec.consoleTitle')}</h3>
        {#if settings.crowdsec_console_configured}<span class="badge text-bg-success">{t('settings.crowdsec.configured')}</span>{/if}
      </div>
      <label class="form-label small mb-1" for="cs-key">{t('settings.crowdsec.consoleKey')}</label>
      <input id="cs-key" type="password" class="form-control font-monospace" placeholder={settings.crowdsec_console_configured ? t('settings.crowdsec.unchanged') : ''} bind:value={consoleKey} data-testid="cs-console-key" />
      <div class="form-text">{t('settings.crowdsec.consoleHint')}</div>
    </div></div>

    <button class="btn btn-primary" onclick={saveConsole} disabled={saving || !consoleKey} data-testid="cs-save">{t('settings.crowdsec.consoleSave')}</button>
  {/if}
</SettingsLayout>

<Modal title={lapiForm.id ? t('settings.crowdsec.lapiEditTitle') : t('settings.crowdsec.lapiAddTitle')} bind:open={lapiOpen}>
  <div class="mb-2">
    <label class="form-label small mb-1" for="cs-name">{t('settings.crowdsec.lapiName')}</label>
    <input id="cs-name" class="form-control" placeholder={t('settings.crowdsec.lapiNamePlaceholder')} bind:value={lapiForm.name} data-testid="cs-lapi-name" />
  </div>
  <div class="mb-2">
    <label class="form-label small mb-1" for="cs-url">{t('settings.crowdsec.lapiUrl')}</label>
    <input id="cs-url" class="form-control font-monospace" placeholder="http://lapi.example:8080" bind:value={lapiForm.url} data-testid="cs-lapi-url" />
  </div>
  <div class="mb-2">
    <label class="form-label small mb-1" for="cs-login">{t('settings.crowdsec.lapiLogin')}</label>
    <input id="cs-login" class="form-control font-monospace" bind:value={lapiForm.login} data-testid="cs-lapi-login" />
  </div>
  <div class="mb-1">
    <label class="form-label small mb-1" for="cs-pw">{t('settings.crowdsec.lapiPassword')}</label>
    <input id="cs-pw" type="password" class="form-control" placeholder={lapiForm.id ? t('settings.crowdsec.unchanged') : ''} bind:value={lapiForm.password} data-testid="cs-lapi-pw" />
  </div>
  <div class="form-text mb-3">{t('settings.crowdsec.lapiHint')}</div>
  <div class="text-end">
    <button class="btn btn-secondary" onclick={() => (lapiOpen = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" onclick={saveLapi} disabled={!lapiForm.name || !lapiForm.url || !lapiForm.login || (!lapiForm.id && !lapiForm.password)} data-testid="cs-lapi-save">{t('common.save')}</button>
  </div>
</Modal>
