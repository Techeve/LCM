<script>
  // Zeit-Einstellungen: die Vorgabe-Zeitserver (Auswahl beim Einrichten von
  // NTP auf einem Server) und die Standard-Zeitzone, mit der das Formular
  // vorbelegt wird. Aufbau bewusst wie die DNS-Seite - dieselbe
  // „Label = Wert"-Schreibweise, damit beide Listen gleich zu pflegen sind.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';

  const t = (k, p) => i18n.t(k, p);

  let settings = $state(null);
  let error = $state('');
  let notice = $state('');
  let saving = $state(false);

  async function load() {
    error = '';
    try {
      settings = await api.system.getSettings();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function save() {
    saving = true;
    error = '';
    notice = '';
    try {
      settings = await api.system.updateSettings(settings);
      notice = t('settings.time.saved');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  load();
</script>

<SettingsLayout title={t('settings.nav.time')}>
  {#if error}<div class="alert alert-danger" role="alert">{error}</div>{/if}
  {#if notice}<div class="alert alert-success" role="status">{notice}</div>{/if}

  {#if !settings}
    <div class="small text-body-secondary">{t('common.loading')}</div>
  {:else}
    <p class="small text-body-secondary">{t('settings.time.intro')}</p>

    <div class="card mb-3"><div class="card-body">
      <h3 class="h6">{t('settings.time.presetsTitle')}</h3>
      <label class="form-label small mb-1" for="ntp-presets">{t('settings.time.presetsLabel')}</label>
      <textarea id="ntp-presets" class="form-control font-monospace" rows="6"
        placeholder={'NTP-Pool (1) = 0.pool.ntp.org\nCloudflare = time.cloudflare.com'}
        bind:value={settings.ntp_server_presets} data-testid="ntp-presets"></textarea>
      <div class="form-text">{t('settings.time.presetsHint')}</div>
    </div></div>

    <div class="card mb-3"><div class="card-body">
      <h3 class="h6">{t('settings.time.timezoneTitle')}</h3>
      <label class="form-label small mb-1" for="default-tz">{t('settings.time.timezoneLabel')}</label>
      <input id="default-tz" class="form-control font-monospace" style="max-width: 26rem;"
        placeholder="Europe/Berlin"
        bind:value={settings.default_timezone} data-testid="default-timezone" />
      <div class="form-text">{t('settings.time.timezoneHint')}</div>
    </div></div>

    <button class="btn btn-primary" onclick={save} disabled={saving} data-testid="time-save">
      {t('common.save')}
    </button>
  {/if}
</SettingsLayout>
