<script>
  // Linux-Benutzer: Betriebssystem-Accounts, die LCM per Sync auf den
  // Servern anlegt. Große Tabelle + Popups zum Erfassen und Verwalten.
  // Wichtige Sicherheitsvorgabe: Ein Benutzer kann erst gelöscht werden,
  // wenn er zuvor aktiv von ALLEN Servern entfernt (deprovisioniert) wurde.
  import { api, ApiError } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  import LinuxUsersLayout from '../components/LinuxUsersLayout.svelte';
  import Modal from '../components/Modal.svelte';
  import SshKeyGuide from '../components/SshKeyGuide.svelte';
  import { downloadText } from '../lib/download.js';
  import { toasts } from '../stores/toast.svelte.js';
  import Pagination from '../components/Pagination.svelte';
  import { PAGE_SIZE, pageCount, pageSlice } from '../lib/paging.js';

  const t = (k, p) => i18n.t(k, p);

  let page = $state(1);
  let users = $state([]);
  let groups = $state([]);
  let profiles = $state([]);
  // Profil je Gruppen-Zuweisung: Gruppen-ID → Profil-ID ('' = Standardprofil).
  let groupProfiles = $state({});
  let busy = $state(false);

  // Erfassen-Popup
  let createOpen = $state(false);
  let form = $state({ username: '', fullName: '', email: '', shell: '/bin/bash', sudo: false });

  // Verwalten-Popup (voll geladener Benutzer) + Hilfsstate
  let manage = $state(null);
  let newKey = $state({ name: '', publicKey: '' });
  let assignGroupId = $state('');
  let deprovReport = $state(null); // Ergebnis von "von allen Servern entfernen"

  // Aktivierungslink- und Schlüssel-Popups
  let activationLink = $state(null);
  let generatedKey = $state(null);

  const canWrite = $derived(auth.can('linuxusers:write'));

  // Ist der (im Popup) verwaltete Benutzer noch irgendwo zugeordnet? Dann darf
  // er nicht gelöscht werden - erst deprovisionieren.
  let manageAssigned = $derived(
    manage ? (manage.servers?.length ?? 0) > 0 || (manage.groups?.length ?? 0) > 0 : false,
  );

  async function load() {
    try {
      [users, groups, profiles] = await Promise.all([
        api.linuxUsers.list(),
        api.groups.list(),
        auth.can('profiles:read') ? api.profiles.list() : Promise.resolve([]),
      ]);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    }
  }

  async function openManage(u) {
    deprovReport = null;
    manage = await api.linuxUsers.get(u.id);
    await loadGroupProfiles();
  }

  // Welches Profil an welcher Gruppen-Zuweisung hängt. Leer = das
  // Standardprofil des Benutzers gilt auch dort.
  async function loadGroupProfiles() {
    if (!manage) return;
    try {
      const rows = await api.linuxUsers.groupAssignments(manage.id);
      groupProfiles = Object.fromEntries(rows.map((r) => [r.server_group_id, r.profile_id ?? '']));
    } catch {
      groupProfiles = {};
    }
  }

  async function setGroupProfile(groupId, value) {
    const profileId = value === '' ? null : Number(value);
    groupProfiles = { ...groupProfiles, [groupId]: value };
    await run(
      () => api.linuxUsers.setGroupProfile(manage.id, groupId, profileId),
      t('linuxUsers.notices.profileSet'),
    );
  }

  // Anzeigename eines Profils (für die Liste der Zuweisungen).
  function profileName(id) {
    const p = profiles.find((x) => x.id === id);
    return p ? p.name : '-';
  }

  // Verzeichnisrechte brauchen ACLs auf dem Zielsystem. Wird ein Profil mit
  // Pfadregeln einer Gruppe zugewiesen, deren Server das nicht alle können,
  // muss das HIER stehen - bevor sich jemand auf Rechte verlässt, die dort
  // nicht wirken.
  function aclWarning(groupId) {
    const chosen = groupProfiles[groupId];
    if (!chosen) return '';
    const profile = profiles.find((p) => p.id === Number(chosen));
    if (!profile || !(profile.path_rules?.length > 0)) return '';
    const group = groups.find((g) => g.id === groupId);
    if (!group || group.acl_servers === 0 || group.acl_capable === group.acl_servers) return '';
    return t('linuxUsers.aclIncomplete', { ok: group.acl_capable, total: group.acl_servers });
  }

  async function refreshManage() {
    if (manage) manage = await api.linuxUsers.get(manage.id);
  }

  // Rueckgabe: true bei Erfolg. Die Aufrufer brauchen das, um ein Popup nur
  // dann zu schliessen, wenn die Aktion auch geklappt hat.
  //
  // Die Meldungen laufen ueber die Toast-Region: Ein Alert am Seitenanfang
  // lag bei den Aktionen im Verwalten-Popup hinter dem Modal-Backdrop und war
  // damit unsichtbar - der Fehler kam an, sah aber aus wie „nichts passiert".
  async function run(fn, msg) {
    if (busy) return false;
    busy = true;
    toasts.clear();
    try {
      await fn();
      toasts.success(msg);
      await load();
      await refreshManage();
      return true;
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
      return false;
    } finally {
      busy = false;
    }
  }

  async function createUser(event) {
    event.preventDefault();
    const ok = await run(() => api.linuxUsers.create(form), t('linuxUsers.notices.created', { name: form.username }));
    if (ok) {
      createOpen = false;
      form = { username: '', fullName: '', email: '', shell: '/bin/bash', sudo: false };
    }
  }

  async function saveEdit() {
    await run(
      () => api.linuxUsers.update(manage.id, {
        fullName: manage.full_name, email: manage.email, shell: manage.shell,
        active: manage.active,
        defaultProfileId: manage.default_profile_id ?? null,
      }),
      t('linuxUsers.notices.saved'),
    );
  }

  async function generateActivation() {
    try {
      const res = await api.linuxUsers.generateActivation(manage.id, 48);
      const base = `${location.origin}${location.pathname}#/linux-aktivierung?token=`;
      activationLink = { url: base + encodeURIComponent(res.token), expires_at: res.expires_at };
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    }
  }

  async function addKey() {
    await run(() => api.linuxUsers.addKey(manage.id, newKey.name, newKey.publicKey), t('linuxUsers.notices.keyAdded'));
    newKey = { name: '', publicKey: '' };
  }

  async function generateKey() {
    const username = manage.username;
    await run(async () => {
      const res = await api.linuxUsers.generateKey(manage.id, newKey.name);
      generatedKey = { privateKey: res.private_key, username };
    }, t('linuxUsers.notices.keypairGenerated'));
    newKey = { name: '', publicKey: '' };
  }

  async function assignGroup() {
    if (!assignGroupId) return;
    await run(() => api.linuxUsers.assignGroup(manage.id, Number(assignGroupId), null), t('linuxUsers.notices.groupAssigned'));
    assignGroupId = '';
    await loadGroupProfiles();
  }

  // Von allen Servern entfernen (deprovisionieren) - Pflichtschritt vor dem Löschen.
  async function removeFromServers() {
    busy = true;
    try {
      const res = await api.linuxUsers.removeFromServers(manage.id);
      deprovReport = res.results ?? [];
      toasts.success(t('linuxUsers.notices.removedFromServers'));
      await load();
      await refreshManage();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  async function deleteUser() {
    busy = true;
    try {
      await api.linuxUsers.remove(manage.id);
      manage = null;
      toasts.success(t('linuxUsers.notices.deleted'));
      await load();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  $effect(() => {
    if (auth.isLoggedIn) load();
  });
</script>

<LinuxUsersLayout>
  <!-- Kopfzeile wie auf den beiden anderen Reitern: Erklärung links,
       Aktion rechts. -->
  <div class="d-flex justify-content-between align-items-center mb-3">
    <p class="small text-body-secondary mb-0">{t('linuxUsers.intro')}</p>
    {#if canWrite}
      <button class="btn btn-sm btn-primary text-nowrap ms-3" onclick={() => (createOpen = true)}>
        {t('linuxUsers.addUser')}
      </button>
    {/if}
  </div>


  <div class="table-responsive">
    <table class="table table-hover align-middle">
      <thead>
        <tr>
          <th>{t('linuxUsers.colUsername')}</th><th>{t('linuxUsers.colDisplayName')}</th><th>{t('linuxUsers.colEmail')}</th>
          <th>{t('linuxUsers.colRights')}</th><th>{t('linuxUsers.colKeys')}</th><th>{t('linuxUsers.colServers')}</th><th>{t('linuxUsers.colStatus')}</th><th></th>
        </tr>
      </thead>
      <tbody>
        {#each pageSlice(users, page) as u (u.id)}
          <tr>
            <td><code>{u.username}</code></td>
            <td>{u.full_name || '-'}</td>
            <td class="small">{u.email || '-'}</td>
            <td>
              {#if u.sudo}<span class="badge text-bg-warning">{profileName(u.default_profile_id)}</span>
              {:else}<span class="small text-body-secondary">{profileName(u.default_profile_id)}</span>{/if}
            </td>
            <td>{u.ssh_keys?.length ?? 0}</td>
            <td>{u.servers?.length ?? 0}{#if (u.groups?.length ?? 0) > 0} <span class="text-body-secondary small">{t('linuxUsers.groupsShort', { count: u.groups.length })}</span>{/if}</td>
            <td>
              {#if !u.active}<span class="badge text-bg-secondary">{t('linuxUsers.statusInactive')}</span>
              {:else if u.has_password}<span class="badge text-bg-success">{t('linuxUsers.statusPassword')}</span>
              {:else}<span class="badge border text-body-secondary">{t('linuxUsers.statusKeysOnly')}</span>{/if}
            </td>
            <td class="text-end">
              <button class="btn btn-sm btn-outline-primary" onclick={() => openManage(u)}>{t('linuxUsers.manage')}</button>
            </td>
          </tr>
        {:else}
          <tr><td colspan="8" class="text-body-secondary">{t('linuxUsers.noUsers')}</td></tr>
        {/each}
      </tbody>
    </table>
    <Pagination
      page={page}
      pageCount={pageCount(users.length)}
      total={users.length}
      pageSize={PAGE_SIZE}
      testid="linuxusers-pagination"
      onchange={(p) => (page = p)}
    />
  </div>
</LinuxUsersLayout>

<!-- Erfassen-Popup -->
<Modal title={t('linuxUsers.createTitle')} bind:open={createOpen}>
  <form onsubmit={createUser}>
    <div class="mb-2">
      <label class="form-label" for="lu-name">{t('linuxUsers.username')}</label>
      <input id="lu-name" class="form-control" bind:value={form.username} placeholder="deploy" required />
    </div>
    <div class="row g-2 mb-2">
      <div class="col">
        <label class="form-label" for="lu-full">{t('linuxUsers.displayName')}</label>
        <input id="lu-full" class="form-control" bind:value={form.fullName} />
      </div>
      <div class="col">
        <label class="form-label" for="lu-email">{t('linuxUsers.emailForActivation')}</label>
        <input id="lu-email" type="email" class="form-control" bind:value={form.email} />
      </div>
    </div>
    <div class="row g-2 mb-3 align-items-end">
      <div class="col">
        <label class="form-label" for="lu-shell">{t('linuxUsers.shell')}</label>
        <input id="lu-shell" class="form-control" bind:value={form.shell} />
      </div>
      <div class="col-auto">
        <div class="form-check">
          <input class="form-check-input" type="checkbox" id="lu-sudo" bind:checked={form.sudo} />
          <label class="form-check-label" for="lu-sudo">sudo</label>
        </div>
      </div>
    </div>
    <button class="btn btn-primary" type="submit" disabled={busy || !form.username}>{t('linuxUsers.create')}</button>
  </form>
</Modal>

<!-- Verwalten-Popup -->
<Modal title={manage ? t('linuxUsers.userTitle', { name: manage.username }) : ''} size="modal-lg"
  bind:open={() => manage !== null, (v) => { if (!v) manage = null; }}>
  {#if manage}
    <div class="row g-2 mb-2">
      <div class="col-md-6">
        <label class="form-label" for="ed-full">{t('linuxUsers.displayName')}</label>
        <input id="ed-full" class="form-control" bind:value={manage.full_name} disabled={!canWrite} />
      </div>
      <div class="col-md-6">
        <label class="form-label" for="ed-email">{t('linuxUsers.email')}</label>
        <input id="ed-email" type="email" class="form-control" bind:value={manage.email} disabled={!canWrite} />
      </div>
    </div>
    <div class="row g-2 mb-2 align-items-end">
      <div class="col-md-6">
        <label class="form-label" for="ed-shell">{t('linuxUsers.shell')}</label>
        <input id="ed-shell" class="form-control" bind:value={manage.shell} disabled={!canWrite} />
      </div>
      <div class="col-md-6 d-flex align-items-end gap-3">
        <div class="form-check"><input class="form-check-input" type="checkbox" id="ed-active" bind:checked={manage.active} disabled={!canWrite} /><label class="form-check-label" for="ed-active">{t('linuxUsers.active')}</label></div>
      </div>
    </div>
    <div class="mb-2">
      <label class="form-label" for="ed-profile">{t('linuxUsers.defaultProfile')}</label>
      <select id="ed-profile" class="form-select" bind:value={manage.default_profile_id} disabled={!canWrite}>
        {#each profiles as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
      </select>
      <p class="small text-body-secondary mt-1 mb-0">{t('linuxUsers.defaultProfileHint')}</p>
    </div>
    {#if canWrite}
      <div class="d-flex gap-2 align-items-center mb-3">
        <button class="btn btn-sm btn-primary" onclick={saveEdit} disabled={busy}>{t('linuxUsers.save')}</button>
        <button class="btn btn-sm btn-outline-primary" onclick={generateActivation} disabled={busy}>{t('linuxUsers.genActivation')}</button>
        <span class="small ms-auto">
          {#if manage.has_password}<span class="badge text-bg-success">{t('linuxUsers.passwordSet')}</span>{:else}<span class="badge text-bg-secondary">{t('linuxUsers.noPassword')}</span>{/if}
        </span>
      </div>
    {/if}

    <h3 class="h6">{t('linuxUsers.sshKeys')}</h3>
    <ul class="list-group mb-2">
      {#each manage.ssh_keys ?? [] as k (k.id)}
        <li class="list-group-item d-flex justify-content-between align-items-center py-1">
          <span><strong>{k.name}</strong> <code class="small text-truncate d-inline-block" style="max-width: 340px">{k.public_key}</code></span>
          {#if canWrite}<button class="btn btn-sm btn-outline-danger" aria-label={t('linuxUsers.a11y.removeKey')} onclick={() => { if (!confirm(t('linuxUsers.confirms.removeKey', { name: k.name }))) return; run(() => api.linuxUsers.removeKey(k.id), t('linuxUsers.notices.keyRemoved')); }}>×</button>{/if}
        </li>
      {:else}
        <li class="list-group-item text-body-secondary small">{t('linuxUsers.noKeys')}</li>
      {/each}
    </ul>
    {#if canWrite}
      <div class="row g-2 mb-1">
        <div class="col-md-3"><input class="form-control form-control-sm" placeholder={t('linuxUsers.name')} bind:value={newKey.name} /></div>
        <div class="col-md-7"><input class="form-control form-control-sm" placeholder="ssh-ed25519 AAAA…" bind:value={newKey.publicKey} /></div>
        <div class="col-md-2"><button class="btn btn-sm btn-primary w-100" onclick={addKey} disabled={busy || !newKey.publicKey}>{t('linuxUsers.addKey')}</button></div>
      </div>
      <div class="small text-body-secondary mb-3 d-flex align-items-center gap-2">
        <span>{t('linuxUsers.noKeypairHint')}</span>
        <button class="btn btn-sm btn-outline-primary" onclick={generateKey} disabled={busy || !!newKey.publicKey}>{t('linuxUsers.genKeypair')}</button>
      </div>
    {/if}

    <h3 class="h6">{t('linuxUsers.serverGroups')}</h3>
    <ul class="list-group mb-2">
      {#each manage.groups ?? [] as g (g.id)}
        <li class="list-group-item d-flex justify-content-between align-items-center gap-2 py-1">
          <span class="text-nowrap">{g.name}</span>
          <!-- Je Gruppe ein eigenes Profil: Derselbe Benutzer kann auf den
               Webservern anders berechtigt sein als auf den Datenbanken. -->
          <select class="form-select form-select-sm" style="max-width: 16rem" disabled={!canWrite}
            value={groupProfiles[g.id] ?? ''}
            onchange={(e) => setGroupProfile(g.id, e.currentTarget.value)}>
            <option value="">{t('linuxUsers.useDefaultProfile')}</option>
            {#each profiles as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
          </select>
          {#if canWrite}<button class="btn btn-sm btn-outline-danger" aria-label={t('linuxUsers.a11y.removeGroup')} onclick={() => run(() => api.linuxUsers.removeGroup(manage.id, g.id), t('linuxUsers.notices.removedFromGroup'))}>×</button>{/if}
        </li>
        {#if aclWarning(g.id)}
          <li class="list-group-item py-1 small text-warning-emphasis bg-warning-subtle">{aclWarning(g.id)}</li>
        {/if}
      {:else}
        <li class="list-group-item text-body-secondary small">{t('linuxUsers.noGroupAssigned')}</li>
      {/each}
    </ul>
    {#if canWrite}
      <div class="input-group input-group-sm mb-3" style="max-width: 360px">
        <select class="form-select" bind:value={assignGroupId}>
          <option value="">{t('linuxUsers.chooseGroup')}</option>
          {#each groups as g (g.id)}<option value={g.id}>{g.name}</option>{/each}
        </select>
        <button class="btn btn-primary" onclick={assignGroup} disabled={busy || !assignGroupId}>{t('linuxUsers.assign')}</button>
      </div>
    {/if}

    {#if manage.servers?.length}
      <p class="small text-body-secondary mb-1">{t('linuxUsers.directServers', { list: manage.servers.map((s) => s.name).join(', ') })}</p>
    {/if}

    {#if deprovReport}
      <div class="alert alert-info py-2 small">
        <strong>{t('linuxUsers.deprovisioned')}</strong>
        {#each deprovReport as r (r.server_id)}
          <div>{r.ok ? '✓' : '✗'} {r.server} - {r.message}</div>
        {:else}
          <div>{t('linuxUsers.deprovNone')}</div>
        {/each}
      </div>
    {/if}

    {#if canWrite}
      <hr />
      <h3 class="h6 text-danger">{t('linuxUsers.removeUser')}</h3>
      {#if manageAssigned}
        <p class="small text-body-secondary mb-2">
          {t('linuxUsers.removeAssignedHintA')}<strong>{t('linuxUsers.removeAssignedBold')}</strong>{t('linuxUsers.removeAssignedHintB')}<code>userdel</code>{t('linuxUsers.removeAssignedHintC')}
        </p>
        <div class="d-flex gap-2">
          <button class="btn btn-outline-danger" onclick={removeFromServers} disabled={busy}>
            {busy ? t('linuxUsers.removing') : t('linuxUsers.removeFromAllServers')}
          </button>
          <button class="btn btn-danger" disabled title={t('linuxUsers.deleteUserDisabledTitle')}>{t('linuxUsers.deleteUser')}</button>
        </div>
      {:else}
        <p class="small text-body-secondary mb-2">
          {t('linuxUsers.removeUnassignedHint')}
        </p>
        <button class="btn btn-danger" onclick={deleteUser} disabled={busy}>{t('linuxUsers.deleteUser')}</button>
      {/if}
    {/if}
  {/if}
</Modal>

<!-- Privater SSH-Schlüssel (Einmal-Download) -->
<Modal title={t('linuxUsers.privKeyTitle')} bind:open={() => generatedKey !== null, (v) => { if (!v) generatedKey = null; }}>
  {#if generatedKey}
    <p class="small">
      {t('linuxUsers.privKeyIntroA')}<code>{generatedKey.username}</code>{t('linuxUsers.privKeyIntroB')}<strong>{t('linuxUsers.privKeyIntroBold')}</strong>{t('linuxUsers.privKeyIntroC')}<strong>{t('linuxUsers.privKeyIntroBold2')}</strong>{t('linuxUsers.privKeyIntroD')}
    </p>
    <div class="d-flex gap-2">
      <button class="btn btn-primary" onclick={() => downloadText(`id_ed25519_${generatedKey.username}`, generatedKey.privateKey)}>
        {t('linuxUsers.downloadPrivKey')}
      </button>
      <button class="btn btn-outline-secondary" onclick={() => navigator.clipboard?.writeText(generatedKey.privateKey)}>{t('linuxUsers.copy')}</button>
    </div>
    <SshKeyGuide username={generatedKey.username} />
  {/if}
</Modal>

<!-- Aktivierungslink -->
<Modal title={t('linuxUsers.activationLinkTitle')} bind:open={() => activationLink !== null, (v) => { if (!v) activationLink = null; }}>
  {#if activationLink}
    <p class="small">
      {t('linuxUsers.activationIntro', { until: new Date(activationLink.expires_at).toLocaleString() })}
    </p>
    <div class="input-group">
      <input class="form-control user-select-all" readonly value={activationLink.url} />
      <button class="btn btn-outline-secondary" onclick={() => navigator.clipboard?.writeText(activationLink.url)}>{t('linuxUsers.copy')}</button>
    </div>
  {/if}
</Modal>
