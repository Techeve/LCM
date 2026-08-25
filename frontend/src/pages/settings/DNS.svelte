<script>
  // DNS-Einstellungen: die in LCM gepflegten Vorgabe-Nameserver (Auswahl beim
  // Setzen der Server-DNS) und die Test-Domains für den DNS-Verfügbarkeitstest.
  // Beide sind einfache Listen-Felder der globalen Einstellungen.
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
      notice = t('settings.dns.saved');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  load();
</script>

<SettingsLayout title={t('settings.dns.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  {#if !settings}
    <div class="small text-body-secondary">{t('common.loading')}</div>
  {:else}
    <p class="small text-body-secondary">{t('settings.dns.intro')}</p>

    <div class="card mb-3"><div class="card-body">
      <h3 class="h6">{t('settings.dns.presetsTitle')}</h3>
      <label class="form-label small mb-1" for="dns-presets">{t('settings.dns.presetsLabel')}</label>
      <textarea id="dns-presets" class="form-control font-monospace" rows="5"
        placeholder={'Cloudflare = 1.1.1.1\nGoogle = 8.8.8.8'}
        bind:value={settings.dns_server_presets} data-testid="dns-presets"></textarea>
      <div class="form-text">{t('settings.dns.presetsHint')}</div>
    </div></div>

    <div class="card mb-3"><div class="card-body">
      <h3 class="h6">{t('settings.dns.domainsTitle')}</h3>
      <label class="form-label small mb-1" for="dns-domains">{t('settings.dns.domainsLabel')}</label>
      <textarea id="dns-domains" class="form-control font-monospace" rows="5"
        placeholder={'deb.debian.org\ngithub.com'}
        bind:value={settings.dns_test_domains} data-testid="dns-domains"></textarea>
      <div class="form-text">{t('settings.dns.domainsHint')}</div>
    </div></div>

    <button class="btn btn-primary" onclick={save} disabled={saving} data-testid="dns-save">
      {t('common.save')}
    </button>
  {/if}
</SettingsLayout>
