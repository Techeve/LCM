<script>
  // Enterprise-Subscription: Key eintragen (Aktivierung beim Anbieter-Dienst),
  // Vertragsstand einsehen („läuft in X Tagen ab" aus dem täglichen
  // Lebenszeichen), manuell prüfen und den LCM-Host auf den
  // Enterprise-Paketkanal umstellen. Community und Enterprise sind funktional
  // identisch - hier wird nichts freigeschaltet, nur der Vertrag verwaltet.
  //
  // Feedback-Muster wie auf der Sicherheit-Seite: busy-Flag, Buttons während
  // der Aktion gesperrt, Job-Erfolg erst NACH waitForJob (nicht beim Start).
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import { toasts } from '../../stores/toast.svelte.js';
  import { waitForJob } from '../../lib/jobs.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';

  const t = (k, p) => i18n.t(k, p);

  let status = $state(null); // SubscriptionStatusView vom Backend
  let keyInput = $state('');
  let serviceUrl = $state('');
  let showAdvanced = $state(false);
  let busy = $state(false);
  let loaded = $state(false);
  // Die Auswahl im Kanal-Formular. Sie folgt dem gemeldeten Stand, bis der
  // Benutzer selbst etwas anderes wählt - umgestellt wird erst auf Klick.
  let pickedChannel = $state('community');

  const APT_CHANNELS = ['community', 'beta', 'enterprise'];

  let activeChannel = $derived(status?.apt_channel ?? 'community');
  // Der Enterprise-Kanal hängt am Vertrag; Community und Beta sind frei.
  let enterpriseAllowed = $derived(!!status?.configured && status?.status === 'active');

  // Abgeleiteter Anzeige-Zustand: Badge-Farbe + Text je Vertragsstatus.
  let badge = $derived.by(() => {
    if (!status?.configured) return null;
    switch (status.status) {
      case 'active':
        if (status.days_left != null && status.days_left <= 30)
          return { cls: 'text-bg-warning', label: t('settings.subscription.state.expiring', { days: status.days_left }) };
        return { cls: 'text-bg-success', label: t('settings.subscription.state.active') };
      case 'expired':
        return { cls: 'text-bg-danger', label: t('settings.subscription.state.expired') };
      case 'revoked':
        return { cls: 'text-bg-danger', label: t('settings.subscription.state.revoked') };
      case 'unreachable':
        return { cls: 'text-bg-secondary', label: t('settings.subscription.state.unreachable') };
      default:
        return { cls: 'text-bg-secondary', label: status.status };
    }
  });

  async function load() {
    // Bewusst KEIN toasts.clear() hier - load() läuft nach Aktionen, deren
    // Erfolgsmeldung stehen bleiben muss.
    try {
      status = await api.system.subscriptionStatus();
      serviceUrl = status.service_url ?? '';
      pickedChannel = activeChannel;
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      loaded = true;
    }
  }

  async function activate() {
    toasts.clear();
    busy = true;
    try {
      status = await api.system.subscriptionActivate(keyInput.trim(), showAdvanced ? serviceUrl.trim() : '');
      keyInput = '';
      toasts.success(t('settings.subscription.notices.activated', { customer: status.customer }));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  async function verifyNow() {
    toasts.clear();
    busy = true;
    try {
      status = await api.system.subscriptionVerify();
      if (status.last_error) toasts.error(status.last_error);
      else toasts.success(t('settings.subscription.notices.verified'));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  async function removeSubscription() {
    if (!confirm(t('settings.subscription.confirmRemove'))) return;
    toasts.clear();
    busy = true;
    try {
      status = await api.system.subscriptionRemove();
      toasts.success(t('settings.subscription.notices.removed'));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  // apt-Umstellung: läuft als Job auf dem LCM-Host - Erfolg erst melden,
  // wenn der Job WIRKLICH durch ist (waitForJob), nie beim POST.
  // Rückfrage nur bei einem echten Wechsel: das erneute Anwenden des aktiven
  // Kanals ändert nichts, was zu bestätigen wäre.
  async function switchChannel(channel) {
    const label = t(`settings.subscription.apt.job.${channel}`);
    if (channel !== activeChannel && channel !== 'enterprise') {
      const ask = channel === 'beta' ? 'confirmBeta' : 'confirmCommunity';
      if (!confirm(t(`settings.subscription.apt.${ask}`))) return;
    }
    toasts.clear();
    busy = true;
    try {
      const job = await api.system.subscriptionAptChannel(channel);
      const started = toasts.info(t('settings.subscription.notices.jobStarted', { label }), { timeout: 120000 });
      const done = await waitForJob(job.server_id, job.id);
      toasts.dismiss(started);
      if (done?.status === 'success') toasts.success(t('settings.subscription.notices.jobDone', { label }));
      else if (done) toasts.error(t('settings.subscription.notices.jobFailed', { label }));
      else toasts.info(t('settings.subscription.notices.jobStillRunning', { label }));
      await load();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
      busy = false;
      return;
    }
    busy = false;
  }

  function fmtDateTime(iso) {
    if (!iso) return '-';
    try {
      return new Date(iso).toLocaleString(i18n.lang === 'de' ? 'de-DE' : 'en-GB');
    } catch {
      return iso;
    }
  }

  load();
</script>

<SettingsLayout title={t('settings.subscription.title')}>
  {#if loaded && status}
    {#if !status.configured}
      <!-- Noch keine Subscription: Erklärung + Aktivierungs-Formular. -->
      <div class="card mb-4" data-testid="subscription-activate-card">
        <div class="card-body">
          <h3 class="h6">{t('settings.subscription.activateTitle')}</h3>
          <p class="text-body-secondary small mb-3">{t('settings.subscription.intro')}</p>
          <form
            onsubmit={(e) => {
              e.preventDefault();
              activate();
            }}
          >
            <div class="row g-2 align-items-end">
              <div class="col-md-7">
                <label class="form-label" for="sub-key">{t('settings.subscription.keyLabel')}</label>
                <input
                  id="sub-key"
                  class="form-control font-monospace"
                  bind:value={keyInput}
                  placeholder="LCM-E-XXXXX-XXXXX-XXXXX-XXXXX"
                  required
                  disabled={busy}
                />
              </div>
              <div class="col-auto">
                <button class="btn btn-primary" disabled={busy || !keyInput.trim()}>
                  {#if busy}<span class="spinner-border spinner-border-sm me-1"></span>{/if}
                  {t('settings.subscription.activate')}
                </button>
              </div>
            </div>
          </form>
          <button class="btn btn-link btn-sm px-0 mt-2" onclick={() => (showAdvanced = !showAdvanced)}>
            {t('settings.subscription.advanced')}
          </button>
          {#if showAdvanced}
            <div class="mt-2 col-md-7">
              <label class="form-label" for="sub-url">{t('settings.subscription.serviceUrl')}</label>
              <input id="sub-url" class="form-control" bind:value={serviceUrl} placeholder="https://repo.techeve.de" disabled={busy} />
              <div class="form-text">{t('settings.subscription.serviceUrlHint')}</div>
            </div>
          {/if}
        </div>
      </div>
    {:else}
      <!-- Vertragsstand -->
      <div class="card mb-4" data-testid="subscription-status-card">
        <div class="card-body">
          <div class="d-flex align-items-center gap-2 mb-3">
            <h3 class="h6 mb-0">{t('settings.subscription.statusTitle')}</h3>
            {#if badge}<span class="badge {badge.cls}">{badge.label}</span>{/if}
          </div>
          <!-- Kontingent überschritten: reiner Hinweis - LCM sperrt nichts. -->
          {#if status.quota_exceeded}
            <div class="alert alert-warning small py-2" data-testid="subscription-quota-hint">
              {t('settings.subscription.quotaExceeded', {
                managed: status.managed_servers,
                included: status.included_servers,
              })}
            </div>
          {/if}
          <dl class="row mb-0 small">
            <dt class="col-sm-4">{t('settings.subscription.customer')}</dt>
            <dd class="col-sm-8">{status.customer || '-'}</dd>
            <dt class="col-sm-4">{t('settings.subscription.key')}</dt>
            <dd class="col-sm-8 font-monospace">{status.key_prefix || '-'}</dd>
            <dt class="col-sm-4">{t('settings.subscription.expires')}</dt>
            <dd class="col-sm-8">
              {#if status.expires_at}
                {status.expires_at}
                {#if status.days_left != null}
                  <span class="text-body-secondary">({t('settings.subscription.daysLeft', { days: status.days_left })})</span>
                {/if}
              {:else}
                {t('settings.subscription.unlimited')}
              {/if}
            </dd>
            {#if status.included_servers > 0}
              <dt class="col-sm-4">{t('settings.subscription.serversLabel')}</dt>
              <dd class="col-sm-8" class:text-warning={status.quota_exceeded} data-testid="subscription-quota">
                {t('settings.subscription.serversValue', {
                  included: status.included_servers,
                  managed: status.managed_servers,
                })}
              </dd>
            {/if}
            <dt class="col-sm-4">{t('settings.subscription.lastCheck')}</dt>
            <dd class="col-sm-8">{fmtDateTime(status.last_check_at)}</dd>
            {#if status.last_error}
              <dt class="col-sm-4">{t('settings.subscription.lastError')}</dt>
              <dd class="col-sm-8 text-danger">{status.last_error}</dd>
            {/if}
          </dl>
          <div class="mt-3 d-flex gap-2 flex-wrap">
            <button class="btn btn-outline-secondary btn-sm" onclick={verifyNow} disabled={busy} data-testid="subscription-verify">
              {#if busy}<span class="spinner-border spinner-border-sm me-1"></span>{/if}
              {t('settings.subscription.verifyNow')}
            </button>
            <button class="btn btn-outline-danger btn-sm" onclick={removeSubscription} disabled={busy}>
              {t('settings.subscription.remove')}
            </button>
          </div>
        </div>
      </div>

    {/if}

    <!-- apt-Kanal des LCM-Hosts - unabhängig vom Vertrag sichtbar: Community
         und Beta sind frei, nur Enterprise hängt an der Subscription. -->
    <div class="card mb-4" data-testid="subscription-apt-card">
      <div class="card-body">
        <h3 class="h6">{t('settings.subscription.apt.title')}</h3>
        <p class="text-body-secondary small">{t('settings.subscription.apt.intro')}</p>
        {#if !status.apt_available}
          <p class="small mb-0 text-body-secondary">{t('settings.subscription.apt.unavailable')}</p>
        {:else}
          <fieldset class="mb-3">
            {#each APT_CHANNELS as channel (channel)}
              {@const locked = channel === 'enterprise' && !enterpriseAllowed}
              <div class="form-check mb-2">
                <input
                  class="form-check-input"
                  type="radio"
                  name="apt-channel"
                  id="apt-channel-{channel}"
                  value={channel}
                  bind:group={pickedChannel}
                  disabled={busy || locked}
                  data-testid="subscription-apt-{channel}"
                />
                <label class="form-check-label" for="apt-channel-{channel}">
                  <span class="fw-semibold">{t(`settings.subscription.apt.channel.${channel}`)}</span>
                  {#if channel === activeChannel}
                    <span class="badge text-bg-success ms-1">{t('settings.subscription.apt.activeBadge')}</span>
                  {/if}
                  <span class="d-block text-body-secondary small">
                    {t(`settings.subscription.apt.channel.${channel}Hint`)}
                  </span>
                </label>
              </div>
            {/each}
          </fieldset>
          <button
            class="btn btn-sm {pickedChannel === activeChannel ? 'btn-outline-primary' : 'btn-primary'}"
            onclick={() => switchChannel(pickedChannel)}
            disabled={busy || (pickedChannel === 'enterprise' && !enterpriseAllowed)}
            data-testid="subscription-apt-apply"
          >
            {#if busy}<span class="spinner-border spinner-border-sm me-1"></span>{/if}
            {pickedChannel === activeChannel
              ? t('settings.subscription.apt.reapply')
              : t('settings.subscription.apt.apply', {
                  channel: t(`settings.subscription.apt.channel.${pickedChannel}`),
                })}
          </button>
          {#if pickedChannel === activeChannel}
            <div class="form-text">{t('settings.subscription.apt.reapplyHint')}</div>
          {/if}
          {#if !enterpriseAllowed}
            <div class="form-text">
              {status.configured
                ? t('settings.subscription.apt.needsActive')
                : t('settings.subscription.apt.needsSubscription')}
            </div>
          {/if}
        {/if}
      </div>
    </div>

    <!-- Instanz-Identität (immer sichtbar - sie existiert unabhängig vom Vertrag) -->
    <div class="card" data-testid="subscription-instance-card">
      <div class="card-body">
        <h3 class="h6">{t('settings.subscription.instanceTitle')}</h3>
        <p class="text-body-secondary small mb-2">{t('settings.subscription.instanceHint')}</p>
        <code class="user-select-all">{status.instance_id}</code>
      </div>
    </div>
  {/if}
</SettingsLayout>
