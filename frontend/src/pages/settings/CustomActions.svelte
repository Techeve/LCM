<script>
  // Benutzerdefinierte Aktionen: wiederverwendbare Command-Listen, die sich
  // in Servergruppen als Rule-Typ „Custom-Aktion" auswählen lassen. Anlegen
  // und Bearbeiten öffnen als Popup.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import Modal from '../../components/Modal.svelte';

  const t = (k, p) => i18n.t(k, p);

  let actions = $state([]);
  let error = $state('');
  let notice = $state('');

  let open = $state(false);
  let form = $state({ id: null, name: '', description: '', commands: '' });

  function commandCount(text) {
    return (text || '')
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l && !l.startsWith('#')).length;
  }

  async function load() {
    error = '';
    try {
      actions = await api.customActions.list();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function openNew() {
    form = { id: null, name: '', description: '', commands: '' };
    open = true;
  }
  function openEdit(a) {
    form = { id: a.id, name: a.name, description: a.description ?? '', commands: a.commands ?? '' };
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

  async function save() {
    const f = { ...form };
    const data = { name: f.name, description: f.description, commands: f.commands };
    if (f.id) await run(() => api.customActions.update(f.id, data), t('settings.customActions.notices.saved'));
    else await run(() => api.customActions.create(data), t('settings.customActions.notices.created'));
    if (!error) open = false;
  }

  async function remove(a) {
    if (!confirm(t('settings.customActions.confirmDelete', { name: a.name }))) return;
    await run(() => api.customActions.remove(a.id), t('settings.customActions.notices.deleted'));
  }

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.customActions.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  <div class="d-flex justify-content-between align-items-center mb-3">
    <p class="small text-body-secondary mb-0">
      {t('settings.customActions.intro')}
    </p>
    <button class="btn btn-sm btn-primary text-nowrap ms-3" onclick={openNew}>{t('settings.customActions.addAction')}</button>
  </div>

  <div class="table-responsive">
    <table class="table table-sm align-middle">
      <thead><tr><th>{t('common.name')}</th><th>{t('common.description')}</th><th>{t('settings.customActions.colCommands')}</th><th class="text-end">{t('settings.customActions.colManage')}</th></tr></thead>
      <tbody>
        {#each actions as a (a.id)}
          <tr>
            <td>{a.name}</td>
            <td class="small text-body-secondary">{a.description || '-'}</td>
            <td class="small">{commandCount(a.commands)}</td>
            <td class="text-end text-nowrap">
              <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEdit(a)}>{t('common.edit')}</button>
              <button class="btn btn-sm btn-outline-danger py-0" onclick={() => remove(a)}>{t('common.delete')}</button>
            </td>
          </tr>
        {:else}
          <tr><td colspan="4" class="text-body-secondary small">{t('settings.customActions.noActions')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
</SettingsLayout>

<Modal title={form.id ? t('settings.customActions.editTitle') : t('settings.customActions.newTitle')} bind:open size="modal-lg">
  <label class="form-label small mb-1" for="ca-name">{t('common.name')}</label>
  <input id="ca-name" class="form-control mb-2" placeholder={t('settings.customActions.namePlaceholder')} bind:value={form.name} />
  <label class="form-label small mb-1" for="ca-desc">{t('common.description')}</label>
  <input id="ca-desc" class="form-control mb-2" placeholder={t('settings.customActions.descPlaceholder')} bind:value={form.description} />
  <label class="form-label small mb-1" for="ca-cmds">{t('settings.customActions.commandsLabel')}</label>
  <textarea id="ca-cmds" class="form-control font-monospace" rows="7" spellcheck="false"
    placeholder={t('settings.customActions.commandsPlaceholder')}
    bind:value={form.commands}></textarea>
  <p class="small text-body-secondary mt-1">
    {t('settings.customActions.commandsHintA', { count: commandCount(form.commands) })}<code>sudo</code>{t('settings.customActions.commandsHintB')}
  </p>
  <div class="text-end mt-2">
    <button class="btn btn-secondary" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" onclick={save} disabled={!form.name.trim() || commandCount(form.commands) === 0}>
      {form.id ? t('common.save') : t('common.create')}
    </button>
  </div>
</Modal>
