<script>
  // CrowdSec-Zugang für die Aktion „Sicherheit-Tools": eine self-hosted zentrale
  // LAPI (URL + Login + Passwort) und/oder ein Console-Enrollment-Key. Passwort
  // und Key sind write-only (leer = unverändert); die Werte werden nie zurückgegeben.
  // Dazu: LAPI-Erreichbarkeits-Check, Überwachungs-Empfehlung (Alarm-Regel
  // crowdsec_lapi_down) und die Liste der an die LAPI angebundenen Server.
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import { auth } from '../../stores/auth.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';

  const t = (k, p) => i18n.t(k, p);

  let settings = $state(null);
  let lapiPassword = $state(''); // write-only
  let consoleKey = $state(''); // write-only
  let error = $state('');
  let notice = $state('');
  let saving = $state(false);

  // LAPI-Erreichbarkeits-Check (Login-Probe vom LCM-Host aus).
  let lapiStatus = $state(null);
  let checking = $state(false);

  // Angebundene Server: CrowdSec installiert UND meldet laut Credentials-Datei
  // an die hier konfigurierte LAPI-URL.
  let servers = $state([]);
  let connected = $derived(
    servers.filter(
      (s) => s.crowdsec_installed && s.crowdsec_lapi_url && settings?.crowdsec_lapi_url && s.crowdsec_lapi_url === settings.crowdsec_lapi_url,
    ),
  );

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
      settings = await api.system.getSettings();
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

  async function checkLapi() {
    checking = true;
    error = '';
    try {
      lapiStatus = await api.system.crowdsecStatus();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      checking = false;
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

  async function save() {
    saving = true;
    error = '';
    notice = '';
    try {
      const payload = {
        ...settings,
        crowdsec_lapi_password: lapiPassword, // leer = unverändert
        crowdsec_console_key: consoleKey,
      };
      await api.system.updateSettings(payload);
      // Frisch laden statt die PATCH-Antwort zu übernehmen: nur GET liefert
      // die abgeleiteten Flags (crowdsec_lapi_configured), an denen die
      // Status-/Überwachungs-/Server-Karten hängen.
      settings = await api.system.getSettings();
      lapiPassword = '';
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
        {#if settings.crowdsec_lapi_configured}<span class="badge text-bg-success">{t('settings.crowdsec.configured')}</span>{/if}
      </div>
      <div class="mb-2">
        <label class="form-label small mb-1" for="cs-url">{t('settings.crowdsec.lapiUrl')}</label>
        <input id="cs-url" class="form-control font-monospace" placeholder="http://lapi.example:8080" bind:value={settings.crowdsec_lapi_url} data-testid="cs-lapi-url" />
      </div>
      <div class="mb-2">
        <label class="form-label small mb-1" for="cs-login">{t('settings.crowdsec.lapiLogin')}</label>
        <input id="cs-login" class="form-control font-monospace" bind:value={settings.crowdsec_lapi_login} />
      </div>
      <div class="mb-1">
        <label class="form-label small mb-1" for="cs-pw">{t('settings.crowdsec.lapiPassword')}</label>
        <input id="cs-pw" type="password" class="form-control" placeholder={settings.crowdsec_lapi_configured ? t('settings.crowdsec.unchanged') : ''} bind:value={lapiPassword} data-testid="cs-lapi-pw" />
      </div>
      <div class="form-text">{t('settings.crowdsec.lapiHint')}</div>
      {#if settings.crowdsec_lapi_configured}
        <div class="d-flex align-items-center gap-2 mt-2">
          <button class="btn btn-sm btn-outline-secondary" onclick={checkLapi} disabled={checking} data-testid="cs-check">
            {checking ? t('common.loading') : t('settings.crowdsec.checkNow')}
          </button>
          {#if lapiStatus}
            {#if lapiStatus.running}
              <span class="badge text-bg-success" data-testid="cs-status-badge">{t('settings.crowdsec.statusOk')}</span>
            {:else if lapiStatus.reachable}
              <span class="badge text-bg-warning" data-testid="cs-status-badge">{t('settings.crowdsec.statusLoginFailed')}</span>
            {:else}
              <span class="badge text-bg-danger" data-testid="cs-status-badge">{t('settings.crowdsec.statusDown')}</span>
            {/if}
            <span class="small text-body-secondary">{lapiStatus.message}</span>
          {/if}
        </div>
      {/if}
    </div></div>

    <!-- Überwachung: Empfehlung für die crowdsec_lapi_down-Alarm-Regel. -->
    {#if settings.crowdsec_lapi_configured}
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
                  <th>{t('settings.crowdsec.colMode')}</th>
                  <th>{t('settings.crowdsec.colActive')}</th>
                </tr></thead>
                <tbody>
                  {#each connected as s (s.id)}
                    <tr>
                      <td><a href={`/servers/${s.id}`} use:link class="fw-semibold text-decoration-none">{s.name}</a></td>
                      <td class="font-monospace small">{s.host}</td>
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

    <button class="btn btn-primary" onclick={save} disabled={saving} data-testid="cs-save">{t('common.save')}</button>
  {/if}
</SettingsLayout>
