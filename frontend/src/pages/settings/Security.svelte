<script>
  // Einstellungen → Sicherheit: alles, was die Schwachstellen-Bewertung
  // steuert, an einem Ort - der tägliche CVE-Scan (Trivy) und die
  // Frühwarnung (OSV). Beide Blöcke standen vorher unter „Allgemein",
  // zwischen Mail und Log-Aufbewahrung; wer sie suchte, fand sie nicht.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import { toasts } from '../../stores/toast.svelte.js';
  import { waitForJob } from '../../lib/jobs.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';

  const t = (k, p) => i18n.t(k, p);

  let settings = $state(null);
  let status = $state(null);
  let error = $state('');
  let notice = $state('');
  let mirroring = $state(false);

  async function load() {
    error = '';
    try {
      settings = await api.system.getSettings();
      status = await api.system.advisoryStatus();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function save(event) {
    event.preventDefault();
    error = '';
    notice = '';
    try {
      settings = await api.system.updateSettings(settings);
      // Der Zustand hängt an den Einstellungen (eine lokale Kopie ohne Daten
      // gilt als „aus") - nach dem Speichern also frisch holen, statt den
      // alten Stand stehen zu lassen.
      status = await api.system.advisoryStatus();
      notice = t('settings.security.saved');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  // Der Spiegellauf laedt zig Megabyte und dauert Minuten - umso wichtiger,
  // dass sein Ergebnis ankommt. Vorher blieb es bei „gestartet": Ob etwas
  // heruntergeladen wurde oder gar nichts zu spiegeln war, erfuhr niemand.
  async function mirrorNow() {
    mirroring = true;
    toasts.clear();
    try {
      const res = await api.system.advisoryMirror();
      const started = toasts.info(t('settings.security.mirrorStarted'), { timeout: 900000 });
      const job = await waitForJob(null, res.job_id, 120);
      toasts.dismiss(started);
      if (!job) {
        toasts.info(t('settings.security.mirrorStillRunning'));
      } else if (job.status === 'failed') {
        toasts.error(t('settings.security.mirrorFailed', { reason: job.output || '' }));
      } else {
        toasts.success(job.output || t('settings.security.mirrorDone'));
      }
      status = await api.system.advisoryStatus();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      mirroring = false;
    }
  }

  const fmtWhen = (s) => {
    if (!s) return '';
    const d = new Date(s);
    return isNaN(d) ? '' : d.toLocaleString();
  };

  load();
</script>

<SettingsLayout title={t('settings.security.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  {#if settings}
    <form onsubmit={save}>
      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.security.cveTitle')}</h3>
          <p class="small text-body-secondary">
            {t('settings.security.cveIntroA')}<em>{t('settings.security.cveIntroEm')}</em>{t('settings.security.cveIntroB')}
          </p>
          <div class="form-check form-switch mb-3">
            <input class="form-check-input" type="checkbox" role="switch" id="cve-en"
              data-testid="cve-enable" bind:checked={settings.cve_scan_enabled} />
            <label class="form-check-label" for="cve-en">{t('settings.security.cveEnable')}</label>
          </div>
          <div style="max-width: 320px">
            <label class="form-label" for="cve-cron">{t('settings.security.cronLabel')}</label>
            <input id="cve-cron" class="form-control" bind:value={settings.cve_scan_cron} placeholder="0 4 * * *" />
            <div class="form-text">{t('settings.security.cveCronHint')}</div>
          </div>
          <div class="mt-3">
            <label class="form-label" for="cve-weight">{t('settings.security.cveWeightLabel')}</label>
            <textarea id="cve-weight" class="form-control font-monospace" rows="3"
              bind:value={settings.cve_high_weight_packages}
              placeholder="nginx, apache2, haproxy, openssh-server, postfix, bind9, postgresql, redis, …"></textarea>
            <div class="form-text">{t('settings.security.cveWeightHint')}</div>
          </div>
        </div>
      </div>

      <div class="card mb-3">
        <div class="card-body">
          <h3 class="h6">{t('settings.security.advisoryTitle')}</h3>
          <p class="small text-body-secondary">{t('settings.security.advisoryIntro')}</p>
          <!-- Bewusst opt-in: Die Abfrage schickt den (deduplizierten,
               serverlosen) Paketbestand an einen fremden Dienst. Das steht
               hier im Klartext - es ist die Entscheidung des Betreibers. -->
          <div class="alert alert-secondary py-2 small">{t('settings.security.advisoryPrivacy')}</div>
          <div class="form-check form-switch mb-3">
            <input class="form-check-input" type="checkbox" role="switch" id="adv-en"
              data-testid="advisory-enable" bind:checked={settings.advisory_polling_enabled} />
            <label class="form-check-label" for="adv-en">{t('settings.security.advisoryEnable')}</label>
          </div>
          <div class="form-check form-switch mb-3">
            <input class="form-check-input" type="checkbox" role="switch" id="adv-local"
              data-testid="advisory-local" bind:checked={settings.advisory_local_copy} />
            <label class="form-check-label" for="adv-local">{t('settings.security.advisoryLocalLabel')}</label>
            <div class="form-text">{t('settings.security.advisoryLocalHint')}</div>
          </div>
          <div style="max-width: 320px">
            <label class="form-label" for="adv-ttl">{t('settings.security.advisoryTtlLabel')}</label>
            <input id="adv-ttl" type="number" min="0" max="30" class="form-control"
              data-testid="advisory-ttl" bind:value={settings.advisory_cache_ttl_minutes} />
            <div class="form-text">{t('settings.security.advisoryTtlHint')}</div>
          </div>

          <!-- Betriebszustand: Wann wurde zuletzt geprueft, und steht im
               lokalen Betrieb ueberhaupt eine Kopie bereit? Ohne diese
               Angaben ist eine leere Fundliste nicht von „noch nie
               nachgesehen" zu unterscheiden. -->
          {#if status}
            <hr class="my-3" />
            <dl class="row mb-0 small" data-testid="advisory-status">
              <dt class="col-sm-4">{t('settings.security.stateLabel')}</dt>
              <dd class="col-sm-8">
                {status.enabled ? t('settings.security.stateActive') : t('settings.security.stateOff')}
                {#if status.local_copy}<span class="badge border text-body-secondary ms-1">{t('settings.security.stateLocal')}</span>{/if}
              </dd>
              <dt class="col-sm-4">{t('settings.security.lastPollLabel')}</dt>
              <dd class="col-sm-8">{status.last_poll_at ? fmtWhen(status.last_poll_at) : t('settings.security.never')}</dd>
              {#if status.local_copy}
                <dt class="col-sm-4">{t('settings.security.mirrorLabel')}</dt>
                <dd class="col-sm-8">
                  {status.mirrored_at ? fmtWhen(status.mirrored_at) : t('settings.security.never')}
                  <button type="button" class="btn btn-sm btn-outline-secondary ms-2"
                    data-testid="advisory-mirror-now"
                    disabled={mirroring || !status.mirrorable?.length} onclick={mirrorNow}>
                    {t('settings.security.mirrorNow')}
                  </button>
                  {#if status.mirrorable?.length}
                    <div class="text-body-secondary">{status.mirrorable.join(', ')}</div>
                  {:else}
                    <!-- Der haeufigste Fall, in dem der Knopf scheinbar nichts
                         tut: Es gibt gar keine Distribution zu spiegeln. -->
                    <div class="text-warning" data-testid="advisory-no-targets">
                      {t('settings.security.mirrorNoTargets')}
                    </div>
                  {/if}
                </dd>
              {/if}
            </dl>
          {/if}
        </div>
      </div>

      <button class="btn btn-primary">{t('common.save')}</button>
    </form>
  {/if}
</SettingsLayout>
