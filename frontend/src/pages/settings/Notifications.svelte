<script>
  // Benachrichtigungskanäle: konfigurierte Instanzen des generischen
  // Notification-Service (MVP: E-Mail/SMTP). Anlegen und Bearbeiten öffnen
  // als Popup; ein Test-Versand prüft die Konfiguration.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import Modal from '../../components/Modal.svelte';

  const t = (k, p) => i18n.t(k, p);

  let channels = $state([]);
  let error = $state('');
  let notice = $state('');

  let open = $state(false);
  // form.config bündelt die provider-spezifischen Felder (E-Mail bzw.
  // Webhook); recipients wird als mehrzeiliger Text erfasst und beim
  // Speichern in ein Array übersetzt. secret ist der sensible Teil
  // (SMTP-Passwort bzw. Webhook-URL) und bleibt leer = unverändert.
  let form = $state(emptyForm());

  function emptyForm() {
    return {
      id: null,
      name: '',
      type: 'email',
      enabled: true,
      secret: '',
      config: {
        host: '', port: 587, username: '', from: '', recipients: '', use_tls: true,
        format: 'teams',
      },
    };
  }

  // Der System-E-Mail-Kanal wird über Einstellungen → Allgemein verwaltet.
  const isSystem = (c) => c.type === 'system_email';

  function parseConfig(raw) {
    try {
      return raw ? JSON.parse(raw) : {};
    } catch {
      return {};
    }
  }

  async function load() {
    error = '';
    try {
      channels = await api.notifications.list();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function openNew() {
    form = emptyForm();
    open = true;
  }

  function openEdit(c) {
    const cfg = parseConfig(c.config);
    form = {
      id: c.id,
      name: c.name,
      type: c.type || 'email',
      enabled: !!c.enabled,
      secret: '', // Secret wird nie zurückgeliefert; leer = unverändert
      config: {
        host: cfg.host ?? '',
        port: cfg.port ?? 587,
        username: cfg.username ?? '',
        from: cfg.from ?? '',
        recipients: (cfg.recipients ?? []).join('\n'),
        use_tls: cfg.use_tls ?? true,
        format: cfg.format ?? 'teams',
      },
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

  function recipientList(text) {
    return (text || '')
      .split(/[\n,;]+/)
      .map((r) => r.trim())
      .filter(Boolean);
  }

  function buildPayload() {
    const f = form;
    const data = { name: f.name.trim(), type: f.type, enabled: f.enabled };
    if (f.type === 'webhook') {
      data.config = { format: f.config.format };
    } else {
      data.config = {
        host: f.config.host.trim(),
        port: Number(f.config.port) || 0,
        username: f.config.username.trim(),
        from: f.config.from.trim(),
        recipients: recipientList(f.config.recipients),
        use_tls: !!f.config.use_tls,
      };
    }
    // Secret (SMTP-Passwort bzw. Webhook-URL) nur mitschicken, wenn
    // gesetzt (leer = unverändert).
    if (f.secret) data.secret = f.secret.trim();
    return data;
  }

  async function save() {
    const f = form;
    const data = buildPayload();
    if (f.id) await run(() => api.notifications.update(f.id, data), t('settings.notifications.saved'));
    else await run(() => api.notifications.create(data), t('settings.notifications.created'));
    if (!error) open = false;
  }

  async function remove(c) {
    if (!confirm(t('settings.notifications.confirmDelete', { name: c.name }))) return;
    await run(() => api.notifications.remove(c.id), t('settings.notifications.deleted'));
  }

  async function test(c) {
    await run(() => api.notifications.test(c.id), t('settings.notifications.testSent', { name: c.name }));
  }

  function summary(c) {
    const cfg = parseConfig(c.config);
    switch (c.type || 'email') {
      case 'email': {
        const rcpt = (cfg.recipients ?? []).length;
        return t('settings.notifications.emailSummary', {
          host: cfg.host || '-',
          port: cfg.port || '-',
          count: rcpt,
        });
      }
      case 'webhook':
        return cfg.format === 'teams'
          ? t('settings.notifications.fmtTeams')
          : t('settings.notifications.fmtGeneric');
      case 'system_email':
        return t('settings.notifications.systemSummary');
      default:
        return '-';
    }
  }

  function typeLabel(ty) {
    return {
      email: t('settings.notifications.lblEmail'),
      webhook: t('settings.notifications.lblWebhook'),
      system_email: t('settings.notifications.lblSystemEmail'),
    }[ty] ?? ty;
  }

  const valid = $derived(
    form.type === 'webhook'
      ? !!(form.name.trim() && (form.secret.trim() || form.id))
      : !!(
          form.name.trim() &&
          form.config.host.trim() &&
          Number(form.config.port) > 0 &&
          form.config.from.trim() &&
          recipientList(form.config.recipients).length > 0
        ),
  );

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.notifications.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  <div class="d-flex justify-content-between align-items-center mb-3">
    <p class="small text-body-secondary mb-0">
      {t('settings.notifications.introA')}<em>{t('settings.notifications.introEm')}</em>{t('settings.notifications.introB')}
    </p>
    <button class="btn btn-sm btn-primary text-nowrap ms-3" onclick={openNew}>{t('settings.notifications.addChannel')}</button>
  </div>

  <div class="table-responsive">
    <table class="table table-sm align-middle">
      <thead>
        <tr><th>{t('common.name')}</th><th>{t('settings.notifications.colType')}</th><th>{t('settings.notifications.colConfig')}</th><th>{t('common.status')}</th><th class="text-end">{t('settings.notifications.colManage')}</th></tr>
      </thead>
      <tbody>
        {#each channels as c (c.id)}
          <tr>
            <td>{c.name}</td>
            <td class="small">{typeLabel(c.type)}</td>
            <td class="small text-body-secondary">{summary(c)}</td>
            <td>
              {#if c.enabled}
                <span class="badge text-bg-success">{t('common.active')}</span>
              {:else}
                <span class="badge text-bg-secondary">{t('settings.notifications.inactive')}</span>
              {/if}
            </td>
            <td class="text-end text-nowrap">
              <button class="btn btn-sm btn-outline-primary py-0" onclick={() => test(c)}>{t('settings.notifications.testBtn')}</button>
              {#if !isSystem(c)}
                <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEdit(c)}>{t('common.edit')}</button>
                <button class="btn btn-sm btn-outline-danger py-0" onclick={() => remove(c)}>{t('common.delete')}</button>
              {/if}
            </td>
          </tr>
        {:else}
          <tr><td colspan="5" class="text-body-secondary small">{t('settings.notifications.noChannels')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
</SettingsLayout>

<Modal title={form.id ? t('settings.notifications.editTitle') : t('settings.notifications.newTitle')} bind:open size="modal-lg">
  <div class="row g-2">
    <div class="col-md-8">
      <label class="form-label small mb-1" for="ch-name">{t('common.name')}</label>
      <input id="ch-name" class="form-control" placeholder={t('settings.notifications.namePlaceholder')} bind:value={form.name} />
    </div>
    <div class="col-md-4">
      <label class="form-label small mb-1" for="ch-type">{t('settings.notifications.colType')}</label>
      <select id="ch-type" class="form-select" bind:value={form.type} disabled={!!form.id}>
        <option value="email">{t('settings.notifications.typeEmail')}</option>
        <option value="webhook">{t('settings.notifications.typeWebhook')}</option>
      </select>
    </div>
  </div>

  <hr class="my-3" />

  {#if form.type === 'webhook'}
    <label class="form-label small mb-1" for="ch-format">{t('settings.notifications.formatLabel')}</label>
    <select id="ch-format" class="form-select" bind:value={form.config.format}>
      <option value="teams">{t('settings.notifications.fmtTeams')}</option>
      <option value="generic">{t('settings.notifications.fmtGeneric')}</option>
    </select>
    <div class="form-text">
      {t('settings.notifications.webhookHint')}
    </div>

    <label class="form-label small mb-1 mt-2" for="ch-url">
      {t('settings.notifications.webhookUrlLabel')} {#if form.id}<span class="text-body-secondary">{t('settings.notifications.unchanged')}</span>{/if}
    </label>
    <input id="ch-url" type="password" class="form-control" autocomplete="off" spellcheck="false"
      placeholder="https://…" bind:value={form.secret} />
    <div class="form-text">
      {t('settings.notifications.webhookUrlHint')}
    </div>
  {:else}
    <div class="row g-2">
      <div class="col-md-8">
        <label class="form-label small mb-1" for="ch-host">{t('settings.notifications.smtpHost')}</label>
        <input id="ch-host" class="form-control" placeholder="smtp.example.com" bind:value={form.config.host} />
      </div>
      <div class="col-md-4">
        <label class="form-label small mb-1" for="ch-port">{t('settings.notifications.portLabel')}</label>
        <input id="ch-port" type="number" class="form-control" bind:value={form.config.port} />
      </div>
    </div>

    <div class="row g-2 mt-0">
      <div class="col-md-6">
        <label class="form-label small mb-1" for="ch-user">{t('settings.notifications.usernameLabel')}</label>
        <input id="ch-user" class="form-control" autocomplete="off" bind:value={form.config.username} />
      </div>
      <div class="col-md-6">
        <label class="form-label small mb-1" for="ch-pass">{t('common.password')} {#if form.id}<span class="text-body-secondary">{t('settings.notifications.unchanged')}</span>{/if}</label>
        <input id="ch-pass" type="password" class="form-control" autocomplete="new-password" bind:value={form.secret} />
      </div>
    </div>

    <label class="form-label small mb-1 mt-2" for="ch-from">{t('settings.notifications.fromLabel')}</label>
    <input id="ch-from" class="form-control" placeholder="lcm@example.com" bind:value={form.config.from} />

    <label class="form-label small mb-1 mt-2" for="ch-rcpt">{t('settings.notifications.recipientsLabel')}</label>
    <textarea id="ch-rcpt" class="form-control" rows="3" spellcheck="false"
      placeholder={'ops@example.com\nadmin@example.com'} bind:value={form.config.recipients}></textarea>

    <div class="form-check mt-2">
      <input id="ch-tls" class="form-check-input" type="checkbox" bind:checked={form.config.use_tls} />
      <label class="form-check-label small" for="ch-tls">{t('settings.notifications.tlsLabel')}</label>
    </div>
  {/if}

  <div class="form-check mt-1">
    <input id="ch-enabled" class="form-check-input" type="checkbox" bind:checked={form.enabled} />
    <label class="form-check-label small" for="ch-enabled">{t('settings.notifications.channelEnabled')}</label>
  </div>

  <div class="text-end mt-3">
    <button class="btn btn-secondary" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" onclick={save} disabled={!valid}>
      {form.id ? t('common.save') : t('common.create')}
    </button>
  </div>
</Modal>
