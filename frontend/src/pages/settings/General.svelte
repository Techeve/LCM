<script>
  // Allgemeine Systemeinstellungen: Default-SSH-Zugang, Log-Retention
  // (steuert auch den Cleanup-Schedule) und 2FA-Enforcement.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';

  const t = (k, p) => i18n.t(k, p);

  let settings = $state(null);

  // 2FA-Pflicht für Administratoren: im regulären Betrieb voreingestellt.
  // Fehlt sie, warnt die Seite ausdrücklich - das Admin-Konto verwaltet die
  // SSH-Zugänge der gesamten Flotte.
  let adminRequires2FA = $derived(
    (settings?.require_2fa_roles ?? '')
      .split(',')
      .map((r) => r.trim().toLowerCase())
      .includes('admin'),
  );
  let error = $state('');
  let notice = $state('');
  let copied = $state(false);

  // Standard-E-Mail-Versand: Passwort ist write-only (leer = unverändert);
  // mailChannel spiegelt den verwalteten Benachrichtigungskanal (system_email).
  let mailPassword = $state('');
  let mailChannel = $state(false);
  let mailTesting = $state(false);

  async function copyOnboardingKey() {
    try {
      await navigator.clipboard.writeText(settings.onboarding_pub_key);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch {
      // Clipboard-API nicht verfügbar (z.B. ohne HTTPS) - still ignorieren.
    }
  }

  async function load() {
    error = '';
    try {
      [settings, { enabled: mailChannel }] = await Promise.all([
        api.system.getSettings(),
        api.system.getMailChannel(),
      ]);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function save(event) {
    event.preventDefault();
    error = '';
    notice = '';
    try {
      settings = await api.system.updateSettings({
        ...settings,
        mail_password: mailPassword,
      });
      await api.system.setMailChannel(!!(mailChannel && settings.mail_enabled));
      mailChannel = !!(mailChannel && settings.mail_enabled);
      mailPassword = '';
      notice = t('settings.general.saved');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  // Testnachricht an die Admin-Empfänger - prüft die GESPEICHERTE
  // Konfiguration (ungespeicherte Änderungen vorher speichern).
  async function testMail() {
    error = '';
    notice = '';
    mailTesting = true;
    try {
      await api.system.testMail();
      notice = t('settings.general.testSent');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      mailTesting = false;
    }
  }

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.general.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  {#if settings}
    <!-- Reihenfolge: erst Onboarding (SSH-Zugang + System-Key), dann
         Sicherheit (2FA-Pflicht), dann Scans, dann Aufbewahrung. -->
    <form onsubmit={save}>
      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.general.sshAccessTitle')}</h3>
          <div class="row g-3">
            <div class="col-md-6">
              <label class="form-label" for="dsu">{t('settings.general.user')}</label>
              <input id="dsu" class="form-control" bind:value={settings.default_ssh_user} />
            </div>
            <div class="col-md-3">
              <label class="form-label" for="dsp">{t('settings.general.port')}</label>
              <input id="dsp" type="number" class="form-control" bind:value={settings.default_ssh_port} />
            </div>
          </div>
        </div>
      </div>

      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.general.onboardingKeyTitle')}</h3>
          <p class="small text-body-secondary">
            {t('settings.general.onboardingHintA')}<strong>{t('settings.general.onboardingHintBold')}</strong>{t('settings.general.onboardingHintB')}<code>~/.ssh/authorized_keys</code>{t('settings.general.onboardingHintC')}<code>root</code>{t('settings.general.onboardingHintD')}
          </p>
          {#if settings.onboarding_pub_key}
            <div class="input-group input-group-sm">
              <input class="form-control font-monospace" style="font-size: .8rem" readonly value={settings.onboarding_pub_key} />
              <button type="button" class="btn btn-outline-secondary" onclick={copyOnboardingKey}>{copied ? t('settings.general.copied') : t('common.copy')}</button>
            </div>
          {:else}
            <span class="text-body-secondary small">{t('settings.general.noOnboardingKey')}</span>
          {/if}
        </div>
      </div>

      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.general.twofaTitle')}</h3>
          <label class="form-label" for="r2fa">{t('settings.general.twofaLabel')}</label>
          <input id="r2fa" class="form-control" bind:value={settings.require_2fa_roles} placeholder="admin,manager" />
          <div class="form-text">{t('settings.general.twofaHint')}</div>
          {#if !adminRequires2FA}
            <div class="alert alert-warning mt-2 mb-0 small" data-testid="twofa-warning">
              {t('settings.general.twofaAdminWarning')}
            </div>
          {/if}
        </div>
      </div>

      <!-- Öffentliche Basis-Adresse: einzige Quelle für Links in Mails.
           Ohne sie kann LCM keinen von außen erreichbaren Link erzeugen -
           aus dem Host-Header darf sie bewusst NICHT stammen. -->
      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.general.baseUrlTitle')}</h3>
          <label class="form-label" for="pburl">{t('settings.general.baseUrlLabel')}</label>
          <input id="pburl" class="form-control" bind:value={settings.public_base_url}
            placeholder="https://lcm.example.com" data-testid="public-base-url" />
          <div class="form-text">{t('settings.general.baseUrlHint')}</div>
          {#if !(settings.public_base_url ?? '').trim()}
            <div class="alert alert-warning mt-2 mb-0 small" data-testid="base-url-warning">
              {t('settings.general.baseUrlWarning')}
            </div>
          {/if}
        </div>
      </div>

      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.general.sessionTitle')}</h3>
          <label class="form-label" for="sttl">{t('settings.general.sessionLabel')}</label>
          <!-- min="0", NICHT min="5": 0 ist der dokumentierte Wert fuer
               „Vorgabe aus der config.json" und zugleich die Voreinstellung
               einer frischen Installation. Mit min="5" war das Feld damit von
               Anfang an ungueltig - und weil ein ungueltiges Feld den
               Submit des GESAMTEN Formulars blockiert, liess sich die Seite
               „Allgemein" ueberhaupt nicht speichern: Der Knopf reagierte
               scheinbar gar nicht. Die Unterkante 5 setzt das Backend
               (clampSessionTTL), wo sie hingehoert. -->
          <input id="sttl" type="number" min="0" max="43200" class="form-control" bind:value={settings.session_ttl_minutes} />
          <div class="form-text">
            {t('settings.general.sessionHintA')}<code>0</code>{t('settings.general.sessionHintB')}
          </div>
        </div>
      </div>

      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.general.jobsTitle')}</h3>
          <p class="small text-body-secondary">
            {t('settings.general.jobsIntro')}
          </p>
          <div style="max-width: 320px">
            <label class="form-label" for="jobidle">{t('settings.general.jobIdleLabel')}</label>
            <input id="jobidle" type="number" min="0" max="1440" class="form-control" bind:value={settings.job_idle_timeout_minutes} />
            <div class="form-text">
              {t('settings.general.jobIdleHintA')}<code>0</code>{t('settings.general.jobIdleHintB')}
            </div>
          </div>
          <div class="mt-3" style="max-width: 320px">
            <label class="form-label" for="jobidleslow">{t('settings.general.jobIdleSlowLabel')}</label>
            <input id="jobidleslow" type="number" min="0" max="1440" class="form-control" bind:value={settings.job_idle_timeout_slow_minutes} />
            <div class="form-text">{t('settings.general.jobIdleSlowHint')}</div>
          </div>
        </div>
      </div>

      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.general.mailTitle')}</h3>
          <p class="small text-body-secondary">
            {t('settings.general.mailIntroA')}<em>{t('settings.general.mailIntroEm')}</em>{t('settings.general.mailIntroB')}
          </p>
          <div class="form-check form-switch mb-3">
            <input class="form-check-input" type="checkbox" role="switch" id="mail-en" bind:checked={settings.mail_enabled} />
            <label class="form-check-label" for="mail-en">{t('settings.general.mailEnable')}</label>
          </div>
          <div class="row g-3">
            <div class="col-md-8">
              <label class="form-label" for="mail-host">{t('settings.general.smtpHost')}</label>
              <input id="mail-host" class="form-control" placeholder="smtp.example.com" bind:value={settings.mail_host} />
            </div>
            <div class="col-md-4">
              <label class="form-label" for="mail-port">{t('settings.general.port')}</label>
              <input id="mail-port" type="number" class="form-control" bind:value={settings.mail_port} />
            </div>
            <div class="col-md-6">
              <label class="form-label" for="mail-user">{t('settings.general.mailUser')}</label>
              <input id="mail-user" class="form-control" autocomplete="off" bind:value={settings.mail_username} />
            </div>
            <div class="col-md-6">
              <label class="form-label" for="mail-pass">{t('settings.general.mailPass')} <span class="text-body-secondary">{t('settings.general.unchangedNote')}</span></label>
              <input id="mail-pass" type="password" class="form-control" autocomplete="new-password" bind:value={mailPassword} />
            </div>
            <div class="col-md-6">
              <label class="form-label" for="mail-from">{t('settings.general.mailFrom')}</label>
              <input id="mail-from" class="form-control" placeholder="lcm@example.com" bind:value={settings.mail_from} />
            </div>
            <div class="col-md-6">
              <label class="form-label" for="mail-admins">{t('settings.general.mailAdmins')}</label>
              <input id="mail-admins" class="form-control" placeholder="admin@example.com" bind:value={settings.mail_admin_recipients} />
            </div>
          </div>
          <div class="form-check mt-3">
            <input id="mail-tls" class="form-check-input" type="checkbox" bind:checked={settings.mail_use_tls} />
            <label class="form-check-label small" for="mail-tls">{t('settings.general.mailTls')}</label>
          </div>
          <div class="form-check mt-1">
            <input id="mail-channel" class="form-check-input" type="checkbox" bind:checked={mailChannel} disabled={!settings.mail_enabled} />
            <label class="form-check-label small" for="mail-channel">
              {t('settings.general.mailChannelLabelA')}{t('settings.general.mailChannelName')}{t('settings.general.mailChannelLabelB')}
            </label>
          </div>
          <div class="d-flex align-items-center gap-2 mt-3">
            <button type="button" class="btn btn-sm btn-outline-primary" onclick={testMail}
              disabled={mailTesting || !settings.mail_enabled}>
              {mailTesting ? t('settings.general.mailSending') : t('settings.general.mailTestBtn')}
            </button>
            <span class="form-text mt-0">{t('settings.general.mailTestHint')}</span>
          </div>
        </div>
      </div>

      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.general.logTitle')}</h3>
          <p class="small text-body-secondary">
            {t('settings.general.logIntroA')}<em>{t('settings.general.logIntroEm')}</em>{t('settings.general.logIntroB')}
          </p>
          <div style="max-width: 220px">
            <label class="form-label" for="lrd">{t('settings.general.retentionDays')}</label>
            <input id="lrd" type="number" min="0" class="form-control" bind:value={settings.log_retention_days} />
          </div>
        </div>
      </div>

      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.general.storageTitle')}</h3>
          <p class="small text-body-secondary">
            {t('settings.general.storageIntro')}
          </p>
          <div style="max-width: 220px">
            <label class="form-label" for="shr">{t('settings.general.retentionDays')}</label>
            <input id="shr" type="number" min="90" max="365" class="form-control" bind:value={settings.storage_history_retention_days} />
            <div class="form-text">{t('settings.general.storageHint')}</div>
          </div>
        </div>
      </div>

      <button class="btn btn-primary">{t('common.save')}</button>
    </form>
  {/if}
</SettingsLayout>
