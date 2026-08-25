<script>
  // Katalog bekannter Paketquellen: die Vorlagen, die im Server-Detail unter
  // „Repository hinzufügen" angeboten werden. Mitgelieferte Einträge lassen
  // sich anpassen, eigene ergänzen. Anlegen/Bearbeiten als Popup.
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import Modal from '../../components/Modal.svelte';

  const t = (k, p) => i18n.t(k, p);

  let repos = $state([]);
  let error = $state('');
  let notice = $state('');

  let open = $state(false);
  let form = $state({ id: null, key: '', name: '', description: '', key_url: '', line: '', package_manager: 'apt' });

  // Paketverwaltungen, für die eine Katalog-Quelle gelten kann. apt erwartet
  // eine vollständige "deb …"-Zeile, alle anderen eine Repository-/‌.repo-URL.
  const PKG_MGRS = [
    { value: 'apt', label: 'APT (Debian/Ubuntu)' },
    { value: 'dnf', label: 'DNF/YUM (RHEL, Fedora, Rocky, Alma)' },
    { value: 'zypper', label: 'Zypper (openSUSE/SLES)' },
    { value: 'pacman', label: 'pacman (Arch)' },
    { value: 'apk', label: 'apk (Alpine)' },
  ];

  // Der APT-Cache (apt-cacher-ng) hat eine eigene Seite (Einstellungen →
  // APT-Cache): URL, Erreichbarkeit, Statistik, Neustart, permanentes Caching
  // und Überwachung an einem Ort. Hier bleibt nur der Verweis darauf.
  async function load() {
    error = '';
    try {
      repos = await api.system.knownRepos();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function openNew() {
    form = { id: null, key: '', name: '', description: '', key_url: '', line: '', package_manager: 'apt' };
    open = true;
  }
  function openEdit(r) {
    form = { id: r.id, key: r.key, name: r.name, description: r.description ?? '', key_url: r.key_url ?? '', line: r.line, package_manager: r.package_manager || 'apt' };
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
    const data = { key: f.key, name: f.name, description: f.description, key_url: f.key_url, line: f.line, package_manager: f.package_manager };
    if (f.id) data.id = f.id;
    await run(() => api.system.saveKnownRepo(data), f.id ? t('settings.repositories.notices.saved') : t('settings.repositories.notices.created'));
    if (!error) open = false;
  }

  async function remove(r) {
    if (!confirm(t('settings.repositories.confirmDelete', { name: r.name }))) return;
    await run(() => api.system.deleteKnownRepo(r.id), t('settings.repositories.notices.deleted'));
  }

  // apt verlangt eine "deb …"-Zeile, alle anderen eine http(s)-URL.
  const lineOk = $derived(
    form.package_manager === 'apt'
      ? form.line.trim().startsWith('deb ')
      : /^https?:\/\//.test(form.line.trim())
  );
  const formValid = $derived(form.key.trim() !== '' && form.name.trim() !== '' && lineOk);

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.repositories.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  <div class="card mb-3">
    <div class="card-body">
      <div class="d-flex justify-content-between align-items-center mb-2">
        <h3 class="h6 mb-0">{t('settings.repositories.catalogTitle')}</h3>
        <button class="btn btn-sm btn-primary text-nowrap ms-3" onclick={openNew}>{t('settings.repositories.addSource')}</button>
      </div>
      <p class="small text-body-secondary">
        {t('settings.repositories.catalogIntroA')}<code>/etc/apt/keyrings/&lt;key&gt;.asc</code>{t('settings.repositories.catalogIntroB')}<code>lcm-&lt;key&gt;.list</code>{t('settings.repositories.catalogIntroC')}<code>$ID</code>{t('settings.repositories.catalogIntroD')}<code>$CODENAME</code>{t('settings.repositories.catalogIntroE')}<code>$ARCH</code>{t('settings.repositories.catalogIntroF')}
      </p>
      <div class="table-responsive">
        <table class="table table-sm align-middle">
          <thead><tr><th>{t('common.name')}</th><th>{t('settings.repositories.colPkgMgr')}</th><th>{t('settings.repositories.colKey')}</th><th>{t('settings.repositories.colSource')}</th><th class="text-end">{t('settings.repositories.colManage')}</th></tr></thead>
          <tbody>
            {#each repos as r (r.id)}
              <tr>
                <td>
                  {r.name}
                  {#if r.description}<div class="small text-body-secondary">{r.description}</div>{/if}
                </td>
                <td><span class="badge border text-body-secondary">{r.package_manager || 'apt'}</span></td>
                <td><code>{r.key}</code></td>
                <td class="small font-monospace text-break" style="max-width: 24rem">{r.line}</td>
                <td class="text-end text-nowrap">
                  <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEdit(r)}>{t('common.edit')}</button>
                  <button class="btn btn-sm btn-outline-danger py-0" onclick={() => remove(r)}>{t('common.delete')}</button>
                </td>
              </tr>
            {:else}
              <tr><td colspan="5" class="text-body-secondary small">{t('settings.repositories.noEntries')}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  </div>

  <div class="card mb-3">
    <div class="card-body d-flex flex-wrap justify-content-between align-items-center gap-2">
      <div>
        <h3 class="h6 mb-1">{t('settings.repositories.cacheTitle')}</h3>
        <p class="small text-body-secondary mb-0">{t('settings.repositories.cachePointer')}</p>
      </div>
      <a class="btn btn-outline-primary text-nowrap" href="/settings/apt-cache" use:link data-testid="apt-cache-page-link">
        {t('settings.repositories.cacheOpenPage')}
      </a>
    </div>
  </div>
</SettingsLayout>

<Modal title={form.id ? t('settings.repositories.editTitle') : t('settings.repositories.newTitle')} bind:open size="modal-lg">
  <div class="row g-2">
    <div class="col-md-6">
      <label class="form-label small mb-1" for="kr-name">{t('common.name')}</label>
      <input id="kr-name" class="form-control" placeholder={t('settings.repositories.namePlaceholder')} bind:value={form.name} />
    </div>
    <div class="col-md-6">
      <label class="form-label small mb-1" for="kr-key">{t('settings.repositories.keyLabel')}</label>
      <input id="kr-key" class="form-control font-monospace" placeholder={t('settings.repositories.keyPlaceholder')} bind:value={form.key} />
    </div>
  </div>
  <label class="form-label small mb-1 mt-2" for="kr-pkgmgr">{t('settings.repositories.pkgMgrLabel')}</label>
  <select id="kr-pkgmgr" class="form-select" bind:value={form.package_manager}>
    {#each PKG_MGRS as m (m.value)}<option value={m.value}>{m.label}</option>{/each}
  </select>
  <p class="small text-body-secondary mt-1 mb-0">{t('settings.repositories.pkgMgrHint')}</p>
  <label class="form-label small mb-1 mt-2" for="kr-desc">{t('common.description')}</label>
  <input id="kr-desc" class="form-control" placeholder={t('settings.repositories.descPlaceholder')} bind:value={form.description} />
  <label class="form-label small mb-1 mt-2" for="kr-keyurl">{t('settings.repositories.keyUrlLabel')}</label>
  <input id="kr-keyurl" class="form-control font-monospace" placeholder="https://…/gpg.key" bind:value={form.key_url} />
  {#if form.package_manager === 'apt'}
    <label class="form-label small mb-1 mt-2" for="kr-line">{t('settings.repositories.lineLabelA')}<code>deb </code>{t('settings.repositories.lineLabelB')}</label>
    <textarea id="kr-line" class="form-control font-monospace" rows="2" spellcheck="false"
      placeholder={'deb [arch=$ARCH signed-by=/etc/apt/keyrings/<key>.asc] https://… $CODENAME main'}
      bind:value={form.line}></textarea>
    <p class="small text-body-secondary mt-1">
      {t('settings.repositories.lineHintA')}<code>$ID</code>{t('settings.repositories.lineHintB')}<code>$CODENAME</code>{t('settings.repositories.lineHintC')}<code>$ARCH</code>{t('settings.repositories.lineHintD')}<code>signed-by</code>{t('settings.repositories.lineHintE')}
    </p>
  {:else}
    <label class="form-label small mb-1 mt-2" for="kr-line">{t('settings.repositories.urlLabel')}</label>
    <input id="kr-line" class="form-control font-monospace" spellcheck="false"
      placeholder={form.package_manager === 'dnf' ? 'https://…/name.repo' : 'https://…'}
      bind:value={form.line} />
    <p class="small text-body-secondary mt-1">{t(`settings.repositories.urlHint.${form.package_manager}`)}</p>
  {/if}
  <div class="text-end mt-2">
    <button class="btn btn-secondary" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" onclick={save} disabled={!formValid}>
      {form.id ? t('common.save') : t('common.create')}
    </button>
  </div>
</Modal>
