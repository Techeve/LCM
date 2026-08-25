<script>
  // IP-Allowlists: benannte, wiederverwendbare Listen von Quell-IPs/-Netzen
  // (IPv4/IPv6, inkl. CIDR). Ein gemeinsamer Pool - auswählbar in
  // Firewall-Regeln (Quell-Einschränkung) sowie bei fail2ban/CrowdSec.
  // Anlegen/Bearbeiten als Popup.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import Modal from '../../components/Modal.svelte';

  const t = (k, p) => i18n.t(k, p);

  let lists = $state([]);
  let error = $state('');
  let notice = $state('');

  let open = $state(false);
  let form = $state({ id: null, name: '', description: '', entries: '' });

  async function load() {
    error = '';
    try {
      lists = await api.system.ipAllowlists();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function openNew() {
    form = { id: null, name: '', description: '', entries: '' };
    open = true;
  }
  function openEdit(l) {
    form = { id: l.id, name: l.name, description: l.description ?? '', entries: l.entries ?? '' };
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
    const data = { name: f.name, description: f.description, entries: f.entries };
    if (f.id) data.id = f.id;
    await run(() => api.system.saveIPAllowlist(data), f.id ? t('allowlists.notices.saved') : t('allowlists.notices.created'));
    if (!error) open = false;
  }

  async function remove(l) {
    if (!confirm(t('allowlists.confirmDelete', { name: l.name }))) return;
    await run(() => api.system.deleteIPAllowlist(l.id), t('allowlists.notices.deleted'));
  }

  // Anzahl der (nicht-leeren) Einträge einer Liste - für die Tabelle.
  function entryCount(entries) {
    return (entries || '').split(/[\n,\s]+/).filter(Boolean).length;
  }

  // Client-Validierung: Name gesetzt, jeder Eintrag sieht wie IP oder CIDR aus.
  const reEntry = /^([0-9.]+|[0-9a-fA-F:.]+)(\/\d{1,3})?$/;
  const entriesOk = $derived(
    (form.entries || '')
      .split(/[\n,\s]+/)
      .filter(Boolean)
      .every((e) => reEntry.test(e.trim())),
  );
  const formValid = $derived(form.name.trim() !== '' && entriesOk);

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('allowlists.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  <div class="card mb-3">
    <div class="card-body">
      <div class="d-flex justify-content-between align-items-center mb-2">
        <h3 class="h6 mb-0">{t('allowlists.listTitle')}</h3>
        <button class="btn btn-sm btn-primary text-nowrap ms-3" data-testid="allowlist-new" onclick={openNew}>{t('allowlists.add')}</button>
      </div>
      <p class="small text-body-secondary">{t('allowlists.intro')}</p>
      <div class="table-responsive">
        <table class="table table-sm align-middle">
          <thead><tr><th>{t('common.name')}</th><th>{t('allowlists.colEntries')}</th><th>{t('allowlists.colPreview')}</th><th class="text-end">{t('allowlists.colManage')}</th></tr></thead>
          <tbody>
            {#each lists as l (l.id)}
              <tr data-testid="allowlist-row">
                <td>
                  {l.name}
                  {#if l.description}<div class="small text-body-secondary">{l.description}</div>{/if}
                </td>
                <td><span class="badge border text-body-secondary">{entryCount(l.entries)}</span></td>
                <td class="small font-monospace text-break" style="max-width: 24rem">{(l.entries || '').split('\n').slice(0, 3).join(', ')}{entryCount(l.entries) > 3 ? ' …' : ''}</td>
                <td class="text-end text-nowrap">
                  <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEdit(l)}>{t('common.edit')}</button>
                  <button class="btn btn-sm btn-outline-danger py-0" onclick={() => remove(l)}>{t('common.delete')}</button>
                </td>
              </tr>
            {:else}
              <tr><td colspan="4" class="text-body-secondary small">{t('allowlists.noEntries')}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  </div>
</SettingsLayout>

<Modal title={form.id ? t('allowlists.editTitle') : t('allowlists.newTitle')} bind:open size="modal-lg">
  <label class="form-label small mb-1" for="al-name">{t('common.name')}</label>
  <input id="al-name" class="form-control" placeholder={t('allowlists.namePlaceholder')} bind:value={form.name} data-testid="allowlist-name" />
  <label class="form-label small mb-1 mt-2" for="al-desc">{t('common.description')}</label>
  <input id="al-desc" class="form-control" placeholder={t('allowlists.descPlaceholder')} bind:value={form.description} />
  <label class="form-label small mb-1 mt-2" for="al-entries">{t('allowlists.entriesLabel')}</label>
  <textarea id="al-entries" class="form-control font-monospace" rows="6" spellcheck="false"
    placeholder={'10.0.0.5\n192.168.1.0/24\n2001:db8::/32'}
    bind:value={form.entries} data-testid="allowlist-entries"></textarea>
  <p class="small text-body-secondary mt-1">{t('allowlists.entriesHint')}</p>
  <div class="text-end mt-2">
    <button class="btn btn-secondary" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" data-testid="allowlist-save" onclick={save} disabled={!formValid}>
      {form.id ? t('common.save') : t('common.create')}
    </button>
  </div>
</Modal>
