<script>
  // Reconnect-Wizard: stellt die Verbindung zu einem BESTEHENDEN Server neu
  // her (geänderte Credentials oder ausgetauschter Server). Ablauf wie beim
  // Onboarding: Verbindungsdaten → Fingerprint bestätigen → neu verbinden.
  // Überschreibt danach die Credentials des Servers (ID bleibt).
  import Modal from './Modal.svelte';
  import { api, ApiError } from '../api';
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);

  let { server, open = $bindable(false), onDone = () => {} } = $props();

  let step = $state(1);
  let busy = $state(false);
  let error = $state('');
  let form = $state({ host: '', port: 22, login_user: 'root', login_password: '', auth_method: 'password', restricted_access: false });
  let fingerprint = $state('');
  let keyType = $state('');

  // Beim Öffnen die aktuellen Verbindungsdaten des Servers vorbelegen; der
  // Rechte-Modus wird mit dem aktuellen Stand des Servers vorbelegt.
  $effect(() => {
    if (open && server) {
      form = { host: server.host, port: server.ssh_port, login_user: 'root', login_password: '', auth_method: 'password', restricted_access: !!server.restricted_sudo };
      step = 1;
      error = '';
      fingerprint = '';
    }
  });

  async function probe() {
    busy = true;
    error = '';
    try {
      const res = await api.servers.probe(form.host, Number(form.port));
      fingerprint = res.fingerprint;
      keyType = res.key_type;
      step = 2;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function reconnect() {
    busy = true;
    error = '';
    try {
      await api.servers.reconnect(server.id, {
        host: form.host,
        port: Number(form.port),
        login_user: form.login_user,
        login_password: form.auth_method === 'key' ? '' : form.login_password,
        auth_method: form.auth_method,
        confirmed_fingerprint: fingerprint,
        restricted_access: form.restricted_access,
      });
      open = false;
      onDone();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<Modal title={t('reconnect.title')} bind:open>
  <p class="small text-body-secondary">
    {t('reconnect.introA')}<strong>{server?.name}</strong>{t('reconnect.introB')}
  </p>

  {#if error}<div class="alert alert-danger py-2">{error}</div>{/if}

  {#if step === 1}
    <div class="row">
      <div class="col-8 mb-2">
        <label class="form-label" for="rc-host">{t('reconnect.hostIp')}</label>
        <input id="rc-host" class="form-control" bind:value={form.host} />
      </div>
      <div class="col-4 mb-2">
        <label class="form-label" for="rc-port">{t('reconnect.sshPort')}</label>
        <input id="rc-port" type="number" class="form-control" bind:value={form.port} />
      </div>
    </div>
    <div class="mb-2">
      <label class="form-label" for="rc-user">{t('reconnect.loginUser')}</label>
      <input id="rc-user" class="form-control" bind:value={form.login_user} autocomplete="off" />
    </div>
    <div class="mb-2">
      <span class="form-label d-block">{t('reconnect.auth')}</span>
      <div class="btn-group btn-group-sm" role="group">
        <input type="radio" class="btn-check" id="rc-auth-pw" value="password" bind:group={form.auth_method} />
        <label class="btn btn-outline-primary" for="rc-auth-pw">{t('reconnect.password')}</label>
        <input type="radio" class="btn-check" id="rc-auth-key" value="key" bind:group={form.auth_method} />
        <label class="btn btn-outline-primary" for="rc-auth-key">{t('reconnect.systemKey')}</label>
      </div>
    </div>
    {#if form.auth_method === 'password'}
      <div class="mb-3">
        <label class="form-label" for="rc-pw">{t('reconnect.password')}</label>
        <input id="rc-pw" type="password" class="form-control" bind:value={form.login_password} autocomplete="off" />
        <div class="form-text">{t('joinWizard.passwordHint')}</div>
      </div>
    {:else}
      <div class="alert alert-info py-2 small mb-3">
        {t('reconnect.keyHintA')}<code>authorized_keys</code>{t('reconnect.keyHintB')}
      </div>
    {/if}
    <div class="form-check mb-3">
      <input class="form-check-input" type="checkbox" id="rc-restricted" bind:checked={form.restricted_access} />
      <label class="form-check-label" for="rc-restricted">{t('joinWizard.restrictedLabel')}</label>
    </div>
    <button class="btn btn-primary" onclick={probe} disabled={busy || !form.host}>
      {busy ? t('reconnect.probing') : t('reconnect.probeNext')}
    </button>
  {:else if step === 2}
    <p class="mb-1">{t('reconnect.confirmFp')}</p>
    <pre class="bg-body-secondary p-2 rounded small"><code>{keyType}
{fingerprint}</code></pre>
    <p class="small text-body-secondary">
      {t('reconnect.fpHint')}
    </p>
    <div class="d-flex gap-2">
      <button class="btn btn-outline-secondary" onclick={() => (step = 1)} disabled={busy}>{t('common.back')}</button>
      <button class="btn btn-success" onclick={reconnect} disabled={busy}>
        {busy ? t('reconnect.reconnecting') : t('reconnect.reconnectConfirm')}
      </button>
    </div>
  {/if}
</Modal>
