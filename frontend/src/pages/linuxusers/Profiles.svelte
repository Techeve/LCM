<script>
  // Berechtigungsprofile: benannte Rechtebündel für die von LCM verteilten
  // Linux-Benutzer. Ein Profil beschreibt, welche Kommandos jemand als root
  // ausführen, welche Dateien er bearbeiten und auf welche Verzeichnisse er
  // zugreifen darf - statt des bisherigen Alles-oder-nichts über „sudo".
  //
  // Die mitgelieferten Profile bilden den heutigen Zustand ab und sind
  // deshalb schreibgeschützt.
  import { api, ApiError } from '../../api';
  import { auth } from '../../stores/auth.svelte.js';
  import { i18n } from '../../stores/i18n.svelte.js';
  import LinuxUsersLayout from '../../components/LinuxUsersLayout.svelte';
  import Modal from '../../components/Modal.svelte';

  const t = (k, p) => i18n.t(k, p);
  const canWrite = auth.can('profiles:write');

  let profiles = $state([]);
  let allBlocks = $state([]);
  let error = $state('');
  let notice = $state('');

  let open = $state(false);
  let form = $state(emptyForm());

  function emptyForm() {
    return { id: null, name: '', slug: '', description: '', accountType: 'shell', sudoRules: [], editRules: [], pathRules: [], blockUses: [] };
  }

  // Parameterzeilen eines Bausteins („name=wert" je Zeile) für das Formular.
  function blockParams(blockId) {
    const b = allBlocks.find((x) => x.id === blockId);
    return b ? (b.params || '').split(',').map((p) => p.trim()).filter(Boolean) : [];
  }
  function blockName(blockId) {
    const b = allBlocks.find((x) => x.id === blockId);
    return b ? b.name : `#${blockId}`;
  }
  function paramValue(use, name) {
    const line = (use.values || '').split('\n').find((l) => l.trim().startsWith(name + '='));
    return line ? line.split('=').slice(1).join('=').trim() : '';
  }
  function setParamValue(use, name, value) {
    const kept = (use.values || '').split('\n').filter((l) => l.trim() && !l.trim().startsWith(name + '='));
    use.values = [...kept, `${name}=${value}`].join('\n');
    form.blockUses = [...form.blockUses];
  }

  // Der Slug wird aus dem Namen vorgeschlagen: Er bildet auf dem Zielsystem
  // den Gruppennamen und ist deshalb auf Kleinbuchstaben, Ziffern und
  // Bindestriche begrenzt.
  function slugFromName(name) {
    return (name || '')
      .toLowerCase()
      .replace(/[äöüß]/g, (c) => ({ ä: 'ae', ö: 'oe', ü: 'ue', ß: 'ss' })[c])
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 20)
      .replace(/-+$/, '');
  }

  function ruleCount(p) {
    return (p.sudo_rules?.length ?? 0) + (p.edit_rules?.length ?? 0) + (p.path_rules?.length ?? 0);
  }

  async function load() {
    error = '';
    try {
      [profiles, allBlocks] = await Promise.all([api.profiles.list(), api.profileBlocks.list()]);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function openNew() {
    form = emptyForm();
    open = true;
  }

  function openEdit(p) {
    form = {
      id: p.id,
      name: p.name,
      slug: p.slug,
      description: p.description ?? '',
      accountType: p.account_type || 'shell',
      sudoRules: (p.sudo_rules ?? []).map((r) => ({
        command: r.command,
        run_as: r.run_as || 'root',
        require_password: !!r.require_password,
        allow_root_equivalent: !!r.allow_root_equivalent,
      })),
      editRules: (p.edit_rules ?? []).map((r) => ({ path: r.path })),
      pathRules: (p.path_rules ?? []).map((r) => ({ path: r.path, mode: r.mode })),
      blockUses: (p.block_uses ?? []).map((u) => ({ block_id: u.block_id, values: u.values ?? '' })),
    };
    open = true;
  }

  const addSudoRule = () =>
    (form.sudoRules = [...form.sudoRules, { command: '', run_as: 'root', require_password: false, allow_root_equivalent: false }]);
  const addEditRule = () => (form.editRules = [...form.editRules, { path: '' }]);
  const addPathRule = () => (form.pathRules = [...form.pathRules, { path: '', mode: 'read' }]);
  const addBlockUse = (id) => (form.blockUses = [...form.blockUses, { block_id: Number(id), values: '' }]);
  const dropBlockUse = (i) => (form.blockUses = form.blockUses.filter((_, n) => n !== i));

  const dropSudoRule = (i) => (form.sudoRules = form.sudoRules.filter((_, n) => n !== i));
  const dropEditRule = (i) => (form.editRules = form.editRules.filter((_, n) => n !== i));
  const dropPathRule = (i) => (form.pathRules = form.pathRules.filter((_, n) => n !== i));

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
      account_type: f.accountType,
      // Leere Zeilen fallen weg: Eine angefangene und wieder verworfene
      // Regel soll das Speichern nicht mit einer Fehlermeldung blockieren.
      sudo_rules: f.sudoRules.filter((r) => r.command.trim()),
      edit_rules: f.editRules.filter((r) => r.path.trim()),
      path_rules: f.pathRules.filter((r) => r.path.trim()),
      block_uses: f.blockUses,
    };
    if (f.id) {
      await run(() => api.profiles.update(f.id, data), t('settings.profiles.notices.saved'));
    } else {
      await run(() => api.profiles.create({ ...data, slug: f.slug || slugFromName(f.name) }), t('settings.profiles.notices.created'));
    }
    if (!error) open = false;
  }

  // Kopieren: Der Name bekommt einen Zusatz, der Slug entsteht daraus. Gibt
  // es die Kopie schon, wird durchgezaehlt - sonst scheitert der zweite
  // Klick am belegten Slug.
  async function copy(p) {
    const name = freeCopyName(p.name);
    await run(() => api.profiles.clone(p.id, slugFromName(name), name), t('settings.profiles.notices.copied'));
  }

  function freeCopyName(base) {
    const suffix = t('settings.profiles.copySuffix');
    let name = `${base} ${suffix}`;
    for (let n = 2; profiles.some((p) => p.name === name); n++) {
      name = `${base} ${suffix} ${n}`;
    }
    return name;
  }

  async function remove(p) {
    if (!confirm(t('settings.profiles.confirmDelete', { name: p.name }))) return;
    await run(() => api.profiles.remove(p.id), t('settings.profiles.notices.deleted'));
  }

  $effect(() => {
    load();
  });
</script>

<LinuxUsersLayout>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  <div class="d-flex justify-content-between align-items-center mb-3">
    <p class="small text-body-secondary mb-0">{t('settings.profiles.intro')}</p>
    {#if canWrite}
      <button class="btn btn-sm btn-primary text-nowrap ms-3" onclick={openNew}>{t('settings.profiles.addProfile')}</button>
    {/if}
  </div>

  <div class="table-responsive">
    <table class="table table-sm align-middle">
      <thead>
        <tr>
          <th>{t('common.name')}</th>
          <th>{t('settings.profiles.colSlug')}</th>
          <th>{t('common.description')}</th>
          <th>{t('settings.profiles.colRules')}</th>
          <th class="text-end">{t('settings.profiles.colManage')}</th>
        </tr>
      </thead>
      <tbody>
        {#each profiles as p (p.id)}
          <tr>
            <td>
              {p.name}
              {#if p.builtin}<span class="badge text-bg-secondary ms-1">{t('settings.profiles.builtin')}</span>{/if}
              {#if p.grants_full_root}<span class="badge text-bg-warning ms-1">{t('settings.profiles.fullRoot')}</span>{/if}
              {#if p.account_type === 'sftp'}<span class="badge text-bg-info ms-1">{t('settings.profiles.accountSFTP')}</span>{/if}
            </td>
            <td class="small font-monospace">{p.slug}</td>
            <td class="small text-body-secondary">{p.description || '-'}</td>
            <td class="small">{ruleCount(p)}</td>
            <td class="text-end text-nowrap">
              {#if canWrite}
                <!-- Kopieren steht auch bei den mitgelieferten Profilen: Sie
                     sind schreibgeschützt, und die Kopie ist der vorgesehene
                     Weg, eines davon anzupassen. -->
                <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => copy(p)}>{t('settings.profiles.copy')}</button>
              {/if}
              {#if p.builtin}
                <span class="small text-body-secondary ms-1">{t('settings.profiles.protected')}</span>
              {:else if canWrite}
                <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEdit(p)}>{t('common.edit')}</button>
                <button class="btn btn-sm btn-outline-danger py-0" onclick={() => remove(p)}>{t('common.delete')}</button>
              {/if}
            </td>
          </tr>
        {:else}
          <tr><td colspan="5" class="text-body-secondary small">{t('settings.profiles.noProfiles')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>

  <p class="small text-body-secondary">{t('settings.profiles.notAppliedYet')}</p>
</LinuxUsersLayout>

<Modal title={form.id ? t('settings.profiles.editTitle') : t('settings.profiles.newTitle')} bind:open size="modal-lg">
  <label class="form-label small mb-1" for="pf-name">{t('common.name')}</label>
  <input id="pf-name" class="form-control mb-2" bind:value={form.name} placeholder={t('settings.profiles.namePlaceholder')} />

  {#if !form.id}
    <label class="form-label small mb-1" for="pf-slug">{t('settings.profiles.colSlug')}</label>
    <input id="pf-slug" class="form-control mb-1 font-monospace" bind:value={form.slug} placeholder={slugFromName(form.name)} />
    <p class="small text-body-secondary">{t('settings.profiles.slugHint')}</p>
  {/if}

  <label class="form-label small mb-1" for="pf-desc">{t('common.description')}</label>
  <input id="pf-desc" class="form-control mb-2" bind:value={form.description} />

  <label class="form-label small mb-1" for="pf-account">{t('settings.profiles.accountType')}</label>
  <select id="pf-account" class="form-select mb-1" bind:value={form.accountType}>
    <option value="shell">{t('settings.profiles.accountShell')}</option>
    <option value="sftp">{t('settings.profiles.accountSFTP')}</option>
  </select>
  <p class="small text-body-secondary mb-3">{t('settings.profiles.accountHint')}</p>

  <!-- Regelbausteine -->
  <h3 class="h6 mb-0">{t('settings.profiles.blocks')}</h3>
  <p class="small text-body-secondary mb-2">{t('settings.profiles.blocksHint')}</p>
  {#each form.blockUses as use, i (i)}
    <div class="border rounded p-2 mb-2">
      <div class="d-flex align-items-center gap-2 mb-1">
        <strong class="small">{blockName(use.block_id)}</strong>
        <button class="btn btn-sm btn-outline-danger py-0 ms-auto" onclick={() => dropBlockUse(i)}>{t('common.delete')}</button>
      </div>
      {#each blockParams(use.block_id) as param (param)}
        <div class="input-group input-group-sm mb-1">
          <span class="input-group-text font-monospace">{param}</span>
          <input class="form-control font-monospace" value={paramValue(use, param)}
            onchange={(e) => setParamValue(use, param, e.currentTarget.value)} spellcheck="false" />
        </div>
      {/each}
    </div>
  {/each}
  <select class="form-select form-select-sm mb-3" value="" onchange={(e) => { if (e.currentTarget.value) { addBlockUse(e.currentTarget.value); e.currentTarget.value = ''; } }}>
    <option value="">{t('settings.profiles.addBlock')}</option>
    {#each allBlocks as b (b.id)}<option value={b.id}>{b.name}</option>{/each}
  </select>

  <!-- Kommandos als root -->
  <div class="d-flex justify-content-between align-items-center">
    <h3 class="h6 mb-0">{t('settings.profiles.sudoRules')}</h3>
    <button class="btn btn-sm btn-outline-secondary py-0" onclick={addSudoRule}>{t('settings.profiles.addRule')}</button>
  </div>
  <p class="small text-body-secondary mb-2">{t('settings.profiles.sudoHint')}</p>
  {#each form.sudoRules as rule, i (i)}
    <div class="border rounded p-2 mb-2">
      <input class="form-control form-control-sm font-monospace mb-1" bind:value={rule.command}
        placeholder="/usr/bin/systemctl restart nginx" spellcheck="false" />
      <div class="d-flex flex-wrap align-items-center gap-3">
        <div class="form-check form-check-inline mb-0">
          <input class="form-check-input" type="checkbox" id="pf-pw-{i}" bind:checked={rule.require_password} />
          <label class="form-check-label small" for="pf-pw-{i}">{t('settings.profiles.requirePassword')}</label>
        </div>
        <div class="form-check form-check-inline mb-0">
          <input class="form-check-input" type="checkbox" id="pf-root-{i}" bind:checked={rule.allow_root_equivalent} />
          <label class="form-check-label small" for="pf-root-{i}">{t('settings.profiles.allowRootEquivalent')}</label>
        </div>
        <button class="btn btn-sm btn-outline-danger py-0 ms-auto" onclick={() => dropSudoRule(i)}>{t('common.delete')}</button>
      </div>
    </div>
  {/each}

  <!-- Dateien per sudoedit -->
  <div class="d-flex justify-content-between align-items-center mt-3">
    <h3 class="h6 mb-0">{t('settings.profiles.editRules')}</h3>
    <button class="btn btn-sm btn-outline-secondary py-0" onclick={addEditRule}>{t('settings.profiles.addRule')}</button>
  </div>
  <p class="small text-body-secondary mb-2">{t('settings.profiles.editHint')}</p>
  {#each form.editRules as rule, i (i)}
    <div class="input-group input-group-sm mb-2">
      <input class="form-control font-monospace" bind:value={rule.path}
        placeholder="/etc/nginx/sites-available/kunde.conf" spellcheck="false" />
      <button class="btn btn-outline-danger" onclick={() => dropEditRule(i)}>{t('common.delete')}</button>
    </div>
  {/each}

  <!-- Verzeichnisrechte -->
  <div class="d-flex justify-content-between align-items-center mt-3">
    <h3 class="h6 mb-0">{t('settings.profiles.pathRules')}</h3>
    <button class="btn btn-sm btn-outline-secondary py-0" onclick={addPathRule}>{t('settings.profiles.addRule')}</button>
  </div>
  <p class="small text-body-secondary mb-2">{t('settings.profiles.pathHint')}</p>
  {#each form.pathRules as rule, i (i)}
    <div class="input-group input-group-sm mb-2">
      <input class="form-control font-monospace" bind:value={rule.path} placeholder="/srv/www" spellcheck="false" />
      <select class="form-select" style="max-width: 12rem" bind:value={rule.mode}>
        <option value="read">{t('settings.profiles.modeRead')}</option>
        <option value="readwrite">{t('settings.profiles.modeReadWrite')}</option>
        <option value="deny">{t('settings.profiles.modeDeny')}</option>
      </select>
      <button class="btn btn-outline-danger" onclick={() => dropPathRule(i)}>{t('common.delete')}</button>
    </div>
  {/each}

  <div class="text-end mt-3">
    <button class="btn btn-secondary" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" onclick={save} disabled={!form.name.trim()}>
      {form.id ? t('common.save') : t('common.create')}
    </button>
  </div>
</Modal>
