<script>
  // Regelbausteine: wiederverwendbare Rechte-Vorlagen für die
  // Berechtigungsprofile. Ein Baustein trägt je Distributions-Familie eine
  // eigene Variante (die Unit heißt auf Debian apache2, auf RHEL httpd) und
  // Platzhalter wie {service}, die beim Einhängen ins Profil gefüllt werden.
  //
  // Mitgelieferte Bausteine werden mit LCM aktualisiert und sind deshalb
  // schreibgeschützt - zum Anpassen klont man sie.
  import { api, ApiError } from '../../api';
  import { auth } from '../../stores/auth.svelte.js';
  import { i18n } from '../../stores/i18n.svelte.js';
  import LinuxUsersLayout from '../../components/LinuxUsersLayout.svelte';
  import Modal from '../../components/Modal.svelte';
  import Pagination from '../../components/Pagination.svelte';
  import { PAGE_SIZE_CATALOG, pageCount, pageSlice } from '../../lib/paging.js';

  const t = (k, p) => i18n.t(k, p);
  const canWrite = auth.can('profiles:write');
  const FAMILIES = ['all', 'apt', 'dnf', 'zypper', 'pacman', 'apk'];

  let page = $state(1);
  let blocks = $state([]);
  // Der mitgelieferte Katalog ist lang genug, dass Scrollen keine Antwort mehr
  // ist: gesucht wird über Name, Slug und Beschreibung - wer „nginx“ tippt,
  // will beide Rollen sehen.
  let filter = $state('');
  let visibleBlocks = $derived(
    blocks.filter((b) => {
      const suche = filter.trim().toLowerCase();
      if (!suche) return true;
      // Gesucht wird in der angezeigten Sprache - sonst fände man einen
      // Baustein nicht unter dem Namen, der auf dem Bildschirm steht.
      return `${i18n.field(b, 'name')} ${b.slug} ${i18n.field(b, 'description')}`.toLowerCase().includes(suche);
    }),
  );
  let error = $state('');
  let notice = $state('');

  let open = $state(false);
  let form = $state(emptyForm());
  let usage = $state([]);
  let preview = $state([]);

  function emptyForm() {
    return { id: null, name: '', slug: '', description: '', name_en: '', description_en: '', params: '', variants: [] };
  }

  function slugFromName(name) {
    return (name || '')
      .toLowerCase()
      .replace(/[äöüß]/g, (c) => ({ ä: 'ae', ö: 'oe', ü: 'ue', ß: 'ss' })[c])
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 40)
      .replace(/-+$/, '');
  }

  async function load() {
    error = '';
    try {
      blocks = await api.profileBlocks.list();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function openNew() {
    form = emptyForm();
    form.variants = [{ family: 'all', run_as: '', sudo_commands: '', edit_paths: '', path_rules: '' }];
    usage = [];
    preview = [];
    open = true;
  }

  async function openEdit(b) {
    form = {
      id: b.id,
      name: b.name,
      slug: b.slug,
      description: b.description ?? '',
      name_en: b.name_en ?? '',
      description_en: b.description_en ?? '',
      params: b.params ?? '',
      variants: (b.variants ?? []).map((v) => ({
        family: v.family,
        run_as: v.run_as ?? '',
        sudo_commands: v.sudo_commands ?? '',
        edit_paths: v.edit_paths ?? '',
        path_rules: v.path_rules ?? '',
      })),
    };
    preview = [];
    // Verwendungsnachweis: Eine Baustein-Änderung verändert Rechte auf allen
    // Servern, die ihn über irgendein Profil nutzen - das muss vor dem
    // Speichern dastehen, nicht danach im Protokoll.
    try {
      usage = (await api.profileBlocks.usage(b.id)).profiles ?? [];
    } catch {
      usage = [];
    }
    open = true;
  }

  const addVariant = () =>
    (form.variants = [...form.variants, { family: 'all', run_as: '', sudo_commands: '', edit_paths: '', path_rules: '' }]);
  const dropVariant = (i) => (form.variants = form.variants.filter((_, n) => n !== i));

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
    const data = {
      name: f.name,
      description: f.description,
      name_en: f.name_en,
      description_en: f.description_en,
      params: f.params,
      variants: f.variants.filter((v) => v.sudo_commands.trim() || v.edit_paths.trim() || v.path_rules.trim()),
    };
    if (f.id) await run(() => api.profileBlocks.update(f.id, data), t('settings.blocks.notices.saved'));
    else await run(() => api.profileBlocks.create({ ...data, slug: f.slug || slugFromName(f.name) }), t('settings.blocks.notices.created'));
    if (!error) open = false;
  }

  // Kopieren: Der Zusatz kommt aus dem Sprachkatalog - fest verdrahtet stand
  // in der englischen Oberfläche „(Kopie)". Und gibt es die Kopie schon, wird
  // durchgezählt: Der Name ist eindeutig, ein zweiter Klick scheiterte sonst
  // mit „dieser Name ist bereits vergeben" - ausgerechnet beim Knopf, der der
  // vorgesehene Weg ist, einen mitgelieferten Baustein anzupassen.
  function freeCopyName(base) {
    const suffix = t('settings.blocks.copySuffix');
    let name = `${base} ${suffix}`;
    for (let n = 2; blocks.some((b) => b.name === name || b.name_en === name); n++) {
      name = `${base} ${suffix} ${n}`;
    }
    return name;
  }

  async function clone(b) {
    const name = freeCopyName(i18n.field(b, 'name'));
    await run(() => api.profileBlocks.clone(b.id, slugFromName(name), name), t('settings.blocks.notices.cloned'));
  }

  async function remove(b) {
    if (!confirm(t('settings.blocks.confirmDelete', { name: i18n.field(b, 'name') }))) return;
    await run(() => api.profileBlocks.remove(b.id), t('settings.blocks.notices.deleted'));
  }

  // Vorschau: Was ergibt der Baustein für eine Familie? Nur für gespeicherte
  // Bausteine - sie rendert serverseitig mit denselben Prüfungen.
  async function showPreview(family) {
    error = '';
    try {
      const values = (form.params || '')
        .split(',')
        .map((p) => p.trim())
        .filter(Boolean)
        .map((p) => `${p}=beispiel`)
        .join('\n');
      preview = (await api.profileBlocks.preview(form.id, family, values)).lines ?? [];
    } catch (e) {
      preview = [];
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  $effect(() => {
    load();
  });
</script>

<LinuxUsersLayout>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  <div class="d-flex justify-content-between align-items-center mb-3">
    <p class="small text-body-secondary mb-0">{t('settings.blocks.intro')}</p>
    {#if canWrite}
      <button class="btn btn-sm btn-primary text-nowrap ms-3" onclick={openNew}>{t('settings.blocks.addBlock')}</button>
    {/if}
  </div>

  <div class="mb-2">
    <input
      class="form-control form-control-sm"
      type="search"
      placeholder={t('settings.blocks.filterPlaceholder')}
      bind:value={filter}
      data-testid="block-filter"
    />
  </div>

  <div class="table-responsive">
    <table class="table table-sm align-middle">
      <thead>
        <tr>
          <th>{t('common.name')}</th>
          <th>{t('settings.blocks.colParams')}</th>
          <th>{t('settings.blocks.colVariants')}</th>
          <th>{t('common.description')}</th>
          <th class="text-end">{t('settings.blocks.colManage')}</th>
        </tr>
      </thead>
      <tbody>
        {#each pageSlice(visibleBlocks, page, PAGE_SIZE_CATALOG) as b (b.id)}
          <tr data-testid="block-{b.slug}">
            <td>
              {i18n.field(b, 'name')}
              {#if b.builtin}<span class="badge text-bg-secondary ms-1">{t('settings.blocks.builtin')}</span>{/if}
            </td>
            <td class="small font-monospace">{b.params || '-'}</td>
            <td class="small">{(b.variants ?? []).map((v) => v.family).join(', ')}</td>
            <td class="small text-body-secondary">{i18n.field(b, 'description') || '-'}</td>
            <td class="text-end text-nowrap">
              {#if canWrite}
                {#if b.builtin}
                  <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => clone(b)}>{t('settings.blocks.clone')}</button>
                {:else}
                  <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEdit(b)}>{t('common.edit')}</button>
                  <button class="btn btn-sm btn-outline-danger py-0" onclick={() => remove(b)}>{t('common.delete')}</button>
                {/if}
              {/if}
            </td>
          </tr>
        {:else}
          <tr><td colspan="5" class="text-body-secondary small">{t('settings.blocks.noBlocks')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
  <Pagination
    page={page}
    pageCount={pageCount(visibleBlocks.length, PAGE_SIZE_CATALOG)}
    total={visibleBlocks.length}
    pageSize={PAGE_SIZE_CATALOG}
    testid="blocks-pagination"
    onchange={(p) => (page = p)}
  />
</LinuxUsersLayout>

<Modal title={form.id ? t('settings.blocks.editTitle') : t('settings.blocks.newTitle')} bind:open size="modal-lg">
  {#if usage.length}
    <div class="alert alert-warning py-2 small">
      {t('settings.blocks.usageWarning', { count: usage.length, list: usage.join(', ') })}
    </div>
  {/if}

  <label class="form-label small mb-1" for="bl-name">{t('common.name')}</label>
  <input id="bl-name" class="form-control mb-2" bind:value={form.name} placeholder={t('settings.blocks.namePlaceholder')} />

  {#if !form.id}
    <label class="form-label small mb-1" for="bl-slug">{t('settings.blocks.colSlug')}</label>
    <input id="bl-slug" class="form-control mb-2 font-monospace" bind:value={form.slug} placeholder={slugFromName(form.name)} />
  {/if}

  <label class="form-label small mb-1" for="bl-desc">{t('common.description')}</label>
  <input id="bl-desc" class="form-control mb-2" bind:value={form.description} />

  <!-- Englische Fassung: optional. Bleibt sie leer, zeigt die englische
       Oberfläche den deutschen Text - besser als ein leeres Feld. -->
  <div class="row g-2">
    <div class="col-md-5">
      <label class="form-label small mb-1" for="bl-name-en">{t('settings.blocks.nameEn')}</label>
      <input id="bl-name-en" class="form-control mb-2" bind:value={form.name_en} />
    </div>
    <div class="col-md-7">
      <label class="form-label small mb-1" for="bl-desc-en">{t('settings.blocks.descriptionEn')}</label>
      <input id="bl-desc-en" class="form-control mb-2" bind:value={form.description_en} />
    </div>
  </div>
  <div class="form-text mb-2">{t('settings.blocks.englishHint')}</div>

  <label class="form-label small mb-1" for="bl-params">{t('settings.blocks.params')}</label>
  <input id="bl-params" class="form-control mb-1 font-monospace" bind:value={form.params} placeholder="service,path" />
  <p class="small text-body-secondary">{t('settings.blocks.paramsHint')}</p>

  <div class="d-flex justify-content-between align-items-center mt-3">
    <h3 class="h6 mb-0">{t('settings.blocks.variants')}</h3>
    <button class="btn btn-sm btn-outline-secondary py-0" onclick={addVariant}>{t('settings.blocks.addVariant')}</button>
  </div>
  <p class="small text-body-secondary mb-2">{t('settings.blocks.variantsHint')}</p>

  {#each form.variants as variant, i (i)}
    <div class="border rounded p-2 mb-2">
      <div class="d-flex align-items-center gap-2 mb-1">
        <select class="form-select form-select-sm" style="max-width: 10rem" bind:value={variant.family}>
          {#each FAMILIES as f (f)}<option value={f}>{f === 'all' ? t('settings.blocks.familyAll') : f}</option>{/each}
        </select>
        <input
          class="form-control form-control-sm"
          style="max-width: 11rem"
          placeholder={t('settings.blocks.runAsPlaceholder')}
          title={t('settings.blocks.runAsHint')}
          bind:value={variant.run_as}
        />
        {#if form.id}
          <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => showPreview(variant.family)}>{t('settings.blocks.previewBtn')}</button>
        {/if}
        <button class="btn btn-sm btn-outline-danger py-0 ms-auto" onclick={() => dropVariant(i)}>{t('common.delete')}</button>
      </div>
      <label class="form-label small mb-1" for="bl-cmds-{i}">{t('settings.blocks.sudoCommands')}</label>
      <textarea id="bl-cmds-{i}" class="form-control form-control-sm font-monospace mb-1" rows="4" spellcheck="false"
        placeholder="/usr/bin/systemctl --no-pager restart &#123;service&#125;" bind:value={variant.sudo_commands}></textarea>
      <label class="form-label small mb-1" for="bl-paths-{i}">{t('settings.blocks.editPaths')}</label>
      <textarea id="bl-paths-{i}" class="form-control form-control-sm font-monospace mb-1" rows="2" spellcheck="false"
        bind:value={variant.edit_paths}></textarea>
      <label class="form-label small mb-1" for="bl-dirs-{i}">{t('settings.blocks.pathRules')}</label>
      <textarea id="bl-dirs-{i}" class="form-control form-control-sm font-monospace" rows="2" spellcheck="false"
        placeholder="readwrite /etc/nginx" bind:value={variant.path_rules}></textarea>
      <p class="form-text small mb-0">{t('settings.blocks.pathRulesHint')}</p>
    </div>
  {/each}

  {#if preview.length}
    <div class="border rounded p-2 mb-2 bg-body-tertiary">
      <p class="small mb-1">{t('settings.blocks.previewTitle')}</p>
      <pre class="small mb-0">{preview.join('\n')}</pre>
    </div>
  {/if}

  <div class="text-end mt-2">
    <button class="btn btn-secondary" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" onclick={save} disabled={!form.name.trim()}>
      {form.id ? t('common.save') : t('common.create')}
    </button>
  </div>
</Modal>
