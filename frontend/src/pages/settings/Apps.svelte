<script>
  // Anwendungskatalog: die Steckbriefe der Software, die nicht über die
  // Paketverwaltung installiert wird. Je Eintrag steht darin, woran LCM die
  // Anwendung erkennt, wie es ihre Version erfährt und wo die neueste steht.
  //
  // Die Felder tragen Kommandos, die beim Scan auf jedem passenden Server
  // ausgeführt werden - die Seite ist deshalb den Administratoren vorbehalten
  // (settings:manage), genau wie die Eigenen Aktionen.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import Modal from '../../components/Modal.svelte';
  import Pagination from '../../components/Pagination.svelte';
  import { PAGE_SIZE_CATALOG, pageCount, pageSlice } from '../../lib/paging.js';

  const t = (k, p) => i18n.t(k, p);

  let page = $state(1);
  let entries = $state([]);
  let actions = $state([]);
  let error = $state('');
  let notice = $state('');

  let open = $state(false);
  const leer = {
    id: null, slug: '', name: '', description: '', name_en: '', description_en: '', enabled: true, markers: '',
    version_command: '', version_pattern: '', compare: 'semver',
    latest_source: '', latest_pattern: '', backup_action_id: '', update_action_id: '',
  };
  let form = $state({ ...leer });
  let builtin = $state(false);

  async function load() {
    error = '';
    try {
      entries = await api.apps.list();
      actions = await api.customActions.list();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function openNew() {
    form = { ...leer };
    builtin = false;
    open = true;
  }

  function openEdit(e) {
    form = {
      id: e.id, slug: e.slug, name: e.name, description: e.description ?? '',
      name_en: e.name_en ?? '', description_en: e.description_en ?? '',
      enabled: e.enabled, markers: e.markers ?? '',
      version_command: e.version_command ?? '', version_pattern: e.version_pattern ?? '',
      compare: e.compare || 'semver',
      latest_source: e.latest_source ?? '', latest_pattern: e.latest_pattern ?? '',
      backup_action_id: e.backup_action_id ?? '', update_action_id: e.update_action_id ?? '',
    };
    builtin = e.builtin;
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

  function payload() {
    const zahl = (v) => (v === '' || v === null ? null : Number(v));
    return {
      slug: form.slug.trim(), name: form.name.trim(), description: form.description,
      name_en: form.name_en.trim(), description_en: form.description_en,
      enabled: form.enabled, markers: form.markers,
      version_command: form.version_command, version_pattern: form.version_pattern,
      compare: form.compare, latest_source: form.latest_source.trim(),
      latest_pattern: form.latest_pattern,
      backup_action_id: zahl(form.backup_action_id),
      update_action_id: zahl(form.update_action_id),
    };
  }

  async function save() {
    const data = payload();
    await run(
      () => (form.id ? api.apps.update(form.id, data) : api.apps.create(data)),
      form.id ? t('settings.apps.notices.saved') : t('settings.apps.notices.created')
    );
    if (!error) open = false;
  }

  async function toggle(e) {
    await run(
      () => api.apps.update(e.id, { ...e, enabled: !e.enabled }),
      e.enabled ? t('settings.apps.notices.disabled') : t('settings.apps.notices.enabled')
    );
  }

  async function remove(e) {
    if (!confirm(t('settings.apps.confirmDelete', { name: e.name }))) return;
    await run(() => api.apps.remove(e.id), t('settings.apps.notices.deleted'));
  }

  const formValid = $derived(form.slug.trim() !== '' && form.name.trim() !== '' && form.markers.trim() !== '');

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.apps.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  <div class="card mb-3">
    <div class="card-body">
      <div class="d-flex justify-content-between align-items-center mb-2">
        <h3 class="h6 mb-0">{t('settings.apps.catalogTitle')}</h3>
        <button class="btn btn-sm btn-primary text-nowrap ms-3" data-testid="app-add" onclick={openNew}>
          {t('settings.apps.add')}
        </button>
      </div>
      <p class="small text-body-secondary">{t('settings.apps.intro')}</p>

      <div class="table-responsive">
        <table class="table table-sm align-middle" data-testid="app-catalog">
          <thead>
            <tr>
              <th>{t('common.name')}</th>
              <th>{t('settings.apps.colMarkers')}</th>
              <th>{t('settings.apps.colLatest')}</th>
              <th class="text-end">{t('settings.apps.colManage')}</th>
            </tr>
          </thead>
          <tbody>
            {#each pageSlice(entries, page, PAGE_SIZE_CATALOG) as e (e.id)}
              <tr class={e.enabled ? '' : 'table-secondary'}>
                <td>
                  {i18n.field(e, 'name')}
                  {#if e.builtin}<span class="badge border text-body-secondary ms-1">{t('settings.apps.builtin')}</span>{/if}
                  {#if !e.enabled}<span class="badge text-bg-secondary ms-1">{t('settings.apps.disabled')}</span>{/if}
                  {#if i18n.field(e, 'description')}<div class="small text-body-secondary">{i18n.field(e, 'description')}</div>{/if}
                </td>
                <td class="small font-monospace text-break" style="max-width: 20rem">{e.markers}</td>
                <td class="small">
                  {#if e.latest_error}
                    <span class="text-danger" title={e.latest_error}>{t('settings.apps.latestError')}</span>
                  {:else if e.latest_version}
                    {e.latest_version}
                  {:else if e.latest_source}
                    <span class="text-body-secondary">{t('settings.apps.latestPending')}</span>
                  {:else}
                    <span class="text-body-secondary">{t('settings.apps.latestNoSource')}</span>
                  {/if}
                </td>
                <td class="text-end text-nowrap">
                  <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => toggle(e)}>
                    {e.enabled ? t('settings.apps.disable') : t('settings.apps.enable')}
                  </button>
                  <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEdit(e)}>{t('common.edit')}</button>
                  {#if !e.builtin}
                    <button class="btn btn-sm btn-outline-danger py-0" onclick={() => remove(e)}>{t('common.delete')}</button>
                  {/if}
                </td>
              </tr>
            {:else}
              <tr><td colspan="4" class="text-body-secondary small">{t('settings.apps.noEntries')}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
      <Pagination
        page={page}
        pageCount={pageCount(entries.length, PAGE_SIZE_CATALOG)}
        total={entries.length}
        pageSize={PAGE_SIZE_CATALOG}
        testid="apps-catalog-pagination"
        onchange={(p) => (page = p)}
      />
    </div>
  </div>
</SettingsLayout>

<Modal bind:open title={form.id ? t('settings.apps.editTitle') : t('settings.apps.newTitle')} size="modal-lg">
  {#if builtin}
    <div class="alert alert-warning small">{t('settings.apps.builtinHint')}</div>
  {/if}
  <div class="row g-3">
    <div class="col-md-4">
      <label class="form-label" for="app-slug">{t('settings.apps.fieldSlug')}</label>
      <input id="app-slug" class="form-control" bind:value={form.slug} disabled={!!form.id} />
    </div>
    <div class="col-md-8">
      <label class="form-label" for="app-name">{t('common.name')}</label>
      <input id="app-name" class="form-control" bind:value={form.name} />
    </div>
    <div class="col-12">
      <label class="form-label" for="app-desc">{t('common.description')}</label>
      <input id="app-desc" class="form-control" bind:value={form.description} />
    </div>

    <!-- Englische Fassung: optional. Bleibt sie leer, zeigt die englische
         Oberfläche den deutschen Text. -->
    <div class="col-md-4">
      <label class="form-label" for="app-name-en">{t('settings.apps.fieldNameEn')}</label>
      <input id="app-name-en" class="form-control" bind:value={form.name_en} />
    </div>
    <div class="col-md-8">
      <label class="form-label" for="app-desc-en">{t('settings.apps.fieldDescriptionEn')}</label>
      <input id="app-desc-en" class="form-control" bind:value={form.description_en} />
      <div class="form-text">{t('settings.apps.englishHint')}</div>
    </div>

    <div class="col-12">
      <label class="form-label" for="app-markers">{t('settings.apps.fieldMarkers')}</label>
      <textarea id="app-markers" class="form-control font-monospace" rows="4" bind:value={form.markers}></textarea>
      <div class="form-text">{t('settings.apps.markersHelp')}</div>
    </div>

    <div class="col-md-8">
      <label class="form-label" for="app-vcmd">{t('settings.apps.fieldVersionCommand')}</label>
      <input id="app-vcmd" class="form-control font-monospace" bind:value={form.version_command} />
      <div class="form-text">{t('settings.apps.versionCommandHelp')}</div>
    </div>
    <div class="col-md-4">
      <label class="form-label" for="app-compare">{t('settings.apps.fieldCompare')}</label>
      <select id="app-compare" class="form-select" bind:value={form.compare}>
        <option value="semver">{t('settings.apps.compareSemver')}</option>
        <option value="exact">{t('settings.apps.compareExact')}</option>
        <option value="none">{t('settings.apps.compareNone')}</option>
      </select>
    </div>
    <div class="col-12">
      <label class="form-label" for="app-vpat">{t('settings.apps.fieldVersionPattern')}</label>
      <input id="app-vpat" class="form-control font-monospace" bind:value={form.version_pattern} />
      <div class="form-text">{t('settings.apps.versionPatternHelp')}</div>
    </div>

    <div class="col-md-8">
      <label class="form-label" for="app-src">{t('settings.apps.fieldLatestSource')}</label>
      <input id="app-src" class="form-control font-monospace" bind:value={form.latest_source} placeholder="github:owner/repo" />
      <div class="form-text">{t('settings.apps.latestSourceHelp')}</div>
    </div>
    <div class="col-md-4">
      <label class="form-label" for="app-spat">{t('settings.apps.fieldLatestPattern')}</label>
      <input id="app-spat" class="form-control font-monospace" bind:value={form.latest_pattern} />
    </div>

    <div class="col-md-6">
      <label class="form-label" for="app-backup">{t('settings.apps.fieldBackupAction')}</label>
      <select id="app-backup" class="form-select" bind:value={form.backup_action_id}>
        <option value="">{t('settings.apps.noAction')}</option>
        {#each actions as a (a.id)}<option value={a.id}>{a.name}</option>{/each}
      </select>
    </div>
    <div class="col-md-6">
      <label class="form-label" for="app-update">{t('settings.apps.fieldUpdateAction')}</label>
      <select id="app-update" class="form-select" bind:value={form.update_action_id}>
        <option value="">{t('settings.apps.noAction')}</option>
        {#each actions as a (a.id)}<option value={a.id}>{a.name}</option>{/each}
      </select>
      <div class="form-text">{t('settings.apps.actionHelp')}</div>
    </div>

    <div class="col-12 form-check ms-2">
      <input class="form-check-input" type="checkbox" id="app-enabled" bind:checked={form.enabled} />
      <label class="form-check-label" for="app-enabled">{t('settings.apps.fieldEnabled')}</label>
    </div>
  </div>

  <div class="text-end mt-3">
    <button class="btn btn-secondary" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" data-testid="app-save" disabled={!formValid} onclick={save}>
      {form.id ? t('common.save') : t('common.create')}
    </button>
  </div>
</Modal>
